<script lang="ts">
	let email = $state('');
	let submitted = $state(false);

	function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		submitted = true;
	}
</script>

<div class="flex min-h-screen flex-col items-center justify-center bg-white px-4">
	<div class="flex flex-col items-center">
		<!-- Stylized T icon -->
		<div
			class="flex h-20 w-20 items-center justify-center rounded-2xl bg-gray-900 text-3xl font-bold text-white"
		>
			T
		</div>

		<h1 class="mt-6 text-5xl font-bold tracking-tight text-gray-900 sm:text-7xl">Tardi</h1>
		<p class="mt-2 text-lg font-medium text-gray-400">Dedicated AI Agent Hosting</p>
	</div>

	<div class="mt-16 max-w-lg text-center">
		<h2 class="text-2xl font-semibold text-gray-900 sm:text-3xl">Something exciting is coming</h2>
		<p class="mt-4 text-gray-500">
			Deploy your AI agent on dedicated infrastructure in minutes. Configure, launch, and manage
			&mdash; zero DevOps required.
		</p>
	</div>

	{#if submitted}
		<div class="mt-10 rounded-lg bg-gray-50 px-6 py-4 text-center">
			<p class="text-sm font-medium text-gray-900">You're on the list!</p>
			<p class="mt-1 text-sm text-gray-500">We'll notify you when Tardi launches.</p>
		</div>
	{:else}
		<form onsubmit={handleSubmit} class="mt-10 flex w-full max-w-sm gap-3">
			<input
				type="email"
				bind:value={email}
				required
				placeholder="you@example.com"
				class="flex-1 rounded-lg border border-gray-300 px-4 py-3 text-sm text-gray-900 placeholder-gray-400 focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900"
			/>
			<button
				type="submit"
				class="rounded-lg bg-gray-900 px-5 py-3 text-sm font-medium text-white transition-colors hover:bg-gray-800"
			>
				Notify Me
			</button>
		</form>
	{/if}

	<p class="mt-20 text-xs text-gray-400">&copy; {new Date().getFullYear()} Tardi</p>
</div>
