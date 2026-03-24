import { writable } from 'svelte/store';
import { browser } from '$app/environment';

export type Theme = 'dark' | 'light';

const STORAGE_KEY = 'tardi-theme';

function getInitialTheme(): Theme {
	if (browser) {
		const stored = localStorage.getItem(STORAGE_KEY);
		if (stored === 'light' || stored === 'dark') return stored;
	}
	return 'dark'; // default to dark
}

export const theme = writable<Theme>(getInitialTheme());

export function toggleTheme() {
	theme.update((t) => {
		const next = t === 'dark' ? 'light' : 'dark';
		if (browser) {
			localStorage.setItem(STORAGE_KEY, next);
			applyTheme(next);
		}
		return next;
	});
}

export function applyTheme(t: Theme) {
	if (!browser) return;
	if (t === 'dark') {
		document.documentElement.classList.add('dark');
	} else {
		document.documentElement.classList.remove('dark');
	}
}

export function initTheme() {
	const t = getInitialTheme();
	theme.set(t);
	applyTheme(t);
}
