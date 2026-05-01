<script lang="ts">
  /**
   * `LinkEditor` — Form CRUD para un `LinkType` con cardinalidades y
   * selectores `source` / `target` (poblados desde `listObjectTypes`).
   *
   * Contrato Svelte 5:
   * - `linkId?: string | null` — edita si está presente; crea si no.
   * - `defaultSourceTypeId?: string`
   * - `defaultTargetTypeId?: string`
   * - `oncreated?: (link: LinkType) => void`
   * - `onupdated?: (link: LinkType) => void`
   * - `ondeleted?: (id: string) => void`
   */
  import {
    createLinkType,
    deleteLinkType,
    listLinkTypes,
    listObjectTypes,
    updateLinkType,
    type LinkType,
    type ObjectType,
  } from '$lib/api/ontology';

  type Cardinality = 'one_to_one' | 'one_to_many' | 'many_to_one' | 'many_to_many';

  type Props = {
    linkId?: string | null;
    defaultSourceTypeId?: string;
    defaultTargetTypeId?: string;
    oncreated?: (link: LinkType) => void;
    onupdated?: (link: LinkType) => void;
    ondeleted?: (id: string) => void;
  };

  const {
    linkId = null,
    defaultSourceTypeId = '',
    defaultTargetTypeId = '',
    oncreated,
    onupdated,
    ondeleted,
  }: Props = $props();

  let saving = $state(false);
  let loading = $state(false);
  let error = $state('');

  let objectTypes = $state<ObjectType[]>([]);
  let editing = $state<LinkType | null>(null);
  let draft = $state({
    name: '',
    display_name: '',
    description: '',
    source_type_id: '',
    target_type_id: '',
    cardinality: 'many_to_many' as Cardinality,
  });

  const isEdit = $derived(Boolean(linkId));

  $effect(() => {
    void loadTypes();
  });

  $effect(() => {
    if (!linkId) {
      editing = null;
      draft = {
        name: '',
        display_name: '',
        description: '',
        source_type_id: defaultSourceTypeId,
        target_type_id: defaultTargetTypeId,
        cardinality: 'many_to_many',
      };
      return;
    }
    void loadLink(linkId);
  });

  async function loadTypes() {
    try {
      const response = await listObjectTypes({ per_page: 200 });
      objectTypes = response.data ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  async function loadLink(id: string) {
    loading = true;
    error = '';
    try {
      // No `getLinkType` endpoint exposed individually; reuse list.
      const response = await listLinkTypes({ per_page: 500 });
      const found = response.data?.find((link) => link.id === id) ?? null;
      if (!found) throw new Error('link type not found');
      editing = found;
      draft = {
        name: found.name,
        display_name: found.display_name,
        description: found.description,
        source_type_id: found.source_type_id,
        target_type_id: found.target_type_id,
        cardinality: (found.cardinality as Cardinality) ?? 'many_to_many',
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
      if (isEdit && linkId) {
        const updated = await updateLinkType(linkId, {
          display_name: draft.display_name,
          description: draft.description,
          cardinality: draft.cardinality,
        });
        editing = updated;
        onupdated?.(updated);
      } else {
        if (!draft.name.trim()) throw new Error('name is required');
        if (!draft.source_type_id) throw new Error('source type is required');
        if (!draft.target_type_id) throw new Error('target type is required');
        const created = await createLinkType({
          name: draft.name,
          display_name: draft.display_name || undefined,
          description: draft.description || undefined,
          source_type_id: draft.source_type_id,
          target_type_id: draft.target_type_id,
          cardinality: draft.cardinality,
        });
        editing = created;
        oncreated?.(created);
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      saving = false;
    }
  }

  async function remove() {
    if (!linkId) return;
    if (!confirm('Delete this link type?')) return;
    saving = true;
    try {
      await deleteLinkType(linkId);
      ondeleted?.(linkId);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      saving = false;
    }
  }
</script>

<section class="link-editor" aria-busy={loading || saving}>
  <header>
    <h2>{isEdit ? `Edit link type: ${editing?.display_name ?? ''}` : 'Create link type'}</h2>
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
      <span>Source object type</span>
      <select bind:value={draft.source_type_id} required disabled={isEdit}>
        <option value="">— select —</option>
        {#each objectTypes as ot (ot.id)}
          <option value={ot.id}>{ot.display_name || ot.name}</option>
        {/each}
      </select>
    </label>
    <label>
      <span>Target object type</span>
      <select bind:value={draft.target_type_id} required disabled={isEdit}>
        <option value="">— select —</option>
        {#each objectTypes as ot (ot.id)}
          <option value={ot.id}>{ot.display_name || ot.name}</option>
        {/each}
      </select>
    </label>
    <label class="full">
      <span>Cardinality</span>
      <select bind:value={draft.cardinality}>
        <option value="one_to_one">one_to_one</option>
        <option value="one_to_many">one_to_many</option>
        <option value="many_to_one">many_to_one</option>
        <option value="many_to_many">many_to_many</option>
      </select>
    </label>

    <div class="actions">
      <button type="submit" disabled={saving}>{isEdit ? 'Save changes' : 'Create link'}</button>
      {#if isEdit}
        <button type="button" class="danger" disabled={saving} onclick={() => void remove()}>
          Delete
        </button>
      {/if}
    </div>
  </form>
</section>

<style>
  .link-editor { display: flex; flex-direction: column; gap: 1rem; }
  .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem 1rem; }
  .grid .full { grid-column: 1 / -1; }
  label { display: flex; flex-direction: column; gap: 0.25rem; font-size: 0.85rem; }
  .actions { grid-column: 1 / -1; display: flex; gap: 0.5rem; }
  .danger { background: #b91c1c; color: white; }
  .error { color: #b91c1c; }
</style>
