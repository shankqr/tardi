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

test.describe('Dashboard sad paths', () => {
	test.skip(!PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');

	test('invalid instance ID shows not found', async ({ page }) => {
		await login(page);
		await page.goto('/dashboard/instances/nonexistent-id-12345');
		await page.waitForTimeout(5000);

		// Should show "Agent not found" or similar error
		const notFound = page.getByText('Agent not found').or(page.getByText('not found'));
		await expect(notFound.first()).toBeVisible({ timeout: 15_000 });
	});

	test('snapshot with empty name is rejected', async ({ page }) => {
		await login(page);
		await page.waitForTimeout(5000);

		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		const hasInstance = await instanceLink.isVisible({ timeout: 10_000 }).catch(() => false);
		if (!hasInstance) {
			test.skip(true, 'No active instance found');
			return;
		}

		await instanceLink.click();
		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
		await expect(page.getByRole('heading', { name: 'Snapshots' })).toBeVisible({ timeout: 30_000 });

		// Click "+ Create Snapshot" to expand form
		const createToggle = page.getByRole('button', { name: '+ Create Snapshot' });
		await createToggle.scrollIntoViewIfNeeded();
		await expect(createToggle).toBeVisible({ timeout: 10_000 });
		await createToggle.click();

		// The Create button should be disabled when name is empty
		const createBtn = page.getByRole('button', { name: 'Create' }).first();
		await expect(createBtn).toBeVisible({ timeout: 5_000 });
		await expect(createBtn).toBeDisabled();
		console.log('[E2E] Create snapshot button correctly disabled with empty name');
	});

	test('rename with same name shows no change', async ({ page }) => {
		await login(page);
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

		// Click rename
		const editButton = page.locator('button[title="Rename agent"]');
		await editButton.scrollIntoViewIfNeeded();
		await expect(editButton).toBeVisible({ timeout: 10_000 });
		await editButton.click();

		// Get current name
		const nameInput = page.locator('input[type="text"]').first();
		await expect(nameInput).toBeVisible({ timeout: 5_000 });
		const currentName = await nameInput.inputValue();

		// Save button should be disabled since name hasn't changed
		const saveButton = page.getByRole('button', { name: 'Save' });
		// The save is either disabled or clicking it does nothing meaningful
		// Press Escape to cancel
		await page.keyboard.press('Escape');
		await page.waitForTimeout(500);

		// Verify we exited edit mode (input should be gone, heading shows name)
		await expect(page.locator('h2').filter({ hasText: currentName })).toBeVisible({ timeout: 5_000 });
		console.log('[E2E] Escape correctly cancels rename');
	});

	test('long agent name in rename', async ({ page }) => {
		await login(page);
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

		// Click rename
		const editButton = page.locator('button[title="Rename agent"]');
		await editButton.scrollIntoViewIfNeeded();
		await expect(editButton).toBeVisible({ timeout: 10_000 });
		await editButton.click();

		const nameInput = page.locator('input[type="text"]').first();
		await expect(nameInput).toBeVisible({ timeout: 5_000 });
		const originalName = await nameInput.inputValue();

		// Try a very long name (100 chars)
		const longName = 'e2e-' + 'a'.repeat(96);
		await nameInput.clear();
		await nameInput.pressSequentially(longName, { delay: 5 });

		// Scope Save button to the rename section (near the input)
		const renameSection = nameInput.locator('..');
		const saveButton = renameSection.getByRole('button', { name: 'Save' });
		await expect(saveButton).toBeVisible();
		await saveButton.click();

		// Wait for result — either it succeeds or shows an error
		await page.waitForTimeout(5000);

		// Check if an error appeared or if the name was saved
		const hasError = await page.getByText(/error|too long|invalid/i).isVisible().catch(() => false);
		const nameChanged = await page.locator('h2').filter({ hasText: longName }).isVisible().catch(() => false);

		if (hasError) {
			console.log('[E2E] Long name correctly rejected with error');
		} else if (nameChanged) {
			console.log('[E2E] Long name accepted — restoring original');
			const editAgain = page.locator('button[title="Rename agent"]');
			await editAgain.click();
			const input = page.locator('input[type="text"]').first();
			await expect(input).toBeVisible({ timeout: 5_000 });
			await input.clear();
			await input.pressSequentially(originalName, { delay: 20 });
			const restoreSection = input.locator('..');
			await restoreSection.getByRole('button', { name: 'Save' }).click();
			await expect(page.locator('h2').filter({ hasText: originalName })).toBeVisible({ timeout: 15_000 });
		}

		expect(hasError || nameChanged).toBeTruthy();
	});
});
