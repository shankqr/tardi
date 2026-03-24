import { test, expect } from '@playwright/test';

test.describe('Homepage', () => {
	test('hero section renders with headline', async ({ page }) => {
		await page.goto('/');
		await expect(
			page.getByText('Deploy your AI agent in minutes')
		).toBeVisible();
	});

	test('Get Started CTA links to /signup', async ({ page }) => {
		await page.goto('/');
		const cta = page.getByRole('link', { name: 'Get Started' }).first();
		await expect(cta).toBeVisible();
		await expect(cta).toHaveAttribute('href', '/signup');
	});

	test('How It Works section shows 3 steps', async ({ page }) => {
		await page.goto('/');
		await expect(page.getByText('How It Works')).toBeVisible();
		await expect(page.getByText('Configure')).toBeVisible();
		await expect(page.getByText('Subscribe')).toBeVisible();
		await expect(page.getByText('Running')).toBeVisible();
	});

	test('bottom CTA section is visible', async ({ page }) => {
		await page.goto('/');
		await expect(
			page.getByText('Ready to deploy your AI agent?')
		).toBeVisible();
	});

	test('footer renders with branding and copyright', async ({ page }) => {
		await page.goto('/');
		const footer = page.locator('footer');
		await expect(footer).toBeVisible();
		await expect(footer.getByText('Tardi')).toBeVisible();
		await expect(footer.getByText(/©/)).toBeVisible();
	});
});
