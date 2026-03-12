<script lang="ts">
	import { onMount } from 'svelte';
	import type { AIProvider } from '$lib/types';
	import { getIdToken } from '$lib/stores/auth';
	import { getAgentConfig, updateAgentConfig, syncConfig, getSyncStatus } from '$lib/api/client';

	const PROVIDER_MODELS: Record<AIProvider, { id: string; name: string }[]> = {
		openrouter: [
			{ id: 'nvidia/nemotron-3-super-120b-a12b:free', name: 'Nemotron 3 Super (Free)' },
			{ id: 'anthropic/claude-sonnet-4', name: 'Claude Sonnet 4' },
			{ id: 'anthropic/claude-opus-4', name: 'Claude Opus 4' },
			{ id: 'openai/gpt-4.1', name: 'GPT-4.1' },
			{ id: 'openai/o3-mini', name: 'o3-mini' },
			{ id: 'google/gemini-2.5-pro', name: 'Gemini 2.5 Pro' },
			{ id: 'deepseek/deepseek-r1', name: 'DeepSeek R1' }
		],
		anthropic: [
			{ id: 'claude-sonnet-4-20250514', name: 'Claude Sonnet 4' },
			{ id: 'claude-opus-4-20250514', name: 'Claude Opus 4' }
		],
		openai: [
			{ id: 'gpt-4.1', name: 'GPT-4.1' },
			{ id: 'o3-mini', name: 'o3-mini' },
			{ id: 'gpt-4.1-mini', name: 'GPT-4.1 Mini' }
		]
	};

	const PROVIDERS: { id: AIProvider; label: string }[] = [
		{ id: 'openrouter', label: 'OpenRouter' },
		{ id: 'anthropic', label: 'Anthropic' },
		{ id: 'openai', label: 'OpenAI' }
	];

	interface Props {
		instanceId: string;
		disabled?: boolean;
	}

	let { instanceId, disabled = false }: Props = $props();

	let selectedProvider = $state<AIProvider>('openrouter');
	let openrouterKey = $state('');
	let anthropicKey = $state('');
	let openaiKey = $state('');
	let selectedModel = $state('nvidia/nemotron-3-super-120b-a12b:free');
	let loading = $state(true);
	let showKey = $state(false);

	// Sync progress state
	type SyncPhase = 'idle' | 'saving' | 'syncing' | 'success' | 'failed';
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

	async function pollSyncStatus() {
		try {
			const token = await getIdToken();
			if (!token) return;
			const result = await getSyncStatus(token, instanceId);
			if (result.status === 'completed') {
				stopSyncTimer();
				stopPollTimer();
				syncPhase = 'success';
				setTimeout(() => { if (syncPhase === 'success') syncPhase = 'idle'; }, 8000);
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

	let openrouterKeyDirty = $state(false);
	let anthropicKeyDirty = $state(false);
	let openaiKeyDirty = $state(false);

	const currentKey = $derived(
		selectedProvider === 'openrouter'
			? openrouterKey
			: selectedProvider === 'anthropic'
				? anthropicKey
				: openaiKey
	);

	const currentKeyDirty = $derived(
		selectedProvider === 'openrouter'
			? openrouterKeyDirty
			: selectedProvider === 'anthropic'
				? anthropicKeyDirty
				: openaiKeyDirty
	);

	const models = $derived(PROVIDER_MODELS[selectedProvider]);

	function setCurrentKey(value: string) {
		if (selectedProvider === 'openrouter') {
			openrouterKey = value;
			openrouterKeyDirty = true;
		} else if (selectedProvider === 'anthropic') {
			anthropicKey = value;
			anthropicKeyDirty = true;
		} else {
			openaiKey = value;
			openaiKeyDirty = true;
		}
	}

	function handleProviderChange(provider: AIProvider) {
		selectedProvider = provider;
		selectedModel = PROVIDER_MODELS[provider][0].id;
		showKey = false;
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
				if (cfg.provider && typeof cfg.provider === 'string') {
					selectedProvider = cfg.provider as AIProvider;
				}
				if (cfg.model && typeof cfg.model === 'string') {
					selectedModel = cfg.model;
				}
				if (cfg.openrouter_api_key && typeof cfg.openrouter_api_key === 'string') {
					openrouterKey = cfg.openrouter_api_key;
				}
				if (cfg.anthropic_api_key && typeof cfg.anthropic_api_key === 'string') {
					anthropicKey = cfg.anthropic_api_key;
				}
				if (cfg.openai_api_key && typeof cfg.openai_api_key === 'string') {
					openaiKey = cfg.openai_api_key;
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
				provider: selectedProvider,
				model: selectedModel,
				openrouter_api_key: openrouterKeyDirty ? openrouterKey : null,
				anthropic_api_key: anthropicKeyDirty ? anthropicKey : null,
				openai_api_key: openaiKeyDirty ? openaiKey : null
			};

			await updateAgentConfig(token, instanceId, config);
			openrouterKeyDirty = false;
			anthropicKeyDirty = false;
			openaiKeyDirty = false;

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

		<!-- Provider selector -->
		<div>
			<span class="text-xs font-medium text-gray-500">Provider</span>
			<div class="mt-1.5 flex gap-1">
				{#each PROVIDERS as provider (provider.id)}
					<button
						type="button"
						disabled={disabled || syncPhase === 'saving' || syncPhase === 'syncing'}
						onclick={() => handleProviderChange(provider.id)}
						class="flex-1 rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-50
							{selectedProvider === provider.id
							? 'border-gray-900 bg-gray-900 text-white'
							: 'border-gray-300 text-gray-600 hover:bg-gray-50'}"
					>
						{provider.label}
					</button>
				{/each}
			</div>
		</div>

		<!-- Model selector -->
		<div>
			<label for="adv-model-select" class="text-xs font-medium text-gray-500">Model</label>
			<select
				id="adv-model-select"
				bind:value={selectedModel}
				disabled={disabled || syncPhase === 'saving' || syncPhase === 'syncing'}
				class="mt-1.5 block w-full rounded-lg border border-gray-300 px-3 py-1.5 text-sm text-gray-900 focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900 disabled:opacity-50"
			>
				{#each models as model (model.id)}
					<option value={model.id}>{model.name}</option>
				{/each}
			</select>
		</div>

		<!-- API Key for selected provider -->
		<div>
			<label for="adv-api-key" class="text-xs font-medium text-gray-500">
				{PROVIDERS.find((p) => p.id === selectedProvider)?.label} API Key
			</label>
			<div class="mt-1.5 flex gap-2">
				<div class="relative flex-1">
					<input
						id="adv-api-key"
						type={showKey ? 'text' : 'password'}
						value={currentKey}
						oninput={(e) => setCurrentKey(e.currentTarget.value)}
						placeholder="Enter API key"
						disabled={disabled || syncPhase === 'saving' || syncPhase === 'syncing'}
						class="block w-full rounded-lg border border-gray-300 px-3 py-1.5 pr-16 text-sm text-gray-900 focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900 disabled:opacity-50"
					/>
					<button
						type="button"
						onclick={() => (showKey = !showKey)}
						class="absolute right-2 top-1/2 -translate-y-1/2 rounded px-2 py-0.5 text-xs text-gray-400 hover:text-gray-600"
					>
						{showKey ? 'Hide' : 'Show'}
					</button>
				</div>
			</div>
			{#if currentKey && !currentKeyDirty}
				<p class="mt-1 text-xs text-gray-400">Key is saved. Enter a new key to update.</p>
			{/if}
		</div>

		<!-- Save -->
		<div class="flex items-center gap-3">
			<button
				type="button"
				onclick={handleSave}
				disabled={disabled || syncPhase === 'saving' || syncPhase === 'syncing'}
				class="rounded-lg bg-gray-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-gray-800 disabled:opacity-50"
			>
				{syncPhase === 'saving' || syncPhase === 'syncing' ? 'Applying...' : 'Save'}
			</button>
		</div>
	</div>
{/if}
