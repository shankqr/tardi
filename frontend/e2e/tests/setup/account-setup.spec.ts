import { test, expect } from '@playwright/test';
import { createTestUser, deleteTestUser } from '../../helpers/firebase-admin';
import { createCheckoutSession, deleteStripeCustomer } from '../../helpers/stripe';
import { waitForAgentRunning } from '../../helpers/openclaw-status';
import {
	API_URL,
	getIdToken,
	waitForSyncComplete,
	waitForInstanceActive,
	ensureInstancePage,
} from '../../helpers/journey-helpers';
import { saveTestState } from '../../helpers/test-state';

const FRAMEWORK = (process.env.E2E_FRAMEWORK as 'openclaw' | 'hermes') || 'openclaw';
const PASSWORD = process.env.E2E_TEST_PASSWORD || 'T4rd1E2e!xK9mQ2z';

test('create test account, subscribe, and deploy instance', async ({ page }) => {
	const runId = Math.floor(Date.now() / 1000) % 100000;
	const email = `e2e-setup-${runId}@tardi-test.ai`;
	let firebaseUid = '';
	let instanceId = '';

	console.log(`[Setup] Email: ${email}, Framework: ${FRAMEWORK}`);

	// ── Step 1: Create Firebase user ──
	await test.step('Create Firebase user', async () => {
		const user = await createTestUser(email, PASSWORD);
		firebaseUid = user.uid;
		console.log(`[Setup] Firebase user created: ${firebaseUid}`);
	});

	// ── Step 2: Log in ──
	await test.step('Log in', async () => {
		await page.goto('/login');
		const signInBtn = page.getByRole('button', { name: 'Sign in' });
		await expect(signInBtn).toBeVisible({ timeout: 10_000 });
		await expect(signInBtn).toBeEnabled();
		await page.waitForTimeout(1000);

		await page.locator('#email').click();
		await page.locator('#email').pressSequentially(email, { delay: 20 });
		await page.locator('#password').click();
		await page.locator('#password').pressSequentially(PASSWORD, { delay: 20 });
		await signInBtn.click();
		await page.waitForURL('**/dashboard**', { timeout: 30_000 });
		console.log('[Setup] Logged in');
	});

	// ── Step 3: Stripe checkout ──
	await test.step('Complete Stripe checkout', async () => {
		const checkoutUrl = await createCheckoutSession(email, firebaseUid);
		await page.goto(checkoutUrl);

		await page.locator('#cardNumber').fill('4242424242424242', { timeout: 15_000 });
		await page.locator('#cardExpiry').fill('1230');
		await page.locator('#cardCvc').fill('123');
		await page.locator('#billingName').fill('E2E Test Account');

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

		await expect(
			page.getByRole('heading', { name: 'Your Agent', exact: true })
		).toBeVisible({ timeout: 15_000 });
		console.log('[Setup] Subscription active');
	});

	// ── Step 4: Deploy instance ──
	await test.step('Deploy instance', async () => {
		await expect(page.getByText('Deploy your agent')).toBeVisible({ timeout: 15_000 });

		if (FRAMEWORK === 'hermes') {
			const hermesBtn = page.getByRole('button', { name: /Hermes/i });
			await expect(hermesBtn).toBeVisible({ timeout: 10_000 });
			await hermesBtn.click();
		}
		// OpenClaw is selected by default — no click needed

		const agentName = `e2e-${FRAMEWORK}-${runId}`;
		await page.locator('#agent-name').fill(agentName);
		await page.getByRole('button', { name: 'Deploy Agent' }).click();

		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });

		const url = page.url();
		const match = url.match(/\/instances\/([^/?#]+)/);
		expect(match).toBeTruthy();
		instanceId = match![1];
		console.log(`[Setup] Instance deployed: ${instanceId}`);

		// Wait for provisioning (up to 10 min)
		await waitForInstanceActive(email, PASSWORD, instanceId, 600_000);
		await ensureInstancePage(page, instanceId);
		console.log('[Setup] Instance active');
	});

	// ── Step 5: Configure API key ──
	await test.step('Set OpenRouter API key', async () => {
		const apiKey = process.env.E2E_OPENROUTER_API_KEY;
		if (!apiKey) {
			console.log('[Setup] Skipping API key — E2E_OPENROUTER_API_KEY not set');
			return;
		}

		await ensureInstancePage(page, instanceId);
		await page.locator('#openrouter-key').fill(apiKey);
		await page.getByRole('button', { name: 'Save' }).first().click();

		await waitForSyncComplete(email, PASSWORD, instanceId);
		await waitForInstanceActive(email, PASSWORD, instanceId);
		await ensureInstancePage(page, instanceId);
		await waitForAgentRunning(page);
		console.log('[Setup] API key configured, agent running');
	});

	// ── Step 6: Save state ──
	saveTestState({ email, password: PASSWORD, instanceId, framework: FRAMEWORK, firebaseUid });
	console.log('[Setup] State saved — dashboard tests can run');
});
