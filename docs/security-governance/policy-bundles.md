# Policy bundles in-process

> Patrón de distribución y evaluación de políticas de autorización para los
> servicios de OpenFoundry. **Esta página es exclusivamente documental: no
> reescribe ningún servicio Rust.** Su objetivo es fijar el contrato del patrón
> "policy bundle in-process" antes de que cualquier servicio lo adopte.

## Por qué este patrón

Las políticas de autorización (RBAC + ABAC + restricted views, ver
[`/security-governance/policies-and-authorization`](./policies-and-authorization.md)
y [`/security-governance/abac-and-cbac-model/`](./abac-and-cbac-model/)) deben
evaluarse en el **hot path** de cada lectura/escritura sobre objetos, queries y
acciones. Un PDP (Policy Decision Point) central accedido por RPC en cada
petición introduce:

- latencia adicional sincrónica en cada operación de datos,
- un **single point of failure** transversal a toda la plataforma,
- acoplamiento operativo entre servicios de datos y `auth-service` /
  `ontology-security-service`.

Por esa razón fijamos como regla arquitectónica:

> **No hay PDP central en el hot path.** Cada servicio evalúa las políticas
> _in-process_ contra un *bundle* versionado y firmado que recibe fuera de
> banda.

Esta regla extiende la separación control-plane / data-plane formalizada en
[`docs/architecture/adr/ADR-0011-control-vs-data-bus-contract.md`](../architecture/adr/ADR-0011-control-vs-data-bus-contract.md):
las **decisiones** de política son data-plane (cada servicio las ejecuta sobre
sus propios datos), mientras que las **actualizaciones** de política viajan por
el control-plane (NATS JetStream, `libs/event-bus-control`).

## Productor: `ontology-security-service`

Responsable de compilar, firmar y publicar el *policy bundle* canónico de la
plataforma. El servicio ya existe en `/services/ontology-security-service`
y se enumera entre los servicios de _governance & semantics_ en
[`docs/architecture/runtime-topology.md`](../architecture/runtime-topology.md)
(§ "Layered service map").

Contrato del productor:

1. **Compilación.** `ontology-security-service` toma como input las políticas
   gestionadas por `auth-service`
   (`services/auth-service/src/handlers/policy_mgmt.rs`,
   `permission_mgmt.rs`, `role_mgmt.rs`, citados en
   [`policies-and-authorization`](./policies-and-authorization.md)) y las
   restricted views, y produce un *bundle* declarativo inmutable.
2. **Versionado.** Cada bundle recibe un identificador monotónico
   (`bundle_version`, p. ej. ULID + epoch lógico) y un hash de contenido
   (`sha256:…`). El bundle anterior queda disponible para rollback.
3. **Almacenamiento.** El bundle se sube al backend S3-compatible operado por
   **Ceph RGW** ya documentado en
   [`docs/operations/deployment.md`](../operations/deployment.md) §"Production
   (Ceph RGW via Rook)" y en
   [`docs/architecture/adr/ADR-0010-cnpg-postgres-operator.md`](../architecture/adr/ADR-0010-cnpg-postgres-operator.md)
   §"Backups y WAL archive a Ceph RGW". La URL distribuida es una **URL
   firmada** (presigned) con TTL acotado.
4. **Firma criptográfica.** El bundle se firma con la clave de release del
   `ontology-security-service`. Los consumidores rechazan cualquier bundle
   cuya firma no verifique contra la clave pública distribuida fuera de banda.
5. **Notificación.** Tras subir el artefacto, `ontology-security-service`
   publica un evento en **NATS JetStream** mediante la librería
   `libs/event-bus-control`:

   - **Subject:** `policy.bundle.updated`
   - **Payload (JSON):**
     ```json
     {
       "bundle_version": "01J…",
       "sha256": "…",
       "signed_url": "https://rgw…/bundles/01J…?X-Amz-Signature=…",
       "signed_url_expires_at": "2026-04-29T21:00:00Z",
       "signature": "base64(ed25519(sha256))",
       "issued_at": "2026-04-29T20:55:00Z"
     }
     ```
   - **Stream:** dentro del control-plane (NATS), nunca en `event-bus-data`
     (Kafka). Esto cumple el contrato mecánico de
     [`ADR-0011`](../architecture/adr/ADR-0011-control-vs-data-bus-contract.md)
     §"Decision" / "Allowlist file".

## Consumidor: `libs/auth-middleware`

Cada servicio de datos enlaza la librería `libs/auth-middleware` y delega en
ella la evaluación de políticas. El middleware encapsula:

1. **Bootstrap.** Al arrancar, el servicio descarga el último bundle conocido
   (vía URL firmada obtenida del `ontology-security-service` o cacheada
   localmente) y verifica su firma. Si la verificación falla, el servicio
   **fail-closed**: rechaza solicitudes hasta disponer de un bundle válido.
2. **Suscripción push.** El middleware se suscribe al subject
   `policy.bundle.updated` en `event-bus-control` (NATS JetStream). Al recibir
   el evento:
   - descarga el nuevo bundle desde la `signed_url`,
   - verifica firma y `sha256`,
   - intercambia atómicamente el bundle activo en memoria (swap detrás de un
     `ArcSwap`/`RwLock`),
   - mantiene el bundle anterior accesible durante un *grace period* corto
     para evaluaciones en curso.
3. **Evaluación in-process.** Toda decisión `allow/deny` se resuelve en el
   propio proceso del servicio, **sin RPC al PDP en el hot path**, contra el
   bundle activo. Esto es lo que hace operativa la regla enunciada arriba.
4. **Caché local con TTL.** El bundle se persiste en disco local con un TTL
   configurable (p. ej. 24 h) para soportar reinicios sin contactar al
   productor. La invalidación normal es **push** vía NATS; el TTL es la red de
   seguridad cuando el control-plane está degradado.
5. **Telemetría y auditoría.** Cada decisión emite un registro estructurado
   (decision id, bundle_version, principal, recurso, acción, resultado) que
   alimenta el flujo descrito en
   [`/security-governance/audit-and-traceability`](./audit-and-traceability.md)
   y en [`/security-governance/audit-model/`](./audit-model/).

### Modos de fallo

| Escenario                                  | Comportamiento del consumidor                                  |
| ------------------------------------------ | -------------------------------------------------------------- |
| NATS no disponible                         | Continúa con el bundle activo; reintenta suscripción.          |
| URL firmada caducada antes de la descarga  | Solicita re-emisión al `ontology-security-service`.            |
| Firma inválida en el nuevo bundle          | **Descarta** el bundle, mantiene el anterior, alerta.          |
| TTL local expirado y productor inalcanzable| **Fail-closed** sobre recursos sensibles, configurable.        |

## Formato del bundle

Se evaluaron tres opciones, todas OSS:

| Opción           | Licencia      | Trazabilidad | Notas                                                                                  |
| ---------------- | ------------- | ------------ | -------------------------------------------------------------------------------------- |
| Rego (OPA)       | Apache-2.0    | Media        | Lenguaje imperativo-lógico; explicaciones de decisión requieren herramientas externas. |
| **Cedar**        | **Apache-2.0**| **Alta**     | Diseñado para análisis estático y *policy reasoning*; trazas de decisión nativas.      |
| JSON declarativo | N/A           | Baja         | Sencillo pero requiere escribir un evaluador y un sistema de pruebas propios.          |

**Recomendación: Cedar (Apache-2.0).** Razones:

- Trazabilidad nativa: cada decisión expone qué política y qué *condition*
  contribuyeron, lo que se integra directamente con el modelo de auditoría
  documentado en [`audit-model/`](./audit-model/).
- Análisis estático formal (validación de esquema, detección de políticas
  inalcanzables o conflictivas) ejecutable en CI sobre el bundle antes de
  publicarlo.
- Licencia Apache-2.0, alineada con el requisito 100% OSS de la plataforma.
- Modelo entidad/atributo coherente con el diseño ABAC/CBAC ya documentado en
  [`abac-and-cbac-model/`](./abac-and-cbac-model/).

Estructura propuesta del bundle (a fijar en una ADR posterior cuando se
implemente):

```
bundle-<version>.tar.zst
├── manifest.json          # version, sha256, schema_version, issued_at
├── schema.cedarschema     # esquema de entidades y acciones
├── policies/              # *.cedar (políticas declarativas)
└── entities/              # snapshots de entidades estables (roles, grupos)
```

## Sub-issue plan: adopción del patrón

> Sub-issue plan asociado a esta página. **No** se modifica código Rust en esta
> tarea; las casillas se marcarán cuando cada servicio adopte
> `libs/auth-middleware` con evaluación in-process del bundle.

Servicios prioritarios, ordenados por superficie de hot path sobre objetos
ontológicos (ver clasificación de servicios de _governance & semantics_ en
[`docs/architecture/runtime-topology.md`](../architecture/runtime-topology.md)
§ "Layered service map"):

- [ ] **`object-database-service`** (`/services/object-database-service`).
  Custodio del estado de objetos; cada lectura/escritura debe evaluar políticas
  in-process. Mayor ganancia de latencia al eliminar el RPC al PDP.
- [ ] **`ontology-query-service`** (`/services/ontology-query-service`).
  Aplica filtros derivados del bundle (restricted views, ABAC) al planificar y
  ejecutar queries; necesita decisión local para *row/column-level* filtering.
- [ ] **`ontology-actions-service`** (`/services/ontology-actions-service`).
  Evalúa permisos sobre acciones (escrituras estructuradas) antes de
  despacharlas; bloquea acciones no autorizadas sin saltos de red.

Cada adopción requerirá su propia PR e incluirá:

1. Enlace de `libs/auth-middleware` en el `Cargo.toml` del servicio.
2. Suscripción a `policy.bundle.updated` mediante `libs/event-bus-control`
   (compatible con la allowlist de buses de
   [`ADR-0011`](../architecture/adr/ADR-0011-control-vs-data-bus-contract.md);
   añadir el servicio a `/.github/bus-allowlist.yaml` si aún no estuviera).
3. Pruebas de integración con un bundle firmado de ejemplo.
4. Métricas de latencia *antes/después* documentadas en la PR.

## Referencias cruzadas

- [`docs/architecture/adr/ADR-0011-control-vs-data-bus-contract.md`](../architecture/adr/ADR-0011-control-vs-data-bus-contract.md)
  — separación control vs data bus; las notificaciones de bundle viajan por el
  bus de control.
- [`docs/architecture/runtime-topology.md`](../architecture/runtime-topology.md)
  — topología y clasificación de servicios.
- [`/security-governance/policies-and-authorization`](./policies-and-authorization.md)
  — superficie actual de políticas en `auth-service`.
- [`/security-governance/abac-and-cbac-model/`](./abac-and-cbac-model/)
  — modelo de atributos que el bundle materializa.
- [`/security-governance/audit-and-traceability`](./audit-and-traceability.md)
  y [`/security-governance/audit-model/`](./audit-model/) — destino de las
  trazas de decisión emitidas in-process.
- [`docs/operations/deployment.md`](../operations/deployment.md) §"Production
  (Ceph RGW via Rook)" — backend S3 que aloja los bundles firmados.
