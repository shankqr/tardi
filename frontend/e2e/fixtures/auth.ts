import { test as base, expect, type Page } from '@playwright/test';

export const PERSISTENT_EMAIL = process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+1@gmail.com';
export const PERSISTENT_PASSWORD =
	process.env.E2E_PERSISTENT_PASSWORD || process.env.E2E_TEST_PASSWORD || '';

/**
 * Login helper using pressSequentially to work around Svelte 5 hydration issues.
 * Exported for tests that need custom login flows (fresh accounts, etc.)
 */
export async function loginWithCredentials(
	page: Page,
	email: string,
	password: string
): Promise<void> {
	await page.goto('/login');
	const signInBtn = page.getByRole('button', { name: 'Sign in' });
	await expect(signInBtn).toBeVisible({ timeout: 10_000 });
	await expect(signInBtn).toBeEnabled();
	await page.waitForTimeout(1000);

	await page.locator('#email').click();
	await page.locator('#email').pressSequentially(email, { delay: 20 });
	await page.locator('#password').click();
	await page.locator('#password').pressSequentially(password, { delay: 20 });
	await signInBtn.click();
	await page.waitForURL('**/dashboard**', { timeout: 30_000 });
}

/**
 * Navigate to the first instance detail page from the dashboard.
 * Returns false if no instance is found (caller should skip the test).
 *
 * Waits for the dashboard API polling to populate the instance list before
 * checking for instance links — the dashboard loads a shell first, then
 * fetches state via /api/dashboard/state which populates instance cards.
 */
export async function navigateToInstance(page: Page): Promise<boolean> {
	// Wait for the dashboard API polling to finish — the page shows "Loading dashboard..."
	// until /api/dashboard/state returns, then renders either instance cards or deploy form.
	// First wait for the loading indicator to disappear, then check for content.
	const loading = page.getByText('Loading dashboard...');
	await loading.waitFor({ state: 'hidden', timeout: 30_000 }).catch(() => {});

	// Now wait for actual dashboard content (use heading role to avoid matching description text)
	await expect(
		page.getByRole('heading', { name: 'Your Agent' }).or(page.getByRole('heading', { name: 'Deploy your agent' })).first()
	).toBeVisible({ timeout: 15_000 });

	const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
	const hasInstance = await instanceLink
		.isVisible({ timeout: 15_000 })
		.catch(() => false);
	if (!hasInstance) return false;
	await instanceLink.click();
	await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
	await expect(page.getByText('Agent Details')).toBeVisible({
		timeout: 30_000,
	});
	return true;
}

/**
 * Auth fixture that logs into the persistent test account.
 * Use for dashboard tests that need a pre-authenticated session with an active instance.
 *
 * Usage:
 *   import { test, expect } from '../../fixtures/auth';
 *   test('my test', async ({ authedPage }) => { ... });
 */
export const test = base.extend<{ authedPage: Page }>({
	authedPage: async ({ page }, use) => {
		if (!PERSISTENT_PASSWORD) {
			throw new Error(
				'E2E_PERSISTENT_PASSWORD not set — use test.skip() at describe level'
			);
		}
		await loginWithCredentials(page, PERSISTENT_EMAIL, PERSISTENT_PASSWORD);
		// Wait for dashboard API polling to finish loading
		const loading = page.getByText('Loading dashboard...');
		await loading.waitFor({ state: 'hidden', timeout: 30_000 }).catch(() => {});
		await expect(
			page.getByRole('heading', { name: 'Your Agent' }).or(page.getByText('Deploy your agent').first()).first()
		).toBeVisible({ timeout: 15_000 });
		await use(page);
	},
});

export { expect };
