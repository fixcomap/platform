# Cómo contribuir

GitFlow como convención de ramas, un solo entorno. Todo lo que llega a `main` se despliega.

## Ramas

| Rama | Sale de | Vuelve a | Despliega |
|---|---|---|---|
| `main` | — | — | Sí: `deploy.yml` (`platform-apply` con aprobación solo si hay cambios; `app` siempre) |
| `develop` | `main` | — | No. Solo checks |
| `feature/<nombre>` | `develop` | `develop` | No |
| `release/<x.y.z>` | `develop` | `main` y de vuelta a `develop` | Al mergear en `main` |
| `hotfix/<x.y.z>` | `main` | `main` y de vuelta a `develop` | Al mergear en `main` |

`main` solo acepta PRs desde `release/*` y `hotfix/*`; `develop` desde `feature/*`, `bugfix/*`,
`release/*` y `hotfix/*`. Lo comprueba el job `gitflow` de `pr-checks.yml`.

Rama por defecto del repo: `develop`. Merges siempre con merge commit (no squash, no rebase): el
back-merge de `release/*` a `develop` depende de que los commits sean los mismos.

## Flujo

```sh
# Feature
git switch develop && git pull
git switch -c feature/nombre-corto
# ... commits ...
git push -u origin feature/nombre-corto
gh pr create --base develop --fill

# Release: sube VERSION, PR a main, y tras el merge back-merge a develop
git switch develop && git pull
git switch -c release/0.2.0
printf '0.2.0\n' > VERSION
git commit -am "chore(release): 0.2.0"
git push -u origin release/0.2.0
gh pr create --base main --title "release: 0.2.0" --body ""
# tras el merge en main:
gh pr create --base develop --head release/0.2.0 --title "merge back: release/0.2.0" --body ""

# Hotfix
git switch main && git pull
git switch -c hotfix/0.2.1
printf '0.2.1\n' > VERSION
# ... fix ...
git push -u origin hotfix/0.2.1
gh pr create --base main --fill
# tras el merge en main:
gh pr create --base develop --head hotfix/0.2.1 --fill
```

El tag `vX.Y.Z` lo crea el job `app` de `deploy.yml` a partir de `VERSION` al mergear en `main`, si no existe.
No se crean tags a mano.

## Checks en cada PR (`pr-checks.yml`)

- `gitflow`: rama origen válida para la rama destino.
- `app`: `gofmt`, `go vet`, `golangci-lint`, `go test -race`.
- `security`: `gitleaks` (historial completo), `trivy fs` (vuln + secret), `trivy config` sobre `infra/`.
- `tofu (bootstrap|platform|app|dns|observability)`: `fmt -check`, `validate`, `tflint`, `checkov`, y `tofu plan` con
  `tf-plan` (solo lectura, `-lock=false`), publicado como comentario en el PR (uno por capa, se actualiza
  en cada push). `dns` y `observability` no tienen plan en PR (sus tokens solo existen en el environment `platform`). Los PRs
  desde forks no obtienen token OIDC: en ellos el plan se omite. Si `tf-plan` aún no existe (arranque
  en frío, antes del primer apply de L1) el job pasa con aviso y sin plan; ver README.
- `cost-guard`: falla si `infra/` contiene tipos de recurso con coste fijo (Cloud SQL, LB, NAT, IPs,
  GKE, KMS…) sin la etiqueta `cost-approved` y una línea `Coste estimado: …` en el cuerpo del PR; y si
  Cloud Run deja de escalar a cero o de facturar por petición.

Un check en rojo bloquea el merge. Un `checkov` que se quiera silenciar lleva `# checkov:skip=ID: motivo`
en el recurso, con el motivo de verdad.

## Qué pasa al mergear en `main`

`deploy.yml` corre en **cada** push a `main`, sin filtro de paths (una release garantiza que todo está
aplicado, no solo lo que tocó ese merge). Tres jobs en secuencia:

1. `platform-plan` (`tf-plan`, solo lectura): plan de L0 y L1 con `-detailed-exitcode`. Decide si hace
   falta aplicar: cambios pendientes, arranque en frío (`tf-plan` no existe), `infra/dns/**` o `infra/observability/**` tocados o
   `workflow_dispatch`. Sin nada de eso, `platform-apply` se salta **sin pedir aprobación**.
2. `platform-apply` (`tf-platform`, environment `platform`): espera aprobación —el revisor ya tiene el
   plan en el step summary del job anterior—, vuelve a planificar con su identidad y aplica L0, L1 y,
   si cambiaron, `dns` (token de Cloudflare del environment) y `observability` (token de Grafana del environment).
3. `app` (`gh-deployer`): solo si `platform-apply` terminó bien o se saltó. Build, `trivy image`, push a
   Artifact Registry, firma keyless con `cosign`, `tofu apply` de L2, tag `vVERSION` si no existe.
   Si la imagen y L2 no cambian, el apply es no-op.
- `drift.yml` corre cada noche a las 04:00 UTC: `plan -detailed-exitcode` en L0, L1 y L2 y abre un
  issue `[drift] infra/<capa>` (etiqueta `drift`) si hay cambios. `dns` y `observability` quedan fuera (sin token).
- `cost-guard.yml` (05:00 UTC) inventaría con `tf-plan` recursos con coste fijo y el tamaño de Artifact
  Registry; abre `[cost] recursos fuera del free tier` (etiqueta `cost`) si encuentra algo.
- Renovate abre PRs contra `develop` los lunes: SHA de Actions (con `# vX.Y.Z`), providers (minor/patch),
  versiones de `tofu`/`tflint`/`gitleaks`/`wrangler`, Go e imagen base. Los major de providers se hacen a
  mano con la guía de upgrade delante.

## Pipelines de los sitios estáticos (`rw-*.yml`)

Los repos de sitios estáticos de la org (`fixcomap/web`, portfolios) no tienen pipeline propio: llaman
a dos *reusable workflows* de este repo con un `ci.yml` de pocas líneas. La lógica, los pines y Renovate
viven aquí una sola vez; cambiar el pipeline es una PR aquí, no una por repo.

- `rw-static-checks.yml`: gitflow (solo en PR), gitleaks + trivy, build opcional, W3C Nu, stylelint si
  el repo lo define, CSP en `_headers`, aviso por assets locales que faltan.
- `rw-pages-deploy.yml`: `wrangler pages deploy` al proyecto indicado. **El mismo job corre en PR
  (preview `<rama>.<proyecto>.pages.dev`), en `develop` (preview) y en `main` (producción)**, y siempre
  termina con un smoke test. Si el pipeline está roto, se ve en la PR, no tras el merge. El environment
  (`preview`/`production`) y los secrets de Cloudflare son del repo caller (o de la org).

Caller mínimo (`fixcomap/web/.github/workflows/ci.yml`):

```yaml
on:
  pull_request: { branches: [develop, main] }
  push: { branches: [develop, main] }
permissions: {}
concurrency: { group: ci-${{ github.head_ref || github.ref_name }}, cancel-in-progress: false }
jobs:
  checks:
    uses: fixcomap/platform/.github/workflows/rw-static-checks.yml@main
    with: { site_dir: public }
  deploy:
    needs: checks
    uses: fixcomap/platform/.github/workflows/rw-pages-deploy.yml@main
    with:
      project_name: fixcomap-landing
      site_dir: public
      environment: ${{ github.ref_name == 'main' && 'production' || 'preview' }}
      production_url: https://fixcomap.com
    secrets: inherit
```

Los callers apuntan a `@main`: un cambio en `rw-*` llega a los sitios cuando se hace release aquí. Para
probar un cambio antes, el caller de una PR de `web` puede apuntar temporalmente a `@feature/<rama>`.
El proyecto de Pages y sus dominios se declaran en `infra/dns/pages.tf` (import del proyecto que crea
`wrangler` la primera vez).

## Runbook: cuando algo está en rojo

Premisa: **un pipeline roto no tira producción**. Pages y Cloud Run son atómicos: si el deploy falla,
sigue sirviendo la versión anterior. Lo que se pierde es poder desplegar, y para eso está esto.

| Síntoma | Qué mirar | Qué hacer |
|---|---|---|
| `deploy` rojo en `platform-plan`/`platform-apply` | step summary del job: el plan | Si el error es del provider (API, cuota) → `gh run rerun <id> --failed`. Si el plan quiere destruir algo inesperado → **no aprobar**, abrir hotfix. |
| `deploy` rojo en `app` | `trivy image` (CVE nueva en la base) o `tofu apply` L2 | CVE: PR bumpeando imagen base/Go. Apply: leer el error; L2 no toca IAM, nada que romper fuera de Cloud Run. |
| `deploy` de `web`/portfolio rojo | job `pages`: build, wrangler o smoke test | Build/lint: arreglar en PR. Wrangler 401/403: token de Cloudflare revocado → regenerar (Account > Pages > Edit + Zone > DNS > Edit) y actualizar el secret. Smoke test: ver la URL de Pages del summary. |
| Landing/app caída (alerta de Cloud Monitoring) | último deploy en Actions | Web: `npx wrangler pages deployment list --project-name fixcomap-landing` y `npx wrangler pages deployment rollback <id>`. App: `gcloud run services update-traffic app --region europe-west1 --to-revisions <anterior>=100`. |
| Issue `[drift] infra/<capa>` | el plan del issue | Alguien tocó a mano. Si el cambio es deseado: llevarlo a código en PR. Si no: aprobar el `deploy` de la siguiente release, que lo revierte. |
| Issue `[cost]` | inventario del issue | Borrar el recurso vía PR (nunca a mano) o justificarlo con `cost-approved` + coste mensual. |
| `drift`/`cost-guard` rojo por auth (`tf-plan` no puede autenticar) | WIF pool/provider (L0) o binding de `tf-plan` (L1) | Solo `tf-platform` lo arregla desde `main`; si L0 rompió la identidad del pipeline, Cloud Shell (README). |
| Aprobación pendiente y el aprobador no está | environment `platform` / `production` | Cualquiera de los reviewers configurados aprueba: `gh api -X POST repos/<repo>/actions/runs/<id>/pending_deployments -F 'environment_ids[]=<id>' -f state=approved -f comment=ok`. |
| Todo `app.` parado tras el 16/12/2026 | Billing | Free Trial expirado sin activar la cuenta. Activar en Billing → *Activate full account*; los recursos vuelven solos. Hacerlo **antes**. |

## Trabajo local con OpenTofu

Nunca `apply` ni credenciales de GCP en local: el estado lo escribe el pipeline. En local se revisa
el plan **offline**: backend local temporal y un token ficticio para que el provider no exija
credenciales. El plan muestra todo como "to add" (no hay estado), que es lo que interesa para revisar
qué crea una capa. `backend_override.tf` está en `.gitignore`.

```sh
cd infra/platform          # o bootstrap, app, dns, observability
printf 'terraform {\n  backend "local" {}\n}\n' > backend_override.tf
tofu init -input=false
GOOGLE_OAUTH_ACCESS_TOKEN=offline tofu plan -var project_number=000000000000
rm -f backend_override.tf && rm -rf .terraform terraform.tfstate*
```

Variables sin default por capa: `platform` → `project_number`; `app` → `image`; `dns` →
`cloudflare_zone_id` (y `CLOUDFLARE_API_TOKEN=offline` en el entorno).

Mismos linters que CI, en local:

```sh
tofu fmt -check -recursive infra
tflint --init --config="$PWD/.tflint.hcl"
tflint --chdir=infra/platform --config="$PWD/.tflint.hcl"
checkov -d infra --framework terraform --quiet --compact
trivy config --severity HIGH,CRITICAL infra
gitleaks git . --no-banner --redact
actionlint
```

## Configuración del repositorio en GitHub (una vez)

```sh
R=fixcomap/platform

# develop desde main y como rama por defecto
git switch main && git switch -c develop && git push -u origin develop
gh repo edit $R --default-branch develop --enable-merge-commit --enable-squash-merge=false --enable-rebase-merge=false --delete-branch-on-merge

# Rulesets: PR obligatorio, 1 aprobación, checks en verde, sin push directo ni borrado
for f in main develop tags; do gh api -X POST repos/$R/rulesets --input .github/rulesets/$f.json; done
```

`develop` exige PR y checks en verde pero **0 aprobaciones** (el trabajo diario fluye); `main` exige
**1 aprobación** del otro DevOps (solo releases y hotfixes). Los rulesets no tienen `bypass_actors`: tampoco los
admins pueden hacer push directo. Los nombres de
los checks requeridos son los `name:` de los jobs de `pr-checks.yml`; si se renombra un job hay que
actualizar `.github/rulesets/*.json` y reaplicar con `gh api -X PUT repos/$R/rulesets/<id>`.

## Commits

Una línea, sin cuerpo: `<tipo>(<ámbito>): <descripción en minúsculas>`. Ejemplo:
`feat(platform): cleanup policies en artifact registry`. Las Actions se referencian por commit SHA con
`# vX.Y.Z` al lado; nunca por tag.
