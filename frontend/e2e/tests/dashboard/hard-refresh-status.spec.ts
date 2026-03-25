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

test.describe('Hard refresh does not flash "Applying Config"', () => {
	test.skip(!PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');

	test('OpenClaw status stays "Running" after hard refresh', async ({ page }) => {
		await login(page);
		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Wait for OpenClaw status to show "Running" initially
		const runningStatus = page.locator('dd').filter({ hasText: /Running/i }).first();
		await expect(runningStatus).toBeVisible({ timeout: 60_000 });
		console.log('[E2E] OpenClaw status is Running before refresh');

		// Save the instance URL for navigation after refresh
		const instanceUrl = page.url();

		// Hard refresh (equivalent to Cmd+Shift+R — bypasses cache)
		await page.goto(instanceUrl, { waitUntil: 'networkidle' });
		console.log('[E2E] Hard refresh completed');

		// Wait for Agent Details to load
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });

		// Allow a moment for sync status check and effects to settle
		await page.waitForTimeout(3000);

		// Status should be "Running", NOT "Applying Config"
		const applyingConfig = page.locator('dd').filter({ hasText: /Applying Config/i }).first();
		const isApplyingConfig = await applyingConfig.isVisible({ timeout: 2_000 }).catch(() => false);

		if (isApplyingConfig) {
			// Take a screenshot for debugging
			await page.screenshot({ path: 'e2e/screenshots/hard-refresh-applying-config.png' });
			throw new Error('Hard refresh caused status to show "Applying Config" instead of "Running"');
		}

		// Verify it shows "Running"
		await expect(runningStatus).toBeVisible({ timeout: 10_000 });
		console.log('[E2E] OpenClaw status is still Running after hard refresh');
	});

	test('OpenClaw status stays "Running" after multiple hard refreshes', async ({ page }) => {
		await login(page);
		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Wait for initial Running status
		const runningStatus = page.locator('dd').filter({ hasText: /Running/i }).first();
		await expect(runningStatus).toBeVisible({ timeout: 60_000 });

		const instanceUrl = page.url();

		// Perform 3 consecutive hard refreshes
		for (let i = 1; i <= 3; i++) {
			await page.goto(instanceUrl, { waitUntil: 'networkidle' });
			await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });
			await page.waitForTimeout(3000);

			const applyingConfig = page.locator('dd').filter({ hasText: /Applying Config/i }).first();
			const isApplyingConfig = await applyingConfig.isVisible({ timeout: 2_000 }).catch(() => false);

			if (isApplyingConfig) {
				throw new Error(`Hard refresh #${i} caused status to show "Applying Config"`);
			}

			await expect(runningStatus).toBeVisible({ timeout: 10_000 });
			console.log(`[E2E] Refresh #${i}: OpenClaw status is Running`);
		}
	});
});
