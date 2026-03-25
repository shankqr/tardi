import { test, expect, PERSISTENT_PASSWORD, navigateToInstance } from '../../fixtures/auth';
import { sendAndWaitForReply } from '../../helpers/telegram-client';

const BOT_USERNAME = process.env.E2E_TELEGRAM_BOT_USERNAME || '';
const TELEGRAM_SESSION = process.env.E2E_TELEGRAM_SESSION || '';

test.describe('Telegram bot reply', () => {
	test.skip(!PERSISTENT_PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');
	test.skip(!TELEGRAM_SESSION, 'E2E_TELEGRAM_SESSION not set');
	test.skip(!BOT_USERNAME, 'E2E_TELEGRAM_BOT_USERNAME not set');

	test('bot responds to a message', async ({ authedPage: page }) => {
		// Verify instance has Telegram connected
		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

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

		// Bot replied with something meaningful (not an error message)
		expect(reply.text).toBeTruthy();
		expect(reply.text.length).toBeGreaterThan(10);
		expect(reply.text).not.toContain('error');
		expect(reply.text).not.toContain('access not configured');

		// No double replies (streaming:off is working)
		expect(reply.messageCount).toBe(1);
	});

	test('bot does not require pairing prompt', async () => {
		// Send /start and verify no pairing prompt
		const reply = await sendAndWaitForReply(BOT_USERNAME, '/start', 60_000);

		console.log(`[E2E] /start reply: "${reply.text.substring(0, 100)}..."`);

		expect(reply.text).not.toContain('access not configured');
		expect(reply.text).not.toContain('pair');
	});
});
