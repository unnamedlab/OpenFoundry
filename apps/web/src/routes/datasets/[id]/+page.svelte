<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import RetentionPoliciesTab, { type RetentionPolicy } from '$lib/components/dataset/RetentionPoliciesTab.svelte';
  import DatasetHeader from '$lib/components/dataset/DatasetHeader.svelte';
  import VirtualizedPreviewTable, {
    type ColumnDef as PreviewColumnDef,
    type ColumnStats as PreviewColumnStats,
  } from '$lib/components/dataset/VirtualizedPreviewTable.svelte';
  import HistoryTimeline from '$lib/components/dataset/HistoryTimeline.svelte';
  import CompareTab, { type CompareSide } from '$lib/components/dataset/CompareTab.svelte';
  import AboutPanel from '$lib/components/dataset/details/AboutPanel.svelte';
  import SchemaPanel, { type SchemaField } from '$lib/components/dataset/details/SchemaPanel.svelte';
  import FilesPanel from '$lib/components/dataset/details/FilesPanel.svelte';
  import JobSpecPanel from '$lib/components/dataset/details/JobSpecPanel.svelte';
  import SyncStatusPanel from '$lib/components/dataset/details/SyncStatusPanel.svelte';
  import CustomMetadataPanel from '$lib/components/dataset/details/CustomMetadataPanel.svelte';
  import CurrentTransactionViewPanel from '$lib/components/dataset/details/CurrentTransactionViewPanel.svelte';
  import ResourceUsagePanel from '$lib/components/dataset/details/ResourceUsagePanel.svelte';
  import { listUsers, type UserProfile } from '$lib/api/auth';
  import {
    checkoutDatasetBranch,
    createDatasetBranch,
    createDatasetQualityRule,
    deleteDatasetQualityRule,
    getDataset,
    getDatasetLint,
    getDatasetQuality,
    getVersions,
    listBranches,
    listDatasetFilesystem,
    listDatasetTransactions,
    previewDataset,
    refreshDatasetQualityProfile,
    updateDataset,
    updateDatasetQualityRule,
    type CreateDatasetBranchParams,
    type DatasetBranch,
    type CreateDatasetQualityRuleParams,
    type Dataset,
    type DatasetColumnProfile,
    type DatasetFilesystemEntry,
    type DatasetLintResponse,
    type DatasetPreviewResponse,
    type DatasetQualityResponse,
    type DatasetQualityRule,
    type DatasetRuleResult,
    type DatasetTransaction,
    type DatasetVersion,
  } from '$lib/api/datasets';

  type RuleType = 'null_check' | 'range' | 'regex' | 'custom_sql';
  type RuleSeverity = 'low' | 'medium' | 'high';
  type RuleOperator = 'gt' | 'gte' | 'eq' | 'lte' | 'lt';

  let dataset = $state<Dataset | null>(null);
  let versions = $state<DatasetVersion[]>([]);
  let branches = $state<DatasetBranch[]>([]);
  let users = $state<UserProfile[]>([]);
  let quality = $state<DatasetQualityResponse | null>(null);
  let lint = $state<DatasetLintResponse | null>(null);
  let loading = $state(true);
  // Legacy inner-tab key. Now derived from the new viewMode +
  // detailsPanel + healthSubTab so the existing Quality/Linter/
  // Retention/Versions/Branches templates below keep rendering with
  // zero changes.
  // (Initial value below; the $derived runs after viewMode/etc are declared.)

  // ── T5 — Foundry-style top-level view tabs ─────────────────────
  let viewMode = $state<'preview' | 'history' | 'details' | 'health' | 'compare'>('details');
  let detailsPanel = $state<
    'about' | 'schema' | 'files' | 'jobspec' | 'sync' | 'custom' | 'current_tx' | 'resources' | 'retention'
  >('about');
  let healthSubTab = $state<'quality' | 'linter'>('quality');

  // History / current-transaction data.
  let transactions = $state<DatasetTransaction[]>([]);
  let transactionsLoading = $state(false);
  let transactionsError = $state('');
  let rollingBack = $state<string | null>(null);

  // Files / filesystem listing.
  let filesystemEntries = $state<DatasetFilesystemEntry[]>([]);
  let filesystemLoading = $state(false);
  let filesystemError = $state('');

  // Custom metadata (placeholder until the catalog service exposes it).
  let customMetadata = $state<Record<string, string>>({});
  let savingCustomMetadata = $state(false);
  let customMetadataError = $state('');

  // Preview tab state.
  let previewLoading = $state(false);
  let previewError = $state('');
  let previewResponse = $state<DatasetPreviewResponse | null>(null);
  let selectedTxId = $state<string | null>(null);

  // Compare state.
  type CompareSelector = { kind: 'transaction' | 'branch'; value: string };
  let compareSelectorA = $state<CompareSelector>({ kind: 'branch', value: '' });
  let compareSelectorB = $state<CompareSelector>({ kind: 'branch', value: '' });
  let compareSideA = $state<CompareSide | null>(null);
  let compareSideB = $state<CompareSide | null>(null);
  let compareLoading = $state(false);
  let compareError = $state('');

  // Derived legacy `activeTab` so the existing in-page templates for
  // Quality / Linter / Retention / Versions / Branches / Schema render
  // unchanged based on the new top-level viewMode + sub-tabs.
  const activeTab = $derived<
    'schema' | 'versions' | 'branches' | 'preview' | 'quality' | 'linter' | 'retention'
  >(
    viewMode === 'health'
      ? healthSubTab
      : viewMode === 'history'
        ? 'versions'
        : viewMode === 'preview'
          ? 'preview'
          : viewMode === 'compare'
            ? 'preview'
            : detailsPanel === 'schema'
              ? 'schema'
              : detailsPanel === 'retention'
                ? 'retention'
                : 'preview',
  );
  let descriptionInput = $state('');
  let tagsInput = $state('');
  let ownerId = $state('');
  let metadataError = $state('');
  let qualityError = $state('');
  let lintError = $state('');
  let branchError = $state('');
  let savingMetadata = $state(false);
  let refreshingQuality = $state(false);
  let refreshingLint = $state(false);
  let creatingBranch = $state(false);
  let checkingOutBranch = $state('');
  let savingRule = $state(false);
  let ruleFormMode = $state<'create' | 'edit'>('create');
  let editingRuleId = $state<string | null>(null);
  let branchName = $state('feature-experiment');
  let branchDescription = $state('');
  let branchSourceVersion = $state('');
  let ruleName = $state('Completeness threshold');
  let ruleType = $state<RuleType>('null_check');
  let ruleSeverity = $state<RuleSeverity>('medium');
  let ruleEnabled = $state(true);
  let columnName = $state('');
  let maxNullRatio = $state('5');
  let rangeMin = $state('');
  let rangeMax = $state('');
  let regexPattern = $state('');
  let regexAllowNulls = $state(true);
  let customSql = $state('SELECT COUNT(*) AS value FROM dataset');
  let comparisonOperator = $state<RuleOperator>('gte');
  let threshold = $state('1');

  // ── T4.4 — retention policies (controlled by this page) ───────────
  let retentionPolicies = $state<RetentionPolicy[]>([]);
  let retentionRelevantOnly = $state(true);
  let retentionLoading = $state(false);
  let retentionError = $state('');

  async function fetchRetentionPolicies(rid: string, relevantOnly: boolean) {
    retentionLoading = true;
    retentionError = '';
    try {
      const url = new URL('/api/v1/policies', window.location.origin);
      if (relevantOnly) url.searchParams.set('dataset_rid', rid);
      url.searchParams.set('active', 'true');
      const response = await fetch(url.toString(), { credentials: 'include' });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      retentionPolicies = (await response.json()) as RetentionPolicy[];
    } catch (err) {
      retentionError = err instanceof Error ? err.message : String(err);
      retentionPolicies = [];
    } finally {
      retentionLoading = false;
    }
  }

  function onToggleRetentionRelevantOnly(next: boolean) {
    retentionRelevantOnly = next;
    if (datasetId) void fetchRetentionPolicies(datasetId, next);
  }

  $effect(() => {
    if (activeTab === 'retention' && datasetId) {
      void fetchRetentionPolicies(datasetId, retentionRelevantOnly);
    }
  });

  // ── T5 — load helpers for the new view tabs ─────────────────────
  async function fetchTransactions(rid: string) {
    transactionsLoading = true;
    transactionsError = '';
    try {
      transactions = await listDatasetTransactions(rid);
    } catch (cause) {
      transactionsError = cause instanceof Error ? cause.message : String(cause);
      transactions = [];
    } finally {
      transactionsLoading = false;
    }
  }

  async function fetchFilesystem(rid: string) {
    filesystemLoading = true;
    filesystemError = '';
    try {
      const res = await listDatasetFilesystem(rid);
      filesystemEntries = res.entries ?? res.items ?? [];
    } catch (cause) {
      filesystemError = cause instanceof Error ? cause.message : String(cause);
      filesystemEntries = [];
    } finally {
      filesystemLoading = false;
    }
  }

  async function fetchPreview(rid: string) {
    previewLoading = true;
    previewError = '';
    try {
      previewResponse = await previewDataset(rid, { limit: 5000 });
    } catch (cause) {
      previewError = cause instanceof Error ? cause.message : String(cause);
      previewResponse = null;
    } finally {
      previewLoading = false;
    }
  }

  function rollbackTransaction(tx: DatasetTransaction) {
    // T5.3 — Backend endpoint not yet wired. Surface the intent so
    // operators know what would happen and so the UI can be hooked up
    // once the dataset-versioning-service exposes the route.
    rollingBack = tx.id;
    setTimeout(() => {
      rollingBack = null;
      transactionsError =
        'Rollback API not yet implemented in dataset-versioning-service. ' +
        `Would create a new SNAPSHOT from transaction ${tx.id}.`;
    }, 250);
  }

  // Auto-fetch when the user enters a tab that needs the data.
  $effect(() => {
    if (!datasetId) return;
    if (viewMode === 'history' && transactions.length === 0 && !transactionsLoading) {
      void fetchTransactions(datasetId);
    }
    if (viewMode === 'details' && detailsPanel === 'files' && filesystemEntries.length === 0 && !filesystemLoading) {
      void fetchFilesystem(datasetId);
    }
    if (viewMode === 'details' && detailsPanel === 'current_tx' && transactions.length === 0 && !transactionsLoading) {
      void fetchTransactions(datasetId);
    }
    if (viewMode === 'preview' && !previewResponse && !previewLoading) {
      void fetchPreview(datasetId);
    }
    if (viewMode === 'compare' && transactions.length === 0 && !transactionsLoading) {
      void fetchTransactions(datasetId);
    }
  });

  // Derive Compare sides from currently loaded data. This is a UI-only
  // diff against what we already have client-side; a true point-in-time
  // fetch will land when the dataset-versioning-service exposes it.
  function buildSideFromCurrent(label: string): CompareSide {
    return {
      label,
      schema: previewSchema,
      files: filesystemEntries,
    };
  }

  function changeCompareSelector(which: 'A' | 'B', selector: CompareSelector) {
    if (which === 'A') {
      compareSelectorA = selector;
      compareSideA = buildSideFromCurrent(`${selector.kind}: ${selector.value || '—'}`);
    } else {
      compareSelectorB = selector;
      compareSideB = buildSideFromCurrent(`${selector.kind}: ${selector.value || '—'}`);
    }
  }

  // Derive a SchemaField[] from the currently loaded quality profile so
  // the SchemaPanel and CompareTab have something to render today.
  const previewSchema = $derived<SchemaField[]>(
    (quality?.profile?.columns ?? []).map((c) => ({
      name: c.name,
      type: (c.field_type ?? 'STRING').toUpperCase(),
      nullable: c.nullable ?? true,
    })),
  );

  // Derive ColumnDef[] for the virtualized preview from whichever
  // schema source has data first (preview response → quality columns).
  const previewColumns = $derived<PreviewColumnDef[]>(
    (previewResponse?.columns?.map((c) => ({
      name: c.name,
      field_type: c.field_type ?? c.data_type,
    })) ??
      quality?.profile?.columns?.map((c) => ({
        name: c.name,
        field_type: c.field_type,
      })) ??
      []) as PreviewColumnDef[],
  );

  const previewRows = $derived<Array<Record<string, unknown>>>(
    previewResponse?.rows ?? [],
  );

  // Build per-column stats from the quality profile so the preview
  // header strip surfaces min / max / null% / distinct without a
  // separate API call.
  const previewStats = $derived<Record<string, PreviewColumnStats>>(
    Object.fromEntries(
      (quality?.profile?.columns ?? []).map((c) => [
        c.name,
        {
          min: c.min_value,
          max: c.max_value,
          null_rate: c.null_rate,
          distinct_count: c.distinct_count,
        },
      ]),
    ),
  );

  function saveCustomMetadata(next: Record<string, string>) {
    // T5.1 — backend store for custom metadata is not yet wired.
    // Persist locally so the panel reflects the latest edits.
    savingCustomMetadata = true;
    customMetadataError = '';
    try {
      customMetadata = next;
    } catch (cause) {
      customMetadataError = cause instanceof Error ? cause.message : String(cause);
    } finally {
      savingCustomMetadata = false;
    }
  }

  const datasetId = $derived($page.params.id ?? '');

  function normalizeRuleType(value: string): RuleType {
    if (value === 'range' || value === 'regex' || value === 'custom_sql') return value;
    return 'null_check';
  }

  function normalizeRuleSeverity(value: string): RuleSeverity {
    if (value === 'low' || value === 'high') return value;
    return 'medium';
  }

  function normalizeRuleOperator(value: string): RuleOperator {
    if (value === 'gt' || value === 'eq' || value === 'lte' || value === 'lt') return value;
    return 'gte';
  }

  function ownerName(userId: string) {
    return users.find((user) => user.id === userId)?.name ?? userId.slice(0, 8);
  }

  function toneFor(score: number | null) {
    if (score === null) return 'text-gray-500';
    if (score >= 90) return 'text-emerald-600 dark:text-emerald-300';
    if (score >= 75) return 'text-amber-600 dark:text-amber-300';
    return 'text-rose-600 dark:text-rose-300';
  }

  function severityBadge(severity: string) {
    if (severity === 'high') return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300';
    if (severity === 'medium') return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300';
    return 'bg-slate-100 text-slate-700 dark:bg-gray-800 dark:text-gray-300';
  }

  function postureBadge(posture: string) {
    if (posture === 'critical') return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300';
    if (posture === 'optimize') return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300';
    if (posture === 'watch') return 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300';
    return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300';
  }

  function findingsBySeverity(severity: string) {
    return lint?.findings.filter((finding) => finding.severity === severity).length ?? 0;
  }

  function parseTags(value: string) {
    return value.split(',').map((tag) => tag.trim()).filter(Boolean);
  }

  function columns(): DatasetColumnProfile[] {
    return quality?.profile?.columns ?? [];
  }

  function activeAlerts() {
    return quality?.alerts.filter((alert) => alert.status === 'active') ?? [];
  }

  function ruleResultFor(rule: DatasetQualityRule): DatasetRuleResult | undefined {
    return quality?.profile?.rule_results.find((result) => result.rule_id === rule.id);
  }

  async function refreshLintState(datasetIdToLoad: string, silent = false) {
    if (!datasetIdToLoad) return;
    if (!silent) refreshingLint = true;
    lintError = '';
    try {
      lint = await getDatasetLint(datasetIdToLoad);
    } catch (cause) {
      lintError = cause instanceof Error ? cause.message : 'Failed to refresh dataset linter';
    } finally {
      if (!silent) refreshingLint = false;
    }
  }

  function resetRuleForm() {
    ruleFormMode = 'create';
    editingRuleId = null;
    ruleName = 'Completeness threshold';
    ruleType = 'null_check';
    ruleSeverity = 'medium';
    ruleEnabled = true;
    columnName = columns()[0]?.name ?? '';
    maxNullRatio = '5';
    rangeMin = '';
    rangeMax = '';
    regexPattern = '';
    regexAllowNulls = true;
    customSql = 'SELECT COUNT(*) AS value FROM dataset';
    comparisonOperator = 'gte';
    threshold = '1';
  }

  function editRule(rule: DatasetQualityRule) {
    const config = rule.config ?? {};
    ruleFormMode = 'edit';
    editingRuleId = rule.id;
    ruleName = rule.name;
    ruleType = normalizeRuleType(rule.rule_type);
    ruleSeverity = normalizeRuleSeverity(rule.severity);
    ruleEnabled = rule.enabled;
    columnName = typeof config['column'] === 'string' ? String(config['column']) : '';
    maxNullRatio = typeof config['max_null_ratio'] === 'number'
      ? String(Number(config['max_null_ratio']) * 100)
      : '5';
    rangeMin = typeof config['min'] === 'number' ? String(config['min']) : '';
    rangeMax = typeof config['max'] === 'number' ? String(config['max']) : '';
    regexPattern = typeof config['pattern'] === 'string' ? String(config['pattern']) : '';
    regexAllowNulls = typeof config['allow_nulls'] === 'boolean' ? Boolean(config['allow_nulls']) : true;
    customSql = typeof config['sql'] === 'string' ? String(config['sql']) : 'SELECT COUNT(*) AS value FROM dataset';
    comparisonOperator = typeof config['operator'] === 'string'
      ? normalizeRuleOperator(String(config['operator']))
      : 'gte';
    threshold = typeof config['threshold'] === 'number' ? String(config['threshold']) : '1';
  }

  function buildRulePayload(): CreateDatasetQualityRuleParams {
    if (ruleType === 'null_check') {
      return {
        name: ruleName,
        rule_type: ruleType,
        severity: ruleSeverity,
        enabled: ruleEnabled,
        config: {
          column: columnName,
          max_null_ratio: Math.max(Number(maxNullRatio) || 0, 0) / 100,
        },
      };
    }

    if (ruleType === 'range') {
      const config: Record<string, unknown> = { column: columnName };
      if (rangeMin) config.min = Number(rangeMin);
      if (rangeMax) config.max = Number(rangeMax);
      return {
        name: ruleName,
        rule_type: ruleType,
        severity: ruleSeverity,
        enabled: ruleEnabled,
        config,
      };
    }

    if (ruleType === 'regex') {
      return {
        name: ruleName,
        rule_type: ruleType,
        severity: ruleSeverity,
        enabled: ruleEnabled,
        config: {
          column: columnName,
          pattern: regexPattern,
          allow_nulls: regexAllowNulls,
        },
      };
    }

    return {
      name: ruleName,
      rule_type: ruleType,
      severity: ruleSeverity,
      enabled: ruleEnabled,
      config: {
        sql: customSql,
        operator: comparisonOperator,
        threshold: Number(threshold) || 0,
      },
    };
  }

  async function load() {
    loading = true;
    try {
      const [nextDataset, nextVersions, nextBranches, nextUsers, nextQuality, nextLint] = await Promise.all([
        getDataset(datasetId),
        getVersions(datasetId),
        listBranches(datasetId).catch(() => [] as DatasetBranch[]),
        listUsers().catch(() => [] as UserProfile[]),
        getDatasetQuality(datasetId).catch(() => null as DatasetQualityResponse | null),
        getDatasetLint(datasetId).catch(() => null as DatasetLintResponse | null),
      ]);
      dataset = nextDataset;
      versions = nextVersions;
      branches = nextBranches;
      users = nextUsers;
      quality = nextQuality;
      lint = nextLint;
      descriptionInput = nextDataset.description;
      tagsInput = nextDataset.tags.join(', ');
      ownerId = nextDataset.owner_id;
      branchSourceVersion = String(nextDataset.current_version);
      if (!columnName) {
        columnName = nextQuality?.profile?.columns[0]?.name ?? '';
      }
    } catch (cause) {
      console.error('Failed to load dataset', cause);
    } finally {
      loading = false;
    }
  }

  async function saveMetadata() {
    if (!dataset) return;
    savingMetadata = true;
    metadataError = '';
    try {
      dataset = await updateDataset(dataset.id, {
        description: descriptionInput,
        owner_id: ownerId || undefined,
        tags: parseTags(tagsInput),
      });
      await refreshLintState(dataset.id, true);
    } catch (cause) {
      metadataError = cause instanceof Error ? cause.message : 'Failed to save metadata';
    } finally {
      savingMetadata = false;
    }
  }

  async function refreshQuality() {
    if (!dataset) return;
    refreshingQuality = true;
    qualityError = '';
    try {
      quality = await refreshDatasetQualityProfile(dataset.id);
      dataset = await getDataset(dataset.id);
      await refreshLintState(dataset.id, true);
      if (!columnName) {
        columnName = quality.profile?.columns[0]?.name ?? '';
      }
    } catch (cause) {
      qualityError = cause instanceof Error ? cause.message : 'Failed to refresh quality profile';
    } finally {
      refreshingQuality = false;
    }
  }

  async function saveBranch() {
    if (!dataset) return;
    creatingBranch = true;
    branchError = '';
    try {
      const payload: CreateDatasetBranchParams = {
        name: branchName.trim(),
        description: branchDescription.trim() || undefined,
        source_version: branchSourceVersion ? Number(branchSourceVersion) : undefined,
      };
      await createDatasetBranch(dataset.id, payload);
      branches = await listBranches(dataset.id);
      await refreshLintState(dataset.id, true);
      branchName = 'feature-experiment';
      branchDescription = '';
    } catch (cause) {
      branchError = cause instanceof Error ? cause.message : 'Failed to create branch';
    } finally {
      creatingBranch = false;
    }
  }

  async function checkoutBranch(name: string) {
    if (!dataset) return;
    checkingOutBranch = name;
    branchError = '';
    try {
      dataset = await checkoutDatasetBranch(dataset.id, name);
      branches = await listBranches(dataset.id);
      versions = await getVersions(dataset.id);
      await refreshLintState(dataset.id, true);
      branchSourceVersion = String(dataset.current_version);
    } catch (cause) {
      branchError = cause instanceof Error ? cause.message : 'Failed to switch branch';
    } finally {
      checkingOutBranch = '';
    }
  }

  async function saveRule() {
    if (!dataset) return;
    savingRule = true;
    qualityError = '';
    try {
      const payload = buildRulePayload();
      quality = ruleFormMode === 'edit' && editingRuleId
        ? await updateDatasetQualityRule(dataset.id, editingRuleId, payload)
        : await createDatasetQualityRule(dataset.id, payload);
      await refreshLintState(dataset.id, true);
      resetRuleForm();
    } catch (cause) {
      qualityError = cause instanceof Error ? cause.message : 'Failed to save quality rule';
    } finally {
      savingRule = false;
    }
  }

  async function removeRule(ruleId: string) {
    if (!dataset || !confirm('Delete this quality rule?')) return;
    qualityError = '';
    try {
      quality = await deleteDatasetQualityRule(dataset.id, ruleId);
      await refreshLintState(dataset.id, true);
      if (editingRuleId === ruleId) {
        resetRuleForm();
      }
    } catch (cause) {
      qualityError = cause instanceof Error ? cause.message : 'Failed to delete quality rule';
    }
  }

  onMount(() => {
    if (datasetId) {
      void (async () => {
        await load();
        // Deep link: if `?branch=` is set and differs from the loaded
        // active branch, check it out so reloads / shared links land on
        // the right branch view.
        const requested = $page.url.searchParams.get('branch');
        if (dataset && requested && requested !== dataset.active_branch) {
          try {
            checkingOutBranch = requested;
            dataset = await checkoutDatasetBranch(datasetId, requested);
          } catch (cause) {
            console.warn('failed to checkout branch from URL', cause);
          } finally {
            checkingOutBranch = '';
          }
        }
      })();
    }
  });
</script>

{#if loading}
  <div class="py-12 text-center text-gray-500">Loading...</div>
{:else if !dataset}
  <div class="py-12 text-center text-gray-500">Dataset not found</div>
{:else}
  <div class="space-y-6">
    <DatasetHeader
      dataset={dataset}
      branches={branches}
      markings={[]}
      busy={creatingBranch || Boolean(checkingOutBranch)}
      onSwitchBranch={async (name) => {
        checkingOutBranch = name;
        try {
          dataset = await checkoutDatasetBranch(datasetId, name);
          versions = await getVersions(datasetId);
          await refreshLintState(datasetId, true);
        } finally {
          checkingOutBranch = '';
        }
      }}
      onCreateBranch={async ({ name, from, description }) => {
        creatingBranch = true;
        try {
          await createDatasetBranch(datasetId, {
            name,
            description,
            source_version: branches.find((b) => b.name === from)?.version,
          });
          branches = await listBranches(datasetId);
        } finally {
          creatingBranch = false;
        }
      }}
    />

    <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
      <div class="rounded-xl border p-4 dark:border-gray-700">
        <div class="text-sm text-gray-500">Size</div>
        <div class="text-lg font-semibold">{(dataset.size_bytes / 1024).toFixed(1)} KB</div>
      </div>
      <div class="rounded-xl border p-4 dark:border-gray-700">
        <div class="text-sm text-gray-500">Rows</div>
        <div class="text-lg font-semibold">{dataset.row_count.toLocaleString()}</div>
      </div>
      <div class="rounded-xl border p-4 dark:border-gray-700">
        <div class="text-sm text-gray-500">Version</div>
        <div class="text-lg font-semibold">{dataset.current_version}</div>
      </div>
      <div class="rounded-xl border p-4 dark:border-gray-700">
        <div class="text-sm text-gray-500">Quality Score</div>
        <div class={`text-lg font-semibold ${toneFor(quality?.score ?? null)}`}>
          {#if quality?.score !== null && quality?.score !== undefined}
            {quality.score.toFixed(1)}
          {:else}
            --
          {/if}
        </div>
      </div>
    </div>

    <!-- T5 — Foundry-style top-level view tabs. -->
    <div class="border-b dark:border-gray-700">
      <nav class="flex gap-4">
        {#each [
          { key: 'preview', label: 'Preview' },
          { key: 'history', label: 'History' },
          { key: 'details', label: 'Details' },
          { key: 'health', label: 'Health' },
          { key: 'compare', label: 'Compare' },
        ] as tab (tab.key)}
          <button
            class="border-b-2 pb-2 px-1 text-sm font-medium transition-colors"
            class:border-blue-600={viewMode === tab.key}
            class:text-blue-600={viewMode === tab.key}
            class:border-transparent={viewMode !== tab.key}
            onclick={() => (viewMode = tab.key as typeof viewMode)}
          >{tab.label}</button>
        {/each}
      </nav>
    </div>

    {#if viewMode === 'preview'}
      {#if previewLoading}
        <div class="rounded border py-8 text-center text-gray-500 dark:border-gray-700">Loading preview…</div>
      {:else if previewError}
        <div class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">{previewError}</div>
      {:else if previewColumns.length === 0}
        <div class="rounded border py-8 text-center text-gray-500 dark:border-gray-700">
          Data preview will appear after upload and the first quality profile refresh.
        </div>
      {:else}
        <VirtualizedPreviewTable
          columns={previewColumns}
          rows={previewRows}
          stats={previewStats}
          transactions={transactions}
          selectedTransactionId={selectedTxId}
          onSelectTransaction={(txId) => (selectedTxId = txId)}
        />
      {/if}
    {:else if viewMode === 'history'}
      {#if transactionsLoading}
        <div class="rounded border py-8 text-center text-gray-500 dark:border-gray-700">Loading transactions…</div>
      {:else if transactionsError}
        <div class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">{transactionsError}</div>
      {/if}
      <HistoryTimeline
        transactions={transactions}
        rollingBack={rollingBack}
        onView={(tx) => {
          selectedTxId = tx.id;
          viewMode = 'preview';
        }}
        onRollback={rollbackTransaction}
      />
    {:else if viewMode === 'compare'}
      <CompareTab
        transactions={transactions}
        branches={branches}
        sideA={compareSideA}
        sideB={compareSideB}
        selectorA={compareSelectorA}
        selectorB={compareSelectorB}
        loading={compareLoading}
        error={compareError}
        onChangeSelector={changeCompareSelector}
      />
    {:else if viewMode === 'details'}
      <div class="grid gap-6 lg:grid-cols-[220px,1fr]">
        <aside class="rounded-2xl border border-slate-200 bg-white p-3 shadow-sm dark:border-gray-700 dark:bg-gray-900">
          <ul class="space-y-1 text-sm">
            {#each [
              { key: 'about', label: 'About' },
              { key: 'schema', label: 'Schema' },
              { key: 'files', label: 'Files' },
              { key: 'jobspec', label: 'Job spec' },
              { key: 'sync', label: 'Sync status' },
              { key: 'custom', label: 'Custom metadata' },
              { key: 'current_tx', label: 'Current transaction view' },
              { key: 'resources', label: 'Resource usage metrics' },
              { key: 'retention', label: 'Retention policies' },
            ] as item (item.key)}
              <li>
                <button
                  type="button"
                  class="w-full rounded-lg px-3 py-2 text-left transition-colors"
                  class:bg-slate-100={detailsPanel === item.key}
                  class:dark:bg-gray-800={detailsPanel === item.key}
                  class:font-semibold={detailsPanel === item.key}
                  class:hover:bg-slate-50={detailsPanel !== item.key}
                  class:dark:hover:bg-gray-800={detailsPanel !== item.key}
                  onclick={() => (detailsPanel = item.key as typeof detailsPanel)}
                >{item.label}</button>
              </li>
            {/each}
          </ul>
        </aside>
        <div>
          {#if detailsPanel === 'about'}
            <AboutPanel
              dataset={dataset}
              users={users}
              saving={savingMetadata}
              error={metadataError}
              onSave={async ({ description, tags, owner_id }) => {
                descriptionInput = description;
                tagsInput = tags.join(', ');
                ownerId = owner_id;
                await saveMetadata();
              }}
            />
          {:else if detailsPanel === 'schema'}
            <SchemaPanel fields={previewSchema} format={dataset.format} />
          {:else if detailsPanel === 'files'}
            {#if filesystemError}
              <div class="mb-3 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">{filesystemError}</div>
            {/if}
            <FilesPanel
              entries={filesystemEntries}
              currentVersion={dataset.current_version}
              activeBranch={dataset.active_branch}
              loading={filesystemLoading}
            />
          {:else if detailsPanel === 'jobspec'}
            <JobSpecPanel jobSpec={null} />
          {:else if detailsPanel === 'sync'}
            <SyncStatusPanel state={null} />
          {:else if detailsPanel === 'custom'}
            <CustomMetadataPanel
              initial={customMetadata}
              saving={savingCustomMetadata}
              error={customMetadataError}
              onSave={saveCustomMetadata}
            />
          {:else if detailsPanel === 'current_tx'}
            <CurrentTransactionViewPanel
              head={transactions[0] ?? null}
              composedOf={transactions.slice(0, 10)}
              fileCount={filesystemEntries.length}
              totalBytes={dataset.size_bytes}
            />
          {:else if detailsPanel === 'resources'}
            <ResourceUsagePanel
              sizeBytes={dataset.size_bytes}
              fileCount={filesystemEntries.length}
              rowCount={dataset.row_count}
            />
          {:else if detailsPanel === 'retention'}
            {#if retentionError}
              <div class="mb-3 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">
                {retentionError}
              </div>
            {/if}
            <RetentionPoliciesTab
              policies={retentionPolicies}
              relevantOnly={retentionRelevantOnly}
              onToggleRelevantOnly={onToggleRetentionRelevantOnly}
              loading={retentionLoading}
            />
          {/if}
        </div>
      </div>
    {:else if viewMode === 'health'}
      <div class="border-b dark:border-gray-700">
        <nav class="flex gap-4">
          <button
            class="border-b-2 pb-2 px-1 text-sm font-medium transition-colors"
            class:border-blue-600={healthSubTab === 'quality'}
            class:text-blue-600={healthSubTab === 'quality'}
            class:border-transparent={healthSubTab !== 'quality'}
            onclick={() => (healthSubTab = 'quality')}
          >Quality</button>
          <button
            class="border-b-2 pb-2 px-1 text-sm font-medium transition-colors"
            class:border-blue-600={healthSubTab === 'linter'}
            class:text-blue-600={healthSubTab === 'linter'}
            class:border-transparent={healthSubTab !== 'linter'}
            onclick={() => (healthSubTab = 'linter')}
          >Linter{#if lint?.summary.total_findings} ({lint.summary.total_findings}){/if}</button>
        </nav>
      </div>

    {#if false}
      <span class="hidden">unreachable: keeps the legacy chain below well-formed</span>
    {:else if activeTab === 'preview'}
      <div class="hidden"></div>
    {:else if activeTab === 'schema'}
      <div class="hidden"></div>
    {:else if activeTab === 'versions'}
      <div class="space-y-2">
        {#each versions as version (version.id)}
          <div class="flex items-center justify-between rounded border p-3 dark:border-gray-700">
            <div>
              <span class="font-medium">v{version.version}</span>
              <span class="ml-2 text-sm text-gray-500">{version.message || 'No message'}</span>
            </div>
            <div class="text-sm text-gray-500">
              {(version.size_bytes / 1024).toFixed(1)} KB · {new Date(version.created_at).toLocaleDateString()}
            </div>
          </div>
        {/each}
        {#if versions.length === 0}
          <div class="py-4 text-center text-gray-500">No versions yet</div>
        {/if}
      </div>
    {:else if activeTab === 'branches'}
      <div class="grid gap-6 xl:grid-cols-[1.1fr,0.9fr]">
        <div class="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900">
          <div class="flex items-center justify-between">
            <div>
              <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Branch Selector</div>
              <div class="mt-1 text-sm text-gray-500">Switch the active dataset branch or inspect which version each branch points to.</div>
            </div>
            <span class="rounded-full bg-slate-100 px-3 py-1 text-xs font-medium text-slate-700 dark:bg-gray-800 dark:text-gray-200">
              Active {dataset.active_branch}
            </span>
          </div>

          <div class="mt-4 space-y-3">
            {#each branches as branch (branch.id)}
              <div class="rounded-xl border border-slate-200 p-4 dark:border-gray-700">
                <div class="flex items-center justify-between gap-3">
                  <div>
                    <div class="flex items-center gap-2">
                      <div class="font-medium">{branch.name}</div>
                      {#if branch.is_default}
                        <span class="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-600 dark:bg-gray-800 dark:text-gray-300">default</span>
                      {/if}
                    </div>
                    <div class="mt-1 text-sm text-gray-500">{branch.description || 'No description'}</div>
                    <div class="mt-2 text-xs text-gray-500">Version {branch.version} · Updated {new Date(branch.updated_at).toLocaleString()}</div>
                  </div>
                  <button
                    type="button"
                    onclick={() => checkoutBranch(branch.name)}
                    disabled={checkingOutBranch === branch.name || branch.name === dataset.active_branch}
                    class="rounded-xl border border-slate-200 px-3 py-2 text-sm disabled:opacity-50 hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
                  >
                    {branch.name === dataset.active_branch ? 'Current' : checkingOutBranch === branch.name ? 'Switching...' : 'Checkout'}
                  </button>
                </div>
              </div>
            {/each}
          </div>
        </div>

        <div class="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900">
          <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Create Branch</div>
          <div class="mt-1 text-sm text-gray-500">Start from the current or any existing dataset version.</div>

          <div class="mt-4 space-y-4">
            <div>
              <label for="branch-name" class="mb-1 block text-sm font-medium">Branch name</label>
              <input id="branch-name" bind:value={branchName} class="w-full rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800" />
            </div>

            <div>
              <label for="branch-description" class="mb-1 block text-sm font-medium">Description</label>
              <textarea id="branch-description" bind:value={branchDescription} rows="3" class="w-full rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800"></textarea>
            </div>

            <div>
              <label for="branch-source-version" class="mb-1 block text-sm font-medium">Source version</label>
              <select id="branch-source-version" bind:value={branchSourceVersion} class="w-full rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800">
                <option value={String(dataset.current_version)}>Current version ({dataset.current_version})</option>
                {#each versions as version (version.id)}
                  <option value={String(version.version)}>Version {version.version}</option>
                {/each}
              </select>
            </div>

            <button type="button" onclick={saveBranch} disabled={creatingBranch} class="w-full rounded-xl bg-slate-900 px-4 py-2 text-white disabled:opacity-50 dark:bg-white dark:text-slate-900">
              {creatingBranch ? 'Creating...' : 'Create branch'}
            </button>

            {#if branchError}
              <div class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">{branchError}</div>
            {/if}
          </div>
        </div>
      </div>
    {:else if activeTab === 'quality'}
      <div class="space-y-6">
        <div class="flex flex-col gap-3 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Quality Dashboard</div>
            <div class="mt-1 text-sm text-gray-500">Profiling report, score trend, alerts, and rule management.</div>
          </div>
          <button onclick={refreshQuality} disabled={refreshingQuality} class="rounded-xl bg-blue-600 px-4 py-2 text-white disabled:opacity-50 hover:bg-blue-700">
            {refreshingQuality ? 'Refreshing...' : 'Refresh Profile'}
          </button>
        </div>

        {#if qualityError}
          <div class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">{qualityError}</div>
        {/if}

        {#if !quality?.profile}
          <div class="rounded-2xl border border-dashed border-slate-300 px-6 py-10 text-center text-gray-500 dark:border-gray-700">
            Generate the first quality profile after uploading data to unlock profiling, scoring, alerts, and rules.
          </div>
        {:else}
          <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
            <div class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
              <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Quality Score</div>
              <div class={`mt-3 text-3xl font-semibold ${toneFor(quality.score)}`}>{quality.score?.toFixed(1) ?? '--'}</div>
            </div>
            <div class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
              <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Completeness</div>
              <div class="mt-3 text-3xl font-semibold">{(quality.profile.completeness_ratio * 100).toFixed(1)}%</div>
            </div>
            <div class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
              <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Uniqueness</div>
              <div class="mt-3 text-3xl font-semibold">{(quality.profile.uniqueness_ratio * 100).toFixed(1)}%</div>
            </div>
            <div class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
              <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Duplicate Rows</div>
              <div class="mt-3 text-3xl font-semibold">{quality.profile.duplicate_rows.toLocaleString()}</div>
            </div>
          </div>

          <div class="grid gap-6 xl:grid-cols-[1.2fr,0.8fr]">
            <div class="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900">
              <div class="flex items-center justify-between">
                <h2 class="text-lg font-semibold">Quality Trend</h2>
                <div class="text-sm text-gray-500">{quality.history.length} run{quality.history.length === 1 ? '' : 's'}</div>
              </div>
              <div class="mt-4 space-y-3">
                {#each quality.history.slice(-8) as point (point.id)}
                  <div class="grid grid-cols-[96px,1fr,56px] items-center gap-3 text-sm">
                    <div class="text-gray-500">{new Date(point.created_at).toLocaleDateString()}</div>
                    <div class="h-2 rounded-full bg-slate-100 dark:bg-gray-800">
                      <div class="h-2 rounded-full bg-blue-500" style={`width:${Math.max(point.score, 4)}%`}></div>
                    </div>
                    <div class="text-right font-medium">{point.score.toFixed(1)}</div>
                  </div>
                {/each}
              </div>
            </div>

            <div class="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900">
              <h2 class="text-lg font-semibold">Active Alerts</h2>
              <div class="mt-4 space-y-3">
                {#each activeAlerts() as alert (alert.id)}
                  <div class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">
                    <div class="font-medium uppercase tracking-[0.16em]">{alert.level}</div>
                    <div class="mt-1">{alert.message}</div>
                  </div>
                {/each}
                {#if activeAlerts().length === 0}
                  <div class="rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700 dark:border-emerald-900/40 dark:bg-emerald-950/40 dark:text-emerald-300">
                    No active alerts on the latest quality run.
                  </div>
                {/if}
              </div>
            </div>
          </div>

          <div class="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900">
            <div class="flex items-center justify-between">
              <h2 class="text-lg font-semibold">Column Profiling</h2>
              <div class="text-sm text-gray-500">{quality.profile.column_count} columns</div>
            </div>
            <div class="mt-4 overflow-x-auto">
              <table class="min-w-full divide-y divide-slate-200 text-sm dark:divide-gray-700">
                <thead>
                  <tr class="text-left text-gray-500">
                    <th class="pb-3 pr-4 font-medium">Column</th>
                    <th class="pb-3 pr-4 font-medium">Type</th>
                    <th class="pb-3 pr-4 font-medium">Null Rate</th>
                    <th class="pb-3 pr-4 font-medium">Uniqueness</th>
                    <th class="pb-3 pr-4 font-medium">Distribution</th>
                    <th class="pb-3 pr-4 font-medium">Min / Max / Avg</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-slate-100 dark:divide-gray-800">
                  {#each quality.profile.columns as column (column.name)}
                    <tr>
                      <td class="py-3 pr-4 font-medium">{column.name}</td>
                      <td class="py-3 pr-4 text-gray-500">{column.field_type}</td>
                      <td class="py-3 pr-4">{(column.null_rate * 100).toFixed(1)}%</td>
                      <td class="py-3 pr-4">{(column.uniqueness_rate * 100).toFixed(1)}%</td>
                      <td class="py-3 pr-4 text-gray-500">
                        {#if column.sample_values.length > 0}
                          {column.sample_values.map((sample) => `${sample.value} (${sample.count})`).join(', ')}
                        {:else}
                          --
                        {/if}
                      </td>
                      <td class="py-3 pr-4 text-gray-500">
                        {column.min_value ?? '--'} / {column.max_value ?? '--'} / {column.average_value !== null ? column.average_value.toFixed(2) : '--'}
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          </div>
        {/if}

        <div class="grid gap-6 xl:grid-cols-[0.95fr,1.05fr]">
          <div class="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900">
            <div class="flex items-center justify-between">
              <h2 class="text-lg font-semibold">Rule Builder</h2>
              {#if ruleFormMode === 'edit'}
                <button onclick={resetRuleForm} class="text-sm text-gray-500 hover:text-gray-700">Cancel edit</button>
              {/if}
            </div>

            <div class="mt-4 grid gap-4">
              <input bind:value={ruleName} placeholder="Rule name" class="rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800" />

              <div class="grid gap-4 md:grid-cols-3">
                <select bind:value={ruleType} class="rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800">
                  <option value="null_check">Null check</option>
                  <option value="range">Range</option>
                  <option value="regex">Regex</option>
                  <option value="custom_sql">Custom SQL</option>
                </select>
                <select bind:value={ruleSeverity} class="rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800">
                  <option value="low">Low</option>
                  <option value="medium">Medium</option>
                  <option value="high">High</option>
                </select>
                <label class="flex items-center gap-2 rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700">
                  <input type="checkbox" bind:checked={ruleEnabled} />
                  <span>Enabled</span>
                </label>
              </div>

              <datalist id="quality-columns">
                {#each columns() as column (column.name)}
                  <option value={column.name}></option>
                {/each}
              </datalist>

              {#if ruleType === 'null_check'}
                <div class="grid gap-4 md:grid-cols-2">
                  <input bind:value={columnName} list="quality-columns" placeholder="Column name" class="rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800" />
                  <input bind:value={maxNullRatio} type="number" min="0" max="100" placeholder="Max null %" class="rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800" />
                </div>
              {:else if ruleType === 'range'}
                <div class="grid gap-4 md:grid-cols-3">
                  <input bind:value={columnName} list="quality-columns" placeholder="Column name" class="rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800" />
                  <input bind:value={rangeMin} type="number" placeholder="Min" class="rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800" />
                  <input bind:value={rangeMax} type="number" placeholder="Max" class="rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800" />
                </div>
              {:else if ruleType === 'regex'}
                <div class="grid gap-4 md:grid-cols-[1fr,1fr,auto]">
                  <input bind:value={columnName} list="quality-columns" placeholder="Column name" class="rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800" />
                  <input bind:value={regexPattern} placeholder="Regex pattern" class="rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800" />
                  <label class="flex items-center gap-2 rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700">
                    <input type="checkbox" bind:checked={regexAllowNulls} />
                    <span>Allow nulls</span>
                  </label>
                </div>
              {:else}
                <div class="grid gap-4">
                  <textarea bind:value={customSql} rows="4" class="rounded-xl border border-slate-200 px-3 py-2 font-mono text-sm dark:border-gray-700 dark:bg-gray-800"></textarea>
                  <div class="grid gap-4 md:grid-cols-2">
                    <select bind:value={comparisonOperator} class="rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800">
                      <option value="gt">&gt;</option>
                      <option value="gte">&gt;=</option>
                      <option value="eq">=</option>
                      <option value="lte">&lt;=</option>
                      <option value="lt">&lt;</option>
                    </select>
                    <input bind:value={threshold} type="number" placeholder="Threshold" class="rounded-xl border border-slate-200 px-3 py-2 dark:border-gray-700 dark:bg-gray-800" />
                  </div>
                </div>
              {/if}

              <button onclick={saveRule} disabled={savingRule} class="rounded-xl bg-slate-900 px-4 py-2 text-white disabled:opacity-50 dark:bg-white dark:text-slate-900">
                {savingRule ? 'Saving...' : ruleFormMode === 'edit' ? 'Update Rule' : 'Add Rule'}
              </button>
            </div>
          </div>

          <div class="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900">
            <h2 class="text-lg font-semibold">Rules</h2>
            <div class="mt-4 space-y-3">
              {#each quality?.rules ?? [] as rule (rule.id)}
                {@const result = ruleResultFor(rule)}
                <div class="rounded-xl border border-slate-200 p-4 dark:border-gray-700">
                  <div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                    <div class="space-y-2">
                      <div class="flex flex-wrap items-center gap-2">
                        <div class="font-medium">{rule.name}</div>
                        <span class="rounded-full bg-slate-100 px-2.5 py-1 text-xs font-medium text-slate-700 dark:bg-gray-800 dark:text-gray-300">{rule.rule_type}</span>
                        <span class="rounded-full bg-slate-100 px-2.5 py-1 text-xs font-medium text-slate-700 dark:bg-gray-800 dark:text-gray-300">{rule.severity}</span>
                        <span class={`rounded-full px-2.5 py-1 text-xs font-medium ${result?.passed ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300' : 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300'}`}>
                          {result?.passed ? 'Passing' : 'Failing'}
                        </span>
                      </div>
                      <div class="text-sm text-gray-500">{JSON.stringify(rule.config)}</div>
                      {#if result}
                        <div class="text-sm text-gray-500">{result.message}{result.measured_value ? ` (${result.measured_value})` : ''}</div>
                      {/if}
                    </div>

                    <div class="flex gap-2">
                      <button onclick={() => editRule(rule)} class="rounded-xl border border-slate-200 px-3 py-2 text-sm hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800">Edit</button>
                      <button onclick={() => removeRule(rule.id)} class="rounded-xl border border-rose-200 px-3 py-2 text-sm text-rose-600 hover:bg-rose-50 dark:border-rose-900/40 dark:hover:bg-rose-950/30">Delete</button>
                    </div>
                  </div>
                </div>
              {/each}

              {#if !quality || quality.rules.length === 0}
                <div class="rounded-xl border border-dashed border-slate-300 px-4 py-6 text-center text-sm text-gray-500 dark:border-gray-700">
                  No quality rules yet.
                </div>
              {/if}
            </div>
          </div>
        </div>
      </div>
    {:else if activeTab === 'retention'}
      {#if retentionError}
        <div class="mb-3 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">
          {retentionError}
        </div>
      {/if}
      <RetentionPoliciesTab
        policies={retentionPolicies}
        relevantOnly={retentionRelevantOnly}
        onToggleRelevantOnly={onToggleRetentionRelevantOnly}
        loading={retentionLoading}
      />
    {:else}
      <div class="space-y-6">
        <div class="flex flex-col gap-3 rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Dataset Linter</div>
            <div class="mt-1 text-sm text-gray-500">Anti-pattern analysis for filesystem layout, enrollment churn, quality coverage, and derived artifact pressure.</div>
          </div>
          <button onclick={() => refreshLintState(datasetId)} disabled={refreshingLint} class="rounded-xl bg-slate-900 px-4 py-2 text-sm text-white disabled:opacity-50 dark:bg-white dark:text-slate-900">
            {refreshingLint ? 'Refreshing...' : 'Refresh Analysis'}
          </button>
        </div>

        {#if lintError}
          <div class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300">{lintError}</div>
        {/if}

        {#if !lint}
          <div class="rounded-2xl border border-dashed border-slate-300 px-6 py-10 text-center text-gray-500 dark:border-gray-700">
            The dataset linter could not load yet.
          </div>
        {:else}
          <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
            <div class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
              <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Resource Posture</div>
              <div class="mt-3 flex items-center gap-3">
                <span class={`rounded-full px-3 py-1 text-xs font-semibold uppercase tracking-[0.16em] ${postureBadge(lint.summary.resource_posture)}`}>
                  {lint.summary.resource_posture}
                </span>
                <span class="text-sm text-gray-500">{new Date(lint.analyzed_at).toLocaleString()}</span>
              </div>
            </div>
            <div class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
              <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Findings</div>
              <div class="mt-3 text-3xl font-semibold">{lint.summary.total_findings}</div>
              <div class="mt-2 flex flex-wrap gap-2 text-xs text-gray-500">
                <span>{findingsBySeverity('high')} high</span>
                <span>{findingsBySeverity('medium')} medium</span>
                <span>{findingsBySeverity('low')} low</span>
              </div>
            </div>
            <div class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
              <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Filesystem Layout</div>
              <div class="mt-3 text-3xl font-semibold">{lint.summary.object_count}</div>
              <div class="mt-2 text-sm text-gray-500">
                {lint.summary.small_file_count} small files · avg {(lint.summary.average_object_size_bytes / (1024 * 1024)).toFixed(1)} MB
              </div>
            </div>
            <div class="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-gray-700 dark:bg-gray-900">
              <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Lifecycle Pressure</div>
              <div class="mt-3 text-3xl font-semibold">{lint.summary.tracked_versions}</div>
              <div class="mt-2 text-sm text-gray-500">
                {lint.summary.branch_count} branches · {lint.summary.materialized_view_count} materialized views
              </div>
            </div>
          </div>

          <div class="grid gap-6 xl:grid-cols-[1.15fr,0.85fr]">
            <div class="space-y-4">
              <div class="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900">
                <div class="flex items-center justify-between">
                  <h2 class="text-lg font-semibold">Findings</h2>
                  <div class="text-sm text-gray-500">
                    {lint.summary.enabled_rule_count} quality rules · {lint.summary.active_alert_count} active alerts
                  </div>
                </div>

                <div class="mt-4 space-y-4">
                  {#if lint.findings.length === 0}
                    <div class="rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-4 text-sm text-emerald-700 dark:border-emerald-900/40 dark:bg-emerald-950/40 dark:text-emerald-300">
                      No anti-patterns detected from the current dataset metadata, filesystem layout, quality state, and transaction history.
                    </div>
                  {:else}
                    {#each lint.findings as finding (finding.code)}
                      <div class="rounded-xl border border-slate-200 p-4 dark:border-gray-700">
                        <div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                          <div class="space-y-3">
                            <div class="flex flex-wrap items-center gap-2">
                              <div class="font-medium">{finding.title}</div>
                              <span class={`rounded-full px-2.5 py-1 text-xs font-medium uppercase tracking-[0.12em] ${severityBadge(finding.severity)}`}>{finding.severity}</span>
                              <span class="rounded-full bg-slate-100 px-2.5 py-1 text-xs font-medium text-slate-700 dark:bg-gray-800 dark:text-gray-300">{finding.category}</span>
                            </div>
                            <div class="text-sm text-gray-600 dark:text-gray-300">{finding.description}</div>
                            <div class="text-sm text-gray-500">{finding.impact}</div>
                            {#if finding.evidence.length > 0}
                              <div class="flex flex-wrap gap-2">
                                {#each finding.evidence as signal}
                                  <span class="rounded-full bg-slate-100 px-2.5 py-1 text-xs font-medium text-slate-700 dark:bg-gray-800 dark:text-gray-300">{signal}</span>
                                {/each}
                              </div>
                            {/if}
                          </div>
                        </div>
                      </div>
                    {/each}
                  {/if}
                </div>
              </div>
            </div>

            <div class="space-y-4">
              <div class="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900">
                <h2 class="text-lg font-semibold">Recommendations</h2>
                <div class="mt-4 space-y-3">
                  {#if lint.recommendations.length === 0}
                    <div class="rounded-xl border border-dashed border-slate-300 px-4 py-6 text-center text-sm text-gray-500 dark:border-gray-700">
                      No remediation actions suggested right now.
                    </div>
                  {:else}
                    {#each lint.recommendations as recommendation (recommendation.code)}
                      <div class="rounded-xl border border-slate-200 p-4 dark:border-gray-700">
                        <div class="flex flex-wrap items-center gap-2">
                          <div class="font-medium">{recommendation.title}</div>
                          <span class={`rounded-full px-2.5 py-1 text-xs font-medium uppercase tracking-[0.12em] ${severityBadge(recommendation.priority)}`}>{recommendation.priority}</span>
                        </div>
                        <div class="mt-2 text-sm text-gray-500">{recommendation.rationale}</div>
                        <div class="mt-3 space-y-2">
                          {#each recommendation.actions as action}
                            <div class="rounded-xl border border-dashed border-slate-200 px-3 py-2 text-sm text-slate-600 dark:border-gray-700 dark:text-gray-300">
                              {action}
                            </div>
                          {/each}
                        </div>
                      </div>
                    {/each}
                  {/if}
                </div>
              </div>

              <div class="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-gray-700 dark:bg-gray-900">
                <h2 class="text-lg font-semibold">Operational Signals</h2>
                <div class="mt-4 space-y-3 text-sm text-gray-500">
                  <div class="flex items-center justify-between">
                    <span>Transactions</span>
                    <span>{lint.summary.transaction_count}</span>
                  </div>
                  <div class="flex items-center justify-between">
                    <span>Pending transactions</span>
                    <span>{lint.summary.pending_transaction_count}</span>
                  </div>
                  <div class="flex items-center justify-between">
                    <span>Failed transactions</span>
                    <span>{lint.summary.failed_transaction_count}</span>
                  </div>
                  <div class="flex items-center justify-between">
                    <span>Stale branches</span>
                    <span>{lint.summary.stale_branch_count}</span>
                  </div>
                  <div class="flex items-center justify-between">
                    <span>Auto-refresh views</span>
                    <span>{lint.summary.auto_refresh_view_count}</span>
                  </div>
                  <div class="flex items-center justify-between">
                    <span>Largest object</span>
                    <span>{(lint.summary.largest_object_bytes / (1024 * 1024)).toFixed(1)} MB</span>
                  </div>
                  <div class="flex items-center justify-between">
                    <span>Quality score</span>
                    <span class={toneFor(lint.summary.quality_score)}>{lint.summary.quality_score?.toFixed(1) ?? '--'}</span>
                  </div>
                </div>
              </div>
            </div>
          </div>
        {/if}
      </div>
    {/if}
    {/if}
  </div>
{/if}

