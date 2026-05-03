<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import {
		listStreamingProfiles,
		createStreamingProfile,
		patchStreamingProfile,
		listStreamingProfileProjectRefs,
		importStreamingProfileToProject,
		removeStreamingProfileFromProject,
		type StreamingProfile,
		type StreamingProfileProjectRef,
		type ProfileCategory,
		type ProfileSizeClass
	} from '$lib/api/streaming';
	import { auth } from '$stores/auth';
	import type { UserProfile } from '$lib/api/auth';

	const CATEGORIES: ProfileCategory[] = [
		'TASKMANAGER_RESOURCES',
		'JOBMANAGER_RESOURCES',
		'PARALLELISM',
		'NETWORK',
		'CHECKPOINTING',
		'ADVANCED'
	];
	const SIZE_CLASSES: ProfileSizeClass[] = ['SMALL', 'MEDIUM', 'LARGE'];

	let profiles: StreamingProfile[] = [];
	let refsByProfile = new Map<string, StreamingProfileProjectRef[]>();
	let loading = false;
	let error = '';

	let categoryFilter: ProfileCategory | '' = '';
	let sizeFilter: ProfileSizeClass | '' = '';

	let importDialogOpen = false;
	let importTargetProfile: StreamingProfile | null = null;
	let importProjectRid = '';
	let importError = '';

	let createOpen = false;
	let newProfile = {
		name: '',
		description: '',
		category: 'TASKMANAGER_RESOURCES' as ProfileCategory,
		size_class: 'SMALL' as ProfileSizeClass,
		restricted: false,
		configJson: '{\n  "taskmanager.numberOfTaskSlots": "2"\n}'
	};
	let createError = '';

	let currentUser: UserProfile | null = null;
	const userUnsubscribe = auth.user.subscribe((u) => (currentUser = u));
	onDestroy(userUnsubscribe);

	$: roles = ((currentUser as UserProfile | null)?.roles ?? []) as string[];
	$: isEnrollmentAdmin =
		roles.includes('admin') || roles.includes('enrollment_resource_administrator');
	$: isStreamingAdmin =
		roles.includes('admin') || roles.includes('streaming_admin');

	async function refresh() {
		loading = true;
		error = '';
		try {
			const res = await listStreamingProfiles({
				category: categoryFilter || undefined,
				size_class: sizeFilter || undefined
			});
			profiles = res.data;
			const refMap = new Map<string, StreamingProfileProjectRef[]>();
			for (const p of profiles) {
				try {
					const refs = await listStreamingProfileProjectRefs(p.id);
					refMap.set(p.id, refs.data);
				} catch (err) {
					refMap.set(p.id, []);
					console.warn('failed to load refs for profile', p.id, err);
				}
			}
			refsByProfile = refMap;
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		} finally {
			loading = false;
		}
	}

	function openImportDialog(profile: StreamingProfile) {
		importTargetProfile = profile;
		importProjectRid = '';
		importError = '';
		importDialogOpen = true;
	}

	async function submitImport() {
		if (!importTargetProfile) return;
		if (!importProjectRid.trim()) {
			importError = 'Project RID is required.';
			return;
		}
		try {
			await importStreamingProfileToProject(
				importProjectRid.trim(),
				importTargetProfile.id
			);
			importDialogOpen = false;
			await refresh();
		} catch (err) {
			importError = err instanceof Error ? err.message : String(err);
		}
	}

	async function removeFromProject(profile: StreamingProfile, projectRid: string) {
		const ok = confirm(
			`Remove '${profile.name}' from project ${projectRid}? This will break any pipeline in that project that uses this profile.`
		);
		if (!ok) return;
		try {
			await removeStreamingProfileFromProject(projectRid, profile.id);
			await refresh();
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
	}

	async function submitCreate() {
		createError = '';
		try {
			const config_json = JSON.parse(newProfile.configJson);
			await createStreamingProfile({
				name: newProfile.name.trim(),
				description: newProfile.description,
				category: newProfile.category,
				size_class: newProfile.size_class,
				restricted: newProfile.restricted,
				config_json
			});
			createOpen = false;
			newProfile.name = '';
			newProfile.description = '';
			await refresh();
		} catch (err) {
			createError = err instanceof Error ? err.message : String(err);
		}
	}

	async function toggleRestricted(profile: StreamingProfile) {
		try {
			await patchStreamingProfile(profile.id, { restricted: !profile.restricted });
			await refresh();
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
	}

	onMount(refresh);
</script>

<section class="streaming-profiles" data-testid="streaming-profiles-page">
	<header>
		<h1>Streaming profiles</h1>
		<p class="hint">
			Profiles override Flink configuration knobs for streaming pipelines. Profiles must be
			imported into the same Project as the pipeline that uses them.
		</p>
		{#if !isEnrollmentAdmin}
			<div class="banner banner-info" role="note">
				<strong>Restricted profiles</strong> require the
				<code>Enrollment Resource Administrator</code> role to be imported. Ask an admin if
				you need a LARGE profile in your project.
			</div>
		{/if}
	</header>

	<div class="toolbar">
		<label>
			Category
			<select bind:value={categoryFilter} on:change={refresh}>
				<option value="">All</option>
				{#each CATEGORIES as c}<option value={c}>{c}</option>{/each}
			</select>
		</label>
		<label>
			Size
			<select bind:value={sizeFilter} on:change={refresh}>
				<option value="">All</option>
				{#each SIZE_CLASSES as s}<option value={s}>{s}</option>{/each}
			</select>
		</label>
		{#if isStreamingAdmin}
			<button
				type="button"
				on:click={() => (createOpen = true)}
				data-testid="create-profile-open"
			>+ Create profile</button>
		{/if}
	</div>

	{#if error}<p class="error">{error}</p>{/if}
	{#if loading}<p>Loading profiles…</p>{/if}

	<table data-testid="profiles-table">
		<thead>
			<tr>
				<th>Name</th>
				<th>Category</th>
				<th>Size</th>
				<th>Restricted</th>
				<th>Projects</th>
				<th>Actions</th>
			</tr>
		</thead>
		<tbody>
			{#each profiles as p (p.id)}
				<tr>
					<td>
						<strong>{p.name}</strong>
						<small class="block">{p.description}</small>
					</td>
					<td><code>{p.category}</code></td>
					<td>{p.size_class}</td>
					<td>
						{#if p.restricted}
							<span class="badge restricted" data-testid={`restricted-${p.id}`}>restricted</span>
						{:else}
							<span class="badge">open</span>
						{/if}
					</td>
					<td>
						<ul class="ref-list">
							{#each refsByProfile.get(p.id) ?? [] as ref}
								<li>
									<code>{ref.project_rid}</code>
									<button
										type="button"
										class="link-danger"
										on:click={() => removeFromProject(p, ref.project_rid)}
										title="Removing breaks pipelines in this project that use the profile"
									>remove</button>
								</li>
							{:else}
								<li class="hint">— not imported anywhere —</li>
							{/each}
						</ul>
					</td>
					<td class="actions">
						<button
							type="button"
							on:click={() => openImportDialog(p)}
							data-testid={`import-${p.id}`}
							disabled={p.restricted && !isEnrollmentAdmin}
							title={p.restricted && !isEnrollmentAdmin
								? 'Restricted profile: Enrollment Resource Administrator required'
								: 'Import this profile into a project'}
						>Import to project…</button>
						{#if isStreamingAdmin}
							<button type="button" on:click={() => toggleRestricted(p)}>
								{p.restricted ? 'Unrestrict' : 'Restrict'}
							</button>
						{/if}
					</td>
				</tr>
			{:else}
				<tr><td colspan="6">No profiles match the current filters.</td></tr>
			{/each}
		</tbody>
	</table>
</section>

{#if importDialogOpen && importTargetProfile}
	<div class="modal-backdrop" role="presentation" on:click|self={() => (importDialogOpen = false)}>
		<div class="modal" role="dialog" aria-modal="true" aria-labelledby="import-title">
			<header>
				<h2 id="import-title">Import "{importTargetProfile.name}"</h2>
				<button on:click={() => (importDialogOpen = false)} aria-label="Close">×</button>
			</header>
			{#if importTargetProfile.restricted}
				<p class="banner banner-danger" role="alert">
					This is a restricted profile. Importing requires the
					<code>Enrollment Resource Administrator</code> role.
				</p>
			{/if}
			<label>
				Project RID
				<input
					type="text"
					bind:value={importProjectRid}
					placeholder="ri.compass.main.project.…"
					data-testid="import-project-rid"
				/>
			</label>
			{#if importError}<p class="error" data-testid="import-error">{importError}</p>{/if}
			<div class="modal-actions">
				<button type="button" on:click={() => (importDialogOpen = false)}>Cancel</button>
				<button type="button" on:click={submitImport} data-testid="import-submit">Import</button>
			</div>
		</div>
	</div>
{/if}

{#if createOpen}
	<div class="modal-backdrop" role="presentation" on:click|self={() => (createOpen = false)}>
		<div class="modal" role="dialog" aria-modal="true" aria-labelledby="create-title">
			<header>
				<h2 id="create-title">Create streaming profile</h2>
				<button on:click={() => (createOpen = false)} aria-label="Close">×</button>
			</header>
			<label>
				Name
				<input bind:value={newProfile.name} required />
			</label>
			<label>
				Description
				<input bind:value={newProfile.description} />
			</label>
			<label>
				Category
				<select bind:value={newProfile.category}>
					{#each CATEGORIES as c}<option value={c}>{c}</option>{/each}
				</select>
			</label>
			<label>
				Size class
				<select bind:value={newProfile.size_class}>
					{#each SIZE_CLASSES as s}<option value={s}>{s}</option>{/each}
				</select>
			</label>
			<label class="checkbox">
				<input type="checkbox" bind:checked={newProfile.restricted} />
				Restricted (Enrollment Resource Admin only)
			</label>
			<label>
				Flink config JSON
				<textarea rows="6" bind:value={newProfile.configJson}></textarea>
			</label>
			{#if createError}<p class="error">{createError}</p>{/if}
			<div class="modal-actions">
				<button type="button" on:click={() => (createOpen = false)}>Cancel</button>
				<button type="button" on:click={submitCreate}>Create</button>
			</div>
		</div>
	</div>
{/if}

<style>
	.streaming-profiles { padding: 1rem 1.5rem; max-width: 1200px; margin: 0 auto; }
	.toolbar { display: flex; gap: 1rem; align-items: end; margin: 1rem 0; }
	.toolbar label { display: flex; flex-direction: column; gap: 0.2rem; font-size: 0.85rem; }
	.banner { padding: 0.5rem 0.75rem; border-radius: 4px; margin: 0.5rem 0; }
	.banner-info { background: #eef5ff; border-left: 4px solid #2a6df0; color: #1a3a8a; }
	.banner-danger { background: #fdecea; border-left: 4px solid #b00020; color: #720010; }
	table { width: 100%; border-collapse: collapse; }
	th, td { text-align: left; padding: 0.4rem 0.6rem; border-bottom: 1px solid #eee; vertical-align: top; }
	.badge { background: #eef; padding: 0.1rem 0.5rem; border-radius: 3px; font-size: 0.75rem; }
	.badge.restricted { background: #fde7e9; color: #720010; font-weight: 600; }
	.ref-list { margin: 0; padding-left: 1rem; }
	.ref-list li { font-size: 0.85rem; }
	.link-danger { background: none; border: 0; color: #b00020; cursor: pointer; padding: 0; margin-left: 0.4rem; }
	.actions { display: flex; gap: 0.3rem; flex-wrap: wrap; }
	.actions button { font-size: 0.8rem; padding: 0.2rem 0.45rem; }
	.error { color: #b00; }
	.hint { color: #666; }
	.block { display: block; color: #777; }
	.modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.4); display: flex; align-items: center; justify-content: center; z-index: 50; }
	.modal { background: #fff; border-radius: 6px; padding: 1rem 1.25rem; width: min(540px, 95vw); display: grid; gap: 0.75rem; }
	.modal header { display: flex; justify-content: space-between; align-items: center; }
	.modal header h2 { margin: 0; font-size: 1.1rem; }
	.modal label { display: grid; gap: 0.25rem; }
	.modal .checkbox { grid-template-columns: auto 1fr; align-items: center; }
	.modal-actions { display: flex; justify-content: flex-end; gap: 0.5rem; }
	textarea { font-family: ui-monospace, monospace; }
</style>
