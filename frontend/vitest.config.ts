import { defineConfig } from 'vitest/config';
import { sveltekit } from '@sveltejs/kit/vite';

export default defineConfig({
	plugins: [sveltekit()],
	test: {
		include: ['src/**/*.test.ts'],
		environment: 'jsdom',
		globals: true,
		alias: {
			'$lib': '/src/lib',
			'$lib/*': '/src/lib/*',
			'$app/environment': '/src/test/mocks/app-environment.ts'
		}
	}
});
