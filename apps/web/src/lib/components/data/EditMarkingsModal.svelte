<!--
  EditMarkingsModal — replace the marking set on a media set with a
  Cedar dry-run preview before commit.
  Mirrors Foundry's "Edit markings" panel (Permissions tab on a
  media-set / dataset detail). The flow:
  1. Operator toggles the available markings.
  2. "Preview" calls `POST /media-sets/{rid}/markings/preview`. The
     response carries the diff (added / removed) plus the count of
     users that would lose access — surfaced as the
     "X users will lose access" warning the spec calls for.
  3. "Save" calls `PATCH /media-sets/{rid}/markings`.
-->
<script lang="ts">
  import {
    patchSetMarkings,
    previewSetMarkings,
    type MediaSet,
    type MarkingsPreviewResponse
  } from '$lib/api/mediaSets';
  import { notifications as toasts } from '$stores/notifications';

  type Props = {
    mediaSet: MediaSet;
    onClose: () => void;
    onSaved: (updated: MediaSet) => void;
  };

  let { mediaSet, onClose, onSaved }: Props = $props();

  /**
   * Hard-coded marking ladder used in dev / tests until the markings
   * catalog endpoint lands. Foundry exposes a ladder
   * `public ⊂ confidential ⊂ pii ⊂ restricted` plus arbitrary
   * tenant-defined markings; both forms work as Cedar resource
   * `Marking::"<lower-name>"` UIDs.
   */
  const KNOWN_MARKINGS = ['public', 'confidential', 'pii', 'secret'];

  let selected = $state<string[]>(
    mediaSet.markings.map((m) => m.toLowerCase())
  );
  let preview = $state<MarkingsPreviewResponse | null>(null);
  let previewing = $state(false);
  let previewError = $state('');
  let saving = $state(false);
  let saveError = $state('');

  function toggle(marking: string, checked: boolean) {
    selected = checked
      ? Array.from(new Set([...selected, marking]))
      : selected.filter((m) => m !== marking);
    // Drop any prior preview — the diff only describes the snapshot we
    // sent, not the live `selected` array.
    preview = null;
  }

  async function runPreview() {
    previewing = true;
    previewError = '';
    try {
      preview = await previewSetMarkings(mediaSet.rid, selected);
    } catch (cause) {
      previewError = cause instanceof Error ? cause.message : 'Preview failed';
    } finally {
      previewing = false;
    }
  }

  async function save() {
    saving = true;
    saveError = '';
    try {
      const updated = await patchSetMarkings(mediaSet.rid, selected);
      toasts.success(`Markings updated for ${updated.name}`);
      onSaved(updated);
    } catch (cause) {
      saveError =
        cause instanceof Error ? cause.message : 'Failed to save markings';
    } finally {
      saving = false;
    }
  }
</script>

<div
  class="fixed inset-0 z-[110] flex items-start justify-center overflow-y-auto bg-black/60 p-6"
  role="presentation"
  data-testid="edit-markings-modal"
>
  <div
    role="dialog"
    aria-modal="true"
    aria-label="Edit markings"
    class="w-full max-w-lg space-y-4 rounded-2xl border border-slate-200 bg-white p-5 text-sm shadow-xl dark:border-gray-700 dark:bg-gray-900"
  >
    <header class="flex items-start justify-between gap-3">
      <div>
        <h2 class="text-base font-semibold">Edit markings</h2>
        <p class="mt-1 text-xs text-slate-500">
          Cedar enforces clearance:
          <code class="font-mono">user.clearances ⊇ resource.markings</code>.
          Removing a marking may grant access; adding one may take it away.
        </p>
      </div>
      <button
        type="button"
        class="rounded p-1 text-slate-500 hover:bg-slate-100 dark:hover:bg-gray-800"
        aria-label="Close"
        onclick={onClose}
      >
        ×
      </button>
    </header>

    <fieldset class="space-y-1">
      <legend class="text-xs uppercase tracking-[0.18em] text-slate-400">
        Markings
      </legend>
      {#each KNOWN_MARKINGS as marking (marking)}
        <label class="flex items-center gap-2">
          <input
            type="checkbox"
            checked={selected.includes(marking)}
            data-testid={`edit-markings-${marking}`}
            onchange={(event) =>
              toggle(marking, (event.currentTarget as HTMLInputElement).checked)}
          />
          <span class="font-mono text-xs uppercase">{marking}</span>
        </label>
      {/each}
    </fieldset>

    <div class="flex flex-wrap items-center gap-2">
      <button
        type="button"
        class="rounded-xl border border-slate-300 px-3 py-1.5 text-xs hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
        onclick={runPreview}
        disabled={previewing}
        data-testid="edit-markings-preview"
      >
        {previewing ? 'Previewing…' : 'Preview impact'}
      </button>
      <button
        type="button"
        class="rounded-xl bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-700 disabled:opacity-60"
        onclick={save}
        disabled={saving}
        data-testid="edit-markings-save"
      >
        {saving ? 'Saving…' : 'Save markings'}
      </button>
    </div>

    {#if previewError}
      <p class="text-xs text-rose-600">{previewError}</p>
    {/if}
    {#if saveError}
      <p class="text-xs text-rose-600">{saveError}</p>
    {/if}

    {#if preview}
      <div
        class="rounded-xl border border-slate-200 bg-slate-50 p-3 text-xs dark:border-gray-700 dark:bg-gray-800/40"
        data-testid="edit-markings-preview-result"
      >
        <p class="font-medium">Dry-run preview</p>
        <ul class="mt-2 space-y-1">
          <li>
            Added: <span class="font-mono">{preview.added.join(', ') || '—'}</span>
          </li>
          <li>
            Removed: <span class="font-mono">{preview.removed.join(', ') || '—'}</span>
          </li>
          <li
            class={preview.users_losing_access > 0
              ? 'font-semibold text-rose-600'
              : 'text-slate-500'}
          >
            {preview.users_losing_access} user{preview.users_losing_access === 1
              ? ''
              : 's'} will lose access
          </li>
        </ul>
      </div>
    {/if}
  </div>
</div>
