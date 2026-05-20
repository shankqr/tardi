<script lang="ts">
	let {
		dashboardUrl,
		authToken
	}: {
		dashboardUrl: string;
		authToken: string;
	} = $props();

	interface ChatMessage {
		role: 'user' | 'assistant';
		content: string;
	}

	let messages = $state<ChatMessage[]>([]);
	let input = $state('');
	let sending = $state(false);
	let error = $state('');
	let messagesContainer: HTMLDivElement;

	function scrollToBottom() {
		if (messagesContainer) {
			messagesContainer.scrollTop = messagesContainer.scrollHeight;
		}
	}

	async function sendMessage(e: Event) {
		e.preventDefault();
		const text = input.trim();
		if (!text || sending) return;

		messages.push({ role: 'user', content: text });
		input = '';
		sending = true;
		error = '';

		setTimeout(scrollToBottom, 0);

		try {
			const response = await fetch(`${dashboardUrl}/v1/chat/completions`, {
				method: 'POST',
				headers: {
					'Content-Type': 'application/json',
					'Authorization': `Bearer ${authToken}`
				},
				body: JSON.stringify({
					model: 'hermes-agent',
					messages: messages.map(m => ({ role: m.role, content: m.content })),
					stream: false
				})
			});

			if (!response.ok) {
				const text = await response.text();
				throw new Error(`API error ${response.status}: ${text}`);
			}

			const data = await response.json();
			if (data.hermes?.failed) {
				const detail = data.hermes?.error || data.choices?.[0]?.message?.content || 'Hermes could not complete the request';
				throw new Error(String(detail).replace(/^API call failed after \d+ retries:\s*/, ''));
			}
			const reply = data.choices?.[0]?.message?.content ?? 'No response';
			messages.push({ role: 'assistant', content: reply });
			setTimeout(scrollToBottom, 0);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to send message';
		} finally {
			sending = false;
		}
	}
</script>

<div class="flex flex-col h-[600px] rounded-xl border border-gray-200 dark:border-gray-700 overflow-hidden">
	<div class="px-4 py-3 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50">
		<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Hermes Agent Chat</h3>
		<p class="text-xs text-gray-500 dark:text-gray-400">Chat with your agent via its API</p>
	</div>

	<div
		bind:this={messagesContainer}
		class="flex-1 overflow-y-auto p-4 space-y-4"
	>
		{#if messages.length === 0}
			<div class="flex items-center justify-center h-full">
				<p class="text-sm text-gray-400 dark:text-gray-500">Send a message to start chatting with your agent.</p>
			</div>
		{/if}

		{#each messages as msg}
			<div class="flex {msg.role === 'user' ? 'justify-end' : 'justify-start'}">
				<div class="max-w-[80%] rounded-lg px-3 py-2 text-sm {msg.role === 'user' ? 'bg-gray-900 dark:bg-white text-white dark:text-gray-900' : 'bg-gray-100 dark:bg-gray-800 text-gray-900 dark:text-white'}">
					<pre class="whitespace-pre-wrap font-sans">{msg.content}</pre>
				</div>
			</div>
		{/each}

		{#if sending}
			<div class="flex justify-start">
				<div class="rounded-lg bg-gray-100 dark:bg-gray-800 px-3 py-2">
					<div class="flex items-center gap-1">
						<div class="h-1.5 w-1.5 rounded-full bg-gray-400 animate-bounce" style="animation-delay: 0ms"></div>
						<div class="h-1.5 w-1.5 rounded-full bg-gray-400 animate-bounce" style="animation-delay: 150ms"></div>
						<div class="h-1.5 w-1.5 rounded-full bg-gray-400 animate-bounce" style="animation-delay: 300ms"></div>
					</div>
				</div>
			</div>
		{/if}
	</div>

	{#if error}
		<div class="px-4 py-2 text-xs text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20">{error}</div>
	{/if}

	<form onsubmit={sendMessage} class="border-t border-gray-200 dark:border-gray-700 p-3 flex gap-2">
		<input
			type="text"
			bind:value={input}
			placeholder="Type a message..."
			disabled={sending}
			class="flex-1 rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 px-3 py-2 text-sm text-gray-900 dark:text-white focus:border-gray-900 dark:focus:border-gray-400 focus:outline-none focus:ring-1 focus:ring-gray-900 dark:focus:ring-gray-400 disabled:opacity-50"
		/>
		<button
			type="submit"
			disabled={sending || !input.trim()}
			class="rounded-lg bg-gray-900 dark:bg-white px-4 py-2 text-sm font-medium text-white dark:text-gray-900 hover:bg-gray-800 dark:hover:bg-gray-100 disabled:opacity-50"
		>
			Send
		</button>
	</form>
</div>
