<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { user, authLoading, signOut } from '$lib/stores/auth';
	import { startPolling, stopPolling } from '$lib/stores/dashboard';
	import { theme, toggleTheme } from '$lib/stores/theme';

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
		<p class="text-gray-400 dark:text-gray-500">Loading...</p>
	</div>
{:else if $user}
	<div class="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8 py-8">
		<div class="flex items-center justify-between mb-8">
			<div>
				<h1 class="text-2xl font-bold text-gray-900 dark:text-white">Dashboard</h1>
				<p class="text-sm text-gray-500 dark:text-gray-400">{$user.email}</p>
			</div>
			<div class="flex items-center gap-4">
				<nav class="hidden sm:flex items-center gap-4 text-sm">
					<a href="/dashboard" class="text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white">Agents</a>
					<a href="/dashboard/billing" class="text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white">Billing</a>
					<a href="/dashboard/settings" class="text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white">Settings</a>
				</nav>
				<button
					onclick={toggleTheme}
					class="rounded-lg p-2 text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white"
					aria-label="Toggle theme"
				>
					{#if $theme === 'dark'}
						<svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
							<path stroke-linecap="round" stroke-linejoin="round" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" />
						</svg>
					{:else}
						<svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
							<path stroke-linecap="round" stroke-linejoin="round" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
						</svg>
					{/if}
				</button>
				<button
					onclick={handleSignOut}
					class="rounded-lg border border-gray-300 dark:border-gray-600 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-800"
				>
					Sign out
				</button>
			</div>
		</div>
		{@render children()}
	</div>
{/if}
