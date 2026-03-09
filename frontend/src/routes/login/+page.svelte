<script lang="ts">
	import { goto } from '$app/navigation';
	import { signIn, resetPassword, authError } from '$lib/stores/auth';

	let email = $state('');
	let password = $state('');
	let submitting = $state(false);
	let forgotMode = $state(false);
	let resetSent = $state(false);

	async function handleSubmit(e: Event) {
		e.preventDefault();
		submitting = true;
		try {
			if (forgotMode) {
				await resetPassword(email);
				resetSent = true;
			} else {
				await signIn(email, password);
				goto('/dashboard');
			}
		} catch {
			// error handled by store
		} finally {
			submitting = false;
		}
	}

	function backToLogin() {
		forgotMode = false;
		resetSent = false;
	}
</script>

<div class="flex min-h-[calc(100vh-10rem)] items-center justify-center px-4">
	<div class="w-full max-w-md">
		<h1 class="text-2xl font-bold text-gray-900 text-center">
			{forgotMode ? 'Reset your password' : 'Log in to Tardi'}
		</h1>
		<p class="mt-2 text-center text-sm text-gray-500">
			{#if forgotMode}
				Enter your email and we'll send you a reset link.
			{:else}
				Don't have an account? <a href="/signup" class="text-gray-900 font-medium hover:underline">Sign up</a>
			{/if}
		</p>

		{#if resetSent}
			<div class="mt-8 rounded-lg bg-green-50 p-4 text-sm text-green-700">
				Check your email for a password reset link.
			</div>
			<button
				onclick={backToLogin}
				class="mt-4 w-full text-center text-sm text-gray-900 font-medium hover:underline"
			>
				Back to login
			</button>
		{:else}
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

				{#if !forgotMode}
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

					<div class="text-right">
						<button
							type="button"
							onclick={() => (forgotMode = true)}
							class="text-sm text-gray-500 hover:text-gray-900 hover:underline"
						>
							Forgot password?
						</button>
					</div>
				{/if}

				<button
					type="submit"
					disabled={submitting}
					class="w-full rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
				>
					{#if forgotMode}
						{submitting ? 'Sending...' : 'Send reset link'}
					{:else}
						{submitting ? 'Signing in...' : 'Sign in'}
					{/if}
				</button>

				{#if forgotMode}
					<button
						type="button"
						onclick={backToLogin}
						class="w-full text-center text-sm text-gray-500 hover:text-gray-900 hover:underline"
					>
						Back to login
					</button>
				{/if}
			</form>
		{/if}

	</div>
</div>
