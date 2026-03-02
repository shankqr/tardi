import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

const EMAIL_REGEX = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export const POST: RequestHandler = async ({ request, platform }) => {
	const webhookUrl = platform?.env?.GOOGLE_SHEETS_WEBHOOK_URL;

	if (!webhookUrl) {
		console.error('GOOGLE_SHEETS_WEBHOOK_URL is not configured');
		return json({ error: 'Waitlist is not available right now. Please try again later.' }, { status: 500 });
	}

	let body: { email?: string };
	try {
		body = await request.json();
	} catch {
		return json({ error: 'Invalid request body.' }, { status: 400 });
	}

	const email = body.email?.trim().toLowerCase();

	if (!email) {
		return json({ error: 'Email is required.' }, { status: 400 });
	}

	if (!EMAIL_REGEX.test(email)) {
		return json({ error: 'Please enter a valid email address.' }, { status: 400 });
	}

	try {
		const response = await fetch(webhookUrl, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ email })
		});

		if (!response.ok) {
			console.error('Google Sheets webhook error:', response.status, await response.text());
			return json({ error: 'Something went wrong. Please try again.' }, { status: 502 });
		}

		return json({ success: true });
	} catch (err) {
		console.error('Failed to reach Google Sheets webhook:', err);
		return json({ error: 'Something went wrong. Please try again.' }, { status: 502 });
	}
};
