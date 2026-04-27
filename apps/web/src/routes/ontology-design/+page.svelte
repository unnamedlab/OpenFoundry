<script lang="ts">
  import { browser } from '$app/environment';
  import { onMount } from 'svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import {
    listActionTypes,
    listInterfaces,
    listLinkTypes,
    listObjectTypes,
    listProjectMemberships,
    listProjects,
    listProperties,
    type ActionType,
    type LinkType,
    type ObjectType,
    type OntologyInterface,
    type OntologyProject,
    type OntologyProjectMembership,
    type Property
  } from '$lib/api/ontology';

  type ReviewTab = 'scorecard' | 'anti-patterns' | 'playbook' | 'review';
  type Severity = 'critical' | 'warning' | 'info';
  type CheckStatus = 'strong' | 'attention' | 'gap';

  interface Finding {
    id: string;
    code: string;
    title: string;
    severity: Severity;
    affected: string;
    summary: string;
    recommendation: string;
    evidence: string[];
  }

  interface PracticeCheck {
    id: string;
    label: string;
    status: CheckStatus;
    score: number;
    detail: string;
  }

  interface ReviewState {
    dismissed: string[];
    notes: string;
  }

  const tabs: Array<{ id: ReviewTab; label: string; glyph: 'graph' | 'history' | 'bookmark' | 'settings' }> = [
    { id: 'scorecard', label: 'Scorecard', glyph: 'graph' },
    { id: 'anti-patterns', label: 'Anti-patterns', glyph: 'history' },
    { id: 'playbook', label: 'Playbook', glyph: 'bookmark' },
    { id: 'review', label: 'Review', glyph: 'settings' }
  ];

  const genericTypeNames = new Set(['item', 'items', 'object', 'objects', 'entity', 'entities', 'record', 'records', 'data']);
  const genericPropertyNames = new Set(['name', 'value', 'type', 'status', 'date', 'id']);
  const genericLinkNames = new Set(['related', 'related_to', 'link', 'association', 'connected_to']);
  const systemKeywords = ['system', 'crm', 'erp', 'sap', 'hr', 'badge', 'project management', 'salesforce', 'workday', 'servicenow'];
  const departmentKeywords = ['sales', 'support', 'finance', 'billing', 'marketing', 'operations', 'ops', 'hr', 'legal'];
  const metadataPatterns = [
    /^_/,
    /etl/i,
    /extracted/i,
    /received/i,
    /batched/i,
    /sequence/i,
    /table_version/i,
    /internal_record_id/i,
    /debug/i,
    /metadata/i
  ];

  let loading = $state(true);
  let loadError = $state('');
  let activeTab = $state<ReviewTab>('scorecard');
  let activeSeverity = $state<'all' | Severity>('all');

  let objectTypes = $state<ObjectType[]>([]);
  let linkTypes = $state<LinkType[]>([]);
  let actionTypes = $state<ActionType[]>([]);
  let interfaces = $state<OntologyInterface[]>([]);
  let projects = $state<OntologyProject[]>([]);
  let projectMemberships = $state<Record<string, OntologyProjectMembership[]>>({});
  let propertiesByType = $state<Record<string, Property[]>>({});
  let reviewState = $state<ReviewState>({ dismissed: [], notes: '' });

  const visibleFindings = $derived.by(() =>
    findings.filter((finding) => !reviewState.dismissed.includes(finding.id) && (activeSeverity === 'all' || finding.severity === activeSeverity))
  );

  function storageKey() {
    return 'of.ontologyDesign.review';
  }

  function normalizeName(value: string) {
    return value
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, ' ')
      .replace(/\s+/g, ' ')
      .trim();
  }

  function normalizeBaseName(value: string) {
    const normalized = normalizeName(value)
      .replace(/\b(system|crm|erp|sap|hr|badge|salesforce|support|finance|billing|marketing|operations|ops|legal)\b/g, '')
      .replace(/\s+/g, ' ')
      .trim();
    return normalized || normalizeName(value);
  }

  function labelForType(typeId: string) {
    return objectTypes.find((item) => item.id === typeId)?.display_name ?? typeId;
  }

  function propertiesFor(typeId: string) {
    return propertiesByType[typeId] ?? [];
  }

  function actionTypesFor(typeId: string) {
    return actionTypes.filter((item) => item.object_type_id === typeId);
  }

  function loadReviewState() {
    if (!browser) return;
    try {
      reviewState = JSON.parse(window.localStorage.getItem(storageKey()) ?? '{"dismissed":[],"notes":""}');
    } catch {
      reviewState = { dismissed: [], notes: '' };
    }
  }

  function persistReviewState() {
    if (!browser) return;
    window.localStorage.setItem(storageKey(), JSON.stringify(reviewState));
  }

  function dismissFinding(id: string) {
    if (reviewState.dismissed.includes(id)) return;
    reviewState = { ...reviewState, dismissed: [...reviewState.dismissed, id] };
    persistReviewState();
  }

  function restoreDismissed() {
    reviewState = { ...reviewState, dismissed: [] };
    persistReviewState();
  }

  async function loadPage() {
    loading = true;
    loadError = '';

    try {
      const [typeResponse, linkResponse, actionResponse, interfaceResponse, projectResponse] = await Promise.all([
        listObjectTypes({ page: 1, per_page: 200 }),
        listLinkTypes({ page: 1, per_page: 200 }),
        listActionTypes({ page: 1, per_page: 200 }).catch(() => ({ data: [], total: 0, page: 1, per_page: 200 })),
        listInterfaces({ page: 1, per_page: 200 }).catch(() => ({ data: [], total: 0, page: 1, per_page: 200 })),
        listProjects({ page: 1, per_page: 200 }).catch(() => ({ data: [], total: 0, page: 1, per_page: 200 }))
      ]);

      objectTypes = typeResponse.data;
      linkTypes = linkResponse.data;
      actionTypes = actionResponse.data;
      interfaces = interfaceResponse.data;
      projects = projectResponse.data;

      const propertyEntries = await Promise.all(
        objectTypes.map(async (objectType) => [objectType.id, await listProperties(objectType.id).catch(() => [])] as const)
      );
      propertiesByType = Object.fromEntries(propertyEntries);

      const membershipEntries = await Promise.all(
        projects.map(async (project) => [project.id, await listProjectMemberships(project.id).catch(() => [])] as const)
      );
      projectMemberships = Object.fromEntries(membershipEntries);

      loadReviewState();
    } catch (error) {
      loadError = error instanceof Error ? error.message : 'Failed to load ontology design review';
    } finally {
      loading = false;
    }
  }

  function findSystemSilos(): Finding[] {
    const grouped = new Map<string, ObjectType[]>();
    for (const objectType of objectTypes) {
      const normalized = normalizeBaseName(objectType.display_name || objectType.name);
      const entries = grouped.get(normalized) ?? [];
      entries.push(objectType);
      grouped.set(normalized, entries);
    }

    return [...grouped.entries()]
      .filter(([, items]) => items.length > 1)
      .filter(([, items]) =>
        items.some((item) => systemKeywords.some((keyword) => normalizeName(`${item.name} ${item.display_name}`).includes(keyword)))
      )
      .map(([base, items]) => ({
        id: `system-silos-${base}`,
        code: 'system_silos',
        title: 'System Silos',
        severity: 'critical' as const,
        affected: items.map((item) => item.display_name).join(', '),
        summary: `Multiple object types look like source-system variants of the same entity base: ${base}.`,
        recommendation: 'Merge system-specific entities into one unified object type and join data upstream in pipelines.',
        evidence: items.map((item) => `${item.display_name} (${item.name})`)
      }));
  }

  function findDepartmentSilos(): Finding[] {
    const grouped = new Map<string, ObjectType[]>();
    for (const objectType of objectTypes) {
      const normalized = normalizeBaseName(objectType.display_name || objectType.name);
      const entries = grouped.get(normalized) ?? [];
      entries.push(objectType);
      grouped.set(normalized, entries);
    }

    return [...grouped.entries()]
      .filter(([, items]) => items.length > 1)
      .filter(([, items]) =>
        items.some((item) => departmentKeywords.some((keyword) => normalizeName(`${item.name} ${item.display_name}`).includes(keyword)))
      )
      .map(([base, items]) => ({
        id: `department-silos-${base}`,
        code: 'department_silos',
        title: 'Department Silos',
        severity: 'critical' as const,
        affected: items.map((item) => item.display_name).join(', '),
        summary: `The ontology appears to split a shared entity base across department-specific types: ${base}.`,
        recommendation: 'Use one shared object type, keep department-specific attributes as properties or links, and curate different object views by team.',
        evidence: items.map((item) => `${item.display_name} (${item.name})`)
      }));
  }

  function findKitchenSink(): Finding[] {
    return objectTypes.flatMap((objectType) => {
      const flagged = propertiesFor(objectType.id).filter((property) =>
        metadataPatterns.some((pattern) => pattern.test(property.name) || pattern.test(property.display_name))
      );
      if (flagged.length < 3) return [];
      return [
        {
          id: `kitchen-sink-${objectType.id}`,
          code: 'kitchen_sink',
          title: 'The Kitchen Sink',
          severity: flagged.length >= 6 ? 'critical' : 'warning',
          affected: objectType.display_name,
          summary: `${objectType.display_name} exposes several technical metadata fields that look better suited to backing datasets than to the ontology surface.`,
          recommendation: 'Remove ETL and system metadata from the ontology-facing schema, or hide it behind curated views instead of core properties.',
          evidence: flagged.slice(0, 8).map((property) => property.name)
        } satisfies Finding
      ];
    });
  }

  function findGodObjects(): Finding[] {
    return objectTypes.flatMap((objectType) => {
      const properties = propertiesFor(objectType.id);
      const genericSignals = properties.filter((property) => genericPropertyNames.has(normalizeName(property.name))).length;
      if (properties.length < 40 && !(properties.length >= 25 && genericSignals >= 3)) return [];
      return [
        {
          id: `god-object-${objectType.id}`,
          code: 'god_object',
          title: 'The God Object',
          severity: properties.length >= 60 ? 'critical' : 'warning',
          affected: objectType.display_name,
          summary: `${objectType.display_name} carries ${properties.length} properties, which suggests the object may represent multiple entities or overly broad use cases.`,
          recommendation: 'Split distinct entities into their own object types and use interfaces for genuinely shared characteristics.',
          evidence: [
            `${properties.length} total properties`,
            `${genericSignals} generic property names`,
            objectType.description || 'No description'
          ]
        }
      ];
    });
  }

  function findGoldenHammer(): Finding[] {
    return objectTypes.flatMap((objectType) => {
      const actions = actionTypesFor(objectType.id);
      const imperative = actions.filter((action) =>
        /^(set|update|calculate|assign|sync|refresh)\b/i.test(action.display_name || action.name)
      );
      if (actions.length < 8 || imperative.length < Math.ceil(actions.length * 0.6)) return [];
      return [
        {
          id: `golden-hammer-${objectType.id}`,
          code: 'golden_hammer',
          title: 'The Golden Hammer',
          severity: 'warning',
          affected: objectType.display_name,
          summary: `${objectType.display_name} relies on a dense cluster of imperative actions, which can indicate that actions are standing in for pipelines, automations, or cleaner workflow design.`,
          recommendation: 'Re-check whether these changes should be precomputed in pipelines, event-driven in automations, or bundled into fewer meaningful operations.',
          evidence: imperative.slice(0, 8).map((action) => action.display_name || action.name)
        }
      ];
    });
  }

  function findActionSprawl(): Finding[] {
    return objectTypes.flatMap((objectType) => {
      const actions = actionTypesFor(objectType.id);
      const singleField = actions.filter((action) => /^(set|update)\b/i.test(action.display_name || action.name));
      if (actions.length < 10 && singleField.length < 5) return [];
      return [
        {
          id: `action-sprawl-${objectType.id}`,
          code: 'action_sprawl',
          title: 'Action Sprawl',
          severity: 'warning',
          affected: objectType.display_name,
          summary: `${objectType.display_name} has ${actions.length} actions, many of which read like single-field updates rather than business operations.`,
          recommendation: 'Bundle related edits into cohesive business actions such as transfer, onboard, approve, or escalate.',
          evidence: singleField.slice(0, 10).map((action) => action.display_name || action.name)
        }
      ];
    });
  }

  function findTimeMachine(): Finding[] {
    return objectTypes.flatMap((objectType) => {
      const properties = propertiesFor(objectType.id);
      const temporalSignals = properties.filter((property) => /(version|revision|is_current|effective_date|amended_at)/i.test(property.name));
      const yearlyNaming = /(?:19|20)\d{2}\b/.test(objectType.display_name) || /(?:19|20)\d{2}\b/.test(objectType.name);
      if (!yearlyNaming && temporalSignals.length < 2) return [];
      return [
        {
          id: `time-machine-${objectType.id}`,
          code: 'time_machine',
          title: 'The Time Machine',
          severity: 'warning',
          affected: objectType.display_name,
          summary: `${objectType.display_name} shows versioning signals that may indicate historical copies are being modeled as first-class objects instead of current state plus history.`,
          recommendation: 'Keep one current object per entity, then model amendments or history in linked objects or time-aware properties.',
          evidence: [
            yearlyNaming ? 'Object type name contains a year/version signal.' : '',
            ...temporalSignals.slice(0, 6).map((property) => property.name)
          ].filter(Boolean)
        }
      ];
    });
  }

  function findMisnomers(): Finding[] {
    const typeFindings = objectTypes
      .filter((objectType) => genericTypeNames.has(normalizeName(objectType.name)) || genericTypeNames.has(normalizeName(objectType.display_name)))
      .map((objectType) => ({
        id: `misnomer-type-${objectType.id}`,
        code: 'misnomer',
        title: 'The Misnomer',
        severity: 'warning' as const,
        affected: objectType.display_name,
        summary: `${objectType.display_name} uses a generic name that is hard to interpret without extra context.`,
        recommendation: 'Rename object types and properties so they explain the business entity directly, not a vague placeholder.',
        evidence: [objectType.name, objectType.display_name]
      }));

    const propertyFindings = objectTypes.flatMap((objectType) => {
      const flagged = propertiesFor(objectType.id).filter((property) => genericPropertyNames.has(normalizeName(property.name)));
      if (flagged.length === 0) return [];
      return [
        {
          id: `misnomer-property-${objectType.id}`,
          code: 'misnomer',
          title: 'The Misnomer',
          severity: 'info' as const,
          affected: objectType.display_name,
          summary: `${objectType.display_name} contains generic property names that may need qualification.`,
          recommendation: 'Prefer names like monetary_value, due_date, product_category, or risk_score over generic labels like value, date, or type.',
          evidence: flagged.slice(0, 8).map((property) => property.name)
        }
      ];
    });

    const linkFindings = linkTypes
      .filter((linkType) => genericLinkNames.has(normalizeName(linkType.name)) || genericLinkNames.has(normalizeName(linkType.display_name)))
      .map((linkType) => ({
        id: `misnomer-link-${linkType.id}`,
        code: 'misnomer',
        title: 'The Misnomer',
        severity: 'info' as const,
        affected: linkType.display_name,
        summary: `${linkType.display_name} does not clearly describe the relationship between the two sides of the link.`,
        recommendation: 'Name links after the relationship, such as Supervisor, Manufacturing Facility, or Purchasing Customer.',
        evidence: [linkType.name, `${labelForType(linkType.source_type_id)} -> ${labelForType(linkType.target_type_id)}`]
      }));

    return [...typeFindings, ...propertyFindings, ...linkFindings];
  }

  const findings = $derived.by<Finding[]>(() => {
    return [
      ...findSystemSilos(),
      ...findDepartmentSilos(),
      ...findKitchenSink(),
      ...findGodObjects(),
      ...findGoldenHammer(),
      ...findActionSprawl(),
      ...findTimeMachine(),
      ...findMisnomers()
    ].sort((left, right) => severityWeight(right.severity) - severityWeight(left.severity));
  });

  function severityWeight(severity: Severity) {
    if (severity === 'critical') return 3;
    if (severity === 'warning') return 2;
    return 1;
  }

  const designScore = $derived.by(() => {
    let score = 100;
    for (const finding of findings) {
      score -= finding.severity === 'critical' ? 12 : finding.severity === 'warning' ? 6 : 2;
    }
    return Math.max(0, score);
  });

  const practiceChecks = $derived.by<PracticeCheck[]>(() => {
    const totalProperties = Object.values(propertiesByType).flat();
    const describedTypes = objectTypes.filter((item) => item.description.trim().length > 0).length;
    const describedProperties = totalProperties.filter((item) => item.description.trim().length > 0).length;
    const descriptionCoverage = totalProperties.length === 0 ? 1 : describedProperties / totalProperties.length;
    const multiMemberProjects = projects.filter((project) => (projectMemberships[project.id] ?? []).length > 1).length;
    const siloCount = findings.filter((item) => item.code === 'system_silos' || item.code === 'department_silos').length;
    const godObjectCount = findings.filter((item) => item.code === 'god_object').length;
    const actionRiskCount = findings.filter((item) => item.code === 'golden_hammer' || item.code === 'action_sprawl').length;
    const misnomerCount = findings.filter((item) => item.code === 'misnomer').length;

    return [
      {
        id: 'model-reality',
        label: 'Model reality, not systems',
        status: siloCount === 0 ? 'strong' : siloCount === 1 ? 'attention' : 'gap',
        score: Math.max(0, 100 - siloCount * 30),
        detail: siloCount === 0 ? 'No major system or department silos detected from naming patterns.' : `${siloCount} silo pattern groups detected across object types.`
      },
      {
        id: 'curate-intentionally',
        label: 'Curate intentionally',
        status: findings.some((item) => item.code === 'kitchen_sink') ? 'attention' : 'strong',
        score: findings.some((item) => item.code === 'kitchen_sink') ? 62 : 91,
        detail: findings.some((item) => item.code === 'kitchen_sink')
          ? 'Technical metadata appears exposed as ontology properties on at least one object type.'
          : 'Property curation looks reasonably intentional from current schema signals.'
      },
      {
        id: 'collaborate-across-teams',
        label: 'Collaborate across teams',
        status: multiMemberProjects > 0 ? 'strong' : 'attention',
        score: multiMemberProjects > 0 ? 88 : 58,
        detail: multiMemberProjects > 0
          ? `${multiMemberProjects} ontology spaces already have multiple memberships, which supports cross-team ownership.`
          : 'No multi-member ontology space was detected from current project memberships.'
      },
      {
        id: 'keep-types-focused',
        label: 'Keep object types focused',
        status: godObjectCount === 0 ? 'strong' : godObjectCount === 1 ? 'attention' : 'gap',
        score: Math.max(0, 100 - godObjectCount * 25),
        detail: godObjectCount === 0 ? 'No likely god-object surface detected from property counts.' : `${godObjectCount} object types look overloaded.`
      },
      {
        id: 'choose-right-tool',
        label: 'Choose the right tool',
        status: actionRiskCount === 0 ? 'strong' : 'attention',
        score: Math.max(0, 90 - actionRiskCount * 12),
        detail: actionRiskCount === 0 ? 'Action surface does not currently show obvious overuse patterns.' : `${actionRiskCount} action-design findings suggest some workflows may belong in pipelines, automations, or broader business actions.`
      },
      {
        id: 'use-interfaces',
        label: 'Use interfaces for abstraction',
        status: interfaces.length > 0 ? 'strong' : 'attention',
        score: interfaces.length > 0 ? 90 : 52,
        detail: interfaces.length > 0 ? `${interfaces.length} interfaces are available for shared abstractions.` : 'No interfaces are currently modeled, which may push too much reuse into wide object types.'
      },
      {
        id: 'document-decisions',
        label: 'Document your decisions',
        status: describedTypes === objectTypes.length && descriptionCoverage >= 0.75 ? 'strong' : descriptionCoverage >= 0.45 ? 'attention' : 'gap',
        score: Math.round(((describedTypes / Math.max(objectTypes.length, 1)) * 40 + descriptionCoverage * 60)),
        detail: `${describedTypes}/${objectTypes.length} object types and ${(descriptionCoverage * 100).toFixed(0)}% of properties have descriptions. ${misnomerCount > 0 ? 'Generic naming still increases documentation pressure.' : ''}`
      }
    ];
  });

  onMount(() => {
    void loadPage();
  });
</script>

<svelte:head>
  <title>OpenFoundry - Ontology Design Review</title>
</svelte:head>

<div class="mx-auto flex max-w-7xl flex-col gap-6 px-4 py-6">
  <section class="overflow-hidden rounded-[2rem] border border-slate-200 bg-[radial-gradient(circle_at_top_left,_rgba(24,61,112,0.18),_transparent_35%),linear-gradient(135deg,_#fbfcff_0%,_#eef3fb_42%,_#fbfcff_100%)] p-6 shadow-sm">
    <div class="grid gap-6 lg:grid-cols-[1.45fr_1fr]">
      <div class="space-y-4">
        <div class="inline-flex items-center gap-2 rounded-full border border-sky-200 bg-white/80 px-3 py-1 text-xs font-semibold uppercase tracking-[0.24em] text-sky-700">
          <Glyph name="bookmark" size={14} />
          Ontology design
        </div>
        <div class="space-y-3">
          <h1 class="text-3xl font-semibold tracking-tight text-slate-950">Best Practices and Anti-patterns</h1>
          <p class="max-w-3xl text-sm leading-6 text-slate-600">
            Review ontology quality against real design guidance: scorecard the current model, detect anti-patterns across object types, properties, links, actions, and interfaces, then route remediation back into the right OpenFoundry products.
          </p>
        </div>
        <div class="flex flex-wrap gap-3 text-xs text-slate-500">
          <a href="/ontology-manager" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-sky-300 hover:text-sky-700">Ontology Manager</a>
          <a href="/object-link-types" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-sky-300 hover:text-sky-700">Object and Link Types</a>
          <a href="/action-types" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-sky-300 hover:text-sky-700">Action Types</a>
          <a href="/interfaces" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-sky-300 hover:text-sky-700">Interfaces</a>
        </div>
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Design score</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{designScore}</p>
          <p class="mt-1 text-sm text-slate-500">Composite ontology design posture.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Findings</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{findings.length}</p>
          <p class="mt-1 text-sm text-slate-500">Detected anti-pattern signals.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Object types</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{objectTypes.length}</p>
          <p class="mt-1 text-sm text-slate-500">Schema surfaces reviewed.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Interfaces</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{interfaces.length}</p>
          <p class="mt-1 text-sm text-slate-500">Shared abstraction contracts discovered.</p>
        </div>
      </div>
    </div>
  </section>

  {#if loadError}
    <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{loadError}</div>
  {/if}

  {#if loading}
    <div class="rounded-3xl border border-slate-200 bg-white px-5 py-10 text-center text-sm text-slate-500">
      Loading ontology design review...
    </div>
  {:else}
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

    {#if activeTab === 'scorecard'}
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="grid gap-3">
            {#each practiceChecks as check}
              <div class="rounded-3xl border border-slate-200 p-4">
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">{check.label}</p>
                    <p class="mt-1 text-sm text-slate-500">{check.detail}</p>
                  </div>
                  <div class="text-right">
                    <div class={`rounded-full px-2 py-1 text-[11px] uppercase tracking-[0.2em] ${
                      check.status === 'strong'
                        ? 'border border-emerald-200 bg-emerald-50 text-emerald-700'
                        : check.status === 'attention'
                          ? 'border border-amber-200 bg-amber-50 text-amber-700'
                          : 'border border-rose-200 bg-rose-50 text-rose-700'
                    }`}>
                      {check.status}
                    </div>
                    <div class="mt-2 text-lg font-semibold text-slate-950">{check.score}</div>
                  </div>
                </div>
              </div>
            {/each}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">What to improve first</div>
          <div class="mt-4 space-y-3">
            {#each practiceChecks.filter((check) => check.status !== 'strong').slice(0, 4) as check}
              <div class="rounded-2xl border border-slate-200 p-4">
                <p class="text-sm font-semibold text-slate-900">{check.label}</p>
                <p class="mt-2 text-sm text-slate-500">{check.detail}</p>
              </div>
            {/each}
          </div>
        </section>
      </div>
    {:else if activeTab === 'anti-patterns'}
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="flex items-start justify-between gap-4">
            <div>
              <p class="text-sm font-semibold text-slate-900">Detected anti-patterns</p>
              <p class="mt-1 text-sm text-slate-500">Signals are derived from current schema, action surface, naming, and collaboration structure.</p>
            </div>
            <div class="flex flex-wrap gap-2">
              {#each ['all', 'critical', 'warning', 'info'] as severity}
                <button
                  class={`rounded-full px-3 py-1.5 text-xs font-medium ${
                    activeSeverity === severity
                      ? 'bg-slate-950 text-white'
                      : 'border border-slate-200 bg-white text-slate-600 hover:border-slate-300'
                  }`}
                  onclick={() => activeSeverity = severity as 'all' | Severity}
                >
                  {severity}
                </button>
              {/each}
            </div>
          </div>
          <div class="mt-4 space-y-3">
            {#each visibleFindings as finding}
              <div class="rounded-2xl border border-slate-200 p-4">
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">{finding.title}</p>
                    <p class="mt-1 text-xs uppercase tracking-[0.2em] text-slate-500">{finding.affected}</p>
                  </div>
                  <span class={`rounded-full px-2 py-1 text-[11px] uppercase tracking-[0.2em] ${
                    finding.severity === 'critical'
                      ? 'border border-rose-200 bg-rose-50 text-rose-700'
                      : finding.severity === 'warning'
                        ? 'border border-amber-200 bg-amber-50 text-amber-700'
                        : 'border border-slate-200 bg-slate-50 text-slate-600'
                  }`}>
                    {finding.severity}
                  </span>
                </div>
                <p class="mt-3 text-sm text-slate-600">{finding.summary}</p>
                <div class="mt-4 rounded-2xl border border-slate-200 bg-slate-50 p-3 text-sm text-slate-600">
                  <div class="font-medium text-slate-900">Recommendation</div>
                  <p class="mt-1">{finding.recommendation}</p>
                </div>
                <div class="mt-4 flex flex-wrap gap-2">
                  {#each finding.evidence as entry}
                    <span class="rounded-full border border-slate-200 bg-white px-3 py-1 text-[11px] text-slate-600">{entry}</span>
                  {/each}
                </div>
                <div class="mt-4">
                  <button class="rounded-full border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700" onclick={() => dismissFinding(finding.id)}>
                    Dismiss
                  </button>
                </div>
              </div>
            {/each}
            {#if visibleFindings.length === 0}
              <div class="rounded-3xl border border-dashed border-slate-300 px-4 py-10 text-center text-sm text-slate-500">
                No active findings for the current severity filter.
              </div>
            {/if}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Finding inventory</div>
          <div class="mt-4 space-y-3 text-sm text-slate-600">
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="font-medium text-slate-900">Critical</div>
              <div class="mt-1">{findings.filter((item) => item.severity === 'critical').length}</div>
            </div>
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="font-medium text-slate-900">Warning</div>
              <div class="mt-1">{findings.filter((item) => item.severity === 'warning').length}</div>
            </div>
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="font-medium text-slate-900">Info</div>
              <div class="mt-1">{findings.filter((item) => item.severity === 'info').length}</div>
            </div>
            <button class="w-full rounded-full border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700" onclick={restoreDismissed}>
              Restore dismissed
            </button>
          </div>
        </section>
      </div>
    {:else if activeTab === 'playbook'}
      <div class="grid gap-4 xl:grid-cols-2">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Best practices</div>
          <div class="mt-4 grid gap-3">
            <div class="rounded-2xl border border-slate-200 p-4"><div class="font-medium text-slate-900">Model reality, not systems</div><p class="mt-2 text-sm text-slate-500">Unify source-system variants into business entities and merge upstream in pipelines.</p></div>
            <div class="rounded-2xl border border-slate-200 p-4"><div class="font-medium text-slate-900">Curate intentionally</div><p class="mt-2 text-sm text-slate-500">Expose only properties that matter to workflows, search, views, and decisions.</p></div>
            <div class="rounded-2xl border border-slate-200 p-4"><div class="font-medium text-slate-900">Collaborate across teams</div><p class="mt-2 text-sm text-slate-500">Use shared ontologies and project memberships to keep one canonical model across departments.</p></div>
            <div class="rounded-2xl border border-slate-200 p-4"><div class="font-medium text-slate-900">Keep object types focused</div><p class="mt-2 text-sm text-slate-500">Split overloaded types and re-use interfaces when behavior is truly shared.</p></div>
            <div class="rounded-2xl border border-slate-200 p-4"><div class="font-medium text-slate-900">Choose the right tool</div><p class="mt-2 text-sm text-slate-500">Actions for decisions, pipelines for transforms, automations for reactions, functions for live complex logic.</p></div>
            <div class="rounded-2xl border border-slate-200 p-4"><div class="font-medium text-slate-900">Document your decisions</div><p class="mt-2 text-sm text-slate-500">Descriptions and precise naming reduce onboarding friction and prevent silent divergence.</p></div>
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Fix path in OpenFoundry</div>
          <div class="mt-4 grid gap-3">
            <a href="/object-link-types" class="rounded-2xl border border-slate-200 p-4 transition hover:border-sky-300">
              <div class="font-medium text-slate-900">Use Object and Link Types</div>
              <p class="mt-2 text-sm text-slate-500">Split system silos, rename ambiguous types, curate properties, and reshape link semantics.</p>
            </a>
            <a href="/interfaces" class="rounded-2xl border border-slate-200 p-4 transition hover:border-sky-300">
              <div class="font-medium text-slate-900">Use Interfaces</div>
              <p class="mt-2 text-sm text-slate-500">Extract shared traits from wide object types instead of overloading one god object.</p>
            </a>
            <a href="/action-types" class="rounded-2xl border border-slate-200 p-4 transition hover:border-sky-300">
              <div class="font-medium text-slate-900">Use Action Types</div>
              <p class="mt-2 text-sm text-slate-500">Replace action sprawl with cohesive business operations and cleaner parameter flows.</p>
            </a>
            <a href="/ontologies" class="rounded-2xl border border-slate-200 p-4 transition hover:border-sky-300">
              <div class="font-medium text-slate-900">Use Ontologies</div>
              <p class="mt-2 text-sm text-slate-500">Run branch reviews, cross-team proposals, and shared-space governance when the model changes affect many teams.</p>
            </a>
            <a href="/ontology-manager" class="rounded-2xl border border-slate-200 p-4 transition hover:border-sky-300">
              <div class="font-medium text-slate-900">Use Ontology Manager</div>
              <p class="mt-2 text-sm text-slate-500">Save staged changes, inspect warnings, and govern import or export flows during remediations.</p>
            </a>
          </div>
        </section>
      </div>
    {:else if activeTab === 'review'}
      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Design review notes</div>
          <label class="mt-4 block space-y-2 text-sm text-slate-700">
            <span class="font-medium">Capture remediation decisions, ownership, or open questions</span>
            <textarea
              rows="16"
              class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500"
              bind:value={reviewState.notes}
              oninput={persistReviewState}
              placeholder="Example: Merge Sales Customer and Support Customer into Customer, then move team-specific metadata into links and object views."
            ></textarea>
          </label>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="text-sm font-semibold text-slate-900">Review summary</div>
          <div class="mt-4 space-y-3 text-sm text-slate-600">
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="font-medium text-slate-900">Design score</div>
              <div class="mt-2 text-2xl font-semibold text-slate-950">{designScore}</div>
            </div>
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="font-medium text-slate-900">Outstanding findings</div>
              <div class="mt-2">{visibleFindings.length}</div>
            </div>
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="font-medium text-slate-900">Dismissed findings</div>
              <div class="mt-2">{reviewState.dismissed.length}</div>
            </div>
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="font-medium text-slate-900">Projects with multiple members</div>
              <div class="mt-2">{projects.filter((project) => (projectMemberships[project.id] ?? []).length > 1).length}</div>
            </div>
          </div>
        </section>
      </div>
    {/if}
  {/if}
</div>
