<script lang="ts">
  import { getBootstrapStatus, register } from '$api/auth';
  import { goto } from '$app/navigation';
  import { createTranslator, currentLocale } from '$lib/i18n/store';
  import { onMount } from 'svelte';

  let name = $state('');
  let email = $state('');
  let password = $state('');
  let error = $state('');
  let loading = $state(false);
  let requiresInitialAdmin = $state(false);
  const t = $derived.by(() => createTranslator($currentLocale));

  onMount(async () => {
    try {
      const status = await getBootstrapStatus();
      requiresInitialAdmin = status.requires_initial_admin;
    } catch {
      requiresInitialAdmin = false;
    }
  });

  async function handleSubmit(e: Event) {
    e.preventDefault();
    error = '';
    loading = true;

    try {
      await register({ name, email, password });
      goto('/auth/login?registered=true');
    } catch (err: any) {
      error = err.message ?? t('auth.register.failed');
    } finally {
      loading = false;
    }
  }
</script>

<svelte:head>
  <title>{t('auth.register.title')}</title>
</svelte:head>

<div class="w-full max-w-sm">
  <div class="text-center mb-8">
    <span class="text-4xl text-indigo-500">◆</span>
    <h1 class="text-2xl font-bold mt-2">{t('auth.register.heading')}</h1>
  </div>

  <form onsubmit={handleSubmit} class="space-y-4">
    {#if requiresInitialAdmin}
      <div class="p-3 text-sm text-indigo-700 bg-indigo-50 dark:bg-indigo-950 dark:text-indigo-200 rounded-lg">
        {t('auth.register.bootstrapNotice')}
      </div>
    {/if}

    {#if error}
      <div class="p-3 text-sm text-red-700 bg-red-50 dark:bg-red-950 dark:text-red-300 rounded-lg">
        {error}
      </div>
    {/if}

    <div>
      <label for="name" class="block text-sm font-medium mb-1">{t('auth.register.name')}</label>
      <input
        id="name"
        type="text"
        bind:value={name}
        required
        class="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 rounded-lg
               bg-white dark:bg-gray-800 focus:outline-none focus:ring-2 focus:ring-indigo-500"
        placeholder={t('auth.register.namePlaceholder')}
      />
    </div>

    <div>
      <label for="email" class="block text-sm font-medium mb-1">{t('auth.register.email')}</label>
      <input
        id="email"
        type="email"
        bind:value={email}
        required
        class="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 rounded-lg
               bg-white dark:bg-gray-800 focus:outline-none focus:ring-2 focus:ring-indigo-500"
        placeholder={t('auth.login.emailPlaceholder')}
      />
    </div>

    <div>
      <label for="password" class="block text-sm font-medium mb-1">{t('auth.register.password')}</label>
      <input
        id="password"
        type="password"
        bind:value={password}
        required
        minlength="8"
        class="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 rounded-lg
               bg-white dark:bg-gray-800 focus:outline-none focus:ring-2 focus:ring-indigo-500"
        placeholder={t('auth.register.passwordPlaceholder')}
      />
    </div>

    <button
      type="submit"
      disabled={loading}
      class="w-full py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-500
             disabled:opacity-50 transition-colors font-medium"
    >
      {loading ? t('auth.register.creating') : t('auth.register.create')}
    </button>
  </form>

  <p class="text-center text-sm text-gray-500 mt-6">
    {t('auth.register.haveAccount')}
    <a href="/auth/login" class="text-indigo-600 hover:text-indigo-500">{t('auth.register.signIn')}</a>
  </p>
</div>
