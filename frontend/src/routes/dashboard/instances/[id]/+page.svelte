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
		deleteSnapshot
	} from '$lib/api/client';
	import StatusBadge from '$lib/components/StatusBadge.svelte';
	import ProvisioningProgress from '$lib/components/ProvisioningProgress.svelte';

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
	let showSnapshotForm = $state(false);
	let snapshotName = $state('');
	let creatingSnapshot = $state(false);
	let restoringSnapshotId = $state<string | null>(null);
	let deletingSnapshotId = $state<string | null>(null);
	let confirmRestoreId = $state<string | null>(null);

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
					{#if instance.ipv4}
						<div class="rounded-xl border border-gray-200 p-5">
							<h3 class="text-sm font-semibold text-gray-900">SSH Access</h3>
							<p class="mt-1 text-xs text-gray-400">Connect to your agent's server via SSH</p>
							<dl class="mt-4 space-y-3 text-sm">
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
