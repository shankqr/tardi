<script lang="ts">
	let mobileMenuOpen = $state(false);
</script>

<nav class="bg-white border-b border-gray-100">
	<div class="mx-auto max-w-7xl px-4 sm:px-6 lg:px-8">
		<div class="flex h-16 items-center justify-between">
			<div class="flex items-center gap-8">
				<a href="/" class="text-xl font-bold text-gray-900">Tardi</a>
				<div class="hidden sm:flex items-center gap-6">
					<a href="https://tardi.ai" class="text-sm text-gray-600 hover:text-gray-900">Features</a>
					<a href="https://tardi.ai" class="text-sm text-gray-600 hover:text-gray-900">Pricing</a>
				</div>
			</div>
			<div class="hidden sm:flex items-center gap-4">
				<a href="/login" class="text-sm text-gray-600 hover:text-gray-900">Log in</a>
				<a
					href="/signup"
					class="rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800"
				>
					Get Started
				</a>
			</div>
			<button
				class="sm:hidden p-2 text-gray-600"
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
	{#if mobileMenuOpen}
		<div class="sm:hidden border-t border-gray-100 px-4 py-4 space-y-3">
			<a href="https://tardi.ai" class="block text-sm text-gray-600">Features</a>
			<a href="https://tardi.ai" class="block text-sm text-gray-600">Pricing</a>
			<a href="/login" class="block text-sm text-gray-600">Log in</a>
			<a
				href="/signup"
				class="block rounded-lg bg-gray-900 px-4 py-2 text-center text-sm font-medium text-white"
			>
				Get Started
			</a>
		</div>
	{/if}
</nav>
