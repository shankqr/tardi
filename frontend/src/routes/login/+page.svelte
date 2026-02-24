<script lang="ts">
	import { goto } from '$app/navigation';
	import { signIn, authError } from '$lib/stores/auth';

	let email = $state('');
	let password = $state('');
	let submitting = $state(false);

	async function handleSubmit(e: Event) {
		e.preventDefault();
		submitting = true;
		try {
			await signIn(email, password);
			goto('/dashboard');
		} catch {
			// error handled by store
		} finally {
			submitting = false;
		}
	}
</script>

<div class="flex min-h-[calc(100vh-10rem)] items-center justify-center px-4">
	<div class="w-full max-w-md">
		<h1 class="text-2xl font-bold text-gray-900 text-center">Log in to Tardi</h1>
		<p class="mt-2 text-center text-sm text-gray-500">
			Don't have an account? <a href="/signup" class="text-gray-900 font-medium hover:underline">Sign up</a>
		</p>

		<form onsubmit={handleSubmit} class="mt-8 space-y-4">
			{#if $authError}
				<div class="rounded-lg bg-red-50 p-3 text-sm text-red-700">{$authError}</div>
			{/if}

			<div>
				<label for="email" class="block text-sm font-medium text-gray-700">Email</label>
				<input
					id="email"
					type="email"
					bind:value={email}
					required
					class="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900"
					placeholder="you@example.com"
				/>
			</div>

			<div>
				<label for="password" class="block text-sm font-medium text-gray-700">Password</label>
				<input
					id="password"
					type="password"
					bind:value={password}
					required
					minlength="8"
					class="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900"
					placeholder="Min 8 characters"
				/>
			</div>

			<button
				type="submit"
				disabled={submitting}
				class="w-full rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
			>
				{submitting ? 'Signing in...' : 'Sign in'}
			</button>
		</form>

	</div>
</div>
