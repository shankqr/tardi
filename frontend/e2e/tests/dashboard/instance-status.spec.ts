import { test, expect, type Page } from '@playwright/test';

const EMAIL = process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+persistent@gmail.com';
const PASSWORD = process.env.E2E_TEST_PASSWORD || '';

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

test.describe('Instance status', () => {
	test.skip(!PASSWORD, 'E2E_TEST_PASSWORD not set');

	test('instance card shows status badge on dashboard', async ({ page }) => {
		await login(page);
		await page.waitForTimeout(5000);

		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		const hasInstance = await instanceLink.isVisible({ timeout: 10_000 }).catch(() => false);
		if (!hasInstance) {
			test.skip(true, 'No active instance found');
			return;
		}

		// The instance card should show a status badge (Active, Provisioning, etc.)
		// StatusBadge renders as a span with rounded-full class
		const statusBadge = page.locator('span.rounded-full').first();
		await expect(statusBadge).toBeVisible({ timeout: 10_000 });

		const statusText = await statusBadge.textContent();
		expect(statusText?.trim()).toBeTruthy();
		console.log(`[E2E] Instance status badge: ${statusText?.trim()}`);
	});

	test('instance detail page shows status badge', async ({ page }) => {
		await login(page);
		await page.waitForTimeout(5000);

		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		const hasInstance = await instanceLink.isVisible({ timeout: 10_000 }).catch(() => false);
		if (!hasInstance) {
			test.skip(true, 'No active instance found');
			return;
		}

		await instanceLink.click();
		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });

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
