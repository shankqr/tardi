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

test.describe('Settings page', () => {
	test.skip(!PASSWORD, 'E2E_TEST_PASSWORD not set');

	test('settings page shows account info', async ({ page }) => {
		await login(page);
		await page.goto('/dashboard/settings');
		await page.waitForTimeout(2000);

		// Should show Settings heading
		await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: 10_000 });

		// Should show the user's email
		await expect(page.getByText(EMAIL)).toBeVisible({ timeout: 10_000 });

		// Should show back to dashboard link
		await expect(page.getByRole('link', { name: /back to dashboard/i })).toBeVisible();
	});

	test('back to dashboard link works', async ({ page }) => {
		await login(page);
		await page.goto('/dashboard/settings');
		await page.waitForTimeout(2000);

		const backLink = page.getByRole('link', { name: /back to dashboard/i });
		await expect(backLink).toBeVisible({ timeout: 10_000 });
		await backLink.click();

		await page.waitForURL('**/dashboard', { timeout: 10_000 });
	});
});
