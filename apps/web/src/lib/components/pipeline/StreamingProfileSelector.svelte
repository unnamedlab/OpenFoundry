<script lang="ts">
	import { onMount } from 'svelte';
	import {
		listStreamingProfiles,
		listStreamingProfileProjectRefs,
		listPipelineStreamingProfiles,
		attachProfileToPipeline,
		detachProfileFromPipeline,
		getPipelineEffectiveFlinkConfig,
		type StreamingProfile,
		type EffectiveFlinkConfig
	} from '$lib/api/streaming';

	/**
	 * Pipeline RID this selector is binding to. Required so the
	 * "Advanced" preview can fetch the effective Flink config.
	 */
	export let pipelineRid: string;
	/**
	 * Project RID the pipeline lives in. Used both to attach profiles
	 * (the API requires the project ref) and to filter the picker so
	 * only profiles imported into this project are surfaced.
	 */
	export let projectRid: string;
	/**
	 * Surfaced when the selector should remain read-only — for example
	 * in a preview pane. Defaults to false.
	 */
	export let readonly: boolean = false;

	let allProfiles: StreamingProfile[] = [];
	let attached: StreamingProfile[] = [];
	let availableInProject = new Set<string>();
	let loading = true;
	let error = '';

	let advancedOpen = false;
	let effective: EffectiveFlinkConfig | null = null;
	let effectiveError = '';

	$: attachedIds = new Set(attached.map((p) => p.id));
	$: availableForPicker = allProfiles.filter(
		(p) => availableInProject.has(p.id) && !attachedIds.has(p.id)
	);

	async function refresh() {
		loading = true;
		error = '';
		try {
			const [profilesRes, attachedRes] = await Promise.all([
				listStreamingProfiles({}),
				listPipelineStreamingProfiles(pipelineRid)
			]);
			allProfiles = profilesRes.data;
			attached = attachedRes.data;
			// Filter to profiles that have a ref in the current project.
			const inProject = new Set<string>();
			for (const p of allProfiles) {
				try {
					const refs = await listStreamingProfileProjectRefs(p.id);
					if (refs.data.some((r) => r.project_rid === projectRid)) {
						inProject.add(p.id);
					}
				} catch (err) {
					console.warn('refs load failed', p.id, err);
				}
			}
			availableInProject = inProject;
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		} finally {
			loading = false;
		}
	}

	async function attach(profile: StreamingProfile) {
		try {
			await attachProfileToPipeline(pipelineRid, {
				project_rid: projectRid,
				profile_id: profile.id
			});
			await refresh();
			if (advancedOpen) await loadEffective();
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
	}

	async function detach(profile: StreamingProfile) {
		try {
			await detachProfileFromPipeline(pipelineRid, profile.id);
			await refresh();
			if (advancedOpen) await loadEffective();
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
	}

	async function loadEffective() {
		effectiveError = '';
		try {
			effective = await getPipelineEffectiveFlinkConfig(pipelineRid);
		} catch (err) {
			effectiveError = err instanceof Error ? err.message : String(err);
		}
	}

	function toggleAdvanced() {
		advancedOpen = !advancedOpen;
		if (advancedOpen && !effective) loadEffective();
	}

	onMount(refresh);
</script>

<section class="profile-selector" data-testid="streaming-profile-selector">
	<header>
		<h4>Streaming profiles</h4>
		<p class="hint">
			Profiles override Flink runtime knobs. Only profiles imported into the current Project
			are listed.
		</p>
	</header>

	{#if error}<p class="error">{error}</p>{/if}
	{#if loading}<p>Loading…</p>{/if}

	<div class="lists">
		<div>
			<h5>Attached</h5>
			<ul data-testid="attached-profiles">
				{#each attached as p (p.id)}
					<li>
						<span>
							<strong>{p.name}</strong>
							<small>{p.category} · {p.size_class}</small>
						</span>
						{#if !readonly}
							<button type="button" on:click={() => detach(p)}>Remove</button>
						{/if}
					</li>
				{:else}
					<li class="hint">No profiles attached. Default Foundry config will apply.</li>
				{/each}
			</ul>
		</div>
		<div>
			<h5>Available</h5>
			<ul data-testid="available-profiles">
				{#each availableForPicker as p (p.id)}
					<li>
						<span>
							<strong>{p.name}</strong>
							<small>{p.category} · {p.size_class}</small>
						</span>
						{#if !readonly}
							<button type="button" on:click={() => attach(p)}>Attach</button>
						{/if}
					</li>
				{:else}
					<li class="hint">
						No more profiles imported in this project. Ask an administrator to import one.
					</li>
				{/each}
			</ul>
		</div>
	</div>

	<button
		type="button"
		class="advanced-toggle"
		on:click={toggleAdvanced}
		data-testid="advanced-toggle"
	>
		{advancedOpen ? '▼' : '▶'} Advanced — preview effective Flink config
	</button>
	{#if advancedOpen}
		<div class="advanced">
			{#if effectiveError}<p class="error">{effectiveError}</p>{/if}
			{#if effective}
				<table>
					<thead><tr><th>Key</th><th>Value</th><th>Source profile</th></tr></thead>
					<tbody>
						{#each Object.keys(effective.config) as key}
							<tr>
								<td><code>{key}</code></td>
								<td><code>{effective.config[key]}</code></td>
								<td>{effective.source_map[key] ?? '—'}</td>
							</tr>
						{:else}
							<tr><td colspan="3" class="hint">No keys set — Foundry defaults apply.</td></tr>
						{/each}
					</tbody>
				</table>
				{#if effective.warnings.length > 0}
					<ul class="warnings">
						{#each effective.warnings as w}<li>{w}</li>{/each}
					</ul>
				{/if}
			{/if}
		</div>
	{/if}
</section>

<style>
	.profile-selector header h4 { margin: 0; }
	.hint { color: #666; font-size: 0.85rem; margin: 0.25rem 0 0.5rem; }
	.lists { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; }
	.lists h5 { margin: 0.5rem 0 0.25rem; }
	.lists ul { list-style: none; padding: 0; margin: 0; }
	.lists li {
		display: flex; justify-content: space-between; align-items: center;
		gap: 0.5rem; padding: 0.4rem 0.6rem;
		border: 1px solid #eee; border-radius: 4px; margin-bottom: 0.3rem;
	}
	.lists li small { display: block; color: #666; }
	.advanced-toggle {
		margin-top: 0.75rem; background: none; border: 0; cursor: pointer;
		font-size: 0.9rem; color: #246;
	}
	.advanced { background: #fafafa; padding: 0.5rem 0.75rem; border-radius: 4px; }
	.advanced table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
	.advanced th, .advanced td { padding: 0.25rem 0.4rem; border-bottom: 1px solid #eee; vertical-align: top; }
	.warnings { color: #a60; font-size: 0.8rem; padding-left: 1rem; margin-top: 0.5rem; }
	.error { color: #b00; }
</style>
