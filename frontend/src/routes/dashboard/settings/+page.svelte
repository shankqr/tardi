<script lang="ts">
	import { user } from '$lib/stores/auth';
</script>

<a href="/dashboard" class="text-sm text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300">&larr; Back to dashboard</a>

<h2 class="mt-4 text-xl font-bold text-gray-900 dark:text-white">Settings</h2>

<div class="mt-6 space-y-6 max-w-lg">
	<div class="rounded-xl border border-gray-200 dark:border-gray-700 p-5">
		<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Account</h3>
		<dl class="mt-4 space-y-3 text-sm">
			<div class="flex justify-between">
				<dt class="text-gray-500 dark:text-gray-400">Email</dt>
				<dd class="text-gray-900 dark:text-white">{$user?.email ?? '—'}</dd>
			</div>
			<div class="flex justify-between">
				<dt class="text-gray-500 dark:text-gray-400">User ID</dt>
				<dd class="font-mono text-xs text-gray-500 dark:text-gray-400">{$user?.uid ?? '—'}</dd>
			</div>
		</dl>
	</div>
</div>
