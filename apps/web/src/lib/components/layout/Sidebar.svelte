<script lang="ts">
  import { page } from '$app/stores';
  import Glyph from '$components/ui/Glyph.svelte';

  type NavIcon =
    | 'home'
    | 'search'
    | 'bell'
    | 'history'
    | 'folder'
    | 'cube'
    | 'object'
    | 'ontology'
    | 'code'
    | 'graph'
    | 'help'
    | 'settings'
    | 'sparkles';

  type NavItem = {
    href: string;
    label: string;
    icon: NavIcon;
    hint?: string;
  };

  const workspaceNav: NavItem[] = [
    { href: '/', label: 'Home', icon: 'home' },
    { href: '/search', label: 'Search...', icon: 'search', hint: 'Ctrl + J' },
    { href: '/object-monitors', label: 'Notifications', icon: 'bell' },
    { href: '/dashboards', label: 'Recent', icon: 'history' },
    { href: '/reports', label: 'Files', icon: 'folder' }
  ];

  const applicationNav: NavItem[] = [
    { href: '/projects', label: 'Projects & files', icon: 'folder' },
    { href: '/object-explorer', label: 'Object Explorer', icon: 'object' },
    { href: '/ontology-manager', label: 'Ontology Manager', icon: 'ontology' },
    { href: '/notebooks', label: 'Workshop', icon: 'code' },
    { href: '/pipelines', label: 'Pipeline Builder', icon: 'graph' },
    { href: '/ml', label: 'Training', icon: 'sparkles' }
  ];

  const utilityNav: NavItem[] = [
    { href: '/developers', label: 'Support', icon: 'help' },
    { href: '/settings', label: 'Account', icon: 'settings' }
  ];

  let applicationsExpanded = $state(true);

  function isActive(href: string, pathname: string) {
    return href === '/' ? pathname === '/' : pathname === href || pathname.startsWith(`${href}/`);
  }

  const hasActiveApplication = $derived.by(() =>
    applicationNav.some((item) => isActive(item.href, $page.url.pathname))
  );
</script>

<aside class="of-sidebar of-scrollbar">
  <div class="of-sidebar__brand">
    <a href="/" class="of-sidebar__logo" aria-label="OpenFoundry home" title="OpenFoundry">
      <Glyph name="cube" size={18} />
    </a>
    <button type="button" class="of-sidebar__rail-toggle" aria-label="Navigation menu">
      <Glyph name="menu" size={16} />
    </button>
  </div>

  <nav class="of-sidebar__section" aria-label="Primary">
    {#each workspaceNav as item}
      <a
        href={item.href}
        class="of-sidebar__link"
        data-active={isActive(item.href, $page.url.pathname)}
      >
        <span class="of-sidebar__icon">
          <Glyph name={item.icon} size={16} />
        </span>
        <span>{item.label}</span>
        {#if item.hint}
          <span class="of-sidebar__hint">{item.hint}</span>
        {/if}
      </a>
    {/each}

    <button
      type="button"
      class="of-sidebar__link of-sidebar__link--button"
      data-active={hasActiveApplication}
      data-expanded={applicationsExpanded}
      onclick={() => {
        applicationsExpanded = !applicationsExpanded;
      }}
    >
      <span class="of-sidebar__icon">
        <Glyph name="cube" size={16} />
      </span>
      <span>Applications</span>
      <span class="of-sidebar__caret">
        <Glyph name={applicationsExpanded ? 'chevron-down' : 'chevron-right'} size={14} />
      </span>
    </button>
  </nav>

  {#if applicationsExpanded}
    <div class="of-sidebar__section">
      <div class="of-sidebar__heading">Applications</div>
      <nav aria-label="Applications">
        {#each applicationNav as item}
          <a
            href={item.href}
            class="of-sidebar__link of-sidebar__link--app"
            data-active={isActive(item.href, $page.url.pathname)}
          >
            <span class="of-sidebar__icon">
              <Glyph name={item.icon} size={15} />
            </span>
            <span>{item.label}</span>
          </a>
        {/each}
      </nav>
    </div>
  {/if}

  <div class="of-sidebar__spacer"></div>

  <nav class="of-sidebar__section of-sidebar__section--footer" aria-label="Utility">
    {#each utilityNav as item}
      <a
        href={item.href}
        class="of-sidebar__link"
        data-active={isActive(item.href, $page.url.pathname)}
      >
        <span class="of-sidebar__icon">
          <Glyph name={item.icon} size={16} />
        </span>
        <span>{item.label}</span>
      </a>
    {/each}
  </nav>
</aside>
