<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { initAuth } from '$lib/stores/auth';
	import { apiUrl } from '$lib/stores/config';
	import Navbar from '$lib/components/Navbar.svelte';
	import Footer from '$lib/components/Footer.svelte';
	import ComingSoon from '$lib/components/ComingSoon.svelte';

	let { data, children } = $props();
	let comingSoon = $derived(data.config.comingSoon);

	$effect(() => {
		apiUrl.set(data.config.apiUrl);
	});

	onMount(() => {
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
	<div class="flex min-h-screen flex-col">
		<Navbar />
		<main class="flex-1">
			{@render children()}
		</main>
		<Footer />
	</div>
{/if}
