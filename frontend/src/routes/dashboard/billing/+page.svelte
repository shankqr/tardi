<script lang="ts">
	import { dashboardState } from '$lib/stores/dashboard';
	import { plan } from '$lib/api/mock';

	const subscription = $derived($dashboardState?.subscription ?? null);

	let showCancelConfirm = $state(false);
	let cancelling = $state(false);

	async function handleCancel() {
		cancelling = true;
		// TODO: Call backend to cancel subscription via Stripe
		await new Promise((r) => setTimeout(r, 1000));
		cancelling = false;
		showCancelConfirm = false;
		alert('Subscription cancellation will be connected when the backend is ready.');
	}
</script>

<a href="/dashboard" class="text-sm text-gray-500 hover:text-gray-700">&larr; Back to dashboard</a>

<h2 class="mt-4 text-xl font-bold text-gray-900">Billing</h2>

{#if subscription}
	<div class="mt-6 rounded-xl border border-gray-200 p-6">
		<div class="flex items-center justify-between">
			<div>
				<p class="text-sm text-gray-500">Current Plan</p>
				<p class="mt-1 text-2xl font-bold text-gray-900">{plan.name} — ${plan.price_monthly}/mo</p>
			</div>
			<span
				class="rounded-full px-3 py-1 text-sm font-medium {subscription.status === 'active'
					? 'bg-green-100 text-green-700'
					: subscription.status === 'canceled'
						? 'bg-gray-100 text-gray-600'
						: 'bg-orange-100 text-orange-700'}"
			>
				{subscription.status === 'canceled' ? 'Canceling' : subscription.status}
			</span>
		</div>
		<p class="mt-4 text-sm text-gray-500">
			{subscription.status === 'canceled'
				? `Access until ${new Date(subscription.current_period_end).toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' })}`
				: `Renews ${new Date(subscription.current_period_end).toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' })}`}
		</p>
	</div>

	<!-- Cancel subscription -->
	{#if subscription.status === 'active'}
		<div class="mt-6 rounded-xl border border-gray-200 p-6">
			<h3 class="text-sm font-semibold text-gray-900">Cancel Subscription</h3>
			<p class="mt-2 text-sm text-gray-500">
				Your agent will remain active until the end of your current billing period. You won't be charged again.
			</p>

			{#if !showCancelConfirm}
				<button
					onclick={() => (showCancelConfirm = true)}
					class="mt-4 rounded-lg border border-red-200 px-4 py-2 text-sm text-red-600 hover:bg-red-50"
				>
					Cancel renewal
				</button>
			{:else}
				<div class="mt-4 rounded-lg bg-red-50 p-4">
					<p class="text-sm text-red-700">
						Are you sure? Your agent will stop running after {new Date(subscription.current_period_end).toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' })}.
					</p>
					<div class="mt-3 flex gap-3">
						<button
							onclick={handleCancel}
							disabled={cancelling}
							class="rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700 disabled:opacity-50"
						>
							{cancelling ? 'Cancelling...' : 'Yes, cancel'}
						</button>
						<button
							onclick={() => (showCancelConfirm = false)}
							class="rounded-lg border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50"
						>
							Keep subscription
						</button>
					</div>
				</div>
			{/if}
		</div>
	{/if}
{/if}

<div class="mt-10">
	<h3 class="text-lg font-semibold text-gray-900">Plan Details</h3>
	<div class="mt-4 rounded-xl border border-gray-200 p-6">
		<ul class="space-y-3">
			{#each plan.features as feature}
				<li class="flex items-start gap-2 text-sm text-gray-600">
					<svg class="h-5 w-5 shrink-0 text-green-500 mt-0.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
					</svg>
					{feature}
				</li>
			{/each}
		</ul>
	</div>
</div>
