<!--
  MediaPermissionsPanel — Foundry "Permissions" tab body for a media
  set, plus the entry point for granular per-item marking edits.

  Two stacked sections:
    1. **Markings** (this is where Cedar enforcement lives). Renders
       inherited vs. explicit markings as `MarkingBadge`s and exposes
       the "Edit markings" modal that walks through the dry-run.
    2. **Sharing** placeholder using the existing `ShareDialog`
       component. Foundry treats per-resource shares + Cedar markings
       as orthogonal access controls; the sharing API for media sets
       is wired in a later H phase, so this panel surfaces the
       component but flags it as preview.
-->
<script lang="ts">
  import EditMarkingsModal from '$lib/components/data/EditMarkingsModal.svelte';
  import MarkingBadge from '$lib/components/dataset/MarkingBadge.svelte';
  import ShareDialog from '$lib/components/workspace/ShareDialog.svelte';
  import type { MediaSet } from '$lib/api/mediaSets';

  type Props = {
    mediaSet: MediaSet;
    onChanged: (next: MediaSet) => void;
  };

  let { mediaSet, onChanged }: Props = $props();

  let showEditMarkings = $state(false);
  let showShareDialog = $state(false);

  function levelOf(name: string): 'public' | 'confidential' | 'pii' | 'restricted' | 'unknown' {
    const lower = name.toLowerCase();
    if (lower === 'public') return 'public';
    if (lower === 'confidential') return 'confidential';
    if (lower === 'pii') return 'pii';
    if (lower === 'secret' || lower === 'restricted') return 'restricted';
    return 'unknown';
  }
</script>

<section class="space-y-6" data-testid="media-permissions-panel">
  <!-- ── Markings ──────────────────────────────────────────────── -->
  <div
    class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900"
  >
    <header class="flex items-start justify-between gap-3">
      <div>
        <h2 class="text-base font-semibold">Markings</h2>
        <p class="mt-1 text-xs text-slate-500">
          Cedar enforces clearance against this set's markings. An item with no
          per-item override inherits the full set; granular per-item markings
          tighten access further (Foundry "Configure granular policies for
          media items").
        </p>
      </div>
      <button
        type="button"
        class="rounded-xl bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-700"
        data-testid="open-edit-markings"
        onclick={() => (showEditMarkings = true)}
      >
        Edit markings
      </button>
    </header>

    <div class="mt-4 space-y-3 text-xs">
      <div>
        <div class="text-[11px] uppercase tracking-[0.18em] text-slate-400">
          Direct (this set)
        </div>
        <div class="mt-1 flex flex-wrap gap-2" data-testid="direct-markings">
          {#if mediaSet.markings.length === 0}
            <span class="text-slate-400 italic">No markings — anyone with project access</span>
          {:else}
            {#each mediaSet.markings as marking (marking)}
              <MarkingBadge
                label={marking.toUpperCase()}
                level={levelOf(marking)}
                source={{ kind: 'direct' }}
                id={marking}
              />
            {/each}
          {/if}
        </div>
      </div>

      <div>
        <div class="text-[11px] uppercase tracking-[0.18em] text-slate-400">
          Inherited (project + tenant)
        </div>
        <div class="mt-1 flex flex-wrap gap-2 text-slate-400 italic">
          Inheritance from the parent project lands when the project ontology is
          wired (H4). For now, only direct markings on this set are enforced.
        </div>
      </div>
    </div>
  </div>

  <!-- ── Sharing (placeholder) ────────────────────────────────── -->
  <div
    class="rounded-2xl border border-dashed border-slate-300 bg-slate-50 p-4 text-xs dark:border-gray-700 dark:bg-gray-800/40"
  >
    <header class="flex items-center justify-between gap-3">
      <div>
        <h2 class="text-sm font-semibold">User & group sharing</h2>
        <p class="mt-1 text-slate-500">
          Per-principal share lists are a separate axis from Cedar markings
          (markings gate clearance, shares grant tenant access). The
          <code class="font-mono">media_set</code> share kind goes live
          alongside the rest of the workspace sharing surface.
        </p>
      </div>
      <button
        type="button"
        class="rounded-xl border border-slate-300 px-3 py-1.5 text-xs hover:bg-slate-100 dark:border-gray-700 dark:hover:bg-gray-800"
        data-testid="open-share-dialog"
        onclick={() => (showShareDialog = true)}
      >
        Open share dialog
      </button>
    </header>
  </div>
</section>

{#if showEditMarkings}
  <EditMarkingsModal
    {mediaSet}
    onClose={() => (showEditMarkings = false)}
    onSaved={(updated) => {
      showEditMarkings = false;
      onChanged(updated);
    }}
  />
{/if}

<!--
  Workspace shares enumerate `dataset|pipeline|app|…|other` today —
  there is no `media_set` kind yet. Keep the dialog mounted under
  `other` until the dedicated kind ships with the workspace sharing
  surface (H4); this lets operators preview the eventual UX.
-->
<ShareDialog
  open={showShareDialog}
  resourceKind={'other'}
  resourceId={mediaSet.rid}
  resourceLabel={mediaSet.name}
  onClose={() => (showShareDialog = false)}
/>
