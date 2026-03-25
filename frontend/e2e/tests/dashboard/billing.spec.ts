import { test, expect } from '@playwright/test';
import { loginWithCredentials } from '../../fixtures/auth';
import {
	createTestUser,
	deleteTestUser,
} from '../../helpers/firebase-admin';
import {
	createCheckoutSession,
	deleteStripeCustomer,
} from '../../helpers/stripe';

const PASSWORD = process.env.E2E_TEST_PASSWORD || 'T4rd1E2e!xK9mQ2z';

test('Billing page shows plan, status, and manage billing button', async ({
	page,
}) => {
	const timestamp = Math.floor(Date.now() / 1000);
	const email = `e2e-billing-${timestamp}@tardi-test.ai`;
	let firebaseUid = '';

	console.log(`[E2E Billing] Test email: ${email}`);

	try {
		// ── Step 1: Create test user via Firebase Admin ──
		await test.step('Create test user', async () => {
			const user = await createTestUser(email, PASSWORD);
			firebaseUid = user.uid;
			console.log(`[E2E Billing] Firebase UID: ${firebaseUid}`);
		});

		// ── Step 2: Login and complete Stripe checkout ──
		await test.step('Login and complete Stripe checkout', async () => {
			await loginWithCredentials(page, email, PASSWORD);

			console.log('[E2E Billing] Creating Stripe Checkout Session...');
			const checkoutUrl = await createCheckoutSession(email, firebaseUid);
			await page.goto(checkoutUrl);

			// Fill Stripe hosted checkout form with test card
			await page.locator('#cardNumber').fill('4242424242424242', {
				timeout: 15_000,
			});
			await page.locator('#cardExpiry').fill('1230');
			await page.locator('#cardCvc').fill('123');
			await page.locator('#billingName').fill('E2E Test');

			// Submit payment
			await page.getByTestId('hosted-payment-submit-button').click();

			// Wait for redirect back to success page
			await page.waitForURL('**/onboarding/success**', { timeout: 60_000 });
			console.log('[E2E Billing] Checkout succeeded, on success page');

			// Success page polls for subscription webhook, then redirects to /dashboard
			try {
				await page.waitForURL('**/dashboard', { timeout: 60_000 });
			} catch {
				const goBtn = page.getByRole('link', { name: 'Go to Dashboard' });
				if (await goBtn.isVisible()) {
					await goBtn.click();
					await page.waitForURL('**/dashboard**', { timeout: 15_000 });
				}
			}

			console.log('[E2E Billing] Reached dashboard after checkout');
		});

		// ── Step 3: Navigate to billing page and verify plan details ──
		await test.step('Billing page shows plan and status', async () => {
			await page.goto('/dashboard/billing');
			await page.waitForURL('**/dashboard/billing**', { timeout: 15_000 });

			// Verify plan name
			await expect(page.getByText('Standard')).toBeVisible({ timeout: 15_000 });

			// Verify active status
			await expect(page.getByText('Active')).toBeVisible({ timeout: 10_000 });

			// Verify price
			await expect(page.getByText('$29')).toBeVisible({ timeout: 10_000 });

			console.log('[E2E Billing] Plan name, status, and price verified');
		});

		// ── Step 4: Verify Manage Billing button exists ──
		await test.step('Manage billing button is visible', async () => {
			const manageBillingBtn = page.getByRole('link', { name: /manage billing/i }).or(
				page.getByRole('button', { name: /manage billing/i })
			);
			await expect(manageBillingBtn).toBeVisible({ timeout: 10_000 });

			console.log('[E2E Billing] Manage Billing button verified');
		});
	} finally {
		console.log('[E2E Billing] Cleaning up test resources...');

		try {
			await deleteStripeCustomer(email);
			console.log('[E2E Billing] Stripe customer deleted');
		} catch (err) {
			console.error('[E2E Billing] Stripe cleanup failed:', err);
		}

		try {
			await deleteTestUser(email);
			console.log('[E2E Billing] Firebase user deleted');
		} catch (err) {
			console.error('[E2E Billing] Firebase cleanup failed:', err);
		}
	}
});
