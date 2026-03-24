import { test, expect, type Page } from '@playwright/test';

const EMAIL = process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+persistent@gmail.com';
const PASSWORD = process.env.E2E_TEST_PASSWORD || '';

/**
 * Log in to the app using pressSequentially (Svelte 5 hydration-safe).
 */
async function login(page: Page): Promise<void> {
	await page.goto('/login');

	const signInBtn = page.getByRole('button', { name: 'Sign in' });
	await expect(signInBtn).toBeVisible({ timeout: 10_000 });
	await expect(signInBtn).toBeEnabled();

	// Wait for Svelte 5 hydration
	await page.waitForTimeout(1000);

	await page.locator('#email').click();
	await page.locator('#email').pressSequentially(EMAIL, { delay: 20 });
	await page.locator('#password').click();
	await page.locator('#password').pressSequentially(PASSWORD, { delay: 20 });

	await signInBtn.click();

	await page.waitForURL('**/dashboard**', { timeout: 30_000 });
}

test.describe('Instance management', () => {
	test.skip(!PASSWORD, 'E2E_TEST_PASSWORD not set — skipping instance management tests');

	test('rename instance and restore original name', async ({ page }) => {
		await login(page);

		// Wait for dashboard to load, check if there's an active instance
		await page.waitForTimeout(5000);
		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		const hasInstance = await instanceLink.isVisible({ timeout: 10_000 }).catch(() => false);
		if (!hasInstance) {
			test.skip(true, 'No active instance found — run journey test or deploy manually first');
			return;
		}

		await instanceLink.click();
		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });

		// Find and click the pencil/edit icon (the rename button with title "Rename agent")
		const editButton = page.locator('button[title="Rename agent"]');
		await editButton.scrollIntoViewIfNeeded();
		await expect(editButton).toBeVisible({ timeout: 10_000 });
		await editButton.click();

		// The name heading should turn into an input
		const nameInput = page.locator('input[type="text"]').first();
		await expect(nameInput).toBeVisible({ timeout: 5_000 });

		// Store the original name
		const originalName = await nameInput.inputValue();
		console.log(`[E2E] Original instance name: ${originalName}`);

		// Clear and type a new name
		const newName = `e2e-renamed-${Date.now()}`;
		await nameInput.clear();
		await nameInput.pressSequentially(newName, { delay: 20 });

		// Click Save button
		const saveButton = page.getByRole('button', { name: 'Save' });
		await expect(saveButton).toBeVisible();
		await saveButton.click();

		// Wait for the rename to complete
		await expect(page.locator('h2').filter({ hasText: newName })).toBeVisible({ timeout: 15_000 });
		console.log(`[E2E] Instance renamed to: ${newName}`);

		// Rename back to the original name
		const editButtonAgain = page.locator('button[title="Rename agent"]');
		await expect(editButtonAgain).toBeVisible({ timeout: 10_000 });
		await editButtonAgain.click();

		const nameInputAgain = page.locator('input[type="text"]').first();
		await expect(nameInputAgain).toBeVisible({ timeout: 5_000 });

		await nameInputAgain.clear();
		await nameInputAgain.pressSequentially(originalName, { delay: 20 });

		const saveButtonAgain = page.getByRole('button', { name: 'Save' });
		await saveButtonAgain.click();

		// Verify the original name is restored
		await expect(page.locator('h2').filter({ hasText: originalName })).toBeVisible({ timeout: 15_000 });
		console.log(`[E2E] Instance name restored to: ${originalName}`);
	});

	test('run health check', async ({ page }) => {
		await login(page);

		// Navigate to instance
		await page.waitForTimeout(5000);
		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		const hasInstance = await instanceLink.isVisible({ timeout: 10_000 }).catch(() => false);
		if (!hasInstance) {
			test.skip(true, 'No active instance found');
			return;
		}

		await instanceLink.click();
		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });

		// Scroll to and expand the Power User section
		const powerUserButton = page.getByText('Power User').first();
		await powerUserButton.scrollIntoViewIfNeeded();
		await expect(powerUserButton).toBeVisible({ timeout: 10_000 });

		// Click to expand if collapsed
		const healthCheckButton = page.getByRole('button', { name: 'Health Check' });
		const isExpanded = await healthCheckButton.isVisible().catch(() => false);
		if (!isExpanded) {
			await powerUserButton.click();
			await page.waitForTimeout(500);
		}

		// Click the Health Check button
		await healthCheckButton.scrollIntoViewIfNeeded();
		await expect(healthCheckButton).toBeVisible({ timeout: 10_000 });
		await expect(healthCheckButton).toBeEnabled({ timeout: 10_000 });
		await healthCheckButton.click();

		// Wait for health check results to appear (up to 60s)
		const resultsHeading = page.getByText('Health Check Results');
		await expect(resultsHeading).toBeVisible({ timeout: 60_000 });
		console.log('[E2E] Health check results appeared');

		// Verify that results contain pass/fail/warn indicators
		const resultsSection = page.locator('div').filter({ hasText: 'Health Check Results' });
		const resultsText = await resultsSection.last().textContent();
		const hasResults =
			resultsText?.includes('pass') ||
			resultsText?.includes('fail') ||
			resultsText?.includes('warn') ||
			resultsText?.includes('error') ||
			resultsText?.includes('Health check failed');

		expect(hasResults).toBeTruthy();
		console.log(`[E2E] Health check completed with results`);
	});
});
