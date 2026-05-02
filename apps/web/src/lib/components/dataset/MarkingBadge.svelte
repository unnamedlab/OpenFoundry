<!--
  T3.5 — MarkingBadge

  Renders a single classification marking (PUBLIC / CONFIDENTIAL / PII /
  RESTRICTED / custom) as a pill, color-coded by sensitivity, with a
  tooltip explaining why the marking applies:

    * "Directo" when the dataset owner attached it explicitly,
    * "Heredado de <upstream rid>" when it propagated up the lineage
      graph from a parent dataset (T3.2/T3.4 inheritance).

  The component is intentionally presentational and *controlled*: the
  caller passes the resolved marking with its label + source. This keeps
  the badge reusable for the dataset header, the Files table rows, and
  the Permissions tab's "Effective markings" section.
-->
<script lang="ts">
  type MarkingSource =
    | { kind: 'direct' }
    | { kind: 'inherited_from_upstream'; upstream_rid: string };

  type MarkingLevel =
    | 'public'
    | 'confidential'
    | 'pii'
    | 'restricted'
    | 'unknown';

  type Props = {
    /** Display label, e.g. "PII" or a custom marking name. */
    label: string;
    /** Sensitivity tier — drives the colour. Defaults to `unknown`. */
    level?: MarkingLevel;
    /** Where this marking comes from. Drives the tooltip. */
    source: MarkingSource;
    /** Optional opaque marking id, surfaced as `data-marking-id` for tests. */
    id?: string;
    /** Compact variant: smaller font/padding for inline table cells. */
    compact?: boolean;
  };

  const {
    label,
    level = 'unknown',
    source,
    id,
    compact = false,
  }: Props = $props();

  const tooltip = $derived(
    source.kind === 'direct'
      ? 'Directo'
      : `Heredado de ${source.upstream_rid}`,
  );

  // Colour map kept tiny and explicit; new tiers should be added here
  // intentionally rather than via an open theme map so a typo doesn't
  // silently fall back to "public".
  const palette: Record<MarkingLevel, string> = {
    public:
      'bg-slate-100 text-slate-700 ring-slate-300 dark:bg-slate-800 dark:text-slate-200 dark:ring-slate-600',
    confidential:
      'bg-amber-100 text-amber-900 ring-amber-300 dark:bg-amber-900/40 dark:text-amber-200 dark:ring-amber-700',
    pii:
      'bg-rose-100 text-rose-900 ring-rose-300 dark:bg-rose-900/40 dark:text-rose-200 dark:ring-rose-700',
    restricted:
      'bg-red-200 text-red-900 ring-red-400 dark:bg-red-900/60 dark:text-red-100 dark:ring-red-600',
    unknown:
      'bg-zinc-100 text-zinc-700 ring-zinc-300 dark:bg-zinc-800 dark:text-zinc-200 dark:ring-zinc-600',
  };

  const sizing = $derived(
    compact ? 'px-1.5 py-0.5 text-[10px]' : 'px-2 py-0.5 text-xs',
  );

  const inheritedBadge = $derived(
    source.kind === 'inherited_from_upstream',
  );
</script>

<span
  class={`inline-flex items-center gap-1 rounded-full font-medium uppercase tracking-wide ring-1 ${palette[level]} ${sizing}`}
  data-marking-id={id}
  data-marking-level={level}
  data-marking-source={source.kind}
  title={tooltip}
  aria-label={`${label} marking — ${tooltip}`}
>
  {label}
  {#if inheritedBadge}
    <!-- Up-arrow glyph indicates the marking propagated from upstream. -->
    <span aria-hidden="true" class="opacity-70">↑</span>
  {/if}
</span>
