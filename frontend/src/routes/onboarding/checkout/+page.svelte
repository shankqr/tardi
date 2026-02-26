<script lang="ts">
	import { onMount } from 'svelte';
	import { onboardingState } from '$lib/stores/onboarding';
	import { user } from '$lib/stores/auth';
	import { stripePricingTableId, stripePublishableKey } from '$lib/stores/config';
	import { plan as planInfo } from '$lib/api/mock';

	const onboarding = $derived.by(() => $onboardingState);
	const selectedPlan = $derived.by(() => (onboarding.selectedPlan ? planInfo : null));
	const currentUser = $derived($user);

	const clientReferenceId = $derived(currentUser?.uid ?? '');
	const pricingTableId = $derived($stripePricingTableId);
	const publishableKey = $derived($stripePublishableKey);

	onMount(() => {
		if (!document.querySelector('script[src*="pricing-table.js"]')) {
			const script = document.createElement('script');
			script.src = 'https://js.stripe.com/v3/pricing-table.js';
			script.async = true;
			document.head.appendChild(script);
		}
	});
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

			<!-- Stripe Pricing Table -->
			{#if pricingTableId && publishableKey && clientReferenceId}
				<div class="mt-2">
					<stripe-pricing-table
						pricing-table-id={pricingTableId}
						publishable-key={publishableKey}
						client-reference-id={clientReferenceId}
					></stripe-pricing-table>
				</div>
			{:else}
				<!-- Dev fallback when Stripe config is missing -->
				<div class="mt-6 rounded-xl border border-gray-200 p-6 text-center">
					<p class="text-sm text-gray-500">Stripe Pricing Table not configured.</p>
					<p class="mt-1 text-xs text-gray-400">
						Set STRIPE_PRICING_TABLE_ID and STRIPE_PUBLISHABLE_KEY in environment.
					</p>
					<a
						href="/onboarding/success"
						class="mt-4 inline-block rounded-lg bg-gray-900 px-6 py-2.5 text-sm font-medium text-white hover:bg-gray-800"
					>
						Simulate Checkout (Dev)
					</a>
				</div>
			{/if}

			<div class="flex justify-start pt-2">
				<a
					href="/onboarding/plan"
					class="rounded-lg border border-gray-300 px-6 py-2.5 text-sm font-medium text-gray-700 hover:bg-gray-50"
				>
					&larr; Back
				</a>
			</div>

			<p class="text-center text-xs text-gray-400">
				Powered by Stripe. Your payment details are handled securely.
			</p>
		</div>
	{/if}
</div>
