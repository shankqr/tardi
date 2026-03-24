<script lang="ts">
	import type { VpsInstance } from '$lib/types';
	import StatusBadge from './StatusBadge.svelte';

	interface Props {
		instance: VpsInstance;
		planName?: string;
	}

	let { instance, planName = 'Standard' }: Props = $props();

	function timeAgo(dateStr: string | null): string {
		if (!dateStr) return 'Never';
		const seconds = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
		if (seconds < 60) return `${seconds}s ago`;
		if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
		if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
		return `${Math.floor(seconds / 86400)}d ago`;
	}
</script>

<a
	href="/dashboard/instances/{instance.id}"
	class="block rounded-xl border border-gray-200 dark:border-gray-700 p-5 hover:border-gray-300 dark:hover:border-gray-600 hover:shadow-sm transition-all"
>
	<div class="flex items-start justify-between">
		<div>
			<h3 class="font-semibold text-gray-900 dark:text-white">{instance.name}</h3>
			<p class="mt-0.5 text-xs text-gray-400 dark:text-gray-500">{planName}</p>
		</div>
		<StatusBadge status={instance.status} updateStatus={instance.openclaw_update_status} />
	</div>

	<div class="mt-4 grid grid-cols-2 gap-4 text-sm">
		<div>
			<p class="text-gray-400 dark:text-gray-500 text-xs">IP Address</p>
			<p class="font-mono text-gray-700 dark:text-gray-300">{instance.ipv4 ?? '—'}</p>
		</div>
		<div>
			<p class="text-gray-400 dark:text-gray-500 text-xs">Last Heartbeat</p>
			<p class="text-gray-700 dark:text-gray-300">{timeAgo(instance.last_heartbeat_at)}</p>
		</div>
	</div>
</a>
