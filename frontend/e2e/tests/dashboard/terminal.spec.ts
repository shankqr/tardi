import { test, expect } from '../../fixtures/auth';
import { API_URL, getIdToken } from '../../helpers/journey-helpers';
import { loadAccountState } from '../../helpers/test-state';
import {
	fetchTerminalTicket,
	runTerminalCommandViaWs,
	runTerminalCommandSequenceViaWs,
	probeTerminalWsWithBadTicket,
} from '../../helpers/terminal-helpers';

/**
 * E2E coverage for the web-terminal feature against the deployed dev
 * environment. Exercises:
 *   - ticket endpoint auth + ownership checks
 *   - WebSocket ticket verification (bad ticket rejected)
 *   - end-to-end PTY shell round-trip (whoami → root)
 *   - dashboard "Open Terminal" button + rendered /terminal page
 *
 * Uses the OpenClaw test account set up by the `setup` project, which
 * has an active instance reachable over SSH. The same backend code path
 * serves both OpenClaw and Hermes VPSes, so one test account is enough.
 */
test.describe('Web terminal', () => {
	test('ticket endpoint requires authentication', async () => {
		const account = loadAccountState('openclaw');
		const res = await fetchTerminalTicket(account.email, account.password, account.instanceId, {
			authOverride: null,
		});
		expect(res.status).toBe(401);
	});

	test('ticket endpoint returns 404 for an instance the user does not own', async () => {
		const account = loadAccountState('openclaw');
		// Well-formed UUID that does not match any instance.
		const res = await fetchTerminalTicket(
			account.email,
			account.password,
			'00000000-0000-0000-0000-000000000001'
		);
		expect(res.status).toBe(404);
	});

	test('ticket endpoint returns a ticket for an owned active instance', async () => {
		const account = loadAccountState('openclaw');
		const res = await fetchTerminalTicket(account.email, account.password, account.instanceId);
		expect(res.status).toBe(200);
		const body = await res.json();
		expect(typeof body.ticket).toBe('string');
		// Ticket format: "<uuid>.<uuid>.<expUnix>.<sig>"
		expect(body.ticket.split('.').length).toBe(4);
	});

	test('WebSocket rejects a tampered ticket', async ({ authedPage: page }) => {
		const account = loadAccountState('openclaw');
		const result = await probeTerminalWsWithBadTicket(
			page,
			API_URL,
			account.instanceId,
			'not-a-real-ticket'
		);
		expect(result.closed).toBe(true);
		// gorilla/websocket upgrader refuses a non-upgrade HTTP request with a
		// 401 body — the browser surfaces that as an abnormal close (1006).
		expect(result.closeCode).not.toBe(1000);
	});

	test('WebSocket shell round-trip: whoami returns root', async ({ authedPage: page }) => {
		const account = loadAccountState('openclaw');

		// Fetch a fresh ticket via the frontend's auth token.
		const idToken = await getIdToken(account.email, account.password);
		const ticketRes = await fetch(
			`${API_URL}/api/instances/${account.instanceId}/terminal/ticket`,
			{
				method: 'POST',
				headers: { Authorization: `Bearer ${idToken}` },
			}
		);
		expect(ticketRes.status).toBe(200);
		const { ticket } = await ticketRes.json();

		// The browser page is loaded on the dev frontend origin, so the
		// WebSocket Origin header will match ALLOWED_ORIGINS.
		const result = await runTerminalCommandViaWs(
			page,
			API_URL,
			account.instanceId,
			ticket,
			'whoami',
			{ readWindowMs: 12_000 }
		);
		console.log(`[terminal] opened=${result.opened} closed=${result.closed} output.len=${result.output.length}`);
		expect(result.opened).toBe(true);
		expect(result.output).toContain('root');
	});

	test('dashboard shows Open Terminal button and /terminal page connects', async ({
		authedPage: page,
	}) => {
		const account = loadAccountState('openclaw');

		await page.goto(`/dashboard/instances/${account.instanceId}`);
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });

		// Expand Power User section to reveal the Open Terminal button
		const powerUserButton = page.getByText('Power User').first();
		await powerUserButton.scrollIntoViewIfNeeded();
		await powerUserButton.click();

		const terminalButton = page.getByRole('link', { name: 'Open Terminal' });
		await expect(terminalButton).toBeVisible({ timeout: 15_000 });
		await expect(terminalButton).toHaveAttribute(
			'href',
			`/dashboard/instances/${account.instanceId}/terminal`
		);

		await terminalButton.click();
		await page.waitForURL(`**/dashboard/instances/${account.instanceId}/terminal`, {
			timeout: 10_000,
		});

		// Status indicator flips to "Connected as root" once the WS handshake completes.
		await expect(page.getByText('Connected as root')).toBeVisible({ timeout: 30_000 });

		// xterm.js renders its viewport and helper textarea; the helper textarea
		// is always present whether or not the canvas has rasterised text.
		await expect(page.locator('.xterm-helper-textarea')).toBeAttached({ timeout: 10_000 });

		// Back link navigates home (also triggers WS teardown).
		await page.getByRole('link', { name: /Back to agent/ }).click();
		await page.waitForURL(`**/dashboard/instances/${account.instanceId}`, { timeout: 10_000 });
	});

	test('launched terminal runs "openclaw status" inside the gateway container', async ({
		authedPage: page,
	}) => {
		const account = loadAccountState('openclaw');

		// Launch the terminal the way a user would: click "Open Terminal" from
		// the instance page and wait for the WS handshake to flip to connected.
		await page.goto(`/dashboard/instances/${account.instanceId}`);
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });
		const powerUserButton = page.getByText('Power User').first();
		await powerUserButton.scrollIntoViewIfNeeded();
		await powerUserButton.click();
		const terminalButton = page.getByRole('link', { name: 'Open Terminal' });
		await expect(terminalButton).toBeVisible({ timeout: 15_000 });
		await terminalButton.click();
		await page.waitForURL(`**/dashboard/instances/${account.instanceId}/terminal`, {
			timeout: 10_000,
		});
		await expect(page.getByText('Connected as root')).toBeVisible({ timeout: 30_000 });

		// Drive the PTY from a parallel WS (same backend path, same VPS):
		//   1) docker exec -it openclaw-gateway sh   → enter the gateway container
		//   2) openclaw status                       → print the status table
		const idToken = await getIdToken(account.email, account.password);
		const ticketRes = await fetch(
			`${API_URL}/api/instances/${account.instanceId}/terminal/ticket`,
			{
				method: 'POST',
				headers: { Authorization: `Bearer ${idToken}` },
			}
		);
		expect(ticketRes.status).toBe(200);
		const { ticket } = await ticketRes.json();

		const result = await runTerminalCommandSequenceViaWs(
			page,
			API_URL,
			account.instanceId,
			ticket,
			['docker exec -it openclaw-gateway sh', 'openclaw status'],
			{ readWindowMs: 45_000, interCommandDelayMs: 4_000 }
		);
		console.log(
			`[terminal] opened=${result.opened} closed=${result.closed} output.len=${result.output.length}`
		);
		expect(result.opened).toBe(true);

		// `openclaw status` renders a table with an "Overview" header and rows
		// for Gateway, Agents, Memory, etc. Checking a few distinct markers
		// keeps the assertion stable across version bumps that tweak copy.
		expect(result.output).toContain('OpenClaw status');
		expect(result.output).toContain('Overview');
		expect(result.output).toContain('Gateway');
		expect(result.output).toContain('Agents');
	});
});
