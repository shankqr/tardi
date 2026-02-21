<script lang="ts">
	import type { ProvisioningStep } from '$lib/types';

	interface Props {
		currentStep?: ProvisioningStep;
	}

	let { currentStep }: Props = $props();

	const steps: { key: ProvisioningStep; label: string }[] = [
		{ key: 'select_provider', label: 'Selecting provider' },
		{ key: 'create_server', label: 'Creating server' },
		{ key: 'wait_server_ready', label: 'Waiting for server' },
		{ key: 'bootstrap', label: 'Bootstrapping OS' },
		{ key: 'install_agent', label: 'Installing agent' },
		{ key: 'activate', label: 'Activating' }
	];

	function getStepState(stepKey: ProvisioningStep): 'completed' | 'current' | 'pending' {
		if (!currentStep) return 'pending';
		const currentIndex = steps.findIndex((s) => s.key === currentStep);
		const stepIndex = steps.findIndex((s) => s.key === stepKey);
		if (stepIndex < currentIndex) return 'completed';
		if (stepIndex === currentIndex) return 'current';
		return 'pending';
	}
</script>

<div class="space-y-3">
	{#each steps as step}
		{@const state = getStepState(step.key)}
		<div class="flex items-center gap-3">
			{#if state === 'completed'}
				<div class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-green-500">
					<svg class="h-3.5 w-3.5 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
						<path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
					</svg>
				</div>
				<span class="text-sm text-gray-500">{step.label}</span>
			{:else if state === 'current'}
				<div class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full border-2 border-yellow-500 bg-yellow-50">
					<svg class="h-3.5 w-3.5 animate-spin text-yellow-600" viewBox="0 0 24 24" fill="none">
						<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
						<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
					</svg>
				</div>
				<span class="text-sm font-medium text-gray-900">{step.label}</span>
			{:else}
				<div class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full border-2 border-gray-200">
					<div class="h-2 w-2 rounded-full bg-gray-200"></div>
				</div>
				<span class="text-sm text-gray-400">{step.label}</span>
			{/if}
		</div>
	{/each}
</div>
