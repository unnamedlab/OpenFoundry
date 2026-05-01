<script lang="ts">
  import { onMount } from 'svelte';
  import { listGroups, listUsers, type GroupRecord, type UserProfile } from '$lib/api/auth';

  export type Principal =
    | { kind: 'user'; id: string; label: string; sublabel?: string }
    | { kind: 'group'; id: string; label: string; sublabel?: string };

  let {
    kind,
    value,
    onChange,
    placeholder = 'Search by name or email…',
  }: {
    kind: 'user' | 'group';
    value: string;
    onChange: (next: { id: string; label: string }) => void;
    placeholder?: string;
  } = $props();

  let users = $state<UserProfile[]>([]);
  let groups = $state<GroupRecord[]>([]);
  let loading = $state(false);
  let loadError = $state('');
  let usersLoaded = false;
  let groupsLoaded = false;

  let query = $state('');
  let open = $state(false);
  let highlightIndex = $state(0);
  let inputEl = $state<HTMLInputElement | null>(null);

  // When `value` changes externally (e.g. parent reset), surface the
  // selected label in the input so the user sees what was committed.
  $effect(() => {
    if (!value) {
      query = '';
      return;
    }
    if (kind === 'user') {
      const match = users.find((u) => u.id === value);
      if (match) query = match.name || match.email;
    } else {
      const match = groups.find((g) => g.id === value);
      if (match) query = match.name;
    }
  });

  $effect(() => {
    if (kind === 'user') void ensureUsers();
    else void ensureGroups();
  });

  async function ensureUsers() {
    if (usersLoaded || loading) return;
    loading = true;
    loadError = '';
    try {
      users = await listUsers();
      usersLoaded = true;
    } catch (cause) {
      loadError = cause instanceof Error ? cause.message : 'Unable to load users';
      users = [];
    } finally {
      loading = false;
    }
  }

  async function ensureGroups() {
    if (groupsLoaded || loading) return;
    loading = true;
    loadError = '';
    try {
      groups = await listGroups();
      groupsLoaded = true;
    } catch (cause) {
      loadError = cause instanceof Error ? cause.message : 'Unable to load groups';
      groups = [];
    } finally {
      loading = false;
    }
  }

  const matches = $derived.by<Principal[]>(() => {
    const q = query.trim().toLowerCase();
    if (kind === 'user') {
      const filtered = q
        ? users.filter(
            (u) =>
              (u.name || '').toLowerCase().includes(q) ||
              (u.email || '').toLowerCase().includes(q),
          )
        : users;
      return filtered.slice(0, 25).map((u) => ({
        kind: 'user' as const,
        id: u.id,
        label: u.name || u.email,
        sublabel: u.email,
      }));
    }
    const filtered = q
      ? groups.filter(
          (g) =>
            (g.name || '').toLowerCase().includes(q) ||
            (g.description || '').toLowerCase().includes(q),
        )
      : groups;
    return filtered.slice(0, 25).map((g) => ({
      kind: 'group' as const,
      id: g.id,
      label: g.name,
      sublabel: g.description ?? undefined,
    }));
  });

  function pick(p: Principal) {
    onChange({ id: p.id, label: p.label });
    query = p.label;
    open = false;
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      open = true;
      highlightIndex = Math.min(highlightIndex + 1, Math.max(matches.length - 1, 0));
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      highlightIndex = Math.max(highlightIndex - 1, 0);
    } else if (event.key === 'Enter') {
      if (open && matches[highlightIndex]) {
        event.preventDefault();
        pick(matches[highlightIndex]);
      }
    } else if (event.key === 'Escape') {
      open = false;
    }
  }

  function handleInput(event: Event) {
    query = (event.currentTarget as HTMLInputElement).value;
    highlightIndex = 0;
    open = true;
    if (!query) onChange({ id: '', label: '' });
  }

  function handleBlur() {
    // delay to allow click on a list item to register
    setTimeout(() => (open = false), 120);
  }

  onMount(() => {
    if (kind === 'user') void ensureUsers();
    else void ensureGroups();
  });
</script>

<div class="relative">
  <input
    bind:this={inputEl}
    class="of-input w-full"
    type="text"
    role="combobox"
    aria-expanded={open}
    aria-autocomplete="list"
    {placeholder}
    value={query}
    oninput={handleInput}
    onfocus={() => (open = true)}
    onblur={handleBlur}
    onkeydown={handleKeydown}
  />

  {#if open && (matches.length > 0 || loading || loadError)}
    <ul
      class="absolute left-0 right-0 top-full z-30 mt-1 max-h-64 overflow-auto rounded-md border border-[var(--border-default)] bg-white py-1 shadow-lg"
      role="listbox"
    >
      {#if loading}
        <li class="px-3 py-2 text-xs text-[var(--text-muted)]">Loading…</li>
      {:else if loadError}
        <li class="px-3 py-2 text-xs text-[#b42318]">{loadError}</li>
      {:else}
        {#each matches as match, index (`${match.kind}:${match.id}`)}
          <li>
            <button
              type="button"
              role="option"
              aria-selected={index === highlightIndex}
              class={`flex w-full flex-col items-start px-3 py-1.5 text-left text-sm ${
                index === highlightIndex ? 'bg-[var(--bg-hover)]' : ''
              }`}
              onmousedown={(event) => {
                // mousedown fires before blur, prevents the dropdown from closing first
                event.preventDefault();
                pick(match);
              }}
              onmouseenter={() => (highlightIndex = index)}
            >
              <span class="font-medium text-[var(--text-strong)]">{match.label}</span>
              {#if match.sublabel}
                <span class="text-xs text-[var(--text-muted)]">{match.sublabel}</span>
              {/if}
            </button>
          </li>
        {/each}
      {/if}
    </ul>
  {/if}
</div>
