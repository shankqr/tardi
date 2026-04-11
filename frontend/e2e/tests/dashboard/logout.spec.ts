import { test, expect } from '../../fixtures/auth';

test.describe('Logout', () => {

	test('logout redirects to homepage and clears session', async ({ authedPage: page }) => {
		// Click Sign out
		const signOutBtn = page.getByRole('button', { name: 'Sign out' });
		await expect(signOutBtn).toBeVisible({ timeout: 10_000 });
		await signOutBtn.click();

		// Should redirect to homepage
		await page.waitForURL('**/', { timeout: 15_000 });

		// Verify we're logged out — navigating to /dashboard should not show dashboard content
		await page.goto('/dashboard');
		await expect(page.getByRole('button', { name: 'Sign out' })).not.toBeVisible({ timeout: 15_000 });
	});
});
