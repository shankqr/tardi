import { test, expect } from '@playwright/test';
import { loginWithCredentials } from '../../fixtures/auth';
import {
	API_URL,
	createInstanceViaAPI,
	deleteExistingInstances,
	ensureInstancePage,
	getIdToken,
	waitForInstanceActive,
	waitForSyncComplete,
} from '../../helpers/journey-helpers';

const PROD_EMAIL = process.env.E2E_PROD_EMAIL || '';
const PROD_PASSWORD = process.env.E2E_PROD_PASSWORD || '';

test.skip(!PROD_EMAIL || !PROD_PASSWORD, 'E2E_PROD_EMAIL / E2E_PROD_PASSWORD not set');

test.afterAll(async () => {
	if (!PROD_EMAIL || !PROD_PASSWORD) return;
	console.log('[Hermes Prod E2E] cleanup - deleting prod Hermes test instances...');
	await deleteExistingInstances(PROD_EMAIL, PROD_PASSWORD);
});

test('Hermes prod E2E: deploy Docker Hermes, configure, dashboard, chat', async ({ page }) => {
	let instanceId = '';

	await test.step('Clean persistent prod test account', async () => {
		await deleteExistingInstances(PROD_EMAIL, PROD_PASSWORD);
	});

	await test.step('Login to prod dashboard', async () => {
		await loginWithCredentials(page, PROD_EMAIL, PROD_PASSWORD);
		await expect(
			page.getByRole('heading', { name: 'Deploy your agent' })
				.or(page.getByText('Deploy your agent'))
		).toBeVisible({ timeout: 30_000 });
	});

	await test.step('Create Hermes instance through prod API', async () => {
		const agentName = `prod-hermes-e2e-${Date.now()}`;
		instanceId = await createInstanceViaAPI(PROD_EMAIL, PROD_PASSWORD, agentName, 'hermes');
		console.log(`[Hermes Prod E2E] Instance ID: ${instanceId}`);

		await waitForInstanceActive(PROD_EMAIL, PROD_PASSWORD, instanceId, 900_000);
		await ensureInstancePage(page, instanceId);

		const bodyText = await page.textContent('body');
		expect(bodyText).toContain('Hermes');
		console.log('[Hermes Prod E2E] Hermes instance is active');
	});

	await test.step('Set OpenRouter API key and sync config', async () => {
		const apiKey = process.env.E2E_OPENROUTER_API_KEY;
		test.skip(!apiKey, 'E2E_OPENROUTER_API_KEY required for Hermes chat');

		await page.locator('#openrouter-key').fill(apiKey);
		await page.getByRole('button', { name: 'Save' }).first().click();

		await waitForSyncComplete(PROD_EMAIL, PROD_PASSWORD, instanceId, 240_000);
		await waitForInstanceActive(PROD_EMAIL, PROD_PASSWORD, instanceId, 300_000);
		await ensureInstancePage(page, instanceId);
		console.log('[Hermes Prod E2E] API key synced');
	});

	await test.step('Open Hermes dashboard through token hash', async () => {
		const idToken = await getIdToken(PROD_EMAIL, PROD_PASSWORD);
		const dashTokenRes = await fetch(`${API_URL}/api/instances/${instanceId}/dashboard-token`, {
			method: 'POST',
			headers: { Authorization: `Bearer ${idToken}` },
		});
		expect(dashTokenRes.status).toBe(200);
		const { token: dashboardToken } = await dashTokenRes.json();
		expect(typeof dashboardToken).toBe('string');

		const dashStateRes = await fetch(`${API_URL}/api/dashboard/state`, {
			headers: { Authorization: `Bearer ${idToken}` },
		});
		expect(dashStateRes.status).toBe(200);
		const dashState = await dashStateRes.json();
		const instance = dashState.instances?.find((i: { id: string }) => i.id === instanceId);
		expect(instance?.framework).toBe('hermes');
		expect(instance?.dashboard_url).toBeTruthy();
		expect(instance?.target_openclaw_version).toBe('latest');

		const dashboardUrl = instance.dashboard_url as string;
		const response = await page.goto(`${dashboardUrl}/#token=${dashboardToken}`, {
			timeout: 60_000,
			waitUntil: 'domcontentloaded',
		});
		expect(response?.ok()).toBeTruthy();

		await expect(page.locator('body')).not.toContainText('Missing token', { timeout: 10_000 });
		await expect(page.locator('body')).not.toContainText('Internal Error', { timeout: 10_000 });
		console.log('[Hermes Prod E2E] Dashboard opened through shim');
	});

	await test.step('Send chat request to Hermes API', async () => {
		const idToken = await getIdToken(PROD_EMAIL, PROD_PASSWORD);
		const dashTokenRes = await fetch(`${API_URL}/api/instances/${instanceId}/dashboard-token`, {
			method: 'POST',
			headers: { Authorization: `Bearer ${idToken}` },
		});
		const { token: dashboardToken } = await dashTokenRes.json();

		const dashStateRes = await fetch(`${API_URL}/api/dashboard/state`, {
			headers: { Authorization: `Bearer ${idToken}` },
		});
		const dashState = await dashStateRes.json();
		const instance = dashState.instances?.find((i: { id: string }) => i.id === instanceId);
		const dashboardUrl = instance?.dashboard_url as string | undefined;
		expect(dashboardUrl).toBeTruthy();

		let lastError = '';
		for (let attempt = 1; attempt <= 4; attempt++) {
			const chatRes = await fetch(`${dashboardUrl}/v1/chat/completions`, {
				method: 'POST',
				headers: {
					Authorization: `Bearer ${dashboardToken}`,
					'Content-Type': 'application/json',
				},
				body: JSON.stringify({
					model: 'hermes-agent',
					messages: [{ role: 'user', content: 'Reply with only the word OK.' }],
					stream: false,
				}),
			});

			const text = await chatRes.text();
			if (chatRes.ok) {
				const data = JSON.parse(text);
				const reply = data.choices?.[0]?.message?.content;
				expect(String(reply || '').trim().length).toBeGreaterThan(0);
				console.log(`[Hermes Prod E2E] Chat responded: ${String(reply).slice(0, 80)}`);
				return;
			}

			lastError = `HTTP ${chatRes.status}: ${text.slice(0, 500)}`;
			console.log(`[Hermes Prod E2E] Chat attempt ${attempt}/4 failed: ${lastError}`);
			await page.waitForTimeout(20_000);
		}

		throw new Error(`Hermes chat did not succeed: ${lastError}`);
	});
});
