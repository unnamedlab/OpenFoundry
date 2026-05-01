<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import {
    createActionWhatIfBranch,
    executeAction,
    executeActionBatch,
    validateAction,
  } from '$lib/api/ontology';
  import type {
    ActionFormCondition,
    ActionFormSchema,
    ActionInputField,
    ActionType,
    ActionWhatIfBranch,
    ExecuteActionResponse,
    ExecuteBatchActionResponse,
    ObjectInstance,
    ValidateActionResponse,
  } from '$lib/api/ontology';
  import { notifications } from '$lib/stores/notifications';

  export let action: ActionType | null = null;
  export let targetObject: ObjectInstance | null = null;
  export let parameters: Record<string, unknown> = {};
  export let title = 'Structured action form';
  export let emptyMessage = 'This action does not require user-entered parameters.';
  // Optional batch mode: when populated, executeActionBatch is used.
  export let batchTargetObjectIds: string[] = [];

  type EffectiveField = ActionInputField & { hidden?: boolean };
  type ResolvedSection = {
    id: string;
    title: string | null;
    description: string | null;
    columns: number;
    collapsible: boolean;
    fields: EffectiveField[];
  };

  const dispatch = createEventDispatcher<{
    change: { parameters: Record<string, unknown> };
    executed: { response: ExecuteActionResponse | ExecuteBatchActionResponse };
    whatif: { branch: ActionWhatIfBranch };
    validated: { response: ValidateActionResponse };
  }>();

  // Submission / validation / what-if state
  let submitting = false;
  let validating = false;
  let creatingWhatIf = false;
  let validationResult: ValidateActionResponse | null = null;
  let lastExecution: ExecuteActionResponse | ExecuteBatchActionResponse | null = null;
  let lastBranch: ActionWhatIfBranch | null = null;
  let submitError = '';
  let confirmOpen = false;
  let justification = '';

  let resolvedSections: ResolvedSection[] = [];
  let fieldDrafts: Record<string, string> = {};
  let fieldErrors: Record<string, string> = {};
  let lastAutoPatched = '';

  function stableStringify(value: unknown): string {
    return JSON.stringify(value ?? {});
  }

  function cloneParameters(value: Record<string, unknown> | null | undefined): Record<string, unknown> {
    return { ...(value ?? {}) };
  }

  function lookupPathValue(source: unknown, path: string): unknown {
    return path
      .split('.')
      .reduce<unknown>((current, segment) => {
        if (!current || typeof current !== 'object' || Array.isArray(current)) return undefined;
        return (current as Record<string, unknown>)[segment];
      }, source);
  }

  function valueExists(value: unknown): boolean {
    return value !== undefined && value !== null;
  }

  function toNumber(value: unknown): number | null {
    if (typeof value === 'number' && Number.isFinite(value)) return value;
    if (typeof value === 'string' && value.trim()) {
      const parsed = Number(value);
      return Number.isFinite(parsed) ? parsed : null;
    }
    return null;
  }

  function matchesCondition(condition: ActionFormCondition, context: Record<string, unknown>): boolean {
    const left = lookupPathValue(context, condition.left);
    switch (condition.operator) {
      case 'exists':
        return valueExists(left);
      case 'not_exists':
        return !valueExists(left);
      case 'is':
        return stableStringify(left) === stableStringify(condition.right);
      case 'is_not':
        return stableStringify(left) !== stableStringify(condition.right);
      case 'includes':
        if (Array.isArray(left)) {
          return left.some((item) => stableStringify(item) === stableStringify(condition.right));
        }
        if (typeof left === 'string' && typeof condition.right === 'string') {
          return left.includes(condition.right);
        }
        return false;
      case 'greater_than': {
        const leftNumber = toNumber(left);
        const rightNumber = toNumber(condition.right);
        return leftNumber !== null && rightNumber !== null && leftNumber > rightNumber;
      }
      case 'greater_than_or_equals': {
        const leftNumber = toNumber(left);
        const rightNumber = toNumber(condition.right);
        return leftNumber !== null && rightNumber !== null && leftNumber >= rightNumber;
      }
      case 'less_than': {
        const leftNumber = toNumber(left);
        const rightNumber = toNumber(condition.right);
        return leftNumber !== null && rightNumber !== null && leftNumber < rightNumber;
      }
      case 'less_than_or_equals': {
        const leftNumber = toNumber(left);
        const rightNumber = toNumber(condition.right);
        return leftNumber !== null && rightNumber !== null && leftNumber <= rightNumber;
      }
      default:
        return false;
    }
  }

  function matchesConditions(
    conditions: ActionFormCondition[] | undefined,
    context: Record<string, unknown>,
  ): boolean {
    return (conditions ?? []).every((condition) => matchesCondition(condition, context));
  }

  function actionContext(
    currentParameters: Record<string, unknown>,
    currentTarget: ObjectInstance | null,
  ): Record<string, unknown> {
    return {
      parameters: currentParameters,
      target: currentTarget
        ? {
            id: currentTarget.id,
            created_by: currentTarget.created_by,
            marking: currentTarget.marking,
            properties: currentTarget.properties,
          }
        : null,
    };
  }

  function normalizeFormSchema(schema: ActionFormSchema | null | undefined): ActionFormSchema {
    return {
      sections: schema?.sections ?? [],
      parameter_overrides: schema?.parameter_overrides ?? [],
    };
  }

  function resolveEffectiveFields(
    inputSchema: ActionInputField[],
    schema: ActionFormSchema,
    currentParameters: Record<string, unknown>,
    currentTarget: ObjectInstance | null,
  ): EffectiveField[] {
    const context = actionContext(currentParameters, currentTarget);
    return inputSchema.map((field) => {
      const next: EffectiveField = { ...field };
      const override = (schema.parameter_overrides ?? []).find(
        (candidate) =>
          candidate.parameter_name === field.name && matchesConditions(candidate.conditions, context),
      );
      if (override) {
        next.hidden = override.hidden;
        if (override.required !== undefined) next.required = override.required;
        if (override.default_value !== undefined) next.default_value = override.default_value;
        if (override.display_name !== undefined) next.display_name = override.display_name;
        if (override.description !== undefined) next.description = override.description;
      }
      return next;
    });
  }

  function resolveSections(
    actionType: ActionType | null,
    currentParameters: Record<string, unknown>,
    currentTarget: ObjectInstance | null,
  ): ResolvedSection[] {
    if (!actionType) return [];
    const schema = normalizeFormSchema(actionType.form_schema);
    const context = actionContext(currentParameters, currentTarget);
    const effectiveFields = resolveEffectiveFields(
      actionType.input_schema,
      schema,
      currentParameters,
      currentTarget,
    ).filter((field) => !field.hidden);
    const fieldsByName = new Map(effectiveFields.map((field) => [field.name, field]));

    if (!schema.sections?.length) {
      return effectiveFields.length
        ? [
            {
              id: 'default',
              title,
              description: null,
              columns: effectiveFields.length > 1 ? 2 : 1,
              collapsible: false,
              fields: effectiveFields,
            },
          ]
        : [];
    }

    const sections: ResolvedSection[] = [];
    const assigned = new Set<string>();

    for (const section of schema.sections ?? []) {
      let titleValue = section.title ?? null;
      let descriptionValue = section.description ?? null;
      let columns = section.columns ?? 1;
      let hidden = section.visible === false;

      const override = (section.overrides ?? []).find((candidate) =>
        matchesConditions(candidate.conditions, context),
      );
      if (override) {
        if (override.hidden !== undefined) hidden = override.hidden;
        if (override.columns !== undefined) columns = override.columns;
        if (override.title !== undefined) titleValue = override.title;
        if (override.description !== undefined) descriptionValue = override.description;
      }

      if (hidden) continue;

      const sectionFields = (section.parameter_names ?? [])
        .map((parameterName) => fieldsByName.get(parameterName) ?? null)
        .filter((field): field is EffectiveField => Boolean(field));
      sectionFields.forEach((field) => assigned.add(field.name));

      if (!sectionFields.length) continue;

      sections.push({
        id: section.id,
        title: titleValue,
        description: descriptionValue,
        columns: columns === 2 ? 2 : 1,
        collapsible: section.collapsible ?? false,
        fields: sectionFields,
      });
    }

    const remaining = effectiveFields.filter((field) => !assigned.has(field.name));
    if (remaining.length) {
      sections.push({
        id: 'additional',
        title: sections.length ? 'Additional parameters' : title,
        description: sections.length
          ? 'Parameters that are not explicitly assigned to a form section.'
          : null,
        columns: remaining.length > 1 ? 2 : 1,
        collapsible: false,
        fields: remaining,
      });
    }

    return sections;
  }

  function applyMissingDefaults(
    actionType: ActionType | null,
    currentParameters: Record<string, unknown>,
    currentTarget: ObjectInstance | null,
  ): Record<string, unknown> {
    if (!actionType) return currentParameters;
    const effectiveFields = resolveEffectiveFields(
      actionType.input_schema,
      normalizeFormSchema(actionType.form_schema),
      currentParameters,
      currentTarget,
    );
    const next = cloneParameters(currentParameters);
    let changed = false;
    for (const field of effectiveFields) {
      if (next[field.name] === undefined && field.default_value !== undefined) {
        next[field.name] = field.default_value;
        changed = true;
      }
    }
    return changed ? next : currentParameters;
  }

  function updateParameters(next: Record<string, unknown>) {
    dispatch('change', { parameters: next });
  }

  function updateField(name: string, value: unknown) {
    const next = cloneParameters(parameters);
    if (value === '' || value === undefined) {
      delete next[name];
    } else {
      next[name] = value;
    }
    updateParameters(next);
  }

  function fieldValue(field: EffectiveField): unknown {
    return parameters[field.name] ?? field.default_value ?? (field.property_type === 'boolean' ? false : '');
  }

  function fieldLabel(field: EffectiveField): string {
    return field.display_name?.trim() || field.name;
  }

  function setJsonDraft(fieldName: string, value: string) {
    fieldDrafts = { ...fieldDrafts, [fieldName]: value };
  }

  function renderJsonDraft(field: EffectiveField): string {
    if (fieldDrafts[field.name] !== undefined) {
      return fieldDrafts[field.name];
    }
    const value = parameters[field.name] ?? field.default_value;
    return value === undefined ? '' : JSON.stringify(value, null, 2);
  }

  function renderMediaDraft(field: EffectiveField): string {
    if (fieldDrafts[field.name] !== undefined) {
      return fieldDrafts[field.name];
    }
    const value = parameters[field.name] ?? field.default_value;
    if (value === undefined) return '';
    return typeof value === 'string' ? value : JSON.stringify(value, null, 2);
  }

  function handleJsonFieldInput(field: EffectiveField, raw: string) {
    setJsonDraft(field.name, raw);
    if (!raw.trim()) {
      fieldErrors = { ...fieldErrors, [field.name]: '' };
      updateField(field.name, undefined);
      return;
    }

    try {
      const parsed = JSON.parse(raw);
      if (field.property_type === 'vector') {
        if (
          !Array.isArray(parsed) ||
          parsed.length === 0 ||
          !parsed.every((entry) => typeof entry === 'number' && Number.isFinite(entry))
        ) {
          throw new Error('Vector fields require a non-empty JSON array of numeric values');
        }
      }
      const nextErrors = { ...fieldErrors };
      delete nextErrors[field.name];
      fieldErrors = nextErrors;
      updateField(field.name, parsed);
    } catch (error) {
      fieldErrors = {
        ...fieldErrors,
        [field.name]: error instanceof Error ? error.message : 'Invalid JSON value',
      };
    }
  }

  function handleMediaFieldInput(field: EffectiveField, raw: string) {
    setJsonDraft(field.name, raw);
    if (!raw.trim()) {
      fieldErrors = { ...fieldErrors, [field.name]: '' };
      updateField(field.name, undefined);
      return;
    }

    const trimmed = raw.trim();
    if (trimmed.startsWith('{')) {
      try {
        const parsed = JSON.parse(trimmed);
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
          throw new Error('Media reference JSON must be an object');
        }
        const record = parsed as Record<string, unknown>;
        const uri = record.uri ?? record.url;
        if (typeof uri !== 'string' || !uri.trim()) {
          throw new Error('Media reference JSON requires a non-empty uri or url');
        }
        const nextErrors = { ...fieldErrors };
        delete nextErrors[field.name];
        fieldErrors = nextErrors;
        updateField(field.name, parsed);
      } catch (error) {
        fieldErrors = {
          ...fieldErrors,
          [field.name]: error instanceof Error ? error.message : 'Invalid media reference JSON',
        };
      }
      return;
    }

    const nextErrors = { ...fieldErrors };
    delete nextErrors[field.name];
    fieldErrors = nextErrors;
    updateField(field.name, raw);
  }

  $: resolvedSections = resolveSections(action, parameters, targetObject);

  $: {
    const nextParameters = applyMissingDefaults(action, parameters, targetObject);
    const currentSerialized = stableStringify(parameters);
    const nextSerialized = stableStringify(nextParameters);
    if (nextSerialized !== currentSerialized && nextSerialized !== lastAutoPatched) {
      lastAutoPatched = nextSerialized;
      updateParameters(nextParameters);
    } else if (nextSerialized === currentSerialized) {
      lastAutoPatched = '';
    }
  }

  // ----- Submit / Validate / What-if --------------------------------------

  function hasFieldErrors(): boolean {
    return Object.values(fieldErrors).some((message) => Boolean(message));
  }

  function buildExecutePayload() {
    return {
      target_object_id: targetObject?.id,
      parameters,
      justification: justification.trim() || undefined,
    };
  }

  async function runValidate() {
    if (!action) return;
    validating = true;
    submitError = '';
    try {
      const response = await validateAction(action.id, {
        target_object_id: targetObject?.id,
        parameters,
      });
      validationResult = response;
      dispatch('validated', { response });
      if (!response.valid) {
        notifications.warning(`Validation failed: ${response.errors.join('; ') || 'see preview'}`);
      } else {
        notifications.success('Validation OK');
      }
    } catch (error) {
      submitError = error instanceof Error ? error.message : String(error);
      notifications.error(`Validate failed: ${submitError}`);
    } finally {
      validating = false;
    }
  }

  async function runWhatIf() {
    if (!action) return;
    creatingWhatIf = true;
    submitError = '';
    try {
      const branch = await createActionWhatIfBranch(action.id, {
        target_object_id: targetObject?.id,
        parameters,
        name: `What-if @ ${new Date().toISOString()}`,
      });
      lastBranch = branch;
      dispatch('whatif', { branch });
      notifications.info('Sandbox branch created');
    } catch (error) {
      submitError = error instanceof Error ? error.message : String(error);
      notifications.error(`What-if failed: ${submitError}`);
    } finally {
      creatingWhatIf = false;
    }
  }

  function requestExecute() {
    if (!action) return;
    if (hasFieldErrors()) {
      notifications.error('Fix invalid fields before submitting');
      return;
    }
    if (action.confirmation_required) {
      // Mirror kernel L832 ensure_confirmation_justification: justification mandatory.
      confirmOpen = true;
      return;
    }
    void doExecute();
  }

  async function doExecute() {
    if (!action) return;
    if (action.confirmation_required && !justification.trim()) {
      submitError = 'justification is required for confirmation_required actions';
      return;
    }
    submitting = true;
    submitError = '';
    try {
      let response: ExecuteActionResponse | ExecuteBatchActionResponse;
      if (batchTargetObjectIds.length > 0) {
        response = await executeActionBatch(action.id, {
          target_object_ids: batchTargetObjectIds,
          parameters,
          justification: justification.trim() || undefined,
        });
        notifications.success(
          `Batch executed: ${(response as ExecuteBatchActionResponse).succeeded}/${(response as ExecuteBatchActionResponse).total}`,
        );
      } else {
        response = await executeAction(action.id, buildExecutePayload());
        notifications.success(`Action "${action.display_name || action.name}" executed`);
      }
      lastExecution = response;
      confirmOpen = false;
      justification = '';
      dispatch('executed', { response });
    } catch (error) {
      submitError = error instanceof Error ? error.message : String(error);
      notifications.error(`Execute failed: ${submitError}`);
    } finally {
      submitting = false;
    }
  }

  function diffEntries(
    before: Record<string, unknown> | null | undefined,
    after: Record<string, unknown> | null | undefined,
  ): Array<{ key: string; before: unknown; after: unknown; changed: boolean }> {
    const beforeProps = (before as { properties?: Record<string, unknown> } | null)?.properties ?? before ?? {};
    const afterProps = (after as { properties?: Record<string, unknown> } | null)?.properties ?? after ?? {};
    const keys = new Set<string>([
      ...Object.keys(beforeProps as Record<string, unknown>),
      ...Object.keys(afterProps as Record<string, unknown>),
    ]);
    return [...keys].sort().map((key) => {
      const beforeValue = (beforeProps as Record<string, unknown>)[key];
      const afterValue = (afterProps as Record<string, unknown>)[key];
      return {
        key,
        before: beforeValue,
        after: afterValue,
        changed: stableStringify(beforeValue) !== stableStringify(afterValue),
      };
    });
  }

  function formatValue(value: unknown): string {
    if (value === undefined) return '∅';
    if (value === null) return 'null';
    if (typeof value === 'string') return value;
    return JSON.stringify(value);
  }
</script>

{#if action}
  <div class="space-y-4">
    {#if resolvedSections.length === 0}
      <div class="rounded-2xl border border-dashed border-slate-300 px-4 py-5 text-sm text-slate-500 dark:border-slate-700 dark:text-slate-400">
        {emptyMessage}
      </div>
    {/if}

    {#each resolvedSections as section (section.id)}
      {#if section.collapsible}
        <details class="rounded-2xl border border-slate-200 p-4 dark:border-slate-800" open>
          <summary class="cursor-pointer text-sm font-semibold text-slate-900 dark:text-slate-100">
            {section.title ?? 'Section'}
          </summary>
          {#if section.description}
            <p class="mt-2 text-sm text-slate-500 dark:text-slate-400">{section.description}</p>
          {/if}
          <div class={`mt-4 grid gap-4 ${section.columns === 2 ? 'md:grid-cols-2' : 'grid-cols-1'}`}>
            {#each section.fields as field (field.name)}
              <div class="space-y-2">
                <label class="block text-sm font-medium text-slate-700 dark:text-slate-200" for={`action-field-${field.name}`}>
                  {fieldLabel(field)}{field.required ? ' *' : ''}
                </label>
                {#if field.property_type === 'boolean'}
                  <label class="flex items-center gap-2 rounded-2xl border border-slate-300 px-4 py-2.5 text-sm dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100">
                    <input
                      id={`action-field-${field.name}`}
                      type="checkbox"
                      checked={Boolean(fieldValue(field))}
                      onchange={(event) => updateField(field.name, (event.currentTarget as HTMLInputElement).checked)}
                    />
                    Enabled
                  </label>
                {:else if field.property_type === 'integer' || field.property_type === 'float'}
                  <input
                    id={`action-field-${field.name}`}
                    type="number"
                    step={field.property_type === 'float' ? 'any' : '1'}
                    value={fieldValue(field) === '' ? '' : String(fieldValue(field) ?? '')}
                    class="w-full rounded-2xl border border-slate-300 px-4 py-2.5 text-sm dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                    oninput={(event) => {
                      const raw = (event.currentTarget as HTMLInputElement).value;
                      if (!raw.trim()) {
                        updateField(field.name, undefined);
                      } else {
                        updateField(field.name, field.property_type === 'integer' ? Number.parseInt(raw, 10) : Number.parseFloat(raw));
                      }
                    }}
                  />
                {:else if field.property_type === 'json' || field.property_type === 'array' || field.property_type === 'vector'}
                  <div class="space-y-2">
                    <textarea
                      id={`action-field-${field.name}`}
                      rows={6}
                      class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 dark:border-slate-700"
                      spellcheck="false"
                      value={renderJsonDraft(field)}
                      oninput={(event) => handleJsonFieldInput(field, (event.currentTarget as HTMLTextAreaElement).value)}
                    ></textarea>
                    {#if fieldErrors[field.name]}
                      <p class="text-xs text-rose-600 dark:text-rose-300">{fieldErrors[field.name]}</p>
                    {/if}
                  </div>
                {:else if field.property_type === 'media_reference'}
                  <div class="space-y-2">
                    <textarea
                      id={`action-field-${field.name}`}
                      rows={3}
                      class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 dark:border-slate-700"
                      spellcheck="false"
                      placeholder={'https://cdn.example.com/file.png or {"uri":"s3://bucket/file.png"}'}
                      value={renderMediaDraft(field)}
                      oninput={(event) => handleMediaFieldInput(field, (event.currentTarget as HTMLTextAreaElement).value)}
                    ></textarea>
                    {#if fieldErrors[field.name]}
                      <p class="text-xs text-rose-600 dark:text-rose-300">{fieldErrors[field.name]}</p>
                    {/if}
                  </div>
                {:else if field.property_type === 'media_reference'}
                  <div class="space-y-2">
                    <textarea
                      id={`action-field-${field.name}`}
                      rows={3}
                      class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 dark:border-slate-700"
                      spellcheck="false"
                      placeholder={'https://cdn.example.com/file.png or {"uri":"s3://bucket/file.png"}'}
                      value={renderMediaDraft(field)}
                      oninput={(event) => handleMediaFieldInput(field, (event.currentTarget as HTMLTextAreaElement).value)}
                    ></textarea>
                    {#if fieldErrors[field.name]}
                      <p class="text-xs text-rose-600 dark:text-rose-300">{fieldErrors[field.name]}</p>
                    {/if}
                  </div>
                {:else}
                  <input
                    id={`action-field-${field.name}`}
                    type={field.property_type === 'date' ? 'date' : 'text'}
                    value={typeof fieldValue(field) === 'string' ? String(fieldValue(field)) : String(fieldValue(field) ?? '')}
                    class="w-full rounded-2xl border border-slate-300 px-4 py-2.5 text-sm dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                    oninput={(event) => updateField(field.name, (event.currentTarget as HTMLInputElement).value)}
                  />
                {/if}
                {#if field.description}
                  <p class="text-xs text-slate-500 dark:text-slate-400">{field.description}</p>
                {:else if field.property_type === 'vector'}
                  <p class="text-xs text-slate-500 dark:text-slate-400">
                    Provide a JSON array of numeric values, for example `[0.12, 0.87, 0.44]`.
                  </p>
                {:else if field.property_type === 'media_reference'}
                  <p class="text-xs text-slate-500 dark:text-slate-400">
                    Accepts a URI or URL string, or a JSON object with `uri` or `url`.
                  </p>
                {/if}
              </div>
            {/each}
          </div>
        </details>
      {:else}
        <section class="rounded-2xl border border-slate-200 p-4 dark:border-slate-800">
          {#if section.title}
            <h3 class="text-sm font-semibold text-slate-900 dark:text-slate-100">{section.title}</h3>
          {/if}
          {#if section.description}
            <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{section.description}</p>
          {/if}
          <div class={`mt-4 grid gap-4 ${section.columns === 2 ? 'md:grid-cols-2' : 'grid-cols-1'}`}>
            {#each section.fields as field (field.name)}
              <div class="space-y-2">
                <label class="block text-sm font-medium text-slate-700 dark:text-slate-200" for={`action-field-${field.name}`}>
                  {fieldLabel(field)}{field.required ? ' *' : ''}
                </label>
                {#if field.property_type === 'boolean'}
                  <label class="flex items-center gap-2 rounded-2xl border border-slate-300 px-4 py-2.5 text-sm dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100">
                    <input
                      id={`action-field-${field.name}`}
                      type="checkbox"
                      checked={Boolean(fieldValue(field))}
                      onchange={(event) => updateField(field.name, (event.currentTarget as HTMLInputElement).checked)}
                    />
                    Enabled
                  </label>
                {:else if field.property_type === 'integer' || field.property_type === 'float'}
                  <input
                    id={`action-field-${field.name}`}
                    type="number"
                    step={field.property_type === 'float' ? 'any' : '1'}
                    value={fieldValue(field) === '' ? '' : String(fieldValue(field) ?? '')}
                    class="w-full rounded-2xl border border-slate-300 px-4 py-2.5 text-sm dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                    oninput={(event) => {
                      const raw = (event.currentTarget as HTMLInputElement).value;
                      if (!raw.trim()) {
                        updateField(field.name, undefined);
                      } else {
                        updateField(field.name, field.property_type === 'integer' ? Number.parseInt(raw, 10) : Number.parseFloat(raw));
                      }
                    }}
                  />
                {:else if field.property_type === 'json' || field.property_type === 'array' || field.property_type === 'vector'}
                  <div class="space-y-2">
                    <textarea
                      id={`action-field-${field.name}`}
                      rows={6}
                      class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 dark:border-slate-700"
                      spellcheck="false"
                      value={renderJsonDraft(field)}
                      oninput={(event) => handleJsonFieldInput(field, (event.currentTarget as HTMLTextAreaElement).value)}
                    ></textarea>
                    {#if fieldErrors[field.name]}
                      <p class="text-xs text-rose-600 dark:text-rose-300">{fieldErrors[field.name]}</p>
                    {/if}
                  </div>
                {:else}
                  <input
                    id={`action-field-${field.name}`}
                    type={field.property_type === 'date' ? 'date' : 'text'}
                    value={typeof fieldValue(field) === 'string' ? String(fieldValue(field)) : String(fieldValue(field) ?? '')}
                    class="w-full rounded-2xl border border-slate-300 px-4 py-2.5 text-sm dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                    oninput={(event) => updateField(field.name, (event.currentTarget as HTMLInputElement).value)}
                  />
                {/if}
                {#if field.description}
                  <p class="text-xs text-slate-500 dark:text-slate-400">{field.description}</p>
                {:else if field.property_type === 'vector'}
                  <p class="text-xs text-slate-500 dark:text-slate-400">
                    Provide a JSON array of numeric values, for example `[0.12, 0.87, 0.44]`.
                  </p>
                {:else if field.property_type === 'media_reference'}
                  <p class="text-xs text-slate-500 dark:text-slate-400">
                    Accepts a URI or URL string, or a JSON object with `uri` or `url`.
                  </p>
                {/if}
              </div>
            {/each}
          </div>
        </section>
      {/if}
    {/each}
  </div>

  <!-- Footer: validate / what-if / execute --------------------------------- -->
  <footer class="action-footer">
    {#if submitError}
      <p class="error" role="alert">{submitError}</p>
    {/if}
    {#if validationResult}
      <div class="validation" data-valid={validationResult.valid}>
        <strong>{validationResult.valid ? 'Valid ✓' : 'Invalid ✗'}</strong>
        {#if validationResult.errors.length}
          <ul>
            {#each validationResult.errors as message}<li>{message}</li>{/each}
          </ul>
        {/if}
      </div>
    {/if}

    {#if lastBranch}
      <section class="whatif">
        <header>
          <strong>What-if sandbox</strong>
          <span>{lastBranch.name}</span>
        </header>
        <table>
          <thead>
            <tr><th>Property</th><th>Before</th><th>After</th></tr>
          </thead>
          <tbody>
            {#each diffEntries(lastBranch.before_object, lastBranch.after_object) as row (row.key)}
              <tr class:changed={row.changed}>
                <td class="key">{row.key}</td>
                <td class="before">{formatValue(row.before)}</td>
                <td class="after">{formatValue(row.after)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </section>
    {/if}

    {#if lastExecution}
      <section class="audit">
        <strong>Last execution</strong>
        <pre>{JSON.stringify(lastExecution, null, 2)}</pre>
      </section>
    {/if}

    <div class="actions">
      <button
        type="button"
        class="secondary"
        disabled={validating || submitting}
        onclick={() => void runValidate()}
      >
        {validating ? 'Validating…' : 'Validate'}
      </button>
      <button
        type="button"
        class="secondary"
        disabled={creatingWhatIf || submitting}
        onclick={() => void runWhatIf()}
      >
        {creatingWhatIf ? 'Building…' : 'What-if'}
      </button>
      <button type="button" class="primary" disabled={submitting} onclick={requestExecute}>
        {submitting
          ? 'Executing…'
          : batchTargetObjectIds.length > 0
            ? `Execute on ${batchTargetObjectIds.length} objects`
            : 'Execute'}
      </button>
    </div>
  </footer>

  {#if confirmOpen}
    <div class="modal-overlay" role="presentation" onclick={() => (confirmOpen = false)}></div>
    <div class="modal" role="dialog" aria-modal="true" aria-label="Confirm action">
      <h3>Confirm “{action.display_name || action.name}”</h3>
      <p>
        This action requires a written justification (audit-grade).
      </p>
      <label>
        <span>Justification</span>
        <textarea bind:value={justification} rows="4" placeholder="Why are you running this action?"></textarea>
      </label>
      <div class="actions">
        <button type="button" class="secondary" onclick={() => (confirmOpen = false)} disabled={submitting}>
          Cancel
        </button>
        <button
          type="button"
          class="primary"
          disabled={submitting || !justification.trim()}
          onclick={() => void doExecute()}
        >
          {submitting ? 'Executing…' : 'Confirm & execute'}
        </button>
      </div>
    </div>
  {/if}
{/if}

<style>
  .action-footer { display: flex; flex-direction: column; gap: 0.75rem; margin-top: 1rem; }
  .actions { display: flex; gap: 0.5rem; justify-content: flex-end; }
  .actions .primary { background: #2563eb; color: white; border: none; padding: 0.5rem 1rem; border-radius: 8px; cursor: pointer; }
  .actions .secondary { background: #1e293b; color: #e2e8f0; border: 1px solid #334155; padding: 0.5rem 1rem; border-radius: 8px; cursor: pointer; }
  .actions button:disabled { opacity: 0.5; cursor: not-allowed; }
  .error { color: #fca5a5; font-size: 0.85rem; }
  .validation { padding: 0.5rem 0.75rem; border-radius: 6px; font-size: 0.85rem; }
  .validation[data-valid='true'] { background: #064e3b; color: #6ee7b7; }
  .validation[data-valid='false'] { background: #7f1d1d; color: #fca5a5; }
  .validation ul { margin: 0.25rem 0 0 1.25rem; }

  .whatif { border: 1px solid #1e293b; border-radius: 8px; padding: 0.75rem; background: #0f172a; }
  .whatif header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem; color: #cbd5e1; font-size: 0.85rem; }
  .whatif table { width: 100%; border-collapse: collapse; font-size: 0.78rem; }
  .whatif th, .whatif td { padding: 0.25rem 0.5rem; text-align: left; border-bottom: 1px solid #1e293b; vertical-align: top; }
  .whatif th { color: #64748b; font-weight: 600; }
  .whatif .key { color: #94a3b8; font-family: monospace; }
  .whatif .before { color: #fca5a5; font-family: monospace; max-width: 30ch; overflow-wrap: anywhere; }
  .whatif .after  { color: #6ee7b7; font-family: monospace; max-width: 30ch; overflow-wrap: anywhere; }
  .whatif tr.changed .before, .whatif tr.changed .after { background: #1e293b; }

  .audit { border: 1px solid #1e293b; border-radius: 8px; padding: 0.5rem 0.75rem; background: #0f172a; }
  .audit pre { margin: 0.25rem 0 0; font-size: 0.75rem; color: #cbd5e1; max-height: 240px; overflow: auto; }

  .modal-overlay { position: fixed; inset: 0; background: rgba(2, 6, 23, 0.55); z-index: 80; }
  .modal {
    position: fixed; left: 50%; top: 50%; transform: translate(-50%, -50%);
    background: #0f172a; border: 1px solid #1e293b; border-radius: 12px;
    padding: 1.25rem; min-width: 420px; max-width: 540px; z-index: 90;
    color: #e2e8f0; display: flex; flex-direction: column; gap: 0.75rem;
  }
  .modal h3 { margin: 0; font-size: 1rem; }
  .modal label { display: flex; flex-direction: column; gap: 0.25rem; font-size: 0.85rem; }
  .modal textarea { font-family: inherit; padding: 0.5rem; border-radius: 6px; border: 1px solid #334155; background: #0b1220; color: inherit; resize: vertical; }
</style>
