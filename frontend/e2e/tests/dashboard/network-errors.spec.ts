import { test, expect } from '../../fixtures/auth';

test.describe('Network error handling', () => {

	test('dashboard handles API failure gracefully', async ({ authedPage: page }) => {
		// Wait for dashboard to fully load first
		await expect(page.getByRole('heading', { name: 'Your Agent' }).or(page.getByRole('heading', { name: 'Deploy your agent' })).first()).toBeVisible({ timeout: 15_000 });

		// Intercept the dashboard state API and return a 500
		await page.route('**/api/dashboard/state', (route) => {
			route.fulfill({
				status: 500,
				body: JSON.stringify({ error: 'Internal Server Error' }),
			});
		});

		// Reload to trigger the intercepted API call
		await page.reload();

		// The page should still render the dashboard shell without crashing
		// Look for any dashboard UI element (nav, heading, or even an error message)
		const dashboardShell = page.getByText('Your Agent')
			.or(page.getByText('Dashboard'))
			.or(page.getByRole('button', { name: 'Sign out' }));
		await expect(dashboardShell.first()).toBeVisible({ timeout: 15_000 });

		console.log('[E2E] Dashboard handled API failure gracefully');
	});

	test('billing page handles missing subscription gracefully', async ({ authedPage: page }) => {
		// Intercept billing/subscription API to return empty
		await page.route('**/api/dashboard/state', (route) => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ instances: [], subscription: null }),
			});
		});

		await page.goto('/dashboard/billing');

		// Page should render billing shell — look for the heading or a "no subscription" state
		const billingContent = page.getByRole('heading', { name: 'Billing' })
			.or(page.getByText(/no.*subscription|subscribe|plan/i));
		await expect(billingContent.first()).toBeVisible({ timeout: 15_000 });

		console.log('[E2E] Billing page handled missing subscription gracefully');
	});
});
