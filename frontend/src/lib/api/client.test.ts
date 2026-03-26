import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock Sentry
vi.mock('@sentry/sveltekit', () => ({
	captureException: vi.fn(),
	addBreadcrumb: vi.fn(),
	setContext: vi.fn()
}));

// Mock config store
vi.mock('$lib/stores/config', () => ({
	getApiUrl: () => 'http://localhost:8080'
}));

describe('API client - mock mode', () => {
	beforeEach(() => {
		vi.stubEnv('VITE_USE_MOCK', 'true');
		vi.resetModules();
	});

	it('getDashboardState returns mock data', async () => {
		const { getDashboardState } = await import('./client');
		const state = await getDashboardState('test-token');

		expect(state.instances).toBeDefined();
		expect(state.instances.length).toBeGreaterThan(0);
		expect(state.subscription).toBeDefined();
		expect(state.subscription?.status).toBe('active');
	});

	it('getModels returns mock models', async () => {
		const { getModels } = await import('./client');
		const result = await getModels();

		expect(result.models.length).toBeGreaterThan(0);
		expect(result.default_model_id).toBeTruthy();
	});

	it('createInstance returns mock instance', async () => {
		const { createInstance } = await import('./client');
		const instance = await createInstance('token', { name: 'test', region: 'eu' });

		expect(instance.id).toBe('mock-instance');
		expect(instance.name).toBe('test');
		expect(instance.status).toBe('requested');
		expect(instance.region).toBe('eu');
	});

	it('deleteInstance resolves in mock mode', async () => {
		const { deleteInstance } = await import('./client');
		await expect(deleteInstance('token', 'inst-1')).resolves.toBeUndefined();
	});

	it('restartInstance resolves in mock mode', async () => {
		const { restartInstance } = await import('./client');
		await expect(restartInstance('token', 'inst-1')).resolves.toBeUndefined();
	});

	it('renameInstance returns updated name', async () => {
		const { renameInstance } = await import('./client');
		const result = await renameInstance('token', 'inst-1', 'new-name');
		expect(result.name).toBe('new-name');
		expect(result.id).toBe('inst-1');
	});

	it('getAgentConfig returns mock config', async () => {
		const { getAgentConfig } = await import('./client');
		const result = await getAgentConfig('token', 'inst-1');
		expect(result.config).toBeDefined();
		expect(result.version).toBe(1);
	});

	it('syncConfig returns synced in mock mode', async () => {
		const { syncConfig } = await import('./client');
		const result = await syncConfig('token', 'inst-1');
		expect(result.synced).toBe(true);
	});

	it('runDoctor returns health checks', async () => {
		const { runDoctor } = await import('./client');
		const result = await runDoctor('token', 'inst-1');
		expect(result.checks).toBeDefined();
		expect(result.checks!.length).toBeGreaterThan(0);
		expect(result.checks![0].name).toBe('Container');
		expect(result.checks![0].status).toBe('pass');
	});

	it('createPortalSession returns URL', async () => {
		const { createPortalSession } = await import('./client');
		const result = await createPortalSession('token');
		expect(result.url).toBe('/dashboard/billing');
	});
});

describe('API client - real mode', () => {
	let fetchSpy: ReturnType<typeof vi.fn>;

	beforeEach(() => {
		vi.stubEnv('VITE_USE_MOCK', 'false');
		vi.resetModules();
		fetchSpy = vi.fn();
		vi.stubGlobal('fetch', fetchSpy);
	});

	it('getDashboardState calls correct URL with auth header', async () => {
		const mockState = {
			instances: [],
			subscription: null,
			pending_jobs: 0,
			snapshots: []
		};
		fetchSpy.mockResolvedValue({
			ok: true,
			json: () => Promise.resolve(mockState)
		});

		const { getDashboardState } = await import('./client');
		const result = await getDashboardState('my-token');

		expect(fetchSpy).toHaveBeenCalledWith(
			'http://localhost:8080/api/dashboard/state',
			expect.objectContaining({
				headers: { Authorization: 'Bearer my-token' },
				cache: 'no-store'
			})
		);
		expect(result).toEqual(mockState);
	});

	it('createInstance sends POST with correct body', async () => {
		const mockInstance = {
			id: 'new-id',
			name: 'my-agent',
			status: 'requested',
			provider: 'hetzner',
			ipv4: null,
			region: 'eu',
			agent_status: null,
			last_heartbeat_at: null,
			dashboard_url: null,
			created_at: '2026-01-01T00:00:00Z'
		};
		fetchSpy.mockResolvedValue({
			ok: true,
			json: () => Promise.resolve(mockInstance)
		});

		const { createInstance } = await import('./client');
		await createInstance('my-token', { name: 'my-agent', region: 'eu' });

		expect(fetchSpy).toHaveBeenCalledWith(
			'http://localhost:8080/api/instances',
			expect.objectContaining({
				method: 'POST',
				headers: {
					Authorization: 'Bearer my-token',
					'Content-Type': 'application/json'
				},
				body: JSON.stringify({ name: 'my-agent', region: 'eu' })
			})
		);
	});

	it('deleteInstance sends DELETE request', async () => {
		fetchSpy.mockResolvedValue({ ok: true });

		const { deleteInstance } = await import('./client');
		await deleteInstance('my-token', 'inst-123');

		expect(fetchSpy).toHaveBeenCalledWith(
			'http://localhost:8080/api/instances/inst-123',
			expect.objectContaining({
				method: 'DELETE',
				headers: { Authorization: 'Bearer my-token' }
			})
		);
	});

	it('throws error for non-ok response with error body', async () => {
		fetchSpy.mockResolvedValue({
			ok: false,
			status: 403,
			json: () => Promise.resolve({ error: 'Forbidden' })
		});

		const { getDashboardState } = await import('./client');
		await expect(getDashboardState('bad-token')).rejects.toThrow('Forbidden');
	});

	it('throws error for non-ok response without error body', async () => {
		fetchSpy.mockResolvedValue({
			ok: false,
			status: 500,
			json: () => Promise.reject(new Error('not json'))
		});

		const { getDashboardState } = await import('./client');
		await expect(getDashboardState('token')).rejects.toThrow('getDashboardState failed: 500');
	});

	it('restartInstance sends POST to correct endpoint', async () => {
		fetchSpy.mockResolvedValue({ ok: true });

		const { restartInstance } = await import('./client');
		await restartInstance('my-token', 'inst-456');

		expect(fetchSpy).toHaveBeenCalledWith(
			'http://localhost:8080/api/instances/inst-456/restart',
			expect.objectContaining({
				method: 'POST',
				headers: { Authorization: 'Bearer my-token' }
			})
		);
	});

	it('getModels calls correct endpoint', async () => {
		const mockModels = { models: [], default_model_id: 'test' };
		fetchSpy.mockResolvedValue({
			ok: true,
			json: () => Promise.resolve(mockModels)
		});

		const { getModels } = await import('./client');
		const result = await getModels();

		expect(fetchSpy).toHaveBeenCalledWith(
			'http://localhost:8080/api/models',
			expect.any(Object)
		);
		expect(result).toEqual(mockModels);
	});
});
