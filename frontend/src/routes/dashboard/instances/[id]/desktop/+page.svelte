<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { page } from '$app/stores';
	import { dashboardState } from '$lib/stores/dashboard';
	import { getIdToken } from '$lib/stores/auth';
	import { createDesktopSession, openDesktopTradingView } from '$lib/api/client';
	import { getApiUrl } from '$lib/stores/config';
	import RFB from '@novnc/novnc';

	const instanceId = $derived($page.params.id);
	const instance = $derived(
		$dashboardState?.instances.find((i) => i.id === instanceId) ?? null
	);

	let host: HTMLDivElement;
	let rfb: RFB | null = null;
	let status = $state<'preparing' | 'connecting' | 'connected' | 'reconnecting' | 'disconnected' | 'error'>('preparing');
	let errorMessage = $state('');
	let launchingTradingView = $state(false);
	let reconnecting = $state(false);
	let linkCopied = $state(false);
	let connectRun = 0;
	let destroyed = false;
	let manualDisconnect = false;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let reconnectAttempt = 0;
	let linkTimer: ReturnType<typeof setTimeout> | null = null;
	let removeLifecycleListeners: (() => void) | null = null;

	const preparePollMs = 4000;
	const prepareMaxAttempts = 150;
	const reconnectMaxMs = 30000;

	function wsURL(ticket: string) {
		const wsBase = getApiUrl().replace(/^http/, 'ws');
		return `${wsBase}/api/instances/${instanceId}/desktop/ws?t=${encodeURIComponent(ticket)}`;
	}

	function disconnect() {
		rfb?.disconnect();
		rfb = null;
	}

	function clearReconnectTimer() {
		if (reconnectTimer) {
			clearTimeout(reconnectTimer);
			reconnectTimer = null;
		}
	}

	function isCurrent(run: number) {
		return !destroyed && run === connectRun;
	}

	function wait(ms: number) {
		return new Promise((resolve) => setTimeout(resolve, ms));
	}

	function scheduleReconnect(run: number) {
		if (!isCurrent(run) || manualDisconnect) return;
		clearReconnectTimer();
		const delay = Math.min(reconnectMaxMs, 1000 * 2 ** Math.min(reconnectAttempt, 5));
		reconnectAttempt += 1;
		status = 'reconnecting';
		errorMessage = '';
		reconnectTimer = setTimeout(() => {
			if (!isCurrent(run) || manualDisconnect) return;
			connect('auto');
		}, delay);
	}

	function attachRfb(ticket: string, run: number) {
		status = 'connecting';
		const client = new RFB(host, wsURL(ticket), {
			shared: true,
			wsProtocols: ['binary']
		});
		client.scaleViewport = true;
		client.resizeSession = false;
		client.viewOnly = false;
		client.focusOnClick = true;
		client.qualityLevel = 7;
		client.compressionLevel = 2;
		client.background = 'rgb(0, 0, 0)';

		client.addEventListener('connect', () => {
			if (!isCurrent(run)) return;
			reconnectAttempt = 0;
			errorMessage = '';
			status = 'connected';
		});
		client.addEventListener('disconnect', (event) => {
			if (!isCurrent(run)) return;
			rfb = null;
			if (manualDisconnect) {
				status = 'disconnected';
				return;
			}
			const clean = (event as CustomEvent<{ clean: boolean }>).detail?.clean;
			if (!clean) reconnectAttempt = Math.max(reconnectAttempt, 1);
			scheduleReconnect(run);
		});
		client.addEventListener('securityfailure', () => {
			if (!isCurrent(run)) return;
			rfb = null;
			scheduleReconnect(run);
		});
		client.addEventListener('credentialsrequired', () => {
			if (!isCurrent(run)) return;
			rfb = null;
			scheduleReconnect(run);
		});
		rfb = client;
	}

	async function connect(mode: 'initial' | 'manual' | 'auto' = 'manual') {
		const run = ++connectRun;
		clearReconnectTimer();
		if (mode !== 'auto') reconnectAttempt = 0;
		manualDisconnect = false;
		disconnect();
		status = mode === 'auto' ? 'reconnecting' : 'preparing';
		errorMessage = '';

		if (!instanceId) {
			status = 'error';
			errorMessage = 'Agent not found.';
			return;
		}

		const token = await getIdToken();
		if (!isCurrent(run)) return;
		if (!token) {
			status = 'error';
			errorMessage = 'Not signed in.';
			return;
		}

		try {
			for (let attempt = 0; attempt < prepareMaxAttempts; attempt += 1) {
				const session = await createDesktopSession(instanceId, token);
				if (!isCurrent(run)) return;
				if (session.status === 'ready' && session.ticket) {
					attachRfb(session.ticket, run);
					return;
				}
				status = 'preparing';
				await wait(preparePollMs);
				if (!isCurrent(run)) return;
			}
			throw new Error('Desktop is still preparing. Try reconnecting in a minute.');
		} catch (err) {
			if (!isCurrent(run)) return;
			if (mode === 'manual') {
				status = 'error';
				errorMessage = err instanceof Error ? err.message : 'Failed to open desktop';
				return;
			}
			scheduleReconnect(run);
		}
	}

	async function reconnect() {
		reconnecting = true;
		try {
			await connect('manual');
		} finally {
			reconnecting = false;
		}
	}

	function manualDisconnectDesktop() {
		manualDisconnect = true;
		connectRun += 1;
		clearReconnectTimer();
		disconnect();
		status = 'disconnected';
		errorMessage = '';
	}

	async function copyDesktopLink() {
		if (typeof window === 'undefined' || !instanceId) return;
		const url = `${window.location.origin}/dashboard/instances/${instanceId}/desktop`;
		try {
			if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable');
			await navigator.clipboard.writeText(url);
		} catch {
			const textarea = document.createElement('textarea');
			textarea.value = url;
			textarea.setAttribute('readonly', '');
			textarea.style.position = 'fixed';
			textarea.style.opacity = '0';
			document.body.appendChild(textarea);
			textarea.select();
			document.execCommand('copy');
			document.body.removeChild(textarea);
		}
		linkCopied = true;
		if (linkTimer) clearTimeout(linkTimer);
		linkTimer = setTimeout(() => {
			linkCopied = false;
			linkTimer = null;
		}, 2000);
	}

	async function launchTradingView() {
		const token = await getIdToken();
		if (!token) {
			status = 'error';
			errorMessage = 'Not signed in.';
			return;
		}

		launchingTradingView = true;
		errorMessage = '';
		try {
			await openDesktopTradingView(instanceId!, token);
		} catch (err) {
			status = 'error';
			errorMessage = err instanceof Error ? err.message : 'Failed to launch TradingView';
		} finally {
			launchingTradingView = false;
		}
	}

	onMount(() => {
		connect('initial');
		const resume = () => {
			if (!manualDisconnect && !rfb && (status === 'error' || status === 'disconnected' || status === 'reconnecting')) {
				connect('auto');
			}
		};
		const onVisibility = () => {
			if (document.visibilityState === 'visible') resume();
		};
		window.addEventListener('online', resume);
		document.addEventListener('visibilitychange', onVisibility);
		removeLifecycleListeners = () => {
			window.removeEventListener('online', resume);
			document.removeEventListener('visibilitychange', onVisibility);
		};
	});

	onDestroy(() => {
		destroyed = true;
		manualDisconnect = true;
		connectRun += 1;
		clearReconnectTimer();
		if (linkTimer) clearTimeout(linkTimer);
		removeLifecycleListeners?.();
		disconnect();
	});
</script>

<div class="space-y-3">
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div class="flex min-w-0 items-center gap-3">
			<a
				href="/dashboard/instances/{instanceId}"
				class="text-sm text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white"
			>
				&larr; Back to agent
			</a>
			{#if instance}
				<span class="text-sm text-gray-400 dark:text-gray-500">/</span>
				<span class="truncate text-sm font-medium text-gray-900 dark:text-white">{instance.name}</span>
				<span class="text-sm text-gray-400 dark:text-gray-500">/</span>
				<span class="text-sm text-gray-700 dark:text-gray-300">Desktop</span>
			{/if}
		</div>

		<div class="flex items-center gap-2">
			<span class="text-xs text-gray-500 dark:text-gray-400">
				{#if status === 'preparing'}Preparing{:else if status === 'connecting'}Connecting{:else if status === 'connected'}Connected{:else if status === 'reconnecting'}Reconnecting{:else if status === 'disconnected'}Disconnected{:else}Error{/if}
			</span>
			<button
				type="button"
				onclick={copyDesktopLink}
				class="rounded-lg border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-800"
			>
				{linkCopied ? 'Copied' : 'Copy link'}
			</button>
			<button
				type="button"
				onclick={launchTradingView}
				disabled={launchingTradingView || status === 'preparing' || status === 'connecting' || status === 'reconnecting'}
				class="rounded-lg border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-800"
			>
				{launchingTradingView ? 'Launching...' : 'TradingView'}
			</button>
			<button
				type="button"
				onclick={reconnect}
				disabled={reconnecting || status === 'preparing' || status === 'connecting' || status === 'reconnecting'}
				class="rounded-lg border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-800"
			>
				{reconnecting ? 'Reconnecting...' : 'Reconnect'}
			</button>
			<button
				type="button"
				onclick={manualDisconnectDesktop}
				disabled={status === 'disconnected'}
				class="rounded-lg border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-800"
			>
				Disconnect
			</button>
		</div>
	</div>

	{#if errorMessage}
		<div class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-400">
			{errorMessage}
		</div>
	{/if}

	<div class="relative overflow-hidden rounded-lg border border-gray-800 bg-black">
		<div bind:this={host} class="h-[calc(100vh-9rem)] min-h-[520px] w-full"></div>
		{#if status === 'preparing' || status === 'connecting' || status === 'reconnecting'}
			<div class="pointer-events-none absolute inset-0 flex items-center justify-center bg-black">
				<div class="h-5 w-5 animate-spin rounded-full border-2 border-gray-700 border-t-gray-200"></div>
			</div>
		{/if}
	</div>
</div>
