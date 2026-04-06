import { test, expect, PERSISTENT_PASSWORD, navigateToInstance } from '../../fixtures/auth';
import { API_URL, getIdToken } from '../../helpers/journey-helpers';

const PERSISTENT_EMAIL = process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+1@gmail.com';
const API_KEY = process.env.E2E_OPENROUTER_API_KEY || '';

async function getInstanceDetails(): Promise<{ id: string; framework: string; dashboard_url: string | null; agent_status: string | null } | null> {
	if (!PERSISTENT_PASSWORD) return null;
	try {
		const idToken = await getIdToken(PERSISTENT_EMAIL, PERSISTENT_PASSWORD);
		const res = await fetch(`${API_URL}/api/dashboard/state`, {
			headers: { Authorization: `Bearer ${idToken}` },
		});
		const state = await res.json();
		const inst = state.instances?.find(
			(i: { status: string }) => i.status !== 'terminated' && i.status !== 'terminating'
		);
		if (!inst) return null;
		return {
			id: inst.id,
			framework: inst.framework || 'openclaw',
			dashboard_url: inst.dashboard_url,
			agent_status: inst.agent_status,
		};
	} catch {
		return null;
	}
}

test.describe('Hermes chat interface', () => {
	test.skip(!PERSISTENT_PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');

	test('"Chat with Agent" link navigates to chat page', async ({ authedPage: page }) => {
		const inst = await getInstanceDetails();
		if (!inst || inst.framework !== 'hermes') {
			test.skip(true, 'No active Hermes instance');
			return;
		}
		if (inst.agent_status !== 'running') {
			test.skip(true, 'Agent not running');
			return;
		}

		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Click "Chat with Agent" link
		const chatLink = page.getByRole('link', { name: 'Chat with Agent' });
		await expect(chatLink).toBeVisible({ timeout: 30_000 });
		await chatLink.click();

		// Should navigate to /dashboard/instances/{id}/chat
		await page.waitForURL(`**/dashboard/instances/${inst.id}/chat`, { timeout: 15_000 });
		console.log('[E2E] Navigated to Hermes chat page');

		// Chat interface should load
		await expect(page.getByText('Hermes Agent Chat')).toBeVisible({ timeout: 30_000 });
		console.log('[E2E] Hermes chat interface loaded');

		// Should have a message input and send button
		const messageInput = page.locator('input[placeholder="Type a message..."]');
		await expect(messageInput).toBeVisible({ timeout: 10_000 });

		const sendBtn = page.getByRole('button', { name: 'Send' });
		await expect(sendBtn).toBeVisible();
		await expect(sendBtn).toBeDisabled(); // disabled when input is empty
		console.log('[E2E] Chat input and send button visible');

		// Back link should work
		const backLink = page.getByRole('link', { name: /Back to agent/i });
		await expect(backLink).toBeVisible();
		console.log('[E2E] Back to agent link visible');
	});

	test('can send a message and receive a response', async ({ authedPage: page }) => {
		test.skip(!API_KEY, 'E2E_OPENROUTER_API_KEY required for chat test');

		const inst = await getInstanceDetails();
		if (!inst || inst.framework !== 'hermes') {
			test.skip(true, 'No active Hermes instance');
			return;
		}
		if (inst.agent_status !== 'running') {
			test.skip(true, 'Agent not running');
			return;
		}

		// Navigate directly to chat page
		await page.goto(`/dashboard/instances/${inst.id}/chat`);
		await expect(page.getByText('Hermes Agent Chat')).toBeVisible({ timeout: 30_000 });

		// Type a simple message
		const messageInput = page.locator('input[placeholder="Type a message..."]');
		await messageInput.fill('Hello, what is 2 + 2?');

		// Send button should be enabled now
		const sendBtn = page.getByRole('button', { name: 'Send' });
		await expect(sendBtn).toBeEnabled();

		// Send the message
		await sendBtn.click();

		// User message should appear in the chat
		await expect(page.getByText('Hello, what is 2 + 2?')).toBeVisible({ timeout: 5_000 });
		console.log('[E2E] User message displayed');

		// Wait for typing indicator (bouncing dots)
		const typingIndicator = page.locator('.animate-bounce').first();
		await expect(typingIndicator).toBeVisible({ timeout: 10_000 });
		console.log('[E2E] Typing indicator shown');

		// Wait for the response (up to 60s for LLM response)
		// The response should contain "4" somewhere
		const responseLocator = page.locator('pre').filter({ hasText: /4/ }).first();
		await expect(responseLocator).toBeVisible({ timeout: 60_000 });
		console.log('[E2E] Received response from Hermes agent');

		// Input should be re-enabled after response
		await expect(messageInput).toBeEnabled({ timeout: 5_000 });
		console.log('[E2E] Chat ready for next message');
	});

	test('chat page shows error for non-Hermes instance', async ({ authedPage: page }) => {
		const inst = await getInstanceDetails();
		if (!inst || inst.framework !== 'openclaw') {
			test.skip(true, 'Need an OpenClaw instance to test redirect');
			return;
		}

		// Navigate to chat page for an OpenClaw instance — should redirect back
		await page.goto(`/dashboard/instances/${inst.id}/chat`);

		// Should redirect to the instance detail page (not the chat page)
		await page.waitForURL(`**/dashboard/instances/${inst.id}`, { timeout: 15_000 });
		console.log('[E2E] OpenClaw instance correctly redirects away from chat page');
	});
});
