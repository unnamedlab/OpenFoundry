<script lang="ts">
  /**
   * Iceberg snapshot operation badge.
   *
   * Used by the Iceberg-tables Snapshots tab. Renders a coloured pill
   * + a tooltip explaining the Foundry equivalent (per
   * `Iceberg tables/Transactions.md` § "Iceberg snapshot types and
   * Foundry dataset transactions").
   *
   *  Iceberg op  →  Foundry transaction
   *    append    →  APPEND
   *    overwrite →  UPDATE *or* SNAPSHOT (full sweep)
   *    delete    →  DELETE
   *    replace   →  (compaction — no equivalent)
   */
  type Operation = 'append' | 'overwrite' | 'delete' | 'replace';

  let {
    operation,
    foundry_equivalent,
    overwrite_kind,
  }: {
    operation: Operation;
    /** Pre-computed by the backend when sufficient context is
     * available; otherwise we fall back to the static heuristic. */
    foundry_equivalent?: 'APPEND' | 'UPDATE' | 'SNAPSHOT' | 'DELETE' | 'INTERNAL_NOOP';
    overwrite_kind?: 'full' | 'partial';
  } = $props();

  const tone = $derived.by(() => {
    switch (operation) {
      case 'append':
        return 'bg-blue-100 text-blue-800 border-blue-200 dark:bg-blue-950 dark:text-blue-300 dark:border-blue-900';
      case 'overwrite':
        return 'bg-orange-100 text-orange-800 border-orange-200 dark:bg-orange-950 dark:text-orange-300 dark:border-orange-900';
      case 'delete':
        return 'bg-red-100 text-red-800 border-red-200 dark:bg-red-950 dark:text-red-300 dark:border-red-900';
      case 'replace':
        return 'bg-gray-100 text-gray-800 border-gray-200 dark:bg-gray-800 dark:text-gray-300 dark:border-gray-700';
    }
  });

  const foundry = $derived.by(() => {
    if (foundry_equivalent) return foundry_equivalent;
    switch (operation) {
      case 'append':
        return 'APPEND';
      case 'delete':
        return 'DELETE';
      case 'overwrite':
        return overwrite_kind === 'full' ? 'SNAPSHOT' : 'UPDATE';
      case 'replace':
        return 'INTERNAL_NOOP';
    }
  });

  const tooltip = $derived(
    `Iceberg ${operation} corresponds to Foundry ${foundry === 'INTERNAL_NOOP' ? '(maintenance — no equivalent)' : foundry}`,
  );
</script>

<span
  class={`inline-flex items-center rounded border px-1.5 py-0.5 text-xs font-semibold ${tone}`}
  title={tooltip}
  data-testid={`iceberg-snapshot-badge-${operation}`}
>
  {operation}
  <span class="ml-1 text-[0.65rem] opacity-70">→ {foundry}</span>
</span>
