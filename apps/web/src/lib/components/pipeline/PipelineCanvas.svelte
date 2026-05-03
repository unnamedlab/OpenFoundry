<!--
  PipelineCanvas — DAG editor for Pipeline Builder.

  Mirrors the canvas surface from Foundry's Pipeline Builder app:
  · Nodes represent dataset transforms (sql / python / llm / wasm /
    passthrough). Each node owns `config` JSON, an optional output dataset
    binding, and `depends_on` edges that feed its inputs.
  · The canvas serializes the in-flight graph as `PipelineNode[]` (the
    exact JSON spec the backend expects on POST /pipelines and
    POST /pipelines/_validate).
  · Live validation calls `validatePipeline` after every change so the user
    sees Foundry-style "Errors must be resolved before deploy" feedback
    inline, without needing to persist the pipeline first.

  This component is intentionally framework-light: a small Svelte 5 SVG
  renderer using a stage-based layout (Kahn topological sort), which is
  enough for the bring-up. A future iteration can swap the renderer for
  cytoscape (already a workspace dep) without changing the public API.
-->
<script lang="ts">
  import {
    validatePipeline,
    type PipelineNode,
    type PipelineScheduleConfig,
    type PipelineValidationResponse,
  } from '$lib/api/pipelines';

  type Props = {
    nodes: PipelineNode[];
    status?: string;
    scheduleConfig?: PipelineScheduleConfig;
    /** Disable inline editing (read-only canvas, e.g. for run history view). */
    readonly?: boolean;
    /** Called every time the graph mutates with the new node list. */
    onChange?: (nodes: PipelineNode[]) => void;
    /** Called when the user picks a node to edit. */
    onSelect?: (node: PipelineNode | null) => void;
    /** Called when validation completes (debounced). */
    onValidate?: (result: PipelineValidationResponse) => void;
  };

  let {
    nodes = $bindable([]),
    status = 'draft',
    scheduleConfig = { enabled: false, cron: null },
    readonly = false,
    onChange,
    onSelect,
    onValidate,
  }: Props = $props();

  const TRANSFORM_OPTIONS = [
    { value: 'passthrough', label: 'Passthrough' },
    { value: 'sql', label: 'SQL' },
    { value: 'python', label: 'Python' },
    { value: 'llm', label: 'LLM' },
    { value: 'wasm', label: 'WASM' },
  ];

  const NODE_W = 180;
  const NODE_H = 64;
  const STAGE_GAP_X = 80;
  const STAGE_GAP_Y = 24;

  let selectedId = $state<string | null>(null);
  let pendingSourceId = $state<string | null>(null);
  let validation = $state<PipelineValidationResponse | null>(null);
  let validating = $state(false);
  let validationError = $state<string | null>(null);
  let validateTimer: ReturnType<typeof setTimeout> | null = null;

  // Topological staging (Kahn). Cycles are surfaced as validation errors;
  // the renderer falls back to a single stage so the user still sees them.
  const stages = $derived.by<string[][]>(() => {
    const indeg = new Map<string, number>();
    const adj = new Map<string, string[]>();
    for (const n of nodes) {
      indeg.set(n.id, 0);
      adj.set(n.id, []);
    }
    for (const n of nodes) {
      for (const dep of n.depends_on) {
        if (!indeg.has(dep)) continue;
        indeg.set(n.id, (indeg.get(n.id) ?? 0) + 1);
        adj.get(dep)!.push(n.id);
      }
    }
    const result: string[][] = [];
    let frontier = nodes.filter((n) => (indeg.get(n.id) ?? 0) === 0).map((n) => n.id);
    const visited = new Set<string>();
    while (frontier.length > 0) {
      result.push(frontier);
      frontier.forEach((id) => visited.add(id));
      const next: string[] = [];
      for (const id of frontier) {
        for (const child of adj.get(id) ?? []) {
          const v = (indeg.get(child) ?? 0) - 1;
          indeg.set(child, v);
          if (v === 0) next.push(child);
        }
      }
      frontier = next;
    }
    // Anything left is part of a cycle: append as a single stage so it stays visible.
    const orphan = nodes.map((n) => n.id).filter((id) => !visited.has(id));
    if (orphan.length > 0) result.push(orphan);
    return result;
  });

  const positions = $derived.by(() => {
    const map = new Map<string, { x: number; y: number }>();
    stages.forEach((stage, sIdx) => {
      stage.forEach((id, idx) => {
        map.set(id, {
          x: sIdx * (NODE_W + STAGE_GAP_X) + 20,
          y: idx * (NODE_H + STAGE_GAP_Y) + 20,
        });
      });
    });
    return map;
  });

  const canvasW = $derived(stages.length * (NODE_W + STAGE_GAP_X) + 40);
  const canvasH = $derived(
    Math.max(1, ...stages.map((s) => s.length)) * (NODE_H + STAGE_GAP_Y) + 40,
  );

  function emitChange() {
    onChange?.(nodes);
    scheduleValidation();
  }

  function scheduleValidation() {
    if (validateTimer) clearTimeout(validateTimer);
    validateTimer = setTimeout(runValidation, 250);
  }

  async function runValidation() {
    validating = true;
    validationError = null;
    try {
      const result = await validatePipeline({
        status,
        schedule_config: scheduleConfig,
        nodes,
      });
      validation = result;
      onValidate?.(result);
    } catch (cause) {
      // /pipelines/_validate returns 400 with a body when invalid; the
      // client throws — extract the body if present.
      const body = (cause as { body?: PipelineValidationResponse })?.body;
      if (body) {
        validation = body;
        onValidate?.(body);
      } else {
        validationError = cause instanceof Error ? cause.message : 'validation failed';
      }
    } finally {
      validating = false;
    }
  }

  function selectNode(id: string | null) {
    if (readonly && id !== null) return;
    selectedId = id;
    pendingSourceId = null;
    onSelect?.(id ? (nodes.find((n) => n.id === id) ?? null) : null);
  }

  function uniqueId(base: string): string {
    let candidate = base.replace(/[^a-zA-Z0-9_]+/g, '_').toLowerCase() || 'node';
    if (!nodes.some((n) => n.id === candidate)) return candidate;
    let i = 2;
    while (nodes.some((n) => n.id === `${candidate}_${i}`)) i += 1;
    return `${candidate}_${i}`;
  }

  function addNode(transform: string) {
    if (readonly) return;
    const id = uniqueId(`${transform}_node`);
    const node: PipelineNode = {
      id,
      label: `New ${transform} node`,
      transform_type: transform,
      config: {},
      depends_on: selectedId ? [selectedId] : [],
      input_dataset_ids: [],
      output_dataset_id: null,
    };
    nodes = [...nodes, node];
    selectNode(id);
    emitChange();
  }

  function removeSelected() {
    if (readonly || !selectedId) return;
    const removed = selectedId;
    nodes = nodes
      .filter((n) => n.id !== removed)
      .map((n) => ({ ...n, depends_on: n.depends_on.filter((d) => d !== removed) }));
    selectNode(null);
    emitChange();
  }

  function startConnect() {
    if (readonly || !selectedId) return;
    pendingSourceId = selectedId;
  }

  function completeConnect(targetId: string) {
    if (!pendingSourceId || pendingSourceId === targetId) {
      pendingSourceId = null;
      return;
    }
    nodes = nodes.map((n) => {
      if (n.id !== targetId) return n;
      if (n.depends_on.includes(pendingSourceId!)) return n;
      return { ...n, depends_on: [...n.depends_on, pendingSourceId!] };
    });
    pendingSourceId = null;
    emitChange();
  }

  function disconnect(targetId: string, sourceId: string) {
    if (readonly) return;
    nodes = nodes.map((n) =>
      n.id === targetId ? { ...n, depends_on: n.depends_on.filter((d) => d !== sourceId) } : n,
    );
    emitChange();
  }

  // Validate once on mount so the initial graph reports its state.
  $effect(() => {
    scheduleValidation();
    return () => {
      if (validateTimer) clearTimeout(validateTimer);
    };
  });

  function nodeFill(node: PipelineNode): string {
    if (selectedId === node.id) return '#1e40af';
    if (pendingSourceId === node.id) return '#7c3aed';
    return '#1f2937';
  }

  function nodeStroke(node: PipelineNode): string {
    const errored = validation?.errors.some((e) => e.includes(`'${node.id}'`)) ?? false;
    if (errored) return '#ef4444';
    if (validation?.warnings.some((w) => w.includes(`'${node.id}'`))) return '#f59e0b';
    return '#334155';
  }

  /**
   * Per-node tooltip text. The compiler surfaces media-node validation
   * issues as `pipeline node 'id': <reason>`; we strip the prefix so
   * the SVG `<title>` shows just the reason on hover. Mirrors the
   * stale/failed pattern callers already see in the diagnostics list.
   */
  function nodeTooltip(node: PipelineNode): string {
    if (!validation) return node.label || node.id;
    const issues = [
      ...validation.errors.filter((e) => e.includes(`'${node.id}'`)),
      ...validation.warnings.filter((w) => w.includes(`'${node.id}'`))
    ].map((line) => line.replace(`pipeline node '${node.id}': `, ''));
    if (issues.length === 0) return node.label || node.id;
    return issues.join('\n');
  }

  function edgePath(srcId: string, dstId: string): string {
    const s = positions.get(srcId);
    const t = positions.get(dstId);
    if (!s || !t) return '';
    const x1 = s.x + NODE_W;
    const y1 = s.y + NODE_H / 2;
    const x2 = t.x;
    const y2 = t.y + NODE_H / 2;
    const mx = (x1 + x2) / 2;
    return `M ${x1} ${y1} C ${mx} ${y1} ${mx} ${y2} ${x2} ${y2}`;
  }
</script>

<div class="canvas">
  <header class="toolbar">
    <div class="actions">
      {#each TRANSFORM_OPTIONS as opt (opt.value)}
        <button type="button" disabled={readonly} onclick={() => addNode(opt.value)}>
          + {opt.label}
        </button>
      {/each}
      <button
        type="button"
        disabled={readonly || !selectedId}
        onclick={startConnect}
        class:active={pendingSourceId !== null}
      >
        {pendingSourceId ? 'Click target…' : 'Connect →'}
      </button>
      <button type="button" disabled={readonly || !selectedId} onclick={removeSelected}>
        Delete
      </button>
      <button type="button" onclick={runValidation} disabled={validating}>
        {validating ? 'Validating…' : 'Validate'}
      </button>
    </div>
    <div class="status">
      {#if validation}
        <span class="badge" class:ok={validation.valid} class:bad={!validation.valid}>
          {validation.valid ? 'Valid' : `${validation.errors.length} error(s)`}
        </span>
        <span>·</span>
        <span>{validation.summary.node_count} nodes</span>
        <span>·</span>
        <span>{validation.summary.edge_count} edges</span>
        {#if validation.next_run_at}
          <span>·</span>
          <span>next run {new Date(validation.next_run_at).toLocaleString()}</span>
        {/if}
      {:else if validationError}
        <span class="badge bad">{validationError}</span>
      {/if}
    </div>
  </header>

  <div class="surface" role="presentation">
    <svg width={canvasW} height={canvasH}>
      <defs>
        <marker
          id="arrow"
          viewBox="0 0 10 10"
          refX="10"
          refY="5"
          markerWidth="8"
          markerHeight="8"
          orient="auto-start-reverse"
        >
          <path d="M 0 0 L 10 5 L 0 10 z" fill="#94a3b8" />
        </marker>
      </defs>

      {#each nodes as node (node.id)}
        {#each node.depends_on as dep (dep)}
          <path
            d={edgePath(dep, node.id)}
            fill="none"
            stroke="#94a3b8"
            stroke-width="1.5"
            marker-end="url(#arrow)"
            role="button"
            tabindex="-1"
            aria-label={`edge from ${dep} to ${node.id} (double-click to remove)`}
            ondblclick={() => disconnect(node.id, dep)}
          ></path>
        {/each}
      {/each}

      {#each nodes as node (node.id)}
        {@const pos = positions.get(node.id) ?? { x: 0, y: 0 }}
        <g
          transform={`translate(${pos.x}, ${pos.y})`}
          role="button"
          tabindex="0"
          onclick={() => (pendingSourceId ? completeConnect(node.id) : selectNode(node.id))}
          onkeydown={(e: KeyboardEvent) => {
            if (e.key === 'Enter' || e.key === ' ') selectNode(node.id);
          }}
        >
          <title>{nodeTooltip(node)}</title>
          <rect
            width={NODE_W}
            height={NODE_H}
            rx="6"
            fill={nodeFill(node)}
            stroke={nodeStroke(node)}
            stroke-width="1.5"
            data-testid={`canvas-node-${node.id}`}
            data-node-state={
              validation?.errors.some((e) => e.includes(`'${node.id}'`))
                ? 'error'
                : validation?.warnings.some((w) => w.includes(`'${node.id}'`))
                  ? 'warning'
                  : 'ok'
            }
          />
          <text x="12" y="22" fill="#f1f5f9" font-size="13" font-weight="600">
            {node.label || node.id}
          </text>
          <text x="12" y="44" fill="#94a3b8" font-size="11">
            {node.transform_type}
            {#if node.output_dataset_id}· → ds{/if}
          </text>
        </g>
      {/each}

      {#if nodes.length === 0}
        <text x="20" y="40" fill="#64748b" font-size="13">
          Empty pipeline. Use the toolbar to add nodes.
        </text>
      {/if}
    </svg>
  </div>

  {#if validation && (validation.errors.length || validation.warnings.length)}
    <ul class="diagnostics">
      {#each validation.errors as err}
        <li class="err">⨯ {err}</li>
      {/each}
      {#each validation.warnings as warn}
        <li class="warn">! {warn}</li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .canvas {
    display: flex;
    flex-direction: column;
    border: 1px solid #1f2937;
    border-radius: 8px;
    background: #0f172a;
    color: #e2e8f0;
    min-height: 360px;
  }
  .toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 10px 12px;
    border-bottom: 1px solid #1f2937;
    flex-wrap: wrap;
    gap: 8px;
  }
  .actions {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .actions button {
    background: #1e293b;
    border: 1px solid #334155;
    color: #e2e8f0;
    padding: 4px 10px;
    border-radius: 4px;
    font-size: 12px;
    cursor: pointer;
  }
  .actions button:hover:not(:disabled) {
    background: #334155;
  }
  .actions button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .actions button.active {
    background: #7c3aed;
    border-color: #a78bfa;
  }
  .status {
    display: flex;
    gap: 6px;
    align-items: center;
    font-size: 12px;
    color: #94a3b8;
  }
  .badge {
    padding: 2px 8px;
    border-radius: 999px;
    font-size: 11px;
    font-weight: 600;
  }
  .badge.ok {
    background: #052e16;
    color: #86efac;
  }
  .badge.bad {
    background: #450a0a;
    color: #fca5a5;
  }
  .surface {
    overflow: auto;
    flex: 1;
    min-height: 280px;
    padding: 8px;
  }
  .diagnostics {
    list-style: none;
    margin: 0;
    padding: 8px 12px;
    border-top: 1px solid #1f2937;
    max-height: 140px;
    overflow: auto;
    font-size: 12px;
  }
  .diagnostics li.err {
    color: #fca5a5;
  }
  .diagnostics li.warn {
    color: #fcd34d;
  }
  g[role='button'] {
    cursor: pointer;
  }
  g[role='button']:focus {
    outline: 2px solid #60a5fa;
    outline-offset: 2px;
  }
</style>
