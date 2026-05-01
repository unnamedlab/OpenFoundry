<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { get } from 'svelte/store';
  import { createTranslator, currentLocale } from '$lib/i18n/store';
  import ConfirmDialog from '$components/workspace/ConfirmDialog.svelte';
  import { dashboards } from '$lib/stores/dashboards';
  import {
    formatDashboardTimestamp,
    serializeDashboardSnapshot,
    type DashboardDefinition,
  } from '$lib/utils/dashboards';

  const dashboardItems = dashboards;
  const t = $derived.by(() => createTranslator($currentLocale));

  let feedback = $state('');
  let confirmState = $state<{ id: string } | null>(null);

  onMount(() => {
    dashboards.restore();
  });

  async function createDashboard() {
    const dashboard = dashboards.create(`New Dashboard ${get(dashboardItems).length + 1}`);
    await goto(`/dashboards/${dashboard.id}`);
  }

  async function duplicateDashboard(id: string) {
    const copy = dashboards.duplicate(id);
    if (!copy) {
      return;
    }

    await goto(`/dashboards/${copy.id}`);
  }

  function deleteDashboard(id: string) {
    confirmState = { id };
  }

  function confirmDelete() {
    if (!confirmState) return;
    dashboards.remove(confirmState.id);
    confirmState = null;
  }

  async function shareDashboard(dashboard: DashboardDefinition) {
    if (typeof window === 'undefined') {
      return;
    }

    const snapshot = serializeDashboardSnapshot(dashboard);
    const shareUrl = `${window.location.origin}/dashboards/${dashboard.id}?snapshot=${snapshot}`;

    try {
      await navigator.clipboard.writeText(shareUrl);
      feedback = t('pages.dashboards.copied');
    } catch {
      feedback = shareUrl;
    }
  }
</script>

<svelte:head>
  <title>{t('pages.dashboards.title')}</title>
</svelte:head>

<div class="mx-auto max-w-7xl space-y-6">
  <section class="rounded-[2rem] border border-slate-200 bg-[linear-gradient(135deg,_rgba(15,118,110,0.12),_rgba(2,132,199,0.12)_42%,_rgba(255,255,255,0.9)_100%)] p-8 shadow-sm dark:border-slate-800 dark:bg-[linear-gradient(135deg,_rgba(15,118,110,0.22),_rgba(2,132,199,0.16)_42%,_rgba(15,23,42,0.92)_100%)]">
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div class="max-w-3xl space-y-3">
        <div class="text-[11px] font-semibold uppercase tracking-[0.24em] text-slate-500">{t('pages.dashboards.badge')}</div>
        <h1 class="text-4xl font-semibold tracking-tight text-slate-950 dark:text-slate-50">{t('pages.dashboards.heading')}</h1>
        <p class="text-base text-slate-600 dark:text-slate-300">
          {t('pages.dashboards.description')}
        </p>
      </div>

      <button
        class="rounded-2xl bg-slate-900 px-5 py-3 text-sm font-medium text-white hover:bg-slate-800 dark:bg-slate-100 dark:text-slate-950 dark:hover:bg-white"
        onclick={createDashboard}
      >
        {t('pages.dashboards.create')}
      </button>
    </div>

    {#if feedback}
      <div class="mt-4 rounded-2xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-300">
        {feedback}
      </div>
    {/if}
  </section>

  <section class="grid gap-4 lg:grid-cols-2 xl:grid-cols-3">
    {#each $dashboardItems as dashboard (dashboard.id)}
      <article class="group rounded-[1.75rem] border border-slate-200 bg-white p-5 shadow-sm transition-transform hover:-translate-y-0.5 hover:border-slate-300 dark:border-slate-800 dark:bg-slate-950 dark:hover:border-slate-700">
        <div class="flex items-start justify-between gap-3">
          <div>
            <div class="text-[11px] font-semibold uppercase tracking-[0.24em] text-slate-400">{t('pages.dashboards.widgets', { count: dashboard.widgets.length })}</div>
            <h2 class="mt-2 text-2xl font-semibold text-slate-950 dark:text-slate-50">{dashboard.name}</h2>
          </div>

          <div class="rounded-full bg-slate-100 px-3 py-1 text-xs font-medium text-slate-600 dark:bg-slate-800 dark:text-slate-300">
            {t('pages.dashboards.updated', { value: formatDashboardTimestamp(dashboard.updatedAt) })}
          </div>
        </div>

        <p class="mt-3 min-h-[3rem] text-sm leading-6 text-slate-600 dark:text-slate-300">{dashboard.description}</p>

        <div class="mt-5 flex flex-wrap gap-2 text-xs text-slate-500 dark:text-slate-400">
          {#each dashboard.widgets as widget}
            <span class="rounded-full border border-slate-200 px-2.5 py-1 dark:border-slate-700">{widget.type}</span>
          {/each}
        </div>

        <div class="mt-6 flex flex-wrap gap-2">
          <a
            href={`/dashboards/${dashboard.id}`}
            class="rounded-xl bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 dark:bg-slate-100 dark:text-slate-950 dark:hover:bg-white"
          >
            {t('pages.dashboards.open')}
          </a>
          <button class="rounded-xl border border-slate-300 px-4 py-2 text-sm font-medium dark:border-slate-700" onclick={() => duplicateDashboard(dashboard.id)}>{t('pages.dashboards.duplicate')}</button>
          <button class="rounded-xl border border-slate-300 px-4 py-2 text-sm font-medium dark:border-slate-700" onclick={() => shareDashboard(dashboard)}>{t('pages.dashboards.share')}</button>
          <button class="rounded-xl border border-rose-300 px-4 py-2 text-sm font-medium text-rose-700 dark:border-rose-900 dark:text-rose-300" onclick={() => deleteDashboard(dashboard.id)}>{t('pages.dashboards.delete')}</button>
        </div>
      </article>
    {/each}
  </section>
</div>

<ConfirmDialog
  open={confirmState !== null}
  title={t('pages.dashboards.delete')}
  message={t('pages.dashboards.confirmDelete')}
  confirmLabel={t('pages.dashboards.delete')}
  danger
  onConfirm={confirmDelete}
  onCancel={() => (confirmState = null)}
/>
