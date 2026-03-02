<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { getDashboardState, createInstance } from '$lib/api/client';
	import { getIdToken } from '$lib/stores/auth';
	import { loadPersistedConfig, clearPersistedConfig } from '$lib/stores/onboarding';

	let status = $state<'polling' | 'deploying' | 'ready' | 'error'>('polling');
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

					// If user already has an instance, go straight to it
					if (state.instances.length > 0) {
						status = 'ready';
						setTimeout(() => goto(`/dashboard/instances/${state.instances[0].id}`), 1500);
						return;
					}

					// Auto-create instance with saved agent config
					status = 'deploying';
					const config = loadPersistedConfig();
					const name = config?.name || 'my-agent';

					try {
						const instance = await createInstance(token, {
							name,
							region: 'eu-central'
						});
						clearPersistedConfig();
						status = 'ready';
						setTimeout(() => goto(`/dashboard/instances/${instance.id}`), 1500);
					} catch {
						// Instance creation failed — redirect to dashboard as fallback
						clearPersistedConfig();
						status = 'ready';
						setTimeout(() => goto('/dashboard'), 1500);
					}
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
			class="mx-auto h-12 w-12 animate-spin rounded-full border-4 border-gray-200 border-t-gray-900"
		></div>
		<h1 class="mt-6 text-2xl font-bold text-gray-900">Setting up your account...</h1>
		<p class="mt-2 text-sm text-gray-500">
			We're confirming your payment. This usually takes just a few seconds.
		</p>
	{:else if status === 'deploying'}
		<div
			class="mx-auto h-12 w-12 animate-spin rounded-full border-4 border-gray-200 border-t-gray-900"
		></div>
		<h1 class="mt-6 text-2xl font-bold text-gray-900">Deploying your agent...</h1>
		<p class="mt-2 text-sm text-gray-500">
			Payment confirmed! We're now provisioning your dedicated infrastructure.
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
		<h1 class="mt-6 text-2xl font-bold text-gray-900">You're all set!</h1>
		<p class="mt-2 text-sm text-gray-500">Redirecting you to your agent...</p>
	{:else}
		<h1 class="mt-6 text-2xl font-bold text-gray-900">Something took longer than expected</h1>
		<p class="mt-2 text-sm text-gray-500">
			Your payment was processed, but it's taking a moment to set up. Please check your dashboard
			in a minute.
		</p>
		<a
			href="/dashboard"
			class="mt-6 inline-block rounded-lg bg-gray-900 px-6 py-2.5 text-sm font-medium text-white hover:bg-gray-800"
		>
			Go to Dashboard
		</a>
	{/if}
</div>
