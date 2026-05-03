<script lang="ts">
	import { onMount } from 'svelte';
	import api from '$lib/api/client';

	interface FieldDescriptor {
		name: string;
		kind: 'string' | 'int' | 'secret';
		required: boolean;
		description: string;
	}

	interface StreamingSourceContract {
		kind: string;
		display_name: string;
		description: string;
		requires_agent: boolean;
		config_fields: FieldDescriptor[];
	}

	let contracts: StreamingSourceContract[] = [];
	let loading = false;
	let error = '';
	let step: 1 | 2 | 3 = 1;
	let selectedKind: string | null = null;
	let formValues = new Map<string, string>();
	let targetStreamRid = '';
	let batchSize = 100;
	let pollIntervalMs = 1_000;
	let schemaInference = false;
	let submitError = '';
	let createdSourceId: string | null = null;

	$: selectedContract = contracts.find((c) => c.kind === selectedKind) ?? null;

	async function refresh() {
		loading = true;
		error = '';
		try {
			const res = await api.get<{ data: StreamingSourceContract[] }>(
				'/data-connection/streaming-sources'
			);
			contracts = res.data;
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		} finally {
			loading = false;
		}
	}

	function pickKind(kind: string) {
		selectedKind = kind;
		formValues = new Map();
		step = 2;
	}

	function setField(name: string, value: string) {
		formValues.set(name, value);
		formValues = new Map(formValues);
	}

	function nextStep() {
		if (step === 2) {
			submitError = '';
			if (!selectedContract) return;
			for (const f of selectedContract.config_fields) {
				if (f.required && !(formValues.get(f.name) ?? '').trim()) {
					submitError = `Field "${f.name}" is required.`;
					return;
				}
			}
		}
		if (step < 3) step = ((step + 1) as 1 | 2 | 3);
	}

	function prevStep() {
		if (step > 1) step = ((step - 1) as 1 | 2 | 3);
	}

	async function submit() {
		if (!selectedContract) return;
		submitError = '';
		try {
			const config: Record<string, string> = {};
			for (const [k, v] of formValues.entries()) config[k] = v;
			const body = {
				name: `${selectedContract.kind}-${Date.now()}`,
				connector_type: selectedContract.kind,
				config: {
					...config,
					streaming_sync: {
						target_stream_rid: targetStreamRid,
						batch_size: batchSize,
						poll_interval_ms: pollIntervalMs,
						schema_inference: schemaInference
					}
				}
			};
			const res = await api.post<{ id: string }>(
				'/data-connection/sources',
				body
			);
			createdSourceId = res.id;
		} catch (err) {
			submitError = err instanceof Error ? err.message : String(err);
		}
	}

	onMount(refresh);
</script>

<section class="streaming-wizard" data-testid="streaming-source-wizard">
	<header>
		<h1>New streaming source</h1>
		<p class="hint">
			Pulls from Kafka, Kinesis, SQS, Google Pub/Sub, Aveva PI or an external Magritte agent
			(ActiveMQ, IBM MQ, RabbitMQ, MQTT, SNS, Solace).
		</p>
	</header>

	{#if loading}<p>Loading…</p>{/if}
	{#if error}<p class="error">{error}</p>{/if}

	{#if step === 1}
		<div class="grid">
			{#each contracts as c (c.kind)}
				<button
					type="button"
					class="card"
					on:click={() => pickKind(c.kind)}
					data-testid={`pick-${c.kind}`}
				>
					<strong>{c.display_name}</strong>
					<small>{c.description}</small>
					{#if c.requires_agent}
						<span class="badge">Requires agent</span>
					{/if}
				</button>
			{/each}
		</div>
	{:else if step === 2 && selectedContract}
		<h2>{selectedContract.display_name}</h2>
		<p class="hint">{selectedContract.description}</p>
		<form>
			{#each selectedContract.config_fields as f}
				<label>
					{f.name}{#if f.required} *{/if}
					<input
						type={f.kind === 'secret' ? 'password' : f.kind === 'int' ? 'number' : 'text'}
						value={formValues.get(f.name) ?? ''}
						on:input={(e) =>
							setField(f.name, (e.currentTarget as HTMLInputElement).value)
						}
						data-testid={`field-${f.name}`}
					/>
					<small class="hint">{f.description}</small>
				</label>
			{/each}
		</form>
		{#if submitError}<p class="error">{submitError}</p>{/if}
		<div class="actions">
			<button type="button" on:click={prevStep}>Back</button>
			<button type="button" on:click={nextStep}>Next</button>
		</div>
	{:else}
		<h2>Streaming sync</h2>
		<form>
			<label>
				Target stream RID *
				<input
					type="text"
					bind:value={targetStreamRid}
					placeholder="ri.streams.main.stream.…"
					data-testid="target-stream-rid"
				/>
			</label>
			<label>
				Batch size
				<input type="number" min="1" max="10000" bind:value={batchSize} />
			</label>
			<label>
				Poll interval (ms)
				<input type="number" min="100" max="60000" bind:value={pollIntervalMs} />
			</label>
			<label class="checkbox">
				<input type="checkbox" bind:checked={schemaInference} />
				Infer schema from first batch
			</label>
		</form>
		{#if submitError}<p class="error">{submitError}</p>{/if}
		{#if createdSourceId}
			<p class="success" data-testid="created-source">
				Source created: <code>{createdSourceId}</code>
			</p>
		{/if}
		<div class="actions">
			<button type="button" on:click={prevStep}>Back</button>
			<button type="button" on:click={submit} data-testid="submit-streaming-source">
				Create streaming source
			</button>
		</div>
	{/if}
</section>

<style>
	.streaming-wizard { padding: 1rem 1.5rem; max-width: 960px; margin: 0 auto; display: grid; gap: 1rem; }
	.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 0.5rem; }
	.card { display: grid; gap: 0.25rem; padding: 0.75rem; border: 1px solid #ddd; border-radius: 6px; background: #fff; cursor: pointer; text-align: left; }
	.card:hover { border-color: #246; }
	.card small { color: #555; }
	.badge { background: #fff3cd; color: #5b4500; padding: 0.05rem 0.3rem; border-radius: 3px; font-size: 0.7rem; align-self: start; }
	form { display: grid; gap: 0.5rem; }
	label { display: grid; gap: 0.25rem; }
	.checkbox { grid-template-columns: auto 1fr; align-items: center; }
	.actions { display: flex; gap: 0.5rem; justify-content: flex-end; }
	.hint { color: #666; font-size: 0.85rem; }
	.error { color: #b00; }
	.success { color: #2a7; }
</style>
