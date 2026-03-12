<script lang="ts">
	import { page } from '$app/state';
	import { dashboardState, dashboardLoading, refreshDashboard } from '$lib/stores/dashboard';
	import { getIdToken } from '$lib/stores/auth';
	import {
		restartInstance,
		resetPassword,
		renameInstance,
		createSnapshot,
		restoreSnapshot,
		deleteSnapshot,
		getWhatsAppQR,
		getWhatsAppStatus,
		getAgentConfig
	} from '$lib/api/client';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import ProvisioningProgress from '$lib/components/ProvisioningProgress.svelte';
	import AIProviderConfig from '$lib/components/AIProviderConfig.svelte';
	import AIProviderAdvanced from '$lib/components/AIProviderAdvanced.svelte';

	const instance = $derived(
		$dashboardState?.instances.find((i) => i.id === page.params.id) ?? null
	);

	const isProvisioning = $derived(
		instance?.status === 'provisioning' ||
		instance?.status === 'bootstrapping' ||
		instance?.status === 'installing_agent'
	);

	const instanceSnapshots = $derived(
		$dashboardState?.snapshots.filter((s) => s.instance_id === instance?.id) ?? []
	);

	// Action state
	let restarting = $state(false);
	let resettingPassword = $state(false);
	let actionError = $state<string | null>(null);
	let showPassword = $state(false);

	// Rename state
	let editing = $state(false);
	let editName = $state('');
	let saving = $state(false);

	// Snapshot state
	let powerUserOpen = $state(false);
	let showSnapshotForm = $state(false);
	let snapshotName = $state('');
	let creatingSnapshot = $state(false);
	let restoringSnapshotId = $state<string | null>(null);
	let deletingSnapshotId = $state<string | null>(null);
	let confirmRestoreId = $state<string | null>(null);

	// WhatsApp state
	let whatsappQR = $state<string | null>(null);
	let whatsappLoading = $state(false);
	let whatsappError = $state<string | null>(null);
	let whatsappLinked = $state(false);
	let whatsappPhone = $state('');
	let whatsappPolling = $state(false);
	let whatsappPollTimer = $state<ReturnType<typeof setInterval> | null>(null);

	// Busy state — disables all actions during non-active statuses
	const isBusy = $derived(instance != null && instance.status !== 'active');

	// Status transition tracking for result notifications
	let previousStatus = $state<string | null>(null);
	let restoreResult = $state<'success' | 'failed' | null>(null);
	let snapshotResult = $state<'success' | 'failed' | null>(null);

	$effect(() => {
		if (!instance) return;
		const current = instance.status;
		if (previousStatus === 'restoring' && current === 'active') restoreResult = 'success';
		if (previousStatus === 'restoring' && current === 'error') restoreResult = 'failed';
		if (previousStatus === 'snapshotting' && current === 'active') snapshotResult = 'success';
		if (previousStatus === 'snapshotting' && current === 'error') snapshotResult = 'failed';
		previousStatus = current;
	});

	function startEditing() {
		if (!instance) return;
		editName = instance.name;
		editing = true;
	}

	async function handleRename() {
		if (!instance || !editName.trim() || editName.trim() === instance.name) {
			editing = false;
			return;
		}
		saving = true;
		actionError = null;
		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');
			await renameInstance(token, instance.id, editName.trim());
			await refreshDashboard();
			editing = false;
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Rename failed';
		} finally {
			saving = false;
		}
	}

	async function handleRestart() {
		if (!instance) return;
		restarting = true;
		actionError = null;
		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');
			await restartInstance(token, instance.id);
			await refreshDashboard();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Restart failed';
		} finally {
			restarting = false;
		}
	}

	async function handleResetPassword() {
		if (!instance) return;
		resettingPassword = true;
		actionError = null;
		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');
			await resetPassword(token, instance.id);
			await refreshDashboard();
			showPassword = true;
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Password reset failed';
		} finally {
			resettingPassword = false;
		}
	}

	async function handleCreateSnapshot() {
		if (!instance || !snapshotName.trim()) return;
		creatingSnapshot = true;
		actionError = null;
		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');
			await createSnapshot(token, instance.id, snapshotName.trim());
			await refreshDashboard();
			snapshotName = '';
			showSnapshotForm = false;
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Snapshot creation failed';
		} finally {
			creatingSnapshot = false;
		}
	}

	async function handleRestoreSnapshot(snapshotId: string) {
		if (!instance) return;
		confirmRestoreId = null;
		restoringSnapshotId = snapshotId;
		actionError = null;
		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');
			await restoreSnapshot(token, snapshotId);
			await refreshDashboard();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Restore failed';
		} finally {
			restoringSnapshotId = null;
		}
	}

	async function handleDeleteSnapshot(snapshotId: string) {
		deletingSnapshotId = snapshotId;
		actionError = null;
		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');
			await deleteSnapshot(token, snapshotId);
			await refreshDashboard();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Delete snapshot failed';
		} finally {
			deletingSnapshotId = null;
		}
	}

	let showApiKeyRequired = $state(false);
	let showApiKeyGuide = $state(false);

	async function handleWhatsAppConnect() {
		if (!instance) return;
		whatsappLoading = true;
		whatsappError = null;
		whatsappQR = null;
		showApiKeyRequired = false;
		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');

			// Check if an API key is configured before allowing WhatsApp connect
			const agentCfg = await getAgentConfig(token, instance.id);
			const cfg = agentCfg.config;
			const hasKey =
				(cfg.openrouter_api_key && typeof cfg.openrouter_api_key === 'string' && cfg.openrouter_api_key.length > 0) ||
				(cfg.anthropic_api_key && typeof cfg.anthropic_api_key === 'string' && cfg.anthropic_api_key.length > 0) ||
				(cfg.openai_api_key && typeof cfg.openai_api_key === 'string' && cfg.openai_api_key.length > 0);

			if (!hasKey) {
				showApiKeyRequired = true;
				whatsappLoading = false;
				return;
			}

			const result = await getWhatsAppQR(token, instance.id);
			if (result.qr_data_url) {
				whatsappQR = result.qr_data_url;
				startWhatsAppPolling();
			} else {
				whatsappError = result.message || 'No QR code returned';
			}
		} catch (err) {
			whatsappError = err instanceof Error ? err.message : 'Failed to get QR code';
		} finally {
			whatsappLoading = false;
		}
	}

	function startWhatsAppPolling() {
		stopWhatsAppPolling();
		whatsappPolling = true;
		whatsappPollTimer = setInterval(async () => {
			if (!instance) return;
			try {
				const token = await getIdToken();
				if (!token) return;
				const status = await getWhatsAppStatus(token, instance.id);
				if (status.linked) {
					whatsappLinked = true;
					whatsappPhone = status.phone;
					whatsappQR = null;
					stopWhatsAppPolling();
				}
			} catch {
				// ignore polling errors
			}
		}, 5000);
		// Stop polling after 2 minutes
		setTimeout(() => stopWhatsAppPolling(), 120000);
	}

	function stopWhatsAppPolling() {
		if (whatsappPollTimer) {
			clearInterval(whatsappPollTimer);
			whatsappPollTimer = null;
		}
		whatsappPolling = false;
	}

	let whatsappChecking = $state(false);

	async function refreshWhatsAppStatus() {
		if (!instance) return;
		whatsappChecking = true;
		try {
			const token = await getIdToken();
			if (!token) return;
			const status = await getWhatsAppStatus(token, instance.id);
			whatsappLinked = status.linked;
			whatsappPhone = status.phone;
		} catch {
			// ignore
		} finally {
			whatsappChecking = false;
		}
	}

	// Check WhatsApp status on load
	$effect(() => {
		if (instance?.status === 'active' && instance.openclaw_auth_token && instance.ipv4) {
			refreshWhatsAppStatus();
		}
		return () => stopWhatsAppPolling();
	});

	function timeAgo(dateStr: string | null): string {
		if (!dateStr) return 'Never';
		const seconds = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
		if (seconds < 60) return `${seconds}s ago`;
		if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
		if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
		return `${Math.floor(seconds / 86400)}d ago`;
	}
</script>

{#if $dashboardLoading || !$dashboardState}
	<div class="flex items-center justify-center py-20">
		<p class="text-gray-400">Loading...</p>
	</div>
{:else if !instance}
	<div class="py-10 text-center">
		<p class="text-gray-500">Agent not found</p>
		<a href="/dashboard" class="mt-2 inline-block text-sm text-gray-900 hover:underline">Back to dashboard</a>
	</div>
{:else}
	<div>
		<a href="/dashboard" class="text-sm text-gray-500 hover:text-gray-700">&larr; Back to dashboard</a>

		<div class="mt-4 flex items-start justify-between">
			<div>
				{#if editing}
					<form onsubmit={(e) => { e.preventDefault(); handleRename(); }} class="flex items-center gap-2">
						<input
							type="text"
							bind:value={editName}
							class="rounded-md border border-gray-300 px-2 py-1 text-xl font-bold text-gray-900 focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900"
							autofocus
							onkeydown={(e) => { if (e.key === 'Escape') editing = false; }}
						/>
						<button
							type="submit"
							disabled={saving}
							class="rounded-md bg-gray-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-gray-800 disabled:opacity-50"
						>
							{saving ? 'Saving...' : 'Save'}
						</button>
						<button
							type="button"
							onclick={() => (editing = false)}
							class="rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-600 hover:bg-gray-50"
						>
							Cancel
						</button>
					</form>
				{:else}
					<div class="flex items-center gap-2">
						<h2 class="text-xl font-bold text-gray-900">{instance.name}</h2>
						<button
							onclick={startEditing}
							class="rounded-md p-1 text-gray-400 hover:text-gray-600"
							title="Rename agent"
						>
							<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="h-4 w-4">
								<path d="M2.695 14.763l-1.262 3.154a.5.5 0 00.65.65l3.155-1.262a4 4 0 001.343-.885L17.5 5.5a2.121 2.121 0 00-3-3L3.58 13.42a4 4 0 00-.885 1.343z" />
							</svg>
						</button>
					</div>
				{/if}
				<p class="mt-0.5 text-sm text-gray-500">Dedicated Agent</p>
			</div>
			<StatusBadge status={instance.status} />
		</div>

		{#if $dashboardState?.subscription?.cancel_at_period_end}
			<div class="mt-4 rounded-lg border border-orange-200 bg-orange-50 p-3 text-sm text-orange-700">
				Your subscription is canceling. This agent and all snapshots will be deleted on
				{new Date($dashboardState.subscription.current_period_end).toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' })}.
				<a href="/dashboard/billing" class="font-medium underline">Manage billing</a> to undo.
			</div>
		{/if}

		{#if actionError}
			<div class="mt-4 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700">
				{actionError}
			</div>
		{/if}

		{#if restoreResult === 'success'}
			<div class="mt-4 flex items-center justify-between rounded-lg border border-green-200 bg-green-50 p-3 text-sm text-green-700">
				<span>Snapshot restored successfully. A new root password has been generated.</span>
				<button onclick={() => (restoreResult = null)} class="ml-3 text-green-500 hover:text-green-700">&times;</button>
			</div>
		{:else if restoreResult === 'failed'}
			<div class="mt-4 flex items-center justify-between rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700">
				<span>Snapshot restore failed. Your agent may need attention.</span>
				<button onclick={() => (restoreResult = null)} class="ml-3 text-red-500 hover:text-red-700">&times;</button>
			</div>
		{/if}

		{#if snapshotResult === 'success'}
			<div class="mt-4 flex items-center justify-between rounded-lg border border-green-200 bg-green-50 p-3 text-sm text-green-700">
				<span>Snapshot created successfully.</span>
				<button onclick={() => (snapshotResult = null)} class="ml-3 text-green-500 hover:text-green-700">&times;</button>
			</div>
		{:else if snapshotResult === 'failed'}
			<div class="mt-4 flex items-center justify-between rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700">
				<span>Snapshot creation failed.</span>
				<button onclick={() => (snapshotResult = null)} class="ml-3 text-red-500 hover:text-red-700">&times;</button>
			</div>
		{/if}

		<div class="mt-8 grid grid-cols-1 lg:grid-cols-2 gap-8">
			<!-- Left column -->
			<div class="space-y-6">
				<div class="rounded-xl border border-gray-200 p-5">
					<h3 class="text-sm font-semibold text-gray-900">Agent Details</h3>
					<dl class="mt-4 space-y-3 text-sm">
						<div class="flex justify-between">
							<dt class="text-gray-500">IP Address</dt>
							<dd class="font-mono text-gray-900">{instance.ipv4 ?? '—'}</dd>
						</div>
						<div class="flex justify-between">
							<dt class="text-gray-500">OpenClaw</dt>
							<dd>
								{#if instance.agent_status === 'running'}
									<span class="inline-flex items-center gap-1.5 text-green-700">
										<span class="h-1.5 w-1.5 rounded-full bg-green-500"></span>
										Running
									</span>
								{:else if instance.agent_status === 'unhealthy'}
									<span class="inline-flex items-center gap-1.5 text-yellow-700">
										<span class="h-1.5 w-1.5 rounded-full bg-yellow-500"></span>
										Unhealthy
									</span>
								{:else if instance.agent_status === 'stopped' || instance.agent_status === 'not_found'}
									<span class="inline-flex items-center gap-1.5 text-red-700">
										<span class="h-1.5 w-1.5 rounded-full bg-red-500"></span>
										Stopped
									</span>
								{:else}
									<span class="text-gray-400">—</span>
								{/if}
							</dd>
						</div>
						<div class="flex justify-between">
							<dt class="text-gray-500">Last Heartbeat</dt>
							<dd class="text-gray-900">{timeAgo(instance.last_heartbeat_at)}</dd>
						</div>
						<div class="flex justify-between">
							<dt class="text-gray-500">Created</dt>
							<dd class="text-gray-900">{new Date(instance.created_at).toLocaleDateString()}</dd>
						</div>
					</dl>
				</div>

				{#if instance.status === 'active' || instance.status === 'restarting' || instance.status === 'snapshotting' || instance.status === 'restoring'}
					<AIProviderConfig instanceId={instance.id} disabled={instance.status !== 'active'} />

					{#if instance.openclaw_auth_token && instance.ipv4}
						<div class="rounded-xl border border-gray-200 p-5">
							<h3 class="text-sm font-semibold text-gray-900">WhatsApp</h3>
							<p class="mt-1 text-xs text-gray-400">Link your WhatsApp account to enable messaging through your agent</p>
							<div class="mt-4">
								{#if whatsappLinked}
									<div class="flex items-center justify-between">
										<div class="flex items-center gap-2 text-sm text-green-700">
											<span class="h-2 w-2 rounded-full bg-green-500"></span>
											Linked{whatsappPhone ? ` (${whatsappPhone})` : ''}
										</div>
										<div class="flex items-center gap-2">
											<button
												onclick={refreshWhatsAppStatus}
												disabled={whatsappChecking}
												class="text-xs text-gray-400 hover:text-gray-600 disabled:opacity-50"
												title="Refresh status"
											>
												{whatsappChecking ? 'Checking...' : 'Refresh'}
											</button>
											<button
												onclick={handleWhatsAppConnect}
												disabled={whatsappLoading}
												class="text-xs text-gray-400 hover:text-gray-600 disabled:opacity-50"
												title="Reconnect with a different WhatsApp account"
											>
												Reconnect
											</button>
										</div>
									</div>
									<div class="mt-4 rounded-lg border border-green-200 bg-green-50 p-4">
										<h4 class="text-sm font-semibold text-green-900">Start chatting with your agent</h4>
										<p class="mt-1 text-xs text-green-800">Your WhatsApp is now linked. To talk to your AI agent, message yourself on WhatsApp:</p>
										<ol class="mt-2 space-y-1.5 text-xs text-green-800">
											<li><span class="font-semibold">1.</span> Open <span class="font-semibold">WhatsApp</span> on your phone</li>
											<li><span class="font-semibold">2.</span> Tap <span class="font-semibold">New Chat</span> (or the compose button)</li>
											<li><span class="font-semibold">3.</span> Search for your own name or number &mdash; you'll see <span class="font-semibold">"Message yourself"</span></li>
											<li><span class="font-semibold">4.</span> Send any message &mdash; your agent will reply!</li>
										</ol>
										<p class="mt-2 text-xs text-green-700">Other people can also message your WhatsApp number to interact with your agent.</p>
									</div>
								{:else if whatsappQR}
									<div class="flex flex-col items-center gap-3">
										<img src={whatsappQR} alt="WhatsApp QR Code" class="h-48 w-48 rounded-lg" />
										<div class="rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 text-xs text-gray-700">
											<p class="font-semibold text-gray-900">How to scan this QR code:</p>
											<ol class="mt-1.5 space-y-1">
												<li><span class="font-semibold">1.</span> Open <span class="font-semibold">WhatsApp</span> on your phone</li>
												<li><span class="font-semibold">2.</span> Go to <span class="font-semibold">Settings &gt; Linked Devices</span></li>
												<li><span class="font-semibold">3.</span> Tap <span class="font-semibold">"Link a Device"</span></li>
												<li><span class="font-semibold">4.</span> Point your phone camera at the QR code above</li>
											</ol>
										</div>
										{#if whatsappPolling}
											<p class="text-xs text-gray-400">Waiting for scan...</p>
										{/if}
										<button
											onclick={handleWhatsAppConnect}
											class="text-xs text-gray-500 hover:text-gray-700"
										>
											Refresh QR
										</button>
									</div>
								{:else}
									{#if showApiKeyRequired}
										<div class="mb-3 rounded-lg border border-orange-200 bg-orange-50 p-3">
											<p class="text-sm font-medium text-orange-800">API key required</p>
											<p class="mt-1 text-xs text-orange-700">
												Your agent needs an AI model API key to respond to WhatsApp messages. Set your OpenRouter API key in the <strong>AI Provider</strong> section above, then try again.
											</p>
											<div class="mt-2 flex items-center gap-3">
												<button
													type="button"
													onclick={() => (showApiKeyGuide = !showApiKeyGuide)}
													class="inline-flex items-center gap-1 text-xs font-medium text-orange-800 underline hover:text-orange-900"
												>
													{showApiKeyGuide ? 'Hide Guide' : 'Guide Me'}
												</button>
												<a
													href="https://openrouter.ai/keys"
													target="_blank"
													rel="noopener noreferrer"
													class="inline-flex items-center gap-1 text-xs font-medium text-orange-800 underline hover:text-orange-900"
												>
													Get an OpenRouter API key
													<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="h-3 w-3">
														<path fill-rule="evenodd" d="M4.25 5.5a.75.75 0 00-.75.75v8.5c0 .414.336.75.75.75h8.5a.75.75 0 00.75-.75v-4a.75.75 0 011.5 0v4A2.25 2.25 0 0112.75 17h-8.5A2.25 2.25 0 012 14.75v-8.5A2.25 2.25 0 014.25 4h5a.75.75 0 010 1.5h-5z" clip-rule="evenodd" />
														<path fill-rule="evenodd" d="M6.194 12.753a.75.75 0 001.06.053L16.5 4.44v2.81a.75.75 0 001.5 0v-4.5a.75.75 0 00-.75-.75h-4.5a.75.75 0 000 1.5h2.553l-9.056 8.194a.75.75 0 00-.053 1.06z" clip-rule="evenodd" />
													</svg>
												</a>
											</div>
											{#if showApiKeyGuide}
												<ol class="mt-3 space-y-1.5 text-xs text-orange-800">
													<li><span class="font-semibold">1.</span> Go to <a href="https://openrouter.ai" target="_blank" rel="noopener noreferrer" class="underline hover:text-orange-900">openrouter.ai</a> and click <span class="font-semibold">Sign Up</span></li>
													<li><span class="font-semibold">2.</span> Create an account using your email, Google, or GitHub</li>
													<li><span class="font-semibold">3.</span> Go to <a href="https://openrouter.ai/keys" target="_blank" rel="noopener noreferrer" class="underline hover:text-orange-900">openrouter.ai/keys</a></li>
													<li><span class="font-semibold">4.</span> Click <span class="font-semibold">Create Key</span> and name it (e.g. "Tardi")</li>
													<li><span class="font-semibold">5.</span> Copy the key (starts with <code class="rounded bg-orange-100 px-1 py-0.5">sk-or-v1-...</code>) and paste it in the <strong>AI Provider</strong> section above</li>
													<li><span class="font-semibold">6.</span> Click <span class="font-semibold">Save</span>, then come back here to connect WhatsApp</li>
												</ol>
											{/if}
										</div>
									{/if}
									{#if whatsappError}
										<p class="mb-3 text-xs text-red-600">{whatsappError}</p>
									{/if}
									<button
										onclick={handleWhatsAppConnect}
										disabled={whatsappLoading || instance.status !== 'active'}
										class="inline-flex items-center gap-2 rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
									>
										<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="h-4 w-4">
											<path d="M17.472 14.382c-.297-.149-1.758-.867-2.03-.967-.273-.099-.471-.148-.67.15-.197.297-.767.966-.94 1.164-.173.199-.347.223-.644.075-.297-.15-1.255-.463-2.39-1.475-.883-.788-1.48-1.761-1.653-2.059-.173-.297-.018-.458.13-.606.134-.133.298-.347.446-.52.149-.174.198-.298.298-.497.099-.198.05-.371-.025-.52-.075-.149-.669-1.612-.916-2.207-.242-.579-.487-.5-.669-.51-.173-.008-.371-.01-.57-.01-.198 0-.52.074-.792.372-.272.297-1.04 1.016-1.04 2.479 0 1.462 1.065 2.875 1.213 3.074.149.198 2.096 3.2 5.077 4.487.709.306 1.262.489 1.694.625.712.227 1.36.195 1.871.118.571-.085 1.758-.719 2.006-1.413.248-.694.248-1.289.173-1.413-.074-.124-.272-.198-.57-.347m-5.421 7.403h-.004a9.87 9.87 0 01-5.031-1.378l-.361-.214-3.741.982.998-3.648-.235-.374a9.86 9.86 0 01-1.51-5.26c.001-5.45 4.436-9.884 9.888-9.884 2.64 0 5.122 1.03 6.988 2.898a9.825 9.825 0 012.893 6.994c-.003 5.45-4.437 9.884-9.885 9.884m8.413-18.297A11.815 11.815 0 0012.05 0C5.495 0 .16 5.335.157 11.892c0 2.096.547 4.142 1.588 5.945L.057 24l6.305-1.654a11.882 11.882 0 005.683 1.448h.005c6.554 0 11.89-5.335 11.893-11.893a11.821 11.821 0 00-3.48-8.413z"/>
										</svg>
										{whatsappLoading ? 'Loading...' : 'Connect WhatsApp'}
									</button>
								{/if}
							</div>
						</div>
					{/if}

					{#if (instance.dashboard_url && instance.openclaw_auth_token) || instance.ipv4}
						<div class="rounded-xl border border-gray-200">
							<button
								onclick={() => (powerUserOpen = !powerUserOpen)}
								class="flex w-full items-center justify-between p-5"
							>
								<div class="text-left">
									<h3 class="text-sm font-semibold text-gray-900">Power User</h3>
									<p class="mt-1 text-xs text-gray-400">Dashboard access and SSH connection</p>
								</div>
								<svg
									xmlns="http://www.w3.org/2000/svg"
									viewBox="0 0 20 20"
									fill="currentColor"
									class="h-5 w-5 text-gray-400 transition-transform {powerUserOpen ? 'rotate-180' : ''}"
								>
									<path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
								</svg>
							</button>

							{#if powerUserOpen}
								<div class="space-y-5 border-t border-gray-200 p-5">
									<div>
										<h4 class="text-sm font-medium text-gray-900">Advanced AI Settings</h4>
										<p class="mt-1 text-xs text-gray-400">Choose provider, model, and manage additional API keys</p>
										<div class="mt-3">
											<AIProviderAdvanced instanceId={instance.id} disabled={instance.status !== 'active'} />
										</div>
									</div>

									{#if instance.dashboard_url && instance.openclaw_auth_token}
										<div>
											<h4 class="text-sm font-medium text-gray-900">Dashboard</h4>
											<p class="mt-1 text-xs text-gray-400">Access your agent's OpenClaw control panel</p>
											<div class="mt-3">
												<a
													href="{instance.dashboard_url}/?token={instance.openclaw_auth_token}"
													target="_blank"
													rel="noopener noreferrer"
													class="inline-flex items-center gap-2 rounded-lg bg-gray-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-gray-800"
												>
													<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="h-4 w-4">
														<path fill-rule="evenodd" d="M4.25 5.5a.75.75 0 00-.75.75v8.5c0 .414.336.75.75.75h8.5a.75.75 0 00.75-.75v-4a.75.75 0 011.5 0v4A2.25 2.25 0 0112.75 17h-8.5A2.25 2.25 0 012 14.75v-8.5A2.25 2.25 0 014.25 4h5a.75.75 0 010 1.5h-5z" clip-rule="evenodd" />
														<path fill-rule="evenodd" d="M6.194 12.753a.75.75 0 001.06.053L16.5 4.44v2.81a.75.75 0 001.5 0v-4.5a.75.75 0 00-.75-.75h-4.5a.75.75 0 000 1.5h2.553l-9.056 8.194a.75.75 0 00-.053 1.06z" clip-rule="evenodd" />
													</svg>
													Open Dashboard
												</a>
												<p class="mt-2 text-xs text-gray-400">You may need to accept the security certificate on first visit.</p>
											</div>
										</div>
									{/if}

									{#if instance.ipv4}
										<div>
											<h4 class="text-sm font-medium text-gray-900">SSH Access</h4>
											<p class="mt-1 text-xs text-gray-400">Connect to your agent's server via SSH</p>
											<dl class="mt-3 space-y-3 text-sm">
												<div>
													<dt class="text-gray-500">Command</dt>
													<dd class="mt-1 flex items-center gap-2">
														<code class="flex-1 rounded-md bg-gray-100 px-3 py-2 font-mono text-xs text-gray-900">ssh root@{instance.ipv4}</code>
														<button
															onclick={() => navigator.clipboard.writeText(`ssh root@${instance.ipv4}`)}
															class="rounded-md border border-gray-300 px-2.5 py-2 text-xs text-gray-600 hover:bg-gray-50"
														>
															Copy
														</button>
													</dd>
												</div>
												{#if instance.root_password}
													<div>
														<dt class="text-gray-500">Root Password</dt>
														<dd class="mt-1 flex items-center gap-2">
															<code class="flex-1 rounded-md bg-gray-100 px-3 py-2 font-mono text-xs text-gray-900">
																{showPassword ? instance.root_password : '••••••••••••'}
															</code>
															<button
																onclick={() => (showPassword = !showPassword)}
																class="rounded-md border border-gray-300 px-2.5 py-2 text-xs text-gray-600 hover:bg-gray-50"
															>
																{showPassword ? 'Hide' : 'Show'}
															</button>
															<button
																onclick={() => navigator.clipboard.writeText(instance.root_password ?? '')}
																class="rounded-md border border-gray-300 px-2.5 py-2 text-xs text-gray-600 hover:bg-gray-50"
															>
																Copy
															</button>
														</dd>
													</div>
												{:else}
													<div>
														<dt class="text-gray-500">Root Password</dt>
														<dd class="mt-1 text-xs text-gray-400">No password stored. Use Reset Password to generate one.</dd>
													</div>
												{/if}
											</dl>
											<div class="mt-4 border-t border-gray-100 pt-3">
												<button
													onclick={handleResetPassword}
													disabled={resettingPassword || instance.status !== 'active'}
													class="text-xs text-gray-500 hover:text-gray-700 disabled:opacity-50"
												>
													{resettingPassword ? 'Resetting...' : 'Reset Password'}
												</button>
											</div>
										</div>
									{/if}
								</div>
							{/if}
						</div>
					{/if}

					<div class="flex gap-3">
						<button
							onclick={handleRestart}
							disabled={restarting || instance.status !== 'active'}
							class="rounded-lg border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50"
						>
							{instance.status === 'restarting' ? 'Restarting...' : restarting ? 'Restarting...' : 'Restart'}
						</button>
					</div>
				{/if}
			</div>

			<!-- Right column -->
			<div class="space-y-6">
				{#if isProvisioning}
					<div class="rounded-xl border border-gray-200 p-5">
						<h3 class="text-sm font-semibold text-gray-900">Provisioning Progress</h3>
						<div class="mt-4">
							<ProvisioningProgress currentStep={instance.step} />
						</div>
					</div>
				{/if}

				{#if instance.status === 'restoring'}
					<div class="rounded-xl border border-blue-300 bg-blue-50 p-5">
						<div class="flex items-center gap-3">
							<svg class="h-5 w-5 animate-spin text-blue-600 shrink-0" viewBox="0 0 24 24" fill="none">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
								<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
							</svg>
							<div>
								<h3 class="text-sm font-semibold text-blue-900">Restoring from Snapshot</h3>
								<p class="mt-1 text-xs text-blue-700">Your agent is being rebuilt from a snapshot. This may take a few minutes. A new root password will be generated.</p>
							</div>
						</div>
					</div>
				{/if}

				{#if instance.status === 'snapshotting'}
					<div class="rounded-xl border border-blue-300 bg-blue-50 p-5">
						<div class="flex items-center gap-3">
							<svg class="h-5 w-5 animate-spin text-blue-600 shrink-0" viewBox="0 0 24 24" fill="none">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
								<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
							</svg>
							<div>
								<h3 class="text-sm font-semibold text-blue-900">Creating Snapshot</h3>
								<p class="mt-1 text-xs text-blue-700">A snapshot of your agent's current state is being created. This may take a few minutes.</p>
							</div>
						</div>
					</div>
				{/if}

				{#if instance.status === 'active' || instance.status === 'snapshotting' || instance.status === 'restoring' || instance.status === 'restarting'}
					<div class="rounded-xl border border-gray-200 p-5">
						<div class="flex items-center justify-between">
							<h3 class="text-sm font-semibold text-gray-900">Snapshots</h3>
							<span class="text-xs text-gray-400">{instanceSnapshots.length}/3 used</span>
						</div>
						<p class="mt-1 text-xs text-gray-400">Create backups of your agent's disk state</p>

						{#if instanceSnapshots.length > 0}
							<div class="mt-4 space-y-3">
								{#each instanceSnapshots as snap (snap.id)}
									<div class="flex items-center justify-between rounded-lg border border-gray-100 px-3 py-2.5">
										<div class="min-w-0 flex-1">
											<p class="text-sm font-medium text-gray-900 truncate">{snap.name}</p>
											<p class="text-xs text-gray-400">
												{new Date(snap.created_at).toLocaleDateString()}
												{#if snap.size_gb}
													&middot; {snap.size_gb.toFixed(1)} GB
												{/if}
												{#if snap.status === 'creating'}
													<span class="ml-1 text-blue-600">Creating...</span>
												{:else if snap.status === 'deleting'}
													<span class="ml-1 text-gray-500">Deleting...</span>
												{:else if snap.status === 'error'}
													<span class="ml-1 text-red-600">Error</span>
												{/if}
											</p>
										</div>
										{#if snap.status === 'ready'}
											<div class="flex items-center gap-1.5 ml-3">
												{#if confirmRestoreId === snap.id}
													<span class="text-xs text-gray-500 mr-1">Are you sure?</span>
													<button
														onclick={() => handleRestoreSnapshot(snap.id)}
														disabled={restoringSnapshotId === snap.id}
														class="rounded-md bg-gray-900 px-2.5 py-1 text-xs font-medium text-white hover:bg-gray-800 disabled:opacity-50"
													>
														Confirm
													</button>
													<button
														onclick={() => (confirmRestoreId = null)}
														class="rounded-md border border-gray-300 px-2.5 py-1 text-xs text-gray-600 hover:bg-gray-50"
													>
														Cancel
													</button>
												{:else}
													<button
														onclick={() => (confirmRestoreId = snap.id)}
														disabled={instance.status !== 'active'}
														class="rounded-md border border-gray-300 px-2.5 py-1 text-xs text-gray-600 hover:bg-gray-50 disabled:opacity-50"
														title="Restore agent from this snapshot"
													>
														Restore
													</button>
													<button
														onclick={() => handleDeleteSnapshot(snap.id)}
														disabled={deletingSnapshotId === snap.id || isBusy}
														class="rounded-md border border-gray-300 px-2.5 py-1 text-xs text-red-600 hover:bg-red-50 disabled:opacity-50"
														title="Delete this snapshot"
													>
														{deletingSnapshotId === snap.id ? 'Deleting...' : 'Delete'}
													</button>
												{/if}
											</div>
										{:else if snap.status === 'creating' || snap.status === 'deleting'}
											<div class="ml-3">
												<svg class="h-4 w-4 animate-spin text-gray-400" viewBox="0 0 24 24" fill="none">
													<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
													<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
												</svg>
											</div>
										{/if}
									</div>
								{/each}
							</div>
						{:else}
							<p class="mt-4 text-xs text-gray-400">No snapshots yet</p>
						{/if}

						<div class="mt-4 border-t border-gray-100 pt-3">
							{#if showSnapshotForm}
								<form onsubmit={(e) => { e.preventDefault(); handleCreateSnapshot(); }} class="flex items-center gap-2">
									<input
										type="text"
										bind:value={snapshotName}
										placeholder="Snapshot name"
										class="flex-1 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm text-gray-900 focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900"
										autofocus
										onkeydown={(e) => { if (e.key === 'Escape') { showSnapshotForm = false; snapshotName = ''; } }}
									/>
									<button
										type="submit"
										disabled={creatingSnapshot || !snapshotName.trim()}
										class="rounded-md bg-gray-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-gray-800 disabled:opacity-50"
									>
										{creatingSnapshot ? 'Creating...' : 'Create'}
									</button>
									<button
										type="button"
										onclick={() => { showSnapshotForm = false; snapshotName = ''; }}
										class="rounded-md border border-gray-300 px-3 py-1.5 text-xs text-gray-600 hover:bg-gray-50"
									>
										Cancel
									</button>
								</form>
							{:else}
								<button
									onclick={() => (showSnapshotForm = true)}
									disabled={instanceSnapshots.length >= 3 || instance.status !== 'active'}
									class="text-sm text-gray-900 hover:text-gray-700 disabled:opacity-50 disabled:cursor-not-allowed"
								>
									+ Create Snapshot
								</button>
							{/if}
						</div>
					</div>
				{/if}
			</div>
		</div>
	</div>
{/if}
