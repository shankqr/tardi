import { readFileSync } from 'node:fs';
import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { sentryVitePlugin } from '@sentry/vite-plugin';
import { parse } from 'smol-toml';
import { defineConfig } from 'vite';

// Load VITE_* vars from wrangler.toml so we have a single source of truth.
// Reads from "production" or "preview" env section based on WRANGLER_ENV.
function loadWranglerEnv(): Record<string, string> {
	const toml = readFileSync('wrangler.toml', 'utf-8');
	const config = parse(toml) as Record<string, unknown>;
	const envName = process.env.WRANGLER_ENV === 'production' ? 'production' : 'preview';
	const vars = (config.env as Record<string, Record<string, unknown>>)?.[envName]?.vars as
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
