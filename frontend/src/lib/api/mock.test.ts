import { describe, it, expect } from 'vitest';
import { mockDashboardState, mockSnapshots, plans, plan } from './mock';

describe('mock data', () => {
	describe('mockDashboardState', () => {
		it('has expected structure', () => {
			expect(mockDashboardState).toHaveProperty('instances');
			expect(mockDashboardState).toHaveProperty('subscription');
			expect(mockDashboardState).toHaveProperty('pending_jobs');
			expect(mockDashboardState).toHaveProperty('snapshots');
		});

		it('has one instance', () => {
			expect(mockDashboardState.instances).toHaveLength(1);
		});

		it('instance has correct fields', () => {
			const instance = mockDashboardState.instances[0];
			expect(instance.id).toBe('a1b2c3d4-e5f6-7890-abcd-ef1234567890');
			expect(instance.name).toBe('my-trading-agent');
			expect(instance.status).toBe('active');
			expect(instance.provider).toBe('hetzner');
			expect(instance.ipv4).toBe('203.0.113.10');
			expect(instance.agent_status).toBe('running');
			expect(instance.openclaw_version).toBe('2026.5.2');
			expect(instance.target_openclaw_version).toBe('latest');
			expect(instance.dashboard_url).toBeTruthy();
		});

		it('subscription is active', () => {
			expect(mockDashboardState.subscription).not.toBeNull();
			expect(mockDashboardState.subscription?.status).toBe('active');
			expect(mockDashboardState.subscription?.plan).toBe('standard');
			expect(mockDashboardState.subscription?.cancel_at_period_end).toBe(false);
		});

		it('pending_jobs is zero', () => {
			expect(mockDashboardState.pending_jobs).toBe(0);
		});

		it('snapshots match mockSnapshots', () => {
			expect(mockDashboardState.snapshots).toBe(mockSnapshots);
		});
	});

	describe('mockSnapshots', () => {
		it('has two snapshots', () => {
			expect(mockSnapshots).toHaveLength(2);
		});

		it('snapshots have ready status', () => {
			for (const snap of mockSnapshots) {
				expect(snap.status).toBe('ready');
			}
		});

		it('snapshots have required fields', () => {
			for (const snap of mockSnapshots) {
				expect(snap.id).toBeTruthy();
				expect(snap.instance_id).toBeTruthy();
				expect(snap.name).toBeTruthy();
				expect(snap.created_at).toBeTruthy();
			}
		});
	});

	describe('plans', () => {
		it('has standard and pro plans', () => {
			expect(plans).toHaveProperty('standard');
			expect(plans).toHaveProperty('pro');
		});

		it('standard plan costs $29/mo', () => {
			expect(plans.standard.price_monthly).toBe(29);
			expect(plans.standard.tier).toBe('standard');
			expect(plans.standard.name).toBe('Standard');
		});

		it('pro plan costs $65/mo', () => {
			expect(plans.pro.price_monthly).toBe(65);
			expect(plans.pro.tier).toBe('pro');
			expect(plans.pro.name).toBe('Pro');
		});

		it('both plans have features arrays', () => {
			expect(plans.standard.features.length).toBeGreaterThan(0);
			expect(plans.pro.features.length).toBeGreaterThan(0);
		});

		it('pro plan includes "Everything in Standard"', () => {
			expect(plans.pro.features).toContain('Everything in Standard');
		});
	});

	describe('plan (default export)', () => {
		it('is the standard plan', () => {
			expect(plan).toBe(plans.standard);
		});
	});
});
