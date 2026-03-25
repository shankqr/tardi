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

	test('billing page renders plan details', async ({ page }) => {
		await login(page);
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

	test('manage billing button triggers Stripe portal (if available)', async ({ page }) => {
		await login(page);
		await page.goto('/dashboard/billing');
		await page.waitForURL('**/dashboard/billing**', { timeout: 15_000 });
		await page.waitForTimeout(3000);

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
