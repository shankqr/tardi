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
		const instances = (state.instances || []).filter(
			(i: { status: string }) => i.status !== 'terminated' && i.status !== 'terminating'
		);
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
		console.log('\nSetup check complete.');
		return;
	}

	const instance = await checkInstance(idToken);

	if (instance) {
		console.log(`   Instance found: ${instance.id} (status: ${instance.status}, name: ${instance.name})`);
		console.log('\nSetup check complete.');
		return;
	}

	// No active instance — deploy one
	console.log('   No active instance found. Deploying...');
	const apiUrl =
		process.env.E2E_API_URL || 'https://tardi-api-dev-lckw22k4gq-uc.a.run.app';

	const createRes = await fetch(`${apiUrl}/api/instances`, {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${idToken}`,
			'Content-Type': 'application/json',
		},
		body: JSON.stringify({ name: 'e2e-persistent', region: 'eu-central' }),
	});

	if (!createRes.ok) {
		const body = await createRes.text().catch(() => '');
		console.error(`   Deploy failed (${createRes.status}): ${body}`);
		process.exit(1);
	}

	const created = await createRes.json();
	console.log(`   Instance created: ${created.id} (status: ${created.status})`);

	// Poll until active (up to 10 minutes)
	console.log('\n4. Waiting for instance to become active...');
	const timeoutMs = 600_000;
	const start = Date.now();
	while (Date.now() - start < timeoutMs) {
		await new Promise((r) => setTimeout(r, 10_000));
		const token = await getFirebaseIdToken(email, password!);
		if (!token) continue;

		const inst = await checkInstance(token);
		if (inst) {
			const elapsed = Math.round((Date.now() - start) / 1000);
			console.log(`   Status: ${inst.status} (${elapsed}s)`);

			if (inst.status === 'active') {
				console.log(`   Instance active: ${inst.id}`);
				console.log('\nSetup complete — persistent instance is ready.');
				return;
			}
			if (inst.status === 'error' || inst.status === 'terminated') {
				console.error(`   Instance entered ${inst.status} state.`);
				process.exit(1);
			}
		}
	}

	console.error('   Timed out waiting for instance to become active.');
	process.exit(1);
}

setup().catch((err) => {
	console.error('Setup failed:', err);
	process.exit(1);
});
