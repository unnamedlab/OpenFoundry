<script lang="ts">
  import { onMount } from 'svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import { getMe, type UserProfile } from '$lib/api/auth';
  import {
    bindProjectResource,
    createProjectBranch,
    createProjectMigration,
    createProjectProposal,
    getProjectWorkingState,
    listActionTypes,
    listInterfaces,
    listLinkTypes,
    listProjectBranches,
    listObjectTypes,
    listProjectMigrations,
    listProjectMemberships,
    listProjectProposals,
    listProjectResources,
    listProjects,
    listSharedPropertyTypes,
    unbindProjectResource,
    updateProjectBranch,
    updateProjectProposal,
    type ActionType,
    type LinkType,
    type ObjectType,
    type OntologyBranch,
    type OntologyInterface,
    type OntologyProjectMigration,
    type OntologyProject,
    type OntologyProjectMembership,
    type OntologyProposal,
    type OntologyProposalComment,
    type OntologyProposalTask,
    type OntologyProjectResourceBinding,
    type OntologyStagedChange,
    type SharedPropertyType
  } from '$lib/api/ontology';

  type OntologyTab =
    | 'overview'
    | 'branches'
    | 'proposals'
    | 'preview'
    | 'review'
    | 'changelog'
    | 'migration'
    | 'shared'
    | 'usage';

  type PreviewStatus = 'indexed' | 'in_progress' | 'blocked';
  type ConflictResolution = 'main' | 'branch' | 'custom';

  interface ConflictCandidate {
    key: string;
    title: string;
    description: string;
    resource_label: string;
    updated_at: string;
  }

  interface PreviewRow {
    id: string;
    label: string;
    status: PreviewStatus;
    note: string;
  }

  const tabs: Array<{ id: OntologyTab; label: string; glyph: 'home' | 'run' | 'history' | 'bookmark' | 'folder' | 'link' | 'graph' | 'settings' }> = [
    { id: 'overview', label: 'Overview', glyph: 'home' },
    { id: 'branches', label: 'Branches', glyph: 'run' },
    { id: 'proposals', label: 'Proposals', glyph: 'bookmark' },
    { id: 'preview', label: 'Preview status', glyph: 'graph' },
    { id: 'review', label: 'Review changes', glyph: 'history' },
    { id: 'changelog', label: 'Changelog', glyph: 'history' },
    { id: 'migration', label: 'Migration', glyph: 'link' },
    { id: 'shared', label: 'Shared', glyph: 'folder' },
    { id: 'usage', label: 'Usage', glyph: 'settings' }
  ];

  let loading = $state(true);
  let saving = $state(false);
  let migrating = $state(false);
  let pageError = $state('');
  let pageSuccess = $state('');

  let activeTab = $state<OntologyTab>('overview');
  let projects = $state<OntologyProject[]>([]);
  let projectMembershipMap = $state<Record<string, OntologyProjectMembership[]>>({});
  let projectResourceMap = $state<Record<string, OntologyProjectResourceBinding[]>>({});
  let objectTypes = $state<ObjectType[]>([]);
  let linkTypes = $state<LinkType[]>([]);
  let actionTypes = $state<ActionType[]>([]);
  let interfaces = $state<OntologyInterface[]>([]);
  let sharedPropertyTypes = $state<SharedPropertyType[]>([]);
  let changeQueue = $state<OntologyStagedChange[]>([]);
  let branches = $state<OntologyBranch[]>([]);
  let proposals = $state<OntologyProposal[]>([]);
  let migrations = $state<OntologyProjectMigration[]>([]);
  let currentUser = $state<UserProfile | null>(null);

  let selectedProjectId = $state('');
  let selectedBranchId = $state('');
  let selectedProposalId = $state('');

  let branchName = $state('');
  let branchDescription = $state('');
  let proposalTitle = $state('');
  let proposalDescription = $state('');
  let reviewerId = $state('');
  let commentDraft = $state('');

  let migrationSourceId = $state('');
  let migrationTargetId = $state('');
  let migrationSelection = $state<string[]>([]);

  const objectTypeMap = $derived.by(() => {
    const map = new Map<string, ObjectType>();
    for (const item of objectTypes) map.set(item.id, item);
    return map;
  });

  const linkTypeMap = $derived.by(() => {
    const map = new Map<string, LinkType>();
    for (const item of linkTypes) map.set(item.id, item);
    return map;
  });

  const actionTypeMap = $derived.by(() => {
    const map = new Map<string, ActionType>();
    for (const item of actionTypes) map.set(item.id, item);
    return map;
  });

  const interfaceMap = $derived.by(() => {
    const map = new Map<string, OntologyInterface>();
    for (const item of interfaces) map.set(item.id, item);
    return map;
  });

  const sharedPropertyMap = $derived.by(() => {
    const map = new Map<string, SharedPropertyType>();
    for (const item of sharedPropertyTypes) map.set(item.id, item);
    return map;
  });

  const projectMap = $derived.by(() => {
    const map = new Map<string, OntologyProject>();
    for (const item of projects) map.set(item.id, item);
    return map;
  });

  const changeMap = $derived.by(() => {
    const map = new Map<string, OntologyStagedChange>();
    for (const item of changeQueue) map.set(item.id, item);
    return map;
  });

  const selectedProject = $derived(projects.find((item) => item.id === selectedProjectId) ?? null);
  const branchesForProject = $derived.by(() => branches.filter((item) => item.project_id === selectedProjectId));
  const selectedBranch = $derived(branches.find((item) => item.id === selectedBranchId) ?? null);
  const selectedProposal = $derived(proposals.find((item) => item.id === selectedProposalId) ?? null);
  const selectedProjectMemberships = $derived(projectMembershipMap[selectedProjectId] ?? []);
  const selectedProjectResources = $derived(projectResourceMap[selectedProjectId] ?? []);
  const migrationSourceResources = $derived(projectResourceMap[migrationSourceId] ?? []);
  const openProposals = $derived(proposals.filter((item) => item.status === 'draft' || item.status === 'in_review' || item.status === 'approved'));
  const selectedProjectRole = $derived.by(() => {
    if (!currentUser || !selectedProject) return null;
    if (selectedProject.owner_id === currentUser.id) return 'owner';
    return selectedProjectMemberships.find((item) => item.user_id === currentUser.id)?.role ?? 'viewer';
  });
  const canEditSelectedProject = $derived(
    selectedProjectRole === 'owner' || selectedProjectRole === 'editor'
  );
  const branchChanges = $derived.by(() => {
    if (!selectedBranch) return [];
    return selectedBranch.changes;
  });

  const conflictCandidates = $derived.by(() => {
    if (!selectedBranch) return [];
    const latest = selectedBranch.latest_rebased_at;
    return branchChanges
      .map((change) => {
        if (!change.targetId) return null;
        const candidate = resolveUpdatedResource(change.targetId);
        if (!candidate) return null;
        if (candidate.updated_at <= latest) return null;
        return {
          key: `${change.id}:${change.targetId}`,
          title: change.label,
          description: change.description,
          resource_label: candidate.label,
          updated_at: candidate.updated_at
        } satisfies ConflictCandidate;
      })
      .filter((item): item is ConflictCandidate => Boolean(item));
  });

  const previewRows = $derived.by(() => {
    return branchChanges.map((change) => ({
      id: change.id,
      label: change.label,
      status: change.errors.length > 0 ? 'blocked' : change.warnings.length > 0 ? 'in_progress' : 'indexed',
      note:
        change.errors.length > 0
          ? change.errors.join(' ')
          : change.warnings.length > 0
            ? change.warnings.join(' ')
            : 'Indexed and ready for branch preview.'
    }) satisfies PreviewRow);
  });

  const usageSummary = $derived.by(() => {
    const resources = selectedProjectResources;
    const volume = resources.length;
    const typeCount = resources.filter((item) => item.resource_kind === 'object_type').length;
    const linkCount = resources.filter((item) => item.resource_kind === 'link_type').length;
    const queryCompute = typeCount * 18 + linkCount * 11 + (selectedProjectMemberships.length || 1) * 7;
    const indexingCompute = previewRows.filter((item) => item.status !== 'blocked').length * 13 + typeCount * 9;
    return {
      volume,
      queryCompute,
      indexingCompute
    };
  });

  const sharedOntologies = $derived.by(() =>
    projects.filter((project) => {
      const workspace = project.workspace_slug ?? '';
      const memberCount = projectMembershipMap[project.id]?.length ?? 0;
      return workspace.includes('shared') || memberCount > 1;
    })
  );

  async function loadProjectState(projectId: string) {
    if (!projectId) {
      changeQueue = [];
      branches = [];
      proposals = [];
      migrations = [];
      return;
    }

    const [workingState, memberships, resources, projectBranches, projectProposals, projectMigrations] = await Promise.all([
      getProjectWorkingState(projectId).catch(() => ({ project_id: projectId, changes: [], updated_by: '', updated_at: new Date().toISOString() })),
      listProjectMemberships(projectId).catch(() => []),
      listProjectResources(projectId).catch(() => []),
      listProjectBranches(projectId).catch(() => []),
      listProjectProposals(projectId).catch(() => []),
      listProjectMigrations(projectId).catch(() => [])
    ]);

    changeQueue = workingState.changes;
    branches = projectBranches;
    proposals = projectProposals;
    migrations = projectMigrations;
    projectMembershipMap = { ...projectMembershipMap, [projectId]: memberships };
    projectResourceMap = { ...projectResourceMap, [projectId]: resources };

    if (!projectBranches.some((item) => item.id === selectedBranchId)) {
      selectedBranchId = projectBranches.sort((left, right) => right.updated_at.localeCompare(left.updated_at))[0]?.id ?? '';
    }
    if (!selectedProposalId) {
      selectedProposalId = projectProposals.find((item) => item.branch_id === selectedBranchId)?.id ?? '';
    }
    if (!migrationSourceId) migrationSourceId = projectId;
    if (!migrationTargetId) migrationTargetId = projects.find((item) => item.id !== projectId)?.id ?? projectId;
  }

  function formatDate(value: string) {
    return new Date(value).toLocaleString();
  }

  function labelForProject(projectId: string) {
    return projectMap.get(projectId)?.display_name ?? projectId;
  }

  function resolveResourceLabel(binding: OntologyProjectResourceBinding) {
    if (binding.resource_kind === 'object_type') return objectTypeMap.get(binding.resource_id)?.display_name ?? binding.resource_id;
    if (binding.resource_kind === 'link_type') return linkTypeMap.get(binding.resource_id)?.display_name ?? binding.resource_id;
    if (binding.resource_kind === 'action_type') return actionTypeMap.get(binding.resource_id)?.display_name ?? binding.resource_id;
    if (binding.resource_kind === 'interface') return interfaceMap.get(binding.resource_id)?.display_name ?? binding.resource_id;
    if (binding.resource_kind === 'shared_property_type') return sharedPropertyMap.get(binding.resource_id)?.display_name ?? binding.resource_id;
    return binding.resource_id;
  }

  function resolveUpdatedResource(resourceId: string) {
    const objectType = objectTypeMap.get(resourceId);
    if (objectType) return { label: objectType.display_name, updated_at: objectType.updated_at };
    const linkType = linkTypeMap.get(resourceId);
    if (linkType) return { label: linkType.display_name, updated_at: linkType.updated_at };
    const actionType = actionTypeMap.get(resourceId);
    if (actionType) return { label: actionType.display_name, updated_at: actionType.updated_at };
    const iface = interfaceMap.get(resourceId);
    if (iface) return { label: iface.display_name, updated_at: iface.updated_at };
    const shared = sharedPropertyMap.get(resourceId);
    if (shared) return { label: shared.display_name, updated_at: shared.updated_at };
    const project = projectMap.get(resourceId);
    if (project) return { label: project.display_name, updated_at: project.updated_at };
    return null;
  }

  function syncSelection() {
    if (!selectedProjectId && projects[0]) selectedProjectId = projects[0].id;
  }

  async function loadPage() {
    loading = true;
    pageError = '';

    try {
      const [
        me,
        projectResponse,
        typeResponse,
        linkResponse,
        actionResponse,
        interfaceResponse,
        sharedResponse
      ] = await Promise.all([
        getMe(),
        listProjects({ page: 1, per_page: 200 }),
        listObjectTypes({ page: 1, per_page: 200 }),
        listLinkTypes({ page: 1, per_page: 200 }),
        listActionTypes({ page: 1, per_page: 200 }).catch(() => ({ data: [], total: 0, page: 1, per_page: 200 })),
        listInterfaces({ page: 1, per_page: 200 }).catch(() => ({ data: [], total: 0, page: 1, per_page: 200 })),
        listSharedPropertyTypes({ page: 1, per_page: 200 }).catch(() => ({ data: [], total: 0, page: 1, per_page: 200 }))
      ]);

      currentUser = me;
      projects = projectResponse.data;
      objectTypes = typeResponse.data;
      linkTypes = linkResponse.data;
      actionTypes = actionResponse.data;
      interfaces = interfaceResponse.data;
      sharedPropertyTypes = sharedResponse.data;

      syncSelection();
      if (selectedProjectId) {
        await loadProjectState(selectedProjectId);
      }
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to load Ontologies';
    } finally {
      loading = false;
    }
  }

  async function switchProject(projectId: string) {
    selectedProjectId = projectId;
    selectedBranchId = '';
    selectedProposalId = '';
    await loadProjectState(projectId);
  }

  function switchBranch(branchId: string) {
    selectedBranchId = branchId;
    selectedProposalId = branches.find((item) => item.id === branchId)?.proposal_id ?? '';
    pageSuccess = '';
    pageError = '';
  }

  async function createBranch() {
    if (!selectedProjectId || !canEditSelectedProject) return;
    const trimmedName = branchName.trim().toLowerCase().replace(/[^a-z0-9-]+/g, '-');
    if (!trimmedName) {
      pageError = 'Branch name is required.';
      return;
    }
    try {
      const nextBranch = await createProjectBranch(selectedProjectId, {
        name: trimmedName,
        description: branchDescription.trim() || 'Isolated ontology branch for testing and review.',
        changes: changeQueue,
        enable_indexing: false
      });
      branches = [nextBranch, ...branches];
      selectedBranchId = nextBranch.id;
      branchName = '';
      branchDescription = '';
      pageSuccess = 'Ontology branch created.';
      pageError = '';
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to create ontology branch';
    }
  }

  async function rebaseBranch() {
    if (!selectedBranch || !selectedProjectId || !canEditSelectedProject) return;
    try {
      const updated = await updateProjectBranch(selectedProjectId, selectedBranch.id, {
        status: conflictCandidates.length > 0 ? 'rebasing' : selectedBranch.status === 'main' ? 'main' : 'draft',
        latest_rebased_at: new Date().toISOString()
      });
      branches = branches.map((branch) => branch.id === updated.id ? updated : branch);
      pageSuccess = conflictCandidates.length > 0 ? 'Branch rebased. Resolve the highlighted conflicts before merge.' : 'Branch rebased with Main.';
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to rebase ontology branch';
    }
  }

  async function resolveConflict(key: string, resolution: ConflictResolution) {
    if (!selectedBranch || !selectedProjectId || !canEditSelectedProject) return;
    try {
      const updated = await updateProjectBranch(selectedProjectId, selectedBranch.id, {
        conflict_resolutions: {
          ...selectedBranch.conflict_resolutions,
          [key]: resolution
        }
      });
      branches = branches.map((branch) => branch.id === updated.id ? updated : branch);
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to persist conflict resolution';
    }
  }

  async function createProposal() {
    if (!selectedBranch || !selectedProjectId || !canEditSelectedProject) return;
    const title = proposalTitle.trim() || `${selectedBranch.name} proposal`;
    const taskList: OntologyProposalTask[] = branchChanges.map((change) => ({
      id: `task-${change.id}`,
      change_id: change.id,
      title: change.label,
      description: change.description,
      status: 'pending',
      reviewer_id: null,
      comments: []
    }));

    try {
      const proposal = await createProjectProposal(selectedProjectId, {
        branch_id: selectedBranch.id,
        title,
        description: proposalDescription.trim() || 'Ontology proposal generated from the current branch.',
        tasks: taskList,
        comments: []
      });
      proposals = [proposal, ...proposals];
      branches = branches.map((branch) =>
        branch.id === selectedBranch.id ? { ...branch, status: 'in_review', proposal_id: proposal.id } : branch
      );
      selectedProposalId = proposal.id;
      proposalTitle = '';
      proposalDescription = '';
      pageSuccess = 'Ontology proposal created.';
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to create ontology proposal';
    }
  }

  async function assignReviewer() {
    if (!selectedProposal || !selectedProjectId || !reviewerId || !canEditSelectedProject) return;
    try {
      const updated = await updateProjectProposal(selectedProjectId, selectedProposal.id, {
        reviewer_ids: selectedProposal.reviewer_ids.includes(reviewerId)
          ? selectedProposal.reviewer_ids
          : [...selectedProposal.reviewer_ids, reviewerId]
      });
      proposals = proposals.map((proposal) => proposal.id === updated.id ? updated : proposal);
      pageSuccess = 'Reviewer assigned.';
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to assign reviewer';
    }
  }

  async function setTaskStatus(taskId: string, status: OntologyProposalTask['status']) {
    if (!selectedProposal || !selectedProjectId || !canEditSelectedProject) return;
    try {
      const updated = await updateProjectProposal(selectedProjectId, selectedProposal.id, {
        tasks: selectedProposal.tasks.map((task) => (task.id === taskId ? { ...task, status, reviewer_id: reviewerId || task.reviewer_id } : task))
      });
      proposals = proposals.map((proposal) => proposal.id === updated.id ? updated : proposal);
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to update proposal review task';
    }
  }

  async function addProposalComment() {
    if (!selectedProposal || !selectedProjectId || !commentDraft.trim() || !canEditSelectedProject) return;
    try {
      const updated = await updateProjectProposal(selectedProjectId, selectedProposal.id, {
        comments: [
          {
            id: `comment-${Date.now()}`,
            author: reviewerId || currentUser?.email || 'current-user',
            body: commentDraft.trim(),
            created_at: new Date().toISOString()
          },
          ...selectedProposal.comments
        ] satisfies OntologyProposalComment[]
      });
      proposals = proposals.map((proposal) => proposal.id === updated.id ? updated : proposal);
      commentDraft = '';
      pageSuccess = 'Comment added.';
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to add proposal comment';
    }
  }

  async function mergeProposal() {
    if (!selectedProposal || !selectedBranch || !selectedProjectId || !canEditSelectedProject) return;
    const hasRejected = selectedProposal.tasks.some((task) => task.status === 'rejected');
    const hasPending = selectedProposal.tasks.some((task) => task.status === 'pending');
    const hasBlockingPreview = previewRows.some((item) => item.status === 'blocked');
    const unresolvedConflicts = conflictCandidates.some((conflict) => !selectedBranch.conflict_resolutions[conflict.key]);

    if (hasRejected || hasPending || hasBlockingPreview || unresolvedConflicts) {
      pageError = 'Resolve pending reviews, blocked preview checks, and rebase conflicts before merging.';
      return;
    }

    try {
      const [proposal, branch] = await Promise.all([
        updateProjectProposal(selectedProjectId, selectedProposal.id, { status: 'merged' }),
        updateProjectBranch(selectedProjectId, selectedBranch.id, { status: 'merged' })
      ]);
      proposals = proposals.map((item) => item.id === proposal.id ? proposal : item);
      branches = branches.map((item) => item.id === branch.id ? branch : item);
      pageSuccess = 'Ontology proposal merged into Main.';
      pageError = '';
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to merge ontology proposal';
    }
  }

  function toggleMigrationSelection(resourceId: string) {
    migrationSelection = migrationSelection.includes(resourceId)
      ? migrationSelection.filter((item) => item !== resourceId)
      : [...migrationSelection, resourceId];
  }

  async function submitMigration() {
    if (!migrationSourceId || !migrationTargetId || migrationSourceId === migrationTargetId || migrationSelection.length === 0) {
      pageError = 'Choose different source and target ontologies and select at least one resource.';
      return;
    }

    migrating = true;
    pageError = '';

    const selectedBindings = migrationSourceResources.filter((binding) => migrationSelection.includes(binding.resource_id));

    try {
      for (const binding of selectedBindings) {
        await unbindProjectResource(migrationSourceId, binding.resource_kind, binding.resource_id);
        await bindProjectResource(migrationTargetId, {
          resource_kind: binding.resource_kind,
          resource_id: binding.resource_id
        });
      }

        const migration = await createProjectMigration(migrationSourceId, {
          source_project_id: migrationSourceId,
          target_project_id: migrationTargetId,
          resources: selectedBindings.map((binding) => ({
            resource_kind: binding.resource_kind,
            resource_id: binding.resource_id,
            label: resolveResourceLabel(binding)
          })),
          note: 'Resources migrated between ontologies.'
        });
        migrations = [migration, ...migrations];
        migrationSelection = [];
        await loadProjectState(selectedProjectId);
        pageSuccess = 'Selected resources migrated to the target ontology.';
      } catch (error) {
        pageError = error instanceof Error ? error.message : 'Migration failed';
      } finally {
        migrating = false;
      }
  }

  onMount(() => {
    void loadPage();
  });
</script>

<svelte:head>
  <title>OpenFoundry - Ontologies</title>
</svelte:head>

<div class="mx-auto flex max-w-7xl flex-col gap-6 px-4 py-6">
  <section class="overflow-hidden rounded-[2rem] border border-slate-200 bg-[radial-gradient(circle_at_top_left,_rgba(29,79,145,0.18),_transparent_35%),linear-gradient(135deg,_#fbfcff_0%,_#eef4fb_48%,_#fbfcff_100%)] p-6 shadow-sm">
    <div class="grid gap-6 lg:grid-cols-[1.4fr_1fr]">
      <div class="space-y-4">
        <div class="inline-flex items-center gap-2 rounded-full border border-sky-200 bg-white/80 px-3 py-1 text-xs font-semibold uppercase tracking-[0.24em] text-sky-700">
          <Glyph name="folder" size={14} />
          Define Ontologies / Ontologies
        </div>
        <div class="space-y-3">
          <h1 class="text-3xl font-semibold tracking-tight text-slate-950">Ontologies</h1>
          <p class="max-w-3xl text-sm leading-6 text-slate-600">
            Operate ontology lifecycle as a first-class product: switch ontologies, branch safely, test preview status, review proposals, resolve rebase conflicts, and migrate resources between shared or private ontologies.
          </p>
        </div>
        <div class="flex flex-wrap gap-3 text-xs text-slate-500">
          <a href="/ontology-manager" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-sky-300 hover:text-sky-700">Ontology Manager</a>
          <a href="/object-link-types" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-sky-300 hover:text-sky-700">Object and Link Types</a>
          <a href="/interfaces" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-sky-300 hover:text-sky-700">Interfaces</a>
        </div>
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Ontologies</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{projects.length}</p>
          <p class="mt-1 text-sm text-slate-500">Project-backed ontology spaces.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Branches</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{branchesForProject.length}</p>
          <p class="mt-1 text-sm text-slate-500">Saved isolated branches for the selected ontology.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Open proposals</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{openProposals.length}</p>
          <p class="mt-1 text-sm text-slate-500">Review-ready ontology proposals.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Working edits</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{changeQueue.length}</p>
          <p class="mt-1 text-sm text-slate-500">Queued ontology edits discovered from the manager working state.</p>
        </div>
      </div>
    </div>
  </section>

  {#if pageError}
    <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{pageError}</div>
  {/if}
  {#if pageSuccess}
    <div class="rounded-3xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">{pageSuccess}</div>
  {/if}

  {#if loading}
    <div class="rounded-3xl border border-slate-200 bg-white px-5 py-10 text-center text-sm text-slate-500">
      Loading ontologies and lifecycle state...
    </div>
  {:else}
    <section class="rounded-[2rem] border border-slate-200 bg-white p-4 shadow-sm">
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px_320px]">
        <label class="space-y-2 text-sm text-slate-700">
          <span class="font-medium">Ontology</span>
          <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={selectedProjectId} onchange={(event) => switchProject((event.currentTarget as HTMLSelectElement).value)}>
            {#each projects as project}
              <option value={project.id}>{project.display_name}</option>
            {/each}
          </select>
        </label>
        <label class="space-y-2 text-sm text-slate-700">
          <span class="font-medium">Branch selector</span>
          <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={selectedBranchId} onchange={(event) => switchBranch((event.currentTarget as HTMLSelectElement).value)}>
            {#each branchesForProject as branch}
              <option value={branch.id}>{branch.name} - {branch.status}</option>
            {/each}
          </select>
        </label>
        <div class="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-600">
          <div class="font-medium text-slate-900">{selectedProject?.display_name ?? 'No ontology selected'}</div>
          <div class="mt-1">Workspace: {selectedProject?.workspace_slug ?? 'private'}</div>
          <div class="mt-1">Members: {selectedProjectMemberships.length}</div>
          <div class="mt-1">Role: {selectedProjectRole ?? 'viewer'}</div>
        </div>
      </div>
    </section>

    <section class="rounded-[2rem] border border-slate-200 bg-white p-4 shadow-sm">
      <div class="flex flex-wrap gap-2">
        {#each tabs as tab}
          <button
            class={`inline-flex items-center gap-2 rounded-full px-4 py-2 text-sm font-medium transition ${
              activeTab === tab.id
                ? 'bg-slate-950 text-white'
                : 'border border-slate-200 bg-white text-slate-600 hover:border-slate-300'
            }`}
            onclick={() => activeTab = tab.id}
          >
            <Glyph name={tab.glyph} size={16} />
            {tab.label}
          </button>
        {/each}
      </div>
    </section>

    {#if activeTab === 'overview'}
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="grid gap-4 md:grid-cols-2">
            <div class="rounded-3xl border border-slate-200 p-4">
              <div class="text-sm font-semibold text-slate-900">Branch lifecycle</div>
              <p class="mt-2 text-sm text-slate-600">Branch from Main, snapshot working-state edits, rebase against upstream changes, and keep proposal metadata attached to the branch.</p>
              <div class="mt-4 flex flex-wrap gap-2 text-xs text-slate-500">
                <span class="rounded-full border border-slate-200 bg-slate-50 px-3 py-1">Current branch: {selectedBranch?.name ?? 'main'}</span>
                <span class="rounded-full border border-slate-200 bg-slate-50 px-3 py-1">Status: {selectedBranch?.status ?? 'main'}</span>
              </div>
            </div>
            <div class="rounded-3xl border border-slate-200 p-4">
              <div class="text-sm font-semibold text-slate-900">Proposal overview</div>
              <p class="mt-2 text-sm text-slate-600">Create reviewable ontology proposals with task-level approval, reviewer assignment, preview status, comments, and merge checks.</p>
              <div class="mt-4 flex flex-wrap gap-2 text-xs text-slate-500">
                <span class="rounded-full border border-slate-200 bg-slate-50 px-3 py-1">Proposal: {selectedProposal?.title ?? 'Not created yet'}</span>
                <span class="rounded-full border border-slate-200 bg-slate-50 px-3 py-1">Open tasks: {selectedProposal?.tasks.length ?? 0}</span>
              </div>
            </div>
            <div class="rounded-3xl border border-slate-200 p-4">
              <div class="text-sm font-semibold text-slate-900">Shared ontologies</div>
              <p class="mt-2 text-sm text-slate-600">Project-backed ontologies can behave like private or shared spaces depending on memberships and workspace posture.</p>
              <div class="mt-4 text-sm text-slate-500">{sharedOntologies.length} ontology spaces currently look shared by membership or workspace naming.</div>
            </div>
            <div class="rounded-3xl border border-slate-200 p-4">
              <div class="text-sm font-semibold text-slate-900">Migration operations</div>
              <p class="mt-2 text-sm text-slate-600">Submit migration plans and move bound ontology resources between ontologies using real project resource bindings.</p>
              <div class="mt-4 text-sm text-slate-500">{migrations.length} migration records tracked locally.</div>
            </div>
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Current ontology resources</div>
          <div class="mt-4 space-y-3">
            {#each selectedProjectResources.slice(0, 10) as binding}
              <div class="rounded-2xl border border-slate-200 px-4 py-3">
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">{resolveResourceLabel(binding)}</p>
                    <p class="mt-1 text-xs uppercase tracking-[0.2em] text-slate-500">{binding.resource_kind}</p>
                  </div>
                  <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1 text-[11px] text-slate-600">{formatDate(binding.created_at)}</span>
                </div>
              </div>
            {/each}
          </div>
        </section>
      </div>
    {:else if activeTab === 'branches'}
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="flex items-start justify-between gap-4">
            <div>
              <p class="text-sm font-semibold text-slate-900">Branch catalog</p>
              <p class="mt-1 text-sm text-slate-500">Create isolated ontology branches, snapshot queued changes, and keep rebase metadata attached to the lifecycle.</p>
            </div>
            <button class="rounded-full border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700 disabled:opacity-50" onclick={rebaseBranch} disabled={!selectedBranch || !canEditSelectedProject}>Rebase branch</button>
          </div>
          <div class="mt-4 space-y-3">
            {#each branchesForProject as branch}
              <button class={`w-full rounded-2xl border px-4 py-3 text-left transition ${selectedBranchId === branch.id ? 'border-sky-400 bg-sky-50' : 'border-slate-200 bg-white hover:border-slate-300'}`} onclick={() => switchBranch(branch.id)}>
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">{branch.name}</p>
                    <p class="mt-1 text-sm text-slate-500">{branch.description}</p>
                  </div>
                  <span class="rounded-full border border-slate-200 bg-white px-2 py-1 text-[11px] uppercase tracking-[0.2em] text-slate-500">{branch.status}</span>
                </div>
                <div class="mt-3 flex flex-wrap gap-2 text-[11px] text-slate-500">
                  <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1">changes {branch.changes.length}</span>
                  <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1">rebased {formatDate(branch.latest_rebased_at)}</span>
                </div>
              </button>
            {/each}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-sm font-semibold text-slate-900">Create new branch</p>
          <div class="mt-4 space-y-4">
            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Branch name</span>
              <input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" type="text" bind:value={branchName} placeholder="feature-resource-governance" />
            </label>
            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Description</span>
              <textarea rows="4" class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={branchDescription} placeholder="Describe what this ontology branch is testing."></textarea>
            </label>
            <div class="rounded-3xl border border-slate-200 bg-slate-50 p-4 text-sm text-slate-600">
              This branch will snapshot the current `Ontology Manager` working state with {changeQueue.length} queued edits.
            </div>
            <button class="rounded-full bg-sky-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-sky-500 disabled:bg-sky-300" onclick={createBranch} disabled={!canEditSelectedProject}>Create branch</button>
            {#if !canEditSelectedProject}
              <p class="text-xs text-amber-700">You only have viewer access on this ontology. Switch ontology or request edit access before creating a branch.</p>
            {/if}
          </div>
        </section>
      </div>
    {:else if activeTab === 'proposals'}
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="flex items-start justify-between gap-4">
            <div>
              <p class="text-sm font-semibold text-slate-900">Proposal catalog</p>
              <p class="mt-1 text-sm text-slate-500">Reviewable ontology proposals behave like pull requests on top of branch changes.</p>
            </div>
            {#if selectedProposal}
               <button class="rounded-full bg-slate-950 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:bg-slate-400" onclick={mergeProposal} disabled={!canEditSelectedProject}>Merge proposal</button>
            {/if}
          </div>
          <div class="mt-4 space-y-3">
            {#each proposals.filter((item) => branchesForProject.some((branch) => branch.id === item.branch_id)) as proposal}
              <button class={`w-full rounded-2xl border px-4 py-3 text-left transition ${selectedProposalId === proposal.id ? 'border-sky-400 bg-sky-50' : 'border-slate-200 bg-white hover:border-slate-300'}`} onclick={() => selectedProposalId = proposal.id}>
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">{proposal.title}</p>
                    <p class="mt-1 text-sm text-slate-500">{proposal.description}</p>
                  </div>
                  <span class="rounded-full border border-slate-200 bg-white px-2 py-1 text-[11px] uppercase tracking-[0.2em] text-slate-500">{proposal.status}</span>
                </div>
                <div class="mt-3 flex flex-wrap gap-2 text-[11px] text-slate-500">
                  <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1">tasks {proposal.tasks.length}</span>
                  <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1">reviewers {proposal.reviewer_ids.length}</span>
                </div>
              </button>
            {/each}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-sm font-semibold text-slate-900">Create proposal</p>
          <div class="mt-4 space-y-4">
            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Proposal title</span>
              <input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" type="text" bind:value={proposalTitle} placeholder="Promote review-ready ontology changes" />
            </label>
            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Description</span>
              <textarea rows="4" class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={proposalDescription}></textarea>
            </label>
            <div class="rounded-3xl border border-slate-200 bg-slate-50 p-4 text-sm text-slate-600">
              The proposal will snapshot {branchChanges.length} branch edits into reviewable ontology tasks.
            </div>
             <button class="rounded-full bg-sky-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-sky-500 disabled:bg-sky-300" onclick={createProposal} disabled={!selectedBranch || branchChanges.length === 0 || !canEditSelectedProject}>
               Create proposal
             </button>
          </div>
        </section>
      </div>
    {:else if activeTab === 'preview'}
      <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
        <div class="flex items-start justify-between gap-4">
          <div>
            <p class="text-sm font-semibold text-slate-900">Preview status</p>
            <p class="mt-1 text-sm text-slate-500">Object types and related ontology tasks that are ready, in progress, or blocked for branch preview.</p>
          </div>
          <div class="flex flex-wrap gap-2 text-xs text-slate-500">
            <span class="rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1">indexed {previewRows.filter((item) => item.status === 'indexed').length}</span>
            <span class="rounded-full border border-amber-200 bg-amber-50 px-3 py-1">in progress {previewRows.filter((item) => item.status === 'in_progress').length}</span>
            <span class="rounded-full border border-rose-200 bg-rose-50 px-3 py-1">blocked {previewRows.filter((item) => item.status === 'blocked').length}</span>
          </div>
        </div>
        <div class="mt-4 grid gap-3 xl:grid-cols-2">
          {#each previewRows as row}
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="flex items-start justify-between gap-3">
                <div>
                  <p class="text-sm font-semibold text-slate-900">{row.label}</p>
                  <p class="mt-1 text-sm text-slate-500">{row.note}</p>
                </div>
                <span class={`rounded-full px-2 py-1 text-[11px] uppercase tracking-[0.2em] ${
                  row.status === 'indexed'
                    ? 'border border-emerald-200 bg-emerald-50 text-emerald-700'
                    : row.status === 'in_progress'
                      ? 'border border-amber-200 bg-amber-50 text-amber-700'
                      : 'border border-rose-200 bg-rose-50 text-rose-700'
                }`}>{row.status}</span>
              </div>
            </div>
          {/each}
        </div>
      </section>
    {:else if activeTab === 'review'}
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="flex items-start justify-between gap-4">
            <div>
              <p class="text-sm font-semibold text-slate-900">Review changes</p>
              <p class="mt-1 text-sm text-slate-500">Approve or reject ontology tasks individually, and keep reviewer decisions attached to the proposal.</p>
            </div>
          </div>
          {#if selectedProposal}
            <div class="mt-4 space-y-3">
              {#each selectedProposal.tasks as task}
                <div class="rounded-2xl border border-slate-200 p-4">
                  <div class="flex items-start justify-between gap-3">
                    <div>
                      <p class="text-sm font-semibold text-slate-900">{task.title}</p>
                      <p class="mt-1 text-sm text-slate-500">{task.description}</p>
                    </div>
                    <span class={`rounded-full px-2 py-1 text-[11px] uppercase tracking-[0.2em] ${
                      task.status === 'approved'
                        ? 'border border-emerald-200 bg-emerald-50 text-emerald-700'
                        : task.status === 'rejected'
                          ? 'border border-rose-200 bg-rose-50 text-rose-700'
                          : 'border border-slate-200 bg-slate-50 text-slate-600'
                    }`}>{task.status}</span>
                  </div>
                  <div class="mt-4 flex flex-wrap gap-2">
                    <button class="rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-xs font-medium text-emerald-700 hover:border-emerald-300 disabled:opacity-50" onclick={() => setTaskStatus(task.id, 'approved')} disabled={!canEditSelectedProject}>Approve</button>
                    <button class="rounded-full border border-rose-200 bg-rose-50 px-3 py-1.5 text-xs font-medium text-rose-700 hover:border-rose-300 disabled:opacity-50" onclick={() => setTaskStatus(task.id, 'rejected')} disabled={!canEditSelectedProject}>Reject</button>
                  </div>
                </div>
              {/each}
            </div>
          {:else}
            <div class="mt-4 rounded-3xl border border-dashed border-slate-300 px-4 py-8 text-center text-sm text-slate-500">Create or select a proposal to review task-level changes.</div>
          {/if}
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-sm font-semibold text-slate-900">Reviewers and comments</p>
          <div class="mt-4 space-y-4">
            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Assign reviewer</span>
              <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={reviewerId}>
                <option value="">Select project member</option>
                {#each selectedProjectMemberships as membership}
                  <option value={membership.user_id}>{membership.user_id} - {membership.role}</option>
                {/each}
              </select>
            </label>
            <button class="rounded-full border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700 disabled:opacity-50" onclick={assignReviewer} disabled={!canEditSelectedProject}>Assign reviewer</button>
            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Comment</span>
              <textarea rows="5" class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={commentDraft}></textarea>
            </label>
            <button class="rounded-full bg-sky-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-sky-500 disabled:bg-sky-300" onclick={addProposalComment} disabled={!canEditSelectedProject}>Add comment</button>
            {#if selectedProposal}
              <div class="space-y-3">
                {#each selectedProposal.comments as comment}
                  <div class="rounded-2xl border border-slate-200 p-3">
                    <div class="text-xs uppercase tracking-[0.2em] text-slate-500">{comment.author}</div>
                    <p class="mt-2 text-sm text-slate-700">{comment.body}</p>
                    <div class="mt-2 text-xs text-slate-500">{formatDate(comment.created_at)}</div>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        </section>
      </div>
    {:else if activeTab === 'changelog'}
      <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
        <div class="text-sm font-semibold text-slate-900">Changelog</div>
        <div class="mt-4 space-y-3">
          {#each branchChanges as change}
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="flex items-start justify-between gap-3">
                <div>
                  <p class="text-sm font-semibold text-slate-900">{change.label}</p>
                  <p class="mt-1 text-sm text-slate-500">{change.description}</p>
                </div>
                <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1 text-[11px] uppercase tracking-[0.2em] text-slate-500">{change.action}</span>
              </div>
              <div class="mt-3 text-xs text-slate-500">{formatDate(change.createdAt)}</div>
            </div>
          {/each}
          {#if selectedProposal}
            {#each selectedProposal.comments as comment}
              <div class="rounded-2xl border border-dashed border-slate-300 p-4">
                <div class="text-xs uppercase tracking-[0.2em] text-slate-500">comment by {comment.author}</div>
                <p class="mt-2 text-sm text-slate-700">{comment.body}</p>
                <div class="mt-2 text-xs text-slate-500">{formatDate(comment.created_at)}</div>
              </div>
            {/each}
          {/if}
        </div>
      </section>
    {:else if activeTab === 'migration'}
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Migrate ontological resources between ontologies</div>
          <div class="mt-4 grid gap-4 md:grid-cols-2">
            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Source ontology</span>
              <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={migrationSourceId}>
                {#each projects as project}
                  <option value={project.id}>{project.display_name}</option>
                {/each}
              </select>
            </label>
            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Target ontology</span>
              <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={migrationTargetId}>
                {#each projects as project}
                  <option value={project.id}>{project.display_name}</option>
                {/each}
              </select>
            </label>
          </div>
          <div class="mt-4 grid gap-3">
            {#each migrationSourceResources as binding}
              <label class="flex items-start gap-3 rounded-2xl border border-slate-200 px-4 py-3 text-sm text-slate-700">
                <input type="checkbox" checked={migrationSelection.includes(binding.resource_id)} onchange={() => toggleMigrationSelection(binding.resource_id)} />
                <span class="flex-1">
                  <span class="block font-medium text-slate-900">{resolveResourceLabel(binding)}</span>
                  <span class="mt-1 block text-xs uppercase tracking-[0.2em] text-slate-500">{binding.resource_kind}</span>
                </span>
              </label>
            {/each}
          </div>
          <div class="mt-4 flex flex-wrap gap-3">
            <button class="rounded-full bg-sky-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-sky-500 disabled:bg-sky-300" onclick={() => void submitMigration()} disabled={migrating || !canEditSelectedProject}>
              {migrating ? 'Submitting...' : 'Submit migration'}
            </button>
            <div class="rounded-full border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-500">
              Selected resources: {migrationSelection.length}
            </div>
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Migration history</div>
          <div class="mt-4 space-y-3">
            {#each migrations.slice(0, 8) as migration}
              <div class="rounded-2xl border border-slate-200 p-4">
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">{labelForProject(migration.source_project_id)} -> {labelForProject(migration.target_project_id)}</p>
                    <p class="mt-1 text-sm text-slate-500">{migration.note}</p>
                  </div>
                  <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1 text-[11px] uppercase tracking-[0.2em] text-slate-500">{migration.status}</span>
                </div>
                <div class="mt-3 flex flex-wrap gap-2">
                  {#each migration.resources as resource}
                    <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1 text-[11px] text-slate-600">{resource.label}</span>
                  {/each}
                </div>
              </div>
            {/each}
          </div>
        </section>
      </div>
    {:else if activeTab === 'shared'}
      <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
        <div class="text-sm font-semibold text-slate-900">Shared ontologies</div>
        <div class="mt-4 grid gap-3 xl:grid-cols-2">
          {#each sharedOntologies as ontology}
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="flex items-start justify-between gap-3">
                <div>
                  <p class="text-sm font-semibold text-slate-900">{ontology.display_name}</p>
                  <p class="mt-1 text-sm text-slate-500">{ontology.description || 'Shared workspace-backed ontology.'}</p>
                </div>
                <span class="rounded-full border border-sky-200 bg-sky-50 px-2 py-1 text-[11px] uppercase tracking-[0.2em] text-sky-700">
                  shared
                </span>
              </div>
              <div class="mt-4 grid gap-2 text-sm text-slate-600">
                <div>Workspace: {ontology.workspace_slug ?? 'shared-space'}</div>
                <div>Members: {projectMembershipMap[ontology.id]?.length ?? 0}</div>
                <div>Resources: {projectResourceMap[ontology.id]?.length ?? 0}</div>
              </div>
            </div>
          {/each}
        </div>
      </section>
    {:else if activeTab === 'usage'}
      <div class="grid gap-4 md:grid-cols-3">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-xs uppercase tracking-[0.24em] text-slate-400">Ontology volume</div>
          <div class="mt-2 text-3xl font-semibold text-slate-950">{usageSummary.volume}</div>
          <p class="mt-2 text-sm text-slate-500">Bound ontology resources in the selected ontology.</p>
        </section>
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-xs uppercase tracking-[0.24em] text-slate-400">Query compute</div>
          <div class="mt-2 text-3xl font-semibold text-slate-950">{usageSummary.queryCompute}</div>
          <p class="mt-2 text-sm text-slate-500">Approximate operational pressure based on resource mix and memberships.</p>
        </section>
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-xs uppercase tracking-[0.24em] text-slate-400">Indexing compute</div>
          <div class="mt-2 text-3xl font-semibold text-slate-950">{usageSummary.indexingCompute}</div>
          <p class="mt-2 text-sm text-slate-500">Branch preview and indexing effort suggested by current branch work.</p>
        </section>
      </div>
    {/if}

    {#if activeTab === 'branches' || activeTab === 'preview'}
      <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
        <div class="text-sm font-semibold text-slate-900">Main branch updates and conflicts</div>
        <div class="mt-4 grid gap-3 xl:grid-cols-2">
          {#each conflictCandidates as conflict}
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="flex items-start justify-between gap-3">
                <div>
                  <p class="text-sm font-semibold text-slate-900">{conflict.resource_label}</p>
                  <p class="mt-1 text-sm text-slate-500">{conflict.description}</p>
                </div>
                <span class="rounded-full border border-amber-200 bg-amber-50 px-2 py-1 text-[11px] uppercase tracking-[0.2em] text-amber-700">conflict</span>
              </div>
              <div class="mt-3 text-xs text-slate-500">Updated on Main: {formatDate(conflict.updated_at)}</div>
              <div class="mt-4 flex flex-wrap gap-2">
                <button class="rounded-full border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700" onclick={() => resolveConflict(conflict.key, 'main')}>Use Main</button>
                <button class="rounded-full border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700" onclick={() => resolveConflict(conflict.key, 'branch')}>Keep branch</button>
                <button class="rounded-full border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700" onclick={() => resolveConflict(conflict.key, 'custom')}>Custom</button>
              </div>
            </div>
          {/each}
          {#if conflictCandidates.length === 0}
            <div class="rounded-2xl border border-dashed border-slate-300 px-4 py-10 text-center text-sm text-slate-500">
              No incoming Main branch conflicts detected for the current branch snapshot.
            </div>
          {/if}
        </div>
      </section>
    {/if}
  {/if}
</div>
