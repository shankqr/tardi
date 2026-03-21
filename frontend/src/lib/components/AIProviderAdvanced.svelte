<script lang="ts">
	import { onMount } from 'svelte';
	import { getIdToken } from '$lib/stores/auth';
	import { getAgentConfig, updateAgentConfig, syncConfig, getSyncStatus } from '$lib/api/client';

	const MODELS: { id: string; name: string }[] = [
		{ id: 'nvidia/nemotron-3-super-120b-a12b:free', name: 'Nemotron 3 Super (Free)' },
		{ id: 'moonshotai/kimi-k2.5', name: 'Kimi K2.5' },
		{ id: 'xiaomi/mimo-v2-pro', name: 'MiMo V2 Pro' },
		{ id: 'anthropic/claude-sonnet-4.6', name: 'Claude Sonnet 4.6' }
	];

	interface Props {
		instanceId: string;
		disabled?: boolean;
		onsyncchange?: (syncing: boolean) => void;
	}

	let { instanceId, disabled = false, onsyncchange }: Props = $props();

	let selectedModel = $state('nvidia/nemotron-3-super-120b-a12b:free');
	let customModel = $state<{ id: string; name: string } | null>(null);
	let loading = $state(true);

	const displayModels = $derived(
		customModel ? [customModel, ...MODELS] : MODELS
	);

	// Sync progress state
	type SyncPhase = 'idle' | 'saving' | 'syncing' | 'finishing' | 'success' | 'failed';
	let syncPhase = $state<SyncPhase>('idle');
	let syncError = $state<string | null>(null);
	let syncElapsed = $state(0);
	let syncTimer: ReturnType<typeof setInterval> | null = null;
	let pollTimer: ReturnType<typeof setInterval> | null = null;

	function startSyncTimer() {
		syncElapsed = 0;
		syncTimer = setInterval(() => { syncElapsed += 1; }, 1000);
	}
	function stopSyncTimer() {
		if (syncTimer) { clearInterval(syncTimer); syncTimer = null; }
	}
	function stopPollTimer() {
		if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
	}

	$effect(() => {
		const isSyncing = syncPhase === 'saving' || syncPhase === 'syncing' || syncPhase === 'finishing';
		onsyncchange?.(isSyncing);
	});

	async function pollSyncStatus() {
		try {
			const token = await getIdToken();
			if (!token) return;
			const result = await getSyncStatus(token, instanceId);
			if (result.status === 'completed') {
				stopSyncTimer();
				stopPollTimer();
				syncPhase = 'finishing';
				setTimeout(() => {
					syncPhase = 'success';
					setTimeout(() => { if (syncPhase === 'success') syncPhase = 'idle'; }, 8000);
				}, 15000);
			} else if (result.status === 'failed') {
				stopSyncTimer();
				stopPollTimer();
				syncError = result.message || 'Config sync failed on your agent';
				syncPhase = 'failed';
			}
		} catch {
			// Ignore poll errors, keep trying
		}
	}

	function dismissSync() {
		syncPhase = 'idle';
		syncError = null;
		stopSyncTimer();
		stopPollTimer();
	}

	async function loadConfig() {
		loading = true;
		try {
			let token = await getIdToken();
			if (!token) {
				await new Promise((r) => setTimeout(r, 1500));
				token = await getIdToken();
				if (!token) return;
			}
			const result = await getAgentConfig(token, instanceId);
			if (result.config) {
				const cfg = result.config;
				if (cfg.model && typeof cfg.model === 'string') {
					selectedModel = cfg.model;
					if (!MODELS.some((m) => m.id === cfg.model)) {
						customModel = { id: cfg.model as string, name: `${cfg.model} (current)` };
					}
				}
			}
		} catch {
			// No config yet
		} finally {
			loading = false;
		}
	}

	async function handleSave() {
		syncPhase = 'saving';
		syncError = null;
		stopSyncTimer();
		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');

			const config: Record<string, unknown> = {
				provider: 'openrouter',
				model: selectedModel
			};

			await updateAgentConfig(token, instanceId, config);

			// Trigger instant sync
			syncPhase = 'syncing';
			startSyncTimer();
			try {
				const result = await syncConfig(token, instanceId);
				if (result.synced) {
					stopPollTimer();
					pollTimer = setInterval(() => {
						if (syncElapsed > 120) {
							stopSyncTimer();
							stopPollTimer();
							syncError = 'Sync is taking longer than expected — it will apply automatically within 5 minutes';
							syncPhase = 'failed';
							return;
						}
						pollSyncStatus();
					}, 5000);
				} else {
					stopSyncTimer();
					syncError = result.error || 'Failed to apply configuration';
					syncPhase = 'failed';
				}
			} catch {
				stopSyncTimer();
				syncError = 'Could not reach your agent — changes will apply within 5 minutes';
				syncPhase = 'failed';
			}
		} catch (err) {
			stopSyncTimer();
			syncError = err instanceof Error ? err.message : 'Failed to save';
			syncPhase = 'failed';
		}
	}

	onMount(() => {
		loadConfig();
	});
</script>

{#if loading}
	<p class="text-sm text-gray-400">Loading...</p>
{:else}
	<div class="space-y-4">
		<!-- Sync progress overlay -->
		{#if syncPhase !== 'idle'}
			<div class="rounded-lg border p-4 {syncPhase === 'success' ? 'border-green-200 bg-green-50' : syncPhase === 'failed' ? 'border-red-200 bg-red-50' : 'border-gray-200 bg-gray-50'}">
				{#if syncPhase === 'saving'}
					<div class="flex items-center gap-3">
						<svg class="h-4 w-4 animate-spin text-gray-600 shrink-0" viewBox="0 0 24 24" fill="none">
							<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
							<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
						</svg>
						<p class="text-sm font-medium text-gray-900">Saving configuration...</p>
					</div>
				{:else if syncPhase === 'syncing'}
					<div class="space-y-3">
						<div class="flex items-center gap-3">
							<svg class="h-4 w-4 animate-spin text-gray-600 shrink-0" viewBox="0 0 24 24" fill="none">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
								<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
							</svg>
							<div class="flex-1">
								<p class="text-sm font-medium text-gray-900">Applying to your agent...</p>
								<p class="text-xs text-gray-500">
									{#if syncElapsed < 10}
										Connecting to your agent
									{:else if syncElapsed < 30}
										Updating configuration
									{:else if syncElapsed < 60}
										Restarting with new settings
									{:else}
										Waiting for health check
									{/if}
									<span class="ml-1 tabular-nums text-gray-400">{syncElapsed}s</span>
								</p>
							</div>
						</div>
						<div class="h-1 overflow-hidden rounded-full bg-gray-200">
							<div
								class="h-full rounded-full bg-gray-600 transition-all duration-1000 ease-linear"
								style="width: {Math.min(syncElapsed / 80 * 100, 95)}%"
							></div>
						</div>
					</div>
				{:else if syncPhase === 'finishing'}
					<div class="space-y-3">
						<div class="flex items-center gap-3">
							<svg class="h-4 w-4 animate-spin text-gray-600 shrink-0" viewBox="0 0 24 24" fill="none">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
								<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
							</svg>
							<div class="flex-1">
								<p class="text-sm font-medium text-gray-900">Finalizing configuration...</p>
								<p class="text-xs text-gray-500">Almost ready</p>
							</div>
						</div>
						<div class="h-1 overflow-hidden rounded-full bg-gray-200">
							<div
								class="h-full rounded-full bg-gray-600 transition-all duration-1000 ease-linear"
								style="width: 95%"
							></div>
						</div>
						<p class="text-xs text-gray-400">Please don't close this page.</p>
					</div>
				{:else if syncPhase === 'success'}
					<div class="flex items-center justify-between">
						<div class="flex items-center gap-3">
							<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="h-5 w-5 text-green-600 shrink-0">
								<path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.857-9.809a.75.75 0 00-1.214-.882l-3.483 4.79-1.88-1.88a.75.75 0 10-1.06 1.061l2.5 2.5a.75.75 0 001.137-.089l4-5.5z" clip-rule="evenodd" />
							</svg>
							<div>
								<p class="text-sm font-medium text-green-800">Configuration applied successfully</p>
								<p class="text-xs text-green-600">Your agent is now running with the new settings</p>
							</div>
						</div>
						<button onclick={dismissSync} class="text-green-500 hover:text-green-700" aria-label="Dismiss">
							<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="h-4 w-4">
								<path d="M6.28 5.22a.75.75 0 00-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 101.06 1.06L10 11.06l3.72 3.72a.75.75 0 101.06-1.06L11.06 10l3.72-3.72a.75.75 0 00-1.06-1.06L10 8.94 6.28 5.22z" />
							</svg>
						</button>
					</div>
				{:else if syncPhase === 'failed'}
					<div class="flex items-start justify-between">
						<div class="flex items-start gap-3">
							<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="h-5 w-5 text-red-500 shrink-0 mt-0.5">
								<path fill-rule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-8-5a.75.75 0 01.75.75v4.5a.75.75 0 01-1.5 0v-4.5A.75.75 0 0110 5zm0 10a1 1 0 100-2 1 1 0 000 2z" clip-rule="evenodd" />
							</svg>
							<div>
								<p class="text-sm font-medium text-red-800">{syncError}</p>
								<p class="text-xs text-red-600 mt-1">Your settings were saved. You can retry, or they will apply automatically within 5 minutes.</p>
								<div class="mt-2 flex gap-2">
									<button
										onclick={handleSave}
										class="rounded-md bg-red-600 px-3 py-1 text-xs font-medium text-white hover:bg-red-700"
									>
										Retry
									</button>
									<button
										onclick={dismissSync}
										class="rounded-md border border-red-300 px-3 py-1 text-xs text-red-700 hover:bg-red-100"
									>
										Dismiss
									</button>
								</div>
							</div>
						</div>
					</div>
				{/if}
			</div>
		{/if}

		<!-- Model selector -->
		<div>
			<label for="adv-model-select" class="text-xs font-medium text-gray-500">Model</label>
			<select
				id="adv-model-select"
				bind:value={selectedModel}
				onchange={() => { if (MODELS.some((m) => m.id === selectedModel)) customModel = null; }}
				disabled={disabled || syncPhase === 'saving' || syncPhase === 'syncing' || syncPhase === 'finishing'}
				class="mt-1.5 block w-full rounded-lg border border-gray-300 px-3 py-1.5 text-sm text-gray-900 focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900 disabled:opacity-50"
			>
				{#each displayModels as model (model.id)}
					<option value={model.id}>{model.name}</option>
				{/each}
			</select>
		</div>

		<!-- Save -->
		<div class="flex items-center gap-3">
			<button
				type="button"
				onclick={handleSave}
				disabled={disabled || syncPhase === 'saving' || syncPhase === 'syncing' || syncPhase === 'finishing'}
				class="rounded-lg bg-gray-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-gray-800 disabled:opacity-50"
			>
				{syncPhase === 'saving' || syncPhase === 'syncing' || syncPhase === 'finishing' ? 'Applying...' : 'Save'}
			</button>
		</div>
	</div>
{/if}
