/**
 * Setup script for persistent test account used by dashboard/instance E2E tests.
 * Run with: npm run test:e2e:setup
 *
 * This script:
 * 1. Ensures a Firebase user exists with a verified email
 * 2. Checks Stripe for an active subscription
 * 3. Checks the Tardi API for an active instance
 */

import { config } from 'dotenv';
config({ path: '.env.e2e' });

import { getTestUserByEmail, createTestUser, verifyUserEmail } from '../helpers/firebase-admin';
import Stripe from 'stripe';

const email = process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+persistent@gmail.com';
const password = process.env.E2E_PERSISTENT_PASSWORD || process.env.E2E_TEST_PASSWORD;

if (!password) {
	console.error('Missing E2E_PERSISTENT_PASSWORD or E2E_TEST_PASSWORD env var');
	process.exit(1);
}

async function getStripe(): Promise<Stripe> {
	const secretKey = process.env.STRIPE_SECRET_KEY;
	if (!secretKey) {
		throw new Error('Missing STRIPE_SECRET_KEY env var');
	}
	return new Stripe(secretKey);
}

async function checkStripeSubscription(customerEmail: string): Promise<boolean> {
	const stripe = await getStripe();
	const customers = await stripe.customers.list({ email: customerEmail, limit: 10 });

	for (const customer of customers.data) {
		const subs = await stripe.subscriptions.list({
			customer: customer.id,
			status: 'active',
		});
		if (subs.data.length > 0) {
			return true;
		}
	}
	return false;
}

async function getFirebaseIdToken(userEmail: string, userPassword: string): Promise<string | null> {
	const apiKey = process.env.FIREBASE_API_KEY;
	if (!apiKey) return null;

	try {
		const res = await fetch(
			`https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=${apiKey}`,
			{
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					email: userEmail,
					password: userPassword,
					returnSecureToken: true,
				}),
			}
		);

		if (!res.ok) return null;
		const data = await res.json();
		return data.idToken || null;
	} catch {
		return null;
	}
}

async function checkInstance(
	idToken: string
): Promise<{ id: string; status: string; name: string } | null> {
	const apiUrl =
		process.env.E2E_API_URL || 'https://tardi-api-dev-lckw22k4gq-uc.a.run.app';

	try {
		const res = await fetch(`${apiUrl}/api/dashboard/state`, {
			headers: { Authorization: `Bearer ${idToken}` },
		});

		if (!res.ok) return null;
		const state = await res.json();
		const instances = state.instances || [];
		if (instances.length === 0) return null;

		const instance = instances[0];
		return { id: instance.id, status: instance.status, name: instance.name };
	} catch {
		return null;
	}
}

async function setup() {
	console.log(`Setting up persistent test account: ${email}\n`);

	// Step 1: Ensure Firebase user exists
	console.log('1. Checking Firebase user...');
	let user = await getTestUserByEmail(email);

	if (user) {
		console.log(`   User exists (uid: ${user.uid}, verified: ${user.emailVerified})`);
		if (!user.emailVerified) {
			console.log('   Email not verified, verifying now...');
			await verifyUserEmail(user.uid);
			console.log('   Email verified.');
		}
	} else {
		console.log('   User does not exist, creating...');
		user = await createTestUser(email, password);
		console.log(`   User created (uid: ${user.uid})`);
	}

	// Step 2: Check Stripe subscription
	console.log('\n2. Checking Stripe subscription...');
	const hasSubscription = await checkStripeSubscription(email);

	if (hasSubscription) {
		console.log('   Active subscription found.');
	} else {
		console.log('   No active subscription found.');
		console.log('   -> Please complete checkout manually or run the journey test first.');
		console.log('   -> npm run test:e2e -- --grep "full flow"');
	}

	// Step 3: Check instance via Tardi API
	console.log('\n3. Checking Tardi API for active instance...');
	const idToken = await getFirebaseIdToken(email, password);

	if (!idToken) {
		console.log('   Could not obtain Firebase ID token. Skipping instance check.');
	} else {
		const instance = await checkInstance(idToken);

		if (instance) {
			console.log(`   Instance found: ${instance.id} (status: ${instance.status}, name: ${instance.name})`);
		} else {
			console.log('   No active instance found.');
			console.log('   -> Please deploy an instance via the dashboard or run the journey test first.');
		}
	}

	console.log('\nSetup check complete.');
}

setup().catch((err) => {
	console.error('Setup failed:', err);
	process.exit(1);
});
