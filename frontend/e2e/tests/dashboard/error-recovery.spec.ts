import { test, expect, PERSISTENT_PASSWORD, navigateToInstance } from '../../fixtures/auth';
import { waitForOpenClawRunning } from '../../helpers/openclaw-status';

test.describe('Agent error and recovery', () => {
	test.skip(!PERSISTENT_PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');

	test('restart agent and verify recovery to Running', async ({ authedPage: page }) => {
		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Wait for agent to be in Running state before we restart
		const runningStatus = page.locator('dd').filter({ hasText: /Running/i }).first();
		await expect(runningStatus).toBeVisible({ timeout: 60_000 });
		console.log('[E2E] Agent is Running — triggering restart');

		// Expand Power User section to find Restart button
		const powerUserButton = page.getByText('Power User').first();
		await powerUserButton.scrollIntoViewIfNeeded();
		await powerUserButton.click();

		const restartButton = page.getByRole('button', { name: /restart/i });
		await restartButton.scrollIntoViewIfNeeded();
		await expect(restartButton).toBeVisible({ timeout: 10_000 });
		await expect(restartButton).toBeEnabled({ timeout: 10_000 });
		await restartButton.click();

		// After clicking restart, the instance status should change
		// It may show "Restarting" or immediately show cooloff countdown
		console.log('[E2E] Restart triggered — waiting for recovery');

		// The restart cooloff is 60s — during this time the button is disabled
		// Verify the button becomes disabled (cooloff active)
		await expect(restartButton).toBeDisabled({ timeout: 15_000 });
		console.log('[E2E] Restart cooloff active (button disabled)');

		// Wait for agent to return to Running state
		await waitForOpenClawRunning(page, 120_000);
		console.log('[E2E] Agent recovered to Running after restart');

		// Verify restart button re-enables after cooloff
		await expect(restartButton).toBeEnabled({ timeout: 90_000 });
		console.log('[E2E] Restart cooloff completed — button re-enabled');
	});

	test('unhealthy agent shows warning banner with quick actions', async ({ authedPage: page }) => {
		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Check if agent is currently unhealthy or has any warning
		const warningBanner = page.getByText(/stopped|unhealthy|not.found|degraded/i);
		const hasWarning = await warningBanner.isVisible({ timeout: 5_000 }).catch(() => false);

		if (hasWarning) {
			console.log('[E2E] Agent has warning state — verifying quick actions');

			// Quick restart button should be available in the warning banner
			const quickRestart = page.getByRole('button', { name: /restart/i }).first();
			const quickHealthCheck = page.getByRole('button', { name: /health check/i }).first();

			const hasQuickRestart = await quickRestart.isVisible({ timeout: 5_000 }).catch(() => false);
			const hasQuickHealthCheck = await quickHealthCheck.isVisible({ timeout: 5_000 }).catch(() => false);

			// At least one quick action should be available
			expect(hasQuickRestart || hasQuickHealthCheck).toBeTruthy();
			console.log(`[E2E] Quick actions: restart=${hasQuickRestart}, healthCheck=${hasQuickHealthCheck}`);
		} else {
			// Agent is healthy — just verify the agent status section exists
			const agentStatus = page.locator('dd').filter({ hasText: /Running|Active/i }).first();
			await expect(agentStatus).toBeVisible({ timeout: 30_000 });
			console.log('[E2E] Agent is healthy — no warning banner (expected)');
		}
	});
});
