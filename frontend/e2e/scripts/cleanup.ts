/**
 * Standalone cleanup script for E2E test resources.
 * Run with: npx tsx e2e/scripts/cleanup.ts
 *
 * Cleans up:
 * - Firebase test users matching e2e-*@tardi-test.ai
 * - Their Stripe subscriptions and customers
 * - Their VPS instances via backend API
 */

import { config } from 'dotenv';
config({ path: '.env.e2e' });

import { listTestUsers, deleteTestUser } from '../helpers/firebase-admin';
import { deleteStripeCustomer } from '../helpers/stripe';

async function cleanup() {
	console.log('🔍 Looking for E2E test users...');

	const users = await listTestUsers();

	if (users.length === 0) {
		console.log('✅ No test users found. Nothing to clean up.');
		return;
	}

	console.log(`Found ${users.length} test user(s):`);
	for (const user of users) {
		console.log(`  - ${user.email} (uid: ${user.uid}, created: ${user.metadata.creationTime})`);
	}

	for (const user of users) {
		const email = user.email!;
		console.log(`\n🧹 Cleaning up ${email}...`);

		// Try to delete instances via API
		try {
			const apiUrl =
				process.env.E2E_API_URL ||
				'https://tardi-api-dev-867058695579.europe-west1.run.app';
			const apiKey = process.env.FIREBASE_API_KEY;

			if (apiKey) {
				// Get Firebase ID token via REST API
				const tokenRes = await fetch(
					`https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=${apiKey}`,
					{
						method: 'POST',
						headers: { 'Content-Type': 'application/json' },
						body: JSON.stringify({
							email,
							password: process.env.E2E_TEST_PASSWORD,
							returnSecureToken: true,
						}),
					}
				);

				if (tokenRes.ok) {
					const tokenData = await tokenRes.json();
					const idToken = tokenData.idToken;

					// Get dashboard state to find instances
					const dashRes = await fetch(`${apiUrl}/api/dashboard/state`, {
						headers: { Authorization: `Bearer ${idToken}` },
					});

					if (dashRes.ok) {
						const dashState = await dashRes.json();
						for (const instance of dashState.instances || []) {
							console.log(`  Deleting instance ${instance.id} (${instance.name})...`);
							await fetch(`${apiUrl}/api/instances/${instance.id}`, {
								method: 'DELETE',
								headers: { Authorization: `Bearer ${idToken}` },
							});
							console.log(`  ✅ Instance deleted`);
						}
					}
				}
			}
		} catch (err) {
			console.error(`  ⚠️ Failed to delete instances:`, err);
		}

		// Delete Stripe customer and subscriptions
		try {
			await deleteStripeCustomer(email);
			console.log(`  ✅ Stripe customer deleted`);
		} catch (err) {
			console.error(`  ⚠️ Failed to clean up Stripe:`, err);
		}

		// Delete Firebase user
		try {
			await deleteTestUser(email);
			console.log(`  ✅ Firebase user deleted`);
		} catch (err) {
			console.error(`  ⚠️ Failed to delete Firebase user:`, err);
		}
	}

	console.log('\n✅ Cleanup complete.');
}

cleanup().catch((err) => {
	console.error('Cleanup failed:', err);
	process.exit(1);
});
