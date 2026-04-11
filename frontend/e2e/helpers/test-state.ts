import { existsSync, readFileSync, writeFileSync, unlinkSync } from 'fs';
import { join } from 'path';

const STATE_FILE = join(__dirname, '..', '.test-state.json');

export interface TestState {
	email: string;
	password: string;
	instanceId: string;
	framework: string;
	firebaseUid: string;
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

export function hasTestState(): boolean {
	return existsSync(STATE_FILE);
}

export function clearTestState(): void {
	if (existsSync(STATE_FILE)) unlinkSync(STATE_FILE);
}
