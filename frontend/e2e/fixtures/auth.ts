import { test as base, expect, type Page } from '@playwright/test';
import { loadAccountState } from '../helpers/test-state';

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
 */
export async function navigateToInstance(page: Page): Promise<boolean> {
	const loading = page.getByText('Loading dashboard...');
	await loading.waitFor({ state: 'hidden', timeout: 30_000 }).catch(() => {});

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

async function waitForDashboard(page: Page): Promise<void> {
	const loading = page.getByText('Loading dashboard...');
	await loading.waitFor({ state: 'hidden', timeout: 30_000 }).catch(() => {});
	await expect(
		page.getByRole('heading', { name: 'Your Agent' }).or(page.getByText('Deploy your agent').first()).first()
	).toBeVisible({ timeout: 15_000 });
}

/**
 * Auth fixture — logs into the OpenClaw test account (default for generic dashboard tests).
 */
export const test = base.extend<{ authedPage: Page; authedHermesPage: Page }>({
	authedPage: async ({ page }, use) => {
		const account = loadAccountState('openclaw');
		await loginWithCredentials(page, account.email, account.password);
		await waitForDashboard(page);
		await use(page);
	},
	authedHermesPage: async ({ page }, use) => {
		const account = loadAccountState('hermes');
		await loginWithCredentials(page, account.email, account.password);
		await waitForDashboard(page);
		await use(page);
	},
});

export { expect };
