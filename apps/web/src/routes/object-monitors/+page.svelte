<script lang="ts">
  import { browser } from '$app/environment';
  import { onMount } from 'svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import {
    appendEvent,
    listAnomalies,
    listEvents,
    type AnomalyAlert,
    type AuditEvent
  } from '$lib/api/audit';
  import {
    listActionTypes,
    listFunctionPackages,
    listObjectSets,
    listObjectTypes,
    type ActionType,
    type FunctionPackageSummary,
    type ObjectSetDefinition,
    type ObjectType
  } from '$lib/api/ontology';
  import {
    createWorkflow,
    deleteWorkflow,
    listWorkflowApprovals,
    listWorkflowRuns,
    listWorkflows,
    startWorkflowRun,
    updateWorkflow,
    type WorkflowApproval,
    type WorkflowDefinition,
    type WorkflowRun,
    type WorkflowStep
  } from '$lib/api/workflows';

  type MonitorConditionKind = 'event' | 'threshold' | 'function';
  type MonitorEventType = 'objects_added' | 'objects_removed' | 'metric_increase' | 'metric_decrease';

  interface MonitorSubscriber {
    label: string;
    muted: boolean;
    channels: string[];
  }

  interface MonitorConfig {
    surface: 'object_monitor';
    legacy_state: 'sunset';
    input_object_set_id: string;
    input_object_type_id: string | null;
    condition_kind: MonitorConditionKind;
    event_type: MonitorEventType;
    threshold_metric: string;
    threshold_operator: '>' | '>=' | '<' | '<=' | '=';
    threshold_value: number;
    function_package_id: string;
    function_package_name: string;
    notification_title: string;
    notification_message: string;
    notification_channels: string[];
    subscribers: MonitorSubscriber[];
    expiration_at: string;
    muted: boolean;
    save_location: string;
    polling_window_hours: number;
  }

  interface MonitorDraft {
    id?: string;
    name: string;
    description: string;
    input_object_set_id: string;
    condition_kind: MonitorConditionKind;
    event_type: MonitorEventType;
    threshold_metric: string;
    threshold_operator: '>' | '>=' | '<' | '<=' | '=';
    threshold_value: string;
    function_package_id: string;
    action_type_id: string;
    notification_channels: string[];
    notification_title: string;
    notification_message: string;
    subscribers_text: string;
    muted: boolean;
    save_location: string;
    expiration_at: string;
    status: string;
  }

  interface MonitorRecord {
    workflow: WorkflowDefinition;
    config: MonitorConfig;
    actionStep: WorkflowStep | null;
    notificationStep: WorkflowStep | null;
    inputSet: ObjectSetDefinition | null;
    objectType: ObjectType | null;
    failedRuns: number;
    pendingApprovals: number;
    expiringSoon: boolean;
  }

  const conditionKinds: { id: MonitorConditionKind; label: string; note: string }[] = [
    { id: 'event', label: 'Event', note: 'Discrete events such as objects added, removed, or metric shifts.' },
    { id: 'threshold', label: 'Threshold', note: 'Continuous true/false monitoring across the input set.' },
    { id: 'function', label: 'Function-backed', note: 'Custom boolean evaluation using a published function package.' }
  ];

  const eventTypes: { id: MonitorEventType; label: string }[] = [
    { id: 'objects_added', label: 'Objects added' },
    { id: 'objects_removed', label: 'Objects removed' },
    { id: 'metric_increase', label: 'Metric increase' },
    { id: 'metric_decrease', label: 'Metric decrease' }
  ];

  let workflows = $state<WorkflowDefinition[]>([]);
  let objectSets = $state<ObjectSetDefinition[]>([]);
  let objectTypes = $state<ObjectType[]>([]);
  let actionTypes = $state<ActionType[]>([]);
  let functionPackages = $state<FunctionPackageSummary[]>([]);
  let events = $state<AuditEvent[]>([]);
  let anomalies = $state<AnomalyAlert[]>([]);
  let runs = $state<WorkflowRun[]>([]);
  let approvals = $state<WorkflowApproval[]>([]);

  let loading = $state(true);
  let saving = $state(false);
  let running = $state(false);
  let error = $state('');
  let search = $state('');
  let selectedMonitorId = $state('');
  let showCreate = $state(false);
  let draft = $state<MonitorDraft>(createEmptyDraft());

  const objectSetMap = $derived.by(() => {
    const map = new Map<string, ObjectSetDefinition>();
    for (const item of objectSets) map.set(item.id, item);
    return map;
  });

  const objectTypeMap = $derived.by(() => {
    const map = new Map<string, ObjectType>();
    for (const item of objectTypes) map.set(item.id, item);
    return map;
  });

  const monitorRecords = $derived.by(() => {
    const approvalCount = new Map<string, number>();
    for (const approval of approvals) {
      approvalCount.set(approval.workflow_id, (approvalCount.get(approval.workflow_id) ?? 0) + 1);
    }

    return workflows
      .filter(isObjectMonitorWorkflow)
      .map((workflow) => {
        const config = parseMonitorConfig(workflow);
        const inputSet = objectSetMap.get(config.input_object_set_id) ?? null;
        const objectType = config.input_object_type_id ? objectTypeMap.get(config.input_object_type_id) ?? null : null;
        const monitorEvents = events.filter(
          (event) => event.resource_type === 'object_monitor' && event.resource_id === workflow.id
        );
        const failedRuns = monitorEvents.filter((event) => event.status === 'failure').length;
        const expiringSoon = new Date(config.expiration_at).getTime() - Date.now() < 1000 * 60 * 60 * 24 * 14;
        const actionStep = workflow.steps.find((step) => step.step_type === 'submit_action') ?? null;
        const notificationStep = workflow.steps.find((step) => step.step_type === 'notification') ?? null;

        return {
          workflow,
          config,
          actionStep,
          notificationStep,
          inputSet,
          objectType,
          failedRuns,
          pendingApprovals: approvalCount.get(workflow.id) ?? 0,
          expiringSoon
        } satisfies MonitorRecord;
      })
      .filter((record) => {
        const haystack = `${record.workflow.name} ${record.workflow.description} ${record.inputSet?.name ?? ''} ${record.objectType?.display_name ?? ''}`.toLowerCase();
        return haystack.includes(search.trim().toLowerCase());
      })
      .sort((left, right) => {
        const leftTs = left.workflow.last_triggered_at ? new Date(left.workflow.last_triggered_at).getTime() : 0;
        const rightTs = right.workflow.last_triggered_at ? new Date(right.workflow.last_triggered_at).getTime() : 0;
        return rightTs - leftTs;
      });
  });

  const selectedMonitor = $derived(
    monitorRecords.find((record) => record.workflow.id === selectedMonitorId) ?? monitorRecords[0] ?? null
  );

  const selectedMonitorEvents = $derived.by(() => {
    if (!selectedMonitor) return [];
    return events
      .filter((event) => event.resource_type === 'object_monitor' && event.resource_id === selectedMonitor.workflow.id)
      .sort((left, right) => new Date(right.occurred_at).getTime() - new Date(left.occurred_at).getTime());
  });

  const selectedActionTypes = $derived.by(() => {
    const inputSet = objectSetMap.get(draft.input_object_set_id);
    if (!inputSet) return actionTypes;
    return actionTypes.filter((item) => item.object_type_id === inputSet.base_object_type_id);
  });

  const overviewStats = $derived.by(() => {
    const total = monitorRecords.length;
    const muted = monitorRecords.filter((record) => record.config.muted || record.workflow.status === 'muted').length;
    const errors = monitorRecords.filter((record) => record.failedRuns > 0 || record.workflow.status === 'error').length;
    const expiring = monitorRecords.filter((record) => record.expiringSoon).length;
    return { total, muted, errors, expiring };
  });

  function createEmptyDraft(): MonitorDraft {
    const expiration = new Date();
    expiration.setMonth(expiration.getMonth() + 3);
    return {
      name: 'New object monitor',
      description: '',
      input_object_set_id: '',
      condition_kind: 'event',
      event_type: 'objects_added',
      threshold_metric: 'count',
      threshold_operator: '>=',
      threshold_value: '10',
      function_package_id: '',
      action_type_id: '',
      notification_channels: ['in_app', 'email'],
      notification_title: 'Monitor triggered',
      notification_message: 'A monitor condition was met for the selected input set.',
      subscribers_text: 'ops-team@example.com',
      muted: false,
      save_location: 'Private',
      expiration_at: expiration.toISOString().slice(0, 16),
      status: 'active'
    };
  }

  function defaultMonitorConfig(): MonitorConfig {
    const expiration = new Date();
    expiration.setMonth(expiration.getMonth() + 3);
    return {
      surface: 'object_monitor',
      legacy_state: 'sunset',
      input_object_set_id: '',
      input_object_type_id: null,
      condition_kind: 'event',
      event_type: 'objects_added',
      threshold_metric: 'count',
      threshold_operator: '>=',
      threshold_value: 10,
      function_package_id: '',
      function_package_name: '',
      notification_title: 'Monitor triggered',
      notification_message: 'A monitor condition was met for the selected input set.',
      notification_channels: ['in_app', 'email'],
      subscribers: [],
      expiration_at: expiration.toISOString(),
      muted: false,
      save_location: 'Private',
      polling_window_hours: 24
    };
  }

  function parseSubscribers(text: string): MonitorSubscriber[] {
    return text
      .split('\n')
      .map((entry) => entry.trim())
      .filter(Boolean)
      .map((entry) => ({
        label: entry,
        muted: false,
        channels: ['in_app', 'email']
      }));
  }

  function parseMonitorConfig(workflow: WorkflowDefinition): MonitorConfig {
    const raw = workflow.trigger_config ?? {};
    const base = defaultMonitorConfig();
    return {
      ...base,
      ...raw,
      input_object_set_id: typeof raw.input_object_set_id === 'string' ? raw.input_object_set_id : base.input_object_set_id,
      input_object_type_id: typeof raw.input_object_type_id === 'string' ? raw.input_object_type_id : null,
      condition_kind: raw.condition_kind === 'threshold' || raw.condition_kind === 'function' ? raw.condition_kind : 'event',
      event_type: raw.event_type === 'objects_removed' || raw.event_type === 'metric_increase' || raw.event_type === 'metric_decrease' ? raw.event_type : 'objects_added',
      threshold_metric: typeof raw.threshold_metric === 'string' ? raw.threshold_metric : base.threshold_metric,
      threshold_operator:
        raw.threshold_operator === '>' || raw.threshold_operator === '>=' || raw.threshold_operator === '<' || raw.threshold_operator === '<=' || raw.threshold_operator === '='
          ? raw.threshold_operator
          : base.threshold_operator,
      threshold_value: typeof raw.threshold_value === 'number' ? raw.threshold_value : base.threshold_value,
      function_package_id: typeof raw.function_package_id === 'string' ? raw.function_package_id : base.function_package_id,
      function_package_name: typeof raw.function_package_name === 'string' ? raw.function_package_name : base.function_package_name,
      notification_title: typeof raw.notification_title === 'string' ? raw.notification_title : base.notification_title,
      notification_message: typeof raw.notification_message === 'string' ? raw.notification_message : base.notification_message,
      notification_channels: Array.isArray(raw.notification_channels) ? raw.notification_channels.filter((value): value is string => typeof value === 'string') : base.notification_channels,
      subscribers: Array.isArray(raw.subscribers)
        ? raw.subscribers
            .map((entry) => {
              if (!entry || typeof entry !== 'object') return null;
              const subscriber = entry as Record<string, unknown>;
              return {
                label: typeof subscriber.label === 'string' ? subscriber.label : 'subscriber',
                muted: Boolean(subscriber.muted),
                channels: Array.isArray(subscriber.channels)
                  ? subscriber.channels.filter((value): value is string => typeof value === 'string')
                  : ['in_app']
              } satisfies MonitorSubscriber;
            })
            .filter((entry): entry is MonitorSubscriber => entry !== null)
        : [],
      expiration_at: typeof raw.expiration_at === 'string' ? raw.expiration_at : base.expiration_at,
      muted: Boolean(raw.muted),
      save_location: typeof raw.save_location === 'string' ? raw.save_location : base.save_location,
      polling_window_hours: typeof raw.polling_window_hours === 'number' ? raw.polling_window_hours : base.polling_window_hours
    };
  }

  function isObjectMonitorWorkflow(workflow: WorkflowDefinition) {
    const raw = workflow.trigger_config ?? {};
    return raw.surface === 'object_monitor' || workflow.name.startsWith('[Monitor]');
  }

  function buildMonitorPayload() {
    const inputSet = objectSetMap.get(draft.input_object_set_id) ?? null;
    const functionPackage = functionPackages.find((item) => item.id === draft.function_package_id) ?? null;

    const trigger_config: MonitorConfig = {
      surface: 'object_monitor',
      legacy_state: 'sunset',
      input_object_set_id: draft.input_object_set_id,
      input_object_type_id: inputSet?.base_object_type_id ?? null,
      condition_kind: draft.condition_kind,
      event_type: draft.event_type,
      threshold_metric: draft.threshold_metric.trim() || 'count',
      threshold_operator: draft.threshold_operator,
      threshold_value: Number(draft.threshold_value || '0'),
      function_package_id: draft.function_package_id,
      function_package_name: functionPackage?.display_name ?? '',
      notification_title: draft.notification_title,
      notification_message: draft.notification_message,
      notification_channels: draft.notification_channels,
      subscribers: parseSubscribers(draft.subscribers_text),
      expiration_at: new Date(draft.expiration_at).toISOString(),
      muted: draft.muted,
      save_location: draft.save_location,
      polling_window_hours: draft.condition_kind === 'event' ? 1 : 24
    };

    const notificationStep: WorkflowStep = {
      id: crypto.randomUUID(),
      name: 'Send monitor notification',
      step_type: 'notification',
      description: 'Deliver in-app and outbound monitor notifications.',
      next_step_id: draft.action_type_id ? 'submit-action' : null,
      branches: [],
      config: {
        title: draft.notification_title,
        message: draft.notification_message,
        channels: draft.notification_channels,
        severity: draft.condition_kind === 'event' ? 'medium' : 'high'
      }
    };

    const actionStep: WorkflowStep | null = draft.action_type_id
      ? {
          id: 'submit-action',
          name: 'Submit ontology action',
          step_type: 'submit_action',
          description: 'Apply a remediation action when the monitor condition is met.',
          next_step_id: null,
          branches: [],
          config: {
            action_id: draft.action_type_id,
            target_object_id_field: 'event.object_id',
            parameters: {},
            justification: 'Triggered automatically by Object Monitors',
            result_key: 'object_monitor.last_action'
          }
        }
      : null;

    const steps = actionStep ? [notificationStep, actionStep] : [notificationStep];

    return {
      name: draft.name.startsWith('[Monitor]') ? draft.name : `[Monitor] ${draft.name}`,
      description: draft.description,
      status: draft.status,
      trigger_type: draft.condition_kind === 'event' ? 'event' : 'cron',
      trigger_config: trigger_config as unknown as Record<string, unknown>,
      steps
    };
  }

  function hydrateDraft(record: MonitorRecord) {
    draft = {
      id: record.workflow.id,
      name: record.workflow.name.replace(/^\[Monitor\]\s*/, ''),
      description: record.workflow.description,
      input_object_set_id: record.config.input_object_set_id,
      condition_kind: record.config.condition_kind,
      event_type: record.config.event_type,
      threshold_metric: record.config.threshold_metric,
      threshold_operator: record.config.threshold_operator,
      threshold_value: String(record.config.threshold_value),
      function_package_id: record.config.function_package_id,
      action_type_id: String(record.actionStep?.config?.action_id ?? ''),
      notification_channels: [...record.config.notification_channels],
      notification_title: record.config.notification_title,
      notification_message: record.config.notification_message,
      subscribers_text: record.config.subscribers.map((entry) => entry.label).join('\n'),
      muted: record.config.muted,
      save_location: record.config.save_location,
      expiration_at: record.config.expiration_at.slice(0, 16),
      status: record.workflow.status
    };
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const [workflowResponse, objectSetResponse, objectTypeResponse, actionResponse, functionResponse, auditEventResponse, anomalyResponse, approvalResponse] =
        await Promise.all([
          listWorkflows({ per_page: 200 }),
          listObjectSets(),
          listObjectTypes({ per_page: 200 }),
          listActionTypes({ per_page: 200 }),
          listFunctionPackages({ per_page: 200 }),
          listEvents(),
          listAnomalies(),
          listWorkflowApprovals({ per_page: 200 })
        ]);

      workflows = workflowResponse.data;
      objectSets = objectSetResponse.data;
      objectTypes = objectTypeResponse.data;
      actionTypes = actionResponse.data;
      functionPackages = functionResponse.data;
      events = auditEventResponse.items;
      anomalies = anomalyResponse;
      approvals = approvalResponse.data;

      if (!draft.input_object_set_id && objectSets[0]) {
        draft.input_object_set_id = objectSets[0].id;
      }

      if (!selectedMonitorId) {
        selectedMonitorId = workflowResponse.data.find(isObjectMonitorWorkflow)?.id ?? '';
      }

      if (selectedMonitorId) {
        await loadRuns(selectedMonitorId);
      } else {
        runs = [];
      }
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to load object monitors';
    } finally {
      loading = false;
    }
  }

  async function loadRuns(id: string) {
    try {
      const response = await listWorkflowRuns(id, { per_page: 50 });
      runs = response.data;
    } catch {
      runs = [];
    }
  }

  async function selectMonitor(id: string) {
    selectedMonitorId = id;
    const record = monitorRecords.find((item) => item.workflow.id === id);
    if (record) {
      hydrateDraft(record);
    }
    await loadRuns(id);
    showCreate = false;
  }

  function beginCreate() {
    showCreate = true;
    selectedMonitorId = '';
    draft = createEmptyDraft();
    if (objectSets[0]) {
      draft.input_object_set_id = objectSets[0].id;
    }
  }

  async function recordAudit(action: string, resourceId: string, status: 'success' | 'failure' | 'denied', metadata: Record<string, unknown>) {
    try {
      await appendEvent({
        source_service: 'object-monitors',
        channel: 'application',
        actor: 'user:workspace',
        action,
        resource_type: 'object_monitor',
        resource_id: resourceId,
        status,
        severity: status === 'failure' ? 'high' : 'medium',
        classification: 'confidential',
        metadata,
        labels: ['object-monitor', resourceId]
      });
    } catch {
      // Best-effort timeline enrichment.
    }
  }

  async function saveMonitor() {
    if (!draft.name.trim() || !draft.input_object_set_id) {
      error = 'Monitor name and input object set are required';
      return;
    }

    saving = true;
    error = '';
    try {
      const payload = buildMonitorPayload();
      const workflow = draft.id
        ? await updateWorkflow(draft.id, payload)
        : await createWorkflow(payload);

      await recordAudit(draft.id ? 'monitor.updated' : 'monitor.created', workflow.id, 'success', {
        condition_kind: draft.condition_kind,
        input_object_set_id: draft.input_object_set_id,
        muted: draft.muted
      });

      await load();
      await selectMonitor(workflow.id);
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to save monitor';
    } finally {
      saving = false;
    }
  }

  async function runMonitor(record: MonitorRecord) {
    running = true;
    error = '';
    try {
      await startWorkflowRun(record.workflow.id, {
        source: 'object_monitors',
        input_object_set_id: record.config.input_object_set_id,
        condition_kind: record.config.condition_kind
      });
      await recordAudit('monitor.evaluated', record.workflow.id, 'success', {
        input_object_set_id: record.config.input_object_set_id,
        condition_kind: record.config.condition_kind
      });
      await load();
      await loadRuns(record.workflow.id);
    } catch (cause) {
      await recordAudit('monitor.evaluated', record.workflow.id, 'failure', {
        error: cause instanceof Error ? cause.message : 'Failed to run monitor'
      });
      error = cause instanceof Error ? cause.message : 'Failed to evaluate monitor';
    } finally {
      running = false;
    }
  }

  async function toggleMute(record: MonitorRecord) {
    const nextMuted = !record.config.muted;
    try {
      await updateWorkflow(record.workflow.id, {
        trigger_config: {
          ...record.workflow.trigger_config,
          muted: nextMuted
        }
      });
      await recordAudit(nextMuted ? 'monitor.muted' : 'monitor.unmuted', record.workflow.id, 'success', {
        muted: nextMuted
      });
      await load();
      if (record.workflow.id === selectedMonitorId) {
        await selectMonitor(record.workflow.id);
      }
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to update monitor mute state';
    }
  }

  async function extendExpiration(record: MonitorRecord) {
    const expiration = new Date(record.config.expiration_at);
    expiration.setMonth(expiration.getMonth() + 3);
    try {
      await updateWorkflow(record.workflow.id, {
        trigger_config: {
          ...record.workflow.trigger_config,
          expiration_at: expiration.toISOString()
        }
      });
      await recordAudit('monitor.expiration_extended', record.workflow.id, 'success', {
        expiration_at: expiration.toISOString()
      });
      await load();
      if (record.workflow.id === selectedMonitorId) {
        await selectMonitor(record.workflow.id);
      }
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to extend monitor expiration';
    }
  }

  async function removeMonitor(record: MonitorRecord) {
    if (!browser || !confirm(`Delete monitor "${record.workflow.name}"?`)) return;
    try {
      await deleteWorkflow(record.workflow.id);
      await recordAudit('monitor.deleted', record.workflow.id, 'success', {
        name: record.workflow.name
      });
      if (selectedMonitorId === record.workflow.id) {
        selectedMonitorId = '';
        runs = [];
        draft = createEmptyDraft();
      }
      await load();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to delete monitor';
    }
  }

  function objectSetLabel(id: string) {
    return objectSetMap.get(id)?.name ?? 'Unknown saved exploration';
  }

  function formatDate(value: string | null | undefined) {
    if (!value) return '—';
    return new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value));
  }

  function conditionSummary(record: MonitorRecord) {
    if (record.config.condition_kind === 'event') {
      return `Event monitor on ${record.config.event_type.replaceAll('_', ' ')}`;
    }
    if (record.config.condition_kind === 'threshold') {
      return `${record.config.threshold_metric} ${record.config.threshold_operator} ${record.config.threshold_value}`;
    }
    return record.config.function_package_name || 'Function-backed condition';
  }

  function runStatusTone(status: string) {
    if (status === 'success' || status === 'approved') return 'text-emerald-700 bg-emerald-50';
    if (status === 'failure' || status === 'rejected') return 'text-rose-700 bg-rose-50';
    return 'text-amber-700 bg-amber-50';
  }

  onMount(() => {
    const params = new URLSearchParams(window.location.search);
    const prefetchedQuery = params.get('q');
    if (prefetchedQuery) {
      draft.description = `Seeded from Object Explorer query "${prefetchedQuery}".`;
      draft.name = `Monitor for ${prefetchedQuery}`;
    }
    void load();
  });
</script>

<svelte:head>
  <title>OpenFoundry - Object Monitors</title>
</svelte:head>

<div class="space-y-5">
  <section class="overflow-hidden rounded-[30px] border border-[var(--border-default)] bg-[linear-gradient(135deg,#fbfcff_0%,#f4f6ff_40%,#f8f3ea_100%)] shadow-[var(--shadow-panel)]">
    <div class="grid gap-6 px-6 py-6 lg:grid-cols-[minmax(0,1.35fr)_330px] lg:px-8">
      <div>
        <div class="of-eyebrow">Object Monitors <span class="ml-2 rounded-full bg-[#fff4d6] px-2 py-0.5 text-[10px] uppercase tracking-[0.18em] text-[#8f5a00]">Sunset</span></div>
        <h1 class="mt-3 max-w-4xl text-[34px] font-semibold tracking-[-0.03em] text-[var(--text-strong)]">
          Watch ontology changes, alert subscribers, and automate remediation when conditions are met.
        </h1>
        <p class="mt-4 max-w-3xl text-[15px] leading-7 text-[var(--text-muted)]">
          This dedicated Object Monitors surface mirrors the legacy product model: input sets from
          Object Explorer, event or threshold conditions, subscriber notifications, action handoff,
          activity history, error tracking, and expiration management. Under the hood, monitors run
          on top of workflow automation primitives so migration to newer automation surfaces stays
          straightforward.
        </p>

        <div class="mt-6 flex flex-wrap gap-3">
          <button class="of-btn of-btn-primary" type="button" onclick={beginCreate}>
            <Glyph name="plus" size={16} />
            <span>Add monitor</span>
          </button>
          <a href="/object-explorer" class="of-btn">
            <Glyph name="search" size={16} />
            <span>Open Object Explorer</span>
          </a>
          <a href="/workflows" class="of-btn">
            <Glyph name="run" size={16} />
            <span>Open Automate runtime</span>
          </a>
        </div>
      </div>

      <aside class="rounded-[22px] border border-white/80 bg-white/82 p-5 backdrop-blur">
        <div class="of-heading-sm">Overview</div>
        <div class="mt-4 grid gap-3">
          <article class="rounded-[18px] border border-[var(--border-subtle)] bg-white px-4 py-3">
            <div class="text-[11px] font-semibold uppercase tracking-[0.18em] text-[var(--text-soft)]">Total monitors</div>
            <div class="mt-1 text-2xl font-semibold text-[var(--text-strong)]">{overviewStats.total}</div>
          </article>
          <article class="rounded-[18px] border border-[var(--border-subtle)] bg-white px-4 py-3">
            <div class="text-[11px] font-semibold uppercase tracking-[0.18em] text-[var(--text-soft)]">Muted</div>
            <div class="mt-1 text-2xl font-semibold text-[var(--text-strong)]">{overviewStats.muted}</div>
          </article>
          <article class="rounded-[18px] border border-[var(--border-subtle)] bg-white px-4 py-3">
            <div class="text-[11px] font-semibold uppercase tracking-[0.18em] text-[var(--text-soft)]">Errors</div>
            <div class="mt-1 text-2xl font-semibold text-[var(--text-strong)]">{overviewStats.errors}</div>
          </article>
          <article class="rounded-[18px] border border-[var(--border-subtle)] bg-white px-4 py-3">
            <div class="text-[11px] font-semibold uppercase tracking-[0.18em] text-[var(--text-soft)]">Expiring soon</div>
            <div class="mt-1 text-2xl font-semibold text-[var(--text-strong)]">{overviewStats.expiring}</div>
          </article>
        </div>
      </aside>
    </div>
  </section>

  {#if error}
    <div class="of-inline-note">{error}</div>
  {/if}

  <section class="grid gap-5 xl:grid-cols-[340px_minmax(0,1fr)_380px]">
    <aside class="of-panel overflow-hidden">
      <div class="border-b border-[var(--border-subtle)] px-5 py-4">
        <div class="flex items-center justify-between gap-3">
          <div>
            <div class="of-heading-sm">Monitors</div>
            <div class="mt-1 text-sm text-[var(--text-muted)]">Full list with status, errors, expiration, and activity posture.</div>
          </div>
          <span class="of-chip">{monitorRecords.length}</span>
        </div>
        <input bind:value={search} placeholder="Search monitors, saved explorations, or types" class="of-input mt-4" />
      </div>

      {#if loading}
        <div class="px-5 py-16 text-center text-sm text-[var(--text-muted)]">Loading monitors...</div>
      {:else if monitorRecords.length === 0}
        <div class="px-5 py-16 text-center text-sm text-[var(--text-muted)]">
          No object monitors yet. Create the first one from a saved Object Explorer list.
        </div>
      {:else}
        <div class="space-y-3 px-4 py-4">
          {#each monitorRecords as record (record.workflow.id)}
            <button
              type="button"
              class={`w-full rounded-[18px] border p-4 text-left transition ${
                selectedMonitor?.workflow.id === record.workflow.id
                  ? 'border-[#b9caec] bg-[#f7f9fd]'
                  : 'border-[var(--border-default)] hover:border-[#c8d4eb] hover:bg-[#fbfcff]'
              }`}
              onclick={() => selectMonitor(record.workflow.id)}
            >
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <div class="truncate text-[15px] font-semibold text-[var(--text-strong)]">{record.workflow.name.replace(/^\[Monitor\]\s*/, '')}</div>
                  <div class="mt-1 text-xs text-[var(--text-muted)]">{objectSetLabel(record.config.input_object_set_id)}</div>
                  <div class="mt-2 text-sm text-[var(--text-default)]">{conditionSummary(record)}</div>
                </div>
                <span class={`rounded-full px-2 py-1 text-[11px] ${record.config.muted ? 'bg-slate-100 text-slate-600' : 'bg-emerald-50 text-emerald-700'}`}>
                  {record.config.muted ? 'Muted' : 'Active'}
                </span>
              </div>

              <div class="mt-3 flex flex-wrap gap-2">
                {#if record.failedRuns > 0}
                  <span class="of-chip bg-rose-50 text-rose-700">{record.failedRuns} errors</span>
                {/if}
                {#if record.pendingApprovals > 0}
                  <span class="of-chip bg-amber-50 text-amber-700">{record.pendingApprovals} approvals</span>
                {/if}
                {#if record.expiringSoon}
                  <span class="of-chip bg-[#fff4d6] text-[#8f5a00]">Expiring soon</span>
                {/if}
              </div>
            </button>
          {/each}
        </div>
      {/if}
    </aside>

    <div class="space-y-5">
      <section class="of-panel overflow-hidden">
        <div class="flex items-center justify-between border-b border-[var(--border-subtle)] px-5 py-4">
          <div>
            <div class="of-heading-sm">{showCreate ? 'Create monitor' : selectedMonitor ? 'Monitor configuration' : 'Create monitor'}</div>
            <div class="mt-1 text-sm text-[var(--text-muted)]">
              {showCreate
                ? 'Configure inputs, conditions, subscribers, notifications, and optional action remediations.'
                : 'Edit the selected monitor or create a new one from existing saved explorations.'}
            </div>
          </div>
          <div class="flex gap-2">
            {#if !showCreate && selectedMonitor}
              <button class="of-btn" type="button" onclick={() => {
                showCreate = true;
                hydrateDraft(selectedMonitor);
              }}>
                <Glyph name="settings" size={15} />
                <span>Edit</span>
              </button>
            {/if}
            <button class="of-btn of-btn-primary" type="button" onclick={saveMonitor} disabled={saving}>
              <Glyph name="bookmark" size={15} />
              <span>{saving ? 'Saving...' : draft.id ? 'Save changes' : 'Save monitor'}</span>
            </button>
          </div>
        </div>

        <div class="grid gap-4 px-5 py-5 lg:grid-cols-2">
          <label class="block text-sm">
            <span class="mb-1 block font-medium text-[var(--text-default)]">Monitor name</span>
            <input bind:value={draft.name} class="of-input" />
          </label>
          <label class="block text-sm">
            <span class="mb-1 block font-medium text-[var(--text-default)]">Save location</span>
            <input bind:value={draft.save_location} class="of-input" placeholder="Private or shared project" />
          </label>

          <label class="block text-sm lg:col-span-2">
            <span class="mb-1 block font-medium text-[var(--text-default)]">Description</span>
            <textarea bind:value={draft.description} rows={3} class="of-input min-h-[96px]"></textarea>
          </label>

          <label class="block text-sm">
            <span class="mb-1 block font-medium text-[var(--text-default)]">Input saved exploration</span>
            <select bind:value={draft.input_object_set_id} class="of-select">
              <option value="">Choose Object Set</option>
              {#each objectSets as item (item.id)}
                <option value={item.id}>{item.name}</option>
              {/each}
            </select>
          </label>

          <label class="block text-sm">
            <span class="mb-1 block font-medium text-[var(--text-default)]">Expiration</span>
            <input bind:value={draft.expiration_at} type="datetime-local" class="of-input" />
          </label>
        </div>

        <div class="border-t border-[var(--border-subtle)] px-5 py-5">
          <div class="of-heading-sm">Condition</div>
          <div class="mt-4 grid gap-3 xl:grid-cols-3">
            {#each conditionKinds as item}
              <button
                type="button"
                class={`rounded-[18px] border p-4 text-left ${
                  draft.condition_kind === item.id ? 'border-[#b9caec] bg-[#f7f9fd]' : 'border-[var(--border-default)] hover:bg-[var(--bg-hover)]'
                }`}
                onclick={() => draft.condition_kind = item.id}
              >
                <div class="text-sm font-medium text-[var(--text-strong)]">{item.label}</div>
                <div class="mt-1 text-sm text-[var(--text-muted)]">{item.note}</div>
              </button>
            {/each}
          </div>

          {#if draft.condition_kind === 'event'}
            <div class="mt-4 grid gap-4 lg:grid-cols-2">
              <label class="block text-sm">
                <span class="mb-1 block font-medium text-[var(--text-default)]">Event type</span>
                <select bind:value={draft.event_type} class="of-select">
                  {#each eventTypes as item}
                    <option value={item.id}>{item.label}</option>
                  {/each}
                </select>
              </label>
              <div class="rounded-[18px] border border-[var(--border-subtle)] bg-[#fbfcfe] px-4 py-4 text-sm text-[var(--text-muted)]">
                Realtime-style monitoring is modeled here as event-driven workflow evaluation. Use this for watched searches, objects added/removed, or changes that should drive discrete activity events.
              </div>
            </div>
          {:else if draft.condition_kind === 'threshold'}
            <div class="mt-4 grid gap-4 lg:grid-cols-3">
              <label class="block text-sm">
                <span class="mb-1 block font-medium text-[var(--text-default)]">Metric</span>
                <input bind:value={draft.threshold_metric} class="of-input" placeholder="count or amount" />
              </label>
              <label class="block text-sm">
                <span class="mb-1 block font-medium text-[var(--text-default)]">Operator</span>
                <select bind:value={draft.threshold_operator} class="of-select">
                  <option value=">">&gt;</option>
                  <option value=">=">&gt;=</option>
                  <option value="<">&lt;</option>
                  <option value="<=">&lt;=</option>
                  <option value="=">=</option>
                </select>
              </label>
              <label class="block text-sm">
                <span class="mb-1 block font-medium text-[var(--text-default)]">Threshold</span>
                <input bind:value={draft.threshold_value} class="of-input" />
              </label>
            </div>
          {:else}
            <div class="mt-4 grid gap-4 lg:grid-cols-[minmax(0,1fr)_320px]">
              <label class="block text-sm">
                <span class="mb-1 block font-medium text-[var(--text-default)]">Function package</span>
                <select bind:value={draft.function_package_id} class="of-select">
                  <option value="">Choose function package</option>
                  {#each functionPackages as item (item.id)}
                    <option value={item.id}>{item.display_name}</option>
                  {/each}
                </select>
              </label>
              <div class="rounded-[18px] border border-[var(--border-subtle)] bg-[#fbfcfe] px-4 py-4 text-sm text-[var(--text-muted)]">
                Function-backed conditions are best for complex monitor logic that cannot be expressed as a single event or threshold condition.
              </div>
            </div>
          {/if}
        </div>

        <div class="grid gap-5 border-t border-[var(--border-subtle)] px-5 py-5 xl:grid-cols-2">
          <div>
            <div class="of-heading-sm">Notifications</div>
            <div class="mt-4 space-y-4">
              <label class="block text-sm">
                <span class="mb-1 block font-medium text-[var(--text-default)]">Notification title</span>
                <input bind:value={draft.notification_title} class="of-input" />
              </label>
              <label class="block text-sm">
                <span class="mb-1 block font-medium text-[var(--text-default)]">Message</span>
                <textarea bind:value={draft.notification_message} rows={4} class="of-input min-h-[112px]"></textarea>
              </label>
              <div class="block text-sm">
                <span class="mb-2 block font-medium text-[var(--text-default)]">Channels</span>
                <div class="flex flex-wrap gap-2">
                  {#each ['in_app', 'email', 'sms', 'webhook'] as channel}
                    <button
                      type="button"
                      class={`rounded-full border px-3 py-1.5 text-xs ${
                        draft.notification_channels.includes(channel)
                          ? 'border-[#9bb7e8] bg-[#edf4ff] text-[#2458b8]'
                          : 'border-[var(--border-default)] text-[var(--text-muted)]'
                      }`}
                      onclick={() => {
                        if (draft.notification_channels.includes(channel)) {
                          draft.notification_channels = draft.notification_channels.filter((item) => item !== channel);
                        } else {
                          draft.notification_channels = [...draft.notification_channels, channel];
                        }
                      }}
                    >
                      {channel}
                    </button>
                  {/each}
                </div>
              </div>
            </div>
          </div>

          <div>
            <div class="of-heading-sm">Subscribers and action</div>
            <div class="mt-4 space-y-4">
              <label class="block text-sm">
                <span class="mb-1 block font-medium text-[var(--text-default)]">Subscribers</span>
                <textarea bind:value={draft.subscribers_text} rows={5} class="of-input min-h-[130px]" placeholder="ops@example.com&#10;regional-watch@example.com"></textarea>
              </label>
              <label class="block text-sm">
                <span class="mb-1 block font-medium text-[var(--text-default)]">Optional action remediation</span>
                <select bind:value={draft.action_type_id} class="of-select">
                  <option value="">No action</option>
                  {#each selectedActionTypes as item (item.id)}
                    <option value={item.id}>{item.display_name}</option>
                  {/each}
                </select>
              </label>
              <label class="flex items-center gap-3 rounded-[16px] border border-[var(--border-subtle)] px-4 py-3 text-sm">
                <input bind:checked={draft.muted} type="checkbox" />
                <span>Mute notifications for this monitor while preserving evaluation and activity logging.</span>
              </label>
            </div>
          </div>
        </div>
      </section>

      <section class="grid gap-5 lg:grid-cols-[minmax(0,1fr)_360px]">
        <div class="of-panel p-5">
          <div class="flex items-center justify-between">
            <div>
              <div class="of-heading-sm">Recent activity</div>
              <div class="mt-1 text-sm text-[var(--text-muted)]">Timeline of monitor evaluations, edits, mute events, and deletes.</div>
            </div>
            <span class="of-chip">{selectedMonitorEvents.length}</span>
          </div>
          <div class="mt-4 space-y-3">
            {#if selectedMonitorEvents.length === 0}
              <div class="rounded-[18px] border border-dashed border-[var(--border-default)] px-4 py-10 text-center text-sm text-[var(--text-muted)]">
                No recorded activity for the selected monitor yet.
              </div>
            {:else}
              {#each selectedMonitorEvents.slice(0, 10) as event (event.id)}
                <article class="rounded-[18px] border border-[var(--border-subtle)] p-4">
                  <div class="flex items-center justify-between gap-3">
                    <div class="text-sm font-medium text-[var(--text-strong)]">{event.action.replaceAll('.', ' ')}</div>
                    <span class={`rounded-full px-2 py-1 text-[11px] ${runStatusTone(event.status)}`}>{event.status}</span>
                  </div>
                  <div class="mt-1 text-sm text-[var(--text-muted)]">{formatDate(event.occurred_at)}</div>
                  {#if Object.keys(event.metadata).length > 0}
                    <div class="mt-2 text-sm text-[var(--text-default)]">{JSON.stringify(event.metadata)}</div>
                  {/if}
                </article>
              {/each}
            {/if}
          </div>
        </div>

        <div class="of-panel p-5">
          <div class="of-heading-sm">Errors and anomalies</div>
          <div class="mt-1 text-sm text-[var(--text-muted)]">
            Surface failing runs, expiring monitors, and cross-platform anomalies close to the monitor detail view.
          </div>

          <div class="mt-4 space-y-3">
            {#if selectedMonitor && selectedMonitor.failedRuns === 0 && !selectedMonitor.expiringSoon && anomalies.length === 0}
              <div class="rounded-[18px] border border-dashed border-[var(--border-default)] px-4 py-10 text-center text-sm text-[var(--text-muted)]">
                No active monitor issues detected.
              </div>
            {/if}

            {#if selectedMonitor?.failedRuns}
              <article class="rounded-[18px] border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700">
                <div class="font-medium">{selectedMonitor.failedRuns} failed monitor events</div>
                <div class="mt-1">Review audit history and workflow runtime for the last failing evaluation.</div>
              </article>
            {/if}

            {#if selectedMonitor?.expiringSoon}
              <article class="rounded-[18px] border border-[#f0d08b] bg-[#fff7e4] p-4 text-sm text-[#8f5a00]">
                <div class="font-medium">Monitor expiring soon</div>
                <div class="mt-1">Extend expiration to keep alerts, actions, and subscriptions live.</div>
              </article>
            {/if}

            {#each anomalies.slice(0, 3) as anomaly (anomaly.id)}
              <article class="rounded-[18px] border border-[var(--border-subtle)] p-4">
                <div class="text-sm font-medium text-[var(--text-strong)]">{anomaly.title}</div>
                <div class="mt-1 text-sm text-[var(--text-muted)]">{anomaly.description}</div>
                <div class="mt-2 text-xs text-[var(--text-soft)]">{formatDate(anomaly.detected_at)}</div>
              </article>
            {/each}
          </div>
        </div>
      </section>
    </div>

    <aside class="space-y-5">
      <section class="of-panel overflow-hidden">
        <div class="border-b border-[var(--border-subtle)] px-5 py-4">
          <div class="of-heading-sm">Monitor details</div>
          <div class="mt-1 text-sm text-[var(--text-muted)]">
            Input, subscribers, action posture, runtime history, and expiration controls.
          </div>
        </div>

        {#if !selectedMonitor}
          <div class="px-5 py-12 text-center text-sm text-[var(--text-muted)]">
            Select a monitor to view details and recent runs.
          </div>
        {:else}
          <div class="space-y-5 px-5 py-5">
            <div>
              <div class="text-[16px] font-semibold text-[var(--text-strong)]">{selectedMonitor.workflow.name.replace(/^\[Monitor\]\s*/, '')}</div>
              <div class="mt-1 text-sm text-[var(--text-muted)]">{selectedMonitor.workflow.description || 'No description provided.'}</div>
            </div>

            <div class="grid gap-3 sm:grid-cols-2">
              <article class="rounded-[16px] border border-[var(--border-subtle)] bg-[#fbfcfe] px-4 py-3">
                <div class="text-[11px] font-semibold uppercase tracking-[0.18em] text-[var(--text-soft)]">Input</div>
                <div class="mt-1 text-sm font-medium text-[var(--text-strong)]">{objectSetLabel(selectedMonitor.config.input_object_set_id)}</div>
              </article>
              <article class="rounded-[16px] border border-[var(--border-subtle)] bg-[#fbfcfe] px-4 py-3">
                <div class="text-[11px] font-semibold uppercase tracking-[0.18em] text-[var(--text-soft)]">Object type</div>
                <div class="mt-1 text-sm font-medium text-[var(--text-strong)]">{selectedMonitor.objectType?.display_name ?? 'Mixed/unknown'}</div>
              </article>
            </div>

            <div>
              <div class="mb-2 of-heading-sm">Condition</div>
              <div class="rounded-[18px] border border-[var(--border-subtle)] p-4 text-sm text-[var(--text-default)]">
                {conditionSummary(selectedMonitor)}
              </div>
            </div>

            <div>
              <div class="mb-2 of-heading-sm">Subscribers</div>
              <div class="space-y-2">
                {#if selectedMonitor.config.subscribers.length === 0}
                  <div class="rounded-[16px] border border-dashed border-[var(--border-default)] px-3 py-5 text-sm text-[var(--text-muted)]">
                    No subscribers configured.
                  </div>
                {:else}
                  {#each selectedMonitor.config.subscribers as subscriber}
                    <div class="flex items-center justify-between rounded-[14px] border border-[var(--border-subtle)] px-3 py-2 text-sm">
                      <span>{subscriber.label}</span>
                      <span class="text-[var(--text-muted)]">{subscriber.channels.join(', ')}</span>
                    </div>
                  {/each}
                {/if}
              </div>
            </div>

            <div>
              <div class="mb-2 of-heading-sm">Details</div>
              <div class="space-y-2">
                <div class="flex items-center justify-between rounded-[14px] border border-[var(--border-subtle)] px-3 py-2 text-sm">
                  <span class="text-[var(--text-muted)]">Last evaluated</span>
                  <span>{formatDate(selectedMonitor.workflow.last_triggered_at)}</span>
                </div>
                <div class="flex items-center justify-between rounded-[14px] border border-[var(--border-subtle)] px-3 py-2 text-sm">
                  <span class="text-[var(--text-muted)]">Next poll</span>
                  <span>{formatDate(selectedMonitor.workflow.next_run_at)}</span>
                </div>
                <div class="flex items-center justify-between rounded-[14px] border border-[var(--border-subtle)] px-3 py-2 text-sm">
                  <span class="text-[var(--text-muted)]">Expiration</span>
                  <span>{formatDate(selectedMonitor.config.expiration_at)}</span>
                </div>
                <div class="flex items-center justify-between rounded-[14px] border border-[var(--border-subtle)] px-3 py-2 text-sm">
                  <span class="text-[var(--text-muted)]">Save location</span>
                  <span>{selectedMonitor.config.save_location}</span>
                </div>
              </div>
            </div>

            <div class="flex flex-wrap gap-2">
              <button class="of-btn of-btn-primary" type="button" onclick={() => runMonitor(selectedMonitor)} disabled={running}>
                <Glyph name="run" size={15} />
                <span>{running ? 'Evaluating...' : 'Evaluate now'}</span>
              </button>
              <button class="of-btn" type="button" onclick={() => toggleMute(selectedMonitor)}>
                <Glyph name="bell" size={15} />
                <span>{selectedMonitor.config.muted ? 'Unmute' : 'Mute'}</span>
              </button>
              <button class="of-btn" type="button" onclick={() => extendExpiration(selectedMonitor)}>
                <Glyph name="history" size={15} />
                <span>Extend 3 months</span>
              </button>
              <button class="of-btn" type="button" onclick={() => removeMonitor(selectedMonitor)}>
                <Glyph name="logout" size={15} />
                <span>Delete</span>
              </button>
            </div>
          </div>
        {/if}
      </section>

      <section class="of-panel p-5">
        <div class="of-heading-sm">Recent runs</div>
        <div class="mt-1 text-sm text-[var(--text-muted)]">Runtime history for the selected monitor workflow.</div>
        <div class="mt-4 space-y-3">
          {#if runs.length === 0}
            <div class="rounded-[18px] border border-dashed border-[var(--border-default)] px-4 py-10 text-center text-sm text-[var(--text-muted)]">
              No workflow runs recorded yet.
            </div>
          {:else}
            {#each runs as run (run.id)}
              <article class="rounded-[18px] border border-[var(--border-subtle)] p-4">
                <div class="flex items-center justify-between gap-3">
                  <div class="text-sm font-medium text-[var(--text-strong)] capitalize">{run.trigger_type}</div>
                  <span class={`rounded-full px-2 py-1 text-[11px] ${runStatusTone(run.status)}`}>{run.status}</span>
                </div>
                <div class="mt-1 text-sm text-[var(--text-muted)]">Started {formatDate(run.started_at)}</div>
                {#if run.error_message}
                  <div class="mt-2 text-sm text-rose-700">{run.error_message}</div>
                {/if}
              </article>
            {/each}
          {/if}
        </div>
      </section>
    </aside>
  </section>
</div>
