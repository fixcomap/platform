# fixcomap/platform

Infraestructura base de fixcomap en GCP (`fixcomap-core`, `europe-west1`) gestionada con OpenTofu
y desplegada íntegramente por GitHub Actions mediante Workload Identity Federation. Sin claves JSON.

## Capas

| Capa | Directorio | Identidad que aplica | Cuándo | Contiene |
|---|---|---|---|---|
| L0 | `infra/bootstrap/` | Persona (Cloud Shell) la primera vez; después `tf-platform` | `deploy.yml` → `platform-apply`, con aprobación | Bucket de estado, APIs mínimas, WIF pool + provider, SA `tf-platform` |
| L1 | `infra/platform/` | `tf-platform` | `deploy.yml` → `platform-apply`, con aprobación | APIs, Artifact Registry, SAs `gh-deployer` / `app-runtime` / `tf-plan`, IAM, Secret Manager |
| L2 | `infra/app/` | `gh-deployer` | `deploy.yml` → `app`, automático en `main` | Cloud Run, domain mapping |
| dns | `infra/dns/` | Token de Cloudflare (secret del environment `platform`) | `deploy.yml` → `platform-apply`, con aprobación | CNAME `app.fixcomap.com` |

Estado: bucket `fixcomap-core-tfstate`, prefijos `bootstrap/`, `platform/`, `app/`, `dns/`.

`www.fixcomap.com` y el apex quedan libres para la landing.

### Identidades y condiciones OIDC

| SA | Puede | Token OIDC aceptado |
|---|---|---|
| `tf-platform` | Crear IAM, SAs, APIs, AR, secretos, estado de todas las capas | `repository == fixcomap/platform` **y** `ref == refs/heads/main` **y** `environment == platform` |
| `gh-deployer` | Push a AR, `run.developer`, actAs `app-runtime`, estado solo `app/` | `repository == fixcomap/platform` **y** `ref == refs/heads/main` |
| `tf-plan` | `roles/viewer`, leer estado de todas las capas | `repository == fixcomap/platform`, cualquier rama |
| `app-runtime` | Leer el secreto `app-config` | No la suplanta nadie; es la identidad del contenedor |

El provider OIDC rechaza en origen cualquier token cuyo `repository_owner` no sea `fixcomap`.
Las condiciones compuestas se materializan como atributos mapeados (`repository_ref`, `sub_ref`)
porque un binding `principalSet://` solo puede filtrar por un atributo. Ver `infra/bootstrap/wif.tf`.

### La única credencial de larga duración

Cloudflare no acepta tokens OIDC de GitHub, así que el CNAME de `app.fixcomap.com` necesita un token
de API. Es la única excepción a "cero credenciales largas" y se acota así:

- Permiso `Zone > DNS > Edit` limitado a la zona `fixcomap.com`. Nada más.
- Guardado como **secret del environment `platform`**, no del repositorio: solo un job con
  `environment: platform`, es decir aprobado por una persona, puede leerlo. Ni los PRs ni `drift.yml`
  ni el job `app` lo ven.
- Consecuencia: `infra/dns/` se aplica en el job `platform-apply` (no en `app`), no tiene
  plan en PR ni detección de drift nocturna. Cloudflare conserva su propio audit log.
- Rotación: crear token nuevo en Cloudflare, `gh secret set CLOUDFLARE_API_TOKEN --env platform`,
  revocar el viejo.

### Formato del `sub` OIDC de GitHub (repos posteriores al 15/07/2026)

Este repo se creó el 16/09/2026, así que GitHub emite el `sub` en su formato "inmutable":
`repo:fixcomap@329960363/platform@1372908880:environment:platform` (nombre `@` ID), en vez de
`repo:fixcomap/platform:environment:platform`. Los claims `repository`, `ref` y `repository_owner`
siguen siendo por nombre. Consecuencias en `infra/bootstrap/wif.tf`:

- Ningún binding usa `assertion.sub` literal. El environment se extrae del `sub` con `contains`/`split`
  (funciona con ambos formatos: los dos terminan en `:environment:NOMBRE`).
- `attribute_condition` exige nombre **e** ID de la organización (`repository_owner_id`), que no se recicla.
- Atributos mapeados: `repository` (tf-plan), `repository_ref` = `repo:ref` (gh-deployer),
  `repo_ref_env` = `repo:ref:environment:NOMBRE` (tf-platform). Separador `:`.

Si `platform-apply` falla con `IAM_PERMISSION_DENIED` en `iam.serviceAccounts.getAccessToken` y la SA
existe, el token pasó STS (condición del provider) pero ningún `principalSet` coincide: comparar el `sub`
real con el atributo mapeado antes de tocar nada.

## Por qué L0 es irreducible

OpenTofu no puede crear el sitio donde guarda su propio estado ni la identidad con la que se autentica.
Alguien tiene que crear, una vez, el bucket, el pool de WIF y la SA `tf-platform`; hasta que existen,
el pipeline no tiene ni dónde leer el estado ni con qué identidad hacerlo. Ese conjunto es L0 y es lo
mínimo: cualquier cosa que pueda esperar a que exista `tf-platform` está en L1.

Se aplica desde Cloud Shell y no desde un portátil porque Cloud Shell usa la identidad de Google de la
persona (sin ficheros de credenciales en disco), deja rastro en Cloud Audit Logs y no depende de qué
haya instalado cada uno. Tras el primer apply, el estado se migra al bucket y L0 pasa a gestionarse por
pipeline con `tf-platform` como cualquier otra capa. Nadie vuelve a aplicar desde una consola.

### Cuándo L0 vuelve a Cloud Shell

Solo cuando L0 rompe la identidad del propio pipeline: si `tf-platform` no puede autenticarse (binding o
mapping de WIF incorrectos), ningún job puede aplicar la corrección. Es el único caso en que se repite el
procedimiento de Cloud Shell, esta vez contra el estado ya migrado al bucket (sin `-backend=false`, sin
`-migrate-state`). Ver "L0 desde Cloud Shell (estado ya en el bucket)".

## Por qué el apply de L1 lleva aprobación humana

`tf-platform` tiene `resourcemanager.projectIamAdmin` y `iam.serviceAccountAdmin`: puede concederse a
sí misma, o a cualquier otra identidad, cualquier rol del proyecto. Es Owner de facto. Un PR mergeado en
`main` que modifique `infra/platform/` es, por tanto, un cambio de quién puede hacer qué en el proyecto,
y eso no lo decide un merge automático. El environment `platform` de GitHub exige que otra persona
apruebe la ejecución, y la condición OIDC de `tf-platform` hace que ese environment sea la única vía
para obtener su token: un workflow sin `environment: platform` no consigue credenciales aunque lo
intente. `gh-deployer` no tiene ningún permiso IAM, así que el job `app` puede ser automático.

La aprobación se pide **solo cuando hace falta**: `platform-plan` (solo lectura) detecta antes si hay
cambios en L0/L1, si `tf-plan` aún no existe o si cambió `infra/dns/`; si no, `platform-apply` se salta.
El revisor aprueba con el plan ya publicado en el step summary.

Si `platform-plan` **no puede** planificar (a `tf-plan` le falta un permiso que justo corrige L1, por
ejemplo), el push queda en rojo y el escape es `gh workflow run deploy.yml --ref main`: en
`workflow_dispatch` se omite el plan previo y `platform-apply` planifica con `tf-platform` y espera
aprobación como siempre.

## Primera puesta en marcha (una sola vez)

### 1. Prerrequisitos fuera de este repo

- Proyecto `fixcomap-core` creado y vinculado a la cuenta de facturación. Comprobar:
  ```sh
  gcloud projects describe fixcomap-core --format='value(lifecycleState)'
  gcloud billing projects describe fixcomap-core --format='value(billingEnabled)'
  ```
  Si `billingEnabled` es `False`: `gcloud billing projects link fixcomap-core --billing-account=<ID>`.
- Token de API de Cloudflare con permiso `Zone > DNS > Edit` limitado a la zona `fixcomap.com`,
  y el Zone ID de la zona (Overview de la zona en el dashboard).

### 2. L0 desde Cloud Shell

Abrir <https://shell.cloud.google.com> con la cuenta que es Owner de `fixcomap-core`. Ejecutar en orden;
cada bloque se puede pegar entero.

```sh
# 1. Proyecto activo y comprobación de facturación
gcloud config set project fixcomap-core
gcloud billing projects describe fixcomap-core --format='value(billingEnabled)'   # debe ser True
```

```sh
# 2. OpenTofu (no viene en Cloud Shell; se instala en el HOME, que persiste)
mkdir -p "$HOME/bin"
curl -fsSL https://get.opentofu.org/install-opentofu.sh -o /tmp/install-opentofu.sh
chmod +x /tmp/install-opentofu.sh
/tmp/install-opentofu.sh --install-method standalone --opentofu-version 1.12.6 --install-path "$HOME/bin" --symlink-path -
export PATH="$HOME/bin:$PATH"
tofu version   # OpenTofu v1.12.6
```

```sh
# 3. Código (rama main; L0 se aplica siempre desde main)
git clone --branch main https://github.com/fixcomap/platform.git "$HOME/platform"
cd "$HOME/platform/infra/bootstrap"
```

```sh
# 4. Las APIs que gestiona L0, habilitadas antes: la propagación puede tardar más que el apply
gcloud services enable serviceusage.googleapis.com cloudresourcemanager.googleapis.com \
  iam.googleapis.com iamcredentials.googleapis.com sts.googleapis.com storage.googleapis.com
```

```sh
# 5. Primer apply con estado LOCAL: el bucket de estado es uno de los recursos que crea
tofu init -backend=false
tofu plan -out=tfplan      # revisar: 6 project_service, 1 bucket, 1 pool, 1 provider, 1 SA, 6 project_iam_member, 1 bucket_iam_member, 1 sa_iam_member
tofu apply tfplan
```

```sh
# 6. Migrar el estado local al bucket recién creado
cp backend.hcl.example backend.hcl
tofu init -migrate-state -backend-config=backend.hcl    # responder: yes
rm -f terraform.tfstate terraform.tfstate.backup tfplan
tofu plan                                                # debe terminar en "No changes."
```

```sh
# 7. Valores para GitHub
tofu output -raw workload_identity_provider; echo
gcloud projects describe fixcomap-core --format='value(projectNumber)'
```

```sh
# 8. Limpieza: nada de este repo vuelve a aplicarse desde una consola
cd "$HOME" && rm -rf "$HOME/platform"
```

Si el paso 5 falla a mitad (típicamente por propagación de APIs), volver a ejecutar `tofu plan -out=tfplan`
y `tofu apply tfplan`: el estado local ya recoge lo creado. Si algo falla en el paso 6 después de
responder `yes`, el estado está en `gs://fixcomap-core-tfstate/bootstrap/default.tfstate` y el local es
una copia: no repetir el apply.

### 2b. L0 desde Cloud Shell (estado ya en el bucket)

Para corregir L0 cuando el pipeline no puede (ver "Cuándo L0 vuelve a Cloud Shell"):

```sh
gcloud config set project fixcomap-core
export PATH="$HOME/bin:$PATH"; tofu version || echo "instalar tofu: paso 2"

rm -rf "$HOME/platform"
git clone --branch main https://github.com/fixcomap/platform.git "$HOME/platform"
cd "$HOME/platform/infra/bootstrap"

cp backend.hcl.example backend.hcl
tofu init -input=false -backend-config=backend.hcl
tofu plan -out=tfplan        # revisar: solo lo que corrige el problema
tofu apply tfplan

tofu plan                    # "No changes."
cd "$HOME" && rm -rf "$HOME/platform"
```

### 3. Configurar GitHub

Con `gh` autenticado como admin de la organización:

```sh
R=fixcomap/platform

# Variables de repositorio (públicas: identificadores, no secretos)
gh variable set GCP_WIF_PROVIDER   -R $R --body "projects/<NUMERO>/locations/global/workloadIdentityPools/github/providers/github-oidc"
gh variable set GCP_PROJECT_NUMBER -R $R --body "<NUMERO>"

# Environment "platform": required reviewer = el otro DevOps, solo desde ramas protegidas
OTHER_ID=$(gh api users/<login-del-otro-devops> --jq .id)
gh api -X PUT repos/$R/environments/platform --input - <<JSON
{ "reviewers": [ { "type": "User", "id": $OTHER_ID } ],
  "deployment_branch_policy": { "protected_branches": true, "custom_branch_policies": false } }
JSON

# Cloudflare: token (Zone > DNS > Edit, solo fixcomap.com) y zone id, SOLO en el environment
gh secret   set CLOUDFLARE_API_TOKEN -R $R --env platform   # pide el valor por stdin
gh variable set CLOUDFLARE_ZONE_ID   -R $R --env platform --body "<zone id>"
```

Renovate: instalar la GitHub App de Mend Renovate en la organización con acceso a `fixcomap/platform`.
`renovate.json` ya está en el repo (SHA de Actions, providers, Go, imagen base).

Ramas y protección: ver `CONTRIBUTING.md`.

### 4. L1 y L2 por pipeline

**Arranque en frío.** `tf-plan` se crea en L1, y L1 solo se aplica desde `main`. El primer PR no
puede planificar: la federación OIDC funciona, pero suplantar `tf-plan` devuelve `404 NOT_FOUND`
(`Gaia id not found for email tf-plan@...`). `pr-checks.yml` detecta **ese error concreto** en el paso
`tf-plan existe`, termina el job en verde con un aviso en el step summary y publica en el PR
"plan omitido (arranque en frío)". Cualquier otro fallo de autenticación sigue tumbando el check.
Segundo caso: una capa que existe pero **nunca se ha aplicado** no tiene objeto de estado, y OpenTofu
intentaría crear uno vacío (escritura que `tf-plan` no puede hacer). El paso `estado de la capa existe`
comprueba `gs://fixcomap-core-tfstate/<capa>/default.tfstate` y, si falta, omite el plan con aviso.
`drift.yml` hace la misma comprobación.
Secuencia:

1. L0 desde Cloud Shell y variables de GitHub (pasos 2 y 3).
2. Primer PR `release/*` → `main`: checks verdes **sin plan**. Al mergear, `deploy.yml` detecta el arranque
   en frío en `platform-plan`, pide aprobación en `platform-apply` y aplica L0 y L1 (crea `tf-plan`).
3. Desde ese momento todos los PRs planifican. Si vuelve a aparecer el aviso, es que `tf-plan` ha
   desaparecido: `drift.yml` y `cost-guard.yml` fallan a diario en ese caso, no lo silencian.

Mergear en `main` (vía `release/*`). `deploy.yml`: `platform-plan` → `platform-apply` (aprobación en el
environment `platform`; aplica L0 sin cambios, L1 y `dns`) → `app` (build, firma, L2). Ver CONTRIBUTING.

### 5. Dominio `app.fixcomap.com`

El domain mapping de Cloud Run exige que la identidad que lo crea sea propietaria verificada del
dominio. Es un paso manual:

1. `gcloud domains verify fixcomap.com` (abre Search Console; verificar con registro TXT en Cloudflare).
2. En Search Console > `fixcomap.com` > Usuarios y permisos, añadir `gh-deployer@fixcomap-core.iam.gserviceaccount.com` como propietario.
3. PR que cambie `enable_domain_mapping` a `true` en `infra/app/variables.tf`. El plan del PR debe
   mostrar solo el `google_cloud_run_domain_mapping`. El CNAME ya existe desde el primer apply de `dns`.

## Coste

Todo lo que crea este repo está en free tier con la carga actual (cero tráfico):

| Recurso | Free tier | Fuera de él |
|---|---|---|
| Cloud Run (1 vCPU, 256 Mi, cpu_idle, max 2) | 2M req, 360k GB-s, 180k vCPU-s/mes | Por uso |
| Artifact Registry | 0.5 GB | $0.10/GB/mes; cleanup policies lo contienen |
| Secret Manager | 6 versiones activas, 10k accesos/mes | $0.06/versión/mes |
| GCS `fixcomap-core-tfstate` | Free tier solo en US; aquí en `europe-west1` | ~$0.02/GB/mes (KB de estado) |
| WIF, IAM, Cloud Run domain mapping, cert gestionado, Cloudflare DNS | Gratis | — |

Deliberadamente no hay: Cloud SQL, load balancer, NAT, IPs estáticas, GKE, KMS (CMEK), Artifact
Analysis (escaneo de pago: $0.26/imagen; lo hace trivy en CI).

## Secreto de configuración

`app-config` se crea en L1 con una versión placeholder (write-only, no entra en el estado). El valor
real se sube fuera del repo:

```sh
printf '%s' '<valor>' | gcloud secrets versions add app-config --project fixcomap-core --data-file=-
```

Cloud Run monta `latest` en `APP_CONFIG`; hace falta un nuevo despliegue (o revisión) para que lo lea.

## Verificar la firma de una imagen

```sh
cosign verify \
  --certificate-identity-regexp '^https://github.com/fixcomap/platform/\.github/workflows/deploy\.yml@refs/heads/main$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  europe-west1-docker.pkg.dev/fixcomap-core/docker/app@sha256:<digest>
```

## Estado

- 2026-09-16: smoke test del pipeline (`pr-checks.yml`) desde `feature/smoke-test`.
