import { test, expect } from '@playwright/test';

const EMAIL = process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+persistent@gmail.com';
const PASSWORD = process.env.E2E_TEST_PASSWORD || '';

test.describe('Session persistence', () => {
	test.skip(!PASSWORD, 'E2E_TEST_PASSWORD not set');

	test('session survives page reload', async ({ page }) => {
		// Login
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

		// Reload the page
		await page.reload();
		await page.waitForTimeout(3000);

		// Should still be on dashboard (not redirected to login)
		expect(page.url()).toContain('/dashboard');

		// Dashboard content should be visible (Sign out button proves we're authenticated)
		await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible({ timeout: 10_000 });
	});
});
