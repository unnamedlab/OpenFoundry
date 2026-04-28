import { get } from 'svelte/store';
import { beforeEach, describe, expect, it } from 'vitest';
import {
	LOCALE_STORAGE_KEY,
	PLATFORM_LOCALE_SETTINGS_KEY,
	currentLocale,
	initializeLocale,
	normalizeLocale,
	restoreLocale,
	setPlatformLocaleSettings,
	translate,
} from './store';

describe('i18n store helpers', () => {
	beforeEach(() => {
		localStorage.clear();
		document.cookie = 'of_locale=; Max-Age=0; path=/';
		initializeLocale('en');
		setPlatformLocaleSettings({ supported_locales: ['en', 'es'], default_locale: 'en' });
	});

	it('normalizes locale values to supported languages', () => {
		expect(normalizeLocale('es-ES')).toBe('es');
		expect(normalizeLocale('EN_us')).toBe('en');
		expect(normalizeLocale('fr', 'es')).toBe('es');
	});

	it('translates keys with interpolation', () => {
		expect(translate('topbar.searchFor', { term: 'assets' }, 'es')).toBe('Buscar "assets"');
	});

	it('restores persisted user locale before platform defaults', () => {
		localStorage.setItem(PLATFORM_LOCALE_SETTINGS_KEY, JSON.stringify({
			supported_locales: ['en', 'es'],
			default_locale: 'es',
		}));
		localStorage.setItem(LOCALE_STORAGE_KEY, 'en');

		restoreLocale();

		expect(get(currentLocale)).toBe('en');
	});

	it('falls back to the platform default when no user locale is stored', () => {
		localStorage.setItem(PLATFORM_LOCALE_SETTINGS_KEY, JSON.stringify({
			supported_locales: ['en', 'es'],
			default_locale: 'es',
		}));

		restoreLocale();

		expect(get(currentLocale)).toBe('es');
	});
});
