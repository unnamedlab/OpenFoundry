<script lang="ts">
  /**
   * `GraphView` — Editor visual ER (Object Types + Link Types) basado en
   * cytoscape + cytoscape-fcose.
   *
   * Interacciones (modo `editable`):
   *   - Layout fCoSE (force-directed) aplicado al cargar y al añadir elementos.
   *   - Doble-click en canvas vacío → abre Drawer con `TypeEditor` en modo crear.
   *   - Doble-click en nodo → abre Drawer con `TypeEditor` en modo editar.
   *   - Click derecho en un nodo → marca `source`. Click derecho en un segundo
   *     nodo → abre Drawer con `LinkEditor` pre-rellenado (source/target). Esc
   *     cancela la selección de origen.
   *   - Click derecho en una arista → abre Drawer con `LinkEditor` editar.
   *   - Persistencia: `TypeEditor` y `LinkEditor` ya invocan los endpoints
   *     `createObjectType` / `createLinkType` / `update*`. Tras guardar
   *     emitimos `onchange` para que el padre recargue el catálogo.
   *
   * Contrato Svelte 5:
   *   - `objectTypes: ObjectType[]`
   *   - `linkTypes: LinkType[]`
   *   - `selectedTypeId?: string | null`
   *   - `editable?: boolean = true`
   *   - `onnodeselect?: (typeId: string) => void`
   *   - `onedgeselect?: (linkTypeId: string) => void`
   *   - `onchange?: () => void`
   */
  import { onDestroy } from 'svelte';
  import type { Core, ElementDefinition, EventObject, LayoutOptions } from 'cytoscape';
  import type { LinkType, ObjectType } from '$lib/api/ontology';
  import Drawer from '$lib/components/ui/Drawer.svelte';
  import TypeEditor from './TypeEditor.svelte';
  import LinkEditor from './LinkEditor.svelte';

  type Props = {
    objectTypes: ObjectType[];
    linkTypes: LinkType[];
    selectedTypeId?: string | null;
    editable?: boolean;
    onnodeselect?: (typeId: string) => void;
    onedgeselect?: (linkTypeId: string) => void;
    onchange?: () => void;
  };

  const {
    objectTypes,
    linkTypes,
    selectedTypeId = null,
    editable = true,
    onnodeselect,
    onedgeselect,
    onchange,
  }: Props = $props();

  let container: HTMLDivElement | null = $state(null);
  let cy: Core | null = null;
  let dragSourceId: string | null = null;

  // Drawer state (Svelte 5 runes)
  let typeDrawerOpen = $state(false);
  let typeDrawerId = $state<string | null>(null);
  let linkDrawerOpen = $state(false);
  let linkDrawerId = $state<string | null>(null);
  let linkDrawerSourceId = $state<string>('');
  let linkDrawerTargetId = $state<string>('');

  $effect(() => {
    if (!container) return;
    void mount(container);
    return () => {
      cy?.destroy();
      cy = null;
    };
  });

  // Re-render when input arrays change (after onchange refreshes parent state).
  $effect(() => {
    void objectTypes;
    void linkTypes;
    if (!cy) return;
    rebuildElements();
  });

  $effect(() => {
    if (!cy) return;
    cy.nodes().removeClass('selected');
    if (selectedTypeId) cy.getElementById(selectedTypeId).addClass('selected');
  });

  onDestroy(() => {
    cy?.destroy();
    cy = null;
    if (typeof window !== 'undefined') {
      window.removeEventListener('keydown', onCanvasKeydown);
    }
  });

  async function mount(target: HTMLDivElement) {
    cy?.destroy();
    const cytoscape = (await import('cytoscape')).default;
    const fcose = (await import('cytoscape-fcose')).default;
    try {
      cytoscape.use(fcose);
    } catch {
      /* already registered */
    }

    cy = cytoscape({
      container: target,
      elements: buildElements(),
      wheelSensitivity: 0.25,
      style: [
        {
          selector: 'node',
          style: {
            'background-color': 'data(color)',
            label: 'data(label)',
            color: '#e5e7eb',
            'text-valign': 'bottom',
            'text-margin-y': 8,
            'text-wrap': 'wrap',
            'text-max-width': '140',
            'font-size': '11px',
            'font-weight': 600,
            width: 44,
            height: 44,
            'border-width': 2,
            'border-color': '#1e293b',
          },
        },
        {
          selector: 'node.selected',
          style: { 'border-width': 4, 'border-color': '#fbbf24' },
        },
        {
          selector: 'node.drag-source',
          style: { 'border-color': '#22d3ee', 'border-width': 4 },
        },
        {
          selector: 'edge',
          style: {
            width: 1.6,
            label: 'data(cardinality)',
            'font-size': '9px',
            color: '#cbd5e1',
            'text-background-color': '#0f172a',
            'text-background-opacity': 0.7,
            'text-background-padding': '2',
            'line-color': '#475569',
            'target-arrow-color': '#475569',
            'target-arrow-shape': 'triangle',
            'curve-style': 'bezier',
          },
        },
      ],
      layout: fcoseLayout(),
    });

    if (selectedTypeId) cy.getElementById(selectedTypeId).addClass('selected');

    cy.on('tap', 'node', (event: EventObject) => {
      const id = event.target.id() as string;
      onnodeselect?.(id);
    });
    cy.on('tap', 'edge', (event: EventObject) => {
      const id = event.target.id() as string;
      onedgeselect?.(id);
    });

    if (editable) {
      cy.on('dbltap', 'node', (event: EventObject) => {
        const id = event.target.id() as string;
        openTypeDrawer(id);
      });
      cy.on('dbltap', (event: EventObject) => {
        if (event.target === cy) openTypeDrawer(null);
      });
      cy.on('cxttap', 'edge', (event: EventObject) => {
        const id = event.target.id() as string;
        openLinkDrawer(id, '', '');
      });
      cy.on('cxttap', 'node', (event: EventObject) => {
        const nodeId = event.target.id() as string;
        if (dragSourceId && dragSourceId !== nodeId) {
          openLinkDrawer(null, dragSourceId, nodeId);
          clearDragState();
        } else {
          clearDragState();
          dragSourceId = nodeId;
          event.target.addClass('drag-source');
        }
      });
      window.addEventListener('keydown', onCanvasKeydown);
    }
  }

  function fcoseLayout(): LayoutOptions {
    return {
      name: 'fcose',
      animate: true,
      animationDuration: 400,
      randomize: false,
      nodeRepulsion: 6500,
      idealEdgeLength: 110,
      padding: 30,
    } as unknown as LayoutOptions;
  }

  function buildElements(): ElementDefinition[] {
    const nodes: ElementDefinition[] = objectTypes.map((type) => ({
      group: 'nodes',
      data: {
        id: type.id,
        label: type.display_name || type.name,
        color: type.color ?? '#2563eb',
      },
    }));
    const edges: ElementDefinition[] = linkTypes.map((link) => ({
      group: 'edges',
      data: {
        id: link.id,
        source: link.source_type_id,
        target: link.target_type_id,
        label: link.display_name || link.name,
        cardinality: link.cardinality,
      },
    }));
    return [...nodes, ...edges];
  }

  function rebuildElements() {
    if (!cy) return;
    cy.batch(() => {
      cy!.elements().remove();
      cy!.add(buildElements());
    });
    runLayout();
  }

  function runLayout() {
    if (!cy) return;
    cy.layout(fcoseLayout()).run();
  }

  function openTypeDrawer(id: string | null) {
    typeDrawerId = id;
    typeDrawerOpen = true;
  }
  function openLinkDrawer(id: string | null, sourceId: string, targetId: string) {
    linkDrawerId = id;
    linkDrawerSourceId = sourceId;
    linkDrawerTargetId = targetId;
    linkDrawerOpen = true;
  }
  function clearDragState() {
    if (dragSourceId && cy) {
      cy.getElementById(dragSourceId).removeClass('drag-source');
    }
    dragSourceId = null;
  }
  function onCanvasKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') clearDragState();
  }

  function notifyChange() {
    typeDrawerOpen = false;
    linkDrawerOpen = false;
    onchange?.();
  }
</script>

<section class="graph">
  <header>
    <h3>Ontology graph ({objectTypes.length} types, {linkTypes.length} links)</h3>
    {#if editable}
      <div class="hint">
        <span>2× click canvas → new type</span>
        <span>2× click node → edit type</span>
        <span>right-click 2 nodes → new link</span>
        <span>right-click edge → edit link</span>
        <button type="button" onclick={runLayout}>Re-layout</button>
      </div>
    {/if}
  </header>
  <div bind:this={container} class="canvas"></div>
</section>

<Drawer
  bind:open={typeDrawerOpen}
  title={typeDrawerId ? 'Edit object type' : 'Create object type'}
  width="520px"
>
  {#snippet children()}
    <TypeEditor
      typeId={typeDrawerId}
      oncreated={() => notifyChange()}
      onupdated={() => notifyChange()}
      ondeleted={() => notifyChange()}
    />
  {/snippet}
</Drawer>

<Drawer
  bind:open={linkDrawerOpen}
  title={linkDrawerId ? 'Edit link type' : 'Create link type'}
  width="520px"
>
  {#snippet children()}
    <LinkEditor
      linkId={linkDrawerId}
      defaultSourceTypeId={linkDrawerSourceId}
      defaultTargetTypeId={linkDrawerTargetId}
      oncreated={() => notifyChange()}
      onupdated={() => notifyChange()}
      ondeleted={() => notifyChange()}
    />
  {/snippet}
</Drawer>

<style>
  .graph { display: flex; flex-direction: column; gap: 0.5rem; height: 100%; min-height: 480px; }
  header { display: flex; justify-content: space-between; align-items: center; gap: 0.5rem; flex-wrap: wrap; }
  .hint { display: flex; gap: 0.5rem; align-items: center; flex-wrap: wrap; font-size: 0.7rem; color: #64748b; }
  .hint button { background: #1e293b; color: #e2e8f0; border: 1px solid #334155; padding: 0.2rem 0.5rem; border-radius: 4px; cursor: pointer; }
  .canvas { flex: 1; min-height: 460px; background: #0b1220; border: 1px solid #1e293b; border-radius: 6px; }
</style>
