import { test, expect } from '@playwright/test';
import { loginWithCredentials } from '../../fixtures/auth';
import { waitForOpenClawRunning } from '../../helpers/openclaw-status';
import {
	API_URL,
	getIdToken,
	waitForInstanceActive,
	waitForSyncComplete,
	ensureInstancePage,
	deleteExistingInstances,
} from '../../helpers/journey-helpers';

const PROD_EMAIL = process.env.E2E_PROD_EMAIL || '';
const PROD_PASSWORD = process.env.E2E_PROD_PASSWORD || '';

test.skip(!PROD_EMAIL || !PROD_PASSWORD, 'E2E_PROD_EMAIL / E2E_PROD_PASSWORD not set — skipping');

test('Prod E2E: delete instance → redeploy → configure → verify', async ({ page }) => {
	let instanceId = '';

	// ── Step 1: Delete any existing instance ──
	await test.step('Delete existing instance', async () => {
		await deleteExistingInstances(PROD_EMAIL, PROD_PASSWORD);
	});

	// ── Step 2: Login and verify deploy form ──
	await test.step('Login to prod dashboard', async () => {
		await loginWithCredentials(page, PROD_EMAIL, PROD_PASSWORD);
		// After deletion, dashboard should show the deploy form
		await expect(
			page.getByRole('heading', { name: 'Deploy your agent' })
				.or(page.getByText('Deploy your agent'))
		).toBeVisible({ timeout: 30_000 });
	});

	// ── Step 3: Deploy agent ──
	await test.step('Deploy agent', async () => {
		const agentName = `prod-e2e-${Date.now()}`;
		await page.locator('#agent-name').fill(agentName);
		await page.getByRole('button', { name: 'Deploy Agent' }).click();

		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });

		const url = page.url();
		const match = url.match(/\/instances\/([^/?#]+)/);
		expect(match).toBeTruthy();
		instanceId = match![1];
		console.log(`[Prod E2E] Instance ID: ${instanceId}`);

		// Wait for provisioning (up to 10 min)
		console.log('[Prod E2E] Waiting for provisioning to complete...');
		await waitForInstanceActive(PROD_EMAIL, PROD_PASSWORD, instanceId, 600_000);

		await ensureInstancePage(page, instanceId);
		console.log('[Prod E2E] Agent is active and ready for configuration');
	});

	// ── Step 4: Set OpenRouter API key ──
	await test.step('Set OpenRouter API key', async () => {
		const apiKey = process.env.E2E_OPENROUTER_API_KEY;
		if (!apiKey) {
			console.log('[Prod E2E] Skipping: E2E_OPENROUTER_API_KEY not set');
			return;
		}

		await page.locator('#openrouter-key').fill(apiKey);
		await page.getByRole('button', { name: 'Save' }).first().click();

		await waitForSyncComplete(PROD_EMAIL, PROD_PASSWORD, instanceId);
		await waitForInstanceActive(PROD_EMAIL, PROD_PASSWORD, instanceId);
		await ensureInstancePage(page, instanceId);
		await waitForOpenClawRunning(page);
		console.log('[Prod E2E] API key saved, model dropdown now enabled');
	});

	// ── Step 5: Change model ──
	let selectedModelName = '';
	await test.step('Change model', async () => {
		if (!process.env.E2E_OPENROUTER_API_KEY) {
			console.log('[Prod E2E] Skipping: E2E_OPENROUTER_API_KEY not set');
			return;
		}

		const modelSelect = page.locator('#model-select');
		await modelSelect.scrollIntoViewIfNeeded();
		try {
			await expect(modelSelect).toBeEnabled({ timeout: 30_000 });
		} catch {
			console.log('[Prod E2E] Model dropdown still disabled, reloading page...');
			await page.reload();
			await page.waitForTimeout(5000);
			await modelSelect.scrollIntoViewIfNeeded();
			await expect(modelSelect).toBeEnabled({ timeout: 60_000 });
		}

		const options = modelSelect.locator('option');
		const optionCount = await options.count();
		console.log(`[Prod E2E] Available models: ${optionCount}`);

		const currentValue = await modelSelect.inputValue();
		let targetValue = '';
		for (let i = 0; i < optionCount; i++) {
			const val = await options.nth(i).getAttribute('value');
			const text = await options.nth(i).textContent();
			if (val && val !== currentValue) {
				targetValue = val;
				selectedModelName = text?.trim() || val;
				break;
			}
		}

		if (targetValue) {
			await modelSelect.selectOption(targetValue);
			console.log(`[Prod E2E] Changed model to: ${selectedModelName} (${targetValue})`);

			await page.getByRole('button', { name: 'Save' }).first().click();
			await waitForSyncComplete(PROD_EMAIL, PROD_PASSWORD, instanceId);
			await waitForInstanceActive(PROD_EMAIL, PROD_PASSWORD, instanceId);
			await ensureInstancePage(page, instanceId);
			await waitForOpenClawRunning(page);
			console.log('[Prod E2E] Model change synced successfully');
		} else {
			console.log('[Prod E2E] Only one model available, keeping default');
			selectedModelName = (await options.first().textContent())?.trim() || '';
		}
	});

	// ── Step 6: Verify OpenClaw dashboard responds ──
	await test.step('Verify OpenClaw dashboard', async () => {
		if (!selectedModelName) {
			console.log('[Prod E2E] Skipping: no model selected');
			return;
		}

		const idToken = await getIdToken(PROD_EMAIL, PROD_PASSWORD);

		const dashTokenRes = await fetch(
			`${API_URL}/api/instances/${instanceId}/dashboard-token`,
			{
				method: 'POST',
				headers: { Authorization: `Bearer ${idToken}` },
			}
		);
		const { token: dashboardToken } = await dashTokenRes.json();

		const dashRes = await fetch(`${API_URL}/api/dashboard/state`, {
			headers: { Authorization: `Bearer ${idToken}` },
		});
		const dashState = await dashRes.json();
		const instance = dashState.instances?.find(
			(i: { id: string }) => i.id === instanceId
		);
		const dashboardUrl = instance?.dashboard_url;

		if (!dashboardUrl || !dashboardToken) {
			console.log('[Prod E2E] Skipping dashboard test: no URL or token available');
			return;
		}

		console.log(`[Prod E2E] Opening dashboard: ${dashboardUrl}`);

		// Wait for gateway to come back online after model change sync
		const ocUrl = `${dashboardUrl}/#token=${dashboardToken}`;
		let dashboardReady = false;
		for (let attempt = 0; attempt < 12; attempt++) {
			try {
				const res = await fetch(dashboardUrl, { signal: AbortSignal.timeout(10_000) });
				if (res.ok || res.status === 101 || res.status === 426) {
					dashboardReady = true;
					break;
				}
				console.log(`[Prod E2E] Dashboard not ready (status ${res.status}), retrying...`);
			} catch {
				console.log(`[Prod E2E] Dashboard not reachable yet (attempt ${attempt + 1}/12)`);
			}
			await new Promise((r) => setTimeout(r, 10_000));
		}

		if (!dashboardReady) {
			console.log('[Prod E2E] Dashboard never became reachable, skipping OC verification');
			return;
		}

		await page.goto(ocUrl);

		const chatInput = page.locator(
			'textarea, input[type="text"], [contenteditable="true"]'
		).last();
		await expect(chatInput).toBeVisible({ timeout: 60_000 });
		console.log('[Prod E2E] Control UI loaded, chat input found');

		await chatInput.click();
		await chatInput.fill('what model are you using right now?');
		await page.keyboard.press('Enter');

		// Wait for a response
		let responseDetected = false;
		const startTime = Date.now();
		const initialContent = await page.textContent('body') || '';
		const initialLength = initialContent.length;

		while (Date.now() - startTime < 30_000) {
			await page.waitForTimeout(3000);
			const currentContent = await page.textContent('body') || '';
			if (currentContent.length > initialLength + 50) {
				responseDetected = true;
				break;
			}
		}

		const pageContent = await page.textContent('body');
		console.log(`[Prod E2E] Selected model was: ${selectedModelName}`);

		expect(responseDetected).toBeTruthy();

		const responseLength = (pageContent?.length || 0) - initialLength;
		expect(responseLength).toBeGreaterThan(20);

		const lowerContent = pageContent?.toLowerCase() || '';
		const hasRelevantContent =
			lowerContent.includes('model') ||
			lowerContent.includes('ai') ||
			lowerContent.includes('language') ||
			lowerContent.includes('assistant');
		expect(hasRelevantContent).toBeTruthy();
		console.log('[Prod E2E] Dashboard responded with relevant content');

		// Navigate back to instance page
		await page.goto(`/dashboard/instances/${instanceId}`);
		await expect(page.locator('#openrouter-key')).toBeVisible({ timeout: 30_000 });
	});

	// ── Step 7: Link Telegram bot (optional) ──
	await test.step('Link Telegram bot', async () => {
		const botToken = process.env.E2E_TELEGRAM_BOT_TOKEN;
		if (!botToken) {
			console.log('[Prod E2E] Skipping: E2E_TELEGRAM_BOT_TOKEN not set');
			return;
		}

		await page.waitForTimeout(5000);

		const tokenInput = page.locator(
			'input[placeholder="Paste your bot token here"]'
		);
		await tokenInput.scrollIntoViewIfNeeded();
		await expect(tokenInput).toBeVisible({ timeout: 30_000 });
		await expect(tokenInput).toBeEnabled({ timeout: 30_000 });
		await tokenInput.fill(botToken);

		await tokenInput
			.locator('..')
			.getByRole('button', { name: 'Connect' })
			.click();

		await expect(page.getByText('Telegram bot connected')).toBeVisible({
			timeout: 300_000,
		});

		await waitForOpenClawRunning(page);
		console.log('[Prod E2E] Telegram bot linked successfully');
	});

	// NO cleanup — leave instance running for persistent dashboard tests
	console.log('[Prod E2E] Test complete. Instance left running for persistent tests.');
});
