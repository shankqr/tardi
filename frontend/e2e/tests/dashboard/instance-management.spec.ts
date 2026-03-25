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

		// Wait for health check results to appear (up to 60s)
		// Results show as individual check items with pass/fail/warn/info status
		const resultsHeading = page.getByText(/health check result/i);
		await expect(resultsHeading).toBeVisible({ timeout: 60_000 });

		// Verify at least one check item is present with a recognizable status
		const checkItems = page.locator('[class*="health"], [class*="check"]').or(
			page.locator('li').filter({ hasText: /pass|fail|warn|✓|✗|container|telegram|api|disk|memory/i })
		);
		const itemCount = await checkItems.count();

		if (itemCount > 0) {
			console.log(`[E2E] Health check returned ${itemCount} check items`);
		} else {
			// Fallback: verify page content contains health check keywords
			const pageContent = (await page.textContent('body') || '').toLowerCase();
			const hasCheckContent =
				pageContent.includes('pass') ||
				pageContent.includes('fail') ||
				pageContent.includes('warn') ||
				pageContent.includes('container') ||
				pageContent.includes('healthy');
			expect(hasCheckContent).toBeTruthy();
			console.log('[E2E] Health check results verified via page content');
		}

		console.log('[E2E] Health check completed with results');
	});
});
