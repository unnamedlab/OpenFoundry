<!--
  T5.1 — CustomMetadataPanel

  Foundry lets users attach arbitrary `key/value` metadata to a
  dataset. We render the existing entries as an editable list and let
  users add or remove rows. The save handler is owned by the parent.
-->
<script lang="ts">
  type Props = {
    initial: Record<string, string>;
    saving?: boolean;
    error?: string;
    onSave: (next: Record<string, string>) => void | Promise<void>;
  };

  const { initial, saving = false, error = '', onSave }: Props = $props();

  // Keep the editor's working copy as an array of pairs to preserve
  // user-entered ordering (a Record<string,string> doesn't).
  type Pair = { key: string; value: string };
  let pairs = $state<Pair[]>(Object.entries(initial).map(([k, v]) => ({ key: k, value: v })));

  $effect(() => {
    pairs = Object.entries(initial).map(([k, v]) => ({ key: k, value: v }));
  });

  function addPair() {
    pairs = [...pairs, { key: '', value: '' }];
  }

  function removePair(index: number) {
    pairs = pairs.filter((_, i) => i !== index);
  }

  async function submit() {
    const next: Record<string, string> = {};
    for (const p of pairs) {
      const k = p.key.trim();
      if (!k) continue; // ignore empty keys
      next[k] = p.value;
    }
    await onSave(next);
  }
</script>

<section class="space-y-4">
  <header class="flex items-start justify-between gap-3">
    <div>
      <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Custom metadata</div>
      <h2 class="mt-1 text-lg font-semibold">Key / value attributes</h2>
      <p class="mt-1 text-sm text-gray-500">
        Free-form metadata indexed by the catalog and surfaced in search.
      </p>
    </div>
    <button
      type="button"
      onclick={submit}
      disabled={saving}
      class="rounded-xl bg-slate-900 px-4 py-2 text-sm text-white disabled:opacity-50 dark:bg-white dark:text-slate-900"
    >
      {saving ? 'Saving…' : 'Save'}
    </button>
  </header>

  <div class="space-y-2">
    {#each pairs as pair, idx (idx)}
      <div class="grid grid-cols-[1fr,1fr,auto] gap-2">
        <input
          aria-label="Key"
          bind:value={pair.key}
          placeholder="key"
          class="rounded-lg border border-slate-200 px-3 py-2 font-mono text-xs dark:border-gray-700 dark:bg-gray-800"
        />
        <input
          aria-label="Value"
          bind:value={pair.value}
          placeholder="value"
          class="rounded-lg border border-slate-200 px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-800"
        />
        <button
          type="button"
          onclick={() => removePair(idx)}
          class="rounded-lg border border-slate-200 px-3 py-2 text-sm text-rose-600 hover:bg-rose-50 dark:border-gray-700 dark:hover:bg-gray-800"
          aria-label="Remove pair"
        >
          ✕
        </button>
      </div>
    {/each}

    <button
      type="button"
      onclick={addPair}
      class="rounded-lg border border-dashed border-slate-300 px-3 py-2 text-sm text-gray-600 hover:bg-slate-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
    >
      + Add metadata entry
    </button>
  </div>

  {#if error}
    <div class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">
      {error}
    </div>
  {/if}
</section>
