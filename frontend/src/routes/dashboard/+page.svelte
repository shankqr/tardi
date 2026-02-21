<script lang="ts">
	import { dashboardState, dashboardLoading, dashboardError } from '$lib/stores/dashboard';
	import InstanceCard from '$lib/components/InstanceCard.svelte';
	import SubscriptionCard from '$lib/components/SubscriptionCard.svelte';
</script>

{#if $dashboardLoading}
	<div class="flex items-center justify-center py-20">
		<p class="text-gray-400">Loading dashboard...</p>
	</div>
{:else if $dashboardError}
	<div class="rounded-lg bg-red-50 p-4 text-sm text-red-700">{$dashboardError}</div>
{:else if $dashboardState}
	<div class="grid grid-cols-1 lg:grid-cols-3 gap-8">
		<!-- Main content -->
		<div class="lg:col-span-2 space-y-6">
			<h2 class="text-lg font-semibold text-gray-900">Your Agent</h2>

			{#if $dashboardState.instances.length === 0}
				<div class="rounded-xl border border-dashed border-gray-300 p-10 text-center">
					<p class="text-gray-500">No agent deployed yet</p>
					<a
						href="/onboarding/configure"
						class="mt-2 inline-block text-sm font-medium text-gray-900 hover:underline"
					>
						Deploy your agent
					</a>
				</div>
			{:else}
				<InstanceCard instance={$dashboardState.instances[0]} />
			{/if}
		</div>

		<!-- Sidebar -->
		<div class="space-y-6">
			<SubscriptionCard subscription={$dashboardState.subscription} />
		</div>
	</div>
{/if}
