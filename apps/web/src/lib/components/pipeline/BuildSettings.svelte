<script lang="ts">
	import type { StreamConsistency } from '$lib/api/streaming';
	import { createEventDispatcher } from 'svelte';
	import StreamingProfileSelector from './StreamingProfileSelector.svelte';

	/**
	 * Streaming consistency selected by the operator. Bound by the
	 * pipeline editor host so the value is persisted alongside the rest
	 * of the DAG (`pipeline.dag.build_settings.streaming_consistency`).
	 *
	 * Mirrors Foundry's "Build settings" panel for streaming pipelines —
	 * see `docs/.../Streams.md` and the `Streams_assets/img_001.png`
	 * screenshot.
	 */
	export let streamingConsistency: StreamConsistency = 'AT_LEAST_ONCE';
	/**
	 * When `false`, the picker explains that consistency cannot be
	 * configured because the pipeline does not produce a streaming
	 * output. Useful in the Pipeline Builder where most pipelines are
	 * batch.
	 */
	export let isStreamingPipeline: boolean = true;

	/**
	 * P2 — Foundry "Branches in builds" panel.
	 *
	 *   * `buildBranch`             — the branch the build runs on.
	 *   * `jobSpecFallback`         — ordered list of branches the
	 *     compilation step falls back to per output dataset.
	 *   * `inputFallbackOverrides`  — per-input override map. Keys are
	 *     dataset RIDs; values are the fallback chain.
	 */
	export let buildBranch: string = 'master';
	export let jobSpecFallback: string[] = [];
	export let inputFallbackOverrides: Array<{
		datasetRid: string;
		fallbackChain: string[];
	}> = [];

	/**
	 * Pipeline RID. When set the "Resolved build plan" preview button
	 * is enabled and POSTs to
	 * `/api/v1/data-integration/pipelines/{rid}/dry-run-resolve`.
	 */
	export let pipelineRid: string | null = null;
	export let outputDatasetRids: string[] = [];

	/**
	 * Project RID surfaced to the streaming-profile selector below
	 * (P3). When set together with `pipelineRid` the selector renders
	 * inline so operators can attach profiles without leaving the
	 * Pipeline Builder.
	 */
	export let projectRid: string | null = null;

	/**
	 * D1.1.5 P2 — execution-time controls.
	 *
	 *   * `forceBuild`  — when true, the next build skips the
	 *     staleness check and recomputes every output (Foundry doc:
	 *     "force build, which recomputes all datasets as part of the
	 *     build, regardless of whether they are already up-to-date").
	 *   * `abortPolicy` — `DEPENDENT_ONLY` (default per Foundry) or
	 *     `ALL_NON_DEPENDENT`. Drives the failure cascade scope.
	 */
	export let forceBuild: boolean = false;
	export type AbortPolicy = 'DEPENDENT_ONLY' | 'ALL_NON_DEPENDENT';
	export let abortPolicy: AbortPolicy = 'DEPENDENT_ONLY';

	/**
	 * Optional list of outputs that the *previous* build marked
	 * stale_skipped. Surfaced under "Stale outputs in last build" so
	 * operators can see at a glance what would be skipped on the next
	 * non-force build.
	 */
	export let lastBuildStaleOutputs: Array<{
		jobSpecRid: string;
		outputDatasetRid: string;
	}> = [];

	const dispatch = createEventDispatcher<{
		change: {
			streamingConsistency: StreamConsistency;
			buildBranch: string;
			jobSpecFallback: string[];
			inputFallbackOverrides: Array<{ datasetRid: string; fallbackChain: string[] }>;
			forceBuild: boolean;
			abortPolicy: AbortPolicy;
		};
		'run-build': { dryRunResult: unknown };
		'save-draft': undefined;
	}>();

	const ABORT_POLICY_OPTIONS: Array<{ value: AbortPolicy; label: string; hint: string }> = [
		{
			value: 'DEPENDENT_ONLY',
			label: 'Dependent only (Foundry default)',
			hint: 'Cancel only jobs that transitively depend on the failed one. Independent jobs keep running.'
		},
		{
			value: 'ALL_NON_DEPENDENT',
			label: 'All non-dependent',
			hint: 'Abort every still-pending job on first failure, even those independent of the failure.'
		}
	];

	const OPTIONS: Array<{ value: StreamConsistency; label: string; hint: string }> = [
		{
			value: 'AT_LEAST_ONCE',
			label: 'AT_LEAST_ONCE',
			hint: 'Lower latency. Downstream consumers may see duplicates.'
		},
		{
			value: 'EXACTLY_ONCE',
			label: 'EXACTLY_ONCE',
			hint: 'Records visible only after each checkpoint completes (~2 s by default).'
		}
	];

	let newFallbackBranch = '';
	let dryRunResult: {
		jobs: Array<{
			job_spec_rid: string;
			resolved_jobspec_branch: string;
			output_dataset_rids: string[];
			resolved_outputs: Array<{
				dataset_rid: string;
				resolved_output: string;
				creates_branch: boolean;
				from_branch: string | null;
			}>;
			resolved_inputs: Array<{
				dataset_rid: string;
				resolved_input_branch: string | null;
				fallback_index: number | null;
				fallback_chain: string[];
			}>;
		}>;
		errors: Array<{ dataset_rid: string | null; kind: string; message: string }>;
	} | null = null;
	let dryRunError: string | null = null;

	function emitChange() {
		dispatch('change', {
			streamingConsistency,
			buildBranch,
			jobSpecFallback: [...jobSpecFallback],
			inputFallbackOverrides: inputFallbackOverrides.map((o) => ({
				datasetRid: o.datasetRid,
				fallbackChain: [...o.fallbackChain]
			})),
			forceBuild,
			abortPolicy
		});
	}

	function addFallback() {
		const trimmed = newFallbackBranch.trim();
		if (!trimmed) return;
		if (jobSpecFallback.includes(trimmed)) return;
		jobSpecFallback = [...jobSpecFallback, trimmed];
		newFallbackBranch = '';
		emitChange();
	}

	function removeFallback(index: number) {
		jobSpecFallback = jobSpecFallback.filter((_, i) => i !== index);
		emitChange();
	}

	function moveFallback(index: number, delta: -1 | 1) {
		const target = index + delta;
		if (target < 0 || target >= jobSpecFallback.length) return;
		const copy = [...jobSpecFallback];
		const [moved] = copy.splice(index, 1);
		copy.splice(target, 0, moved);
		jobSpecFallback = copy;
		emitChange();
	}

	function setOverrideChain(datasetRid: string, chain: string[]) {
		const idx = inputFallbackOverrides.findIndex((o) => o.datasetRid === datasetRid);
		if (idx === -1) {
			inputFallbackOverrides = [
				...inputFallbackOverrides,
				{ datasetRid, fallbackChain: chain }
			];
		} else {
			inputFallbackOverrides = inputFallbackOverrides.map((o, i) =>
				i === idx ? { ...o, fallbackChain: chain } : o
			);
		}
		emitChange();
	}

	async function runDryRun() {
		if (!pipelineRid) return;
		dryRunError = null;
		dryRunResult = null;
		try {
			const res = await fetch(
				`/api/v1/data-integration/pipelines/${encodeURIComponent(pipelineRid)}/dry-run-resolve`,
				{
					method: 'POST',
					headers: { 'content-type': 'application/json' },
					body: JSON.stringify({
						build_branch: buildBranch,
						job_spec_fallback: jobSpecFallback,
						output_dataset_rids: outputDatasetRids,
						input_overrides: inputFallbackOverrides.map((o) => ({
							dataset_rid: o.datasetRid,
							fallback_chain: o.fallbackChain
						}))
					})
				}
			);
			if (!res.ok) {
				dryRunError = `dry-run failed: ${res.status}`;
				return;
			}
			dryRunResult = await res.json();
		} catch (e) {
			dryRunError = String(e);
		}
	}
</script>

<section class="build-settings" data-testid="pipeline-build-settings">
	<header>
		<h3>Build settings</h3>
	</header>

	<!-- Build branch ---------------------------------------------------- -->
	<fieldset>
		<legend>Build branch</legend>
		<p class="hint">
			Foundry runs every build on a single <em>build branch</em>. JobSpecs and
			outputs target this branch; inputs walk the fallback chain when missing.
		</p>
		<input
			type="text"
			bind:value={buildBranch}
			on:input={emitChange}
			placeholder="master"
			data-testid="build-branch-input"
		/>
	</fieldset>

	<!-- JobSpec fallback chain ----------------------------------------- -->
	<fieldset>
		<legend>JobSpec fallback chain</legend>
		<p class="hint">
			Ordered list of branches the build's compilation step falls back to when
			no JobSpec is published on the build branch.
		</p>
		<ul class="chain" data-testid="jobspec-fallback-chain">
			{#each jobSpecFallback as branch, idx}
				<li data-testid={`jobspec-fallback-${idx}`}>
					<span class="branch">{branch}</span>
					<button
						type="button"
						on:click={() => moveFallback(idx, -1)}
						disabled={idx === 0}
						aria-label="move up"
						data-testid={`jobspec-fallback-up-${idx}`}>↑</button
					>
					<button
						type="button"
						on:click={() => moveFallback(idx, 1)}
						disabled={idx === jobSpecFallback.length - 1}
						aria-label="move down"
						data-testid={`jobspec-fallback-down-${idx}`}>↓</button
					>
					<button
						type="button"
						on:click={() => removeFallback(idx)}
						aria-label="remove"
						data-testid={`jobspec-fallback-remove-${idx}`}>×</button
					>
				</li>
			{/each}
		</ul>
		<div class="add-row">
			<input
				type="text"
				bind:value={newFallbackBranch}
				placeholder="develop"
				data-testid="jobspec-fallback-new"
			/>
			<button
				type="button"
				on:click={addFallback}
				disabled={!newFallbackBranch.trim()}
				data-testid="jobspec-fallback-add">Add fallback</button
			>
		</div>
	</fieldset>

	<!-- Per-input overrides --------------------------------------------- -->
	{#if inputFallbackOverrides.length > 0}
		<fieldset>
			<legend>Input dataset fallback overrides</legend>
			<table class="overrides" data-testid="input-fallback-overrides">
				<thead>
					<tr>
						<th>Dataset RID</th>
						<th>Fallback chain</th>
					</tr>
				</thead>
				<tbody>
					{#each inputFallbackOverrides as override}
						<tr data-testid={`input-override-${override.datasetRid}`}>
							<td><code>{override.datasetRid}</code></td>
							<td>
								<input
									type="text"
									value={override.fallbackChain.join(', ')}
									on:change={(e) =>
										setOverrideChain(
											override.datasetRid,
											(e.currentTarget as HTMLInputElement).value
												.split(',')
												.map((s) => s.trim())
												.filter(Boolean)
										)}
								/>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</fieldset>
	{/if}

	<!-- Resolved build plan preview ------------------------------------- -->
	<fieldset>
		<legend>Resolved build plan</legend>
		<button
			type="button"
			on:click={runDryRun}
			disabled={!pipelineRid}
			data-testid="dry-run-resolve-button">Preview build resolution</button
		>
		{#if dryRunError}
			<p class="error" data-testid="dry-run-error">{dryRunError}</p>
		{/if}
		{#if dryRunResult}
			<table class="resolution" data-testid="dry-run-table">
				<thead>
					<tr>
						<th>Output dataset</th>
						<th>Resolved JobSpec branch</th>
						<th>Output branch</th>
						<th>Inputs</th>
					</tr>
				</thead>
				<tbody>
					{#each dryRunResult.jobs as job}
						{#each job.resolved_outputs as out}
							<tr>
								<td><code>{out.dataset_rid}</code></td>
								<td>{job.resolved_jobspec_branch}</td>
								<td>
									{out.resolved_output}
									{#if out.creates_branch}<small>(creates from {out.from_branch})</small>{/if}
								</td>
								<td>
									{#each job.resolved_inputs as inp}
										<div>
											<code>{inp.dataset_rid}</code> → {inp.resolved_input_branch ??
												'unresolved'}
										</div>
									{/each}
								</td>
							</tr>
						{/each}
					{/each}
				</tbody>
			</table>

			<!-- P5 — Validation panel: surfaces three classes of
			     dry-run finding.
			       * `creates_branch = true` on an output ⇒ warning,
			         the build will fork on the dataset.
			       * `fallback_index > 0` on an input ⇒ info badge,
			         the input is being read from a fallback.
			       * `MISSING_JOB_SPEC` / `CYCLE` etc. errors from the
			         resolver block the "Run build" button.
			-->
			<section class="validation" data-testid="dry-run-validation">
				<header>Validation</header>
				<ul>
					{#each dryRunResult.jobs as job}
						{#each job.resolved_outputs as out}
							{#if out.creates_branch}
								<li class="validation-warning" data-testid="dry-run-warning-create-branch">
									<strong>Warning:</strong>
									Output <code>{out.dataset_rid}</code> will create a new
									branch on the target dataset, forked from
									<code>{out.from_branch ?? 'master'}</code>.
								</li>
							{/if}
						{/each}
						{#each job.resolved_inputs as inp}
							{#if inp.fallback_index != null && inp.fallback_index > 0}
								<li class="validation-info" data-testid="dry-run-info-fallback">
									<strong>Info:</strong>
									Input <code>{inp.dataset_rid}</code> will be read from
									fallback branch
									<code>{inp.resolved_input_branch}</code>.
								</li>
							{/if}
						{/each}
					{/each}
					{#each dryRunResult.errors as err}
						<li class="validation-error" data-testid="dry-run-error-row">
							<strong>{err.kind}:</strong> {err.message}
						</li>
					{/each}
				</ul>
				<div class="validation-actions">
					<button
						type="button"
						class="run-build"
						disabled={dryRunResult.errors.length > 0}
						data-testid="run-build-button"
						on:click={() => dispatch('run-build', { dryRunResult })}
					>Run build</button>
					<button
						type="button"
						class="save-draft"
						data-testid="save-draft-button"
						on:click={() => dispatch('save-draft', undefined)}
					>Save draft</button>
				</div>
			</section>
		{/if}
	</fieldset>

	<!-- Execution settings (P2) ------------------------------------------ -->
	<fieldset>
		<legend>Execution settings</legend>
		<div class="exec-row">
			<label class="toggle" data-testid="force-build-toggle-label">
				<input
					type="checkbox"
					bind:checked={forceBuild}
					on:change={emitChange}
					data-testid="force-build-toggle"
				/>
				<span>
					<strong>Force build</strong>
					<small
						title="Recomputes every output regardless of staleness. Equivalent to Foundry's force-build button — use it when you suspect the staleness check is wrong."
					>
						Recomputes every output regardless of whether inputs and logic
						are unchanged.
					</small>
				</span>
			</label>
		</div>
		<div class="abort-row">
			<p class="hint">
				<strong>Abort policy</strong> — what happens when a job fails mid-build.
			</p>
			<div class="radio-group" data-testid="abort-policy-group">
				{#each ABORT_POLICY_OPTIONS as opt}
					<label>
						<input
							type="radio"
							name="abort_policy"
							value={opt.value}
							checked={abortPolicy === opt.value}
							on:change={() => {
								abortPolicy = opt.value;
								emitChange();
							}}
							data-testid={`abort-policy-${opt.value}`}
						/>
						<span>
							<strong>{opt.label}</strong>
							<small>{opt.hint}</small>
						</span>
					</label>
				{/each}
			</div>
		</div>
	</fieldset>

	<!-- Stale outputs in last build (P2) --------------------------------- -->
	{#if lastBuildStaleOutputs.length > 0}
		<fieldset>
			<legend>Stale outputs in last build</legend>
			<p class="hint">
				These outputs were marked fresh and skipped during the previous build.
				A non-force build will skip them again unless inputs or logic change.
			</p>
			<table class="stale" data-testid="stale-outputs-table">
				<thead>
					<tr>
						<th>JobSpec RID</th>
						<th>Output dataset</th>
					</tr>
				</thead>
				<tbody>
					{#each lastBuildStaleOutputs as row}
						<tr data-testid={`stale-output-${row.outputDatasetRid}`}>
							<td><code>{row.jobSpecRid}</code></td>
							<td><code>{row.outputDatasetRid}</code></td>
						</tr>
					{/each}
				</tbody>
			</table>
		</fieldset>
	{/if}

	<!-- Streaming consistency ------------------------------------------- -->
	<fieldset disabled={!isStreamingPipeline}>
		<legend>Streaming consistency</legend>
		{#if !isStreamingPipeline}
			<p class="hint">
				This pipeline does not produce a streaming output. Add a stream sink to
				configure consistency.
			</p>
		{/if}
		<div class="radio-group">
			{#each OPTIONS as opt}
				<label>
					<input
						type="radio"
						name="streaming_consistency"
						value={opt.value}
						checked={streamingConsistency === opt.value}
						on:change={() => {
							streamingConsistency = opt.value;
							emitChange();
						}}
						data-testid={`pipeline-consistency-${opt.value}`}
					/>
					<span>
						<strong>{opt.label}</strong>
						<small>{opt.hint}</small>
					</span>
				</label>
			{/each}
		</div>
	</fieldset>

	{#if pipelineRid && projectRid}
		<fieldset>
			<legend>Streaming profiles</legend>
			<StreamingProfileSelector
				pipelineRid={pipelineRid}
				projectRid={projectRid}
			/>
		</fieldset>
	{/if}
</section>

<style>
	.build-settings {
		display: grid;
		gap: 0.75rem;
	}
	.build-settings header h3 {
		margin: 0;
	}
	.hint {
		color: #666;
		font-size: 0.85rem;
		margin: 0.25rem 0 0.5rem;
	}
	fieldset {
		border: 1px solid var(--border, #e5e5e5);
		border-radius: 6px;
		padding: 0.75rem 1rem;
	}
	fieldset[disabled] {
		opacity: 0.6;
	}
	legend {
		font-weight: 600;
		padding: 0 0.4rem;
	}
	.radio-group {
		display: grid;
		gap: 0.4rem;
	}
	.radio-group label {
		display: flex;
		gap: 0.5rem;
		align-items: flex-start;
		padding: 0.4rem;
		border-radius: 4px;
	}
	.radio-group label:hover {
		background: #f6f6f8;
	}
	.radio-group small {
		display: block;
		color: #666;
		font-weight: normal;
	}
	.chain {
		list-style: none;
		padding: 0;
		display: grid;
		gap: 0.25rem;
	}
	.chain li {
		display: flex;
		gap: 0.5rem;
		align-items: center;
		padding: 0.25rem 0.5rem;
		background: #f6f6f8;
		border-radius: 4px;
	}
	.chain .branch {
		flex: 1;
		font-family: ui-monospace, monospace;
	}
	.add-row {
		display: flex;
		gap: 0.5rem;
		margin-top: 0.5rem;
	}
	table.overrides,
	table.resolution {
		width: 100%;
		border-collapse: collapse;
	}
	th,
	td {
		text-align: left;
		padding: 0.3rem 0.4rem;
		border-bottom: 1px solid #eee;
	}
	.error {
		color: #b00020;
	}
	.errors {
		margin-top: 0.5rem;
		color: #b00020;
	}
	.exec-row {
		display: flex;
		gap: 0.5rem;
		align-items: flex-start;
		margin-bottom: 0.5rem;
	}
	.toggle {
		display: flex;
		gap: 0.5rem;
		align-items: flex-start;
		cursor: pointer;
	}
	.toggle small {
		display: block;
		color: #666;
		font-weight: normal;
	}
	.abort-row {
		border-top: 1px dashed #eee;
		padding-top: 0.5rem;
	}
	table.stale code {
		font-family: ui-monospace, monospace;
	}
</style>
