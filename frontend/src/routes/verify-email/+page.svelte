<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import {
		user,
		authLoading,
		emailVerified,
		reloadUser,
		forceTokenRefresh,
		sendVerificationEmail,
		signOut
	} from '$lib/stores/auth';

	let resendCooldown = $state(0);
	let resending = $state(false);

	const currentUser = $derived($user);
	const loading = $derived($authLoading);
	const verified = $derived($emailVerified);

	onMount(() => {
		// Send verification email on mount (covers login flow for unverified users)
		sendVerificationEmail().then(() => {
			resendCooldown = 60;
		}).catch(() => {});

		// Poll for verification every 3 seconds
		const pollInterval = setInterval(async () => {
			const isVerified = await reloadUser();
			if (isVerified) {
				clearInterval(pollInterval);
				await forceTokenRefresh();
				goto('/onboarding/checkout');
			}
		}, 3000);

		// Cooldown countdown
		const cooldownInterval = setInterval(() => {
			if (resendCooldown > 0) resendCooldown--;
		}, 1000);

		return () => {
			clearInterval(pollInterval);
			clearInterval(cooldownInterval);
		};
	});

	// Redirect if not logged in
	$effect(() => {
		if (!loading && !currentUser) {
			goto('/signup');
		}
	});

	// Redirect if already verified
	$effect(() => {
		if (!loading && currentUser && verified) {
			goto('/onboarding/checkout');
		}
	});

	async function handleResend() {
		resending = true;
		try {
			await sendVerificationEmail();
			resendCooldown = 60;
		} finally {
			resending = false;
		}
	}

	async function handleChangeEmail() {
		await signOut();
		goto('/signup');
	}
</script>

<div class="flex min-h-[calc(100vh-10rem)] items-center justify-center px-4">
	<div class="w-full max-w-md text-center">
		<!-- Email icon -->
		<div class="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-gray-100">
			<svg class="h-8 w-8 text-gray-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
				<path stroke-linecap="round" stroke-linejoin="round" d="M21.75 6.75v10.5a2.25 2.25 0 0 1-2.25 2.25h-15a2.25 2.25 0 0 1-2.25-2.25V6.75m19.5 0A2.25 2.25 0 0 0 19.5 4.5h-15a2.25 2.25 0 0 0-2.25 2.25m19.5 0v.243a2.25 2.25 0 0 1-1.07 1.916l-7.5 4.615a2.25 2.25 0 0 1-2.36 0L3.32 8.91a2.25 2.25 0 0 1-1.07-1.916V6.75" />
			</svg>
		</div>

		<h1 class="mt-6 text-2xl font-bold text-gray-900">Check your inbox</h1>
		<p class="mt-2 text-sm text-gray-500">
			We sent a verification link to
			{#if currentUser?.email}
				<span class="font-medium text-gray-900">{currentUser.email}</span>
			{/if}
		</p>
		<p class="mt-1 text-sm text-gray-500">
			Click the link to continue.
		</p>

		<div class="mt-8 space-y-3">
			<button
				onclick={handleResend}
				disabled={resendCooldown > 0 || resending}
				class="w-full rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
			>
				{#if resending}
					Sending...
				{:else if resendCooldown > 0}
					Resend email ({resendCooldown}s)
				{:else}
					Resend verification email
				{/if}
			</button>

			<button
				onclick={handleChangeEmail}
				class="w-full text-sm text-gray-500 hover:text-gray-900 hover:underline"
			>
				Use a different email?
			</button>
		</div>
	</div>
</div>
