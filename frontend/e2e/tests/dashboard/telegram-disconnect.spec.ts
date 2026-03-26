import { test, expect, PERSISTENT_PASSWORD, navigateToInstance } from '../../fixtures/auth';

test.describe('Telegram disconnect', () => {
	test.skip(!PERSISTENT_PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');

	test('telegram section renders with connect form or disconnect button', async ({ authedPage: page }) => {
		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Scroll to Telegram section
		const telegramHeading = page.getByText('Telegram').first();
		await telegramHeading.scrollIntoViewIfNeeded();

		// Wait for the Telegram section to finish loading — either the disconnect
		// button (connected) or the connect input (not connected) must appear
		const disconnectBtn = page.getByRole('button', { name: /disconnect/i });
		const connectInput = page.locator('input[placeholder="Paste your bot token here"]');
		await expect(disconnectBtn.or(connectInput)).toBeVisible({ timeout: 30_000 });

		const isConnected = await disconnectBtn.isVisible().catch(() => false);

		if (isConnected) {
			console.log('[E2E] Telegram is connected, Disconnect button visible');

			// Verify masked token is shown (e.g. "821...LMu4")
			const maskedToken = page.locator('p').filter({ hasText: /\.\.\./  });
			await expect(maskedToken).toBeVisible({ timeout: 5_000 });
			console.log('[E2E] Masked bot token is displayed');
		} else {
			console.log('[E2E] Telegram not connected, connect form shown');
			await expect(connectInput).toBeVisible();
		}
	});
});
