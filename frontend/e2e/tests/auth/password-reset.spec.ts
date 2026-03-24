import { test, expect } from '@playwright/test';

test.describe('Password reset', () => {
	test('forgot password toggle shows reset form', async ({ page }) => {
		await page.goto('/login');

		const signInBtn = page.getByRole('button', { name: 'Sign in' });
		await expect(signInBtn).toBeVisible({ timeout: 10_000 });
		await page.waitForTimeout(1000);

		// Click "Forgot password?" link
		await page.getByRole('button', { name: 'Forgot password?' }).click();

		// Verify the heading changed
		await expect(page.getByRole('heading', { name: 'Reset your password' })).toBeVisible({ timeout: 10_000 });

		// Verify email input is still present
		await expect(page.locator('#email')).toBeVisible();

		// Verify password field is hidden in forgot mode
		await expect(page.locator('#password')).not.toBeVisible();

		// Verify "Send reset link" button appears
		await expect(page.getByRole('button', { name: 'Send reset link' })).toBeVisible();
	});

	test('reset email sent shows confirmation', async ({ page }) => {
		await page.goto('/login');

		const signInBtn = page.getByRole('button', { name: 'Sign in' });
		await expect(signInBtn).toBeVisible({ timeout: 10_000 });
		await page.waitForTimeout(1000);

		// Switch to forgot password mode
		await page.getByRole('button', { name: 'Forgot password?' }).click();
		await expect(page.getByRole('button', { name: 'Send reset link' })).toBeVisible();
		await page.waitForTimeout(500);

		// Enter email and submit
		await page.locator('#email').click();
		await page.locator('#email').pressSequentially('reset-test@tardi-test.ai', { delay: 20 });

		await page.getByRole('button', { name: 'Send reset link' }).click();

		// Should show confirmation message
		await expect(page.getByText('Check your email for a password reset link')).toBeVisible({
			timeout: 10_000,
		});
	});

	test('back to login returns to login form', async ({ page }) => {
		await page.goto('/login');

		const signInBtn = page.getByRole('button', { name: 'Sign in' });
		await expect(signInBtn).toBeVisible({ timeout: 10_000 });

		// Switch to forgot password mode
		await page.getByRole('button', { name: 'Forgot password?' }).click();
		await expect(page.getByRole('heading', { name: 'Reset your password' })).toBeVisible({ timeout: 10_000 });

		// Click "Back to login"
		await page.getByRole('button', { name: 'Back to login' }).click();

		// Verify login form is back
		await expect(page.getByText('Log in to Tardi')).toBeVisible();
		await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible();
		await expect(page.locator('#password')).toBeVisible();
	});

	test('after reset sent, back to login returns to login form', async ({ page }) => {
		await page.goto('/login');

		const signInBtn = page.getByRole('button', { name: 'Sign in' });
		await expect(signInBtn).toBeVisible({ timeout: 10_000 });
		await page.waitForTimeout(1000);

		// Switch to forgot mode and send reset
		await page.getByRole('button', { name: 'Forgot password?' }).click();
		await page.waitForTimeout(500);

		await page.locator('#email').click();
		await page.locator('#email').pressSequentially('reset-back-test@tardi-test.ai', { delay: 20 });

		await page.getByRole('button', { name: 'Send reset link' }).click();

		// Wait for confirmation
		await expect(page.getByText('Check your email for a password reset link')).toBeVisible({
			timeout: 10_000,
		});

		// The "Back to login" button appears after reset is sent
		await page.getByRole('button', { name: 'Back to login' }).click();

		// Verify we're back to the normal login form
		await expect(page.getByText('Log in to Tardi')).toBeVisible();
		await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible();
	});
});
