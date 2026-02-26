import type { DashboardState } from '$lib/types';
import { mockDashboardState } from './mock';
import { getApiUrl } from '$lib/stores/config';

const USE_MOCK = import.meta.env.VITE_USE_MOCK === 'true';

export async function getDashboardState(token: string): Promise<DashboardState> {
	if (USE_MOCK) {
		return mockDashboardState;
	}

	const res = await fetch(`${getApiUrl()}/api/dashboard/state`, {
		headers: { Authorization: `Bearer ${token}` }
	});

	if (!res.ok) {
		throw new Error(`Dashboard fetch failed: ${res.status}`);
	}

	return res.json();
}

export async function createInstance(
	token: string,
	data: { name: string; region: string }
): Promise<void> {
	if (USE_MOCK) return;

	const res = await fetch(`${getApiUrl()}/api/instances`, {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify(data)
	});

	if (!res.ok) {
		throw new Error(`Create instance failed: ${res.status}`);
	}
}

export async function restartInstance(token: string, instanceId: string): Promise<void> {
	if (USE_MOCK) return;

	const res = await fetch(`${getApiUrl()}/api/instances/${instanceId}/restart`, {
		method: 'POST',
		headers: { Authorization: `Bearer ${token}` }
	});

	if (!res.ok) {
		throw new Error(`Restart failed: ${res.status}`);
	}
}

export async function deleteInstance(token: string, instanceId: string): Promise<void> {
	if (USE_MOCK) return;

	const res = await fetch(`${getApiUrl()}/api/instances/${instanceId}`, {
		method: 'DELETE',
		headers: { Authorization: `Bearer ${token}` }
	});

	if (!res.ok) {
		throw new Error(`Delete failed: ${res.status}`);
	}
}

export async function createPortalSession(token: string): Promise<{ url: string }> {
	if (USE_MOCK) {
		return { url: '/dashboard/billing' };
	}

	const res = await fetch(`${getApiUrl()}/api/billing/portal`, {
		method: 'POST',
		headers: { Authorization: `Bearer ${token}` }
	});

	if (!res.ok) {
		throw new Error(`Create portal session failed: ${res.status}`);
	}

	return res.json();
}
