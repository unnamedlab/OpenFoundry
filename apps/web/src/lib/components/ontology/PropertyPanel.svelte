<script lang="ts">
  /**
   * `PropertyPanel` — editor in-line para una `Property`: tipo, constraints,
   * embedding flag, primary-key flag, default value JSON.
   *
   * Contrato Svelte 5:
   * - `property: Property`               — propiedad actual.
   * - `typeId: string`                   — id del object_type padre (para PATCH).
   * - `isPrimaryKey?: boolean`           — render-only flag (PK se gestiona en `TypeEditor`).
   * - `onupdated?: (property: Property) => void`
   * - `ondeleted?: () => void`
   */
  import { updateProperty, type Property } from '$lib/api/ontology';

  type Props = {
    property: Property;
    typeId: string;
    isPrimaryKey?: boolean;
    onupdated?: (property: Property) => void;
    ondeleted?: () => void;
  };

  const { property, typeId, isPrimaryKey = false, onupdated, ondeleted }: Props = $props();

  let editing = $state(false);
  let saving = $state(false);
  let error = $state('');

  type Draft = {
    display_name: string;
    description: string;
    required: boolean;
    unique_constraint: boolean;
    time_dependent: boolean;
    default_value_json: string;
  };

  function makeDraft(source: Property): Draft {
    return {
      display_name: source.display_name,
      description: source.description,
      required: source.required,
      unique_constraint: source.unique_constraint,
      time_dependent: source.time_dependent,
      default_value_json:
        source.default_value === null || source.default_value === undefined
          ? ''
          : JSON.stringify(source.default_value, null, 2),
    };
  }

  let draft = $state<Draft>({
    display_name: '',
    description: '',
    required: false,
    unique_constraint: false,
    time_dependent: false,
    default_value_json: '',
  });

  $effect(() => {
    // re-sync when parent passes a new property prop
    draft = makeDraft(property);
  });

  const isVector = $derived(property.property_type === 'vector');

  async function save() {
    saving = true;
    error = '';
    try {
      let defaultValue: unknown = undefined;
      if (draft.default_value_json.trim()) {
        try {
          defaultValue = JSON.parse(draft.default_value_json);
        } catch (parseError) {
          throw new Error(
            `default value must be valid JSON: ${
              parseError instanceof Error ? parseError.message : String(parseError)
            }`,
          );
        }
      }
      const updated = await updateProperty(typeId, property.id, {
        display_name: draft.display_name,
        description: draft.description,
        required: draft.required,
        unique_constraint: draft.unique_constraint,
        time_dependent: draft.time_dependent,
        default_value: defaultValue,
      });
      onupdated?.(updated);
      editing = false;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      saving = false;
    }
  }
</script>

<article class="property" class:editing>
  <header>
    <div class="title">
      <strong>{property.display_name || property.name}</strong>
      <span class="slug">{property.name}</span>
      <span class="type-pill" data-type={property.property_type}>{property.property_type}</span>
      {#if isPrimaryKey}<span class="badge pk">PK</span>{/if}
      {#if property.required}<span class="badge req">required</span>{/if}
      {#if property.unique_constraint}<span class="badge uniq">unique</span>{/if}
      {#if property.time_dependent}<span class="badge time">time-dep</span>{/if}
      {#if isVector}<span class="badge vec">embedding</span>{/if}
    </div>
    <div class="ctrls">
      <button type="button" onclick={() => (editing = !editing)}>
        {editing ? 'Close' : 'Edit'}
      </button>
      {#if ondeleted}
        <button type="button" class="danger" onclick={() => ondeleted()}>Delete</button>
      {/if}
    </div>
  </header>

  {#if editing}
    <form
      class="form"
      onsubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <label>
        <span>Display name</span>
        <input bind:value={draft.display_name} />
      </label>
      <label>
        <span>Description</span>
        <textarea bind:value={draft.description} rows="2"></textarea>
      </label>
      <div class="checks">
        <label class="inline"><input type="checkbox" bind:checked={draft.required} /> required</label>
        <label class="inline"><input type="checkbox" bind:checked={draft.unique_constraint} /> unique</label>
        <label class="inline"><input type="checkbox" bind:checked={draft.time_dependent} /> time-dependent</label>
      </div>
      <label>
        <span>Default value (JSON)</span>
        <textarea bind:value={draft.default_value_json} rows="3" placeholder="null"></textarea>
      </label>
      <div class="actions">
        <button type="submit" disabled={saving}>Save</button>
      </div>
    </form>
  {/if}
</article>

<style>
  .property { border: 1px solid #1f2937; border-radius: 6px; padding: 0.5rem 0.75rem; background: #0f172a; }
  .property.editing { border-color: #2563eb; }
  header { display: flex; justify-content: space-between; align-items: center; gap: 0.5rem; }
  .title { display: flex; gap: 0.4rem; align-items: center; flex-wrap: wrap; }
  .slug { color: #64748b; font-family: monospace; font-size: 0.8rem; }
  .type-pill { background: #1e293b; padding: 0.1rem 0.4rem; border-radius: 4px; font-size: 0.75rem; color: #93c5fd; }
  .badge { padding: 0.05rem 0.35rem; border-radius: 3px; font-size: 0.7rem; }
  .badge.pk { background: #fbbf24; color: #1f2937; }
  .badge.req { background: #b91c1c; color: white; }
  .badge.uniq { background: #047857; color: white; }
  .badge.time { background: #6d28d9; color: white; }
  .badge.vec { background: #be185d; color: white; }
  .ctrls { display: flex; gap: 0.25rem; }
  .danger { background: #b91c1c; color: white; }
  .form { margin-top: 0.5rem; display: flex; flex-direction: column; gap: 0.5rem; }
  .checks { display: flex; gap: 0.75rem; }
  label { display: flex; flex-direction: column; gap: 0.2rem; font-size: 0.8rem; }
  label.inline { flex-direction: row; align-items: center; gap: 0.25rem; }
  .actions { display: flex; justify-content: flex-end; }
  .error { color: #fca5a5; font-size: 0.8rem; }
</style>
