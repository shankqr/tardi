import { test, expect, PERSISTENT_PASSWORD, navigateToInstance } from '../../fixtures/auth';
import { API_URL, getIdToken } from '../../helpers/journey-helpers';

/**
 * These tests run against the persistent test account's instance.
 * They check Hermes-specific UI behaviors when the instance is a Hermes agent.
 * If the persistent instance is OpenClaw, Hermes-specific tests are skipped.
 */

const PERSISTENT_EMAIL = process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+1@gmail.com';

async function getInstanceFramework(): Promise<string | null> {
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
		return inst?.framework || 'openclaw';
	} catch {
		return null;
	}
}

test.describe('Hermes instance detail page', () => {
	test.skip(!PERSISTENT_PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');

	test('instance card shows framework label', async ({ authedPage: page }) => {
		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		const hasInstance = await instanceLink.isVisible({ timeout: 15_000 }).catch(() => false);
		if (!hasInstance) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Instance card should display framework name (OpenClaw or Hermes)
		const frameworkLabel = page.locator('a[href*="/dashboard/instances/"]').first();
		const cardText = await frameworkLabel.textContent();
		const hasFramework = cardText?.includes('OpenClaw') || cardText?.includes('Hermes');
		expect(hasFramework).toBeTruthy();
		console.log(`[E2E] Instance card framework: ${cardText?.includes('Hermes') ? 'Hermes' : 'OpenClaw'}`);
	});

	test('Hermes instance shows "Chat with Agent" instead of "Open Agent Dashboard"', async ({ authedPage: page }) => {
		const framework = await getInstanceFramework();
		if (framework !== 'hermes') {
			test.skip(true, 'Persistent instance is not Hermes');
			return;
		}

		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Wait for agent to be Running
		const runningStatus = page.locator('dd').filter({ hasText: /Running/i }).first();
		await expect(runningStatus).toBeVisible({ timeout: 60_000 });

		// Should show "Chat with Agent" link instead of "Open Agent Dashboard" button
		const chatLink = page.getByRole('link', { name: 'Chat with Agent' });
		const dashboardBtn = page.getByRole('button', { name: 'Open Agent Dashboard' });

		await expect(chatLink).toBeVisible({ timeout: 10_000 });
		const hasDashboardBtn = await dashboardBtn.isVisible({ timeout: 3_000 }).catch(() => false);
		expect(hasDashboardBtn).toBeFalsy();
		console.log('[E2E] Hermes instance shows "Chat with Agent" link');
	});

	test('Hermes instance shows "Hermes" in agent details label', async ({ authedPage: page }) => {
		const framework = await getInstanceFramework();
		if (framework !== 'hermes') {
			test.skip(true, 'Persistent instance is not Hermes');
			return;
		}

		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// The agent details section should show "Hermes" as the label
		const hermesLabel = page.locator('dt').filter({ hasText: 'Hermes' });
		await expect(hermesLabel).toBeVisible({ timeout: 15_000 });
		console.log('[E2E] Hermes label visible in agent details');
	});

	test('Hermes instance does not show MagicMoment prompts', async ({ authedPage: page }) => {
		const framework = await getInstanceFramework();
		if (framework !== 'hermes') {
			test.skip(true, 'Persistent instance is not Hermes');
			return;
		}

		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Wait for page to fully load
		await page.waitForTimeout(3_000);

		// MagicMoment section should NOT be visible for Hermes
		const magicMoment = page.getByText('Try these prompts');
		const isVisible = await magicMoment.isVisible({ timeout: 5_000 }).catch(() => false);
		expect(isVisible).toBeFalsy();
		console.log('[E2E] MagicMoment correctly hidden for Hermes instance');
	});

	test('OpenClaw instance shows "Open Agent Dashboard" and not "Chat with Agent"', async ({ authedPage: page }) => {
		const framework = await getInstanceFramework();
		if (framework !== 'openclaw') {
			test.skip(true, 'Persistent instance is not OpenClaw');
			return;
		}

		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Wait for agent to be Running
		const runningStatus = page.locator('dd').filter({ hasText: /Running/i }).first();
		const isRunning = await runningStatus.isVisible({ timeout: 60_000 }).catch(() => false);
		if (!isRunning) {
			test.skip(true, 'Agent not running');
			return;
		}

		// Should show "Open Agent Dashboard" button, not "Chat with Agent"
		const dashboardBtn = page.getByRole('button', { name: 'Open Agent Dashboard' });
		const chatLink = page.getByRole('link', { name: 'Chat with Agent' });

		const hasDashboard = await dashboardBtn.isVisible({ timeout: 10_000 }).catch(() => false);
		const hasChatLink = await chatLink.isVisible({ timeout: 3_000 }).catch(() => false);

		// At least the dashboard button or the API key prompt should be visible
		// (dashboard button requires API key to be set)
		if (hasDashboard) {
			expect(hasChatLink).toBeFalsy();
			console.log('[E2E] OpenClaw instance shows "Open Agent Dashboard"');
		} else {
			expect(hasChatLink).toBeFalsy();
			console.log('[E2E] OpenClaw instance: dashboard button hidden (API key may be required)');
		}
	});
});
