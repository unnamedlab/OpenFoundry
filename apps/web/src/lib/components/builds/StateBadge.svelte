<!--
  StateBadge — color-coded pill for `BuildState` / `JobState` (D1.1.5
  semantic tokens, P5 polish). Pulses for active states (RUNNING /
  ABORTING) so the eye picks them out instantly.
-->
<script lang="ts">
	import {
		BUILD_STATE_COLORS,
		JOB_STATE_COLORS,
		type BuildState,
		type JobState
	} from '$lib/api/buildsV1';

	type Props = {
		kind: 'build' | 'job';
		state: BuildState | JobState | string;
		size?: 'sm' | 'md';
	};

	let { kind, state, size = 'md' }: Props = $props();

	const palette = $derived(
		kind === 'build'
			? (BUILD_STATE_COLORS[state as BuildState] ?? { bg: '#374151', text: '#e5e7eb' })
			: (JOB_STATE_COLORS[state as JobState] ?? { bg: '#374151', text: '#e5e7eb' })
	);

	const pulse = $derived(
		kind === 'build' ? Boolean((BUILD_STATE_COLORS[state as BuildState] ?? {}).pulse) : false
	);
</script>

<span
	class="badge"
	class:pulse
	class:sm={size === 'sm'}
	style:--badge-bg={palette.bg}
	style:--badge-text={palette.text}
	data-state={state}
	data-testid={`state-badge-${state}`}
>
	{state}
</span>

<style>
	.badge {
		display: inline-flex;
		align-items: center;
		padding: 2px 8px;
		border-radius: 999px;
		background: var(--badge-bg);
		color: var(--badge-text);
		font-size: 11px;
		font-weight: 600;
		font-family: ui-monospace, 'SF Mono', Consolas, monospace;
		letter-spacing: 0.02em;
		white-space: nowrap;
	}
	.badge.sm {
		padding: 1px 6px;
		font-size: 10px;
	}
	.badge.pulse::before {
		content: '';
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--badge-text);
		margin-right: 6px;
		animation: pulse 1.4s ease-in-out infinite;
	}
	@keyframes pulse {
		0%, 100% { opacity: 0.3; }
		50% { opacity: 1; }
	}
</style>
