import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit()],
	build: {
		// The Monaco editor core remains a single lazy-loaded chunk after trimming optional language packs.
		chunkSizeWarningLimit: 2600,
		rolldownOptions: {
			checks: {
				// Rolldown's plugin timing report is noisy here and doesn't indicate a correctness problem.
				pluginTimings: false
			}
		}
	},
	server: {
		host: '0.0.0.0',
		port: 5173,
		proxy: {
			'/api': {
				target: 'http://127.0.0.1:8080',
				changeOrigin: true,
				ws: true
			}
		}
	}
});
