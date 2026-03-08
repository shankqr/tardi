<script lang="ts">
	import { goto } from '$app/navigation';
	import { signUp, authError } from '$lib/stores/auth';

	let email = $state('');
	let password = $state('');
	let confirmPassword = $state('');
	let submitting = $state(false);
	let validationError = $state('');

	async function handleSubmit(e: Event) {
		e.preventDefault();
		validationError = '';

		if (password !== confirmPassword) {
			validationError = 'Passwords do not match';
			return;
		}

		submitting = true;
		try {
			await signUp(email, password);
			goto('/onboarding/checkout');
		} catch {
			// error handled by store
		} finally {
			submitting = false;
		}
	}
</script>

<div class="flex min-h-[calc(100vh-10rem)] items-center justify-center px-4">
	<div class="w-full max-w-md">
		<h1 class="text-2xl font-bold text-gray-900 text-center">Create your account</h1>
		<p class="mt-2 text-center text-sm text-gray-500">
			Already have an account? <a href="/login" class="text-gray-900 font-medium hover:underline">Log in</a>
		</p>

		<form onsubmit={handleSubmit} class="mt-8 space-y-4">
			{#if $authError || validationError}
				<div class="rounded-lg bg-red-50 p-3 text-sm text-red-700">{validationError || $authError}</div>
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

			<div>
				<label for="confirm-password" class="block text-sm font-medium text-gray-700">Confirm password</label>
				<input
					id="confirm-password"
					type="password"
					bind:value={confirmPassword}
					required
					minlength="8"
					class="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900"
					placeholder="Repeat password"
				/>
			</div>

			<button
				type="submit"
				disabled={submitting}
				class="w-full rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
			>
				{submitting ? 'Creating account...' : 'Create account'}
			</button>
		</form>

		<p class="mt-6 text-center text-xs text-gray-400">
			By creating an account, you agree to our Terms of Service and Privacy Policy.
		</p>
	</div>
</div>
