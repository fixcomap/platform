#!/usr/bin/env bash
# Aplica a uno o varios repos de la org la configuración estándar de GitHub:
#
#   rulesets/develop.json, main.json   PR + checks + sin force-push ni borrado; sin bypass
#   rulesets/main-reviews.json         1 aprobación en main; bypass solo para admin
#                                      (se salta reviewers, nunca checks)
#   checks/<repo>.json                 checks obligatorios (default.json: "cicd / ok")
#   environment preview | development  sin aprobación ni restricción de rama (static | webapp)
#   environment production             solo si falta: aprobación del admin, ramas protegidas
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
  existing=$(gh api "repos/$ORG/$repo/rulesets" --jq '.[] | select(.source_type == "Repository") | "\(.name)\t\(.id)"')

  for f in "$DIR"/rulesets/*.json; do
    name=$(jq -r .name "$f")
    # BOOTSTRAP=1: rulesets desactivados para el push inicial de un repo nuevo
    # (crear develop y main sin PR); después, apply.sh sin BOOTSTRAP los activa.
    body=$(jq --slurpfile c "$checks" --arg enf "${BOOTSTRAP:+disabled}" '
      .rules |= map(if .type == "required_status_checks"
        then .parameters.required_status_checks = ($c[0] | map({context: .}))
        else . end)
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

  # platform no tiene previews ni producción propia: su único environment es "platform".
  [ "$repo" != platform ] || continue

  # Entorno sin protección según el tipo del catálogo: previews de Pages (static)
  # o el servicio <service>-dev (webapp).
  open_env=preview
  grep -q '^type: webapp' "$DIR/../catalog/$repo.yaml" 2>/dev/null && open_env=development
  write PUT "repos/$ORG/$repo/environments/$open_env" '{"wait_timer": 0, "reviewers": [], "deployment_branch_policy": null}'
  echo "$repo: environment $open_env abierto"

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
