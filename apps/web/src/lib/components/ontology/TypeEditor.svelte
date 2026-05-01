<script lang="ts">
  /**
   * `TypeEditor` — Form CRUD para un `ObjectType` y sus `Property[]`.
   *
   * Contrato Svelte 5 (runes):
   * - `typeId?: string` — si se pasa, edita; si es `null`/`undefined`, crea.
   * - `oncreated?: (type: ObjectType) => void`
   * - `onupdated?: (type: ObjectType) => void`
   * - `ondeleted?: (id: string) => void`
   *
   * Llama directamente a `createObjectType`, `updateObjectType`,
   * `deleteObjectType`, `listProperties`, `createProperty`, `updateProperty`,
   * `deleteProperty`. El editor in-line de cada propiedad delega en
   * `PropertyPanel.svelte`.
   */
  import {
    createObjectType,
    createProperty,
    deleteObjectType,
    deleteProperty,
    getObjectType,
    listProperties,
    updateObjectType,
    type ObjectType,
    type Property,
  } from '$lib/api/ontology';
  import PropertyPanel from './PropertyPanel.svelte';

  type Props = {
    typeId?: string | null;
    oncreated?: (type: ObjectType) => void;
    onupdated?: (type: ObjectType) => void;
    ondeleted?: (id: string) => void;
  };

  const { typeId = null, oncreated, onupdated, ondeleted }: Props = $props();

  let loading = $state(false);
  let saving = $state(false);
  let error = $state('');

  let draft = $state({
    name: '',
    display_name: '',
    description: '',
    primary_key_property: '',
    icon: '',
    color: '',
  });
  let properties = $state<Property[]>([]);
  let editingType = $state<ObjectType | null>(null);

  const isEdit = $derived(Boolean(typeId));

  $effect(() => {
    if (!typeId) {
      editingType = null;
      properties = [];
      draft = { name: '', display_name: '', description: '', primary_key_property: '', icon: '', color: '' };
      return;
    }
    void loadType(typeId);
  });

  async function loadType(id: string) {
    loading = true;
    error = '';
    try {
      const [type, props] = await Promise.all([getObjectType(id), listProperties(id)]);
      editingType = type;
      properties = props;
      draft = {
        name: type.name,
        display_name: type.display_name,
        description: type.description,
        primary_key_property: type.primary_key_property ?? '',
        icon: type.icon ?? '',
        color: type.color ?? '',
      };
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function save() {
    saving = true;
    error = '';
    try {
      if (isEdit && typeId) {
        const updated = await updateObjectType(typeId, {
          display_name: draft.display_name,
          description: draft.description,
          primary_key_property: draft.primary_key_property || undefined,
          icon: draft.icon || undefined,
          color: draft.color || undefined,
        });
        editingType = updated;
        onupdated?.(updated);
      } else {
        if (!draft.name.trim()) throw new Error('name is required');
        const created = await createObjectType({
          name: draft.name,
          display_name: draft.display_name || undefined,
          description: draft.description || undefined,
          primary_key_property: draft.primary_key_property || undefined,
          icon: draft.icon || undefined,
          color: draft.color || undefined,
        });
        editingType = created;
        oncreated?.(created);
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      saving = false;
    }
  }

  async function remove() {
    if (!typeId) return;
    if (!confirm('Delete this object type and all its properties?')) return;
    saving = true;
    try {
      await deleteObjectType(typeId);
      ondeleted?.(typeId);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      saving = false;
    }
  }

  let newPropertyOpen = $state(false);
  let newPropertyDraft = $state({ name: '', display_name: '', property_type: 'string', required: false });

  async function addProperty() {
    if (!editingType) return;
    try {
      const created = await createProperty(editingType.id, {
        name: newPropertyDraft.name,
        display_name: newPropertyDraft.display_name || undefined,
        property_type: newPropertyDraft.property_type,
        required: newPropertyDraft.required,
      });
      properties = [...properties, created];
      newPropertyDraft = { name: '', display_name: '', property_type: 'string', required: false };
      newPropertyOpen = false;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  async function removeProperty(prop: Property) {
    if (!editingType) return;
    if (!confirm(`Delete property "${prop.name}"?`)) return;
    try {
      await deleteProperty(editingType.id, prop.id);
      properties = properties.filter((p) => p.id !== prop.id);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  function onPropertyUpdated(updated: Property) {
    properties = properties.map((p) => (p.id === updated.id ? updated : p));
  }
</script>

<section class="type-editor" aria-busy={loading || saving}>
  <header>
    <h2>{isEdit ? `Edit object type: ${editingType?.display_name ?? ''}` : 'Create object type'}</h2>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </header>

  <form
    class="grid"
    onsubmit={(event) => {
      event.preventDefault();
      void save();
    }}
  >
    <label>
      <span>Name (slug)</span>
      <input bind:value={draft.name} required disabled={isEdit} />
    </label>
    <label>
      <span>Display name</span>
      <input bind:value={draft.display_name} />
    </label>
    <label class="full">
      <span>Description</span>
      <textarea bind:value={draft.description} rows="2"></textarea>
    </label>
    <label>
      <span>Primary key property</span>
      <input bind:value={draft.primary_key_property} placeholder="e.g. tail_number" />
    </label>
    <label>
      <span>Icon</span>
      <input bind:value={draft.icon} />
    </label>
    <label>
      <span>Color</span>
      <input bind:value={draft.color} type="color" />
    </label>

    <div class="actions">
      <button type="submit" disabled={saving}>{isEdit ? 'Save changes' : 'Create type'}</button>
      {#if isEdit}
        <button type="button" class="danger" disabled={saving} onclick={() => void remove()}>
          Delete
        </button>
      {/if}
    </div>
  </form>

  {#if isEdit && editingType}
    <section class="properties">
      <header>
        <h3>Properties ({properties.length})</h3>
        <button type="button" onclick={() => (newPropertyOpen = !newPropertyOpen)}>
          {newPropertyOpen ? 'Cancel' : '+ Add property'}
        </button>
      </header>

      {#if newPropertyOpen}
        <form
          class="new-prop"
          onsubmit={(event) => {
            event.preventDefault();
            void addProperty();
          }}
        >
          <input bind:value={newPropertyDraft.name} placeholder="name (slug)" required />
          <input bind:value={newPropertyDraft.display_name} placeholder="display name" />
          <select bind:value={newPropertyDraft.property_type}>
            <option value="string">string</option>
            <option value="number">number</option>
            <option value="boolean">boolean</option>
            <option value="datetime">datetime</option>
            <option value="json">json</option>
            <option value="vector">vector</option>
          </select>
          <label class="inline">
            <input type="checkbox" bind:checked={newPropertyDraft.required} /> required
          </label>
          <button type="submit">Add</button>
        </form>
      {/if}

      <ul class="prop-list">
        {#each properties as property (property.id)}
          <li>
            <PropertyPanel
              {property}
              typeId={editingType.id}
              isPrimaryKey={property.name === draft.primary_key_property}
              onupdated={onPropertyUpdated}
              ondeleted={() => void removeProperty(property)}
            />
          </li>
        {/each}
      </ul>
    </section>
  {/if}
</section>

<style>
  .type-editor { display: flex; flex-direction: column; gap: 1.25rem; }
  .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem 1rem; }
  .grid .full { grid-column: 1 / -1; }
  label { display: flex; flex-direction: column; gap: 0.25rem; font-size: 0.85rem; }
  label.inline { flex-direction: row; align-items: center; gap: 0.25rem; }
  .actions { grid-column: 1 / -1; display: flex; gap: 0.5rem; }
  .danger { background: #b91c1c; color: white; }
  .error { color: #b91c1c; }
  .properties header { display: flex; justify-content: space-between; align-items: center; }
  .new-prop { display: grid; grid-template-columns: repeat(5, 1fr); gap: 0.5rem; margin-bottom: 0.75rem; }
  .prop-list { list-style: none; padding: 0; display: flex; flex-direction: column; gap: 0.5rem; }
</style>
