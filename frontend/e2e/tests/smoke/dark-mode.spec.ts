import { test, expect } from '@playwright/test';

test.describe('Dark mode', () => {
	test('toggle dark mode on homepage', async ({ page }) => {
		await page.goto('/');

		// Find the theme toggle button
		const themeToggle = page.getByLabel('Toggle theme').first();
		await expect(themeToggle).toBeVisible({ timeout: 10_000 });

		// Check initial state — get the html element's class
		const htmlEl = page.locator('html');
		const initialClass = await htmlEl.getAttribute('class') || '';
		const startedDark = initialClass.includes('dark');

		// Click toggle and wait for class to change
		await themeToggle.click();
		if (startedDark) {
			await expect(htmlEl).not.toHaveClass(/dark/, { timeout: 3_000 });
		} else {
			await expect(htmlEl).toHaveClass(/dark/, { timeout: 3_000 });
		}

		// Click again to toggle back
		await themeToggle.click();
		if (startedDark) {
			await expect(htmlEl).toHaveClass(/dark/, { timeout: 3_000 });
		} else {
			await expect(htmlEl).not.toHaveClass(/dark/, { timeout: 3_000 });
		}
	});

	test('dark mode persists across page navigation', async ({ page }) => {
		await page.goto('/');

		const themeToggle = page.getByLabel('Toggle theme').first();
		await expect(themeToggle).toBeVisible({ timeout: 10_000 });

		const htmlEl = page.locator('html');

		// Get initial state and toggle
		const initialClass = await htmlEl.getAttribute('class') || '';
		const startedDark = initialClass.includes('dark');
		await themeToggle.click();

		// Wait for the toggle to take effect
		if (startedDark) {
			await expect(htmlEl).not.toHaveClass(/dark/, { timeout: 3_000 });
		} else {
			await expect(htmlEl).toHaveClass(/dark/, { timeout: 3_000 });
		}

		// Navigate to another page
		await page.goto('/login');

		// Theme should persist
		if (startedDark) {
			await expect(htmlEl).not.toHaveClass(/dark/);
		} else {
			await expect(htmlEl).toHaveClass(/dark/);
		}

		// Toggle back to restore original state
		const loginToggle = page.getByLabel('Toggle theme').first();
		await loginToggle.click();
	});
});
