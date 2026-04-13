<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/stores';
	import { dashboardState } from '$lib/stores/dashboard';
	import { getIdToken } from '$lib/stores/auth';
	import { getTerminalTicket } from '$lib/api/client';
	import { getApiUrl } from '$lib/stores/config';
	import { Terminal } from '@xterm/xterm';
	import { FitAddon } from '@xterm/addon-fit';
	import { WebLinksAddon } from '@xterm/addon-web-links';
	import '@xterm/xterm/css/xterm.css';

	const instanceId = $derived($page.params.id);
	const instance = $derived(
		$dashboardState?.instances.find((i) => i.id === instanceId) ?? null
	);

	let host: HTMLDivElement;
	let term: Terminal | null = null;
	let fit: FitAddon | null = null;
	let ws: WebSocket | null = null;
	let resizeObserver: ResizeObserver | null = null;
	let status = $state<'connecting' | 'open' | 'closed' | 'error'>('connecting');
	let errorMessage = $state('');

	function sendResize() {
		if (!term || !ws || ws.readyState !== WebSocket.OPEN) return;
		ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
	}

	async function connect() {
		const idToken = await getIdToken();
		if (!idToken) {
			status = 'error';
			errorMessage = 'Not signed in.';
			return;
		}
		try {
			const { ticket } = await getTerminalTicket(instanceId!, idToken);
			const wsBase = getApiUrl().replace(/^http/, 'ws');
			ws = new WebSocket(`${wsBase}/api/instances/${instanceId}/terminal/ws?t=${encodeURIComponent(ticket)}`);
			ws.binaryType = 'arraybuffer';

			ws.onopen = () => {
				status = 'open';
				sendResize();
				term?.focus();
			};
			ws.onmessage = (ev) => {
				if (typeof ev.data === 'string') {
					try {
						const msg = JSON.parse(ev.data);
						if (msg.type === 'error' && msg.message) {
							term?.write(`\r\n\x1b[31m[tardi] ${msg.message}\x1b[0m\r\n`);
						}
					} catch {
						/* ignore non-JSON text frames */
					}
					return;
				}
				term?.write(new Uint8Array(ev.data as ArrayBuffer));
			};
			ws.onclose = () => {
				status = 'closed';
				term?.write('\r\n\x1b[33m[tardi] session closed\x1b[0m\r\n');
			};
			ws.onerror = () => {
				status = 'error';
				errorMessage = 'WebSocket error';
			};
		} catch (err) {
			status = 'error';
			errorMessage = err instanceof Error ? err.message : 'Failed to open terminal';
		}
	}

	onMount(() => {
		term = new Terminal({
			fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
			fontSize: 13,
			cursorBlink: true,
			convertEol: false,
			theme: { background: '#0b0b0b' },
			scrollback: 10000
		});
		fit = new FitAddon();
		term.loadAddon(fit);
		term.loadAddon(new WebLinksAddon());
		term.open(host);
		fit.fit();

		const encoder = new TextEncoder();
		term.onData((data) => {
			if (ws?.readyState === WebSocket.OPEN) {
				ws.send(encoder.encode(data));
			}
		});

		resizeObserver = new ResizeObserver(() => {
			fit?.fit();
			sendResize();
		});
		resizeObserver.observe(host);

		connect();
	});

	onDestroy(() => {
		resizeObserver?.disconnect();
		ws?.close();
		term?.dispose();
	});
</script>

<div class="space-y-3">
	<div class="flex items-center justify-between gap-3">
		<div class="flex items-center gap-3">
			<a
				href="/dashboard/instances/{instanceId}"
				class="text-sm text-gray-500 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white"
			>
				&larr; Back to agent
			</a>
			{#if instance}
				<span class="text-sm text-gray-400 dark:text-gray-500">/</span>
				<span class="text-sm font-medium text-gray-900 dark:text-white">{instance.name}</span>
				<span class="text-sm text-gray-400 dark:text-gray-500">/</span>
				<span class="text-sm text-gray-700 dark:text-gray-300">Terminal</span>
			{/if}
		</div>
		<span class="text-xs text-gray-500 dark:text-gray-400">
			{#if status === 'connecting'}Connecting…{:else if status === 'open'}Connected as root{:else if status === 'closed'}Disconnected{:else}Error{/if}
		</span>
	</div>

	<p class="text-xs text-gray-500 dark:text-gray-400">
		You are connected as <span class="font-mono">root</span> to your agent's VPS. Sessions are limited to 60 minutes.
	</p>

	{#if status === 'error' && errorMessage}
		<div class="rounded-lg bg-red-50 dark:bg-red-900/20 p-3 text-sm text-red-700 dark:text-red-400">
			{errorMessage}
		</div>
	{/if}

	<div class="rounded-lg border border-gray-800 bg-black p-2">
		<div bind:this={host} class="h-[calc(100vh-14rem)] w-full"></div>
	</div>
</div>
