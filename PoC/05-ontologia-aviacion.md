# 05 — Ontología de aviación

> El **modelo ontológico** es el activo central de Foundry y de OpenFoundry. Define las "cosas que importan" al negocio, sus propiedades y cómo se relacionan. Aquí está el modelo completo para la PoC, listo para cargar en `ontology-definition-service`.

---

## 🧱 Entidades (object types)

| ID | Nombre | Descripción | Tabla origen |
|---|---|---|---|
| `Aircraft` | Avión físico | Una unidad de flota (un *tail number*). | `curated.aircraft` |
| `Airport` | Aeropuerto | Localización con código IATA/ICAO. | `curated.airports` |
| `Flight` | Vuelo | Un vuelo programado o realizado. | `curated.flights` |
| `FlightSegment` | Segmento ADS-B | Tracking en vivo (posición, vel.). | `curated.flight_segments` |
| `WeatherObservation` | Observación meteo | Estado meteo en una celda + hora. | `curated.weather_observations` |
| `MaintenanceEvent` | Evento de mantenimiento | Work order o inspección. | `curated.maintenance_events` |
| `Part` | Pieza | Componente del catálogo. | `curated.parts` |
| `PartUsage` | Uso de pieza | Pieza usada en un MaintenanceEvent. | `curated.part_usages` |
| `Engineer` | Ingeniero MRO | Persona técnica. | `curated.engineers` |
| `Airline` | Aerolínea operadora | | `curated.airlines` |
| `AircraftModel` | Modelo (e.g., A320-214) | | `curated.aircraft_models` |

---

## 🔗 Relaciones (link types)

| Relación | De | A | Cardinalidad |
|---|---|---|---|
| `OPERATES` | `Airline` | `Aircraft` | 1—N |
| `IS_MODEL_OF` | `Aircraft` | `AircraftModel` | N—1 |
| `OPERATED_BY` | `Flight` | `Aircraft` | N—1 |
| `DEPARTS_FROM` | `Flight` | `Airport` | N—1 |
| `ARRIVES_AT` | `Flight` | `Airport` | N—1 |
| `TRACKED_BY` | `Flight` | `FlightSegment` | 1—N |
| `OBSERVED_AT` | `WeatherObservation` | `Airport` | N—1 |
| `INFLUENCED_BY` | `Flight` | `WeatherObservation` | N—N (computado) |
| `HAS_EVENT` | `Aircraft` | `MaintenanceEvent` | 1—N |
| `USED_PART` | `MaintenanceEvent` | `PartUsage` | 1—N |
| `OF_PART` | `PartUsage` | `Part` | N—1 |
| `ASSIGNED_TO` | `MaintenanceEvent` | `Engineer` | N—1 |
| `COMPATIBLE_WITH` | `Part` | `AircraftModel` | N—N |

---

## 🧬 Propiedades por entidad

### `Aircraft`
| Propiedad | Tipo | PII | Notas |
|---|---|---|---|
| `tail_number` (PK) | string | no | "N12345" |
| `serial_number` | string | no | |
| `model_id` | string | no | FK → `AircraftModel.id` |
| `manufacturer` | string | no | |
| `year_built` | int | no | |
| `engine_count` | int | no | |
| `airline_id` | string | no | FK → `Airline.id` |
| `home_base_airport` | string | no | FK → `Airport.iata` |
| `total_flight_hours` | float | no | computed |
| `last_inspection_date` | date | no | computed |
| `current_status` | enum | no | IN_FLIGHT / ON_GROUND / IN_MAINTENANCE / GROUNDED |

### `Flight`
| Propiedad | Tipo | Notas |
|---|---|---|
| `flight_id` (PK) | string | "AAL123_20260415" |
| `flight_number` | string | "AA123" |
| `airline_id` | string | FK |
| `aircraft_tail_number` | string | FK |
| `origin_iata` | string | FK |
| `destination_iata` | string | FK |
| `scheduled_departure_utc` | timestamp | |
| `actual_departure_utc` | timestamp nullable | |
| `scheduled_arrival_utc` | timestamp | |
| `actual_arrival_utc` | timestamp nullable | |
| `dep_delay_minutes` | int nullable | |
| `arr_delay_minutes` | int nullable | |
| `delay_root_cause` | enum nullable | WEATHER / MAINTENANCE / ATC / CARRIER / SECURITY / LATE_AIRCRAFT |
| `cancelled` | bool | |
| `diverted` | bool | |
| `distance_km` | float | |
| **Computed** | | |
| `risk_score` | float [0,1] | output del pipeline `delay_risk_predictor` |
| `risk_band` | enum | LOW/MEDIUM/HIGH/CRITICAL |

### `Airport`
`iata` (PK), `icao`, `name`, `city`, `country`, `lat`, `lon`, `elevation_ft`, `timezone`, `runway_count`, `is_hub_for` (lista de `airline_id`).

### `WeatherObservation`
`observation_id` (PK), `station_iata`, `valid_at_utc`, `wind_speed_kt`, `wind_dir_deg`, `visibility_m`, `ceiling_ft`, `precipitation_mm_h`, `turbulence_index`, `convective_sigmet` (bool).

### `MaintenanceEvent`
`event_id` (PK), `tail_number` (FK), `event_type` (LINE/A/B/C/D check, AOG, defect-finding), `defect_code` (ATA), `severity`, `discovered_at_utc`, `closed_at_utc`, `mttr_hours`, `assigned_engineer_id` (FK), `description`, `attached_documents` (lista S3 URIs).

### `Part`, `PartUsage`, `Engineer`, `Airline`, `AircraftModel` — propiedades estándar (id, nombre, atributos descriptivos).

---

## ⚡ Acciones (action types) registradas en `ontology-actions-service`

> Una **action** es una operación de escritura sobre la ontología, con permisos, validación, audit y posible desencadenamiento de workflows.

| Action ID | Sobre | Parámetros | Efecto | Permiso requerido |
|---|---|---|---|---|
| `flag-aircraft-for-inspection` | `Aircraft` | `reason: string`, `priority: enum`, `due_by: date` | crea un `MaintenanceEvent` en estado OPEN, dispara workflow `mro-inspection` | `role:mro-lead` |
| `assign-maintenance-event` | `MaintenanceEvent` | `engineer_id: string` | actualiza `assigned_engineer_id`, notifica al ingeniero | `role:mro-lead` |
| `acknowledge-delay-risk` | `Flight` | `note: string` | añade un audit entry, no cambia estado | `role:ops-controller` |
| `reroute-flight` | `Flight` | `new_destination: iata`, `reason: string` | crea un nuevo `Flight` ligado al original, notifica ATC sim | `role:ops-controller` + `approval:duty-manager` |
| `order-part` | `Part` | `quantity: int`, `requested_by: user_id`, `for_event: event_id` | crea registro en backlog de compras, notifica supply chain | `role:mro-lead` |

> Estas acciones se ejecutan desde la UI **y** las puede invocar el copiloto AIP — siempre con audit y, cuando aplica, con aprobación humana.

---

## 📥 Carga de la ontología en `ontology-definition-service`

El servicio acepta una definición **declarativa** YAML/JSON. Plantilla:

```yaml
ontology:
  id: aviation-poc
  version: 1
  description: "Ontology for OpenFoundry Aviation PoC"

  object_types:
    - id: Aircraft
      primary_key: tail_number
      backed_by:
        dataset: curated.aircraft
        branch: main
      properties:
        - { id: tail_number, type: string, required: true }
        - { id: serial_number, type: string }
        - { id: model_id, type: string }
        - { id: manufacturer, type: string }
        - { id: year_built, type: int }
        - { id: engine_count, type: int }
        - { id: airline_id, type: string }
        - { id: home_base_airport, type: string }
        - { id: total_flight_hours, type: float, computed: true }
        - { id: last_inspection_date, type: date, computed: true }
        - { id: current_status, type: enum, values: [IN_FLIGHT, ON_GROUND, IN_MAINTENANCE, GROUNDED] }

    - id: Flight
      primary_key: flight_id
      backed_by:
        dataset: curated.flights
        branch: main
      properties:
        - { id: flight_id, type: string, required: true }
        - { id: flight_number, type: string }
        - { id: aircraft_tail_number, type: string }
        - { id: origin_iata, type: string }
        - { id: destination_iata, type: string }
        - { id: scheduled_departure_utc, type: timestamp }
        - { id: actual_departure_utc, type: timestamp, nullable: true }
        - { id: dep_delay_minutes, type: int, nullable: true }
        - { id: delay_root_cause, type: enum, nullable: true,
            values: [WEATHER, MAINTENANCE, ATC, CARRIER, SECURITY, LATE_AIRCRAFT] }
        - { id: distance_km, type: float }
        - { id: risk_score, type: float, computed: true }
        - { id: risk_band, type: enum, values: [LOW, MEDIUM, HIGH, CRITICAL], computed: true }

    # ... resto: Airport, WeatherObservation, MaintenanceEvent, Part, PartUsage,
    #     Engineer, Airline, AircraftModel, FlightSegment

  link_types:
    - { id: OPERATED_BY,    from: Flight,            to: Aircraft,         cardinality: N-1 }
    - { id: DEPARTS_FROM,   from: Flight,            to: Airport,          cardinality: N-1 }
    - { id: ARRIVES_AT,     from: Flight,            to: Airport,          cardinality: N-1 }
    - { id: HAS_EVENT,      from: Aircraft,          to: MaintenanceEvent, cardinality: 1-N }
    - { id: ASSIGNED_TO,    from: MaintenanceEvent,  to: Engineer,         cardinality: N-1 }
    - { id: USED_PART,      from: MaintenanceEvent,  to: PartUsage,        cardinality: 1-N }
    - { id: OF_PART,        from: PartUsage,         to: Part,             cardinality: N-1 }
    - { id: TRACKED_BY,     from: Flight,            to: FlightSegment,    cardinality: 1-N }
    - { id: INFLUENCED_BY,  from: Flight,            to: WeatherObservation, cardinality: N-N, computed: true }
    - { id: COMPATIBLE_WITH, from: Part,             to: AircraftModel,    cardinality: N-N }

  action_types:
    - id: flag-aircraft-for-inspection
      target: Aircraft
      params:
        - { id: reason,    type: string,   required: true }
        - { id: priority,  type: enum, values: [LOW, MEDIUM, HIGH, CRITICAL] }
        - { id: due_by,    type: date }
      effect:
        - kind: create
          object: MaintenanceEvent
          fields:
            tail_number: "{{target.tail_number}}"
            event_type: "DEFECT_FINDING"
            severity: "{{params.priority}}"
            discovered_at_utc: "{{now()}}"
            description: "{{params.reason}}"
        - kind: trigger_workflow
          workflow: mro-inspection
      auth:
        required_roles: [mro-lead]
      audit: true

    - id: acknowledge-delay-risk
      target: Flight
      params:
        - { id: note, type: string, required: true }
      effect:
        - kind: audit_only
      auth:
        required_roles: [ops-controller]

    # ... resto: assign-maintenance-event, reroute-flight, order-part
```

### Comando de carga
```bash
curl -X POST https://poc.openfoundry.dev/api/ontology/v1/definitions \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/yaml" \
  --data-binary @PoC/assets/ontology-aviation.yaml
```

> Tarea pendiente: **crear `PoC/assets/ontology-aviation.yaml`** con el YAML completo. **No crear ahora** (decisión: dejar la plantilla aquí en el `.md` y materializarla cuando se implemente).

---

## 🔍 Ejemplos de queries que el cliente verá funcionar

### 1) Aviones con más eventos críticos en 30 días
```
ONTOLOGY MATCH (a:Aircraft)-[:HAS_EVENT]->(e:MaintenanceEvent)
WHERE e.severity = 'CRITICAL'
  AND e.discovered_at_utc >= now() - INTERVAL '30 days'
RETURN a.tail_number, a.model_id, count(e) AS critical_events
ORDER BY critical_events DESC
LIMIT 10
```

### 2) Vuelos en riesgo HIGH/CRITICAL llegando a JFK en próximas 4 h
```
ONTOLOGY MATCH (f:Flight)-[:ARRIVES_AT]->(a:Airport {iata:'JFK'})
WHERE f.risk_band IN ['HIGH','CRITICAL']
  AND f.scheduled_arrival_utc BETWEEN now() AND now() + INTERVAL '4 hours'
RETURN f.flight_number, f.aircraft_tail_number, f.risk_score, f.scheduled_arrival_utc
```

### 3) Defectos ATA-27 recientes en flota A320 (UC-3)
```
ONTOLOGY MATCH (a:Aircraft)-[:IS_MODEL_OF]->(m:AircraftModel),
               (a)-[:HAS_EVENT]->(e:MaintenanceEvent)
WHERE m.family = 'A320'
  AND e.defect_code STARTS WITH '27-'
  AND e.discovered_at_utc >= now() - INTERVAL '60 days'
RETURN a.tail_number, e.defect_code, e.severity, e.discovered_at_utc
ORDER BY e.discovered_at_utc DESC
```

---

## ✅ Acciones concretas (cuando se ejecute la PoC)

1. Materializar `PoC/assets/ontology-aviation.yaml` desde la plantilla.
2. Cargarlo en `ontology-definition-service`.
3. Ejecutar las 3 queries de arriba como **smoke test** y validar que devuelven > 0 filas.
4. Asignar permisos a los roles `ops-controller` y `mro-lead` sobre las acciones correspondientes.
5. Validar que un usuario `ana` (ops-controller) **no** puede ejecutar `flag-aircraft-for-inspection` (debe darle 403).
