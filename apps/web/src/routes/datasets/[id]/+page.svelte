<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import { ApiError } from '$lib/api/client';
  import {
    getMe,
    listPermissions,
    listUsers,
    type PermissionRecord,
    type UserProfile,
  } from '$lib/api/auth';
  import {
    getDatasetColumnLineage,
    getDatasetLineage,
    getDatasetLineageImpact,
    triggerLineageBuilds,
    type ColumnLineageEdge,
    type LineageGraph,
    type LineageImpactAnalysis,
    type LineageImpactItem,
  } from '$lib/api/pipelines';
  import {
    checkoutDatasetBranch,
    createDatasetBranch,
    deleteDataset,
    getDataset,
    getDatasetLint,
    getDatasetQuality,
    getDatasetSchema,
    getRetentionPreview,
    getViewSchema,
    getVersions,
    listBranches,
    loadJobSpecStatus,
    listDatasetFilesystem,
    listDatasetTransactions,
    previewDataset,
    refreshDatasetQualityProfile,
    updateDataset,
    uploadData,
    type Dataset,
    type DatasetBranch,
    type DatasetJobSpecStatus,
    type DatasetColumnProfile,
    type DatasetFilesystemEntry,
    type DatasetLintResponse,
    type DatasetPreviewResponse,
    type DatasetQualityResponse,
    type DatasetSchema,
    type DatasetSchemaResponse,
    type DatasetTransaction,
    type RetentionPreviewResponse,
    type DatasetVersion,
  } from '$lib/api/datasets';
  import CompareTab, { type CompareSide } from '$lib/components/dataset/CompareTab.svelte';
  import DatasetHeader from '$lib/components/dataset/DatasetHeader.svelte';
  import OpenTransactionBanner from '$lib/components/dataset/OpenTransactionBanner.svelte';
  import HistoryTimeline from '$lib/components/dataset/HistoryTimeline.svelte';
  import VirtualizedPreviewTable, {
    type ColumnDef as PreviewColumnDef,
    type ColumnStats as PreviewColumnStats,
  } from '$lib/components/dataset/VirtualizedPreviewTable.svelte';
  import CurrentTransactionViewPanel from '$lib/components/dataset/details/CurrentTransactionViewPanel.svelte';
  import FilesPanel from '$lib/components/dataset/details/FilesPanel.svelte';
  import ResourceUsagePanel from '$lib/components/dataset/details/ResourceUsagePanel.svelte';
  import SchemaPanel, { type SchemaField } from '$lib/components/dataset/details/SchemaPanel.svelte';
  import SchemaViewer from '$lib/components/dataset/SchemaViewer.svelte';
  import FilesTab from '$lib/components/dataset/details/FilesTab.svelte';
  import StorageDetailsTab from '$lib/components/dataset/details/StorageDetailsTab.svelte';
  import RetentionPoliciesTab from '$lib/components/dataset/RetentionPoliciesTab.svelte';
  import PublishToMarketplaceModal from '$lib/components/dataset/PublishToMarketplaceModal.svelte';
  import QualityDashboard from '$lib/components/dataset/QualityDashboard.svelte';

  type Tab =
    | 'preview'
    | 'schema'
    | 'files'
    | 'storage'
    | 'retention'
    | 'lineage'
    | 'history'
    | 'permissions'
    | 'details'
    | 'health'
    | 'metrics'
    | 'compare';

  type CompareSelector = { kind: 'transaction' | 'branch'; value: string };
  type Area = Tab | 'dataset' | 'branches' | 'files' | 'quality' | 'lint' | 'metadata' | 'upload' | 'build';

  // P3 — `Files` is always visible; `Storage` is gated below by
  // `hasDatasetPermission('admin')` since it surfaces backend creds /
  // bucket info that only manage roles should see.
  const tabs = $derived<Array<{ key: Tab; label: string }>>([
    { key: 'preview', label: 'Preview' },
    { key: 'schema', label: 'Schema' },
    { key: 'files', label: 'Files' },
    // P4 — Retention sits next to Files because the doc puts the two
    // surfaces side-by-side under "View retention policies for a
    // dataset [Beta]". Visible to every user; manage-role gated CRUD
    // happens *inside* the tab.
    { key: 'retention', label: 'Retention' },
    ...(hasDatasetPermission('admin') ? [{ key: 'storage' as Tab, label: 'Storage' }] : []),
    { key: 'lineage', label: 'Lineage' },
    { key: 'history', label: 'History' },
    { key: 'permissions', label: 'Permissions' },
    { key: 'details', label: 'Details' },
    { key: 'health', label: 'Health' },
    { key: 'metrics', label: 'Metrics' },
    { key: 'compare', label: 'Compare' },
  ]);

  const datasetId = $derived($page.params.id);

  let loadedDatasetId = $state('');
  let activeTab = $state<Tab>('preview');

  let dataset = $state<Dataset | null>(null);
  let versions = $state<DatasetVersion[]>([]);
  let branches = $state<DatasetBranch[]>([]);
  let jobSpecStatus = $state<DatasetJobSpecStatus | null>(null);
  let users = $state<UserProfile[]>([]);
  let currentUser = $state<UserProfile | null>(null);
  let permissionRecords = $state<PermissionRecord[]>([]);
  let quality = $state<DatasetQualityResponse | null>(null);
  let lint = $state<DatasetLintResponse | null>(null);
  let datasetSchema = $state<DatasetSchema | null>(null);
  // T6.x — view-scoped Foundry schema (Datasets.md § "Schemas"). When the
  // backend exposes the new `/views/{view_id}/schema` endpoint we use the
  // typed payload to drive the SchemaViewer editor; otherwise we fall
  // back to the legacy quality-derived `schemaFields`.
  let viewSchema = $state<DatasetSchemaResponse | null>(null);
  let schemaEditMode = $state(false);
  // P4 — preview is fetched lazily on first dataset load + refreshed
  // when the page returns to it. Surfaces the cross-link badges on
  // FilesTab and HistoryTimeline; failures are non-fatal.
  let retentionPreview = $state<RetentionPreviewResponse | null>(null);
  let previewResponse = $state<DatasetPreviewResponse | null>(null);
  let filesystemEntries = $state<DatasetFilesystemEntry[]>([]);
  let transactions = $state<DatasetTransaction[]>([]);
  let lineageGraph = $state<LineageGraph | null>(null);
  let columnLineage = $state<ColumnLineageEdge[]>([]);
  let lineageImpact = $state<LineageImpactAnalysis | null>(null);

  let loading = $state(true);
  let previewLoading = $state(false);
  let schemaLoading = $state(false);
  let lineageLoading = $state(false);
  let filesLoading = $state(false);
  let transactionsLoading = $state(false);
  let qualityLoading = $state(false);
  let compareLoading = $state(false);
  let uploadLoading = $state(false);
  let savingMetadata = $state(false);
  // P5 — controlled visibility of the Publish-to-Marketplace modal.
  let publishModalOpen = $state(false);
  let branchBusy = $state('');
  let rollingBack = $state<string | null>(null);

  let errors = $state<Record<string, string>>({});
  let forbidden = $state<Record<string, boolean>>({});
  let notice = $state('');

  let descriptionInput = $state('');
  let tagsInput = $state('');
  let ownerIdInput = $state('');
  let selectedTxId = $state<string | null>(null);
  let uploadFile = $state<File | null>(null);

  let compareSelectorA = $state<CompareSelector>({ kind: 'branch', value: '' });
  let compareSelectorB = $state<CompareSelector>({ kind: 'branch', value: '' });
  let compareSideA = $state<CompareSide | null>(null);
  let compareSideB = $state<CompareSide | null>(null);

  const owner = $derived(users.find((user) => user.id === dataset?.owner_id) ?? null);
  const headTransaction = $derived(transactions[0] ?? null);
  const fileItems = $derived(filesystemEntries.filter((entry) => entry.entry_type === 'file'));
  const totalFileBytes = $derived(
    fileItems.reduce((sum, entry) => sum + Number(entry.size_bytes ?? 0), 0),
  );
  const schemaFields = $derived<SchemaField[]>(normalizeSchemaFields(datasetSchema?.fields, previewResponse, quality));
  const previewColumns = $derived<PreviewColumnDef[]>(derivePreviewColumns(previewResponse, schemaFields));
  const previewRows = $derived(previewResponse?.rows ?? []);
  const columnStats = $derived<Record<string, PreviewColumnStats>>(deriveColumnStats(quality));
  const storageHistory = $derived(
    versions
      .slice()
      .sort((a, b) => a.version - b.version)
      .map((version) => ({ ts: version.created_at, bytes: version.size_bytes })),
  );
  const metrics = $derived(deriveMetrics());
  const datasetPermissionRecords = $derived(
    permissionRecords.filter((permission) => permission.resource.toLowerCase().includes('dataset')),
  );

  $effect(() => {
    const rid = datasetId;
    if (!rid || rid === loadedDatasetId) return;
    loadedDatasetId = rid;
    void loadPage(rid);
  });

  $effect(() => {
    const rid = datasetId;
    if (!rid || loading) return;
    if (activeTab === 'preview' && !previewResponse && !previewLoading) void fetchPreview(rid);
    if ((activeTab === 'details' || activeTab === 'metrics') && filesystemEntries.length === 0 && !filesLoading) {
      void fetchFilesystem(rid);
    }
    if ((activeTab === 'history' || activeTab === 'details' || activeTab === 'metrics') && transactions.length === 0 && !transactionsLoading) {
      void fetchTransactions(rid);
    }
    if (activeTab === 'lineage' && !lineageGraph && !lineageLoading) void fetchLineage(rid);
    if (activeTab === 'compare') {
      ensureCompareDefaults();
      if (transactions.length === 0 && !transactionsLoading) void fetchTransactions(rid);
      if (compareSelectorA.value && compareSelectorB.value && !compareLoading) void fetchCompareSides(rid);
    }
  });

  async function loadPage(rid: string) {
    resetState();
    loading = true;
    try {
      dataset = await getDataset(rid);
      syncMetadataForm();
    } catch (cause) {
      markFailure('dataset', cause, 'Dataset could not be loaded.');
      dataset = null;
      loading = false;
      return;
    }

    await Promise.all([
      fetchVersions(rid),
      fetchBranches(rid),
      fetchSchema(rid),
      fetchQuality(rid),
      fetchLint(rid),
      fetchGovernance(),
      fetchPreview(rid),
      fetchRetentionPreview(rid),
    ]);
    ensureCompareDefaults();
    loading = false;
  }

  async function fetchRetentionPreview(rid: string) {
    // Best-effort: the badge cross-link is non-essential, so a 404 /
    // service-down doesn't block the rest of the page.
    try {
      retentionPreview = await getRetentionPreview(rid, 0);
    } catch {
      retentionPreview = null;
    }
  }

  // Maps consumed by FilesTab and HistoryTimeline. Built once per
  // preview load; the days-until-purge math is identical for every
  // would-delete file/txn (preview is rendered "as_of=now").
  const retentionFileMarkers = $derived<Record<string, { policyName: string; daysUntilPurge: number }>>(
    Object.fromEntries(
      (retentionPreview?.files ?? []).map((f) => [
        f.id,
        {
          policyName: f.policy_name,
          daysUntilPurge: 0,
        },
      ]),
    ),
  );
  const retentionTransactionMarkers = $derived<Record<string, { policyName: string; daysUntilPurge: number }>>(
    Object.fromEntries(
      (retentionPreview?.transactions ?? [])
        .filter((t) => t.would_delete && t.policy_name)
        .map((t) => [
          t.id,
          {
            policyName: t.policy_name as string,
            daysUntilPurge: 0,
          },
        ]),
    ),
  );

  function resetState() {
    dataset = null;
    versions = [];
    branches = [];
    users = [];
    currentUser = null;
    permissionRecords = [];
    quality = null;
    lint = null;
    datasetSchema = null;
    viewSchema = null;
    schemaEditMode = false;
    retentionPreview = null;
    previewResponse = null;
    filesystemEntries = [];
    transactions = [];
    lineageGraph = null;
    columnLineage = [];
    lineageImpact = null;
    errors = {};
    forbidden = {};
    notice = '';
    selectedTxId = null;
    uploadFile = null;
    compareSideA = null;
    compareSideB = null;
    compareSelectorA = { kind: 'branch', value: '' };
    compareSelectorB = { kind: 'branch', value: '' };
  }

  function syncMetadataForm() {
    if (!dataset) return;
    descriptionInput = dataset.description ?? '';
    tagsInput = (dataset.tags ?? []).join(', ');
    ownerIdInput = dataset.owner_id ?? '';
  }

  async function fetchVersions(rid: string) {
    try {
      versions = await getVersions(rid);
      clearFailure('history');
    } catch (cause) {
      markFailure('history', cause, 'Versions are unavailable.');
      versions = [];
    }
  }

  async function fetchBranches(rid: string) {
    try {
      branches = await listBranches(rid);
      clearFailure('branches');
    } catch (cause) {
      markFailure('branches', cause, 'Branches are unavailable.');
      branches = [];
    }
    // P3 — JobSpec status badge in the header. Tolerant: treat any
    // failure as "no JobSpec on master" so a transient
    // pipeline-authoring outage doesn't blank the header.
    try {
      jobSpecStatus = await loadJobSpecStatus(rid);
    } catch {
      jobSpecStatus = { has_master_jobspec: false, branches_with_jobspec: [] };
    }
  }

  async function fetchSchema(rid: string) {
    schemaLoading = true;
    try {
      datasetSchema = await getDatasetSchema(rid);
      clearFailure('schema');
    } catch (cause) {
      markFailure('schema', cause, 'Schema is unavailable.');
      datasetSchema = null;
    } finally {
      schemaLoading = false;
    }
    // The view-scoped Foundry schema lives in dataset-versioning-service;
    // a 404 simply means the dataset has no committed view yet, so the
    // failure is non-fatal and the editor stays disabled.
    try {
      const resolved = await resolveCurrentViewId(rid);
      if (resolved) viewSchema = await getViewSchema(rid, resolved);
      else viewSchema = null;
    } catch {
      viewSchema = null;
    }
  }

  async function resolveCurrentViewId(rid: string): Promise<string | null> {
    try {
      const response = await fetch(`/api/v1/datasets/${rid}/views/current`, {
        headers: localStorage.getItem('of_access_token')
          ? { Authorization: `Bearer ${localStorage.getItem('of_access_token')}` }
          : {},
      });
      if (!response.ok) return null;
      const body = (await response.json()) as { id?: string | null };
      return body.id ?? null;
    } catch {
      return null;
    }
  }

  async function fetchPreview(rid: string, params: { version?: number; branch?: string } = {}) {
    previewLoading = true;
    try {
      previewResponse = await previewDataset(rid, { limit: 1000, ...params });
      clearFailure('preview');
    } catch (cause) {
      markFailure('preview', cause, 'Preview is unavailable.');
      previewResponse = null;
    } finally {
      previewLoading = false;
    }
  }

  async function fetchFilesystem(rid: string, path?: string) {
    filesLoading = true;
    try {
      const response = await listDatasetFilesystem(rid, path ? { path } : undefined);
      filesystemEntries = response.entries ?? response.items ?? [];
      clearFailure('files');
    } catch (cause) {
      markFailure('files', cause, 'Files are unavailable.');
      filesystemEntries = [];
    } finally {
      filesLoading = false;
    }
  }

  async function fetchTransactions(rid: string) {
    transactionsLoading = true;
    try {
      transactions = await listDatasetTransactions(rid);
      clearFailure('history');
    } catch (cause) {
      markFailure('history', cause, 'Transaction history is unavailable.');
      transactions = [];
    } finally {
      transactionsLoading = false;
    }
  }

  async function fetchQuality(rid: string) {
    qualityLoading = true;
    try {
      quality = await getDatasetQuality(rid);
      clearFailure('quality');
    } catch (cause) {
      markFailure('quality', cause, 'Quality profile is unavailable.');
      quality = null;
    } finally {
      qualityLoading = false;
    }
  }

  async function fetchLint(rid: string) {
    try {
      lint = await getDatasetLint(rid);
      clearFailure('lint');
    } catch (cause) {
      markFailure('lint', cause, 'Dataset health checks are unavailable.');
      lint = null;
    }
  }

  async function fetchGovernance() {
    await Promise.all([
      listUsers({ limit: 200 })
        .then((value) => {
          users = value;
        })
        .catch((cause) => markFailure('permissions', cause, 'Users are unavailable.')),
      getMe()
        .then((value) => {
          currentUser = value;
        })
        .catch((cause) => markFailure('permissions', cause, 'Current user permissions are unavailable.')),
      listPermissions()
        .then((value) => {
          permissionRecords = value;
        })
        .catch((cause) => markFailure('permissions', cause, 'Permission catalog is unavailable.')),
    ]);
  }

  async function fetchLineage(rid: string) {
    lineageLoading = true;
    try {
      const [graph, columns, impact] = await Promise.all([
        getDatasetLineage(rid),
        getDatasetColumnLineage(rid),
        getDatasetLineageImpact(rid),
      ]);
      lineageGraph = graph;
      columnLineage = columns;
      lineageImpact = impact;
      clearFailure('lineage');
    } catch (cause) {
      markFailure('lineage', cause, 'Lineage is unavailable.');
      lineageGraph = null;
      columnLineage = [];
      lineageImpact = null;
    } finally {
      lineageLoading = false;
    }
  }

  async function refreshQuality() {
    if (!datasetId) return;
    qualityLoading = true;
    try {
      quality = await refreshDatasetQualityProfile(datasetId);
      await fetchLint(datasetId);
      clearFailure('quality');
      notice = 'Quality profile refreshed.';
    } catch (cause) {
      markFailure('quality', cause, 'Quality refresh failed.');
    } finally {
      qualityLoading = false;
    }
  }

  async function switchBranch(name: string) {
    if (!datasetId) return;
    branchBusy = name;
    try {
      dataset = await checkoutDatasetBranch(datasetId, name);
      syncMetadataForm();
      previewResponse = null;
      filesystemEntries = [];
      transactions = [];
      compareSideA = null;
      compareSideB = null;
      await Promise.all([fetchBranches(datasetId), fetchVersions(datasetId), fetchSchema(datasetId), fetchPreview(datasetId, { branch: name })]);
      ensureCompareDefaults();
      notice = `Switched to branch ${name}.`;
    } catch (cause) {
      markFailure('branches', cause, 'Branch checkout failed.');
      throw cause;
    } finally {
      branchBusy = '';
    }
  }

  async function createBranch(params: { name: string; from: string; description?: string }) {
    if (!datasetId || !dataset) return;
    branchBusy = params.name;
    try {
      const source = branches.find((branch) => branch.name === params.from);
      await createDatasetBranch(datasetId, {
        name: params.name,
        source_version: source?.version ?? dataset.current_version,
        description: params.description,
      });
      await fetchBranches(datasetId);
      ensureCompareDefaults();
      notice = `Created branch ${params.name}.`;
    } catch (cause) {
      markFailure('branches', cause, 'Branch creation failed.');
      throw cause;
    } finally {
      branchBusy = '';
    }
  }

  async function saveMetadata() {
    if (!datasetId || !dataset) return;
    savingMetadata = true;
    try {
      dataset = await updateDataset(datasetId, {
        description: descriptionInput.trim(),
        owner_id: ownerIdInput.trim() || undefined,
        tags: tagsInput
          .split(',')
          .map((tag) => tag.trim())
          .filter(Boolean),
      });
      syncMetadataForm();
      clearFailure('metadata');
      notice = 'Dataset details saved.';
    } catch (cause) {
      markFailure('metadata', cause, 'Dataset details could not be saved.');
    } finally {
      savingMetadata = false;
    }
  }

  async function uploadSelectedFile() {
    if (!datasetId || !uploadFile) return;
    uploadLoading = true;
    try {
      await uploadData(datasetId, uploadFile);
      uploadFile = null;
      await Promise.all([
        getDataset(datasetId).then((value) => {
          dataset = value;
          syncMetadataForm();
        }),
        fetchPreview(datasetId),
        fetchSchema(datasetId),
        fetchFilesystem(datasetId),
        fetchTransactions(datasetId),
        fetchVersions(datasetId),
        fetchQuality(datasetId),
        fetchLint(datasetId),
      ]);
      clearFailure('upload');
      notice = 'Upload complete.';
    } catch (cause) {
      markFailure('upload', cause, 'Upload failed.');
    } finally {
      uploadLoading = false;
    }
  }

  async function handleHeaderAction(action: string) {
    if (!datasetId) return;
    if (action === 'permissions') {
      activeTab = 'permissions';
      return;
    }
    if (action === 'delete') {
      const confirmed = window.confirm('Delete this dataset? This cannot be undone.');
      if (!confirmed) return;
      try {
        await deleteDataset(datasetId);
        await goto('/datasets');
      } catch (cause) {
        markFailure('dataset', cause, 'Dataset delete failed.');
      }
      return;
    }
    if (action === 'star' || action === 'move') {
      notice = `${action} is not exposed by the backend for datasets yet.`;
    }
  }

  async function handleBuild(action: string) {
    if (!datasetId) return;
    try {
      const dryRun = action !== 'build_now';
      const result = await triggerLineageBuilds(datasetId, { dry_run: dryRun, include_workflows: true });
      notice = dryRun
        ? `${result.candidates.length} lineage build candidate${result.candidates.length === 1 ? '' : 's'} found.`
        : `${result.triggered.length} build${result.triggered.length === 1 ? '' : 's'} triggered.`;
      clearFailure('build');
      activeTab = 'lineage';
      await fetchLineage(datasetId);
    } catch (cause) {
      markFailure('build', cause, 'Build trigger failed.');
    }
  }

  function handleAnalyze(target: string) {
    if (!datasetId) return;
    if (target === 'open_contour') void goto(`/contour?dataset_rid=${datasetId}`);
    else void goto(`/quiver?dataset_rid=${datasetId}`);
  }

  function handleExplorePipeline() {
    activeTab = 'lineage';
    if (datasetId && !lineageGraph) void fetchLineage(datasetId);
  }

  function viewTransaction(tx: DatasetTransaction) {
    selectedTxId = tx.id;
    activeTab = 'preview';
    const version = versionForTransaction(tx);
    if (datasetId && version !== null) void fetchPreview(datasetId, { version });
  }

  function rollbackTransaction(tx: DatasetTransaction) {
    rollingBack = tx.id;
    errors = {
      ...errors,
      history: `Rollback is not exposed by the backend yet. Transaction ${tx.id} can still be inspected from Preview.`,
    };
    setTimeout(() => {
      rollingBack = null;
    }, 300);
  }

  function onUploadFileChange(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    uploadFile = input.files?.[0] ?? null;
  }

  function onCompareSelector(which: 'A' | 'B', selector: CompareSelector) {
    if (which === 'A') compareSelectorA = selector;
    else compareSelectorB = selector;
    compareSideA = null;
    compareSideB = null;
    if (datasetId) void fetchCompareSides(datasetId);
  }

  async function fetchCompareSides(rid: string) {
    if (!compareSelectorA.value || !compareSelectorB.value) return;
    compareLoading = true;
    try {
      const [sideA, sideB] = await Promise.all([
        buildCompareSide(rid, compareSelectorA),
        buildCompareSide(rid, compareSelectorB),
      ]);
      compareSideA = sideA;
      compareSideB = sideB;
      clearFailure('compare');
    } catch (cause) {
      markFailure('compare', cause, 'Comparison data is unavailable.');
      compareSideA = null;
      compareSideB = null;
    } finally {
      compareLoading = false;
    }
  }

  async function buildCompareSide(rid: string, selector: CompareSelector): Promise<CompareSide> {
    if (selector.kind === 'branch') {
      const preview = await previewDataset(rid, { limit: 1, branch: selector.value });
      const files = await listDatasetFilesystem(rid, { path: `branches/${selector.value}` });
      return {
        label: `Branch ${selector.value}`,
        schema: normalizeSchemaFields(null, preview, quality),
        files: files.entries ?? files.items ?? [],
      };
    }

    const tx = transactions.find((item) => item.id === selector.value);
    const version = tx ? versionForTransaction(tx) : null;
    const preview = await previewDataset(rid, { limit: 1, version: version ?? undefined });
    const files = await listDatasetFilesystem(rid, version !== null ? { path: `versions/v${version}` } : undefined);
    return {
      label: version !== null ? `v${version} (${selector.value.slice(0, 8)})` : `Transaction ${selector.value.slice(0, 8)}`,
      schema: normalizeSchemaFields(null, preview, quality),
      files: files.entries ?? files.items ?? [],
    };
  }

  function ensureCompareDefaults() {
    if (!compareSelectorA.value) {
      const defaultBranch = branches.find((branch) => branch.name === dataset?.active_branch) ?? branches[0];
      if (defaultBranch) compareSelectorA = { kind: 'branch', value: defaultBranch.name };
    }
    if (!compareSelectorB.value) {
      const alternateBranch = branches.find((branch) => branch.name !== compareSelectorA.value);
      const latestTx = transactions[0];
      if (alternateBranch) compareSelectorB = { kind: 'branch', value: alternateBranch.name };
      else if (latestTx) compareSelectorB = { kind: 'transaction', value: latestTx.id };
      else if (compareSelectorA.value) compareSelectorB = compareSelectorA;
    }
  }

  function versionForTransaction(tx: DatasetTransaction): number | null {
    const versionRecord = versions.find((item) => item.transaction_id === tx.id);
    if (versionRecord) return versionRecord.version;
    const fromMetadata = tx.metadata?.version ?? tx.metadata?.current_version ?? tx.metadata?.dataset_version;
    const numeric = Number(fromMetadata);
    return Number.isFinite(numeric) ? numeric : null;
  }

  function markFailure(area: Area, cause: unknown, fallback: string) {
    const isForbidden = cause instanceof ApiError && cause.status === 403;
    const message = isForbidden
      ? 'You do not have enough permissions to view or change this section.'
      : cause instanceof Error
        ? cause.message
        : fallback;
    errors = { ...errors, [area]: message };
    forbidden = { ...forbidden, [area]: isForbidden };
  }

  function clearFailure(area: Area) {
    const nextErrors = { ...errors };
    const nextForbidden = { ...forbidden };
    delete nextErrors[area];
    delete nextForbidden[area];
    errors = nextErrors;
    forbidden = nextForbidden;
  }

  function formatBytes(value?: number | null): string {
    const bytes = Number(value ?? 0);
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
  }

  function formatDate(value?: string | null): string {
    return value ? new Date(value).toLocaleString() : '-';
  }

  function formatScore(value?: number | null): string {
    if (value === null || value === undefined) return '-';
    return value > 1 ? value.toFixed(1) : `${Math.round(value * 100)}%`;
  }

  function displayUser(userId?: string | null): string {
    if (!userId) return '-';
    const user = users.find((item) => item.id === userId);
    return user ? `${user.name} (${user.email})` : userId;
  }

  function hasDatasetPermission(action: 'read' | 'write' | 'delete' | 'admin'): boolean {
    if (!currentUser || !dataset) return false;
    if (currentUser.id === dataset.owner_id) return true;
    return currentUser.permissions.some((permission) => {
      const normalized = permission.toLowerCase();
      return (
        normalized === '*' ||
        normalized === `datasets:${action}` ||
        normalized === `dataset:${action}` ||
        normalized === `datasets.${action}` ||
        normalized === `datasets:*` ||
        normalized === `dataset:*`
      );
    });
  }

  function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null && !Array.isArray(value);
  }

  function normalizeSchemaFields(
    raw: unknown,
    preview: DatasetPreviewResponse | null,
    qualityResponse: DatasetQualityResponse | null,
  ): SchemaField[] {
    const direct = schemaArrayFromUnknown(raw);
    if (direct.length > 0) return direct;

    const previewFields =
      preview?.columns?.map((column) => ({
        name: column.name,
        type: column.field_type ?? column.data_type ?? 'unknown',
        nullable: column.nullable,
      })) ?? [];
    if (previewFields.length > 0) return previewFields;

    return (
      qualityResponse?.profile?.columns.map((column) => ({
        name: column.name,
        type: column.field_type,
        nullable: column.nullable,
      })) ?? []
    );
  }

  function schemaArrayFromUnknown(raw: unknown): SchemaField[] {
    if (Array.isArray(raw)) return raw.map(normalizeSchemaField).filter(Boolean) as SchemaField[];
    if (isRecord(raw)) {
      const candidates = [raw.fields, raw.columns, raw.schema];
      for (const candidate of candidates) {
        const fields = schemaArrayFromUnknown(candidate);
        if (fields.length > 0) return fields;
      }
    }
    return [];
  }

  function normalizeSchemaField(value: unknown): SchemaField | null {
    if (!isRecord(value)) return null;
    const name = String(value.name ?? value.field_name ?? value.column ?? '');
    if (!name) return null;
    return {
      name,
      type: String(value.type ?? value.field_type ?? value.data_type ?? 'unknown'),
      nullable: typeof value.nullable === 'boolean' ? value.nullable : undefined,
      description: typeof value.description === 'string' ? value.description : undefined,
      subSchemas: schemaArrayFromUnknown(value.subSchemas ?? value.fields ?? value.children),
    };
  }

  function derivePreviewColumns(preview: DatasetPreviewResponse | null, fields: SchemaField[]): PreviewColumnDef[] {
    if (preview?.columns?.length) {
      return preview.columns.map((column) => ({
        name: column.name,
        field_type: column.field_type ?? column.data_type,
      }));
    }
    if (fields.length > 0) return fields.map((field) => ({ name: field.name, field_type: field.type }));
    const firstRow = preview?.rows?.[0];
    return firstRow ? Object.keys(firstRow).map((name) => ({ name })) : [];
  }

  function deriveColumnStats(response: DatasetQualityResponse | null): Record<string, PreviewColumnStats> {
    const stats: Record<string, PreviewColumnStats> = {};
    for (const column of response?.profile?.columns ?? []) {
      stats[column.name] = {
        min: column.min_value,
        max: column.max_value,
        null_rate: column.null_rate,
        distinct_count: column.distinct_count,
      };
    }
    return stats;
  }

  function describeColumn(column: DatasetColumnProfile): string {
    return `${column.field_type}${column.nullable ? ', nullable' : ', required'}; null ${Math.round(column.null_rate * 100)}%; distinct ${column.distinct_count}`;
  }

  function deriveMetrics() {
    const failedTransactions = transactions.filter((tx) => tx.status.toLowerCase() === 'failed').length;
    const pendingTransactions = transactions.filter((tx) => ['pending', 'running'].includes(tx.status.toLowerCase())).length;
    const activeAlerts = quality?.alerts.filter((alert) => alert.status.toLowerCase() !== 'resolved').length ?? 0;
    const qualityScore = quality?.score ?? lint?.summary.quality_score ?? null;
    return {
      rows: dataset?.row_count ?? previewResponse?.total_rows ?? 0,
      bytes: dataset?.size_bytes ?? totalFileBytes,
      files: fileItems.length,
      versions: versions.length,
      branches: branches.length,
      transactions: transactions.length,
      failedTransactions,
      pendingTransactions,
      activeAlerts,
      qualityScore,
      findings: lint?.summary.total_findings ?? 0,
      highFindings: lint?.summary.high_severity ?? 0,
      staleBranches: lint?.summary.stale_branch_count ?? 0,
      avgObjectSize: lint?.summary.average_object_size_bytes ?? (fileItems.length ? totalFileBytes / fileItems.length : 0),
    };
  }
</script>

<svelte:head>
  <title>{dataset ? `${dataset.name} - Dataset` : 'Dataset'}</title>
</svelte:head>

{#if loading}
  <div class="mx-auto max-w-7xl px-4 py-8">
    <div class="rounded-lg border border-slate-200 bg-white px-5 py-8 text-sm text-slate-600 shadow-sm dark:border-gray-800 dark:bg-gray-950 dark:text-gray-300">
      Loading dataset...
    </div>
  </div>
{:else if forbidden.dataset}
  <div class="mx-auto max-w-3xl px-4 py-12">
    <div class="rounded-lg border border-amber-200 bg-amber-50 p-6 text-amber-900 dark:border-amber-900/50 dark:bg-amber-950/40 dark:text-amber-100">
      <h1 class="text-lg font-semibold">Insufficient permissions</h1>
      <p class="mt-2 text-sm">{errors.dataset}</p>
    </div>
  </div>
{:else if !dataset}
  <div class="mx-auto max-w-3xl px-4 py-12">
    <div class="rounded-lg border border-rose-200 bg-rose-50 p-6 text-rose-800 dark:border-rose-900/50 dark:bg-rose-950/40 dark:text-rose-100">
      <h1 class="text-lg font-semibold">Dataset unavailable</h1>
      <p class="mt-2 text-sm">{errors.dataset ?? 'The dataset could not be found.'}</p>
    </div>
  </div>
{:else}
  <div class="mx-auto max-w-7xl space-y-5 px-4 py-6">
    <DatasetHeader
      {dataset}
      {branches}
      markings={[]}
      busy={Boolean(branchBusy)}
      jobSpecStatus={jobSpecStatus ?? undefined}
      canManage={hasDatasetPermission('admin')}
      onSwitchBranch={switchBranch}
      onCreateBranch={createBranch}
      onAllActions={handleHeaderAction}
      onBuild={handleBuild}
      onAnalyze={handleAnalyze}
      onExplorePipeline={handleExplorePipeline}
      onPublishToMarketplace={() => (publishModalOpen = true)}
    />
    <PublishToMarketplaceModal
      dataset={dataset}
      open={publishModalOpen}
      onClose={() => (publishModalOpen = false)}
    />

    {#if notice}
      <div class="rounded-lg border border-blue-200 bg-blue-50 px-4 py-2 text-sm text-blue-800 dark:border-blue-900/50 dark:bg-blue-950/40 dark:text-blue-100">
        {notice}
      </div>
    {/if}

    {#if branches.find((b) => b.name === dataset!.active_branch)?.has_open_transaction}
      {@const openTxn = transactions.find(
        (t) => t.status === 'OPEN' && t.branch_name === dataset!.active_branch,
      )}
      <OpenTransactionBanner
        datasetId={dataset.id}
        branch={dataset.active_branch}
        openTransactionId={openTxn?.id ?? null}
        canManage={hasDatasetPermission('admin')}
        onResolved={() => {
          void fetchBranches(dataset!.id);
          void fetchTransactions(dataset!.id);
        }}
      />
    {/if}

    {#if errors.build}
      <div class="rounded-lg border border-rose-200 bg-rose-50 px-4 py-2 text-sm text-rose-800 dark:border-rose-900/50 dark:bg-rose-950/40 dark:text-rose-100">
        {errors.build}
      </div>
    {/if}

    <nav class="flex gap-1 overflow-x-auto border-b border-slate-200 dark:border-gray-800" aria-label="Dataset views">
      {#each tabs as tab (tab.key)}
        <button
          type="button"
          class={`whitespace-nowrap border-b-2 px-3 py-2 text-sm font-medium ${
            activeTab === tab.key
              ? 'border-blue-600 text-blue-700 dark:text-blue-300'
              : 'border-transparent text-slate-600 hover:border-slate-300 hover:text-slate-900 dark:text-gray-400 dark:hover:text-gray-100'
          }`}
          onclick={() => (activeTab = tab.key)}
        >
          {tab.label}
        </button>
      {/each}
    </nav>

    {#if activeTab === 'preview'}
      <section class="space-y-4">
        <div class="flex flex-col gap-3 rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950 md:flex-row md:items-center md:justify-between">
          <div>
            <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Preview</div>
            <h2 class="mt-1 text-lg font-semibold">Current view</h2>
            <p class="mt-1 text-sm text-slate-500">
              Branch <span class="font-mono">{previewResponse?.branch ?? dataset.active_branch}</span>,
              version <span class="font-mono">v{previewResponse?.version ?? dataset.current_version}</span>.
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <input
              type="file"
              class="max-w-64 text-sm file:mr-3 file:rounded-md file:border-0 file:bg-slate-100 file:px-3 file:py-1.5 file:text-sm file:text-slate-700 dark:file:bg-gray-800 dark:file:text-gray-200"
              onchange={onUploadFileChange}
            />
            <button
              type="button"
              class="rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
              disabled={!uploadFile || uploadLoading}
              onclick={() => void uploadSelectedFile()}
            >
              {uploadLoading ? 'Uploading...' : 'Upload data'}
            </button>
            <button
              type="button"
              class="rounded-md border border-slate-300 px-3 py-1.5 text-sm hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-900"
              onclick={() => {
                if (datasetId) void fetchPreview(datasetId);
              }}
            >
              Refresh
            </button>
          </div>
        </div>

        {#if errors.upload}
          <div class="rounded-lg border border-rose-200 bg-rose-50 px-4 py-2 text-sm text-rose-800 dark:border-rose-900/50 dark:bg-rose-950/40 dark:text-rose-100">
            {errors.upload}
          </div>
        {/if}

        {#if forbidden.preview}
          {@render StateCard('Insufficient permissions', errors.preview)}
        {:else if errors.preview}
          {@render StateCard('Preview unavailable', errors.preview, 'error')}
        {:else if previewLoading}
          {@render StateCard('Loading preview', 'Reading rows from the dataset backend.')}
        {:else if previewColumns.length === 0}
          {@render StateCard('No preview rows', 'The backend returned no readable rows or columns for this dataset version.')}
        {:else}
          <VirtualizedPreviewTable
            columns={previewColumns}
            rows={previewRows}
            stats={columnStats}
            {transactions}
            selectedTransactionId={selectedTxId}
            onSelectTransaction={(txId) => (selectedTxId = txId)}
            viewportHeight={520}
          />
        {/if}
      </section>
    {:else if activeTab === 'schema'}
      <section class="space-y-4">
        {#if forbidden.schema}
          {@render StateCard('Insufficient permissions', errors.schema)}
        {:else if errors.schema && schemaFields.length === 0 && !viewSchema}
          {@render StateCard('Schema unavailable', errors.schema, 'error')}
        {:else if schemaLoading}
          {@render StateCard('Loading schema', 'Reading schema metadata from the catalog.')}
        {:else if viewSchema}
          {#if hasDatasetPermission('write')}
            <div class="flex justify-end">
              <button
                type="button"
                class="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm hover:bg-slate-50 dark:border-gray-700 dark:bg-gray-900 dark:hover:bg-gray-800"
                onclick={() => (schemaEditMode = !schemaEditMode)}
                data-testid="schema-toggle-mode"
              >
                {schemaEditMode ? 'Done editing' : 'Edit schema'}
              </button>
            </div>
          {/if}
          <SchemaViewer
            schema={viewSchema.schema}
            mode={schemaEditMode ? 'edit' : 'view'}
            datasetRid={datasetId}
            viewId={viewSchema.view_id}
            onOpenHistory={() => (activeTab = 'history')}
            onSaved={(response) => {
              viewSchema = response;
              schemaEditMode = false;
            }}
          />
        {:else}
          <SchemaPanel fields={schemaFields} format={dataset.format} />
        {/if}
      </section>
    {:else if activeTab === 'files'}
      <FilesTab
        datasetRid={dataset.id}
        branch={dataset.active_branch}
        viewId={viewSchema?.view_id}
        retentionPurges={retentionFileMarkers}
        onOpenRetention={() => (activeTab = 'retention')}
      />
    {:else if activeTab === 'storage'}
      {#if hasDatasetPermission('admin')}
        <StorageDetailsTab datasetRid={dataset.id} />
      {:else}
        {@render StateCard('Insufficient permissions', 'Storage details require dataset-manage permission.')}
      {/if}
    {:else if activeTab === 'retention'}
      <RetentionPoliciesTab
        datasetRid={dataset.id}
        canManage={hasDatasetPermission('admin')}
      />
    {:else if activeTab === 'lineage'}
      <section class="space-y-4">
        {#if forbidden.lineage}
          {@render StateCard('Insufficient permissions', errors.lineage)}
        {:else if errors.lineage}
          {@render StateCard('Lineage unavailable', errors.lineage, 'error')}
        {:else if lineageLoading}
          {@render StateCard('Loading lineage', 'Reading graph, column lineage, and impact analysis.')}
        {:else if !lineageGraph || lineageGraph.nodes.length === 0}
          {@render StateCard('No lineage yet', 'No upstream or downstream resources have been registered for this dataset.')}
        {:else}
          <div class="grid grid-cols-1 gap-3 md:grid-cols-4">
            {@render MetricCard('Resources', lineageGraph.nodes.length.toLocaleString())}
            {@render MetricCard('Edges', lineageGraph.edges.length.toLocaleString())}
            {@render MetricCard('Column edges', columnLineage.length.toLocaleString())}
            {@render MetricCard('Build candidates', (lineageImpact?.build_candidates.length ?? 0).toLocaleString())}
          </div>

          <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
            {@render LineageList('Upstream', lineageImpact?.upstream ?? [], 'No upstream inputs.')}
            {@render LineageList('Downstream', lineageImpact?.downstream ?? [], 'No downstream consumers.')}
          </div>

          <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950">
            <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Graph edges</div>
            <ul class="mt-3 divide-y divide-slate-100 dark:divide-gray-800">
              {#each lineageGraph.edges as edge (edge.id)}
                <li class="flex flex-col gap-1 py-2 text-sm md:flex-row md:items-center md:justify-between">
                  <span class="font-mono text-xs">{edge.source} -> {edge.target}</span>
                  <span class="text-slate-500">{edge.relation_kind} / {edge.effective_marking}</span>
                </li>
              {/each}
            </ul>
          </div>
        {/if}
      </section>
    {:else if activeTab === 'history'}
      <section class="space-y-4">
        {#if forbidden.history}
          {@render StateCard('Insufficient permissions', errors.history)}
        {:else if errors.history && transactions.length === 0 && versions.length === 0}
          {@render StateCard('History unavailable', errors.history, 'error')}
        {:else if transactionsLoading}
          {@render StateCard('Loading history', 'Reading transactions and versions.')}
        {:else}
          <HistoryTimeline
            {transactions}
            {rollingBack}
            onView={viewTransaction}
            onRollback={rollbackTransaction}
            retentionPurges={retentionTransactionMarkers}
            onOpenRetention={() => (activeTab = 'retention')}
          />

          <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950">
            <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Versions</div>
            {#if versions.length === 0}
              <p class="mt-3 text-sm text-slate-500">No versions have been committed yet.</p>
            {:else}
              <div class="mt-3 overflow-x-auto">
                <table class="min-w-full divide-y divide-slate-200 text-sm dark:divide-gray-800">
                  <thead class="text-left text-xs uppercase tracking-wide text-slate-500">
                    <tr>
                      <th class="py-2 pr-4">Version</th>
                      <th class="py-2 pr-4">Rows</th>
                      <th class="py-2 pr-4">Size</th>
                      <th class="py-2 pr-4">Created</th>
                      <th class="py-2 pr-4">Message</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-slate-100 dark:divide-gray-800">
                    {#each versions as version (version.id)}
                      <tr>
                        <td class="py-2 pr-4 font-mono">v{version.version}</td>
                        <td class="py-2 pr-4">{version.row_count.toLocaleString()}</td>
                        <td class="py-2 pr-4">{formatBytes(version.size_bytes)}</td>
                        <td class="py-2 pr-4">{formatDate(version.created_at)}</td>
                        <td class="py-2 pr-4">{version.message || '-'}</td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
            {/if}
          </div>
        {/if}
      </section>
    {:else if activeTab === 'permissions'}
      <section class="space-y-4">
        {#if forbidden.permissions}
          {@render StateCard('Insufficient permissions', errors.permissions)}
        {:else}
          <div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
            <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950">
              <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Owner</div>
              <h2 class="mt-2 text-lg font-semibold">{owner?.name ?? dataset.owner_id}</h2>
              <p class="mt-1 text-sm text-slate-500">{owner?.email ?? 'Owner profile not loaded.'}</p>
            </div>
            {@render MetricCard('Can view', hasDatasetPermission('read') ? 'Yes' : 'No')}
            {@render MetricCard('Can edit', hasDatasetPermission('write') ? 'Yes' : 'No')}
          </div>

          <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950">
            <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Current user</div>
            {#if currentUser}
              <dl class="mt-3 grid grid-cols-1 gap-3 text-sm md:grid-cols-3">
                {@render InfoItem('Name', currentUser.name)}
                {@render InfoItem('Email', currentUser.email)}
                {@render InfoItem('Roles', currentUser.roles.join(', ') || '-')}
              </dl>
              <div class="mt-3 flex flex-wrap gap-1">
                {#each currentUser.permissions as permission (permission)}
                  <span class="rounded-full bg-slate-100 px-2 py-0.5 font-mono text-xs text-slate-700 dark:bg-gray-800 dark:text-gray-200">{permission}</span>
                {/each}
              </div>
            {:else}
              <p class="mt-3 text-sm text-slate-500">The current user profile was not returned by the backend.</p>
            {/if}
          </div>

          <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950">
            <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Dataset permission catalog</div>
            {#if datasetPermissionRecords.length === 0}
              <p class="mt-3 text-sm text-slate-500">No dataset-scoped permission records were returned.</p>
            {:else}
              <ul class="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
                {#each datasetPermissionRecords as permission (permission.id)}
                  <li class="rounded-md border border-slate-200 p-3 text-sm dark:border-gray-800">
                    <div class="font-mono text-xs">{permission.resource}:{permission.action}</div>
                    <p class="mt-1 text-slate-500">{permission.description ?? 'No description.'}</p>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        {/if}
      </section>
    {:else if activeTab === 'details'}
      <section class="space-y-4">
        {#if forbidden.metadata}
          {@render StateCard('Insufficient permissions', errors.metadata)}
        {/if}
        {#if errors.metadata && !forbidden.metadata}
          {@render StateCard('Details update failed', errors.metadata, 'error')}
        {/if}

        <div class="grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1fr)_360px]">
          <form class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950" onsubmit={(event) => { event.preventDefault(); void saveMetadata(); }}>
            <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Details</div>
            <div class="mt-4 space-y-4">
              <label class="block text-sm">
                <span class="font-medium">Description</span>
                <textarea
                  class="mt-1 min-h-28 w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900"
                  bind:value={descriptionInput}
                ></textarea>
              </label>
              <label class="block text-sm">
                <span class="font-medium">Tags</span>
                <input
                  class="mt-1 w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900"
                  bind:value={tagsInput}
                  placeholder="pii, curated, finance"
                />
              </label>
              <label class="block text-sm">
                <span class="font-medium">Owner ID</span>
                <input
                  class="mt-1 w-full rounded-md border border-slate-300 bg-white px-3 py-2 font-mono text-sm dark:border-gray-700 dark:bg-gray-900"
                  bind:value={ownerIdInput}
                />
              </label>
            </div>
            <button
              type="submit"
              class="mt-4 rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
              disabled={savingMetadata}
            >
              {savingMetadata ? 'Saving...' : 'Save details'}
            </button>
          </form>

          <div class="space-y-4">
            <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950">
              <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Catalog metadata</div>
              <dl class="mt-3 space-y-2 text-sm">
                {@render InfoItem('RID', dataset.id, true)}
                {@render InfoItem('Format', dataset.format)}
                {@render InfoItem('Storage path', dataset.storage_path, true)}
                {@render InfoItem('Owner', displayUser(dataset.owner_id))}
                {@render InfoItem('Created', formatDate(dataset.created_at))}
                {@render InfoItem('Updated', formatDate(dataset.updated_at))}
              </dl>
            </div>
            <ResourceUsagePanel
              sizeBytes={dataset.size_bytes}
              fileCount={fileItems.length}
              rowCount={dataset.row_count}
              history={storageHistory}
            />
          </div>
        </div>

        {#if forbidden.files}
          {@render StateCard('Insufficient permissions', errors.files)}
        {:else}
          <FilesPanel
            entries={filesystemEntries}
            currentVersion={dataset.current_version}
            activeBranch={dataset.active_branch}
            loading={filesLoading}
            error={errors.files ?? ''}
          />
        {/if}

        <CurrentTransactionViewPanel
          head={headTransaction}
          composedOf={transactions}
          fileCount={fileItems.length}
          totalBytes={totalFileBytes}
        />
      </section>
    {:else if activeTab === 'health'}
      <section class="space-y-4">
        <!-- P6 — Foundry "Data Health" dashboard. Lives above the
             legacy quality/lint cards so the six-card matrix is the
             primary signal a dataset owner sees. -->
        {#if dataset?.id}
          <QualityDashboard datasetRid={dataset.id} />
        {/if}

        <div class="flex flex-col gap-3 rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950 md:flex-row md:items-center md:justify-between">
          <div>
            <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Health</div>
            <h2 class="mt-1 text-lg font-semibold">Quality profile and lint findings</h2>
          </div>
          <button
            type="button"
            class="rounded-md bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50"
            disabled={qualityLoading}
            onclick={() => void refreshQuality()}
          >
            {qualityLoading ? 'Refreshing...' : 'Refresh profile'}
          </button>
        </div>

        {#if forbidden.quality || forbidden.lint}
          {@render StateCard('Insufficient permissions', errors.quality ?? errors.lint)}
        {:else}
          <div class="grid grid-cols-1 gap-3 md:grid-cols-4">
            {@render MetricCard('Quality score', formatScore(quality?.score))}
            {@render MetricCard('Active alerts', (quality?.alerts.filter((alert) => alert.status.toLowerCase() !== 'resolved').length ?? 0).toLocaleString())}
            {@render MetricCard('Findings', (lint?.summary.total_findings ?? 0).toLocaleString())}
            {@render MetricCard('Posture', lint?.summary.resource_posture ?? '-')}
          </div>

          {#if errors.quality && !quality}
            {@render StateCard('Quality unavailable', errors.quality, 'error')}
          {:else if !quality?.profile}
            {@render StateCard('No quality profile', 'Run a profile refresh after data is uploaded to populate completeness and uniqueness metrics.')}
          {:else}
            <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950">
              <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Column profile</div>
              <ul class="mt-3 divide-y divide-slate-100 dark:divide-gray-800">
                {#each quality.profile.columns as column (column.name)}
                  <li class="flex flex-col gap-1 py-2 text-sm md:flex-row md:items-center md:justify-between">
                    <span class="font-mono text-xs">{column.name}</span>
                    <span class="text-slate-500">{describeColumn(column)}</span>
                  </li>
                {/each}
              </ul>
            </div>
          {/if}

          {#if errors.lint && !lint}
            {@render StateCard('Lint unavailable', errors.lint, 'error')}
          {:else if lint}
            <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
              <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950">
                <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Findings</div>
                {#if lint.findings.length === 0}
                  <p class="mt-3 text-sm text-slate-500">No health findings were returned.</p>
                {:else}
                  <ul class="mt-3 space-y-2">
                    {#each lint.findings as finding (finding.code)}
                      <li class="rounded-md border border-slate-200 p-3 text-sm dark:border-gray-800">
                        <div class="flex items-center justify-between gap-3">
                          <span class="font-medium">{finding.title}</span>
                          <span class="rounded-full bg-slate-100 px-2 py-0.5 text-xs uppercase text-slate-700 dark:bg-gray-800 dark:text-gray-200">{finding.severity}</span>
                        </div>
                        <p class="mt-1 text-slate-500">{finding.description}</p>
                      </li>
                    {/each}
                  </ul>
                {/if}
              </div>
              <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950">
                <div class="text-xs uppercase tracking-[0.18em] text-slate-400">Recommendations</div>
                {#if lint.recommendations.length === 0}
                  <p class="mt-3 text-sm text-slate-500">No recommendations.</p>
                {:else}
                  <ul class="mt-3 space-y-2">
                    {#each lint.recommendations as recommendation (recommendation.code)}
                      <li class="rounded-md border border-slate-200 p-3 text-sm dark:border-gray-800">
                        <div class="font-medium">{recommendation.title}</div>
                        <p class="mt-1 text-slate-500">{recommendation.rationale}</p>
                      </li>
                    {/each}
                  </ul>
                {/if}
              </div>
            </div>
          {/if}
        {/if}
      </section>
    {:else if activeTab === 'metrics'}
      <section class="space-y-4">
        <div class="grid grid-cols-1 gap-3 md:grid-cols-4">
          {@render MetricCard('Rows', metrics.rows.toLocaleString())}
          {@render MetricCard('Storage', formatBytes(metrics.bytes))}
          {@render MetricCard('Files', metrics.files.toLocaleString())}
          {@render MetricCard('Avg file size', formatBytes(metrics.avgObjectSize))}
          {@render MetricCard('Versions', metrics.versions.toLocaleString())}
          {@render MetricCard('Branches', metrics.branches.toLocaleString())}
          {@render MetricCard('Transactions', metrics.transactions.toLocaleString())}
          {@render MetricCard('Pending tx', metrics.pendingTransactions.toLocaleString())}
          {@render MetricCard('Failed tx', metrics.failedTransactions.toLocaleString())}
          {@render MetricCard('Quality', formatScore(metrics.qualityScore))}
          {@render MetricCard('High findings', metrics.highFindings.toLocaleString())}
          {@render MetricCard('Stale branches', metrics.staleBranches.toLocaleString())}
        </div>
        <ResourceUsagePanel
          sizeBytes={dataset.size_bytes}
          fileCount={fileItems.length}
          rowCount={dataset.row_count}
          history={storageHistory}
        />
      </section>
    {:else if activeTab === 'compare'}
      <section class="space-y-4">
        {#if forbidden.compare}
          {@render StateCard('Insufficient permissions', errors.compare)}
        {:else if branches.length === 0 && transactions.length === 0}
          {@render StateCard('Nothing to compare', 'Create another branch or commit a transaction to compare dataset states.')}
        {:else}
          <CompareTab
            {transactions}
            {branches}
            sideA={compareSideA}
            sideB={compareSideB}
            selectorA={compareSelectorA}
            selectorB={compareSelectorB}
            loading={compareLoading}
            error={errors.compare ?? ''}
            onChangeSelector={onCompareSelector}
          />
        {/if}
      </section>
    {/if}
  </div>
{/if}

{#snippet StateCard(title: string, message: string, tone: 'default' | 'error' = 'default')}
  <div class={`rounded-lg border px-5 py-8 text-center shadow-sm ${
    tone === 'error'
      ? 'border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-900/50 dark:bg-rose-950/40 dark:text-rose-100'
      : 'border-slate-200 bg-white text-slate-600 dark:border-gray-800 dark:bg-gray-950 dark:text-gray-300'
  }`}>
    <h2 class="text-base font-semibold">{title}</h2>
    <p class="mt-2 text-sm">{message}</p>
  </div>
{/snippet}

{#snippet MetricCard(label: string, value: string)}
  <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950">
    <div class="text-xs uppercase tracking-[0.18em] text-slate-400">{label}</div>
    <div class="mt-2 text-2xl font-semibold">{value}</div>
  </div>
{/snippet}

{#snippet InfoItem(label: string, value: string, mono = false)}
  <div>
    <dt class="text-xs uppercase tracking-wide text-slate-400">{label}</dt>
    <dd class={`mt-0.5 break-words ${mono ? 'font-mono text-xs' : ''}`}>{value}</dd>
  </div>
{/snippet}

{#snippet LineageList(title: string, items: LineageImpactItem[], empty: string)}
  <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-800 dark:bg-gray-950">
    <div class="text-xs uppercase tracking-[0.18em] text-slate-400">{title}</div>
    {#if items.length === 0}
      <p class="mt-3 text-sm text-slate-500">{empty}</p>
    {:else}
      <ul class="mt-3 divide-y divide-slate-100 dark:divide-gray-800">
        {#each items as item (item.id)}
          <li class="py-2 text-sm">
            <div class="flex items-center justify-between gap-3">
              <span class="font-medium">{item.label}</span>
              <span class="text-xs text-slate-500">{item.kind} / distance {item.distance}</span>
            </div>
            <div class="mt-1 font-mono text-xs text-slate-500">{item.id}</div>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
{/snippet}
