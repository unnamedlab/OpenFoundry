# 10 — Seguridad y gobierno

> Para un cliente enterprise (especialmente en aviación) **la primera pregunta que harán** después del "wow" es: *"¿es esto seguro y auditable?"*. Este documento prepara la respuesta.

---

## 👥 Usuarios y roles para la demo

Crear en Keycloak (realm `openfoundry-poc`):

| Usuario | Email | Rol(es) | Pantalla por defecto |
|---|---|---|---|
| Ana Morales | `ana@acme-airlines.demo` | `ops-controller` | Operations Live |
| Luis García | `luis@acme-airlines.demo` | `mro-lead` | Fleet Health |
| Marta Ríos | `marta@acme-airlines.demo` | `duty-manager` | Operations Live (con inbox aprobaciones) |
| Diego Ruiz | `diego@acme-airlines.demo` | `mro-engineer` | tarea asignada (vista limitada) |
| Admin | `admin@acme-airlines.demo` | `platform-admin` | reservado para reset |

Contraseñas: gestionadas por sopa, **nunca en el repo**. Generadas con `pwgen 24 1`.

---

## 🛡️ Política RBAC (resumen)

Matriz de permisos sobre acciones de la ontología:

| Acción | ops-controller | mro-lead | mro-engineer | duty-manager | platform-admin |
|---|:---:|:---:|:---:|:---:|:---:|
| Read all objects | ✅ | ✅ | ⚠ (solo asignados) | ✅ | ✅ |
| `acknowledge-delay-risk` | ✅ | ⛔ | ⛔ | ✅ | ✅ |
| `flag-aircraft-for-inspection` | ⛔ | ✅ | ⛔ | ✅ | ✅ |
| `assign-maintenance-event` | ⛔ | ✅ | ⛔ | ✅ | ✅ |
| `order-part` | ⛔ | ✅ | ⛔ | ⛔ | ✅ |
| `reroute-flight` | propose | propose | ⛔ | approve+execute | execute |
| Manage ontology definitions | ⛔ | ⛔ | ⛔ | ⛔ | ✅ |
| Branch datasets | ⛔ | ⛔ | ⛔ | ⛔ | ✅ |

Definir como YAML en `authorization-policy-service`:

```yaml
policy:
  id: aviation-poc-v1
  bindings:
    - role: ops-controller
      grants:
        - { resource: "ontology://*", verbs: [read] }
        - { resource: "ontology.action://acknowledge-delay-risk", verbs: [execute] }
        - { resource: "ontology.action://reroute-flight", verbs: [propose] }
    - role: mro-lead
      grants:
        - { resource: "ontology://*", verbs: [read] }
        - { resource: "ontology.action://flag-aircraft-for-inspection", verbs: [execute] }
        - { resource: "ontology.action://assign-maintenance-event", verbs: [execute] }
        - { resource: "ontology.action://order-part", verbs: [execute] }
    - role: mro-engineer
      grants:
        - { resource: "ontology://MaintenanceEvent?assigned_engineer_id={{user.id}}", verbs: [read, update] }
    - role: duty-manager
      grants:
        - { resource: "ontology://*", verbs: [read] }
        - { resource: "ontology.action://reroute-flight", verbs: [approve, execute] }
        - { resource: "ontology.action://*", verbs: [execute] }
    - role: platform-admin
      grants:
        - { resource: "*", verbs: ["*"] }
```

> **ABAC además de RBAC**: notar la regla del `mro-engineer` que filtra por `assigned_engineer_id={{user.id}}` — esto es atributo del recurso, no solo rol. Lo mostramos en la demo.

---

## 📜 Audit log — qué se registra

`audit-compliance-service` captura **toda** acción de escritura y, opcionalmente, lecturas a recursos sensibles. Cada entry contiene:

```json
{
  "audit_id": "uuid",
  "timestamp_utc": "2026-04-30T09:42:11Z",
  "actor": { "user_id": "luis", "session_id": "...", "source_ip": "10.0.0.42", "user_agent": "..." },
  "action": "ontology.action.execute",
  "action_id": "flag-aircraft-for-inspection",
  "target": { "object_type": "Aircraft", "id": "N12345" },
  "params": { "reason": "Recurring ATA-27 defect", "priority": "HIGH" },
  "outcome": "success",
  "produced_objects": [{ "object_type": "MaintenanceEvent", "id": "evt_abc123" }],
  "triggered_workflows": ["mro-inspection:run_xyz"],
  "via": { "client": "ui", "feature": "fleet-health-app" },
  "policy_decision": { "policy_id": "aviation-poc-v1", "binding": "mro-lead/flag-aircraft-for-inspection" }
}
```

**Inmutabilidad:** los audit entries se replican a S3 con object-lock (mode `COMPLIANCE`, retención 7 años). Mostrar al cliente los buckets con políticas.

---

## 🔐 Datos sensibles

Para la PoC **no hay PII** (solo tail numbers, callsigns, ATA codes). Pero demostramos la capacidad:
- En el catálogo, etiquetar campos con `pii: true / quasi_pii: true / public: true`.
- `cipher-service` puede aplicar **field-level encryption** o tokenización.
- `data-asset-catalog-service` muestra etiquetas y permite consultas "find all PII assets".

---

## 🌳 Branches y *time travel* (gobernanza de cambios en datos)

Demostrar:
1. Branch `feat/risk-model-v2` desde `main` (acción de `mro-lead`+).
2. Cambio (reentreno modelo).
3. Diff visual (filas afectadas, métricas comparadas).
4. Merge → audit entry: quién aprobó, cuándo, qué cambió.
5. Rollback al snapshot anterior (time travel) en un click.

---

## 🔑 Identidad federada

`identity-federation-service` con OIDC contra Keycloak. **SAML pendiente** según el roadmap; documentar al cliente que se planifica para la fase post-PoC si lo necesitan.

---

## 🔒 Aislamiento de red

Para la demo cloud:
- VPC privada, subnets privadas para servicios internos.
- ALB público solo para UI + Keycloak.
- Egress controlado (allowlist: opensky, S3 NOAA, transtats.bts.gov, Azure OpenAI).
- TLS termination en ALB con cert ACM/Let's Encrypt.

---

## 🧯 Plan de respuesta si algo "se filtra" en la demo

(Improbable pero por si acaso)
- Tenemos `kill-switch.sh` que apaga `apps/web` y muestra una página estática "maintenance".
- El presentador tiene este script en una pestaña abierta del terminal.

---

## 📋 Compliance — qué decir al cliente

OpenFoundry **no es** EASA Part-145 ni SOC2 hoy (la PoC no lo certifica). Pero **es compatible** con esos frameworks porque:
- Audit inmutable y exportable a SIEM.
- RBAC + ABAC granular.
- Encryption at rest (KMS-backed) y in transit (TLS 1.3).
- Lineage end-to-end (clave para auditorías regulatorias).
- Branches y aprobaciones para cambios controlados (clave para Part-145).
- Self-hosted: el cliente mantiene el control de los datos.

---

## ✅ Acciones concretas (cuando se ejecute la PoC)

1. Crear realm `openfoundry-poc` en Keycloak con los 5 usuarios.
2. Cargar la policy YAML en `authorization-policy-service`.
3. Activar audit en todos los servicios del subset (config global).
4. Configurar S3 object-lock COMPLIANCE para el bucket de audit.
5. Probar matriz RBAC con cada usuario antes del ensayo (script `tools/poc-aviation/test_rbac.sh`).
6. Tener `kill-switch.sh` preparado y probado.
