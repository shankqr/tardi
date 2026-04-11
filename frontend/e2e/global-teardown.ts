import { hasTestState, loadTestState, clearTestState, type AccountState } from './helpers/test-state';
import { deleteTestUser } from './helpers/firebase-admin';
import { deleteStripeCustomer } from './helpers/stripe';
import { deleteExistingInstances } from './helpers/journey-helpers';

async function cleanupAccount(label: string, account: AccountState) {
	console.log(`[Teardown:${label}] Cleaning up ${account.email}`);

	try {
		await deleteExistingInstances(account.email, account.password);
		console.log(`[Teardown:${label}] Instances deleted`);
	} catch (err) {
		console.error(`[Teardown:${label}] Instance cleanup failed:`, err);
	}

	try {
		await deleteStripeCustomer(account.email);
		console.log(`[Teardown:${label}] Stripe customer deleted`);
	} catch (err) {
		console.error(`[Teardown:${label}] Stripe cleanup failed:`, err);
	}

	try {
		await deleteTestUser(account.email);
		console.log(`[Teardown:${label}] Firebase user deleted`);
	} catch (err) {
		console.error(`[Teardown:${label}] Firebase cleanup failed:`, err);
	}
}

export default async function globalTeardown() {
	if (!hasTestState()) {
		console.log('[Teardown] No test state — nothing to clean up');
		return;
	}

	const state = loadTestState();

	// Clean up both accounts in parallel
	await Promise.all([
		cleanupAccount('openclaw', state.openclaw),
		cleanupAccount('hermes', state.hermes),
	]);

	clearTestState();
	console.log('[Teardown] All accounts cleaned up, no VPS instances remain');
}
