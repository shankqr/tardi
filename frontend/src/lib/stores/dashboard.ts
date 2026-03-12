import * as Sentry from '@sentry/sveltekit';
import { writable } from 'svelte/store';
import type { DashboardState } from '$lib/types';
import { getDashboardState } from '$lib/api/client';
import { getIdToken } from '$lib/stores/auth';

export const dashboardState = writable<DashboardState | null>(null);
export const dashboardLoading = writable(true);
export const dashboardError = writable<string | null>(null);

let pollInterval: ReturnType<typeof setInterval> | null = null;

export async function refreshDashboard() {
	return fetchDashboard();
}

async function fetchDashboard() {
	try {
		const token = await getIdToken();
		if (!token) return;
		const state = await getDashboardState(token);
		dashboardState.set(state);
		dashboardError.set(null);
	} catch (err) {
		const message = err instanceof Error ? err.message : 'Failed to load dashboard';
		dashboardError.set(message);
		Sentry.captureException(err, { tags: { source: 'dashboard-polling' } });
	} finally {
		dashboardLoading.set(false);
	}
}

export function startPolling() {
	fetchDashboard();
	pollInterval = setInterval(fetchDashboard, 5000);
}

export function stopPolling() {
	if (pollInterval) {
		clearInterval(pollInterval);
		pollInterval = null;
	}
}
