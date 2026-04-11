import { test, expect, navigateToInstance } from '../../fixtures/auth';
import { waitForOpenClawRunning } from '../../helpers/openclaw-status';

const API_KEY_1 = process.env.E2E_OPENROUTER_API_KEY || '';
const API_KEY_2 = process.env.E2E_OPENROUTER_API_KEY_2 || '';

test.describe('Config swap: API key', () => {

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
});
