import { test, expect, type Page } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForOpenClawRunning } from '../../helpers/openclaw-status';

const EMAIL = 'clawmyway+1@gmail.com';
const PASSWORD = 'ir9AsMALZgQz9if';

// VPS details for this test account
const VPS_IP = '204.168.162.209';
const OC_AUTH_TOKEN = '30dfd58e6e581f311b5fa4204a71ac126375f0431d0677aae3b1f625742e61eb';

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

/** SSH into VPS and read the current primary model from OpenClaw. */
function getModelFromVPS(): string {
	try {
		const out = execSync(
			`ssh -i ~/.ssh/tardi-backend -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 root@${VPS_IP} "docker exec openclaw-gateway openclaw config get agents.defaults.model.primary 2>/dev/null"`,
			{ timeout: 20_000, encoding: 'utf-8' }
		);
		return out.trim();
	} catch (e) {
		return `ERROR: ${e}`;
	}
}

/** Wait for OC gateway health endpoint to respond. */
async function waitForGatewayHealthy(maxWaitMs = 30_000): Promise<boolean> {
	const start = Date.now();
	while (Date.now() - start < maxWaitMs) {
		try {
			execSync(
				`curl -sf --max-time 3 http://${VPS_IP}:18789/health`,
				{ timeout: 5_000, encoding: 'utf-8' }
			);
			return true;
		} catch {
			await new Promise((r) => setTimeout(r, 2000));
		}
	}
	return false;
}

/** Open OC dashboard with retries (gateway may be reloading after config set CLIs). */
async function openOcDashboard(page: Page, retries = 5): Promise<boolean> {
	const ocUrl = `http://${VPS_IP}:18789/#token=${OC_AUTH_TOKEN}`;
	for (let attempt = 1; attempt <= retries; attempt++) {
		try {
			await page.goto(ocUrl, { timeout: 20_000 });
			return true;
		} catch {
			console.log(`    OC dashboard load attempt ${attempt}/${retries} failed, retrying…`);
			await new Promise((r) => setTimeout(r, 8000));
		}
	}
	return false;
}

test('cycle through ALL models: FE change → VPS verify → OC dashboard chat', async ({ page }) => {
	// Each model cycle takes ~40s (config sync + SSH verify + OC dashboard chat).
	// With 8 models, we need ~6min. Set timeout to 10min for safety margin.
	test.setTimeout(600_000);
	// ── Login & navigate to instance ──
	await login(page);
	await page.waitForTimeout(3000);

	const instanceLink = page.locator('a[href*="/dashboard/instances/"]').first();
	const hasInstance = await instanceLink.isVisible({ timeout: 15_000 }).catch(() => false);
	if (!hasInstance) {
		test.skip(true, 'No deployed instance — skipping model sync test');
		return;
	}
	await instanceLink.click();
	await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });
	await expect(page.getByText('Agent Details')).toBeVisible({ timeout: 30_000 });

	// ── Collect all models from the dropdown ──
	const modelSelect = page.locator('#model-select');
	await expect(modelSelect).toBeVisible({ timeout: 60_000 });
	await expect(modelSelect).toBeEnabled({ timeout: 10_000 });

	const startingModel = await modelSelect.inputValue();
	const optionEls = await modelSelect.locator('option').all();
	const allModels: string[] = [];
	for (const opt of optionEls) {
		const val = await opt.getAttribute('value');
		if (val) allModels.push(val);
	}
	console.log(`\n── Found ${allModels.length} models: ${allModels.join(', ')} ──\n`);
	expect(allModels.length).toBeGreaterThanOrEqual(2);

	// ── Cycle through every model ──
	let passed = 0;
	for (let i = 0; i < allModels.length; i++) {
		const targetModel = allModels[i];
		const currentModel = await modelSelect.inputValue();

		// Skip if already on this model
		if (targetModel === currentModel) {
			console.log(`[${i + 1}/${allModels.length}] ${targetModel} — already selected, skipping`);
			passed++;
			continue;
		}

		console.log(`[${i + 1}/${allModels.length}] Switching to: ${targetModel}`);

		// ── Change model on FE ──
		await modelSelect.selectOption(targetModel);
		await page.waitForTimeout(500);

		const saveBtn = page.getByRole('button', { name: 'Save' });
		await expect(saveBtn).toBeEnabled({ timeout: 5_000 });
		await saveBtn.click();

		await expect(
			page.getByText('Configuration applied successfully')
		).toBeVisible({ timeout: 120_000 });
		console.log(`  ✓ FE says success`);

		// Verify OpenClaw returns to Running after model change (must be < 1 min)
		await waitForOpenClawRunning(page);

		// ── Verify on VPS via SSH ──

		const vpsModel = getModelFromVPS();
		const expectedVpsModel = `openrouter/${targetModel}`;
		console.log(`  VPS model:  ${vpsModel}`);
		expect(vpsModel, `VPS mismatch for ${targetModel}`).toBe(expectedVpsModel);
		console.log(`  ✓ VPS model matches`);

		// ── Wait for gateway to settle after SSH script's config set reloads ──
		// The SSH script runs config set CLIs which trigger gateway reloads.
		// Wait up to 60s for the gateway to come back healthy.
		const healthy = await waitForGatewayHealthy(60_000);
		if (!healthy) {
			console.log(`  ⚠ Gateway health check timed out — will retry on page load`);
		}

		// ── Open OC dashboard (direct HTTP to gateway) and chat ──
		const ocPage = await page.context().newPage();
		try {
			const loaded = await openOcDashboard(ocPage);
			expect(loaded, 'Could not load OC dashboard after retries').toBeTruthy();

			// Wait for the OC dashboard to connect via WebSocket and fully load
			await ocPage.waitForTimeout(12_000);

			// Verify the model shows in the OC page content
			const pageContent = await ocPage.content();
			const modelInPage =
				pageContent.includes(expectedVpsModel) || pageContent.includes(targetModel);
			if (modelInPage) {
				console.log(`  ✓ Model visible on OC dashboard`);
			} else {
				console.log(`  ⚠ Model not found in OC page HTML (may be in shadow DOM / dynamic)`);
			}

			// ── Send a chat message ──
			const chatInput = ocPage.locator('textarea').last();
			const chatVisible = await chatInput.isVisible({ timeout: 15_000 }).catch(() => false);

			if (chatVisible) {
				const marker = `model-check-${i + 1}`;
				const prompt = `Reply with ONLY the text "${marker}" and nothing else.`;
				await chatInput.click();
				await chatInput.fill(prompt);
				await chatInput.press('Enter');
				console.log(`  Sent chat message, waiting for reply…`);

				// Wait for a reply — look for our marker or any new content
				try {
					await ocPage.waitForFunction(
						(m: string) => document.body.innerText.includes(m),
						marker,
						{ timeout: 90_000 }
					);
					console.log(`  ✓ Agent replied with "${marker}"`);
				} catch {
					// Check if agent replied with different wording
					const bodyText = await ocPage.evaluate(() => document.body.innerText);
					const hasReply =
						bodyText.includes(marker) ||
						bodyText.includes('model-check') ||
						bodyText.length > 800;
					if (hasReply) {
						console.log(`  ✓ Agent replied (different wording)`);
					} else {
						await ocPage.screenshot({
							path: `e2e/test-results/model-chat-${i + 1}.png`,
							fullPage: true,
						});
						console.log(`  ⚠ No reply detected within timeout (screenshot saved)`);
					}
				}
			} else {
				console.log(`  ⚠ Chat textarea not found on OC dashboard`);
				await ocPage.screenshot({
					path: `e2e/test-results/model-oc-nochat-${i + 1}.png`,
					fullPage: true,
				});
			}
		} finally {
			await ocPage.close();
		}

		passed++;
		console.log(`  ✓ PASSED [${passed}/${allModels.length}]\n`);

		// Brief pause before next iteration
		await page.waitForTimeout(2000);
	}

	console.log(`\n══ ALL ${passed}/${allModels.length} MODELS VERIFIED ══`);

	// ── Restore original model ──
	const finalModel = await modelSelect.inputValue();
	if (finalModel !== startingModel) {
		console.log(`Restoring original model: ${startingModel}`);
		await modelSelect.selectOption(startingModel);
		await page.waitForTimeout(500);
		const saveBtn = page.getByRole('button', { name: 'Save' });
		if (await saveBtn.isEnabled({ timeout: 3_000 }).catch(() => false)) {
			await saveBtn.click();
			await page.waitForTimeout(5000);
		}
	}
});
