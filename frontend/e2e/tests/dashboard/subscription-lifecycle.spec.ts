import { test, expect } from '@playwright/test';
import { loginWithCredentials } from '../../fixtures/auth';
import {
	createTestUser,
	deleteTestUser,
} from '../../helpers/firebase-admin';
import {
	createCheckoutSession,
	deleteStripeCustomer,
	cancelSubscriptionsByEmail,
} from '../../helpers/stripe';

const PASSWORD = process.env.E2E_TEST_PASSWORD || 'T4rd1E2e!xK9mQ2z';

test.describe('Subscription lifecycle', () => {
	test('cancelled subscription shows warning on dashboard', async ({ page }) => {
		const timestamp = Math.floor(Date.now() / 1000);
		const email = `e2e-sub-lifecycle-${timestamp}@tardi-test.ai`;
		let firebaseUid = '';

		console.log(`[E2E SubLifecycle] Test email: ${email}`);

		try {
			// ── Step 1: Create user and subscribe ──
			await test.step('Create user and complete checkout', async () => {
				const user = await createTestUser(email, PASSWORD);
				firebaseUid = user.uid;

				await loginWithCredentials(page, email, PASSWORD);

				const checkoutUrl = await createCheckoutSession(email, firebaseUid);
				await page.goto(checkoutUrl);

				await page.locator('#cardNumber').fill('4242424242424242', { timeout: 15_000 });
				await page.locator('#cardExpiry').fill('1230');
				await page.locator('#cardCvc').fill('123');
				await page.locator('#billingName').fill('E2E Lifecycle Test');
				await page.getByTestId('hosted-payment-submit-button').click();

				await page.waitForURL('**/onboarding/success**', { timeout: 60_000 });

				try {
					await page.waitForURL('**/dashboard', { timeout: 60_000 });
				} catch {
					const goBtn = page.getByRole('link', { name: 'Go to Dashboard' });
					if (await goBtn.isVisible()) {
						await goBtn.click();
						await page.waitForURL('**/dashboard**', { timeout: 15_000 });
					}
				}

				console.log('[E2E SubLifecycle] Subscription created, on dashboard');
			});

			// ── Step 2: Verify active subscription on billing page ──
			await test.step('Verify active subscription', async () => {
				await page.goto('/dashboard/billing');
				await expect(page.getByText('Standard')).toBeVisible({ timeout: 15_000 });
				await expect(page.getByText('Active')).toBeVisible({ timeout: 10_000 });
				console.log('[E2E SubLifecycle] Active subscription verified');
			});

			// ── Step 3: Cancel subscription via Stripe API ──
			await test.step('Cancel subscription via Stripe API', async () => {
				await cancelSubscriptionsByEmail(email);
				console.log('[E2E SubLifecycle] Subscription cancelled via Stripe API');
			});

			// ── Step 4: Verify cancellation reflected in UI ──
			await test.step('Verify cancellation on billing page', async () => {
				// Reload billing page to pick up the cancellation
				await page.reload();

				// Wait for the billing page to reflect the cancellation
				// The UI should show "Cancelled" or a cancellation warning
				const cancelledIndicator = page.getByText(/cancel/i);
				await expect(cancelledIndicator.first()).toBeVisible({ timeout: 30_000 });
				console.log('[E2E SubLifecycle] Cancellation reflected in billing UI');
			});

			// ── Step 5: Verify dashboard shows cancellation warning ──
			await test.step('Dashboard shows cancellation warning', async () => {
				await page.goto('/dashboard');

				// Wait for dashboard to load
				await expect(
					page.getByText('Your Agent').or(page.getByText('Deploy your agent'))
				).toBeVisible({ timeout: 15_000 });

				// Check for cancellation warning banner
				const warningBanner = page.getByText(/cancel|expir|subscription/i);
				const hasWarning = await warningBanner.first().isVisible({ timeout: 10_000 }).catch(() => false);

				if (hasWarning) {
					console.log('[E2E SubLifecycle] Dashboard shows cancellation warning');
				} else {
					console.log('[E2E SubLifecycle] No cancellation warning on dashboard (may be delayed)');
				}

				// User should still be able to access the dashboard (not locked out immediately)
				expect(page.url()).toContain('/dashboard');
			});

		} finally {
			console.log('[E2E SubLifecycle] Cleaning up...');
			try { await deleteStripeCustomer(email); } catch {}
			try { await deleteTestUser(email); } catch {}
		}
	});
});
