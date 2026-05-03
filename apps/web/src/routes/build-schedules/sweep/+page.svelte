<!--
  Sweep Schedules — Foundry-parity surface for the
  scheduling-linter (P3). Renders findings grouped by rule with bulk
  apply actions. The actual rule library lives in
  `libs/scheduling-linter`; this page just drives `:sweep` and
  `:sweep:apply`.
-->
<script lang="ts">
  import {
    type LinterFinding,
    type SweepReport,
    applySweep,
    runSweep,
  } from '$lib/api/schedules';

  let report = $state<SweepReport | null>(null);
  let loading = $state(false);
  let errorMsg = $state<string | null>(null);
  let project = $state('');
  let production = $state(false);
  let selected = $state(new Set<string>());
  let applyResult = $state<Array<Record<string, unknown>> | null>(null);

  async function run() {
    loading = true;
    errorMsg = null;
    applyResult = null;
    try {
      const params: { project?: string; production?: boolean } = {};
      if (project.trim()) params.project = project.trim();
      params.production = production;
      const res = await runSweep(params);
      report = { findings: res.findings };
      selected = new Set(res.findings.map((f) => f.id));
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : String(err);
      report = null;
    } finally {
      loading = false;
    }
  }

  async function applySelection() {
    if (!report) return;
    try {
      errorMsg = null;
      const res = await applySweep({
        finding_ids: Array.from(selected),
        report,
      });
      applyResult = res.applied;
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : String(err);
    }
  }

  function toggleFinding(id: string) {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selected = next;
  }

  let groupedFindings = $derived.by(() => {
    if (!report) return new Map<string, LinterFinding[]>();
    const m = new Map<string, LinterFinding[]>();
    for (const f of report.findings) {
      const key = f.rule_id;
      const list = m.get(key) ?? [];
      list.push(f);
      m.set(key, list);
    }
    return m;
  });
</script>

<main class="sweep-page" data-testid="sweep-page">
  <header>
    <h1>Sweep schedules</h1>
    <p class="subtitle">
      Run rule SCH-001 through SCH-007 against the schedule inventory.
      Findings are bucketed by rule with a bulk apply.
    </p>
  </header>

  <section class="controls">
    <label>
      <span>Project RID (optional)</span>
      <input
        type="text"
        bind:value={project}
        placeholder="ri.foundry.main.project.alpha"
        data-testid="sweep-project-input"
      />
    </label>
    <label class="checkbox-label">
      <input type="checkbox" bind:checked={production} data-testid="sweep-production-toggle" />
      <span>Production environment (gates SCH-006 high-frequency rule)</span>
    </label>
    <button type="button" data-testid="sweep-run-button" onclick={run} disabled={loading}>
      {loading ? 'Running…' : 'Run sweep'}
    </button>
  </section>

  {#if errorMsg}
    <p class="error" role="alert">{errorMsg}</p>
  {/if}

  {#if report}
    <section class="results" data-testid="sweep-results">
      {#if report.findings.length === 0}
        <p class="hint">No findings — every schedule looks healthy.</p>
      {:else}
        {#each Array.from(groupedFindings.entries()) as [ruleId, findings] (ruleId)}
          <article class="rule-bucket" data-testid="rule-bucket-{ruleId}">
            <header>
              <h2>{ruleId}</h2>
              <span class="count">{findings.length} finding{findings.length === 1 ? '' : 's'}</span>
            </header>
            <ul>
              {#each findings as finding (finding.id)}
                <li>
                  <label>
                    <input
                      type="checkbox"
                      checked={selected.has(finding.id)}
                      data-testid="sweep-finding-checkbox"
                      onchange={() => toggleFinding(finding.id)}
                    />
                    <span class="severity {finding.severity.toLowerCase()}">{finding.severity}</span>
                    <code class="rid">{finding.schedule_rid}</code>
                    <span class="message">{finding.message}</span>
                    <span class="action">→ {finding.recommended_action}</span>
                  </label>
                </li>
              {/each}
            </ul>
          </article>
        {/each}
        <button
          type="button"
          class="apply-button"
          data-testid="sweep-apply-button"
          onclick={applySelection}
          disabled={selected.size === 0}
        >
          Apply ({selected.size})
        </button>
      {/if}
    </section>
  {/if}

  {#if applyResult}
    <section class="apply-result" data-testid="sweep-apply-result">
      <h2>Apply result</h2>
      <pre>{JSON.stringify(applyResult, null, 2)}</pre>
    </section>
  {/if}
</main>

<style>
  .sweep-page {
    padding: 24px;
    max-width: 1024px;
    margin: 0 auto;
    color: #e2e8f0;
  }
  header h1 { margin: 0 0 4px; font-size: 18px; }
  .subtitle { color: #94a3b8; font-size: 13px; margin: 0 0 16px; }
  .controls {
    display: grid;
    grid-template-columns: 1fr 1fr auto;
    gap: 12px;
    align-items: end;
    background: #0b1220;
    padding: 12px;
    border-radius: 8px;
    margin-bottom: 16px;
  }
  .controls label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 12px;
  }
  .controls input[type='text'] {
    background: #1e293b;
    color: #f1f5f9;
    border: 1px solid #334155;
    padding: 4px 6px;
    border-radius: 4px;
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
  }
  .checkbox-label { flex-direction: row; align-items: center; gap: 6px; }
  .controls button {
    background: #2563eb;
    color: #f1f5f9;
    border: none;
    padding: 6px 16px;
    border-radius: 4px;
    font-size: 13px;
    cursor: pointer;
  }
  .results { display: flex; flex-direction: column; gap: 12px; }
  .rule-bucket {
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 6px;
    padding: 10px;
  }
  .rule-bucket header {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    margin-bottom: 6px;
  }
  .rule-bucket h2 { margin: 0; font-size: 14px; }
  .count { color: #94a3b8; font-size: 11px; }
  .rule-bucket ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .rule-bucket li {
    background: #111827;
    padding: 6px 8px;
    border-radius: 4px;
    font-size: 12px;
  }
  .rule-bucket label {
    display: grid;
    grid-template-columns: auto auto auto 1fr auto;
    gap: 8px;
    align-items: center;
    cursor: pointer;
  }
  .severity {
    padding: 1px 6px;
    border-radius: 3px;
    font-size: 10px;
    font-weight: 600;
  }
  .severity.info { background: #334155; color: #cbd5e1; }
  .severity.warning { background: #422006; color: #fcd34d; }
  .severity.error { background: #7f1d1d; color: #fecaca; }
  .rid {
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    color: #93c5fd;
    font-size: 11px;
  }
  .message { color: #cbd5e1; }
  .action { color: #94a3b8; font-style: italic; }
  .apply-button {
    align-self: flex-start;
    background: #b45309;
    color: #fff7ed;
    border: none;
    padding: 6px 16px;
    border-radius: 4px;
    cursor: pointer;
  }
  .apply-button:disabled { opacity: 0.4; cursor: not-allowed; }
  .apply-result {
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 6px;
    padding: 10px;
    margin-top: 16px;
  }
  .apply-result pre {
    margin: 0;
    color: #cbd5e1;
    font-size: 11px;
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
  }
  .hint { color: #94a3b8; font-style: italic; }
  .error { color: #fca5a5; }
</style>
