<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { user, authLoading, signOut } from '$lib/stores/auth';
	import { startPolling, stopPolling } from '$lib/stores/dashboard';

	let { children } = $props();

	onMount(() => {
		const unsubscribe = user.subscribe((u) => {
			if (!$authLoading && !u) {
				goto('/login');
			}
		});

		startPolling();

		return () => {
			unsubscribe();
			stopPolling();
		};
	});

	async function handleSignOut() {
		await signOut();
		goto('/');
	}
</script>

{#if $authLoading}
	<div class="flex items-center justify-center py-20">
		<p class="text-gray-400">Loading...</p>
	</div>
{:else if $user}
	<div class="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8 py-8">
		<div class="flex items-center justify-between mb-8">
			<div>
				<h1 class="text-2xl font-bold text-gray-900">Dashboard</h1>
				<p class="text-sm text-gray-500">{$user.email}</p>
			</div>
			<div class="flex items-center gap-4">
				<nav class="hidden sm:flex items-center gap-4 text-sm">
					<a href="/dashboard" class="text-gray-600 hover:text-gray-900">Agents</a>
					<a href="/dashboard/billing" class="text-gray-600 hover:text-gray-900">Billing</a>
					<a href="/dashboard/settings" class="text-gray-600 hover:text-gray-900">Settings</a>
				</nav>
				<button
					onclick={handleSignOut}
					class="rounded-lg border border-gray-300 px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-50"
				>
					Sign out
				</button>
			</div>
		</div>
		{@render children()}
	</div>
{/if}
