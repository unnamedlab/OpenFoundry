<script lang="ts">
  let {
    open,
    title,
    message,
    confirmLabel = 'Confirm',
    cancelLabel = 'Cancel',
    danger = false,
    busy = false,
    onConfirm,
    onCancel,
  }: {
    open: boolean;
    title: string;
    message: string;
    confirmLabel?: string;
    cancelLabel?: string;
    danger?: boolean;
    busy?: boolean;
    onConfirm: () => void;
    onCancel: () => void;
  } = $props();

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && !busy) {
      event.preventDefault();
      onCancel();
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
    role="dialog"
    aria-modal="true"
    aria-labelledby="confirm-dialog-title"
  >
    <div class="w-full max-w-md rounded-md border border-[var(--border-default)] bg-white shadow-xl">
      <div class="border-b border-[var(--border-default)] px-4 py-3">
        <div id="confirm-dialog-title" class="text-sm font-semibold text-[var(--text-strong)]">{title}</div>
      </div>
      <div class="px-4 py-4">
        <p class="m-0 text-sm text-[var(--text-default)]">{message}</p>
      </div>
      <div class="flex justify-end gap-2 border-t border-[var(--border-default)] px-4 py-3">
        <button type="button" class="of-btn of-btn-ghost" onclick={onCancel} disabled={busy}>
          {cancelLabel}
        </button>
        <button
          type="button"
          class={`of-btn ${danger ? 'bg-[#b42318] text-white hover:bg-[#922018]' : 'of-btn-primary'}`}
          onclick={onConfirm}
          disabled={busy}
        >
          {busy ? 'Working…' : confirmLabel}
        </button>
      </div>
    </div>
  </div>
{/if}
