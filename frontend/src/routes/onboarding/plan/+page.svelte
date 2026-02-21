<script lang="ts">
	import { goto } from '$app/navigation';
	import { setSelectedPlan } from '$lib/stores/onboarding';
	import { plan } from '$lib/api/mock';

	function handleContinue() {
		setSelectedPlan('standard');
		goto('/onboarding/checkout');
	}
</script>

<div class="mx-auto max-w-2xl py-12 px-4">
	<!-- Progress -->
	<div class="flex items-center justify-center gap-2 text-sm text-gray-400">
		<span>1. Configure</span>
		<span>&rarr;</span>
		<span class="font-medium text-gray-900">2. Review Plan</span>
		<span>&rarr;</span>
		<span>3. Checkout</span>
	</div>

	<h1 class="mt-8 text-2xl font-bold text-gray-900 text-center">Your plan</h1>
	<p class="mt-2 text-center text-sm text-gray-500">
		One simple plan. Dedicated infrastructure for your AI agent.
	</p>

	<div class="mt-10 max-w-md mx-auto rounded-2xl border-2 border-gray-900 shadow-lg p-6">
		<h3 class="text-lg font-semibold text-gray-900">{plan.name}</h3>
		<div class="mt-3">
			<span class="text-3xl font-bold text-gray-900">${plan.price_monthly}</span>
			<span class="text-sm text-gray-500">/mo</span>
		</div>
		<ul class="mt-4 space-y-2">
			{#each plan.features as feature}
				<li class="flex items-start gap-2 text-sm text-gray-600">
					<svg class="h-4 w-4 shrink-0 text-green-500 mt-0.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
					</svg>
					{feature}
				</li>
			{/each}
		</ul>
	</div>

	<div class="mt-8 flex justify-between">
		<a
			href="/onboarding/configure"
			class="rounded-lg border border-gray-300 px-6 py-2.5 text-sm font-medium text-gray-700 hover:bg-gray-50"
		>
			&larr; Back
		</a>
		<button
			onclick={handleContinue}
			class="rounded-lg bg-gray-900 px-6 py-2.5 text-sm font-medium text-white hover:bg-gray-800"
		>
			Continue to Checkout &rarr;
		</button>
	</div>
</div>
