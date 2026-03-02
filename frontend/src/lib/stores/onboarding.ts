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

// localStorage persistence — survives Stripe redirect
const STORAGE_KEY = 'tardi_onboarding_config';

export function persistConfig(config: AgentConfiguration) {
	localStorage.setItem(STORAGE_KEY, JSON.stringify(config));
}

export function loadPersistedConfig(): AgentConfiguration | null {
	const raw = localStorage.getItem(STORAGE_KEY);
	if (!raw) return null;
	try {
		return JSON.parse(raw);
	} catch {
		return null;
	}
}

export function clearPersistedConfig() {
	localStorage.removeItem(STORAGE_KEY);
}
