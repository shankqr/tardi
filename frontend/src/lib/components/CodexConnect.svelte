<script lang="ts">
	import { getIdToken } from '$lib/stores/auth';
	import { startCodexLink, getCodexLinkStatus, unlinkCodex } from '$lib/api/client';

	let { instanceId, instanceStatus }: { instanceId: string; instanceStatus: string } = $props();

	type Phase = 'idle' | 'starting' | 'waiting' | 'linked' | 'unlinking' | 'failed';

	let phase = $state<Phase>('idle');
	let code = $state<string | null>(null);
	let verificationUrl = $state<string>('https://auth.openai.com/codex/device');
	let error = $state<string | null>(null);
	let pollTimer: ReturnType<typeof setInterval> | null = null;
	let expiryTimer: ReturnType<typeof setTimeout> | null = null;
	let isLinked = $state(false);

	const isActive = $derived(instanceStatus === 'active');

	$effect(() => {
		checkStatus();
		return () => cleanup();
	});

	async function checkStatus() {
		try {
			const token = await getIdToken();
			if (!token) return;
			const s = await getCodexLinkStatus(token, instanceId);
			isLinked = s.status === 'linked';
		} catch {
			// Ignore — treat as not linked
		}
	}

	async function handleStart() {
		error = null;
		phase = 'starting';
		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');

			const res = await startCodexLink(token, instanceId);
			code = res.code;
			verificationUrl = res.verification_url;
			phase = 'waiting';

			pollTimer = setInterval(async () => {
				try {
					const s = await getCodexLinkStatus(token, instanceId);
					if (s.status === 'linked') {
						cleanup();
						isLinked = true;
						phase = 'linked';
					}
				} catch {
					// keep polling
				}
			}, 3000);

			// Device code lifetime is 15 min
			expiryTimer = setTimeout(() => {
				if (phase === 'waiting') {
					cleanup();
					error = 'The code expired. Click Link Codex to get a fresh one.';
					phase = 'failed';
				}
			}, 15 * 60 * 1000);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to start linking';
			phase = 'failed';
		}
	}

	async function handleCancel() {
		cleanup();
		phase = 'idle';
		code = null;
	}

	async function handleUnlink() {
		if (!confirm('Unlink your Codex account? Your agent will lose access to codex/* models until you link again.')) return;
		error = null;
		phase = 'unlinking';
		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');
			await unlinkCodex(token, instanceId);
			isLinked = false;
			phase = 'idle';
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to unlink';
			phase = 'failed';
		}
	}

	function copyCode() {
		if (code) navigator.clipboard?.writeText(code).catch(() => {});
	}

	function cleanup() {
		if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
		if (expiryTimer) { clearTimeout(expiryTimer); expiryTimer = null; }
	}
</script>

<div class="rounded-xl border border-gray-200 dark:border-gray-700 p-5">
	<div class="flex items-center gap-2">
		<!-- OpenAI-style mark -->
		<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" class="h-5 w-5 text-gray-900 dark:text-white" fill="currentColor">
			<path d="M22.28 9.82a5.92 5.92 0 0 0-.51-4.86 6 6 0 0 0-6.46-2.88A6 6 0 0 0 5.2 4.1a5.92 5.92 0 0 0-3.96 2.87 6 6 0 0 0 .74 7.03 5.92 5.92 0 0 0 .51 4.86 6 6 0 0 0 6.46 2.88 5.98 5.98 0 0 0 4.52 2.03 6 6 0 0 0 5.72-4.16 5.92 5.92 0 0 0 3.96-2.87 6 6 0 0 0-.87-6.92ZM13.48 21.1a4.46 4.46 0 0 1-2.87-1.04l.14-.08 4.77-2.76a.78.78 0 0 0 .39-.68V9.8l2.02 1.17c.02.01.04.03.04.05v5.58a4.5 4.5 0 0 1-4.49 4.5Zm-9.66-4.13a4.47 4.47 0 0 1-.54-3.01l.14.08 4.77 2.76a.77.77 0 0 0 .78 0l5.83-3.37v2.33c0 .02 0 .04-.03.05L9.95 18.6a4.5 4.5 0 0 1-6.13-1.63Zm-1.26-10.4a4.47 4.47 0 0 1 2.35-1.97v5.68a.77.77 0 0 0 .39.68l5.8 3.35-2.02 1.17a.07.07 0 0 1-.07 0L4.2 12.39a4.5 4.5 0 0 1-1.64-5.82Zm16.57 3.85-5.8-3.36 2.01-1.16a.07.07 0 0 1 .07 0l4.82 2.79a4.5 4.5 0 0 1-.68 8.1v-5.68a.78.78 0 0 0-.42-.69Zm2.01-3.03-.14-.09-4.76-2.77a.78.78 0 0 0-.79 0L9.62 7.89V5.56a.08.08 0 0 1 .03-.06l4.82-2.78a4.5 4.5 0 0 1 6.68 4.66ZM8.52 12.35 6.5 11.18a.08.08 0 0 1-.04-.06V5.55a4.5 4.5 0 0 1 7.38-3.46l-.14.08-4.77 2.76a.78.78 0 0 0-.39.68Zm1.1-2.37 2.6-1.5 2.6 1.5v3l-2.6 1.5-2.6-1.5Z"/>
		</svg>
		<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Codex (ChatGPT)</h3>
	</div>
	<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">
		Link your ChatGPT account to use <code>codex/gpt-5.4</code> models on your agent — no API key needed.
	</p>

	<div class="mt-4">
		{#if phase === 'waiting' && code}
			<div class="space-y-3">
				<p class="text-sm text-gray-700 dark:text-gray-300">
					1. Open <a href={verificationUrl} target="_blank" rel="noopener noreferrer" class="font-medium underline">openai.com/codex/device</a>
				</p>
				<p class="text-sm text-gray-700 dark:text-gray-300">2. Enter this code (expires in 15 min):</p>
				<div class="flex items-center gap-3">
					<code class="rounded-lg bg-gray-100 dark:bg-gray-800 px-3 py-2 font-mono text-base tracking-widest text-gray-900 dark:text-white">{code}</code>
					<button
						onclick={copyCode}
						class="rounded-lg border border-gray-200 dark:border-gray-700 px-3 py-1.5 text-xs font-medium text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-800"
					>Copy</button>
					<button
						onclick={handleCancel}
						class="rounded-lg border border-gray-200 dark:border-gray-700 px-3 py-1.5 text-xs font-medium text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-800"
					>Cancel</button>
				</div>
				<div class="flex items-center gap-2 text-xs text-amber-700 dark:text-amber-400">
					<svg class="h-4 w-4 animate-spin" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
						<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
						<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
					</svg>
					<span>Waiting for you to authorize on openai.com…</span>
				</div>
			</div>
		{:else if phase === 'linked' || isLinked}
			<div class="flex items-center justify-between">
				<div class="flex items-center gap-2 text-sm text-green-700 dark:text-green-400">
					<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="h-4 w-4">
						<path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.857-9.809a.75.75 0 00-1.214-.882l-3.483 4.79-1.88-1.88a.75.75 0 10-1.06 1.061l2.5 2.5a.75.75 0 001.137-.089l4-5.5z" clip-rule="evenodd" />
					</svg>
					<span>Linked to ChatGPT — codex/* models are available</span>
				</div>
				<button
					onclick={handleUnlink}
					disabled={phase === 'unlinking' || !isActive}
					class="rounded-lg border border-gray-200 dark:border-gray-700 px-3 py-1.5 text-xs font-medium text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-50"
				>
					{phase === 'unlinking' ? 'Unlinking…' : 'Unlink'}
				</button>
			</div>
		{:else}
			<button
				onclick={handleStart}
				disabled={phase === 'starting' || !isActive}
				class="rounded-lg bg-gray-900 dark:bg-white px-4 py-2 text-sm font-medium text-white dark:text-gray-900 hover:bg-gray-800 dark:hover:bg-gray-100 disabled:opacity-50"
			>
				{phase === 'starting' ? 'Starting…' : 'Link Codex Account'}
			</button>
		{/if}

		{#if error}
			<p class="mt-3 text-xs text-red-600 dark:text-red-400">{error}</p>
		{/if}
	</div>
</div>
