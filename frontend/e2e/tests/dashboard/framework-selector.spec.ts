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

test.describe('Framework selector on deploy page', () => {
	test('deploy form shows framework selector with OpenClaw and Hermes', async ({ page }) => {
		const timestamp = Math.floor(Date.now() / 1000);
		const email = `e2e-fw-select-${timestamp}@tardi-test.ai`;
		let firebaseUid = '';

		try {
			const user = await createTestUser(email, PASSWORD);
			firebaseUid = user.uid;

			await loginWithCredentials(page, email, PASSWORD);

			// Complete checkout to get subscription
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

			// Deploy card should be visible
			await expect(page.getByText('Deploy your agent')).toBeVisible({ timeout: 15_000 });

			// Framework selector should show two options
			const openclawBtn = page.getByRole('button', { name: /OpenClaw/i });
			const hermesBtn = page.getByRole('button', { name: /Hermes/i });

			await expect(openclawBtn).toBeVisible({ timeout: 10_000 });
			await expect(hermesBtn).toBeVisible({ timeout: 10_000 });

			// OpenClaw should be selected by default (has the active border class)
			await expect(openclawBtn).toHaveClass(/border-gray-900|border-white/);
			console.log('[E2E] Framework selector visible with OpenClaw selected by default');

			// Click Hermes and verify it becomes selected
			await hermesBtn.click();
			await expect(hermesBtn).toHaveClass(/border-gray-900|border-white/);
			console.log('[E2E] Hermes framework selectable');

			// Switch back to OpenClaw
			await openclawBtn.click();
			await expect(openclawBtn).toHaveClass(/border-gray-900|border-white/);
			console.log('[E2E] Can switch back to OpenClaw');

			// Verify framework descriptions
			await expect(page.getByText('Web dashboard & code execution')).toBeVisible();
			await expect(page.getByText('Self-improving agent with skills & memory')).toBeVisible();
			console.log('[E2E] Framework descriptions visible');

		} finally {
			try { await deleteStripeCustomer(email); } catch {}
			try { await deleteTestUser(email); } catch {}
		}
	});

	test('deploying with Hermes framework selected sends correct framework', async ({ page }) => {
		const timestamp = Math.floor(Date.now() / 1000);
		const email = `e2e-fw-hermes-${timestamp}@tardi-test.ai`;
		let firebaseUid = '';

		try {
			const user = await createTestUser(email, PASSWORD);
			firebaseUid = user.uid;

			await loginWithCredentials(page, email, PASSWORD);

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

			await expect(page.getByText('Deploy your agent')).toBeVisible({ timeout: 15_000 });

			// Select Hermes framework
			await page.getByRole('button', { name: /Hermes/i }).click();

			// Fill agent name
			const agentName = `e2e-hermes-${Date.now()}`;
			await page.locator('#agent-name').fill(agentName);

			// Intercept the API call to verify framework is sent
			const [request] = await Promise.all([
				page.waitForRequest(req => req.url().includes('/api/instances') && req.method() === 'POST'),
				page.getByRole('button', { name: 'Deploy Agent' }).click(),
			]);

			const body = JSON.parse(request.postData() || '{}');
			expect(body.framework).toBe('hermes');
			console.log(`[E2E] Deploy request sent with framework: ${body.framework}`);

			// Wait for redirect to instance page
			await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
			console.log('[E2E] Hermes instance deploy initiated');

		} finally {
			// Clean up: delete any instances, Stripe, Firebase
			try {
				const { getIdToken, deleteExistingInstances } = await import('../../helpers/journey-helpers');
				await deleteExistingInstances(email, PASSWORD);
			} catch {}
			try { await deleteStripeCustomer(email); } catch {}
			try { await deleteTestUser(email); } catch {}
		}
	});
});
