<script lang="ts">
	import { getIdToken } from '$lib/stores/auth';
	import { getGoogleOAuthUrl, getGoogleOAuthStatus, disconnectGoogle, syncConfig, getSyncStatus } from '$lib/api/client';
	import type { GoogleOAuthStatus } from '$lib/types';

	let {
		instanceId,
		instanceStatus,
		onSyncStart = () => {},
		onSyncEnd = () => {}
	}: {
		instanceId: string;
		instanceStatus: string;
		onSyncStart?: () => void;
		onSyncEnd?: () => void;
	} = $props();

	let status = $state<GoogleOAuthStatus>({ connected: false });
	let loading = $state(false);
	let error = $state<string | null>(null);
	let syncPhase = $state<'idle' | 'syncing' | 'finishing' | 'success' | 'failed'>('idle');
	let syncElapsed = $state(0);
	let syncTimer: ReturnType<typeof setInterval> | null = null;
	let pollTimer: ReturnType<typeof setInterval> | null = null;

	const isActive = $derived(instanceStatus === 'active');

	// Fetch status on mount
	$effect(() => {
		fetchStatus();
		return () => {
			if (syncTimer) clearInterval(syncTimer);
			if (pollTimer) clearInterval(pollTimer);
		};
	});

	async function fetchStatus() {
		try {
			const token = await getIdToken();
			if (!token) return;
			status = await getGoogleOAuthStatus(token);
		} catch {
			// Ignore — show as disconnected
		}
	}

	async function handleConnect() {
		error = null;
		loading = true;

		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');

			const { redirect_url } = await getGoogleOAuthUrl(token);
			if (redirect_url === '#') {
				error = 'Google OAuth is not configured';
				return;
			}

			// Open popup
			const popup = window.open(redirect_url, 'google-oauth', 'width=600,height=700,left=200,top=100');

			// Listen for postMessage from callback page
			const handleMessage = async (event: MessageEvent) => {
				if (event.data?.type !== 'google-oauth-result') return;
				window.removeEventListener('message', handleMessage);

				if (event.data.success) {
					status = { connected: true, email: event.data.email };
					// Trigger config sync so credentials reach the VPS
					await triggerSync();
				} else {
					error = event.data.error || 'Google authorization failed';
				}
			};

			window.addEventListener('message', handleMessage);

			// Fallback: check if popup was closed without sending message
			const checkClosed = setInterval(() => {
				if (popup && popup.closed) {
					clearInterval(checkClosed);
					// Give a moment for the message to arrive
					setTimeout(() => {
						if (!status.connected && syncPhase === 'idle') {
							window.removeEventListener('message', handleMessage);
							// Refresh status in case the callback saved tokens but postMessage failed
							fetchStatus();
						}
					}, 1000);
				}
			}, 500);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to start Google authorization';
		} finally {
			loading = false;
		}
	}

	async function handleDisconnect() {
		if (!confirm('Disconnect your Google account? Your agent will lose access to Google Calendar, Gmail, Docs, etc.')) return;

		error = null;
		loading = true;

		try {
			const token = await getIdToken();
			if (!token) throw new Error('Not authenticated');
			await disconnectGoogle(token);
			status = { connected: false };
			// Trigger sync to remove credentials from VPS
			await triggerSync();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to disconnect Google';
		} finally {
			loading = false;
		}
	}

	async function triggerSync() {
		try {
			const token = await getIdToken();
			if (!token) return;

			syncPhase = 'syncing';
			syncElapsed = 0;
			syncTimer = setInterval(() => { syncElapsed += 1; }, 1000);
			onSyncStart();

			const result = await syncConfig(token, instanceId);
			if (!result.synced) {
				throw new Error(result.error || 'Sync failed');
			}

			// Poll for completion
			pollTimer = setInterval(async () => {
				try {
					const syncResult = await getSyncStatus(token, instanceId);
					if (syncResult.status === 'completed') {
						cleanup();
						syncPhase = 'finishing';
						setTimeout(() => {
							syncPhase = 'success';
							onSyncEnd();
							setTimeout(() => { if (syncPhase === 'success') syncPhase = 'idle'; }, 8000);
						}, 5000);
					} else if (syncResult.status === 'failed') {
						cleanup();
						error = syncResult.message || 'Config sync failed';
						syncPhase = 'failed';
						onSyncEnd();
					}
				} catch { /* keep polling */ }
			}, 3000);

			// Timeout after 120s
			setTimeout(() => {
				if (syncPhase === 'syncing') {
					cleanup();
					error = 'Sync is taking longer than expected — credentials will apply automatically within a few minutes';
					syncPhase = 'failed';
					onSyncEnd();
				}
			}, 120_000);
		} catch (err) {
			cleanup();
			error = err instanceof Error ? err.message : 'Sync failed';
			syncPhase = 'failed';
			onSyncEnd();
		}
	}

	function cleanup() {
		if (syncTimer) { clearInterval(syncTimer); syncTimer = null; }
		if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
	}
</script>

<div class="rounded-xl border border-gray-200 p-5">
	<div class="flex items-center gap-2">
		<!-- Google icon -->
		<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" class="h-5 w-5">
			<path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z" fill="#4285F4"/>
			<path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/>
			<path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" fill="#FBBC05"/>
			<path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/>
		</svg>
		<h3 class="text-sm font-semibold text-gray-900">Google Account</h3>
	</div>
	<p class="mt-1 text-xs text-gray-400">Connect your Google account so your agent can use Calendar, Gmail, Docs, Sheets, and Drive</p>

	<div class="mt-4">
		{#if status.connected}
			{#if syncPhase === 'syncing' || syncPhase === 'finishing'}
				<!-- Syncing state -->
				<div class="space-y-3">
					<div class="flex items-center gap-2 text-sm text-amber-700">
						<svg class="h-4 w-4 animate-spin" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
							<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
							<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
						</svg>
						<span>{syncPhase === 'finishing' ? 'Finishing up...' : `Syncing credentials to your agent (${syncElapsed}s)`}</span>
					</div>
					<p class="text-xs text-gray-400">This usually takes about a minute.</p>
				</div>
			{:else if syncPhase === 'success'}
				<div class="flex items-center gap-2 text-sm text-green-700">
					<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="h-4 w-4">
						<path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.857-9.809a.75.75 0 00-1.214-.882l-3.483 4.79-1.88-1.88a.75.75 0 10-1.06 1.061l2.5 2.5a.75.75 0 001.137-.089l4-5.5z" clip-rule="evenodd" />
					</svg>
					<span>Google credentials synced to your agent</span>
				</div>
			{:else}
				<!-- Connected state -->
				<div class="flex items-center justify-between">
					<div class="flex items-center gap-2 text-sm text-green-700">
						<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" class="h-4 w-4">
							<path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.857-9.809a.75.75 0 00-1.214-.882l-3.483 4.79-1.88-1.88a.75.75 0 10-1.06 1.061l2.5 2.5a.75.75 0 001.137-.089l4-5.5z" clip-rule="evenodd" />
						</svg>
						<span>Connected as <strong>{status.email}</strong></span>
					</div>
					<button
						onclick={handleDisconnect}
						disabled={loading || !isActive}
						class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs font-medium text-gray-600 hover:bg-gray-50 disabled:opacity-50"
					>
						{loading ? 'Disconnecting...' : 'Disconnect'}
					</button>
				</div>
			{/if}
		{:else}
			<!-- Disconnected state -->
			{#if syncPhase === 'syncing' || syncPhase === 'finishing'}
				<div class="space-y-3">
					<div class="flex items-center gap-2 text-sm text-amber-700">
						<svg class="h-4 w-4 animate-spin" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
							<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
							<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
						</svg>
						<span>Removing credentials from your agent ({syncElapsed}s)</span>
					</div>
				</div>
			{:else}
				<button
					onclick={handleConnect}
					disabled={loading || !isActive}
					class="rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
				>
					{loading ? 'Connecting...' : 'Connect Google Account'}
				</button>
			{/if}
		{/if}

		{#if error}
			<p class="mt-3 text-xs text-red-600">{error}</p>
		{/if}
	</div>
</div>
