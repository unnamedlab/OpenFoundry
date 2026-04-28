<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { auth } from '$stores/auth';
	import { createTranslator, currentLocale } from '$lib/i18n/store';

	let error = $state('');
	const t = $derived.by(() => createTranslator($currentLocale));

	onMount(async () => {
		const params = new URLSearchParams(window.location.search);
		const code = params.get('code');
		const state = params.get('state');
		const samlResponse = params.get('SAMLResponse');
		const relayState = params.get('RelayState');

		if ((!code || !state) && (!samlResponse || !relayState)) {
			error = t('auth.callback.missing');
			return;
		}

		try {
			const result = await auth.handleSsoCallback({
				code: code ?? undefined,
				state: state ?? undefined,
				saml_response: samlResponse ?? undefined,
				relay_state: relayState ?? undefined,
			});
			goto(result.status === 'mfa_required' ? '/auth/mfa' : '/');
		} catch (err: any) {
			error = err.message ?? t('auth.callback.failed');
		}
	});
</script>

<svelte:head>
	<title>{t('auth.callback.title')}</title>
</svelte:head>

<div class="w-full max-w-md rounded-3xl border border-gray-200 bg-white p-8 text-center shadow-sm dark:border-gray-800 dark:bg-gray-900">
	<div class="text-xs uppercase tracking-[0.25em] text-gray-400">{t('auth.callback.badge')}</div>
	<h1 class="mt-2 text-2xl font-bold">{t('auth.callback.heading')}</h1>
	{#if error}
		<p class="mt-4 rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">{error}</p>
		<a href="/auth/login" class="mt-6 inline-block text-sm text-indigo-600 hover:text-indigo-500">{t('auth.callback.back')}</a>
	{:else}
		<p class="mt-4 text-sm text-gray-500">{t('auth.callback.subtitle')}</p>
	{/if}
</div>
