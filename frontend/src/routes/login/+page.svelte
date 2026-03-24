<script lang="ts">
	import { goto } from '$app/navigation';
	import { signIn, resetPassword, authError, user, signInWithGoogle } from '$lib/stores/auth';
	import { get } from 'svelte/store';

	let email = $state('');
	let password = $state('');
	let submitting = $state(false);
	let googleSubmitting = $state(false);
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
				const currentUser = get(user);
				if (currentUser && !currentUser.emailVerified) {
					goto('/verify-email');
				} else {
					goto('/dashboard');
				}
			}
		} catch {
			// error handled by store
		} finally {
			submitting = false;
		}
	}

	async function handleGoogleSignIn() {
		googleSubmitting = true;
		try {
			await signInWithGoogle();
			const currentUser = get(user);
			if (currentUser?.emailVerified) {
				goto('/dashboard');
			} else {
				goto('/verify-email');
			}
		} catch {
			// error handled by store
		} finally {
			googleSubmitting = false;
		}
	}

	function backToLogin() {
		forgotMode = false;
		resetSent = false;
	}
</script>

<div class="flex min-h-[calc(100vh-10rem)] items-center justify-center px-4">
	<div class="w-full max-w-md">
		<h1 class="text-2xl font-bold text-gray-900 dark:text-white text-center">
			{forgotMode ? 'Reset your password' : 'Log in to Tardi'}
		</h1>
		<p class="mt-2 text-center text-sm text-gray-500 dark:text-gray-400">
			{#if forgotMode}
				Enter your email and we'll send you a reset link.
			{:else}
				Don't have an account? <a href="/signup" class="text-gray-900 dark:text-white font-medium hover:underline">Sign up</a>
			{/if}
		</p>

		{#if resetSent}
			<div class="mt-8 rounded-lg bg-green-50 dark:bg-green-900/20 p-4 text-sm text-green-700 dark:text-green-400">
				Check your email for a password reset link.
			</div>
			<button
				onclick={backToLogin}
				class="mt-4 w-full text-center text-sm text-gray-900 dark:text-white font-medium hover:underline"
			>
				Back to login
			</button>
		{:else}
			<div class="mt-8">
				{#if $authError}
					<div class="mb-4 rounded-lg bg-red-50 dark:bg-red-900/20 p-3 text-sm text-red-700 dark:text-red-400">{$authError}</div>
				{/if}

				{#if !forgotMode}
					<button
						onclick={handleGoogleSignIn}
						disabled={googleSubmitting || submitting}
						class="flex w-full items-center justify-center gap-3 rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 px-4 py-2.5 text-sm font-medium text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700 disabled:opacity-50"
					>
						<svg class="h-5 w-5" viewBox="0 0 24 24">
							<path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z" fill="#4285F4" />
							<path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853" />
							<path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18A11.96 11.96 0 0 0 0 12c0 1.94.46 3.77 1.28 5.4l3.56-2.77z" fill="#FBBC05" />
							<path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335" />
						</svg>
						{googleSubmitting ? 'Signing in...' : 'Continue with Google'}
					</button>

					<div class="relative my-6">
						<div class="absolute inset-0 flex items-center">
							<div class="w-full border-t border-gray-200 dark:border-gray-700"></div>
						</div>
						<div class="relative flex justify-center text-sm">
							<span class="bg-white dark:bg-gray-950 px-4 text-gray-400 dark:text-gray-500">or</span>
						</div>
					</div>
				{/if}

				<form onsubmit={handleSubmit} class="space-y-4">
					<div>
						<label for="email" class="block text-sm font-medium text-gray-700 dark:text-gray-300">Email</label>
						<input
							id="email"
							type="email"
							bind:value={email}
							required
							class="mt-1 block w-full rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 px-3 py-2 text-sm text-gray-900 dark:text-white focus:border-gray-900 dark:focus:border-gray-400 focus:outline-none focus:ring-1 focus:ring-gray-900 dark:focus:ring-gray-400"
							placeholder="you@example.com"
						/>
					</div>

					{#if !forgotMode}
						<div>
							<label for="password" class="block text-sm font-medium text-gray-700 dark:text-gray-300">Password</label>
							<input
								id="password"
								type="password"
								bind:value={password}
								required
								minlength="8"
								class="mt-1 block w-full rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 px-3 py-2 text-sm text-gray-900 dark:text-white focus:border-gray-900 dark:focus:border-gray-400 focus:outline-none focus:ring-1 focus:ring-gray-900 dark:focus:ring-gray-400"
								placeholder="Min 8 characters"
							/>
						</div>

						<div class="text-right">
							<button
								type="button"
								onclick={() => (forgotMode = true)}
								class="text-sm text-gray-500 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white hover:underline"
							>
								Forgot password?
							</button>
						</div>
					{/if}

					<button
						type="submit"
						disabled={submitting || googleSubmitting}
						class="w-full rounded-lg bg-gray-900 dark:bg-white px-4 py-2.5 text-sm font-medium text-white dark:text-gray-900 hover:bg-gray-800 dark:hover:bg-gray-100 disabled:opacity-50"
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
							class="w-full text-center text-sm text-gray-500 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white hover:underline"
						>
							Back to login
						</button>
					{/if}
				</form>
			</div>
		{/if}

	</div>
</div>
