import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import { onboardingState, setAgentConfig, setSelectedPlan, resetOnboarding } from './onboarding';
import type { AgentConfiguration } from './onboarding';
import type { PlanTier } from '$lib/types';

describe('onboarding store', () => {
	beforeEach(() => {
		resetOnboarding();
	});

	it('has null agentConfig and null selectedPlan by default', () => {
		const state = get(onboardingState);
		expect(state.agentConfig).toBeNull();
		expect(state.selectedPlan).toBeNull();
	});

	it('setAgentConfig updates the store', () => {
		const config: AgentConfiguration = {
			name: 'Test Agent',
			model: 'claude-3',
			description: 'A test agent',
			openrouter_api_key: 'test-key'
		};
		setAgentConfig(config);
		const state = get(onboardingState);
		expect(state.agentConfig).toEqual(config);
	});

	it('setSelectedPlan updates the store', () => {
		const plan: PlanTier = 'standard';
		setSelectedPlan(plan);
		const state = get(onboardingState);
		expect(state.selectedPlan).toBe('standard');
	});

	it('resetOnboarding resets to initial state', () => {
		setAgentConfig({
			name: 'Test',
			model: 'claude-3',
			description: 'desc',
			openrouter_api_key: 'key'
		});
		setSelectedPlan('pro');
		resetOnboarding();
		const state = get(onboardingState);
		expect(state.agentConfig).toBeNull();
		expect(state.selectedPlan).toBeNull();
	});
});
