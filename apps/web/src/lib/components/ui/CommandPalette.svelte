<!--
  CommandPalette — quick-action overlay accessible via `Ctrl+K` (or
  `Cmd+K` on macOS). D1.1.5 closure surfaces three Builds commands so
  operators can jump into the Builds application without leaving
  whatever page they're on:

    * `Run build` — open the /builds page with the run modal.
    * `Open builds` — navigate to /builds.
    * `View build {rid}` — when the user types a build RID prefix,
      offer a direct deep-link to /builds/{rid}.
-->
<script lang="ts" module>
	export type CommandAction = {
		id: string;
		label: string;
		hint?: string;
		shortcut?: string;
		run: () => void | Promise<void>;
	};
</script>

<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { browser } from '$app/environment';
	import { goto } from '$app/navigation';

	let open = $state<boolean>(false);
	let query = $state<string>('');
	let inputEl: HTMLInputElement | undefined = $state();

	const STATIC_COMMANDS: CommandAction[] = [
		{
			id: 'builds.open',
			label: 'Open builds',
			hint: 'Foundry-style cross-pipeline build queue',
			shortcut: 'g b',
			run: () => goto('/builds')
		},
		{
			id: 'builds.run',
			label: 'Run build',
			hint: 'Open the Run-build modal in /builds',
			shortcut: 'r',
			run: () => goto('/builds?run=1')
		},
		{
			id: 'pipelines.open',
			label: 'Open Pipeline Builder',
			hint: 'Browse and edit pipelines',
			run: () => goto('/pipelines')
		}
	];

	function close() {
		open = false;
		query = '';
	}

	function dynamicCommands(q: string): CommandAction[] {
		const trimmed = q.trim();
		if (!trimmed) return [];
		const dynamic: CommandAction[] = [];
		// View build by RID. Accept either a full RID
		// (`ri.foundry.main.build.<uuid>`) or the bare UUID suffix.
		if (/^ri\.foundry\.main\.build\.[a-f0-9-]+/i.test(trimmed)) {
			dynamic.push({
				id: `builds.view.${trimmed}`,
				label: `View build ${trimmed}`,
				hint: '/builds/{rid}',
				run: () => goto(`/builds/${encodeURIComponent(trimmed)}`)
			});
		} else if (/^[a-f0-9-]{8,}$/i.test(trimmed)) {
			const rid = `ri.foundry.main.build.${trimmed}`;
			dynamic.push({
				id: `builds.view.${trimmed}`,
				label: `View build ${rid}`,
				hint: 'Treat the suffix as a build UUID',
				run: () => goto(`/builds/${encodeURIComponent(rid)}`)
			});
		}
		return dynamic;
	}

	let allCommands = $derived<CommandAction[]>([
		...STATIC_COMMANDS.filter(
			(c) => !query.trim() || c.label.toLowerCase().includes(query.trim().toLowerCase())
		),
		...dynamicCommands(query)
	]);

	function handleKey(event: KeyboardEvent) {
		const cmdKey = event.metaKey || event.ctrlKey;
		if (cmdKey && event.key.toLowerCase() === 'k') {
			event.preventDefault();
			open = !open;
			if (open) {
				queueMicrotask(() => inputEl?.focus());
			}
			return;
		}
		if (open && event.key === 'Escape') {
			event.preventDefault();
			close();
		}
	}

	async function runFirstMatch() {
		const first = allCommands[0];
		if (!first) return;
		close();
		await first.run();
	}

	// Guarded with `browser` because Svelte 5 SSR runs `onDestroy`
	// cleanups inline at the end of the synchronous render — accessing
	// `window` from there crashes the layout for every route. The
	// `onMount` callback only fires client-side anyway, but we keep
	// both branches symmetric for clarity.
	onMount(() => {
		if (browser) window.addEventListener('keydown', handleKey);
	});
	onDestroy(() => {
		if (browser) window.removeEventListener('keydown', handleKey);
	});
</script>

{#if open}
	<div class="cp-backdrop" role="presentation" onclick={close} data-testid="command-palette-backdrop">
		<div
			class="cp-panel"
			role="dialog"
			aria-modal="true"
			aria-label="Command palette"
			onclick={(e) => e.stopPropagation()}
			data-testid="command-palette"
		>
			<input
				bind:this={inputEl}
				type="search"
				placeholder="Type a command or paste a build RID…"
				bind:value={query}
				onkeyup={(e) => e.key === 'Enter' && runFirstMatch()}
				data-testid="command-palette-input"
			/>
			<ul class="cp-list" data-testid="command-palette-list">
				{#each allCommands as cmd (cmd.id)}
					<li>
						<button
							type="button"
							onclick={async () => {
								close();
								await cmd.run();
							}}
							data-testid={`command-${cmd.id}`}
						>
							<span class="label">{cmd.label}</span>
							{#if cmd.hint}<span class="hint">{cmd.hint}</span>{/if}
							{#if cmd.shortcut}<span class="shortcut">{cmd.shortcut}</span>{/if}
						</button>
					</li>
				{/each}
				{#if allCommands.length === 0}
					<li class="empty" data-testid="command-palette-empty">No commands match.</li>
				{/if}
			</ul>
		</div>
	</div>
{/if}

<style>
	.cp-backdrop {
		position: fixed;
		inset: 0;
		background: rgba(0, 0, 0, 0.55);
		display: flex;
		align-items: flex-start;
		justify-content: center;
		padding-top: 12vh;
		z-index: 200;
	}
	.cp-panel {
		width: 100%;
		max-width: 560px;
		background: #0b1220;
		border: 1px solid #1f2937;
		border-radius: 8px;
		box-shadow: 0 25px 60px rgba(0, 0, 0, 0.5);
		overflow: hidden;
	}
	input[type='search'] {
		width: 100%;
		background: #0b1220;
		color: #e2e8f0;
		border: none;
		border-bottom: 1px solid #1f2937;
		padding: 12px 14px;
		font: inherit;
		font-size: 14px;
		outline: none;
	}
	.cp-list {
		list-style: none;
		padding: 0;
		margin: 0;
		max-height: 360px;
		overflow-y: auto;
	}
	.cp-list li button {
		width: 100%;
		display: flex;
		gap: 12px;
		align-items: center;
		text-align: left;
		background: transparent;
		color: #e2e8f0;
		border: none;
		padding: 10px 14px;
		font: inherit;
		font-size: 13px;
		cursor: pointer;
	}
	.cp-list li button:hover {
		background: #111827;
	}
	.cp-list .label {
		flex: 1;
	}
	.cp-list .hint {
		color: #94a3b8;
		font-size: 11px;
	}
	.cp-list .shortcut {
		background: #1e293b;
		color: #cbd5e1;
		padding: 1px 6px;
		border-radius: 4px;
		font-family: ui-monospace, monospace;
		font-size: 11px;
	}
	.cp-list .empty {
		color: #94a3b8;
		padding: 14px;
		text-align: center;
		font-size: 12px;
	}
</style>
