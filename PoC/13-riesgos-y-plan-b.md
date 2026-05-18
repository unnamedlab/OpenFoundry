# 13 — Risks and plan B

> A live demo always breaks somewhere. The difference between a *disaster* and a *hiccup* is how well you've planned plan B. Here are the risks by probability and impact, with the **silent mitigation** that the presenter applies without the customer noticing.

---

## 🎯 Risk matrix

| ID | Risk | Probability | Impact | Mitigation |
|---|---|:---:|:---:|---|
| R1 | Presenter loses internet | Medium | Critical | Mobile hotspot + plan B video |
| R2 | LLM (Ollama or Azure) doesn't respond / latency > 15 s | Medium | High | Replay mode with cached responses |
| R3 | OpenSky live stops emitting | Medium | Medium | Replay of the last 24h loaded into Kafka |
| R4 | Gold pipeline takes longer than expected | Low | High | Pre-compute before the demo |
| R5 | A subset service goes down | Low | Critical | Restart from AMI snapshot |
| R6 | Slow UI (> 3s render) | Medium | Medium | Warmed cache, simplified queries |
| R7 | Customer asks something outside the subset | High | Low | Prepared deflection phrases |
| R8 | "Ugly" map data or no activity | Low | Medium | Force bbox to USA/Europe with dense traffic |
| R9 | Workflow doesn't fire a notification | Low | Medium | Mailtrap pre-loaded with a sample email |
| R10 | Audit log doesn't show recent action | Low | High | Manual refresh, expected lag < 5 s |
| R11 | Branch comparison shows odd results | Medium | Medium | Pre-validate at T-1 that distributions differ and are explainable |
| R12 | Customer asks to see source code live | Medium | Low | Have `apps/web` and a Rust service open in VSCode |
| R13 | Question about license/AGPL | High | Low | Prepared answer (see §FAQ) |
| R14 | Question about enterprise cost | High | Low | Model already thought through (see §FAQ) |
| R15 | Customer wants to use it in production tomorrow | High | Low (good!) | 6-week pilot roadmap ready |

---

## 🆘 Primary plan B — recorded video

**When to activate it:** if in the first 5 min of Act 1 we see the network is down or something isn't loading.

### How
1. The presenter says: *"let's watch a recorded version so we don't waste time, and we'll do live questions at the end"*.
2. Play `PoC/assets/plan-b/demo-recording.mp4` (10 min, edited from the last rehearsal).
3. Pause the video at key moments to narrate (it's the same as in the live version).
4. After the video: live Q&A.

### Video requirements
- 1920×1080 resolution.
- Embedded subtitles (in the customer's language).
- No rehearsal audio (presenter narrates live).
- Chapters: 1 per act.
- Stored **locally**, never streamed.

---

## 🩹 Silent mitigations per risk

### R2 — LLM not responding
- Activate `AIP_REPLAY_MODE=true` via a toggle in a pre-opened admin tab.
- The copilot returns the cached response from `PoC/assets/aip-cache/D{n}.md`.
- Simulated latency of 1–2 s so it feels natural.
- The presenter **does not mention it**.

### R3 — OpenSky live goes down
- In `ingestion-replication-service` (the binary that covers both REST polling and Kafka publishing via the `event-bus-data` lib) activate the secondary source `replay-from-yesterday`:
  - Reads rows from `bronze.opensky_states_live` from the last 24h.
  - Rescales timestamps to "now".
  - Pushes to Kafka (Strimzi) as if they were live.
- The customer sees a map with traffic, doesn't spot the difference (aircraft change, callsigns change).
- Optionally show a small "Replay mode: ON" indicator in a corner if we want to be transparent (presenter's call).

### R4 — Slow pipeline
- All gold pipelines are **pre-executed at T-2 hours**.
- During the demo we only kick off one short one (`gd-airport-load`) to show the run.

### R5 — Service goes down
- Keep in a tab: `docker compose restart <service>` ready.
- AMI/volume snapshot of the known-good state → restore in 15 min if catastrophic failure.
- If the failure is the UI (`apps/web`), redirect the customer to "let's look at this from another view" while we restore.

### R6 — Slow UI
- Pre-warm the `ontology-query-service` cache at T-2h by running all the script's queries 5 times.
- If a specific screen is slow, have full-screen pre-loaded screenshots to "comment on" without rendering.

### R7 — Out-of-scope question
Prepared phrases:
- *"Good question — that lives in `<service-X>` which I haven't turned on for the demo. We'll show it in a follow-up so I don't go over time."*
- *"That's exactly what we'd tackle in the pilot with your data. With synthetic data right now it wouldn't be faithful."*
- *"I'm taking note, I'll get back to you by email tomorrow with a short video."*

### R8 — Empty map
- Forcing the bbox to `lat ∈ [25, 50], lon ∈ [-125, -65]` (CONUS) guarantees > 800 aircraft at almost any hour.
- If the demo is during European night / US early morning, move the bbox to Asia (Japan/China).

### R10 — Audit lag
- From the admin menu, force `audit-compliance-service` to flush with a button.
- Wait 5 s, refresh.

---

## 🧯 Kill everything in an emergency

If something genuinely serious happens (customer data leaked, demo out of control):
```bash
bash tools/poc-aviation/kill-switch.sh
# → tears down apps/web, serves a static page, keeps backend up
```

The presenter says: *"We have a technical issue, let's use the remaining 15 minutes for Q&A and we'll send you a recorded continuation this afternoon."* (stay calm; if you apologize with confidence, you don't lose the customer).

---

## ❓ Likely customer FAQ — prepared answers

### "How does it compare with real Foundry? Are you at parity?"
> *"In components, yes: today there are 50 service directories under `services/` (per [`docs/reference/repository-layout.md`](../docs/reference/repository-layout.md)) covering ingestion, ontology, pipelines, AIP, workflows, governance and publishing. In UX maturity and edge cases we're still behind — expected for an open-source project under active development. The key point: the core works and the customer can contribute or pay to accelerate whatever they need."*

### "What about the AGPL license?"
> *"AGPL ensures that any improvements come back to the community. For internal use it does not require you to publish anything. It only applies if you offer OpenFoundry as a SaaS to third parties. For an airline, there's no implication."*

### "Cost for production?"
> *"The software is $0. The real cost is infra (cloud or on-prem) + professional services (deployment + integrations). For your scale we estimate $X/month in infra and a team of 2 of our engineers for 3 months for the first end-to-end use case."*

### "Enterprise SLA support?"
> *"We offer a 24/7 support contract with SLA P1 < 1h, P2 < 4h. Details outside the PoC."*

### "Truly self-hosted, or do you need SaaS?"
> *"Truly. Right now on Hetzner / EKS / your own Kubernetes. No call-home, no mandatory telemetry. If you want cross-org observability, you enable it yourselves."*

### "How do you migrate our real use cases?"
> *"With a 6-week pilot: weeks 1–2 ingest 1 of your sources, 3–4 ontology and pipelines, 5 dashboard + workflow, 6 AIP copilot. Around 1 concrete use case that you specify."*

### "What about sensitive data, PII, certified data?"
> *"We support field-level encryption, ABAC and immutable audit. For regulatory certification (Part-145, EASA) we need an additional process, but the platform is compatible."*

### "What if OpenFoundry as a project dies?"
> *"AGPL → you have the code in perpetuity. Forks happen all the time. And our business model depends on enterprise support → we have a clear incentive to maintain it."*

---

## 🧠 Presenter mindset

- **Confidence without arrogance.** The customer forgives a mistake; they don't forgive panic.
- **Respect the clock.** If you're 5 min behind, skip Act 6 — don't stretch.
- **Don't make things up.** If you don't know, *"I'll confirm tomorrow"* is perfectly professional.
- **Close with a concrete next step.** Don't leave the room without an action item for both sides.

---

## ✅ Concrete actions (when the PoC is executed)

1. Generate `tools/poc-aviation/kill-switch.sh` and test it.
2. Implement `replay-from-yesterday` in `ingestion-replication-service`.
3. Implement `AIP_REPLAY_MODE` in `agent-runtime-service`.
4. Record the plan B video (10 min) after Rehearsal 3.
5. Print the **prepared phrases** and carry them on a card next to the script.
6. Have a **silent second presenter** (if in-person) who runs the silent mitigations while the main presenter narrates.
