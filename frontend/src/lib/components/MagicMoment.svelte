<script lang="ts">
	interface Props {
		googleConnected?: boolean;
		onConnectGoogle?: () => void;
	}

	let { googleConnected = false, onConnectGoogle = () => {} }: Props = $props();

	let copiedIndex = $state<number | null>(null);

	const prompts = [
		{
			icon: '📅',
			title: 'Plan my week',
			description: 'Create a structured weekly schedule in Google Calendar',
			prompt:
				'Create a productive weekly schedule in my Google Calendar for next week. Add focused work blocks (9am-12pm), lunch break (12-1pm), meetings placeholder (2-3pm), and a Friday wrap-up session. Color-code by category. When done, return the Google Calendar link so I can review it.'
		},
		{
			icon: '📧',
			title: 'Draft a follow-up email',
			description: 'Compose a professional follow-up and save it as a Gmail draft',
			prompt:
				"Create a Gmail draft for a polite follow-up email to a client named Alex about a project proposal I sent last week. Keep it professional but warm, mention I'm happy to jump on a call to discuss details. Save it as a draft in Gmail and return the link to the draft so I can review before sending."
		},
		{
			icon: '📝',
			title: 'Write a meeting notes template',
			description: 'Create a reusable meeting notes doc in Google Docs',
			prompt:
				'Create a Google Doc titled "Meeting Notes Template" with sections for: Date, Attendees, Agenda Items, Discussion Notes, Action Items (with owner and due date columns as a table), and Next Meeting. Format it cleanly with headers. Share the Google Doc URL with me so I can bookmark it.'
		},
		{
			icon: '📊',
			title: 'Build a budget tracker',
			description: 'Set up a personal budget spreadsheet in Google Sheets',
			prompt:
				'Create a Google Sheets personal budget tracker for this month. Include columns for Date, Category (groceries, dining, transport, entertainment, bills), Description, and Amount. Add a summary section at the top with totals per category using SUMIF formulas, and a remaining budget calculation assuming a $3,000 monthly budget. Return the Google Sheets URL so I can start logging expenses.'
		},
		{
			icon: '📁',
			title: 'Organize my Drive',
			description: 'Create a project folder structure in Google Drive',
			prompt:
				'Create an organized folder structure in my Google Drive for a project called "Q2 Launch". Create subfolders: "01 - Planning", "02 - Design Assets", "03 - Content Drafts", "04 - Reviews & Feedback", and "05 - Final Deliverables". In the Planning folder, create a Google Doc called "Project Brief" with placeholder sections for Objectives, Timeline, Team, and Budget. Return the link to the main project folder.'
		}
	];

	function copyPrompt(index: number) {
		if (!googleConnected) {
			onConnectGoogle();
			return;
		}
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
			Copy a prompt, paste it in your agent's dashboard, and watch it work with your Google apps.
		</p>
		{#if !googleConnected}
			<p class="mt-1 text-xs text-amber-600 dark:text-amber-400">
				Clicking a prompt will connect your Google account first
			</p>
		{/if}

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
							{:else if !googleConnected}
								Connect & Copy
							{:else}
								Copy
							{/if}
						</button>
					</div>
				</div>
			{/each}
		</div>

</div>
