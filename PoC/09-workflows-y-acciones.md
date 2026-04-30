# 09 — Workflows y acciones operacionales

> El valor diferencial de Foundry no es ver datos, es **convertir un insight en una acción coordinada**. Aquí están los 3 workflows que se demostrarán y su definición declarativa para `workflow-automation-service`.

---

## 🌀 Workflow 1 — `mro-inspection`

**Disparado por:** acción `flag-aircraft-for-inspection` (manual o vía copiloto AIP).

### Pasos
1. **Crear** `MaintenanceEvent` (ya creado por la action; el workflow lo recoge).
2. **Asignar** ingeniero — `assign-maintenance-event` con criterio "lowest workload at home base".
3. **Reservar piezas críticas** — si el evento referencia `Part` con stock bajo, crear `order-part` (ver Workflow 2).
4. **Notificar** al engineer asignado (email + push en la UI) vía `notification-alerting-service`.
5. **Esperar aprobación** de `mro-lead` si `severity = CRITICAL` (vía `approvals-service`).
6. **Bloquear el avión** — actualizar `Aircraft.current_status = IN_MAINTENANCE` cuando se inicie.
7. **SLA timer** — si el evento no se cierra en `due_by`, escalar a `mro-director`.
8. **Cierre** — al cerrar el evento, recalcular `recurring_defects` (trigger `gd-recurring-defects`) y notificar.

### Definición declarativa
```yaml
workflow:
  id: mro-inspection
  version: 1
  trigger:
    on_action: flag-aircraft-for-inspection
  variables:
    - { name: event,    from: "{{trigger.created_object}}" }
    - { name: aircraft, query: "ontology.get(Aircraft, tail_number={{event.tail_number}})" }
  steps:

    - id: assign-engineer
      type: action
      action: assign-maintenance-event
      params:
        target: "{{event.event_id}}"
        engineer_id: |
          {{ ontology.query("
            SELECT e.engineer_id
            FROM Engineer e
            WHERE e.home_base = '{{aircraft.home_base_airport}}'
            ORDER BY e.current_workload ASC LIMIT 1
          ") }}

    - id: check-parts
      type: branch
      condition: "{{ event.requires_parts_with_low_stock }}"
      then:
        - { type: trigger_workflow, workflow: order-critical-parts, params: { for_event: "{{event.event_id}}" } }

    - id: notify-engineer
      type: notify
      channel: [email, in-app]
      template: |
        New {{event.severity}} inspection assigned to you.
        Aircraft: {{aircraft.tail_number}} ({{aircraft.model_id}})
        Defect: {{event.description}}
        Due by: {{event.due_by}}

    - id: critical-approval
      type: branch
      condition: "{{ event.severity == 'CRITICAL' }}"
      then:
        - type: approval
          approver_role: mro-lead
          timeout: 30m
          on_timeout: escalate
          on_reject: cancel_workflow

    - id: ground-aircraft
      type: action
      action: update-object
      params:
        target: "{{aircraft.tail_number}}"
        fields: { current_status: IN_MAINTENANCE }

    - id: sla-timer
      type: timer
      until: "{{event.due_by}}"
      on_expire:
        - type: notify
          to_role: mro-director
          template: "SLA breached for event {{event.event_id}}"

    - id: post-close
      type: on_event
      event: "MaintenanceEvent.{{event.event_id}}.closed"
      do:
        - { type: trigger_pipeline, pipeline: gd-recurring-defects }
        - { type: notify, to_role: mro-lead, template: "Event {{event.event_id}} closed; recurrence stats refreshed." }
```

---

## 📦 Workflow 2 — `order-critical-parts`

**Disparado por:** otro workflow (`mro-inspection`) o acción `order-part`.

### Pasos
1. Validar stock actual.
2. Si stock < umbral, crear PO (purchase order).
3. Notificar a `supply-chain` role.
4. Esperar confirmación del proveedor (mock: timer 1 min en demo).
5. Marcar pieza como "in transit" y registrar lead time.

```yaml
workflow:
  id: order-critical-parts
  trigger:
    on_action: order-part
  steps:
    - { id: check-stock,    type: query,  query: "SELECT stock FROM Part WHERE part_id='{{params.part_id}}'" }
    - { id: create-po,      type: action, action: create-purchase-order, params: { ... } }
    - { id: notify-supply,  type: notify, to_role: supply-chain }
    - { id: wait-confirm,   type: wait,   timeout: 60s, on_timeout: notify_escalate }
    - { id: mark-in-transit,type: action, action: update-object, params: { fields: { status: IN_TRANSIT } } }
```

---

## 🌪 Workflow 3 — `weather-disruption-response`

**Disparado por:** evento de `monitoring-rules-service` cuando un aeropuerto-hub cae bajo umbrales meteo.

### Pasos
1. Detectar disrupción (regla: `visibility < 800 m` o `wind_speed > 40 kt` durante 30 min).
2. Listar vuelos afectados (próximas 6h al/desde ese aeropuerto).
3. Recalcular `risk_score` para esos vuelos (trigger pipeline incremental).
4. Crear "incident card" agregada para Ops Controllers.
5. Sugerir reroutes vía copiloto AIP (acción `reroute-flight` requiere aprobación duty-manager).

```yaml
workflow:
  id: weather-disruption-response
  trigger:
    on_event: "monitoring-rules.airport-weather-breach"
  steps:
    - id: list-affected
      type: query
      query: |
        SELECT flight_id FROM Flight
        WHERE (origin_iata='{{trigger.airport}}' OR destination_iata='{{trigger.airport}}')
          AND scheduled_departure_utc BETWEEN now() AND now()+INTERVAL '6 hours'

    - id: rescore
      type: trigger_pipeline
      pipeline: gd-flights-enriched
      partition_filter: "flight_id IN ({{list-affected.results}})"

    - id: create-incident
      type: action
      action: create-incident
      params:
        severity: HIGH
        title: "Weather disruption at {{trigger.airport}}"
        affected_flights: "{{list-affected.results}}"

    - id: notify-ops
      type: notify
      to_role: ops-controller
      with_link: "/incidents/{{create-incident.id}}"
```

---

## 🔔 Notificaciones (servicio `notification-alerting-service`)

| Canal | Configuración | Uso en demo |
|---|---|---|
| In-app (UI badge) | nativo | Todas |
| Email (SMTP) | mailtrap.io o real SMTP | Acto 5 (al engineer y al mro-lead) |
| Webhook | Slack / Teams | Si el cliente quiere ver integración con Slack mostramos un workspace propio |
| Push móvil | (out-of-scope PoC) | — |

Plantillas guardadas en `PoC/assets/notifications/*.tpl` (a materializar en ejecución).

---

## ✋ Aprobaciones (servicio `approvals-service`)

Demo flow:
1. Acción `reroute-flight` requiere aprobación de un usuario con role `duty-manager`.
2. UI muestra una "approval inbox" para `duty-manager`.
3. Aprobado → la acción se ejecuta y se registra en audit con `approved_by`.
4. Rechazado → se cancela y se notifica al iniciador con motivo.

Para la demo creamos un tercer usuario `marta@acme-airlines.demo` con role `duty-manager` para enseñar este flujo solo si queda tiempo.

---

## 📊 Visualización del workflow en la UI

`workflow-trace-service` debe exponer:
- Timeline visual del workflow (gantt simplificado).
- Estado de cada step (pending/running/done/failed).
- Click en step → logs y output.

Es lo que el cliente ve después del Acto 5 cuando el copiloto ejecutó las acciones.

---

## ✅ Acciones concretas (cuando se ejecute la PoC)

1. Materializar los 3 YAML en `PoC/assets/workflows/`.
2. Registrarlos en `workflow-automation-service`.
3. Crear los 3 usuarios (`ana`, `luis`, `marta`) en Keycloak con sus roles.
4. Configurar SMTP de prueba (mailtrap) y Slack workspace propio.
5. Ejecutar smoke test: lanzar `flag-aircraft-for-inspection` → ver workflow en `workflow-trace-service` → recibir notificación.
