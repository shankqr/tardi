import { test, expect } from '../../fixtures/auth';
import { API_URL, getIdToken } from '../../helpers/journey-helpers';
import { loadAccountState } from '../../helpers/test-state';

const API_KEY = process.env.E2E_OPENROUTER_API_KEY || '';

async function getHermesInstance(): Promise<{ id: string; agent_status: string | null } | null> {
	try {
		const account = loadAccountState('hermes');
		const idToken = await getIdToken(account.email, account.password);
		const res = await fetch(`${API_URL}/api/dashboard/state`, {
			headers: { Authorization: `Bearer ${idToken}` },
		});
		const data = await res.json();
		const inst = data.instances?.find(
			(i: { status: string }) => i.status !== 'terminated' && i.status !== 'terminating'
		);
		if (!inst || (inst.framework || 'openclaw') !== 'hermes') return null;
		return { id: inst.id, agent_status: inst.agent_status };
	} catch {
		return null;
	}
}

test.describe('Hermes chat interface', () => {
	test('"Open Agent Dashboard" link loads the Hermes chat page', async ({ authedHermesPage: page }) => {
		const inst = await getHermesInstance();
		if (!inst) { test.skip(true, 'No active Hermes instance'); return; }
		if (inst.agent_status !== 'running') { test.skip(true, 'Agent not running'); return; }

		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		if (!(await instanceLink.isVisible({ timeout: 15_000 }).catch(() => false))) {
			test.skip(true, 'No instance link'); return;
		}
		await instanceLink.click();
		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });

		const dashboardLink = page.getByRole('link', { name: 'Open Agent Dashboard' });
		await expect(dashboardLink).toBeVisible({ timeout: 30_000 });
		await expect(dashboardLink).toHaveAttribute('target', '_blank');

		await page.goto(`/dashboard/instances/${inst.id}/chat`);
		console.log('[E2E] Navigated to Hermes chat page');

		await expect(page.getByText('Hermes Agent Chat')).toBeVisible({ timeout: 30_000 });
		console.log('[E2E] Hermes chat interface loaded');

		const messageInput = page.locator('input[placeholder="Type a message..."]');
		await expect(messageInput).toBeVisible({ timeout: 10_000 });

		const sendBtn = page.getByRole('button', { name: 'Send' });
		await expect(sendBtn).toBeVisible();
		await expect(sendBtn).toBeDisabled();
		console.log('[E2E] Chat input and send button visible');
	});

	test('can send a message and receive a response', async ({ authedHermesPage: page }) => {
		test.skip(!API_KEY, 'E2E_OPENROUTER_API_KEY required for chat test');

		const inst = await getHermesInstance();
		if (!inst) { test.skip(true, 'No active Hermes instance'); return; }
		if (inst.agent_status !== 'running') { test.skip(true, 'Agent not running'); return; }

		await page.goto(`/dashboard/instances/${inst.id}/chat`);
		await expect(page.getByText('Hermes Agent Chat')).toBeVisible({ timeout: 30_000 });

		const messageInput = page.locator('input[placeholder="Type a message..."]');
		await messageInput.fill('Hello, what is 2 + 2?');

		const sendBtn = page.getByRole('button', { name: 'Send' });
		await expect(sendBtn).toBeEnabled();
		await sendBtn.click();

		await expect(page.getByText('Hello, what is 2 + 2?')).toBeVisible({ timeout: 5_000 });
		console.log('[E2E] User message displayed');

		const typingIndicator = page.locator('.animate-bounce').first();
		await expect(typingIndicator).toBeVisible({ timeout: 10_000 });
		console.log('[E2E] Typing indicator shown');

		const responseLocator = page.locator('pre').filter({ hasText: /4/ }).first();
		await expect(responseLocator).toBeVisible({ timeout: 60_000 });
		console.log('[E2E] Received response from Hermes agent');

		await expect(messageInput).toBeEnabled({ timeout: 5_000 });
		console.log('[E2E] Chat ready for next message');
	});
});
