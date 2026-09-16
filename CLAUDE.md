# fixcomap/platform

Base de infraestructura y prueba de concepto de cómo trabajamos dos DevOps.
El producto aún no está definido.

## Contexto

- GCP: proyecto `fixcomap-core`, región `europe-west1`. Free Trial ($300) hasta el 16/12/2026.
- GitHub: org `fixcomap`, repo `fixcomap/platform`, **público** desde el 16/09/2026 (rulesets y required
  reviewers activos; Actions sin límite). Aun así: minimizar runs, agrupar cambios. Landing en `fixcomap/web`.
- Dominio `fixcomap.com`, DNS en Cloudflare. App en `app.fixcomap.com`; landing estática (repo `fixcomap/web`) en Cloudflare Pages en el apex y `www`; aquí solo Pages+DNS (`infra/dns`).
- IaC: OpenTofu (`tofu`, nunca `terraform`). Provider google `~> 8.3`, cloudflare `~> 5.25`.

## Reglas no negociables

1. El coste manda. Nada fuera del free tier sin justificar antes con coste mensual estimado.
   Vigilar: Cloud SQL, load balancers, NAT Gateway, IPs externas estáticas, Kubernetes.
2. Cero claves de servicio JSON. GitHub Actions entra en GCP por Workload Identity Federation (OIDC).
   Si algo parece necesitar una clave de larga duración, el enfoque está mal.
   Única excepción: el token de API de Cloudflare (no federa OIDC), scoped a `Zone > DNS > Edit`
   de `fixcomap.com` y guardado como secret del environment `platform`.
   El repo es posterior al 15/07/2026: el `sub` OIDC es `repo:ORG@ID/REPO@ID:...`. Nunca bindear por `sub` literal;
   el environment se extrae del `sub` en el attribute mapping (`infra/bootstrap/wif.tf`).
3. Todo se aplica por pipeline. Cada capa con su propia SA y permisos mínimos:
   - L0 `infra/bootstrap/`: se aplica UNA vez desde Cloud Shell; luego lo gestiona el pipeline con `tf-platform`.
   - L1 `infra/platform/`: pipeline con `tf-platform`, environment `platform` con aprobación humana.
   - L2 `infra/app/`: pipeline con `gh-deployer`. Nunca toca IAM.
   - `infra/dns/`: pipeline con el token de Cloudflare (secret del environment `platform`).
   - `tf-plan`: solo lectura (`roles/viewer`), cualquier rama, para plan en PR y drift.
   Todo el despliegue va en `deploy.yml` (jobs `platform-plan` → `platform-apply` → `app`), en cada push a
   `main`. Ningún job crea IAM salvo `platform-apply`.
4. `tofu plan` es el criterio de verdad. Nada está hecho hasta que el plan sale limpio.
5. Secretos y estado fuera del repo (buckets `fixcomap-core-tfstate` y `-app`, Secret Manager, GitHub secrets).

## Cómo trabajar aquí

- Verificar versión y atributos del provider contra la documentación de esa versión, no de memoria.
- Comentarios en castellano explicando el porqué, no el qué.
- Nada de módulos ni abstracciones hasta que haya repetición real.
- Al terminar cambios de infra: `tofu fmt`, `tofu validate`, `tofu plan`. Nunca `apply` desde una máquina local.
- GitFlow solo como convención de ramas: `feature/*` → `develop`; `release/*` y `hotfix/*` → `main`. Ver `CONTRIBUTING.md`.
- Versión en `VERSION`; el tag `vX.Y.Z` lo crea el job `app` de `deploy.yml` al mergear en `main`.
- Actions pineadas por commit SHA con comentario `# vX.Y.Z`; Renovate las mantiene (`renovate.json`).
- Plan offline (sin credenciales): `backend_override.tf` local + `GOOGLE_OAUTH_ACCESS_TOKEN=offline`. Ver `CONTRIBUTING.md`.
- `cost-guard.yml` bloquea recursos de coste fijo en PR salvo etiqueta `cost-approved` + "Coste estimado:".
