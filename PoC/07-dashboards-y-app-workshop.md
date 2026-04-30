# 07 — Dashboards y Workshop App

> La UI es lo que el cliente ve durante el 70% de la demo. Si los dashboards son pobres, da igual cuán potente sea el backend. Aquí está el diseño de las **3 pantallas** y la **Workshop App** que se construirán en `apps/web` + `app-builder-service`.

---

## 🖥️ Pantalla 1 — "Operations Live" (dashboard principal)

Vista que ve **Ana (Ops Controller)** al hacer login.

### Layout (16:9, 1920×1080)
```
┌──────────────────────────────────────────────────────────────────────────────────┐
│  OpenFoundry · Acme Airlines · Operations Live           [Ana ▾]   [🔔 12]      │
├──────────────────────────────────────────────────────────────────────────────────┤
│ ┌─KPI 1─────────┐ ┌─KPI 2─────────┐ ┌─KPI 3─────────┐ ┌─KPI 4─────────┐         │
│ │ 🛫 Flights    │ │ ⚠ At Risk     │ │ ⏱ Avg Delay   │ │ 🛠 Open Events│         │
│ │  airborne     │ │  (HIGH/CRIT)  │ │  (last 1h)    │ │  (CRITICAL)   │         │
│ │   1,247       │ │     38        │ │   12 min      │ │      4        │         │
│ └───────────────┘ └───────────────┘ └───────────────┘ └───────────────┘         │
│                                                                                  │
│ ┌────────────────────── Live Map (60% width) ─────────────┐ ┌─Risk feed (40%)─┐│
│ │                                                          │ │ AAL 256 →JFK   ││
│ │   ╱╱ aircraft tracks (color = risk)                      │ │   HIGH  · 14m  ││
│ │   ● airports (size = throughput)                         │ │ DAL 1342 →ATL  ││
│ │   ⛅ weather overlay (toggle)                             │ │   CRIT  · 22m  ││
│ │                                                          │ │ … (scroll)     ││
│ └──────────────────────────────────────────────────────────┘ └────────────────┘│
│                                                                                  │
│ ┌── Top Risk Flights (table) ────────────────────────────────────────────────┐ │
│ │ Flight | A/C tail | Origin → Dest | ETD UTC | Risk | Cause            | ▸ │ │
│ │ ──────  ────────   ──────────────   ───────   ────   ───────────────    ── │ │
│ │ AAL256  N12345     LHR → JFK        14:30    HIGH   Convective at JFK  ▸ │ │
│ │ DAL1342 N98765     CDG → ATL        15:00    CRIT   Late aircraft       ▸ │ │
│ │ …                                                                          │ │
│ └────────────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────────────┘
```

### Widgets y servicios que los alimentan
| Widget | Servicio | Refresco |
|---|---|---|
| KPIs (4) | `ontology-query-service` (consulta agregada) | 30 s |
| Live Map | `geospatial-intelligence-service` (tracks) + `event-streaming-service` | 5 s |
| Weather overlay | tiles desde `silver.weather_by_airport` | 5 min |
| Risk feed | `ontology-query-service` (top 50 risk_band ≥ HIGH) | 30 s |
| Top Risk Flights | mismo, con paginación | 30 s |

### Interacciones
- Click en un avión del mapa → side-panel con detalle del Flight + Aircraft + última observación meteo.
- Click en una fila de la tabla → abre **Pantalla 3 (Flight Detail)**.
- Botón en el header **"Ask AIP"** → abre el copiloto en overlay.

---

## 🛠️ Pantalla 2 — "Fleet Health" (vista MRO)

Vista que ve **Luis (MRO Lead)** al hacer login.

### Layout
```
┌──────────────────────────────────────────────────────────────────────────────────┐
│ Fleet Health  ·  Acme Airlines        [Luis ▾]   [🔔 5]   Filter: A320 ✓        │
├──────────────────────────────────────────────────────────────────────────────────┤
│ ┌─Recurring Defects (Heatmap) ──────────┐  ┌── Open Events by Severity ────────┐│
│ │  ATA chapter × Aircraft model         │  │ CRIT ████ 4                       ││
│ │  Color = count last 30d               │  │ HIGH ████████ 18                  ││
│ │  ↘ click cell to drill                │  │ MED  ████████████████ 47          ││
│ └───────────────────────────────────────┘  │ LOW  ████████████████████████ 132 ││
│                                            └────────────────────────────────────┘│
│ ┌── Aircraft list (sortable, filterable) ─────────────────────────────────────┐ │
│ │ Tail   | Model    | Hours | Last insp | Open evts | Status         | Actions│ │
│ │ N12345 | A320-214 | 38127 | 12d ago   |   3 (1H)  | IN_FLIGHT      |  ⋮     │ │
│ │ N67890 | A320-251 | 41209 | 5d  ago   |   1 (CRIT)| IN_MAINTENANCE |  ⋮     │ │
│ │ …                                                                            │ │
│ └─────────────────────────────────────────────────────────────────────────────┘ │
│                                                                                  │
│ ┌── Parts at risk (lead time × demand) ────────────────────────────────────────┐│
│ │  HW-AIL-7421  ░░░░░░░░ lead 21d · forecast 12 needed in 14d  ⚠               ││
│ │  HW-ENG-1102  ░░░░     lead  7d · forecast 30 needed in 30d  ✓               ││
│ └─────────────────────────────────────────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────────────────────────────┘
```

### Interacciones
- **Heatmap** click → tabla de los `MaintenanceEvent` que componen esa celda (drill).
- **Aircraft list ⋮** → menú con acciones de la ontología: `flag-aircraft-for-inspection`, `assign-maintenance-event`.
- **Parts at risk** click → muestra en qué workflows están reservadas y permite crear `order-part`.

---

## 🛩️ Pantalla 3 — "Flight Detail" / "Aircraft Detail"

Vista de detalle al hacer click en un objeto de la ontología. Es la **Object View** estilo Foundry.

### Estructura (tabs)
1. **Overview** — propiedades clave, mapa pequeño, riesgo.
2. **Linked objects** — relaciones de la ontología: aircraft, airports origen/destino, weather observations, maintenance events.
3. **Timeline** — eventos cronológicos: scheduled departure, ADS-B segments, weather alerts, MRO events.
4. **Lineage** — embebido del `lineage-service`: de qué pipelines proviene cada propiedad.
5. **Audit** — quién ha visto/modificado este objeto (de `audit-compliance-service`).
6. **Actions** — botones de acción (con permisos respetados).

---

## 🧱 Workshop App — "MRO Triage Workbench"

> La **Workshop App** equivalente a Foundry Workshop. La construye el operador con `app-builder-service` (low-code) sin tocar código.

### Propósito
Una sola pantalla donde Luis triager los `MaintenanceEvent` críticos de las últimas 24h y decide qué hacer con cada uno.

### Componentes
| Bloque | Tipo | Datos |
|---|---|---|
| Filtros (sidebar) | controls (model, severity, ATA chapter, fleet base) | params |
| Lista de eventos | object-list widget bound a `MaintenanceEvent` filtrado | ontology query |
| Panel central | object-card del evento seleccionado | ontology |
| Sub-panel "Aircraft history" | mini-tabla de últimos 10 eventos del mismo tail | ontology graph traversal |
| Sub-panel "Similar defects in fleet" | tabla de eventos con mismo `defect_code` + modelo | ontology graph traversal |
| Acciones | botones que invocan `assign-maintenance-event`, `order-part`, `flag-aircraft-for-inspection` | ontology actions |
| Side widget | "Ask AIP about this aircraft" | copiloto contextual |

### Configuración exportable (formato `app-builder-service`)
```yaml
app:
  id: mro-triage-workbench
  title: "MRO Triage Workbench"
  audience: [mro-lead, mro-engineer]
  layout:
    type: 3-column
    left:  [filters-control]
    main:  [event-list, event-detail-card]
    right: [aircraft-history, similar-defects, ask-aip]
  components:
    - id: filters-control
      type: control-panel
      params:
        - { id: model_filter,    type: multi-select, options_query: "SELECT DISTINCT model_id FROM AircraftModel" }
        - { id: severity_filter, type: multi-select, default: [HIGH, CRITICAL] }
        - { id: ata_filter,      type: text }
    - id: event-list
      type: object-list
      object_type: MaintenanceEvent
      filter: |
        a.model_id IN ({{model_filter}})
        AND severity IN ({{severity_filter}})
        AND defect_code LIKE '{{ata_filter}}%'
        AND discovered_at_utc >= now() - INTERVAL '24 hours'
      sort: severity DESC, discovered_at_utc DESC
      on_select: bind:selected_event
    - id: event-detail-card
      type: object-card
      object: "{{selected_event}}"
      show_actions: [assign-maintenance-event, order-part]
    - id: aircraft-history
      type: object-list
      object_type: MaintenanceEvent
      filter: tail_number = '{{selected_event.tail_number}}'
      limit: 10
    - id: similar-defects
      type: object-list
      object_type: MaintenanceEvent
      filter: |
        defect_code = '{{selected_event.defect_code}}'
        AND aircraft.model_id = '{{selected_event.aircraft.model_id}}'
      limit: 20
    - id: ask-aip
      type: copilot-panel
      context_objects: [selected_event, selected_event.aircraft]
```

> Tarea pendiente: materializar `PoC/assets/apps/mro-triage-workbench.yaml` cuando se implemente.

---

## 🎨 Branding mínimo

- Logo `images/logo.png` arriba a la izquierda.
- Color primario: el del repo (revisar `apps/web` por palette).
- Branding **opcional** del cliente: poder cargar su logo en `tenancy-organizations-service` y que aparezca en el header.

---

## 🚦 Performance objetivo (medirlo)

| Pantalla | First contentful paint | Time to interactive |
|---|---|---|
| Operations Live | < 1.5 s | < 3 s |
| Fleet Health | < 1.5 s | < 3 s |
| Flight Detail | < 1 s | < 2 s |
| Workshop App | < 2 s | < 4 s |

Si no se cumplen, **cachear en `ontology-query-service`** y precomputar agregaciones.

---

## ✅ Acciones concretas (cuando se ejecute la PoC)

1. Diseñar las 3 pantallas en Figma o en el propio `apps/web` antes de implementar.
2. Implementar componentes reutilizables: `LiveMap`, `KpiCard`, `ObjectCard`, `ObjectList`, `ActionButton`, `CopilotPanel`.
3. Materializar la Workshop App en `app-builder-service`.
4. Validar performance con Lighthouse y `k6` (simulando 5 usuarios concurrentes).
5. Capturar screenshots para el plan B (vídeo de respaldo).
