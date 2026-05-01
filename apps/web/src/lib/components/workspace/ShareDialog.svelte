<script lang="ts">
  import Glyph from '$components/ui/Glyph.svelte';
  import PrincipalPicker from '$components/workspace/PrincipalPicker.svelte';
  import { notifications } from '$stores/notifications';
  import {
    createShare,
    listResourceShares,
    revokeShare,
    type AccessLevel,
    type ResourceKind,
    type ResourceShare,
  } from '$lib/api/workspace';

  let {
    open,
    resourceKind,
    resourceId,
    resourceLabel,
    onClose,
    onChange,
  }: {
    open: boolean;
    resourceKind: ResourceKind | null;
    resourceId: string | null;
    resourceLabel?: string;
    onClose?: () => void;
    onChange?: () => void;
  } = $props();

  type Principal = 'user' | 'group';

  let shares = $state<ResourceShare[]>([]);
  let loading = $state(false);
  let loadError = $state('');
  let submitting = $state(false);

  let principal = $state<Principal>('user');
  let principalId = $state('');
  let principalLabelState = $state('');
  let accessLevel = $state<AccessLevel>('viewer');
  let note = $state('');
  let expiresAt = $state('');

  // Refresh + reset the form whenever the dialog opens for a new target.
  $effect(() => {
    if (!open || !resourceKind || !resourceId) {
      shares = [];
      loadError = '';
      return;
    }
    principalId = '';
    principalLabelState = '';
    note = '';
    expiresAt = '';
    accessLevel = 'viewer';
    void refresh();
  });

  async function refresh() {
    if (!resourceKind || !resourceId) return;
    loading = true;
    loadError = '';
    try {
      shares = await listResourceShares(resourceKind, resourceId);
    } catch (cause) {
      loadError = cause instanceof Error ? cause.message : 'Unable to load shares';
      shares = [];
    } finally {
      loading = false;
    }
  }

  async function submit() {
    if (!resourceKind || !resourceId) return;
    const id = principalId.trim();
    if (!id) {
      notifications.warning('Provide a user or group id.');
      return;
    }

    submitting = true;
    try {
      await createShare(resourceKind, resourceId, {
        shared_with_user_id: principal === 'user' ? id : undefined,
        shared_with_group_id: principal === 'group' ? id : undefined,
        access_level: accessLevel,
        note: note.trim() || undefined,
        expires_at: expiresAt ? new Date(expiresAt).toISOString() : null,
      });
      notifications.success('Share created');
      principalId = '';
      principalLabelState = '';
      note = '';
      expiresAt = '';
      onChange?.();
      await refresh();
    } catch (cause) {
      notifications.error(cause instanceof Error ? cause.message : 'Unable to create share');
    } finally {
      submitting = false;
    }
  }

  async function revoke(share: ResourceShare) {
    try {
      await revokeShare(share.id);
      notifications.success('Share revoked');
      onChange?.();
      await refresh();
    } catch (cause) {
      notifications.error(cause instanceof Error ? cause.message : 'Unable to revoke share');
    }
  }

  function principalLabel(share: ResourceShare) {
    if (share.shared_with_user_id) return `User · ${share.shared_with_user_id}`;
    if (share.shared_with_group_id) return `Group · ${share.shared_with_group_id}`;
    return 'Unknown principal';
  }

  function formatDate(value: string | null) {
    if (!value) return '—';
    return new Intl.DateTimeFormat('en', {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
    }).format(new Date(value));
  }
</script>

{#if open && resourceKind && resourceId}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-[rgba(15,23,42,0.45)] p-4"
    role="dialog"
    aria-modal="true"
    aria-labelledby="share-dialog-title"
  >
    <div class="w-full max-w-2xl overflow-hidden rounded-xl border border-[var(--border-default)] bg-white shadow-[0_18px_48px_rgba(15,23,42,0.22)]">
      <header class="flex items-start justify-between gap-4 border-b border-[var(--border-default)] px-6 py-4">
        <div class="min-w-0">
          <div class="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--text-muted)]">
            Share access
          </div>
          <h2
            id="share-dialog-title"
            class="m-0 mt-1 truncate text-lg font-semibold text-[var(--text-strong)]"
          >
            {resourceLabel ?? `${resourceKind} · ${resourceId.slice(0, 8)}…`}
          </h2>
        </div>
        <button
          type="button"
          class="rounded-md p-2 text-[var(--text-muted)] hover:bg-[var(--bg-hover)]"
          aria-label="Close share dialog"
          onclick={() => onClose?.()}
        >
          <Glyph name="x" size={16} />
        </button>
      </header>

      <div class="grid gap-0 lg:grid-cols-[1.1fr,0.9fr]">
        <section class="border-b border-[var(--border-default)] px-6 py-5 lg:border-b-0 lg:border-r">
          <div class="text-sm font-semibold text-[var(--text-strong)]">Add a recipient</div>
          <p class="m-0 mt-1 text-xs text-[var(--text-muted)]">
            Provide a user id or group id together with the access level. Expiry is optional.
          </p>

          <div class="mt-4 space-y-3 text-sm">
            <div class="flex gap-2">
              <button
                type="button"
                class={`flex-1 rounded-md border px-3 py-2 transition ${
                  principal === 'user'
                    ? 'border-[#3f7be0] bg-[#eef4fd] text-[#2458b8]'
                    : 'border-[var(--border-default)] hover:border-[#b7c7df]'
                }`}
                onclick={() => {
                  principal = 'user';
                  principalId = '';
                  principalLabelState = '';
                }}
              >
                User
              </button>
              <button
                type="button"
                class={`flex-1 rounded-md border px-3 py-2 transition ${
                  principal === 'group'
                    ? 'border-[#3f7be0] bg-[#eef4fd] text-[#2458b8]'
                    : 'border-[var(--border-default)] hover:border-[#b7c7df]'
                }`}
                onclick={() => {
                  principal = 'group';
                  principalId = '';
                  principalLabelState = '';
                }}
              >
                Group
              </button>
            </div>

            <div>
              <span class="mb-1 block font-medium text-[var(--text-strong)]">
                {principal === 'user' ? 'User' : 'Group'}
              </span>
              <PrincipalPicker
                kind={principal}
                value={principalId}
                onChange={(next) => {
                  principalId = next.id;
                  principalLabelState = next.label;
                }}
              />
              {#if principalLabelState && principalId}
                <div class="mt-1 text-xs text-[var(--text-muted)]">Selected: {principalLabelState}</div>
              {/if}
            </div>

            <label class="block">
              <span class="mb-1 block font-medium text-[var(--text-strong)]">Access level</span>
              <select bind:value={accessLevel} class="of-select">
                <option value="viewer">Viewer</option>
                <option value="editor">Editor</option>
                <option value="owner">Owner</option>
              </select>
            </label>

            <label class="block">
              <span class="mb-1 block font-medium text-[var(--text-strong)]">Note (optional)</span>
              <textarea
                bind:value={note}
                class="of-textarea !min-h-[64px]"
                placeholder="Why is this being shared?"
              ></textarea>
            </label>

            <label class="block">
              <span class="mb-1 block font-medium text-[var(--text-strong)]">Expires (optional)</span>
              <input bind:value={expiresAt} class="of-input" type="datetime-local" />
            </label>

            <button
              type="button"
              class="of-btn of-btn-primary w-full"
              disabled={submitting}
              onclick={submit}
            >
              {#if submitting}Sharing…{:else}Share{/if}
            </button>
          </div>
        </section>

        <section class="px-6 py-5">
          <div class="flex items-center justify-between">
            <div class="text-sm font-semibold text-[var(--text-strong)]">Existing shares</div>
            <span class="text-xs text-[var(--text-soft)]">{shares.length}</span>
          </div>

          {#if loading}
            <div class="mt-3 text-sm text-[var(--text-muted)]">Loading shares…</div>
          {:else if loadError}
            <div class="mt-3 text-sm text-[#b42318]">{loadError}</div>
          {:else if shares.length === 0}
            <div class="mt-3 rounded-md border border-dashed border-[var(--border-default)] px-3 py-4 text-sm text-[var(--text-muted)]">
              Nobody else has access yet.
            </div>
          {:else}
            <ul class="m-0 mt-3 list-none space-y-2 p-0">
              {#each shares as share (share.id)}
                <li class="rounded-md border border-[var(--border-default)] bg-[#fbfcfe] px-3 py-2">
                  <div class="flex items-start justify-between gap-2">
                    <div class="min-w-0">
                      <div class="truncate text-sm font-medium text-[var(--text-strong)]">
                        {principalLabel(share)}
                      </div>
                      <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-[var(--text-muted)]">
                        <span class="of-chip">{share.access_level}</span>
                        <span>Expires {formatDate(share.expires_at)}</span>
                      </div>
                      {#if share.note}
                        <div class="mt-1 text-xs text-[var(--text-muted)]">{share.note}</div>
                      {/if}
                    </div>
                    <button
                      type="button"
                      class="text-xs font-medium text-[#b42318] hover:underline"
                      onclick={() => revoke(share)}
                    >
                      Revoke
                    </button>
                  </div>
                </li>
              {/each}
            </ul>
          {/if}
        </section>
      </div>
    </div>
  </div>
{/if}
