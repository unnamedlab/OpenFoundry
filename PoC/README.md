# 🛫 PoC OpenFoundry — Aviación / MRO

> **Estado:** documentación preparada. La PoC **no se ejecuta todavía** — esperamos a que el MVP de OpenFoundry alcance un nivel viable.
>
> **Vertical elegida:** Aviación civil (operaciones de vuelo + mantenimiento MRO + meteorología + cadena de suministro de piezas).
>
> **Cliente objetivo:** organización con flota o infraestructura aeronáutica que hoy reconozca el caso "Skywise / Palantir Foundry" y quiera una alternativa open-source y self-hosted.

---

## 🎯 Objetivo de la PoC

Demostrar a un cliente, en una sesión de **45–60 minutos**, que OpenFoundry puede:

1. **Ingerir** datos heterogéneos reales (streaming + batch + ficheros) a escala (**≥ 1 TB**).
2. **Modelar** una ontología operacional (`Flight`, `Aircraft`, `Airport`, `MaintenanceEvent`, `WeatherObservation`, `Part`, `Crew`).
3. **Construir pipelines** versionados con **lineage** end-to-end y **calidad** automatizada.
4. **Visualizar** la operación en un dashboard tipo *Quiver* y una app tipo *Workshop*.
5. **Razonar con IA** mediante un copiloto (AIP-like) que consulte la ontología y dispare acciones.
6. **Coordinar** con workflows: convertir un insight en una tarea asignada, con SLA y notificación.
7. **Gobernar** con RBAC, audit log, *branches* de datasets y *time travel*.

El **mensaje único** para el cliente: *"todo lo que hoy hace Foundry, en abierto, sobre tu propia infraestructura, sin lock-in."*

---

## 📚 Índice de documentos

| # | Documento | Propósito |
|---|---|---|
| 01 | [`01-vision-y-caso-de-uso.md`](01-vision-y-caso-de-uso.md) | Vertical, personas, KPIs de negocio, alcance |
| 02 | [`02-arquitectura-y-servicios.md`](02-arquitectura-y-servicios.md) | Qué microservicios encender de los 95 (subset de ~12) |
| 03 | [`03-datasets-y-fuentes-de-datos.md`](03-datasets-y-fuentes-de-datos.md) | Cómo conseguir ≥ 1 TB de datos reales y legales |
| 04 | [`04-infraestructura-y-despliegue.md`](04-infraestructura-y-despliegue.md) | Hardware, cloud, despliegue con compose/k8s |
| 05 | [`05-ontologia-aviacion.md`](05-ontologia-aviacion.md) | Entidades, propiedades, relaciones, acciones |
| 06 | [`06-pipelines-y-transformaciones.md`](06-pipelines-y-transformaciones.md) | Pipelines batch + streaming, calidad, lineage |
| 07 | [`07-dashboards-y-app-workshop.md`](07-dashboards-y-app-workshop.md) | UI: dashboard operacional + app Workshop |
| 08 | [`08-aip-copiloto-prompts.md`](08-aip-copiloto-prompts.md) | Prompts exactos del copiloto y *system prompts* |
| 09 | [`09-workflows-y-acciones.md`](09-workflows-y-acciones.md) | Workflows, acciones, notificaciones |
| 10 | [`10-seguridad-y-gobierno.md`](10-seguridad-y-gobierno.md) | RBAC/ABAC, audit, branches, retención |
| 11 | [`11-guion-demo.md`](11-guion-demo.md) | Guion minuto a minuto de la sesión con el cliente |
| 12 | [`12-checklist-preparacion.md`](12-checklist-preparacion.md) | Checklist accionable T-30, T-7, T-1, T-0 |
| 13 | [`13-riesgos-y-plan-b.md`](13-riesgos-y-plan-b.md) | Riesgos, fallos posibles, plan B grabado |

---

## 🧭 Cómo usar esta documentación

1. **Cuando el MVP esté listo**, lee los documentos en orden 01 → 13.
2. Cada documento es **autocontenido** y tiene una sección "Acciones concretas" al final.
3. Los **prompts literales** para el copiloto, para los pipelines y para los `curl` de los servicios están en bloques de código copy-paste.
4. Antes de la demo, completa íntegramente [`12-checklist-preparacion.md`](12-checklist-preparacion.md).
5. Si algo falla en directo, sigue [`13-riesgos-y-plan-b.md`](13-riesgos-y-plan-b.md).

---

## ⏱️ Estimación de esfuerzo (orientativa, depende del MVP)

| Bloque | Esfuerzo |
|---|---|
| Provisión de infra y descarga de datasets | 3–5 días |
| Modelar ontología y cargar datos en pipelines | 4–7 días |
| Construir dashboard + Workshop app | 3–5 días |
| Integrar copiloto AIP y validar prompts | 3 días |
| Ensayos end-to-end + grabación plan B | 2 días |
| **Total realista** | **~3 semanas** de trabajo enfocado de 1 ingeniero senior |

---

## 📌 Decisiones ya tomadas

- **Vertical:** Aviación / MRO (caso *Airbus Skywise* análogo).
- **Datos:** combinación de fuentes públicas (OpenSky + NOAA + BTS) + sintético controlado para mantenimiento.
- **Volumen objetivo:** 1.0–1.5 TB en almacenamiento de objetos, ~4.000 millones de filas analizables.
- **Storage:** S3-compatible (MinIO en local, S3 en cloud), formato **Apache Iceberg** o **Delta Lake**.
- **Cómputo:** Spark 3.5 o Apache DataFusion para batch; Kafka/Redpanda para streaming.
- **LLM del copiloto:** dos modos — **Ollama (Llama 3.1 70B)** local para demo offline, **Azure OpenAI GPT-4o** para demo online.
- **Frontend:** `apps/web` del repo + extensiones específicas para la app de aviación.

---

## 🚧 Lo que **no** hace esta PoC

Para ser honestos con el cliente:

- No reemplaza un sistema de mantenimiento certificado (AMOS, TRAX, etc.).
- No pretende ser EASA Part-145 compliant — es prueba de plataforma.
- No demuestra los 95 microservicios; sólo el subset documentado en [`02-arquitectura-y-servicios.md`](02-arquitectura-y-servicios.md).
- No usa datos del cliente — usa fuentes públicas. La fase siguiente sería un *piloto* con sus datos.
