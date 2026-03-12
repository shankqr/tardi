<script lang="ts">
	import { page } from '$app/stores';
</script>

<div class="flex min-h-[60vh] items-center justify-center px-4">
	<div class="text-center">
		<h1 class="text-6xl font-bold text-gray-900">{$page.status}</h1>
		<p class="mt-4 text-lg text-gray-600">{$page.error?.message || 'Something went wrong'}</p>
		{#if $page.error?.eventId}
			<p class="mt-2 text-sm text-gray-400">Error ID: {$page.error.eventId}</p>
		{/if}
		<a
			href="/dashboard"
			class="mt-8 inline-block rounded-lg bg-gray-900 px-6 py-3 text-white hover:bg-gray-800"
		>
			Back to Dashboard
		</a>
	</div>
</div>
