<script lang="ts">
	import { goto } from '$app/navigation';
	import { dashboardState, dashboardLoading, dashboardError } from '$lib/stores/dashboard';
	import { getIdToken } from '$lib/stores/auth';
	import { createInstance } from '$lib/api/client';
	import InstanceCard from '$lib/components/InstanceCard.svelte';
	import SubscriptionCard from '$lib/components/SubscriptionCard.svelte';

	let agentName = $state('');
	let deploying = $state(false);
	let deployError = $state('');

	async function handleDeploy(e: Event) {
		e.preventDefault();
		deploying = true;
		deployError = '';
		try {
			const token = await getIdToken();
			if (!token) return;
			const instance = await createInstance(token, {
				name: agentName,
				region: 'eu-central'
			});
			goto(`/dashboard/instances/${instance.id}`);
		} catch (err) {
			deployError = err instanceof Error ? err.message : 'Failed to deploy agent';
		} finally {
			deploying = false;
		}
	}
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

			{#if $dashboardState.instances.length > 0}
				<InstanceCard instance={$dashboardState.instances[0]} />
			{:else if $dashboardState.subscription}
				<!-- Has subscription but no instance — show deploy card -->
				<div class="rounded-xl border border-gray-200 p-6">
					<h3 class="text-sm font-semibold text-gray-900">Deploy your agent</h3>
					<p class="mt-1 text-sm text-gray-500">
						Your subscription is active. Deploy your dedicated AI agent to get started.
					</p>

					{#if deployError}
						<div class="mt-4 rounded-lg bg-red-50 p-3 text-sm text-red-700">{deployError}</div>
					{/if}

					<form onsubmit={handleDeploy} class="mt-4 space-y-4">
						<div>
							<label for="agent-name" class="block text-sm font-medium text-gray-700"
								>Agent Name</label
							>
							<input
								id="agent-name"
								type="text"
								bind:value={agentName}
								required
								class="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900"
								placeholder="e.g. my-trading-agent"
							/>
						</div>
						<button
							type="submit"
							disabled={deploying}
							class="rounded-lg bg-gray-900 px-6 py-2.5 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
						>
							{deploying ? 'Deploying...' : 'Deploy Agent'}
						</button>
					</form>
				</div>
			{:else}
				<!-- No subscription — prompt to subscribe -->
				<div class="rounded-xl border border-dashed border-gray-300 p-10 text-center">
					<p class="text-gray-500">No active subscription</p>
					<a
						href="/onboarding/checkout"
						class="mt-2 inline-block text-sm font-medium text-gray-900 hover:underline"
					>
						Subscribe to deploy your agent
					</a>
				</div>
			{/if}
		</div>

		<!-- Sidebar -->
		<div class="space-y-6">
			<SubscriptionCard subscription={$dashboardState.subscription} />
		</div>
	</div>
{/if}
