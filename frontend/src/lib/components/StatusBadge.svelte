<script lang="ts">
	import type { VpsStatus } from '$lib/types';

	interface Props {
		status: VpsStatus;
		updateStatus?: string | null;
	}

	let { status, updateStatus }: Props = $props();

	const isUpdatingVersion = $derived(
		status === 'active' && !!updateStatus && ['pulling', 'updating'].includes(updateStatus)
	);

	const colors: Record<VpsStatus, string> = {
		requested: 'bg-gray-100 text-gray-700',
		provisioning: 'bg-yellow-100 text-yellow-700',
		bootstrapping: 'bg-yellow-100 text-yellow-700',
		installing_agent: 'bg-yellow-100 text-yellow-700',
		active: 'bg-green-100 text-green-700',
		restarting: 'bg-blue-100 text-blue-700',
		snapshotting: 'bg-blue-100 text-blue-700',
		restoring: 'bg-yellow-100 text-yellow-700',
		upgrading: 'bg-blue-100 text-blue-700',
		downgrading: 'bg-orange-100 text-orange-700',
		suspending: 'bg-orange-100 text-orange-700',
		suspended: 'bg-orange-100 text-orange-700',
		resuming: 'bg-blue-100 text-blue-700',
		terminating: 'bg-red-100 text-red-700',
		terminated: 'bg-gray-100 text-gray-500',
		error: 'bg-red-100 text-red-700'
	};

	const labels: Record<VpsStatus, string> = {
		requested: 'Requested',
		provisioning: 'Provisioning',
		bootstrapping: 'Bootstrapping',
		installing_agent: 'Installing',
		active: 'Active',
		restarting: 'Restarting',
		snapshotting: 'Snapshotting',
		restoring: 'Restoring',
		upgrading: 'Upgrading',
		downgrading: 'Downgrading',
		suspending: 'Suspending',
		suspended: 'Suspended',
		resuming: 'Resuming',
		terminating: 'Terminating',
		terminated: 'Terminated',
		error: 'Error'
	};

	const inProgressStatuses = ['provisioning', 'bootstrapping', 'installing_agent', 'restarting', 'snapshotting', 'restoring', 'upgrading', 'downgrading', 'resuming', 'suspending', 'terminating'];
</script>

<span class="inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium {isUpdatingVersion ? 'bg-blue-100 text-blue-700' : colors[status]}">
	{#if isUpdatingVersion || inProgressStatuses.includes(status)}
		<svg class="mr-1 h-3 w-3 animate-spin" viewBox="0 0 24 24" fill="none">
			<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
			<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
		</svg>
	{/if}
	{isUpdatingVersion ? 'Updating Version' : labels[status]}
</span>
