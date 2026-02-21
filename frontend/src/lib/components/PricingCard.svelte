<script lang="ts">
	import type { PlanInfo } from '$lib/types';

	interface Props {
		plan: PlanInfo;
	}

	let { plan }: Props = $props();
</script>

<div class="relative rounded-2xl border border-gray-900 shadow-lg p-8 flex flex-col">
	<div>
		<h3 class="text-lg font-semibold text-gray-900">{plan.name}</h3>
		<p class="mt-1 text-sm text-gray-500">Everything you need to run your AI agent</p>
	</div>
	<div class="mt-4">
		<span class="text-4xl font-bold text-gray-900">${plan.price_monthly}</span>
		<span class="text-sm text-gray-500">/month</span>
	</div>
	<ul class="mt-6 space-y-3 flex-1">
		{#each plan.features as feature}
			<li class="flex items-start gap-2 text-sm text-gray-600">
				<svg class="h-5 w-5 shrink-0 text-green-500 mt-0.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
				</svg>
				{feature}
			</li>
		{/each}
	</ul>
	<a
		href="/signup"
		class="mt-8 block rounded-lg bg-gray-900 px-4 py-2.5 text-center text-sm font-medium text-white hover:bg-gray-800"
	>
		Get Started
	</a>
</div>
