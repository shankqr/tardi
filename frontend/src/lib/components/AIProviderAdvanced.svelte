<script lang="ts">
	import { onMount } from 'svelte';
	import { getIdToken } from '$lib/stores/auth';
	import { getAgentConfig, updateAgentConfig, syncConfig, getSyncStatus, getModels } from '$lib/api/client';
	import type { ModelInfo } from '$lib/types';

	interface Props {
		instanceId: string;
		disabled?: boolean;
		onsyncchange?: (syncing: boolean) => void;
	}

	let { instanceId, disabled = false, onsyncchange }: Props = $props();

	let models = $state<ModelInfo[]>([]);
	let defaultModelId = $state('');
	let selectedModel = $state('');
	let customModel = $state<{ id: string; name: string } | null>(null);
	let loading = $state(true);
	let modelsError = $state(false);

	const selectedModelInfo = $derived(models.find((m) => m.id === selectedModel));

	function formatPrice(perToken: string | undefined): string {
		if (!perToken || perToken === '0') return 'Free';
		const perMillion = parseFloat(perToken) * 1_000_000;
		if (perMillion < 0.01) return '<$0.01 / 1M tokens';
		return `$${perMillion.toFixed(2)} / 1M tokens`;
	}

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

	async function loadData() {
		loading = true;
		try {
			// Load models catalog and agent config in parallel
			const [modelsResult, configResult] = await Promise.all([
				getModels().catch(() => {
					modelsError = true;
					return null;
				}),
				(async () => {
					let token = await getIdToken();
					if (!token) {
						await new Promise((r) => setTimeout(r, 1500));
						token = await getIdToken();
						if (!token) return null;
					}
					return getAgentConfig(token, instanceId).catch(() => null);
				})()
			]);

			if (modelsResult) {
				models = modelsResult.models;
				defaultModelId = modelsResult.default_model_id;
			}

			// Apply saved config
			if (configResult?.config) {
				const cfg = configResult.config;
				if (cfg.model && typeof cfg.model === 'string') {
					selectedModel = cfg.model;
					// If saved model is not in the catalog, show it as custom
					if (!models.some((m) => m.id === cfg.model)) {
						customModel = { id: cfg.model as string, name: `${cfg.model} (current)` };
					}
				}
			}

			// If no model was saved, use the default from catalog
			if (!selectedModel && defaultModelId) {
				selectedModel = defaultModelId;
			}
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
		loadData();
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
			{#if modelsError}
				<p class="mt-1.5 text-xs text-red-500">Could not load models. Try refreshing the page.</p>
			{:else}
				<select
					id="adv-model-select"
					bind:value={selectedModel}
					onchange={() => { if (models.some((m) => m.id === selectedModel)) customModel = null; }}
					disabled={disabled || syncPhase === 'saving' || syncPhase === 'syncing' || syncPhase === 'finishing'}
					class="mt-1.5 block w-full rounded-lg border border-gray-300 px-3 py-1.5 text-sm text-gray-900 focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900 disabled:opacity-50"
				>
					{#each models as model (model.id)}
						<option value={model.id}>
							{model.display_name} — {model.tier === 'free' ? 'Free' : 'Paid'}{model.is_default ? ' (Default)' : ''}
						</option>
					{/each}
					{#if customModel}
						<option value={customModel.id}>{customModel.name}</option>
					{/if}
				</select>
			{/if}
		</div>

		<!-- Model info -->
		{#if selectedModelInfo}
			<div class="space-y-2">
				{#if selectedModelInfo.description}
					<p class="text-xs text-gray-500">{selectedModelInfo.description}</p>
				{/if}
				<div class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-400">
					{#if selectedModelInfo.context_length}
						<span>Context: {(selectedModelInfo.context_length / 1000).toFixed(0)}K tokens</span>
					{/if}
					<span>Input: {formatPrice(selectedModelInfo.prompt_price)}</span>
					<span>Output: {formatPrice(selectedModelInfo.completion_price)}</span>
				</div>
			</div>
		{/if}

		<!-- Paid model top-up notice -->
		{#if selectedModelInfo && selectedModelInfo.tier === 'paid'}
			<div class="rounded-lg border border-blue-200 bg-blue-50 p-3">
				<div class="flex items-start gap-2.5">
					<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="h-4 w-4 text-blue-500 shrink-0 mt-0.5">
						<path fill-rule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a.75.75 0 000 1.5h.253a.25.25 0 01.244.304l-.459 2.066A1.75 1.75 0 0010.747 15H11a.75.75 0 000-1.5h-.253a.25.25 0 01-.244-.304l.459-2.066A1.75 1.75 0 009.253 9H9z" clip-rule="evenodd" />
					</svg>
					<div>
						<p class="text-sm font-medium text-blue-800">This model requires OpenRouter credits</p>
						<p class="mt-0.5 text-xs text-blue-600">
							Make sure your OpenRouter account has sufficient credits.
							<a
								href="https://openrouter.ai/credits"
								target="_blank"
								rel="noopener noreferrer"
								class="inline-flex items-center gap-0.5 font-medium underline hover:text-blue-800"
							>
								Top up your account
								<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" class="h-3 w-3">
									<path fill-rule="evenodd" d="M4.22 11.78a.75.75 0 010-1.06L9.44 5.5H5.75a.75.75 0 010-1.5h5.5a.75.75 0 01.75.75v5.5a.75.75 0 01-1.5 0V6.56l-5.22 5.22a.75.75 0 01-1.06 0z" clip-rule="evenodd" />
								</svg>
							</a>
						</p>
					</div>
				</div>
			</div>
		{/if}

		<!-- Save -->
		<div class="flex items-center gap-3">
			<button
				type="button"
				onclick={handleSave}
				disabled={disabled || modelsError || syncPhase === 'saving' || syncPhase === 'syncing' || syncPhase === 'finishing'}
				class="rounded-lg bg-gray-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-gray-800 disabled:opacity-50"
			>
				{syncPhase === 'saving' || syncPhase === 'syncing' || syncPhase === 'finishing' ? 'Applying...' : 'Save'}
			</button>
		</div>
	</div>
{/if}
