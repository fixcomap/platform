#!/usr/bin/env bash
# Higiene del repo: lo que nunca se versiona en fixcomap. Lo corren el job
# higiene de cicd.yml (todos los repos de proyecto) y pr-checks.yml (platform).
#
#   - configuración de herramientas de IA (CLAUDE.md, .claude/, AGENTS.md, .cursor…)
#   - .env (salvo .env.example), estado y planes de OpenTofu, .terraform/
#   - node_modules/, .DS_Store / Thumbs.db
#   - binarios ejecutables (Mach-O, ELF, PE) y ficheros de más de MAX_MB
#   - en una PR (BASE_SHA..HEAD_SHA): commits con co-autor o firma de IA
#
# Uso: scripts/higiene.sh  (desde la raíz del repo, con el historial de la PR)
set -euo pipefail

MAX_MB="${MAX_MB:-5}"
err=0
fail() { echo "::error::$1"; err=1; }

prohibidos='(^|/)(CLAUDE(\.local)?\.md|AGENTS\.md|GEMINI\.md|\.claude|\.cursor|\.cursorrules|\.windsurf|\.windsurfrules|\.aider[^/]*|\.codex|\.continue)(/|$)'
prohibidos+='|(^|/)\.github/copilot-instructions\.md$'
prohibidos+='|(^|/)\.env(\.[^/]*)?$'
prohibidos+='|(^|/)(node_modules|\.terraform)/|\.tfstate(\.backup)?$|(^|/)tfplan$'
prohibidos+='|(^|/)(\.DS_Store|Thumbs\.db)$'

while IFS= read -r f; do
  fail "$f no se versiona (ver scripts/higiene.sh de fixcomap/platform)"
done < <(git ls-files | grep -E "$prohibidos" | grep -vE '(^|/)\.env\.example$' || true)

while IFS= read -r f; do
  [ -f "$f" ] || continue
  bytes=$(wc -c < "$f")
  if [ "$bytes" -gt $((MAX_MB * 1024 * 1024)) ]; then
    fail "$f pesa $((bytes / 1024 / 1024)) MB (máximo ${MAX_MB} MB)"
  fi
  case "$(file -b --mime-type "$f")" in
    application/x-mach-binary|application/x-executable|application/x-pie-executable|application/x-sharedlib|application/x-dosexec)
      fail "$f es un binario ejecutable: se construye en el pipeline, no se versiona" ;;
  esac
done < <(git ls-files)

if [ -n "${BASE_SHA:-}" ] && [ -n "${HEAD_SHA:-}" ]; then
  firmas='co-authored-by:.*(claude|anthropic|copilot|cursor|openai|chatgpt|gemini)|generated with \[?(claude|copilot|cursor|chatgpt)|noreply@anthropic'
  for c in $(git rev-list "$BASE_SHA..$HEAD_SHA"); do
    if git log -1 --format=%B "$c" | grep -qiE "$firmas"; then
      fail "commit ${c:0:12} con co-autor o firma de IA: los commits los firma solo quien los hace"
    fi
  done
fi

[ "$err" -eq 0 ] && echo "higiene: ok"
exit "$err"
