<!--
  ParameterizedPanel — Foundry-parity Parameterized Pipelines [Beta].
  Mirrors the doc § "Parameterized pipelines": same logic, different
  parameter values, one deployment per value-set, manual-only dispatch.
-->
<script lang="ts">
  import {
    type ParameterizedPipeline,
    type PipelineDeployment,
    createDeployment,
    deleteDeployment,
    enableParameterized,
    listDeployments,
    runDeployment,
  } from '$lib/api/parameterized';

  type Props = {
    pipelineRid: string;
    /** Existing parameterized config if already enabled, else null. */
    existing?: ParameterizedPipeline | null;
  };

  let { pipelineRid, existing = null }: Props = $props();

  let parameterized = $state<ParameterizedPipeline | null>(existing);
  let deployments = $state<PipelineDeployment[]>([]);
  let errorMsg = $state<string | null>(null);

  let deploymentKeyParam = $state('');
  let outputRids = $state('');
  let unionViewRid = $state('');

  let newDeploymentKey = $state('');
  let newDeploymentValues = $state('{}');

  async function refresh() {
    if (!parameterized) return;
    try {
      const res = await listDeployments(parameterized.id);
      deployments = res.data;
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : String(err);
    }
  }

  async function onEnableClicked() {
    try {
      errorMsg = null;
      const rids = outputRids
        .split(',')
        .map((s) => s.trim())
        .filter((s) => s.length > 0);
      parameterized = await enableParameterized(pipelineRid, {
        deployment_key_param: deploymentKeyParam.trim(),
        output_dataset_rids: rids,
        union_view_dataset_rid: unionViewRid.trim(),
      });
      await refresh();
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : String(err);
    }
  }

  async function onCreateDeploymentClicked() {
    if (!parameterized) return;
    try {
      errorMsg = null;
      const values = JSON.parse(newDeploymentValues || '{}');
      await createDeployment(parameterized.id, {
        deployment_key: newDeploymentKey.trim(),
        parameter_values: values,
      });
      newDeploymentKey = '';
      newDeploymentValues = '{}';
      await refresh();
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : String(err);
    }
  }

  async function onRunClicked(deployment: PipelineDeployment) {
    if (!parameterized) return;
    try {
      errorMsg = null;
      await runDeployment(parameterized.id, deployment.id, 'MANUAL');
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : String(err);
    }
  }

  async function onDeleteClicked(deployment: PipelineDeployment) {
    if (!parameterized) return;
    if (!window.confirm(`Delete deployment "${deployment.deployment_key}"?`)) return;
    try {
      errorMsg = null;
      await deleteDeployment(parameterized.id, deployment.id);
      await refresh();
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : String(err);
    }
  }

  $effect(() => {
    if (parameterized) {
      void refresh();
    }
  });
</script>

<section class="parameterized" data-testid="parameterized-panel">
  <header>
    <h3>Parameterized pipelines</h3>
    <span class="beta-badge" data-testid="beta-badge">Beta</span>
  </header>

  <p class="banner">
    Parameterized pipelines run the same logic with different parameter values.
    <strong>Automated triggers are not yet supported</strong> — every run is a manual dispatch from a deployment.
  </p>

  {#if errorMsg}
    <p class="error" role="alert">{errorMsg}</p>
  {/if}

  {#if !parameterized}
    <section class="enable-form" data-testid="enable-form">
      <h4>Enable parameterized mode</h4>
      <label>
        <span>Deployment key parameter</span>
        <input
          type="text"
          bind:value={deploymentKeyParam}
          placeholder="region"
          data-testid="enable-deployment-key-param"
        />
      </label>
      <label>
        <span>Output dataset RIDs (comma-separated)</span>
        <input
          type="text"
          bind:value={outputRids}
          placeholder="ri.foundry.main.dataset.alpha-out"
          data-testid="enable-output-rids"
        />
      </label>
      <label>
        <span>Union view dataset RID</span>
        <input
          type="text"
          bind:value={unionViewRid}
          placeholder="ri.foundry.main.dataset.alpha-view"
          data-testid="enable-union-view-rid"
        />
      </label>
      <button
        type="button"
        onclick={onEnableClicked}
        data-testid="enable-parameterized-button"
        disabled={!deploymentKeyParam || !outputRids || !unionViewRid}
      >
        Enable parameterized
      </button>
    </section>
  {:else}
    <section class="enabled" data-testid="enabled-state">
      <p class="summary">
        Deployment key: <code>{parameterized.deployment_key_param}</code>.
        Union view: <code>{parameterized.union_view_dataset_rid}</code>.
      </p>

      <h4>Deployments</h4>
      {#if deployments.length === 0}
        <p class="hint">No deployments yet — create one to dispatch a parameterized run.</p>
      {:else}
        <ul class="deployment-list" data-testid="deployment-list">
          {#each deployments as d (d.id)}
            <li data-testid="deployment-row">
              <code class="key">{d.deployment_key}</code>
              <code class="values">{JSON.stringify(d.parameter_values)}</code>
              <button
                type="button"
                onclick={() => onRunClicked(d)}
                data-testid="deployment-run-button"
              >
                Run
              </button>
              <button
                type="button"
                onclick={() => onDeleteClicked(d)}
                data-testid="deployment-delete-button"
              >
                Delete
              </button>
            </li>
          {/each}
        </ul>
      {/if}

      <section class="new-deployment">
        <h5>New deployment</h5>
        <label>
          <span>Deployment key</span>
          <input
            type="text"
            bind:value={newDeploymentKey}
            placeholder="eu-west"
            data-testid="new-deployment-key-input"
          />
        </label>
        <label>
          <span>Parameter values (JSON)</span>
          <textarea
            bind:value={newDeploymentValues}
            placeholder={'{\n  "region": "eu-west",\n  "limit": 1000\n}'}
            data-testid="new-deployment-values-input"
          ></textarea>
        </label>
        <button
          type="button"
          onclick={onCreateDeploymentClicked}
          data-testid="new-deployment-create-button"
          disabled={!newDeploymentKey}
        >
          Create deployment
        </button>
      </section>
    </section>
  {/if}
</section>

<style>
  .parameterized {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px;
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 8px;
    color: #e2e8f0;
  }
  header {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }
  h3 { margin: 0; font-size: 14px; }
  h4 { margin: 6px 0 4px; font-size: 12px; color: #cbd5e1; }
  h5 { margin: 6px 0 2px; font-size: 11px; color: #94a3b8; }
  .beta-badge {
    background: #422006;
    color: #fcd34d;
    padding: 1px 6px;
    border-radius: 3px;
    font-size: 10px;
    font-weight: 600;
  }
  .banner {
    margin: 0;
    font-size: 12px;
    color: #cbd5e1;
    background: #111827;
    padding: 8px;
    border-radius: 4px;
    border-left: 3px solid #b45309;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 12px;
    color: #cbd5e1;
  }
  input[type='text'], textarea {
    background: #1e293b;
    color: #f1f5f9;
    border: 1px solid #334155;
    border-radius: 4px;
    padding: 6px 8px;
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    font-size: 12px;
  }
  textarea { min-height: 80px; }
  button {
    background: #1e293b;
    color: #cbd5e1;
    border: 1px solid #334155;
    border-radius: 4px;
    padding: 4px 10px;
    font-size: 11px;
    cursor: pointer;
  }
  button:hover:not(:disabled) { background: #334155; }
  button:disabled { opacity: 0.4; cursor: not-allowed; }
  .deployment-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .deployment-list li {
    display: grid;
    grid-template-columns: auto 1fr auto auto;
    gap: 8px;
    align-items: center;
    background: #111827;
    padding: 6px 8px;
    border-radius: 4px;
  }
  .key { color: #93c5fd; font-weight: 600; }
  .values { color: #cbd5e1; font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .summary { font-size: 12px; color: #cbd5e1; margin: 4px 0; }
  .hint { color: #94a3b8; font-style: italic; font-size: 12px; margin: 0; }
  .error { color: #fca5a5; font-size: 12px; margin: 0; }
  .new-deployment {
    display: flex;
    flex-direction: column;
    gap: 6px;
    background: #111827;
    padding: 8px;
    border-radius: 4px;
  }
</style>
