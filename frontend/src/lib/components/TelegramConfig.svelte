<script lang="ts">
	import { onMount } from 'svelte';
	import { getIdToken } from '$lib/stores/auth';
	import { getAgentConfig, updateAgentConfig, syncConfig, getSyncStatus } from '$lib/api/client';

	let {
		instanceId,
		disabled = false,
		onsyncchange = (_syncing: boolean) => {}
	}: {
		instanceId: string;
		disabled?: boolean;
		onsyncchange?: (syncing: boolean) => void;
	} = $props();

	let telegramBotToken = $state('');
	let telegramAllowedUsers = $state('');
	let loading = $state(true);
	let showToken = $state(false);
	let tokenDirty = $state(false);
	let allowedUsersDirty = $state(false);

	type SyncPhase = 'idle' | 'saving' | 'syncing' | 'success' | 'failed';
	let syncPhase = $state<SyncPhase>('idle');
	let syncError = $state<string | null>(null);
	let syncElapsed = $state(0);
	let syncTimer: ReturnType<typeof setInterval> | null = null;
	let pollTimer: ReturnType<typeof setInterval> | null = null;
	let syncTimeout: ReturnType<typeof setTimeout> | null = null;

	const isSyncing = $derived(syncPhase === 'saving' || syncPhase === 'syncing');
	const normalizedAllowedUsers = $derived(
		telegramAllowedUsers
			.split(/[,\s]+/)
			.map((id) => id.trim())
			.filter(Boolean)
			.join(',')
	);
	const validationError = $derived(validateConfig());
	const canSave = $derived(
		!disabled &&
			!isSyncing &&
			!loading &&
			(tokenDirty || allowedUsersDirty) &&
			!validationError
	);

	$effect(() => {
		onsyncchange(isSyncing);
	});

	function validateConfig(): string | null {
		const botToken = telegramBotToken.trim();
		const allowedUsers = normalizedAllowedUsers;

		if (!botToken && allowedUsers) {
			return 'Add a bot token or clear the Telegram user ID.';
		}
		if (botToken && !/^\d{6,}:[A-Za-z0-9_-]{20,}$/.test(botToken)) {
			return 'Bot token should look like 123456789:ABC...';
		}
		if (botToken && !allowedUsers) {
			return 'Add the Telegram user ID that can chat with this bot.';
		}
		if (allowedUsers && !/^\d+(,\d+)*$/.test(allowedUsers)) {
			return 'Telegram user IDs must be numbers, separated by commas.';
		}
		return null;
	}

	function cleanupTimers() {
		if (syncTimer) {
			clearInterval(syncTimer);
			syncTimer = null;
		}
		if (pollTimer) {
			clearInterval(pollTimer);
			pollTimer = null;
		}
		if (syncTimeout) {
			clearTimeout(syncTimeout);
			syncTimeout = null;
		}
	}

	async function loadConfig() {
		loading = true;
		try {
			const token = await getIdToken();
			if (!token) return;
			const result = await getAgentConfig(token, instanceId);
			const cfg = result.config;

			if (typeof cfg.telegram_bot_token === 'string') {
				telegramBotToken = cfg.telegram_bot_token;
			}
			if (typeof cfg.telegram_allowed_users === 'string') {
				telegramAllowedUsers = cfg.telegram_allowed_users;
			}
			tokenDirty = false;
			allowedUsersDirty = false;
		} catch {
			syncError = 'Could not load Telegram settings';
			syncPhase = 'failed';
		} finally {
			loading = false;
		}
	}

	async function pollSyncStatus(token: string) {
		try {
			const result = await getSyncStatus(token, instanceId);
			if (result.status === 'completed') {
				cleanupTimers();
				syncPhase = 'success';
				setTimeout(() => {
					if (syncPhase === 'success') syncPhase = 'idle';
				}, 6000);
			} else if (result.status === 'failed') {
				cleanupTimers();
				syncError = result.message || 'Telegram settings were saved but did not apply cleanly';
				syncPhase = 'failed';
			}
		} catch {
			// Keep polling while the agent restarts.
		}
	}

	async function handleSave() {
		if (!canSave) return;

		syncPhase = 'saving';
		syncError = null;
		cleanupTimers();

		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');

			await updateAgentConfig(token, instanceId, {
				telegram_bot_token: tokenDirty ? telegramBotToken.trim() : null,
				telegram_allowed_users: allowedUsersDirty ? normalizedAllowedUsers : null
			});

			if (allowedUsersDirty) {
				telegramAllowedUsers = normalizedAllowedUsers;
			}
			tokenDirty = false;
			allowedUsersDirty = false;

			syncPhase = 'syncing';
			syncElapsed = 0;
			syncTimer = setInterval(() => {
				syncElapsed += 1;
			}, 1000);

			try {
				const result = await syncConfig(token, instanceId);
				if (!result.synced) {
					throw new Error(result.error || 'Failed to apply Telegram settings');
				}
			} catch {
				// Saved config will still apply on heartbeat; poll the agent state.
			}

			pollTimer = setInterval(() => {
				pollSyncStatus(token);
			}, 3000);
			syncTimeout = setTimeout(() => {
				if (syncPhase === 'syncing') {
					cleanupTimers();
					syncError = 'Telegram settings were saved and will apply automatically within a few minutes';
					syncPhase = 'failed';
				}
			}, 180_000);
		} catch (err) {
			cleanupTimers();
			syncError = err instanceof Error ? err.message : 'Failed to save Telegram settings';
			syncPhase = 'failed';
		}
	}

	function dismissSync() {
		syncPhase = 'idle';
		syncError = null;
	}

	onMount(() => {
		loadConfig();
		return cleanupTimers;
	});
</script>

<div class="rounded-xl border border-gray-200 dark:border-gray-700 p-5">
	<div class="flex items-center justify-between gap-3">
		<div>
			<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Telegram</h3>
			<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">Link a Telegram bot and restrict access to your Telegram user ID</p>
		</div>
		{#if telegramBotToken && !tokenDirty}
			<span class="rounded-full bg-green-50 px-2 py-1 text-xs font-medium text-green-700 dark:bg-green-900/25 dark:text-green-300">Linked</span>
		{/if}
	</div>

	{#if loading}
		<div class="mt-4 flex items-center justify-center py-4">
			<p class="text-sm text-gray-400 dark:text-gray-500">Loading...</p>
		</div>
	{:else}
		<div class="mt-4 space-y-4">
			{#if syncPhase !== 'idle'}
				<div class="rounded-lg border p-4 {syncPhase === 'success' ? 'border-green-200 bg-green-50 dark:border-green-800 dark:bg-green-900/20' : syncPhase === 'failed' ? 'border-amber-200 bg-amber-50 dark:border-amber-800 dark:bg-amber-900/20' : 'border-gray-200 bg-gray-50 dark:border-gray-700 dark:bg-gray-800'}">
					{#if syncPhase === 'saving' || syncPhase === 'syncing'}
						<div class="flex items-center gap-3">
							<svg class="h-4 w-4 shrink-0 animate-spin text-gray-600 dark:text-gray-400" viewBox="0 0 24 24" fill="none">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
								<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
							</svg>
							<div>
								<p class="text-sm font-medium text-gray-900 dark:text-white">Applying Telegram settings...</p>
								<p class="text-xs text-gray-500 dark:text-gray-400">Restarting Hermes with the new values <span class="tabular-nums">{syncElapsed}s</span></p>
							</div>
						</div>
					{:else if syncPhase === 'success'}
						<div class="flex items-center justify-between gap-3">
							<div>
								<p class="text-sm font-medium text-green-800 dark:text-green-300">Telegram settings applied</p>
								<p class="text-xs text-green-700 dark:text-green-400">Your bot can now receive chats from the allowed user ID.</p>
							</div>
							<button type="button" onclick={dismissSync} class="text-sm text-green-700 hover:text-green-900 dark:text-green-300 dark:hover:text-green-100">Dismiss</button>
						</div>
					{:else}
						<div class="flex items-start justify-between gap-3">
							<div>
								<p class="text-sm font-medium text-amber-800 dark:text-amber-300">{syncError}</p>
								<p class="mt-1 text-xs text-amber-700 dark:text-amber-400">The saved values will retry on the next heartbeat.</p>
							</div>
							<button type="button" onclick={dismissSync} class="text-sm text-amber-700 hover:text-amber-900 dark:text-amber-300 dark:hover:text-amber-100">Dismiss</button>
						</div>
					{/if}
				</div>
			{/if}

			<div>
				<label for="telegram-bot-token" class="text-sm font-medium text-gray-700 dark:text-gray-300">Bot token</label>
				<div class="mt-1.5 flex gap-2">
					<div class="relative flex-1">
						<input
							id="telegram-bot-token"
							type={showToken ? 'text' : 'password'}
							value={telegramBotToken}
							oninput={(e) => { telegramBotToken = e.currentTarget.value; tokenDirty = true; }}
							placeholder="123456789:ABC..."
							disabled={disabled || isSyncing}
							class="block w-full rounded-lg border border-gray-300 px-3 py-2 pr-16 text-sm text-gray-900 focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900 disabled:opacity-50 dark:border-gray-600 dark:bg-gray-800 dark:text-white dark:focus:border-gray-400 dark:focus:ring-gray-400"
						/>
						<button
							type="button"
							onclick={() => (showToken = !showToken)}
							class="absolute right-2 top-1/2 -translate-y-1/2 rounded px-2 py-0.5 text-xs text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300"
						>
							{showToken ? 'Hide' : 'Show'}
						</button>
					</div>
				</div>
			</div>

			<div>
				<label for="telegram-allowed-users" class="text-sm font-medium text-gray-700 dark:text-gray-300">Telegram user ID</label>
				<input
					id="telegram-allowed-users"
					type="text"
					value={telegramAllowedUsers}
					oninput={(e) => { telegramAllowedUsers = e.currentTarget.value; allowedUsersDirty = true; }}
					placeholder="1484592240"
					disabled={disabled || isSyncing}
					class="mt-1.5 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm text-gray-900 focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900 disabled:opacity-50 dark:border-gray-600 dark:bg-gray-800 dark:text-white dark:focus:border-gray-400 dark:focus:ring-gray-400"
				/>
				<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">Use commas to allow more than one Telegram user ID.</p>
			</div>

			{#if validationError}
				<p class="text-sm text-amber-600 dark:text-amber-400">{validationError}</p>
			{/if}

			<div class="flex items-center justify-end gap-3">
				<button
					type="button"
					onclick={handleSave}
					disabled={!canSave}
					class="rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-white dark:text-gray-900 dark:hover:bg-gray-100"
				>
					{syncPhase === 'saving' ? 'Saving...' : syncPhase === 'syncing' ? 'Applying...' : 'Save Telegram'}
				</button>
			</div>
		</div>
	{/if}
</div>
