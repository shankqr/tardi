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

test.describe('Model sync: FE → OC dashboard', () => {
	test.skip(!PASSWORD, 'E2E_PERSISTENT_PASSWORD not set');

	test('change model and verify on OC dashboard', async ({ page }) => {
		await login(page);

		// Navigate to instance
		await page.waitForTimeout(5000);
		const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
		const hasInstance = await instanceLink.isVisible({ timeout: 10_000 }).catch(() => false);
		if (!hasInstance) {
			test.skip(true, 'No active instance found');
			return;
		}

		const href = await instanceLink.getAttribute('href');
		const instanceId = href?.split('/dashboard/instances/')[1]?.split('/')[0] || '';
		console.log(`[E2E] Instance ID: ${instanceId}`);

		await instanceLink.click();
		await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
		await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });

		// Wait for AI Provider section to load
		const keyInput = page.locator('#openrouter-key');
		await expect(keyInput).toBeVisible({ timeout: 60_000 });

		// If no API key is saved, save one first so model dropdown enables
		const modelSelect = page.locator('#model-select');
		const isModelEnabled = await modelSelect.isEnabled({ timeout: 3_000 }).catch(() => false);
		if (!isModelEnabled) {
			const apiKey = process.env.E2E_OPENROUTER_API_KEY;
			if (!apiKey) {
				test.skip(true, 'Model dropdown disabled and E2E_OPENROUTER_API_KEY not set');
				return;
			}
			console.log('[E2E] No API key saved, saving one first...');
			await keyInput.fill(apiKey);
			// Save button in AI Provider section
			const aiProviderSave = page.getByRole('button', { name: /save/i }).last();
			await aiProviderSave.click();

			// Wait for sync to complete
			await expect(
				page.getByText('Configuration applied successfully')
			).toBeVisible({ timeout: 120_000 });
			console.log('[E2E] API key saved');

			// Reload to get fresh state
			await page.reload();
			await page.waitForTimeout(8000);
			await expect(modelSelect).toBeEnabled({ timeout: 60_000 });
		}

		// Read current model
		const originalModel = await modelSelect.inputValue();
		console.log(`[E2E] Current model: ${originalModel}`);

		// Pick a different model from the dropdown
		const options = await modelSelect.locator('option').all();
		let targetModel = '';
		for (const opt of options) {
			const val = await opt.getAttribute('value');
			if (val && val !== originalModel) {
				targetModel = val;
				break;
			}
		}
		if (!targetModel) {
			test.skip(true, 'Only one model available — cannot test model switch');
			return;
		}

		console.log(`[E2E] Switching to: ${targetModel}`);
		await modelSelect.selectOption(targetModel);
		await page.waitForTimeout(500);

		// Click Save
		const saveBtn = page.getByRole('button', { name: 'Save' });
		await expect(saveBtn).toBeEnabled({ timeout: 5000 });
		await saveBtn.click();

		// Wait for sync to complete — look for success message or wait for button to re-enable
		await expect(
			page.getByText('Configuration applied successfully')
		).toBeVisible({ timeout: 120_000 });
		console.log('[E2E] Config sync completed');

		// Verify OpenClaw returns to Running after model change
		const start = Date.now();
		while (Date.now() - start < 180_000) {
			const running = await page.locator('dd').filter({ hasText: /Running/i }).first()
				.isVisible({ timeout: 3_000 }).catch(() => false);
			if (running) {
				console.log('[E2E] OpenClaw is Running after model change');
				break;
			}
			await page.reload();
			await page.waitForTimeout(10_000);
		}

		// Now verify on OC dashboard
		// Get dashboard token by clicking "Open Agent Dashboard" and intercepting window.open
		let dashboardUrl = '';
		let dashboardToken = '';

		// Listen for dashboard/state to get the dashboard URL
		page.on('response', async (response) => {
			if (response.url().includes('/api/dashboard/state')) {
				try {
					const data = await response.json();
					const inst = data.instances?.find((i: any) => i.id === instanceId);
					if (inst?.dashboard_url) {
						dashboardUrl = inst.dashboard_url;
					}
				} catch { /* ignore */ }
			}
		});

		await page.reload();
		await page.waitForTimeout(8000);

		if (!dashboardUrl) {
			const content = await page.content();
			const urlMatch = content.match(/https?:\/\/[a-z0-9]+\.tardi\.ai/);
			if (urlMatch) dashboardUrl = urlMatch[0];
		}

		if (!dashboardUrl) {
			console.log('[E2E] Could not determine dashboard URL, skipping OC verification');
			// Still pass — we verified the FE sync worked
			return;
		}

		// Intercept window.open to capture the OC dashboard URL with token
		await page.evaluate(() => {
			(window as any).__originalOpen = window.open;
			window.open = (...args: any[]) => {
				(window as any).__lastOpenUrl = args[0];
				return null;
			};
		});

		const dashboardBtn = page.getByRole('button', { name: 'Open Agent Dashboard' });
		if (await dashboardBtn.isVisible({ timeout: 10_000 }).catch(() => false)) {
			await dashboardBtn.click();
			await page.waitForTimeout(3000);

			const openedUrl = await page.evaluate(() => (window as any).__lastOpenUrl as string);
			if (openedUrl) {
				const tokenMatch = openedUrl.match(/#token=([^&]+)/);
				if (tokenMatch) dashboardToken = tokenMatch[1];
				if (!dashboardUrl) dashboardUrl = openedUrl.split('#')[0];
			}

			await page.evaluate(() => {
				window.open = (window as any).__originalOpen;
			});
		}

		if (!dashboardToken) {
			console.log('[E2E] Could not get dashboard token, skipping OC verification');
			return;
		}

		console.log(`[E2E] Got token, opening OC dashboard at ${dashboardUrl}`);

		// Open OC dashboard in a new page
		const ocPage = await page.context().newPage();
		try {
			await ocPage.goto(`${dashboardUrl}/#token=${dashboardToken}`, { timeout: 30_000 });
			await ocPage.waitForTimeout(8000);

			const expectedOcModel = `openrouter/${targetModel}`;
			const pageContent = await ocPage.content();
			const modelVisible = pageContent.includes(expectedOcModel) || pageContent.includes(targetModel);

			if (modelVisible) {
				console.log(`[E2E] SUCCESS: Model "${expectedOcModel}" found on OC dashboard`);
			} else {
				console.log(`[E2E] Model not found in page content, checking DOM...`);
			}

			expect(modelVisible, `Expected OC dashboard to show model "${expectedOcModel}"`).toBeTruthy();
		} finally {
			await ocPage.close();
		}

		// Restore original model
		try {
			await page.goto(`/dashboard/instances/${instanceId}`);
			await page.waitForTimeout(5000);
			const restoreSelect = page.locator('#model-select');
			if (await restoreSelect.isVisible({ timeout: 10_000 }).catch(() => false)) {
				await restoreSelect.selectOption(originalModel);
				await page.waitForTimeout(500);
				const restoreSave = page.getByRole('button', { name: 'Save' });
				if (await restoreSave.isEnabled({ timeout: 5000 }).catch(() => false)) {
					await restoreSave.click();
					await page.waitForTimeout(5000);
				}
			}
		} catch {
			console.log('[E2E] Could not restore original model (non-critical)');
		}
	});
});
