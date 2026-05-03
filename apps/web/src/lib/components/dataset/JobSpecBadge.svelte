<!--
  P3 — JobSpecBadge

  Tiny coloured badge that mirrors the Foundry doc § "Job graph
  compilation":

    > Dataset icon color provides information about JobSpecs and
    > branching. If a dataset's icon is gray, this indicates that no
    > JobSpec exists on the master branch. If the dataset icon is
    > blue, a JobSpec is defined on the master branch.

  Used in:
    * `routes/datasets/+page.svelte` (catalog cards)
    * `DatasetHeader.svelte` (next to the dataset name)
    * `routes/lineage/+page.svelte` (per-dataset node colouring)
-->
<script lang="ts">
  type Props = {
    hasMasterJobspec: boolean;
    /** Optional list of branches that publish a JobSpec — used for the tooltip. */
    branchesWithJobspec?: string[];
    /** "compact" hides the label so the badge collapses into a dot. */
    compact?: boolean;
  };

  const { hasMasterJobspec, branchesWithJobspec = [], compact = false }: Props = $props();

  const tooltip = $derived(() =>
    hasMasterJobspec
      ? `JobSpec defined on master${
          branchesWithJobspec.length > 1
            ? ` (also on ${branchesWithJobspec.filter((b) => b !== 'master').join(', ')})`
            : ''
        }`
      : 'No JobSpec on master',
  );
</script>

<span
  class={`inline-flex items-center gap-1 rounded-full text-xs font-medium ${
    hasMasterJobspec
      ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300'
      : 'bg-gray-200 text-gray-600 dark:bg-gray-800 dark:text-gray-400'
  } ${compact ? 'h-2 w-2 rounded-full p-0' : 'px-2 py-0.5'}`}
  title={tooltip()}
  aria-label={tooltip()}
  data-testid="jobspec-badge"
  data-jobspec-on-master={hasMasterJobspec}
>
  {#if !compact}
    <span aria-hidden="true">●</span>
    <span>{hasMasterJobspec ? 'JobSpec on master' : 'No JobSpec'}</span>
  {/if}
</span>
