<script lang="ts">
	import { page } from '$app/state';
	import { dashboardState, dashboardLoading, refreshDashboard } from '$lib/stores/dashboard';
	import { getIdToken } from '$lib/stores/auth';
	import { restartInstance, resetPassword } from '$lib/api/client';
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
	let resettingPassword = $state(false);
	let actionError = $state<string | null>(null);
	let showPassword = $state(false);

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

				{#if instance.status === 'active' || instance.status === 'restarting'}
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
							disabled={restarting || instance.status === 'restarting'}
							class="rounded-lg border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50"
						>
							{instance.status === 'restarting' ? 'Restarting...' : restarting ? 'Restarting...' : 'Restart'}
						</button>
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
				{/if}
			</div>
		</div>
	</div>
{/if}
