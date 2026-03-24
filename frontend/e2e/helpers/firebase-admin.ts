import admin from 'firebase-admin';
import { config } from 'dotenv';

config({ path: '.env.e2e' });

let app: admin.app.App | null = null;

function getApp(): admin.app.App {
	if (app) return app;

	const projectId = process.env.FIREBASE_PROJECT_ID;
	const clientEmail = process.env.FIREBASE_CLIENT_EMAIL;
	const privateKeyBase64 = process.env.FIREBASE_PRIVATE_KEY;

	if (!projectId || !clientEmail || !privateKeyBase64) {
		throw new Error(
			'Missing Firebase Admin env vars: FIREBASE_PROJECT_ID, FIREBASE_CLIENT_EMAIL, FIREBASE_PRIVATE_KEY'
		);
	}

	const privateKey = Buffer.from(privateKeyBase64, 'base64').toString('utf-8');

	const credential = admin.credential.cert({ projectId, clientEmail, privateKey });
	app = admin.initializeApp({ credential });

	return app;
}

export async function createTestUser(
	email: string,
	password: string
): Promise<admin.auth.UserRecord> {
	const auth = getApp().auth();
	const user = await auth.createUser({
		email,
		password,
		emailVerified: true, // Pre-verify so we can test the checkout flow directly
	});
	return user;
}

export async function verifyUserEmail(uid: string): Promise<void> {
	const auth = getApp().auth();
	await auth.updateUser(uid, { emailVerified: true });
}

export async function getTestUserByEmail(
	email: string
): Promise<admin.auth.UserRecord | null> {
	try {
		const auth = getApp().auth();
		return await auth.getUserByEmail(email);
	} catch (err: unknown) {
		const code = (err as { code?: string }).code;
		if (code === 'auth/user-not-found') return null;
		console.error(`[Firebase Admin] getUserByEmail failed:`, err);
		return null;
	}
}

export async function deleteTestUser(email: string): Promise<void> {
	const user = await getTestUserByEmail(email);
	if (user) {
		const auth = getApp().auth();
		await auth.deleteUser(user.uid);
	}
}

export async function listTestUsers(): Promise<admin.auth.UserRecord[]> {
	const auth = getApp().auth();
	const result = await auth.listUsers(1000);
	return result.users.filter((u) => u.email?.match(/^clawmyway\+\d+@gmail\.com$/));
}
