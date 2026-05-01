<script lang="ts">
  /**
   * `ObjectCard` — card reutilizable para una `ObjectInstance`.
   *
   * Contrato Svelte 5:
   * - `object: ObjectInstance`
   * - `properties?: Property[]`             — para etiquetas y orden.
   * - `objectType?: ObjectType | null`      — para título / icono / color.
   * - `actions?: Array<{ label: string; onclick: () => void; danger?: boolean }>`
   * - `onclick?: () => void`
   */
  import type { ObjectInstance, ObjectType, Property } from '$lib/api/ontology';

  type Action = { label: string; onclick: () => void; danger?: boolean };
  type Props = {
    object: ObjectInstance;
    properties?: Property[];
    objectType?: ObjectType | null;
    actions?: Action[];
    onclick?: () => void;
  };

  const {
    object,
    properties = [],
    objectType = null,
    actions = [],
    onclick,
  }: Props = $props();

  const visible = $derived(properties.slice(0, 4));
  const marking = $derived(object.marking ?? 'public');
  const title = $derived(() => {
    const pkProp = objectType?.primary_key_property;
    if (pkProp && object.properties?.[pkProp] !== undefined) {
      return String(object.properties[pkProp]);
    }
    return object.id.slice(0, 8) + '…';
  });
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<article
  class="card"
  style:border-left-color={objectType?.color ?? '#475569'}
  onclick={() => onclick?.()}
  onkeydown={(event) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      onclick?.();
    }
  }}
  role={onclick ? 'button' : undefined}
  tabindex={onclick ? 0 : undefined}
>
  <header>
    <strong title={object.id}>{title()}</strong>
    <span class="marking" data-marking={marking}>{marking}</span>
  </header>

  {#if objectType}
    <p class="type">{objectType.display_name || objectType.name}</p>
  {/if}

  <dl>
    {#each visible as property (property.id)}
      <div>
        <dt>{property.display_name || property.name}</dt>
        <dd>{formatValue(object.properties?.[property.name])}</dd>
      </div>
    {/each}
  </dl>

  {#if actions.length > 0}
    <footer>
      {#each actions as action}
        <button
          type="button"
          class:danger={action.danger}
          onclick={(event) => {
            event.stopPropagation();
            action.onclick();
          }}
        >
          {action.label}
        </button>
      {/each}
    </footer>
  {/if}
</article>

<script lang="ts" module>
  function formatValue(value: unknown): string {
    if (value === null || value === undefined) return '—';
    if (typeof value === 'string') return value;
    if (typeof value === 'number' || typeof value === 'boolean') return String(value);
    return JSON.stringify(value);
  }
</script>

<style>
  .card {
    background: #0f172a;
    border: 1px solid #1e293b;
    border-left: 4px solid #475569;
    border-radius: 6px;
    padding: 0.6rem 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    cursor: pointer;
    transition: background 0.15s;
  }
  .card:hover { background: #1e293b; }
  header { display: flex; justify-content: space-between; align-items: center; gap: 0.4rem; }
  .marking { padding: 0.1rem 0.35rem; border-radius: 3px; font-size: 0.7rem; }
  .marking[data-marking='public'] { background: #064e3b; color: #6ee7b7; }
  .marking[data-marking='confidential'] { background: #7c2d12; color: #fdba74; }
  .marking[data-marking='pii'] { background: #831843; color: #f9a8d4; }
  .type { color: #94a3b8; font-size: 0.8rem; margin: 0; }
  dl { display: grid; grid-template-columns: 1fr; gap: 0.2rem; margin: 0; font-size: 0.8rem; }
  dl > div { display: flex; gap: 0.4rem; }
  dt { color: #64748b; min-width: 90px; }
  dd { margin: 0; color: #e2e8f0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  footer { display: flex; gap: 0.4rem; flex-wrap: wrap; }
  .danger { background: #b91c1c; color: white; }
</style>
