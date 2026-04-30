# 12 — Checklist de preparación

> Checklist accionable. Tachar conforme se completa. Hitos por **T-30 días, T-7, T-1, T-0**. Si en T-7 hay > 5 ítems sin tachar de T-30, **postpone la demo**.

---

## 📅 T-30 días — Fundación

### Decisiones
- [ ] Confirmar fecha y hora de la demo con cliente.
- [ ] Confirmar modalidad (presencial / remota / híbrida).
- [ ] Confirmar idioma (ES/EN) y traducir prompts si aplica.
- [ ] Confirmar que el MVP de OpenFoundry tiene los 15 servicios del subset arrancables.

### Infra
- [ ] Decidir opción A/B/C de [`04-infraestructura-y-despliegue.md`](04-infraestructura-y-despliegue.md).
- [ ] Si C: provisionar AWS account dedicada + budget alert ($500/semana).
- [ ] Si B: contratar Hetzner AX102 + montar discos.
- [ ] Registrar dominios `poc.openfoundry.dev` y `keycloak.poc.openfoundry.dev`.

### Datos
- [ ] Crear cuenta OpenSky (acceso Trino confirmado).
- [ ] Solicitar EUROCONTROL R&D (toma 1–2 semanas).
- [ ] Verificar acceso a buckets S3 NOAA (sin credenciales necesarias).
- [ ] Generar y firmar licencia de uso para OurAirports (ODbL).

### Servicios
- [ ] `cargo build --workspace -p <cada servicio del subset>` → 100% verde.
- [ ] Crear `infra/docker-compose.poc-aviation.yml` con los 15 servicios.
- [ ] Crear (si C) `infra/terraform/poc-aviation/`.

---

## 📅 T-21 días — Construcción

### Pipelines y datos
- [ ] Lanzar descargas batch:
  - [ ] OpenSky histórico 12 meses (~600 GB).
  - [ ] NOAA HRRR 6 meses CONUS (~250 GB).
  - [ ] NOAA GFS 6 meses Europa (~150 GB).
  - [ ] BTS 2018–2024 (~50 GB).
  - [ ] FAA Registry, OurAirports.
- [ ] Verificar `aws s3 ls --summarize`: ≥ 1.0 TB en `s3://acme-poc/raw/`.
- [ ] Implementar y ejecutar `tools/poc-aviation/generate_mro.py` (250M filas).
- [ ] Materializar pipelines bronze (ejecutar `bz-*`).
- [ ] Materializar pipelines silver y gold.

### Ontología
- [ ] Materializar `PoC/assets/ontology-aviation.yaml`.
- [ ] Cargar en `ontology-definition-service`.
- [ ] Validar 3 queries de ejemplo (ver `05-ontologia-aviacion.md` §Ejemplos).

### Modelo
- [ ] Entrenar `delay_risk_predictor`.
- [ ] Validar AUC > 0.75 sobre BTS 2024 holdout.
- [ ] Publicar en `model-serving-service`.

---

## 📅 T-14 días — UX y Workflows

### UI
- [ ] Implementar Operations Live (3 pantallas P1, P2, P3 en `apps/web`).
- [ ] Materializar Workshop App `mro-triage-workbench` en `app-builder-service`.
- [ ] Performance: Lighthouse score > 80 en cada pantalla.

### Workflows
- [ ] Materializar `mro-inspection.yaml`, `order-critical-parts.yaml`, `weather-disruption-response.yaml`.
- [ ] Cargar en `workflow-automation-service`.
- [ ] Smoke test: lanzar `flag-aircraft-for-inspection` → workflow completo en < 30 s.

### Copiloto
- [ ] Configurar Ollama con Llama 3.1 70B (descarga 40 GB; reservar tiempo).
- [ ] Configurar Azure OpenAI fallback.
- [ ] Cargar system prompt en `ai-application-generation-service`.
- [ ] Registrar las 10 tools MCP en `mcp-orchestration-service`.

### Seguridad
- [ ] Crear realm Keycloak `openfoundry-poc`.
- [ ] Crear los 5 usuarios.
- [ ] Cargar policy YAML.
- [ ] Probar matriz RBAC (script `tools/poc-aviation/test_rbac.sh`).

---

## 📅 T-7 días — Hardening

### Validación
- [ ] Ejecutar smoke tests end-to-end (script `tools/poc-aviation/smoke.sh`):
  - [ ] Login de los 5 usuarios.
  - [ ] Cada pantalla renderiza < 3 s.
  - [ ] 3 queries ontológicas < 2 s.
  - [ ] Workflow `mro-inspection` completo < 30 s.
  - [ ] 7 prompts D1–D7 del copiloto OK.
  - [ ] 2 prompts de "ataque" bloqueados.
- [ ] Validar audit log inmutable (intentar borrar y fallar).
- [ ] Validar branch + diff + merge + rollback.

### Observabilidad
- [ ] 3 dashboards Grafana funcionando con datos reales.
- [ ] Captura de pantalla del dashboard "números finales" (Acto 7).

### Cacheo y plan B
- [ ] Generar `PoC/assets/aip-cache/D1..D7.md` desde respuestas reales.
- [ ] Probar replay mode con red al LLM cortada.
- [ ] Grabar vídeo plan B (10 min, ver [`13-riesgos-y-plan-b.md`](13-riesgos-y-plan-b.md)).

### Ensayos
- [ ] **Ensayo 1**: 1 persona, sin público.
- [ ] **Ensayo 2**: con un colega que haga preguntas tipo cliente.
- [ ] **Ensayo 3**: con red lenta simulada (`tc qdisc add dev eth0 root netem delay 200ms`).

---

## 📅 T-1 día — Sellado

### Estado del sistema
- [ ] **Snapshot completo** del entorno (AMI o volumen).
- [ ] Detener cualquier despliegue / cambio.
- [ ] Confirmar que el branch `feat/risk-model-v2` está creado y poblado.
- [ ] Confirmar que hay tráfico OpenSky live entrando (verificar lag Kafka < 10 s).

### Logística
- [ ] Confirmar enlace Zoom/Meet/sala física.
- [ ] Recordatorio al cliente con agenda y link.
- [ ] Lista de asistentes y sus roles (para personalizar comentarios).
- [ ] Tener móvil con datos como red de respaldo (hotspot).
- [ ] Cargar laptop al 100% + cargador.

### Materiales
- [ ] PDF imprimible del guion (`11-guion-demo.md`).
- [ ] Pestañas del navegador pre-abiertas en orden.
- [ ] Vídeo plan B descargado en local (no streaming).
- [ ] Slides: portada + 3 mensajes clave + cierre con números + Q&A + thank-you.

### Seguridad
- [ ] Cambiar passwords de los 5 usuarios (los anteriores pueden estar en logs).
- [ ] Verificar que el bucket de audit es **append-only**.
- [ ] Verificar `.env` no commiteado: `git status` limpio.

---

## 📅 T-0 (día de la demo)

### 2 horas antes
- [ ] Levantar stack completo (`docker compose up -d`).
- [ ] Esperar healthchecks verdes (objetivo < 4 min para los 15).
- [ ] Smoke test rápido (`tools/poc-aviation/smoke.sh --quick`).
- [ ] Calentar caché del copiloto: ejecutar D1–D7 una vez.
- [ ] Verificar OpenSky live polling.

### 30 min antes
- [ ] Cerrar todas las apps no necesarias en la laptop.
- [ ] Modo "no molestar" en sistema y móvil.
- [ ] Test de pantalla compartida (resolución 1920×1080, escalado 100%).
- [ ] Test de audio.
- [ ] Café 🙂.

### Durante
- [ ] Seguir el guion. Sin improvisación de prompts.
- [ ] Si algo falla → ver `13-riesgos-y-plan-b.md` y aplicar contingencia silenciosamente.

### Después
- [ ] Notas de Q&A en un doc separado.
- [ ] Capturas de pantalla de momentos clave.
- [ ] Snapshot final del sistema (por si hay follow-up con el cliente).
- [ ] Apagar (si C) recursos cloud para ahorrar.
- [ ] Enviar al cliente: gracias + 1 PDF resumen + 1 pregunta clara para next step.

---

## ✅ Criterio "go / no-go" en T-7

La demo se ejecuta solo si **TODOS** estos están verdes en T-7:
- [ ] Subset de 15 servicios arranca y pasa healthchecks.
- [ ] Volumen de datos ≥ 1.0 TB confirmado.
- [ ] Ontología cargada y queries < 2 s.
- [ ] 7 prompts del copiloto generan respuestas válidas.
- [ ] 1 workflow completo end-to-end < 30 s.
- [ ] Audit log inmutable validado.
- [ ] Vídeo plan B grabado.

Si **uno** falla → posponer demo 1 semana, no negociable.
