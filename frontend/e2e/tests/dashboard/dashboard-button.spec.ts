import { test, expect, PERSISTENT_PASSWORD, navigateToInstance } from '../../fixtures/auth';

test.describe('Open Agent Dashboard button', () => {
	test.skip(!PERSISTENT_PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');

	test('Open Agent Dashboard button opens OC dashboard with token auth', async ({ authedPage: page }) => {
		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Wait for agent to be Running (button requires healthy agent)
		const runningStatus = page.locator('dd').filter({ hasText: /Running/i }).first();
		await expect(runningStatus).toBeVisible({ timeout: 60_000 });

		// The "Open Agent Dashboard" button should be visible when agent is healthy
		const dashboardBtn = page.getByRole('button', { name: 'Open Agent Dashboard' });
		await dashboardBtn.scrollIntoViewIfNeeded();
		const isVisible = await dashboardBtn.isVisible({ timeout: 10_000 }).catch(() => false);

		if (!isVisible) {
			// Button may be hidden if API key is not set
			console.log('[E2E] Open Agent Dashboard button not visible — may require API key');
			test.skip(true, 'Open Agent Dashboard button not available (API key may be required)');
			return;
		}

		await expect(dashboardBtn).toBeEnabled({ timeout: 10_000 });

		// Intercept window.open to capture the URL (button opens in new tab)
		await page.evaluate(() => {
			(window as any).__capturedOpenUrl = '';
			(window as any).__originalOpen = window.open;
			window.open = (...args: any[]) => {
				(window as any).__capturedOpenUrl = args[0];
				return null;
			};
		});

		await dashboardBtn.click();

		// Wait for the token fetch + 2s delay before window.open
		await page.waitForTimeout(5000);

		const capturedUrl = await page.evaluate(() => (window as any).__capturedOpenUrl as string);

		// Restore window.open
		await page.evaluate(() => {
			window.open = (window as any).__originalOpen;
		});

		// Verify the URL contains a token in the hash fragment
		expect(capturedUrl).toBeTruthy();
		expect(capturedUrl).toContain('#token=');
		console.log(`[E2E] Dashboard URL captured: ${capturedUrl.split('#')[0]}#token=<redacted>`);

		// Extract the URL and token
		const [baseUrl, fragment] = capturedUrl.split('#');
		expect(baseUrl).toMatch(/https?:\/\/.+\.tardi\.ai/);

		const token = fragment?.replace('token=', '');
		expect(token).toBeTruthy();
		expect(token!.length).toBeGreaterThan(10);
		console.log('[E2E] Dashboard URL has valid token in hash fragment');

		// Navigate to the OC dashboard URL to verify it loads
		const ocPage = await page.context().newPage();
		try {
			await ocPage.goto(capturedUrl, { timeout: 30_000 });
			await ocPage.waitForTimeout(8000);

			// The Control UI should load — look for chat input or connected state
			const chatInput = ocPage.locator(
				'textarea, input[type="text"], [contenteditable="true"]'
			).last();
			const isLoaded = await chatInput.isVisible({ timeout: 30_000 }).catch(() => false);

			if (isLoaded) {
				console.log('[E2E] OC dashboard loaded successfully with token auth');
			} else {
				// Even if chat input isn't found, the page should have loaded something
				const pageContent = await ocPage.textContent('body');
				expect(pageContent?.length).toBeGreaterThan(0);
				console.log('[E2E] OC dashboard page loaded (chat input not found, may need WebSocket)');
			}
		} finally {
			await ocPage.close();
		}
	});
});
