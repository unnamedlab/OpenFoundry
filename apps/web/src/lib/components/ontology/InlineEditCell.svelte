<!--
  TASK L — InlineEditCell

  Renders a single property value as a cell that can be activated by
  double-click into an editable input. On blur, the buffered value is
  POSTed via the inline-edit API. Mirrors the Foundry "cell-level
  writeback" UX described in `Inline edits.md`.

  Usage:
    <InlineEditCell
      typeId={objectType.id}
      objectId={object.id}
      property={property}
      value={object.properties[property.name]}
      onUpdated={(next) => (object.properties[property.name] = next)}
    />

  Properties without `inline_edit_config` render in read-only mode.
-->
<script lang="ts">
  import { executeInlineEdit, type Property } from '$lib/api/ontology';

  let {
    typeId,
    objectId,
    property,
    value,
    onUpdated
  }: {
    typeId: string;
    objectId: string;
    property: Property;
    value: unknown;
    onUpdated?: (nextValue: unknown) => void;
  } = $props();

  let editing = $state(false);
  let draft = $state('');
  let saving = $state(false);
  let error = $state('');

  const editable = $derived(Boolean(property.inline_edit_config));

  function formatDisplay(raw: unknown): string {
    if (raw === null || raw === undefined) return '';
    if (typeof raw === 'object') return JSON.stringify(raw);
    return String(raw);
  }

  function startEditing() {
    if (!editable) return;
    draft = formatDisplay(value);
    error = '';
    editing = true;
  }

  function parseSubmissionValue(raw: string): unknown {
    const trimmed = raw.trim();
    switch (property.property_type) {
      case 'integer':
        return trimmed === '' ? null : Number.parseInt(trimmed, 10);
      case 'float':
        return trimmed === '' ? null : Number.parseFloat(trimmed);
      case 'boolean':
        return trimmed.toLowerCase() === 'true';
      case 'json':
      case 'array':
      case 'struct':
        return trimmed === '' ? null : JSON.parse(trimmed);
      default:
        return trimmed === '' ? null : trimmed;
    }
  }

  async function commit() {
    if (!editing || saving) return;
    saving = true;
    try {
      const submissionValue = parseSubmissionValue(draft);
      // No-op when the value didn't actually change.
      if (formatDisplay(submissionValue) === formatDisplay(value)) {
        editing = false;
        return;
      }
      await executeInlineEdit(typeId, objectId, property.id, { value: submissionValue });
      onUpdated?.(submissionValue);
      editing = false;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      saving = false;
    }
  }

  function cancel() {
    editing = false;
    error = '';
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Enter') {
      event.preventDefault();
      void commit();
    } else if (event.key === 'Escape') {
      event.preventDefault();
      cancel();
    }
  }
</script>

{#if editing}
  <div class="flex flex-col gap-1">
    {#if property.property_type === 'boolean'}
      <select
        bind:value={draft}
        onblur={commit}
        onkeydown={handleKeydown}
        disabled={saving}
        class="rounded-lg border border-emerald-400 bg-white px-2 py-1 text-sm dark:bg-slate-900 dark:text-slate-100"
      >
        <option value="true">true</option>
        <option value="false">false</option>
      </select>
    {:else}
      <input
        bind:value={draft}
        onblur={commit}
        onkeydown={handleKeydown}
        disabled={saving}
        autofocus
        class="rounded-lg border border-emerald-400 bg-white px-2 py-1 text-sm dark:bg-slate-900 dark:text-slate-100"
        type={property.property_type === 'date' ? 'date' : property.property_type === 'integer' || property.property_type === 'float' ? 'number' : 'text'}
        step={property.property_type === 'float' ? 'any' : undefined}
      />
    {/if}
    {#if saving}
      <span class="text-[11px] text-slate-500">Saving…</span>
    {/if}
    {#if error}
      <span class="text-[11px] text-rose-600 dark:text-rose-300">{error}</span>
    {/if}
  </div>
{:else}
  <button
    type="button"
    ondblclick={startEditing}
    title={editable ? 'Double-click to edit' : 'No inline edit configured'}
    class={`block w-full rounded px-1 py-0.5 text-left text-sm ${
      editable
        ? 'cursor-text hover:bg-emerald-50 dark:hover:bg-emerald-950/40'
        : 'cursor-default text-slate-700 dark:text-slate-300'
    }`}
  >
    {formatDisplay(value) || (editable ? '—' : '')}
  </button>
{/if}
