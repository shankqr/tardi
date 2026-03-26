import { describe, it, expect } from 'vitest';
import { get } from 'svelte/store';
import { apiUrl, stripePricingTableId, stripePublishableKey, getApiUrl } from './config';

describe('config store', () => {
	it('apiUrl has default value', () => {
		expect(get(apiUrl)).toBe('http://localhost:8080');
	});

	it('getApiUrl returns current apiUrl value', () => {
		expect(getApiUrl()).toBe('http://localhost:8080');
	});

	it('getApiUrl reflects updated apiUrl', () => {
		apiUrl.set('https://api.example.com');
		expect(getApiUrl()).toBe('https://api.example.com');
		// reset
		apiUrl.set('http://localhost:8080');
	});

	it('stripePricingTableId defaults to empty string', () => {
		expect(get(stripePricingTableId)).toBe('');
	});

	it('stripePublishableKey defaults to empty string', () => {
		expect(get(stripePublishableKey)).toBe('');
	});
});
