<script lang="ts">
	import { onMount } from 'svelte';
	import {
		listMonitoringViews,
		createMonitoringView,
		listRulesForView,
		createMonitorRule,
		listEvaluationsForRule,
		MONITOR_KIND_CATALOGUE,
		type MonitoringView,
		type MonitorRule,
		type MonitorKind,
		type ResourceType,
		type Comparator,
		type Severity
	} from '$lib/api/monitoring';

	type Tab = 'views' | 'manage';
	let activeTab: Tab = 'views';

	let views: MonitoringView[] = [];
	let activeView: MonitoringView | null = null;
	let rulesByView = new Map<string, MonitorRule[]>();
	let firingByRule = new Map<string, boolean>();
	let firingCountByRule = new Map<string, number>();
	let loading = false;
	let error = '';

	// Create-view modal.
	let createViewOpen = false;
	let newView = { name: '', description: '', project_rid: '' };

	// Add-monitor wizard state.
	let wizardOpen = false;
	let wizardStep: 1 | 2 | 3 | 4 | 5 = 1;
	let wizard = {
		view_id: '',
		resource_type: 'STREAMING_DATASET' as ResourceType,
		resource_rid: '',
		monitor_kind: 'INGEST_RECORDS' as MonitorKind,
		window_seconds: 300,
		comparator: 'LTE' as Comparator,
		threshold: 0,
		severity: 'WARN' as Severity,
		name: ''
	};
	let wizardError = '';

	const RESOURCE_TYPES: Array<{ value: ResourceType; label: string; beta?: boolean }> = [
		{ value: 'STREAMING_DATASET', label: 'Streaming dataset' },
		{ value: 'STREAMING_PIPELINE', label: 'Streaming pipeline' },
		{ value: 'TIME_SERIES_SYNC', label: 'Time series sync', beta: true },
		{ value: 'GEOTEMPORAL_OBSERVATIONS', label: 'Geotemporal observations', beta: true }
	];

	$: kindsForResource = MONITOR_KIND_CATALOGUE.filter((k) =>
		k.resourceTypes.includes(wizard.resource_type)
	);

	async function refresh() {
		loading = true;
		error = '';
		try {
			const res = await listMonitoringViews();
			views = res.data;
			if (!activeView && views.length > 0) activeView = views[0];
			await refreshRules();
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		} finally {
			loading = false;
		}
	}

	async function refreshRules() {
		if (!activeView) return;
		try {
			const res = await listRulesForView(activeView.id);
			rulesByView.set(activeView.id, res.data);
			rulesByView = new Map(rulesByView);
			// Best-effort firing state for each rule.
			for (const rule of res.data) {
				try {
					const evals = await listEvaluationsForRule(rule.id, 24);
					const last = evals.data[0];
					firingByRule.set(rule.id, !!last?.fired);
					firingCountByRule.set(
						rule.id,
						evals.data.filter((e) => e.fired).length
					);
				} catch {
					firingByRule.set(rule.id, false);
					firingCountByRule.set(rule.id, 0);
				}
			}
			firingByRule = new Map(firingByRule);
			firingCountByRule = new Map(firingCountByRule);
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
	}

	async function submitCreateView() {
		try {
			const v = await createMonitoringView({
				name: newView.name.trim(),
				description: newView.description,
				project_rid: newView.project_rid.trim()
			});
			views = [v, ...views];
			activeView = v;
			createViewOpen = false;
			newView = { name: '', description: '', project_rid: '' };
			await refreshRules();
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
	}

	function openWizard() {
		if (!activeView) {
			error = 'Select or create a Monitoring View first.';
			return;
		}
		wizard = {
			view_id: activeView.id,
			resource_type: 'STREAMING_DATASET',
			resource_rid: '',
			monitor_kind: 'INGEST_RECORDS',
			window_seconds: 300,
			comparator: 'LTE',
			threshold: 0,
			severity: 'WARN',
			name: ''
		};
		wizardStep = 1;
		wizardError = '';
		wizardOpen = true;
	}

	function nextStep() {
		wizardError = '';
		if (wizardStep === 2 && !wizard.resource_rid.trim()) {
			wizardError = 'Resource RID is required.';
			return;
		}
		if (wizardStep < 5) wizardStep = ((wizardStep + 1) as 1 | 2 | 3 | 4 | 5);
	}

	function prevStep() {
		if (wizardStep > 1) wizardStep = ((wizardStep - 1) as 1 | 2 | 3 | 4 | 5);
	}

	async function submitWizard() {
		try {
			await createMonitorRule({
				view_id: wizard.view_id,
				name: wizard.name || `${wizard.monitor_kind} on ${wizard.resource_rid}`,
				resource_type: wizard.resource_type,
				resource_rid: wizard.resource_rid,
				monitor_kind: wizard.monitor_kind,
				window_seconds: wizard.window_seconds,
				comparator: wizard.comparator,
				threshold: wizard.threshold,
				severity: wizard.severity
			});
			wizardOpen = false;
			await refreshRules();
		} catch (err) {
			wizardError = err instanceof Error ? err.message : String(err);
		}
	}

	$: rules = activeView ? rulesByView.get(activeView.id) ?? [] : [];

	onMount(refresh);
</script>

<section class="data-health" data-testid="data-health-page">
	<header>
		<h1>Data Health</h1>
		<p class="hint">
			Monitor streaming datasets, pipelines, time-series syncs and geotemporal
			observations. Beta features below are clearly marked.
		</p>
		<div class="banner banner-beta" role="note">
			<strong>Beta</strong> — Stream monitoring is in beta. Time-series and geotemporal
			monitors observe records sent rather than processed.
		</div>
	</header>

	<nav class="tabs">
		<button class:active={activeTab === 'views'} on:click={() => (activeTab = 'views')}>
			Monitoring Views
		</button>
		<button class:active={activeTab === 'manage'} on:click={() => (activeTab = 'manage')}>
			Manage Monitors
		</button>
	</nav>

	{#if error}<p class="error">{error}</p>{/if}
	{#if loading}<p>Loading…</p>{/if}

	{#if activeTab === 'views'}
		<div class="views-toolbar">
			<button type="button" on:click={() => (createViewOpen = true)} data-testid="create-view-btn">
				+ Create monitoring view
			</button>
		</div>
		<table data-testid="views-table">
			<thead>
				<tr><th>Name</th><th>Project</th><th>Created</th><th>Created by</th><th></th></tr>
			</thead>
			<tbody>
				{#each views as v (v.id)}
					<tr>
						<td><strong>{v.name}</strong><small class="block">{v.description}</small></td>
						<td><code>{v.project_rid}</code></td>
						<td>{new Date(v.created_at).toLocaleString()}</td>
						<td>{v.created_by}</td>
						<td>
							<button
								type="button"
								on:click={() => {
									activeView = v;
									activeTab = 'manage';
									refreshRules();
								}}
							>Open</button>
						</td>
					</tr>
				{:else}
					<tr><td colspan="5" class="hint">No views yet.</td></tr>
				{/each}
			</tbody>
		</table>
	{:else}
		<div class="manage-toolbar">
			<select bind:value={activeView} on:change={refreshRules}>
				{#each views as v (v.id)}<option value={v}>{v.name}</option>{/each}
			</select>
			<button
				type="button"
				on:click={openWizard}
				disabled={!activeView}
				data-testid="add-monitor-btn"
			>+ Add new alert</button>
		</div>
		<table data-testid="rules-table">
			<thead>
				<tr>
					<th>Name</th><th>Resource</th><th>Monitor</th><th>Window</th>
					<th>Threshold</th><th>Severity</th><th>State</th><th>Fired (24h)</th>
				</tr>
			</thead>
			<tbody>
				{#each rules as rule (rule.id)}
					<tr>
						<td><strong>{rule.name || rule.monitor_kind}</strong></td>
						<td>
							<small>{rule.resource_type}</small>
							<code>{rule.resource_rid}</code>
						</td>
						<td>{rule.monitor_kind}</td>
						<td>{rule.window_seconds} s</td>
						<td>{rule.comparator} {rule.threshold}</td>
						<td>{rule.severity}</td>
						<td>
							{#if firingByRule.get(rule.id)}
								<span class="badge fail" data-testid={`firing-${rule.id}`}>FIRING</span>
							{:else}
								<span class="badge ok">ok</span>
							{/if}
						</td>
						<td>{firingCountByRule.get(rule.id) ?? 0}</td>
					</tr>
				{:else}
					<tr><td colspan="8" class="hint">No rules in this view.</td></tr>
				{/each}
			</tbody>
		</table>
	{/if}
</section>

{#if createViewOpen}
	<div class="modal-backdrop" role="presentation" on:click|self={() => (createViewOpen = false)}>
		<div class="modal" role="dialog" aria-modal="true">
			<header>
				<h2>Create monitoring view</h2>
				<button on:click={() => (createViewOpen = false)} aria-label="Close">×</button>
			</header>
			<label>Name <input bind:value={newView.name} /></label>
			<label>Project RID <input bind:value={newView.project_rid} placeholder="ri.compass.main.project.…" /></label>
			<label>Description <input bind:value={newView.description} /></label>
			<div class="modal-actions">
				<button on:click={() => (createViewOpen = false)}>Cancel</button>
				<button on:click={submitCreateView} data-testid="create-view-submit">Create</button>
			</div>
		</div>
	</div>
{/if}

{#if wizardOpen}
	<div class="modal-backdrop" role="presentation" on:click|self={() => (wizardOpen = false)}>
		<div class="modal wizard" role="dialog" aria-modal="true" data-testid="add-monitor-wizard">
			<header>
				<h2>Add new alert · step {wizardStep} / 5</h2>
				<button on:click={() => (wizardOpen = false)} aria-label="Close">×</button>
			</header>

			{#if wizardStep === 1}
				<p class="hint">Choose the resource type to monitor.</p>
				<div class="chip-grid">
					{#each RESOURCE_TYPES as r}
						<button
							type="button"
							class="chip"
							class:selected={wizard.resource_type === r.value}
							on:click={() => (wizard.resource_type = r.value)}
						>
							{r.label}
							{#if r.beta}<span class="chip-beta">Beta</span>{/if}
						</button>
					{/each}
				</div>
			{:else if wizardStep === 2}
				<p class="hint">Enter the resource RID.</p>
				<label>
					Resource RID
					<input
						bind:value={wizard.resource_rid}
						placeholder={wizard.resource_type === 'STREAMING_DATASET'
							? 'ri.streams.main.stream.…'
							: 'ri.…'}
						data-testid="wizard-resource-rid"
					/>
				</label>
			{:else if wizardStep === 3}
				<p class="hint">Pick a monitor kind. Grouped by category.</p>
				{#each ['INGEST', 'OUTPUT', 'CHECKPOINT', 'PERFORMANCE'] as group}
					{@const filtered = kindsForResource.filter((k) => k.group === group)}
					{#if filtered.length}
						<fieldset>
							<legend>{group}</legend>
							{#each filtered as k}
								<label class="kind-row">
									<input
										type="radio"
										name="kind"
										value={k.kind}
										checked={wizard.monitor_kind === k.kind}
										on:change={() => (wizard.monitor_kind = k.kind)}
										data-testid={`wizard-kind-${k.kind}`}
									/>
									<span>
										<strong>{k.label}</strong>
										<small>{k.hint}</small>
									</span>
								</label>
							{/each}
						</fieldset>
					{/if}
				{/each}
			{:else if wizardStep === 4}
				<p class="hint">Window, comparator, threshold and severity.</p>
				<label>
					Window
					<select bind:value={wizard.window_seconds}>
						<option value={300}>5 minutes</option>
						<option value={1800}>30 minutes</option>
						<option value={900}>15 minutes</option>
						<option value={3600}>1 hour</option>
					</select>
				</label>
				<label>
					Comparator
					<select bind:value={wizard.comparator}>
						<option value="LTE">≤</option>
						<option value="LT">&lt;</option>
						<option value="GTE">≥</option>
						<option value="GT">&gt;</option>
						<option value="EQ">=</option>
					</select>
				</label>
				<label>
					Threshold
					<input
						type="number"
						step="any"
						bind:value={wizard.threshold}
						data-testid="wizard-threshold"
					/>
				</label>
				<label>
					Severity
					<select bind:value={wizard.severity}>
						<option value="INFO">INFO</option>
						<option value="WARN">WARN</option>
						<option value="CRITICAL">CRITICAL</option>
					</select>
				</label>
				<label>
					Name (optional)
					<input bind:value={wizard.name} placeholder="Auto-named if blank" />
				</label>
			{:else}
				<p class="hint">Review your monitor and save.</p>
				<dl class="kv">
					<dt>Resource</dt><dd>{wizard.resource_type} · <code>{wizard.resource_rid}</code></dd>
					<dt>Monitor</dt><dd>{wizard.monitor_kind}</dd>
					<dt>Rule</dt><dd>{wizard.comparator} {wizard.threshold} over {wizard.window_seconds}s</dd>
					<dt>Severity</dt><dd>{wizard.severity}</dd>
				</dl>
			{/if}

			{#if wizardError}<p class="error">{wizardError}</p>{/if}
			<div class="modal-actions">
				{#if wizardStep > 1}
					<button on:click={prevStep}>Back</button>
				{/if}
				{#if wizardStep < 5}
					<button on:click={nextStep} data-testid="wizard-next">Next</button>
				{:else}
					<button on:click={submitWizard} data-testid="wizard-save">Save monitoring rules</button>
				{/if}
			</div>
		</div>
	</div>
{/if}

<style>
	.data-health { padding: 1rem 1.5rem; max-width: 1200px; margin: 0 auto; }
	.banner { padding: 0.5rem 0.75rem; border-radius: 4px; margin: 0.5rem 0; }
	.banner-beta { background: #fff8e1; border-left: 4px solid #d97706; color: #92400e; }
	.tabs { display: flex; gap: 0.25rem; border-bottom: 1px solid #ddd; margin: 1rem 0; }
	.tabs button { background: none; border: 0; padding: 0.5rem 1rem; cursor: pointer; border-bottom: 2px solid transparent; }
	.tabs button.active { border-bottom-color: currentColor; font-weight: 600; }
	.views-toolbar, .manage-toolbar { display: flex; gap: 0.5rem; align-items: center; margin-bottom: 0.5rem; }
	table { width: 100%; border-collapse: collapse; }
	th, td { text-align: left; padding: 0.4rem 0.6rem; border-bottom: 1px solid #eee; vertical-align: top; }
	.badge { display: inline-block; padding: 0.05rem 0.45rem; border-radius: 3px; font-size: 0.7rem; }
	.badge.ok { background: #d4f4dd; color: #1f5631; }
	.badge.fail { background: #fde7e9; color: #720010; font-weight: 600; }
	.error { color: #b00; }
	.hint { color: #666; }
	.block { display: block; color: #777; }
	.modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.4); display: flex; align-items: center; justify-content: center; z-index: 50; }
	.modal { background: #fff; border-radius: 6px; padding: 1rem 1.25rem; width: min(640px, 95vw); display: grid; gap: 0.75rem; }
	.modal.wizard { gap: 0.5rem; }
	.modal header { display: flex; justify-content: space-between; align-items: center; }
	.modal header h2 { margin: 0; font-size: 1.1rem; }
	.modal label { display: grid; gap: 0.25rem; }
	.modal-actions { display: flex; justify-content: flex-end; gap: 0.5rem; }
	.chip-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 0.5rem; }
	.chip { display: flex; gap: 0.4rem; align-items: center; justify-content: center; padding: 0.5rem; border: 1px solid #ddd; border-radius: 4px; background: #fff; cursor: pointer; }
	.chip.selected { background: #246; color: #fff; border-color: #246; }
	.chip-beta { background: #fff3cd; color: #5b4500; padding: 0.05rem 0.3rem; border-radius: 3px; font-size: 0.7rem; }
	.kind-row { display: flex; align-items: flex-start; gap: 0.5rem; padding: 0.4rem 0; }
	.kind-row small { display: block; color: #666; }
	fieldset { border: 1px solid #eee; border-radius: 4px; padding: 0.5rem 0.75rem; margin: 0; }
	.kv { display: grid; grid-template-columns: max-content 1fr; gap: 0.4rem 1rem; }
	.kv dt { font-weight: 600; color: #555; }
</style>
