# fixcomap/platform

Plataforma interna de fixcomap: infraestructura compartida en Google Cloud y Cloudflare, un catálogo de
proyectos y un pipeline estándar que usan todos los repos. Dar de alta un proyecto nuevo es añadir un
fichero YAML; no se copia ningún pipeline.

Todo es código (OpenTofu + GitHub Actions) y se despliega sin credenciales de larga duración en GCP.

## Proyectos

| Repo | Tipo | Producción |
|---|---|---|
| [`plantilla`](https://github.com/fixcomap/plantilla) | webapp (Go, Cloud Run) | plantilla.fixcomap.com |
| [`florindario`](https://github.com/fixcomap/florindario) | webapp (React + Node, Cloud Run) | florindario.fixcomap.com |
| [`jjpachangas`](https://github.com/fixcomap/jjpachangas) | webapp (React + Node, Cloud Run) | en desarrollo |
| [`reservas`](https://github.com/fixcomap/reservas) | webapp (React + Node, Cloud Run) | en desarrollo |
| [`web`](https://github.com/fixcomap/web) | static (Cloudflare Pages) | fixcomap.com |
| [`agustin`](https://github.com/fixcomap/agustin) | static | agustin.fixcomap.com |
| [`elias`](https://github.com/fixcomap/elias) | static (React + Vite) | elias.fixcomap.com |

## Arquitectura

```
fixcomap/<proyecto>                         fixcomap/platform
  código                                      catalog/<proyecto>.yaml    qué es y dónde vive
  .github/workflows/ci.yml  ──── uses ────▶   .github/workflows/cicd.yml pipeline estándar
  (idéntico en todos)                           ├─ static → checks + Cloudflare Pages
                                                └─ webapp → checks + imagen firmada + Cloud Run
                                              infra/                     identidad, secretos, DNS,
                                                                         dominios por proyecto
```

- **Catálogo como única fuente.** Cada proyecto se describe en `catalog/<repo>.yaml` (tipo, hostnames,
  front, migraciones, secretos). El pipeline y la infraestructura derivan todo de ahí; un repo sin
  entrada no despliega nada.
- **Pipeline versionado.** Cada repo fija una versión de la plataforma (`cicd.yml@vX.Y.Z`), así que un
  cambio aquí llega a cada proyecto de forma controlada.
- **Infraestructura en capas.** El bootstrap mínimo (estado y federación de identidades), la plataforma
  (IAM, secretos, registro, dominios), los servicios de cada webapp y DNS/CDN se aplican por separado,
  cada uno con la identidad y los permisos justos.

## Flujo

| Rama | Qué pasa |
|---|---|
| PR | checks de calidad y seguridad; nada se despliega |
| `develop` | entorno de desarrollo, privado |
| `main` | producción, con aprobación manual |

Gitflow con releases y hotfixes versionados. `main` y `develop` están protegidas: PR obligatorio y
checks en verde para todos, sin excepciones. Detalle en [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Seguridad

- **Sin claves de servicio.** GitHub Actions se autentica en GCP con Workload Identity Federation;
  cada token se acepta solo para un repo y una rama concretos.
- **Mínimo privilegio por proyecto.** Cada webapp despliega con su propia identidad, que solo alcanza
  sus servicios, su estado y sus secretos.
- **Cambios de IAM con aprobación.** Lo que modifica quién puede hacer qué se aplica solo tras revisar
  el plan.
- **Cadena de suministro.** Actions fijadas por commit SHA, dependencias actualizadas con Renovate,
  imágenes firmadas con cosign y lista cerrada de Actions permitidas.
- **Análisis en cada PR.** gitleaks, trivy (código, IaC e imágenes), checkov, tflint, actionlint y
  CodeQL.
- **Entornos de desarrollo privados.** Protegidos con Cloudflare Access y restringidos también en la
  propia API.
- **Secretos fuera del repo.** Viven en Secret Manager y se cargan a mano; el código solo declara
  dónde están.

## Operación

- **Drift:** un plan nocturno abre un issue si la infraestructura real se aleja del código.
- **Coste:** un check en cada PR y un inventario diario frenan recursos de pago no aprobados.
- **Backups:** volcado diario de las bases de producción a almacenamiento con retención, con una
  identidad que puede escribir pero no borrar.
- **Observabilidad:** OpenTelemetry hacia Grafana Cloud, con dashboard, SLOs y alertas como código.
- **Disponibilidad:** uptime checks y alertas de Cloud Monitoring.

## Coste

Diseñado para funcionar dentro de los free tiers con la carga actual: Cloud Run con escalado a cero,
Postgres serverless (Neon), Cloudflare Pages y DNS. Deliberadamente no hay load balancers, NAT, IPs
estáticas, Cloud SQL ni GKE.

## Stack

OpenTofu · GitHub Actions · Google Cloud (Cloud Run, Artifact Registry, Secret Manager, Cloud Storage,
Cloud Monitoring) · Cloudflare (DNS, Pages, Access) · Neon · Grafana Cloud · cosign · trivy · checkov
