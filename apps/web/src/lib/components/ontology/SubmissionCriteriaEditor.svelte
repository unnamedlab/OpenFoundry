<!--
  TASK C — Submission Criteria editor (recursive Svelte 5 component).

  Mirrors the AST in `libs/ontology-kernel/src/models/submission_criteria.rs`
  and serialises to the JSON shape `submission_eval::evaluate` accepts.

  This file is split into:
    * a wrapper section that renders the root and a "Add criteria" / "Clear"
      header,
    * a recursive node editor (this same file via `<svelte:self>`).

  Both are driven by an `onChange(next)` callback so the parent page can keep
  the entire tree in `$state`.
-->
<script lang="ts" module>
  import type {
    SubmissionNode,
    SubmissionOperand,
    SubmissionOperator,
    SubmissionUserAttr
  } from '$lib/api/ontology';

  export const OPERATORS: { value: SubmissionOperator; label: string; unary?: boolean }[] = [
    { value: 'is', label: 'is' },
    { value: 'is_not', label: 'is not' },
    { value: 'matches', label: 'matches (regex)' },
    { value: 'lt', label: '<' },
    { value: 'lte', label: '<=' },
    { value: 'gt', label: '>' },
    { value: 'gte', label: '>=' },
    { value: 'includes', label: 'includes' },
    { value: 'includes_any', label: 'includes any of' },
    { value: 'is_included_in', label: 'is included in' },
    { value: 'each_is', label: 'each is' },
    { value: 'each_is_not', label: 'each is not' },
    { value: 'is_empty', label: 'is empty', unary: true },
    { value: 'is_not_empty', label: 'is not empty', unary: true }
  ];

  export const USER_ATTRS: { value: SubmissionUserAttr; label: string }[] = [
    { value: 'user_id', label: 'User ID' },
    { value: 'email', label: 'Email' },
    { value: 'organization_id', label: 'Organization ID' },
    { value: 'roles', label: 'Roles' },
    { value: 'permissions', label: 'Permissions' },
    { value: 'auth_methods', label: 'Auth methods' }
  ];

  export function makeLeaf(): SubmissionNode {
    return {
      type: 'leaf',
      left: { kind: 'param', name: '' },
      op: 'is',
      right: { kind: 'static', value: '' }
    };
  }

  export function makeAll(): SubmissionNode {
    return { type: 'all', children: [makeLeaf()] };
  }

  export function makeAny(): SubmissionNode {
    return { type: 'any', children: [makeLeaf()] };
  }
</script>

<script lang="ts">
  import type { ActionInputField } from '$lib/api/ontology';
  import Self from './SubmissionCriteriaEditor.svelte';

  interface Props {
    /** Root node, or `null` when no criteria are configured. */
    value?: SubmissionNode | null;
    /** Action input fields used to populate parameter pickers. */
    parameters: ActionInputField[];
    /** Emits the new root, or `null` when cleared. */
    onChange: (next: SubmissionNode | null) => void;

    // ── Internal recursion props (used by `<svelte:self>`). ─────────────
    /** Current node when rendered as a sub-tree. */
    node?: SubmissionNode;
    /** Whether to show the per-node Delete button. */
    canDelete?: boolean;
    /** Recursive callback that replaces this node in its parent. */
    onNodeChange?: (next: SubmissionNode | null) => void;
  }

  let {
    value = null,
    parameters,
    onChange,
    node,
    canDelete = false,
    onNodeChange
  }: Props = $props();

  // Render mode: "root" wrapper vs. "recursive" sub-tree.
  const isRoot = $derived(node === undefined);

  function rootChange(next: SubmissionNode | null) {
    onChange(next);
  }

  function recursiveChange(next: SubmissionNode | null) {
    onNodeChange?.(next);
  }

  function clearAll() {
    if (typeof window !== 'undefined' && !window.confirm('Clear all submission criteria?'))
      return;
    onChange(null);
  }

  function wrapInNot() {
    if (!node || !onNodeChange) return;
    onNodeChange({ type: 'not', child: node });
  }

  function unwrapNot() {
    if (!node || node.type !== 'not' || !onNodeChange) return;
    onNodeChange(node.child);
  }

  function setFailureMessage(msg: string) {
    if (!node || !onNodeChange) return;
    const trimmed = msg.trim();
    onNodeChange({ ...node, failure_message: trimmed === '' ? undefined : trimmed });
  }

  function addChild(kind: 'leaf' | 'all' | 'any') {
    if (!node || !onNodeChange) return;
    if (node.type !== 'all' && node.type !== 'any') return;
    const next =
      kind === 'leaf' ? makeLeaf() : kind === 'all' ? makeAll() : makeAny();
    onNodeChange({ ...node, children: [...node.children, next] });
  }

  function replaceChildAt(index: number, child: SubmissionNode | null) {
    if (!node || !onNodeChange) return;
    if (node.type !== 'all' && node.type !== 'any') return;
    if (child === null) {
      onNodeChange({ ...node, children: node.children.filter((_, i) => i !== index) });
    } else {
      onNodeChange({ ...node, children: node.children.map((c, i) => (i === index ? child : c)) });
    }
  }

  function setOperator(op: SubmissionNode & { type: 'leaf' }, raw: string) {
    if (!onNodeChange) return;
    onNodeChange({ ...op, op: raw as SubmissionNode extends infer T ? T extends { op: infer O } ? O : never : never });
  }

  function setOperand(side: 'left' | 'right', next: SubmissionOperand) {
    if (!node || node.type !== 'leaf' || !onNodeChange) return;
    onNodeChange({ ...node, [side]: next });
  }

  function changeOperandKind(current: SubmissionOperand, kind: SubmissionOperand['kind']): SubmissionOperand {
    if (kind === 'param') return { kind: 'param', name: parameters[0]?.name ?? '' };
    if (kind === 'param_property')
      return { kind: 'param_property', param: parameters[0]?.name ?? '', property: '' };
    if (kind === 'current_user') return { kind: 'current_user', attribute: 'user_id' };
    return { kind: 'static', value: '' };
  }

  function parseStaticInput(raw: string): unknown {
    try {
      return JSON.parse(raw);
    } catch {
      return raw;
    }
  }

  function operandToInputString(op: SubmissionOperand): string {
    if (op.kind !== 'static') return '';
    if (typeof op.value === 'string') return op.value;
    return JSON.stringify(op.value);
  }
</script>

{#if isRoot}
  <section class="space-y-3 rounded-lg border border-zinc-700 bg-zinc-900/40 p-4">
    <header class="flex items-center justify-between">
      <div>
        <h3 class="text-base font-semibold text-zinc-100">Submission criteria</h3>
        <p class="text-xs text-zinc-400">
          Server-side gate evaluated after authorization and parameter validation,
          before any writeback. Empty = always allowed.
        </p>
      </div>
      <div class="flex items-center gap-2">
        {#if value === null}
          <button
            type="button"
            class="rounded bg-emerald-600 px-3 py-1 text-xs font-medium text-white hover:bg-emerald-500"
            onclick={() => rootChange(makeAll())}
          >
            Add criteria
          </button>
        {:else}
          <button
            type="button"
            class="rounded border border-zinc-600 px-3 py-1 text-xs text-zinc-200 hover:bg-zinc-700"
            onclick={clearAll}
          >
            Clear
          </button>
        {/if}
      </div>
    </header>

    {#if value !== null}
      <Self
        node={value}
        {parameters}
        canDelete={false}
        onNodeChange={(next) => rootChange(next)}
        {onChange}
      />
    {:else}
      <p class="text-xs italic text-zinc-500">No submission criteria configured.</p>
    {/if}
  </section>
{:else if node}
  <div
    class="space-y-2 rounded border border-zinc-700 bg-zinc-950/40 p-3"
    data-node-type={node.type}
  >
    <div class="flex flex-wrap items-center gap-2">
      <span
        class="rounded bg-zinc-800 px-2 py-0.5 text-[11px] font-semibold uppercase tracking-wide text-zinc-300"
      >
        {node.type}
      </span>

      {#if node.type === 'all' || node.type === 'any'}
        <button
          type="button"
          class="rounded border border-zinc-600 px-2 py-0.5 text-[11px] text-zinc-200 hover:bg-zinc-700"
          onclick={() => addChild('leaf')}>+ Leaf</button
        >
        <button
          type="button"
          class="rounded border border-zinc-600 px-2 py-0.5 text-[11px] text-zinc-200 hover:bg-zinc-700"
          onclick={() => addChild('all')}>+ AND</button
        >
        <button
          type="button"
          class="rounded border border-zinc-600 px-2 py-0.5 text-[11px] text-zinc-200 hover:bg-zinc-700"
          onclick={() => addChild('any')}>+ OR</button
        >
      {/if}

      {#if node.type !== 'not'}
        <button
          type="button"
          class="rounded border border-zinc-600 px-2 py-0.5 text-[11px] text-zinc-200 hover:bg-zinc-700"
          onclick={wrapInNot}>Wrap in NOT</button
        >
      {:else}
        <button
          type="button"
          class="rounded border border-zinc-600 px-2 py-0.5 text-[11px] text-zinc-200 hover:bg-zinc-700"
          onclick={unwrapNot}>Unwrap NOT</button
        >
      {/if}

      {#if canDelete}
        <button
          type="button"
          class="ml-auto rounded border border-rose-600 px-2 py-0.5 text-[11px] text-rose-300 hover:bg-rose-900/30"
          onclick={() => recursiveChange(null)}
        >
          Delete
        </button>
      {/if}
    </div>

    {#if node.type === 'leaf'}
      <div class="grid grid-cols-1 gap-2 lg:grid-cols-[1fr_auto_1fr]">
        <!-- Left operand -->
        <div class="space-y-1">
          <label class="text-[10px] uppercase tracking-wide text-zinc-500">Left</label>
          <div class="flex flex-wrap items-center gap-2">
            <select
              class="rounded border border-zinc-600 bg-zinc-800 px-2 py-1 text-xs text-zinc-100"
              value={node.left.kind}
              onchange={(e) =>
                setOperand(
                  'left',
                  changeOperandKind(
                    node.left,
                    (e.currentTarget as HTMLSelectElement).value as SubmissionOperand['kind']
                  )
                )}
            >
              <option value="param">Parameter</option>
              <option value="param_property">Parameter property</option>
              <option value="current_user">Current user</option>
              <option value="static">Static value</option>
            </select>
            {#if node.left.kind === 'param'}
              <select
                class="rounded border border-zinc-600 bg-zinc-800 px-2 py-1 text-xs text-zinc-100"
                value={node.left.name}
                onchange={(e) =>
                  setOperand('left', {
                    kind: 'param',
                    name: (e.currentTarget as HTMLSelectElement).value
                  })}
              >
                <option value="">— select —</option>
                {#each parameters as p}
                  <option value={p.name}>{p.display_name || p.name}</option>
                {/each}
              </select>
            {:else if node.left.kind === 'param_property'}
              <select
                class="rounded border border-zinc-600 bg-zinc-800 px-2 py-1 text-xs text-zinc-100"
                value={node.left.param}
                onchange={(e) =>
                  setOperand('left', {
                    kind: 'param_property',
                    param: (e.currentTarget as HTMLSelectElement).value,
                    property: node.left.kind === 'param_property' ? node.left.property : ''
                  })}
              >
                <option value="">— select —</option>
                {#each parameters as p}
                  <option value={p.name}>{p.display_name || p.name}</option>
                {/each}
              </select>
              <input
                type="text"
                class="rounded border border-zinc-600 bg-zinc-800 px-2 py-1 text-xs text-zinc-100"
                placeholder="property"
                value={node.left.property}
                oninput={(e) =>
                  setOperand('left', {
                    kind: 'param_property',
                    param: node.left.kind === 'param_property' ? node.left.param : '',
                    property: (e.currentTarget as HTMLInputElement).value
                  })}
              />
            {:else if node.left.kind === 'current_user'}
              <select
                class="rounded border border-zinc-600 bg-zinc-800 px-2 py-1 text-xs text-zinc-100"
                value={node.left.attribute}
                onchange={(e) =>
                  setOperand('left', {
                    kind: 'current_user',
                    attribute: (e.currentTarget as HTMLSelectElement).value as SubmissionUserAttr
                  })}
              >
                {#each USER_ATTRS as attr}
                  <option value={attr.value}>{attr.label}</option>
                {/each}
              </select>
            {:else}
              <input
                type="text"
                class="w-48 rounded border border-zinc-600 bg-zinc-800 px-2 py-1 text-xs text-zinc-100"
                placeholder='"approved" / 42 / ["a"]'
                value={operandToInputString(node.left)}
                oninput={(e) =>
                  setOperand('left', {
                    kind: 'static',
                    value: parseStaticInput((e.currentTarget as HTMLInputElement).value)
                  })}
              />
            {/if}
          </div>
        </div>

        <!-- Operator -->
        <div class="space-y-1">
          <label class="text-[10px] uppercase tracking-wide text-zinc-500">Operator</label>
          <select
            class="rounded border border-zinc-600 bg-zinc-800 px-2 py-1 text-xs text-zinc-100"
            value={node.op}
            onchange={(e) => {
              const next = (e.currentTarget as HTMLSelectElement).value;
              if (node.type === 'leaf') {
                recursiveChange({ ...node, op: next as typeof node.op });
              }
            }}
          >
            {#each OPERATORS as op}
              <option value={op.value}>{op.label}</option>
            {/each}
          </select>
        </div>

        <!-- Right operand (hidden for unary operators) -->
        {#if !OPERATORS.find((o) => o.value === node.op)?.unary}
          <div class="space-y-1">
            <label class="text-[10px] uppercase tracking-wide text-zinc-500">Right</label>
            <div class="flex flex-wrap items-center gap-2">
              <select
                class="rounded border border-zinc-600 bg-zinc-800 px-2 py-1 text-xs text-zinc-100"
                value={node.right.kind}
                onchange={(e) =>
                  setOperand(
                    'right',
                    changeOperandKind(
                      node.right,
                      (e.currentTarget as HTMLSelectElement).value as SubmissionOperand['kind']
                    )
                  )}
              >
                <option value="param">Parameter</option>
                <option value="param_property">Parameter property</option>
                <option value="current_user">Current user</option>
                <option value="static">Static value</option>
              </select>
              {#if node.right.kind === 'param'}
                <select
                  class="rounded border border-zinc-600 bg-zinc-800 px-2 py-1 text-xs text-zinc-100"
                  value={node.right.name}
                  onchange={(e) =>
                    setOperand('right', {
                      kind: 'param',
                      name: (e.currentTarget as HTMLSelectElement).value
                    })}
                >
                  <option value="">— select —</option>
                  {#each parameters as p}
                    <option value={p.name}>{p.display_name || p.name}</option>
                  {/each}
                </select>
              {:else if node.right.kind === 'param_property'}
                <select
                  class="rounded border border-zinc-600 bg-zinc-800 px-2 py-1 text-xs text-zinc-100"
                  value={node.right.param}
                  onchange={(e) =>
                    setOperand('right', {
                      kind: 'param_property',
                      param: (e.currentTarget as HTMLSelectElement).value,
                      property: node.right.kind === 'param_property' ? node.right.property : ''
                    })}
                >
                  <option value="">— select —</option>
                  {#each parameters as p}
                    <option value={p.name}>{p.display_name || p.name}</option>
                  {/each}
                </select>
                <input
                  type="text"
                  class="rounded border border-zinc-600 bg-zinc-800 px-2 py-1 text-xs text-zinc-100"
                  placeholder="property"
                  value={node.right.property}
                  oninput={(e) =>
                    setOperand('right', {
                      kind: 'param_property',
                      param: node.right.kind === 'param_property' ? node.right.param : '',
                      property: (e.currentTarget as HTMLInputElement).value
                    })}
                />
              {:else if node.right.kind === 'current_user'}
                <select
                  class="rounded border border-zinc-600 bg-zinc-800 px-2 py-1 text-xs text-zinc-100"
                  value={node.right.attribute}
                  onchange={(e) =>
                    setOperand('right', {
                      kind: 'current_user',
                      attribute: (e.currentTarget as HTMLSelectElement).value as SubmissionUserAttr
                    })}
                >
                  {#each USER_ATTRS as attr}
                    <option value={attr.value}>{attr.label}</option>
                  {/each}
                </select>
              {:else}
                <input
                  type="text"
                  class="w-48 rounded border border-zinc-600 bg-zinc-800 px-2 py-1 text-xs text-zinc-100"
                  placeholder='"approved" / 42 / ["a"]'
                  value={operandToInputString(node.right)}
                  oninput={(e) =>
                    setOperand('right', {
                      kind: 'static',
                      value: parseStaticInput((e.currentTarget as HTMLInputElement).value)
                    })}
                />
              {/if}
            </div>
          </div>
        {/if}
      </div>
    {:else if node.type === 'all' || node.type === 'any'}
      <div class="space-y-2 border-l border-zinc-700 pl-3">
        {#each node.children as child, idx (idx)}
          <Self
            node={child}
            {parameters}
            canDelete={true}
            onNodeChange={(next) => replaceChildAt(idx, next)}
            {onChange}
          />
        {/each}
        {#if node.children.length === 0}
          <p class="text-[11px] italic text-zinc-500">No child criteria yet.</p>
        {/if}
      </div>
    {:else if node.type === 'not'}
      <div class="border-l border-zinc-700 pl-3">
        <Self
          node={node.child}
          {parameters}
          canDelete={false}
          onNodeChange={(next) => {
            if (next === null) recursiveChange(null);
            else recursiveChange({ ...node, child: next });
          }}
          {onChange}
        />
      </div>
    {/if}

    <div class="space-y-1">
      <label class="text-[10px] uppercase tracking-wide text-zinc-500"
        >Failure message (optional)</label
      >
      <input
        type="text"
        class="w-full rounded border border-zinc-600 bg-zinc-800 px-2 py-1 text-xs text-zinc-100"
        placeholder="Surfaced to the operator when this node fails"
        value={node.failure_message ?? ''}
        oninput={(e) => setFailureMessage((e.currentTarget as HTMLInputElement).value)}
      />
    </div>
  </div>
{/if}
