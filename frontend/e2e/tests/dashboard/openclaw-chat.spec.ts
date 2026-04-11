import { test, expect } from '../../fixtures/auth';
import { API_URL, getIdToken } from '../../helpers/journey-helpers';
import { loadAccountState } from '../../helpers/test-state';

const API_KEY = process.env.E2E_OPENROUTER_API_KEY || '';

async function getOpenClawInstance(): Promise<{
	id: string;
	dashboard_url: string | null;
	agent_status: string | null;
} | null> {
	try {
		const account = loadAccountState('openclaw');
		const idToken = await getIdToken(account.email, account.password);
		const res = await fetch(`${API_URL}/api/dashboard/state`, {
			headers: { Authorization: `Bearer ${idToken}` },
		});
		const data = await res.json();
		const inst = data.instances?.find(
			(i: { status: string }) => i.status !== 'terminated' && i.status !== 'terminating'
		);
		if (!inst || (inst.framework && inst.framework !== 'openclaw')) return null;
		return { id: inst.id, dashboard_url: inst.dashboard_url, agent_status: inst.agent_status };
	} catch {
		return null;
	}
}

test.describe('OpenClaw chat via Control UI', () => {
	test('can send a message and receive a response on OC dashboard', async ({ authedPage: page }) => {
		test.skip(!API_KEY, 'E2E_OPENROUTER_API_KEY required for chat test');

		const inst = await getOpenClawInstance();
		if (!inst) { test.skip(true, 'No active OpenClaw instance'); return; }
		if (inst.agent_status !== 'running' && inst.agent_status !== 'healthy') {
			test.skip(true, `Agent not running (status: ${inst.agent_status})`); return;
		}
		if (!inst.dashboard_url) { test.skip(true, 'No dashboard URL'); return; }

		// Get dashboard token via API
		const account = loadAccountState('openclaw');
		const idToken = await getIdToken(account.email, account.password);
		const dashTokenRes = await fetch(
			`${API_URL}/api/instances/${inst.id}/dashboard-token`,
			{ method: 'POST', headers: { Authorization: `Bearer ${idToken}` } }
		);
		expect(dashTokenRes.ok, 'Failed to get dashboard token').toBeTruthy();
		const { token: dashboardToken } = await dashTokenRes.json();
		expect(dashboardToken, 'Dashboard token is empty').toBeTruthy();

		// Wait for dashboard to be reachable
		const ocUrl = `${inst.dashboard_url}/#token=${dashboardToken}`;
		let dashboardReady = false;
		for (let attempt = 0; attempt < 12; attempt++) {
			try {
				const res = await fetch(inst.dashboard_url, { signal: AbortSignal.timeout(10_000) });
				if (res.ok || res.status === 101 || res.status === 426) { dashboardReady = true; break; }
				console.log(`[E2E] Dashboard not ready (status ${res.status}), retrying...`);
			} catch {
				console.log(`[E2E] Dashboard not reachable yet (attempt ${attempt + 1}/12)`);
			}
			await new Promise((r) => setTimeout(r, 10_000));
		}
		if (!dashboardReady) { test.skip(true, 'Dashboard never became reachable'); return; }

		// Navigate to Control UI
		await page.goto(ocUrl);

		const chatInput = page.locator('textarea, input[type="text"], [contenteditable="true"]').last();
		await expect(chatInput).toBeVisible({ timeout: 60_000 });
		console.log('[E2E] Control UI loaded, chat input found');

		await chatInput.click();
		await chatInput.fill('Hello, what is 2 + 2?');
		await page.keyboard.press('Enter');
		console.log('[E2E] Message sent');

		// Wait for response
		const initialContent = await page.textContent('body') || '';
		const initialLength = initialContent.length;
		let responseDetected = false;
		const startTime = Date.now();

		while (Date.now() - startTime < 60_000) {
			await page.waitForTimeout(3000);
			const currentContent = await page.textContent('body') || '';
			if (currentContent.length > initialLength + 50) {
				responseDetected = true;
				console.log(`[E2E] Response detected after ${Math.round((Date.now() - startTime) / 1000)}s`);
				break;
			}
		}

		expect(responseDetected, 'Agent did not respond within 60s').toBeTruthy();

		const pageContent = await page.textContent('body') || '';
		expect(pageContent.length - initialLength).toBeGreaterThan(20);

		const lowerContent = pageContent.toLowerCase();
		const hasRelevantContent =
			lowerContent.includes('4') || lowerContent.includes('four') ||
			lowerContent.includes('model') || lowerContent.includes('assistant');
		expect(hasRelevantContent, 'Response does not contain expected content').toBeTruthy();
		console.log('[E2E] OpenClaw agent responded with relevant content');
	});
});
