import { readFileSync } from 'node:fs';
import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { sentryVitePlugin } from '@sentry/vite-plugin';
import { parse } from 'smol-toml';
import { defineConfig } from 'vite';

// Load VITE_* vars from wrangler.toml preview env so we have a single source of truth.
function loadWranglerEnv(): Record<string, string> {
	const toml = readFileSync('wrangler.toml', 'utf-8');
	const config = parse(toml) as Record<string, unknown>;
	const vars = (config.env as Record<string, Record<string, unknown>>)?.preview?.vars as
		| Record<string, string>
		| undefined;
	if (!vars) return {};

	const env: Record<string, string> = {};
	for (const [key, value] of Object.entries(vars)) {
		if (key.startsWith('VITE_')) {
			env[key] = value;
		}
	}
	return env;
}

export default defineConfig({
	build: {
		sourcemap: true
	},
	plugins: [
		tailwindcss(),
		sveltekit(),
		sentryVitePlugin({
			org: process.env.SENTRY_ORG,
			project: process.env.SENTRY_PROJECT,
			authToken: process.env.SENTRY_AUTH_TOKEN,
			release: {
				name: process.env.VITE_SENTRY_RELEASE
			},
			sourcemaps: {
				filesToDeleteAfterUpload: ['**.map']
			},
			disable: !process.env.SENTRY_AUTH_TOKEN
		})
	],
	define: Object.fromEntries(
		Object.entries(loadWranglerEnv()).map(([k, v]) => [`import.meta.env.${k}`, JSON.stringify(v)])
	)
});
