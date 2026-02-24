<script lang="ts">
	import { onboardingState } from '$lib/stores/onboarding';
	import { plan as planInfo } from '$lib/api/mock';

	const onboarding = $derived.by(() => $onboardingState);
	const selectedPlan = $derived.by(() => (onboarding.selectedPlan ? planInfo : null));

	let processing = $state(false);

	async function handleCheckout() {
		processing = true;
		// TODO: Call backend to create Stripe Checkout Session and redirect
		// For now, simulate a delay
		await new Promise((r) => setTimeout(r, 1500));
		processing = false;
		alert('Stripe Checkout integration will be connected when the backend is ready.');
	}
</script>

<div class="mx-auto max-w-2xl py-12 px-4">
	<!-- Progress -->
	<div class="flex items-center justify-center gap-2 text-sm text-gray-400">
		<span>1. Configure</span>
		<span>&rarr;</span>
		<span>2. Review Plan</span>
		<span>&rarr;</span>
		<span class="font-medium text-gray-900">3. Checkout</span>
	</div>

	<h1 class="mt-8 text-2xl font-bold text-gray-900 text-center">Review & Checkout</h1>

	{#if !onboarding.agentConfig || !selectedPlan}
		<div class="mt-8 rounded-lg bg-yellow-50 p-4 text-sm text-yellow-700 text-center">
			<p>Missing configuration. Please start from the beginning.</p>
			<a href="/onboarding/configure" class="mt-2 inline-block font-medium underline">Start over</a>
		</div>
	{:else}
		<div class="mt-8 space-y-6">
			<!-- Agent summary -->
			<div class="rounded-xl border border-gray-200 p-5">
				<div class="flex items-center justify-between">
					<h3 class="text-sm font-semibold text-gray-900">Agent Configuration</h3>
					<a href="/onboarding/configure" class="text-xs text-gray-500 hover:text-gray-700">Edit</a>
				</div>
				<dl class="mt-3 space-y-2 text-sm">
					<div class="flex justify-between">
						<dt class="text-gray-500">Name</dt>
						<dd class="text-gray-900">{onboarding.agentConfig.name}</dd>
					</div>
					{#if onboarding.agentConfig.description}
						<div class="flex justify-between">
							<dt class="text-gray-500">Description</dt>
							<dd class="text-gray-900 text-right max-w-xs">{onboarding.agentConfig.description}</dd>
						</div>
					{/if}
				</dl>
			</div>

			<!-- Plan summary -->
			<div class="rounded-xl border border-gray-200 p-5">
				<div class="flex items-center justify-between">
					<h3 class="text-sm font-semibold text-gray-900">Plan</h3>
					<a href="/onboarding/plan" class="text-xs text-gray-500 hover:text-gray-700">Change</a>
				</div>
				<div class="mt-3 flex items-baseline justify-between">
					<p class="text-lg font-semibold text-gray-900">{selectedPlan.name}</p>
					<div class="text-right">
						<p class="text-2xl font-bold text-gray-900">${selectedPlan.price_monthly}</p>
						<p class="text-xs text-gray-500">/month</p>
					</div>
				</div>
			</div>

			<!-- Checkout button -->
			<div class="flex justify-between pt-2">
				<a
					href="/onboarding/plan"
					class="rounded-lg border border-gray-300 px-6 py-2.5 text-sm font-medium text-gray-700 hover:bg-gray-50"
				>
					&larr; Back
				</a>
				<button
					onclick={handleCheckout}
					disabled={processing}
					class="rounded-lg bg-gray-900 px-8 py-2.5 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
				>
					{processing ? 'Processing...' : 'Proceed to Payment'}
				</button>
			</div>

			<p class="text-center text-xs text-gray-400">
				You'll be redirected to Stripe for secure payment processing.
			</p>
		</div>
	{/if}
</div>
