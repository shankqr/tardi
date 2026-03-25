import { expect, type Page } from '@playwright/test';

/**
 * Wait for OpenClaw status to show "Running" after a config change.
 * Polls the instance page for up to timeoutMs (default 60s), reloading periodically.
 * Throws if OpenClaw does not return to Running within the timeout.
 *
 * The helper extracts the instance URL from the current page URL so that
 * reloads navigate directly to the instance detail page (avoids landing on
 * the dashboard list "Loading..." screen which has no status badge).
 */
export async function waitForOpenClawRunning(page: Page, timeoutMs = 60_000): Promise<void> {
	// Extract instance URL so we always reload the correct page
	const currentUrl = page.url();
	const instanceMatch = currentUrl.match(/\/dashboard\/instances\/[^/]+/);
	const instancePath = instanceMatch ? instanceMatch[0] : null;

	const start = Date.now();
	while (Date.now() - start < timeoutMs) {
		// Check for Running/Healthy in a <dd> element (status badge on instance page)
		const statusText = await page.locator('dd').filter({ hasText: /Running|Healthy/i }).first()
			.isVisible({ timeout: 3_000 }).catch(() => false);
		if (statusText) {
			console.log(`[E2E] OpenClaw status: Running (${Math.round((Date.now() - start) / 1000)}s)`);
			return;
		}

		console.log(`[E2E] Waiting for OpenClaw to be Running... (${Math.round((Date.now() - start) / 1000)}s)`);

		// Navigate to instance page (not just reload — avoids stale dashboard state)
		if (instancePath) {
			await page.goto(instancePath);
		} else {
			await page.reload();
		}
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 15_000 }).catch(() => {});
		await page.waitForTimeout(3_000);
	}
	throw new Error(`OpenClaw did not return to Running status within ${timeoutMs / 1000}s`);
}
