import { vi } from 'vitest';

// Mock $app/environment
vi.mock('$app/environment', () => ({
	browser: true,
	dev: true,
	building: false,
	version: 'test'
}));

// Mock $app/stores
vi.mock('$app/stores', () => {
	const { readable, writable } = require('svelte/store');
	return {
		page: readable({ url: new URL('http://localhost'), params: {} }),
		navigating: readable(null),
		updated: { check: vi.fn() }
	};
});

// Mock $app/navigation
vi.mock('$app/navigation', () => ({
	goto: vi.fn(),
	invalidate: vi.fn(),
	invalidateAll: vi.fn(),
	prefetch: vi.fn(),
	prefetchRoutes: vi.fn(),
	beforeNavigate: vi.fn(),
	afterNavigate: vi.fn()
}));
