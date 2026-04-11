import { test, expect, navigateToInstance } from '../../fixtures/auth';

test.describe('Hard refresh does not flash "Applying Config"', () => {

	test('OpenClaw status stays "Running" after hard refresh', async ({ authedPage: page }) => {
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

	test('OpenClaw status stays "Running" after multiple hard refreshes', async ({ authedPage: page }) => {
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
