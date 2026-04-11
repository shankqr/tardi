import { test, expect } from '../../fixtures/auth';
import { loadAccountState } from '../../helpers/test-state';

test.describe('Settings page', () => {
	test('settings page shows account info', async ({ authedPage: page }) => {
		const account = loadAccountState('openclaw');
		await page.goto('/dashboard/settings');

		// Should show Settings heading
		await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: 10_000 });

		// Should show the user's email
		await expect(page.getByText(account.email).first()).toBeVisible({ timeout: 10_000 });

		// Should show back to dashboard link
		await expect(page.getByRole('link', { name: /back to dashboard/i })).toBeVisible();
	});

	test('back to dashboard link works', async ({ authedPage: page }) => {
		await page.goto('/dashboard/settings');

		const backLink = page.getByRole('link', { name: /back to dashboard/i });
		await expect(backLink).toBeVisible({ timeout: 10_000 });
		await backLink.click();

		await page.waitForURL('**/dashboard', { timeout: 10_000 });
	});
});
