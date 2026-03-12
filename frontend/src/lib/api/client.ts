import type { DashboardState, Snapshot, VpsInstance } from '$lib/types';
import { mockDashboardState } from './mock';
import { getApiUrl } from '$lib/stores/config';

const USE_MOCK = import.meta.env.VITE_USE_MOCK === 'true';

export async function getDashboardState(token: string): Promise<DashboardState> {
	if (USE_MOCK) {
		return mockDashboardState;
	}

	const res = await fetch(`${getApiUrl()}/api/dashboard/state`, {
		headers: { Authorization: `Bearer ${token}` },
		cache: 'no-store'
	});

	if (!res.ok) {
		throw new Error(`Dashboard fetch failed: ${res.status}`);
	}

	return res.json();
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

	return res.json();
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

export async function renameInstance(
	token: string,
	instanceId: string,
	name: string
): Promise<VpsInstance> {
	if (USE_MOCK) {
		return { id: instanceId, name, status: 'active', provider: 'hetzner', ipv4: null, region: 'eu', agent_status: null, last_heartbeat_at: null, dashboard_url: null, created_at: new Date().toISOString() };
	}

	const res = await fetch(`${getApiUrl()}/api/instances/${instanceId}`, {
		method: 'PATCH',
		headers: {
			Authorization: `Bearer ${token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({ name })
	});

	if (!res.ok) {
		throw new Error(`Rename failed: ${res.status}`);
	}

	return res.json();
}

export async function resetPassword(
	token: string,
	instanceId: string
): Promise<{ root_password: string }> {
	if (USE_MOCK) {
		return { root_password: 'mock-reset-password-12345' };
	}

	const res = await fetch(`${getApiUrl()}/api/instances/${instanceId}/reset-password`, {
		method: 'POST',
		headers: { Authorization: `Bearer ${token}` }
	});

	if (!res.ok) {
		throw new Error(`Reset password failed: ${res.status}`);
	}

	return res.json();
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

	const res = await fetch(`${getApiUrl()}/api/instances/${instanceId}/config`, {
		headers: { Authorization: `Bearer ${token}` }
	});

	if (!res.ok) {
		throw new Error(`Get config failed: ${res.status}`);
	}

	return res.json();
}

export async function updateAgentConfig(
	token: string,
	instanceId: string,
	config: Record<string, unknown>
): Promise<{ config: Record<string, unknown>; version: number }> {
	if (USE_MOCK) {
		return { config, version: 1 };
	}

	const res = await fetch(`${getApiUrl()}/api/instances/${instanceId}/config`, {
		method: 'PUT',
		headers: {
			Authorization: `Bearer ${token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({ config })
	});

	if (!res.ok) {
		throw new Error(`Update config failed: ${res.status}`);
	}

	return res.json();
}

export async function syncConfig(
	token: string,
	instanceId: string
): Promise<{ synced: boolean; config_version?: number; error?: string }> {
	if (USE_MOCK) {
		return { synced: true, config_version: 1 };
	}

	const controller = new AbortController();
	const timer = setTimeout(() => controller.abort(), 100000);
	const res = await fetch(`${getApiUrl()}/api/instances/${instanceId}/sync-config`, {
		method: 'POST',
		headers: { Authorization: `Bearer ${token}` },
		signal: controller.signal
	}).finally(() => clearTimeout(timer));

	if (!res.ok) {
		const body = await res.json().catch(() => ({}));
		throw new Error(body.error || `Sync failed: ${res.status}`);
	}

	return res.json();
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

	const res = await fetch(`${getApiUrl()}/api/instances/${instanceId}/snapshots`, {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({ name })
	});

	if (!res.ok) {
		throw new Error(`Create snapshot failed: ${res.status}`);
	}

	return res.json();
}

export async function restoreSnapshot(token: string, snapshotId: string): Promise<void> {
	if (USE_MOCK) return;

	const res = await fetch(`${getApiUrl()}/api/snapshots/${snapshotId}/restore`, {
		method: 'POST',
		headers: { Authorization: `Bearer ${token}` }
	});

	if (!res.ok) {
		throw new Error(`Restore failed: ${res.status}`);
	}
}

export async function deleteSnapshot(token: string, snapshotId: string): Promise<void> {
	if (USE_MOCK) return;

	const res = await fetch(`${getApiUrl()}/api/snapshots/${snapshotId}`, {
		method: 'DELETE',
		headers: { Authorization: `Bearer ${token}` }
	});

	if (!res.ok) {
		throw new Error(`Delete snapshot failed: ${res.status}`);
	}
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
	const res = await fetch(url, {
		method: 'POST',
		headers: { Authorization: `Bearer ${token}` },
		signal: controller.signal
	}).finally(() => clearTimeout(timer));

	if (!res.ok) {
		const body = await res.json().catch(() => ({}));
		throw new Error(body.error || `WhatsApp QR failed: ${res.status}`);
	}

	return res.json();
}

export async function getWhatsAppStatus(
	token: string,
	instanceId: string
): Promise<{ linked: boolean; phone: string }> {
	if (USE_MOCK) {
		return { linked: false, phone: '' };
	}

	const res = await fetch(`${getApiUrl()}/api/instances/${instanceId}/whatsapp/status`, {
		headers: { Authorization: `Bearer ${token}` }
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

	const res = await fetch(`${getApiUrl()}/api/instances/${instanceId}/telegram/connect`, {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${token}`,
			'Content-Type': 'application/json'
		},
		body: JSON.stringify({ bot_token: botToken })
	});

	if (!res.ok) {
		const body = await res.json().catch(() => ({}));
		throw new Error(body.error || `Telegram connect failed: ${res.status}`);
	}

	return res.json();
}

export async function disconnectTelegram(
	token: string,
	instanceId: string
): Promise<{ connected: boolean }> {
	if (USE_MOCK) {
		return { connected: false };
	}

	const res = await fetch(`${getApiUrl()}/api/instances/${instanceId}/telegram/disconnect`, {
		method: 'POST',
		headers: { Authorization: `Bearer ${token}` }
	});

	if (!res.ok) {
		throw new Error(`Telegram disconnect failed: ${res.status}`);
	}

	return res.json();
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
