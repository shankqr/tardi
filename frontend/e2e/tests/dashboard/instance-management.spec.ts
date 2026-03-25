import { test, expect, PERSISTENT_PASSWORD, navigateToInstance } from '../../fixtures/auth';

test.describe('Instance management', () => {
	test.skip(!PERSISTENT_PASSWORD, 'E2E_PERSISTENT_PASSWORD not set — skipping instance management tests');

	test('rename instance and restore original name', async ({ authedPage: page }) => {
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

	test('run health check and verify results', async ({ authedPage: page }) => {
		if (!(await navigateToInstance(page))) {
			test.skip(true, 'No active instance found');
			return;
		}

		// Expand Power User section to find Health Check button
		const powerUserButton = page.getByText('Power User').first();
		await powerUserButton.scrollIntoViewIfNeeded();
		await powerUserButton.click();
		await page.waitForTimeout(500);

		// Health Check button inside Power User accordion
		const healthCheckButton = page.getByRole('button', { name: 'Health Check' }).first();
		await healthCheckButton.scrollIntoViewIfNeeded();
		await expect(healthCheckButton).toBeVisible({ timeout: 10_000 });
		await expect(healthCheckButton).toBeEnabled({ timeout: 10_000 });
		await healthCheckButton.click();

		// Button should change to "Checking..." while running
		await expect(page.getByRole('button', { name: 'Checking...' }).first()).toBeVisible({ timeout: 5_000 }).catch(() => {
			console.log('[E2E] Health check button did not show Checking... state (may have completed fast)');
		});

		// Wait for health check results heading to appear (up to 90s)
		const resultsHeading = page.getByText('Health Check Results');
		await expect(resultsHeading).toBeVisible({ timeout: 90_000 });
		console.log('[E2E] Health check results heading visible');

		// Verify the results contain check items with pass/fail/warn indicators
		const pageContent = (await page.textContent('body') || '').toLowerCase();
		const hasCheckContent =
			pageContent.includes('pass') ||
			pageContent.includes('fail') ||
			pageContent.includes('warn') ||
			pageContent.includes('✓') ||
			pageContent.includes('✗');
		expect(hasCheckContent).toBeTruthy();
		console.log('[E2E] Health check completed with results');
	});
});
