<script lang="ts">
  import Glyph from '$components/ui/Glyph.svelte';
  import ShareDialog from '$components/workspace/ShareDialog.svelte';
  import { notifications } from '$stores/notifications';
  import {
    createFavorite,
    deleteFavorite,
    listResourceShares,
    type ResourceKind,
    type ResourceShare,
  } from '$lib/api/workspace';

  export interface ResourceSummary {
    id: string;
    name: string;
    kind: ResourceKind;
    description?: string | null;
    owner_id?: string | null;
    location?: string | null;
    tags?: string[];
    created_at?: string | null;
    updated_at?: string | null;
  }

  let {
    open,
    resource,
    isFavorite = false,
    onClose,
    onFavoriteToggle,
  }: {
    open: boolean;
    resource: ResourceSummary | null;
    isFavorite?: boolean;
    onClose?: () => void;
    onFavoriteToggle?: (next: boolean) => void;
  } = $props();

  let shares = $state<ResourceShare[]>([]);
  let loadingShares = $state(false);
  let sharesError = $state('');
  let togglingFavorite = $state(false);
  let shareDialogOpen = $state(false);

  // Reload shares whenever the panel opens for a new resource. Failures
  // are non-fatal — the panel still renders metadata.
  $effect(() => {
    if (!open || !resource) {
      shares = [];
      sharesError = '';
      return;
    }
    void loadShares(resource);
  });

  async function loadShares(target: ResourceSummary) {
    loadingShares = true;
    sharesError = '';
    try {
      shares = await listResourceShares(target.kind, target.id);
    } catch (cause) {
      sharesError = cause instanceof Error ? cause.message : 'Unable to load shares';
      shares = [];
    } finally {
      loadingShares = false;
    }
  }

  async function toggleFavorite() {
    if (!resource || togglingFavorite) return;
    togglingFavorite = true;
    try {
      if (isFavorite) {
        await deleteFavorite(resource.kind, resource.id);
        onFavoriteToggle?.(false);
        notifications.success('Removed from favorites');
      } else {
        await createFavorite({ resource_kind: resource.kind, resource_id: resource.id });
        onFavoriteToggle?.(true);
        notifications.success('Added to favorites');
      }
    } catch (cause) {
      notifications.error(cause instanceof Error ? cause.message : 'Unable to update favorites');
    } finally {
      togglingFavorite = false;
    }
  }

  function formatDate(value: string | null | undefined) {
    if (!value) return '—';
    return new Intl.DateTimeFormat('en', {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    }).format(new Date(value));
  }

  function shareLabel(share: ResourceShare) {
    if (share.shared_with_user_id) return `User · ${share.shared_with_user_id.slice(0, 8)}…`;
    if (share.shared_with_group_id) return `Group · ${share.shared_with_group_id.slice(0, 8)}…`;
    return 'Unknown principal';
  }
</script>

{#if open && resource}
  <aside
    class="fixed inset-y-0 right-0 z-40 flex w-[380px] max-w-full flex-col border-l border-[var(--border-default)] bg-white shadow-[0_0_24px_rgba(15,23,42,0.12)]"
    aria-label="Resource details"
  >
    <header class="flex items-start justify-between gap-3 border-b border-[var(--border-default)] px-4 py-4">
      <div class="min-w-0">
        <div class="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--text-muted)]">
          {resource.kind.replace(/_/g, ' ')}
        </div>
        <div class="mt-1 truncate text-base font-semibold text-[var(--text-strong)]">
          {resource.name}
        </div>
      </div>
      <div class="flex items-center gap-2">
        <button
          type="button"
          class={`rounded-md p-2 transition ${
            isFavorite
              ? 'bg-[#fff7e0] text-[#a86a00]'
              : 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)]'
          }`}
          aria-pressed={isFavorite}
          aria-label={isFavorite ? 'Remove from favorites' : 'Add to favorites'}
          disabled={togglingFavorite}
          onclick={toggleFavorite}
        >
          <Glyph name="bookmark" size={16} />
        </button>
        <button
          type="button"
          class="rounded-md p-2 text-[var(--text-muted)] hover:bg-[var(--bg-hover)]"
          aria-label="Close details panel"
          onclick={() => onClose?.()}
        >
          <Glyph name="x" size={16} />
        </button>
      </div>
    </header>

    <div class="flex-1 overflow-y-auto px-4 py-4">
      {#if resource.description}
        <p class="m-0 text-sm text-[var(--text-default)]">{resource.description}</p>
      {/if}

      <dl class="mt-4 space-y-3 text-sm">
        {#if resource.location}
          <div class="flex items-start justify-between gap-3">
            <dt class="text-[var(--text-muted)]">Location</dt>
            <dd class="m-0 text-right text-[var(--text-strong)]">{resource.location}</dd>
          </div>
        {/if}
        {#if resource.owner_id}
          <div class="flex items-start justify-between gap-3">
            <dt class="text-[var(--text-muted)]">Owner</dt>
            <dd class="m-0 text-right text-[var(--text-strong)]">{resource.owner_id.slice(0, 8)}…</dd>
          </div>
        {/if}
        <div class="flex items-start justify-between gap-3">
          <dt class="text-[var(--text-muted)]">Created</dt>
          <dd class="m-0 text-right text-[var(--text-strong)]">{formatDate(resource.created_at)}</dd>
        </div>
        <div class="flex items-start justify-between gap-3">
          <dt class="text-[var(--text-muted)]">Last modified</dt>
          <dd class="m-0 text-right text-[var(--text-strong)]">{formatDate(resource.updated_at)}</dd>
        </div>
      </dl>

      {#if resource.tags && resource.tags.length > 0}
        <section class="mt-4">
          <div class="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--text-muted)]">Tags</div>
          <div class="mt-2 flex flex-wrap gap-2">
            {#each resource.tags as tag}
              <span class="of-chip">{tag}</span>
            {/each}
          </div>
        </section>
      {/if}

      <section class="mt-6">
        <div class="flex items-center justify-between">
          <div class="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--text-muted)]">Shared with</div>
          <div class="flex items-center gap-2">
            <span class="text-xs text-[var(--text-soft)]">{shares.length}</span>
            <button
              type="button"
              class="text-xs font-medium text-[var(--text-link)] hover:underline"
              onclick={() => (shareDialogOpen = true)}
            >
              Manage
            </button>
          </div>
        </div>

        {#if loadingShares}
          <div class="mt-3 text-sm text-[var(--text-muted)]">Loading shares…</div>
        {:else if sharesError}
          <div class="mt-3 text-sm text-[#b42318]">{sharesError}</div>
        {:else if shares.length === 0}
          <div class="mt-3 rounded-md border border-dashed border-[var(--border-default)] px-3 py-4 text-sm text-[var(--text-muted)]">
            Nobody else has access yet.
          </div>
        {:else}
          <ul class="m-0 mt-3 list-none space-y-2 p-0">
            {#each shares as share (share.id)}
              <li class="flex items-center justify-between rounded-md border border-[var(--border-default)] bg-[#fbfcfe] px-3 py-2 text-sm">
                <div class="min-w-0">
                  <div class="truncate font-medium text-[var(--text-strong)]">{shareLabel(share)}</div>
                  {#if share.note}
                    <div class="truncate text-xs text-[var(--text-muted)]">{share.note}</div>
                  {/if}
                </div>
                <span class="of-chip">{share.access_level}</span>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    </div>
  </aside>

  <ShareDialog
    open={shareDialogOpen}
    resourceKind={resource.kind}
    resourceId={resource.id}
    resourceLabel={resource.name}
    onClose={() => (shareDialogOpen = false)}
    onChange={() => {
      if (resource) void loadShares(resource);
    }}
  />
{/if}
