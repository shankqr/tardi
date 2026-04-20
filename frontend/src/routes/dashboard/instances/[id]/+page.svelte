<script lang="ts">
	import { page } from '$app/state';
	import { dashboardState, dashboardLoading, refreshDashboard } from '$lib/stores/dashboard';
	import { getIdToken } from '$lib/stores/auth';
	import {
		restartInstance,
		renameInstance,
		createSnapshot,
		restoreSnapshot,
		deleteSnapshot,
		getAgentConfig,
		syncConfig,
		getSyncStatus,
		runDoctor,
		getDashboardToken,
		type HealthCheck
	} from '$lib/api/client';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import ProvisioningProgress from '$lib/components/ProvisioningProgress.svelte';
	import AIProviderConfig from '$lib/components/AIProviderConfig.svelte';
	import MagicMoment from '$lib/components/MagicMoment.svelte';
	import GoogleConnect from '$lib/components/GoogleConnect.svelte';
	import CodexConnect from '$lib/components/CodexConnect.svelte';
	import { featureFlags } from '$lib/featureFlags';

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
	let actionError = $state<string | null>(null);

	// Restart cooloff — prevents rapid successive restarts after server reboots
	let restartCooloffUntil = $state(0);
	let now = $state(Date.now());
	const restartCooloff = $derived(now < restartCooloffUntil);
	const restartCooloffRemaining = $derived(Math.ceil((restartCooloffUntil - now) / 1000));

	$effect(() => {
		if (!restartCooloff) return;
		const timer = setInterval(() => { now = Date.now(); }, 1000);
		return () => clearInterval(timer);
	});

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
	let dashboardBtnLoading = $state(false);

	// Config sync tracking — when any component is actively syncing config
	// Grace period: keep showing "Applying Config" after sync ends (90s max,
	// 60s minimum) because config.patch causes OpenClaw to briefly restart.
	let aiConfigSyncing = $state(false);
	let googleConnected = $state(false);
	let googleConnectRequests = $state(0);
	let syncGraceActive = $state(false);
	let syncGraceTimer: ReturnType<typeof setTimeout> | null = null;
	let syncGraceStartedAt = $state(0); // timestamp when grace started

	const isAnySyncing = $derived(aiConfigSyncing);
	const isConfigSyncing = $derived(isAnySyncing || syncGraceActive);

	// Watch for sync ending → start grace period (max 90s).
	// The config.patch RPC causes OpenClaw to briefly restart, so the
	// dashboard is unreachable for a few seconds. We keep the grace active
	// for a minimum of 60s to avoid showing "Unhealthy" while VPS restarts.
	$effect(() => {
		if (isAnySyncing) {
			// Sync just started — cancel any existing grace timer
			if (syncGraceTimer) clearTimeout(syncGraceTimer);
			syncGraceActive = true;
			syncGraceStartedAt = Date.now();
		} else if (syncGraceActive) {
			// Sync just ended — keep grace active for max 90 seconds
			syncGraceTimer = setTimeout(() => {
				syncGraceActive = false;
				syncGraceTimer = null;
			}, 90_000);
		}
		return () => {
			if (syncGraceTimer) clearTimeout(syncGraceTimer);
		};
	});

	// End grace period early once the heartbeat confirms healthy status,
	// but only after a minimum 60s cool-off to let OpenClaw fully restart.
	$effect(() => {
		if (syncGraceActive && !isAnySyncing && instance?.agent_status === 'running') {
			const elapsed = Date.now() - syncGraceStartedAt;
			const MIN_COOLOFF = 60_000;
			if (elapsed >= MIN_COOLOFF) {
				if (syncGraceTimer) clearTimeout(syncGraceTimer);
				syncGraceActive = false;
				syncGraceTimer = null;
			}
		}
	});

	// Post-activation cooling-off: suppress health status for 8 minutes
	// after creation if the agent hasn't reported healthy yet.
	// Derived from server data so it survives page navigation.
	const activationCooloff = $derived(
		instance?.status === 'active' &&
			instance.agent_status !== 'running' &&
			Date.now() - new Date(instance.created_at).getTime() < 8 * 60 * 1000
	);

	// Gate: show a dedicated "Setting Up" view until the VPS is healthy
	const isSettingUp = $derived(
		isProvisioning ||
			activationCooloff ||
			(instance?.status === 'error' && !instance?.agent_status)
	);

	// Elapsed timer for the setup view
	let setupElapsed = $state(0);

	$effect(() => {
		if (isSettingUp && instance?.created_at) {
			setupElapsed = Math.floor(
				(Date.now() - new Date(instance.created_at).getTime()) / 1000
			);
			const timer = setInterval(() => {
				setupElapsed += 1;
			}, 1000);
			return () => clearInterval(timer);
		}
	});

	// Config-sync grace: if agent is unhealthy but had a heartbeat within
	// the last 5 min, it's likely mid-restart from a config change. Derived
	// from server data so it survives page navigation.
	// Guard: only for instances older than 10 min to avoid false "Applying Config"
	// on freshly provisioned agents that haven't finished starting up yet.
	const recentlyRestarted = $derived(
		instance?.status === 'active' &&
			instance.agent_status !== 'running' &&
			instance.agent_status !== null &&
			instance.last_heartbeat_at !== null &&
			Date.now() - new Date(instance.created_at).getTime() > 10 * 60 * 1000 &&
			Date.now() - new Date(instance.last_heartbeat_at).getTime() < 300_000
	);

	// Doctor state
	let doctorRunning = $state(false);
	let doctorChecks = $state<HealthCheck[] | null>(null);
	let doctorRaw = $state<string | null>(null);
	let doctorError = $state<string | null>(null);

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
		if (previousStatus === 'snapshotting' && current === 'active')
			snapshotResult = 'success';
		if (previousStatus === 'snapshotting' && current === 'error') snapshotResult = 'failed';
		if (previousStatus === 'restarting' && current === 'active') {
			restartCooloffUntil = Date.now() + 60_000;
			now = Date.now();
		}
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

	// Restore "Applying Config" state on page load if a sync is still running
	// on the backend. This survives navigation because we query the actual
	// systemd unit state on the VPS rather than relying on in-memory flags.
	let syncChecked = false;
	let restoredSyncPollTimer: ReturnType<typeof setInterval> | null = null;
	$effect(() => {
		if (instance?.status === 'active' && !syncChecked) {
			syncChecked = true;
			(async () => {
				try {
					const token = await getIdToken();
					if (!token) {
						syncChecked = false;
						return;
					}
					const result = await getSyncStatus(token, instance.id);
					if (result.status === 'running') {
						// A sync is in progress — restore the syncing UI
						aiConfigSyncing = true;
						restoredSyncPollTimer = setInterval(async () => {
							try {
								const t = await getIdToken();
								if (!t) return;
								const r = await getSyncStatus(t, instance.id);
								if (r.status === 'completed' || r.status === 'failed') {
									aiConfigSyncing = false;
									if (restoredSyncPollTimer) {
										clearInterval(restoredSyncPollTimer);
										restoredSyncPollTimer = null;
									}
								}
							} catch {
								// ignore poll errors
							}
						}, 5000);
					}
				} catch {
					syncChecked = false;
				}
			})();
		}
		return () => {
			if (restoredSyncPollTimer) {
				clearInterval(restoredSyncPollTimer);
				restoredSyncPollTimer = null;
			}
		};
	});

	// Check API key status on load
	let configChecked = false;
	$effect(() => {
		if (instance?.status === 'active' && !configChecked) {
			configChecked = true;
			(async () => {
				try {
					const token = await getIdToken();
					if (!token) {
						configChecked = false;
						return;
					}
					const cfg = await getAgentConfig(token, instance.id);
					const hasKey = (k: string): boolean =>
						!!(
							cfg.config[k] &&
							typeof cfg.config[k] === 'string' &&
							(cfg.config[k] as string).length > 0
						);
					hasApiKey =
						hasKey('openrouter_api_key') ||
						hasKey('anthropic_api_key') ||
						hasKey('openai_api_key');
				} catch {
					configChecked = false; // allow retry on error
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
			const hasKey = (k: string): boolean =>
				!!(
					cfg.config[k] &&
					typeof cfg.config[k] === 'string' &&
					(cfg.config[k] as string).length > 0
				);
			hasApiKey =
				hasKey('openrouter_api_key') ||
				hasKey('anthropic_api_key') ||
				hasKey('openai_api_key');
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
		<p class="text-gray-400 dark:text-gray-500">Loading...</p>
	</div>
{:else if !instance}
	<div class="py-10 text-center">
		<p class="text-gray-500 dark:text-gray-400">Agent not found</p>
		<a href="/dashboard" class="mt-2 inline-block text-sm text-gray-900 dark:text-white hover:underline">Back to dashboard</a>
	</div>
{:else}
	<div>
		<a href="/dashboard" class="text-sm text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300">&larr; Back to dashboard</a>

		<div class="mt-4 flex items-start justify-between">
			<div>
				{#if editing}
					<form onsubmit={(e) => { e.preventDefault(); handleRename(); }} class="flex items-center gap-2">
						<input
							type="text"
							bind:value={editName}
							class="rounded-md border border-gray-300 dark:border-gray-600 px-2 py-1 text-xl font-bold text-gray-900 dark:text-white dark:bg-gray-800 focus:border-gray-900 dark:focus:border-gray-400 focus:outline-none focus:ring-1 focus:ring-gray-900 dark:focus:ring-gray-400"
							autofocus
							onkeydown={(e) => { if (e.key === 'Escape') editing = false; }}
						/>
						<button
							type="submit"
							disabled={saving}
							class="rounded-md bg-gray-900 dark:bg-white px-3 py-1.5 text-xs font-medium text-white dark:text-gray-900 hover:bg-gray-800 dark:hover:bg-gray-100 disabled:opacity-50"
						>
							{saving ? 'Saving...' : 'Save'}
						</button>
						<button
							type="button"
							onclick={() => (editing = false)}
							class="rounded-md border border-gray-300 dark:border-gray-600 px-3 py-1.5 text-xs text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-800"
						>
							Cancel
						</button>
					</form>
				{:else}
					<div class="flex items-center gap-2">
						<h2 class="text-xl font-bold text-gray-900 dark:text-white">{instance.name}</h2>
						<button
							onclick={startEditing}
							class="rounded-md p-1 text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-400"
							title="Rename agent"
						>
							<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="h-4 w-4">
								<path d="M2.695 14.763l-1.262 3.154a.5.5 0 00.65.65l3.155-1.262a4 4 0 001.343-.885L17.5 5.5a2.121 2.121 0 00-3-3L3.58 13.42a4 4 0 00-.885 1.343z" />
							</svg>
						</button>
					</div>
				{/if}
				<p class="mt-0.5 text-sm text-gray-500 dark:text-gray-400">Dedicated Agent</p>
			</div>
			<StatusBadge status={instance.status} updateStatus={instance.openclaw_update_status} />
		</div>

		{#if $dashboardState?.subscription?.cancel_at_period_end}
			<div class="mt-4 rounded-lg border border-orange-200 dark:border-orange-800 bg-orange-50 dark:bg-orange-900/20 p-3 text-sm text-orange-700 dark:text-orange-400">
				Your subscription is canceling. This agent and all snapshots will be deleted on
				{new Date($dashboardState.subscription.current_period_end).toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' })}.
				<a href="/dashboard/billing" class="font-medium underline">Manage billing</a> to undo.
			</div>
		{/if}

		{#if actionError}
			<div class="mt-4 rounded-lg border border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20 p-3 text-sm text-red-700 dark:text-red-400">
				{actionError}
			</div>
		{/if}

		{#if restoreResult === 'success'}
			<div class="mt-4 flex items-center justify-between rounded-lg border border-green-200 dark:border-green-800 bg-green-50 dark:bg-green-900/20 p-3 text-sm text-green-700 dark:text-green-400">
				<span>Snapshot restored successfully. A new root password has been generated.</span>
				<button onclick={() => (restoreResult = null)} class="ml-3 text-green-500 hover:text-green-700 dark:hover:text-green-400">&times;</button>
			</div>
		{:else if restoreResult === 'failed'}
			<div class="mt-4 flex items-center justify-between rounded-lg border border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20 p-3 text-sm text-red-700 dark:text-red-400">
				<span>Snapshot restore failed. Your agent may need attention.</span>
				<button onclick={() => (restoreResult = null)} class="ml-3 text-red-500 hover:text-red-700 dark:hover:text-red-400">&times;</button>
			</div>
		{/if}

		{#if snapshotResult === 'success'}
			<div class="mt-4 flex items-center justify-between rounded-lg border border-green-200 dark:border-green-800 bg-green-50 dark:bg-green-900/20 p-3 text-sm text-green-700 dark:text-green-400">
				<span>Snapshot created successfully.</span>
				<button onclick={() => (snapshotResult = null)} class="ml-3 text-green-500 hover:text-green-700 dark:hover:text-green-400">&times;</button>
			</div>
		{:else if snapshotResult === 'failed'}
			<div class="mt-4 flex items-center justify-between rounded-lg border border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20 p-3 text-sm text-red-700 dark:text-red-400">
				<span>Snapshot creation failed.</span>
				<button onclick={() => (snapshotResult = null)} class="ml-3 text-red-500 hover:text-red-700 dark:hover:text-red-400">&times;</button>
			</div>
		{/if}

		{#if isSettingUp}
			<div class="mt-12 flex justify-center">
				<div class="w-full max-w-md">
					<div class="rounded-xl border border-gray-200 dark:border-gray-700 p-8 text-center">
						{#if instance.status === 'error'}
							<div class="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-red-100 dark:bg-red-900/30">
								<svg class="h-6 w-6 text-red-600 dark:text-red-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
									<path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126z" />
								</svg>
							</div>
							<h3 class="text-lg font-semibold text-gray-900 dark:text-white">Setup Failed</h3>
							<p class="mt-2 text-sm text-gray-500 dark:text-gray-400">Something went wrong while setting up your agent. Our team has been notified and is looking into it.</p>
						{:else}
							<div class="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-gray-100 dark:bg-gray-800">
								<svg class="h-6 w-6 animate-spin text-gray-600 dark:text-gray-400" viewBox="0 0 24 24" fill="none">
									<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
									<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
								</svg>
							</div>
							<h3 class="text-lg font-semibold text-gray-900 dark:text-white">Setting up your agent</h3>
							<p class="mt-2 text-sm text-gray-500 dark:text-gray-400">This usually takes 7 minutes</p>

							<div class="mt-6 text-left">
								{#if isProvisioning}
									<ProvisioningProgress currentStep={instance.step} />
								{:else if activationCooloff}
									<!-- All provisioning steps completed, waiting for OpenClaw health -->
									<div class="space-y-3">
										{#each ['Selecting provider', 'Creating server', 'Waiting for server', 'Bootstrapping OS', 'Installing agent', 'Activating'] as label}
											<div class="flex items-center gap-3">
												<div class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-green-500">
													<svg class="h-3.5 w-3.5 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
														<path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
													</svg>
												</div>
												<span class="text-sm text-gray-500 dark:text-gray-400">{label}</span>
											</div>
										{/each}
										<div class="flex items-center gap-3">
											<div class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full border-2 border-yellow-500 bg-yellow-50 dark:bg-yellow-900/20">
												<svg class="h-3.5 w-3.5 animate-spin text-yellow-600 dark:text-yellow-400" viewBox="0 0 24 24" fill="none">
													<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
													<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
												</svg>
											</div>
											<span class="text-sm font-medium text-gray-900 dark:text-white">Starting {instance.framework === 'hermes' ? 'Hermes' : 'OpenClaw'}</span>
										</div>
									</div>
								{/if}
							</div>

							<p class="mt-6 text-xs text-gray-400 dark:text-gray-500">
								{Math.floor(setupElapsed / 60)}:{String(setupElapsed % 60).padStart(2, '0')} elapsed
							</p>
						{/if}
					</div>
				</div>
			</div>
		{:else}
		<div class="mt-8 grid grid-cols-1 lg:grid-cols-2 gap-8">
			<!-- Left column -->
			<div class="space-y-6">
				<div class="rounded-xl border border-gray-200 dark:border-gray-700 p-5">
					<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Agent Details</h3>
					<dl class="mt-4 space-y-3 text-sm">
						<div class="flex justify-between">
							<dt class="text-gray-500 dark:text-gray-400">IP Address</dt>
							<dd class="font-mono text-gray-900 dark:text-white">{instance.ipv4 ?? '—'}</dd>
						</div>
						<div class="flex justify-between">
							<dt class="text-gray-500 dark:text-gray-400">{instance.framework === 'hermes' ? 'Hermes' : 'OpenClaw'}</dt>
							<dd>
								{#if isProvisioning || activationCooloff}
									<span class="inline-flex items-center gap-1.5 text-gray-500 dark:text-gray-400">
										<svg class="h-3.5 w-3.5 animate-spin text-gray-400 dark:text-gray-500" viewBox="0 0 24 24" fill="none">
											<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
											<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
										</svg>
										Setting up…
									</span>
								{:else if isConfigSyncing || recentlyRestarted}
									<span class="inline-flex items-center gap-1.5 text-blue-700 dark:text-blue-400">
										<svg class="h-3.5 w-3.5 animate-spin text-blue-500 dark:text-blue-400" viewBox="0 0 24 24" fill="none">
											<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
											<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
										</svg>
										Applying Config
									</span>
								{:else if instance.agent_status === 'running'}
									{#if instance.agent_error}
										<span class="inline-flex items-center gap-1.5 text-amber-700 dark:text-amber-400">
											<span class="h-1.5 w-1.5 rounded-full bg-amber-500"></span>
											Degraded
										</span>
									{:else}
										<span class="inline-flex items-center gap-1.5 text-green-700 dark:text-green-400">
											<span class="h-1.5 w-1.5 rounded-full bg-green-500"></span>
											Running
										</span>
									{/if}
								{:else if instance.agent_status === 'unhealthy'}
									{#if !instance.last_heartbeat_at}
										<span class="inline-flex items-center gap-1.5 text-gray-500 dark:text-gray-400">
											<svg class="h-3.5 w-3.5 animate-spin text-gray-400 dark:text-gray-500" viewBox="0 0 24 24" fill="none">
												<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
												<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
											</svg>
											Starting
										</span>
									{:else}
										<span class="inline-flex items-center gap-1.5 text-yellow-700 dark:text-yellow-400">
											<span class="h-1.5 w-1.5 rounded-full bg-yellow-500"></span>
											Unhealthy
										</span>
									{/if}
								{:else if instance.agent_status === 'stopped' || instance.agent_status === 'not_found'}
									<span class="inline-flex items-center gap-1.5 text-red-700 dark:text-red-400">
										<span class="h-1.5 w-1.5 rounded-full bg-red-500"></span>
										Stopped
									</span>
								{:else}
									<span class="text-gray-400 dark:text-gray-500">—</span>
								{/if}
							</dd>
						</div>
						{#if instance.framework === 'openclaw' && instance.openclaw_version}
							<div class="flex justify-between">
								<dt class="text-gray-500 dark:text-gray-400">Version</dt>
								<dd class="font-mono text-gray-900 dark:text-white">{instance.openclaw_version}</dd>
							</div>
						{/if}
						<div class="flex justify-between">
							<dt class="text-gray-500 dark:text-gray-400">Last Heartbeat</dt>
							<dd class="text-gray-900 dark:text-white">{timeAgo(instance.last_heartbeat_at)}</dd>
						</div>
						<div class="flex justify-between">
							<dt class="text-gray-500 dark:text-gray-400">Created</dt>
							<dd class="text-gray-900 dark:text-white">{new Date(instance.created_at).toLocaleDateString()}</dd>
						</div>
					</dl>

					{#if instance.status === 'active' && instance.agent_status === 'running' && !isConfigSyncing && instance.dashboard_url}
						{#if hasApiKey}
							{#if instance.framework === 'hermes'}
								<a
									href="/dashboard/instances/{instance.id}/chat"
									target="_blank"
									rel="noopener"
									class="mt-4 inline-flex items-center gap-1.5 rounded-lg bg-gray-900 dark:bg-white px-3 py-1.5 text-xs font-medium text-white dark:text-gray-900 transition-colors hover:bg-gray-800 dark:hover:bg-gray-100"
								>
									Open Agent Dashboard
								</a>
							{:else}
								<button
									onclick={async () => {
										dashboardBtnLoading = true;
										try {
											const [, result] = await Promise.all([
												new Promise(r => setTimeout(r, 2000)),
												(async () => {
													const token = await getIdToken();
													if (!token) return null;
													const { token: scopedToken } = await getDashboardToken(instance!.id, token);
													return scopedToken;
												})()
											]);
											if (result) {
												window.open(`${instance!.dashboard_url!}/#token=${result}`, '_blank');
											}
										} catch {
											alert('Failed to generate dashboard token. Please try again.');
										} finally {
											dashboardBtnLoading = false;
										}
									}}
									disabled={dashboardBtnLoading}
									class="mt-4 inline-flex items-center gap-1.5 rounded-lg bg-gray-900 dark:bg-white px-3 py-1.5 text-xs font-medium text-white dark:text-gray-900 transition-colors hover:bg-gray-800 dark:hover:bg-gray-100 disabled:opacity-70"
								>
									{#if dashboardBtnLoading}
										<svg class="h-3 w-3 animate-spin" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
											<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
											<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
										</svg>
										Opening...
									{:else}
										Open Agent Dashboard
									{/if}
								</button>
							{/if}
						{:else}
							<p class="mt-4 text-xs text-gray-400 dark:text-gray-500">Set up your OpenRouter API key below to access the dashboard.</p>
						{/if}
					{/if}
				</div>

				{#if instance.status === 'active' || instance.status === 'restarting' || instance.status === 'snapshotting' || instance.status === 'restoring'}
					<AIProviderConfig instanceId={instance.id} disabled={instance.status !== 'active'} onsaved={recheckConfig} onsyncchange={(s) => aiConfigSyncing = s} />

					<!-- Codex (ChatGPT) — OpenClaw only -->
					{#if instance.framework !== 'hermes'}
					<CodexConnect instance={instance} />
					{/if}

					<!-- Google Account -->
					{#if featureFlags.googleWorkspace}
					<GoogleConnect
						instanceId={instance.id}
						instanceStatus={instance.status}
						onSyncStart={() => { aiConfigSyncing = true; }}
						onSyncEnd={() => { aiConfigSyncing = false; }}
						onStatusChange={(connected) => { googleConnected = connected; }}
						connectRequestCount={googleConnectRequests}
					/>
					{/if}

					{#if instance.ipv4}
						<div class="rounded-xl border border-gray-200 dark:border-gray-700">
							<button
								onclick={() => (powerUserOpen = !powerUserOpen)}
								class="flex w-full items-center justify-between p-5"
							>
								<div class="text-left">
									<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Power User</h3>
									<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">Advanced settings and SSH connection</p>
								</div>
								<svg
									xmlns="http://www.w3.org/2000/svg"
									viewBox="0 0 20 20"
									fill="currentColor"
									class="h-5 w-5 text-gray-400 dark:text-gray-500 transition-transform {powerUserOpen ? 'rotate-180' : ''}"
								>
									<path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
								</svg>
							</button>

							{#if powerUserOpen}
								<div class="space-y-4 border-t border-gray-200 dark:border-gray-700 p-5">
									<div class="flex gap-3">
										<button
											onclick={handleRestart}
											disabled={restarting || restartCooloff || instance.status !== 'active'}
											class="rounded-lg border border-gray-300 dark:border-gray-600 px-4 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-50"
										>
											{instance.status === 'restarting' ? 'Restarting...' : restarting ? 'Restarting...' : restartCooloff ? `Restart (${restartCooloffRemaining}s)` : 'Restart'}
										</button>
										{#if instance.status === 'active'}
											<a
												href="/dashboard/instances/{instance.id}/terminal"
												class="rounded-lg border border-gray-300 dark:border-gray-600 px-4 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800"
											>
												Open Terminal
											</a>
										{/if}
										{#if featureFlags.healthCheck}
										<button
											onclick={handleRunDoctor}
											disabled={doctorRunning || instance.status !== 'active'}
											class="rounded-lg border border-gray-300 dark:border-gray-600 px-4 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-50"
										>
											{doctorRunning ? 'Checking...' : 'Health Check'}
										</button>
										{/if}
									</div>

									{#if featureFlags.healthCheck && (doctorChecks !== null || doctorRaw !== null || doctorError !== null)}
										<div class="rounded-lg border border-gray-200 dark:border-gray-700 p-4">
											<div class="flex items-center justify-between">
												<h4 class="text-sm font-medium text-gray-900 dark:text-white">Health Check Results</h4>
												<button
													onclick={() => { doctorChecks = null; doctorRaw = null; doctorError = null; }}
													class="text-xs text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-400"
												>Dismiss</button>
											</div>
											{#if doctorError}
												<div class="mt-3 rounded-lg border border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20 p-3 text-xs text-red-700 dark:text-red-400">
													{doctorError}
												</div>
											{:else if doctorChecks}
												<div class="mt-3 space-y-1.5">
													{#each doctorChecks as check (check.name)}
														<div class="flex items-start gap-2.5 rounded-lg px-3 py-2 {check.status === 'fail' ? 'bg-red-50 dark:bg-red-900/20' : check.status === 'warn' ? 'bg-yellow-50 dark:bg-yellow-900/20' : ''}">
															<span class="mt-0.5 shrink-0 text-sm">
																{#if check.status === 'pass'}
																	<span class="text-green-600 dark:text-green-400">&#10003;</span>
																{:else if check.status === 'fail'}
																	<span class="text-red-600 dark:text-red-400">&#10007;</span>
																{:else if check.status === 'warn'}
																	<span class="text-yellow-600 dark:text-yellow-400">&#9888;</span>
																{:else}
																	<span class="text-gray-400 dark:text-gray-500">&#8226;</span>
																{/if}
															</span>
															<div class="min-w-0 flex-1">
																<div class="flex items-baseline gap-2">
																	<span class="text-xs font-medium text-gray-500 dark:text-gray-400">{check.name}</span>
																	<span class="text-xs {check.status === 'fail' ? 'text-red-700 dark:text-red-400 font-medium' : check.status === 'warn' ? 'text-yellow-700 dark:text-yellow-400' : 'text-gray-700 dark:text-gray-300'}">{check.message}</span>
																</div>
																{#if check.detail}
																	<p class="mt-0.5 text-xs text-gray-400 dark:text-gray-500 whitespace-pre-wrap break-words">{check.detail}</p>
																{/if}
															</div>
														</div>
													{/each}
												</div>
												{#if doctorChecks.some(c => c.status === 'fail')}
													<div class="mt-3 border-t border-gray-100 dark:border-gray-800 pt-3 text-xs text-gray-500 dark:text-gray-400">
														Issues found. Try <button onclick={handleRestart} class="font-medium text-gray-700 dark:text-gray-300 hover:underline">restarting your agent</button> if problems persist.
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

					{#if !isConfigSyncing && !isProvisioning && !activationCooloff && instance.agent_error}
						<div class="rounded-lg border border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20 p-3 text-sm text-red-800 dark:text-red-400">
							{#if instance.agent_error === 'openrouter_credits_exhausted'}
								<p class="font-medium">OpenRouter API credits exhausted</p>
								<p class="mt-1">Your OpenRouter API key has hit its credit limit. <a href="https://openrouter.ai/settings/credits" target="_blank" rel="noopener" class="underline font-medium hover:text-red-900 dark:hover:text-red-300">Top up your credits</a> to resume.</p>
							{:else if instance.agent_error === 'invalid_api_key'}
								<p class="font-medium">Invalid API key</p>
								<p class="mt-1">Your AI provider API key appears invalid. Update it in Settings below.</p>
							{:else}
								<p>{instance.agent_error}</p>
							{/if}
						</div>
					{/if}

					{#if !isConfigSyncing && !isProvisioning && !activationCooloff && !recentlyRestarted && instance.last_heartbeat_at && (instance.agent_status === 'unhealthy' || instance.agent_status === 'stopped' || instance.agent_status === 'not_found')}
						<div class="rounded-lg border border-yellow-200 dark:border-yellow-800 bg-yellow-50 dark:bg-yellow-900/20 p-3 text-sm text-yellow-800 dark:text-yellow-400 flex items-center justify-between">
							{#if instance.agent_status === 'stopped' || instance.agent_status === 'not_found'}
								<span>Your agent appears stopped. Restart to bring it back online.</span>
								<button
									onclick={handleRestart}
									disabled={restarting || restartCooloff || instance.status !== 'active'}
									class="ml-3 shrink-0 rounded-md bg-yellow-600 px-3 py-1 text-xs font-medium text-white hover:bg-yellow-700 disabled:opacity-50"
								>
									{restarting ? 'Restarting...' : restartCooloff ? `Restart (${restartCooloffRemaining}s)` : 'Restart'}
								</button>
							{:else}
								<span>Your agent appears unhealthy. Try restarting to resolve the issue.</span>
								<button
									onclick={handleRestart}
									disabled={restarting || restartCooloff || instance.status !== 'active'}
									class="ml-3 shrink-0 rounded-md bg-yellow-600 px-3 py-1 text-xs font-medium text-white hover:bg-yellow-700 disabled:opacity-50"
								>
									{restarting ? 'Restarting...' : restartCooloff ? `Restart (${restartCooloffRemaining}s)` : 'Restart'}
								</button>
							{/if}
						</div>
					{/if}

				{/if}
			</div>

			<!-- Right column -->
			<div class="space-y-6">
				{#if isProvisioning}
					<div class="rounded-xl border border-gray-200 dark:border-gray-700 p-5">
						<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Provisioning Progress</h3>
						<div class="mt-4">
							<ProvisioningProgress currentStep={instance.step} />
						</div>
					</div>
				{/if}

				{#if instance.status === 'restoring'}
					<div class="rounded-xl border border-blue-300 dark:border-blue-800 bg-blue-50 dark:bg-blue-900/20 p-5">
						<div class="flex items-center gap-3">
							<svg class="h-5 w-5 animate-spin text-blue-600 dark:text-blue-400 shrink-0" viewBox="0 0 24 24" fill="none">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
								<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
							</svg>
							<div>
								<h3 class="text-sm font-semibold text-blue-900 dark:text-blue-300">Restoring from Snapshot</h3>
								<p class="mt-1 text-xs text-blue-700 dark:text-blue-400">Your agent is being rebuilt from a snapshot. This may take a few minutes. A new root password will be generated.</p>
							</div>
						</div>
					</div>
				{/if}

				{#if instance.status === 'snapshotting'}
					<div class="rounded-xl border border-blue-300 dark:border-blue-800 bg-blue-50 dark:bg-blue-900/20 p-5">
						<div class="flex items-center gap-3">
							<svg class="h-5 w-5 animate-spin text-blue-600 dark:text-blue-400 shrink-0" viewBox="0 0 24 24" fill="none">
								<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
								<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
							</svg>
							<div>
								<h3 class="text-sm font-semibold text-blue-900 dark:text-blue-300">Creating Snapshot</h3>
								<p class="mt-1 text-xs text-blue-700 dark:text-blue-400">A snapshot of your agent's current state is being created. This may take a few minutes.</p>
							</div>
						</div>
					</div>
				{/if}

				{#if instance.status === 'active' && instance.agent_status === 'running' && !isConfigSyncing && instance.dashboard_url && instance.framework !== 'hermes'}
					<MagicMoment
						previewUrl={instance.preview_url}
						googleConnected={googleConnected}
						onConnectGoogle={() => { googleConnectRequests++; }}
						showGoogle={featureFlags.googleWorkspace}
					/>
				{/if}

				{#if instance.status === 'active' || instance.status === 'snapshotting' || instance.status === 'restoring' || instance.status === 'restarting'}
					<div class="rounded-xl border border-gray-200 dark:border-gray-700 p-5">
						<div class="flex items-center justify-between">
							<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Snapshots</h3>
							<span class="text-xs text-gray-400 dark:text-gray-500">{instanceSnapshots.length}/3 used</span>
						</div>
						<p class="mt-1 text-xs text-gray-400 dark:text-gray-500">Create backups of your agent's disk state</p>

						{#if instanceSnapshots.length > 0}
							<div class="mt-4 space-y-3">
								{#each instanceSnapshots as snap (snap.id)}
									<div class="flex items-center justify-between rounded-lg border border-gray-100 dark:border-gray-800 px-3 py-2.5">
										<div class="min-w-0 flex-1">
											<p class="text-sm font-medium text-gray-900 dark:text-white truncate">{snap.name}</p>
											<p class="text-xs text-gray-400 dark:text-gray-500">
												{new Date(snap.created_at).toLocaleDateString()}
												{#if snap.size_gb}
													&middot; {snap.size_gb.toFixed(1)} GB
												{/if}
												{#if snap.status === 'creating'}
													<span class="ml-1 text-blue-600 dark:text-blue-400">Creating...</span>
												{:else if snap.status === 'deleting'}
													<span class="ml-1 text-gray-500 dark:text-gray-400">Deleting...</span>
												{:else if snap.status === 'error'}
													<span class="ml-1 text-red-600 dark:text-red-400">Error</span>
												{/if}
											</p>
										</div>
										{#if snap.status === 'ready'}
											<div class="flex items-center gap-1.5 ml-3">
												{#if confirmRestoreId === snap.id}
													<span class="text-xs text-gray-500 dark:text-gray-400 mr-1">Are you sure?</span>
													<button
														onclick={() => handleRestoreSnapshot(snap.id)}
														disabled={restoringSnapshotId === snap.id}
														class="rounded-md bg-gray-900 dark:bg-white px-2.5 py-1 text-xs font-medium text-white dark:text-gray-900 hover:bg-gray-800 dark:hover:bg-gray-100 disabled:opacity-50"
													>
														Confirm
													</button>
													<button
														onclick={() => (confirmRestoreId = null)}
														class="rounded-md border border-gray-300 dark:border-gray-600 px-2.5 py-1 text-xs text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-800"
													>
														Cancel
													</button>
												{:else if confirmDeleteId === snap.id}
													<div class="flex flex-col items-end gap-1.5">
														<span class="text-xs text-gray-500 dark:text-gray-400">Type <strong>{snap.name}</strong> to confirm</span>
														<input
															type="text"
															bind:value={confirmDeleteInput}
															placeholder={snap.name}
															class="w-40 rounded-md border border-gray-300 dark:border-gray-600 px-2 py-1 text-xs text-gray-900 dark:text-white dark:bg-gray-800 focus:border-red-500 focus:outline-none focus:ring-1 focus:ring-red-500"
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
																class="rounded-md border border-gray-300 dark:border-gray-600 px-2.5 py-1 text-xs text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-800"
															>
																Cancel
															</button>
														</div>
													</div>
												{:else}
													<button
														onclick={() => (confirmRestoreId = snap.id)}
														disabled={instance.status !== 'active'}
														class="rounded-md border border-gray-300 dark:border-gray-600 px-2.5 py-1 text-xs text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-50"
														title="Restore agent from this snapshot"
													>
														Restore
													</button>
													<button
														onclick={() => promptDeleteSnapshot(snap.id)}
														disabled={deletingSnapshotId === snap.id || isBusy}
														class="rounded-md border border-gray-300 dark:border-gray-600 px-2.5 py-1 text-xs text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50"
														title="Delete this snapshot"
													>
														{deletingSnapshotId === snap.id ? 'Deleting...' : 'Delete'}
													</button>
												{/if}
											</div>
										{:else if snap.status === 'creating' || snap.status === 'deleting'}
											<div class="ml-3">
												<svg class="h-4 w-4 animate-spin text-gray-400 dark:text-gray-500" viewBox="0 0 24 24" fill="none">
													<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
													<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
												</svg>
											</div>
										{/if}
									</div>
								{/each}
							</div>
						{:else}
							<p class="mt-4 text-xs text-gray-400 dark:text-gray-500">No snapshots yet</p>
						{/if}

						<div class="mt-4 border-t border-gray-100 dark:border-gray-800 pt-3">
							{#if showSnapshotForm}
								<form onsubmit={(e) => { e.preventDefault(); handleCreateSnapshot(); }} class="flex items-center gap-2">
									<input
										type="text"
										bind:value={snapshotName}
										placeholder="Snapshot name"
										class="flex-1 rounded-md border border-gray-300 dark:border-gray-600 px-2.5 py-1.5 text-sm text-gray-900 dark:text-white dark:bg-gray-800 focus:border-gray-900 dark:focus:border-gray-400 focus:outline-none focus:ring-1 focus:ring-gray-900 dark:focus:ring-gray-400"
										autofocus
										onkeydown={(e) => { if (e.key === 'Escape') { showSnapshotForm = false; snapshotName = ''; } }}
									/>
									<button
										type="submit"
										disabled={creatingSnapshot || !snapshotName.trim()}
										class="rounded-md bg-gray-900 dark:bg-white px-3 py-1.5 text-xs font-medium text-white dark:text-gray-900 hover:bg-gray-800 dark:hover:bg-gray-100 disabled:opacity-50"
									>
										{creatingSnapshot ? 'Creating...' : 'Create'}
									</button>
									<button
										type="button"
										onclick={() => { showSnapshotForm = false; snapshotName = ''; }}
										class="rounded-md border border-gray-300 dark:border-gray-600 px-3 py-1.5 text-xs text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-800"
									>
										Cancel
									</button>
								</form>
							{:else}
								<button
									onclick={() => (showSnapshotForm = true)}
									disabled={instanceSnapshots.length >= 3 || instance.status !== 'active'}
									class="text-sm text-gray-900 dark:text-white hover:text-gray-700 dark:hover:text-gray-300 disabled:opacity-50 disabled:cursor-not-allowed"
								>
									+ Create Snapshot
								</button>
							{/if}
						</div>
					</div>
				{/if}
			</div>
		</div>
		{/if}
	</div>
{/if}
