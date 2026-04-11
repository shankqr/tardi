import { defineConfig } from '@playwright/test';
import { config } from 'dotenv';

const envFile = process.env.E2E_ENV === 'prod' ? '.env.e2e.prod' : '.env.e2e';
config({ path: envFile });

export default defineConfig({
	testDir: './e2e/tests',
	timeout: 900_000, // 15 min — VPS provisioning can take 5+ min
	expect: { timeout: 10_000 },
	fullyParallel: false,
	retries: 0,
	workers: 1,
	reporter: [['list'], ['html', { open: 'never' }]],
	globalTeardown: './e2e/global-teardown.ts',
	use: {
		baseURL: process.env.E2E_BASE_URL || 'https://dev.tardi-467.pages.dev',
		trace: 'retain-on-failure',
		screenshot: 'only-on-failure',
		video: 'retain-on-failure',
	},
	projects: [
		// ── Pre-account: no setup needed ──
		{
			name: 'smoke',
			testDir: './e2e/tests/smoke',
			timeout: 30_000,
		},
		{
			name: 'auth',
			testDir: './e2e/tests/auth',
			timeout: 60_000,
		},
		// ── Account setup: creates user, subscribes, deploys instance ──
		{
			name: 'setup',
			testDir: './e2e/tests/setup',
			testMatch: 'account-setup.spec.ts',
			timeout: 900_000, // 15 min — provisioning takes 5-10 min
		},
		// ── Post-account: requires setup to have run ──
		{
			name: 'dashboard',
			testDir: './e2e/tests/dashboard',
			timeout: 300_000, // 5 min — snapshots + health checks can be slow
			dependencies: ['setup'],
		},
		// ── Journey tests: create their own accounts ──
		{
			name: 'journey',
			testDir: './e2e/tests/journey',
			testMatch: 'full-flow.spec.ts',
		},
		{
			name: 'hermes',
			testDir: './e2e/tests/journey',
			testMatch: 'hermes-flow.spec.ts',
		},
		{
			name: 'prod-e2e',
			testDir: './e2e/tests/prod',
			timeout: 1_500_000, // 25 min — provisioning ~7 min + multiple config syncs ~2 min each
		},
	],
});
