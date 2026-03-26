import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';

// Mock Sentry
vi.mock('@sentry/sveltekit', () => ({
	captureException: vi.fn(),
	addBreadcrumb: vi.fn(),
	setContext: vi.fn()
}));

// Mock the auth store
vi.mock('$lib/stores/auth', () => ({
	getIdToken: vi.fn().mockResolvedValue('mock-token')
}));

// Mock the API client
vi.mock('$lib/api/client', () => ({
	getDashboardState: vi.fn()
}));

import { dashboardState, dashboardLoading, dashboardError, startPolling, stopPolling, refreshDashboard } from './dashboard';
import { getDashboardState } from '$lib/api/client';
import type { DashboardState } from '$lib/types';

const mockGetDashboardState = getDashboardState as ReturnType<typeof vi.fn>;

describe('dashboard store', () => {
	beforeEach(() => {
		vi.useFakeTimers();
		// Reset store values
		dashboardState.set(null);
		dashboardLoading.set(true);
		dashboardError.set(null);
		mockGetDashboardState.mockReset();
	});

	afterEach(() => {
		stopPolling();
		vi.useRealTimers();
	});

	it('has null initial dashboardState', () => {
		expect(get(dashboardState)).toBeNull();
	});

	it('has true initial dashboardLoading', () => {
		expect(get(dashboardLoading)).toBe(true);
	});

	it('has null initial dashboardError', () => {
		expect(get(dashboardError)).toBeNull();
	});

	it('refreshDashboard fetches and updates state', async () => {
		const mockState: DashboardState = {
			instances: [],
			subscription: null,
			pending_jobs: 0,
			snapshots: []
		};
		mockGetDashboardState.mockResolvedValue(mockState);

		await refreshDashboard();

		expect(get(dashboardState)).toEqual(mockState);
		expect(get(dashboardLoading)).toBe(false);
		expect(get(dashboardError)).toBeNull();
	});

	it('refreshDashboard sets error on failure', async () => {
		mockGetDashboardState.mockRejectedValue(new Error('Network error'));

		await refreshDashboard();

		expect(get(dashboardError)).toBe('Network error');
		expect(get(dashboardLoading)).toBe(false);
	});

	it('refreshDashboard handles non-Error exceptions', async () => {
		mockGetDashboardState.mockRejectedValue('string error');

		await refreshDashboard();

		expect(get(dashboardError)).toBe('Failed to load dashboard');
		expect(get(dashboardLoading)).toBe(false);
	});

	it('startPolling calls fetchDashboard immediately', async () => {
		const mockState: DashboardState = {
			instances: [],
			subscription: null,
			pending_jobs: 0,
			snapshots: []
		};
		mockGetDashboardState.mockResolvedValue(mockState);

		startPolling();
		// Flush the initial microtask (the immediate fetchDashboard call)
		await vi.advanceTimersByTimeAsync(0);

		expect(mockGetDashboardState).toHaveBeenCalled();
	});

	it('stopPolling stops the interval', async () => {
		const mockState: DashboardState = {
			instances: [],
			subscription: null,
			pending_jobs: 0,
			snapshots: []
		};
		mockGetDashboardState.mockResolvedValue(mockState);

		startPolling();
		await vi.advanceTimersByTimeAsync(0);

		stopPolling();
		mockGetDashboardState.mockClear();

		await vi.advanceTimersByTimeAsync(10000);
		expect(mockGetDashboardState).not.toHaveBeenCalled();
	});
});
