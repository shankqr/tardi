import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import { theme, toggleTheme, initTheme } from './theme';

describe('theme store', () => {
	beforeEach(() => {
		localStorage.clear();
		// Reset to default
		theme.set('dark');
	});

	it('defaults to dark theme', () => {
		expect(get(theme)).toBe('dark');
	});

	it('toggleTheme switches from dark to light', () => {
		theme.set('dark');
		toggleTheme();
		expect(get(theme)).toBe('light');
	});

	it('toggleTheme switches from light to dark', () => {
		theme.set('light');
		toggleTheme();
		expect(get(theme)).toBe('dark');
	});

	it('toggleTheme persists to localStorage', () => {
		theme.set('dark');
		toggleTheme();
		expect(localStorage.getItem('tardi-theme')).toBe('light');
	});

	it('initTheme reads from localStorage', () => {
		localStorage.setItem('tardi-theme', 'light');
		initTheme();
		expect(get(theme)).toBe('light');
	});

	it('initTheme defaults to dark when localStorage is empty', () => {
		localStorage.removeItem('tardi-theme');
		initTheme();
		expect(get(theme)).toBe('dark');
	});

	it('initTheme ignores invalid localStorage values', () => {
		localStorage.setItem('tardi-theme', 'invalid');
		initTheme();
		expect(get(theme)).toBe('dark');
	});

	it('toggleTheme updates document classList for dark', () => {
		theme.set('light');
		toggleTheme();
		expect(document.documentElement.classList.contains('dark')).toBe(true);
	});

	it('toggleTheme updates document classList for light', () => {
		document.documentElement.classList.add('dark');
		theme.set('dark');
		toggleTheme();
		expect(document.documentElement.classList.contains('dark')).toBe(false);
	});
});
