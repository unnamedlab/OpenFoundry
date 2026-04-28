<svelte:head>
  <title>OpenFoundry — Search</title>
</svelte:head>

<div class="of-content-grid">
  <section class="of-panel p-6">
    <div class="of-eyebrow">Workspace</div>
    <h1 class="of-heading-lg mt-2">Search</h1>
    <p class="mt-2 max-w-3xl text-sm text-[var(--text-muted)]">
      Use the dedicated search entry in the shell to jump into discovery workflows, object
      exploration, and query-driven analysis.
    </p>
  </section>

  <section class="of-card-grid md:grid-cols-2 xl:grid-cols-3">
    <a href="/object-explorer" class="of-card">
      <div class="of-kicker">Explore</div>
      <div class="of-heading-sm">Object Explorer</div>
      <p class="m-0 text-sm text-[var(--text-muted)]">
        Search ontology objects, pivot across relationships, and reopen saved explorations.
      </p>
    </a>

    <a href="/queries" class="of-card">
      <div class="of-kicker">Analyze</div>
      <div class="of-heading-sm">Queries</div>
      <p class="m-0 text-sm text-[var(--text-muted)]">
        Run SQL-style searches and inspect structured results.
      </p>
    </a>

    <a href="/ontology" class="of-card">
      <div class="of-kicker">Schema</div>
      <div class="of-heading-sm">Ontology</div>
      <p class="m-0 text-sm text-[var(--text-muted)]">
        Inspect types, links, and schema details that shape search behavior.
      </p>
    </a>
  </section>
</div>
