<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import NotificationBell from '$components/layout/NotificationBell.svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import { auth } from '$stores/auth';

  const isAuthenticated = auth.isAuthenticated;
  const user = auth.user;

  const titleMap: Record<string, string> = {
    '/': 'Projects & files',
    '/apps': 'Applications',
    '/dashboards': 'Recent',
    '/datasets': 'Datasets',
    '/ml': 'Training',
    '/notebooks': 'Workshop',
    '/object-explorer': 'Object Explorer',
    '/object-monitors': 'Notifications',
    '/ontology': 'Ontology',
    '/ontology-manager': 'Ontology Manager',
    '/pipelines': 'Pipeline Builder',
    '/projects': 'Projects & files',
    '/queries': 'Queries',
    '/reports': 'Files',
    '/search': 'Search',
    '/settings': 'Account'
  };

  const pageTitle = $derived.by(() => {
    const pathname = $page.url.pathname;
    const sorted = Object.keys(titleMap).sort((a, b) => b.length - a.length);
    const match = sorted.find((key) => pathname === key || pathname.startsWith(`${key}/`));
    return match ? titleMap[match] : 'OpenFoundry';
  });

  function handleLogout() {
    auth.logout();
    goto('/auth/login');
  }
</script>

<header class="of-topbar">
  <div class="of-topbar__crumbs">
    <span class="of-topbar__crumb-icon">
      <Glyph name="folder" size={14} />
    </span>
    <span class="of-topbar__crumb">OpenFoundry Workspace</span>
    <Glyph name="chevron-right" size={12} />
    <span class="of-topbar__crumb of-topbar__crumb--current">{pageTitle}</span>
  </div>

  <div class="of-topbar__actions">
    <a href="/apps" class="of-topbar__action">
      <Glyph name="cube" size={14} />
      <span>Applications</span>
    </a>

    {#if $isAuthenticated}
      <NotificationBell />
      <div class="of-topbar__user">
        <span class="of-topbar__avatar">OF</span>
        <div class="min-w-0">
          <div class="truncate text-[12px] font-semibold text-[var(--text-strong)]">
            {$user?.name ?? 'OpenFoundry User'}
          </div>
          <div class="truncate text-[11px] text-[var(--text-muted)]">Workspace session</div>
        </div>
      </div>
      <button type="button" class="of-topbar__action" onclick={handleLogout} aria-label="Logout">
        <Glyph name="logout" size={14} />
      </button>
    {:else}
      <a href="/auth/login" class="of-topbar__action">Login</a>
    {/if}
  </div>
</header>
