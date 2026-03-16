import type { DashboardState, PlanInfo, Snapshot } from '$lib/types';

export const mockSnapshots: Snapshot[] = [
	{
		id: 'snap-001',
		instance_id: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
		name: 'before-config-update',
		status: 'ready',
		created_at: '2026-02-10T14:30:00Z',
		size_gb: 2.4
	},
	{
		id: 'snap-002',
		instance_id: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
		name: 'stable-v1',
		status: 'ready',
		created_at: '2026-02-18T09:15:00Z',
		size_gb: 2.6
	}
];

export const mockDashboardState: DashboardState = {
	instances: [
		{
			id: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
			name: 'my-trading-agent',
			status: 'active',
			provider: 'hetzner',
			ipv4: '203.0.113.10',
			region: 'eu-central',
			agent_status: 'running',
			last_heartbeat_at: new Date(Date.now() - 15000).toISOString(),
			dashboard_url: 'https://203.0.113.10',
			openclaw_auth_token: 'a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2',
			created_at: '2026-01-15T10:30:00Z'
		}
	],
	subscription: {
		plan: 'standard',
		status: 'active',
		current_period_end: '2026-03-21T00:00:00Z',
		cancel_at_period_end: false
	},
	pending_jobs: 0,
	snapshots: mockSnapshots
};

export const plans: Record<string, PlanInfo> = {
	standard: {
		tier: 'standard',
		name: 'Standard',
		price_monthly: 29,
		features: [
			'Your own AI agent, always online',
			'Shared cloud infrastructure',
			'Deploy in under 2 minutes',
			'Full dashboard & live monitoring',
			'One-click restarts & updates',
			'Priority support'
		]
	},
	pro: {
		tier: 'pro',
		name: 'Pro',
		price_monthly: 45,
		features: [
			'Everything in Standard',
			'Dedicated CPU for faster performance',
			'Your own AI agent, always online',
			'Deploy in under 2 minutes',
			'Full dashboard & live monitoring',
			'One-click restarts & updates',
			'Priority support'
		]
	}
};

export const plan: PlanInfo = plans.standard;
