<script lang="ts">
  import { onMount } from 'svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import ActionExecutor from '$lib/components/ontology/ActionExecutor.svelte';
  import { listEvents, type AuditEvent } from '$lib/api/audit';
  import {
    createActionType,
    createActionWhatIfBranch,
    deleteActionType,
    deleteActionWhatIfBranch,
    executeAction,
    executeActionBatch,
    listActionTypes,
    listActionWhatIfBranches,
    listFunctionPackages,
    listObjectTypes,
    listObjects,
    listProperties,
    updateActionType,
    updateProperty,
    validateAction,
    type ActionAuthorizationPolicy,
    type ActionFormSchema,
    type ActionInputField,
    type ActionOperationKind,
    type ActionType,
    type ActionWhatIfBranch,
    type ExecuteActionResponse,
    type ExecuteBatchActionResponse,
    type FunctionPackageSummary,
    type ObjectInstance,
    type ObjectType,
    type Property,
    type ValidateActionResponse
  } from '$lib/api/ontology';

  type WorkbenchTab = 'authoring' | 'operate' | 'inline-edits' | 'monitoring';

  interface NotificationSideEffectDraft {
    title: string;
    body: string;
    severity?: string;
    category?: string;
    channels?: string[];
    user_ids?: string[];
    user_id_input_name?: string;
    target_user_property_name?: string;
    send_to_actor?: boolean;
    send_to_target_creator?: boolean;
    broadcast?: boolean;
    metadata?: Record<string, unknown>;
  }

  interface ActionConfigEnvelopeDraft {
    operation: unknown;
    notification_side_effects: NotificationSideEffectDraft[];
  }

  const workbenchTabs: { id: WorkbenchTab; label: string; glyph: 'code' | 'run' | 'object' | 'history' }[] = [
    { id: 'authoring', label: 'Authoring', glyph: 'code' },
    { id: 'operate', label: 'Operate', glyph: 'run' },
    { id: 'inline-edits', label: 'Inline edits', glyph: 'object' },
    { id: 'monitoring', label: 'Monitoring', glyph: 'history' }
  ];

  const propertyTypeOptions = [
    'string',
    'integer',
    'float',
    'boolean',
    'date',
    'json',
    'array',
    'reference',
    'geo_point',
    'media_reference',
    'vector'
  ];

  const operationKinds: { value: ActionOperationKind; label: string; detail: string }[] = [
    {
      value: 'update_object',
      label: 'Update object',
      detail: 'Map validated inputs into object property mutations.'
    },
    {
      value: 'create_link',
      label: 'Create link',
      detail: 'Create governed relationships from a selected source object.'
    },
    {
      value: 'delete_object',
      label: 'Delete object',
      detail: 'Require confirmation and log high-risk destructive changes.'
    },
    {
      value: 'invoke_function',
      label: 'Invoke function',
      detail: 'Run function-backed logic with ontology-aware context and optional patches.'
    },
    {
      value: 'invoke_webhook',
      label: 'Invoke webhook',
      detail: 'Call an external system and attach side effects or notifications.'
    }
  ];

  const actionTemplates: Record<
    ActionOperationKind,
    { inputSchema: ActionInputField[]; config: Record<string, unknown>; notes: string }
  > = {
    update_object: {
      inputSchema: [
        {
          name: 'status',
          display_name: 'Status',
          description: 'New property value to write onto the selected object.',
          property_type: 'string',
          required: true
        }
      ],
      config: {
        property_mappings: [{ property_name: 'status', input_name: 'status' }]
      },
      notes: 'Maps structured parameters onto object properties. Optional static_patch can add fixed values.'
    },
    create_link: {
      inputSchema: [
        {
          name: 'related_object_id',
          display_name: 'Related object ID',
          description: 'UUID of the object that should be linked.',
          property_type: 'reference',
          required: true
        },
        {
          name: 'link_properties',
          display_name: 'Link properties',
          description: 'Optional metadata stored on the created relationship.',
          property_type: 'json',
          required: false,
          default_value: {}
        }
      ],
      config: {
        link_type_id: '00000000-0000-0000-0000-000000000000',
        target_input_name: 'related_object_id',
        source_role: 'source',
        properties_input_name: 'link_properties'
      },
      notes: 'Replace link_type_id with a real link type and tune source_role before publishing.'
    },
    delete_object: {
      inputSchema: [],
      config: {},
      notes: 'Destructive operations should usually require confirmation and a justification.'
    },
    invoke_function: {
      inputSchema: [
        {
          name: 'payload',
          display_name: 'Payload',
          description: 'Any JSON body passed into the function runtime.',
          property_type: 'json',
          required: false,
          default_value: {}
        }
      ],
      config: {
        runtime: 'typescript',
        source: `export default async function handler(context) {
  const targetId = context.targetObject?.id;
  const related = await context.sdk.ontology.search({
    query: context.parameters.payload?.query ?? 'customer health',
    kind: 'object_instance',
    limit: 5,
  });

  return {
    output: {
      targetId,
      related,
    },
  };
}`
      },
      notes: 'Use inline source or replace it with {"function_package_id":"..."} to bind a reusable package.'
    },
    invoke_webhook: {
      inputSchema: [
        {
          name: 'event',
          display_name: 'Event body',
          description: 'JSON event fragment sent to the external webhook.',
          property_type: 'json',
          required: false,
          default_value: {}
        }
      ],
      config: {
        url: 'https://example.com/webhooks/action',
        method: 'POST',
        headers: {}
      },
      notes: 'Webhook actions return the external response and can fan out notifications after completion.'
    }
  };

  let objectTypes = $state<ObjectType[]>([]);
  let actions = $state<ActionType[]>([]);
  let properties = $state<Property[]>([]);
  let objects = $state<ObjectInstance[]>([]);
  let functionPackages = $state<FunctionPackageSummary[]>([]);
  let auditEvents = $state<AuditEvent[]>([]);
  let whatIfBranches = $state<ActionWhatIfBranch[]>([]);

  let loading = $state(true);
  let catalogLoading = $state(false);
  let auditLoading = $state(false);
  let branchesLoading = $state(false);
  let savingAction = $state(false);
  let deletingAction = $state(false);
  let validatingActionRun = $state(false);
  let executingActionRun = $state(false);
  let batchingActionRun = $state(false);
  let creatingWhatIf = $state(false);
  let bindingInlinePropertyId = $state('');

  let pageError = $state('');
  let actionError = $state('');
  let actionSuccess = $state('');
  let runError = $state('');
  let branchError = $state('');
  let inlineEditError = $state('');
  let inlineEditSuccess = $state('');

  let selectedTypeId = $state('');
  let selectedActionId = $state('');
  let selectedTargetObjectId = $state('');
  let activeTab = $state<WorkbenchTab>('authoring');
  let catalogQuery = $state('');

  let draftName = $state('');
  let draftDisplayName = $state('');
  let draftDescription = $state('');
  let draftOperationKind = $state<ActionOperationKind>('update_object');
  let draftConfirmationRequired = $state(false);
  let draftPermissionKey = $state('');
  let draftAuthorizationPolicyText = $state('{\n  "required_permission_keys": [],\n  "any_role": [],\n  "all_roles": [],\n  "attribute_equals": {},\n  "allowed_markings": [],\n  "minimum_clearance": null,\n  "deny_guest_sessions": false\n}');
  let draftInputSchema = $state<ActionInputField[]>([]);
  let draftFormSchemaText = $state('{\n  "sections": [],\n  "parameter_overrides": []\n}');
  let draftOperationConfigText = $state('{}');
  let draftNotificationEffectsText = $state('[]');

  let parameters = $state<Record<string, unknown>>({});
  let justification = $state('');
  let validation = $state<ValidateActionResponse | null>(null);
  let execution = $state<ExecuteActionResponse | null>(null);
  let batchExecution = $state<ExecuteBatchActionResponse | null>(null);
  let selectedBatchObjectIds = $state<string[]>([]);
  let whatIfName = $state('');
  let whatIfDescription = $state('');
  let inlineInputNames = $state<Record<string, string>>({});

  const selectedType = $derived(objectTypes.find((item) => item.id === selectedTypeId) ?? null);
  const selectedAction = $derived(actions.find((item) => item.id === selectedActionId) ?? null);
  const selectedTargetObject = $derived(objects.find((item) => item.id === selectedTargetObjectId) ?? null);
  const isCreateMode = $derived(!selectedActionId);
  const filteredActions = $derived.by(() => {
    const query = catalogQuery.trim().toLowerCase();
    if (!query) return actions;
    return actions.filter((item) =>
      `${item.display_name} ${item.name} ${item.description} ${item.operation_kind}`.toLowerCase().includes(query)
    );
  });
  const boundInlineProperties = $derived.by(() =>
    selectedActionId ? properties.filter((property) => property.inline_edit_config?.action_type_id === selectedActionId) : []
  );
  const actionEvents = $derived.by(() => {
    if (!selectedActionId) return [];
    return auditEvents.filter((event) => eventMatchesAction(event, selectedActionId));
  });
  const actionMetrics = $derived.by(() => {
    const events = actionEvents;
    return {
      executions: events.length,
      failed: events.filter((event) => event.status === 'failure').length,
      denied: events.filter((event) => event.status === 'denied').length,
      critical: events.filter((event) => event.severity === 'critical').length
    };
  });
  const draftChecklist = $derived.by(() => {
    const checks = [
      { label: 'Object type selected', ok: Boolean(selectedTypeId) },
      { label: 'Stable action name', ok: Boolean(draftName.trim() || selectedActionId) },
      { label: 'Display name provided', ok: Boolean(draftDisplayName.trim()) },
      { label: 'Input schema present or intentionally empty', ok: draftInputSchema.length >= 0 },
      { label: 'Form schema parses', ok: canParseJson(draftFormSchemaText) },
      { label: 'Operation config parses', ok: canParseJson(draftOperationConfigText) },
      { label: 'Notification side effects parse', ok: canParseJson(draftNotificationEffectsText) },
      { label: 'Authorization policy parses', ok: canParseJson(draftAuthorizationPolicyText) }
    ];

    if (draftConfirmationRequired) {
      checks.push({
        label: 'Confirmation policy is paired with a permission or auth policy',
        ok: Boolean(draftPermissionKey.trim() || draftAuthorizationPolicyText.includes('required_permission_keys'))
      });
    }

    return checks;
  });

  function prettyJson(value: unknown) {
    return JSON.stringify(value ?? null, null, 2);
  }

  function canParseJson(value: string) {
    try {
      JSON.parse(value);
      return true;
    } catch {
      return false;
    }
  }

  function parseJson<T>(value: string, label: string): T {
    try {
      return JSON.parse(value) as T;
    } catch (error) {
      throw new Error(`${label}: ${error instanceof Error ? error.message : 'Invalid JSON'}`);
    }
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

  function formatDate(value: string) {
    return new Date(value).toLocaleString();
  }

  function operationRequiresTarget(kind: ActionOperationKind) {
    return kind === 'update_object' || kind === 'create_link' || kind === 'delete_object';
  }

  function splitConfigEnvelope(config: unknown): ActionConfigEnvelopeDraft {
    if (!config || typeof config !== 'object' || Array.isArray(config)) {
      return { operation: config ?? {}, notification_side_effects: [] };
    }

    const record = config as Record<string, unknown>;
    if ('operation' in record || 'notification_side_effects' in record) {
      return {
        operation: record.operation ?? {},
        notification_side_effects: Array.isArray(record.notification_side_effects)
          ? (record.notification_side_effects as NotificationSideEffectDraft[])
          : []
      };
    }

    return { operation: config, notification_side_effects: [] };
  }

  function buildDefaultActionFormSchema(inputSchema: ActionInputField[]): ActionFormSchema {
    const requiredFields = inputSchema.filter((field) => field.required).map((field) => field.name);
    const optionalFields = inputSchema.filter((field) => !field.required).map((field) => field.name);
    const sections = [];

    if (requiredFields.length) {
      sections.push({
        id: 'core',
        title: 'Core inputs',
        description: 'The minimum information required to run this action.',
        columns: requiredFields.length > 1 ? 2 : 1,
        parameter_names: requiredFields
      });
    }

    if (optionalFields.length) {
      sections.push({
        id: sections.length ? 'optional' : 'main',
        title: sections.length ? 'Optional context' : 'Parameters',
        description: sections.length
          ? 'Additional context that can refine the action execution.'
          : 'Parameters accepted by this action.',
        columns: optionalFields.length > 1 ? 2 : 1,
        parameter_names: optionalFields
      });
    }

    return {
      sections,
      parameter_overrides: []
    };
  }

  function resetRunSurface() {
    parameters = {};
    justification = '';
    validation = null;
    execution = null;
    batchExecution = null;
    runError = '';
    branchError = '';
    selectedBatchObjectIds = [];
  }

  function createBlankDraft(typeId = selectedTypeId) {
    const template = actionTemplates[draftOperationKind];
    draftName = selectedActionId ? draftName : `${selectedType?.name ?? 'action'}_action`;
    draftDisplayName = '';
    draftDescription = '';
    draftOperationKind = 'update_object';
    draftConfirmationRequired = false;
    draftPermissionKey = '';
    draftAuthorizationPolicyText = prettyJson({
      required_permission_keys: [],
      any_role: [],
      all_roles: [],
      attribute_equals: {},
      allowed_markings: [],
      minimum_clearance: null,
      deny_guest_sessions: false
    });
    draftInputSchema = template.inputSchema.map((field) => ({ ...field }));
    draftFormSchemaText = prettyJson(buildDefaultActionFormSchema(template.inputSchema));
    draftOperationConfigText = prettyJson(template.config);
    draftNotificationEffectsText = prettyJson([]);
    selectedActionId = '';
    if (typeId) {
      selectedTypeId = typeId;
    }
    resetRunSurface();
    actionError = '';
    actionSuccess = '';
  }

  function syncDraftFromAction(action: ActionType | null) {
    if (!action) {
      createBlankDraft(selectedTypeId);
      return;
    }

    const envelope = splitConfigEnvelope(action.config);
    draftName = action.name;
    draftDisplayName = action.display_name;
    draftDescription = action.description;
    draftOperationKind = action.operation_kind;
    draftConfirmationRequired = action.confirmation_required;
    draftPermissionKey = action.permission_key ?? '';
    draftAuthorizationPolicyText = prettyJson(action.authorization_policy);
    draftInputSchema = action.input_schema.map((field) => ({ ...field }));
    draftFormSchemaText = prettyJson(action.form_schema);
    draftOperationConfigText = prettyJson(envelope.operation ?? {});
    draftNotificationEffectsText = prettyJson(envelope.notification_side_effects ?? []);
    resetRunSurface();
  }

  function setOperationTemplate(kind: ActionOperationKind) {
    const template = actionTemplates[kind];
    draftOperationKind = kind;
    draftInputSchema = template.inputSchema.map((field) => ({ ...field }));
    draftFormSchemaText = prettyJson(buildDefaultActionFormSchema(template.inputSchema));
    draftOperationConfigText = prettyJson(template.config);
    draftNotificationEffectsText = prettyJson([]);
  }

  function addInputField() {
    draftInputSchema = [
      ...draftInputSchema,
      {
        name: `parameter_${draftInputSchema.length + 1}`,
        display_name: '',
        description: '',
        property_type: 'string',
        required: false
      }
    ];
  }

  function updateInputField(index: number, patch: Partial<ActionInputField>) {
    draftInputSchema = draftInputSchema.map((field, candidateIndex) =>
      candidateIndex === index ? { ...field, ...patch } : field
    );
  }

  function removeInputField(index: number) {
    draftInputSchema = draftInputSchema.filter((_, candidateIndex) => candidateIndex !== index);
  }

  function eventMatchesAction(event: AuditEvent, actionId: string) {
    const metadataActionId = typeof event.metadata?.action_id === 'string' ? event.metadata.action_id : '';
    return event.resource_id === actionId || metadataActionId === actionId;
  }

  function currentInlineInputName(property: Property) {
    return (
      inlineInputNames[property.id] ??
      property.inline_edit_config?.input_name ??
      draftInputSchema[0]?.name ??
      ''
    );
  }

  async function refreshAuditEvents() {
    auditLoading = true;
    try {
      const response = await listEvents({ source_service: 'ontology-service' });
      auditEvents = response.items;
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to load action audit events';
    } finally {
      auditLoading = false;
    }
  }

  async function refreshWhatIfBranches() {
    if (!selectedActionId) {
      whatIfBranches = [];
      return;
    }

    branchesLoading = true;
    try {
      const response = await listActionWhatIfBranches(selectedActionId, { per_page: 100 });
      whatIfBranches = response.data;
    } catch (error) {
      branchError = error instanceof Error ? error.message : 'Failed to load what-if branches';
    } finally {
      branchesLoading = false;
    }
  }

  function primeInlineInputNames(nextProperties: Property[]) {
    const nextMap: Record<string, string> = {};
    for (const property of nextProperties) {
      nextMap[property.id] = property.inline_edit_config?.input_name ?? draftInputSchema[0]?.name ?? '';
    }
    inlineInputNames = nextMap;
  }

  async function loadTypeContext(typeId: string) {
    catalogLoading = true;
    pageError = '';
    try {
      const [propertyResponse, objectResponse, actionResponse] = await Promise.all([
        listProperties(typeId),
        listObjects(typeId, { per_page: 50 }),
        listActionTypes({ object_type_id: typeId, per_page: 100 })
      ]);

      properties = propertyResponse;
      objects = objectResponse.data;
      actions = actionResponse.data;

      if (!objects.some((item) => item.id === selectedTargetObjectId)) {
        selectedTargetObjectId = objects[0]?.id ?? '';
      }

      if (!actions.some((item) => item.id === selectedActionId)) {
        selectedActionId = actions[0]?.id ?? '';
      }

      syncDraftFromAction(actions.find((item) => item.id === selectedActionId) ?? null);
      primeInlineInputNames(propertyResponse);
      await refreshWhatIfBranches();
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to load action type context';
    } finally {
      catalogLoading = false;
    }
  }

  async function loadPage() {
    loading = true;
    pageError = '';

    try {
      const [typeResponse, functionResponse] = await Promise.all([
        listObjectTypes({ per_page: 200 }),
        listFunctionPackages({ per_page: 100 }).catch(() => ({ data: [], total: 0, page: 1, per_page: 100 }))
      ]);

      objectTypes = typeResponse.data;
      functionPackages = functionResponse.data;
      selectedTypeId = selectedTypeId || objectTypes[0]?.id || '';

      if (selectedTypeId) {
        await loadTypeContext(selectedTypeId);
      } else {
        createBlankDraft();
      }

      await refreshAuditEvents();
    } catch (error) {
      pageError = error instanceof Error ? error.message : 'Failed to load Action Types';
    } finally {
      loading = false;
    }
  }

  async function selectType(typeId: string) {
    selectedTypeId = typeId;
    selectedActionId = '';
    await loadTypeContext(typeId);
  }

  async function selectAction(actionId: string) {
    selectedActionId = actionId;
    syncDraftFromAction(actions.find((item) => item.id === actionId) ?? null);
    await refreshWhatIfBranches();
    primeInlineInputNames(properties);
  }

  async function saveAction() {
    actionError = '';
    actionSuccess = '';
    savingAction = true;

    try {
      if (!selectedTypeId) {
        throw new Error('Select an object type before saving an action');
      }

      const authorizationPolicy = parseJson<ActionAuthorizationPolicy>(
        draftAuthorizationPolicyText,
        'Authorization policy JSON'
      );
      const formSchema = parseJson<ActionFormSchema>(draftFormSchemaText, 'Form schema JSON');
      const operationConfig = parseJson<unknown>(draftOperationConfigText, 'Operation config JSON');
      const notificationEffects = parseJson<NotificationSideEffectDraft[]>(
        draftNotificationEffectsText,
        'Notification side effects JSON'
      );

      const payload = {
        display_name: draftDisplayName.trim() || draftName.trim(),
        description: draftDescription.trim(),
        operation_kind: draftOperationKind,
        input_schema: draftInputSchema.map((field) => ({
          ...field,
          display_name: field.display_name?.trim() || null,
          description: field.description?.trim() || null
        })),
        form_schema: formSchema,
        config: {
          operation: operationConfig,
          notification_side_effects: notificationEffects
        },
        confirmation_required: draftConfirmationRequired,
        permission_key: draftPermissionKey.trim() || undefined,
        authorization_policy: authorizationPolicy
      };

      if (selectedActionId) {
        await updateActionType(selectedActionId, payload);
        actionSuccess = 'Action type updated.';
      } else {
        if (!draftName.trim()) {
          throw new Error('Action name is required when creating a new action');
        }

        await createActionType({
          name: draftName.trim(),
          object_type_id: selectedTypeId,
          ...payload
        });
        actionSuccess = 'Action type created.';
      }

      await loadTypeContext(selectedTypeId);

      const candidateId = actions.find((item) => item.name === draftName.trim())?.id ?? '';
      if (!selectedActionId && candidateId) {
        selectedActionId = candidateId;
      }
    } catch (error) {
      actionError = error instanceof Error ? error.message : 'Failed to save action type';
    } finally {
      savingAction = false;
    }
  }

  async function removeAction() {
    if (!selectedActionId) return;
    if (typeof window !== 'undefined' && !window.confirm('Delete this action type?')) return;

    deletingAction = true;
    actionError = '';
    actionSuccess = '';

    try {
      await deleteActionType(selectedActionId);
      actionSuccess = 'Action type deleted.';
      selectedActionId = '';
      await loadTypeContext(selectedTypeId);
    } catch (error) {
      actionError = error instanceof Error ? error.message : 'Failed to delete action type';
    } finally {
      deletingAction = false;
    }
  }

  async function runValidation() {
    if (!selectedActionId) return;
    validatingActionRun = true;
    runError = '';
    validation = null;

    try {
      validation = await validateAction(selectedActionId, {
        target_object_id: selectedTargetObjectId || undefined,
        parameters
      });
    } catch (error) {
      runError = error instanceof Error ? error.message : 'Failed to validate action';
    } finally {
      validatingActionRun = false;
    }
  }

  async function runAction() {
    if (!selectedActionId) return;
    executingActionRun = true;
    runError = '';
    execution = null;

    try {
      execution = await executeAction(selectedActionId, {
        target_object_id: selectedTargetObjectId || undefined,
        parameters,
        justification: justification.trim() || undefined
      });
      await refreshAuditEvents();
    } catch (error) {
      runError = error instanceof Error ? error.message : 'Failed to execute action';
    } finally {
      executingActionRun = false;
    }
  }

  async function runBatch() {
    if (!selectedActionId || selectedBatchObjectIds.length === 0) return;
    batchingActionRun = true;
    runError = '';
    batchExecution = null;

    try {
      batchExecution = await executeActionBatch(selectedActionId, {
        target_object_ids: selectedBatchObjectIds,
        parameters,
        justification: justification.trim() || undefined
      });
      await refreshAuditEvents();
    } catch (error) {
      runError = error instanceof Error ? error.message : 'Failed to execute batch action';
    } finally {
      batchingActionRun = false;
    }
  }

  async function createWhatIfBranch() {
    if (!selectedActionId) return;
    creatingWhatIf = true;
    branchError = '';

    try {
      await createActionWhatIfBranch(selectedActionId, {
        target_object_id: selectedTargetObjectId || undefined,
        parameters,
        name: whatIfName.trim() || undefined,
        description: whatIfDescription.trim() || undefined
      });
      whatIfName = '';
      whatIfDescription = '';
      await refreshWhatIfBranches();
    } catch (error) {
      branchError = error instanceof Error ? error.message : 'Failed to create what-if branch';
    } finally {
      creatingWhatIf = false;
    }
  }

  async function removeWhatIfBranch(branchId: string) {
    if (!selectedActionId) return;
    try {
      await deleteActionWhatIfBranch(selectedActionId, branchId);
      await refreshWhatIfBranches();
    } catch (error) {
      branchError = error instanceof Error ? error.message : 'Failed to delete what-if branch';
    }
  }

  async function bindInlineEdit(property: Property) {
    if (!selectedTypeId || !selectedActionId) return;
    bindingInlinePropertyId = property.id;
    inlineEditError = '';
    inlineEditSuccess = '';

    try {
      await updateProperty(selectedTypeId, property.id, {
        inline_edit_config: {
          action_type_id: selectedActionId,
          input_name: currentInlineInputName(property) || undefined
        }
      });
      inlineEditSuccess = 'Inline edit binding updated.';
      await loadTypeContext(selectedTypeId);
    } catch (error) {
      inlineEditError = error instanceof Error ? error.message : 'Failed to update inline edit binding';
    } finally {
      bindingInlinePropertyId = '';
    }
  }

  async function clearInlineEdit(property: Property) {
    if (!selectedTypeId) return;
    bindingInlinePropertyId = property.id;
    inlineEditError = '';
    inlineEditSuccess = '';

    try {
      await updateProperty(selectedTypeId, property.id, { inline_edit_config: null });
      inlineEditSuccess = 'Inline edit binding removed.';
      await loadTypeContext(selectedTypeId);
    } catch (error) {
      inlineEditError = error instanceof Error ? error.message : 'Failed to clear inline edit binding';
    } finally {
      bindingInlinePropertyId = '';
    }
  }

  onMount(() => {
    void loadPage();
  });
</script>

<svelte:head>
  <title>OpenFoundry - Action Types</title>
</svelte:head>

<div class="mx-auto flex max-w-7xl flex-col gap-6 px-4 py-6">
  <section class="overflow-hidden rounded-[2rem] border border-slate-200 bg-[radial-gradient(circle_at_top_left,_rgba(15,118,110,0.18),_transparent_35%),linear-gradient(135deg,_#f8fafc_0%,_#eef7f5_45%,_#f8fafc_100%)] p-6 shadow-sm">
    <div class="grid gap-6 lg:grid-cols-[1.5fr_1fr]">
      <div class="space-y-4">
        <div class="inline-flex items-center gap-2 rounded-full border border-emerald-200 bg-white/80 px-3 py-1 text-xs font-semibold uppercase tracking-[0.24em] text-emerald-700">
          <Glyph name="run" size={14} />
          Define Ontologies / Action types
        </div>
        <div class="space-y-3">
          <h1 class="text-3xl font-semibold tracking-tight text-slate-950">Action Types</h1>
          <p class="max-w-3xl text-sm leading-6 text-slate-600">
            Author governed actions with parameter sections, confirmation flows, permissions, side effects, function-backed runtime, inline edits, action logs, batch execution, and what-if branches from one dedicated product surface.
          </p>
        </div>
        <div class="flex flex-wrap gap-3 text-xs text-slate-500">
          <a href="/object-views" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-emerald-300 hover:text-emerald-700">Object Views</a>
          <a href="/foundry-rules" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-emerald-300 hover:text-emerald-700">Foundry Rules</a>
          <a href="/object-monitors" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-emerald-300 hover:text-emerald-700">Object Monitors</a>
          <a href="/apps" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-emerald-300 hover:text-emerald-700">Workshop</a>
        </div>
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Action types</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{actions.length}</p>
          <p class="mt-1 text-sm text-slate-500">Configured for the selected object type.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Function packages</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{functionPackages.length}</p>
          <p class="mt-1 text-sm text-slate-500">Reusable backends for function-backed actions.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Inline edit bindings</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{boundInlineProperties.length}</p>
          <p class="mt-1 text-sm text-slate-500">Properties currently powered by this action.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Action log events</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{actionEvents.length}</p>
          <p class="mt-1 text-sm text-slate-500">Filtered from audit events emitted by the real runtime.</p>
        </div>
      </div>
    </div>
  </section>

  {#if pageError}
    <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{pageError}</div>
  {/if}

  {#if loading}
    <div class="rounded-3xl border border-slate-200 bg-white px-5 py-10 text-center text-sm text-slate-500">
      Loading action authoring surfaces...
    </div>
  {:else}
    <div class="grid gap-6 xl:grid-cols-[330px_minmax(0,1fr)]">
      <aside class="space-y-4">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="flex items-center justify-between gap-3">
            <div>
              <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Catalog</p>
              <h2 class="mt-1 text-lg font-semibold text-slate-950">Action inventory</h2>
            </div>
            <button
              class="inline-flex items-center gap-2 rounded-full bg-emerald-600 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-500"
              onclick={() => createBlankDraft(selectedTypeId)}
            >
              <Glyph name="plus" size={16} />
              New
            </button>
          </div>

          <div class="mt-4 space-y-3">
            <label for="action-types-object-type" class="block text-xs font-semibold uppercase tracking-[0.18em] text-slate-400">
              Object type
            </label>
            <select
              id="action-types-object-type"
              class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
              value={selectedTypeId}
              onchange={(event) => void selectType((event.currentTarget as HTMLSelectElement).value)}
            >
              {#each objectTypes as typeItem}
                <option value={typeItem.id}>{typeItem.display_name}</option>
              {/each}
            </select>

            <label for="action-types-search" class="block text-xs font-semibold uppercase tracking-[0.18em] text-slate-400">
              Search
            </label>
            <input
              id="action-types-search"
              class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
              type="text"
              bind:value={catalogQuery}
              placeholder="Search action names, descriptions, or operation kind"
            />
          </div>

          <div class="mt-4 space-y-2">
            {#if catalogLoading}
              <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-500">
                Refreshing catalog...
              </div>
            {:else if filteredActions.length === 0}
              <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-500">
                No action types match this view yet.
              </div>
            {:else}
              {#each filteredActions as action}
                <button
                  class={`w-full rounded-2xl border px-4 py-3 text-left transition ${
                    selectedActionId === action.id
                      ? 'border-emerald-400 bg-emerald-50'
                      : 'border-slate-200 bg-white hover:border-slate-300'
                  }`}
                  onclick={() => void selectAction(action.id)}
                >
                  <div class="flex items-start justify-between gap-3">
                    <div>
                      <p class="text-sm font-semibold text-slate-950">{action.display_name}</p>
                      <p class="mt-1 text-xs text-slate-500">{action.name}</p>
                    </div>
                    <span class="rounded-full bg-slate-100 px-2 py-1 text-[11px] font-medium uppercase tracking-[0.16em] text-slate-600">
                      {action.operation_kind}
                    </span>
                  </div>
                  <p class="mt-2 line-clamp-2 text-sm text-slate-600">{action.description || 'No description provided yet.'}</p>
                  <div class="mt-3 flex flex-wrap gap-2 text-[11px] text-slate-500">
                    {#if action.confirmation_required}
                      <span class="rounded-full border border-amber-200 bg-amber-50 px-2 py-1 text-amber-700">Confirmation</span>
                    {/if}
                    {#if action.permission_key}
                      <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1">{action.permission_key}</span>
                    {/if}
                  </div>
                </button>
              {/each}
            {/if}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Submission criteria</p>
          <div class="mt-4 space-y-3">
            {#each draftChecklist as check}
              <div class="flex items-center justify-between gap-3 rounded-2xl border border-slate-200 px-3 py-2.5">
                <span class="text-sm text-slate-700">{check.label}</span>
                <span class={`rounded-full px-2 py-1 text-[11px] font-medium uppercase tracking-[0.16em] ${check.ok ? 'bg-emerald-100 text-emerald-700' : 'bg-amber-100 text-amber-700'}`}>
                  {check.ok ? 'ready' : 'review'}
                </span>
              </div>
            {/each}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Operation notes</p>
          <p class="mt-3 text-sm leading-6 text-slate-600">
            {actionTemplates[draftOperationKind].notes}
          </p>
        </section>
      </aside>

      <main class="space-y-4">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-4 shadow-sm">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Workbench</p>
              <h2 class="mt-1 text-xl font-semibold text-slate-950">
                {isCreateMode ? 'New action type' : selectedAction?.display_name}
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

        {#if actionError}
          <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{actionError}</div>
        {/if}
        {#if actionSuccess}
          <div class="rounded-3xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">{actionSuccess}</div>
        {/if}

        {#if activeTab === 'authoring'}
          <div class="grid gap-4 2xl:grid-cols-[minmax(0,1.1fr)_380px]">
            <section class="space-y-4 rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              <div class="grid gap-4 md:grid-cols-2">
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Action name</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500 disabled:bg-slate-100"
                    type="text"
                    bind:value={draftName}
                    disabled={!isCreateMode}
                    placeholder="customer_escalate"
                  />
                </label>
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Display name</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                    type="text"
                    bind:value={draftDisplayName}
                    placeholder="Escalate customer"
                  />
                </label>
              </div>

              <label class="space-y-2 text-sm text-slate-700">
                <span class="font-medium">Description</span>
                <textarea
                  rows="3"
                  class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                  bind:value={draftDescription}
                  placeholder="Explain when this action should be used, what it changes, and what the reviewer should expect."
                ></textarea>
              </label>

              <div class="grid gap-4 md:grid-cols-2">
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Operation kind</span>
                  <select
                    class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                    value={draftOperationKind}
                    onchange={(event) => draftOperationKind = (event.currentTarget as HTMLSelectElement).value as ActionOperationKind}
                  >
                    {#each operationKinds as kind}
                      <option value={kind.value}>{kind.label}</option>
                    {/each}
                  </select>
                </label>
                <label class="space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Permission key</span>
                  <input
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                    type="text"
                    bind:value={draftPermissionKey}
                    placeholder="ontology.customer.escalate"
                  />
                </label>
              </div>

              <div class="rounded-3xl border border-slate-200 bg-slate-50 p-4">
                <div class="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">Operation template</p>
                    <p class="mt-1 text-sm text-slate-500">
                      Load a backend-valid starter for the selected operation kind.
                    </p>
                  </div>
                  <button
                    class="rounded-full border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700"
                    onclick={() => setOperationTemplate(draftOperationKind)}
                  >
                    Load template
                  </button>
                </div>

                <div class="mt-4 grid gap-3 md:grid-cols-2">
                  {#each operationKinds as kind}
                    <div class={`rounded-2xl border px-4 py-3 ${draftOperationKind === kind.value ? 'border-emerald-300 bg-white' : 'border-slate-200 bg-slate-50'}`}>
                      <p class="text-sm font-semibold text-slate-900">{kind.label}</p>
                      <p class="mt-1 text-sm leading-6 text-slate-500">{kind.detail}</p>
                    </div>
                  {/each}
                </div>
              </div>

              <div class="flex items-center gap-3 rounded-2xl border border-slate-200 px-4 py-3">
                <input
                  id="confirmation-required"
                  type="checkbox"
                  checked={draftConfirmationRequired}
                  onchange={(event) => draftConfirmationRequired = (event.currentTarget as HTMLInputElement).checked}
                />
                <label for="confirmation-required" class="text-sm text-slate-700">
                  Require confirmation and justification before execution
                </label>
              </div>

              <div class="rounded-3xl border border-slate-200 p-4">
                <div class="flex items-center justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">Input schema</p>
                    <p class="mt-1 text-sm text-slate-500">Structured parameters used by action forms, inline edits, batch execution, and what-if simulations.</p>
                  </div>
                  <button
                    class="rounded-full border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700"
                    onclick={addInputField}
                  >
                    Add field
                  </button>
                </div>

                <div class="mt-4 space-y-3">
                  {#if draftInputSchema.length === 0}
                    <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-500">
                      This action currently uses no user-entered parameters.
                    </div>
                  {:else}
                    {#each draftInputSchema as field, index}
                      <div class="rounded-2xl border border-slate-200 p-4">
                        <div class="grid gap-3 md:grid-cols-2">
                          <label class="space-y-2 text-sm text-slate-700">
                            <span class="font-medium">Parameter name</span>
                            <input
                              class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                              type="text"
                              value={field.name}
                              oninput={(event) => updateInputField(index, { name: (event.currentTarget as HTMLInputElement).value })}
                            />
                          </label>
                          <label class="space-y-2 text-sm text-slate-700">
                            <span class="font-medium">Display name</span>
                            <input
                              class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                              type="text"
                              value={field.display_name ?? ''}
                              oninput={(event) => updateInputField(index, { display_name: (event.currentTarget as HTMLInputElement).value })}
                            />
                          </label>
                        </div>

                        <div class="mt-3 grid gap-3 md:grid-cols-[1fr_220px_120px_auto]">
                          <label class="space-y-2 text-sm text-slate-700">
                            <span class="font-medium">Description</span>
                            <input
                              class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                              type="text"
                              value={field.description ?? ''}
                              oninput={(event) => updateInputField(index, { description: (event.currentTarget as HTMLInputElement).value })}
                            />
                          </label>
                          <label class="space-y-2 text-sm text-slate-700">
                            <span class="font-medium">Property type</span>
                            <select
                              class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                              value={field.property_type}
                              onchange={(event) => updateInputField(index, { property_type: (event.currentTarget as HTMLSelectElement).value })}
                            >
                              {#each propertyTypeOptions as propertyType}
                                <option value={propertyType}>{propertyType}</option>
                              {/each}
                            </select>
                          </label>
                          <label class="flex items-end gap-2 rounded-2xl border border-slate-200 px-4 py-3 text-sm text-slate-700">
                            <input
                              type="checkbox"
                              checked={field.required}
                              onchange={(event) => updateInputField(index, { required: (event.currentTarget as HTMLInputElement).checked })}
                            />
                            Required
                          </label>
                          <div class="flex items-end justify-end">
                            <button
                              class="rounded-full border border-rose-200 bg-rose-50 px-4 py-2 text-sm font-medium text-rose-700 hover:border-rose-300"
                              onclick={() => removeInputField(index)}
                            >
                              Remove
                            </button>
                          </div>
                        </div>
                      </div>
                    {/each}
                  {/if}
                </div>
              </div>

              <div class="grid gap-4 xl:grid-cols-2">
                <div class="rounded-3xl border border-slate-200 p-4">
                  <div class="flex items-center justify-between gap-3">
                    <div>
                      <p class="text-sm font-semibold text-slate-900">Configure sections</p>
                      <p class="mt-1 text-sm text-slate-500">Author form sections, overrides, and parameter visibility.</p>
                    </div>
                    <button
                      class="rounded-full border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700"
                      onclick={() => draftFormSchemaText = prettyJson(buildDefaultActionFormSchema(draftInputSchema))}
                    >
                      Generate
                    </button>
                  </div>
                  <textarea
                    rows="18"
                    class="mt-4 w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-emerald-500"
                    bind:value={draftFormSchemaText}
                    spellcheck="false"
                  ></textarea>
                </div>

                <div class="space-y-4">
                  <div class="rounded-3xl border border-slate-200 p-4">
                    <p class="text-sm font-semibold text-slate-900">Authorization policy</p>
                    <p class="mt-1 text-sm text-slate-500">Support roles, markings, attribute gates, and guest-session restrictions.</p>
                    <textarea
                      rows="10"
                      class="mt-4 w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-emerald-500"
                      bind:value={draftAuthorizationPolicyText}
                      spellcheck="false"
                    ></textarea>
                  </div>

                  <div class="rounded-3xl border border-slate-200 p-4">
                    <p class="text-sm font-semibold text-slate-900">Use actions in the platform</p>
                    <div class="mt-4 grid gap-3">
                      <a href="/object-views" class="rounded-2xl border border-slate-200 px-4 py-3 hover:border-emerald-300">
                        <p class="text-sm font-semibold text-slate-900">Object Views</p>
                        <p class="mt-1 text-sm text-slate-500">Expose first-class action callouts inside full and panel object views.</p>
                      </a>
                      <a href="/object-monitors" class="rounded-2xl border border-slate-200 px-4 py-3 hover:border-emerald-300">
                        <p class="text-sm font-semibold text-slate-900">Object Monitors</p>
                        <p class="mt-1 text-sm text-slate-500">Trigger remediation actions from watched searches and thresholds.</p>
                      </a>
                      <a href="/apps" class="rounded-2xl border border-slate-200 px-4 py-3 hover:border-emerald-300">
                        <p class="text-sm font-semibold text-slate-900">Workshop</p>
                        <p class="mt-1 text-sm text-slate-500">Bind actions into published operational apps and review panels.</p>
                      </a>
                    </div>
                  </div>
                </div>
              </div>
            </section>

            <section class="space-y-4">
              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <p class="text-sm font-semibold text-slate-900">Operation config</p>
                <p class="mt-1 text-sm text-slate-500">Raw backend config for object patches, link creation, function runtime, or webhook calls.</p>
                <textarea
                  rows="16"
                  class="mt-4 w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-emerald-500"
                  bind:value={draftOperationConfigText}
                  spellcheck="false"
                ></textarea>
              </div>

              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <p class="text-sm font-semibold text-slate-900">Side effects</p>
                <p class="mt-1 text-sm text-slate-500">Notification and webhook fanout defined as notification_side_effects in the config envelope.</p>
                <textarea
                  rows="14"
                  class="mt-4 w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-emerald-500"
                  bind:value={draftNotificationEffectsText}
                  spellcheck="false"
                ></textarea>
              </div>

              {#if draftOperationKind === 'invoke_function'}
                <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                  <div class="flex items-center justify-between gap-3">
                    <div>
                      <p class="text-sm font-semibold text-slate-900">Function-backed actions</p>
                      <p class="mt-1 text-sm text-slate-500">Swap the inline config for a reusable package reference with one click.</p>
                    </div>
                  </div>
                  <div class="mt-4 space-y-3">
                    {#each functionPackages as functionPackage}
                      <div class="rounded-2xl border border-slate-200 p-4">
                        <div class="flex items-start justify-between gap-3">
                          <div>
                            <p class="text-sm font-semibold text-slate-900">{functionPackage.display_name}</p>
                            <p class="mt-1 text-xs text-slate-500">{functionPackage.name} v{functionPackage.version}</p>
                          </div>
                          <button
                            class="rounded-full border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700"
                            onclick={() => draftOperationConfigText = prettyJson({ function_package_id: functionPackage.id })}
                          >
                            Use package
                          </button>
                        </div>
                        <p class="mt-3 text-sm text-slate-500">
                          Runtime: {functionPackage.runtime} · Entrypoint: {functionPackage.entrypoint}
                        </p>
                      </div>
                    {/each}
                  </div>
                </div>
              {/if}

              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <div class="flex flex-wrap gap-3">
                  <button
                    class="rounded-full bg-emerald-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-emerald-500 disabled:cursor-not-allowed disabled:bg-emerald-300"
                    onclick={() => void saveAction()}
                    disabled={savingAction}
                  >
                    {savingAction ? 'Saving...' : isCreateMode ? 'Create action type' : 'Save changes'}
                  </button>
                  {#if !isCreateMode}
                    <button
                      class="rounded-full border border-rose-200 bg-rose-50 px-5 py-2.5 text-sm font-medium text-rose-700 hover:border-rose-300 disabled:cursor-not-allowed disabled:opacity-60"
                      onclick={() => void removeAction()}
                      disabled={deletingAction}
                    >
                      {deletingAction ? 'Deleting...' : 'Delete action'}
                    </button>
                  {/if}
                </div>
              </div>
            </section>
          </div>
        {:else if activeTab === 'operate'}
          <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_420px]">
            <section class="space-y-4 rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              {#if !selectedAction && isCreateMode}
                <div class="rounded-3xl border border-dashed border-slate-200 px-4 py-10 text-center text-sm text-slate-500">
                  Create or select an action type to validate and execute it.
                </div>
              {:else}
                {#if operationRequiresTarget(draftOperationKind)}
                  <label class="block space-y-2 text-sm text-slate-700">
                    <span class="font-medium">Target object</span>
                    <select
                      class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                      bind:value={selectedTargetObjectId}
                    >
                      {#each objects as objectItem}
                        <option value={objectItem.id}>{objectLabel(objectItem)}</option>
                      {/each}
                    </select>
                  </label>
                {:else}
                  <div class="rounded-2xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">
                    This action can be executed without binding to a specific target object.
                  </div>
                {/if}

                <ActionExecutor
                  action={{
                    id: selectedAction?.id ?? 'draft',
                    name: draftName || 'draft_action',
                    display_name: draftDisplayName || draftName || 'Draft action',
                    description: draftDescription,
                    object_type_id: selectedTypeId,
                    operation_kind: draftOperationKind,
                    input_schema: draftInputSchema,
                    form_schema: canParseJson(draftFormSchemaText) ? parseJson<ActionFormSchema>(draftFormSchemaText, 'Form schema JSON') : { sections: [], parameter_overrides: [] },
                    config: {},
                    confirmation_required: draftConfirmationRequired,
                    permission_key: draftPermissionKey || null,
                    authorization_policy: canParseJson(draftAuthorizationPolicyText)
                      ? parseJson<ActionAuthorizationPolicy>(draftAuthorizationPolicyText, 'Authorization policy JSON')
                      : {},
                    owner_id: selectedAction?.owner_id ?? 'draft',
                    created_at: selectedAction?.created_at ?? new Date().toISOString(),
                    updated_at: selectedAction?.updated_at ?? new Date().toISOString()
                  }}
                  targetObject={selectedTargetObject}
                  bind:parameters
                  title="Action parameters"
                  emptyMessage="This action does not require structured parameters."
                  on:change={(event) => parameters = event.detail.parameters}
                />

                <label class="block space-y-2 text-sm text-slate-700">
                  <span class="font-medium">Justification</span>
                  <textarea
                    rows="3"
                    class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                    bind:value={justification}
                    placeholder="Why is this action being run now?"
                  ></textarea>
                </label>

                <div class="flex flex-wrap gap-3">
                  <button
                    class="rounded-full border border-slate-300 bg-white px-4 py-2.5 text-sm font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700 disabled:cursor-not-allowed disabled:opacity-60"
                    onclick={() => void runValidation()}
                    disabled={!selectedActionId || validatingActionRun}
                  >
                    {validatingActionRun ? 'Validating...' : 'Validate'}
                  </button>
                  <button
                    class="rounded-full bg-emerald-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-emerald-500 disabled:cursor-not-allowed disabled:bg-emerald-300"
                    onclick={() => void runAction()}
                    disabled={!selectedActionId || executingActionRun}
                  >
                    {executingActionRun ? 'Executing...' : 'Execute'}
                  </button>
                  <button
                    class="rounded-full border border-slate-300 bg-white px-4 py-2.5 text-sm font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700 disabled:cursor-not-allowed disabled:opacity-60"
                    onclick={() => void createWhatIfBranch()}
                    disabled={!selectedActionId || creatingWhatIf}
                  >
                    {creatingWhatIf ? 'Creating what-if...' : 'Create what-if'}
                  </button>
                </div>

                <div class="rounded-3xl border border-slate-200 bg-slate-50 p-4">
                  <div class="grid gap-4 lg:grid-cols-2">
                    <label class="space-y-2 text-sm text-slate-700">
                      <span class="font-medium">What-if branch name</span>
                      <input
                        class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                        type="text"
                        bind:value={whatIfName}
                        placeholder="Customer escalation dry run"
                      />
                    </label>
                    <label class="space-y-2 text-sm text-slate-700">
                      <span class="font-medium">Description</span>
                      <input
                        class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                        type="text"
                        bind:value={whatIfDescription}
                        placeholder="Capture the expected patch before scheduling the real run"
                      />
                    </label>
                  </div>
                </div>
              {/if}
            </section>

            <section class="space-y-4">
              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <p class="text-sm font-semibold text-slate-900">Batch execution</p>
                <p class="mt-1 text-sm text-slate-500">Pick several target objects and run the same validated parameters across the selected set.</p>
                <div class="mt-4 space-y-2">
                  {#each objects.slice(0, 12) as objectItem}
                    <label class="flex items-center gap-3 rounded-2xl border border-slate-200 px-4 py-3 text-sm text-slate-700">
                      <input
                        type="checkbox"
                        checked={selectedBatchObjectIds.includes(objectItem.id)}
                        onchange={(event) => {
                          const checked = (event.currentTarget as HTMLInputElement).checked;
                          selectedBatchObjectIds = checked
                            ? [...selectedBatchObjectIds, objectItem.id]
                            : selectedBatchObjectIds.filter((id) => id !== objectItem.id);
                        }}
                      />
                      <span>{objectLabel(objectItem)}</span>
                    </label>
                  {/each}
                </div>
                <button
                  class="mt-4 rounded-full border border-slate-300 bg-white px-4 py-2.5 text-sm font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700 disabled:cursor-not-allowed disabled:opacity-60"
                  onclick={() => void runBatch()}
                  disabled={!selectedActionId || selectedBatchObjectIds.length === 0 || batchingActionRun}
                >
                  {batchingActionRun ? 'Executing batch...' : `Execute batch (${selectedBatchObjectIds.length})`}
                </button>
              </div>

              {#if runError}
                <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{runError}</div>
              {/if}
              {#if branchError}
                <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{branchError}</div>
              {/if}

              {#if validation}
                <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                  <p class="text-sm font-semibold text-slate-900">Validation preview</p>
                  <p class={`mt-2 inline-flex rounded-full px-3 py-1 text-xs font-medium uppercase tracking-[0.16em] ${validation.valid ? 'bg-emerald-100 text-emerald-700' : 'bg-amber-100 text-amber-700'}`}>
                    {validation.valid ? 'valid' : 'needs review'}
                  </p>
                  <pre class="mt-4 overflow-x-auto rounded-2xl bg-slate-950 p-4 text-xs text-slate-100">{prettyJson(validation.preview)}</pre>
                  {#if validation.errors.length}
                    <div class="mt-4 space-y-2">
                      {#each validation.errors as error}
                        <div class="rounded-2xl border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-700">{error}</div>
                      {/each}
                    </div>
                  {/if}
                </div>
              {/if}

              {#if execution}
                <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                  <p class="text-sm font-semibold text-slate-900">Execution result</p>
                  <pre class="mt-4 overflow-x-auto rounded-2xl bg-slate-950 p-4 text-xs text-slate-100">{prettyJson(execution)}</pre>
                </div>
              {/if}

              {#if batchExecution}
                <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                  <p class="text-sm font-semibold text-slate-900">Batch result</p>
                  <div class="mt-4 grid gap-3 sm:grid-cols-3">
                    <div class="rounded-2xl border border-slate-200 p-4">
                      <p class="text-xs uppercase tracking-[0.18em] text-slate-400">Total</p>
                      <p class="mt-2 text-2xl font-semibold text-slate-950">{batchExecution.total}</p>
                    </div>
                    <div class="rounded-2xl border border-slate-200 p-4">
                      <p class="text-xs uppercase tracking-[0.18em] text-slate-400">Succeeded</p>
                      <p class="mt-2 text-2xl font-semibold text-emerald-700">{batchExecution.succeeded}</p>
                    </div>
                    <div class="rounded-2xl border border-slate-200 p-4">
                      <p class="text-xs uppercase tracking-[0.18em] text-slate-400">Failed</p>
                      <p class="mt-2 text-2xl font-semibold text-rose-700">{batchExecution.failed}</p>
                    </div>
                  </div>
                  <pre class="mt-4 overflow-x-auto rounded-2xl bg-slate-950 p-4 text-xs text-slate-100">{prettyJson(batchExecution.results)}</pre>
                </div>
              {/if}
            </section>
          </div>
        {:else if activeTab === 'inline-edits'}
          <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
            <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              <div class="flex items-start justify-between gap-4">
                <div>
                  <p class="text-sm font-semibold text-slate-900">Inline edit bindings</p>
                  <p class="mt-1 text-sm text-slate-500">Attach update-object actions to property inline edit controls and choose which parameter should receive the edited value.</p>
                </div>
                <span class={`rounded-full px-3 py-1 text-xs font-medium uppercase tracking-[0.16em] ${draftOperationKind === 'update_object' ? 'bg-emerald-100 text-emerald-700' : 'bg-amber-100 text-amber-700'}`}>
                  {draftOperationKind === 'update_object' ? 'eligible' : 'update_object required'}
                </span>
              </div>

              {#if inlineEditError}
                <div class="mt-4 rounded-2xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{inlineEditError}</div>
              {/if}
              {#if inlineEditSuccess}
                <div class="mt-4 rounded-2xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">{inlineEditSuccess}</div>
              {/if}

              <div class="mt-4 space-y-3">
                {#each properties as property}
                  <div class="rounded-2xl border border-slate-200 p-4">
                    <div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_220px_auto_auto] lg:items-end">
                      <div>
                        <p class="text-sm font-semibold text-slate-900">{property.display_name}</p>
                        <p class="mt-1 text-xs text-slate-500">{property.name} · {property.property_type}</p>
                        <p class="mt-2 text-sm text-slate-500">
                          Current binding:
                          {#if property.inline_edit_config}
                            <span class="font-medium text-slate-700">{property.inline_edit_config.action_type_id}</span>
                          {:else}
                            none
                          {/if}
                        </p>
                      </div>
                      <label class="space-y-2 text-sm text-slate-700">
                        <span class="font-medium">Input parameter</span>
                        <select
                          class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-emerald-500"
                          value={currentInlineInputName(property)}
                          onchange={(event) =>
                            inlineInputNames = {
                              ...inlineInputNames,
                              [property.id]: (event.currentTarget as HTMLSelectElement).value
                            }}
                        >
                          {#each draftInputSchema as field}
                            <option value={field.name}>{field.display_name || field.name}</option>
                          {/each}
                        </select>
                      </label>
                      <button
                        class="rounded-full border border-slate-300 bg-white px-4 py-2.5 text-sm font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700 disabled:cursor-not-allowed disabled:opacity-60"
                        onclick={() => void bindInlineEdit(property)}
                        disabled={!selectedActionId || draftOperationKind !== 'update_object' || bindingInlinePropertyId === property.id || draftInputSchema.length === 0}
                      >
                        {bindingInlinePropertyId === property.id ? 'Saving...' : 'Bind selected action'}
                      </button>
                      <button
                        class="rounded-full border border-rose-200 bg-rose-50 px-4 py-2.5 text-sm font-medium text-rose-700 hover:border-rose-300 disabled:cursor-not-allowed disabled:opacity-60"
                        onclick={() => void clearInlineEdit(property)}
                        disabled={!property.inline_edit_config || bindingInlinePropertyId === property.id}
                      >
                        Clear
                      </button>
                    </div>
                  </div>
                {/each}
              </div>
            </section>

            <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              <p class="text-sm font-semibold text-slate-900">Inline edit guidance</p>
              <div class="mt-4 space-y-3 text-sm leading-6 text-slate-600">
                <p>Only `update_object` actions can power inline edits because the runtime needs a deterministic property mapping.</p>
                <p>The selected input parameter becomes the edited value that the property sends into the configured action.</p>
                <p>These bindings are stored on the property definition itself, so they immediately affect object views and other inline edit surfaces.</p>
              </div>
            </section>
          </div>
        {:else}
          <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
            <section class="space-y-4 rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
              <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                <div class="rounded-2xl border border-slate-200 p-4">
                  <p class="text-xs uppercase tracking-[0.18em] text-slate-400">Executions</p>
                  <p class="mt-2 text-2xl font-semibold text-slate-950">{actionMetrics.executions}</p>
                </div>
                <div class="rounded-2xl border border-slate-200 p-4">
                  <p class="text-xs uppercase tracking-[0.18em] text-slate-400">Failures</p>
                  <p class="mt-2 text-2xl font-semibold text-rose-700">{actionMetrics.failed}</p>
                </div>
                <div class="rounded-2xl border border-slate-200 p-4">
                  <p class="text-xs uppercase tracking-[0.18em] text-slate-400">Denied</p>
                  <p class="mt-2 text-2xl font-semibold text-amber-700">{actionMetrics.denied}</p>
                </div>
                <div class="rounded-2xl border border-slate-200 p-4">
                  <p class="text-xs uppercase tracking-[0.18em] text-slate-400">What-if branches</p>
                  <p class="mt-2 text-2xl font-semibold text-slate-950">{whatIfBranches.length}</p>
                </div>
              </div>

              <div class="rounded-3xl border border-slate-200 p-4">
                <div class="flex items-center justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">Action log</p>
                    <p class="mt-1 text-sm text-slate-500">Real audit events emitted by the ontology runtime and filtered to this action id.</p>
                  </div>
                  <button
                    class="rounded-full border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:border-emerald-400 hover:text-emerald-700 disabled:opacity-60"
                    onclick={() => void refreshAuditEvents()}
                    disabled={auditLoading}
                  >
                    {auditLoading ? 'Refreshing...' : 'Refresh log'}
                  </button>
                </div>

                <div class="mt-4 space-y-3">
                  {#if actionEvents.length === 0}
                    <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-500">
                      No audit events matched this action yet.
                    </div>
                  {:else}
                    {#each actionEvents.slice(0, 14) as event}
                      <div class="rounded-2xl border border-slate-200 p-4">
                        <div class="flex flex-wrap items-center gap-2 text-xs text-slate-500">
                          <span class={`rounded-full px-2 py-1 font-medium uppercase tracking-[0.16em] ${
                            event.status === 'success'
                              ? 'bg-emerald-100 text-emerald-700'
                              : event.status === 'denied'
                                ? 'bg-amber-100 text-amber-700'
                                : 'bg-rose-100 text-rose-700'
                          }`}>
                            {event.status}
                          </span>
                          <span>{event.actor}</span>
                          <span>·</span>
                          <span>{formatDate(event.occurred_at)}</span>
                        </div>
                        <p class="mt-3 text-sm font-semibold text-slate-900">{event.action}</p>
                        <p class="mt-1 text-sm text-slate-500">
                          Resource: {event.resource_type} / {event.resource_id}
                        </p>
                        <pre class="mt-3 overflow-x-auto rounded-2xl bg-slate-950 p-4 text-xs text-slate-100">{prettyJson(event.metadata)}</pre>
                      </div>
                    {/each}
                  {/if}
                </div>
              </div>
            </section>

            <section class="space-y-4">
              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <div class="flex items-center justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">What-if branches</p>
                    <p class="mt-1 text-sm text-slate-500">Saved previews of action outcomes before the real run.</p>
                  </div>
                  {#if branchesLoading}
                    <span class="text-xs text-slate-500">Refreshing...</span>
                  {/if}
                </div>

                <div class="mt-4 space-y-3">
                  {#if whatIfBranches.length === 0}
                    <div class="rounded-2xl border border-dashed border-slate-200 px-4 py-6 text-center text-sm text-slate-500">
                      No what-if branches saved yet.
                    </div>
                  {:else}
                    {#each whatIfBranches as branch}
                      <div class="rounded-2xl border border-slate-200 p-4">
                        <div class="flex items-start justify-between gap-3">
                          <div>
                            <p class="text-sm font-semibold text-slate-900">{branch.name}</p>
                            <p class="mt-1 text-xs text-slate-500">{formatDate(branch.created_at)}</p>
                          </div>
                          <button
                            class="rounded-full border border-rose-200 bg-rose-50 px-3 py-1.5 text-xs font-medium text-rose-700 hover:border-rose-300"
                            onclick={() => void removeWhatIfBranch(branch.id)}
                          >
                            Delete
                          </button>
                        </div>
                        {#if branch.description}
                          <p class="mt-3 text-sm text-slate-500">{branch.description}</p>
                        {/if}
                        <pre class="mt-3 overflow-x-auto rounded-2xl bg-slate-950 p-4 text-xs text-slate-100">{prettyJson(branch.preview)}</pre>
                      </div>
                    {/each}
                  {/if}
                </div>
              </div>

              <div class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
                <p class="text-sm font-semibold text-slate-900">Monitoring notes</p>
                <div class="mt-4 space-y-3 text-sm leading-6 text-slate-600">
                  <p>Action metrics here come from the same audit events emitted by the backend execution path, including denied and failed runs.</p>
                  <p>Side effects are still part of the stored action config envelope, so notification changes are tracked together with the action definition.</p>
                  <p>Batch runs, inline edits, and what-if previews all reuse the same underlying action runtime instead of a separate mock implementation.</p>
                </div>
              </div>
            </section>
          </div>
        {/if}
      </main>
    </div>
  {/if}
</div>
