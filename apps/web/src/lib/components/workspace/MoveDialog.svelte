<script lang="ts">
  import { notifications } from '$stores/notifications';
  import { batchApply, moveResource, type BatchAction, type ResourceKind } from '$lib/api/workspace';
  import { listProjectFolders, type OntologyProject, type OntologyProjectFolder } from '$lib/api/ontology';

  type BulkTarget = { kind: ResourceKind; id: string; label: string };

  let {
    open,
    resourceKind,
    resourceId,
    resourceLabel,
    projects,
    initialProjectId,
    targets,
    onClose,
    onMoved,
  }: {
    open: boolean;
    resourceKind: ResourceKind | null;
    resourceId: string | null;
    resourceLabel?: string;
    projects: OntologyProject[];
    initialProjectId?: string | null;
    targets?: BulkTarget[];
    onClose?: () => void;
    onMoved?: () => void;
  } = $props();

  const isBulk = $derived(Array.isArray(targets) && targets.length > 0);

  let targetProjectId = $state<string>('');
  let targetFolderId = $state<string>('');
  let folders = $state<OntologyProjectFolder[]>([]);
  let loadingFolders = $state(false);
  let submitting = $state(false);

  $effect(() => {
    if (!open) return;
    targetProjectId = initialProjectId ?? projects[0]?.id ?? '';
    targetFolderId = '';
    submitting = false;
  });

  $effect(() => {
    if (!open || !targetProjectId) {
      folders = [];
      return;
    }
    void loadFolders(targetProjectId);
  });

  async function loadFolders(projectId: string) {
    loadingFolders = true;
    try {
      folders = await listProjectFolders(projectId);
    } catch (cause) {
      notifications.error(cause instanceof Error ? cause.message : 'Unable to load folders');
      folders = [];
    } finally {
      loadingFolders = false;
    }
  }

  async function submit() {
    if (!targetProjectId) {
      notifications.warning('Pick a destination project.');
      return;
    }
    submitting = true;
    try {
      if (isBulk && targets) {
        const actions: BatchAction[] = targets.map((t) => ({
          op: 'move',
          resource_kind: t.kind,
          resource_id: t.id,
          target_folder_id: targetFolderId || null,
        }));
        const { results } = await batchApply(actions);
        const failed = results.filter((r) => !r.ok);
        if (failed.length === 0) {
          notifications.success(`Moved ${results.length} item(s).`);
        } else {
          notifications.warning(
            `${results.length - failed.length} succeeded, ${failed.length} failed.`,
          );
        }
      } else {
        if (!resourceKind || !resourceId) {
          notifications.warning('Pick a destination project.');
          return;
        }
        await moveResource(resourceKind, resourceId, {
          target_project_id: targetProjectId,
          target_folder_id: targetFolderId || null,
        });
        notifications.success('Moved successfully.');
      }
      onMoved?.();
      onClose?.();
    } catch (cause) {
      notifications.error(cause instanceof Error ? cause.message : 'Unable to move resource');
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
    aria-label="Move resource"
  >
    <div class="w-full max-w-md rounded-md border border-[var(--border-default)] bg-white shadow-xl">
      <div class="flex items-center justify-between border-b border-[var(--border-default)] px-4 py-3">
        <div>
          <div class="text-sm font-semibold text-[var(--text-strong)]">
            {isBulk ? `Move ${targets?.length ?? 0} item(s)` : 'Move resource'}
          </div>
          {#if !isBulk && resourceLabel}
            <div class="text-xs text-[var(--text-muted)]">{resourceLabel}</div>
          {:else if isBulk && targets}
            <div class="text-xs text-[var(--text-muted)]">
              {targets.slice(0, 3).map((t) => t.label).join(', ')}{targets.length > 3 ? `, +${targets.length - 3} more` : ''}
            </div>
          {/if}
        </div>
        <button type="button" class="text-sm text-[var(--text-muted)] hover:text-[var(--text-strong)]" onclick={close}>
          ✕
        </button>
      </div>
      <div class="space-y-3 p-4">
        <div>
          <label class="block text-xs font-semibold uppercase tracking-wide text-[var(--text-muted)]" for="move-project">
            Target project
          </label>
          <select id="move-project" class="of-select mt-1 w-full" bind:value={targetProjectId}>
            {#each projects as project (project.id)}
              <option value={project.id}>{project.display_name || project.slug}</option>
            {/each}
          </select>
        </div>
        <div>
          <label class="block text-xs font-semibold uppercase tracking-wide text-[var(--text-muted)]" for="move-folder">
            Target folder
          </label>
          <select id="move-folder" class="of-select mt-1 w-full" bind:value={targetFolderId} disabled={loadingFolders}>
            <option value="">— Project root —</option>
            {#each folders as folder (folder.id)}
              <option value={folder.id}>{folder.name}</option>
            {/each}
          </select>
          {#if loadingFolders}
            <div class="mt-1 text-xs text-[var(--text-muted)]">Loading folders…</div>
          {/if}
        </div>
      </div>
      <div class="flex justify-end gap-2 border-t border-[var(--border-default)] px-4 py-3">
        <button type="button" class="of-btn of-btn-ghost" onclick={close} disabled={submitting}>Cancel</button>
        <button type="button" class="of-btn of-btn-primary" onclick={() => void submit()} disabled={submitting}>
          {submitting ? 'Moving…' : 'Move here'}
        </button>
      </div>
    </div>
  </div>
{/if}
