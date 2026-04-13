import type { Page } from '@playwright/test';
import { API_URL, getIdToken } from './journey-helpers';

/**
 * POST /api/instances/{id}/terminal/ticket and return the raw fetch Response.
 * Callers can branch on status code; this helper doesn't throw on non-2xx.
 */
export async function fetchTerminalTicket(
	email: string,
	password: string,
	instanceId: string,
	opts: { authOverride?: string | null } = {}
): Promise<Response> {
	const idToken =
		opts.authOverride === null
			? ''
			: (opts.authOverride ?? (await getIdToken(email, password)));
	const headers: Record<string, string> = {};
	if (idToken) headers['Authorization'] = `Bearer ${idToken}`;
	return fetch(`${API_URL}/api/instances/${instanceId}/terminal/ticket`, {
		method: 'POST',
		headers,
	});
}

export interface WsRoundtripResult {
	opened: boolean;
	closed: boolean;
	closeCode: number;
	output: string;
	error?: string;
}

/**
 * Open a terminal WebSocket inside the Playwright browser context, run a
 * command, and return the captured PTY output. The browser context is used
 * so the Origin header matches the allowed-origins list in the backend
 * WebSocket upgrader.
 */
export async function runTerminalCommandViaWs(
	page: Page,
	apiUrl: string,
	instanceId: string,
	ticket: string,
	command: string,
	options: { readWindowMs?: number } = {}
): Promise<WsRoundtripResult> {
	const readWindowMs = options.readWindowMs ?? 8000;
	return page.evaluate(
		async ({ apiUrl, instanceId, ticket, command, readWindowMs }) => {
			const wsBase = apiUrl.replace(/^http/, 'ws');
			const url = `${wsBase}/api/instances/${instanceId}/terminal/ws?t=${encodeURIComponent(ticket)}`;
			const ws = new WebSocket(url);
			ws.binaryType = 'arraybuffer';
			const result = {
				opened: false,
				closed: false,
				closeCode: 0,
				output: '',
				error: undefined as string | undefined,
			};
			const decoder = new TextDecoder();
			const sent = { cmd: false };
			return await new Promise<typeof result>((resolve) => {
				const done = () => {
					try {
						ws.close();
					} catch {
						/* ignore */
					}
					resolve(result);
				};
				const timeoutId = setTimeout(done, readWindowMs);
				ws.onopen = () => {
					result.opened = true;
					ws.send(JSON.stringify({ type: 'resize', cols: 80, rows: 24 }));
				};
				ws.onmessage = (ev) => {
					if (typeof ev.data === 'string') {
						result.output += `\n[text] ${ev.data}`;
						return;
					}
					const chunk = decoder.decode(new Uint8Array(ev.data as ArrayBuffer));
					result.output += chunk;
					if (!sent.cmd && result.output.length > 0) {
						sent.cmd = true;
						// Small delay so the shell has rendered the initial prompt before typing.
						setTimeout(() => {
							try {
								ws.send(new TextEncoder().encode(command + '\n'));
							} catch {
								/* ignore */
							}
						}, 400);
					}
				};
				ws.onclose = (ev) => {
					result.closed = true;
					result.closeCode = ev.code;
					clearTimeout(timeoutId);
					resolve(result);
				};
				ws.onerror = () => {
					result.error = 'websocket error';
				};
			});
		},
		{ apiUrl, instanceId, ticket, command, readWindowMs }
	);
}

/**
 * Open a WebSocket with an intentionally bogus ticket and return the close
 * code. Used to verify the backend rejects tampered tickets.
 */
export async function probeTerminalWsWithBadTicket(
	page: Page,
	apiUrl: string,
	instanceId: string,
	badTicket: string
): Promise<{ closed: boolean; closeCode: number }> {
	return page.evaluate(
		async ({ apiUrl, instanceId, badTicket }) => {
			const wsBase = apiUrl.replace(/^http/, 'ws');
			const url = `${wsBase}/api/instances/${instanceId}/terminal/ws?t=${encodeURIComponent(badTicket)}`;
			const ws = new WebSocket(url);
			return await new Promise<{ closed: boolean; closeCode: number }>((resolve) => {
				const result = { closed: false, closeCode: 0 };
				const timeoutId = setTimeout(() => resolve(result), 5000);
				ws.onclose = (ev) => {
					result.closed = true;
					result.closeCode = ev.code;
					clearTimeout(timeoutId);
					resolve(result);
				};
				ws.onerror = () => {
					/* ignore — close will still fire */
				};
			});
		},
		{ apiUrl, instanceId, badTicket }
	);
}
