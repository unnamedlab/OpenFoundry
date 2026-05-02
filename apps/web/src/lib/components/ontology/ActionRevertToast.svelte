<!--
  Post-execution undo toast. Pops up after a successful action run that
  produced a revertible execution_id, exposes a `Undo` button and counts
  down the configurable window (default 30 s). On click it calls the
  revert endpoint and reports back through `onReverted` / `onError` so the
  parent surface can refresh its data.
-->
<script lang="ts">
  import { onDestroy } from 'svelte';
  import Toast from '$lib/components/ui/Toast.svelte';
  import { revertActionExecution, type RevertActionExecutionResponse } from '$lib/api/ontology';

  interface Props {
    executionId: string | null | undefined;
    actionLabel?: string;
    windowMs?: number;
    onReverted?: (response: RevertActionExecutionResponse) => void;
    onError?: (message: string) => void;
    onDismiss?: () => void;
  }

  let {
    executionId,
    actionLabel = 'action',
    windowMs = 30_000,
    onReverted,
    onError,
    onDismiss
  }: Props = $props();

  let remainingMs = $state(windowMs);
  let busy = $state(false);
  let dismissed = $state(false);
  let timer: ReturnType<typeof setInterval> | null = null;

  $effect(() => {
    // Re-arm whenever a new execution_id arrives.
    if (!executionId) {
      stopTimer();
      dismissed = false;
      return;
    }
    dismissed = false;
    busy = false;
    remainingMs = windowMs;
    stopTimer();
    timer = setInterval(() => {
      remainingMs -= 1_000;
      if (remainingMs <= 0) {
        dismiss();
      }
    }, 1_000);
  });

  function stopTimer() {
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
  }

  function dismiss() {
    stopTimer();
    dismissed = true;
    onDismiss?.();
  }

  async function handleUndo() {
    if (!executionId || busy) return;
    busy = true;
    try {
      const response = await revertActionExecution(executionId);
      stopTimer();
      dismissed = true;
      onReverted?.(response);
    } catch (error) {
      onError?.(error instanceof Error ? error.message : String(error));
    } finally {
      busy = false;
    }
  }

  onDestroy(stopTimer);

  const open = $derived(Boolean(executionId) && !dismissed);
  const seconds = $derived(Math.max(0, Math.ceil(remainingMs / 1_000)));
</script>

<Toast {open} tone="info">
  <span class="text-sm">
    {actionLabel} applied. Undo available for <strong>{seconds}s</strong>.
  </span>
  <button
    type="button"
    class="rounded-full border border-slate-300 bg-white px-3 py-1 text-xs font-semibold text-slate-700 hover:bg-slate-50 disabled:opacity-50"
    onclick={handleUndo}
    disabled={busy || !executionId}
  >
    {busy ? 'Reverting…' : 'Undo'}
  </button>
  <button
    type="button"
    class="rounded-full px-2 py-1 text-xs text-slate-500 hover:text-slate-700"
    onclick={dismiss}
    aria-label="Dismiss"
  >
    ✕
  </button>
</Toast>
