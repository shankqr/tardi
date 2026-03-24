import type { Page } from '@playwright/test';

/**
 * Shared login helper for E2E tests.
 * Uses pressSequentially instead of fill() to work around Svelte 5 hydration issues.
 */
export async function loginAs(page: Page, email: string, password: string): Promise<void> {
	await page.goto('/login');

	// Wait for the sign-in button to be visible and enabled
	const signInButton = page.getByRole('button', { name: 'Sign in' });
	await signInButton.waitFor({ state: 'visible' });
	await signInButton.waitFor({ state: 'attached' });

	// Wait for Svelte hydration to complete
	await page.waitForTimeout(1000);

	// Use pressSequentially instead of fill() — Svelte 5 hydration doesn't
	// reliably pick up programmatic input events from fill()
	const emailInput = page.locator('#email');
	await emailInput.click();
	await emailInput.pressSequentially(email, { delay: 20 });

	const passwordInput = page.locator('#password');
	await passwordInput.click();
	await passwordInput.pressSequentially(password, { delay: 20 });

	// Click sign in
	await signInButton.click();

	// Wait for redirect to dashboard
	await page.waitForURL('**/dashboard**', { timeout: 30_000 });

	// Wait for dashboard heading to be visible
	await page.getByRole('heading', { name: 'Dashboard' }).waitFor({ state: 'visible' });
}
