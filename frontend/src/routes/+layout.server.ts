import type { LayoutServerLoad } from './$types';

export const load: LayoutServerLoad = ({ platform }) => {
	return {
		config: {
			comingSoon: platform?.env?.COMING_SOON === 'true',
			apiUrl: platform?.env?.API_URL ?? 'http://localhost:8080',
			stripePricingTableId: platform?.env?.STRIPE_PRICING_TABLE_ID ?? '',
			stripePublishableKey: platform?.env?.STRIPE_PUBLISHABLE_KEY ?? ''
		}
	};
};
