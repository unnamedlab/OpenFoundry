<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import NotificationBell from '$components/layout/NotificationBell.svelte';
  import Glyph from '$components/ui/Glyph.svelte';
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

  const titleMap: Record<string, MessageKey> = {
    '/': 'nav.home',
    '/apps': 'nav.applications',
    '/dashboards': 'nav.recent',
    '/datasets': 'nav.datasets',
    '/training': 'nav.training',
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
</script>

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
