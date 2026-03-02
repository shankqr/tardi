// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
declare global {
	namespace App {
		// interface Error {}
		// interface Locals {}
		// interface PageData {}
		// interface PageState {}
		interface Platform {
			env?: {
				COMING_SOON?: string;
				API_URL?: string;
				STRIPE_PRICING_TABLE_ID?: string;
				STRIPE_PUBLISHABLE_KEY?: string;
				GOOGLE_SHEETS_WEBHOOK_URL?: string;
			};
		}
	}
}

export {};
