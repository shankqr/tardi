<script lang="ts">
	import '../app.css';
	import * as Sentry from '@sentry/sveltekit';
	import { onMount } from 'svelte';
	import { initAuth, user } from '$lib/stores/auth';
	import { apiUrl, stripePricingTableId, stripePublishableKey } from '$lib/stores/config';
	import { initTheme } from '$lib/stores/theme';
	import Navbar from '$lib/components/Navbar.svelte';
	import Footer from '$lib/components/Footer.svelte';
	import ComingSoon from '$lib/components/ComingSoon.svelte';

	let { data, children } = $props();
	let comingSoon = $derived(data.config.comingSoon);

	$effect(() => {
		apiUrl.set(data.config.apiUrl);
		stripePricingTableId.set(data.config.stripePricingTableId);
		stripePublishableKey.set(data.config.stripePublishableKey);
	});

	$effect(() => {
		const unsubscribe = user.subscribe(($user) => {
			if ($user) {
				Sentry.setUser({ id: $user.uid, email: $user.email || undefined });
			} else {
				Sentry.setUser(null);
			}
		});
		return unsubscribe;
	});

	onMount(() => {
		initTheme();
		if (!comingSoon) {
			initAuth();
		}
	});
</script>

<svelte:head>
	<title>Tardi — Dedicated AI Agent Hosting</title>
</svelte:head>

{#if comingSoon}
	<ComingSoon />
{:else}
	<div class="flex min-h-screen flex-col bg-white dark:bg-gray-950">
		<Navbar />
		<main class="flex-1">
			{@render children()}
		</main>
		<Footer />
	</div>
{/if}
