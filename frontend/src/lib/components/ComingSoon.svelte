<script lang="ts">
	let email = $state('');
	let submitting = $state(false);
	let success = $state(false);
	let error = $state('');

	async function handleSubmit(e: Event) {
		e.preventDefault();
		error = '';
		submitting = true;

		try {
			const res = await fetch('/api/waitlist', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ email })
			});

			const data = await res.json();

			if (!res.ok) {
				error = data.error || 'Something went wrong. Please try again.';
				return;
			}

			success = true;
		} catch {
			error = 'Something went wrong. Please try again.';
		} finally {
			submitting = false;
		}
	}
</script>

<div class="flex min-h-screen flex-col items-center justify-center bg-white px-4">
	<div class="flex flex-col items-center">
		<img src="/tardiai-moscot.jpeg" alt="Tardi.ai mascot" class="max-w-xs" />

		<h1 class="mt-6 text-5xl font-bold tracking-tight text-gray-900 sm:text-7xl">Tardi.ai</h1>
		<p class="mt-2 text-lg font-medium text-gray-400">Dedicated AI Agent Hosting</p>
	</div>

	<div class="mt-16 max-w-lg text-center">
		<h2 class="text-2xl font-semibold text-gray-900 sm:text-3xl">Something exciting is coming</h2>
		<p class="mt-4 text-gray-500">
			Deploy your AI agent on dedicated infrastructure in minutes. Configure, launch, and manage
			&mdash; zero DevOps required.
		</p>

		{#if success}
			<div class="mt-8 rounded-lg bg-green-50 p-4 text-sm text-green-700">
				You're on the list! We'll be in touch.
			</div>
		{:else}
			<form onsubmit={handleSubmit} class="mt-8 flex flex-col items-center gap-3 sm:flex-row sm:gap-2">
				<input
					type="email"
					bind:value={email}
					required
					placeholder="you@example.com"
					disabled={submitting}
					class="w-full rounded-lg border border-gray-300 px-3 py-2.5 text-sm focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900 disabled:opacity-50 sm:flex-1"
				/>
				<button
					type="submit"
					disabled={submitting}
					class="w-full rounded-lg bg-gray-900 px-6 py-2.5 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50 sm:w-auto"
				>
					{submitting ? 'Joining...' : 'Join Waitlist'}
				</button>
			</form>

			{#if error}
				<div class="mt-3 rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</div>
			{/if}
		{/if}
	</div>

	<p class="mt-20 text-xs text-gray-400">&copy; {new Date().getFullYear()} Tardi.ai</p>
</div>
