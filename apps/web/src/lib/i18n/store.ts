import { get, writable } from 'svelte/store';
import { messages, type MessageKey } from './messages';

export const SUPPORTED_LOCALES = ['en', 'es'] as const;
export type AppLocale = (typeof SUPPORTED_LOCALES)[number];

export interface PlatformLocaleSettings {
	supported_locales: AppLocale[];
	default_locale: AppLocale;
}

export const DEFAULT_LOCALE: AppLocale = 'en';
export const LOCALE_STORAGE_KEY = 'of_locale';
export const LOCALE_COOKIE_KEY = 'of_locale';
export const PLATFORM_LOCALE_SETTINGS_KEY = 'of_platform_locale_settings';

export const currentLocale = writable<AppLocale>(DEFAULT_LOCALE);
export const supportedLocales = writable<AppLocale[]>([...SUPPORTED_LOCALES]);
export const platformDefaultLocale = writable<AppLocale>(DEFAULT_LOCALE);

function isSupportedLocale(value: unknown): value is AppLocale {
	return typeof value === 'string' && SUPPORTED_LOCALES.includes(value as AppLocale);
}

export function normalizeLocale(value: unknown, fallback: AppLocale = DEFAULT_LOCALE): AppLocale {
	if (typeof value !== 'string') return fallback;
	const normalized = value.toLowerCase().trim();
	const base = normalized.split(/[-_]/)[0];
	return isSupportedLocale(base) ? base : fallback;
}

function applyDocumentLocale(locale: AppLocale) {
	if (typeof document === 'undefined') return;
	document.documentElement.lang = locale;
}

function writeLocaleCookie(locale: AppLocale) {
	if (typeof document === 'undefined') return;
	document.cookie = `${LOCALE_COOKIE_KEY}=${locale}; Path=/; Max-Age=31536000; SameSite=Lax`;
}

function sanitizePlatformLocaleSettings(
	value?: Partial<PlatformLocaleSettings> | null,
): PlatformLocaleSettings {
	const supported = Array.from(
		new Set((value?.supported_locales ?? [...SUPPORTED_LOCALES]).map((locale) => normalizeLocale(locale))),
	).filter(isSupportedLocale);
	const nextSupported = supported.length > 0 ? supported : [...SUPPORTED_LOCALES];
	const defaultLocale = normalizeLocale(value?.default_locale, nextSupported[0] ?? DEFAULT_LOCALE);
	return {
		supported_locales: nextSupported,
		default_locale: nextSupported.includes(defaultLocale) ? defaultLocale : (nextSupported[0] ?? DEFAULT_LOCALE),
	};
}

function persistPlatformLocaleSettings(value: PlatformLocaleSettings) {
	if (typeof localStorage === 'undefined') return;
	try {
		localStorage.setItem(PLATFORM_LOCALE_SETTINGS_KEY, JSON.stringify(value));
	} catch {
		// Ignore storage persistence failures and continue with in-memory locale state.
	}
}

function loadPersistedPlatformLocaleSettings(): PlatformLocaleSettings | null {
	if (typeof localStorage === 'undefined') return null;
	let raw: string | null = null;
	try {
		raw = localStorage.getItem(PLATFORM_LOCALE_SETTINGS_KEY);
	} catch {
		return null;
	}
	if (!raw) return null;

	try {
		return sanitizePlatformLocaleSettings(JSON.parse(raw) as PlatformLocaleSettings);
	} catch {
		return null;
	}
}

function setLocaleValue(locale: AppLocale, persist = true) {
	currentLocale.set(locale);
	applyDocumentLocale(locale);
	if (persist && typeof localStorage !== 'undefined') {
		try {
			localStorage.setItem(LOCALE_STORAGE_KEY, locale);
		} catch {
			// Ignore storage persistence failures and continue with the active locale.
		}
	}
	if (persist) {
		writeLocaleCookie(locale);
	}
}

function resolveBootstrapLocale(initialLocale?: string | null) {
	const persistedPlatform = loadPersistedPlatformLocaleSettings();
	if (persistedPlatform) {
		supportedLocales.set(persistedPlatform.supported_locales);
		platformDefaultLocale.set(persistedPlatform.default_locale);
	}

	const platformFallback = persistedPlatform?.default_locale ?? DEFAULT_LOCALE;
	const browserLocale =
		typeof navigator !== 'undefined'
			? normalizeLocale(navigator.language, platformFallback)
			: platformFallback;
	let savedLocale: string | null = null;
	if (typeof localStorage !== 'undefined') {
		try {
			savedLocale = localStorage.getItem(LOCALE_STORAGE_KEY);
		} catch {
			savedLocale = null;
		}
	}

	return normalizeLocale(savedLocale ?? initialLocale ?? browserLocale, platformFallback);
}

export function initializeLocale(initialLocale?: string | null) {
	setLocaleValue(normalizeLocale(initialLocale, get(currentLocale)), false);
}

export function restoreLocale(initialLocale?: string | null) {
	setLocaleValue(resolveBootstrapLocale(initialLocale));
}

export function setLocale(locale: AppLocale) {
	setLocaleValue(normalizeLocale(locale, get(platformDefaultLocale)));
}

export function setPlatformLocaleSettings(
	value?: Partial<PlatformLocaleSettings> | null,
	options: { persist?: boolean } = {},
) {
	const nextSettings = sanitizePlatformLocaleSettings(value);
	supportedLocales.set(nextSettings.supported_locales);
	platformDefaultLocale.set(nextSettings.default_locale);
	if (options.persist ?? true) {
		persistPlatformLocaleSettings(nextSettings);
	}

	const activeLocale = get(currentLocale);
	if (!nextSettings.supported_locales.includes(activeLocale)) {
		setLocaleValue(nextSettings.default_locale, options.persist ?? true);
	}
}

export function getUserLocalePreference(attributes: Record<string, unknown> | null | undefined) {
	const raw = attributes?.locale;
	if (typeof raw !== 'string') return null;
	const normalized = normalizeLocale(raw, DEFAULT_LOCALE);
	return raw.toLowerCase().startsWith(normalized) ? normalized : null;
}

export function applyUserLocalePreference(attributes: Record<string, unknown> | null | undefined) {
	const preferredLocale = getUserLocalePreference(attributes);
	if (preferredLocale) {
		setLocaleValue(preferredLocale);
	}
}

export function translate(
	key: MessageKey,
	params?: Record<string, string | number>,
	locale = get(currentLocale),
) {
	const template = (messages[locale][key] ?? messages[DEFAULT_LOCALE][key] ?? key) as string;
	if (!params) return template;
	return Object.entries(params).reduce(
		(result, [name, value]) => result.replaceAll(`{${name}}`, String(value)),
		template,
	);
}

export function createTranslator(locale: AppLocale) {
	return (key: MessageKey, params?: Record<string, string | number>) => translate(key, params, locale);
}

export function getLocaleLabel(locale: AppLocale, displayLocale: AppLocale = get(currentLocale)) {
	return (displayLocale === 'es'
		? locale === 'en'
			? messages.es['locale.english']
			: messages.es['locale.spanish']
		: locale === 'en'
			? messages.en['locale.english']
			: messages.en['locale.spanish']) as string;
}
