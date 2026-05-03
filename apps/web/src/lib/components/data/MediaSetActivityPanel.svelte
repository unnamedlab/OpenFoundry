<!--
  MediaSetActivityPanel — read-only audit log scoped to one media set.

  Wraps the existing AuditLogViewer with three pre-applied filters:
    * `source_service = "media-sets-service"` — narrows the audit warehouse
      to events emitted by this service.
    * `resource_id = mediaSet.rid` — surfaces only the events that touch
      this set or its items (per `audit_trail::events::AuditEvent::resource_rid`).
    * `showProbeForm = false` — the global Audit page uses the manual probe
      to seed test events; the per-resource panel is read-only.

  The viewer is intentionally reused (not forked) so a single component
  governs the audit log presentation across the app — when the global
  page gets a new column, this panel inherits it for free.
-->
<script lang="ts">
  import AuditLogViewer from '$lib/components/audit/AuditLogViewer.svelte';
  import {
    listEvents,
    type AnomalyAlert,
    type AuditEvent,
    type ClassificationCatalogEntry,
    type ClassificationLevel
  } from '$lib/api/audit';
  import type { MediaSet } from '$lib/api/mediaSets';

  type Props = {
    mediaSet: MediaSet;
  };

  let { mediaSet }: Props = $props();

  const SOURCE_SERVICE = 'media-sets-service';

  let events = $state<AuditEvent[]>([]);
  let anomalies = $state<AnomalyAlert[]>([]);
  let loading = $state(true);
  let error = $state('');

  // The global ClassificationCatalogEntry list is fetched on the audit
  // page; the dropdown here only surfaces the levels we have wired so
  // far so the filter picker stays usable when the catalog endpoint
  // is unreachable from this page.
  const classifications: ClassificationCatalogEntry[] = [
    { classification: 'public' as ClassificationLevel, description: 'Public' },
    { classification: 'confidential' as ClassificationLevel, description: 'Confidential' },
    { classification: 'pii' as ClassificationLevel, description: 'PII' }
  ];

  // Filter state mirrors AuditLogViewer's contract. `subject_id` and
  // `classification` stay user-editable; `source_service` is locked to
  // `media-sets-service` so flipping it would lose the resource scope.
  let filters = $state({
    source_service: SOURCE_SERVICE,
    subject_id: '',
    classification: ''
  });

  // Draft is required by the viewer's prop contract even when the
  // probe form is hidden; we pass a no-op skeleton so the viewer
  // doesn't choke on `undefined` field reads.
  const draft = $state({
    source_service: SOURCE_SERVICE,
    channel: 'audit',
    actor: '',
    action: '',
    resource_type: 'media_set',
    resource_id: mediaSet.rid,
    status: 'success' as const,
    severity: 'low' as const,
    classification: 'public' as ClassificationLevel,
    subject_id: '',
    ip_address: '',
    location: '',
    labels_text: '',
    metadata_text: '{}',
    retention_days: '365'
  });

  async function load() {
    loading = true;
    error = '';
    try {
      const response = await listEvents({
        source_service: SOURCE_SERVICE,
        subject_id: filters.subject_id || undefined,
        classification: filters.classification || undefined,
        resource_id: mediaSet.rid
      });
      events = response.items;
      anomalies = response.anomalies;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to load audit events';
    } finally {
      loading = false;
    }
  }

  // Rerun the fetch whenever the active set changes (operator navigates
  // between sets without unmounting the layout).
  $effect(() => {
    void mediaSet.rid;
    void load();
  });

  function onFilterChange(patch: Partial<typeof filters>) {
    filters = { ...filters, ...patch, source_service: SOURCE_SERVICE };
  }

  function onApplyFilters() {
    void load();
  }
</script>

<section class="space-y-4" data-testid="media-set-activity-panel">
  <header class="flex items-start justify-between gap-3">
    <div>
      <h2 class="text-base font-semibold">Activity</h2>
      <p class="mt-1 text-xs text-slate-500">
        Every action that touches this media set or its items lands in the
        audit warehouse via the Postgres outbox (ADR-0022). The list below
        is filtered to <code class="font-mono">resource_id = {mediaSet.rid}</code>.
      </p>
    </div>
    <button
      type="button"
      class="rounded-xl border border-slate-200 px-3 py-1.5 text-xs hover:bg-slate-50 dark:border-gray-700 dark:hover:bg-gray-800"
      data-testid="media-set-activity-refresh"
      onclick={load}
      disabled={loading}
    >
      {loading ? 'Refreshing…' : 'Refresh'}
    </button>
  </header>

  {#if error}
    <div
      class="rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700 dark:border-rose-900/40 dark:bg-rose-950/40 dark:text-rose-300"
      data-testid="media-set-activity-error"
    >
      {error}
    </div>
  {:else if anomalies.length > 0}
    <ul class="space-y-1 text-xs" data-testid="media-set-activity-anomalies">
      {#each anomalies as anomaly (anomaly.id)}
        <li class="rounded-lg border border-amber-300 bg-amber-50 px-3 py-1.5 text-amber-800">
          {anomaly.title} — {anomaly.recommended_action}
        </li>
      {/each}
    </ul>
  {/if}

  <AuditLogViewer
    {events}
    {classifications}
    {filters}
    {draft}
    busy={loading}
    {onFilterChange}
    {onApplyFilters}
    onDraftChange={() => {}}
    onAppendEvent={() => {}}
    showProbeForm={false}
  />
</section>
