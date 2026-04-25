/**
 * Prod-only cleanup script for the dedicated prod E2E account.
 * Run with: npx tsx e2e/scripts/cleanup-prod.ts
 *
 * Deletes any non-terminated VPS instances on the prod test account so a
 * crashed / killed / cancelled GitHub Actions run never leaks a paid VPS.
 *
 * Required env: E2E_PROD_EMAIL, E2E_PROD_PASSWORD, E2E_API_URL, FIREBASE_API_KEY.
 */

import { config } from 'dotenv';
import { existsSync } from 'fs';

// Load .env.e2e.prod when running locally; CI passes vars via job env directly.
// dotenv must run BEFORE journey-helpers is imported, since it captures
// API_URL at module load time (ESM hoists imports — use dynamic import below).
if (existsSync('.env.e2e.prod')) {
	config({ path: '.env.e2e.prod' });
}

async function main() {
	const email = process.env.E2E_PROD_EMAIL;
	const password = process.env.E2E_PROD_PASSWORD;
	if (!email || !password) {
		console.error('E2E_PROD_EMAIL / E2E_PROD_PASSWORD not set — refusing to run');
		process.exit(2);
	}
	if (!process.env.E2E_API_URL || !process.env.FIREBASE_API_KEY) {
		console.error('E2E_API_URL / FIREBASE_API_KEY not set — refusing to run');
		process.exit(2);
	}

	const { deleteExistingInstances } = await import('../helpers/journey-helpers');
	console.log(`[Prod Cleanup] Sweeping prod E2E account: ${email}`);
	await deleteExistingInstances(email, password);
	console.log('[Prod Cleanup] Done — no VPS instances persist on the prod E2E account');
}

main().catch((err) => {
	console.error('[Prod Cleanup] FAILED:', err);
	process.exit(1);
});
