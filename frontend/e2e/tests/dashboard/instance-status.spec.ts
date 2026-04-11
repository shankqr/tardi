import { test, expect, navigateToInstance } from '../../fixtures/auth';

test.describe('Instance status', () => {

	test('instance card shows status badge on dashboard', async ({ authedPage: page }) => {
		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		const hasInstance = await instanceLink.isVisible({ timeout: 15_000 }).catch(() => false);
		if (!hasInstance) {
			test.skip(true, 'No active instance found');
			return;
		}

		// The instance card should show a status badge (Active, Provisioning, etc.)
		const statusBadge = page.locator('span.rounded-full').first();
		await expect(statusBadge).toBeVisible({ timeout: 10_000 });

		const statusText = await statusBadge.textContent();
		expect(statusText?.trim()).toBeTruthy();
		console.log(`[E2E] Instance status badge: ${statusText?.trim()}`);
	});

	test('instance detail page shows status badge', async ({ authedPage: page }) => {
		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Status badge should be visible on the detail page
		const statusBadge = page.locator('span.rounded-full').first();
		await expect(statusBadge).toBeVisible({ timeout: 10_000 });

		const statusText = await statusBadge.textContent();
		const validStatuses = ['Active', 'Provisioning', 'Bootstrapping', 'Installing', 'Restarting', 'Snapshotting', 'Restoring', 'Upgrading', 'Downgrading', 'Resuming', 'Suspending', 'Terminating', 'Terminated', 'Error'];
		const isValidStatus = validStatuses.some(s => statusText?.includes(s));
		expect(isValidStatus).toBeTruthy();
		console.log(`[E2E] Instance detail status: ${statusText?.trim()}`);
	});
});
