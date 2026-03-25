import { test, expect } from '@playwright/test';

const MOBILE_VIEWPORT = { width: 375, height: 667 };

test.describe('Mobile responsive', () => {
	test('homepage renders on mobile', async ({ page }) => {
		await page.setViewportSize(MOBILE_VIEWPORT);
		await page.goto('/');
		await expect(
			page.getByRole('heading', { name: 'Deploy your AI agent in' })
		).toBeVisible();
	});

	test('mobile hamburger menu', async ({ page }) => {
		await page.setViewportSize(MOBILE_VIEWPORT);
		await page.goto('/');

		// Hamburger button should be visible on mobile
		const hamburger = page.getByLabel('Toggle menu');
		await expect(hamburger).toBeVisible();

		// Click hamburger and verify mobile menu links appear
		await hamburger.click();
		await page.waitForTimeout(500);

		// After clicking, the mobile menu should show Log in and Get Started links
		await expect(page.getByRole('link', { name: 'Log in' }).first()).toBeVisible();
		await expect(page.getByRole('link', { name: 'Get Started' }).first()).toBeVisible();
	});

	test('login form usable on mobile', async ({ page }) => {
		await page.setViewportSize(MOBILE_VIEWPORT);
		await page.goto('/login');

		await expect(page.getByLabel(/email/i)).toBeVisible();
		await expect(page.getByLabel(/password/i).first()).toBeVisible();
		await expect(page.getByRole('button', { name: /sign in/i })).toBeVisible();

		// Verify form is not overflowing the viewport
		const body = page.locator('body');
		const box = await body.boundingBox();
		expect(box).not.toBeNull();
		expect(box!.width).toBeLessThanOrEqual(MOBILE_VIEWPORT.width);
	});

	test('signup form usable on mobile', async ({ page }) => {
		await page.setViewportSize(MOBILE_VIEWPORT);
		await page.goto('/signup');

		await expect(page.getByLabel(/email/i)).toBeVisible();
		const passwordFields = page.locator('input[type="password"]');
		await expect(passwordFields).toHaveCount(2);
		await expect(page.getByRole('button', { name: /create account/i })).toBeVisible();
	});
});
