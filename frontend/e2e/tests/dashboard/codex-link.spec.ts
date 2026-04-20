import { test, expect, navigateToInstance } from '../../fixtures/auth';
import type { Page } from '@playwright/test';

/**
 * UI-level test for the Codex (ChatGPT) linking flow on the instance
 * detail page. All three codex endpoints are mocked via `page.route()`
 * so we can drive every state transition deterministically without
 * depending on a real ChatGPT account or the OpenAI OAuth pages (which
 * cannot be reliably automated).
 */

const MOCK_EMAIL = 'e2e-codex@example.com';
const MOCK_LINKED_AT = '2026-04-20T08:37:31.000Z';

/**
 * Returns the raw dashboard-state body from the real backend so we can
 * mutate just the codex fields instead of hand-rolling a full instance
 * response that the rest of the page would need.
 */
async function mockDashboardStateWithCodex(
	page: Page,
	linked: boolean,
): Promise<void> {
	await page.route('**/api/dashboard/state', async (route, request) => {
		const response = await route.fetch();
		const body = await response.json();
		if (body?.instances?.length) {
			for (const inst of body.instances) {
				if (linked) {
					inst.codex_linked_at = MOCK_LINKED_AT;
					inst.codex_account_email = MOCK_EMAIL;
				} else {
					inst.codex_linked_at = null;
					inst.codex_account_email = null;
				}
			}
		}
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(body),
		});
	});
}

function mockCodexStart(page: Page, codes: string[]): () => number {
	let idx = 0;
	page.route('**/api/instances/*/codex/link/start', (route) => {
		const code = codes[Math.min(idx, codes.length - 1)];
		idx++;
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({
				code,
				verification_url: 'https://auth.openai.com/codex/device',
				expires_in: 900,
			}),
		});
	});
	return () => idx;
}

/**
 * Mocks status as a finite state machine. `sequence` is the list of
 * statuses to return on successive calls — the last entry is held once
 * exhausted so the UI stays stable for assertions.
 */
function mockCodexStatus(
	page: Page,
	sequence: Array<{ status: string; email?: string }>,
): () => number {
	let idx = 0;
	page.route('**/api/instances/*/codex/link/status', (route) => {
		const next = sequence[Math.min(idx, sequence.length - 1)];
		idx++;
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify(next),
		});
	});
	return () => idx;
}

function mockCodexUnlink(page: Page): () => number {
	let calls = 0;
	page.route('**/api/instances/*/codex/unlink', (route) => {
		calls++;
		route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ status: 'unlinked' }),
		});
	});
	return () => calls;
}

test.describe('Codex linking UI', () => {
	test('shows Linked state when dashboard state has codex_linked_at set', async ({
		authedPage: page,
	}) => {
		await mockDashboardStateWithCodex(page, true);
		// Reload so the dashboard state is fetched with our mock applied.
		await page.reload();

		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		const card = page.locator('div', {
			has: page.getByRole('heading', { name: 'Codex (ChatGPT)' }),
		}).first();
		await card.scrollIntoViewIfNeeded();

		await expect(
			card.getByText(/Linked to ChatGPT as/, { exact: false }),
		).toBeVisible({ timeout: 10_000 });
		await expect(card.getByText(MOCK_EMAIL)).toBeVisible();
		await expect(card.getByRole('button', { name: 'Unlink' })).toBeVisible();
	});

	test('full link flow: click → device code → restarting → linked', async ({
		authedPage: page,
	}) => {
		await mockDashboardStateWithCodex(page, false);
		mockCodexStart(page, ['ABCD-12345']);
		mockCodexStatus(page, [
			{ status: 'pending' },
			{ status: 'restarting' },
			{ status: 'linked', email: MOCK_EMAIL },
		]);
		await page.reload();

		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		const card = page.locator('div', {
			has: page.getByRole('heading', { name: 'Codex (ChatGPT)' }),
		}).first();
		await card.scrollIntoViewIfNeeded();

		const linkBtn = card.getByRole('button', { name: 'Link Codex Account' });
		await expect(linkBtn).toBeVisible();
		await linkBtn.click();

		// Device code + countdown appear.
		await expect(card.getByText('ABCD-12345')).toBeVisible({ timeout: 10_000 });
		await expect(card.getByText(/expires in \d+:\d{2}/)).toBeVisible();
		await expect(
			card.getByText('Waiting for you to authorize on openai.com…'),
		).toBeVisible();

		// Poll cycles to restarting, then linked.
		await expect(
			card.getByText('Applying credentials to your agent…'),
		).toBeVisible({ timeout: 10_000 });
		await expect(
			card.getByText(/Linked to ChatGPT/),
		).toBeVisible({ timeout: 10_000 });
	});

	test('Copy button flashes Copied, Get new code swaps the code, Cancel returns to idle', async ({
		authedPage: page,
		browserName,
	}) => {
		// Clipboard permission must be granted for navigator.clipboard to
		// resolve without prompting; chromium needs this explicitly.
		if (browserName === 'chromium') {
			const context = page.context();
			await context.grantPermissions(['clipboard-read', 'clipboard-write']);
		}

		await mockDashboardStateWithCodex(page, false);
		mockCodexStart(page, ['FIRST-CODE0', 'SECND-CODE1']);
		// Keep pending so the flow doesn't transition away during the test.
		mockCodexStatus(page, [{ status: 'pending' }]);
		await page.reload();

		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		const card = page.locator('div', {
			has: page.getByRole('heading', { name: 'Codex (ChatGPT)' }),
		}).first();
		await card.scrollIntoViewIfNeeded();

		await card.getByRole('button', { name: 'Link Codex Account' }).click();
		await expect(card.getByText('FIRST-CODE0')).toBeVisible({ timeout: 10_000 });

		const copyBtn = card.getByRole('button', { name: 'Copy' });
		await copyBtn.click();
		await expect(card.getByRole('button', { name: 'Copied!' })).toBeVisible({
			timeout: 3_000,
		});

		await card.getByRole('button', { name: 'Get new code' }).click();
		await expect(card.getByText('SECND-CODE1')).toBeVisible({ timeout: 10_000 });
		await expect(card.getByText('FIRST-CODE0')).toHaveCount(0);

		await card.getByRole('button', { name: 'Cancel' }).click();
		await expect(
			card.getByRole('button', { name: 'Link Codex Account' }),
		).toBeVisible();
	});

	test('expiry shows guidance hint when the countdown runs out', async ({
		authedPage: page,
	}) => {
		await mockDashboardStateWithCodex(page, false);
		// Short expiry so the countdown hits 0 within the test timeout.
		await page.route('**/api/instances/*/codex/link/start', (route) => {
			route.fulfill({
				status: 200,
				contentType: 'application/json',
				body: JSON.stringify({
					code: 'EXPR-12345',
					verification_url: 'https://auth.openai.com/codex/device',
					expires_in: 3,
				}),
			});
		});
		mockCodexStatus(page, [{ status: 'pending' }]);
		await page.reload();

		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		const card = page.locator('div', {
			has: page.getByRole('heading', { name: 'Codex (ChatGPT)' }),
		}).first();
		await card.scrollIntoViewIfNeeded();

		await card.getByRole('button', { name: 'Link Codex Account' }).click();
		await expect(card.getByText('EXPR-12345')).toBeVisible({ timeout: 10_000 });

		// After ~3s, the countdown should expire and an error should surface
		// mentioning Device Code Authorization (the most common cause).
		await expect(card.getByText(/Device Code Authorization/i)).toBeVisible({
			timeout: 8_000,
		});
	});

	test('unlink with inline confirm returns the card to Link Codex Account', async ({
		authedPage: page,
	}) => {
		await mockDashboardStateWithCodex(page, true);
		const unlinkCalls = mockCodexUnlink(page);
		await page.reload();

		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		const card = page.locator('div', {
			has: page.getByRole('heading', { name: 'Codex (ChatGPT)' }),
		}).first();
		await card.scrollIntoViewIfNeeded();

		await card.getByRole('button', { name: 'Unlink' }).click();
		// Inline confirm should appear with both Yes/Cancel buttons.
		await expect(card.getByText('Unlink?')).toBeVisible();
		await card.getByRole('button', { name: 'Cancel' }).click();
		// After cancel we should be back to the linked view.
		await expect(card.getByRole('button', { name: 'Unlink' })).toBeVisible();

		// Real unlink.
		await card.getByRole('button', { name: 'Unlink' }).click();
		await card.getByRole('button', { name: 'Yes, unlink' }).click();
		await expect(
			card.getByRole('button', { name: 'Link Codex Account' }),
		).toBeVisible({ timeout: 10_000 });
		expect(unlinkCalls()).toBeGreaterThan(0);
	});
});
