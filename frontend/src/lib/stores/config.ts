import { writable } from 'svelte/store';
import { get } from 'svelte/store';

export const apiUrl = writable('http://localhost:8080');

export function getApiUrl(): string {
	return get(apiUrl);
}
