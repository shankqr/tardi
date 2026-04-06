<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { dashboardState } from '$lib/stores/dashboard';
	import { getIdToken } from '$lib/stores/auth';
	import { getDashboardToken } from '$lib/api/client';
	import HermesChat from '$lib/components/HermesChat.svelte';

	const instanceId = $derived($page.params.id);
	const instance = $derived(
		$dashboardState?.instances.find((i) => i.id === instanceId) ?? null
	);

	let authToken = $state<string | null>(null);
	let loading = $state(true);
	let error = $state('');

	$effect(() => {
		if (instance && !authToken) {
			loadToken();
		}
	});

	async function loadToken() {
		loading = true;
		try {
			const token = await getIdToken();
			if (!token || !instance) return;
			if (instance.framework !== 'hermes') {
				goto(`/dashboard/instances/${instanceId}`);
				return;
			}
			const { token: scopedToken } = await getDashboardToken(instance.id, token);
			authToken = scopedToken;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load chat';
		} finally {
			loading = false;
		}
	}
</script>

<div class="space-y-4">
	<div class="flex items-center gap-3">
		<a
			href="/dashboard/instances/{instanceId}"
			class="text-sm text-gray-500 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white"
		>
			&larr; Back to agent
		</a>
		{#if instance}
			<span class="text-sm text-gray-400 dark:text-gray-500">/</span>
			<span class="text-sm font-medium text-gray-900 dark:text-white">{instance.name}</span>
		{/if}
	</div>

	{#if loading}
		<div class="flex items-center justify-center py-20">
			<p class="text-gray-400 dark:text-gray-500">Connecting to agent...</p>
		</div>
	{:else if error}
		<div class="rounded-lg bg-red-50 dark:bg-red-900/20 p-4 text-sm text-red-700 dark:text-red-400">{error}</div>
	{:else if instance && authToken && instance.dashboard_url}
		<HermesChat dashboardUrl={instance.dashboard_url} {authToken} />
	{:else}
		<div class="rounded-lg bg-yellow-50 dark:bg-yellow-900/20 p-4 text-sm text-yellow-700 dark:text-yellow-400">
			Agent is not ready yet. Please wait for it to become active.
		</div>
	{/if}
</div>
