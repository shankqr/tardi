import * as Sentry from '@sentry/sveltekit';
import type { DashboardState, ModelInfo, Snapshot, VpsInstance } from '$lib/types';
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

export async function getModels(): Promise<{ models: ModelInfo[]; default_model_id: string }> {
	if (USE_MOCK) {
		return {
			models: [
				{ id: 'nvidia/nemotron-3-super-120b-a12b:free', display_name: 'Nemotron 3 Super', provider: 'openrouter', tier: 'free', is_default: true, description: 'Free 120B parameter model', context_length: 131072, prompt_price: '0', completion_price: '0' },
				{ id: 'xiaomi/mimo-v2-pro', display_name: 'MiMo V2 Pro', provider: 'openrouter', tier: 'paid', is_default: false, description: 'High-performance model', context_length: 131072, prompt_price: '0.0000006', completion_price: '0.0000024' },
				{ id: 'minimax/minimax-m2.7', display_name: 'MiniMax M2.7', provider: 'openrouter', tier: 'paid', is_default: false, description: 'MiniMax large language model', context_length: 131072, prompt_price: '0.0000006', completion_price: '0.0000024' },
				{ id: 'z-ai/glm-5-turbo', display_name: 'GLM-5 Turbo', provider: 'openrouter', tier: 'paid', is_default: false, description: 'Zhipu AI fast inference model', context_length: 131072, prompt_price: '0.0000006', completion_price: '0.0000024' },
				{ id: 'anthropic/claude-opus-4.6', display_name: 'Claude Opus 4.6', provider: 'openrouter', tier: 'paid', is_default: false, description: 'Anthropic\'s most capable model', context_length: 200000, prompt_price: '0.000015', completion_price: '0.000075' },
				{ id: 'openai/gpt-5.4', display_name: 'GPT-5.4', provider: 'openrouter', tier: 'paid', is_default: false, description: 'OpenAI flagship model', context_length: 131072, prompt_price: '0.000002', completion_price: '0.000008' },
				{ id: 'qwen/qwen3.5-397b-a17b', display_name: 'Qwen 3.5 397B', provider: 'openrouter', tier: 'paid', is_default: false, description: 'Alibaba large MoE model', context_length: 131072, prompt_price: '0.0000006', completion_price: '0.0000024' },
				{ id: 'anthropic/claude-sonnet-4.6', display_name: 'Claude Sonnet 4.6', provider: 'openrouter', tier: 'paid', is_default: false, description: 'Anthropic\'s balanced model for coding and analysis', context_length: 200000, prompt_price: '0.000003', completion_price: '0.000015' },
				{ id: 'minimax/minimax-m2.5:free', display_name: 'MiniMax M2.5', provider: 'openrouter', tier: 'free', is_default: false, description: 'MiniMax free model', context_length: 131072, prompt_price: '0', completion_price: '0' }
			],
			default_model_id: 'nvidia/nemotron-3-super-120b-a12b:free'
		};
	}

	return apiFetch(`${getApiUrl()}/api/models`, {}, 'getModels');
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
				model: 'anthropic/claude-sonnet-4.5',
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

export interface HealthCheck {
	name: string;
	status: 'pass' | 'fail' | 'warn' | 'info';
	message: string;
	detail: string;
}

export interface DoctorResult {
	checks?: HealthCheck[];
	raw?: string;
	error?: string;
	detail?: string;
}

export async function runDoctor(token: string, instanceId: string): Promise<DoctorResult> {
	if (USE_MOCK) {
		return {
			checks: [
				{ name: 'Container', status: 'pass', message: 'Running and healthy', detail: 'Restarts: 0' },
				{
					name: 'Telegram: Double Replies',
					status: 'pass',
					message: 'Streaming is off',
					detail: "Messages won't be sent twice"
				},
				{
					name: 'Telegram: Pairing',
					status: 'pass',
					message: 'DM policy is open',
					detail: 'Users can message the bot without pairing'
				},
				{ name: 'API Keys', status: 'pass', message: 'OpenRouter', detail: '' },
				{ name: 'Config Sync', status: 'pass', message: 'In sync (version 3)', detail: '' },
				{ name: 'Recent Logs', status: 'pass', message: 'No errors in last 50 log lines', detail: '' },
				{ name: 'Disk Space', status: 'pass', message: '42% used (11G free)', detail: '' },
				{ name: 'Memory', status: 'pass', message: '312MB / 1024MB (30%)', detail: '' }
			]
		};
	}

	// Doctor SSHes into VPS and runs checks (~45s). Use AbortController
	// to prevent the request from hanging forever if the backend is slow.
	const controller = new AbortController();
	const timeout = setTimeout(() => controller.abort(), 60_000);
	try {
		return await apiFetch(
			`${getApiUrl()}/api/instances/${instanceId}/doctor`,
			{ method: 'POST', headers: authHeaders(token), signal: controller.signal },
			'runDoctor'
		);
	} catch (err) {
		if (err instanceof DOMException && err.name === 'AbortError') {
			return { error: 'Health check timed out', detail: 'Could not reach your agent within 60 seconds. The VPS may be unreachable.' };
		}
		throw err;
	} finally {
		clearTimeout(timeout);
	}
}

export async function createPortalSession(token: string, flow?: string): Promise<{ url: string }> {
	if (USE_MOCK) {
		return { url: '/dashboard/billing' };
	}

	const params = flow ? `?flow=${flow}` : '';
	return apiFetch(
		`${getApiUrl()}/api/billing/portal${params}`,
		{ method: 'POST', headers: authHeaders(token) },
		'createPortalSession'
	);
}
