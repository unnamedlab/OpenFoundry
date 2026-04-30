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
			// MVP A1 dev proxy: bypass the edge-gateway and hit the two
			// backends directly. Egress policies live in network-boundary-service;
			// the rest of /api/v1/data-connection lives in connector-management-service.
			'/api/v1/data-connection/egress-policies': {
				target: 'http://127.0.0.1:50119',
				changeOrigin: true
			},
			'/api/v1/data-connection': {
				target: 'http://127.0.0.1:50088',
				changeOrigin: true
			},
			// MVP dev-auth shim lives in connector-management-service today
			// (OPENFOUNDRY_DEV_AUTH=1). Routes /api/v1/auth/* + /api/v1/users/me.
			'/api/v1/auth': {
				target: 'http://127.0.0.1:50088',
				changeOrigin: true
			},
			'/api/v1/users/me': {
				target: 'http://127.0.0.1:50088',
				changeOrigin: true
			},
			'/api': {
				target: 'http://127.0.0.1:8080',
				changeOrigin: true,
				ws: true
			}
		}
	}
});
