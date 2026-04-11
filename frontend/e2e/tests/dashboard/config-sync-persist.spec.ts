import { test, expect, navigateToInstance } from '../../fixtures/auth';
import { waitForOpenClawRunning } from '../../helpers/openclaw-status';

const API_KEY = process.env.E2E_OPENROUTER_API_KEY || '';

test.describe('Config sync persists across navigation', () => {

	test('"Applying Config" survives navigation away and back', async ({ authedPage: page }) => {
		test.skip(!API_KEY, 'E2E_OPENROUTER_API_KEY required');

		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Wait for the API key input to appear (config loaded)
		const keyInput = page.locator('#openrouter-key');
		await expect(keyInput).toBeVisible({ timeout: 60_000 });

		// Trigger a config sync by saving the API key (same key is fine — it still triggers a sync)
		console.log('[E2E] Triggering config sync...');
		await keyInput.fill(API_KEY);
		const saveBtn = page.getByRole('button', { name: /save/i }).last();
		await saveBtn.click();

		// Wait for "Applying Config" to appear on the instance page
		const applyingConfig = page.locator('dd').filter({ hasText: /Applying Config/i }).first();
		await expect(applyingConfig).toBeVisible({ timeout: 30_000 });
		console.log('[E2E] "Applying Config" visible');

		// Navigate away to the dashboard list
		console.log('[E2E] Navigating away to dashboard...');
		await page.goto('/dashboard');

		// Wait for dashboard to load
		await expect(page.locator('a[href*="/dashboard/instances/"]').first()).toBeVisible({ timeout: 15_000 });

		// Navigate back to the instance page
		console.log('[E2E] Navigating back to instance...');
		if (!(await navigateToInstance(page))) {
			throw new Error('Could not navigate back to instance');
		}

		// "Applying Config" should still be visible (restored from backend sync status)
		const applyingConfigAfterNav = page.locator('dd').filter({ hasText: /Applying Config/i }).first();
		const stillApplying = await applyingConfigAfterNav.isVisible({ timeout: 15_000 }).catch(() => false);

		// It should either still show "Applying Config" or have already transitioned to "Running"
		// (if the sync completed during navigation). Both are acceptable.
		const running = page.locator('dd').filter({ hasText: /Running/i }).first();
		const isRunning = await running.isVisible({ timeout: 5_000 }).catch(() => false);

		if (stillApplying) {
			console.log('[E2E] "Applying Config" persisted across navigation');
		} else if (isRunning) {
			console.log('[E2E] Sync completed during navigation — status is Running');
		} else {
			// This is the bug: status should not be blank/unhealthy during an active sync
			throw new Error('"Applying Config" did not persist across navigation and status is not Running');
		}

		// Wait for eventual completion
		await waitForOpenClawRunning(page, 120_000);
		console.log('[E2E] Config sync persist test complete — OpenClaw Running');
	});
});
