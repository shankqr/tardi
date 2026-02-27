<script lang="ts">
	import type { Subscription } from '$lib/types';

	interface Props {
		subscription: Subscription | null;
	}

	let { subscription }: Props = $props();

	const planLabels: Record<string, string> = {
		standard: 'Standard'
	};

	function formatDate(dateStr: string): string {
		return new Date(dateStr).toLocaleDateString('en-US', {
			month: 'short',
			day: 'numeric',
			year: 'numeric'
		});
	}
</script>

{#if subscription}
	<div class="rounded-xl border border-gray-200 p-5">
		<div class="flex items-center justify-between">
			<div>
				<p class="text-xs text-gray-400">Current Plan</p>
				<p class="mt-0.5 text-lg font-semibold text-gray-900">{planLabels[subscription.plan] ?? subscription.plan}</p>
			</div>
			<span
				class="rounded-full px-2.5 py-0.5 text-xs font-medium {subscription.status === 'active'
					? 'bg-green-100 text-green-700'
					: subscription.status === 'past_due'
						? 'bg-orange-100 text-orange-700'
						: 'bg-gray-100 text-gray-700'}"
			>
				{subscription.status === 'active' ? 'Active' : subscription.status === 'past_due' ? 'Past Due' : subscription.status}
			</span>
		</div>
		<p class="mt-3 text-xs text-gray-400">
			Renews {formatDate(subscription.current_period_end)}
		</p>
		<a
			href="/dashboard/billing"
			class="mt-3 inline-block text-sm text-gray-600 hover:text-gray-900"
		>
			Manage billing &rarr;
		</a>
	</div>
{:else}
	<div class="rounded-xl border border-dashed border-gray-300 p-5 text-center">
		<p class="text-sm text-gray-500">No active subscription</p>
		<a href="/dashboard/billing" class="mt-2 inline-block text-sm font-medium text-gray-900 hover:underline">
			Subscribe &rarr;
		</a>
	</div>
{/if}
