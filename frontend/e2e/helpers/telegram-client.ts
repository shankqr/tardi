import { TelegramClient } from 'telegram';
import { StringSession } from 'telegram/sessions/index.js';
import { NewMessage, type NewMessageEvent } from 'telegram/events/index.js';

export interface TelegramReply {
	text: string;
	messageCount: number;
}

const API_ID = Number(process.env.E2E_TELEGRAM_API_ID);
const API_HASH = process.env.E2E_TELEGRAM_API_HASH || '';
const SESSION = process.env.E2E_TELEGRAM_SESSION || '';

/**
 * Send a message to a Telegram bot and wait for its reply.
 * Uses gramjs (MTProto) to connect as a real Telegram user.
 *
 * Returns the reply text and the number of messages received
 * (messageCount > 1 indicates double replies from streaming:partial).
 */
export async function sendAndWaitForReply(
	botUsername: string,
	message: string,
	timeoutMs = 90_000
): Promise<TelegramReply> {
	if (!API_ID || !API_HASH || !SESSION) {
		throw new Error(
			'Missing Telegram client env vars. Set E2E_TELEGRAM_API_ID, E2E_TELEGRAM_API_HASH, and E2E_TELEGRAM_SESSION.'
		);
	}

	const client = new TelegramClient(new StringSession(SESSION), API_ID, API_HASH, {
		connectionRetries: 3,
	});

	try {
		await client.connect();
	} catch (err) {
		throw new Error(
			`Telegram session expired or invalid. Re-run: npm run test:e2e:setup-telegram\n${err}`
		);
	}

	try {
		const bot = await client.getEntity(botUsername);
		const botId = 'id' in bot ? bot.id : undefined;

		const replies: string[] = [];
		let resolveWait: (() => void) | null = null;
		let settleTimer: ReturnType<typeof setTimeout> | null = null;

		const SETTLE_MS = 5_000;

		const onNewMessage = (event: NewMessageEvent) => {
			const msg = event.message;
			// Only collect messages from the bot
			if (msg.senderId && botId && msg.senderId.valueOf() === botId.valueOf()) {
				replies.push(msg.text || '');
				// Reset the settle timer on each new message
				if (settleTimer) clearTimeout(settleTimer);
				settleTimer = setTimeout(() => {
					resolveWait?.();
				}, SETTLE_MS);
			}
		};

		client.addEventHandler(onNewMessage, new NewMessage({}));

		// Send the message
		await client.sendMessage(bot, { message });

		// Wait for reply with timeout
		await new Promise<void>((resolve, reject) => {
			resolveWait = resolve;
			const deadline = setTimeout(() => {
				if (replies.length > 0) {
					resolve();
				} else {
					reject(new Error(`Bot did not reply within ${timeoutMs / 1000}s`));
				}
			}, timeoutMs);

			// Clean up deadline when resolved
			const origResolve = resolve;
			resolveWait = () => {
				clearTimeout(deadline);
				origResolve();
			};
		});

		return {
			text: replies[0] || '',
			messageCount: replies.length,
		};
	} finally {
		await client.disconnect();
	}
}
