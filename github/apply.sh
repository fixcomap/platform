#!/usr/bin/env bash
# Aplica a uno o varios repos de la org la configuración estándar de GitHub:
#
#   rulesets/develop.json, main.json   PR + checks + sin force-push ni borrado; sin bypass
#   rulesets/main-reviews.json         1 aprobación en main; bypass solo para admin
#                                      (se salta reviewers, nunca checks)
#   checks/<repo>.json                 checks obligatorios (default.json: "cicd / ok")
#   environment preview                sin aprobación, solo desde develop (static)
#   environment development            sin aprobación; solo despliega desde develop (webapp)
#   environment production             solo si falta: aprobación del admin, ramas protegidas
#   acciones-permitidas.json           solo acciones de GitHub, de fixcomap y las que usa el pipeline
#   CodeQL (repos públicos)            configuración por defecto; el merge se bloquea con alertas altas
#   PR de forks                        sus workflows esperan aprobación siempre (no solo la primera vez)
#
# Idempotente: crea o actualiza cada ruleset por nombre. Un production que ya
# existe no se toca (sus reviewers son decisión de cada repo).
# Lo ejecuta un admin de la org a mano; CI no tiene permisos para esto.
#
#   github/apply.sh elias agustin web platform
#   DRY_RUN=1 github/apply.sh web     muestra lo que haría, sin escribir
#   BOOTSTRAP=1 github/apply.sh x     repo nuevo: rulesets desactivados hasta el push inicial
set -euo pipefail

ORG=fixcomap
DIR="$(cd "$(dirname "$0")" && pwd)"

[ $# -gt 0 ] || { echo "uso: $0 <repo>..." >&2; exit 1; }

write() { # write <método> <path> <body>
  if [ -n "${DRY_RUN:-}" ]; then
    echo "--- $1 $2"; jq -c . <<<"$3"
  else
    gh api -X "$1" "$2" --input - <<<"$3" >/dev/null
  fi
}

for repo in "$@"; do
  checks="$DIR/checks/$repo.json"
  [ -f "$checks" ] || checks="$DIR/checks/default.json"
  # Rulesets y code scanning (CodeQL) solo en repos públicos: en los privados el plan
  # gratuito no tiene rulesets y CodeQL exige Advanced Security (de pago).
  publico=$(gh api "repos/$ORG/$repo" --jq '.visibility == "public"')
  existing=""
  [ "$publico" != true ] || existing=$(gh api "repos/$ORG/$repo/rulesets" --jq '.[] | select(.source_type == "Repository") | "\(.name)\t\(.id)"')
  [ "$publico" = true ] || echo "$repo: privado, sin rulesets (plan gratuito)"

  for f in "$DIR"/rulesets/*.json; do
    [ "$publico" = true ] || break
    name=$(jq -r .name "$f")
    # BOOTSTRAP=1: rulesets desactivados para el push inicial de un repo nuevo
    # (crear develop y main sin PR); después, apply.sh sin BOOTSTRAP los activa.
    body=$(jq --slurpfile c "$checks" --arg enf "${BOOTSTRAP:+disabled}" --arg pub "$publico" '
      .rules |= map(if .type == "required_status_checks"
        then .parameters.required_status_checks = ($c[0] | map({context: .}))
        else . end)
      | if $pub != "true" then .rules |= map(select(.type != "code_scanning")) else . end
      | if $enf != "" then .enforcement = $enf else . end' "$f")
    id=$(awk -F'\t' -v n="$name" '$1 == n {print $2}' <<<"$existing")
    if [ -n "$id" ]; then
      write PUT "repos/$ORG/$repo/rulesets/$id" "$body"
      echo "$repo: ruleset $name actualizado"
    else
      write POST "repos/$ORG/$repo/rulesets" "$body"
      echo "$repo: ruleset $name creado"
    fi
  done

  # CodeQL con la configuración por defecto (lenguajes detectados por GitHub):
  # analiza cada PR y cada push; los rulesets bloquean el merge con alertas altas.
  if [ "$publico" = true ]; then
    write PATCH "repos/$ORG/$repo/code-scanning/default-setup" '{"state": "configured", "query_suite": "default"}'
    echo "$repo: CodeQL activado"
  else
    echo "$repo: privado, sin CodeQL (requiere Advanced Security)"
  fi

  # Acciones: solo las de la lista (el pipeline y sus pasos); el resto no corre.
  write PUT "repos/$ORG/$repo/actions/permissions" '{"enabled": true, "allowed_actions": "selected"}'
  write PUT "repos/$ORG/$repo/actions/permissions/selected-actions" "$(cat "$DIR/acciones-permitidas.json")"
  echo "$repo: acciones limitadas a acciones-permitidas.json"
  # Un PR desde un fork no ejecuta nada hasta que un admin lo aprueba.
  write PUT "repos/$ORG/$repo/actions/permissions/fork-pr-contributor-approval" '{"approval_policy": "all_external_contributors"}'
  echo "$repo: workflows de PR de forks con aprobación siempre"

  # platform no tiene previews ni producción propia: su único environment es "platform".
  [ "$repo" != platform ] || continue

  # Entorno sin protección según el tipo del catálogo: previews de Pages (static)
  # o el servicio <service>-dev (webapp).
  if grep -q '^type: webapp' "$DIR/../catalog/$repo.yaml" 2>/dev/null; then
    # development solo desde develop: otra rama no puede usar sus secretos.
    write PUT "repos/$ORG/$repo/environments/development" '{"wait_timer": 0, "reviewers": [], "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}}'
    if [ -n "${DRY_RUN:-}" ] || ! gh api "repos/$ORG/$repo/environments/development/deployment-branch-policies" --jq '.branch_policies[].name' | grep -qx develop; then
      write POST "repos/$ORG/$repo/environments/development/deployment-branch-policies" '{"name": "develop", "type": "branch"}'
    fi
    echo "$repo: environment development sin aprobación, solo desde develop"
  else
    # preview solo desde develop: una rama cualquiera no puede usar el token de Pages.
    write PUT "repos/$ORG/$repo/environments/preview" '{"wait_timer": 0, "reviewers": [], "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}}'
    if [ -n "${DRY_RUN:-}" ] || ! gh api "repos/$ORG/$repo/environments/preview/deployment-branch-policies" --jq '.branch_policies[].name' | grep -qx develop; then
      write POST "repos/$ORG/$repo/environments/preview/deployment-branch-policies" '{"name": "develop", "type": "branch"}'
    fi
    echo "$repo: environment preview sin aprobación, solo desde develop"
  fi

  # production solo se crea si falta (sus reviewers son decisión de cada repo).
  # Sin esto, el primer deploy a main lo crearía GitHub sin aprobación.
  if gh api "repos/$ORG/$repo/environments/production" >/dev/null 2>&1; then
    echo "$repo: environment production ya existe, no se toca"
  else
    me=$(gh api user --jq .id)
    write PUT "repos/$ORG/$repo/environments/production" "{\"wait_timer\": 0, \"reviewers\": [{\"type\": \"User\", \"id\": $me}], \"deployment_branch_policy\": {\"protected_branches\": true, \"custom_branch_policies\": false}}"
    echo "$repo: environment production creado (aprobación del admin, solo ramas protegidas)"
  fi
done
