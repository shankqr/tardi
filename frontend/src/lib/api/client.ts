import type { DashboardState } from '$lib/types';
import { mockDashboardState } from './mock';

const USE_MOCK = true;
const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080';

export async function getDashboardState(token: string): Promise<DashboardState> {
	if (USE_MOCK) {
		return mockDashboardState;
	}

	const res = await fetch(`${API_BASE}/api/dashboard/state`, {
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

	const res = await fetch(`${API_BASE}/api/instances`, {
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

	const res = await fetch(`${API_BASE}/api/instances/${instanceId}/restart`, {
		method: 'POST',
		headers: { Authorization: `Bearer ${token}` }
	});

	if (!res.ok) {
		throw new Error(`Restart failed: ${res.status}`);
	}
}

export async function deleteInstance(token: string, instanceId: string): Promise<void> {
	if (USE_MOCK) return;

	const res = await fetch(`${API_BASE}/api/instances/${instanceId}`, {
		method: 'DELETE',
		headers: { Authorization: `Bearer ${token}` }
	});

	if (!res.ok) {
		throw new Error(`Delete failed: ${res.status}`);
	}
}
