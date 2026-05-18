# 🛫 OpenFoundry PoC — Aviation / MRO

> **Status:** documentation ready. The PoC **is not being executed yet** — we are waiting for the OpenFoundry MVP to reach a viable level. The PoC is now governed by the Foundry-native contract in [`00-contrato-foundry-native.md`](00-contrato-foundry-native.md): user-visible behavior must match how the workflow would be built in Palantir Foundry.
>
> **Chosen vertical:** Civil aviation (flight operations + MRO maintenance + meteorology + parts supply chain).
>
> **Target customer:** an organization with an aircraft fleet or aviation infrastructure that already recognizes the "Skywise / Palantir Foundry" use case and wants an open-source, self-hosted alternative.

---

## 🎯 PoC objective

Demonstrate to a customer, in a **45–60 minute** session, that OpenFoundry can:

1. **Ingest** heterogeneous real-world data (streaming + batch + files) at scale (**≥ 1 TB**).
2. **Model** an operational ontology (`Flight`, `Aircraft`, `Airport`, `MaintenanceEvent`, `WeatherObservation`, `Part`, `Crew`).
3. **Build** versioned pipelines with end-to-end **lineage** and automated **quality** checks.
4. **Visualize** operations in a *Quiver*-style dashboard and a *Workshop*-style app.
5. **Reason with AI** through an (AIP-like) copilot that queries the ontology and triggers actions.
6. **Coordinate** through workflows: turn an insight into an assigned task, with SLA and notification.
7. **Govern** with RBAC, audit log, dataset *branches* and *time travel*.

The **single message** for the customer: *"this PoC follows the same operational pattern a Foundry team would use — data connections, datasets, pipelines, ontology, actions, Workshop, AIP, lineage, governance and branching — implemented in OpenFoundry with explicit parity checks and no unverified parity claims."*

---

## 📚 Document index

| # | Document | Purpose |
|---|---|---|
| 00 | [`00-contrato-foundry-native.md`](00-contrato-foundry-native.md) | Non-negotiable Foundry-native parity contract, public documentation baseline, gaps, and OpenFoundry code adaptation checklist |
| 01 | [`01-vision-y-caso-de-uso.md`](01-vision-y-caso-de-uso.md) | Vertical, personas, business KPIs, scope |
| 02 | [`02-arquitectura-y-servicios.md`](02-arquitectura-y-servicios.md) | Which microservices to spin up from the 50 current service directories (~15 subset; see [`docs/reference/repository-layout.md`](../docs/reference/repository-layout.md)) |
| 03 | [`03-datasets-y-fuentes-de-datos.md`](03-datasets-y-fuentes-de-datos.md) | How to obtain ≥ 1 TB of real, legal data |
| 04 | [`04-infraestructura-y-despliegue.md`](04-infraestructura-y-despliegue.md) | Hardware, cloud, deployment with compose/k8s |
| 05 | [`05-ontologia-aviacion.md`](05-ontologia-aviacion.md) | Entities, properties, relationships, actions |
| 06 | [`06-pipelines-y-transformaciones.md`](06-pipelines-y-transformaciones.md) | Batch + streaming pipelines, quality, lineage |
| 07 | [`07-dashboards-y-app-workshop.md`](07-dashboards-y-app-workshop.md) | UI: operational dashboard + Workshop app |
| 08 | [`08-aip-copiloto-prompts.md`](08-aip-copiloto-prompts.md) | Exact copilot prompts and *system prompts* |
| 09 | [`09-workflows-y-acciones.md`](09-workflows-y-acciones.md) | Workflows, actions, notifications |
| 10 | [`10-seguridad-y-gobierno.md`](10-seguridad-y-gobierno.md) | RBAC/ABAC, audit, branches, retention |
| 11 | [`11-guion-demo.md`](11-guion-demo.md) | Minute-by-minute script for the customer session |
| 12 | [`12-checklist-preparacion.md`](12-checklist-preparacion.md) | Actionable checklist at T-30, T-7, T-1, T-0 |
| 13 | [`13-riesgos-y-plan-b.md`](13-riesgos-y-plan-b.md) | Risks, possible failures, recorded plan B |

---

## 🧭 How to use this documentation

1. **When the MVP is ready**, read [`00-contrato-foundry-native.md`](00-contrato-foundry-native.md) first, then the documents in order 01 → 13.
2. Each document is **self-contained** and has a "Concrete actions" section at the end.
3. The **literal prompts** for the copilot, the pipelines, and the service `curl` calls are in copy-paste code blocks.
4. Before the demo, fully complete [`12-checklist-preparacion.md`](12-checklist-preparacion.md).
5. If something fails live, follow [`13-riesgos-y-plan-b.md`](13-riesgos-y-plan-b.md).

---

## ⏱️ Effort estimate (indicative, depends on the MVP)

| Block | Effort |
|---|---|
| Infra provisioning and dataset download | 3–5 days |
| Modeling the ontology and loading data through pipelines | 4–7 days |
| Building the dashboard + Workshop app | 3–5 days |
| Integrating the AIP copilot and validating prompts | 3 days |
| End-to-end rehearsals + plan B recording | 2 days |
| **Realistic total** | **~3 weeks** of focused work by 1 senior engineer |

---

## 📌 Decisions already made

- **Vertical:** Aviation / MRO (analogous to the *Airbus Skywise* case).
- **Data:** combination of public sources (OpenSky + NOAA + BTS) + controlled synthetic data for maintenance.
- **Target volume:** 1.0–1.5 TB in object storage, ~4 billion analyzable rows.
- **Storage:** S3-compatible (MinIO locally, S3 in cloud), **Apache Iceberg** or **Delta Lake** format.
- **Compute:** Spark 3.5 or Apache DataFusion for batch; Kafka/Redpanda for streaming.
- **Copilot LLM:** two modes — **Ollama (Llama 3.1 70B)** local for offline demo, **Azure OpenAI GPT-4o** for online demo.
- **Frontend:** `apps/web` from the repo + specific extensions for the aviation app.
- **Foundry-native constraint:** every customer-facing artifact must map to a public Foundry concept; OpenFoundry service names are implementation details only.

---

## 🚧 What this PoC does **not** do

To be honest with the customer:

- It does not replace a certified maintenance system (AMOS, TRAX, etc.).
- It does not aim to be EASA Part-145 compliant — it is a platform proof.
- It does not demonstrate all 50 current service directories from [`docs/reference/repository-layout.md`](../docs/reference/repository-layout.md); only the subset documented in [`02-arquitectura-y-servicios.md`](02-arquitectura-y-servicios.md).
- It does not use customer data — it uses public sources. The next phase would be a *pilot* with their data.
