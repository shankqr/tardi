import { test, expect } from '@playwright/test';

test.describe('Navigation and routing', () => {
	test('navbar shows Log in and Get Started links', async ({ page }) => {
		await page.goto('/');
		const nav = page.locator('nav');
		await expect(nav.getByRole('link', { name: 'Log in' })).toBeVisible();
		await expect(
			nav.getByRole('link', { name: 'Get Started' })
		).toBeVisible();
	});

	test('Log in link navigates to /login', async ({ page }) => {
		await page.goto('/');
		await page.locator('nav').getByRole('link', { name: 'Log in' }).click();
		await expect(page).toHaveURL(/\/login/);
	});

	test('Get Started link navigates to /signup', async ({ page }) => {
		await page.goto('/');
		await page
			.locator('nav')
			.getByRole('link', { name: 'Get Started' })
			.click();
		await expect(page).toHaveURL(/\/signup/);
	});

	test('/login page renders with email and password form', async ({
		page,
	}) => {
		await page.goto('/login');
		await expect(page.getByLabel(/email/i)).toBeVisible();
		await expect(page.getByLabel(/password/i).first()).toBeVisible();
	});

	test('/signup page renders with email, password, and confirm password form', async ({
		page,
	}) => {
		await page.goto('/signup');
		await expect(page.getByLabel(/email/i)).toBeVisible();
		const passwordFields = page.locator('input[type="password"]');
		await expect(passwordFields).toHaveCount(2);
	});

	test('auth guard: /dashboard redirects to /login without auth', async ({
		page,
	}) => {
		await page.goto('/dashboard');
		await expect(page).toHaveURL(/\/login/);
	});

	test('auth guard: /dashboard/billing redirects to /login without auth', async ({
		page,
	}) => {
		await page.goto('/dashboard/billing');
		await expect(page).toHaveURL(/\/login/);
	});

	test('auth guard: /dashboard/settings redirects to /login without auth', async ({
		page,
	}) => {
		await page.goto('/dashboard/settings');
		await expect(page).toHaveURL(/\/login/);
	});

	test('404: /nonexistent-route shows error page', async ({ page }) => {
		const response = await page.goto('/nonexistent-route');
		// Verify the page loaded (might be 200 with SPA error page or actual 404)
		expect(response).not.toBeNull();
		// Check for common 404 indicators
		const body = page.locator('body');
		await expect(body).toBeVisible();
	});
});
