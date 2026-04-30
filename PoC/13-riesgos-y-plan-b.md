# 13 — Riesgos y plan B

> Una demo en directo siempre falla en algún punto. La diferencia entre un *desastre* y un *contratiempo* es cuán bien hayas planeado el plan B. Aquí están los riesgos por probabilidad e impacto, con la **mitigación silenciosa** que el presentador aplica sin que el cliente lo note.

---

## 🎯 Matriz de riesgos

| ID | Riesgo | Probabilidad | Impacto | Mitigación |
|---|---|:---:|:---:|---|
| R1 | Caída de internet del presentador | Media | Crítico | Hotspot móvil + plan B vídeo |
| R2 | LLM (Ollama o Azure) no responde / latencia > 15 s | Media | Alto | Replay mode con respuestas cacheadas |
| R3 | OpenSky live deja de emitir | Media | Medio | Re-play de últimas 24h cargadas en Kafka |
| R4 | Pipeline gold tarda más de lo esperado | Baja | Alto | Pre-computar antes de la demo |
| R5 | Servicio del subset cae | Baja | Crítico | Restart desde snapshot AMI |
| R6 | UI lenta (> 3s render) | Media | Medio | Cache calentado, queries simplificadas |
| R7 | Cliente pregunta algo fuera del subset | Alta | Bajo | Frases preparadas para deflectar |
| R8 | Datos en mapa "feos" o sin actividad | Baja | Medio | Forzar bbox a USA/Europa con tráfico denso |
| R9 | Workflow no dispara notificación | Baja | Medio | Mailtrap pre-cargado con email "ejemplo" |
| R10 | Audit log no muestra acción reciente | Baja | Alto | Refrescar manualmente, lag esperado < 5 s |
| R11 | Branch comparison muestra resultados raros | Media | Medio | Pre-validar en T-1 que las distribuciones son distintas y explicables |
| R12 | Cliente pide ver código fuente en directo | Media | Bajo | Tener `apps/web` y un servicio Rust abiertos en VSCode |
| R13 | Pregunta sobre licencia/AGPL | Alta | Bajo | Respuesta preparada (ver §FAQ) |
| R14 | Pregunta sobre coste enterprise | Alta | Bajo | Modelo ya pensado (ver §FAQ) |
| R15 | Cliente quiere usarlo en producción mañana | Alta | Bajo (¡bueno!) | Roadmap de 6 semanas piloto preparado |

---

## 🆘 Plan B principal — vídeo grabado

**Cuándo activarlo:** si en los primeros 5 min del Acto 1 vemos que la red está caída o que algo no carga.

### Cómo
1. El presentador dice: *"vamos a verlo en una versión grabada para no perder tiempo, y al final hacemos preguntas en vivo"*.
2. Reproduce `PoC/assets/plan-b/demo-recording.mp4` (10 min, ediciada del último ensayo).
3. Pausa el vídeo en momentos clave para narrar (es lo mismo que haría en directo).
4. Después del vídeo: Q&A en directo.

### Requisitos del vídeo
- Resolución 1920×1080.
- Subtítulos embebidos (en idioma del cliente).
- Sin audio del ensayo (presentador narra en directo).
- Capítulos: 1 por acto.
- Almacenado **en local**, nunca streaming.

---

## 🩹 Mitigaciones silenciosas por riesgo

### R2 — LLM no responde
- Activar `AIP_REPLAY_MODE=true` con un toggle en una pestaña de admin previamente abierta.
- El copiloto devuelve la respuesta cacheada de `PoC/assets/aip-cache/D{n}.md`.
- Latencia simulada 1–2 s para que parezca natural.
- El presentador **no lo menciona**.

### R3 — OpenSky live cae
- En `event-streaming-service` activar source secundario `replay-from-yesterday`:
  - Lee de `bronze.opensky_states_live` filas de las últimas 24h.
  - Reescala timestamps a "now".
  - Empuja a Kafka como si fueran live.
- El cliente ve un mapa con tráfico, no nota la diferencia (los aviones cambian, los callsigns cambian).
- Mostrar opcionalmente un "Replay mode: ON" pequeño en una esquina si queremos ser transparentes (decisión del presentador).

### R4 — Pipeline lento
- Todos los pipelines gold se **pre-ejecutan en T-2 horas**.
- Durante la demo solo lanzamos uno corto (`gd-airport-load`) para enseñar el run.

### R5 — Servicio cae
- Tener en una pestaña: `docker compose restart <servicio>` listo.
- Snapshot AMI/volumen del estado conocido bueno → restore en 15 min si caída catastrófica.
- Si la caída es UI (`apps/web`), redirigir al cliente a "vamos a verlo en otra vista" mientras restauramos.

### R6 — UI lenta
- Pre-calentar caché del `ontology-query-service` en T-2h ejecutando todas las queries del guion 5 veces.
- Si una pantalla concreta va lenta, tener pre-loaded screenshots a pantalla completa para "comentar" sin renderizar.

### R7 — Pregunta fuera de scope
Frases preparadas:
- *"Buena pregunta — eso vive en `<servicio-X>` que no he encendido para la demo. Lo enseñamos en un follow-up para no salirme de la hora."*
- *"Eso es exactamente lo que abordaríamos en el piloto con vuestros datos. Ahora con datos sintéticos sería poco fiel."*
- *"Tomo nota, te lo respondo en email mañana con un mini-vídeo."*

### R8 — Mapa vacío
- Forzar bbox a `lat ∈ [25, 50], lon ∈ [-125, -65]` (CONUS) garantiza > 800 aviones a casi cualquier hora.
- Si la demo es por la noche europea/madrugada USA, mover bbox a Asia (Japón/China).

### R10 — Audit lag
- En el menú de admin, forzar `audit-compliance-service` a flush con un botón.
- Esperar 5 s, refrescar.

---

## 🧯 Apagar todo de emergencia

Si algo realmente serio (datos del cliente filtrados, demo descontrolada):
```bash
bash tools/poc-aviation/kill-switch.sh
# → tira apps/web, deja página estática, mantiene backend
```

El presentador dice: *"Tenemos un problema técnico, vamos a hacer Q&A los 15 minutos restantes y os enviamos una continuación grabada esta tarde."* (mantiene la calma; si pides perdón con confianza, no se pierde el cliente).

---

## ❓ FAQ probable del cliente — respuestas preparadas

### "¿Cómo se compara con Foundry de verdad? ¿Estáis a paridad?"
> *"En componentes, sí: cubrimos los 25 que enumera Foundry. En madurez de UX y edge cases, todavía estamos por detrás — es lo esperable de un proyecto open-source en activo desarrollo. Lo importante: el core funciona y el cliente puede contribuir o pagar para acelerar lo que necesite."*

### "¿Qué pasa con la licencia AGPL?"
> *"AGPL protege que cualquier mejora vuelva a la comunidad. Para uso interno no os obliga a publicar nada. Solo aplica si ofrecéis OpenFoundry como SaaS a terceros. Para una aerolínea, no hay implicación."*

### "¿Coste para producción?"
> *"El software es 0. Coste real es infra (cloud o on-prem) + servicios profesionales (despliegue + integraciones). Estimamos para vuestra escala $X/mes en infra y un equipo de 2 ingenieros nuestros 3 meses para el primer caso de uso end-to-end."*

### "¿Soporte SLA enterprise?"
> *"Ofrecemos contrato de soporte 24/7 con SLA P1 < 1h, P2 < 4h. Detalles fuera de la PoC."*

### "¿Self-hosted real, o necesitáis SaaS?"
> *"Real. Hoy mismo en Hetzner / EKS / vuestro propio Kubernetes. Sin call-home, sin telemetría obligatoria. Si queréis observabilidad cruzada lo activáis vosotros."*

### "¿Cómo migráis casos reales nuestros?"
> *"Con un piloto de 6 semanas: semanas 1–2 ingestar 1 fuente vuestra, 3–4 ontología y pipelines, 5 dashboard + workflow, 6 copiloto AIP. Con 1 caso de uso concreto que nos digáis."*

### "¿Y si tenemos datos sensibles, PII, datos certificados?"
> *"Soportamos field-level encryption, ABAC y audit inmutable. Para certificación regulatoria (Part-145, EASA) necesitamos un proceso adicional, pero la plataforma es compatible."*

### "¿Qué pasa si OpenFoundry como proyecto muere?"
> *"AGPL → tenéis el código a perpetuidad. Forks habituales. Y nuestro modelo de negocio depende de soporte enterprise → tenemos incentivo claro de mantenerlo."*

---

## 🧠 Mentalidad del presentador

- **Confianza sin arrogancia.** El cliente perdona un fallo, no perdona pánico.
- **Respeta el tiempo.** Si vas con 5 min de retraso, salta el Acto 6 — no estires.
- **No inventes.** Si no sabes, *"te lo confirmo mañana"* es perfectamente profesional.
- **Cierra con un siguiente paso concreto.** No salgas de la sala sin una acción para ambos lados.

---

## ✅ Acciones concretas (cuando se ejecute la PoC)

1. Generar `tools/poc-aviation/kill-switch.sh` y probarlo.
2. Implementar `replay-from-yesterday` en `event-streaming-service`.
3. Implementar `AIP_REPLAY_MODE` en `ai-application-generation-service`.
4. Grabar el vídeo plan B (10 min) tras el Ensayo 3.
5. Imprimir las **frases preparadas** y llevarlas en una tarjeta junto al guion.
6. Tener un **segundo presentador en silencio** (si presencial) que ejecute mitigaciones silenciosas mientras el principal narra.
