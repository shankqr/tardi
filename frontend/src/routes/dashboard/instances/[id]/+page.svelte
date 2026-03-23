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
		getAgentConfig,
		connectTelegram,
		disconnectTelegram,
		cleanupTelegramConfig,
		syncConfig,
		getSyncStatus,
		runDoctor,
		type HealthCheck
	} from '$lib/api/client';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import ProvisioningProgress from '$lib/components/ProvisioningProgress.svelte';
	import AIProviderConfig from '$lib/components/AIProviderConfig.svelte';
	import MagicMoment from '$lib/components/MagicMoment.svelte';

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
	let confirmDeleteId = $state<string | null>(null);
	let confirmDeleteInput = $state('');

	// API key gate
	let hasApiKey = $state(false);

	// Telegram state
	let telegramToken = $state('');
	let telegramLoading = $state(false);
	let telegramError = $state<string | null>(null);
	let telegramConnected = $state(false);
	let showUpdateToken = $state(false);
	type TelegramSyncPhase = 'idle' | 'syncing' | 'finishing' | 'success' | 'failed';
	let telegramSyncPhase = $state<TelegramSyncPhase>('idle');
	let telegramSyncElapsed = $state(0);
	let telegramSyncTimer: ReturnType<typeof setInterval> | null = null;
	let telegramPollTimer: ReturnType<typeof setInterval> | null = null;

	// Config sync tracking — when any component is actively syncing config
	// Grace period: keep showing "Applying Config" for 90s after sync ends,
	// because the container restart causes a brief unhealthy heartbeat window.
	let aiConfigSyncing = $state(false);
	let syncGraceActive = $state(false);
	let syncGraceTimer: ReturnType<typeof setTimeout> | null = null;

	const isAnySyncing = $derived(
		aiConfigSyncing ||
		telegramSyncPhase === 'syncing' || telegramSyncPhase === 'finishing'
	);
	const isConfigSyncing = $derived(isAnySyncing || syncGraceActive);

	// Watch for sync ending → start grace period (max 30s).
	// The sync script triggers an immediate heartbeat on success, so
	// agent_status updates within seconds. We end the grace early once
	// the status flips to "running" (see effect below).
	$effect(() => {
		if (isAnySyncing) {
			// Sync just started — cancel any existing grace timer
			if (syncGraceTimer) clearTimeout(syncGraceTimer);
			syncGraceActive = true;
		} else if (syncGraceActive) {
			// Sync just ended — keep grace active for max 30s
			syncGraceTimer = setTimeout(() => {
				syncGraceActive = false;
				syncGraceTimer = null;
			}, 30_000);
		}
		return () => {
			if (syncGraceTimer) clearTimeout(syncGraceTimer);
		};
	});

	// End grace period early once the heartbeat confirms healthy status
	$effect(() => {
		if (syncGraceActive && !isAnySyncing && instance?.agent_status === 'running') {
			if (syncGraceTimer) clearTimeout(syncGraceTimer);
			syncGraceActive = false;
			syncGraceTimer = null;
		}
	});

	// Doctor state
	let doctorRunning = $state(false);
	let doctorChecks = $state<HealthCheck[] | null>(null);
	let doctorRaw = $state<string | null>(null);
	let doctorError = $state<string | null>(null);

	function startTelegramSyncTimer() {
		telegramSyncElapsed = 0;
		telegramSyncTimer = setInterval(() => { telegramSyncElapsed += 1; }, 1000);
	}
	function stopTelegramSyncTimer() {
		if (telegramSyncTimer) { clearInterval(telegramSyncTimer); telegramSyncTimer = null; }
	}
	function stopTelegramPollTimer() {
		if (telegramPollTimer) { clearInterval(telegramPollTimer); telegramPollTimer = null; }
	}

	async function pollTelegramSyncStatus() {
		if (!instance) return;
		try {
			const token = await getIdToken();
			if (!token) return;
			const result = await getSyncStatus(token, instance.id);
			if (result.status === 'completed') {
				stopTelegramSyncTimer();
				stopTelegramPollTimer();
				// Remove channels.telegram from OpenClaw's internal config to prevent
				// duplicate handlers (internal config + env var auto-detection = double replies)
				try {
					await cleanupTelegramConfig(token, instance.id);
				} catch { /* non-critical, env var handler still works */ }
				telegramSyncPhase = 'finishing';
				setTimeout(() => {
					telegramSyncPhase = 'success';
					setTimeout(() => { if (telegramSyncPhase === 'success') telegramSyncPhase = 'idle'; }, 8000);
				}, 15000);
			} else if (result.status === 'failed') {
				stopTelegramSyncTimer();
				stopTelegramPollTimer();
				telegramError = result.message || 'Config sync failed on your agent';
				telegramSyncPhase = 'failed';
			}
		} catch {
			// Ignore poll errors, keep trying
		}
	}

	// Busy state — disables all actions during non-active statuses
	const isBusy = $derived(instance != null && instance.status !== 'active');

	// Status transition tracking for result notifications
	// NOTE: these are plain variables (not $state) to avoid reactive loops —
	// they are read+written in $effect blocks, and $state would cause re-triggers.
	let previousStatus: string | null = null;
	let previousSnapshotStatuses: Record<string, string> = {};
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

	// Also track individual snapshot status transitions (creating → ready/error)
	$effect(() => {
		if (!instanceSnapshots.length) return;
		const newStatuses: Record<string, string> = {};
		for (const snap of instanceSnapshots) {
			const prev = previousSnapshotStatuses[snap.id];
			if (prev === 'creating' && snap.status === 'ready') snapshotResult = 'success';
			if (prev === 'creating' && snap.status === 'error') snapshotResult = 'failed';
			newStatuses[snap.id] = snap.status;
		}
		previousSnapshotStatuses = newStatuses;
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

	function promptDeleteSnapshot(snapshotId: string) {
		confirmDeleteId = snapshotId;
		confirmDeleteInput = '';
	}

	function cancelDeleteSnapshot() {
		confirmDeleteId = null;
		confirmDeleteInput = '';
	}

	async function handleDeleteSnapshot(snapshotId: string) {
		deletingSnapshotId = snapshotId;
		confirmDeleteId = null;
		confirmDeleteInput = '';
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

	async function triggerTelegramSync() {
		if (!instance) return;
		telegramSyncPhase = 'syncing';
		telegramError = null;
		startTelegramSyncTimer();
		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');
			const result = await syncConfig(token, instance.id);
			if (result.synced) {
				stopTelegramPollTimer();
				telegramPollTimer = setInterval(() => {
					if (telegramSyncElapsed > 300) {
						stopTelegramSyncTimer();
						stopTelegramPollTimer();
						telegramError = 'Sync is taking longer than expected — it will apply automatically within a few minutes';
						telegramSyncPhase = 'failed';
						return;
					}
					pollTelegramSyncStatus();
				}, 5000);
			} else {
				stopTelegramSyncTimer();
				telegramError = result.error || 'Sync failed — click Retry to try again';
				telegramSyncPhase = 'failed';
			}
		} catch {
			stopTelegramSyncTimer();
			telegramError = 'Could not reach your agent — changes will apply within 5 minutes';
			telegramSyncPhase = 'failed';
		}
	}

	async function handleTelegramConnect() {
		if (!instance || !telegramToken.trim()) return;
		telegramLoading = true;
		telegramError = null;
		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');
			await connectTelegram(token, instance.id, telegramToken.trim());
			telegramConnected = true;
			telegramToken = '';
			showUpdateToken = false;
			telegramLoading = false;
			await triggerTelegramSync();
		} catch (err) {
			telegramError = err instanceof Error ? err.message : 'Failed to connect Telegram';
			telegramLoading = false;
		}
	}

	async function handleTelegramDisconnect() {
		if (!instance) return;
		telegramLoading = true;
		telegramError = null;
		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');
			await disconnectTelegram(token, instance.id);
			telegramConnected = false;
			telegramLoading = false;
			await triggerTelegramSync();
		} catch (err) {
			telegramError = err instanceof Error ? err.message : 'Failed to disconnect Telegram';
			telegramLoading = false;
		}
	}

	async function handleRunDoctor() {
		if (!instance) return;
		doctorRunning = true;
		doctorChecks = null;
		doctorRaw = null;
		doctorError = null;
		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');
			const result = await runDoctor(token, instance.id);
			if (result.error) {
				doctorError = result.detail || result.error;
			} else if (result.checks) {
				doctorChecks = result.checks;
			} else if (result.raw) {
				doctorRaw = result.raw;
			}
		} catch (err) {
			doctorError = err instanceof Error ? err.message : 'Health check failed';
		} finally {
			doctorRunning = false;
		}
	}

	// Check Telegram status on load (token exists in config = connected)
	$effect(() => {
		if (instance?.status === 'active') {
			(async () => {
				try {
					const token = await getIdToken();
					if (!token) return;
					const cfg = await getAgentConfig(token, instance.id);
					telegramConnected = !!(cfg.config.telegram_bot_token && typeof cfg.config.telegram_bot_token === 'string' && cfg.config.telegram_bot_token.length > 0);
					const hasKey = (k: string): boolean => !!(cfg.config[k] && typeof cfg.config[k] === 'string' && (cfg.config[k] as string).length > 0);
					hasApiKey = hasKey('openrouter_api_key') || hasKey('anthropic_api_key') || hasKey('openai_api_key');
				} catch {
					// ignore
				}
			})();
		}
	});

	async function recheckConfig() {
		if (!instance) return;
		try {
			const token = await getIdToken();
			if (!token) return;
			const cfg = await getAgentConfig(token, instance.id);
			const hasKey = (k: string): boolean => !!(cfg.config[k] && typeof cfg.config[k] === 'string' && (cfg.config[k] as string).length > 0);
			hasApiKey = hasKey('openrouter_api_key') || hasKey('anthropic_api_key') || hasKey('openai_api_key');
		} catch {
			// ignore
		}
	}

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
								{#if isConfigSyncing}
									<span class="inline-flex items-center gap-1.5 text-blue-700">
										<svg class="h-3.5 w-3.5 animate-spin text-blue-500" viewBox="0 0 24 24" fill="none">
											<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
											<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
										</svg>
										Applying Config
									</span>
								{:else if instance.agent_status === 'running'}
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

					{#if instance.status === 'active' && instance.agent_status === 'running' && instance.dashboard_url && instance.openclaw_auth_token}
						<button
							onclick={() => window.open(`${instance.dashboard_url}/#password=${instance.openclaw_auth_token}`, '_blank')}
							class="mt-4 rounded-lg bg-gray-900 px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-gray-800"
						>
							Open Agent Dashboard
						</button>
					{/if}
				</div>

				{#if instance.status === 'active' || instance.status === 'restarting' || instance.status === 'snapshotting' || instance.status === 'restoring'}
					<AIProviderConfig instanceId={instance.id} disabled={instance.status !== 'active'} onsaved={recheckConfig} onsyncchange={(s) => aiConfigSyncing = s} />

					<div class="rounded-xl border border-gray-200 p-5">
						<div class="flex items-center gap-2">
							<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" class="h-5 w-5 text-[#2AABEE]">
								<path d="M11.944 0A12 12 0 0 0 0 12a12 12 0 0 0 12 12 12 12 0 0 0 12-12A12 12 0 0 0 12 0a12 12 0 0 0-.056 0zm4.962 7.224c.1-.002.321.023.465.14a.506.506 0 0 1 .171.325c.016.093.036.306.02.472-.18 1.898-.962 6.502-1.36 8.627-.168.9-.499 1.201-.82 1.23-.696.065-1.225-.46-1.9-.902-1.056-.693-1.653-1.124-2.678-1.8-1.185-.78-.417-1.21.258-1.91.177-.184 3.247-2.977 3.307-3.23.007-.032.014-.15-.056-.212s-.174-.041-.249-.024c-.106.024-1.793 1.14-5.061 3.345-.48.33-.913.49-1.302.48-.428-.008-1.252-.241-1.865-.44-.752-.245-1.349-.374-1.297-.789.027-.216.325-.437.893-.663 3.498-1.524 5.83-2.529 6.998-3.014 3.332-1.386 4.025-1.627 4.476-1.635z"/>
							</svg>
							<h3 class="text-sm font-semibold text-gray-900">Telegram</h3>
						</div>
						<p class="mt-1 text-xs text-gray-400">Link a Telegram bot to enable messaging through your agent</p>

						<div class="mt-4">
							{#if !hasApiKey && !telegramConnected}
								<div class="rounded-lg border border-gray-200 bg-gray-50 px-4 py-3">
									<p class="text-xs text-gray-500">Set up your AI provider key above before connecting Telegram.</p>
								</div>
							{:else if telegramConnected}
								<div class="flex items-center justify-between">
									<div class="flex items-center gap-2 text-sm {telegramSyncPhase === 'syncing' || telegramSyncPhase === 'finishing' ? 'text-amber-700' : 'text-green-700'}">
										{#if telegramSyncPhase === 'syncing'}
											<svg class="h-4 w-4 animate-spin" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
												<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
												<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
											</svg>
											Deploying to your agent...
										{:else if telegramSyncPhase === 'finishing'}
											<svg class="h-4 w-4 animate-spin" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
												<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
												<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
											</svg>
											Finalizing setup...
										{:else}
											<span class="h-2 w-2 rounded-full bg-green-500"></span>
											Telegram bot connected
										{/if}
									</div>
									{#if telegramSyncPhase !== 'syncing' && telegramSyncPhase !== 'finishing'}
										<div class="flex items-center gap-3">
											<button
												onclick={() => { showUpdateToken = !showUpdateToken; telegramError = null; }}
												class="text-xs text-gray-500 hover:text-gray-700"
											>
												{showUpdateToken ? 'Cancel' : 'Update Token'}
											</button>
											<button
												onclick={handleTelegramDisconnect}
												disabled={telegramLoading}
												class="text-xs text-gray-400 hover:text-gray-600 disabled:opacity-50"
											>
												{telegramLoading ? 'Disconnecting...' : 'Disconnect'}
											</button>
										</div>
									{/if}
								</div>
								{#if showUpdateToken && telegramSyncPhase === 'idle'}
									<div class="mt-3 flex items-center gap-2">
										<input
											type="text"
											bind:value={telegramToken}
											placeholder="Paste new bot token"
											disabled={telegramLoading || instance.status !== 'active'}
											class="flex-1 rounded-lg border border-gray-300 px-3 py-2 text-sm text-gray-900 placeholder-gray-400 focus:border-gray-500 focus:outline-none focus:ring-1 focus:ring-gray-500 disabled:opacity-50"
										/>
										<button
											onclick={handleTelegramConnect}
											disabled={telegramLoading || !telegramToken.trim() || instance.status !== 'active'}
											class="inline-flex items-center rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
										>
											{telegramLoading ? 'Updating...' : 'Update'}
										</button>
									</div>
								{/if}
								{#if telegramSyncPhase === 'syncing'}
									<div class="mt-3 rounded-lg border border-gray-200 bg-gray-50 p-4">
										<div class="space-y-3">
											<div class="flex items-center gap-3">
												<svg class="h-4 w-4 animate-spin text-gray-600 shrink-0" viewBox="0 0 24 24" fill="none">
													<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
													<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
												</svg>
												<div class="flex-1">
													<p class="text-sm font-medium text-gray-900">Deploying Telegram bot to your agent...</p>
													<p class="text-xs text-gray-500">
														{#if telegramSyncElapsed < 10}
															Connecting to your agent
														{:else if telegramSyncElapsed < 30}
															Updating configuration
														{:else if telegramSyncElapsed < 60}
															Restarting with new settings
														{:else}
															Waiting for health check
														{/if}
														<span class="ml-1 tabular-nums text-gray-400">{telegramSyncElapsed}s</span>
													</p>
												</div>
											</div>
											<div class="h-1 overflow-hidden rounded-full bg-gray-200">
												<div
													class="h-full rounded-full bg-gray-600 transition-all duration-1000 ease-linear"
													style="width: {Math.min(telegramSyncElapsed / 80 * 100, 95)}%"
												></div>
											</div>
											<p class="text-xs text-gray-400">This usually takes about a minute. Please wait before messaging the bot.</p>
										</div>
									</div>
								{:else if telegramSyncPhase === 'finishing'}
									<div class="mt-3 rounded-lg border border-gray-200 bg-gray-50 p-4">
										<div class="space-y-3">
											<div class="flex items-center gap-3">
												<svg class="h-4 w-4 animate-spin text-gray-600 shrink-0" viewBox="0 0 24 24" fill="none">
													<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
													<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
												</svg>
												<div class="flex-1">
													<p class="text-sm font-medium text-gray-900">Finalizing Telegram bot setup...</p>
													<p class="text-xs text-gray-500">Almost ready</p>
												</div>
											</div>
											<div class="h-1 overflow-hidden rounded-full bg-gray-200">
												<div
													class="h-full rounded-full bg-gray-600 transition-all duration-1000 ease-linear"
													style="width: 95%"
												></div>
											</div>
											<p class="text-xs text-gray-400">Please wait before messaging the bot.</p>
										</div>
									</div>
								{:else if telegramSyncPhase === 'success'}
									<div class="mt-3 rounded-lg border border-green-200 bg-green-50 p-4">
										<div class="flex items-center gap-3">
											<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="h-5 w-5 text-green-600 shrink-0">
												<path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.857-9.809a.75.75 0 00-1.214-.882l-3.483 4.79-1.88-1.88a.75.75 0 10-1.06 1.061l2.5 2.5a.75.75 0 001.137-.089l4-5.5z" clip-rule="evenodd" />
											</svg>
											<div>
												<p class="text-sm font-medium text-green-800">Telegram bot deployed successfully</p>
												<p class="text-xs text-green-600">Anyone who messages your bot will get a response from your AI agent</p>
											</div>
										</div>
									</div>
								{:else if telegramError}
									<div class="mt-3 rounded-lg border border-amber-200 bg-amber-50 p-4">
										<p class="text-xs font-medium text-amber-800">{telegramError}</p>
										<p class="text-xs text-amber-600 mt-1">Your token was saved. It will apply automatically within a few minutes.</p>
										<button
											onclick={() => { telegramSyncPhase = 'idle'; telegramError = null; }}
											class="mt-2 rounded-md border border-amber-300 px-3 py-1 text-xs text-amber-700 hover:bg-amber-100"
										>
											Dismiss
										</button>
									</div>
								{:else}
									<div class="mt-3 rounded-lg border border-green-200 bg-green-50 p-4">
										<h4 class="text-sm font-semibold text-green-900">Your Telegram bot is live!</h4>
										<p class="mt-1 text-xs text-green-800">Anyone who messages your bot on Telegram will get a response from your AI agent.</p>
									</div>
								{/if}
							{:else}
								<div class="rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 text-xs text-gray-700">
									<p class="font-semibold text-gray-900">How to set up your Telegram bot:</p>
									<ol class="mt-2 space-y-2.5">
										<li>
											<span class="font-semibold">1.</span> Open Telegram and search for
											<a href="https://t.me/BotFather" target="_blank" rel="noopener noreferrer" class="font-semibold text-[#2AABEE] underline hover:text-blue-700">@BotFather</a>
										</li>
										<li>
											<span class="font-semibold">2.</span> Send <code class="rounded bg-gray-200 px-1.5 py-0.5 font-mono text-gray-800">/newbot</code> to create a new bot
										</li>
										<li>
											<span class="font-semibold">3.</span> Choose a <span class="font-semibold">display name</span> for your bot (e.g. "My AI Agent")
										</li>
										<li>
											<span class="font-semibold">4.</span> Choose a <span class="font-semibold">username</span> ending in <code class="rounded bg-gray-200 px-1.5 py-0.5 font-mono text-gray-800">bot</code> (e.g. <code class="rounded bg-gray-200 px-1.5 py-0.5 font-mono text-gray-800">my_ai_agent_bot</code>)
										</li>
										<li>
											<span class="font-semibold">5.</span> BotFather will send you an <span class="font-semibold">API token</span> &mdash; copy it (looks like <code class="rounded bg-gray-200 px-1.5 py-0.5 font-mono text-gray-800">123456:ABC-DEF1234...</code>)
										</li>
										<li>
											<span class="font-semibold">6.</span> Paste the token below and click <span class="font-semibold">Connect</span>
										</li>
									</ol>

									<div class="mt-3 border-t border-gray-200 pt-3">
										<p class="font-semibold text-gray-900">Optional: Customize your bot</p>
										<ul class="mt-1.5 space-y-1 text-gray-600">
											<li>&bull; Send <code class="rounded bg-gray-200 px-1.5 py-0.5 font-mono text-gray-800">/setdescription</code> to BotFather to set a bio</li>
											<li>&bull; Send <code class="rounded bg-gray-200 px-1.5 py-0.5 font-mono text-gray-800">/setuserpic</code> to BotFather to set a profile picture</li>
											<li>&bull; Send <code class="rounded bg-gray-200 px-1.5 py-0.5 font-mono text-gray-800">/setabouttext</code> to set the "About" section</li>
										</ul>
									</div>
								</div>

								{#if telegramError}
									<p class="mt-3 text-xs text-red-600">{telegramError}</p>
								{/if}
								<div class="mt-4 flex items-center gap-2">
									<input
										type="text"
										bind:value={telegramToken}
										placeholder="Paste your bot token here"
										disabled={telegramLoading || instance.status !== 'active'}
										class="flex-1 rounded-lg border border-gray-300 px-3 py-2 text-sm text-gray-900 placeholder-gray-400 focus:border-gray-500 focus:outline-none focus:ring-1 focus:ring-gray-500 disabled:opacity-50"
									/>
									<button
										onclick={handleTelegramConnect}
										disabled={telegramLoading || !telegramToken.trim() || instance.status !== 'active'}
										class="inline-flex items-center gap-2 rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
									>
										{telegramLoading ? 'Connecting...' : 'Connect'}
									</button>
								</div>
							{/if}
						</div>
					</div>

					{#if instance.ipv4}
						<div class="rounded-xl border border-gray-200">
							<button
								onclick={() => (powerUserOpen = !powerUserOpen)}
								class="flex w-full items-center justify-between p-5"
							>
								<div class="text-left">
									<h3 class="text-sm font-semibold text-gray-900">Power User</h3>
									<p class="mt-1 text-xs text-gray-400">Advanced settings and SSH connection</p>
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
								<div class="space-y-4 border-t border-gray-200 p-5">
									{#if instance.ipv4}
										<div class="rounded-lg border border-gray-200 p-4">
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

									<div class="flex gap-3">
										<button
											onclick={handleRestart}
											disabled={restarting || instance.status !== 'active'}
											class="rounded-lg border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50"
										>
											{instance.status === 'restarting' ? 'Restarting...' : restarting ? 'Restarting...' : 'Restart'}
										</button>
										<button
											onclick={handleRunDoctor}
											disabled={doctorRunning || instance.status !== 'active'}
											class="rounded-lg border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50"
										>
											{doctorRunning ? 'Checking...' : 'Health Check'}
										</button>
									</div>

									{#if doctorChecks !== null || doctorRaw !== null || doctorError !== null}
										<div class="rounded-lg border border-gray-200 p-4">
											<div class="flex items-center justify-between">
												<h4 class="text-sm font-medium text-gray-900">Health Check Results</h4>
												<button
													onclick={() => { doctorChecks = null; doctorRaw = null; doctorError = null; }}
													class="text-xs text-gray-400 hover:text-gray-600"
												>Dismiss</button>
											</div>
											{#if doctorError}
												<div class="mt-3 rounded-lg border border-red-200 bg-red-50 p-3 text-xs text-red-700">
													{doctorError}
												</div>
											{:else if doctorChecks}
												<div class="mt-3 space-y-1.5">
													{#each doctorChecks as check (check.name)}
														<div class="flex items-start gap-2.5 rounded-lg px-3 py-2 {check.status === 'fail' ? 'bg-red-50' : check.status === 'warn' ? 'bg-yellow-50' : ''}">
															<span class="mt-0.5 shrink-0 text-sm">
																{#if check.status === 'pass'}
																	<span class="text-green-600">&#10003;</span>
																{:else if check.status === 'fail'}
																	<span class="text-red-600">&#10007;</span>
																{:else if check.status === 'warn'}
																	<span class="text-yellow-600">&#9888;</span>
																{:else}
																	<span class="text-gray-400">&#8226;</span>
																{/if}
															</span>
															<div class="min-w-0 flex-1">
																<div class="flex items-baseline gap-2">
																	<span class="text-xs font-medium text-gray-500">{check.name}</span>
																	<span class="text-xs {check.status === 'fail' ? 'text-red-700 font-medium' : check.status === 'warn' ? 'text-yellow-700' : 'text-gray-700'}">{check.message}</span>
																</div>
																{#if check.detail}
																	<p class="mt-0.5 text-xs text-gray-400 whitespace-pre-wrap break-words">{check.detail}</p>
																{/if}
															</div>
														</div>
													{/each}
												</div>
												{#if doctorChecks.some(c => c.status === 'fail')}
													<div class="mt-3 border-t border-gray-100 pt-3 text-xs text-gray-500">
														Issues found. Try <button onclick={handleRestart} class="font-medium text-gray-700 hover:underline">restarting your agent</button> if problems persist.
													</div>
												{/if}
											{:else if doctorRaw}
												<pre class="mt-3 max-h-80 overflow-auto rounded-lg bg-gray-900 p-4 text-xs text-green-400 font-mono whitespace-pre-wrap">{doctorRaw}</pre>
											{/if}
										</div>
									{/if}
								</div>
							{/if}
						</div>
					{/if}

					{#if !isConfigSyncing && (instance.agent_status === 'unhealthy' || instance.agent_status === 'stopped' || instance.agent_status === 'not_found')}
						<div class="rounded-lg border border-yellow-200 bg-yellow-50 p-3 text-sm text-yellow-800 flex items-center justify-between">
							{#if instance.agent_status === 'stopped' || instance.agent_status === 'not_found'}
								<span>Your agent appears stopped. Restart to bring it back online.</span>
								<button
									onclick={handleRestart}
									disabled={restarting || instance.status !== 'active'}
									class="ml-3 shrink-0 rounded-md bg-yellow-600 px-3 py-1 text-xs font-medium text-white hover:bg-yellow-700 disabled:opacity-50"
								>
									{restarting ? 'Restarting...' : 'Restart'}
								</button>
							{:else}
								<span>Your agent appears unhealthy. Run a health check to diagnose the issue.</span>
								<button
									onclick={handleRunDoctor}
									disabled={doctorRunning || instance.status !== 'active'}
									class="ml-3 shrink-0 rounded-md bg-yellow-600 px-3 py-1 text-xs font-medium text-white hover:bg-yellow-700 disabled:opacity-50"
								>
									{doctorRunning ? 'Checking...' : 'Health Check'}
								</button>
							{/if}
						</div>
					{/if}

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

				{#if instance.status === 'active' && instance.agent_status === 'running' && instance.dashboard_url && instance.openclaw_auth_token}
					<MagicMoment
						previewUrl={instance.preview_url}
					/>
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
												{:else if confirmDeleteId === snap.id}
													<div class="flex flex-col items-end gap-1.5">
														<span class="text-xs text-gray-500">Type <strong>{snap.name}</strong> to confirm</span>
														<input
															type="text"
															bind:value={confirmDeleteInput}
															placeholder={snap.name}
															class="w-40 rounded-md border border-gray-300 px-2 py-1 text-xs text-gray-900 focus:border-red-500 focus:outline-none focus:ring-1 focus:ring-red-500"
														/>
														<div class="flex items-center gap-1.5">
															<button
																onclick={() => handleDeleteSnapshot(snap.id)}
																disabled={confirmDeleteInput !== snap.name}
																class="rounded-md bg-red-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-red-700 disabled:opacity-50 disabled:cursor-not-allowed"
															>
																Delete
															</button>
															<button
																onclick={cancelDeleteSnapshot}
																class="rounded-md border border-gray-300 px-2.5 py-1 text-xs text-gray-600 hover:bg-gray-50"
															>
																Cancel
															</button>
														</div>
													</div>
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
														onclick={() => promptDeleteSnapshot(snap.id)}
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
