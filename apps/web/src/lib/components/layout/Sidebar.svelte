<script lang="ts">
  import { page } from '$app/stores';
  import Glyph from '$components/ui/Glyph.svelte';
  import type { MessageKey } from '$lib/i18n/messages';
  import { createTranslator, currentLocale } from '$lib/i18n/store';

  const t = $derived.by(() => createTranslator($currentLocale));

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
    labelKey: MessageKey;
    icon: NavIcon;
    hint?: string;
  };

  const workspaceNav: NavItem[] = [
    { href: '/', labelKey: 'nav.home', icon: 'home' },
    { href: '/search', labelKey: 'nav.search', icon: 'search', hint: 'Ctrl + J' },
    { href: '/object-monitors', labelKey: 'nav.notifications', icon: 'bell' },
    { href: '/dashboards', labelKey: 'nav.recent', icon: 'history' },
    { href: '/reports', labelKey: 'nav.files', icon: 'folder' }
  ];

  const applicationNav: NavItem[] = [
    { href: '/projects', labelKey: 'nav.projects', icon: 'folder' },
    { href: '/object-explorer', labelKey: 'nav.objectExplorer', icon: 'object' },
    { href: '/ontology-manager', labelKey: 'nav.ontologyManager', icon: 'ontology' },
    { href: '/notebooks', labelKey: 'nav.workshop', icon: 'code' },
    { href: '/pipelines', labelKey: 'nav.pipelineBuilder', icon: 'graph' },
    { href: '/ml', labelKey: 'nav.training', icon: 'sparkles' }
  ];

  const utilityNav: NavItem[] = [
    { href: '/developers', labelKey: 'nav.support', icon: 'help' },
    { href: '/settings', labelKey: 'nav.account', icon: 'settings' }
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
    <a href="/" class="of-sidebar__logo" aria-label={t('nav.home')} title="OpenFoundry">
      <Glyph name="cube" size={18} />
    </a>
    <span class="of-sidebar__brand-meta" aria-hidden="true">
      <Glyph name="menu" size={14} />
    </span>
  </div>

  <nav class="of-sidebar__section" aria-label="Primary">
    {#each workspaceNav as item}
      <a
        href={item.href}
        class="of-sidebar__link"
        data-active={isActive(item.href, $page.url.pathname)}
        title={t(item.labelKey)}
        aria-label={t(item.labelKey)}
      >
        <span class="of-sidebar__icon">
          <Glyph name={item.icon} size={16} />
        </span>
        <span class="of-sidebar__label">{t(item.labelKey)}</span>
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
      <span class="of-sidebar__label">{t('nav.applications')}</span>
      <span class="of-sidebar__caret">
        <Glyph name={applicationsExpanded ? 'chevron-down' : 'chevron-right'} size={14} />
      </span>
    </button>
  </nav>

  {#if applicationsExpanded}
    <div class="of-sidebar__section">
      <div class="of-sidebar__heading">{t('nav.applications')}</div>
      <nav aria-label="Applications">
        {#each applicationNav as item}
          <a
            href={item.href}
            class="of-sidebar__link of-sidebar__link--app"
            data-active={isActive(item.href, $page.url.pathname)}
            title={t(item.labelKey)}
            aria-label={t(item.labelKey)}
          >
            <span class="of-sidebar__icon">
              <Glyph name={item.icon} size={15} />
            </span>
            <span class="of-sidebar__label">{t(item.labelKey)}</span>
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
        title={t(item.labelKey)}
        aria-label={t(item.labelKey)}
      >
        <span class="of-sidebar__icon">
          <Glyph name={item.icon} size={16} />
        </span>
        <span class="of-sidebar__label">{t(item.labelKey)}</span>
      </a>
    {/each}
  </nav>
</aside>
