import { test, expect } from '@playwright/test';
import {
	createTestUser,
	deleteTestUser,
	verifyUserEmail,
} from '../../helpers/firebase-admin';

test.describe('Login validation', () => {
	test('wrong password shows error', async ({ page }) => {
		const timestamp = Date.now();
		const email = `e2e-login-wrongpw-${timestamp}@tardi-test.ai`;
		const password = 'CorrectPass123!';

		// Create a real user to test wrong password against
		const user = await createTestUser(email, password);

		try {
			await page.goto('/login');

			const signInBtn = page.getByRole('button', { name: 'Sign in' });
			await expect(signInBtn).toBeVisible({ timeout: 10_000 });
			await expect(signInBtn).toBeEnabled();
			await page.waitForTimeout(1000);

			await page.locator('#email').click();
			await page.locator('#email').pressSequentially(email, { delay: 20 });
			await page.locator('#password').click();
			await page.locator('#password').pressSequentially('WrongPassword99!', { delay: 20 });

			await signInBtn.click();

			const errorDiv = page.locator('.bg-red-50');
			await expect(errorDiv).toBeVisible({ timeout: 10_000 });
			await expect(errorDiv).toContainText('Incorrect');
		} finally {
			await deleteTestUser(email);
		}
	});

	test('nonexistent account shows error', async ({ page }) => {
		await page.goto('/login');

		const signInBtn = page.getByRole('button', { name: 'Sign in' });
		await expect(signInBtn).toBeVisible({ timeout: 10_000 });
		await expect(signInBtn).toBeEnabled();
		await page.waitForTimeout(1000);

		const fakeEmail = `e2e-nonexistent-${Date.now()}@tardi-test.ai`;
		await page.locator('#email').click();
		await page.locator('#email').pressSequentially(fakeEmail, { delay: 20 });
		await page.locator('#password').click();
		await page.locator('#password').pressSequentially('SomePassword123!', { delay: 20 });

		await signInBtn.click();

		const errorDiv = page.locator('.bg-red-50');
		await expect(errorDiv).toBeVisible({ timeout: 10_000 });
		// Firebase returns auth/invalid-credential for both wrong password and nonexistent user
		await expect(errorDiv).toContainText('Incorrect');
	});

	test('empty form submit triggers HTML validation', async ({ page }) => {
		await page.goto('/login');

		const signInBtn = page.getByRole('button', { name: 'Sign in' });
		await expect(signInBtn).toBeVisible({ timeout: 10_000 });
		await expect(signInBtn).toBeEnabled();
		await page.waitForTimeout(1000);

		await signInBtn.click();

		// The form uses required attributes, so the browser prevents submission.
		// We should still be on /login (no navigation happened).
		expect(page.url()).toContain('/login');

		const emailInput = page.locator('#email');
		const validationMessage = await emailInput.evaluate(
			(el) => (el as HTMLInputElement).validationMessage
		);
		expect(validationMessage).toBeTruthy();
	});

	test('successful login redirects to dashboard', async ({ page }) => {
		const timestamp = Date.now();
		const email = `e2e-login-ok-${timestamp}@tardi-test.ai`;
		const password = 'ValidPass123!xyz';

		// Create a verified user
		const user = await createTestUser(email, password);
		await verifyUserEmail(user.uid);

		try {
			await page.goto('/login');

			const signInBtn = page.getByRole('button', { name: 'Sign in' });
			await expect(signInBtn).toBeVisible({ timeout: 10_000 });
			await expect(signInBtn).toBeEnabled();
			await page.waitForTimeout(1000);

			await page.locator('#email').click();
			await page.locator('#email').pressSequentially(email, { delay: 20 });
			await page.locator('#password').click();
			await page.locator('#password').pressSequentially(password, { delay: 20 });

			await signInBtn.click();

			await page.waitForURL('**/dashboard**', { timeout: 15_000 });
			expect(page.url()).toContain('/dashboard');
		} finally {
			await deleteTestUser(email);
		}
	});

	test('unverified email redirects to verify-email', async ({ page }) => {
		const timestamp = Date.now();
		const email = `e2e-login-unverified-${timestamp}@tardi-test.ai`;
		const password = 'ValidPass123!xyz';

		// Create user WITHOUT verifying email
		// createTestUser sets emailVerified: true by default, so we need to unset it
		const user = await createTestUser(email, password);
		// Explicitly mark as unverified
		const { default: admin } = await import('firebase-admin');
		const auth = admin.app().auth();
		await auth.updateUser(user.uid, { emailVerified: false });

		try {
			await page.goto('/login');

			const signInBtn = page.getByRole('button', { name: 'Sign in' });
			await expect(signInBtn).toBeVisible({ timeout: 10_000 });
			await expect(signInBtn).toBeEnabled();
			await page.waitForTimeout(1000);

			await page.locator('#email').click();
			await page.locator('#email').pressSequentially(email, { delay: 20 });
			await page.locator('#password').click();
			await page.locator('#password').pressSequentially(password, { delay: 20 });

			await signInBtn.click();

			await page.waitForURL('**/verify-email**', { timeout: 15_000 });
			expect(page.url()).toContain('/verify-email');
		} finally {
			await deleteTestUser(email);
		}
	});
});
