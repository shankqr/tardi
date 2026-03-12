<script lang="ts">
	import { onMount } from 'svelte';
	import { getIdToken } from '$lib/stores/auth';
	import { getAgentConfig, updateAgentConfig } from '$lib/api/client';

	const DEFAULT_MODEL = 'nvidia/nemotron-3-super-120b-a12b:free';

	interface Props {
		instanceId: string;
		disabled?: boolean;
	}

	let { instanceId, disabled = false }: Props = $props();

	let openrouterKey = $state('');
	let currentModel = $state(DEFAULT_MODEL);
	let currentProvider = $state('openrouter');
	let saving = $state(false);
	let loading = $state(true);
	let saveError = $state<string | null>(null);
	let saveSuccess = $state(false);
	let showKey = $state(false);
	let keyDirty = $state(false);
	let showGuide = $state(false);

	async function loadConfig() {
		loading = true;
		try {
			const token = await getIdToken();
			if (!token) {
				// Auth not ready yet — retry once after a short delay
				await new Promise((r) => setTimeout(r, 1500));
				const retryToken = await getIdToken();
				if (!retryToken) return;
				const result = await getAgentConfig(retryToken, instanceId);
				if (result.config) {
					applyConfig(result.config);
				}
				return;
			}
			const result = await getAgentConfig(token, instanceId);
			if (result.config) {
				applyConfig(result.config);
			}
		} catch {
			// No config yet, use defaults
		} finally {
			loading = false;
		}
	}

	function applyConfig(cfg: Record<string, unknown>) {
		if (cfg.openrouter_api_key && typeof cfg.openrouter_api_key === 'string') {
			openrouterKey = cfg.openrouter_api_key;
		}
		if (cfg.model && typeof cfg.model === 'string') {
			currentModel = cfg.model;
		}
		if (cfg.provider && typeof cfg.provider === 'string') {
			currentProvider = cfg.provider;
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
				provider: currentProvider,
				model: currentModel,
				openrouter_api_key: keyDirty ? openrouterKey : null,
				anthropic_api_key: null,
				openai_api_key: null
			};

			await updateAgentConfig(token, instanceId, config);
			saveSuccess = true;
			keyDirty = false;
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

<div class="rounded-xl border border-gray-200 p-5">
	<h3 class="text-sm font-semibold text-gray-900">AI Provider</h3>
	<p class="mt-1 text-xs text-gray-400">Enter your OpenRouter API key to power your agent</p>

	{#if loading}
		<div class="mt-4 flex items-center justify-center py-4">
			<p class="text-sm text-gray-400">Loading...</p>
		</div>
	{:else}
		<div class="mt-4 space-y-4">
			<!-- OpenRouter API Key -->
			<div>
				<label for="openrouter-key" class="text-sm font-medium text-gray-700">OpenRouter API Key</label>
				<div class="mt-1.5 flex gap-2">
					<div class="relative flex-1">
						<input
							id="openrouter-key"
							type={showKey ? 'text' : 'password'}
							value={openrouterKey}
							oninput={(e) => { openrouterKey = e.currentTarget.value; keyDirty = true; }}
							placeholder="sk-or-v1-..."
							disabled={disabled}
							class="block w-full rounded-lg border border-gray-300 px-3 py-2 pr-16 text-sm text-gray-900 focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900 disabled:opacity-50"
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
				{#if openrouterKey && !keyDirty}
					<p class="mt-1 text-xs text-gray-400">Key is saved. Enter a new key to update it.</p>
				{/if}
			</div>

			<!-- Actions -->
			<div class="flex items-center gap-3">
				<button
					type="button"
					onclick={handleSave}
					disabled={disabled || saving}
					class="rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
				>
					{saving ? 'Saving...' : 'Save'}
				</button>
				<a
					href="https://openrouter.ai/keys"
					target="_blank"
					rel="noopener noreferrer"
					class="inline-flex items-center gap-1.5 rounded-lg border border-gray-300 px-4 py-2.5 text-sm font-medium text-gray-700 hover:bg-gray-50"
				>
					Get API Key
					<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="h-3.5 w-3.5">
						<path fill-rule="evenodd" d="M4.25 5.5a.75.75 0 00-.75.75v8.5c0 .414.336.75.75.75h8.5a.75.75 0 00.75-.75v-4a.75.75 0 011.5 0v4A2.25 2.25 0 0112.75 17h-8.5A2.25 2.25 0 012 14.75v-8.5A2.25 2.25 0 014.25 4h5a.75.75 0 010 1.5h-5z" clip-rule="evenodd" />
						<path fill-rule="evenodd" d="M6.194 12.753a.75.75 0 001.06.053L16.5 4.44v2.81a.75.75 0 001.5 0v-4.5a.75.75 0 00-.75-.75h-4.5a.75.75 0 000 1.5h2.553l-9.056 8.194a.75.75 0 00-.053 1.06z" clip-rule="evenodd" />
					</svg>
				</a>
				<button
					type="button"
					onclick={() => (showGuide = !showGuide)}
					class="inline-flex items-center gap-1.5 rounded-lg border border-gray-300 px-4 py-2.5 text-sm font-medium text-gray-700 hover:bg-gray-50"
				>
					{showGuide ? 'Hide Guide' : 'Guide Me'}
				</button>
				{#if saveSuccess}
					<span class="text-sm text-green-600">Saved</span>
				{/if}
				{#if saveError}
					<span class="text-sm text-red-600">{saveError}</span>
				{/if}
			</div>

			{#if showGuide}
				<div class="rounded-lg border border-blue-200 bg-blue-50 p-4">
					<div class="flex items-start justify-between">
						<h4 class="text-sm font-semibold text-blue-900">How to get your OpenRouter API key</h4>
						<button
							type="button"
							onclick={() => (showGuide = false)}
							aria-label="Close guide"
							class="text-blue-400 hover:text-blue-600"
						>
							<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="h-4 w-4">
								<path d="M6.28 5.22a.75.75 0 00-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 101.06 1.06L10 11.06l3.72 3.72a.75.75 0 101.06-1.06L11.06 10l3.72-3.72a.75.75 0 00-1.06-1.06L10 8.94 6.28 5.22z" />
							</svg>
						</button>
					</div>
					<ol class="mt-3 space-y-2 text-sm text-blue-800">
						<li><span class="font-semibold">1.</span> Go to <a href="https://openrouter.ai" target="_blank" rel="noopener noreferrer" class="underline hover:text-blue-900">openrouter.ai</a> and click <span class="font-semibold">Sign Up</span></li>
						<li><span class="font-semibold">2.</span> Create an account using your email, Google, or GitHub</li>
						<li><span class="font-semibold">3.</span> Once logged in, go to <a href="https://openrouter.ai/keys" target="_blank" rel="noopener noreferrer" class="underline hover:text-blue-900">openrouter.ai/keys</a></li>
						<li><span class="font-semibold">4.</span> Click <span class="font-semibold">Create Key</span> and give it a name (e.g. "Tardi")</li>
						<li><span class="font-semibold">5.</span> Copy the key (starts with <code class="rounded bg-blue-100 px-1 py-0.5 text-xs">sk-or-v1-...</code>) and paste it in the field above</li>
					</ol>
				</div>
			{/if}

			<p class="text-xs text-gray-400">Changes take effect within 5 minutes on your running agent.</p>
		</div>
	{/if}
</div>
