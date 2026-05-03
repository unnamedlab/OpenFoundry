<script lang="ts">
	import { onMount } from 'svelte';
	import {
		getStreamConfig,
		updateStreamConfig,
		getCurrentStreamView,
		resetStream,
		type StreamConfig,
		type StreamType,
		type StreamConsistency,
		type StreamView,
		type StreamKind
	} from '$lib/api/streaming';

	export let streamId: string;
	export let streamName: string = '';
	export let streamKind: StreamKind = 'INGEST';

	let config: StreamConfig | null = null;
	let loading = false;
	let saving = false;
	let error = '';
	let savedMessage = '';

	// Reset modal state.
	let resetModalOpen = false;
	let resetSchemaJson = '';
	let resetForceReset = false;
	let resetConfirmName = '';
	let resetSubmitting = false;
	let resetError = '';
	let resetSuccess: { newViewRid: string; pushUrl: string } | null = null;

	// Push URL state.
	let currentView: StreamView | null = null;
	let pushUrl = '';
	let pushUrlError = '';
	let copiedKey: 'url' | 'curl' | '' = '';

	const STREAM_TYPES: Array<{ value: StreamType; label: string; hint: string }> = [
		{ value: 'STANDARD', label: 'Standard', hint: 'Low latency, no batching tweaks.' },
		{
			value: 'HIGH_THROUGHPUT',
			label: 'High throughput',
			hint: 'Larger batches; introduces some latency.'
		},
		{
			value: 'COMPRESSED',
			label: 'Compressed',
			hint: 'lz4 compression on producer batches.'
		},
		{
			value: 'HIGH_THROUGHPUT_COMPRESSED',
			label: 'High throughput + compressed',
			hint: 'Both, for high-volume streams with repetitive payloads.'
		}
	];

	async function refresh() {
		loading = true;
		error = '';
		try {
			const [cfg, view] = await Promise.all([
				getStreamConfig(streamId),
				getCurrentStreamView(streamId).catch(() => null)
			]);
			config = cfg;
			if (view) {
				currentView = view;
				pushUrl = buildPushUrl(view.view_rid);
				resetSchemaJson = view.schema_json
					? JSON.stringify(view.schema_json, null, 2)
					: '';
			}
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		} finally {
			loading = false;
		}
	}

	function buildPushUrl(viewRid: string): string {
		const base = typeof window === 'undefined' ? '' : window.location.origin;
		return `${base}/streams-push/${viewRid}/records`;
	}

	async function copyToClipboard(text: string, key: 'url' | 'curl') {
		try {
			await navigator.clipboard.writeText(text);
			copiedKey = key;
			setTimeout(() => (copiedKey = ''), 1500);
		} catch (err) {
			pushUrlError = `Could not copy: ${err instanceof Error ? err.message : String(err)}`;
		}
	}

	function curlSnippet(): string {
		if (!pushUrl) return '';
		return [
			'curl -X POST \\',
			`  "${pushUrl}" \\`,
			'  -H "Authorization: Bearer $ACCESS_TOKEN" \\',
			'  -H "Content-Type: application/json" \\',
			"  -d '[{ \"value\": {\"sensor_id\":\"sensor1\",\"temperature\":4.1} }]'"
		].join('\n');
	}

	function openResetModal() {
		resetError = '';
		resetSuccess = null;
		resetForceReset = false;
		resetConfirmName = '';
		resetModalOpen = true;
	}

	function closeResetModal() {
		resetModalOpen = false;
	}

	$: resetConfirmMatches =
		streamName.length > 0 && resetConfirmName.trim() === streamName.trim();

	async function submitReset() {
		if (!resetConfirmMatches) {
			resetError = 'Type the stream name exactly to confirm.';
			return;
		}
		resetSubmitting = true;
		resetError = '';
		try {
			let parsedSchema: unknown | undefined;
			if (resetSchemaJson.trim().length > 0) {
				try {
					parsedSchema = JSON.parse(resetSchemaJson);
				} catch (parseErr) {
					resetError = `Schema must be valid JSON: ${parseErr instanceof Error ? parseErr.message : String(parseErr)}`;
					resetSubmitting = false;
					return;
				}
			}
			const new_config = config
				? {
						stream_type: config.stream_type,
						compression: config.compression,
						partitions: config.partitions,
						retention_ms: config.retention_ms,
						pipeline_consistency: config.pipeline_consistency,
						checkpoint_interval_ms: config.checkpoint_interval_ms
					}
				: undefined;
			const response = await resetStream(streamId, {
				new_schema: parsedSchema,
				new_config,
				force: resetForceReset
			});
			resetSuccess = {
				newViewRid: response.new_view_rid,
				pushUrl: response.push_url
			};
			pushUrl = response.push_url;
			currentView = response.view;
			savedMessage = `Stream reset: viewRid is now ${response.new_view_rid}`;
			resetConfirmName = '';
		} catch (err) {
			resetError = err instanceof Error ? err.message : String(err);
		} finally {
			resetSubmitting = false;
		}
	}

	async function save() {
		if (!config) return;
		saving = true;
		error = '';
		savedMessage = '';
		try {
			config = await updateStreamConfig(streamId, {
				stream_type: config.stream_type,
				compression: config.compression,
				partitions: config.partitions,
				retention_ms: config.retention_ms,
				pipeline_consistency: config.pipeline_consistency,
				checkpoint_interval_ms: config.checkpoint_interval_ms
				// `ingest_consistency` is forced to AT_LEAST_ONCE per Foundry
				// docs — we omit it from the patch so the server keeps the
				// existing value.
			});
			savedMessage = 'Saved. Pipelines consuming this stream will need redeploy.';
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		} finally {
			saving = false;
		}
	}

	function setStreamType(value: StreamType) {
		if (!config) return;
		config.stream_type = value;
		// Keep the orthogonal `compression` flag in sync with the chosen
		// type so the UI reflects what the producer will actually do.
		config.compression =
			value === 'COMPRESSED' || value === 'HIGH_THROUGHPUT_COMPRESSED';
	}

	function pipelineConsistencyLabel(value: StreamConsistency): string {
		return value === 'EXACTLY_ONCE'
			? 'EXACTLY_ONCE — records visible only after each checkpoint'
			: 'AT_LEAST_ONCE — lower latency, duplicates possible';
	}

	onMount(refresh);
</script>

<section class="stream-settings" data-testid="stream-settings">
	{#if loading}
		<p>Loading stream settings…</p>
	{:else if error && !config}
		<p class="error">{error}</p>
	{:else if config}
		<form on:submit|preventDefault={save}>
			<div class="card">
				<h3>Stream type</h3>
				<p class="hint">
					Tunes the Kafka producer's batching and compression. Only change after
					inspecting stream metrics.
				</p>
				<div class="radio-group">
					{#each STREAM_TYPES as opt}
						<label>
							<input
								type="radio"
								name="stream_type"
								value={opt.value}
								checked={config.stream_type === opt.value}
								on:change={() => setStreamType(opt.value)}
							/>
							<span>
								<strong>{opt.label}</strong>
								<small>{opt.hint}</small>
							</span>
						</label>
					{/each}
				</div>
			</div>

			<div class="card">
				<h3>Partitions</h3>
				<p class="hint">
					Each partition adds ~5 MB/s of throughput. Range 1..50.
				</p>
				<input
					type="range"
					min="1"
					max="50"
					step="1"
					bind:value={config.partitions}
					list="partition-marks"
					data-testid="partitions-slider"
				/>
				<datalist id="partition-marks">
					<option value="1"></option>
					<option value="10"></option>
					<option value="20"></option>
					<option value="30"></option>
					<option value="40"></option>
					<option value="50"></option>
				</datalist>
				<div class="slider-meta">
					<span><strong>{config.partitions}</strong> partition{config.partitions === 1 ? '' : 's'}</span>
					<span title="Estimate based on Foundry's ~5 MB/s per partition heuristic">
						≈ {(config.partitions * 5).toLocaleString()} MB/s ceiling
					</span>
				</div>
			</div>

			<div class="card">
				<h3>Streaming consistency guarantees</h3>
				<p class="hint">
					Streaming sources only support <code>AT_LEAST_ONCE</code> for extracts and
					exports. Streaming pipelines may opt into <code>EXACTLY_ONCE</code>.
				</p>
				<div class="consistency-row">
					<span class="label">Ingest</span>
					<span
						class="locked"
						title="Foundry streaming sources only support AT_LEAST_ONCE for extracts and exports."
					>
						AT_LEAST_ONCE (locked)
					</span>
				</div>
				<div class="consistency-row">
					<span class="label">Pipeline</span>
					<select
						bind:value={config.pipeline_consistency}
						data-testid="pipeline-consistency-select"
					>
						<option value="AT_LEAST_ONCE">AT_LEAST_ONCE</option>
						<option value="EXACTLY_ONCE">EXACTLY_ONCE</option>
					</select>
				</div>
				<small class="hint">
					{pipelineConsistencyLabel(config.pipeline_consistency)}
				</small>
				<div class="consistency-row">
					<span class="label">Checkpoint interval (ms)</span>
					<input
						type="number"
						min="100"
						step="100"
						bind:value={config.checkpoint_interval_ms}
					/>
				</div>
			</div>

			{#if error}
				<p class="error" data-testid="stream-settings-error">{error}</p>
			{/if}
			{#if savedMessage}
				<p class="success" data-testid="stream-settings-saved">{savedMessage}</p>
			{/if}

			<div class="actions">
				<button type="submit" disabled={saving} data-testid="stream-settings-save">
					{saving ? 'Saving…' : 'Save settings'}
				</button>
				<small class="hint">Pipelines consuming this stream will need redeploy.</small>
			</div>
		</form>

		<div class="card" data-testid="push-url-card">
			<h3>Push URL</h3>
			<p class="hint">
				Active POST URL for push consumers. <strong>This URL will rotate on the next reset</strong>;
				clients should refetch when they receive a <code>stream.reset.v1</code> event.
			</p>
			{#if pushUrlError}
				<p class="error">{pushUrlError}</p>
			{/if}
			{#if currentView}
				<div class="push-url-row">
					<code data-testid="push-url-input">{pushUrl}</code>
					<button
						type="button"
						on:click={() => copyToClipboard(pushUrl, 'url')}
						data-testid="copy-push-url"
					>
						{copiedKey === 'url' ? 'Copied!' : 'Copy URL'}
					</button>
				</div>
				<p class="hint">
					viewRid <code>{currentView.view_rid}</code> · generation
					<strong>{currentView.generation}</strong>
				</p>
				<details>
					<summary>Example curl</summary>
					<pre><code>{curlSnippet()}</code></pre>
					<button
						type="button"
						on:click={() => copyToClipboard(curlSnippet(), 'curl')}
					>
						{copiedKey === 'curl' ? 'Copied!' : 'Copy curl example'}
					</button>
				</details>
			{:else}
				<p class="hint">No active view. Reset the stream to mint one.</p>
			{/if}
		</div>

		{#if streamKind === 'INGEST'}
			<div class="card reset-card" data-testid="reset-stream-card">
				<h3>Reset stream</h3>
				<div class="banner banner-danger" role="alert">
					Resetting clears existing records and rotates the viewRid. <strong>This is irreversible.</strong>
				</div>
				<p class="hint">
					Push consumers will get <code>404 PUSH_VIEW_RETIRED</code> against the old URL
					until they re-fetch. Downstream pipelines must be replayed.
				</p>
				<button
					type="button"
					class="danger"
					on:click={openResetModal}
					data-testid="reset-stream-open"
				>
					Reset stream…
				</button>
			</div>
		{/if}
	{/if}
</section>

{#if resetModalOpen}
	<div
		class="modal-backdrop"
		on:click|self={closeResetModal}
		role="presentation"
	>
		<div class="modal" role="dialog" aria-modal="true" aria-labelledby="reset-modal-title">
			<header>
				<h2 id="reset-modal-title">Reset stream</h2>
				<button type="button" on:click={closeResetModal} aria-label="Close">×</button>
			</header>
			<div class="banner banner-danger" role="alert">
				This deletes existing records and rotates the viewRid. Downstream pipelines must replay.
			</div>
			<form on:submit|preventDefault={submitReset} class="reset-form">
				<label>
					<span>New schema (optional, JSON)</span>
					<textarea
						rows="8"
						bind:value={resetSchemaJson}
						placeholder="Leave blank to reuse the current schema"
						data-testid="reset-schema-input"
					></textarea>
				</label>
				<label class="checkbox">
					<input
						type="checkbox"
						bind:checked={resetForceReset}
						data-testid="reset-force-toggle"
					/>
					<span>
						<strong>Force reset</strong>
						<small>
							Required when downstream pipelines are still active. They must be
							replayed against the new viewRid.
						</small>
					</span>
				</label>
				<label>
					<span>
						Type the stream name to confirm
						{#if streamName}
							(<code>{streamName}</code>)
						{/if}
					</span>
					<input
						type="text"
						bind:value={resetConfirmName}
						placeholder={streamName}
						data-testid="reset-confirm-name"
					/>
				</label>
				{#if resetError}
					<p class="error" data-testid="reset-error">{resetError}</p>
				{/if}
				{#if resetSuccess}
					<p class="success" data-testid="reset-success">
						New viewRid: <code>{resetSuccess.newViewRid}</code><br />
						New POST URL: <code>{resetSuccess.pushUrl}</code>
					</p>
				{/if}
				<div class="modal-actions">
					<button type="button" on:click={closeResetModal}>Cancel</button>
					<button
						type="submit"
						class="danger"
						disabled={resetSubmitting || !resetConfirmMatches}
						data-testid="reset-stream-submit"
					>
						{resetSubmitting ? 'Resetting…' : 'Reset stream'}
					</button>
				</div>
			</form>
		</div>
	</div>
{/if}

<style>
	.stream-settings {
		display: grid;
		gap: 1rem;
	}
	.card {
		background: var(--panel, #fff);
		border: 1px solid var(--border, #e5e5e5);
		border-radius: 6px;
		padding: 1rem;
	}
	.card h3 {
		margin: 0 0 0.25rem;
		font-size: 1rem;
	}
	.card p.hint,
	.hint {
		color: #666;
		font-size: 0.85rem;
		margin: 0 0 0.5rem;
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
	.slider-meta {
		display: flex;
		gap: 1rem;
		justify-content: space-between;
		margin-top: 0.4rem;
		font-size: 0.85rem;
	}
	.consistency-row {
		display: grid;
		grid-template-columns: 11rem 1fr;
		align-items: center;
		gap: 0.5rem;
		margin: 0.4rem 0;
	}
	.consistency-row .label {
		font-weight: 600;
	}
	.locked {
		display: inline-block;
		padding: 0.15rem 0.5rem;
		background: #eef;
		border-radius: 3px;
		color: #335;
	}
	.actions {
		display: flex;
		align-items: center;
		gap: 1rem;
	}
	.error {
		color: #b00;
	}
	.success {
		color: #2a7;
	}
	input[type='range'] {
		width: 100%;
	}
	.banner {
		padding: 0.5rem 0.75rem;
		border-radius: 4px;
		margin-bottom: 0.5rem;
	}
	.banner-danger {
		background: #fdecea;
		border-left: 4px solid #b00020;
		color: #720010;
	}
	button.danger {
		background: #b00020;
		color: #fff;
		border: 0;
		border-radius: 4px;
		padding: 0.45rem 0.85rem;
		cursor: pointer;
	}
	button.danger:disabled {
		opacity: 0.55;
		cursor: not-allowed;
	}
	.reset-card {
		border-color: #f1c2c0;
	}
	.push-url-row {
		display: flex;
		gap: 0.5rem;
		align-items: center;
		flex-wrap: wrap;
	}
	.push-url-row code {
		flex: 1 1 12rem;
		padding: 0.25rem 0.5rem;
		background: #f3f3f3;
		border-radius: 3px;
		word-break: break-all;
	}
	.modal-backdrop {
		position: fixed;
		inset: 0;
		background: rgba(0, 0, 0, 0.4);
		display: flex;
		align-items: center;
		justify-content: center;
		z-index: 50;
	}
	.modal {
		background: #fff;
		border-radius: 6px;
		padding: 1rem 1.25rem;
		width: min(540px, 95vw);
		box-shadow: 0 20px 50px rgba(0, 0, 0, 0.2);
	}
	.modal header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 0.5rem;
	}
	.modal header h2 {
		margin: 0;
		font-size: 1.1rem;
	}
	.modal header button {
		background: transparent;
		border: 0;
		font-size: 1.25rem;
		cursor: pointer;
	}
	.reset-form {
		display: grid;
		gap: 0.75rem;
	}
	.reset-form label {
		display: grid;
		gap: 0.25rem;
	}
	.reset-form .checkbox {
		grid-template-columns: auto 1fr;
		align-items: start;
	}
	.modal-actions {
		display: flex;
		justify-content: flex-end;
		gap: 0.5rem;
	}
	pre {
		background: #f6f6f6;
		padding: 0.5rem;
		overflow: auto;
		border-radius: 4px;
	}
</style>
