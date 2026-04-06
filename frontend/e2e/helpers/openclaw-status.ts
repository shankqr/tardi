import type { Page } from '@playwright/test';

/**
 * Wait for agent status to show "Running" after a config change.
 * Works for both OpenClaw and Hermes frameworks.
 * Polls the instance page for up to timeoutMs, reloading periodically.
 * Throws if the agent does not return to Running within the timeout.
 */
export async function waitForAgentRunning(page: Page, timeoutMs = 300_000): Promise<void> {
	const currentUrl = page.url();
	const instanceMatch = currentUrl.match(/\/dashboard\/instances\/[^/]+/);
	const instancePath = instanceMatch ? instanceMatch[0] : null;

	const start = Date.now();
	let reloadCount = 0;
	while (Date.now() - start < timeoutMs) {
		// Check for Running/Healthy in a <dd> element (status badge on instance page)
		const statusText = await page.locator('dd').filter({ hasText: /Running|Healthy/i }).first()
			.isVisible({ timeout: 10_000 }).catch(() => false);
		if (statusText) {
			console.log(`[E2E] Agent status: Running (${Math.round((Date.now() - start) / 1000)}s)`);
			return;
		}

		console.log(`[E2E] Waiting for agent to be Running... (${Math.round((Date.now() - start) / 1000)}s)`);

		// Reload every 3rd check to get fresh dashboard state; otherwise just wait
		// for the in-page 5s polling to update
		reloadCount++;
		if (reloadCount % 3 === 0) {
			if (instancePath) {
				await page.goto(instancePath);
			} else {
				await page.reload();
			}
			await page.getByText('Agent Details').waitFor({ state: 'visible', timeout: 15_000 }).catch(() => {});
		}
		await page.waitForTimeout(5_000);
	}
	throw new Error(`Agent did not return to Running status within ${timeoutMs / 1000}s`);
}

/**
 * @deprecated Use waitForAgentRunning instead. Kept for backward compatibility.
 */
export async function waitForOpenClawRunning(page: Page, timeoutMs = 300_000): Promise<void> {
	return waitForAgentRunning(page, timeoutMs);
}
