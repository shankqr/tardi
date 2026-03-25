import { test, expect, type Page } from '@playwright/test';

const EMAIL = process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+1@gmail.com';
const PASSWORD = process.env.E2E_PERSISTENT_PASSWORD || process.env.E2E_TEST_PASSWORD || '';

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

test.describe('API key masking', () => {
	test.skip(!PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');

	test('saved API key is shown as password field', async ({ page }) => {
		await login(page);
		await page.waitForTimeout(5000);

		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		const hasInstance = await instanceLink.isVisible({ timeout: 10_000 }).catch(() => false);
		if (!hasInstance) {
			test.skip(true, 'No active instance found');
			return;
		}

		await instanceLink.click();
		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });

		// Wait for AI Provider section to load
		const keyInput = page.locator('#openrouter-key');
		await expect(keyInput).toBeVisible({ timeout: 60_000 });

		// If a key is already saved, the input should be type="password" (masked)
		const inputType = await keyInput.getAttribute('type');

		// Check if there's a "Key is saved" message indicating an existing key
		const keySavedMsg = page.getByText('Key is saved');
		const hasExistingKey = await keySavedMsg.isVisible({ timeout: 5_000 }).catch(() => false);

		if (hasExistingKey) {
			// Key is saved — input should be password type
			expect(inputType).toBe('password');
			console.log('[E2E] API key is masked (password field)');

			// Show/Hide toggle should exist
			const showBtn = page.getByRole('button', { name: /show|hide/i });
			await expect(showBtn).toBeVisible();

			// Click Show to reveal key
			await showBtn.click();
			const revealedType = await keyInput.getAttribute('type');
			expect(revealedType).toBe('text');
			console.log('[E2E] API key revealed after clicking Show');

			// Click Hide to mask again
			await showBtn.click();
			const maskedAgain = await keyInput.getAttribute('type');
			expect(maskedAgain).toBe('password');
			console.log('[E2E] API key masked again after clicking Hide');
		} else {
			// No key saved yet — just verify the input exists
			console.log('[E2E] No existing API key, skipping masking check');
		}
	});
});
