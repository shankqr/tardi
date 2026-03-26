import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import { theme, toggleTheme } from './theme';

describe('theme store', () => {
	beforeEach(() => {
		theme.set('dark');
	});

	it('defaults to dark', () => {
		expect(get(theme)).toBe('dark');
	});

	it('toggleTheme toggles from dark to light', () => {
		toggleTheme();
		expect(get(theme)).toBe('light');
	});

	it('toggleTheme toggles back from light to dark', () => {
		toggleTheme(); // dark -> light
		toggleTheme(); // light -> dark
		expect(get(theme)).toBe('dark');
	});
});
