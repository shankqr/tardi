import type { HetznerCountry } from '$lib/types';

export const hetznerCountryOptions: Array<{ value: HetznerCountry; label: string }> = [
	{ value: 'fi', label: 'Finland' },
	{ value: 'de', label: 'Germany' }
];

export const defaultHetznerCountry: HetznerCountry = 'fi';
