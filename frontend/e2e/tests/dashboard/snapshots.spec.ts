import { test, expect, navigateToInstance } from '../../fixtures/auth';

test.describe('Snapshot create, restore, and delete', () => {

	test('create, restore, and delete a snapshot', async ({ authedPage: page }) => {
		let snapshotName: string;

		// ── Step 1: Navigate to instance ──
		await test.step('Navigate to instance', async () => {
			if (!(await navigateToInstance(page))) {
				test.skip(true, 'No active instance found — run journey test or deploy manually first');
				return;
			}

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
			// When status is "ready", the UI renders Restore/Delete buttons instead of text
			const snapshotRow = page.getByText(snapshotName).locator('..').locator('..');
			await expect(snapshotRow.getByRole('button', { name: 'Delete' })).toBeVisible({ timeout: 180_000 });
			console.log(`[E2E] Snapshot "${snapshotName}" created and ready`);
		});

		// ── Step 3: Verify snapshot in list ──
		await test.step('Verify snapshot in list', async () => {
			// Confirm the snapshot is visible with the correct name
			const snapshotEntry = page.getByText(snapshotName);
			await expect(snapshotEntry).toBeVisible();

			// Verify it shows "ready" status — when ready, Restore and Delete buttons are rendered
			const snapshotRow = snapshotEntry.locator('..').locator('..');
			await expect(snapshotRow.getByRole('button', { name: 'Restore' })).toBeVisible();
			await expect(snapshotRow.getByRole('button', { name: 'Delete' })).toBeVisible();
		});

		// ── Step 4: Restore snapshot ──
		await test.step('Restore snapshot', async () => {
			const snapshotEntry = page.getByText(snapshotName);
			const snapshotRow = snapshotEntry.locator('..').locator('..');
			const restoreBtn = snapshotRow.getByRole('button', { name: 'Restore' });
			await restoreBtn.click();

			// Confirmation dialog should appear
			const confirmBtn = page.getByRole('button', { name: /confirm|yes|restore/i }).last();
			await expect(confirmBtn).toBeVisible({ timeout: 5_000 });
			await confirmBtn.click();

			// Wait for restore to complete — instance status transitions through "Restoring"
			// and eventually returns to active/running
			// Look for success message or status returning to normal
			const restoreSuccess = page.getByText(/restore.*success|restored|running/i);
			const agentDetails = page.getByText('Agent Details');

			// Wait up to 3 minutes for restore to complete
			await expect(restoreSuccess.or(agentDetails)).toBeVisible({ timeout: 180_000 });

			// Verify instance returns to a healthy state
			const runningStatus = page.locator('dd').filter({ hasText: /Running|Active/i }).first();
			await expect(runningStatus).toBeVisible({ timeout: 120_000 });
			console.log(`[E2E] Snapshot "${snapshotName}" restored successfully`);
		});

		// ── Step 5: Delete snapshot ──
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
			console.log(`[E2E] Snapshot "${snapshotName}" deleted`);
		});
	});
});
