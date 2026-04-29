# ADR-0010: CloudNativePG (CNPG) como operador único de PostgreSQL

- **Status:** Accepted
- **Date:** 2026-04-29
- **Deciders:** OpenFoundry platform architecture group
- **Related work:**
  - `docs/architecture/runtime-topology.md` — "Postgres for service-owned
    relational state" (database-per-service).
  - `services/*/migrations/` — cada servicio mantiene su propio esquema
    (p. ej. `services/cipher-service/migrations`,
    `services/data-asset-catalog-service/migrations`,
    `services/marketplace-service/migrations`,
    `services/sql-warehousing-service/migrations`,
    `services/identity-federation-service/migrations`, etc.).
  - `infra/k8s/cnpg/templates/cluster.yaml`
    — plantilla de referencia del CRD `postgresql.cnpg.io/v1 Cluster`
    usada por los servicios de plataforma.
  - `infra/k8s/rook/` (`cluster.yaml`, `objectstore.yaml`, `bucket.yaml`) —
    Ceph + RGW como proveedor S3 cluster-local.
  - ADR-0007 (`docs/architecture/adr/ADR-0007-search-engine-choice.md`) —
    precedente de "un único operador / un único stack stateful por capacidad".

## Context

OpenFoundry sigue el patrón **base de datos por servicio**: cada bounded
context posee su propio Postgres lógico, con migraciones versionadas dentro
de `services/<svc>/migrations/`. Esto se confirma en
`docs/architecture/runtime-topology.md`:

> "Postgres for service-owned relational state […] The CI smoke job creates
> multiple service-specific databases, which strongly suggests
> database-per-service isolation rather than a shared operational schema."

Hoy conviven dos realidades:

1. **Un uso aislado de CloudNativePG** ya introducido como plantilla de
   referencia para clústeres Postgres de plataforma (ver
   `infra/k8s/cnpg/templates/cluster.yaml`),
   donde el operador reconcilia un clúster con replicación en streaming y
   expone los servicios `<name>-rw` / `<name>-ro`.
2. **El resto de servicios** no tiene un operador estandarizado. Las
   alternativas históricamente consideradas (Patroni externo + HAProxy/VIP,
   StatefulSets "a mano", Postgres gestionado por proveedor) introducen:
   - VIPs y balanceadores adicionales (SPOFs operativos),
   - rutas de failover divergentes por servicio,
   - backup/restore artesanal por equipo,
   - dificultad para auditar RPO/RTO de forma homogénea,
   - duplicidad de runbooks.

Sin un operador único:

- Cada equipo reinventa HA, backups, WAL archive, rotación de credenciales y
  upgrades menores/mayores.
- No hay una forma estándar de declarar topología (primary + réplicas),
  ventanas de mantenimiento, o políticas de retención.
- El plano de almacenamiento S3 disponible en cluster
  (`infra/k8s/rook/objectstore.yaml`) está infrautilizado para WAL/base
  backups.

El proyecto mantiene una postura **100% OSS** (Apache-2.0 / MIT / BSD); el
operador elegido debe encajar en esa restricción y alinearse con el
precedente de ADR-0007 ("un solo stack stateful por capacidad").

## Options considered

### Opción A — CloudNativePG (CNPG) como operador único (elegida)

- Operador Apache-2.0, nativo Kubernetes, sin dependencia de etcd externo
  ni de Patroni.
- CRD `Cluster` que declara topología (instancias, sync replicas), bootstrap,
  storage, recursos, scheduled backups, y `barmanObjectStore` para
  WAL/base backups a S3.
- Failover gestionado por el operador vía la API de Kubernetes (sin VIP).
- Servicios `<name>-rw`, `<name>-ro`, `<name>-r` generados automáticamente
  → los servicios de aplicación se conectan por DNS estable.
- Ya en uso como plantilla de referencia bajo `infra/k8s/cnpg/` (precedente
  vivo en el repo).

### Opción B — Patroni + HAProxy / keepalived (VIP)

- Requiere etcd/Consul externo, HAProxy y VIP por clúster.
- Añade SPOFs operativos (VIP, balanceador), runbooks de failover manuales
  y un plano de control fuera de Kubernetes.
- Backups y WAL archive resueltos con scripts ad-hoc (pgBackRest/barman
  invocados a mano).

### Opción C — Zalando postgres-operator

- OSS (MIT), maduro, basado en Patroni interno.
- Modelo de configuración (Spilo/Patroni) más opaco que CNPG y con
  superficie de ajuste mayor.
- Integración de backups vía WAL-G; correcta pero menos declarativa que
  `barmanObjectStore` de CNPG.

### Opción D — StackGres

- OSS (AGPL-3.0 para componentes core en algunas distribuciones).
- **Rechazada** por incompatibilidad con la postura OSS del proyecto
  (Apache-2.0 / MIT / BSD), análoga a la lógica aplicada en ADR-0007.

### Opción E — Status quo (StatefulSets sin operador)

- Mantener Postgres por servicio sin operador, con HA y backups gestionados
  por cada equipo.
- Maximiza la deuda operativa y dispersa los runbooks; no resuelve el
  problema planteado.

## Decision

Adoptamos la **Opción A — CloudNativePG (CNPG) como operador único de
PostgreSQL** para todos los clústeres Postgres del plano de producción de
OpenFoundry.

- **Operador único.** CNPG (Apache-2.0) es el único operador soportado para
  Postgres en `infra/k8s/**`. No se introducirán Patroni externos, VIPs,
  HAProxy de Postgres, ni operadores alternativos (Zalando, StackGres,
  etc.) mientras este ADR esté vigente.
- **Topología por bounded context.** Cada servicio que requiera Postgres
  declara su propio CR `postgresql.cnpg.io/v1 Cluster` con la topología
  base **1 primary + 2 réplicas (al menos 1 síncrona)**
  (`spec.instances: 3`, `spec.minSyncReplicas: 1`,
  `spec.maxSyncReplicas: 1`). Esto preserva el aislamiento
  database-per-service descrito en `docs/architecture/runtime-topology.md`.
- **Backups y WAL archive a Ceph RGW.** Todos los clústeres configuran
  `spec.backup.barmanObjectStore` apuntando a
  `s3://openfoundry-pg-backups/<service>/<cluster>/`, sirviendo Ceph RGW
  declarado en `infra/k8s/rook/objectstore.yaml` y
  `infra/k8s/rook/bucket.yaml`. Esto incluye:
  - **WAL archiving** continuo (`wal: { compression: gzip }`).
  - **Base backups programados** (`ScheduledBackup`) con cadencia diaria.
  - **Retención** mínima de 14 días, máxima de 30 días por defecto;
    overridable por servicio.
  - **Cifrado en reposo** delegado al bucket de Ceph RGW.
- **Conexiones desde servicios.** Los servicios consumen los endpoints
  generados por CNPG: `<cluster>-rw` para escrituras, `<cluster>-ro` para
  lecturas escalables. No se permiten conexiones directas a pods ni VIPs
  externos.
- **Credenciales.** Se gestionan mediante `Secret` de tipo
  `kubernetes.io/basic-auth`, siguiendo el patrón establecido en
  `infra/k8s/cnpg/templates/cluster.yaml`.
- **Upgrades.** Las versiones mayores de Postgres se planifican vía el
  flujo de upgrade in-place del operador o mediante `Cluster` de réplica
  lógica + cutover, según se documente en el runbook.
- **Desarrollo local.** `infra/docker-compose.yml` y
  `infra/docker-compose.dev.yml` siguen usando contenedores Postgres
  estándar para DX. CNPG aplica solo al plano Kubernetes.

## Consequences

### Positivas

- **Una sola superficie operativa** para todos los Postgres del plano de
  producción: un operador, un CRD, un runbook, un dashboard.
- **HA declarativa** (1 primary + 2 réplicas con al menos 1 síncrona) y
  failover automático sin VIPs ni SPOFs externos.
- **Backups y PITR uniformes** sobre Ceph RGW, reutilizando el almacenamiento
  S3 ya provisto por `infra/k8s/rook/`.
- **Aislamiento por bounded context** preservado: cada servicio mantiene su
  propio `Cluster` y sus propias migraciones bajo
  `services/<svc>/migrations/`.
- **Coherencia con ADR-0007**: una sola tecnología por capacidad stateful,
  reduciendo carga cognitiva y dependencias.
- **100% OSS** (Apache-2.0).

### Negativas / trade-offs

- Acoplamiento del plano de datos a Kubernetes y a la API de CNPG.
- Coste base de 3 instancias por servicio que requiera HA estricta; los
  servicios no críticos pueden declarar `instances: 1` con backups (sin
  HA), pero deben justificarlo en su ADR de servicio.
- Dependencia operativa adicional sobre Ceph RGW para la cadena de
  backup/restore: una indisponibilidad del object store degrada el WAL
  archive (no el plano de escritura, gracias al buffer local).
- Curva de aprendizaje del CRD `Cluster` para equipos que solo conocen
  Postgres "plano".

### Migración / cleanup

- La plantilla CNPG bajo
  `infra/k8s/cnpg/templates/cluster.yaml`
  se considera el patrón de referencia. Nuevos charts de servicio deben
  reutilizar la misma forma (CR `Cluster` + `Secret` basic-auth).
- No existen Patroni externos ni VIPs de Postgres en `infra/k8s/**` al
  momento de este ADR; no hay nada que retirar.
- Cualquier roadmap, prompt o documento que mencione "Patroni",
  "HAProxy para Postgres" o "Postgres administrado externamente" debe
  enlazar a este ADR y reformularse en términos de CNPG.

## Conditions under which this decision would be reopened

Este ADR debe reabrirse si **cualquiera** de las siguientes condiciones se
cumple:

1. CNPG cambia su licencia, gobierno o cadencia de releases de forma que
   deje de ser compatible con la postura OSS del proyecto (precedente
   Qdrant / OpenSearch en ADR-0007).
2. Un destino regulado obliga al uso de un Postgres gestionado por un
   proveedor concreto o de un operador certificado distinto.
3. Un workload concreto demuestra, con benchmarks reproducibles bajo
   `benchmarks/`, que CNPG no puede sostener el RPO/RTO o el throughput
   requerido para un bounded context dado.
4. Se decide consolidar Postgres en un clúster compartido multi-tenant,
   lo que rompería el principio "database-per-service" y requeriría
   reabrir tanto este ADR como
   `docs/architecture/runtime-topology.md`.

## References

- `docs/architecture/runtime-topology.md` — modelo "Postgres por servicio".
- `docs/architecture/adr/ADR-0007-search-engine-choice.md` — precedente de
  "un solo stack stateful por capacidad" y de filtrado por licencia OSS.
- `infra/k8s/cnpg/templates/cluster.yaml`
  — patrón de CR `Cluster` + `Secret` basic-auth de referencia.
- `infra/k8s/rook/objectstore.yaml`, `infra/k8s/rook/bucket.yaml` —
  proveedor S3 para `s3://openfoundry-pg-backups/`.
- `services/*/migrations/` — esquemas por servicio (p. ej.
  `services/cipher-service/migrations`,
  `services/data-asset-catalog-service/migrations`).
- CloudNativePG: <https://cloudnative-pg.io/> (Apache-2.0).
- Barman / `barmanObjectStore`:
  <https://cloudnative-pg.io/documentation/current/backup_recovery/>.
