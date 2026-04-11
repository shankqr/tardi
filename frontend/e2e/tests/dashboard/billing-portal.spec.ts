import { test, expect } from '../../fixtures/auth';

test.describe('Billing portal', () => {

	test('billing page renders plan details', async ({ authedPage: page }) => {
		await page.goto('/dashboard/billing');
		await page.waitForURL('**/dashboard/billing**', { timeout: 15_000 });

		// Billing heading should be visible
		await expect(page.getByRole('heading', { name: 'Billing' })).toBeVisible({ timeout: 10_000 });

		// Plan Details heading should be visible
		await expect(page.getByText('Plan Details')).toBeVisible({ timeout: 10_000 });

		// Back to dashboard link should exist
		await expect(page.getByText(/back to dashboard/i)).toBeVisible();

		console.log('[E2E] Billing page rendered correctly');
	});

	test('manage billing button triggers Stripe portal (if available)', async ({ authedPage: page }) => {
		await page.goto('/dashboard/billing');
		await page.waitForURL('**/dashboard/billing**', { timeout: 15_000 });

		// Wait for billing content to load
		await expect(page.getByText('Plan Details')).toBeVisible({ timeout: 10_000 });

		// Find the Manage Billing button/link — may not exist for all accounts
		const manageBillingBtn = page.getByRole('link', { name: /manage billing/i }).or(
			page.getByRole('button', { name: /manage billing/i })
		);
		const hasManageBilling = await manageBillingBtn.isVisible({ timeout: 5_000 }).catch(() => false);

		if (!hasManageBilling) {
			console.log('[E2E] No Manage Billing button — account may not have Stripe customer');
			test.skip(true, 'Manage Billing button not available for this account');
			return;
		}

		// Click and verify it triggers the billing portal API call
		const [response] = await Promise.all([
			page.waitForEvent('response', {
				predicate: (r) => r.url().includes('/billing-portal') || r.url().includes('/create-portal'),
				timeout: 15_000,
			}).catch(() => null),
			manageBillingBtn.click(),
		]);

		await page.waitForTimeout(3000);

		const currentUrl = page.url();
		const wentToStripe = currentUrl.includes('stripe.com') || currentUrl.includes('billing.stripe.com');
		const apiCalled = response !== null;

		expect(wentToStripe || apiCalled).toBeTruthy();
		console.log('[E2E] Manage Billing triggered Stripe portal');
	});
});
