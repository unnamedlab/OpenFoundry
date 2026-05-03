<!--
  P3 — CreateBranchDialog

  Modal that drives the P1 v2 source-aware create-branch flow:
    * source = `from_branch` → pick a parent branch (default "master")
    * source = `from_transaction` → paste a transaction RID
    * source = `as_root` → only allowed when the dataset has none yet

  Plus a fallback-chain editor (comma-separated branch names; trimmed).
-->
<script lang="ts">
  import {
    createBranchV2,
    type DatasetBranch,
  } from '$lib/api/datasets';
  import { ApiError } from '$lib/api/client';

  type Props = {
    datasetRid: string;
    open: boolean;
    branches: DatasetBranch[];
    onClose: () => void;
    onCreated?: (branch: DatasetBranch) => void;
  };

  const { datasetRid, open, branches, onClose, onCreated }: Props = $props();

  let mode = $state<'from_branch' | 'from_transaction' | 'as_root'>('from_branch');
  let name = $state('');
  let fromBranch = $state('master');
  let fromTxnRid = $state('');
  let fallbackChainRaw = $state('');
  let labelsRaw = $state('');
  let busy = $state(false);
  let error = $state('');

  $effect(() => {
    if (open) {
      mode = branches.length === 0 ? 'as_root' : 'from_branch';
      name = '';
      fromBranch = branches.find((b) => b.is_default)?.name ?? branches[0]?.name ?? 'master';
      fromTxnRid = '';
      fallbackChainRaw = '';
      labelsRaw = '';
      error = '';
    }
  });

  function parseFallback(): string[] {
    return fallbackChainRaw
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
  }

  function parseLabels(): Record<string, string> | undefined {
    const out: Record<string, string> = {};
    for (const part of labelsRaw.split(',')) {
      const trimmed = part.trim();
      if (!trimmed) continue;
      const [k, v] = trimmed.split('=').map((s) => s.trim());
      if (k && v) out[k] = v;
    }
    return Object.keys(out).length ? out : undefined;
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!name.trim()) {
      error = 'Branch name is required';
      return;
    }
    let source;
    if (mode === 'from_branch') {
      if (!fromBranch.trim()) {
        error = 'Source branch is required';
        return;
      }
      source = { from_branch: fromBranch.trim() };
    } else if (mode === 'from_transaction') {
      if (!fromTxnRid.trim()) {
        error = 'Source transaction RID is required';
        return;
      }
      source = { from_transaction_rid: fromTxnRid.trim() };
    } else {
      source = { as_root: true as const };
    }
    busy = true;
    error = '';
    try {
      const branch = await createBranchV2(datasetRid, {
        name: name.trim(),
        source,
        fallback_chain: parseFallback(),
        labels: parseLabels(),
      });
      onCreated?.(branch);
      onClose();
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Failed to create branch';
    } finally {
      busy = false;
    }
  }
</script>

{#if open}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
    role="dialog"
    aria-modal="true"
    data-testid="create-branch-dialog"
  >
    <form
      class="w-full max-w-lg space-y-3 rounded-2xl border border-slate-200 bg-white p-5 shadow-xl dark:border-gray-700 dark:bg-gray-900"
      onsubmit={submit}
    >
      <h2 class="text-lg font-semibold">Create branch</h2>

      <label class="block text-sm">
        <span class="text-gray-500">Branch name</span>
        <input
          type="text"
          bind:value={name}
          placeholder="feature-x"
          class="mt-1 block w-full rounded border border-slate-300 px-2 py-1 dark:border-gray-700 dark:bg-gray-800"
          data-testid="create-branch-name"
          required
        />
      </label>

      <fieldset class="rounded border border-slate-200 px-3 py-2 text-sm dark:border-gray-700">
        <legend class="px-1 text-xs uppercase tracking-wide text-gray-500">Source</legend>
        <label class="flex items-start gap-2">
          <input type="radio" bind:group={mode} value="from_branch" data-testid="create-branch-source-from-branch" />
          <span>
            <span class="font-medium">From another branch</span>
            <select
              bind:value={fromBranch}
              class="mt-1 block w-full rounded border border-slate-300 px-2 py-1 dark:border-gray-700 dark:bg-gray-800"
              disabled={mode !== 'from_branch'}
              data-testid="create-branch-from-branch-select"
            >
              {#each branches as b (b.id)}
                <option value={b.name}>{b.name}{b.is_default ? ' (default)' : ''}</option>
              {/each}
            </select>
          </span>
        </label>
        <label class="mt-2 flex items-start gap-2">
          <input type="radio" bind:group={mode} value="from_transaction" data-testid="create-branch-source-from-transaction" />
          <span>
            <span class="font-medium">From a specific transaction</span>
            <input
              type="text"
              bind:value={fromTxnRid}
              placeholder="ri.foundry.main.transaction.…"
              class="mt-1 block w-full rounded border border-slate-300 px-2 py-1 font-mono text-xs dark:border-gray-700 dark:bg-gray-800"
              disabled={mode !== 'from_transaction'}
              data-testid="create-branch-from-transaction-input"
            />
          </span>
        </label>
        <label class="mt-2 flex items-start gap-2">
          <input
            type="radio"
            bind:group={mode}
            value="as_root"
            disabled={branches.length > 0}
            data-testid="create-branch-source-as-root"
          />
          <span>
            <span class="font-medium">As root branch</span>
            <span class="block text-xs text-gray-500">
              Only valid when the dataset has no other branches yet.
            </span>
          </span>
        </label>
      </fieldset>

      <label class="block text-sm">
        <span class="text-gray-500">Fallback chain (comma-separated)</span>
        <input
          type="text"
          bind:value={fallbackChainRaw}
          placeholder="develop, master"
          class="mt-1 block w-full rounded border border-slate-300 px-2 py-1 dark:border-gray-700 dark:bg-gray-800"
          data-testid="create-branch-fallback-chain"
        />
      </label>

      <label class="block text-sm">
        <span class="text-gray-500">Labels (key=value, comma-separated)</span>
        <input
          type="text"
          bind:value={labelsRaw}
          placeholder="persona=data-eng, ticket=PR-123"
          class="mt-1 block w-full rounded border border-slate-300 px-2 py-1 dark:border-gray-700 dark:bg-gray-800"
          data-testid="create-branch-labels"
        />
      </label>

      {#if error}
        <p class="text-xs text-rose-600 dark:text-rose-300">{error}</p>
      {/if}

      <div class="flex justify-end gap-2 pt-2">
        <button
          type="button"
          class="rounded border border-slate-300 px-3 py-1 text-sm hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
          onclick={onClose}
          data-testid="create-branch-cancel"
        >Cancel</button>
        <button
          type="submit"
          class="rounded bg-blue-600 px-3 py-1 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
          disabled={busy}
          data-testid="create-branch-submit"
        >Create</button>
      </div>
    </form>
  </div>
{/if}
