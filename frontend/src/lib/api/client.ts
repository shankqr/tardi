import * as Sentry from '@sentry/sveltekit';
import type { DashboardState, Snapshot, VpsInstance } from '$lib/types';
import { mockDashboardState } from './mock';
import { getApiUrl } from '$lib/stores/config';

const USE_MOCK = import.meta.env.VITE_USE_MOCK === 'true';

async function apiFetch<T>(
	url: string,
	options: RequestInit,
	operation: string
): Promise<T> {
	Sentry.addBreadcrumb({
		category: 'api',
		message: `${options.method || 'GET'} ${operation}`,
		data: { url },
		level: 'info'
	});

	const res = await fetch(url, options);

	if (!res.ok) {
		const body = await res.json().catch(() => ({}));
		const error = new Error(body.error || `${operation} failed: ${res.status}`);

		Sentry.setContext('api_error', {
			operation,
			url,
			status: res.status,
			responseBody: body
		});

		throw error;
	}

	return res.json();
}

async function apiFetchVoid(
	url: string,
	options: RequestInit,
	operation: string
): Promise<void> {
	Sentry.addBreadcrumb({
		category: 'api',
		message: `${options.method || 'GET'} ${operation}`,
		data: { url },
		level: 'info'
	});

	const res = await fetch(url, options);

	if (!res.ok) {
		const body = await res.json().catch(() => ({}));
		const error = new Error(body.error || `${operation} failed: ${res.status}`);

		Sentry.setContext('api_error', {
			operation,
			url,
			status: res.status,
			responseBody: body
		});

		throw error;
	}
}

function authHeaders(token: string): Record<string, string> {
	return { Authorization: `Bearer ${token}` };
}

function authJsonHeaders(token: string): Record<string, string> {
	return {
		Authorization: `Bearer ${token}`,
		'Content-Type': 'application/json'
	};
}

export async function getDashboardState(token: string): Promise<DashboardState> {
	if (USE_MOCK) return mockDashboardState;

	return apiFetch(
		`${getApiUrl()}/api/dashboard/state`,
		{ headers: authHeaders(token), cache: 'no-store' },
		'getDashboardState'
	);
}

export async function createInstance(
	token: string,
	data: { name: string; region: string }
): Promise<VpsInstance> {
	if (USE_MOCK) {
		return {
			id: 'mock-instance',
			name: data.name,
			status: 'requested',
			provider: 'hetzner',
			ipv4: null,
			region: data.region,
			agent_status: null,
			last_heartbeat_at: null,
			dashboard_url: null,
			created_at: new Date().toISOString()
		};
	}

	return apiFetch(
		`${getApiUrl()}/api/instances`,
		{
			method: 'POST',
			headers: authJsonHeaders(token),
			body: JSON.stringify(data)
		},
		'createInstance'
	);
}

export async function restartInstance(token: string, instanceId: string): Promise<void> {
	if (USE_MOCK) return;

	return apiFetchVoid(
		`${getApiUrl()}/api/instances/${instanceId}/restart`,
		{ method: 'POST', headers: authHeaders(token) },
		'restartInstance'
	);
}

export async function deleteInstance(token: string, instanceId: string): Promise<void> {
	if (USE_MOCK) return;

	return apiFetchVoid(
		`${getApiUrl()}/api/instances/${instanceId}`,
		{ method: 'DELETE', headers: authHeaders(token) },
		'deleteInstance'
	);
}

export async function renameInstance(
	token: string,
	instanceId: string,
	name: string
): Promise<VpsInstance> {
	if (USE_MOCK) {
		return {
			id: instanceId,
			name,
			status: 'active',
			provider: 'hetzner',
			ipv4: null,
			region: 'eu',
			agent_status: null,
			last_heartbeat_at: null,
			dashboard_url: null,
			created_at: new Date().toISOString()
		};
	}

	return apiFetch(
		`${getApiUrl()}/api/instances/${instanceId}`,
		{
			method: 'PATCH',
			headers: authJsonHeaders(token),
			body: JSON.stringify({ name })
		},
		'renameInstance'
	);
}

export async function resetPassword(
	token: string,
	instanceId: string
): Promise<{ root_password: string }> {
	if (USE_MOCK) {
		return { root_password: 'mock-reset-password-12345' };
	}

	return apiFetch(
		`${getApiUrl()}/api/instances/${instanceId}/reset-password`,
		{ method: 'POST', headers: authHeaders(token) },
		'resetPassword'
	);
}

export async function getAgentConfig(
	token: string,
	instanceId: string
): Promise<{ config: Record<string, unknown>; version: number }> {
	if (USE_MOCK) {
		return {
			config: {
				provider: 'openrouter',
				model: 'nvidia/nemotron-3-super-120b-a12b:free',
				openrouter_api_key: 'sk-or-...1234'
			},
			version: 1
		};
	}

	return apiFetch(
		`${getApiUrl()}/api/instances/${instanceId}/config`,
		{ headers: authHeaders(token) },
		'getAgentConfig'
	);
}

export async function updateAgentConfig(
	token: string,
	instanceId: string,
	config: Record<string, unknown>
): Promise<{ config: Record<string, unknown>; version: number }> {
	if (USE_MOCK) {
		return { config, version: 1 };
	}

	return apiFetch(
		`${getApiUrl()}/api/instances/${instanceId}/config`,
		{
			method: 'PUT',
			headers: authJsonHeaders(token),
			body: JSON.stringify({ config })
		},
		'updateAgentConfig'
	);
}

export async function syncConfig(
	token: string,
	instanceId: string
): Promise<{ synced: boolean; config_version?: number; error?: string }> {
	if (USE_MOCK) {
		return { synced: true, config_version: 1 };
	}

	const url = `${getApiUrl()}/api/instances/${instanceId}/sync-config`;
	const controller = new AbortController();
	const timer = setTimeout(() => controller.abort(), 20000);

	Sentry.addBreadcrumb({
		category: 'api',
		message: 'POST syncConfig',
		data: { url },
		level: 'info'
	});

	try {
		const res = await fetch(url, {
			method: 'POST',
			headers: authHeaders(token),
			signal: controller.signal
		});

		if (!res.ok) {
			const body = await res.json().catch(() => ({}));
			const error = new Error(body.error || `Sync failed: ${res.status}`);
			Sentry.setContext('api_error', {
				operation: 'syncConfig',
				url,
				status: res.status,
				responseBody: body
			});
			throw error;
		}

		return res.json();
	} finally {
		clearTimeout(timer);
	}
}

export async function getSyncStatus(
	token: string,
	instanceId: string
): Promise<{ status: string; message: string }> {
	if (USE_MOCK) {
		return { status: 'completed', message: 'Mock sync complete' };
	}

	return apiFetch(
		`${getApiUrl()}/api/instances/${instanceId}/sync-status`,
		{ headers: authHeaders(token) },
		'getSyncStatus'
	);
}

export async function createSnapshot(
	token: string,
	instanceId: string,
	name: string
): Promise<Snapshot> {
	if (USE_MOCK) {
		return {
			id: 'mock-snap',
			instance_id: instanceId,
			name,
			status: 'creating',
			created_at: new Date().toISOString()
		};
	}

	return apiFetch(
		`${getApiUrl()}/api/instances/${instanceId}/snapshots`,
		{
			method: 'POST',
			headers: authJsonHeaders(token),
			body: JSON.stringify({ name })
		},
		'createSnapshot'
	);
}

export async function restoreSnapshot(token: string, snapshotId: string): Promise<void> {
	if (USE_MOCK) return;

	return apiFetchVoid(
		`${getApiUrl()}/api/snapshots/${snapshotId}/restore`,
		{ method: 'POST', headers: authHeaders(token) },
		'restoreSnapshot'
	);
}

export async function deleteSnapshot(token: string, snapshotId: string): Promise<void> {
	if (USE_MOCK) return;

	return apiFetchVoid(
		`${getApiUrl()}/api/snapshots/${snapshotId}`,
		{ method: 'DELETE', headers: authHeaders(token) },
		'deleteSnapshot'
	);
}

export async function getWhatsAppQR(
	token: string,
	instanceId: string,
	force = false
): Promise<{ qr_data_url: string; message: string }> {
	if (USE_MOCK) {
		return { qr_data_url: '', message: 'Mock mode' };
	}

	const url = `${getApiUrl()}/api/instances/${instanceId}/whatsapp/qr${force ? '?force=true' : ''}`;
	const controller = new AbortController();
	const timer = setTimeout(() => controller.abort(), 50000);

	Sentry.addBreadcrumb({
		category: 'api',
		message: 'POST getWhatsAppQR',
		data: { url },
		level: 'info'
	});

	try {
		const res = await fetch(url, {
			method: 'POST',
			headers: authHeaders(token),
			signal: controller.signal
		});

		if (!res.ok) {
			const body = await res.json().catch(() => ({}));
			const error = new Error(body.error || `WhatsApp QR failed: ${res.status}`);
			Sentry.setContext('api_error', {
				operation: 'getWhatsAppQR',
				url,
				status: res.status,
				responseBody: body
			});
			throw error;
		}

		return res.json();
	} finally {
		clearTimeout(timer);
	}
}

export async function getWhatsAppStatus(
	token: string,
	instanceId: string
): Promise<{ linked: boolean; phone: string }> {
	if (USE_MOCK) {
		return { linked: false, phone: '' };
	}

	const res = await fetch(`${getApiUrl()}/api/instances/${instanceId}/whatsapp/status`, {
		headers: authHeaders(token)
	});

	if (!res.ok) {
		return { linked: false, phone: '' };
	}

	return res.json();
}

export async function connectTelegram(
	token: string,
	instanceId: string,
	botToken: string
): Promise<{ connected: boolean }> {
	if (USE_MOCK) {
		return { connected: true };
	}

	return apiFetch(
		`${getApiUrl()}/api/instances/${instanceId}/telegram/connect`,
		{
			method: 'POST',
			headers: authJsonHeaders(token),
			body: JSON.stringify({ bot_token: botToken })
		},
		'connectTelegram'
	);
}

export async function cleanupTelegramConfig(
	token: string,
	instanceId: string
): Promise<{ cleaned: boolean; error?: string }> {
	if (USE_MOCK) {
		return { cleaned: true };
	}

	return apiFetch(
		`${getApiUrl()}/api/instances/${instanceId}/telegram/cleanup`,
		{ method: 'POST', headers: authHeaders(token) },
		'cleanupTelegramConfig'
	);
}

export async function disconnectTelegram(
	token: string,
	instanceId: string
): Promise<{ connected: boolean }> {
	if (USE_MOCK) {
		return { connected: false };
	}

	return apiFetch(
		`${getApiUrl()}/api/instances/${instanceId}/telegram/disconnect`,
		{ method: 'POST', headers: authHeaders(token) },
		'disconnectTelegram'
	);
}

export async function createPortalSession(token: string): Promise<{ url: string }> {
	if (USE_MOCK) {
		return { url: '/dashboard/billing' };
	}

	return apiFetch(
		`${getApiUrl()}/api/billing/portal`,
		{ method: 'POST', headers: authHeaders(token) },
		'createPortalSession'
	);
}
