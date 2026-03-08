import { writable } from 'svelte/store';
import type { PlanTier } from '$lib/types';

export interface AgentConfiguration {
	name: string;
	model: string;
	description: string;
}

export interface OnboardingState {
	agentConfig: AgentConfiguration | null;
	selectedPlan: PlanTier | null;
}

export const onboardingState = writable<OnboardingState>({
	agentConfig: null,
	selectedPlan: null
});

export function setAgentConfig(config: AgentConfiguration) {
	onboardingState.update((s) => ({ ...s, agentConfig: config }));
}

export function setSelectedPlan(plan: PlanTier) {
	onboardingState.update((s) => ({ ...s, selectedPlan: plan }));
}

export function resetOnboarding() {
	onboardingState.set({ agentConfig: null, selectedPlan: null });
}
