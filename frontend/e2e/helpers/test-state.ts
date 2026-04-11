import { existsSync, readFileSync, writeFileSync, unlinkSync } from 'fs';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const STATE_FILE = join(__dirname, '..', '.test-state.json');

export interface AccountState {
	email: string;
	password: string;
	instanceId: string;
	firebaseUid: string;
}

export interface TestState {
	openclaw: AccountState;
	hermes: AccountState;
}

export function saveTestState(state: TestState): void {
	writeFileSync(STATE_FILE, JSON.stringify(state, null, 2));
}

export function loadTestState(): TestState {
	if (!existsSync(STATE_FILE)) {
		throw new Error(
			'Test state file not found — run the setup project first (npx playwright test --project=setup)'
		);
	}
	return JSON.parse(readFileSync(STATE_FILE, 'utf-8'));
}

/** Load account state for a specific framework. */
export function loadAccountState(framework: 'openclaw' | 'hermes'): AccountState {
	const state = loadTestState();
	return state[framework];
}

export function hasTestState(): boolean {
	return existsSync(STATE_FILE);
}

export function clearTestState(): void {
	if (existsSync(STATE_FILE)) unlinkSync(STATE_FILE);
}
