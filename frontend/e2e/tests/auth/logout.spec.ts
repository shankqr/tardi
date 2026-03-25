import { test, expect } from '@playwright/test';

const EMAIL = process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+persistent@gmail.com';
const PASSWORD = process.env.E2E_TEST_PASSWORD || '';

test.describe('Logout', () => {
	test.skip(!PASSWORD, 'E2E_TEST_PASSWORD not set');

	test('logout redirects to homepage and clears session', async ({ page }) => {
		// Login first
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

		// Click Sign out
		const signOutBtn = page.getByRole('button', { name: 'Sign out' });
		await expect(signOutBtn).toBeVisible({ timeout: 10_000 });
		await signOutBtn.click();

		// Should redirect to homepage
		await page.waitForURL('**/', { timeout: 15_000 });

		// Verify we're logged out — navigating to /dashboard should not show dashboard content
		await page.goto('/dashboard');
		await page.waitForTimeout(5000);
		await expect(page.getByRole('heading', { name: 'Dashboard' })).not.toBeVisible();
	});
});
