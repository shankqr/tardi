<script lang="ts">
	import { onMount } from 'svelte';
	import type { AIProvider } from '$lib/types';
	import { getIdToken } from '$lib/stores/auth';
	import { getAgentConfig, updateAgentConfig } from '$lib/api/client';

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
	let saving = $state(false);
	let loading = $state(true);
	let saveError = $state<string | null>(null);
	let saveSuccess = $state(false);
	let showKey = $state(false);

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
		saving = true;
		saveError = null;
		saveSuccess = false;
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
			saveSuccess = true;
			openrouterKeyDirty = false;
			anthropicKeyDirty = false;
			openaiKeyDirty = false;
			setTimeout(() => (saveSuccess = false), 3000);
		} catch (err) {
			saveError = err instanceof Error ? err.message : 'Failed to save';
		} finally {
			saving = false;
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
		<!-- Provider selector -->
		<div>
			<span class="text-xs font-medium text-gray-500">Provider</span>
			<div class="mt-1.5 flex gap-1">
				{#each PROVIDERS as provider (provider.id)}
					<button
						type="button"
						disabled={disabled}
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
				disabled={disabled}
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
						disabled={disabled}
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
				disabled={disabled || saving}
				class="rounded-lg bg-gray-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-gray-800 disabled:opacity-50"
			>
				{saving ? 'Saving...' : 'Save'}
			</button>
			{#if saveSuccess}
				<span class="text-xs text-green-600">Saved</span>
			{/if}
			{#if saveError}
				<span class="text-xs text-red-600">{saveError}</span>
			{/if}
		</div>
	</div>
{/if}
