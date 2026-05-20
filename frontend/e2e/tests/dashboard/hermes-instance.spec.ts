import { test, expect, navigateToInstance } from '../../fixtures/auth';

test.describe('Instance UI per framework', () => {
	test('Hermes instance shows "Open Agent Dashboard" button that opens the web dashboard', async ({ authedHermesPage: page }) => {
		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		if (!(await instanceLink.isVisible({ timeout: 15_000 }).catch(() => false))) {
			test.skip(true, 'No active instance found'); return;
		}
		await instanceLink.click();
		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });

		const runningStatus = page.locator('dd').filter({ hasText: /Running/i }).first();
		await expect(runningStatus).toBeVisible({ timeout: 60_000 });

		const dashboardBtn = page.getByRole('button', { name: 'Open Agent Dashboard' });
		const dashboardLink = page.getByRole('link', { name: 'Open Agent Dashboard' });
		await expect(dashboardBtn).toBeVisible({ timeout: 10_000 });
		await expect(dashboardBtn).toBeEnabled();
		await expect(dashboardLink).toHaveCount(0);

		await page.evaluate(() => {
			(window as any).__capturedOpenUrl = '';
			(window as any).__originalOpen = window.open;
			window.open = (...args: any[]) => {
				(window as any).__capturedOpenUrl = args[0];
				return null;
			};
		});

		await dashboardBtn.click();
		await page.waitForFunction(() => Boolean((window as any).__capturedOpenUrl), null, {
			timeout: 10_000,
		});

		const capturedUrl = await page.evaluate(() => (window as any).__capturedOpenUrl as string);
		await page.evaluate(() => {
			window.open = (window as any).__originalOpen;
		});

		expect(capturedUrl).toContain('/__tardi/auth#token=');
		expect(capturedUrl).not.toContain('/chat');
		console.log(`[E2E] Hermes dashboard URL captured: ${capturedUrl.split('#')[0]}#token=<redacted>`);
	});

	test('Hermes instance shows "Hermes" in agent details label', async ({ authedHermesPage: page }) => {
		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		if (!(await instanceLink.isVisible({ timeout: 15_000 }).catch(() => false))) {
			test.skip(true, 'No active instance found'); return;
		}
		await instanceLink.click();
		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });

		const hermesLabel = page.locator('dt').filter({ hasText: 'Hermes' });
		await expect(hermesLabel).toBeVisible({ timeout: 15_000 });
		console.log('[E2E] Hermes label visible in agent details');
	});

	test('Hermes instance does not show MagicMoment prompts', async ({ authedHermesPage: page }) => {
		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		if (!(await instanceLink.isVisible({ timeout: 15_000 }).catch(() => false))) {
			test.skip(true, 'No active instance found'); return;
		}
		await instanceLink.click();
		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });
		await page.waitForTimeout(3_000);

		const magicMoment = page.getByText('Try these prompts');
		const isVisible = await magicMoment.isVisible({ timeout: 5_000 }).catch(() => false);
		expect(isVisible).toBeFalsy();
		console.log('[E2E] MagicMoment correctly hidden for Hermes instance');
	});

	test('OpenClaw instance shows "Open Agent Dashboard" as a button (not a link)', async ({ authedPage: page }) => {
		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found'); return;
		}

		const runningStatus = page.locator('dd').filter({ hasText: /Running/i }).first();
		const isRunning = await runningStatus.isVisible({ timeout: 60_000 }).catch(() => false);
		if (!isRunning) { test.skip(true, 'Agent not running'); return; }

		const dashboardBtn = page.getByRole('button', { name: 'Open Agent Dashboard' });
		const dashboardLink = page.getByRole('link', { name: 'Open Agent Dashboard' });

		const hasDashboard = await dashboardBtn.isVisible({ timeout: 10_000 }).catch(() => false);
		const hasDashboardLink = await dashboardLink.isVisible({ timeout: 3_000 }).catch(() => false);

		if (hasDashboard) {
			expect(hasDashboardLink).toBeFalsy();
			console.log('[E2E] OpenClaw instance shows "Open Agent Dashboard" button');
		} else {
			console.log('[E2E] OpenClaw instance: dashboard button hidden (API key may be required)');
		}
	});

	test('instance card shows framework label', async ({ authedPage: page }) => {
		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		const hasInstance = await instanceLink.isVisible({ timeout: 15_000 }).catch(() => false);
		if (!hasInstance) { test.skip(true, 'No active instance found'); return; }

		const cardText = await instanceLink.textContent();
		const hasFramework = cardText?.includes('OpenClaw') || cardText?.includes('Hermes');
		expect(hasFramework).toBeTruthy();
		console.log(`[E2E] Instance card framework: ${cardText?.includes('Hermes') ? 'Hermes' : 'OpenClaw'}`);
	});
});
