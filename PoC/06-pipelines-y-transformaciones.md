# 06 — Pipelines y transformaciones

> Esquema medallion **bronze → silver → gold → ontology**, con calidad en cada salto y lineage automático. Todos los pipelines se definen en `pipeline-authoring-service` y se ejecutan vía `pipeline-build-service` + `pipeline-schedule-service`.

---

## 🥉 Capa Bronze — datos crudos

| Pipeline | Trigger | Input | Output | Frecuencia |
|---|---|---|---|---|
| `bz-opensky-batch` | manual / cron | trino://opensky/state_vectors_data4 | `bronze.opensky_states` (Iceberg) | one-shot histórico |
| `bz-opensky-stream` | event | kafka:`opensky.states.live` | `bronze.opensky_states_live` (Iceberg, micro-batch) | continuo |
| `bz-noaa-hrrr` | cron diario | s3://noaa-hrrr-bdp-pds | `bronze.noaa_hrrr` | diario |
| `bz-bts` | manual | https BTS zips | `bronze.bts_ontime` | one-shot histórico |
| `bz-faa-registry` | manual | https FAA | `bronze.faa_aircraft_registry` | mensual |
| `bz-mro-synth` | manual | generador | `bronze.mro_work_orders`, `bronze.mro_parts`, `bronze.mro_inspections` | one-shot |
| `bz-airports` | manual | OurAirports | `bronze.airports`, `bronze.runways` | mensual |

**Reglas de calidad bronze (Great Expectations / Soda):**
- Schema fijo (no nuevas columnas inesperadas).
- `not_null` sobre PK candidata.
- `row_count > 0` por partición.

---

## 🥈 Capa Silver — limpio, tipado, deduplicado, enriquecido

| Pipeline | Inputs | Output | Lógica clave |
|---|---|---|---|
| `sv-aircraft` | `bronze.faa_aircraft_registry` | `silver.aircraft` | Dedup por tail_number; mapping a modelos canónicos |
| `sv-airports` | `bronze.airports`, `bronze.runways` | `silver.airports` | Validar lat/lon, IATA único |
| `sv-flights-historical` | `bronze.bts_ontime` | `silver.flights_historical` | Convertir tiempos locales → UTC; canonicalizar carrier codes |
| `sv-flight-segments-batch` | `bronze.opensky_states` | `silver.flight_segments` | Segmentar por `(icao24, callsign)` y huecos > 30 min |
| `sv-flight-segments-stream` | `bronze.opensky_states_live` | `silver.flight_segments_live` | Idem, micro-batch 1 min |
| `sv-weather-by-airport` | `bronze.noaa_hrrr` | `silver.weather_by_airport` | Reproject GRIB → punto más cercano a aeropuerto; agregación horaria |
| `sv-mro-clean` | `bronze.mro_*` | `silver.mro_*` | Tipado, normalización ATA codes |

**Reglas de calidad silver:**
- `aircraft.tail_number` único.
- `flights_historical.distance_km > 0` y `< 20000`.
- `flight_segments.lat` ∈ [-90, 90], `lon` ∈ [-180, 180].
- `weather_by_airport`: `wind_speed_kt < 250` (filtra outliers).

---

## 🥇 Capa Gold — agregaciones de negocio + features ML

| Pipeline | Inputs | Output | Para qué |
|---|---|---|---|
| `gd-flights-enriched` | `silver.flights_historical` + `silver.aircraft` + `silver.weather_by_airport` | `gold.flights_enriched` | Vuelo + meteo origen/destino + datos avión |
| `gd-aircraft-utilization` | `silver.flight_segments` | `gold.aircraft_utilization` | Horas voladas/día por tail |
| `gd-recurring-defects` | `silver.mro_work_orders` + `silver.aircraft` | `gold.recurring_defects` | Detección por (model, ATA chapter) en ventanas móviles |
| `gd-airport-load` | `silver.flights_historical` + `silver.flight_segments_live` | `gold.airport_load` | Carga prevista vs real por aeropuerto/hora |
| `gd-delay-features` | `gold.flights_enriched` + `gold.aircraft_utilization` | `gold.delay_features` | Features para `delay_risk_predictor` |

**Reglas de calidad gold:**
- Cobertura ≥ 95% (no más del 5% de filas con campos clave nulos).
- Drift de distribución vs ventana anterior < 20% (reglas Soda).

---

## 🧠 Capa Modelo — predictor de riesgo de retraso

### Pipeline `delay_risk_predictor`
- **Tipo:** binario (HIGH/CRITICAL = 1, resto = 0) + score continuo.
- **Modelo:** GBT (LightGBM) entrenado con `gold.delay_features`.
- **Features:**
  - `dep_hour`, `dep_dayofweek`, `month`
  - `origin_avg_delay_30d`, `dest_avg_delay_30d`
  - `aircraft_age_years`, `tail_avg_delay_30d`
  - `wind_speed_origin`, `visibility_origin`, `convective_origin`
  - `wind_speed_dest`, `visibility_dest`, `convective_dest`
  - `route_distance_km`, `airline_id` (one-hot)
  - `late_aircraft_propagation_score` (cuánto llega tarde el tail al origen)
- **Output:** `gold.flight_risk_predictions` con `(flight_id, risk_score, risk_band)`.
- **Servicio:** `model-serving-service` lo expone como REST; consumido por `gd-flights-enriched-with-risk`.

> Para la PoC podemos entrenar **una sola vez** con datos 2023–2024 y servir inferencias en streaming. El acto del copiloto consume estas predicciones.

---

## 🔭 Capa Ontology — vistas materializadas que sirven `ontology-query-service`

| Vista materializada | Procede de |
|---|---|
| `ontology.aircraft` | `silver.aircraft` + agregados de `silver.mro_*` |
| `ontology.flights` | `gold.flights_enriched` + `gold.flight_risk_predictions` |
| `ontology.airports` | `silver.airports` |
| `ontology.weather_observations` | `silver.weather_by_airport` (rolling 14 días) |
| `ontology.maintenance_events` | `silver.mro_work_orders` |
| `ontology.parts` | `silver.mro_parts` |
| `ontology.engineers` | `silver.mro_engineers` |

Refresco: incremental cada 5 min (gold) y append-only (streaming).

---

## 📐 Especificación declarativa de pipelines (formato esperado por `pipeline-authoring-service`)

```yaml
pipeline:
  id: gd-flights-enriched
  description: "Flights joined with weather + aircraft master"
  version: 1
  schedule:
    cron: "*/15 * * * *"   # cada 15 min
    catchup: false
  inputs:
    - dataset: silver.flights_historical@main
    - dataset: silver.aircraft@main
    - dataset: silver.weather_by_airport@main
  output:
    dataset: gold.flights_enriched@main
    write_mode: merge
    merge_key: [flight_id]
  transform:
    engine: spark
    language: sql
    code: |
      WITH wx_origin AS (
        SELECT station_iata, valid_at_utc, wind_speed_kt AS wind_origin,
               visibility_m AS vis_origin, convective_sigmet AS conv_origin
        FROM silver.weather_by_airport
      ),
      wx_dest AS (
        SELECT station_iata, valid_at_utc, wind_speed_kt AS wind_dest,
               visibility_m AS vis_dest, convective_sigmet AS conv_dest
        FROM silver.weather_by_airport
      )
      SELECT
        f.flight_id,
        f.flight_number,
        f.airline_id,
        f.aircraft_tail_number,
        f.origin_iata,
        f.destination_iata,
        f.scheduled_departure_utc,
        f.actual_departure_utc,
        f.dep_delay_minutes,
        f.delay_root_cause,
        f.distance_km,
        a.model_id,
        a.year_built,
        wo.wind_origin, wo.vis_origin, wo.conv_origin,
        wd.wind_dest,   wd.vis_dest,   wd.conv_dest
      FROM silver.flights_historical f
      LEFT JOIN silver.aircraft a
             ON f.aircraft_tail_number = a.tail_number
      LEFT JOIN wx_origin wo
             ON wo.station_iata = f.origin_iata
            AND wo.valid_at_utc = date_trunc('hour', f.scheduled_departure_utc)
      LEFT JOIN wx_dest wd
             ON wd.station_iata = f.destination_iata
            AND wd.valid_at_utc = date_trunc('hour', f.scheduled_arrival_utc)
  quality:
    expectations:
      - column: flight_id
        rule: not_null
      - column: flight_id
        rule: unique
      - column: distance_km
        rule: between
        min: 0
        max: 20000
      - rule: row_count_min
        value: 1000
  lineage:
    auto: true
    emit_to: openlineage://lineage-service
```

> Tarea pendiente al ejecutar: generar 12 ficheros YAML en `PoC/assets/pipelines/` (uno por pipeline). Aquí dejamos plantilla y reglas; la materialización se hace cuando ejecutemos.

---

## 🌳 Branches y *time travel* (Foundry-style)

Patrón a demostrar en el Acto 6:

1. Crear branch `feat/risk-model-v2` desde `main` sobre el dataset `gold.flights_enriched`:
   ```bash
   curl -X POST .../api/datasets/v1/gold.flights_enriched/branches \
     -d '{"name":"feat/risk-model-v2","from":"main"}'
   ```
2. Reentrenar `delay_risk_predictor` en la branch.
3. Comparar métricas en branch vs `main` lado a lado.
4. Merge si OK; rollback si KO. Ambos quedan en audit y lineage.

---

## 🧪 Smoke tests por pipeline

Tras cada despliegue, ejecutar:

```bash
# Cada pipeline silver/gold debe terminar < 3 min y no romper expectations
for p in sv-aircraft sv-airports sv-flights-historical sv-weather-by-airport \
         gd-flights-enriched gd-recurring-defects gd-airport-load gd-delay-features; do
  curl -X POST .../api/pipelines/v1/$p/runs \
       -d '{"trigger":"manual"}'
done

# Validar
curl .../api/pipelines/v1/runs?status=FAILED&since=now-10m
# Debe devolver lista vacía
```

---

## ✅ Acciones concretas (cuando se ejecute la PoC)

1. Materializar `PoC/assets/pipelines/*.yaml` (12 ficheros) desde las plantillas.
2. Registrar conectores en `connector-management-service` (S3 NOAA, Trino OpenSky, REST OpenSky live, HTTPS BTS).
3. Lanzar bronze históricos (esperar varias horas — son TBs).
4. Lanzar silver y gold incrementalmente.
5. Entrenar `delay_risk_predictor` y publicar en `model-serving-service`.
6. Materializar vistas `ontology.*` y verificar latencia de query.
7. Demostrar branches con `feat/risk-model-v2` antes de la demo (es el "wow" del Acto 6).
