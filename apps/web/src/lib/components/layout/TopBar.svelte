<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import NotificationBell from '$components/layout/NotificationBell.svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import { ontologySearch } from '$lib/stores/ontologySearch';
  import type { MessageKey } from '$lib/i18n/messages';
  import {
    createTranslator,
    currentLocale,
    getLocaleLabel,
    setLocale,
    supportedLocales,
    type AppLocale
  } from '$lib/i18n/store';
  import { auth } from '$stores/auth';

  const isAuthenticated = auth.isAuthenticated;
  const user = auth.user;
  const t = $derived.by(() => createTranslator($currentLocale));
  const languageOptions = supportedLocales;
  let createMenuOpen = $state(false);
  let createMenuSearch = $state('');
  let createMenuCategory = $state<'all' | 'integration'>('all');
  let createMenuRef = $state<HTMLDivElement | null>(null);

  const isPipelineBuilderVisible = $derived.by(() => {
    const searchTerm = createMenuSearch.trim().toLowerCase();
    const matchesCategory = createMenuCategory === 'all' || createMenuCategory === 'integration';
    const title = t('nav.pipelineBuilder').toLowerCase();
    const description = t('topbar.pipelineBuilderDescription').toLowerCase();
    return matchesCategory && (!searchTerm || title.includes(searchTerm) || description.includes(searchTerm));
  });

  const titleMap: Record<string, MessageKey> = {
    '/': 'nav.home',
    '/apps': 'nav.applications',
    '/dashboards': 'nav.recent',
    '/datasets': 'nav.datasets',
    '/ml': 'nav.training',
    '/notebooks': 'nav.workshop',
    '/object-explorer': 'nav.objectExplorer',
    '/object-monitors': 'nav.notifications',
    '/ontology': 'nav.ontology',
    '/ontology-manager': 'nav.ontologyManager',
    '/pipelines': 'nav.pipelineBuilder',
    '/projects': 'nav.projects',
    '/queries': 'nav.queries',
    '/reports': 'nav.files',
    '/search': 'nav.search',
    '/settings': 'nav.account',
    '/control-panel': 'common.controlPanel'
  };

  const pageTitle = $derived.by(() => {
    const pathname = $page.url.pathname;
    const sorted = Object.keys(titleMap).sort((a, b) => b.length - a.length);
    const match = sorted.find((key) => pathname === key || pathname.startsWith(`${key}/`));
    return match ? t(titleMap[match]) : t('topbar.pageDefault');
  });

  function handleLogout() {
    auth.logout();
    goto('/auth/login');
  }

  function closeCreateMenu() {
    createMenuOpen = false;
    createMenuSearch = '';
    createMenuCategory = 'all';
  }

  function toggleCreateMenu() {
    createMenuOpen = !createMenuOpen;
    if (!createMenuOpen) {
      createMenuSearch = '';
      createMenuCategory = 'all';
    }
  }

  function openPipelineBuilder() {
    closeCreateMenu();
    goto('/pipelines/new');
  }

  function handleWindowClick(event: MouseEvent) {
    if (!createMenuOpen || !createMenuRef) return;
    if (createMenuRef.contains(event.target as Node)) return;
    closeCreateMenu();
  }

  function handleWindowKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && createMenuOpen) {
      closeCreateMenu();
    }
  }
</script>

<svelte:window onclick={handleWindowClick} onkeydown={handleWindowKeydown} />

<header class="of-topbar">
  <div class="of-topbar__crumbs">
    <div class="of-topbar__trail">
      <span class="of-topbar__crumb-icon">
        <Glyph name="folder" size={13} />
      </span>
      <span class="of-topbar__crumb">{t('topbar.workspace')}</span>
    </div>
    <span aria-hidden="true">
      <Glyph name="chevron-right" size={11} />
    </span>
    <div class="of-topbar__trail of-topbar__trail--current">
      <span class="of-topbar__crumb">{pageTitle}</span>
    </div>
  </div>

  <div class="of-topbar__actions">
    <div class="of-topbar__create-menu" bind:this={createMenuRef}>
      <button type="button" class="of-topbar__action of-topbar__action--primary" aria-haspopup="dialog" aria-expanded={createMenuOpen} onclick={toggleCreateMenu}>
        <Glyph name="plus" size={14} />
        <span>{t('topbar.createNew')}</span>
        <Glyph name="chevron-down" size={12} />
      </button>

      {#if createMenuOpen}
        <div class="of-topbar__create-panel" role="dialog" aria-label={t('topbar.createNew')}>
          <label class="of-topbar__create-search">
            <span class="of-topbar__create-search-icon">
              <Glyph name="search" size={14} />
            </span>
            <input bind:value={createMenuSearch} type="search" placeholder={t('topbar.createSearchPlaceholder')} aria-label={t('topbar.createSearchPlaceholder')} />
          </label>

          <div class="of-topbar__create-body">
            <div class="of-topbar__create-categories">
              <button type="button" class="of-topbar__create-category" data-active={createMenuCategory === 'all'} onclick={() => (createMenuCategory = 'all')}>
                {t('topbar.createCategoryAll')}
              </button>
              <button type="button" class="of-topbar__create-category" data-active={createMenuCategory === 'integration'} onclick={() => (createMenuCategory = 'integration')}>
                {t('topbar.createCategoryIntegration')}
              </button>
            </div>

            <div class="of-topbar__create-results">
              {#if isPipelineBuilderVisible}
                <button type="button" class="of-topbar__create-item" onclick={openPipelineBuilder}>
                  <span class="of-topbar__create-item-icon">
                    <Glyph name="graph" size={17} />
                  </span>
                  <span class="of-topbar__create-item-copy">
                    <span class="of-topbar__create-item-title">{t('nav.pipelineBuilder')}</span>
                    <span class="of-topbar__create-item-description">{t('topbar.pipelineBuilderDescription')}</span>
                  </span>
                  <span class="of-topbar__create-item-arrow">
                    <Glyph name="chevron-right" size={14} />
                  </span>
                </button>
              {:else}
                <div class="of-topbar__create-empty">{t('topbar.createEmpty')}</div>
              {/if}
            </div>
          </div>
        </div>
      {/if}
    </div>

    <label class="of-topbar__action">
      <span>{t('topbar.userLanguage')}</span>
      <select
        class="bg-transparent text-[11px] font-semibold outline-none"
        value={$currentLocale}
        onchange={(event) => setLocale((event.currentTarget as HTMLSelectElement).value as AppLocale)}
      >
        {#each $languageOptions as locale}
          <option value={locale}>{getLocaleLabel(locale, $currentLocale)}</option>
        {/each}
      </select>
    </label>

    <button
      type="button"
      class="of-topbar__action"
      onclick={() => ontologySearch.open()}
      aria-label="Search ontology (⌘K)"
      title="Search ontology (⌘K)"
    >
      <Glyph name="search" size={14} />
      <span>Search</span>
      <kbd class="of-topbar__kbd">⌘K</kbd>
    </button>

    <a href="/apps" class="of-topbar__action">
      <Glyph name="cube" size={14} />
      <span>{t('nav.applications')}</span>
    </a>

    {#if $isAuthenticated}
      <NotificationBell />
      <div class="of-topbar__user">
        <span class="of-topbar__avatar">OF</span>
        <div class="min-w-0">
          <div class="truncate text-[12px] font-semibold text-[var(--text-strong)]">
            {$user?.name ?? t('topbar.operator')}
          </div>
          <div class="truncate text-[11px] text-[var(--text-muted)]">{t('topbar.workspaceSession')}</div>
        </div>
      </div>
      <button type="button" class="of-topbar__action" onclick={handleLogout} aria-label={t('common.logout')}>
        <Glyph name="logout" size={14} />
      </button>
    {:else}
      <a href="/auth/login" class="of-topbar__action">{t('common.login')}</a>
    {/if}
  </div>
</header>
