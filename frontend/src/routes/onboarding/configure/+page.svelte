<script lang="ts">
	import { goto } from '$app/navigation';
	import { setAgentConfig } from '$lib/stores/onboarding';

	let name = $state('');
	let description = $state('');

	function handleSubmit(e: Event) {
		e.preventDefault();
		setAgentConfig({ name, model: '', description });
		goto('/onboarding/plan');
	}
</script>

<div class="mx-auto max-w-2xl py-12 px-4">
	<!-- Progress -->
	<div class="flex items-center justify-center gap-2 text-sm text-gray-400">
		<span class="font-medium text-gray-900">1. Configure</span>
		<span>&rarr;</span>
		<span>2. Select Plan</span>
		<span>&rarr;</span>
		<span>3. Checkout</span>
	</div>

	<h1 class="mt-8 text-2xl font-bold text-gray-900 text-center">Configure your AI agent</h1>
	<p class="mt-2 text-center text-sm text-gray-500">
		Set up your OpenClaw agent parameters. You can change these later.
	</p>

	<form onsubmit={handleSubmit} class="mt-8 space-y-6">
		<div>
			<label for="name" class="block text-sm font-medium text-gray-700">Agent Name</label>
			<input
				id="name"
				type="text"
				bind:value={name}
				required
				class="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900"
				placeholder="e.g. my-trading-agent"
			/>
			<p class="mt-1 text-xs text-gray-400">Used to identify your agent in the dashboard</p>
		</div>

		<div>
			<label for="description" class="block text-sm font-medium text-gray-700">Description (optional)</label>
			<textarea
				id="description"
				bind:value={description}
				rows="3"
				class="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900"
				placeholder="What does this agent do?"
			></textarea>
		</div>

		<div class="flex justify-end">
			<button
				type="submit"
				class="rounded-lg bg-gray-900 px-6 py-2.5 text-sm font-medium text-white hover:bg-gray-800"
			>
				Continue to Plans &rarr;
			</button>
		</div>
	</form>
</div>
