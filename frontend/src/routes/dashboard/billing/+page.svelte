<script lang="ts">
	import { dashboardState } from '$lib/stores/dashboard';
	import { getIdToken } from '$lib/stores/auth';
	import { createPortalSession } from '$lib/api/client';
	import { plans } from '$lib/api/mock';

	const subscription = $derived($dashboardState?.subscription ?? null);
	const currentPlan = $derived(subscription ? plans[subscription.plan] ?? plans.standard : plans.standard);
	const isStandard = $derived(subscription?.plan === 'standard');
	const isPro = $derived(subscription?.plan === 'pro');

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

	async function handleChangePlan() {
		loadingPortal = true;
		try {
			const token = await getIdToken();
			if (!token) return;
			const { url } = await createPortalSession(token, 'subscription_update');
			window.location.href = url;
		} catch {
			alert('Failed to open billing portal. Please try again.');
			loadingPortal = false;
		}
	}
</script>

<a href="/dashboard" class="text-sm text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300">&larr; Back to dashboard</a>

<h2 class="mt-4 text-xl font-bold text-gray-900 dark:text-white">Billing</h2>

{#if subscription}
	<div class="mt-6 rounded-xl border border-gray-200 dark:border-gray-700 p-6">
		<div class="flex items-center justify-between">
			<div>
				<p class="text-sm text-gray-500 dark:text-gray-400">Current Plan</p>
				<p class="mt-1 text-2xl font-bold text-gray-900 dark:text-white">{currentPlan.name} — ${currentPlan.price_monthly}/mo</p>
			</div>
			<span
				class="rounded-full px-3 py-1 text-sm font-medium {subscription.cancel_at_period_end
					? 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
					: subscription.status === 'active'
						? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
						: subscription.status === 'canceled'
							? 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400'
							: 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'}"
			>
				{subscription.cancel_at_period_end ? 'Canceling' : subscription.status === 'canceled' ? 'Canceled' : subscription.status}
			</span>
		</div>
		<p class="mt-4 text-sm text-gray-500 dark:text-gray-400">
			{subscription.cancel_at_period_end || subscription.status === 'canceled'
				? `Access until ${new Date(subscription.current_period_end).toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' })}`
				: `Renews ${new Date(subscription.current_period_end).toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' })}`}
		</p>
	</div>

	{#if subscription.cancel_at_period_end}
		<div class="mt-6 rounded-lg border border-orange-200 dark:border-orange-800 bg-orange-50 dark:bg-orange-900/20 p-4">
			<p class="text-sm font-medium text-orange-800 dark:text-orange-400">
				Your subscription will cancel on {new Date(subscription.current_period_end).toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' })}.
				Your agent and all snapshots will be permanently deleted.
			</p>
			<p class="mt-2 text-sm text-orange-700 dark:text-orange-400">
				You can undo this through the billing portal below.
			</p>
		</div>
	{/if}

	<!-- Plan change CTA -->
	{#if subscription.status === 'active' && !subscription.cancel_at_period_end}
		{#if isStandard}
			<div class="mt-6 rounded-xl border border-blue-200 dark:border-blue-800 bg-blue-50 dark:bg-blue-900/20 p-6">
				<div class="flex items-center justify-between">
					<div>
						<h3 class="text-sm font-semibold text-blue-900 dark:text-blue-300">Upgrade to Pro — ${plans.pro.price_monthly}/mo</h3>
						<p class="mt-1 text-sm text-blue-700 dark:text-blue-400">
							Dedicated CPU for faster performance. Your agent data will be preserved during the upgrade.
						</p>
					</div>
					<button
						onclick={handleChangePlan}
						disabled={loadingPortal}
						class="shrink-0 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
					>
						{loadingPortal ? 'Opening...' : 'Upgrade'}
					</button>
				</div>
			</div>
		{:else if isPro}
			<div class="mt-6 rounded-xl border border-gray-200 dark:border-gray-700 p-6">
				<div class="flex items-center justify-between">
					<div>
						<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Downgrade to Standard — ${plans.standard.price_monthly}/mo</h3>
						<p class="mt-1 text-sm text-red-600 dark:text-red-400">
							Warning: Downgrading will delete all agent data. A new server will be provisioned from scratch.
						</p>
					</div>
					<button
						onclick={handleChangePlan}
						disabled={loadingPortal}
						class="shrink-0 rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700 disabled:opacity-50"
					>
						{loadingPortal ? 'Opening...' : 'Downgrade'}
					</button>
				</div>
			</div>
		{/if}
	{/if}

	<!-- Manage subscription via Stripe Customer Portal -->
	<div class="mt-6 rounded-xl border border-gray-200 dark:border-gray-700 p-6">
		<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Manage Subscription</h3>
		<p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
			Update payment method, view invoices, or cancel your subscription through the billing portal.
		</p>
		<button
			onclick={handleManageBilling}
			disabled={loadingPortal}
			class="mt-4 rounded-lg bg-gray-900 dark:bg-white px-4 py-2 text-sm font-medium text-white dark:text-gray-900 hover:bg-gray-800 dark:hover:bg-gray-100 disabled:opacity-50"
		>
			{loadingPortal ? 'Opening...' : 'Manage Billing'}
		</button>
	</div>
{/if}

<div class="mt-10">
	<h3 class="text-lg font-semibold text-gray-900 dark:text-white">Plan Details</h3>
	<div class="mt-4 rounded-xl border border-gray-200 dark:border-gray-700 p-6">
		<ul class="space-y-3">
			{#each currentPlan.features as feature}
				<li class="flex items-start gap-2 text-sm text-gray-600 dark:text-gray-300">
					<svg class="h-5 w-5 shrink-0 text-green-500 mt-0.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
					</svg>
					{feature}
				</li>
			{/each}
		</ul>
	</div>
</div>
