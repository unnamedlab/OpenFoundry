<!--
  Marketplace product detail (P3) — surfaces the Schedules tab over
  the product manifests stored in `marketplace_schedule_manifests`.
  Per Foundry doc § "Add schedule to a Marketplace product", every
  manifest can be activated at install time via a checkbox.
-->
<script lang="ts">
  import { page } from '$app/stores';
  import {
    type ProductScheduleManifest,
    previewInstallSchedules,
  } from '$lib/api/marketplace-schedules';

  let productId = $derived($page.params.id);

  let activeTab = $state<'overview' | 'schedules'>('overview');
  let manifests = $state<ProductScheduleManifest[]>([]);
  let activated = $state(new Set<string>());
  let materialised = $state<ProductScheduleManifest[] | null>(null);
  let errorMsg = $state<string | null>(null);
  let productVersionId = $state('');

  async function previewInstall() {
    try {
      errorMsg = null;
      const res = await previewInstallSchedules(productId, {
        product_version_id: productVersionId,
        activate_manifests: Array.from(activated),
      });
      materialised = res.materialised;
      manifests = res.materialised;
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : String(err);
    }
  }

  function toggleManifest(name: string) {
    const next = new Set(activated);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    activated = next;
  }
</script>

<main class="product-page" data-testid="marketplace-product-page">
  <header>
    <h1>Marketplace product</h1>
    <code class="rid">{productId}</code>
  </header>

  <nav class="tabs" role="tablist">
    <button
      type="button"
      role="tab"
      aria-selected={activeTab === 'overview'}
      class:active={activeTab === 'overview'}
      data-testid="overview-tab"
      onclick={() => (activeTab = 'overview')}
    >
      Overview
    </button>
    <button
      type="button"
      role="tab"
      aria-selected={activeTab === 'schedules'}
      class:active={activeTab === 'schedules'}
      data-testid="schedules-tab"
      onclick={() => (activeTab = 'schedules')}
    >
      Schedules
    </button>
  </nav>

  {#if errorMsg}
    <p class="error">{errorMsg}</p>
  {/if}

  {#if activeTab === 'schedules'}
    <section class="schedules" data-testid="product-schedules">
      <label>
        <span>Product version id</span>
        <input
          type="text"
          bind:value={productVersionId}
          placeholder="00000000-0000-0000-0000-000000000000"
          data-testid="product-version-input"
        />
      </label>
      <button
        type="button"
        data-testid="preview-install-button"
        onclick={previewInstall}
        disabled={!productVersionId}
      >
        Preview install
      </button>

      {#if manifests.length === 0}
        <p class="hint">No schedule manifests for this product yet.</p>
      {:else}
        <ul class="manifests">
          {#each manifests as m (m.name)}
            <li data-testid="manifest-row">
              <label>
                <input
                  type="checkbox"
                  checked={activated.has(m.name)}
                  data-testid="manifest-activate-checkbox"
                  onchange={() => toggleManifest(m.name)}
                />
                <strong>{m.name}</strong>
                <span class="scope">[{m.scope_kind || 'USER'}]</span>
                <span class="desc">{m.description}</span>
              </label>
            </li>
          {/each}
        </ul>
      {/if}

      {#if materialised}
        <section class="materialised" data-testid="materialised-preview">
          <h3>Resolved manifests</h3>
          <pre>{JSON.stringify(materialised, null, 2)}</pre>
        </section>
      {/if}
    </section>
  {/if}
</main>

<style>
  .product-page {
    padding: 24px;
    max-width: 1024px;
    margin: 0 auto;
    color: #e2e8f0;
  }
  header {
    display: flex;
    align-items: baseline;
    gap: 12px;
    margin-bottom: 12px;
  }
  h1 { margin: 0; font-size: 18px; }
  .rid {
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
    font-size: 12px;
    color: #94a3b8;
  }
  .tabs {
    display: flex;
    gap: 4px;
    border-bottom: 1px solid #1f2937;
    margin-bottom: 12px;
  }
  .tabs button {
    background: transparent;
    border: none;
    color: #94a3b8;
    padding: 6px 10px;
    cursor: pointer;
    border-bottom: 2px solid transparent;
  }
  .tabs button.active {
    color: #f1f5f9;
    border-bottom-color: #38bdf8;
  }
  .schedules {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .manifests { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
  .manifests li {
    background: #111827;
    padding: 6px 8px;
    border-radius: 4px;
  }
  .scope {
    color: #6ee7b7;
    font-size: 10px;
    margin-left: 6px;
  }
  .desc { color: #cbd5e1; margin-left: 8px; }
  .materialised {
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 6px;
    padding: 10px;
  }
  .materialised pre {
    margin: 0;
    color: #cbd5e1;
    font-size: 11px;
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
  }
  .error { color: #fca5a5; }
  .hint { color: #94a3b8; font-style: italic; }
</style>
