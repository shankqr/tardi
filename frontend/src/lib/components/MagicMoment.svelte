<script lang="ts">
	interface Props {
		previewUrl?: string | null;
	}

	let { previewUrl }: Props = $props();

	let copiedIndex = $state<number | null>(null);

	const prompts = [
		{
			icon: '🌐',
			title: 'Build a portfolio website',
			description: 'Personal site with hero, projects, and contact form',
			prompt:
				'Build a personal portfolio website with a hero section, about me, projects gallery, and contact form. Use a modern dark theme with gradient accents. Serve it on port 3000.'
		},
		{
			icon: '🚀',
			title: 'Create a startup landing page',
			description: 'Landing page for "FreshBite" meal delivery service',
			prompt:
				'Create a landing page for a startup called "FreshBite" — a healthy meal delivery service. Include a hero with CTA, feature highlights, pricing cards, testimonials section, and footer. Modern design, serve on port 3000.'
		},
		{
			icon: '✅',
			title: 'Build a full-stack todo app',
			description: 'Todo app with SQLite, categories, and due dates',
			prompt:
				'Build a full-stack todo app with a SQLite database. Features: add, complete, delete tasks, with categories and due dates. Clean minimal UI. Serve on port 3000.'
		}
	];

	function copyPrompt(index: number) {
		navigator.clipboard.writeText(prompts[index].prompt);
		copiedIndex = index;
		setTimeout(() => {
			copiedIndex = null;
		}, 1500);
	}

</script>

<div class="rounded-xl border border-gray-200 dark:border-gray-700 p-5">
		<div class="flex items-center gap-2">
			<svg class="h-5 w-5 text-yellow-500" fill="currentColor" viewBox="0 0 20 20">
				<path
					d="M9.049 2.927c.3-.921 1.603-.921 1.902 0l1.07 3.292a1 1 0 00.95.69h3.462c.969 0 1.371 1.24.588 1.81l-2.8 2.034a1 1 0 00-.364 1.118l1.07 3.292c.3.921-.755 1.688-1.54 1.118l-2.8-2.034a1 1 0 00-1.175 0l-2.8 2.034c-.784.57-1.838-.197-1.539-1.118l1.07-3.292a1 1 0 00-.364-1.118L2.98 8.72c-.783-.57-.38-1.81.588-1.81h3.461a1 1 0 00.951-.69l1.07-3.292z"
				/>
			</svg>
			<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Try Your Agent</h3>
		</div>
		<p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
			Copy a prompt, open your agent's dashboard, and watch it build something real.
		</p>

		<div class="mt-4 space-y-3">
			{#each prompts as item, index}
				<div
					class="group rounded-lg border border-gray-100 dark:border-gray-700 p-3 transition-all hover:border-gray-300 dark:hover:border-gray-600"
				>
					<div class="flex items-start justify-between gap-3">
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-2">
								<span class="text-base">{item.icon}</span>
								<p class="text-sm font-medium text-gray-900 dark:text-white">{item.title}</p>
							</div>
							<p class="mt-1 text-xs text-gray-500 dark:text-gray-400 pl-7">{item.description}</p>
						</div>
						<button
							onclick={() => copyPrompt(index)}
							class="shrink-0 rounded-md border border-gray-200 dark:border-gray-600 px-2.5 py-1 text-xs font-medium text-gray-600 dark:text-gray-300 transition-colors hover:bg-gray-50 dark:hover:bg-gray-800 hover:text-gray-900 dark:hover:text-white"
						>
							{#if copiedIndex === index}
								<span class="text-green-600 dark:text-green-400">Copied!</span>
							{:else}
								Copy
							{/if}
						</button>
					</div>
				</div>
			{/each}
		</div>

		{#if previewUrl}
			<p class="mt-3 text-xs text-gray-400 dark:text-gray-500 text-center">
				After your agent finishes, visit
				<a href={previewUrl} target="_blank" rel="noopener noreferrer" class="font-mono text-gray-500 dark:text-gray-400 underline hover:text-gray-700 dark:hover:text-gray-300">{previewUrl}</a>
				to see what it built
			</p>
		{/if}
</div>
