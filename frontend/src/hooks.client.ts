import * as Sentry from '@sentry/sveltekit';
import { handleErrorWithSentry } from '@sentry/sveltekit';

const dsn = import.meta.env.VITE_SENTRY_DSN;

if (dsn) {
	Sentry.init({
		dsn,
		environment: import.meta.env.VITE_SENTRY_ENVIRONMENT || 'development',

		// Performance: 100% in dev, 20% in prod
		tracesSampleRate:
			import.meta.env.VITE_SENTRY_ENVIRONMENT === 'production' ? 0.2 : 1.0,

		// Session Replay: 10% of sessions, 100% of error sessions
		replaysSessionSampleRate: 0.1,
		replaysOnErrorSampleRate: 1.0,

		integrations: [
			Sentry.browserTracingIntegration(),
			Sentry.replayIntegration({
				maskAllText: false,
				blockAllMedia: false
			})
		],

		ignoreErrors: [
			'ResizeObserver loop',
			/Loading chunk \d+ failed/,
			/Failed to fetch dynamically imported module/
		]
	});
}

export const handleError = handleErrorWithSentry();
