<script lang="ts">
	import { dashboardState } from '$lib/stores/dashboard';
	import { getIdToken } from '$lib/stores/auth';
	import { createPortalSession } from '$lib/api/client';
	import { plan } from '$lib/api/mock';

	const subscription = $derived($dashboardState?.subscription ?? null);

	let loadingPortal = $state(false);

	async function handleManageBilling() {
		loadingPortal = true;
		try {
			const token = await getIdToken();
			if (!token) return;
			const { url } = await createPortalSession(token);
			window.location.href = url;
		} catch {
			alert('Failed to open billing portal. Please try again.');
			loadingPortal = false;
		}
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

	<!-- Manage subscription via Stripe Customer Portal -->
	<div class="mt-6 rounded-xl border border-gray-200 p-6">
		<h3 class="text-sm font-semibold text-gray-900">Manage Subscription</h3>
		<p class="mt-2 text-sm text-gray-500">
			Update payment method, view invoices, or cancel your subscription through the billing portal.
		</p>
		<button
			onclick={handleManageBilling}
			disabled={loadingPortal}
			class="mt-4 rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
		>
			{loadingPortal ? 'Opening...' : 'Manage Billing'}
		</button>
	</div>
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
