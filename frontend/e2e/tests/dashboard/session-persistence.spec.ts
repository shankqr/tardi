import { test, expect } from '../../fixtures/auth';

test.describe('Session persistence', () => {

	test('session survives page reload', async ({ authedPage: page }) => {
		// Reload the page
		await page.reload();

		// Should still be on dashboard (not redirected to login)
		expect(page.url()).toContain('/dashboard');

		// Dashboard content should be visible (Sign out button proves we're authenticated)
		await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible({ timeout: 10_000 });
	});
});
