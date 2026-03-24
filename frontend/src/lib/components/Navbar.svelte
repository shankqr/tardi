<script lang="ts">
	import { theme, toggleTheme } from '$lib/stores/theme';
	let mobileMenuOpen = $state(false);
</script>

<nav class="bg-white dark:bg-gray-900 border-b border-gray-100 dark:border-gray-800">
	<div class="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
		<div class="flex h-16 items-center justify-between">
			<div class="flex items-center gap-8">
				<a href="https://tardi.ai" class="text-xl font-bold text-gray-900 dark:text-white">Tardi</a>
			</div>
			<div class="hidden sm:flex items-center gap-4">
				<button
					onclick={toggleTheme}
					class="rounded-lg p-2 text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white"
					aria-label="Toggle theme"
				>
					{#if $theme === 'dark'}
						<svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
							<path stroke-linecap="round" stroke-linejoin="round" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" />
						</svg>
					{:else}
						<svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
							<path stroke-linecap="round" stroke-linejoin="round" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
						</svg>
					{/if}
				</button>
				<a href="/login" class="text-sm text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white">Log in</a>
				<a
					href="/signup"
					class="rounded-lg bg-gray-900 dark:bg-white px-4 py-2 text-sm font-medium text-white dark:text-gray-900 hover:bg-gray-800 dark:hover:bg-gray-100"
				>
					Get Started
				</a>
			</div>
			<div class="flex items-center gap-2 sm:hidden">
				<button
					onclick={toggleTheme}
					class="p-2 text-gray-500 dark:text-gray-400"
					aria-label="Toggle theme"
				>
					{#if $theme === 'dark'}
						<svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
							<path stroke-linecap="round" stroke-linejoin="round" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" />
						</svg>
					{:else}
						<svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
							<path stroke-linecap="round" stroke-linejoin="round" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
						</svg>
					{/if}
				</button>
				<button
					class="p-2 text-gray-600 dark:text-gray-400"
					onclick={() => (mobileMenuOpen = !mobileMenuOpen)}
					aria-label="Toggle menu"
				>
					<svg class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
						{#if mobileMenuOpen}
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
						{:else}
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
						{/if}
					</svg>
				</button>
			</div>
		</div>
	</div>
	{#if mobileMenuOpen}
		<div class="sm:hidden border-t border-gray-100 dark:border-gray-800 px-4 py-4 space-y-3">
			<a href="/login" class="block text-sm text-gray-600 dark:text-gray-400">Log in</a>
			<a
				href="/signup"
				class="block rounded-lg bg-gray-900 dark:bg-white px-4 py-2 text-center text-sm font-medium text-white dark:text-gray-900"
			>
				Get Started
			</a>
		</div>
	{/if}
</nav>
