# 04 — Infraestructura y despliegue

> Mover ≥ 1 TB de forma creíble exige una infraestructura proporcionada. Una laptop "pelada" hace que la demo sea lenta y poco creíble. Aquí están las **3 opciones** (local, on-prem dedicado, cloud), con costes y comandos.

---

## 🎚️ Opción A — Local en laptop potente (solo ensayos)

**Útil para:** desarrollo, ensayos sin red, prototipos.
**No recomendado** para la demo final con cliente.

| Componente | Mínimo |
|---|---|
| CPU | 12 cores (Apple M2 Max o Ryzen 7) |
| RAM | 64 GB |
| Disco | 2 TB NVMe libres |
| OS | macOS 14+ o Linux con kernel ≥ 6 |

Limitación clave: **no puedes mover 1 TB de NOAA en local en tiempo razonable**. En esta opción, *el TB es un dataset montado read-only desde un disco USB SSD pre-cargado*.

---

## 🖥️ Opción B — Servidor dedicado (Hetzner / OVH / propio)

**Útil para:** demo en remoto controlado por nosotros, ensayos repetidos, coste fijo.
**Recomendado** si la demo será en remoto vía pantalla compartida.

### Hardware sugerido (1 nodo)
- **Hetzner AX102** o equivalente:
  - AMD Ryzen 9 7950X3D (16C/32T)
  - 128 GB DDR5 ECC
  - 2× NVMe 1.92 TB (RAID-1) para sistema + 4× SSD 7.68 TB (RAID-10) para datos
  - 1 Gbit unmetered
- **Coste:** ~150 €/mes
- **Localización:** Helsinki o Falkenstein (latencia OK desde Europa).

### Layout en disco
```
/data/minio        → 4 TB (RAID-10)  → object storage
/data/postgres     → 200 GB
/data/kafka-logs   → 200 GB
/data/spark-shuffle→ 500 GB
/var/lib/docker    → 100 GB
```

### Despliegue
```bash
# Provisión
ssh root@poc-server
apt update && apt install -y docker.io docker-compose-plugin git
git clone https://github.com/unnamedlab/OpenFoundry.git
cd OpenFoundry && cp .env.example .env

# Editar .env con secretos reales (Keycloak admin, MinIO root, OpenSky creds, Azure OpenAI key)
$EDITOR .env

# Levantar stack PoC (overlay a crear cuando ejecutemos)
docker compose -f compose.yaml -f infra/docker-compose.poc-aviation.yml up -d

# Verificar
docker compose ps
```

---

## ☁️ Opción C — Cloud (AWS, recomendado para demo "wow")

**Útil para:** demo presencial con muchos asistentes, demos repetidas, máxima fiabilidad de red.
**Recomendado** para la presentación final al cliente.

### Topología AWS
| Recurso | Tamaño | Coste aprox/día encendido |
|---|---|---|
| 1× EC2 `m6i.2xlarge` (control plane + Postgres + Redis + Keycloak) | 8 vCPU, 32 GB | $9 |
| 3× EC2 `r6i.4xlarge` (workers Spark + servicios pesados) | 16 vCPU, 128 GB c/u | $90 |
| EBS gp3, 2 TB total | 16k IOPS | $7 |
| S3 `acme-poc` | 1.5 TB stored | $35/mes (no/día) |
| MSK Serverless (Kafka gestionado) | Bajo throughput | $20 |
| OpenSearch t3.medium x 2 | | $10 |
| ALB + Route53 + ACM | | $2 |
| **Total encendido (8h demo)** | | **~$45/día** |
| Total siempre encendido (mes) | | ~$3.500/mes |

> **Apagar entre demos**. Stop EC2 + scale-to-zero MSK reduce coste a ~$50/mes (solo storage).

### Región
**`us-east-1`** — para no pagar egress de los buckets `noaa-*-bdp-pds` (están allí).

### Despliegue (Terraform + Helm)
> A construir cuando se ejecute la PoC. Tareas pendientes:

1. `infra/terraform/poc-aviation/` con: VPC, subnets, EKS o EC2 ASG, S3, MSK, IAM, Route53.
2. `infra/helm/poc-aviation/values.yaml` con los 15 servicios habilitados.
3. `make poc-up` y `make poc-down` en el `Makefile` raíz para idempotencia.

---

## 🌐 DNS y certificados

Para la demo, registrar dos URLs:
- `poc.openfoundry.dev` → UI principal (Workshop App + dashboards).
- `keycloak.poc.openfoundry.dev` → login.

Certificados via **Let's Encrypt** (cert-manager si Kubernetes; certbot si compose).

---

## 🔌 Conectividad de red mínima

| Endpoint externo | Por qué | Mínimo recomendado |
|---|---|---|
| `opensky-network.org` | Streaming live | latencia < 200 ms |
| `*.s3.amazonaws.com` (NOAA) | Descarga batch | 1 Gbps |
| `transtats.bts.gov` | BTS | 100 Mbps |
| Azure OpenAI (si fallback) | LLM | latencia < 500 ms |
| Cliente final (asistentes) | Acceso UI | 10 Mbps por participante |

---

## 📈 Sizing de cómputo (Spark)

Para que las queries de la demo respondan en < 2 s y los pipelines en < 3 min:

| Recurso | Configuración |
|---|---|
| `spark.executor.instances` | 12 |
| `spark.executor.memory` | 8g |
| `spark.executor.cores` | 4 |
| `spark.driver.memory` | 8g |
| `spark.sql.shuffle.partitions` | 200 |
| `spark.sql.adaptive.enabled` | `true` |
| `spark.sql.adaptive.coalescePartitions.enabled` | `true` |
| Iceberg `write.target-file-size-bytes` | 134217728 (128 MB) |

---

## 🔐 Secretos

Guardar **fuera del repo** (usar `.env` no commiteado, AWS Secrets Manager o Vault si producción):
- `OPENSKY_USER`, `OPENSKY_PASS`
- `KEYCLOAK_ADMIN_PASS`
- `MINIO_ROOT_USER`, `MINIO_ROOT_PASS`
- `POSTGRES_PASSWORD`
- `AZURE_OPENAI_API_KEY` (si usamos fallback)
- `OLLAMA_HOST` (interno)

Verificar antes de la demo: `grep -RIn "PASSWORD\|API_KEY\|SECRET" PoC/ infra/` debe devolver **0 resultados** salvo plantillas con `<placeholder>`.

---

## 📊 Observabilidad mínima

Tres dashboards de Grafana visibles **solo a los presentadores** (no al cliente, salvo el Acto 6):

1. **Health overview** — uptime de los 15 servicios, error rate.
2. **Pipeline throughput** — filas procesadas/min, lag de Kafka, particiones Iceberg.
3. **Query latency** — p50/p95/p99 de `ontology-query-service` y `geospatial-intelligence-service`.

Grabar capturas de pantalla durante el ensayo final, **se usan en el Acto 7 (cierre)** para mostrar números reales.

---

## ✅ Acciones concretas (cuando se ejecute la PoC)

1. Decidir A vs B vs C según presupuesto y modalidad de demo.
2. Si C: lanzar Terraform 1 semana antes y dejar `terraform destroy` listo.
3. Si B: provisionar Hetzner 2 semanas antes para tener tiempo de reinstalar si algo falla.
4. Configurar DNS y certificados 5 días antes (TTL bajos para poder cambiar el día D).
5. Provisionar Grafana y validar que los 3 dashboards reciben datos.
6. Guardar `terraform.tfstate` o el snapshot del servidor — la noche antes de la demo, **snapshot completo** para poder restaurar en 15 min.
