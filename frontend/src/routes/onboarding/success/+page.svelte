<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { getDashboardState } from '$lib/api/client';
	import { getIdToken } from '$lib/stores/auth';

	let status = $state<'polling' | 'ready' | 'error'>('polling');
	let attempts = $state(0);
	const maxAttempts = 30;

	onMount(() => {
		const interval = setInterval(async () => {
			attempts++;
			try {
				const token = await getIdToken();
				if (!token) return;
				const state = await getDashboardState(token);
				if (state.subscription) {
					clearInterval(interval);
					status = 'ready';
					setTimeout(() => goto('/dashboard'), 1500);
				}
			} catch {
				// Keep polling on transient errors
			}
			if (attempts >= maxAttempts) {
				status = 'error';
				clearInterval(interval);
			}
		}, 2000);

		return () => clearInterval(interval);
	});
</script>

<div class="mx-auto max-w-lg py-20 px-4 text-center">
	{#if status === 'polling'}
		<div
			class="mx-auto h-12 w-12 animate-spin rounded-full border-4 border-gray-200 dark:border-gray-700 border-t-gray-900 dark:border-t-white"
		></div>
		<h1 class="mt-6 text-2xl font-bold text-gray-900 dark:text-white">Setting up your account...</h1>
		<p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
			We're confirming your payment. This usually takes just a few seconds.
		</p>
	{:else if status === 'ready'}
		<svg
			class="mx-auto h-12 w-12 text-green-500"
			fill="none"
			viewBox="0 0 24 24"
			stroke="currentColor"
		>
			<path
				stroke-linecap="round"
				stroke-linejoin="round"
				stroke-width="2"
				d="M5 13l4 4L19 7"
			/>
		</svg>
		<h1 class="mt-6 text-2xl font-bold text-gray-900 dark:text-white">You're all set!</h1>
		<p class="mt-2 text-sm text-gray-500 dark:text-gray-400">Redirecting you to your dashboard...</p>
	{:else}
		<h1 class="mt-6 text-2xl font-bold text-gray-900 dark:text-white">Something took longer than expected</h1>
		<p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
			Your payment was processed, but it's taking a moment to set up. Please check your dashboard
			in a minute.
		</p>
		<a
			href="/dashboard"
			class="mt-6 inline-block rounded-lg bg-gray-900 dark:bg-white px-6 py-2.5 text-sm font-medium text-white dark:text-gray-900 hover:bg-gray-800 dark:hover:bg-gray-100"
		>
			Go to Dashboard
		</a>
	{/if}
</div>
