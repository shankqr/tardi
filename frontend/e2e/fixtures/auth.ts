import { test as base, type Page } from '@playwright/test';

/**
 * Auth fixture that logs into the app via the /login page.
 * Use for tests that need a pre-authenticated session with an existing user.
 */
export const test = base.extend<{ authedPage: Page }>({
	authedPage: async ({ page }, use) => {
		const email = process.env.E2E_TEST_EMAIL;
		const password = process.env.E2E_TEST_PASSWORD;

		if (!email || !password) {
			throw new Error('Missing E2E_TEST_EMAIL or E2E_TEST_PASSWORD env vars');
		}

		await page.goto('/login');
		await page.locator('#email').fill(email);
		await page.locator('#password').fill(password);
		await page.getByRole('button', { name: 'Sign in' }).click();
		await page.waitForURL('**/dashboard**', { timeout: 15_000 });

		await use(page);
	},
});

export { expect } from '@playwright/test';
