import { test, expect, type Page } from '@playwright/test';
import { waitForOpenClawRunning } from '../../helpers/openclaw-status';

const EMAIL = process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+1@gmail.com';
const PASSWORD = process.env.E2E_PERSISTENT_PASSWORD || process.env.E2E_TEST_PASSWORD || '';
const API_KEY = process.env.E2E_OPENROUTER_API_KEY || '';

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

test.describe('Config sync persists across navigation', () => {
	test.skip(!PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');

	test('"Applying Config" survives navigation away and back', async ({ page }) => {
		test.skip(!API_KEY, 'E2E_OPENROUTER_API_KEY required');

		await login(page);
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
		await page.waitForTimeout(2000);

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
