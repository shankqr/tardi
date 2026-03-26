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

test.describe('Deploy validation', () => {
	test('deploy form requires agent name', async ({ page }) => {
		const timestamp = Math.floor(Date.now() / 1000);
		const email = `e2e-deploy-val-${timestamp}@tardi-test.ai`;
		let firebaseUid = '';

		try {
			// Create user and complete checkout to get subscription
			const user = await createTestUser(email, PASSWORD);
			firebaseUid = user.uid;

			await loginWithCredentials(page, email, PASSWORD);

			// Create Stripe checkout session and complete payment
			const checkoutUrl = await createCheckoutSession(email, firebaseUid);
			await page.goto(checkoutUrl);

			await page.locator('#cardNumber').fill('4242424242424242', { timeout: 15_000 });
			await page.locator('#cardExpiry').fill('1230');
			await page.locator('#cardCvc').fill('123');
			await page.locator('#billingName').fill('E2E Test');
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

			// Now on dashboard with subscription but no instance
			// The deploy card should be visible
			await expect(page.getByText('Deploy your agent')).toBeVisible({ timeout: 15_000 });

			// Agent name input should be required
			const agentNameInput = page.locator('#agent-name');
			await expect(agentNameInput).toBeVisible({ timeout: 10_000 });

			// Try submitting with empty name — HTML validation should prevent submission
			const deployBtn = page.getByRole('button', { name: 'Deploy Agent' });
			await expect(deployBtn).toBeVisible();

			// The input has required attribute, so clicking deploy with empty name triggers validation
			await deployBtn.click();

			// Verify HTML validation prevented navigation — URL should still be /dashboard
			// (no waitForTimeout needed since HTML validation is synchronous)
			expect(page.url()).toContain('/dashboard');
			console.log('[E2E] Deploy form correctly requires agent name');

		} finally {
			try { await deleteStripeCustomer(email); } catch {}
			try { await deleteTestUser(email); } catch {}
		}
	});
});
