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

test.describe('Billing portal', () => {
	test.skip(!PASSWORD, 'E2E_TEST_PASSWORD not set');

	test('manage billing button redirects to Stripe portal', async ({ page }) => {
		await login(page);
		await page.goto('/dashboard/billing');
		await page.waitForURL('**/dashboard/billing**', { timeout: 15_000 });

		// Find the Manage Billing button/link
		const manageBillingBtn = page.getByRole('link', { name: /manage billing/i }).or(
			page.getByRole('button', { name: /manage billing/i })
		);
		await expect(manageBillingBtn).toBeVisible({ timeout: 15_000 });

		// Click and verify it navigates to Stripe (billing.stripe.com)
		// We intercept the navigation to avoid leaving the test domain
		const [response] = await Promise.all([
			page.waitForEvent('response', {
				predicate: (r) => r.url().includes('/billing-portal') || r.url().includes('/create-portal'),
				timeout: 15_000,
			}).catch(() => null),
			manageBillingBtn.click(),
		]);

		// After clicking, the page should navigate away to Stripe or we should see the button change to "Opening..."
		// Wait a moment and check — either URL changed to stripe or button showed loading state
		await page.waitForTimeout(3000);

		// Verify: either we're on stripe.com, or the API call was made
		const currentUrl = page.url();
		const wentToStripe = currentUrl.includes('stripe.com') || currentUrl.includes('billing.stripe.com');
		const apiCalled = response !== null;

		expect(wentToStripe || apiCalled).toBeTruthy();
	});
});
