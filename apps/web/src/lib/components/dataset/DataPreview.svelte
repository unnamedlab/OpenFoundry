<!--
  P2 — DataPreview.

  View-scoped Foundry preview wrapper. Fetches `previewView` against
  the dataset-versioning-service preview endpoint, hands the rows to
  `VirtualizedPreviewTable`, and surfaces:

   * the file-format badge (PARQUET / AVRO / TEXT-CSV / TEXT-JSON);
   * the "Schema not configured" banner when the persisted schema is
     empty and the reader fell back to inference;
   * the live CsvOptions tooltip for TEXT previews.

  Inputs:
   * `datasetRid`, `viewId` — required, identify which view to preview.
   * `transactions` — passed through to the table for the "view picker"
     timeline (parent-owned).
   * `selectedTransactionId` — optional controlled selection.
   * `onOpenSchemaEditor` — CTA wired by the parent dataset detail page.
-->
<script lang="ts">
  import {
    previewView,
    type DatasetTransaction,
    type ViewPreviewResponse,
  } from '$lib/api/datasets';
  import VirtualizedPreviewTable, {
    type ColumnDef,
    type CsvOptionsSummary,
  } from './VirtualizedPreviewTable.svelte';

  type Props = {
    datasetRid: string;
    viewId: string;
    transactions: DatasetTransaction[];
    selectedTransactionId?: string | null;
    onSelectTransaction?: (txId: string | null) => void;
    onOpenSchemaEditor?: () => void;
    /** Override format for this preview only (debug / mis-configured TEXT). */
    formatOverride?: 'auto' | 'parquet' | 'avro' | 'text';
    /** Override CSV options for this preview only (no persistence). */
    csvOverrides?: {
      delimiter?: string;
      quote?: string;
      escape?: string;
      header?: boolean;
      null_value?: string;
      charset?: string;
      date_format?: string;
      timestamp_format?: string;
      csv?: boolean;
    };
    rowHeight?: number;
    viewportHeight?: number;
    limit?: number;
  };

  const {
    datasetRid,
    viewId,
    transactions,
    selectedTransactionId = null,
    onSelectTransaction,
    onOpenSchemaEditor,
    formatOverride = 'auto',
    csvOverrides,
    rowHeight = 32,
    viewportHeight = 480,
    limit = 1000,
  }: Props = $props();

  let response = $state<ViewPreviewResponse | null>(null);
  let loading = $state(false);
  let errorMessage = $state<string | null>(null);

  // Track the last (datasetRid, viewId, overrides) tuple so we don't
  // refetch when the parent re-renders with the same args.
  let lastFetchKey = $state<string>('');

  $effect(() => {
    const key = JSON.stringify({
      datasetRid,
      viewId,
      formatOverride,
      csvOverrides,
      limit,
    });
    if (!datasetRid || !viewId) return;
    if (key === lastFetchKey) return;
    lastFetchKey = key;
    void load();
  });

  async function load() {
    loading = true;
    errorMessage = null;
    try {
      response = await previewView(datasetRid, viewId, {
        limit,
        offset: 0,
        format: formatOverride,
        csv_delimiter: csvOverrides?.delimiter,
        csv_quote: csvOverrides?.quote,
        csv_escape: csvOverrides?.escape,
        csv_header: csvOverrides?.header,
        csv_null_value: csvOverrides?.null_value,
        csv_charset: csvOverrides?.charset,
        csv_date_format: csvOverrides?.date_format,
        csv_timestamp_format: csvOverrides?.timestamp_format,
        csv: csvOverrides?.csv,
      });
    } catch (cause) {
      errorMessage = cause instanceof Error ? cause.message : 'Preview failed.';
      response = null;
    } finally {
      loading = false;
    }
  }

  const columns: ColumnDef[] = $derived(
    (response?.columns ?? []).map((col) => ({
      name: col.name,
      field_type: col.field_type,
    })),
  );
  const rows = $derived(response?.rows ?? []);
  const csvSummary: CsvOptionsSummary | null = $derived(
    response?.csv_options
      ? {
          delimiter: response.csv_options.delimiter,
          quote: response.csv_options.quote,
          escape: response.csv_options.escape,
          header: response.csv_options.header,
          null_value: response.csv_options.null_value,
          date_format: response.csv_options.date_format,
          timestamp_format: response.csv_options.timestamp_format,
          charset: response.csv_options.charset,
        }
      : null,
  );
</script>

<section class="space-y-3" data-component="data-preview">
  {#if loading}
    <div class="rounded-lg border border-slate-200 bg-white px-4 py-3 text-sm text-slate-600 shadow-sm dark:border-gray-800 dark:bg-gray-950 dark:text-gray-300" role="status">
      Loading preview...
    </div>
  {:else if errorMessage}
    <div class="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-800 dark:border-rose-900/50 dark:bg-rose-950/40 dark:text-rose-100" role="alert" data-testid="preview-error">
      {errorMessage}
    </div>
  {:else if response}
    <VirtualizedPreviewTable
      columns={columns}
      rows={rows}
      transactions={transactions}
      selectedTransactionId={selectedTransactionId}
      onSelectTransaction={onSelectTransaction ?? (() => {})}
      rowHeight={rowHeight}
      viewportHeight={viewportHeight}
      fileFormat={response.file_format}
      textSubFormat={response.text_sub_format}
      schemaInferred={response.schema_inferred}
      csvOptions={csvSummary}
      onOpenSchemaEditor={onOpenSchemaEditor}
    />
  {/if}
</section>
