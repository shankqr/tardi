import * as Sentry from '@sentry/sveltekit';
import type { DashboardState, GoogleOAuthStatus, ModelInfo, Snapshot, VpsInstance } from '$lib/types';
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
				{ id: 'nvidia/nemotron-3-super-120b-a12b:free', display_name: 'Nemotron 3 Super', provider: 'openrouter', tier: 'free', is_default: true, tags: ['Best Free'], description: 'Free 120B parameter model', context_length: 131072, prompt_price: '0', completion_price: '0' },
				{ id: 'qwen/qwen3.6-plus:free', display_name: 'Qwen 3.6 Plus', provider: 'openrouter', tier: 'free', is_default: false, description: 'Qwen 3.6 Plus free model', context_length: 131072, prompt_price: '0', completion_price: '0' },
				{ id: 'xiaomi/mimo-v2-pro', display_name: 'MiMo V2 Pro', provider: 'openrouter', tier: 'paid', is_default: false, tags: ['Best Value'], description: 'High-performance model', context_length: 131072, prompt_price: '0.0000006', completion_price: '0.0000024' },
				{ id: 'minimax/minimax-m2.7', display_name: 'MiniMax M2.7', provider: 'openrouter', tier: 'paid', is_default: false, tags: ['Best Value'], description: 'MiniMax large language model', context_length: 131072, prompt_price: '0.0000006', completion_price: '0.0000024' },
				{ id: 'z-ai/glm-5-turbo', display_name: 'GLM-5 Turbo', provider: 'openrouter', tier: 'paid', is_default: false, description: 'Zhipu AI fast inference model', context_length: 131072, prompt_price: '0.0000006', completion_price: '0.0000024' },
				{ id: 'anthropic/claude-opus-4.6', display_name: 'Claude Opus 4.6', provider: 'openrouter', tier: 'paid', is_default: false, tags: ['Best Quality'], description: 'Anthropic\'s most capable model', context_length: 200000, prompt_price: '0.000015', completion_price: '0.000075' },
				{ id: 'openai/gpt-5.4', display_name: 'GPT-5.4', provider: 'openrouter', tier: 'paid', is_default: false, description: 'OpenAI flagship model', context_length: 131072, prompt_price: '0.000002', completion_price: '0.000008' },
				{ id: 'qwen/qwen3.5-397b-a17b', display_name: 'Qwen 3.5 397B', provider: 'openrouter', tier: 'paid', is_default: false, description: 'Alibaba large MoE model', context_length: 131072, prompt_price: '0.0000006', completion_price: '0.0000024' },
				{ id: 'arcee-ai/trinity-large-thinking', display_name: 'Trinity Large Thinking', provider: 'openrouter', tier: 'paid', is_default: false, description: 'Arcee AI reasoning model', context_length: 131072, prompt_price: '0.0000006', completion_price: '0.0000024' },
				{ id: 'anthropic/claude-sonnet-4.6', display_name: 'Claude Sonnet 4.6', provider: 'openrouter', tier: 'paid', is_default: false, description: 'Anthropic\'s balanced model for coding and analysis', context_length: 200000, prompt_price: '0.000003', completion_price: '0.000015' }
			],
			default_model_id: 'nvidia/nemotron-3-super-120b-a12b:free'
		};
	}

	return apiFetch(`${getApiUrl()}/api/models`, {}, 'getModels');
}

export async function createInstance(
	token: string,
	data: { name: string; region: string; framework?: string }
): Promise<VpsInstance> {
	if (USE_MOCK) {
		return {
			id: 'mock-instance',
			name: data.name,
			framework: (data.framework as 'openclaw' | 'hermes') ?? 'openclaw',
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
			framework: 'openclaw',
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

// --- Dashboard Token ---

export async function getDashboardToken(
	instanceId: string,
	token: string
): Promise<{ token: string }> {
	if (USE_MOCK) {
		await new Promise((r) => setTimeout(r, 500));
		return { token: 'mock-scoped-token-abc123' };
	}

	const controller = new AbortController();
	const timeout = setTimeout(() => controller.abort(), 30_000);
	try {
		return await apiFetch(
			`${getApiUrl()}/api/instances/${instanceId}/dashboard-token`,
			{ method: 'POST', headers: authHeaders(token), signal: controller.signal },
			'getDashboardToken'
		);
	} finally {
		clearTimeout(timeout);
	}
}

// --- Google OAuth ---

export async function getGoogleOAuthUrl(token: string): Promise<{ redirect_url: string }> {
	if (USE_MOCK) {
		return { redirect_url: '#' };
	}

	return apiFetch(
		`${getApiUrl()}/api/oauth/google/authorize`,
		{ headers: authHeaders(token) },
		'getGoogleOAuthUrl'
	);
}

export async function getGoogleOAuthStatus(token: string): Promise<GoogleOAuthStatus> {
	if (USE_MOCK) {
		return { connected: false };
	}

	return apiFetch(
		`${getApiUrl()}/api/oauth/google/status`,
		{ headers: authHeaders(token) },
		'getGoogleOAuthStatus'
	);
}

export async function disconnectGoogle(token: string): Promise<{ disconnected: boolean }> {
	if (USE_MOCK) {
		return { disconnected: true };
	}

	return apiFetch(
		`${getApiUrl()}/api/oauth/google/disconnect`,
		{ method: 'POST', headers: authHeaders(token) },
		'disconnectGoogle'
	);
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
