# 01 — Visión y caso de uso

## 🎬 Narrativa única (elevator pitch para el cliente)

> *"Una compañía aérea pierde 50 millones al año por retrasos evitables y por mantenimiento reactivo. La información existe — pero está repartida entre el ERP de mantenimiento, el sistema de operaciones de vuelo, los partes meteorológicos y la cadena de suministro de piezas. OpenFoundry une todo en una sola realidad operacional, permite a los equipos coordinarse en tiempo real y deja que un copiloto de IA proponga acciones concretas. Y todo es open-source, sobre tu propia infraestructura."*

---

## 🏢 Vertical y sub-vertical

- **Vertical:** Aviación civil.
- **Sub-vertical:** **Operaciones de flota + MRO (Maintenance, Repair & Overhaul)**.
- **Análogo Palantir:** Skywise (Airbus) y Foundry MRO solutions.

---

## 👥 Personas (las dos que aparecerán en la demo)

### 👩‍💼 Ana — Operations Controller
- **Rol:** controladora de operaciones en el centro de control de aerolínea.
- **Día a día:** monitoriza vuelos en curso, detecta riesgos de retraso, decide reasignaciones.
- **Frustración hoy:** abre 6 herramientas distintas (FlightAware, Sabre, hojas Excel, email del meteorólogo, MRO system, Slack).
- **Lo que verá en la demo:** dashboard único con vuelos en vivo, alertas predictivas, capacidad de actuar.

### 👨‍🔧 Luis — MRO Maintenance Lead
- **Rol:** responsable de un hangar de mantenimiento.
- **Día a día:** prioriza órdenes de trabajo, chequea disponibilidad de piezas, decide qué avión sale a operar.
- **Frustración hoy:** descubre los defectos repetitivos en una flota 3 meses tarde, cuando alguien escribe un informe.
- **Lo que verá en la demo:** vista del avión con histórico de defectos, predicción de fallo, tarea creada por IA.

> Ambos personajes deben tener cuentas reales en Keycloak, con roles diferentes. Más detalle en [`10-seguridad-y-gobierno.md`](10-seguridad-y-gobierno.md).

---

## 🎯 Casos de uso concretos a demostrar

| ID | Caso de uso | Servicios OpenFoundry implicados | KPI mostrado |
|----|---|---|---|
| UC-1 | **Visibilidad unificada de la flota en vivo** (mapa + tabla) | `event-streaming-service`, `geospatial-intelligence-service`, `app-builder-service` | "X vuelos en vuelo, Y aterrizando próx 30 min" |
| UC-2 | **Predicción de retraso por meteo** | `pipeline-build-service`, `ontology-query-service`, `dataset-quality-service` | "12 vuelos a JFK en riesgo HIGH" |
| UC-3 | **Detección de defecto recurrente en una flota** | `ontology-query-service`, `analytical-logic-service`, `tabular-analysis-service` | "Defecto X en 7 aviones A320 en 30 días" |
| UC-4 | **Copiloto AIP responde y actúa** | `ai-application-generation-service`, `mcp-orchestration-service`, `retrieval-context-service`, `ontology-actions-service` | "Tarea creada en 3 segundos" |
| UC-5 | **Workflow MRO de extremo a extremo** | `workflow-automation-service`, `notification-alerting-service`, `approvals-service` | "SLA 4h cumplido, notificado a Luis" |
| UC-6 | **Branch de dataset + time travel** ("Foundry-style") | `dataset-versioning-service`, `global-branch-service`, `lineage-service` | "Versión auditable de cada decisión" |
| UC-7 | **Audit & gobierno** | `audit-compliance-service`, `authorization-policy-service` | "100% acciones trazadas a usuario" |

---

## 📈 KPIs que mostraremos en el cierre

Estos números **deben aparecer en un panel** al final de la demo (extraídos de `execution-observability-service` y `telemetry-governance-service`):

- **Volumen ingerido:** ≥ 1 TB.
- **Filas analizables:** ≥ 4.000 millones.
- **Vuelos modelados:** ≥ 50 millones (12 meses BTS + OpenSky).
- **Aviones en la ontología:** ≥ 30.000 (registros tail-number reales).
- **Pipelines activos:** ≥ 12.
- **Latencia de query p95** sobre la ontología: < 2 s.
- **Latencia ingesta streaming → dashboard:** < 5 s.
- **Acciones del copiloto trazadas en audit:** 100%.
- **Cero datos sensibles expuestos.**

---

## 🚦 Alcance — qué SÍ y qué NO

### ✅ Sí entra en el PoC
- Streaming ADS-B (OpenSky) — últimos 6 meses + ventana en vivo durante la demo.
- Histórico BTS On-Time 2018–2024 (USA).
- Meteo NOAA HRRR/GFS — últimos 6 meses sobre USA + Europa.
- Sintético MRO (work orders, parts, defects) generado a partir de los tail numbers reales.
- 1 idioma de UI (inglés) y 1 zona horaria de referencia (UTC) con conversión por usuario.

### ❌ No entra en el PoC
- Datos PII reales de pasajeros o tripulación.
- Integración con sistemas reales del cliente (eso es siguiente fase: piloto).
- Certificación regulatoria (Part-145, EASA, FAA AC).
- Multi-tenant complejo — un único tenant `acme-airlines` para la PoC.
- Mobile app — solo web responsive.

---

## 📅 Línea temporal de un día simulado de la demo

Para que los datos en vivo "tengan sentido", la demo se ancla en un **"día operacional simulado"**:

- **T0 = momento de la demo.**
- Entre **T0-7 días y T0** ingestamos datos batch (BTS + NOAA + sintético).
- Durante la demo conectamos **stream OpenSky en vivo** — el cliente ve aviones reales moviéndose.
- El copiloto razona sobre "hoy" usando el corte estable batch + el stream.

---

## ✅ Acciones concretas (cuando se ejecute la PoC)

1. Confirmar con el cliente que la vertical aviación le encaja (si es banca o salud, hay que rehacer 03 y 05).
2. Crear los dos usuarios `ana@acme-airlines.demo` y `luis@acme-airlines.demo` con roles distintos.
3. Definir los **3 mensajes que el cliente debe llevarse a casa** (escribirlos en una sola frase). Sugeridos:
   - *"OpenFoundry conecta datos heterogéneos a escala TB en una ontología viva."*
   - *"Convierte insights en acción mediante workflows y copiloto IA."*
   - *"Es open-source, self-hosted y auditable — sin lock-in."*
4. Validar el rango de fechas del "día simulado" 48h antes de la demo.
