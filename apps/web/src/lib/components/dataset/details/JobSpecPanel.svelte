<!--
  T5.1 — JobSpecPanel

  Surfaces "what pipeline produces this dataset". The catalog dataset
  row doesn't yet carry a job_spec field, so the parent passes an
  optional `jobSpec` that may be `null` (we render an empty state).
  When present, we show the pipeline name, the source repo + branch,
  and a deep link to the Pipeline Builder / Code Repos UI.
-->
<script lang="ts">
  type JobSpec = {
    pipeline_id: string;
    pipeline_name: string;
    repo_url?: string;
    repo_path?: string;
    branch?: string;
    last_run_at?: string | null;
    last_run_status?: 'success' | 'failed' | 'running' | string;
  };

  type Props = {
    jobSpec: JobSpec | null;
  };

  const { jobSpec }: Props = $props();

  function statusTone(s?: string): string {
    if (s === 'success')
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300';
    if (s === 'failed')
      return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300';
    if (s === 'running')
      return 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300';
    return 'bg-slate-100 text-slate-700 dark:bg-gray-800 dark:text-gray-300';
  }
</script>

<section class="space-y-4">
  <header>
    <div class="text-xs uppercase tracking-[0.22em] text-gray-400">Job spec</div>
    <h2 class="mt-1 text-lg font-semibold">Producing pipeline</h2>
    <p class="mt-1 text-sm text-gray-500">
      The pipeline definition responsible for materialising this dataset.
    </p>
  </header>

  {#if !jobSpec}
    <div class="rounded-xl border border-dashed border-slate-300 px-4 py-8 text-center text-sm text-gray-500 dark:border-gray-700">
      No pipeline is bound to this dataset. Bind one from
      <a href="/pipeline-builder" class="text-blue-600 underline hover:text-blue-700">
        Pipeline Builder
      </a>
      or push a job-spec from a Code Repo.
    </div>
  {:else}
    <dl class="grid grid-cols-1 gap-3 text-sm md:grid-cols-2">
      <div>
        <dt class="text-xs uppercase tracking-wide text-gray-400">Pipeline</dt>
        <dd class="mt-0.5">
          <a
            href={`/pipeline-builder/${jobSpec.pipeline_id}`}
            class="text-blue-600 underline hover:text-blue-700"
          >
            {jobSpec.pipeline_name}
          </a>
        </dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-gray-400">Branch</dt>
        <dd class="mt-0.5 font-mono text-xs">{jobSpec.branch ?? '—'}</dd>
      </div>
      <div class="md:col-span-2">
        <dt class="text-xs uppercase tracking-wide text-gray-400">Source</dt>
        <dd class="mt-0.5">
          {#if jobSpec.repo_url}
            <a
              href={jobSpec.repo_url}
              class="text-blue-600 underline hover:text-blue-700"
              target="_blank"
              rel="noopener"
            >
              {jobSpec.repo_url}{jobSpec.repo_path ? ` :: ${jobSpec.repo_path}` : ''}
            </a>
          {:else}
            <span class="text-gray-500">No source repository linked</span>
          {/if}
        </dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-gray-400">Last run</dt>
        <dd class="mt-0.5 flex items-center gap-2">
          {#if jobSpec.last_run_at}
            <span>{new Date(jobSpec.last_run_at).toLocaleString()}</span>
          {:else}
            <span class="text-gray-500">Never</span>
          {/if}
          {#if jobSpec.last_run_status}
            <span class={`rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide ${statusTone(jobSpec.last_run_status)}`}>
              {jobSpec.last_run_status}
            </span>
          {/if}
        </dd>
      </div>
    </dl>
  {/if}
</section>
