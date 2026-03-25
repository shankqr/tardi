import { test, expect, type Page } from '@playwright/test';
import { waitForOpenClawRunning } from '../../helpers/openclaw-status';

const EMAIL = process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+1@gmail.com';
const PASSWORD = process.env.E2E_PERSISTENT_PASSWORD || process.env.E2E_TEST_PASSWORD || '';

const API_KEY_1 = process.env.E2E_OPENROUTER_API_KEY || '';
const API_KEY_2 = process.env.E2E_OPENROUTER_API_KEY_2 || '';
const TG_TOKEN_1 = process.env.E2E_TELEGRAM_BOT_TOKEN || '';
const TG_TOKEN_2 = process.env.E2E_TELEGRAM_BOT_TOKEN_2 || '';

async function login(page: Page): Promise<void> {
	await page.goto('/login');
	const signInBtn = page.getByRole('button', { name: 'Sign in' });
	await expect(signInBtn).toBeVisible({ timeout: 10_000 });
	await expect(signInBtn).toBeEnabled();
	await page.waitForTimeout(1000);

	await page.locator('#email').click();
	await page.locator('#email').pressSequentially(EMAIL, { delay: 20 });
	await page.locator('#password').click();
	await page.locator('#password').pressSequentially(PASSWORD, { delay: 20 });
	await signInBtn.click();
	await page.waitForURL('**/dashboard**', { timeout: 30_000 });
}

async function navigateToInstance(page: Page): Promise<boolean> {
	await page.waitForTimeout(5000);
	const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
	const hasInstance = await instanceLink.isVisible({ timeout: 10_000 }).catch(() => false);
	if (!hasInstance) return false;
	await instanceLink.click();
	await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
	await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });
	return true;
}

test.describe('Config swap: API key and Telegram token', () => {
	test.skip(!PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');

	test('swap OpenRouter API key and restore', async ({ page }) => {
		test.skip(!API_KEY_1 || !API_KEY_2, 'Both E2E_OPENROUTER_API_KEY and E2E_OPENROUTER_API_KEY_2 required');

		await login(page);
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
		await page.waitForTimeout(5000);
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

	test('swap Telegram bot token and restore', async ({ page }) => {
		test.skip(!TG_TOKEN_1 || !TG_TOKEN_2, 'Both E2E_TELEGRAM_BOT_TOKEN and E2E_TELEGRAM_BOT_TOKEN_2 required');

		await login(page);
		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Scroll to Telegram section
		const telegramHeading = page.getByText('Telegram').first();
		await telegramHeading.scrollIntoViewIfNeeded();
		await page.waitForTimeout(2000);

		// Check if Telegram is already connected
		const disconnectBtn = page.getByRole('button', { name: /disconnect/i });
		const isConnected = await disconnectBtn.isVisible({ timeout: 5_000 }).catch(() => false);

		// Step 1: Connect/update with token 2
		console.log('[E2E] Setting Telegram token 2...');
		if (isConnected) {
			// Use the "update" input
			const updateInput = page.locator('input[placeholder="Paste new bot token"]');
			if (await updateInput.isVisible({ timeout: 5_000 }).catch(() => false)) {
				await updateInput.click();
				await updateInput.pressSequentially(TG_TOKEN_2, { delay: 10 });
				const updateBtn = page.getByRole('button', { name: /update/i });
				await updateBtn.click();
			} else {
				// Disconnect first, then connect with new token
				await disconnectBtn.click();
				await page.waitForTimeout(3000);
				const connectInput = page.locator('input[placeholder="Paste your bot token here"]');
				await expect(connectInput).toBeVisible({ timeout: 10_000 });
				await connectInput.click();
				await connectInput.pressSequentially(TG_TOKEN_2, { delay: 10 });
				await page.getByRole('button', { name: 'Connect', exact: true }).click();
			}
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
		await page.waitForTimeout(5000);

		// Re-locate Telegram section after reload
		const telegramHeading2 = page.getByText('Telegram').first();
		await telegramHeading2.scrollIntoViewIfNeeded();
		await page.waitForTimeout(2000);

		const updateInput = page.locator('input[placeholder="Paste new bot token"]');
		if (await updateInput.isVisible({ timeout: 5_000 }).catch(() => false)) {
			await updateInput.click();
			await updateInput.pressSequentially(TG_TOKEN_1, { delay: 10 });
			await page.getByRole('button', { name: /update/i }).click();
		} else {
			// May need to disconnect first
			const disc = page.getByRole('button', { name: /disconnect/i });
			if (await disc.isVisible({ timeout: 3_000 }).catch(() => false)) {
				await disc.click();
				await page.waitForTimeout(5000);
				await telegramHeading2.scrollIntoViewIfNeeded();
			}
			const connectInput = page.locator('input[placeholder="Paste your bot token here"]');
			await expect(connectInput).toBeVisible({ timeout: 15_000 });
			await connectInput.click();
			await connectInput.pressSequentially(TG_TOKEN_1, { delay: 10 });
			await page.getByRole('button', { name: 'Connect', exact: true }).click();
		}

		await expect(page.getByRole('button', { name: /disconnect/i })).toBeVisible({ timeout: 120_000 });
		console.log('[E2E] Telegram token 1 restored');

		// Verify OpenClaw returns to Running
		await waitForOpenClawRunning(page);
		console.log('[E2E] Telegram token swap complete — OpenClaw Running');
	});
});
