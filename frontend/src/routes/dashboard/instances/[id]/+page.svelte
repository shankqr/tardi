<script lang="ts">
	import { page } from '$app/state';
	import { dashboardState, dashboardLoading, refreshDashboard } from '$lib/stores/dashboard';
	import { getIdToken } from '$lib/stores/auth';
	import { restartInstance } from '$lib/api/client';
	import { mockSnapshots } from '$lib/api/mock';
	import type { Snapshot } from '$lib/types';
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

	// Action state
	let restarting = $state(false);
	let actionError = $state<string | null>(null);

	// Snapshots
	let snapshots = $state<Snapshot[]>([...mockSnapshots]);
	let snapshotName = $state('');
	let takingSnapshot = $state(false);
	let restoringId = $state<string | null>(null);
	let confirmRestoreId = $state<string | null>(null);

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

	async function handleTakeSnapshot() {
		if (!snapshotName.trim() || !instance) return;
		takingSnapshot = true;
		// TODO: Call backend when snapshot API is ready
		await new Promise((r) => setTimeout(r, 1000));
		snapshots = [
			...snapshots,
			{
				id: `snap-${Date.now()}`,
				instance_id: instance.id,
				name: snapshotName.trim(),
				created_at: new Date().toISOString(),
				size_gb: +(Math.random() * 3 + 1).toFixed(1)
			}
		];
		snapshotName = '';
		takingSnapshot = false;
	}

	async function handleRestore(snapshotId: string) {
		restoringId = snapshotId;
		// TODO: Call backend when snapshot API is ready
		await new Promise((r) => setTimeout(r, 1500));
		restoringId = null;
		confirmRestoreId = null;
	}

	function timeAgo(dateStr: string | null): string {
		if (!dateStr) return 'Never';
		const seconds = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
		if (seconds < 60) return `${seconds}s ago`;
		if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
		if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
		return `${Math.floor(seconds / 86400)}d ago`;
	}

	function formatDate(dateStr: string): string {
		return new Date(dateStr).toLocaleDateString('en-US', {
			month: 'short',
			day: 'numeric',
			year: 'numeric',
			hour: '2-digit',
			minute: '2-digit'
		});
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
				<h2 class="text-xl font-bold text-gray-900">{instance.name}</h2>
				<p class="mt-0.5 text-sm text-gray-500">Dedicated Agent</p>
			</div>
			<StatusBadge status={instance.status} />
		</div>

		{#if actionError}
			<div class="mt-4 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700">
				{actionError}
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

				{#if instance.status === 'active'}
					<div class="flex gap-3">
						<button
							onclick={handleRestart}
							disabled={restarting}
							class="rounded-lg border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50"
						>
							{restarting ? 'Restarting...' : 'Restart'}
						</button>
					</div>

					<!-- Messaging links -->
					<div class="rounded-xl border border-gray-200 p-5">
						<h3 class="text-sm font-semibold text-gray-900">Chat with your Agent</h3>
						<p class="mt-1 text-xs text-gray-400">Open your agent's bot on these platforms</p>
						<div class="mt-4 flex gap-3">
							<a
								href="https://wa.me/1234567890"
								target="_blank"
								rel="noopener noreferrer"
								class="flex items-center gap-2 rounded-lg border border-gray-300 px-4 py-2.5 text-sm font-medium text-gray-700 hover:bg-gray-50"
							>
								<svg class="h-5 w-5 text-[#25D366]" viewBox="0 0 24 24" fill="currentColor">
									<path d="M17.472 14.382c-.297-.149-1.758-.867-2.03-.967-.273-.099-.471-.148-.67.15-.197.297-.767.966-.94 1.164-.173.199-.347.223-.644.075-.297-.15-1.255-.463-2.39-1.475-.883-.788-1.48-1.761-1.653-2.059-.173-.297-.018-.458.13-.606.134-.133.298-.347.446-.52.149-.174.198-.298.298-.497.099-.198.05-.371-.025-.52-.075-.149-.669-1.612-.916-2.207-.242-.579-.487-.5-.669-.51-.173-.008-.371-.01-.57-.01-.198 0-.52.074-.792.372-.272.297-1.04 1.016-1.04 2.479 0 1.462 1.065 2.875 1.213 3.074.149.198 2.096 3.2 5.077 4.487.709.306 1.262.489 1.694.625.712.227 1.36.195 1.871.118.571-.085 1.758-.719 2.006-1.413.248-.694.248-1.289.173-1.413-.074-.124-.272-.198-.57-.347m-5.421 7.403h-.004a9.87 9.87 0 01-5.031-1.378l-.361-.214-3.741.982.998-3.648-.235-.374a9.86 9.86 0 01-1.51-5.26c.001-5.45 4.436-9.884 9.888-9.884 2.64 0 5.122 1.03 6.988 2.898a9.825 9.825 0 012.893 6.994c-.003 5.45-4.437 9.884-9.885 9.884m8.413-18.297A11.815 11.815 0 0012.05 0C5.495 0 .16 5.335.157 11.892c0 2.096.547 4.142 1.588 5.945L.057 24l6.305-1.654a11.882 11.882 0 005.683 1.448h.005c6.554 0 11.89-5.335 11.893-11.893a11.821 11.821 0 00-3.48-8.413z"/>
								</svg>
								WhatsApp
							</a>
							<a
								href="https://t.me/your_agent_bot"
								target="_blank"
								rel="noopener noreferrer"
								class="flex items-center gap-2 rounded-lg border border-gray-300 px-4 py-2.5 text-sm font-medium text-gray-700 hover:bg-gray-50"
							>
								<svg class="h-5 w-5 text-[#2AABEE]" viewBox="0 0 24 24" fill="currentColor">
									<path d="M11.944 0A12 12 0 000 12a12 12 0 0012 12 12 12 0 0012-12A12 12 0 0012 0a12 12 0 00-.056 0zm4.962 7.224c.1-.002.321.023.465.14a.506.506 0 01.171.325c.016.093.036.306.02.472-.18 1.898-.962 6.502-1.36 8.627-.168.9-.499 1.201-.82 1.23-.696.065-1.225-.46-1.9-.902-1.056-.693-1.653-1.124-2.678-1.8-1.185-.78-.417-1.21.258-1.91.177-.184 3.247-2.977 3.307-3.23.007-.032.014-.15-.056-.212s-.174-.041-.249-.024c-.106.024-1.793 1.14-5.061 3.345-.479.33-.913.492-1.302.48-.428-.012-1.252-.242-1.865-.442-.751-.245-1.349-.374-1.297-.789.027-.216.325-.437.893-.663 3.498-1.524 5.83-2.529 6.998-3.014 3.332-1.386 4.025-1.627 4.476-1.635z"/>
								</svg>
								Telegram
							</a>
						</div>
					</div>
				{/if}
			</div>

			<!-- Right column -->
			<div>
				{#if isProvisioning}
					<div class="rounded-xl border border-gray-200 p-5">
						<h3 class="text-sm font-semibold text-gray-900">Provisioning Progress</h3>
						<div class="mt-4">
							<ProvisioningProgress currentStep={instance.step} />
						</div>
					</div>
				{:else if instance.status === 'active'}
					<div class="rounded-xl border border-gray-200 p-5">
						<h3 class="text-sm font-semibold text-gray-900">Snapshots</h3>
						<p class="mt-1 text-xs text-gray-400">Save and restore your agent's state</p>

						<!-- Take snapshot -->
						<form
							onsubmit={(e) => { e.preventDefault(); handleTakeSnapshot(); }}
							class="mt-4 flex gap-2"
						>
							<input
								type="text"
								bind:value={snapshotName}
								placeholder="Snapshot name"
								required
								class="flex-1 rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900"
							/>
							<button
								type="submit"
								disabled={takingSnapshot}
								class="rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
							>
								{takingSnapshot ? 'Saving...' : 'Take'}
							</button>
						</form>

						<!-- Snapshot list -->
						{#if snapshots.length === 0}
							<p class="mt-6 text-center text-sm text-gray-400">No snapshots yet</p>
						{:else}
							<ul class="mt-4 space-y-3">
								{#each snapshots as snapshot (snapshot.id)}
									<li class="rounded-lg border border-gray-100 bg-gray-50 p-3">
										<div class="flex items-start justify-between">
											<div>
												<p class="text-sm font-medium text-gray-900">{snapshot.name}</p>
												<p class="mt-0.5 text-xs text-gray-400">
													{formatDate(snapshot.created_at)} &middot; {snapshot.size_gb} GB
												</p>
											</div>
											{#if confirmRestoreId === snapshot.id}
												<div class="flex gap-2">
													<button
														onclick={() => handleRestore(snapshot.id)}
														disabled={restoringId === snapshot.id}
														class="rounded-md bg-red-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-red-700 disabled:opacity-50"
													>
														{restoringId === snapshot.id ? 'Restoring...' : 'Confirm'}
													</button>
													<button
														onclick={() => (confirmRestoreId = null)}
														class="rounded-md border border-gray-300 px-2.5 py-1 text-xs text-gray-600 hover:bg-gray-100"
													>
														Cancel
													</button>
												</div>
											{:else}
												<button
													onclick={() => (confirmRestoreId = snapshot.id)}
													class="rounded-md border border-gray-300 px-2.5 py-1 text-xs text-gray-600 hover:bg-gray-100"
												>
													Restore
												</button>
											{/if}
										</div>
									</li>
								{/each}
							</ul>
						{/if}
					</div>
				{/if}
			</div>
		</div>
	</div>
{/if}
