import type { Config } from 'tailwindcss';

const config: Config = {
  content: ['./src/**/*.{html,js,svelte,ts}', './tests/**/*.{js,ts}'],
  theme: {
    extend: {},
  },
  plugins: [],
};

export default config;
