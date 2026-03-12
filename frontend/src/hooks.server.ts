import type { HandleServerError } from '@sveltejs/kit';

// Cloudflare Workers doesn't support the full Node.js Sentry SDK.
// Server errors surface to the client where Sentry captures them.
// This hook logs the error and passes an eventId for the error page.
export const handleError: HandleServerError = ({ error, event }) => {
	const eventId = crypto.randomUUID();
	console.error(`[${eventId}] Server error on ${event.url.pathname}:`, error);
	return {
		message: 'An unexpected error occurred.',
		eventId
	};
};
