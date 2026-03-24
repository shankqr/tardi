import { test, expect } from '@playwright/test';
import {
	createTestUser,
	deleteTestUser,
} from '../../helpers/firebase-admin';

test.describe('Signup validation', () => {
	test('password mismatch shows error', async ({ page }) => {
		await page.goto('/signup');

		const createBtn = page.getByRole('button', { name: 'Create account' });
		await expect(createBtn).toBeVisible({ timeout: 10_000 });
		await expect(createBtn).toBeEnabled();
		await page.waitForTimeout(1000);

		await page.locator('#email').click();
		await page.locator('#email').pressSequentially('mismatch-test@tardi-test.ai', { delay: 20 });
		await page.locator('#password').click();
		await page.locator('#password').pressSequentially('Test1234!', { delay: 20 });
		await page.locator('#confirm-password').click();
		await page.locator('#confirm-password').pressSequentially('Different1!', { delay: 20 });

		await createBtn.click();

		const errorDiv = page.locator('.bg-red-50');
		await expect(errorDiv).toBeVisible({ timeout: 5_000 });
		await expect(errorDiv).toContainText('Passwords do not match');
	});

	test('weak password shows Firebase error', async ({ page }) => {
		await page.goto('/signup');

		const createBtn = page.getByRole('button', { name: 'Create account' });
		await expect(createBtn).toBeVisible({ timeout: 10_000 });
		await expect(createBtn).toBeEnabled();
		await page.waitForTimeout(1000);

		await page.locator('#email').click();
		await page.locator('#email').pressSequentially('weak-pw-test@tardi-test.ai', { delay: 20 });
		await page.locator('#password').click();
		await page.locator('#password').pressSequentially('123', { delay: 20 });
		await page.locator('#confirm-password').click();
		await page.locator('#confirm-password').pressSequentially('123', { delay: 20 });

		// The form has minlength=8 on password fields — disable HTML validation
		// entirely by setting noValidate on the form, which is more reliable than
		// removing individual attributes (Svelte may re-apply them on re-render)
		await page.locator('form').evaluate((form) => {
			(form as HTMLFormElement).noValidate = true;
		});

		await createBtn.click();

		const errorDiv = page.locator('.bg-red-50');
		await expect(errorDiv).toBeVisible({ timeout: 10_000 });
		await expect(errorDiv).toContainText('too weak');
	});

	test('duplicate email shows error', async ({ page }) => {
		const timestamp = Date.now();
		const email = `e2e-signup-dup-${timestamp}@tardi-test.ai`;
		const password = 'Test1234!secure';

		// Pre-create the user via Firebase Admin
		await createTestUser(email, password);

		try {
			await page.goto('/signup');

			const createBtn = page.getByRole('button', { name: 'Create account' });
			await expect(createBtn).toBeVisible({ timeout: 10_000 });
			await expect(createBtn).toBeEnabled();
			await page.waitForTimeout(1000);

			await page.locator('#email').click();
			await page.locator('#email').pressSequentially(email, { delay: 20 });
			await page.locator('#password').click();
			await page.locator('#password').pressSequentially(password, { delay: 20 });
			await page.locator('#confirm-password').click();
			await page.locator('#confirm-password').pressSequentially(password, { delay: 20 });

			await createBtn.click();

			const errorDiv = page.locator('.bg-red-50');
			await expect(errorDiv).toBeVisible({ timeout: 10_000 });
			await expect(errorDiv).toContainText('already exists');
		} finally {
			await deleteTestUser(email);
		}
	});

	test('empty form submit triggers HTML validation', async ({ page }) => {
		await page.goto('/signup');

		const createBtn = page.getByRole('button', { name: 'Create account' });
		await expect(createBtn).toBeVisible({ timeout: 10_000 });
		await expect(createBtn).toBeEnabled();
		await page.waitForTimeout(1000);

		await createBtn.click();

		// The form uses required attributes, so the browser prevents submission.
		// We should still be on /signup (no navigation happened).
		expect(page.url()).toContain('/signup');

		// Verify the email input shows the browser's built-in validation message
		const emailInput = page.locator('#email');
		const validationMessage = await emailInput.evaluate(
			(el) => (el as HTMLInputElement).validationMessage
		);
		expect(validationMessage).toBeTruthy();
	});
});
