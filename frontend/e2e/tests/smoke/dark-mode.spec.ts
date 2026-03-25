import { test, expect } from '@playwright/test';

test.describe('Dark mode', () => {
	test('toggle dark mode on homepage', async ({ page }) => {
		await page.goto('/');
		await page.waitForTimeout(1000);

		// Find the theme toggle button
		const themeToggle = page.getByLabel('Toggle theme').first();
		await expect(themeToggle).toBeVisible({ timeout: 10_000 });

		// Check initial state — get the html element's class
		const htmlEl = page.locator('html');
		const initialClass = await htmlEl.getAttribute('class') || '';
		const startedDark = initialClass.includes('dark');

		// Click toggle
		await themeToggle.click();
		await page.waitForTimeout(500);

		// Class should have changed
		const newClass = await htmlEl.getAttribute('class') || '';
		if (startedDark) {
			expect(newClass).not.toContain('dark');
		} else {
			expect(newClass).toContain('dark');
		}

		// Click again to toggle back
		await themeToggle.click();
		await page.waitForTimeout(500);

		const restoredClass = await htmlEl.getAttribute('class') || '';
		if (startedDark) {
			expect(restoredClass).toContain('dark');
		} else {
			expect(restoredClass).not.toContain('dark');
		}
	});

	test('dark mode persists across page navigation', async ({ page }) => {
		await page.goto('/');
		await page.waitForTimeout(1000);

		const themeToggle = page.getByLabel('Toggle theme').first();
		const htmlEl = page.locator('html');

		// Get initial state and toggle
		const initialClass = await htmlEl.getAttribute('class') || '';
		const startedDark = initialClass.includes('dark');
		await themeToggle.click();
		await page.waitForTimeout(500);

		// Navigate to another page
		await page.goto('/login');
		await page.waitForTimeout(1000);

		// Theme should persist
		const loginClass = await htmlEl.getAttribute('class') || '';
		if (startedDark) {
			expect(loginClass).not.toContain('dark');
		} else {
			expect(loginClass).toContain('dark');
		}

		// Toggle back to restore original state
		const loginToggle = page.getByLabel('Toggle theme').first();
		await loginToggle.click();
	});
});
