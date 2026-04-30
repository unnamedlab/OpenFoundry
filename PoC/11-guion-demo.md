# 11 — Guion de la demo (45–60 min)

> Este es el **script literal minuto a minuto** para presentar la PoC al cliente. Léelo en voz alta antes de cada ensayo. Lo que aparece entre `«»` son indicaciones para el presentador (no se dicen).

---

## ⏱️ Timeline general

| Tiempo | Acto | Duración | Quién |
|---|---|---|---|
| 00:00–05:00 | **Acto 0 — Apertura** | 5 min | Presentador |
| 05:00–10:00 | **Acto 1 — Conectar el caos** | 5 min | Demo |
| 10:00–20:00 | **Acto 2 — Modelar la realidad (Ontología)** | 10 min | Demo |
| 20:00–30:00 | **Acto 3 — Pipeline + calidad + lineage** | 10 min | Demo |
| 30:00–40:00 | **Acto 4 — Workshop App + dashboards** | 10 min | Demo |
| 40:00–50:00 | **Acto 5 — Copiloto AIP + workflows** | 10 min | Demo |
| 50:00–55:00 | **Acto 6 — Gobierno (RBAC + audit + branches)** | 5 min | Demo |
| 55:00–60:00 | **Acto 7 — Cierre con números + Q&A** | 5 min + Q&A | Presentador |

---

## 🎬 Acto 0 — Apertura (5 min)

«Pantalla de portada con logo OpenFoundry y nombre del cliente»

> *"Buenos días. Hoy os vamos a enseñar OpenFoundry: la alternativa open-source y self-hosted a Palantir Foundry, sobre vuestra propia infraestructura, sin lock-in. Para que sea relevante hemos elegido un caso aviación, idéntico al que Foundry resuelve en Skywise: unir operaciones de vuelo, mantenimiento, meteo y supply chain en una sola realidad operacional, con copiloto IA encima."*

> *"En 50 minutos vais a ver cómo conectamos más de 1 terabyte de datos reales — vuelos ADS-B en directo, meteo NOAA, datos públicos del DOT americano, y un MRO sintético — los modelamos como ontología, los explotamos en dashboards y app, y dejamos que un copiloto razone, proponga acciones y dispare workflows. Todo auditado, todo con permisos, todo trazable."*

«Mostrar slide con los **3 mensajes clave** (ver `01-vision-y-caso-de-uso.md`)»

> *"Si os tenéis que llevar 3 ideas: **conecta**, **acciona**, **abierto**. Empezamos."*

---

## 🔌 Acto 1 — Conectar el caos (5 min)

«Cambiar a la UI. Login como `admin@acme-airlines.demo`. Ir a `data-asset-catalog-service`.»

> *"Esto es el catálogo. Ya hemos pre-cargado en background los datasets — son TBs, no se cargan en directo, pero os enseño los conectores."*

«Click en `connector-management-service` → mostrar 4 conectores ya activos:»
- `opensky-historical` (Trino) — *"vuelos reales de toda la red ADS-B mundial"*
- `opensky-live` (REST polling 5s) — *"y este sí, en directo. Ahora mismo recibe aviones cada 5 segundos."*
- `noaa-hrrr` (S3 sync) — *"meteorología pública de NOAA. ~50 GB por mes."*
- `mro-synth` (file upload) — *"y aquí, sintético de mantenimiento, generado a partir de matrículas reales para que tenga consistencia."*

«Mostrar el lineage del catálogo (gráfica) que ya conecta bronze → silver → gold.»

> *"Esto es el lineage real, no es un dibujo. Cada flecha la genera el pipeline al ejecutarse. Más adelante os enseño cómo se usa para auditar."*

---

## 🧬 Acto 2 — Modelar la realidad: la ontología (10 min)

«Ir a `ontology-definition-service`. Mostrar el grafo de tipos.»

> *"Esto es lo más importante de Foundry, y de OpenFoundry. La ontología es el diccionario de vuestro negocio: avión, vuelo, aeropuerto, evento de mantenimiento, pieza, ingeniero. Y las relaciones entre ellos."*

«Hacer click en `Aircraft` → mostrar propiedades, relaciones, acciones disponibles.»

> *"Cada objeto tiene propiedades — tail_number, modelo, horas voladas — y acciones que se pueden ejecutar sobre él. Las acciones son lo que convierte la ontología en algo vivo, no un modelo dibujado en Confluence."*

«Buscar en la barra superior: "N12345"» «Aparece la ficha del avión con su grafo asociado.»

> *"Esto es Object Explorer. Os muestra ese avión, sus últimos vuelos, los eventos de mantenimiento abiertos, las piezas que ha gastado, el ingeniero asignado. Todo en un click. Notad que el avión está volando ahora mismo — esa información viene del stream OpenSky en directo."*

«Click en un `Flight` enlazado → ir al Flight Detail.»

> *"Y aquí veis el vuelo con sus enlaces a aeropuerto origen, destino, observaciones meteo correlacionadas, y el modelo predictor que le ha calculado un risk_score."*

---

## 🧪 Acto 3 — Pipeline, calidad y lineage (10 min)

«Ir a `pipeline-authoring-service`. Mostrar la lista de pipelines.»

> *"Tenemos 12 pipelines activos, en arquitectura medallion: bronze, silver, gold, ontology. Os abro uno: el que enriquece vuelos con meteo."*

«Abrir `gd-flights-enriched` → enseñar el SQL declarativo, los inputs versionados, las expectations de calidad.»

> *"Notad tres cosas: primero, es declarativo — versionado en git, no es un script perdido. Segundo, las expectations: si los datos rompen schema o las distribuciones, el pipeline falla y se queda en estado FAILED. Tercero, el lineage se emite automáticamente a OpenLineage."*

«Click en "Run history" → mostrar runs verdes y un FAILED reciente.»

> *"Aquí teníamos un run que falló porque `distance_km` salía negativa. Os lo dejé adrede para que veáis el sistema reaccionando."*

«Ir a `lineage-service` → mostrar el grafo end-to-end de `risk_score` para `flight AAL256`.»

> *"De dónde sale el risk_score de este vuelo. El servicio de modelo, los features, el join con meteo, los datasets bronze de origen. En auditoría regulatoria esto es oro."*

«Mostrar `dataset-quality-service` → 4 reglas verdes y 1 amarilla.»

---

## 🖥️ Acto 4 — Workshop App + dashboards (10 min)

«Logout admin → Login como `ana@acme-airlines.demo`.»

> *"Ahora soy Ana, controladora de operaciones. Esta es mi pantalla."*

«Mostrar Operations Live (ver `07-dashboards-y-app-workshop.md`).»

- *"1.247 vuelos en el aire ahora mismo. Reales — los podéis verificar en FlightRadar."*
- *"38 vuelos clasificados HIGH o CRITICAL para retraso por nuestro modelo predictor."*
- *"Mapa con colores según riesgo, overlay de meteo del NOAA…"*

«Hover en un avión del mapa → side panel.»

«Click en "AAL256" en la tabla → abre Flight Detail.»

«Tab Linked objects → click en el aircraft → muestra historial.»

> *"En 2 clicks he pasado del mapa al avión, al modelo, al historial de mantenimiento. Es lo que llamamos navegar el grafo de la ontología."*

«Cambiar a Workshop App `mro-triage-workbench`. Ya logueado como Ana, mostrar que **no puede ejecutar** acciones MRO (botones grises, tooltip "requires mro-lead role").»

> *"Vais a notar algo importante: yo, como Ana, veo todo, pero no puedo ejecutar acciones de mantenimiento. RBAC de verdad."*

«Logout → login como `luis@acme-airlines.demo`.»

> *"Ahora soy Luis, jefe de hangar. Esta es mi vista."*

«Mostrar Fleet Health: heatmap de defectos recurrentes, lista de aviones, parts at risk.»

«Click en el heatmap → drill al cluster ATA-27 en flota A320.»

> *"Aquí veo un cluster anómalo de defectos en aileron, A320, últimos 60 días. ¿Es cosa de un avión o un problema de flota? Vamos a preguntárselo al copiloto."*

---

## 🤖 Acto 5 — Copiloto AIP + workflows (10 min)

«Click "Ask AIP" en el header. Se abre el copiloto en overlay.»

«Pegar prompt **D1**:»
```
What flights arriving at JFK in the next 4 hours are at HIGH or CRITICAL risk
of delay? Show me the top 5, ordered by risk, with the main contributing
weather factor for each.
```

«Esperar respuesta (~5 s). Aparece tabla. Comentar que el copiloto ha llamado a 2 tools (visible en panel de "tool calls").»

«Pegar prompt **D2**:**
```
Tell me more about the first one. Why is its risk score so high?
```

«Comentar la explicación, citar los datos.»

«Pegar prompt **D3**:**
```
For the A320 fleet, are there any recurring defects in the last 60 days?
Highlight any ATA chapter that is statistically anomalous compared to the
prior 60-day baseline.
```

> *"Recordad: el copiloto no inventa. Está consultando la ontología. Si la información no existe, lo dice."*

«Pegar prompt **D4** (la propuesta de acción):»
```
For the affected aircraft, propose flagging them for an unscheduled inspection
within 72 hours, priority HIGH, with a clear justification linked to the
recurring ATA-27 defect.
```

> *"Atención a esto: el copiloto **propone** las acciones. No las ejecuta. Esto es human-in-the-loop. Voy a aprobar."*

«Pegar prompt **D5**:**
```
Yes, execute these actions. Assign the inspections to the engineer with the
lowest current workload at the home base of each aircraft.
```

«Las acciones se ejecutan. Aparecen toasts de notificación. Cambiar a la pestaña de `workflow-trace-service` y mostrar los workflows `mro-inspection` corriendo.»

> *"Esto es lo que más demuestra Foundry: del insight a la acción coordinada en 30 segundos, todo trazado, todo asignado, todo notificado."*

---

## 🔐 Acto 6 — Gobierno: RBAC, audit, branches (5 min)

«Logout → login como `diego@acme-airlines.demo` (mro-engineer).»

> *"Ahora soy Diego, ingeniero. Mirad qué veo."*

«Solo aparecen los `MaintenanceEvent` asignados a él. Intentar abrir el workshop app → 403.»

> *"ABAC de verdad: el filtro está a nivel de fila, no solo a nivel de pantalla."*

«Logout → login como admin. Ir a `audit-compliance-service`.»

«Filtrar por `actor.user_id = ai-copilot` y `action_id = flag-aircraft-for-inspection` últimos 5 min.»

> *"Cada acción del copiloto queda con quién la propuso, quién la confirmó, qué objetos creó, qué workflows disparó. Inmutable, replicado en S3 con object-lock."*

«Pegar prompt **D7** (branch comparison) en el copiloto:**
```
Compare the risk_score distribution for flights to JFK on the 'main' branch
vs the 'feat/risk-model-v2' branch. Are there meaningful differences?
```

«Mostrar tabla side-by-side.»

> *"Branches sobre datasets — Foundry-style. Probáis modelos nuevos en una rama, los comparáis, los mergeáis si funcionan, hacéis rollback si no. Y todo queda en lineage."*

---

## 🎯 Acto 7 — Cierre con números (5 min)

«Mostrar dashboard de Grafana de **observabilidad** (preparado para esto):»

- 1.18 TB ingeridos.
- 4.300 millones de filas analizables.
- 12 pipelines activos, ratio de éxito 99.2%.
- Latencia query p95 ontología: 1.4 s.
- Latencia ingesta stream → dashboard: 3.8 s.
- 100% acciones del copiloto en audit log.
- 0 datos PII expuestos.

> *"Esto es la PoC. ¿Qué viene después? Un piloto con vuestros datos reales, en vuestra infra, en 6 semanas. Sin lock-in. Sin pago de licencia. Y todo el código está en GitHub."*

«Mostrar `github.com/unnamedlab/OpenFoundry`.»

«Pasar a Q&A.»

---

## 🧷 Cosas que **nunca** se dicen en directo

- "Esto está en beta" / "Esto es experimental" → debilita.
- Mencionar bugs recientes.
- Tocar funciones que **no estén en el subset de los 15 servicios**.
- Improvisar prompts del copiloto fuera de D1–D7. Si el cliente pide algo nuevo: *"buena pregunta, lo enseñamos en un follow-up para no quedarnos sin tiempo"*.

---

## ✅ Acciones concretas (cuando se ejecute la PoC)

1. Imprimir este documento. Llevarlo encima.
2. Ensayar 3 veces el guion completo cronometrado.
3. Grabar el ensayo final como **plan B** (vídeo de 10 min, ver [`13-riesgos-y-plan-b.md`](13-riesgos-y-plan-b.md)).
4. Si el cliente es no-anglófono, traducir prompts D1–D7.
5. Tener pestañas del navegador pre-abiertas en el orden del guion.
