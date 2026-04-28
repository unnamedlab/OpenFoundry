<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import NotificationBell from '$components/layout/NotificationBell.svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import type { MessageKey } from '$lib/i18n/messages';
  import { auth } from '$stores/auth';
  import { createTranslator, currentLocale, getLocaleLabel, setLocale, supportedLocales, type AppLocale } from '$lib/i18n/store';

  const isAuthenticated = auth.isAuthenticated;
  const user = auth.user;
  const t = $derived.by(() => createTranslator($currentLocale));
  const languageOptions = supportedLocales;

  const titleMap: Record<string, MessageKey> = {
    '/': 'nav.home',
    '/ontology': 'nav.ontology',
    '/queries': 'nav.queries',
    '/datasets': 'nav.datasets',
    '/pipelines': 'nav.pipelines',
    '/settings': 'nav.settings',
    '/control-panel': 'common.controlPanel',
  };

  let quickSearch = $state('');

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

<header class="border-b border-[var(--border-default)] bg-white">
  <div class="flex h-[58px] items-stretch">
    <a
      href="/search"
      class="flex min-w-[178px] items-center gap-2 border-r border-[var(--border-default)] px-14 text-[15px] font-medium text-[var(--text-strong)] transition hover:bg-[var(--bg-hover)]"
    >
      <span class="of-icon-box h-7 w-7">
        <Glyph name="search" size={15} />
      </span>
      <span>{t('topbar.searchFor', { term: quickSearch || t('topbar.searchFallback') })}</span>
    </a>

    <a
      href="/ontology"
      class="flex min-w-[172px] items-center gap-2 border-r border-[var(--border-default)] px-10 text-[15px] font-medium text-[var(--text-strong)] transition hover:bg-[var(--bg-hover)]"
    >
      <span class="of-icon-box h-7 w-7 bg-[#eef3fb] text-[var(--text-muted)]">
        <Glyph name="plus" size={14} />
      </span>
      <span>{t('topbar.newExploration')}</span>
    </a>

    <div class="flex min-w-0 flex-1 items-center justify-between gap-6 px-6">
      <div class="min-w-0">
        <div class="truncate text-[15px] font-semibold text-[var(--text-strong)]">{pageTitle}</div>
        <div class="truncate text-xs text-[var(--text-muted)]">
          {t('topbar.subtitle')}
        </div>
      </div>

      <div class="flex items-center gap-3">
        <label class="of-search-shell min-w-[340px] max-w-[460px]">
          <div class="of-search-input-wrap">
            <Glyph name="search" size={18} />
            <input
              bind:value={quickSearch}
              type="text"
              class="of-search-input"
              placeholder={t('topbar.searchPlaceholder')}
            />
          </div>
        </label>

        <label class="flex items-center gap-2 rounded-xl border border-[var(--border-default)] bg-white px-3 py-2 text-[13px] text-[var(--text-strong)]">
          <span class="hidden md:inline">{t('topbar.userLanguage')}</span>
          <select
            class="bg-transparent outline-none"
            value={$currentLocale}
            onchange={(event) => setLocale((event.currentTarget as HTMLSelectElement).value as AppLocale)}
          >
            {#each $languageOptions as locale}
              <option value={locale}>{getLocaleLabel(locale, $currentLocale)}</option>
            {/each}
          </select>
        </label>

        <button type="button" class="of-btn gap-2 px-3 text-[13px]">
          <Glyph name="object" size={16} />
          <span>{t('topbar.explorations')}</span>
          <Glyph name="chevron-down" size={14} />
        </button>

        <button type="button" class="of-btn gap-2 px-3 text-[13px]">
          <Glyph name="list" size={16} />
          <span>{t('topbar.lists')}</span>
          <Glyph name="chevron-down" size={14} />
        </button>

        {#if $isAuthenticated}
          <NotificationBell />
          <div class="hidden text-right md:block">
            <div class="text-[13px] font-medium text-[var(--text-strong)]">{$user?.name ?? t('topbar.operator')}</div>
            <div class="text-[11px] text-[var(--text-muted)]">{t('topbar.workspaceSession')}</div>
          </div>
          <button type="button" class="of-btn px-3" onclick={handleLogout} aria-label={t('common.logout')}>
            <Glyph name="logout" size={16} />
          </button>
        {:else}
          <a href="/auth/login" class="of-btn">{t('common.login')}</a>
        {/if}
      </div>
    </div>
  </div>
</header>
