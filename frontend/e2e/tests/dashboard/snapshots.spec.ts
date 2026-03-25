import { test, expect } from '@playwright/test';

const PERSISTENT_EMAIL =
	process.env.E2E_PERSISTENT_EMAIL || 'clawmyway+1@gmail.com';
const TEST_PASSWORD = process.env.E2E_PERSISTENT_PASSWORD || process.env.E2E_TEST_PASSWORD;

test.describe('Snapshot create/delete', () => {
	test.skip(!TEST_PASSWORD, 'E2E_PERSISTENT_PASSWORD not set — skipping snapshot tests');

	let snapshotName: string;

	test('create and delete a snapshot', async ({ page }) => {
		// ── Step 1: Login and navigate to instance ──
		await test.step('Login and navigate to instance', async () => {
			await page.goto('/login');

			const signInBtn = page.getByRole('button', { name: 'Sign in' });
			await expect(signInBtn).toBeVisible({ timeout: 10_000 });
			await expect(signInBtn).toBeEnabled();
			await page.waitForTimeout(1000);

			await page.locator('#email').click();
			await page.locator('#email').pressSequentially(PERSISTENT_EMAIL, { delay: 20 });
			await page.locator('#password').click();
			await page.locator('#password').pressSequentially(TEST_PASSWORD!, { delay: 20 });

			await signInBtn.click();
			await page.waitForURL('**/dashboard**', { timeout: 30_000 });

			// Click the first instance card to navigate to instance details
			await page.waitForTimeout(5000);
			const instanceLink = page.locator('a[href^="/dashboard/instances/"]').first();
			const hasInstance = await instanceLink.isVisible({ timeout: 10_000 }).catch(() => false);
			if (!hasInstance) {
				test.skip(true, 'No active instance found — run journey test or deploy manually first');
				return;
			}
			await instanceLink.click();

			await page.waitForURL('**/dashboard/instances/**', { timeout: 15_000 });

			// Verify the instance page loaded — look for the Snapshots heading
			await expect(page.getByRole('heading', { name: 'Snapshots' })).toBeVisible({ timeout: 30_000 });
		});

		// ── Step 2: Create snapshot ──
		await test.step('Create snapshot', async () => {
			snapshotName = `e2e-snapshot-${Date.now()}`;

			// Click "+ Create Snapshot" to expand the form
			const createToggle = page.getByRole('button', { name: '+ Create Snapshot' });
			await createToggle.scrollIntoViewIfNeeded();
			await expect(createToggle).toBeVisible({ timeout: 10_000 });
			await createToggle.click();

			// Fill in the snapshot name
			const nameInput = page.locator('input[placeholder="Snapshot name"]');
			await expect(nameInput).toBeVisible({ timeout: 5_000 });
			await nameInput.click();
			await nameInput.pressSequentially(snapshotName, { delay: 20 });

			// Click the Create submit button
			const createBtn = page.getByRole('button', { name: 'Create' }).first();
			await expect(createBtn).toBeEnabled();
			await createBtn.click();

			// Wait for the snapshot to appear in the list (creating status first)
			await expect(page.getByText(snapshotName)).toBeVisible({ timeout: 30_000 });

			// Wait for status to transition from "creating" to "ready" (up to 3 minutes)
			await expect(page.getByText(/ready/i).first()).toBeVisible({ timeout: 180_000 });
		});

		// ── Step 3: Verify snapshot in list ──
		await test.step('Verify snapshot in list', async () => {
			// Confirm the snapshot is visible with the correct name
			const snapshotEntry = page.getByText(snapshotName);
			await expect(snapshotEntry).toBeVisible();

			// Verify it shows "ready" status
			// The snapshot row contains both the name and a StatusBadge with "ready"
			const snapshotRow = snapshotEntry.locator('..').locator('..');
			await expect(snapshotRow.getByText(/ready/i)).toBeVisible();
		});

		// ── Step 4: Delete snapshot ──
		await test.step('Delete snapshot', async () => {
			// Find the snapshot row and click its Delete button
			const snapshotEntry = page.getByText(snapshotName);
			const snapshotRow = snapshotEntry.locator('..').locator('..');
			const deleteBtn = snapshotRow.getByRole('button', { name: 'Delete' });
			await deleteBtn.click();

			// Confirmation modal appears — type the snapshot name to confirm
			const confirmInput = snapshotRow.locator('input[type="text"]');
			await expect(confirmInput).toBeVisible({ timeout: 5_000 });
			await confirmInput.click();
			await confirmInput.pressSequentially(snapshotName, { delay: 20 });

			// Click the confirm Delete button (now enabled after typing the name)
			const confirmDeleteBtn = snapshotRow.getByRole('button', { name: 'Delete' });
			await expect(confirmDeleteBtn).toBeEnabled();
			await confirmDeleteBtn.click();

			// Wait for the snapshot to disappear from the list
			await expect(page.getByText(snapshotName)).toBeHidden({ timeout: 30_000 });
		});
	});
});
