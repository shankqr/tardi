import { writable, derived } from 'svelte/store';
import type { User } from 'firebase/auth';

const USE_MOCK_AUTH = import.meta.env.VITE_USE_MOCK_AUTH === 'true';

interface AuthState {
	user: User | null;
	loading: boolean;
	error: string | null;
}

const authState = writable<AuthState>({
	user: null,
	loading: true,
	error: null
});

let initialized = false;

function createMockUser(email: string): User {
	return {
		uid: 'mock-uid-12345',
		email,
		emailVerified: true,
		displayName: email.split('@')[0],
		photoURL: null,
		phoneNumber: null,
		isAnonymous: false,
		providerId: 'mock',
		metadata: {},
		providerData: [],
		refreshToken: '',
		tenantId: null,
		delete: async () => {},
		getIdToken: async () => 'mock-token',
		getIdTokenResult: async () => ({}) as any,
		reload: async () => {},
		toJSON: () => ({})
	} as unknown as User;
}

export function initAuth() {
	if (initialized) return;
	initialized = true;

	if (USE_MOCK_AUTH) {
		authState.set({ user: null, loading: false, error: null });
		return;
	}

	import('$lib/firebase').then(({ getFirebaseAuth }) => {
		import('firebase/auth').then(({ onAuthStateChanged }) => {
			const auth = getFirebaseAuth();
			onAuthStateChanged(auth, (user) => {
				authState.set({ user, loading: false, error: null });
			});
		});
	});
}

export const user = derived(authState, ($state) => $state.user);
export const authLoading = derived(authState, ($state) => $state.loading);
export const authError = derived(authState, ($state) => $state.error);
export const isAuthenticated = derived(authState, ($state) => !!$state.user && !$state.loading);
export const emailVerified = derived(authState, ($state) => $state.user?.emailVerified ?? false);

export async function signIn(email: string, password: string) {
	authState.update((s) => ({ ...s, error: null }));

	if (USE_MOCK_AUTH) {
		authState.set({ user: createMockUser(email), loading: false, error: null });
		return;
	}

	const { getFirebaseAuth } = await import('$lib/firebase');
	const { signInWithEmailAndPassword } = await import('firebase/auth');
	const auth = getFirebaseAuth();
	try {
		await signInWithEmailAndPassword(auth, email, password);
	} catch (err) {
		const message = err instanceof Error ? err.message : 'Sign in failed';
		authState.update((s) => ({ ...s, error: message }));
		throw err;
	}
}

export async function signUp(email: string, password: string) {
	authState.update((s) => ({ ...s, error: null }));

	if (USE_MOCK_AUTH) {
		authState.set({ user: createMockUser(email), loading: false, error: null });
		return;
	}

	const { getFirebaseAuth } = await import('$lib/firebase');
	const { createUserWithEmailAndPassword } = await import('firebase/auth');
	const auth = getFirebaseAuth();
	try {
		await createUserWithEmailAndPassword(auth, email, password);
	} catch (err) {
		const message = err instanceof Error ? err.message : 'Sign up failed';
		authState.update((s) => ({ ...s, error: message }));
		throw err;
	}
}

export async function signInWithGoogle() {
	authState.update((s) => ({ ...s, error: null }));

	if (USE_MOCK_AUTH) {
		authState.set({ user: createMockUser('mock@google.com'), loading: false, error: null });
		return;
	}

	const { getFirebaseAuth } = await import('$lib/firebase');
	const { GoogleAuthProvider, signInWithPopup } = await import('firebase/auth');
	const auth = getFirebaseAuth();
	try {
		const provider = new GoogleAuthProvider();
		await signInWithPopup(auth, provider);
	} catch (err) {
		const message = err instanceof Error ? err.message : 'Google sign-in failed';
		authState.update((s) => ({ ...s, error: message }));
		throw err;
	}
}

export async function signOut() {
	if (USE_MOCK_AUTH) {
		authState.set({ user: null, loading: false, error: null });
		return;
	}

	const { getFirebaseAuth } = await import('$lib/firebase');
	const { signOut: firebaseSignOut } = await import('firebase/auth');
	const auth = getFirebaseAuth();
	await firebaseSignOut(auth);
}

export async function resetPassword(email: string) {
	authState.update((s) => ({ ...s, error: null }));

	if (USE_MOCK_AUTH) {
		return;
	}

	const { getFirebaseAuth } = await import('$lib/firebase');
	const { sendPasswordResetEmail } = await import('firebase/auth');
	const auth = getFirebaseAuth();
	try {
		await sendPasswordResetEmail(auth, email);
	} catch (err) {
		const message = err instanceof Error ? err.message : 'Password reset failed';
		authState.update((s) => ({ ...s, error: message }));
		throw err;
	}
}

export async function sendVerificationEmail() {
	if (USE_MOCK_AUTH) return;
	const { getFirebaseAuth } = await import('$lib/firebase');
	const { sendEmailVerification } = await import('firebase/auth');
	const auth = getFirebaseAuth();
	if (auth.currentUser) {
		await sendEmailVerification(auth.currentUser);
	}
}

export async function reloadUser(): Promise<boolean> {
	if (USE_MOCK_AUTH) return true;
	const { getFirebaseAuth } = await import('$lib/firebase');
	const auth = getFirebaseAuth();
	if (auth.currentUser) {
		await auth.currentUser.reload();
		authState.set({ user: auth.currentUser, loading: false, error: null });
		return auth.currentUser.emailVerified;
	}
	return false;
}

export async function forceTokenRefresh(): Promise<void> {
	if (USE_MOCK_AUTH) return;
	const { getFirebaseAuth } = await import('$lib/firebase');
	const auth = getFirebaseAuth();
	if (auth.currentUser) {
		await auth.currentUser.getIdToken(true);
	}
}

export async function getIdToken(): Promise<string | null> {
	if (USE_MOCK_AUTH) {
		return 'mock-token';
	}

	const { getFirebaseAuth } = await import('$lib/firebase');
	const auth = getFirebaseAuth();
	const currentUser = auth.currentUser;
	if (!currentUser) return null;
	return currentUser.getIdToken();
}
