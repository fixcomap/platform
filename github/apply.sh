#!/usr/bin/env bash
# Aplica a uno o varios repos de la org la configuración estándar de GitHub:
#
#   rulesets/develop.json, main.json   PR + checks + sin force-push ni borrado; sin bypass
#   rulesets/main-reviews.json         1 aprobación en main; bypass solo para admin
#                                      (se salta reviewers, nunca checks)
#   checks/<repo>.json                 checks obligatorios (default.json: "cicd / ok")
#   environment preview                sin aprobación ni restricción de rama (previews de PR)
#
# Idempotente: crea o actualiza cada ruleset por nombre. No toca el environment
# production (reviewers y rama protegida son decisión de cada repo).
# Lo ejecuta un admin de la org a mano; CI no tiene permisos para esto.
#
#   github/apply.sh elias agustin web platform
#   DRY_RUN=1 github/apply.sh web     muestra lo que haría, sin escribir
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
    body=$(jq --slurpfile c "$checks" '
      .rules |= map(if .type == "required_status_checks"
        then .parameters.required_status_checks = ($c[0] | map({context: .}))
        else . end)' "$f")
    id=$(awk -F'\t' -v n="$name" '$1 == n {print $2}' <<<"$existing")
    if [ -n "$id" ]; then
      write PUT "repos/$ORG/$repo/rulesets/$id" "$body"
      echo "$repo: ruleset $name actualizado"
    else
      write POST "repos/$ORG/$repo/rulesets" "$body"
      echo "$repo: ruleset $name creado"
    fi
  done

  # platform no tiene previews: su único environment es "platform".
  if [ "$repo" != platform ]; then
    write PUT "repos/$ORG/$repo/environments/preview" '{"wait_timer": 0, "reviewers": [], "deployment_branch_policy": null}'
    echo "$repo: environment preview abierto"
  fi
done
