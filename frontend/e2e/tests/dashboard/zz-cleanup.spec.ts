/**
 * Cleanup: runs last (alphabetically) to delete the persistent account's
 * instance and save Hetzner VPS costs after all dashboard tests complete.
 */
import { test } from '@playwright/test';
import { PERSISTENT_EMAIL, PERSISTENT_PASSWORD } from '../../fixtures/auth';
import { deleteExistingInstances } from '../../helpers/journey-helpers';

test.describe('Cleanup persistent account', () => {
	test.skip(!PERSISTENT_PASSWORD, 'E2E_PERSISTENT_PASSWORD not set — skipping cleanup');

	test('delete persistent account instance', async () => {
		console.log('[E2E] Cleaning up persistent account — deleting instance to save costs...');
		await deleteExistingInstances(PERSISTENT_EMAIL, PERSISTENT_PASSWORD);
		console.log('[E2E] Persistent account instance deleted.');
	});
});
