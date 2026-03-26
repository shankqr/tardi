import { describe, it, expect } from 'vitest';
import { get } from 'svelte/store';
import { onboardingState, setAgentConfig, setSelectedPlan, resetOnboarding } from './onboarding';
import type { AgentConfiguration } from './onboarding';

describe('onboarding store', () => {
	it('has null initial state', () => {
		const state = get(onboardingState);
		expect(state.agentConfig).toBeNull();
		expect(state.selectedPlan).toBeNull();
	});

	it('setAgentConfig updates agentConfig', () => {
		const config: AgentConfiguration = {
			name: 'Test Agent',
			model: 'gpt-4',
			description: 'A test agent',
			openrouter_api_key: 'sk-test-123'
		};

		setAgentConfig(config);

		const state = get(onboardingState);
		expect(state.agentConfig).toEqual(config);
		expect(state.agentConfig?.name).toBe('Test Agent');
		expect(state.agentConfig?.model).toBe('gpt-4');
	});

	it('setSelectedPlan updates selectedPlan', () => {
		setSelectedPlan('standard');
		expect(get(onboardingState).selectedPlan).toBe('standard');

		setSelectedPlan('pro');
		expect(get(onboardingState).selectedPlan).toBe('pro');
	});

	it('setAgentConfig does not affect selectedPlan', () => {
		resetOnboarding();
		setSelectedPlan('standard');

		const config: AgentConfiguration = {
			name: 'Agent',
			model: 'claude',
			description: 'desc',
			openrouter_api_key: 'key'
		};
		setAgentConfig(config);

		const state = get(onboardingState);
		expect(state.selectedPlan).toBe('standard');
		expect(state.agentConfig?.name).toBe('Agent');
	});

	it('resetOnboarding clears all state', () => {
		setAgentConfig({
			name: 'Agent',
			model: 'model',
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
