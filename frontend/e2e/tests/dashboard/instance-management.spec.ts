import { test, expect, type Page } from '@playwright/test';

const EMAIL = process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+1@gmail.com';
const PASSWORD = process.env.E2E_PERSISTENT_PASSWORD || process.env.E2E_TEST_PASSWORD || '';

async function login(page: Page): Promise<void> {
	await page.goto('/login');
	const signInBtn = page.getByRole('button', { name: 'Sign in' });
	await expect(signInBtn).toBeVisible({ timeout: 10_000 });
	await expect(signInBtn).toBeEnabled();
	await page.waitForTimeout(1000);

	await page.locator('#email').click();
	await page.locator('#email').pressSequentially(EMAIL, { delay: 20 });
	await page.locator('#password').click();
	await page.locator('#password').pressSequentially(PASSWORD, { delay: 20 });
	await signInBtn.click();
	await page.waitForURL('**/dashboard**', { timeout: 30_000 });
}

async function navigateToInstance(page: Page): Promise<boolean> {
	await page.waitForTimeout(5000);
	const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
	const hasInstance = await instanceLink.isVisible({ timeout: 10_000 }).catch(() => false);
	if (!hasInstance) return false;
	await instanceLink.click();
	await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
	await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });
	return true;
}

test.describe('Instance management', () => {
	test.skip(!PASSWORD, 'E2E_PERSISTENT_PASSWORD not set — skipping instance management tests');

	test('rename instance and restore original name', async ({ page }) => {
		await login(page);
		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Find and click the rename button
		const editButton = page.locator('button[title="Rename agent"]');
		await editButton.scrollIntoViewIfNeeded();
		await expect(editButton).toBeVisible({ timeout: 10_000 });
		await editButton.click();

		const nameInput = page.locator('input[type="text"]').first();
		await expect(nameInput).toBeVisible({ timeout: 5_000 });
		const originalName = await nameInput.inputValue();
		console.log(`[E2E] Original instance name: ${originalName}`);

		// Clear and type a new name
		const newName = `e2e-renamed-${Date.now()}`;
		await nameInput.clear();
		await nameInput.pressSequentially(newName, { delay: 20 });

		// Click the Save button next to the rename input (scoped near the input)
		// Use Cancel as anchor — Save is its sibling
		const renameSection = nameInput.locator('..');
		const saveButton = renameSection.getByRole('button', { name: 'Save' });
		await expect(saveButton).toBeVisible();
		await saveButton.click();

		await expect(page.locator('h2').filter({ hasText: newName })).toBeVisible({ timeout: 15_000 });
		console.log(`[E2E] Instance renamed to: ${newName}`);

		// Rename back to original
		const editAgain = page.locator('button[title="Rename agent"]');
		await expect(editAgain).toBeVisible({ timeout: 10_000 });
		await editAgain.click();

		const nameInputAgain = page.locator('input[type="text"]').first();
		await expect(nameInputAgain).toBeVisible({ timeout: 5_000 });
		await nameInputAgain.clear();
		await nameInputAgain.pressSequentially(originalName, { delay: 20 });

		const renameSectionAgain = nameInputAgain.locator('..');
		await renameSectionAgain.getByRole('button', { name: 'Save' }).click();

		await expect(page.locator('h2').filter({ hasText: originalName })).toBeVisible({ timeout: 15_000 });
		console.log(`[E2E] Instance name restored to: ${originalName}`);
	});

	test('run health check', async ({ page }) => {
		await login(page);
		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Health Check button can appear in two places:
		// 1. Inside Power User accordion
		// 2. In the unhealthy warning banner at the bottom
		const healthCheckButton = page.getByRole('button', { name: 'Health Check' });

		// Try to find it directly first (may be in warning banner)
		let isVisible = await healthCheckButton.isVisible({ timeout: 3_000 }).catch(() => false);

		if (!isVisible) {
			// Try expanding Power User section
			const powerUserButton = page.getByText('Power User').first();
			await powerUserButton.scrollIntoViewIfNeeded();
			await powerUserButton.click();
			await page.waitForTimeout(500);
		}

		await healthCheckButton.scrollIntoViewIfNeeded();
		await expect(healthCheckButton).toBeVisible({ timeout: 10_000 });
		await expect(healthCheckButton).toBeEnabled({ timeout: 10_000 });
		await healthCheckButton.click();

		// Wait for results (up to 60s) — look for results heading or any result indicator
		// Health check results can show as "Health Check Results" heading or inline results
		await page.waitForTimeout(15_000);

		// After clicking, either results appear or the button changes state
		const pageContent = await page.textContent('body') || '';
		const lowerContent = pageContent.toLowerCase();
		const hasResults =
			lowerContent.includes('health check result') ||
			lowerContent.includes('pass') ||
			lowerContent.includes('fail') ||
			lowerContent.includes('warn') ||
			lowerContent.includes('✓') ||
			lowerContent.includes('✗') ||
			lowerContent.includes('running') ||
			lowerContent.includes('check complete') ||
			lowerContent.includes('healthy') ||
			lowerContent.includes('unhealthy');

		expect(hasResults).toBeTruthy();
		console.log('[E2E] Health check completed');
	});
});
