import { writable, get } from 'svelte/store';

export const apiUrl = writable('http://localhost:8080');
export const stripePricingTableId = writable('');
export const stripePublishableKey = writable('');

export function getApiUrl(): string {
	return get(apiUrl);
}
