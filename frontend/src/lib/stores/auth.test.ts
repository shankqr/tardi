import { describe, it, expect, vi, beforeEach } from 'vitest';
import { get } from 'svelte/store';

// We need to set VITE_USE_MOCK_AUTH before importing the module.
// vitest respects import.meta.env, so we set it via vi.stubEnv.
vi.stubEnv('VITE_USE_MOCK_AUTH', 'true');

// Mock Sentry to avoid import issues
vi.mock('@sentry/sveltekit', () => ({
	captureException: vi.fn(),
	addBreadcrumb: vi.fn(),
	setContext: vi.fn()
}));

// Mock $lib/firebase so it never gets loaded
vi.mock('$lib/firebase', () => ({
	getFirebaseAuth: vi.fn()
}));

const {
	user,
	authLoading,
	authError,
	isAuthenticated,
	emailVerified,
	initAuth,
	signIn,
	signUp,
	signOut,
	signInWithGoogle,
	getIdToken,
	resetPassword,
	reloadUser
} = await import('./auth');

describe('auth store (mock mode)', () => {
	beforeEach(() => {
		// Reset auth state by signing out
		signOut();
	});

	describe('initial state', () => {
		it('user is null before initAuth', () => {
			// After signOut, user should be null
			expect(get(user)).toBeNull();
		});

		it('authError is null initially', () => {
			expect(get(authError)).toBeNull();
		});
	});

	describe('initAuth', () => {
		it('sets loading to false in mock mode', () => {
			initAuth();
			expect(get(authLoading)).toBe(false);
		});

		it('user remains null after initAuth', () => {
			initAuth();
			expect(get(user)).toBeNull();
		});
	});

	describe('signIn', () => {
		it('sets user with provided email', async () => {
			await signIn('test@example.com', 'password');
			const u = get(user);
			expect(u).not.toBeNull();
			expect(u?.email).toBe('test@example.com');
			expect(u?.uid).toBe('mock-uid-12345');
		});

		it('sets isAuthenticated to true after sign in', async () => {
			await signIn('test@example.com', 'password');
			expect(get(isAuthenticated)).toBe(true);
		});

		it('sets emailVerified to true', async () => {
			await signIn('test@example.com', 'password');
			expect(get(emailVerified)).toBe(true);
		});
	});

	describe('signUp', () => {
		it('sets user with provided email', async () => {
			await signUp('new@example.com', 'password123');
			const u = get(user);
			expect(u).not.toBeNull();
			expect(u?.email).toBe('new@example.com');
		});

		it('sets displayName from email prefix', async () => {
			await signUp('testuser@example.com', 'password');
			expect(get(user)?.displayName).toBe('testuser');
		});
	});

	describe('signInWithGoogle', () => {
		it('sets user with mock google email', async () => {
			await signInWithGoogle();
			const u = get(user);
			expect(u).not.toBeNull();
			expect(u?.email).toBe('mock@google.com');
		});
	});

	describe('signOut', () => {
		it('clears user', async () => {
			await signIn('test@example.com', 'password');
			expect(get(user)).not.toBeNull();

			await signOut();
			expect(get(user)).toBeNull();
		});

		it('sets isAuthenticated to false', async () => {
			await signIn('test@example.com', 'password');
			await signOut();
			expect(get(isAuthenticated)).toBe(false);
		});
	});

	describe('getIdToken', () => {
		it('returns mock-token in mock mode', async () => {
			const token = await getIdToken();
			expect(token).toBe('mock-token');
		});
	});

	describe('resetPassword', () => {
		it('resolves without error in mock mode', async () => {
			await expect(resetPassword('test@example.com')).resolves.toBeUndefined();
		});
	});

	describe('reloadUser', () => {
		it('returns true in mock mode', async () => {
			const result = await reloadUser();
			expect(result).toBe(true);
		});
	});
});
