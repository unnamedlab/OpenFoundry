<script lang="ts">
  import { browser } from '$app/environment';
  import { onMount } from 'svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import {
    createFunctionPackage,
    deleteFunctionPackage,
    getFunctionAuthoringSurface,
    getFunctionPackageMetrics,
    listFunctionPackageRuns,
    listFunctionPackages,
    listObjectTypes,
    listObjects,
    simulateFunctionPackage,
    updateFunctionPackage,
    validateFunctionPackage,
    type FunctionAuthoringSurface,
    type FunctionAuthoringTemplate,
    type FunctionCapabilities,
    type FunctionPackage,
    type FunctionPackageMetrics,
    type FunctionPackageRun,
    type FunctionSdkPackageReference,
    type ObjectInstance,
    type ObjectType
  } from '$lib/api/ontology';

  type WorkbenchTab = 'authoring' | 'testing' | 'release' | 'monitoring' | 'consumption';
  type DependencyMode = 'pin_by_id' | 'version_range' | 'auto_upgrade_same_major';
  type ReleaseChannel = 'dev' | 'staging' | 'production';
  type ReleaseStatus = 'draft' | 'approved' | 'published';
  type RuntimeKind = 'all' | 'typescript' | 'python';

  interface LocalReleaseRecord {
    id: string;
    package_name: string;
    package_version: string;
    channel: ReleaseChannel;
    dependency_mode: DependencyMode;
    status: ReleaseStatus;
    marketplace_product: string;
    api_slug: string;
    notes: string;
    published_at: string;
  }

  interface LocalTestScenario {
    id: string;
    package_id: string;
    name: string;
    object_type_id: string;
    target_object_id: string;
    parameters: Record<string, unknown>;
    justification: string;
    expected_mode: 'validate' | 'simulate';
    last_status: 'idle' | 'success' | 'failure';
    last_run_at: string | null;
    last_result: Record<string, unknown> | null;
  }

  const workbenchTabs: { id: WorkbenchTab; label: string; glyph: 'code' | 'run' | 'history' | 'sparkles' | 'artifact' }[] = [
    { id: 'authoring', label: 'Authoring', glyph: 'code' },
    { id: 'testing', label: 'Testing', glyph: 'run' },
    { id: 'release', label: 'Release', glyph: 'sparkles' },
    { id: 'monitoring', label: 'Monitoring', glyph: 'history' },
    { id: 'consumption', label: 'Consumption', glyph: 'artifact' }
  ];

  const runtimeFilters: { value: RuntimeKind; label: string }[] = [
    { value: 'all', label: 'All runtimes' },
    { value: 'typescript', label: 'TypeScript' },
    { value: 'python', label: 'Python' }
  ];

  const dependencyModes: { value: DependencyMode; label: string; detail: string }[] = [
    {
      value: 'pin_by_id',
      label: 'Pin by id',
      detail: 'Use an exact package id when a single reviewed build must remain immutable.'
    },
    {
      value: 'version_range',
      label: 'Version range',
      detail: 'Reference package name and version so consumers can resolve the intended release.'
    },
    {
      value: 'auto_upgrade_same_major',
      label: 'Auto-upgrade same major',
      detail: 'Allow stable upgrades within the same major stream once a release channel is approved.'
    }
  ];

  const channelOptions: ReleaseChannel[] = ['dev', 'staging', 'production'];
  const releaseStatuses: ReleaseStatus[] = ['draft', 'approved', 'published'];

  const capabilityPresets: {
    id: string;
    label: string;
    description: string;
    value: Partial<FunctionCapabilities>;
  }[] = [
    {
      id: 'read-only-ai',
      label: 'Read-only AI companion',
      description: 'Ontology reads and model access, but no writes or network.',
      value: {
        allow_ontology_read: true,
        allow_ontology_write: false,
        allow_ai: true,
        allow_network: false,
        timeout_seconds: 15,
        max_source_bytes: 65536
      }
    },
    {
      id: 'governed-mutation',
      label: 'Governed mutation',
      description: 'Ontology writes for action-backed edits without external network calls.',
      value: {
        allow_ontology_read: true,
        allow_ontology_write: true,
        allow_ai: false,
        allow_network: false,
        timeout_seconds: 20,
        max_source_bytes: 65536
      }
    },
    {
      id: 'network-orchestrator',
      label: 'Network orchestrator',
      description: 'External APIs enabled for integrations, webhooks, or federated lookups.',
      value: {
        allow_ontology_read: true,
        allow_ontology_write: true,
        allow_ai: false,
        allow_network: true,
        timeout_seconds: 30,
        max_source_bytes: 65536
      }
    }
  ];

  const functionSourceTemplates = {
    typescript: `export default async function handler(context) {
  const target = context.targetObject;
  const related = await context.sdk.ontology.search({
    query: target?.properties?.name ?? 'high risk case',
    kind: 'object_instance',
    limit: 5,
  });

  const summary = context.capabilities.allowAi
    ? await context.llm.complete({
        userMessage: \`Summarize operating posture for \${target?.id ?? 'this selection'}.\`,
        maxTokens: 160,
      })
    : null;

  return {
    output: {
      targetId: target?.id ?? null,
      related,
      summary: summary?.reply ?? null,
    },
  };
}`,
    python: `def handler(context):
    target = context.get("target_object")
    related = context["sdk"].ontology.search(
        query=(target or {}).get("properties", {}).get("name", "high risk case"),
        kind="object_instance",
        limit=5,
    )

    summary = None
    if context["capabilities"].get("allow_ai"):
        summary = context["llm"].complete(
            user_message=f"Summarize operating posture for {(target or {}).get('id', 'this selection')}.",
            max_tokens=160,
        )

    return {
        "output": {
            "targetId": (target or {}).get("id"),
            "related": related,
            "summary": summary,
        }
    }`
  } as const;

  let authoringSurface = $state<FunctionAuthoringSurface | null>(null);
  let packages = $state<FunctionPackage[]>([]);
  let objectTypes = $state<ObjectType[]>([]);
  let objects = $state<ObjectInstance[]>([]);
  let metrics = $state<FunctionPackageMetrics | null>(null);
  let runs = $state<FunctionPackageRun[]>([]);
  let releaseRecords = $state<LocalReleaseRecord[]>([]);
  let testScenarios = $state<LocalTestScenario[]>([]);

  let loading = $state(true);
  let catalogLoading = $state(false);
  let monitoringLoading = $state(false);
  let validationLoading = $state(false);
  let simulationLoading = $state(false);
  let savingPackage = $state(false);
  let deletingPackage = $state(false);

  let pageError = $state('');
  let packageError = $state('');
  let packageSuccess = $state('');
  let runtimeError = $state('');
  let monitoringError = $state('');
  let releaseError = $state('');
  let releaseSuccess = $state('');

  let activeTab = $state<WorkbenchTab>('authoring');
  let runtimeFilter = $state<RuntimeKind>('all');
  let catalogQuery = $state('');
  let selectedPackageId = $state('');
  let selectedTypeId = $state('');
  let selectedObjectId = $state('');
  let selectedRunStatus = $state<'all' | 'success' | 'failure'>('all');
  let selectedInvocationKind = $state<'all' | 'simulation' | 'action'>('all');

  let draftName = $state('');
  let draftVersion = $state('0.1.0');
  let draftDisplayName = $state('');
  let draftDescription = $state('');
  let draftRuntime = $state<'typescript' | 'python'>('typescript');
  let draftEntrypoint = $state<'default' | 'handler'>('default');
  let draftCapabilitiesText = $state(
    JSON.stringify(
      {
        allow_ontology_read: true,
        allow_ontology_write: true,
        allow_ai: true,
        allow_network: false,
        timeout_seconds: 15,
        max_source_bytes: 65536
      },
      null,
      2
    )
  );
  let draftSource = $state<string>(functionSourceTemplates.typescript);

  let invocationParametersText = $state('{\n  "payload": {\n    "query": "customer health"\n  }\n}');
  let invocationJustification = $state('');
  let validationResult = $state<Record<string, unknown> | null>(null);
  let simulationResult = $state<Record<string, unknown> | null>(null);

  let testScenarioName = $state('');
  let releaseChannel = $state<ReleaseChannel>('dev');
  let dependencyMode = $state<DependencyMode>('pin_by_id');
  let releaseStatus = $state<ReleaseStatus>('draft');
  let releaseMarketplaceProduct = $state('Ontology Core');
  let releaseApiSlug = $state('customer-triage');
  let releaseNotes = $state('');

  const selectedPackage = $derived(packages.find((item) => item.id === selectedPackageId) ?? null);
  const selectedType = $derived(objectTypes.find((item) => item.id === selectedTypeId) ?? null);
  const selectedObject = $derived(objects.find((item) => item.id === selectedObjectId) ?? null);
  const filteredPackages = $derived.by(() => {
    const query = catalogQuery.trim().toLowerCase();
    return packages.filter((pkg) => {
      const matchesRuntime = runtimeFilter === 'all' ? true : pkg.runtime === runtimeFilter;
      const haystack = `${pkg.name} ${pkg.display_name} ${pkg.description} ${pkg.runtime} ${pkg.version}`.toLowerCase();
      const matchesQuery = query ? haystack.includes(query) : true;
      return matchesRuntime && matchesQuery;
    });
  });
  const selectedPackageFamily = $derived.by(() => {
    if (!selectedPackage) return [];
    return packages.filter((pkg) => pkg.name === selectedPackage.name);
  });
  const selectedPackageReleases = $derived.by(() => {
    if (!selectedPackage) return [];
    return releaseRecords.filter(
      (record) =>
        record.package_name === selectedPackage.name && record.package_version === selectedPackage.version
    );
  });
  const packageFamilyReleases = $derived.by(() => {
    if (!selectedPackage) return [];
    return releaseRecords.filter((record) => record.package_name === selectedPackage.name);
  });
  const packageTests = $derived.by(() => {
    if (!selectedPackageId) return [];
    return testScenarios.filter((scenario) => scenario.package_id === selectedPackageId);
  });
  const authoringStats = $derived.by(() => [
    {
      label: 'Packages',
      value: packages.length,
      detail: 'Reusable TypeScript and Python functions'
    },
    {
      label: 'Templates',
      value: authoringSurface?.templates.length ?? 0,
      detail: 'Backend-defined authoring kits'
    },
    {
      label: 'SDK packages',
      value: authoringSurface?.sdk_packages.length ?? 0,
      detail: 'Platform SDK references available in authoring'
    },
    {
      label: 'Published releases',
      value: releaseRecords.filter((record) => record.status === 'published').length,
      detail: 'Locally tracked promotion workflow snapshots'
    }
  ]);
  const monitoringInsights = $derived.by(() => {
    if (!metrics) return [];
    const recommendations: string[] = [];

    if ((metrics.avg_duration_ms ?? 0) > 3000) {
      recommendations.push('Average duration is elevated; consider reducing ontology fan-out or moving expensive work behind cached queries.');
    }
    if (metrics.success_rate < 0.9 && metrics.total_runs > 3) {
      recommendations.push('Success rate is below 90%; inspect recent failures and add a unit test scenario for the dominant error path.');
    }
    if (selectedPackage?.capabilities.allow_network) {
      recommendations.push('Network access is enabled; ensure timeouts and retry semantics are explicit before promoting this package to production.');
    }
    if (selectedPackage?.capabilities.allow_ai) {
      recommendations.push('AI access is enabled; validate prompts with representative objects and keep a pinned release record for regulated environments.');
    }
    if (recommendations.length === 0) {
      recommendations.push('This package currently looks healthy; keep validating target object coverage as new releases are published.');
    }

    return recommendations;
  });

  function storageKey(name: string) {
    return `of.functions.${name}`;
  }

  function loadStoredArray<T>(name: string): T[] {
    if (!browser) return [];
    try {
      const raw = window.localStorage.getItem(storageKey(name));
      const parsed = raw ? JSON.parse(raw) : [];
      return Array.isArray(parsed) ? (parsed as T[]) : [];
    } catch {
      return [];
    }
  }

  function persistStoredArray(name: string, value: unknown[]) {
    if (!browser) return;
    window.localStorage.setItem(storageKey(name), JSON.stringify(value));
  }

  function prettyJson(value: unknown) {
    return JSON.stringify(value ?? null, null, 2);
  }

  function parseJsonObject(source: string, label: string): Record<string, unknown> {
    try {
      const parsed = JSON.parse(source);
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        throw new Error('expected a JSON object');
      }
      return parsed as Record<string, unknown>;
    } catch (error) {
      throw new Error(`${label}: ${error instanceof Error ? error.message : 'Invalid JSON'}`);
    }
  }

  function formatTimestamp(value: string | null | undefined) {
    return value ? new Date(value).toLocaleString() : 'Never';
  }

  function formatDuration(value: number | null | undefined) {
    if (value === null || value === undefined) return '—';
    if (value < 1000) return `${value} ms`;
    return `${(value / 1000).toFixed(1)} s`;
  }

  function formatPercent(value: number | null | undefined) {
    if (value === null || value === undefined) return '—';
    return `${(value * 100).toFixed(0)}%`;
  }

  function packageLabel(pkg: FunctionPackage) {
    return `${pkg.display_name} · ${pkg.version}`;
  }

  function objectLabel(instance: ObjectInstance) {
    const props = instance.properties ?? {};
    const candidates = ['name', 'title', 'display_name', 'label', 'code', 'identifier'];
    for (const key of candidates) {
      const value = props[key];
      if (typeof value === 'string' && value.trim()) return value;
    }
    return instance.id;
  }

  function applyTemplate(template: FunctionAuthoringTemplate) {
    draftRuntime = template.runtime === 'python' ? 'python' : 'typescript';
    draftEntrypoint = template.entrypoint === 'handler' ? 'handler' : 'default';
    draftDisplayName = template.display_name;
    draftDescription = template.description;
    draftCapabilitiesText = prettyJson(template.default_capabilities);
    draftSource = template.starter_source;
  }

  function applyCapabilityPreset(presetId: string) {
    const preset = capabilityPresets.find((item) => item.id === presetId);
    if (!preset) return;
    draftCapabilitiesText = prettyJson({
      allow_ontology_read: true,
      allow_ontology_write: true,
      allow_ai: true,
      allow_network: false,
      timeout_seconds: 15,
      max_source_bytes: 65536,
      ...preset.value
    });
  }

  function applyRuntimeStarter(runtime: 'typescript' | 'python') {
    draftRuntime = runtime;
    draftEntrypoint = runtime === 'typescript' ? 'default' : 'handler';
    draftSource = functionSourceTemplates[runtime];
  }

  function resetDraft() {
    draftName = '';
    draftVersion = '0.1.0';
    draftDisplayName = '';
    draftDescription = '';
    draftRuntime = 'typescript';
    draftEntrypoint = 'default';
    draftCapabilitiesText = prettyJson({
      allow_ontology_read: true,
      allow_ontology_write: true,
      allow_ai: true,
      allow_network: false,
      timeout_seconds: 15,
      max_source_bytes: 65536
    });
    draftSource = functionSourceTemplates.typescript;
    selectedPackageId = '';
    packageError = '';
    packageSuccess = '';
  }

  function syncDraftFromPackage(pkg: FunctionPackage | null) {
    if (!pkg) {
      resetDraft();
      return;
    }

    draftName = pkg.name;
    draftVersion = pkg.version;
    draftDisplayName = pkg.display_name;
    draftDescription = pkg.description;
    draftRuntime = pkg.runtime === 'python' ? 'python' : 'typescript';
    draftEntrypoint = pkg.entrypoint === 'handler' ? 'handler' : 'default';
    draftCapabilitiesText = prettyJson(pkg.capabilities);
    draftSource = pkg.source;
    packageError = '';
    packageSuccess = '';
  }

  function currentDependencySnippet(pkg: FunctionPackage | null, mode: DependencyMode) {
    if (!pkg) return '{}';
    if (mode === 'pin_by_id') {
      return prettyJson({ function_package_id: pkg.id });
    }
    if (mode === 'version_range') {
      return prettyJson({
        function_package_name: pkg.name,
        function_package_version: pkg.version
      });
    }
    return prettyJson({
      function_package_name: pkg.name,
      function_package_version: pkg.version,
      function_package_auto_upgrade: true
    });
  }

  async function loadObjectContext(typeId: string) {
    if (!typeId) {
      objects = [];
      selectedObjectId = '';
      return;
    }

    try {
      const response = await listObjects(typeId, { per_page: 50 });
      objects = response.data;
      if (!objects.some((item) => item.id === selectedObjectId)) {
        selectedObjectId = objects[0]?.id ?? '';
      }
    } catch (error) {
      runtimeError = error instanceof Error ? error.message : 'Failed to load objects for function testing';
      objects = [];
      selectedObjectId = '';
    }
  }

  async function loadMonitoring(packageId: string) {
    if (!packageId) {
      metrics = null;
      runs = [];
      monitoringError = '';
      return;
    }

    monitoringLoading = true;
    monitoringError = '';

    try {
      const [nextMetrics, nextRuns] = await Promise.all([
        getFunctionPackageMetrics(packageId),
        listFunctionPackageRuns(packageId, {
          page: 1,
          per_page: 20,
          status: selectedRunStatus === 'all' ? undefined : selectedRunStatus,
          invocation_kind: selectedInvocationKind === 'all' ? undefined : selectedInvocationKind
        })
      ]);
      metrics = nextMetrics;
      runs = nextRuns.data;
    } catch (error) {
      monitoringError = error instanceof Error ? error.message : 'Failed to load function monitoring';
      metrics = null;
      runs = [];
    } finally {
      monitoringLoading = false;
    }
  }

  async function loadPage() {
    loading = true;
    pageError = '';

    try {
      const [nextSurface, nextPackages, nextTypes] = await Promise.all([
        getFunctionAuthoringSurface(),
        listFunctionPackages({ page: 1, per_page: 200 }),
        listObjectTypes({ per_page: 200 })
      ]);

      authoringSurface = nextSurface;
      packages = nextPackages.data;
      objectTypes = nextTypes.data;
      releaseRecords = loadStoredArray<LocalReleaseRecord>('releases');
      testScenarios = loadStoredArray<LocalTestScenario>('tests');

      if (!selectedTypeId) {
        selectedTypeId = objectTypes[0]?.id ?? '';
      }
      await loadObjectContext(selectedTypeId);

      if (packages.length && !selectedPackageId) {
        selectedPackageId = packages[0].id;
        syncDraftFromPackage(packages[0]);
        await loadMonitoring(packages[0].id);
      } else if (selectedPackageId) {
        const pkg = packages.find((item) => item.id === selectedPackageId) ?? null;
        syncDraftFromPackage(pkg);
        await loadMonitoring(selectedPackageId);
      }
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to load Functions';
    } finally {
      loading = false;
    }
  }

  async function selectPackage(packageId: string) {
    selectedPackageId = packageId;
    syncDraftFromPackage(packages.find((item) => item.id === packageId) ?? null);
    runtimeError = '';
    validationResult = null;
    simulationResult = null;
    await loadMonitoring(packageId);
  }

  async function savePackage() {
    packageError = '';
    packageSuccess = '';
    savingPackage = true;

    try {
      const capabilities = parseJsonObject(draftCapabilitiesText, 'Capabilities JSON') as unknown as Partial<FunctionCapabilities>;

      if (selectedPackageId) {
        await updateFunctionPackage(selectedPackageId, {
          display_name: draftDisplayName.trim() || undefined,
          description: draftDescription.trim() || undefined,
          runtime: draftRuntime,
          source: draftSource,
          entrypoint: draftEntrypoint,
          capabilities
        });
        packageSuccess = 'Function package updated.';
      } else {
        if (!draftName.trim()) throw new Error('Package name is required.');
        await createFunctionPackage({
          name: draftName.trim(),
          version: draftVersion.trim() || undefined,
          display_name: draftDisplayName.trim() || undefined,
          description: draftDescription.trim() || undefined,
          runtime: draftRuntime,
          source: draftSource,
          entrypoint: draftEntrypoint,
          capabilities
        });
        packageSuccess = 'Function package created.';
      }

      const response = await listFunctionPackages({ page: 1, per_page: 200 });
      packages = response.data;

      const matchingPackage =
        packages.find(
          (pkg) => pkg.name === draftName.trim() && pkg.version === (draftVersion.trim() || draftVersion)
        ) ?? packages.find((pkg) => pkg.id === selectedPackageId) ?? null;

      if (matchingPackage) {
        selectedPackageId = matchingPackage.id;
        syncDraftFromPackage(matchingPackage);
        await loadMonitoring(matchingPackage.id);
      }
    } catch (error) {
      packageError = error instanceof Error ? error.message : 'Failed to save function package';
    } finally {
      savingPackage = false;
    }
  }

  async function removePackage() {
    if (!selectedPackageId) return;
    if (typeof window !== 'undefined' && !window.confirm('Delete this function package?')) return;

    deletingPackage = true;
    packageError = '';
    packageSuccess = '';

    try {
      await deleteFunctionPackage(selectedPackageId);
      packageSuccess = 'Function package deleted.';
      testScenarios = testScenarios.filter((scenario) => scenario.package_id !== selectedPackageId);
      persistStoredArray('tests', testScenarios);

      const deletedId = selectedPackageId;
      const response = await listFunctionPackages({ page: 1, per_page: 200 });
      packages = response.data;
      selectedPackageId = packages[0]?.id ?? '';
      syncDraftFromPackage(packages.find((pkg) => pkg.id === selectedPackageId) ?? null);
      if (selectedPackageId && selectedPackageId !== deletedId) {
        await loadMonitoring(selectedPackageId);
      } else {
        metrics = null;
        runs = [];
      }
    } catch (error) {
      packageError = error instanceof Error ? error.message : 'Failed to delete function package';
    } finally {
      deletingPackage = false;
    }
  }

  async function runValidation() {
    if (!selectedPackageId) return;
    validationLoading = true;
    runtimeError = '';
    validationResult = null;

    try {
      validationResult = await validateFunctionPackage(selectedPackageId, {
        object_type_id: selectedTypeId || undefined,
        target_object_id: selectedObjectId || undefined,
        parameters: parseJsonObject(invocationParametersText, 'Invocation parameters JSON'),
        justification: invocationJustification.trim() || undefined
      }) as unknown as Record<string, unknown>;
    } catch (error) {
      runtimeError = error instanceof Error ? error.message : 'Failed to validate function package';
    } finally {
      validationLoading = false;
    }
  }

  async function runSimulation() {
    if (!selectedPackageId || !selectedTypeId) return;
    simulationLoading = true;
    runtimeError = '';
    simulationResult = null;

    try {
      simulationResult = await simulateFunctionPackage(selectedPackageId, {
        object_type_id: selectedTypeId,
        target_object_id: selectedObjectId || undefined,
        parameters: parseJsonObject(invocationParametersText, 'Invocation parameters JSON'),
        justification: invocationJustification.trim() || undefined
      }) as unknown as Record<string, unknown>;
      await loadMonitoring(selectedPackageId);
    } catch (error) {
      runtimeError = error instanceof Error ? error.message : 'Failed to simulate function package';
    } finally {
      simulationLoading = false;
    }
  }

  function saveTestScenario() {
    if (!selectedPackageId) return;

    try {
      const nextScenario: LocalTestScenario = {
        id: crypto.randomUUID(),
        package_id: selectedPackageId,
        name: testScenarioName.trim() || `Scenario ${packageTests.length + 1}`,
        object_type_id: selectedTypeId,
        target_object_id: selectedObjectId,
        parameters: parseJsonObject(invocationParametersText, 'Invocation parameters JSON'),
        justification: invocationJustification.trim(),
        expected_mode: 'simulate',
        last_status: 'idle',
        last_run_at: null,
        last_result: null
      };
      testScenarios = [...testScenarios, nextScenario];
      persistStoredArray('tests', testScenarios);
      testScenarioName = '';
    } catch (error) {
      runtimeError = error instanceof Error ? error.message : 'Failed to save test scenario';
    }
  }

  async function runScenario(scenario: LocalTestScenario) {
    if (!selectedPackageId) return;

    try {
      const result =
        scenario.expected_mode === 'validate'
          ? ((await validateFunctionPackage(selectedPackageId, {
              object_type_id: scenario.object_type_id || undefined,
              target_object_id: scenario.target_object_id || undefined,
              parameters: scenario.parameters,
              justification: scenario.justification || undefined
            })) as unknown as Record<string, unknown>)
          : ((await simulateFunctionPackage(selectedPackageId, {
              object_type_id: scenario.object_type_id,
              target_object_id: scenario.target_object_id || undefined,
              parameters: scenario.parameters,
              justification: scenario.justification || undefined
            })) as unknown as Record<string, unknown>);

      testScenarios = testScenarios.map((item) =>
        item.id === scenario.id
          ? {
              ...item,
              last_status: 'success',
              last_run_at: new Date().toISOString(),
              last_result: result
            }
          : item
      );
      persistStoredArray('tests', testScenarios);
      if (scenario.expected_mode === 'simulate') {
        await loadMonitoring(selectedPackageId);
      }
    } catch (error) {
      testScenarios = testScenarios.map((item) =>
        item.id === scenario.id
          ? {
              ...item,
              last_status: 'failure',
              last_run_at: new Date().toISOString(),
              last_result: { error: error instanceof Error ? error.message : 'Unknown error' }
            }
          : item
      );
      persistStoredArray('tests', testScenarios);
    }
  }

  function deleteScenario(id: string) {
    testScenarios = testScenarios.filter((scenario) => scenario.id !== id);
    persistStoredArray('tests', testScenarios);
  }

  function publishRelease() {
    if (!selectedPackage) {
      releaseError = 'Select a function package before publishing a release record.';
      releaseSuccess = '';
      return;
    }

    releaseError = '';
    releaseSuccess = '';

    const record: LocalReleaseRecord = {
      id: crypto.randomUUID(),
      package_name: selectedPackage.name,
      package_version: selectedPackage.version,
      channel: releaseChannel,
      dependency_mode: dependencyMode,
      status: releaseStatus,
      marketplace_product: releaseMarketplaceProduct.trim(),
      api_slug: releaseApiSlug.trim(),
      notes: releaseNotes.trim(),
      published_at: new Date().toISOString()
    };

    releaseRecords = [record, ...releaseRecords];
    persistStoredArray('releases', releaseRecords);
    releaseSuccess = 'Release workflow snapshot saved.';
  }

  function deleteRelease(id: string) {
    releaseRecords = releaseRecords.filter((record) => record.id !== id);
    persistStoredArray('releases', releaseRecords);
  }

  onMount(() => {
    void loadPage();
  });
</script>

<svelte:head>
  <title>OpenFoundry - Functions</title>
</svelte:head>

<div class="mx-auto flex max-w-7xl flex-col gap-6 px-4 py-6">
  <section class="overflow-hidden rounded-[2rem] border border-slate-200 bg-[radial-gradient(circle_at_top_left,_rgba(59,130,246,0.18),_transparent_35%),linear-gradient(135deg,_#f8fafc_0%,_#eef3ff_45%,_#f8fafc_100%)] p-6 shadow-sm">
    <div class="grid gap-6 lg:grid-cols-[1.45fr_1fr]">
      <div class="space-y-4">
        <div class="inline-flex items-center gap-2 rounded-full border border-sky-200 bg-white/80 px-3 py-1 text-xs font-semibold uppercase tracking-[0.24em] text-sky-700">
          <Glyph name="code" size={14} />
          Define Ontologies / Functions
        </div>
        <div class="space-y-3">
          <h1 class="text-3xl font-semibold tracking-tight text-slate-950">Functions</h1>
          <p class="max-w-3xl text-sm leading-6 text-slate-600">
            Build reusable TypeScript and Python function packages with templates, SDK references, validation, simulation, run monitoring, version-aware release workflows, and direct handoff into action types, Workshop, Pipeline Builder, and API-style consumption patterns.
          </p>
        </div>
        <div class="flex flex-wrap gap-3 text-xs text-slate-500">
          <a href="/action-types" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-sky-300 hover:text-sky-700">Action Types</a>
          <a href="/apps" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-sky-300 hover:text-sky-700">Workshop</a>
          <a href="/pipelines" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-sky-300 hover:text-sky-700">Pipelines</a>
          <a href="/ontology" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-sky-300 hover:text-sky-700">Ontology workbench</a>
        </div>
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        {#each authoringStats as stat}
          <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
            <p class="text-xs uppercase tracking-[0.24em] text-slate-400">{stat.label}</p>
            <p class="mt-2 text-3xl font-semibold text-slate-950">{stat.value}</p>
            <p class="mt-1 text-sm text-slate-500">{stat.detail}</p>
          </div>
        {/each}
      </div>
    </div>
  </section>

  {#if pageError}
    <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{pageError}</div>
  {/if}

  {#if loading}
    <div class="rounded-3xl border border-slate-200 bg-white px-5 py-10 text-center text-sm text-slate-500">
      Loading function authoring surfaces...
    </div>
  {:else}
    <div class="grid gap-6 xl:grid-cols-[330px_minmax(0,1fr)]">
      <aside class="space-y-4">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="flex items-center justify-between gap-3">
            <div>
              <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Catalog</p>
              <h2 class="mt-1 text-lg font-semibold text-slate-950">Function inventory</h2>
            </div>
            <button
              class="inline-flex items-center gap-2 rounded-full bg-sky-600 px-4 py-2 text-sm font-medium text-white hover:bg-sky-500"
              onclick={resetDraft}
            >
              <Glyph name="plus" size={16} />
              New
            </button>
          </div>

          <div class="mt-4 space-y-3">
            <label for="functions-runtime-filter" class="block text-xs font-semibold uppercase tracking-[0.18em] text-slate-400">
              Runtime
            </label>
            <select
              id="functions-runtime-filter"
              class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500"
              bind:value={runtimeFilter}
            >
              {#each runtimeFilters as filter}
                <option value={filter.value}>{filter.label}</option>
              {/each}
            </select>

            <label for="functions-search" class="block text-xs font-semibold uppercase tracking-[0.18em] text-slate-400">
              Search
            </label>
            <input
              id="functions-search"
              class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500"
              type="text"
              bind:value={catalogQuery}
              placeholder="Search package name, display name, runtime, version"
            />
          </div>

          <div class="mt-4 space-y-2">
            {#if catalogLoading}
              <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-500">
                Refreshing catalog...
              </div>
            {:else if filteredPackages.length === 0}
              <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-500">
                No function packages match this filter.
              </div>
            {:else}
              {#each filteredPackages as pkg}
                <button
                  class={`w-full rounded-2xl border px-4 py-3 text-left transition ${
                    selectedPackageId === pkg.id
                      ? 'border-sky-400 bg-sky-50'
                      : 'border-slate-200 bg-white hover:border-slate-300'
                  }`}
                  onclick={() => void selectPackage(pkg.id)}
                >
                  <div class="flex items-start justify-between gap-3">
                    <div>
                      <p class="text-sm font-semibold text-slate-950">{pkg.display_name}</p>
                      <p class="mt-1 text-xs font-mono text-slate-500">{pkg.name}</p>
                    </div>
                    <span class="rounded-full bg-slate-100 px-2 py-1 text-[11px] font-medium uppercase tracking-[0.16em] text-slate-600">
                      {pkg.runtime}
                    </span>
                  </div>
                  <p class="mt-2 text-sm text-slate-600">{pkg.description || 'No description provided yet.'}</p>
                  <div class="mt-3 flex flex-wrap gap-2 text-[11px] text-slate-500">
                    <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1 font-mono">{pkg.version}</span>
                    <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1">{pkg.entrypoint}</span>
                    {#if pkg.capabilities.allow_ai}
                      <span class="rounded-full border border-violet-200 bg-violet-50 px-2 py-1 text-violet-700">AI</span>
                    {/if}
                    {#if pkg.capabilities.allow_network}
                      <span class="rounded-full border border-amber-200 bg-amber-50 px-2 py-1 text-amber-700">Network</span>
                    {/if}
                  </div>
                </button>
              {/each}
            {/if}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Feature support</p>
          <div class="mt-4 space-y-3">
            <div class="rounded-2xl border border-slate-200 p-4">
              <p class="text-sm font-semibold text-slate-900">TypeScript</p>
              <p class="mt-1 text-sm text-slate-500">Default entrypoint, platform SDK, AI-enabled companions, governed mutations, and version-aware release patterns.</p>
            </div>
            <div class="rounded-2xl border border-slate-200 p-4">
              <p class="text-sm font-semibold text-slate-900">Python</p>
              <p class="mt-1 text-sm text-slate-500">Handler-based authoring, object inspection, controlled AI analysis, pipeline-style reuse, and local development scaffolds.</p>
            </div>
            <div class="rounded-2xl border border-slate-200 p-4">
              <p class="text-sm font-semibold text-slate-900">Language-agnostic</p>
              <p class="mt-1 text-sm text-slate-500">Notifications, ontology edits, API calls, versioning, published release management, and run telemetry.</p>
            </div>
          </div>
        </section>
      </aside>

      <main class="space-y-4">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-4 shadow-sm">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Workbench</p>
              <h2 class="mt-1 text-xl font-semibold text-slate-950">
                {selectedPackage ? packageLabel(selectedPackage) : 'New function package'}
              </h2>
            </div>
            <div class="flex flex-wrap gap-2">
              {#each workbenchTabs as tab}
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
          </div>
        </section>

        {#if packageError}
          <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{packageError}</div>
        {/if}
        {#if packageSuccess}
          <div class="rounded-3xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">{packageSuccess}</div>
        {/if}

        {#if activeTab === 'authoring'}
          <div class="grid gap-4 2xl:grid-cols-[minmax(0,1.15fr)_400px]">
            <section class="space-y-4 rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              {#if authoringSurface}
                <div class="rounded-3xl border border-slate-200 bg-slate-50 p-4">
                  <div class="flex flex-wrap items-start justify-between gap-3">
                    <div>
                      <p class="text-sm font-semibold text-slate-900">Authoring kits</p>
                      <p class="mt-1 text-sm text-slate-500">Built-in templates, SDK packages, and CLI scaffolds from the backend authoring surface.</p>
                    </div>
                    <span class="rounded-full bg-white px-3 py-1 text-xs font-medium text-slate-600">
                      {authoringSurface.templates.length} templates
                    </span>
                  </div>

                  <div class="mt-4 grid gap-3 xl:grid-cols-3">
                    {#each authoringSurface.templates as template (template.id)}
                      <article class="rounded-2xl border border-slate-200 bg-white p-4">
                        <div class="flex flex-wrap items-center gap-2">
                          <p class="text-sm font-semibold text-slate-900">{template.display_name}</p>
                          <span class="rounded-full bg-slate-100 px-2 py-1 text-[11px] uppercase tracking-[0.16em] text-slate-600">{template.runtime}</span>
                        </div>
                        <p class="mt-2 text-sm text-slate-500">{template.description}</p>
                        <div class="mt-3 flex flex-wrap gap-2">
                          {#each template.recommended_use_cases as useCase}
                            <span class="rounded-full border border-sky-200 bg-sky-50 px-2 py-1 text-[11px] text-sky-700">{useCase}</span>
                          {/each}
                        </div>
                        <div class="mt-4 flex items-center justify-between gap-3 text-xs text-slate-500">
                          <span>Entrypoint: <span class="font-mono">{template.entrypoint}</span></span>
                          <button
                            class="rounded-full border border-sky-300 bg-white px-3 py-1.5 font-medium text-sky-700 hover:bg-sky-50"
                            onclick={() => applyTemplate(template)}
                          >
                            Apply
                          </button>
                        </div>
                      </article>
                    {/each}
                  </div>
                </div>
              {/if}

              <div class="grid gap-4 md:grid-cols-2">
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Package name</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 font-mono text-sm outline-none transition focus:border-sky-500 disabled:bg-slate-100"
                    type="text"
                    bind:value={draftName}
                    disabled={Boolean(selectedPackageId)}
                    placeholder="customer_triage"
                  />
                </label>
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Version</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 font-mono text-sm outline-none transition focus:border-sky-500 disabled:bg-slate-100"
                    type="text"
                    bind:value={draftVersion}
                    disabled={Boolean(selectedPackageId)}
                    placeholder="1.0.0"
                  />
                </label>
              </div>

              <div class="grid gap-4 md:grid-cols-2">
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Display name</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500"
                    type="text"
                    bind:value={draftDisplayName}
                    placeholder="Customer triage"
                  />
                </label>
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Description</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500"
                    type="text"
                    bind:value={draftDescription}
                    placeholder="Reusable object triage and summary workflow"
                  />
                </label>
              </div>

              <div class="grid gap-4 md:grid-cols-[1fr_220px_220px]">
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Runtime</span>
                  <select
                    class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500"
                    bind:value={draftRuntime}
                  >
                    <option value="typescript">typescript</option>
                    <option value="python">python</option>
                  </select>
                </label>
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Entrypoint</span>
                  <select
                    class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500"
                    bind:value={draftEntrypoint}
                  >
                    <option value="default">default</option>
                    <option value="handler">handler</option>
                  </select>
                </label>
                <div class="flex items-end">
                  <button
                    class="w-full rounded-full border border-slate-300 bg-white px-4 py-3 text-sm font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700"
                    onclick={() => applyRuntimeStarter(draftRuntime)}
                  >
                    Load {draftRuntime} starter
                  </button>
                </div>
              </div>

              <div class="rounded-3xl border border-slate-200 p-4">
                <div class="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">Capability presets</p>
                    <p class="mt-1 text-sm text-slate-500">Apply a policy envelope and then fine-tune the JSON if needed.</p>
                  </div>
                </div>
                <div class="mt-4 grid gap-3 md:grid-cols-3">
                  {#each capabilityPresets as preset}
                    <button
                      class="rounded-2xl border border-slate-200 p-4 text-left hover:border-sky-300 hover:bg-sky-50"
                      onclick={() => applyCapabilityPreset(preset.id)}
                    >
                      <p class="text-sm font-semibold text-slate-900">{preset.label}</p>
                      <p class="mt-1 text-sm text-slate-500">{preset.description}</p>
                    </button>
                  {/each}
                </div>
              </div>

              <div class="grid gap-4 xl:grid-cols-2">
                <div class="rounded-3xl border border-slate-200 p-4">
                  <p class="text-sm font-semibold text-slate-900">Capabilities JSON</p>
                  <textarea
                    rows="14"
                    class="mt-4 w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-sky-500"
                    bind:value={draftCapabilitiesText}
                    spellcheck="false"
                  ></textarea>
                </div>
                <div class="rounded-3xl border border-slate-200 p-4">
                  <div class="flex items-center justify-between gap-3">
                    <p class="text-sm font-semibold text-slate-900">Source</p>
                    <button
                      class="rounded-full border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700"
                      onclick={() => applyRuntimeStarter(draftRuntime)}
                    >
                      Reset starter
                    </button>
                  </div>
                  <textarea
                    rows="14"
                    class="mt-4 w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-sky-500"
                    bind:value={draftSource}
                    spellcheck="false"
                  ></textarea>
                </div>
              </div>
            </section>

            <section class="space-y-4">
              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <p class="text-sm font-semibold text-slate-900">SDK references</p>
                <div class="mt-4 space-y-3">
                  {#each authoringSurface?.sdk_packages ?? [] as sdk (sdk.language)}
                    <div class="rounded-2xl border border-slate-200 p-4">
                      <div class="flex items-center gap-2">
                        <p class="text-sm font-semibold text-slate-900">{sdk.language}</p>
                        <span class="rounded-full bg-slate-100 px-2 py-1 text-[11px] font-mono text-slate-600">{sdk.package_name}</span>
                      </div>
                      <p class="mt-2 text-xs font-mono text-slate-500">{sdk.path}</p>
                      <p class="mt-2 text-xs text-slate-500">{sdk.generated_by}</p>
                    </div>
                  {/each}
                </div>
              </div>

              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <p class="text-sm font-semibold text-slate-900">CLI scaffolds</p>
                <div class="mt-4 space-y-3">
                  {#each authoringSurface?.cli_commands ?? [] as command}
                    <pre class="overflow-x-auto rounded-2xl bg-slate-950 p-4 text-[11px] text-slate-100">{command}</pre>
                  {/each}
                </div>
              </div>

              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <div class="flex flex-wrap gap-3">
                  <button
                    class="rounded-full bg-sky-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-sky-500 disabled:cursor-not-allowed disabled:bg-sky-300"
                    onclick={() => void savePackage()}
                    disabled={savingPackage}
                  >
                    {savingPackage ? 'Saving...' : selectedPackageId ? 'Save changes' : 'Create function package'}
                  </button>
                  {#if selectedPackageId}
                    <button
                      class="rounded-full border border-rose-200 bg-rose-50 px-5 py-2.5 text-sm font-medium text-rose-700 hover:border-rose-300 disabled:cursor-not-allowed disabled:opacity-60"
                      onclick={() => void removePackage()}
                      disabled={deletingPackage}
                    >
                      {deletingPackage ? 'Deleting...' : 'Delete package'}
                    </button>
                  {/if}
                </div>
              </div>
            </section>
          </div>
        {:else if activeTab === 'testing'}
          <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_420px]">
            <section class="space-y-4 rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              <div class="grid gap-4 md:grid-cols-2">
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Object type</span>
                  <select
                    class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500"
                    bind:value={selectedTypeId}
                    onchange={() => void loadObjectContext(selectedTypeId)}
                  >
                    {#each objectTypes as typeItem}
                      <option value={typeItem.id}>{typeItem.display_name}</option>
                    {/each}
                  </select>
                </label>
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Target object</span>
                  <select
                    class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500"
                    bind:value={selectedObjectId}
                  >
                    <option value="">No target object</option>
                    {#each objects as objectItem}
                      <option value={objectItem.id}>{objectLabel(objectItem)}</option>
                    {/each}
                  </select>
                </label>
              </div>

              <label class="block space-y-2 text-sm text-slate-700">
                <span class="font-medium">Invocation parameters JSON</span>
                <textarea
                  rows="10"
                  class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-sky-500"
                  bind:value={invocationParametersText}
                  spellcheck="false"
                ></textarea>
              </label>

              <label class="block space-y-2 text-sm text-slate-700">
                <span class="font-medium">Justification</span>
                <textarea
                  rows="3"
                  class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500"
                  bind:value={invocationJustification}
                  placeholder="Why should this function run against the selected object?"
                ></textarea>
              </label>

              <div class="flex flex-wrap gap-3">
                <button
                  class="rounded-full border border-slate-300 bg-white px-4 py-2.5 text-sm font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700 disabled:cursor-not-allowed disabled:opacity-60"
                  onclick={() => void runValidation()}
                  disabled={!selectedPackageId || validationLoading}
                >
                  {validationLoading ? 'Validating...' : 'Validate'}
                </button>
                <button
                  class="rounded-full bg-sky-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-sky-500 disabled:cursor-not-allowed disabled:bg-sky-300"
                  onclick={() => void runSimulation()}
                  disabled={!selectedPackageId || !selectedTypeId || simulationLoading}
                >
                  {simulationLoading ? 'Simulating...' : 'Simulate'}
                </button>
                <button
                  class="rounded-full border border-slate-300 bg-white px-4 py-2.5 text-sm font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700 disabled:cursor-not-allowed disabled:opacity-60"
                  onclick={saveTestScenario}
                  disabled={!selectedPackageId}
                >
                  Save scenario
                </button>
              </div>

              {#if runtimeError}
                <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{runtimeError}</div>
              {/if}

              {#if validationResult}
                <div class="rounded-3xl border border-slate-200 p-4">
                  <p class="text-sm font-semibold text-slate-900">Validation result</p>
                  <pre class="mt-4 overflow-x-auto rounded-2xl bg-slate-950 p-4 text-xs text-slate-100">{prettyJson(validationResult)}</pre>
                </div>
              {/if}

              {#if simulationResult}
                <div class="rounded-3xl border border-slate-200 p-4">
                  <p class="text-sm font-semibold text-slate-900">Simulation result</p>
                  <pre class="mt-4 overflow-x-auto rounded-2xl bg-slate-950 p-4 text-xs text-slate-100">{prettyJson(simulationResult)}</pre>
                </div>
              {/if}
            </section>

            <section class="space-y-4">
              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <div class="flex items-center justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">Unit testing scenarios</p>
                    <p class="mt-1 text-sm text-slate-500">Persist test vectors per package and rerun them against the live simulation path.</p>
                  </div>
                </div>
                <label class="mt-4 block space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Scenario name</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500"
                    type="text"
                    bind:value={testScenarioName}
                    placeholder="Regression: approved customer should stay active"
                  />
                </label>

                <div class="mt-4 space-y-3">
                  {#if packageTests.length === 0}
                    <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-500">
                      No test scenarios saved for this package yet.
                    </div>
                  {:else}
                    {#each packageTests as scenario}
                      <div class="rounded-2xl border border-slate-200 p-4">
                        <div class="flex items-start justify-between gap-3">
                          <div>
                            <p class="text-sm font-semibold text-slate-900">{scenario.name}</p>
                            <p class="mt-1 text-xs text-slate-500">{formatTimestamp(scenario.last_run_at)}</p>
                          </div>
                          <span class={`rounded-full px-2 py-1 text-[11px] uppercase tracking-[0.16em] ${
                            scenario.last_status === 'success'
                              ? 'bg-emerald-100 text-emerald-700'
                              : scenario.last_status === 'failure'
                                ? 'bg-rose-100 text-rose-700'
                                : 'bg-slate-100 text-slate-600'
                          }`}>
                            {scenario.last_status}
                          </span>
                        </div>
                        <p class="mt-3 text-xs font-mono text-slate-500">{prettyJson(scenario.parameters)}</p>
                        <div class="mt-4 flex flex-wrap gap-2">
                          <button
                            class="rounded-full border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700"
                            onclick={() => void runScenario(scenario)}
                          >
                            Run
                          </button>
                          <button
                            class="rounded-full border border-rose-200 bg-rose-50 px-3 py-1.5 text-xs font-medium text-rose-700 hover:border-rose-300"
                            onclick={() => deleteScenario(scenario.id)}
                          >
                            Delete
                          </button>
                        </div>
                      </div>
                    {/each}
                  {/if}
                </div>
              </div>

              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <p class="text-sm font-semibold text-slate-900">Testing playbook</p>
                <div class="mt-4 space-y-3 text-sm leading-6 text-slate-600">
                  <p>Create stub objects by selecting a representative object type and saving scenarios with stable parameter JSON.</p>
                  <p>Mock dates, UUIDs, and user context in the source itself by centralizing helper functions, then keep one saved scenario per edge case.</p>
                  <p>Verify ontology edits by running simulation first and promoting only the release records that pass your saved scenarios.</p>
                </div>
              </div>
            </section>
          </div>
        {:else if activeTab === 'release'}
          <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_380px]">
            <section class="space-y-4 rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              <div class="grid gap-4 md:grid-cols-2">
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Release channel</span>
                  <select
                    class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500"
                    bind:value={releaseChannel}
                  >
                    {#each channelOptions as channel}
                      <option value={channel}>{channel}</option>
                    {/each}
                  </select>
                </label>
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Status</span>
                  <select
                    class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500"
                    bind:value={releaseStatus}
                  >
                    {#each releaseStatuses as status}
                      <option value={status}>{status}</option>
                    {/each}
                  </select>
                </label>
              </div>

              <div class="rounded-3xl border border-slate-200 p-4">
                <p class="text-sm font-semibold text-slate-900">Version range dependencies</p>
                <div class="mt-4 grid gap-3 md:grid-cols-3">
                  {#each dependencyModes as mode}
                    <button
                      class={`rounded-2xl border p-4 text-left ${
                        dependencyMode === mode.value ? 'border-sky-300 bg-sky-50' : 'border-slate-200 bg-white'
                      }`}
                      onclick={() => dependencyMode = mode.value}
                    >
                      <p class="text-sm font-semibold text-slate-900">{mode.label}</p>
                      <p class="mt-1 text-sm text-slate-500">{mode.detail}</p>
                    </button>
                  {/each}
                </div>
              </div>

              <div class="grid gap-4 md:grid-cols-2">
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Marketplace product</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500"
                    type="text"
                    bind:value={releaseMarketplaceProduct}
                    placeholder="Ontology Core"
                  />
                </label>
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">API slug</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 font-mono text-sm outline-none transition focus:border-sky-500"
                    type="text"
                    bind:value={releaseApiSlug}
                    placeholder="customer-triage"
                  />
                </label>
              </div>

              <label class="block space-y-2 text-sm text-slate-700">
                <span class="font-medium">Release notes</span>
                <textarea
                  rows="4"
                  class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500"
                  bind:value={releaseNotes}
                  placeholder="Summarize compatibility changes, rollout posture, and testing evidence."
                ></textarea>
              </label>

              <div class="rounded-3xl border border-slate-200 bg-slate-50 p-4">
                <p class="text-sm font-semibold text-slate-900">Consumption snippet</p>
                <pre class="mt-4 overflow-x-auto rounded-2xl bg-slate-950 p-4 text-xs text-slate-100">{currentDependencySnippet(selectedPackage, dependencyMode)}</pre>
              </div>

              {#if releaseError}
                <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{releaseError}</div>
              {/if}
              {#if releaseSuccess}
                <div class="rounded-3xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">{releaseSuccess}</div>
              {/if}

              <button
                class="rounded-full bg-sky-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-sky-500 disabled:cursor-not-allowed disabled:bg-sky-300"
                onclick={publishRelease}
                disabled={!selectedPackage}
              >
                Save release workflow snapshot
              </button>
            </section>

            <section class="space-y-4">
              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <p class="text-sm font-semibold text-slate-900">Package family</p>
                <div class="mt-4 space-y-3">
                  {#if selectedPackageFamily.length === 0}
                    <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-500">
                      Select a package to inspect its version family.
                    </div>
                  {:else}
                    {#each selectedPackageFamily as pkg}
                      <div class="rounded-2xl border border-slate-200 p-4">
                        <div class="flex items-center justify-between gap-3">
                          <p class="text-sm font-semibold text-slate-900">{pkg.version}</p>
                          <span class="rounded-full bg-slate-100 px-2 py-1 text-[11px] font-medium text-slate-600">{pkg.runtime}</span>
                        </div>
                        <p class="mt-2 text-sm text-slate-500">{pkg.description || 'No description provided.'}</p>
                      </div>
                    {/each}
                  {/if}
                </div>
              </div>

              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <p class="text-sm font-semibold text-slate-900">Published records</p>
                <div class="mt-4 space-y-3">
                  {#if packageFamilyReleases.length === 0}
                    <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-500">
                      No release workflow snapshots have been saved for this package family yet.
                    </div>
                  {:else}
                    {#each packageFamilyReleases as record}
                      <div class="rounded-2xl border border-slate-200 p-4">
                        <div class="flex items-start justify-between gap-3">
                          <div>
                            <p class="text-sm font-semibold text-slate-900">{record.channel} · {record.package_version}</p>
                            <p class="mt-1 text-xs text-slate-500">{record.marketplace_product} · {formatTimestamp(record.published_at)}</p>
                          </div>
                          <button
                            class="rounded-full border border-rose-200 bg-rose-50 px-3 py-1.5 text-xs font-medium text-rose-700 hover:border-rose-300"
                            onclick={() => deleteRelease(record.id)}
                          >
                            Delete
                          </button>
                        </div>
                        <div class="mt-3 flex flex-wrap gap-2 text-[11px] text-slate-500">
                          <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1">{record.status}</span>
                          <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1">{record.dependency_mode}</span>
                          <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1 font-mono">/{record.api_slug}</span>
                        </div>
                        {#if record.notes}
                          <p class="mt-3 text-sm text-slate-500">{record.notes}</p>
                        {/if}
                      </div>
                    {/each}
                  {/if}
                </div>
              </div>
            </section>
          </div>
        {:else if activeTab === 'monitoring'}
          <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_320px]">
            <section class="space-y-4 rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              <div class="flex flex-wrap items-end justify-between gap-4">
                <div class="grid gap-4 md:grid-cols-2">
                  <label class="space-y-2 text-sm text-slate-700">
                    <span class="font-medium">Run status</span>
                    <select
                      class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500"
                      bind:value={selectedRunStatus}
                      onchange={() => void loadMonitoring(selectedPackageId)}
                    >
                      <option value="all">all</option>
                      <option value="success">success</option>
                      <option value="failure">failure</option>
                    </select>
                  </label>
                  <label class="space-y-2 text-sm text-slate-700">
                    <span class="font-medium">Invocation kind</span>
                    <select
                      class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500"
                      bind:value={selectedInvocationKind}
                      onchange={() => void loadMonitoring(selectedPackageId)}
                    >
                      <option value="all">all</option>
                      <option value="simulation">simulation</option>
                      <option value="action">action</option>
                    </select>
                  </label>
                </div>

                <button
                  class="rounded-full border border-slate-300 bg-white px-4 py-2.5 text-sm font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700 disabled:opacity-60"
                  onclick={() => void loadMonitoring(selectedPackageId)}
                  disabled={!selectedPackageId || monitoringLoading}
                >
                  {monitoringLoading ? 'Refreshing...' : 'Refresh monitoring'}
                </button>
              </div>

              {#if monitoringError}
                <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{monitoringError}</div>
              {/if}

              {#if metrics}
                <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                  <div class="rounded-2xl border border-slate-200 p-4">
                    <p class="text-xs uppercase tracking-[0.18em] text-slate-400">Executions</p>
                    <p class="mt-2 text-2xl font-semibold text-slate-950">{metrics.total_runs}</p>
                    <p class="mt-1 text-xs text-slate-500">{metrics.action_runs} action · {metrics.simulation_runs} simulation</p>
                  </div>
                  <div class="rounded-2xl border border-slate-200 p-4">
                    <p class="text-xs uppercase tracking-[0.18em] text-slate-400">Success rate</p>
                    <p class="mt-2 text-2xl font-semibold text-slate-950">{formatPercent(metrics.success_rate)}</p>
                    <p class="mt-1 text-xs text-slate-500">{metrics.successful_runs} success · {metrics.failed_runs} failure</p>
                  </div>
                  <div class="rounded-2xl border border-slate-200 p-4">
                    <p class="text-xs uppercase tracking-[0.18em] text-slate-400">Average duration</p>
                    <p class="mt-2 text-2xl font-semibold text-slate-950">{formatDuration(metrics.avg_duration_ms)}</p>
                    <p class="mt-1 text-xs text-slate-500">P95 {formatDuration(metrics.p95_duration_ms)}</p>
                  </div>
                  <div class="rounded-2xl border border-slate-200 p-4">
                    <p class="text-xs uppercase tracking-[0.18em] text-slate-400">Last run</p>
                    <p class="mt-2 text-sm font-semibold text-slate-950">{formatTimestamp(metrics.last_run_at)}</p>
                    <p class="mt-1 text-xs text-slate-500">Last success {formatTimestamp(metrics.last_success_at)}</p>
                  </div>
                </div>

                {#if runs.length === 0}
                  <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-500">
                    No runs match the current filters.
                  </div>
                {:else}
                  <div class="overflow-x-auto rounded-3xl border border-slate-200">
                    <table class="min-w-full divide-y divide-slate-200 text-left text-sm">
                      <thead class="bg-slate-50 text-xs uppercase tracking-[0.16em] text-slate-500">
                        <tr>
                          <th class="px-4 py-3">When</th>
                          <th class="px-4 py-3">Kind</th>
                          <th class="px-4 py-3">Status</th>
                          <th class="px-4 py-3">Duration</th>
                          <th class="px-4 py-3">Context</th>
                          <th class="px-4 py-3">Error</th>
                        </tr>
                      </thead>
                      <tbody class="divide-y divide-slate-200">
                        {#each runs as run}
                          <tr class="align-top">
                            <td class="px-4 py-3 text-slate-600">{formatTimestamp(run.completed_at)}</td>
                            <td class="px-4 py-3">
                              <span class="rounded-full bg-slate-100 px-2 py-1 text-[11px] font-medium text-slate-600">{run.invocation_kind}</span>
                            </td>
                            <td class="px-4 py-3">
                              <span class={`rounded-full px-2 py-1 text-[11px] font-medium ${
                                run.status === 'success' ? 'bg-emerald-100 text-emerald-700' : 'bg-rose-100 text-rose-700'
                              }`}>
                                {run.status}
                              </span>
                            </td>
                            <td class="px-4 py-3 text-slate-600">{formatDuration(run.duration_ms)}</td>
                            <td class="px-4 py-3 text-xs text-slate-500">
                              <div>{run.action_name ?? 'Standalone simulation'}</div>
                              <div class="mt-1 font-mono">{run.target_object_id ?? 'no target object'}</div>
                            </td>
                            <td class="px-4 py-3 text-xs text-slate-500">{run.error_message ?? '—'}</td>
                          </tr>
                        {/each}
                      </tbody>
                    </table>
                  </div>
                {/if}
              {:else}
                <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-500">
                  Select a package to inspect function monitoring.
                </div>
              {/if}
            </section>

            <section class="space-y-4">
              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <p class="text-sm font-semibold text-slate-900">Optimize performance</p>
                <div class="mt-4 space-y-3">
                  {#each monitoringInsights as insight}
                    <div class="rounded-2xl border border-slate-200 px-4 py-3 text-sm text-slate-600">{insight}</div>
                  {/each}
                </div>
              </div>
            </section>
          </div>
        {:else}
          <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_360px]">
            <section class="space-y-4 rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                <a href="/action-types" class="rounded-3xl border border-slate-200 p-4 hover:border-sky-300 hover:bg-sky-50">
                  <p class="text-sm font-semibold text-slate-900">Use functions in Action Types</p>
                  <p class="mt-2 text-sm text-slate-500">Bind packages into governed actions with pinned ids, version references, or same-major auto-upgrade strategies.</p>
                </a>
                <a href="/apps" class="rounded-3xl border border-slate-200 p-4 hover:border-sky-300 hover:bg-sky-50">
                  <p class="text-sm font-semibold text-slate-900">Use functions in Workshop</p>
                  <p class="mt-2 text-sm text-slate-500">Expose function-backed workflows in operational apps, side panels, and review actions.</p>
                </a>
                <a href="/pipelines" class="rounded-3xl border border-slate-200 p-4 hover:border-sky-300 hover:bg-sky-50">
                  <p class="text-sm font-semibold text-slate-900">Use functions in Pipeline Builder</p>
                  <p class="mt-2 text-sm text-slate-500">Carry Python or TypeScript logic into data and orchestration flows when a package is ready for broader reuse.</p>
                </a>
              </div>

              <div class="rounded-3xl border border-slate-200 p-4">
                <p class="text-sm font-semibold text-slate-900">Functions on objects and models</p>
                <div class="mt-4 grid gap-3 md:grid-cols-2">
                  <div class="rounded-2xl border border-slate-200 p-4">
                    <p class="text-sm font-semibold text-slate-900">Objects and links</p>
                    <p class="mt-2 text-sm text-slate-500">Selected object types and target objects feed directly into simulation, which mirrors the runtime context available when a package is called from an action.</p>
                  </div>
                  <div class="rounded-2xl border border-slate-200 p-4">
                    <p class="text-sm font-semibold text-slate-900">Language models</p>
                    <p class="mt-2 text-sm text-slate-500">Packages with `allow_ai` can use the shared model surface; keep release snapshots when prompts or model assumptions change.</p>
                  </div>
                  <div class="rounded-2xl border border-slate-200 p-4">
                    <p class="text-sm font-semibold text-slate-900">Attachments and media</p>
                    <p class="mt-2 text-sm text-slate-500">Use structured parameters and object context to pass attachment handles, media references, and object identifiers into governed logic.</p>
                  </div>
                  <div class="rounded-2xl border border-slate-200 p-4">
                    <p class="text-sm font-semibold text-slate-900">API and webhook patterns</p>
                    <p class="mt-2 text-sm text-slate-500">Network-enabled packages can bridge external APIs while release workflow snapshots capture promotion posture and dependency strategy.</p>
                  </div>
                </div>
              </div>
            </section>

            <section class="space-y-4">
              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <p class="text-sm font-semibold text-slate-900">Query/API gateway style handoff</p>
                <pre class="mt-4 overflow-x-auto rounded-2xl bg-slate-950 p-4 text-xs text-slate-100">{selectedPackage
? `POST /api/functions/${releaseApiSlug || selectedPackage.name}

{
  "function_package_name": "${selectedPackage.name}",
  "function_package_version": "${selectedPackage.version}",
  "parameters": ${invocationParametersText}
}`
: 'Select a function package to generate a handoff snippet.'}</pre>
              </div>

              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <p class="text-sm font-semibold text-slate-900">Published family overview</p>
                <div class="mt-4 space-y-3">
                  {#if selectedPackageReleases.length === 0}
                    <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-500">
                      No releases saved for the selected version yet.
                    </div>
                  {:else}
                    {#each selectedPackageReleases as record}
                      <div class="rounded-2xl border border-slate-200 p-4">
                        <p class="text-sm font-semibold text-slate-900">{record.channel} · {record.status}</p>
                        <p class="mt-1 text-xs text-slate-500">{record.marketplace_product} · {record.dependency_mode}</p>
                      </div>
                    {/each}
                  {/if}
                </div>
              </div>
            </section>
          </div>
        {/if}
      </main>
    </div>
  {/if}
</div>
