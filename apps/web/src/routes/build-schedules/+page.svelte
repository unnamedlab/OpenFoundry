<!--
  Build Schedules application — Foundry-parity surface for the
  "Find and manage schedules" workflow described in
  Schedules.md § "Find and manage schedules":
    - Files / User / Projects / Name / Pause status filters.
    - Sort by Name / Creation / Last run / Last update.
    - URL-syncable filters so the page is bookmark/share-able.

  The default landing applies "owned by current user", emitting the
  doc-mandated banner: "Showing your schedules. Add filters to broaden."
-->
<script lang="ts">
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import {
    type Schedule,
    listSchedules,
    type ListSchedulesQuery,
  } from '$lib/api/schedules';

  let schedules = $state<Schedule[]>([]);
  let loading = $state(false);
  let errorMsg = $state<string | null>(null);

  // Filter state — initialised from the URL on mount, then mirrored
  // back to the URL as the user edits so every interaction stays
  // bookmark-able.
  const url = $state.snapshot($page.url);
  let filterFiles = $state<string[]>(url.searchParams.getAll('files'));
  let filterUsers = $state<string[]>(url.searchParams.getAll('users'));
  let filterProjects = $state<string[]>(url.searchParams.getAll('projects'));
  let filterName = $state<string>(url.searchParams.get('q') ?? '');
  type PauseFilter = 'all' | 'paused' | 'active';
  let filterPaused = $state<PauseFilter>(
    (url.searchParams.get('paused_filter') ?? 'all') as PauseFilter,
  );
  type SortKey = 'name' | 'created_at' | 'last_run_at' | 'updated_at';
  let sortBy = $state<SortKey>(
    (url.searchParams.get('sort') ?? 'updated_at') as SortKey,
  );

  // "Showing your schedules" banner shows when no broadening filters
  // are present yet — matches the Foundry default landing copy.
  let showOwnerOnlyBanner = $derived(
    filterFiles.length === 0 &&
      filterProjects.length === 0 &&
      filterName.trim() === '' &&
      filterUsers.length === 0,
  );

  let filterInputFiles = $state('');
  let filterInputUsers = $state('');
  let filterInputProjects = $state('');

  function syncToUrl() {
    const next = new URLSearchParams();
    for (const f of filterFiles) next.append('files', f);
    for (const u of filterUsers) next.append('users', u);
    for (const p of filterProjects) next.append('projects', p);
    if (filterName.trim()) next.set('q', filterName.trim());
    if (filterPaused !== 'all') next.set('paused_filter', filterPaused);
    if (sortBy !== 'updated_at') next.set('sort', sortBy);
    const path = `${$page.url.pathname}${next.toString() ? `?${next}` : ''}`;
    goto(path, { replaceState: true, keepFocus: true, noScroll: true });
  }

  async function refresh() {
    loading = true;
    errorMsg = null;
    try {
      const query: ListSchedulesQuery = {
        files: filterFiles,
        users: filterUsers,
        projects: filterProjects,
        q: filterName.trim() || undefined,
        sort: sortBy,
      };
      if (filterPaused === 'paused') query.paused = true;
      else if (filterPaused === 'active') query.paused = false;
      const res = await listSchedules(query);
      schedules = res.data;
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : String(err);
      schedules = [];
    } finally {
      loading = false;
    }
  }

  function addFilter(arr: string[], value: string): string[] {
    const v = value.trim();
    if (!v || arr.includes(v)) return arr;
    return [...arr, v];
  }

  function removeFilter(arr: string[], value: string): string[] {
    return arr.filter((x) => x !== value);
  }

  function summarizeTrigger(s: Schedule): string {
    if ('time' in s.trigger.kind) {
      return `${s.trigger.kind.time.cron} (${s.trigger.kind.time.time_zone})`;
    }
    if ('event' in s.trigger.kind) {
      return `On ${s.trigger.kind.event.type} → ${s.trigger.kind.event.target_rid}`;
    }
    if ('compound' in s.trigger.kind) {
      return `${s.trigger.kind.compound.op} of ${s.trigger.kind.compound.components.length} components`;
    }
    return 'unknown trigger';
  }

  $effect(() => {
    void filterFiles;
    void filterUsers;
    void filterProjects;
    void filterName;
    void filterPaused;
    void sortBy;
    syncToUrl();
    refresh();
  });
</script>

<main class="build-schedules" data-testid="build-schedules-page">
  <header>
    <h1>Build Schedules</h1>
  </header>

  {#if showOwnerOnlyBanner}
    <p class="banner" data-testid="owner-only-banner">
      Showing your schedules. Add filters to broaden.
    </p>
  {/if}

  {#if errorMsg}
    <p class="error" role="alert">{errorMsg}</p>
  {/if}

  <div class="layout">
    <aside class="filters" data-testid="filters-sidebar">
      <h2>Filters</h2>

      <section class="filter-group" data-testid="filter-files">
        <h3>Files</h3>
        <input
          type="text"
          placeholder="Add dataset RID + Enter"
          data-testid="filter-files-input"
          bind:value={filterInputFiles}
          onkeydown={(e) => {
            if (e.key === 'Enter') {
              filterFiles = addFilter(filterFiles, filterInputFiles);
              filterInputFiles = '';
            }
          }}
        />
        <ul class="chips">
          {#each filterFiles as f (f)}
            <li>
              <span>{f}</span>
              <button type="button" onclick={() => (filterFiles = removeFilter(filterFiles, f))}>×</button>
            </li>
          {/each}
        </ul>
      </section>

      <section class="filter-group" data-testid="filter-users">
        <h3>User</h3>
        <input
          type="text"
          placeholder="Add user id + Enter"
          data-testid="filter-users-input"
          bind:value={filterInputUsers}
          onkeydown={(e) => {
            if (e.key === 'Enter') {
              filterUsers = addFilter(filterUsers, filterInputUsers);
              filterInputUsers = '';
            }
          }}
        />
        <ul class="chips">
          {#each filterUsers as u (u)}
            <li>
              <span>{u}</span>
              <button type="button" onclick={() => (filterUsers = removeFilter(filterUsers, u))}>×</button>
            </li>
          {/each}
        </ul>
      </section>

      <section class="filter-group" data-testid="filter-projects">
        <h3>Projects</h3>
        <input
          type="text"
          placeholder="Add project RID + Enter"
          data-testid="filter-projects-input"
          bind:value={filterInputProjects}
          onkeydown={(e) => {
            if (e.key === 'Enter') {
              filterProjects = addFilter(filterProjects, filterInputProjects);
              filterInputProjects = '';
            }
          }}
        />
        <ul class="chips">
          {#each filterProjects as p (p)}
            <li>
              <span>{p}</span>
              <button type="button" onclick={() => (filterProjects = removeFilter(filterProjects, p))}>×</button>
            </li>
          {/each}
        </ul>
      </section>

      <section class="filter-group">
        <h3>Name</h3>
        <input
          type="text"
          placeholder="Substring search"
          data-testid="filter-name-input"
          bind:value={filterName}
        />
      </section>

      <section class="filter-group">
        <h3>Pause status</h3>
        <select bind:value={filterPaused} data-testid="filter-paused-select">
          <option value="all">All</option>
          <option value="paused">Paused</option>
          <option value="active">Active</option>
        </select>
      </section>

      <section class="filter-group">
        <h3>Sort</h3>
        <select bind:value={sortBy} data-testid="sort-select">
          <option value="name">Name</option>
          <option value="created_at">Creation date</option>
          <option value="last_run_at">Last run</option>
          <option value="updated_at">Last update</option>
        </select>
      </section>
    </aside>

    <section class="results">
      {#if loading}
        <p class="hint">Loading…</p>
      {:else if schedules.length === 0}
        <p class="hint">No schedules match the current filters.</p>
      {:else}
        <table data-testid="schedules-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Project</th>
              <th>Trigger</th>
              <th>Scope</th>
              <th>Paused</th>
              <th>Last run</th>
              <th>Last update</th>
              <th>Owner</th>
            </tr>
          </thead>
          <tbody>
            {#each schedules as s (s.rid)}
              <tr data-testid="schedule-row">
                <td>
                  <a href={`/schedules/${s.rid}`}>{s.name}</a>
                </td>
                <td><code class="rid">{s.project_rid}</code></td>
                <td class="trigger">{summarizeTrigger(s)}</td>
                <td>
                  <span class="scope-badge {s.scope_kind === 'PROJECT_SCOPED' ? 'project' : 'user'}">
                    {s.scope_kind}
                  </span>
                </td>
                <td>{s.paused ? '⏸︎' : '▶︎'}</td>
                <td>{s.last_run_at ? new Date(s.last_run_at).toLocaleString() : '—'}</td>
                <td>{new Date(s.updated_at).toLocaleString()}</td>
                <td><code class="rid">{s.created_by}</code></td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  </div>
</main>

<style>
  .build-schedules {
    padding: 24px;
    max-width: 1280px;
    margin: 0 auto;
    color: #e2e8f0;
  }
  header h1 { margin: 0 0 12px; font-size: 18px; }
  .banner {
    background: #0b1220;
    border: 1px solid #334155;
    color: #cbd5e1;
    padding: 8px 12px;
    border-radius: 6px;
    font-size: 12px;
    margin-bottom: 12px;
  }
  .error { color: #fca5a5; }
  .hint { color: #94a3b8; font-style: italic; }
  .layout {
    display: grid;
    grid-template-columns: 240px 1fr;
    gap: 16px;
  }
  .filters {
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 8px;
    padding: 12px;
  }
  .filters h2 { margin: 0 0 8px; font-size: 13px; }
  .filter-group { margin-bottom: 12px; }
  .filter-group h3 {
    margin: 0 0 4px;
    font-size: 11px;
    text-transform: uppercase;
    color: #94a3b8;
    letter-spacing: 0.05em;
  }
  .filter-group input,
  .filter-group select {
    width: 100%;
    background: #1e293b;
    color: #f1f5f9;
    border: 1px solid #334155;
    border-radius: 4px;
    padding: 4px 6px;
    font-size: 12px;
    box-sizing: border-box;
  }
  .chips {
    list-style: none;
    margin: 4px 0 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .chips li {
    display: flex;
    align-items: center;
    gap: 4px;
    background: #1e293b;
    border-radius: 3px;
    padding: 2px 6px;
    font-size: 11px;
    font-family: ui-monospace, 'SF Mono', Consolas, monospace;
  }
  .chips button {
    background: transparent;
    border: none;
    color: #94a3b8;
    cursor: pointer;
  }
  .results {
    background: #0b1220;
    border: 1px solid #1f2937;
    border-radius: 8px;
    padding: 12px;
    overflow-x: auto;
  }
  table { width: 100%; border-collapse: collapse; font-size: 12px; }
  th { text-align: left; padding: 6px 8px; color: #94a3b8; font-weight: 500; border-bottom: 1px solid #1f2937; }
  td { padding: 6px 8px; border-bottom: 1px solid #111827; }
  tr:hover td { background: #111827; }
  .rid { font-family: ui-monospace, 'SF Mono', Consolas, monospace; color: #93c5fd; }
  .trigger { font-family: ui-monospace, 'SF Mono', Consolas, monospace; color: #cbd5e1; }
  .scope-badge {
    padding: 2px 6px;
    border-radius: 3px;
    font-size: 10px;
    font-weight: 600;
  }
  .scope-badge.user { background: #1e293b; color: #cbd5e1; }
  .scope-badge.project { background: #064e3b; color: #6ee7b7; }
  a { color: #93c5fd; }
</style>
