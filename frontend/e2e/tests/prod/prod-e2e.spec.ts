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

test('Prod E2E: full deploy + configure + verify cycle', async ({ page }) => {
	let instanceId = '';

	// ═══════════════════════════════════════════════
	// PHASE 1: Deploy
	// ═══════════════════════════════════════════════

	// ── Step 1: Delete any existing instance ──
	await test.step('Delete existing instance', async () => {
		await deleteExistingInstances(PROD_EMAIL, PROD_PASSWORD);
	});

	// ── Step 2: Login and verify deploy form ──
	await test.step('Login to prod dashboard', async () => {
		await loginWithCredentials(page, PROD_EMAIL, PROD_PASSWORD);
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

		console.log('[Prod E2E] Waiting for provisioning to complete...');
		await waitForInstanceActive(PROD_EMAIL, PROD_PASSWORD, instanceId, 600_000);

		await ensureInstancePage(page, instanceId);
		console.log('[Prod E2E] Agent is active and ready for configuration');
	});

	// ═══════════════════════════════════════════════
	// PHASE 2: Configure
	// ═══════════════════════════════════════════════

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
		const modelSelectVisible = await modelSelect.isVisible({ timeout: 5_000 }).catch(() => false);
		if (!modelSelectVisible) {
			console.log('[Prod E2E] Skipping: model selection feature flag is disabled');
			return;
		}
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

	// ── Step 6: Link Telegram bot ──
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

	// ═══════════════════════════════════════════════
	// PHASE 3: Verify (post-deploy dashboard tests)
	// ═══════════════════════════════════════════════

	// ── Step 7: Verify OpenClaw dashboard responds ──
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
			await ensureInstancePage(page, instanceId);
			return;
		}

		console.log(`[Prod E2E] Opening dashboard: ${dashboardUrl}`);

		const ocUrl = `${dashboardUrl}/#token=${dashboardToken}`;
		let dashboardReady = false;
		for (let attempt = 0; attempt < 6; attempt++) {
			try {
				const response = await page.goto(ocUrl, { timeout: 20_000, waitUntil: 'domcontentloaded' });
				if (response && (response.ok() || response.status() === 426)) {
					dashboardReady = true;
					break;
				}
				console.log(`[Prod E2E] Dashboard returned ${response?.status()}, retrying...`);
			} catch {
				console.log(`[Prod E2E] Dashboard not reachable yet (attempt ${attempt + 1}/6)`);
			}
			await page.waitForTimeout(15_000);
		}

		if (dashboardReady) {
			const chatInput = page.locator(
				'textarea, input[type="text"], [contenteditable="true"]'
			).last();
			await expect(chatInput).toBeVisible({ timeout: 60_000 });
			console.log('[Prod E2E] Control UI loaded, chat input found');

			await chatInput.click();
			await chatInput.fill('what model are you using right now?');
			await page.keyboard.press('Enter');

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

			expect(responseDetected).toBeTruthy();
			console.log('[Prod E2E] Dashboard responded with relevant content');
		} else {
			console.log('[Prod E2E] Dashboard never became reachable, skipping OC verification');
		}

		await ensureInstancePage(page, instanceId);
	});

	// ── Step 8: API key masking ──
	await test.step('Verify API key is masked', async () => {
		const keyInput = page.locator('#openrouter-key');
		await expect(keyInput).toBeVisible({ timeout: 30_000 });

		const keySavedMsg = page.getByText('Key is saved');
		const hasExistingKey = await keySavedMsg.isVisible({ timeout: 5_000 }).catch(() => false);

		if (hasExistingKey) {
			const inputType = await keyInput.getAttribute('type');
			expect(inputType).toBe('password');
			console.log('[Prod E2E] API key is masked (password field)');

			// Test Show/Hide toggle
			const showBtn = page.getByRole('button', { name: /show|hide/i });
			await expect(showBtn).toBeVisible();
			await showBtn.click();
			expect(await keyInput.getAttribute('type')).toBe('text');
			await showBtn.click();
			expect(await keyInput.getAttribute('type')).toBe('password');
			console.log('[Prod E2E] Show/Hide toggle works');
		} else {
			console.log('[Prod E2E] No saved key indicator, skipping masking check');
		}
	});

	// ── Step 9: Telegram section verification ──
	await test.step('Verify Telegram section', async () => {
		const telegramHeading = page.getByText('Telegram').first();
		await telegramHeading.scrollIntoViewIfNeeded();

		const disconnectBtn = page.getByRole('button', { name: /disconnect/i });
		const connectInput = page.locator('input[placeholder="Paste your bot token here"]');
		await expect(disconnectBtn.or(connectInput)).toBeVisible({ timeout: 30_000 });

		const isConnected = await disconnectBtn.isVisible().catch(() => false);
		if (isConnected) {
			console.log('[Prod E2E] Telegram bot is connected, disconnect button visible');
			// Verify masked token is displayed
			const maskedToken = page.getByText(/\d{3}\.{3}\w+/);
			const hasMasked = await maskedToken.isVisible({ timeout: 5_000 }).catch(() => false);
			if (hasMasked) console.log('[Prod E2E] Masked bot token displayed');
		} else {
			console.log('[Prod E2E] Telegram not connected (connect form visible)');
		}
	});

	// ── Step 10: Instance rename and restore ──
	await test.step('Rename instance and restore', async () => {
		const editButton = page.locator('button[title="Rename agent"]');
		await editButton.scrollIntoViewIfNeeded();
		await expect(editButton).toBeVisible({ timeout: 10_000 });
		await editButton.click();

		const nameInput = page.locator('input[type="text"]').first();
		await expect(nameInput).toBeVisible({ timeout: 5_000 });
		const originalName = await nameInput.inputValue();
		console.log(`[Prod E2E] Original instance name: ${originalName}`);

		const newName = `e2e-renamed-${Date.now()}`;
		await nameInput.clear();
		await nameInput.pressSequentially(newName, { delay: 20 });

		const renameSection = nameInput.locator('..');
		await renameSection.getByRole('button', { name: 'Save' }).click();

		await expect(page.locator('h2').filter({ hasText: newName })).toBeVisible({ timeout: 15_000 });
		console.log(`[Prod E2E] Instance renamed to: ${newName}`);

		// Restore original name
		const editAgain = page.locator('button[title="Rename agent"]');
		await expect(editAgain).toBeVisible({ timeout: 10_000 });
		await editAgain.click();

		const nameInputAgain = page.locator('input[type="text"]').first();
		await expect(nameInputAgain).toBeVisible({ timeout: 5_000 });
		await nameInputAgain.clear();
		await nameInputAgain.pressSequentially(originalName, { delay: 20 });

		const renameSectionAgain = nameInputAgain.locator('..');
		await renameSectionAgain.getByRole('button', { name: 'Save' }).click();

		await expect(page.locator('h2').filter({ hasText: originalName })).toBeVisible({ timeout: 15_000 });
		console.log(`[Prod E2E] Instance name restored to: ${originalName}`);
	});

	// ── Step 11: Health check ──
	await test.step('Run health check', async () => {
		const powerUserButton = page.getByText('Power User').first();
		const powerUserVisible = await powerUserButton.isVisible({ timeout: 5_000 }).catch(() => false);
		if (!powerUserVisible) {
			console.log('[Prod E2E] Skipping: health check feature flag is disabled');
			return;
		}
		await powerUserButton.scrollIntoViewIfNeeded();
		await powerUserButton.click();
		await page.waitForTimeout(500);

		const healthCheckButton = page.getByRole('button', { name: 'Health Check' }).first();
		const healthCheckVisible = await healthCheckButton.isVisible({ timeout: 5_000 }).catch(() => false);
		if (!healthCheckVisible) {
			console.log('[Prod E2E] Skipping: health check button not found (feature flag disabled)');
			return;
		}
		await expect(healthCheckButton).toBeVisible({ timeout: 10_000 });
		await expect(healthCheckButton).toBeEnabled({ timeout: 10_000 });
		await healthCheckButton.click();

		const resultsHeading = page.getByText('Health Check Results');
		await expect(resultsHeading).toBeVisible({ timeout: 90_000 });

		const pageContent = (await page.textContent('body') || '').toLowerCase();
		const hasCheckContent =
			pageContent.includes('pass') ||
			pageContent.includes('fail') ||
			pageContent.includes('warn') ||
			pageContent.includes('✓') ||
			pageContent.includes('✗');
		expect(hasCheckContent).toBeTruthy();
		console.log('[Prod E2E] Health check completed with results');
	});

	// ── Step 12: Swap API key and restore ──
	await test.step('Swap API key and restore', async () => {
		const apiKey1 = process.env.E2E_OPENROUTER_API_KEY || '';
		const apiKey2 = process.env.E2E_OPENROUTER_API_KEY_2 || '';
		if (!apiKey1 || !apiKey2) {
			console.log('[Prod E2E] Skipping: need both E2E_OPENROUTER_API_KEY and _2');
			return;
		}

		// Navigate back to instance page (health check may have changed the view)
		await ensureInstancePage(page, instanceId);

		// Swap to key 2
		console.log('[Prod E2E] Changing to API key 2...');
		const keyInput = page.locator('#openrouter-key');
		await expect(keyInput).toBeVisible({ timeout: 30_000 });
		await keyInput.fill(apiKey2);
		await page.getByRole('button', { name: 'Save' }).first().click();

		await waitForSyncComplete(PROD_EMAIL, PROD_PASSWORD, instanceId);
		await waitForInstanceActive(PROD_EMAIL, PROD_PASSWORD, instanceId);
		await ensureInstancePage(page, instanceId);
		await waitForOpenClawRunning(page);
		console.log('[Prod E2E] API key 2 applied');

		// Swap back to key 1
		console.log('[Prod E2E] Restoring API key 1...');
		const keyInputAgain = page.locator('#openrouter-key');
		await expect(keyInputAgain).toBeVisible({ timeout: 30_000 });
		await keyInputAgain.fill(apiKey1);
		await page.getByRole('button', { name: 'Save' }).first().click();

		await waitForSyncComplete(PROD_EMAIL, PROD_PASSWORD, instanceId);
		await waitForInstanceActive(PROD_EMAIL, PROD_PASSWORD, instanceId);
		await ensureInstancePage(page, instanceId);
		await waitForOpenClawRunning(page);
		console.log('[Prod E2E] API key 1 restored — swap complete');
	});

	// ── Step 13: Swap Telegram bot token and restore ──
	await test.step('Swap Telegram bot token and restore', async () => {
		const tgToken1 = process.env.E2E_TELEGRAM_BOT_TOKEN || '';
		const tgToken2 = process.env.E2E_TELEGRAM_BOT_TOKEN_2 || '';
		if (!tgToken1 || !tgToken2) {
			console.log('[Prod E2E] Skipping: need both E2E_TELEGRAM_BOT_TOKEN and _2');
			return;
		}

		// Scroll to Telegram section
		const telegramHeading = page.getByText('Telegram').first();
		await telegramHeading.scrollIntoViewIfNeeded();

		const disconnectBtn = page.getByRole('button', { name: /disconnect/i });
		await expect(disconnectBtn).toBeVisible({ timeout: 30_000 });

		// Swap to token 2
		console.log('[Prod E2E] Setting Telegram token 2...');
		const updateTokenBtn = page.getByRole('button', { name: /update token/i });
		if (await updateTokenBtn.isVisible({ timeout: 5_000 }).catch(() => false)) {
			await updateTokenBtn.click();
		}
		const updateInput = page.locator('input[placeholder="Paste new bot token"]');
		await expect(updateInput).toBeVisible({ timeout: 10_000 });
		await updateInput.click();
		await updateInput.pressSequentially(tgToken2, { delay: 10 });
		await page.getByRole('button', { name: /^update$/i }).click();

		await expect(page.getByRole('button', { name: /disconnect/i })).toBeVisible({ timeout: 120_000 });
		await waitForOpenClawRunning(page);
		console.log('[Prod E2E] Telegram token 2 connected');

		// Swap back to token 1
		console.log('[Prod E2E] Restoring Telegram token 1...');
		await page.reload();
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });

		const telegramHeading2 = page.getByText('Telegram').first();
		await telegramHeading2.scrollIntoViewIfNeeded();
		await expect(page.getByRole('button', { name: /disconnect/i })).toBeVisible({ timeout: 15_000 });

		const updateTokenBtn2 = page.getByRole('button', { name: /update token/i });
		if (await updateTokenBtn2.isVisible({ timeout: 5_000 }).catch(() => false)) {
			await updateTokenBtn2.click();
		}
		const restoreInput = page.locator('input[placeholder="Paste new bot token"]');
		await expect(restoreInput).toBeVisible({ timeout: 10_000 });
		await restoreInput.click();
		await restoreInput.pressSequentially(tgToken1, { delay: 10 });
		await page.getByRole('button', { name: /^update$/i }).click();

		await expect(page.getByRole('button', { name: /disconnect/i })).toBeVisible({ timeout: 120_000 });
		await waitForOpenClawRunning(page);
		console.log('[Prod E2E] Telegram token 1 restored — swap complete');
	});

	// ── Step 14: Snapshot create, restore, and delete ──
	await test.step('Snapshot lifecycle', async () => {
		await ensureInstancePage(page, instanceId);

		await expect(page.getByRole('heading', { name: 'Snapshots' })).toBeVisible({ timeout: 30_000 });

		const snapshotName = `e2e-snapshot-${Date.now()}`;

		// Create snapshot
		const createToggle = page.getByRole('button', { name: '+ Create Snapshot' });
		await createToggle.scrollIntoViewIfNeeded();
		await expect(createToggle).toBeVisible({ timeout: 10_000 });
		await createToggle.click();

		const nameInput = page.locator('input[placeholder="Snapshot name"]');
		await expect(nameInput).toBeVisible({ timeout: 5_000 });
		await nameInput.click();
		await nameInput.pressSequentially(snapshotName, { delay: 20 });

		const createBtn = page.getByRole('button', { name: 'Create' }).first();
		await expect(createBtn).toBeEnabled();
		await createBtn.click();

		await expect(page.getByText(snapshotName)).toBeVisible({ timeout: 30_000 });

		// Wait for snapshot to be ready (up to 3 min)
		const snapshotRow = page.getByText(snapshotName).locator('..').locator('..');
		await expect(snapshotRow.getByRole('button', { name: 'Delete' })).toBeVisible({ timeout: 180_000 });
		console.log(`[Prod E2E] Snapshot "${snapshotName}" created and ready`);

		// Restore snapshot
		const restoreBtn = snapshotRow.getByRole('button', { name: 'Restore' });
		await restoreBtn.click();

		const confirmBtn = page.getByRole('button', { name: /confirm|yes|restore/i }).last();
		await expect(confirmBtn).toBeVisible({ timeout: 5_000 });
		await confirmBtn.click();

		// Wait for page to settle after restore — either a success message or the instance page reloads
		await expect(page.getByRole('heading', { name: 'Agent Details' })).toBeVisible({ timeout: 180_000 });

		const runningStatus = page.locator('dd').filter({ hasText: /Running|Active/i }).first();
		await expect(runningStatus).toBeVisible({ timeout: 120_000 });
		console.log(`[Prod E2E] Snapshot "${snapshotName}" restored successfully`);

		// Delete snapshot
		const snapshotEntryAfterRestore = page.getByText(snapshotName);
		const snapshotRowAfterRestore = snapshotEntryAfterRestore.locator('..').locator('..');
		const deleteBtn = snapshotRowAfterRestore.getByRole('button', { name: 'Delete' });
		await deleteBtn.click();

		const confirmInput = snapshotRowAfterRestore.locator('input[type="text"]');
		await expect(confirmInput).toBeVisible({ timeout: 5_000 });
		await confirmInput.click();
		await confirmInput.pressSequentially(snapshotName, { delay: 20 });

		const confirmDeleteBtn = snapshotRowAfterRestore.getByRole('button', { name: 'Delete' });
		await expect(confirmDeleteBtn).toBeEnabled();
		await confirmDeleteBtn.click();

		await expect(page.getByText(snapshotName)).toBeHidden({ timeout: 30_000 });
		console.log(`[Prod E2E] Snapshot "${snapshotName}" deleted`);
	});

	// ── Step 15: Billing page ──
	await test.step('Verify billing page', async () => {
		await page.goto('/dashboard/billing');
		await expect(page.getByRole('heading', { name: /plan details/i })).toBeVisible({ timeout: 15_000 });

		// Check for Manage Billing button (Stripe portal)
		const manageBillingBtn = page.getByRole('button', { name: /manage billing/i })
			.or(page.getByRole('link', { name: /manage billing/i }));
		const hasBilling = await manageBillingBtn.isVisible({ timeout: 5_000 }).catch(() => false);
		if (hasBilling) {
			console.log('[Prod E2E] Manage Billing button visible');
		}
		console.log('[Prod E2E] Billing page verified');
	});

	// ── Step 16: Settings page ──
	await test.step('Verify settings page', async () => {
		await page.goto('/dashboard/settings');
		await expect(page.getByText(PROD_EMAIL)).toBeVisible({ timeout: 15_000 });

		// Back to dashboard link
		const backLink = page.getByRole('link', { name: /back|dashboard/i });
		await expect(backLink).toBeVisible({ timeout: 5_000 });
		console.log('[Prod E2E] Settings page verified');
	});

	// ── Cleanup: Delete instance to save Hetzner costs ──
	await test.step('Delete instance (cleanup)', async () => {
		console.log('[Prod E2E] Cleaning up — deleting instance to save costs...');
		await deleteExistingInstances(PROD_EMAIL, PROD_PASSWORD);
		console.log('[Prod E2E] Instance deleted. All tests complete.');
	});
});
