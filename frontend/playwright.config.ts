import { defineConfig } from '@playwright/test';
import { config } from 'dotenv';

config({ path: '.env.e2e' });

export default defineConfig({
	testDir: './e2e/tests',
	timeout: 900_000, // 15 min — VPS provisioning can take 5+ min
	expect: { timeout: 10_000 },
	fullyParallel: false,
	retries: 0,
	workers: 1,
	reporter: [['list'], ['html', { open: 'never' }]],
	use: {
		baseURL: process.env.E2E_BASE_URL || 'https://dev.tardi-467.pages.dev',
		trace: 'retain-on-failure',
		screenshot: 'only-on-failure',
		video: 'retain-on-failure',
	},
	projects: [
		{
			name: 'smoke',
			testDir: './e2e/tests/smoke',
			timeout: 30_000,
		},
		{
			name: 'journey',
			testDir: './e2e/tests/journey',
		},
	],
});
