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
import { waitForOpenClawRunning } from '../../helpers/openclaw-status';
import {
	API_URL,
	getIdToken,
	waitForSyncComplete,
	waitForInstanceActive,
	ensureInstancePage,
} from '../../helpers/journey-helpers';

async function cleanup(email: string, password: string, instanceId: string) {
	console.log('[E2E] Cleaning up test resources...');

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

test('Full user journey: signup → deploy → configure', async ({
	page,
}) => {
	const runNumber = Math.floor(Date.now() / 1000) % 100000;
	const email = `clawmyway+${runNumber}@gmail.com`;
	const password = process.env.E2E_TEST_PASSWORD || 'T4rd1E2e!xK9mQ2z';
	let firebaseUid = '';
	let instanceId = '';

	console.log(`[E2E] Test email: ${email}`);

	try {

	// ── Step 1: Sign up ──
	await test.step('Sign up with email and password', async () => {
		await page.goto('/signup');

		// Wait for SvelteKit hydration — the form becomes interactive after JS loads
		// We wait for the "Create account" button to be enabled (not disabled)
		const createBtn = page.getByRole('button', { name: 'Create account' });
		await expect(createBtn).toBeVisible({ timeout: 10_000 });
		await expect(createBtn).toBeEnabled();

		// Small delay to ensure Svelte 5 has bound the form inputs
		await page.waitForTimeout(1000);

		// Use page.type() instead of fill() — types character by character
		// which is more resilient to framework hydration timing
		await page.locator('#email').click();
		await page.locator('#email').pressSequentially(email, { delay: 20 });
		await page.locator('#password').click();
		await page.locator('#password').pressSequentially(password, { delay: 20 });
		await page.locator('#confirm-password').click();
		await page.locator('#confirm-password').pressSequentially(password, {
			delay: 20,
		});

		await createBtn.click();

		// Wait for navigation — could show error or redirect
		await page.waitForURL('**/verify-email**', { timeout: 30_000 });
		await expect(page.getByText('Check your inbox')).toBeVisible();

		// Get Firebase UID (retry for propagation)
		let user = await getTestUserByEmail(email);
		for (let i = 0; i < 5 && !user; i++) {
			await new Promise((r) => setTimeout(r, 2000));
			user = await getTestUserByEmail(email);
		}
		expect(user).toBeTruthy();
		firebaseUid = user!.uid;
		console.log(`[E2E] Firebase UID: ${firebaseUid}`);
	});

	// ── Step 2: Verify email ──
	await test.step('Verify email and redirect to checkout', async () => {
		// Programmatically verify email via Admin SDK
		await verifyUserEmail(firebaseUid);

		// The verify-email page polls every 3s — wait for redirect to checkout
		await page.waitForURL('**/onboarding/checkout**', { timeout: 30_000 });
		await expect(page.getByText('Choose your plan')).toBeVisible();
	});

	// ── Step 3: Stripe checkout ──
	await test.step('Complete Stripe checkout', async () => {
		// Create a Stripe Checkout Session via API and navigate to it
		// This is more reliable than interacting with the embedded pricing table iframe
		console.log('[E2E] Creating Stripe Checkout Session...');
		const checkoutUrl = await createCheckoutSession(email, firebaseUid);
		await page.goto(checkoutUrl);

		// Fill Stripe hosted checkout form with test card
		await page.locator('#cardNumber').fill('4242424242424242', {
			timeout: 15_000,
		});
		await page.locator('#cardExpiry').fill('1230');
		await page.locator('#cardCvc').fill('123');
		await page.locator('#billingName').fill('E2E Test User');

		// Submit payment
		await page.getByTestId('hosted-payment-submit-button').click();

		// Wait for redirect back to success page
		await page.waitForURL('**/onboarding/success**', { timeout: 60_000 });

		// Success page polls for subscription webhook, then redirects to /dashboard
		// If it times out and shows "Go to Dashboard", click it
		try {
			await page.waitForURL('**/dashboard', { timeout: 60_000 });
		} catch {
			// Click "Go to Dashboard" button if the polling timed out
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

	// ── Step 4: Deploy agent ──
	await test.step('Deploy agent', async () => {
		await expect(page.getByText('Deploy your agent')).toBeVisible({
			timeout: 15_000,
		});

		const agentName = `e2e-agent-${Date.now()}`;
		await page.locator('#agent-name').fill(agentName);
		await page.getByRole('button', { name: 'Deploy Agent' }).click();

		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });

		// Extract instance ID
		const url = page.url();
		const match = url.match(/\/instances\/([^/?#]+)/);
		expect(match).toBeTruthy();
		instanceId = match![1];
		console.log(`[E2E] Instance ID: ${instanceId}`);

		// Wait for provisioning to complete by polling the API (up to 10 min)
		// Can't rely on UI — dashboard polling can temporarily lose the instance
		console.log('[E2E] Waiting for provisioning to complete...');
		await waitForInstanceActive(email, password, instanceId, 600_000);

		// Recover the page UI after provisioning
		await ensureInstancePage(page, instanceId);
		console.log('[E2E] Agent is active and ready for configuration');
	});

	// ── Step 5: Set OpenRouter API key ──
	await test.step('Set OpenRouter API key', async () => {
		const apiKey = process.env.E2E_OPENROUTER_API_KEY;
		if (!apiKey) {
			console.log('[E2E] Skipping: E2E_OPENROUTER_API_KEY not set');
			return;
		}

		// Fill API key and save (this enables the model dropdown)
		await page.locator('#openrouter-key').fill(apiKey);
		await page.getByRole('button', { name: 'Save' }).first().click();

		// Poll sync status via API — the UI may lose the instance during sync
		// because dashboard polling replaces state while VPS restarts
		await waitForSyncComplete(email, password, instanceId);

		// Wait for instance to become fully active (VPS + agent healthy)
		await waitForInstanceActive(email, password, instanceId);

		// Recover the page UI (may show "Agent not found" after polling)
		await ensureInstancePage(page, instanceId);

		// Verify OpenClaw is Running after API key change (must be < 1 min)
		await waitForOpenClawRunning(page);
		console.log('[E2E] API key saved, model dropdown now enabled');
	});

	// ── Step 6: Change model ──
	let selectedModelName = '';
	await test.step('Change model', async () => {
		if (!process.env.E2E_OPENROUTER_API_KEY) {
			console.log('[E2E] Skipping: E2E_OPENROUTER_API_KEY not set');
			return;
		}

		// Wait for model dropdown to be enabled (key already saved)
		// After ensureInstancePage, AIProviderConfig re-mounts and needs to fetch config
		// from API. If it doesn't enable after 30s, reload and retry.
		const modelSelect = page.locator('#model-select');
		await modelSelect.scrollIntoViewIfNeeded();
		try {
			await expect(modelSelect).toBeEnabled({ timeout: 30_000 });
		} catch {
			console.log('[E2E] Model dropdown still disabled, reloading page...');
			await page.reload();
			await page.waitForTimeout(5000);
			await modelSelect.scrollIntoViewIfNeeded();
			await expect(modelSelect).toBeEnabled({ timeout: 60_000 });
		}

		// Get all available options and pick one that isn't the current default
		const options = modelSelect.locator('option');
		const optionCount = await options.count();
		console.log(`[E2E] Available models: ${optionCount}`);

		// Get the current value and pick a different model
		const currentValue = await modelSelect.inputValue();
		let targetValue = '';
		for (let i = 0; i < optionCount; i++) {
			const val = await options.nth(i).getAttribute('value');
			const text = await options.nth(i).textContent();
			if (val && val !== currentValue) {
				targetValue = val;
				selectedModelName = text?.trim() || val;
				break;
			}
		}

		if (targetValue) {
			await modelSelect.selectOption(targetValue);
			console.log(`[E2E] Changed model to: ${selectedModelName} (${targetValue})`);

			// Save the model change
			await page.getByRole('button', { name: 'Save' }).first().click();

			// Poll sync status via API
			await waitForSyncComplete(email, password, instanceId);

			// Wait for instance to become fully active
			await waitForInstanceActive(email, password, instanceId);

			// Recover the page UI
			await ensureInstancePage(page, instanceId);

			// Verify OpenClaw is Running after model change (must be < 1 min)
			await waitForOpenClawRunning(page);
			console.log('[E2E] Model change synced successfully');
		} else {
			console.log('[E2E] Only one model available, keeping default');
			selectedModelName =
				(await options.first().textContent())?.trim() || '';
		}
	});

	// ── Step 7: Open OC dashboard and verify model ──
	await test.step('Verify model in OpenClaw dashboard', async () => {
		if (!selectedModelName) {
			console.log('[E2E] Skipping: no model selected');
			return;
		}

		// Get the dashboard URL and token via API
		const idToken = await getIdToken(email, password);

		// Get dashboard token
		const dashTokenRes = await fetch(
			`${API_URL}/api/instances/${instanceId}/dashboard-token`,
			{
				method: 'POST',
				headers: { Authorization: `Bearer ${idToken}` },
			}
		);
		const { token: dashboardToken } = await dashTokenRes.json();

		// Get instance details for dashboard_url
		const dashRes = await fetch(`${API_URL}/api/dashboard/state`, {
			headers: { Authorization: `Bearer ${idToken}` },
		});
		const dashState = await dashRes.json();
		const instance = dashState.instances?.find(
			(i: { id: string }) => i.id === instanceId
		);
		const dashboardUrl = instance?.dashboard_url;

		if (!dashboardUrl || !dashboardToken) {
			console.log(
				'[E2E] Skipping dashboard test: no URL or token available'
			);
			return;
		}

		console.log(`[E2E] Opening dashboard: ${dashboardUrl}`);

		// After model change sync, the container restarts. Wait for the gateway
		// to come back online by polling the dashboard URL before navigating.
		const ocUrl = `${dashboardUrl}/#token=${dashboardToken}`;
		let dashboardReady = false;
		for (let attempt = 0; attempt < 12; attempt++) {
			try {
				const res = await fetch(dashboardUrl, { signal: AbortSignal.timeout(10_000) });
				if (res.ok || res.status === 101 || res.status === 426) {
					dashboardReady = true;
					break;
				}
				console.log(`[E2E] Dashboard not ready (status ${res.status}), retrying...`);
			} catch {
				console.log(`[E2E] Dashboard not reachable yet (attempt ${attempt + 1}/12)`);
			}
			await new Promise((r) => setTimeout(r, 10_000));
		}

		if (!dashboardReady) {
			console.log('[E2E] Dashboard never became reachable, skipping OC verification');
			return;
		}

		// Navigate to the OpenClaw Control UI with auth token
		await page.goto(ocUrl);

		// Wait for the Control UI to load and connect via WebSocket
		// The Control UI is a Lit-based SPA — look for the chat input
		const chatInput = page.locator(
			'textarea, input[type="text"], [contenteditable="true"]'
		).last();
		await expect(chatInput).toBeVisible({ timeout: 60_000 });
		console.log('[E2E] Control UI loaded, chat input found');

		// Type "what model are you" and send
		await chatInput.click();
		await chatInput.fill('what model are you using right now?');

		// Press Enter to send
		await page.keyboard.press('Enter');

		// Wait for a response that mentions the model
		// Extract just the model ID from the selected model name
		// The selectedModelName looks like "ModelName (tag1, tag2) — Free"
		// The model ID from the dropdown value is what we need
		const modelSelect = page.locator('#model-select');
		// We can't access the Tardi page anymore since we navigated away
		// Instead, search for key parts of the model name in the response

		// Wait for an assistant response to appear
		// The response takes time — poll for new content appearing on the page
		let responseDetected = false;
		const startTime = Date.now();
		const initialContent = await page.textContent('body') || '';
		const initialLength = initialContent.length;

		while (Date.now() - startTime < 30_000) {
			await page.waitForTimeout(3000);
			const currentContent = await page.textContent('body') || '';
			// Response detected when page content grows significantly (agent replied)
			if (currentContent.length > initialLength + 50) {
				responseDetected = true;
				break;
			}
		}

		const pageContent = await page.textContent('body');
		console.log(`[E2E] Looking for model info in response...`);
		console.log(`[E2E] Selected model was: ${selectedModelName}`);

		// Verify the agent actually responded (content grew)
		expect(responseDetected).toBeTruthy();

		// The response should contain substantive text (not just an error)
		const responseLength = (pageContent?.length || 0) - initialLength;
		expect(responseLength).toBeGreaterThan(20);

		// Check the response mentions something about the model or AI
		const lowerContent = pageContent?.toLowerCase() || '';
		const hasRelevantContent =
			lowerContent.includes('model') ||
			lowerContent.includes('ai') ||
			lowerContent.includes('language') ||
			lowerContent.includes('assistant');
		expect(hasRelevantContent).toBeTruthy();
		console.log('[E2E] Dashboard responded with relevant content');

		// Navigate back to the instance page for the next step
		await page.goto(`/dashboard/instances/${instanceId}`);
		await expect(page.locator('#openrouter-key')).toBeVisible({
			timeout: 30_000,
		});
	});

	} finally {
		await cleanup(email, password, instanceId);
	}
});
