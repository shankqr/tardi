import { test, expect } from '@playwright/test';
import {
	verifyUserEmail,
	getTestUserByEmail,
	deleteTestUser,
} from '../../helpers/firebase-admin';
import {
	createCheckoutSession,
	deleteStripeCustomer,
} from '../../helpers/stripe';
import { waitForAgentRunning } from '../../helpers/openclaw-status';
import {
	API_URL,
	getIdToken,
	waitForSyncComplete,
	waitForInstanceActive,
	ensureInstancePage,
} from '../../helpers/journey-helpers';

async function cleanup(email: string, password: string, instanceId: string) {
	console.log('[E2E] Cleaning up Hermes test resources...');

	if (instanceId) {
		try {
			const idToken = await getIdToken(email, password);
			await fetch(`${API_URL}/api/instances/${instanceId}`, {
				method: 'DELETE',
				headers: { Authorization: `Bearer ${idToken}` },
			});
			console.log('[E2E] Instance deleted');
		} catch (err) {
			console.error('[E2E] Failed to delete instance:', err);
		}
	}

	try {
		await deleteStripeCustomer(email);
		console.log('[E2E] Stripe customer deleted');
	} catch (err) {
		console.error('[E2E] Stripe cleanup failed:', err);
	}

	try {
		await deleteTestUser(email);
		console.log('[E2E] Firebase user deleted');
	} catch (err) {
		console.error('[E2E] Firebase cleanup failed:', err);
	}
}

test('Hermes journey: signup -> deploy Hermes -> configure -> chat', async ({
	page,
}) => {
	const runNumber = Math.floor(Date.now() / 1000) % 100000;
	const email = `clawmyway+hm${runNumber}@gmail.com`;
	const password = process.env.E2E_TEST_PASSWORD || 'T4rd1E2e!xK9mQ2z';
	let firebaseUid = '';
	let instanceId = '';

	console.log(`[E2E] Hermes test email: ${email}`);

	try {

	// -- Step 1: Sign up --
	await test.step('Sign up with email and password', async () => {
		await page.goto('/signup');

		const createBtn = page.getByRole('button', { name: 'Create account' });
		await expect(createBtn).toBeVisible({ timeout: 10_000 });
		await expect(createBtn).toBeEnabled();
		await page.waitForTimeout(1000);

		await page.locator('#email').click();
		await page.locator('#email').pressSequentially(email, { delay: 20 });
		await page.locator('#password').click();
		await page.locator('#password').pressSequentially(password, { delay: 20 });
		await page.locator('#confirm-password').click();
		await page.locator('#confirm-password').pressSequentially(password, { delay: 20 });

		await createBtn.click();
		await page.waitForURL('**/verify-email**', { timeout: 30_000 });

		let user = await getTestUserByEmail(email);
		for (let i = 0; i < 5 && !user; i++) {
			await new Promise((r) => setTimeout(r, 2000));
			user = await getTestUserByEmail(email);
		}
		expect(user).toBeTruthy();
		firebaseUid = user!.uid;
		console.log(`[E2E] Firebase UID: ${firebaseUid}`);
	});

	// -- Step 2: Verify email --
	await test.step('Verify email and redirect to checkout', async () => {
		await verifyUserEmail(firebaseUid);
		await page.waitForURL('**/onboarding/checkout**', { timeout: 30_000 });
	});

	// -- Step 3: Stripe checkout --
	await test.step('Complete Stripe checkout', async () => {
		const checkoutUrl = await createCheckoutSession(email, firebaseUid);
		await page.goto(checkoutUrl);

		await page.locator('#cardNumber').fill('4242424242424242', { timeout: 15_000 });
		await page.locator('#cardExpiry').fill('1230');
		await page.locator('#cardCvc').fill('123');
		await page.locator('#billingName').fill('E2E Hermes Test');

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

		await expect(page.getByRole('heading', { name: 'Your Agent', exact: true })).toBeVisible({
			timeout: 15_000,
		});
	});

	// -- Step 4: Deploy Hermes agent --
	await test.step('Deploy Hermes agent', async () => {
		await expect(page.getByText('Deploy your agent')).toBeVisible({ timeout: 15_000 });

		// Select Hermes framework
		const hermesBtn = page.getByRole('button', { name: /Hermes/i });
		await expect(hermesBtn).toBeVisible({ timeout: 10_000 });
		await hermesBtn.click();

		// Verify Hermes is selected
		await expect(hermesBtn).toHaveClass(/border-gray-900|border-white/);
		console.log('[E2E] Hermes framework selected');

		// Fill agent name and deploy
		const agentName = `e2e-hermes-${Date.now()}`;
		await page.locator('#agent-name').fill(agentName);
		await page.getByRole('button', { name: 'Deploy Agent' }).click();

		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });

		// Extract instance ID
		const url = page.url();
		const match = url.match(/\/instances\/([^/?#]+)/);
		expect(match).toBeTruthy();
		instanceId = match![1];
		console.log(`[E2E] Hermes Instance ID: ${instanceId}`);

		// Wait for provisioning to complete (up to 10 min)
		console.log('[E2E] Waiting for Hermes provisioning...');
		await waitForInstanceActive(email, password, instanceId, 600_000);

		// Recover UI
		await ensureInstancePage(page, instanceId);
		console.log('[E2E] Hermes agent is active');
	});

	// -- Step 5: Verify Hermes-specific UI elements --
	await test.step('Verify Hermes-specific UI on instance page', async () => {
		// Should show "Hermes" label in agent details
		const hermesLabel = page.locator('dt').filter({ hasText: 'Hermes' });
		await expect(hermesLabel).toBeVisible({ timeout: 15_000 });
		console.log('[E2E] "Hermes" label visible in agent details');

		// Instance card should show framework on the dashboard
		await page.goto('/dashboard');
		const loading = page.getByText('Loading dashboard...');
		await loading.waitFor({ state: 'hidden', timeout: 30_000 }).catch(() => {});

		const instanceCard = page.locator('a[href*="/dashboard/instances/"]').first();
		await expect(instanceCard).toBeVisible({ timeout: 15_000 });
		const cardText = await instanceCard.textContent();
		expect(cardText).toContain('Hermes');
		console.log('[E2E] Instance card shows Hermes framework');

		// Navigate back to instance
		await instanceCard.click();
		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
	});

	// -- Step 6: Set OpenRouter API key --
	await test.step('Set OpenRouter API key', async () => {
		const apiKey = process.env.E2E_OPENROUTER_API_KEY;
		if (!apiKey) {
			console.log('[E2E] Skipping: E2E_OPENROUTER_API_KEY not set');
			return;
		}

		await ensureInstancePage(page, instanceId);

		await page.locator('#openrouter-key').fill(apiKey);
		await page.getByRole('button', { name: 'Save' }).first().click();

		// Poll sync status via API
		await waitForSyncComplete(email, password, instanceId);
		await waitForInstanceActive(email, password, instanceId);

		await ensureInstancePage(page, instanceId);
		await waitForAgentRunning(page);
		console.log('[E2E] Hermes API key saved and synced');
	});

	// -- Step 7: Test Hermes chat --
	await test.step('Chat with Hermes agent', async () => {
		if (!process.env.E2E_OPENROUTER_API_KEY) {
			console.log('[E2E] Skipping: E2E_OPENROUTER_API_KEY not set');
			return;
		}

		// Wait for agent to be running
		await waitForAgentRunning(page);

		// Click "Chat with Agent" link
		const chatLink = page.getByRole('link', { name: 'Chat with Agent' });
		await expect(chatLink).toBeVisible({ timeout: 30_000 });
		await chatLink.click();

		// Verify chat page loads
		await page.waitForURL(`**/chat`, { timeout: 15_000 });
		await expect(page.getByText('Hermes Agent Chat')).toBeVisible({ timeout: 30_000 });

		// Send a test message
		const messageInput = page.locator('input[placeholder="Type a message..."]');
		await expect(messageInput).toBeVisible({ timeout: 10_000 });
		await messageInput.fill('Say hello in exactly 5 words');

		const sendBtn = page.getByRole('button', { name: 'Send' });
		await sendBtn.click();

		// Verify user message appears
		await expect(page.getByText('Say hello in exactly 5 words')).toBeVisible({ timeout: 5_000 });

		// Wait for response (up to 60s)
		// Look for any new pre element (assistant response)
		const assistantMessages = page.locator('pre');
		const initialCount = await assistantMessages.count();

		// Wait for a new message to appear
		let responseReceived = false;
		const startTime = Date.now();
		while (Date.now() - startTime < 60_000) {
			const currentCount = await assistantMessages.count();
			if (currentCount > initialCount) {
				responseReceived = true;
				break;
			}
			await page.waitForTimeout(2_000);
		}

		expect(responseReceived).toBeTruthy();
		console.log('[E2E] Hermes agent responded to chat message');
	});

	} finally {
		await cleanup(email, password, instanceId);
	}
});
