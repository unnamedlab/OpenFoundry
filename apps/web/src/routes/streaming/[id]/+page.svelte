<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import {
		listStreams,
		listTopologies,
		listBranches,
		createBranch,
		deleteBranch,
		mergeBranch,
		archiveBranch,
		getSchemaHistory,
		validateSchema,
		listCheckpoints,
		triggerCheckpoint,
		resetTopology,
		getRuntime,
		listStreamViews,
		listPipelineStreamingProfiles,
		listDeadLetters,
		type StreamDefinition,
		type TopologyDefinition,
		type StreamBranch,
		type StreamSchemaVersion,
		type ValidateSchemaResponse,
		type Checkpoint,
		type TopologyRuntimeSnapshot,
		type StreamView,
		type StreamingDeadLetter
	} from '$lib/api/streaming';
	import StreamSettings from '$lib/components/streaming/StreamSettings.svelte';
	import JobDetails from '$lib/components/streaming/JobDetails.svelte';
	import StreamLiveDataView from '$lib/components/streaming/StreamLiveDataView.svelte';
	import StreamUsage from '$lib/components/streaming/StreamUsage.svelte';

	$: streamId = ($page.params.id ?? '') as string;

	type Tab =
		| 'overview'
		| 'schema'
		| 'jobgraph'
		| 'checkpoints'
		| 'backpressure'
		| 'branches'
		| 'settings'
		| 'history'
		| 'profile'
		| 'jobdetails'
		| 'live'
		| 'usage'
		| 'deadletters'
		| 'lineage';
	let activeTab: Tab = 'overview';

	let stream: StreamDefinition | null = null;
	let topologies: TopologyDefinition[] = [];
	let selectedTopologyId = '';
	let loadingMain = false;
	let mainError = '';

	$: relatedTopologies = topologies.filter((t) =>
		t.source_stream_ids.includes(streamId)
	);
	$: coldDatasetId = stream?.source_binding.config?.cold_dataset_id as string | undefined;

	let branches: StreamBranch[] = [];
	let branchesError = '';
	let newBranchName = '';
	let newBranchDescription = '';
	let newBranchDatasetId = '';

	let history: StreamSchemaVersion[] = [];
	let historyError = '';

	let viewHistory: StreamView[] = [];
	let viewHistoryError = '';

	async function refreshViewHistory() {
		try {
			const res = await listStreamViews(streamId);
			viewHistory = res.data;
		} catch (err) {
			viewHistoryError = err instanceof Error ? err.message : String(err);
		}
	}

	function diffSchemas(curr: unknown, prev: unknown): string {
		if (!prev) return 'Initial schema';
		const a = JSON.stringify(curr);
		const b = JSON.stringify(prev);
		return a === b ? 'unchanged' : 'modified';
	}
	let schemaJson = '';
	let validateMode = '';
	let validation: ValidateSchemaResponse | null = null;
	let validateError = '';
	let validating = false;

	let checkpoints: Checkpoint[] = [];
	let checkpointsError = '';
	let runtime: TopologyRuntimeSnapshot | null = null;
	let runtimeError = '';

	async function refreshMain() {
		loadingMain = true;
		mainError = '';
		try {
			const [streamsRes, topRes] = await Promise.all([listStreams(), listTopologies()]);
			stream = streamsRes.data.find((s) => s.id === streamId) ?? null;
			topologies = topRes.data;
			if (relatedTopologies.length > 0 && !selectedTopologyId) {
				selectedTopologyId = relatedTopologies[0].id;
			}
		} catch (err) {
			mainError = err instanceof Error ? err.message : String(err);
		} finally {
			loadingMain = false;
		}
	}

	async function refreshBranches() {
		try {
			const res = await listBranches(streamId);
			branches = res.data;
		} catch (err) {
			branchesError = err instanceof Error ? err.message : String(err);
		}
	}

	async function refreshHistory() {
		try {
			const res = await getSchemaHistory(streamId);
			history = res.data;
		} catch (err) {
			historyError = err instanceof Error ? err.message : String(err);
		}
	}

	async function refreshCheckpoints() {
		if (!selectedTopologyId) return;
		try {
			const res = await listCheckpoints(selectedTopologyId);
			checkpoints = res.data.slice(0, 20);
		} catch (err) {
			checkpointsError = err instanceof Error ? err.message : String(err);
		}
	}

	async function refreshRuntime() {
		if (!selectedTopologyId) return;
		try {
			runtime = await getRuntime(selectedTopologyId);
		} catch (err) {
			runtimeError = err instanceof Error ? err.message : String(err);
		}
	}

	async function handleCreateBranch() {
		if (!newBranchName.trim()) return;
		try {
			await createBranch(streamId, {
				name: newBranchName.trim(),
				description: newBranchDescription || null,
				dataset_branch_id: newBranchDatasetId || null
			});
			newBranchName = '';
			newBranchDescription = '';
			newBranchDatasetId = '';
			await refreshBranches();
		} catch (err) {
			branchesError = err instanceof Error ? err.message : String(err);
		}
	}

	async function handleMerge(branch: StreamBranch) {
		try {
			await mergeBranch(streamId, branch.name, { target_branch: 'main' });
			await refreshBranches();
		} catch (err) {
			branchesError = err instanceof Error ? err.message : String(err);
		}
	}

	async function handleArchive(branch: StreamBranch, commitCold: boolean) {
		try {
			await archiveBranch(streamId, branch.name, { commit_cold: commitCold });
			await refreshBranches();
		} catch (err) {
			branchesError = err instanceof Error ? err.message : String(err);
		}
	}

	async function handleDeleteBranch(branch: StreamBranch) {
		if (branch.name === 'main') return;
		if (!confirm(`Delete branch '${branch.name}'?`)) return;
		try {
			await deleteBranch(streamId, branch.name);
			await refreshBranches();
		} catch (err) {
			branchesError = err instanceof Error ? err.message : String(err);
		}
	}

	async function handleValidate() {
		validateError = '';
		validation = null;
		validating = true;
		try {
			const parsed = JSON.parse(schemaJson);
			validation = await validateSchema(streamId, {
				schema_avro: parsed,
				compatibility: validateMode || undefined
			});
		} catch (err) {
			validateError = err instanceof Error ? err.message : String(err);
		} finally {
			validating = false;
		}
	}

	async function handleTriggerCheckpoint() {
		if (!selectedTopologyId) return;
		try {
			await triggerCheckpoint(selectedTopologyId, { trigger: 'manual' });
			await refreshCheckpoints();
		} catch (err) {
			checkpointsError = err instanceof Error ? err.message : String(err);
		}
	}

	async function handleRestoreCheckpoint(cp: Checkpoint) {
		if (!confirm(`Restore topology from checkpoint ${cp.id}?`)) return;
		try {
			await resetTopology(cp.topology_id, { from_checkpoint_id: cp.id });
			await refreshCheckpoints();
			await refreshRuntime();
		} catch (err) {
			checkpointsError = err instanceof Error ? err.message : String(err);
		}
	}

	function tabClick(tab: Tab) {
		activeTab = tab;
		if (tab === 'branches' && branches.length === 0) refreshBranches();
		if (tab === 'schema' && history.length === 0) refreshHistory();
		if (tab === 'checkpoints') refreshCheckpoints();
		if (tab === 'backpressure') refreshRuntime();
		if (tab === 'history' && viewHistory.length === 0) refreshViewHistory();
	}

	async function fetchDeadLetters(rid: string): Promise<StreamingDeadLetter[]> {
		const res = await listDeadLetters(rid);
		return res.data;
	}

	onMount(() => {
		refreshMain();
		refreshBranches();
		refreshHistory();
	});
</script>

<section class="stream-detail">
	<header>
		<a href="/streaming">← Streams</a>
		{#if stream}
			<h1>{stream.name}</h1>
			<p class="meta">
				<span class="badge">{stream.status}</span>
				<span class="badge" data-testid="stream-type-badge">
					{stream.stream_type ?? 'STANDARD'}
				</span>
				<span
					class="badge"
					data-testid="stream-pipeline-consistency-badge"
				>
					pipeline: {stream.pipeline_consistency ?? 'AT_LEAST_ONCE'}
				</span>
				<span>partitions: {stream.partitions}</span>
			</p>
		{:else if loadingMain}
			<h1>Loading…</h1>
		{:else}
			<h1>Stream {streamId}</h1>
			{#if mainError}<p class="error">{mainError}</p>{/if}
		{/if}
	</header>

	<nav class="tabs">
		{#each [
			['overview', 'Overview'],
			['schema', 'Schema'],
			['jobgraph', 'Job Graph'],
			['jobdetails', 'Job Details'],
			['live', 'Live'],
			['usage', 'Usage'],
			['checkpoints', 'Checkpoints'],
			['deadletters', 'Dead letters'],
			['backpressure', 'Backpressure'],
			['branches', 'Branches'],
			['history', 'History'],
			['profile', 'Profile'],
			['settings', 'Settings'],
			['lineage', 'Lineage']
		] as [key, label]}
			<button
				class:active={activeTab === key}
				on:click={() => tabClick(key as Tab)}
			>{label}</button>
		{/each}
	</nav>

	{#if activeTab === 'overview' && stream}
		<section class="panel">
			<h2>Overview</h2>
			<dl class="kv">
				<dt>ID</dt><dd><code>{stream.id}</code></dd>
				<dt>Description</dt><dd>{stream.description || '—'}</dd>
				<dt>Source</dt>
				<dd>
					<code>{stream.source_binding.connector_type}://{stream.source_binding.endpoint}</code>
				</dd>
				<dt>Format</dt><dd>{stream.source_binding.format}</dd>
				<dt>Retention</dt><dd>{stream.retention_hours} h</dd>
				<dt>Created</dt><dd>{new Date(stream.created_at).toLocaleString()}</dd>
			</dl>
			{#if coldDatasetId}
				<a class="btn" href={`/datasets/${coldDatasetId}`}>Open as Dataset →</a>
			{:else}
				<p class="hint">
					No cold dataset linked yet. Configure
					<code>source_binding.config.cold_dataset_id</code> to enable the
					"Open as Dataset" link (Bloque F3).
				</p>
			{/if}
			<h3>Related topologies</h3>
			<ul>
				{#each relatedTopologies as t}
					<li><strong>{t.name}</strong> — {t.status}</li>
				{:else}
					<li class="hint">No topology consumes this stream.</li>
				{/each}
			</ul>
		</section>
	{:else if activeTab === 'schema'}
		<section class="panel">
			<h2>Validate Avro schema</h2>
			<textarea
				rows="10"
				bind:value={schemaJson}
				placeholder={'{"type":"record","name":"Order","fields":[…]}'}
			></textarea>
			<label>
				Compatibility mode:
				<select bind:value={validateMode}>
					<option value="">(use stream default)</option>
					<option value="NONE">NONE</option>
					<option value="BACKWARD">BACKWARD</option>
					<option value="FORWARD">FORWARD</option>
					<option value="FULL">FULL</option>
				</select>
			</label>
			<button on:click={handleValidate} disabled={validating}>
				{validating ? 'Validating…' : 'Validate'}
			</button>
			{#if validateError}<p class="error">{validateError}</p>{/if}
			{#if validation}
				<div class="validation-result" class:valid={validation.valid}>
					<p>
						<strong>{validation.valid ? '✓ Valid' : '✗ Invalid'}</strong>
						{#if validation.fingerprint}<code>{validation.fingerprint}</code>{/if}
					</p>
					{#if validation.compatibility}
						<p>
							Compatibility ({validation.compatibility.mode}):
							{validation.compatibility.compatible ? 'OK' : validation.compatibility.reason}
						</p>
					{/if}
					{#if validation.errors.length > 0}
						<ul>{#each validation.errors as err}<li class="error">{err}</li>{/each}</ul>
					{/if}
					{#if validation.warnings.length > 0}
						<ul>{#each validation.warnings as w}<li class="warning">{w}</li>{/each}</ul>
					{/if}
				</div>
			{/if}

			<h3>History</h3>
			{#if historyError}<p class="error">{historyError}</p>{/if}
			<table>
				<thead><tr><th>Version</th><th>Fingerprint</th><th>Compat</th><th>By</th><th>At</th></tr></thead>
				<tbody>
					{#each history as v}
						<tr>
							<td>v{v.version}</td>
							<td><code>{v.fingerprint}</code></td>
							<td>{v.compatibility}</td>
							<td>{v.created_by}</td>
							<td>{new Date(v.created_at).toLocaleString()}</td>
						</tr>
					{:else}
						<tr><td colspan="5">No schema versions recorded.</td></tr>
					{/each}
				</tbody>
			</table>
		</section>
	{:else if activeTab === 'jobgraph'}
		<section class="panel">
			<h2>Job Graph</h2>
			<label>
				Topology:
				<select bind:value={selectedTopologyId}>
					<option value="">(none)</option>
					{#each relatedTopologies as t}
						<option value={t.id}>{t.name}</option>
					{/each}
				</select>
			</label>
			{#if selectedTopologyId}
				<p class="hint">
					Open <a href={`/streaming/topology/${selectedTopologyId}`}>full job graph</a>
					(uses the dedicated topology view).
				</p>
			{:else}
				<p class="hint">No related topologies.</p>
			{/if}
		</section>
	{:else if activeTab === 'checkpoints'}
		<section class="panel">
			<h2>Checkpoints</h2>
			<label>
				Topology:
				<select bind:value={selectedTopologyId} on:change={refreshCheckpoints}>
					<option value="">(none)</option>
					{#each relatedTopologies as t}
						<option value={t.id}>{t.name}</option>
					{/each}
				</select>
			</label>
			<button on:click={handleTriggerCheckpoint} disabled={!selectedTopologyId}>
				Trigger manual checkpoint
			</button>
			{#if checkpointsError}<p class="error">{checkpointsError}</p>{/if}
			<table>
				<thead>
					<tr>
						<th>ID</th><th>Started</th><th>Duration</th>
						<th>Trigger</th><th>Status</th><th>Savepoint</th><th></th>
					</tr>
				</thead>
				<tbody>
					{#each checkpoints as cp}
						<tr>
							<td><code>{cp.id.slice(0, 8)}</code></td>
							<td>{new Date(cp.created_at).toLocaleString()}</td>
							<td>{cp.duration_ms} ms</td>
							<td>{cp.trigger}</td>
							<td>{cp.status}</td>
							<td>{cp.savepoint_uri ?? '—'}</td>
							<td>
								<button on:click={() => handleRestoreCheckpoint(cp)}>
									Restore from this checkpoint
								</button>
							</td>
						</tr>
					{:else}
						<tr><td colspan="7">No checkpoints recorded.</td></tr>
					{/each}
				</tbody>
			</table>
		</section>
	{:else if activeTab === 'backpressure'}
		<section class="panel">
			<h2>Backpressure</h2>
			<label>
				Topology:
				<select bind:value={selectedTopologyId} on:change={refreshRuntime}>
					<option value="">(none)</option>
					{#each relatedTopologies as t}
						<option value={t.id}>{t.name}</option>
					{/each}
				</select>
			</label>
			{#if runtimeError}<p class="error">{runtimeError}</p>{/if}
			{#if runtime}
				<details open>
					<summary>Runtime payload</summary>
					<pre>{JSON.stringify(runtime, null, 2)}</pre>
				</details>
			{/if}
		</section>
	{:else if activeTab === 'branches'}
		<section class="panel">
			<h2>Branches</h2>
			{#if branchesError}<p class="error">{branchesError}</p>{/if}
			<form on:submit|preventDefault={handleCreateBranch} class="create-form">
				<input bind:value={newBranchName} placeholder="branch name" required />
				<input bind:value={newBranchDescription} placeholder="description" />
				<input bind:value={newBranchDatasetId} placeholder="dataset_branch_id (optional)" />
				<button type="submit">Create branch</button>
			</form>
			<table>
				<thead>
					<tr>
						<th>Name</th><th>Status</th><th>Head</th>
						<th>Dataset</th><th>Created</th><th>Actions</th>
					</tr>
				</thead>
				<tbody>
					{#each branches as b}
						<tr>
							<td><strong>{b.name}</strong></td>
							<td>{b.status}</td>
							<td>{b.head_sequence_no}</td>
							<td>{b.dataset_branch_id ?? '—'}</td>
							<td>{new Date(b.created_at).toLocaleString()}</td>
							<td class="actions">
								{#if b.name !== 'main' && b.status === 'active'}
									<button on:click={() => handleMerge(b)}>Merge → main</button>
									<button on:click={() => handleArchive(b, false)}>Archive</button>
									<button on:click={() => handleArchive(b, true)}>Archive + cold</button>
								{/if}
								{#if b.name !== 'main'}
									<button class="danger" on:click={() => handleDeleteBranch(b)}>Delete</button>
								{/if}
							</td>
						</tr>
					{:else}
						<tr><td colspan="6">No branches yet.</td></tr>
					{/each}
				</tbody>
			</table>
		</section>
	{:else if activeTab === 'settings' && stream}
		<section class="panel">
			<h2>Settings</h2>
			<StreamSettings
				{streamId}
				streamName={stream.name}
				streamKind={stream.kind ?? 'INGEST'}
			/>
		</section>
	{:else if activeTab === 'history'}
		<section class="panel" data-testid="history-tab">
			<h2>History</h2>
			<p class="hint">
				Every Reset Stream rotates the viewRid. Push consumers must update their POST URL on each
				new generation; downstream pipelines must replay.
			</p>
			{#if viewHistoryError}<p class="error">{viewHistoryError}</p>{/if}
			<table>
				<thead>
					<tr>
						<th>Generation</th>
						<th>viewRid</th>
						<th>Status</th>
						<th>Schema</th>
						<th>Created</th>
						<th>Retired</th>
						<th>By</th>
					</tr>
				</thead>
				<tbody>
					{#each viewHistory as v, idx (v.id)}
						<tr>
							<td><strong>{v.generation}</strong></td>
							<td><code>{v.view_rid}</code></td>
							<td>
								{#if v.active}
									<span class="badge active-badge">active</span>
								{:else}
									<span class="badge">retired</span>
								{/if}
							</td>
							<td>
								{diffSchemas(
									v.schema_json,
									viewHistory[idx + 1]?.schema_json ?? null
								)}
							</td>
							<td>{new Date(v.created_at).toLocaleString()}</td>
							<td>{v.retired_at ? new Date(v.retired_at).toLocaleString() : '—'}</td>
							<td>{v.created_by}</td>
						</tr>
					{:else}
						<tr><td colspan="7">No views recorded.</td></tr>
					{/each}
				</tbody>
			</table>
		</section>
	{:else if activeTab === 'jobdetails' && stream}
		<section class="panel">
			<h2>Job Details</h2>
			<JobDetails
				{streamId}
				streamName={stream.name}
				topology={relatedTopologies[0] ?? null}
			/>
		</section>
	{:else if activeTab === 'live' && stream}
		<section class="panel">
			<h2>Live data</h2>
			<StreamLiveDataView {streamId} />
		</section>
	{:else if activeTab === 'usage' && stream}
		<section class="panel" data-testid="usage-tab">
			<h2>Usage</h2>
			<StreamUsage {streamId} />
		</section>
	{:else if activeTab === 'deadletters' && stream}
		<section class="panel" data-testid="deadletters-tab">
			<h2>Dead letters</h2>
			<p class="hint">
				Records the push proxy or runtime rejected. The reason column tells you
				whether it was a schema mismatch (Avro/structural), an avro-validate
				failure, or a hot buffer publish error.
			</p>
			{#await fetchDeadLetters(streamId)}
				<p>Loading…</p>
			{:then deadLetters}
				<table>
					<thead>
						<tr><th>Reason</th><th>Event time</th><th>Replays</th><th>Payload</th></tr>
					</thead>
					<tbody>
						{#each deadLetters as dl (dl.id)}
							<tr>
								<td>{dl.reason}</td>
								<td>{new Date(dl.event_time).toLocaleString()}</td>
								<td>{dl.replay_count}</td>
								<td>
									<details>
										<summary>{Object.keys(dl.payload).length} fields</summary>
										<pre>{JSON.stringify(dl.payload, null, 2)}</pre>
									</details>
								</td>
							</tr>
						{:else}
							<tr><td colspan="4">No dead letters.</td></tr>
						{/each}
					</tbody>
				</table>
			{:catch err}
				<p class="error">{err instanceof Error ? err.message : String(err)}</p>
			{/await}
		</section>
	{:else if activeTab === 'profile' && stream}
		<section class="panel" data-testid="profile-tab">
			<h2>Streaming profiles</h2>
			<p class="hint">
				Profiles attached to topologies that consume this stream. To attach a
				profile, open the topology and edit its build settings, or
				<a href="/control-panel/streaming-profiles">manage profile imports in Control Panel</a>.
			</p>
			{#each relatedTopologies as t (t.id)}
				<div class="profile-block">
					<h3>{t.name}</h3>
					<p class="hint">topology <code>{t.id}</code></p>
					{#await listPipelineStreamingProfiles(t.id)}
						<p>Loading…</p>
					{:then result}
						<ul>
							{#each result.data as p}
								<li>
									<strong>{p.name}</strong>
									<small>{p.category} · {p.size_class}</small>
									{#if p.restricted}<span class="badge restricted">restricted</span>{/if}
								</li>
							{:else}
								<li class="hint">No profiles attached — Foundry defaults apply.</li>
							{/each}
						</ul>
					{:catch err}
						<p class="error">{err instanceof Error ? err.message : String(err)}</p>
					{/await}
				</div>
			{:else}
				<p class="hint">No topology consumes this stream yet.</p>
			{/each}
		</section>
	{:else if activeTab === 'lineage'}
		<section class="panel">
			<h2>Lineage</h2>
			<p class="hint">
				This stream feeds {relatedTopologies.length} topologies; downstream
				datasets and dashboards are tracked by data-asset-catalog-service.
			</p>
			<ul>
				{#each relatedTopologies as t}
					<li>{t.name} → topology <code>{t.id.slice(0, 8)}</code></li>
				{/each}
			</ul>
			{#if coldDatasetId}
				<p>Cold dataset: <a href={`/datasets/${coldDatasetId}`}>{coldDatasetId}</a></p>
			{/if}
		</section>
	{/if}
</section>

<style>
	.stream-detail {
		padding: 1rem 1.5rem;
		max-width: 1200px;
		margin: 0 auto;
	}
	header h1 {
		margin: 0.25rem 0 0.4rem;
		font-size: 1.5rem;
	}
	.meta {
		display: flex;
		gap: 0.5rem;
		flex-wrap: wrap;
		font-size: 0.85rem;
		color: #555;
	}
	.badge {
		background: #eef;
		padding: 0.1rem 0.5rem;
		border-radius: 3px;
	}
	.badge.active-badge {
		background: #d4f4dd;
		color: #1f5631;
	}
	.badge.restricted {
		background: #fde7e9;
		color: #720010;
		font-weight: 600;
		margin-left: 0.4rem;
	}
	.profile-block {
		margin: 0.75rem 0;
		padding: 0.5rem 0.75rem;
		border: 1px solid #eee;
		border-radius: 4px;
	}
	.tabs {
		display: flex;
		gap: 0.25rem;
		border-bottom: 1px solid var(--border, #ddd);
		margin: 1rem 0;
		flex-wrap: wrap;
	}
	.tabs button {
		background: transparent;
		border: 0;
		padding: 0.5rem 1rem;
		cursor: pointer;
		border-bottom: 2px solid transparent;
	}
	.tabs button.active {
		border-bottom-color: currentColor;
		font-weight: 600;
	}
	.panel {
		background: var(--panel, #fff);
		padding: 1rem;
		border-radius: 6px;
		box-shadow: 0 1px 3px rgba(0, 0, 0, 0.05);
	}
	.kv {
		display: grid;
		grid-template-columns: max-content 1fr;
		gap: 0.4rem 1rem;
		margin: 0.5rem 0 1rem;
	}
	.kv dt { font-weight: 600; color: #555; }
	.kv dd { margin: 0; }
	.btn {
		display: inline-block;
		padding: 0.4rem 0.8rem;
		background: #246;
		color: #fff;
		border-radius: 4px;
		text-decoration: none;
	}
	.create-form {
		display: flex;
		gap: 0.5rem;
		flex-wrap: wrap;
		margin-bottom: 1rem;
	}
	.create-form input { flex: 1 1 180px; padding: 0.4rem 0.6rem; }
	table { width: 100%; border-collapse: collapse; margin-top: 0.5rem; }
	th, td {
		text-align: left;
		padding: 0.4rem 0.6rem;
		border-bottom: 1px solid #eee;
		vertical-align: top;
	}
	.actions { display: flex; flex-wrap: wrap; gap: 0.3rem; }
	.actions button { font-size: 0.75rem; padding: 0.2rem 0.45rem; }
	.danger { color: #b00; }
	textarea { width: 100%; font-family: ui-monospace, monospace; }
	.validation-result {
		border-left: 4px solid #b00;
		padding: 0.5rem 0.75rem;
		margin: 0.75rem 0;
		background: #fff3f3;
	}
	.validation-result.valid { border-color: #2a7; background: #f3fff5; }
	.error { color: #b00; }
	.warning { color: #a60; }
	.hint { color: #666; font-style: italic; }
	code { font-family: ui-monospace, monospace; font-size: 0.85em; }
	pre { background: #f6f6f6; padding: 0.5rem; overflow: auto; }
</style>
