import { test, expect, navigateToInstance } from '../../fixtures/auth';

test.describe('API key masking', () => {

	test('saved API key is shown as password field', async ({ authedPage: page }) => {
		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

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
