import { hasTestState, loadTestState, clearTestState } from './helpers/test-state';
import { deleteTestUser } from './helpers/firebase-admin';
import { deleteStripeCustomer } from './helpers/stripe';
import { getIdToken, deleteExistingInstances } from './helpers/journey-helpers';

export default async function globalTeardown() {
	if (!hasTestState()) {
		console.log('[Teardown] No test state — nothing to clean up');
		return;
	}

	const state = loadTestState();
	console.log(`[Teardown] Cleaning up account: ${state.email}`);

	// Delete instance
	try {
		await deleteExistingInstances(state.email, state.password);
		console.log('[Teardown] Instances deleted');
	} catch (err) {
		console.error('[Teardown] Instance cleanup failed:', err);
	}

	// Delete Stripe customer
	try {
		await deleteStripeCustomer(state.email);
		console.log('[Teardown] Stripe customer deleted');
	} catch (err) {
		console.error('[Teardown] Stripe cleanup failed:', err);
	}

	// Delete Firebase user
	try {
		await deleteTestUser(state.email);
		console.log('[Teardown] Firebase user deleted');
	} catch (err) {
		console.error('[Teardown] Firebase cleanup failed:', err);
	}

	clearTestState();
	console.log('[Teardown] State cleared');
}
