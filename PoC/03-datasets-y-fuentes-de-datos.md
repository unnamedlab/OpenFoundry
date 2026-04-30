# 03 — Datasets y fuentes de datos (≥ 1 TB reales y legales)

> **Regla de oro:** todos los datos de la PoC son **públicos, legales y de uso comercial permitido**. Cero datos del cliente, cero PII real. Si una fuente requiere licencia, está marcado.

---

## 📊 Resumen del mix de datos

| Fuente | Tipo | Volumen objetivo | Uso comercial | Coste |
|---|---|---|---|---|
| **OpenSky Network — historical state vectors** | Streaming + batch | ~600 GB (12 meses globales) | ✅ Académico/comercial con atribución | Gratis (cuenta) |
| **NOAA HRRR + GFS** (vía AWS Open Data) | Batch GRIB2 | ~400 GB (6 meses CONUS + Europa) | ✅ Dominio público USA | Gratis (egress S3 si in-region) |
| **BTS On-Time Performance** (USA DOT) | Batch CSV/Parquet | ~50 GB (1987–2024 todo) | ✅ Dominio público USA | Gratis |
| **EUROCONTROL R&D Data Archive** | Batch CSV | ~10 GB | ✅ R&D — pedir cuenta gratuita | Gratis con registro |
| **FAA Aircraft Registry** | Batch CSV | ~200 MB | ✅ Dominio público USA | Gratis |
| **OurAirports** (airports.csv, runways.csv) | Batch CSV | ~50 MB | ✅ ODbL | Gratis |
| **Sintético MRO** (work orders, parts, defects) | Batch Parquet | ~50 GB (generado) | ✅ Generado por nosotros | Gratis (compute) |
| **Total** | | **≈ 1.1 TB** | | |

> Si necesitamos engrosar a 1.5 TB: extender OpenSky a 24 meses (+600 GB) o NOAA a 12 meses (+400 GB).

---

## 1️⃣ OpenSky Network — ADS-B histórico y en vivo

**Qué es:** red colaborativa que recoge mensajes ADS-B de aviones civiles en todo el mundo. Cada `state_vector` contiene `icao24, callsign, origin_country, time_position, lon, lat, baro_altitude, on_ground, velocity, heading, vertical_rate, geo_altitude, squawk, ...`.

**Por qué es bueno para la PoC:** datos reales y verificables (el cliente puede cruzar con FlightRadar24 en su móvil), sirve a la vez para *streaming* (live API) y para *batch* (Trino/Impala histórico).

### A) Histórico (batch) — vía Trino (sucesor de Impala)
- URL: `https://opensky-network.org/data/impala`
- Acceso: cuenta gratuita en `opensky-network.org` → solicitar acceso al endpoint Trino (`trino.opensky-network.org`).
- Tabla principal: `state_vectors_data4` — **~50 GB/mes comprimido en Parquet**.

#### Plantilla de query para descarga (12 meses)
```sql
SELECT *
FROM state_vectors_data4
WHERE hour >= UNIX_TIMESTAMP('2025-04-01 00:00:00')
  AND hour <  UNIX_TIMESTAMP('2026-04-01 00:00:00')
  -- Opcional: limitar a una bbox para reducir
  AND lat BETWEEN 24 AND 72
  AND lon BETWEEN -130 AND 40
```

#### Comando de descarga (ejemplo, ejecutar el día de la PoC)
```bash
# Cliente Trino oficial
trino --server https://trino.opensky-network.org \
      --user $OPENSKY_USER --password \
      --execute "$(cat opensky_query.sql)" \
      --output-format CSV_HEADER > opensky_2025-2026.csv

# Convertir a Parquet particionado por día
python tools/csv_to_parquet.py \
  --input opensky_2025-2026.csv \
  --output s3://acme-poc/raw/opensky/ \
  --partition-by day
```

### B) En vivo (streaming) — durante la demo
- REST: `GET https://opensky-network.org/api/states/all` cada 5 s (autenticado: 1 req / 5 s).
- Conector dedicado en `connector-management-service` con tipo `opensky-rest`.

#### Configuración del conector (JSON para `connector-management-service`)
```json
{
  "name": "opensky-live",
  "type": "rest-poller",
  "endpoint": "https://opensky-network.org/api/states/all",
  "auth": { "type": "basic", "user_secret": "OPENSKY_USER", "pass_secret": "OPENSKY_PASS" },
  "poll_interval_seconds": 5,
  "sink": { "type": "kafka", "topic": "opensky.states.live" },
  "schema_inference": true
}
```

### Atribución obligatoria
> *"Data from the OpenSky Network — https://opensky-network.org"*. Aparece en el footer del dashboard.

---

## 2️⃣ NOAA HRRR + GFS — meteo

**Qué es:** modelos numéricos del NOAA. **HRRR** (High-Resolution Rapid Refresh) cubre CONUS a 3 km cada hora. **GFS** (Global Forecast System) cubre todo el planeta a 0.25°.

**Bucket S3 público (sin egress fee si vamos a us-east-1):**
- `s3://noaa-hrrr-bdp-pds/`
- `s3://noaa-gfs-bdp-pds/`

### Comando de descarga selectiva (6 meses CONUS, variables clave)
```bash
# Variables relevantes para aviación: viento, visibilidad, techo de nubes, turbulencia
VARS="UGRD|VGRD|VIS|HGT_ceiling|TURB"

aws s3 sync s3://noaa-hrrr-bdp-pds/hrrr.20251101 \
            s3://acme-poc/raw/noaa-hrrr/2025-11-01/ \
            --no-sign-request \
            --exclude "*" --include "*.wrfsfcf00.grib2" --include "*.wrfsfcf06.grib2"

# Post-proceso: GRIB2 → Parquet con xarray + cfgrib
python tools/grib_to_parquet.py \
  --input s3://acme-poc/raw/noaa-hrrr/ \
  --output s3://acme-poc/raw/noaa-hrrr-parquet/ \
  --vars "$VARS"
```

### Cómo aterriza en la ontología
- Tabla `weather_observation` (granularidad: estación más cercana a aeropuerto + hora).
- Join con `Flight` por `(scheduled_departure_airport, scheduled_departure_hour)` y `(arrival_airport, arrival_hour)`.

---

## 3️⃣ BTS On-Time Performance

**Qué es:** Bureau of Transportation Statistics. CSV mensual con cada vuelo doméstico USA: `Year, Month, DayOfMonth, FlightDate, Reporting_Airline, Tail_Number, Origin, Dest, CRSDepTime, DepTime, DepDelay, ArrDelay, CarrierDelay, WeatherDelay, NASDelay, SecurityDelay, LateAircraftDelay, Cancelled, Diverted, AirTime, Distance, ...`.

**URL:** `https://www.transtats.bts.gov/PREZIP/On_Time_Reporting_Carrier_On_Time_Performance_1987_present_<YYYY>_<MM>.zip`

### Script de descarga masiva
```bash
for year in $(seq 2018 2024); do
  for month in $(seq -w 1 12); do
    url="https://www.transtats.bts.gov/PREZIP/On_Time_Reporting_Carrier_On_Time_Performance_1987_present_${year}_${month}.zip"
    curl -fsSL "$url" -o "/data/raw/bts/${year}_${month}.zip" || echo "skip ${year}-${month}"
  done
done

# Unzip + concat + a Parquet particionado por (year, month)
python tools/bts_to_parquet.py --input /data/raw/bts/ --output s3://acme-poc/raw/bts/
```

---

## 4️⃣ FAA Aircraft Registry + OurAirports

### FAA Aircraft Registry
- URL: `https://registry.faa.gov/database/ReleasableAircraft.zip`
- Contiene `MASTER.txt` (registros) y `ACFTREF.txt` (modelos).
- Aporta: `tail_number → manufacturer, model, year, engines, owner_type`.

### OurAirports
- URL: `https://davidmegginson.github.io/ourairports-data/`
- Ficheros: `airports.csv`, `runways.csv`, `navaids.csv`, `countries.csv`.
- Aporta: 80.000 aeropuertos con coordenadas, IATA/ICAO, elevación.

```bash
curl -L https://davidmegginson.github.io/ourairports-data/airports.csv -o airports.csv
curl -L https://davidmegginson.github.io/ourairports-data/runways.csv  -o runways.csv
```

---

## 5️⃣ EUROCONTROL R&D Data Archive (opcional, recomendado para vuelos europeos)

- URL: `https://www.eurocontrol.int/dashboard/rnd-data-archive`
- Registro gratuito, condiciones de uso: R&D + demos no comerciales (ojo: para *piloto comercial real* hay que renegociar).
- Aporta: 1 mes de muestra con trayectorias 4D, eficiencia horizontal/vertical, planes de vuelo.

---

## 6️⃣ Sintético MRO — generado por nosotros

> Necesario porque **no existe un dataset público de mantenimiento aeronáutico real** suficientemente amplio. Lo generamos a partir de los `tail_number` reales de la FAA Registry para que tenga consistencia.

### Esquema sintético

#### `work_orders.parquet`
| columna | tipo | distribución |
|---|---|---|
| `wo_id` | string | UUID |
| `tail_number` | string | sample uniforme de FAA Registry |
| `aircraft_model` | string | derivado de FAA |
| `created_at` | timestamp | uniform en 24 meses |
| `defect_code` | enum | catálogo ATA chapters (21–80) |
| `severity` | enum | LOW/MEDIUM/HIGH/CRITICAL (Pareto 70/20/8/2) |
| `discovered_during` | enum | LINE_CHECK / A_CHECK / C_CHECK / IN_FLIGHT |
| `closed_at` | timestamp nullable | NULL en ~5% (abiertos) |
| `mttr_hours` | float | LogNormal(μ=2.5, σ=1.2) |
| `parts_used` | list<string> | 0–5 part_ids |
| `assigned_engineer_id` | string | sample de 200 engineers |

#### `parts.parquet`
| columna | tipo | notas |
|---|---|---|
| `part_id` | string | catálogo de 50.000 |
| `part_name` | string | familias ATA |
| `manufacturer` | string | sample real (Honeywell, Safran…) |
| `unit_cost_usd` | float | LogNormal |
| `lead_time_days` | int | Poisson |
| `compatible_models` | list<string> | |

#### `inspections.parquet`
| columna | tipo |
|---|---|
| `inspection_id`, `tail_number`, `inspection_type` (A/B/C/D check), `scheduled_at`, `performed_at`, `findings_count` |

### Generador (a implementar al ejecutar la PoC)
```bash
# Pseudocódigo del generador
python tools/generate_mro.py \
  --tail-numbers from-faa-registry \
  --start 2024-01-01 --end 2026-04-01 \
  --output s3://acme-poc/raw/mro/ \
  --target-rows 250_000_000 \
  --seed 42
```

> **Importante:** el generador debe ser **determinista (seed fija)** para que los ensayos y la demo sean reproducibles.

### Patrones intencionales en el sintético (para que la demo sea espectacular)
- **Defecto recurrente plantado:** `defect_code = 27-30 (Flight Controls — Aileron)` con frecuencia anómala en flota A320 entre `2026-02` y `2026-04`. Esto permite que UC-3 *"detección de defecto recurrente"* sea visualmente impactante.
- **Correlación meteo–retraso:** los retrasos sintéticos del MRO se correlacionan con tormentas reales del HRRR, para que el copiloto pueda explicarlo.
- **Pieza con lead time alto:** una pieza concreta (`part_id = HW-AIL-7421`) tiene lead time 21 días — usada en el flujo del workflow MRO.

---

## 🗂️ Layout final en el lago de datos

```
s3://acme-poc/
├── raw/
│   ├── opensky/           year=YYYY/month=MM/day=DD/*.parquet
│   ├── noaa-hrrr/         year=YYYY/month=MM/day=DD/hour=HH/*.parquet
│   ├── bts/               year=YYYY/month=MM/*.parquet
│   ├── faa-registry/      master.parquet, acftref.parquet
│   ├── ourairports/       airports.parquet, runways.parquet
│   └── mro/               work_orders/, parts/, inspections/
├── staging/    (limpio, tipado, deduplicado — output de pipelines bronze→silver)
├── curated/    (tablas de la ontología — silver→gold)
└── ontology/   (vistas materializadas servidas por ontology-query-service)
```

Formato: **Apache Iceberg** sobre Parquet, particionado por fecha. Catálogo en Iceberg REST.

---

## 📜 Resumen de licencias y atribuciones (a mostrar en la app)

| Fuente | Licencia | Atribución requerida |
|---|---|---|
| OpenSky | CC-BY (académico/comercial con atribución) | "Data from the OpenSky Network" |
| NOAA HRRR/GFS | US Public Domain | "NOAA / National Weather Service" (recomendado) |
| BTS | US Public Domain | "U.S. Bureau of Transportation Statistics" (recomendado) |
| FAA Registry | US Public Domain | "FAA Aircraft Registry" (recomendado) |
| OurAirports | ODbL | "© OurAirports contributors, ODbL" |
| EUROCONTROL R&D | Específica R&D | Ver licencia descargada — solo demos no comerciales |

---

## ✅ Acciones concretas (cuando se ejecute la PoC)

1. Crear cuentas: OpenSky (Trino), EUROCONTROL R&D, AWS (acceso S3 a NOAA).
2. Provisionar bucket `s3://acme-poc` con 2 TB (margen).
3. Desarrollar 4 scripts en `/tools/poc-aviation/`:
   - `download_opensky.sh` (batch)
   - `download_noaa.sh`
   - `download_bts.sh`
   - `generate_mro.py`
4. Lanzar las descargas con **3–5 días de antelación** (ancho de banda crítico para NOAA).
5. Verificar el TB: `aws s3 ls s3://acme-poc --recursive --summarize --human-readable | tail -3`.
6. Cargar el footer de licencias en la UI antes del primer ensayo.
