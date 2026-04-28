import type { LayoutServerLoad } from './$types';
import { LOCALE_COOKIE_KEY } from '$lib/i18n/store';

export const load: LayoutServerLoad = async ({ cookies }) => {
  return {
    initialLocale: cookies.get(LOCALE_COOKIE_KEY) ?? null,
  };
};
