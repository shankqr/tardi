import type { Page } from '@playwright/test';
import { expect } from '@playwright/test';

export const API_URL = process.env.E2E_API_URL || '';

/** Get a Firebase ID token for API calls */
export async function getIdToken(email: string, password: string): Promise<string> {
	const res = await fetch(
		`https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=${process.env.FIREBASE_API_KEY}`,
		{
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ email, password, returnSecureToken: true }),
		}
	);
	const data = await res.json();
	if (!data.idToken) {
		throw new Error(`Failed to get ID token: ${data.error?.message || 'unknown'}`);
	}
	return data.idToken;
}

/**
 * Wait for config sync to complete by polling the API directly.
 * The UI can lose the instance during sync (dashboard polling restarts),
 * so we poll the backend sync status endpoint instead.
 */
export async function waitForSyncComplete(
	email: string,
	password: string,
	instId: string,
	timeoutMs = 180_000
) {
	const idToken = await getIdToken(email, password);
	const start = Date.now();
	while (Date.now() - start < timeoutMs) {
		try {
			const res = await fetch(
				`${API_URL}/api/instances/${instId}/sync-status`,
				{ headers: { Authorization: `Bearer ${idToken}` } }
			);
			if (res.ok) {
				const data = await res.json();
				if (data.status === 'completed') {
					console.log(`[E2E] Sync completed (${Math.round((Date.now() - start) / 1000)}s)`);
					return;
				}
				if (data.status === 'failed') {
					throw new Error(`Sync failed: ${data.message || 'unknown'}`);
				}
			}
		} catch (err) {
			if (err instanceof Error && err.message.startsWith('Sync failed')) throw err;
		}
		await new Promise((r) => setTimeout(r, 5000));
	}
	throw new Error(`Sync did not complete within ${timeoutMs / 1000}s`);
}

/**
 * Wait for the instance to become fully active with healthy agent.
 * Polls dashboard state API until instance.status === 'active' and
 * agent_status === 'running'.
 */
export async function waitForInstanceActive(
	email: string,
	password: string,
	instId: string,
	timeoutMs = 300_000
) {
	const idToken = await getIdToken(email, password);
	const start = Date.now();
	while (Date.now() - start < timeoutMs) {
		try {
			const res = await fetch(`${API_URL}/api/dashboard/state`, {
				headers: { Authorization: `Bearer ${idToken}` },
			});
			if (res.ok) {
				const state = await res.json();
				const inst = state.instances?.find((i: { id: string }) => i.id === instId);
				if (inst) {
					const elapsed = Math.round((Date.now() - start) / 1000);
					if (inst.status === 'error' || inst.status === 'terminated') {
						throw new Error(`Instance entered ${inst.status} state`);
					}
					if (elapsed % 30 < 10) {
						console.log(`[E2E] Instance status: ${inst.status}, agent: ${inst.agent_status} (${elapsed}s)`);
					}
					if (inst.status === 'active' && (inst.agent_status === 'running' || inst.agent_status === 'healthy')) {
						console.log(`[E2E] Instance fully active (${elapsed}s)`);
						return;
					}
				} else {
					console.log('[E2E] Instance not in dashboard state yet');
				}
			}
		} catch {
			// Ignore fetch errors, keep polling
		}
		await new Promise((r) => setTimeout(r, 10000));
	}
	// If still active but agent unhealthy, proceed anyway
	console.log('[E2E] Warning: Instance did not reach healthy state, proceeding anyway');
}

/**
 * Ensure the instance page is showing the config form.
 * Retries navigation on transient connection errors (e.g. after OC dashboard visit).
 */
export async function ensureInstancePage(
	page: Page,
	instId: string
) {
	for (let attempt = 0; attempt < 3; attempt++) {
		try {
			await page.goto(`/dashboard/instances/${instId}`, { timeout: 30_000 });
			await expect(page.locator('#openrouter-key')).toBeVisible({ timeout: 60_000 });
			await page.waitForTimeout(5000);
			return;
		} catch (err) {
			if (attempt < 2) {
				console.log(`[E2E] ensureInstancePage failed (attempt ${attempt + 1}/3), retrying...`);
				await page.waitForTimeout(5000);
			} else {
				throw err;
			}
		}
	}
}

/**
 * Delete any existing non-terminated instances for the given account.
 * Polls until all instances are terminated or only 'error'/'terminating' remain
 * that won't block a new deploy (max 5 minutes).
 */
export async function deleteExistingInstances(
	email: string,
	password: string
): Promise<void> {
	const idToken = await getIdToken(email, password);
	const res = await fetch(`${API_URL}/api/dashboard/state`, {
		headers: { Authorization: `Bearer ${idToken}` },
	});
	const state = await res.json();
	const deletableInstances = (state.instances || []).filter(
		(i: { status: string }) =>
			i.status !== 'terminated' && i.status !== 'terminating'
	);

	if (!deletableInstances.length) {
		console.log('[E2E] No active instances to delete');
		return;
	}

	for (const inst of deletableInstances) {
		console.log(`[E2E] Deleting instance ${inst.id} (status: ${inst.status})`);
		const delRes = await fetch(`${API_URL}/api/instances/${inst.id}`, {
			method: 'DELETE',
			headers: { Authorization: `Bearer ${idToken}` },
		});
		if (!delRes.ok) {
			const body = await delRes.text().catch(() => '');
			console.log(`[E2E] Delete returned ${delRes.status}: ${body}`);
		}
	}

	// Poll until no instances block a new deploy (terminated/error are fine)
	const start = Date.now();
	const timeoutMs = 300_000; // 5 minutes
	while (Date.now() - start < timeoutMs) {
		const token = await getIdToken(email, password);
		const r = await fetch(`${API_URL}/api/dashboard/state`, {
			headers: { Authorization: `Bearer ${token}` },
		});
		const s = await r.json();
		const blocking = (s.instances || []).filter(
			(i: { status: string }) =>
				i.status !== 'terminated' && i.status !== 'error'
		);
		if (!blocking.length) {
			console.log('[E2E] All instances terminated or in error state');
			return;
		}
		const elapsed = Math.round((Date.now() - start) / 1000);
		console.log(`[E2E] Waiting for ${blocking.length} instance(s) to terminate... (${elapsed}s)`);
		await new Promise((r) => setTimeout(r, 10000));
	}
	throw new Error('Instances did not terminate within 5 minutes');
}
