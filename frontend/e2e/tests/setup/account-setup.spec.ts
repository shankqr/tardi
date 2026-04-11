import { test, expect } from '@playwright/test';
import { createTestUser } from '../../helpers/firebase-admin';
import { createCheckoutSession } from '../../helpers/stripe';
import { waitForAgentRunning } from '../../helpers/openclaw-status';
import {
	API_URL,
	getIdToken,
	waitForSyncComplete,
	waitForInstanceActive,
	ensureInstancePage,
} from '../../helpers/journey-helpers';
import { saveTestState, type AccountState, type TestState } from '../../helpers/test-state';

const PASSWORD = process.env.E2E_TEST_PASSWORD || 'T4rd1E2e!xK9mQ2z';
const RUN_ID = Math.floor(Date.now() / 1000) % 100000;

/**
 * Sign up a user, complete Stripe checkout, deploy an instance, and configure the API key.
 * Returns the account state for persistence.
 */
async function setupAccount(
	page: import('@playwright/test').Page,
	framework: 'openclaw' | 'hermes',
	step: typeof test.step
): Promise<AccountState> {
	const email = `e2e-${framework}-${RUN_ID}@tardi-test.ai`;
	let firebaseUid = '';
	let instanceId = '';

	console.log(`[Setup:${framework}] Email: ${email}`);

	await step(`Create Firebase user (${framework})`, async () => {
		const user = await createTestUser(email, PASSWORD);
		firebaseUid = user.uid;
		console.log(`[Setup:${framework}] Firebase user created: ${firebaseUid}`);
	});

	await step(`Log in (${framework})`, async () => {
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
		console.log(`[Setup:${framework}] Logged in`);
	});

	await step(`Complete Stripe checkout (${framework})`, async () => {
		const checkoutUrl = await createCheckoutSession(email, firebaseUid);
		await page.goto(checkoutUrl);

		await page.locator('#cardNumber').fill('4242424242424242', { timeout: 15_000 });
		await page.locator('#cardExpiry').fill('1230');
		await page.locator('#cardCvc').fill('123');
		await page.locator('#billingName').fill(`E2E ${framework}`);

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
		console.log(`[Setup:${framework}] Subscription active`);
	});

	await step(`Deploy ${framework} instance`, async () => {
		await expect(page.getByText('Deploy your agent')).toBeVisible({ timeout: 15_000 });

		if (framework === 'hermes') {
			const hermesBtn = page.getByRole('button', { name: /Hermes/i });
			await expect(hermesBtn).toBeVisible({ timeout: 10_000 });
			await hermesBtn.click();
		}

		const agentName = `e2e-${framework}-${RUN_ID}`;
		await page.locator('#agent-name').fill(agentName);
		await page.getByRole('button', { name: 'Deploy Agent' }).click();

		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });

		const url = page.url();
		const match = url.match(/\/instances\/([^/?#]+)/);
		expect(match).toBeTruthy();
		instanceId = match![1];
		console.log(`[Setup:${framework}] Instance deployed: ${instanceId}`);

		await waitForInstanceActive(email, PASSWORD, instanceId, 600_000);
		await ensureInstancePage(page, instanceId);
		console.log(`[Setup:${framework}] Instance active`);
	});

	await step(`Set OpenRouter API key (${framework})`, async () => {
		const apiKey = process.env.E2E_OPENROUTER_API_KEY;
		if (!apiKey) {
			console.log(`[Setup:${framework}] Skipping API key — E2E_OPENROUTER_API_KEY not set`);
			return;
		}

		await ensureInstancePage(page, instanceId);
		await page.locator('#openrouter-key').fill(apiKey);
		await page.getByRole('button', { name: 'Save' }).first().click();

		await waitForSyncComplete(email, PASSWORD, instanceId);
		await waitForInstanceActive(email, PASSWORD, instanceId);
		await ensureInstancePage(page, instanceId);
		await waitForAgentRunning(page);
		console.log(`[Setup:${framework}] API key configured, agent running`);
	});

	// Log out so next account can log in
	await step(`Log out (${framework})`, async () => {
		const signOutBtn = page.getByRole('button', { name: 'Sign out' });
		if (await signOutBtn.isVisible({ timeout: 5_000 }).catch(() => false)) {
			await signOutBtn.click();
			await page.waitForURL('**/', { timeout: 15_000 });
		}
		// Clear cookies/storage for clean slate
		await page.context().clearCookies();
		console.log(`[Setup:${framework}] Logged out`);
	});

	return { email, password: PASSWORD, instanceId, firebaseUid };
}

test('create OpenClaw + Hermes accounts and deploy instances', async ({ page }) => {
	const state: TestState = {} as TestState;

	// Deploy OpenClaw first
	state.openclaw = await setupAccount(page, 'openclaw', test.step);

	// Deploy Hermes second
	state.hermes = await setupAccount(page, 'hermes', test.step);

	saveTestState(state);
	console.log('[Setup] Both accounts created — dashboard tests can run');
});
