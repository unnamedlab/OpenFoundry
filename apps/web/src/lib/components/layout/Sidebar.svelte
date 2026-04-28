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
    | 'database'
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

  type LocalizedCopy = {
    en: string;
    es: string;
  };

  type LocaleKey = keyof LocalizedCopy;

  type LauncherCategoryId =
    | 'all'
    | 'platform'
    | 'administration'
    | 'development'
    | 'integration'
    | 'toolchain'
    | 'models'
    | 'ontology'
    | 'governance';

  type LauncherCategory = {
    id: LauncherCategoryId;
    label: LocalizedCopy;
  };

  type LauncherApp = {
    id: string;
    href: string;
    icon: NavIcon;
    name: LocalizedCopy;
    description: LocalizedCopy;
    badge: LocalizedCopy;
    categoryIds: LauncherCategoryId[];
    createHref?: string;
    browseHref?: string;
  };

  const workspaceNav: NavItem[] = [
    { href: '/', labelKey: 'nav.home', icon: 'home' },
    { href: '/search', labelKey: 'nav.search', icon: 'search', hint: 'Ctrl + J' },
    { href: '/object-monitors', labelKey: 'nav.notifications', icon: 'bell' },
    { href: '/dashboards', labelKey: 'nav.recent', icon: 'history' },
    { href: '/reports', labelKey: 'nav.files', icon: 'folder' }
  ];

  const launcherCopy = {
    en: {
      title: 'Applications',
      searchPlaceholder: 'Search for applications…',
      close: 'Close applications launcher',
      open: 'Open',
      createNew: 'Create new',
      browseCatalog: 'Browse catalog',
      favorites: 'Favorites & recents',
      emptyFavorites: 'No favorites or recents yet.',
      emptySearch: 'No applications matched this search.',
      searchAriaLabel: 'Search applications'
    },
    es: {
      title: 'Aplicaciones',
      searchPlaceholder: 'Buscar aplicaciones…',
      close: 'Cerrar lanzador de aplicaciones',
      open: 'Abrir',
      createNew: 'Crear nuevo',
      browseCatalog: 'Explorar catálogo',
      favorites: 'Favoritos y recientes',
      emptyFavorites: 'Todavía no hay favoritos ni recientes.',
      emptySearch: 'Ninguna aplicación coincide con esta búsqueda.',
      searchAriaLabel: 'Buscar aplicaciones'
    }
  } as const;

  const launcherCategories: LauncherCategory[] = [
    { id: 'all', label: { en: 'All apps', es: 'Todas las apps' } },
    { id: 'platform', label: { en: 'Platform apps', es: 'Apps de plataforma' } },
    { id: 'administration', label: { en: 'Administration', es: 'Administración' } },
    { id: 'development', label: { en: 'Application development', es: 'Desarrollo de aplicaciones' } },
    { id: 'integration', label: { en: 'Data integration', es: 'Integración de datos' } },
    { id: 'toolchain', label: { en: 'Developer toolchain', es: 'Toolchain de desarrollo' } },
    { id: 'models', label: { en: 'Models', es: 'Modelos' } },
    { id: 'ontology', label: { en: 'Ontology', es: 'Ontología' } },
    { id: 'governance', label: { en: 'Security & governance', es: 'Seguridad y gobierno' } }
  ];

  const launcherApps: LauncherApp[] = [
    {
      id: 'datasets',
      href: '/datasets',
      icon: 'database',
      name: { en: 'Data Catalog', es: 'Catálogo de datos' },
      description: {
        en: 'Search datasets by owner, tags, and quality status from one registry.',
        es: 'Busca datasets por owner, etiquetas y estado de calidad desde un único registro.'
      },
      badge: { en: 'Data integration', es: 'Integración de datos' },
      categoryIds: ['all', 'platform', 'integration'],
      createHref: '/datasets/upload',
      browseHref: '/datasets'
    },
    {
      id: 'pipelines',
      href: '/pipelines',
      icon: 'graph',
      name: { en: 'Pipeline Builder', es: 'Constructor de pipelines' },
      description: {
        en: 'Design and monitor operational pipelines, runs, and automation jobs.',
        es: 'Diseña y monitoriza pipelines operativos, ejecuciones y trabajos de automatización.'
      },
      badge: { en: 'Data integration', es: 'Integración de datos' },
      categoryIds: ['all', 'integration'],
      createHref: '/pipelines/new',
      browseHref: '/pipelines'
    },
    {
      id: 'lineage',
      href: '/lineage',
      icon: 'graph',
      name: { en: 'Data Lineage', es: 'Lineage de datos' },
      description: {
        en: 'Inspect upstream and downstream dependencies across the data estate.',
        es: 'Inspecciona dependencias upstream y downstream a través del patrimonio de datos.'
      },
      badge: { en: 'Ontology', es: 'Ontología' },
      categoryIds: ['all', 'integration', 'ontology'],
      browseHref: '/lineage'
    },
    {
      id: 'object-explorer',
      href: '/object-explorer',
      icon: 'object',
      name: { en: 'Object Explorer', es: 'Explorador de objetos' },
      description: {
        en: 'Explore linked operational entities, activity, and related records.',
        es: 'Explora entidades operativas vinculadas, actividad y registros relacionados.'
      },
      badge: { en: 'Platform app', es: 'App de plataforma' },
      categoryIds: ['all', 'platform', 'ontology'],
      browseHref: '/object-explorer'
    },
    {
      id: 'ontology-manager',
      href: '/ontology-manager',
      icon: 'ontology',
      name: { en: 'Ontology Manager', es: 'Gestor de ontología' },
      description: {
        en: 'Shape core object models, semantics, and linked operational concepts.',
        es: 'Modela objetos clave, semántica y conceptos operativos enlazados.'
      },
      badge: { en: 'Ontology', es: 'Ontología' },
      categoryIds: ['all', 'platform', 'ontology'],
      browseHref: '/ontology-manager'
    },
    {
      id: 'apps',
      href: '/apps',
      icon: 'cube',
      name: { en: 'Workshop App Builder', es: 'Workshop App Builder' },
      description: {
        en: 'Build internal applications with widgets, templates, runtime previews, and publishing.',
        es: 'Construye aplicaciones internas con widgets, plantillas, vista previa runtime y publicación.'
      },
      badge: { en: 'Application development', es: 'Desarrollo de aplicaciones' },
      categoryIds: ['all', 'development', 'toolchain'],
      createHref: '/apps',
      browseHref: '/apps'
    },
    {
      id: 'notebooks',
      href: '/notebooks',
      icon: 'code',
      name: { en: 'Workshop', es: 'Workshop' },
      description: {
        en: 'Develop notebook-based workflows, analyses, and collaborative experiments.',
        es: 'Desarrolla workflows basados en notebooks, análisis y experimentos colaborativos.'
      },
      badge: { en: 'Developer toolchain', es: 'Toolchain de desarrollo' },
      categoryIds: ['all', 'development', 'toolchain'],
      browseHref: '/notebooks'
    },
    {
      id: 'code-repos',
      href: '/code-repos',
      icon: 'code',
      name: { en: 'Code Repositories', es: 'Repositorios de código' },
      description: {
        en: 'Browse repositories, reviews, CI gates, commits, and protected merge flows.',
        es: 'Explora repositorios, revisiones, puertas CI, commits y flujos de merge protegidos.'
      },
      badge: { en: 'Developer toolchain', es: 'Toolchain de desarrollo' },
      categoryIds: ['all', 'development', 'toolchain'],
      browseHref: '/code-repos'
    },
    {
      id: 'marketplace',
      href: '/marketplace',
      icon: 'folder',
      name: { en: 'Marketplace', es: 'Marketplace' },
      description: {
        en: 'Discover internal packages, release channels, and one-click rollout bundles.',
        es: 'Descubre paquetes internos, canales de release y bundles de despliegue con un clic.'
      },
      badge: { en: 'Application development', es: 'Desarrollo de aplicaciones' },
      categoryIds: ['all', 'development'],
      browseHref: '/marketplace'
    },
    {
      id: 'ai',
      href: '/ai',
      icon: 'sparkles',
      name: { en: 'AI Platform', es: 'Plataforma AI' },
      description: {
        en: 'Manage providers, prompts, guardrails, agents, and copilots from one control plane.',
        es: 'Gestiona proveedores, prompts, guardrails, agentes y copilots desde un solo plano de control.'
      },
      badge: { en: 'Models', es: 'Modelos' },
      categoryIds: ['all', 'models'],
      browseHref: '/ai'
    },
    {
      id: 'ml',
      href: '/ml',
      icon: 'sparkles',
      name: { en: 'ML Studio', es: 'ML Studio' },
      description: {
        en: 'Track experiments, training jobs, model versions, and online feature flows.',
        es: 'Sigue experimentos, trabajos de entrenamiento, versiones de modelo y flujos de features online.'
      },
      badge: { en: 'Models', es: 'Modelos' },
      categoryIds: ['all', 'models'],
      browseHref: '/ml'
    },
    {
      id: 'streaming',
      href: '/streaming',
      icon: 'graph',
      name: { en: 'Streaming', es: 'Streaming' },
      description: {
        en: 'Operate real-time topologies, windows, joins, and live event tails.',
        es: 'Opera topologías en tiempo real, ventanas, joins y colas de eventos en vivo.'
      },
      badge: { en: 'Data integration', es: 'Integración de datos' },
      categoryIds: ['all', 'integration'],
      browseHref: '/streaming'
    },
    {
      id: 'audit',
      href: '/audit',
      icon: 'bell',
      name: { en: 'Governance Center', es: 'Centro de gobierno' },
      description: {
        en: 'Review policies, approvals, applications, and governance templates.',
        es: 'Revisa políticas, aprobaciones, aplicaciones y plantillas de gobierno.'
      },
      badge: { en: 'Security & governance', es: 'Seguridad y gobierno' },
      categoryIds: ['all', 'administration', 'governance'],
      browseHref: '/audit'
    },
    {
      id: 'settings',
      href: '/settings',
      icon: 'settings',
      name: { en: 'Settings', es: 'Configuración' },
      description: {
        en: 'Configure identity, language, security posture, and account defaults.',
        es: 'Configura identidad, idioma, postura de seguridad y valores por defecto de la cuenta.'
      },
      badge: { en: 'Administration', es: 'Administración' },
      categoryIds: ['all', 'administration', 'governance'],
      browseHref: '/settings'
    }
  ];

  const utilityNav: NavItem[] = [
    { href: '/developers', labelKey: 'nav.support', icon: 'help' },
    { href: '/settings', labelKey: 'nav.account', icon: 'settings' }
  ];

  let applicationsLauncherOpen = $state(false);
  let selectedLauncherCategory = $state<LauncherCategoryId>('all');
  let selectedLauncherAppId = $state('datasets');
  let launcherSearch = $state('');

  function isActive(href: string, pathname: string) {
    return href === '/' ? pathname === '/' : pathname === href || pathname.startsWith(`${href}/`);
  }

  function localize(copy: LocalizedCopy) {
    return ($currentLocale === 'es' ? copy.es : copy.en) as string;
  }

  const hasActiveApplication = $derived.by(() =>
    launcherApps.some((item) => isActive(item.href, $page.url.pathname))
  );

  const activeLauncherCopy = $derived.by(() =>
    ($currentLocale === 'es' ? launcherCopy.es : launcherCopy.en) as (typeof launcherCopy)[LocaleKey]
  );
  const launcherCategoryCounts = $derived.by(() =>
    Object.fromEntries(
      launcherCategories.map((category) => [
        category.id,
        category.id === 'all'
          ? launcherApps.length
          : launcherApps.filter((item) => item.categoryIds.includes(category.id)).length
      ])
    ) as Record<LauncherCategoryId, number>
  );
  const visibleLauncherApps = $derived.by(() => {
    const searchTerm = launcherSearch.trim().toLowerCase();
    return launcherApps.filter((item) => {
      const matchesCategory =
        selectedLauncherCategory === 'all' || item.categoryIds.includes(selectedLauncherCategory);
      const localizedName = localize(item.name).toLowerCase();
      const localizedDescription = localize(item.description).toLowerCase();
      const matchesSearch =
        !searchTerm ||
        localizedName.includes(searchTerm) ||
        localizedDescription.includes(searchTerm);
      return matchesCategory && matchesSearch;
    });
  });
  const selectedLauncherApp = $derived.by(
    () =>
      visibleLauncherApps.find((item: LauncherApp) => item.id === selectedLauncherAppId) ??
      visibleLauncherApps[0] ??
      null
  );

  $effect(() => {
    if (selectedLauncherApp?.id !== selectedLauncherAppId) {
      selectedLauncherAppId = selectedLauncherApp?.id ?? '';
    }
  });

  function closeApplicationsLauncher() {
    applicationsLauncherOpen = false;
  }

  function openApplicationsLauncher() {
    const activeApp = launcherApps.find((item) => isActive(item.href, $page.url.pathname));
    selectedLauncherCategory = activeApp?.categoryIds[1] ?? 'all';
    selectedLauncherAppId = activeApp?.id ?? launcherApps[0].id;
    launcherSearch = '';
    applicationsLauncherOpen = true;
  }

  function toggleApplicationsLauncher() {
    if (applicationsLauncherOpen) {
      closeApplicationsLauncher();
      return;
    }
    openApplicationsLauncher();
  }

  function handleWindowKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && applicationsLauncherOpen) {
      closeApplicationsLauncher();
    }
  }
</script>

<svelte:window onkeydown={handleWindowKeydown} />

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
      data-active={hasActiveApplication || applicationsLauncherOpen}
      data-expanded={applicationsLauncherOpen}
      aria-haspopup="dialog"
      aria-expanded={applicationsLauncherOpen}
      onclick={toggleApplicationsLauncher}
    >
      <span class="of-sidebar__icon">
        <Glyph name="cube" size={16} />
      </span>
      <span class="of-sidebar__label">{t('nav.applications')}</span>
      <span class="of-sidebar__caret">
        <Glyph name={applicationsLauncherOpen ? 'chevron-down' : 'chevron-right'} size={14} />
      </span>
    </button>
  </nav>

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

{#if applicationsLauncherOpen}
  <div class="of-app-launcher">
    <button
      type="button"
      class="of-app-launcher__backdrop"
      aria-label={activeLauncherCopy.close}
      onclick={closeApplicationsLauncher}
    ></button>
    <div
      class="of-app-launcher__surface"
      role="dialog"
      aria-modal="true"
      aria-label={activeLauncherCopy.title}
    >
      <div class="of-app-launcher__header">
        <label class="of-app-launcher__search">
          <span class="sr-only">{activeLauncherCopy.searchAriaLabel}</span>
          <span class="of-app-launcher__search-icon">
            <Glyph name="search" size={14} />
          </span>
          <input
            bind:value={launcherSearch}
            type="search"
            placeholder={activeLauncherCopy.searchPlaceholder}
            aria-label={activeLauncherCopy.searchAriaLabel}
          />
        </label>
        <button
          type="button"
          class="of-app-launcher__close"
          aria-label={activeLauncherCopy.close}
          onclick={closeApplicationsLauncher}
        >
          ×
        </button>
      </div>

      <div class="of-app-launcher__body">
        <div class="of-app-launcher__categories">
          {#each launcherCategories as category}
            <button
              type="button"
              class="of-app-launcher__category"
              data-active={selectedLauncherCategory === category.id}
              onclick={() => {
                selectedLauncherCategory = category.id;
              }}
            >
              <span>{localize(category.label)}</span>
              <span class="of-app-launcher__category-count">{launcherCategoryCounts[category.id]}</span>
            </button>
          {/each}
        </div>

        <div class="of-app-launcher__catalog">
          {#if visibleLauncherApps.length > 0}
            {#each visibleLauncherApps as app}
              <a
                href={app.href}
                class="of-app-launcher__item"
                data-selected={selectedLauncherApp?.id === app.id}
                data-active={isActive(app.href, $page.url.pathname)}
                onmouseenter={() => {
                  selectedLauncherAppId = app.id;
                }}
                onfocus={() => {
                  selectedLauncherAppId = app.id;
                }}
                onclick={closeApplicationsLauncher}
              >
                <span class="of-app-launcher__item-icon">
                  <Glyph name={app.icon} size={16} />
                </span>
                <span class="of-app-launcher__item-copy">
                  <span class="of-app-launcher__item-name">{localize(app.name)}</span>
                  <span class="of-app-launcher__item-description">{localize(app.description)}</span>
                </span>
              </a>
            {/each}
          {:else}
            <div class="of-app-launcher__empty">{activeLauncherCopy.emptySearch}</div>
          {/if}
        </div>

        <div class="of-app-launcher__detail">
          {#if selectedLauncherApp}
            <div class="of-app-launcher__detail-icon">
              <Glyph name={selectedLauncherApp.icon} size={18} />
            </div>
            <div class="of-app-launcher__detail-copy">
              <div class="of-app-launcher__detail-badge">{localize(selectedLauncherApp.badge)}</div>
              <h2>{localize(selectedLauncherApp.name)}</h2>
              <p>{localize(selectedLauncherApp.description)}</p>
            </div>

            <div class="of-app-launcher__actions">
              <a href={selectedLauncherApp.href} class="of-app-launcher__link" onclick={closeApplicationsLauncher}>
                {activeLauncherCopy.open}
              </a>
              <a
                href={selectedLauncherApp.createHref ?? selectedLauncherApp.href}
                class="of-app-launcher__button of-app-launcher__button--primary"
                onclick={closeApplicationsLauncher}
              >
                {activeLauncherCopy.createNew}
              </a>
              <a
                href={selectedLauncherApp.browseHref ?? '/apps'}
                class="of-app-launcher__button"
                onclick={closeApplicationsLauncher}
              >
                {activeLauncherCopy.browseCatalog}
              </a>
            </div>

            <div class="of-app-launcher__favorites">
              <div class="of-app-launcher__favorites-title">{activeLauncherCopy.favorites}</div>
              <p>{activeLauncherCopy.emptyFavorites}</p>
            </div>
          {/if}
        </div>
      </div>
    </div>
  </div>
{/if}
