import { test, expect, type Page } from '@playwright/test';

const EMAIL = process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+1@gmail.com';
const PASSWORD = process.env.E2E_PERSISTENT_PASSWORD || process.env.E2E_TEST_PASSWORD || '';

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

test.describe('Network error handling', () => {
	test.skip(!PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');

	test('dashboard handles API failure gracefully', async ({ page }) => {
		await login(page);
		await page.waitForTimeout(3000);

		// Intercept the dashboard state API and return a 500
		await page.route('**/api/dashboard/state', (route) => {
			route.fulfill({
				status: 500,
				body: JSON.stringify({ error: 'Internal Server Error' }),
			});
		});

		// Reload to trigger the intercepted API call
		await page.reload();
		await page.waitForTimeout(5000);

		// The page should still render without crashing
		// It may show an error message or just show empty state
		const pageContent = await page.textContent('body');
		expect(pageContent).toBeTruthy();

		// Page should not show a white screen or unhandled error
		const hasContent = (pageContent?.length || 0) > 50;
		expect(hasContent).toBeTruthy();
		console.log('[E2E] Dashboard handled API failure gracefully');
	});

	test('billing page handles missing subscription gracefully', async ({ page }) => {
		await login(page);

		// Intercept billing/subscription API to return empty
		await page.route('**/api/dashboard/state', (route) => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({ instances: [], subscription: null }),
			});
		});

		await page.goto('/dashboard/billing');
		await page.waitForTimeout(5000);

		// Page should render without crashing
		const pageContent = await page.textContent('body');
		expect(pageContent).toBeTruthy();
		expect((pageContent?.length || 0) > 50).toBeTruthy();
		console.log('[E2E] Billing page handled missing subscription gracefully');
	});
});
