# Cómo contribuir

## Ramas

GitFlow: `feature/*` y `bugfix/*` salen de `develop` y vuelven por PR; `release/x.y.z` lleva `develop` a
`main`; `hotfix/x.y.z` sale de `main`. Tras cada release o hotfix, back-merge a `develop`.

Merges siempre con merge commit. `main` solo acepta `release/*` y `hotfix/*`, y lo verifica el pipeline.

## Pull requests

- Todo cambio entra por PR, con los checks en verde.
- Los cambios de infraestructura publican su plan en el PR y se revisan antes de aplicar.
- Recursos con coste fijo requieren aprobación explícita.

## Commits

Una línea, sin cuerpo: `<tipo>(<ámbito>): <descripción en minúsculas>`.
Ejemplo: `feat(catalog): sitios static privados`.

## Convenciones

- Actions referenciadas por commit SHA con `# vX.Y.Z` al lado, nunca por tag.
- Nada de secretos en el repo: se gestionan fuera y el código solo declara dónde viven.
- Los repos no versionan configuración local de editores ni de herramientas de IA.

## Alta de un proyecto

Un PR que añade `catalog/<repo>.yaml` y, en el repo del proyecto, el `ci.yml` canónico de
`caller/ci.yml`. El resto lo hacen el pipeline y la infraestructura.
