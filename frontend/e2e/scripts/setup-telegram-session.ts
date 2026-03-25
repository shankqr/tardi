/**
 * One-time setup script to generate a gramjs session string for E2E Telegram tests.
 * Run with: npm run test:e2e:setup-telegram
 *
 * Prerequisites:
 * 1. Create a Telegram API app at https://my.telegram.org → "API development tools"
 * 2. Set E2E_TELEGRAM_API_ID and E2E_TELEGRAM_API_HASH in .env.e2e
 * 3. Have a Telegram account (separate test account recommended)
 *
 * This script will:
 * 1. Ask for your phone number
 * 2. Send a Telegram login code to that phone
 * 3. Ask you to enter the code
 * 4. Output a session string to add to .env.e2e
 */

import { config } from 'dotenv';
config({ path: '.env.e2e' });

import { TelegramClient } from 'telegram';
import { StringSession } from 'telegram/sessions/index.js';
import * as readline from 'readline';

const API_ID = Number(process.env.E2E_TELEGRAM_API_ID);
const API_HASH = process.env.E2E_TELEGRAM_API_HASH || '';

if (!API_ID || !API_HASH) {
	console.error('Missing E2E_TELEGRAM_API_ID or E2E_TELEGRAM_API_HASH in .env.e2e');
	console.error('Create a Telegram API app at https://my.telegram.org');
	process.exit(1);
}

const rl = readline.createInterface({ input: process.stdin, output: process.stdout });

function ask(question: string): Promise<string> {
	return new Promise((resolve) => rl.question(question, resolve));
}

async function main() {
	console.log('Telegram Session Setup');
	console.log('======================\n');

	const client = new TelegramClient(new StringSession(''), API_ID, API_HASH, {
		connectionRetries: 3,
	});

	await client.start({
		phoneNumber: () => ask('Phone number (with country code, e.g. +1234567890): '),
		phoneCode: () => ask('Login code (check Telegram app): '),
		password: () => ask('2FA password (if enabled): '),
		onError: (err) => console.error('Error:', err),
	});

	const session = client.session.save() as unknown as string;

	console.log('\n✅ Session created successfully!\n');
	console.log('Add this to your .env.e2e:\n');
	console.log(`E2E_TELEGRAM_SESSION=${session}`);
	console.log();

	await client.disconnect();
	rl.close();
}

main().catch((err) => {
	console.error('Setup failed:', err);
	rl.close();
	process.exit(1);
});
