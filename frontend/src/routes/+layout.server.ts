import type { LayoutServerLoad } from './$types';

export const load: LayoutServerLoad = ({ platform }) => {
	return {
		config: {
			comingSoon: platform?.env?.COMING_SOON === 'true',
			apiUrl: platform?.env?.API_URL ?? 'http://localhost:8080'
		}
	};
};
