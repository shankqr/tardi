import { test, expect, PERSISTENT_PASSWORD, navigateToInstance } from '../../fixtures/auth';
import { waitForOpenClawRunning } from '../../helpers/openclaw-status';

const API_KEY_1 = process.env.E2E_OPENROUTER_API_KEY || '';
const API_KEY_2 = process.env.E2E_OPENROUTER_API_KEY_2 || '';
const TG_TOKEN_1 = process.env.E2E_TELEGRAM_BOT_TOKEN || '';
const TG_TOKEN_2 = process.env.E2E_TELEGRAM_BOT_TOKEN_2 || '';

test.describe('Config swap: API key and Telegram token', () => {
	test.skip(!PERSISTENT_PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');

	test('swap OpenRouter API key and restore', async ({ authedPage: page }) => {
		test.skip(!API_KEY_1 || !API_KEY_2, 'Both E2E_OPENROUTER_API_KEY and E2E_OPENROUTER_API_KEY_2 required');

		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		const keyInput = page.locator('#openrouter-key');
		await expect(keyInput).toBeVisible({ timeout: 60_000 });

		// Step 1: Set API key to the second key
		console.log('[E2E] Changing to API key 2...');
		await keyInput.fill(API_KEY_2);
		const saveBtn = page.getByRole('button', { name: /save/i }).last();
		await saveBtn.click();

		// Wait for sync success
		await expect(
			page.getByText('Configuration applied successfully')
		).toBeVisible({ timeout: 120_000 });
		console.log('[E2E] API key 2 applied');

		// Verify OpenClaw returns to Running
		await waitForOpenClawRunning(page);

		// Step 2: Swap back to the original key
		console.log('[E2E] Restoring API key 1...');
		await page.reload();
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });
		const keyInputAgain = page.locator('#openrouter-key');
		await expect(keyInputAgain).toBeVisible({ timeout: 30_000 });
		await keyInputAgain.fill(API_KEY_1);
		const saveBtnAgain = page.getByRole('button', { name: /save/i }).last();
		await saveBtnAgain.click();

		await expect(
			page.getByText('Configuration applied successfully')
		).toBeVisible({ timeout: 120_000 });
		console.log('[E2E] API key 1 restored');

		// Verify OpenClaw returns to Running
		await waitForOpenClawRunning(page);
		console.log('[E2E] OpenRouter API key swap complete — OpenClaw Running');
	});

	test('swap Telegram bot token and restore', async ({ authedPage: page }) => {
		test.skip(!TG_TOKEN_1 || !TG_TOKEN_2, 'Both E2E_TELEGRAM_BOT_TOKEN and E2E_TELEGRAM_BOT_TOKEN_2 required');

		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Scroll to Telegram section and wait for it to fully load its connection state
		const telegramHeading = page.getByText('Telegram').first();
		await telegramHeading.scrollIntoViewIfNeeded();

		// Wait for the Telegram section to finish loading from the API.
		// The connection state loads asynchronously — wait for either the disconnect
		// button (connected) or the connect input (not connected) to appear.
		const disconnectBtn = page.getByRole('button', { name: /disconnect/i });
		const connectInput = page.locator('input[placeholder="Paste your bot token here"]');
		await expect(disconnectBtn.or(connectInput)).toBeVisible({ timeout: 30_000 });
		const isConnected = await disconnectBtn.isVisible().catch(() => false);

		// Step 1: Connect/update with token 2
		console.log('[E2E] Setting Telegram token 2...');
		if (isConnected) {
			// Click "Update Token" to reveal the update input
			const updateTokenBtn = page.getByRole('button', { name: /update token/i });
			if (await updateTokenBtn.isVisible({ timeout: 5_000 }).catch(() => false)) {
				await updateTokenBtn.click();
			}
			const updateInput = page.locator('input[placeholder="Paste new bot token"]');
			await expect(updateInput).toBeVisible({ timeout: 10_000 });
			await updateInput.click();
			await updateInput.pressSequentially(TG_TOKEN_2, { delay: 10 });
			const updateBtn = page.getByRole('button', { name: /^update$/i });
			await updateBtn.click();
		} else {
			const connectInput = page.locator('input[placeholder="Paste your bot token here"]');
			await expect(connectInput).toBeVisible({ timeout: 10_000 });
			await connectInput.click();
			await connectInput.pressSequentially(TG_TOKEN_2, { delay: 10 });
			await page.getByRole('button', { name: 'Connect', exact: true }).click();
		}

		// Wait for sync — look for success indicator or the disconnect button to reappear
		await expect(page.getByRole('button', { name: /disconnect/i })).toBeVisible({ timeout: 120_000 });
		console.log('[E2E] Telegram token 2 connected');

		// Verify OpenClaw returns to Running
		await waitForOpenClawRunning(page);

		// Step 2: Swap back to token 1
		console.log('[E2E] Restoring Telegram token 1...');
		await page.reload();
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });

		// Re-locate Telegram section after reload
		const telegramHeading2 = page.getByText('Telegram').first();
		await telegramHeading2.scrollIntoViewIfNeeded();

		// Wait for disconnect button to confirm connected state loaded
		await expect(page.getByRole('button', { name: /disconnect/i })).toBeVisible({ timeout: 15_000 });

		// Click "Update Token" to reveal the update input
		const updateTokenBtn2 = page.getByRole('button', { name: /update token/i });
		if (await updateTokenBtn2.isVisible({ timeout: 5_000 }).catch(() => false)) {
			await updateTokenBtn2.click();
		}
		const restoreInput = page.locator('input[placeholder="Paste new bot token"]');
		await expect(restoreInput).toBeVisible({ timeout: 10_000 });
		await restoreInput.click();
		await restoreInput.pressSequentially(TG_TOKEN_1, { delay: 10 });
		await page.getByRole('button', { name: /^update$/i }).click();

		await expect(page.getByRole('button', { name: /disconnect/i })).toBeVisible({ timeout: 120_000 });
		console.log('[E2E] Telegram token 1 restored');

		// Verify OpenClaw returns to Running
		await waitForOpenClawRunning(page);
		console.log('[E2E] Telegram token swap complete — OpenClaw Running');
	});
});
