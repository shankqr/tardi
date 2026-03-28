<script lang="ts">
	interface Props {
		previewUrl?: string | null;
		googleConnected?: boolean;
		onConnectGoogle?: () => void;
		showGoogle?: boolean;
	}

	let { previewUrl, googleConnected = false, onConnectGoogle = () => {}, showGoogle = true }: Props = $props();

	let copiedIndex = $state<string | null>(null);

	const servingInstruction = $derived(
		previewUrl
			? `Serve it on port 3000. IMPORTANT: The finished result will be publicly accessible at ${previewUrl} — make sure the app works correctly when visited at that URL. Use relative paths for all assets and links so everything loads properly.`
			: 'Serve it on port 3000.'
	);

	const basePrompts = [
		{
			icon: '🌐',
			title: 'Build a portfolio website',
			description: 'Personal site with hero, projects, and contact form',
			prompt:
				'Build a personal portfolio website with a hero section, about me, projects gallery, and contact form. Use a modern dark theme with gradient accents.'
		},
		{
			icon: '🚀',
			title: 'Create a startup landing page',
			description: 'Landing page for "FreshBite" meal delivery service',
			prompt:
				'Create a landing page for a startup called "FreshBite" — a healthy meal delivery service. Include a hero with CTA, feature highlights, pricing cards, testimonials section, and footer. Modern design.'
		},
		{
			icon: '✅',
			title: 'Build a full-stack todo app',
			description: 'Todo app with SQLite, categories, and due dates',
			prompt:
				'Build a full-stack todo app with a SQLite database. Features: add, complete, delete tasks, with categories and due dates. Clean minimal UI.'
		}
	];

	const webPrompts = $derived(
		basePrompts.map((p) => ({
			...p,
			prompt: `${p.prompt} ${servingInstruction}`
		}))
	);

	const googleInstruction =
		'Use the pre-installed `gog` CLI tool for all Google Workspace operations. Google credentials are already configured at ~/.config/gogcli/ (credentials.json and tokens/ directory) — no authentication setup is needed. Run `gog <service> --help` to discover available commands (e.g., `gog docs --help`, `gog sheets --help`, `gog calendar --help`, `gog gmail --help`, `gog drive --help`).';

	const googlePrompts = [
		{
			icon: '📅',
			title: 'Plan my week',
			description: 'Create a structured weekly schedule in Google Calendar',
			prompt:
				'Create a productive weekly schedule in my Google Calendar for next week. Add focused work blocks (9am-12pm), lunch break (12-1pm), meetings placeholder (2-3pm), and a Friday wrap-up session. Color-code by category. When done, return the Google Calendar link so I can review it.',
			requiresGoogle: true
		},
		{
			icon: '📧',
			title: 'Draft a follow-up email',
			description: 'Compose a professional follow-up and save it as a Gmail draft',
			prompt:
				"Create a Gmail draft for a polite follow-up email to a client named Alex about a project proposal I sent last week. Keep it professional but warm, mention I'm happy to jump on a call to discuss details. Save it as a draft in Gmail and return the link to the draft so I can review before sending.",
			requiresGoogle: true
		},
		{
			icon: '📈',
			title: 'Research top NASDAQ stocks',
			description: 'Research top 10 NASDAQ stocks and predict price movements in Google Sheets',
			prompt:
				'Research the latest top 10 stocks of the NASDAQ index by market cap. For each stock, find the current price, 52-week high/low, and recent performance. Create a Google Sheets spreadsheet titled "NASDAQ Top 10 — Stock Research" with columns for: Rank, Ticker, Company Name, Current Price, 52-Week High, 52-Week Low, YTD Performance (%), and a Short-Term Price Prediction column with a brief bullish/bearish/neutral outlook and reasoning. Add a summary section at the top with overall market sentiment. Return the Google Sheets URL so I can review the analysis.',
			requiresGoogle: true
		},
		{
			icon: '🛒',
			title: 'Research top Amazon products',
			description: 'Research top 10 trending Amazon products and summarize in Google Docs',
			prompt:
				'Research the latest top 10 trending or best-selling products on Amazon right now. For each product, find the product name, category, price, average rating, and number of reviews. Create a Google Doc titled "Top 10 Amazon Products — Research Summary" with a section for each product that includes: product name, category, price, rating, a brief summary of what the product is, key features, and why it\'s trending. Add an overview section at the top with general trends you noticed. Format it cleanly with headers and bullet points. Share the Google Doc URL with me so I can review the research.',
			requiresGoogle: true
		},
		{
			icon: '📁',
			title: 'Organize my Drive',
			description: 'Create a project folder structure in Google Drive',
			prompt:
				'Create an organized folder structure in my Google Drive for a project called "Q2 Launch". Create subfolders: "01 - Planning", "02 - Design Assets", "03 - Content Drafts", "04 - Reviews & Feedback", and "05 - Final Deliverables". In the Planning folder, create a Google Doc called "Project Brief" with placeholder sections for Objectives, Timeline, Team, and Budget. Return the link to the main project folder.',
			requiresGoogle: true
		}
	];

	const googleWorkspacePrompts = $derived(
		googlePrompts.map((p) => ({
			...p,
			prompt: `${p.prompt} ${googleInstruction}`
		}))
	);

	function copyWebPrompt(index: number) {
		navigator.clipboard.writeText(webPrompts[index].prompt);
		copiedIndex = `web-${index}`;
		setTimeout(() => { copiedIndex = null; }, 1500);
	}

	function copyGooglePrompt(index: number) {
		if (!googleConnected) {
			onConnectGoogle();
			return;
		}
		navigator.clipboard.writeText(googleWorkspacePrompts[index].prompt);
		copiedIndex = `google-${index}`;
		setTimeout(() => { copiedIndex = null; }, 1500);
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

		<!-- Web dev prompts -->
		<div class="mt-4 space-y-3">
			{#each webPrompts as item, index}
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
							onclick={() => copyWebPrompt(index)}
							class="shrink-0 rounded-md border border-gray-200 dark:border-gray-600 px-2.5 py-1 text-xs font-medium text-gray-600 dark:text-gray-300 transition-colors hover:bg-gray-50 dark:hover:bg-gray-800 hover:text-gray-900 dark:hover:text-white"
						>
							{#if copiedIndex === `web-${index}`}
								<span class="text-green-600 dark:text-green-400">Copied!</span>
							{:else}
								Copy
							{/if}
						</button>
					</div>
				</div>
			{/each}
		</div>

		<!-- Google Workspace prompts -->
		{#if showGoogle}
		<div class="mt-5 flex items-center gap-2">
			<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" class="h-4 w-4">
				<path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z" fill="#4285F4"/>
				<path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/>
				<path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" fill="#FBBC05"/>
				<path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/>
			</svg>
			<h4 class="text-xs font-semibold text-gray-700 dark:text-gray-300">Google Workspace</h4>
		</div>
	
		<div class="mt-3 space-y-3">
			{#each googleWorkspacePrompts as item, index}
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
							onclick={() => copyGooglePrompt(index)}
							class="shrink-0 rounded-md border border-gray-200 dark:border-gray-600 px-2.5 py-1 text-xs font-medium text-gray-600 dark:text-gray-300 transition-colors hover:bg-gray-50 dark:hover:bg-gray-800 hover:text-gray-900 dark:hover:text-white"
						>
							{#if copiedIndex === `google-${index}`}
								<span class="text-green-600 dark:text-green-400">Copied!</span>
							{:else}
								Copy
							{/if}
						</button>
					</div>
				</div>
			{/each}
		</div>
		{/if}

		{#if previewUrl}
			<p class="mt-3 text-xs text-gray-400 dark:text-gray-500 text-center">
				After your agent finishes, visit
				<a href={previewUrl} target="_blank" rel="noopener noreferrer" class="font-mono text-gray-500 dark:text-gray-400 underline hover:text-gray-700 dark:hover:text-gray-300">{previewUrl}</a>
				to see what it built
			</p>
		{/if}

</div>
