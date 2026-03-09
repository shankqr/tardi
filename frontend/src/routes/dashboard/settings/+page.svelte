<script lang="ts">
	import { user } from '$lib/stores/auth';
</script>

<a href="/dashboard" class="text-sm text-gray-500 hover:text-gray-700">&larr; Back to dashboard</a>

<h2 class="mt-4 text-xl font-bold text-gray-900">Settings</h2>

<div class="mt-6 space-y-6 max-w-lg">
	<div class="rounded-xl border border-gray-200 p-5">
		<h3 class="text-sm font-semibold text-gray-900">Account</h3>
		<dl class="mt-4 space-y-3 text-sm">
			<div class="flex justify-between">
				<dt class="text-gray-500">Email</dt>
				<dd class="text-gray-900">{$user?.email ?? '—'}</dd>
			</div>
			<div class="flex justify-between">
				<dt class="text-gray-500">User ID</dt>
				<dd class="font-mono text-xs text-gray-500">{$user?.uid ?? '—'}</dd>
			</div>
		</dl>
	</div>
</div>
