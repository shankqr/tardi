import { test, expect, type Page } from '@playwright/test';
import { sendAndWaitForReply } from '../../helpers/telegram-client';

const EMAIL = process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+1@gmail.com';
const PASSWORD = process.env.E2E_PERSISTENT_PASSWORD || process.env.E2E_TEST_PASSWORD || '';
const BOT_USERNAME = process.env.E2E_TELEGRAM_BOT_USERNAME || '';
const TELEGRAM_SESSION = process.env.E2E_TELEGRAM_SESSION || '';

async function login(page: Page): Promise<void> {
	await page.goto('/login');
	const signInBtn = page.getByRole('button', { name: 'Sign in' });
	await expect(signInBtn).toBeVisible({ timeout: 10_000 });
	await expect(signInBtn).toBeEnabled();
	await page.waitForTimeout(1000);

	await page.locator('#email').click();
	await page.locator('#email').pressSequentially(EMAIL, { delay: 20 });
	await page.locator('#password').click();
	await page.locator('#password').pressSequentially(PASSWORD, { delay: 20 });
	await signInBtn.click();
	await page.waitForURL('**/dashboard**', { timeout: 30_000 });
}

test.describe('Telegram bot reply', () => {
	test.skip(!PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');
	test.skip(!TELEGRAM_SESSION, 'E2E_TELEGRAM_SESSION not set');
	test.skip(!BOT_USERNAME, 'E2E_TELEGRAM_BOT_USERNAME not set');

	test('bot responds to a message', async ({ page }) => {
		// Verify instance has Telegram connected
		await login(page);
		await page.waitForTimeout(5000);

		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		const hasInstance = await instanceLink.isVisible({ timeout: 10_000 }).catch(() => false);
		if (!hasInstance) {
			test.skip(true, 'No active instance found');
			return;
		}

		await instanceLink.click();
		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });

		// Check if Telegram is connected
		const telegramHeading = page.getByText('Telegram Bot').first();
		await telegramHeading.scrollIntoViewIfNeeded();
		const disconnectBtn = page.getByRole('button', { name: /disconnect/i });
		const isConnected = await disconnectBtn.isVisible({ timeout: 5_000 }).catch(() => false);

		if (!isConnected) {
			test.skip(true, 'Telegram bot not connected on this instance');
			return;
		}

		// Send a message via gramjs and wait for reply
		const testMessage = `E2E test ping ${Date.now()}`;
		console.log(`[E2E] Sending to @${BOT_USERNAME}: "${testMessage}"`);

		const reply = await sendAndWaitForReply(BOT_USERNAME, testMessage, 90_000);

		console.log(`[E2E] Bot replied: "${reply.text.substring(0, 100)}..."`);
		console.log(`[E2E] Reply count: ${reply.messageCount} (expected 1)`);

		// Bot replied with something
		expect(reply.text).toBeTruthy();
		expect(reply.text.length).toBeGreaterThan(0);

		// No double replies (streaming:off is working)
		expect(reply.messageCount).toBe(1);
	});

	test('bot does not require pairing prompt', async ({ page }) => {
		// Skip dashboard check — just test Telegram directly
		// Send /start and verify no pairing prompt
		const reply = await sendAndWaitForReply(BOT_USERNAME, '/start', 60_000);

		console.log(`[E2E] /start reply: "${reply.text.substring(0, 100)}..."`);

		expect(reply.text).not.toContain('access not configured');
		expect(reply.text).not.toContain('pair');
	});
});
