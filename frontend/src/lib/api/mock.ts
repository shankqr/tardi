import type { DashboardState, PlanInfo, Snapshot } from '$lib/types';

export const mockDashboardState: DashboardState = {
	instances: [
		{
			id: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
			name: 'my-trading-agent',
			status: 'active',
			provider: 'hetzner',
			ipv4: '203.0.113.10',
			region: 'eu-central',
			last_heartbeat_at: new Date(Date.now() - 15000).toISOString(),
			created_at: '2026-01-15T10:30:00Z'
		}
	],
	subscription: {
		plan: 'standard',
		status: 'active',
		current_period_end: '2026-03-21T00:00:00Z'
	},
	pending_jobs: 0
};

export const mockSnapshots: Snapshot[] = [
	{
		id: 'snap-001',
		instance_id: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
		name: 'before-config-update',
		created_at: '2026-02-10T14:30:00Z',
		size_gb: 2.4
	},
	{
		id: 'snap-002',
		instance_id: 'a1b2c3d4-e5f6-7890-abcd-ef1234567890',
		name: 'stable-v1',
		created_at: '2026-02-18T09:15:00Z',
		size_gb: 2.6
	}
];

export const plan: PlanInfo = {
	tier: 'standard',
	name: 'Standard',
	price_monthly: 29,
	features: [
		'Your own AI agent, always online',
		'Dedicated cloud infrastructure',
		'Deploy in under 2 minutes',
		'Full dashboard & live monitoring',
		'One-click restarts & updates',
		'Priority support'
	]
};
