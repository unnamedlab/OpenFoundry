<!--
  T5.1 — AboutPanel

  Foundry's "About" sub-panel under Details. Surfaces the immutable
  identity (RID, project path) plus editable owner/description/tags
  metadata. The component is *controlled*: parent owns `dataset` and
  the `users` list and supplies an `onSave` callback that performs
  the actual API call. This keeps the page-level loading state in one
  place.
-->
<script lang="ts">
  import type { Dataset } from '$lib/api/datasets';
  import type { UserProfile } from '$lib/api/auth';

  type Props = {
    dataset: Dataset;
    users: UserProfile[];
    saving?: boolean;
    error?: string;
    onSave: (patch: { description: string; tags: string[]; owner_id: string }) => void | Promise<void>;
  };

  const { dataset, users, saving = false, error = '', onSave }: Props = $props();

  // Local editable state, seeded from the dataset prop. We deliberately
  // don't use $derived here so the user's typed-in changes survive a
  // parent rerender that doesn't mutate the dataset (e.g. a refetched
  // tag facet list).
  let description = $state(dataset.description);
  let tagsInput = $state(dataset.tags.join(', '));
  let ownerId = $state(dataset.owner_id);

  // If the dataset rid actually changes (navigating between datasets in
  // the same SPA shell), reset.
  $effect(() => {
    description = dataset.description;
    tagsInput = dataset.tags.join(', ');
    ownerId = dataset.owner_id;
  });

  function ownerName(uid: string): string {
    return users.find((u) => u.id === uid)?.name ?? uid.slice(0, 8);
  }

  function projectPath(): string {
    // Datasets in the catalog don't carry a project path field yet, so
    // we synthesise one from the storage path. Foundry shows something
    // like `My Project / sub-folder / dataset-name`. Until the catalog
    // exposes a real `project_id`, we mirror the storage hierarchy.
    const path = dataset.storage_path || '';
    const parts = path.split('/').filter(Boolean);
    if (parts.length === 0) return dataset.name;
    return parts.join(' / ');
  }

  async function submit() {
    const tags = tagsInput
      .split(',')
      .map((t) => t.trim())
      .filter(Boolean);
    await onSave({ description, tags, owner_id: ownerId });
  }
</script>

<section class="space-y-5">
  <header class="flex items-start justify-between gap-3">
    <div>
      <div class="text-xs uppercase tracking-[0.22em] text-gray-400">About</div>
      <h2 class="mt-1 text-lg font-semibold">{dataset.name}</h2>
      <p class="mt-1 text-sm text-gray-500">Identity, ownership and human-readable metadata.</p>
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

  <dl class="grid grid-cols-1 gap-3 text-sm md:grid-cols-2">
    <div>
      <dt class="text-xs uppercase tracking-wide text-gray-400">RID</dt>
      <dd class="mt-0.5 break-all font-mono text-xs">{dataset.id}</dd>
    </div>
    <div>
      <dt class="text-xs uppercase tracking-wide text-gray-400">Project path</dt>
      <dd class="mt-0.5">{projectPath()}</dd>
    </div>
    <div>
      <dt class="text-xs uppercase tracking-wide text-gray-400">Format</dt>
      <dd class="mt-0.5">{dataset.format}</dd>
    </div>
    <div>
      <dt class="text-xs uppercase tracking-wide text-gray-400">Active branch</dt>
      <dd class="mt-0.5">{dataset.active_branch} (v{dataset.current_version})</dd>
    </div>
    <div>
      <dt class="text-xs uppercase tracking-wide text-gray-400">Created</dt>
      <dd class="mt-0.5">{new Date(dataset.created_at).toLocaleString()}</dd>
    </div>
    <div>
      <dt class="text-xs uppercase tracking-wide text-gray-400">Updated</dt>
      <dd class="mt-0.5">{new Date(dataset.updated_at).toLocaleString()}</dd>
    </div>
  </dl>

  <div class="space-y-3">
    <div>
      <label for="about-owner" class="mb-1 block text-sm font-medium">Owner</label>
      <select
        id="about-owner"
        bind:value={ownerId}
        class="w-full rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800"
      >
        {#each users as user (user.id)}
          <option value={user.id}>{user.name}</option>
        {/each}
      </select>
      <p class="mt-1 text-xs text-gray-500">Currently: {ownerName(dataset.owner_id)}</p>
    </div>

    <div>
      <label for="about-description" class="mb-1 block text-sm font-medium">
        Description (Markdown)
      </label>
      <textarea
        id="about-description"
        rows="5"
        bind:value={description}
        class="w-full rounded-xl border border-slate-200 px-3 py-2 font-mono text-sm dark:border-gray-700 dark:bg-gray-800"
      ></textarea>
    </div>

    <div>
      <label for="about-tags" class="mb-1 block text-sm font-medium">Tags</label>
      <input
        id="about-tags"
        bind:value={tagsInput}
        placeholder="finance, monthly, curated"
        class="w-full rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800"
      />
      <div class="mt-2 flex flex-wrap gap-2">
        {#each dataset.tags as tag (tag)}
          <span class="rounded-full bg-blue-100 px-2.5 py-1 text-xs font-medium text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">
            {tag}
          </span>
        {/each}
      </div>
    </div>
  </div>

  {#if error}
    <div class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">
      {error}
    </div>
  {/if}
</section>
