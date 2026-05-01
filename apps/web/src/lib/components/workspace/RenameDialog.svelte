<script lang="ts">
  import { notifications } from '$stores/notifications';
  import { renameResource, type ResourceKind } from '$lib/api/workspace';

  let {
    open,
    resourceKind,
    resourceId,
    currentName,
    onClose,
    onRenamed,
  }: {
    open: boolean;
    resourceKind: ResourceKind | null;
    resourceId: string | null;
    currentName: string;
    onClose?: () => void;
    onRenamed?: (newName: string) => void;
  } = $props();

  let value = $state('');
  let submitting = $state(false);

  $effect(() => {
    if (open) {
      value = currentName;
      submitting = false;
    }
  });

  async function submit() {
    if (!resourceKind || !resourceId) return;
    const trimmed = value.trim();
    if (!trimmed) {
      notifications.warning('Name cannot be empty.');
      return;
    }
    if (trimmed === currentName) {
      onClose?.();
      return;
    }
    submitting = true;
    try {
      await renameResource(resourceKind, resourceId, { name: trimmed });
      notifications.success('Renamed successfully.');
      onRenamed?.(trimmed);
      onClose?.();
    } catch (cause) {
      notifications.error(cause instanceof Error ? cause.message : 'Unable to rename resource');
    } finally {
      submitting = false;
    }
  }

  function close() {
    if (submitting) return;
    onClose?.();
  }
</script>

{#if open}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
    role="dialog"
    aria-modal="true"
    aria-label="Rename resource"
  >
    <div class="w-full max-w-md rounded-md border border-[var(--border-default)] bg-white shadow-xl">
      <div class="flex items-center justify-between border-b border-[var(--border-default)] px-4 py-3">
        <div class="text-sm font-semibold text-[var(--text-strong)]">Rename</div>
        <button type="button" class="text-sm text-[var(--text-muted)] hover:text-[var(--text-strong)]" onclick={close}>
          ✕
        </button>
      </div>
      <div class="space-y-3 p-4">
        <label class="block text-xs font-semibold uppercase tracking-wide text-[var(--text-muted)]" for="rename-input">
          New name
        </label>
        <input
          id="rename-input"
          class="of-input w-full"
          bind:value
          onkeydown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault();
              void submit();
            }
          }}
        />
      </div>
      <div class="flex justify-end gap-2 border-t border-[var(--border-default)] px-4 py-3">
        <button type="button" class="of-btn of-btn-ghost" onclick={close} disabled={submitting}>Cancel</button>
        <button type="button" class="of-btn of-btn-primary" onclick={() => void submit()} disabled={submitting}>
          {submitting ? 'Renaming…' : 'Rename'}
        </button>
      </div>
    </div>
  </div>
{/if}
