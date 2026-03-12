<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { user, authLoading, emailVerified } from '$lib/stores/auth';
	import { stripePricingTableId, stripePublishableKey } from '$lib/stores/config';

	const currentUser = $derived($user);

	// Redirect unverified users to verify-email
	$effect(() => {
		if (!$authLoading && currentUser && !$emailVerified) {
			goto('/verify-email');
		}
	});

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
		<span>1. Sign Up</span>
		<span>&rarr;</span>
		<span class="font-medium text-gray-900">2. Checkout</span>
	</div>

	<h1 class="mt-8 text-2xl font-bold text-gray-900 text-center">Choose your plan</h1>
	<p class="mt-2 text-center text-sm text-gray-500">
		Subscribe to get your dedicated AI agent infrastructure.
	</p>

	<div class="mt-8 space-y-6">
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

		<p class="text-center text-xs text-gray-400">
			Powered by Stripe. Your payment details are handled securely.
		</p>
	</div>
</div>
