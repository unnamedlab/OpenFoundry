<svelte:head>
  <title>OpenFoundry — Projects & files</title>
</svelte:head>

<div class="of-content-grid">
  <section class="of-panel p-6">
    <div class="of-eyebrow">Applications</div>
    <h1 class="of-heading-lg mt-2">Projects & files</h1>
    <p class="mt-2 max-w-3xl text-sm text-[var(--text-muted)]">
      Start from the same workspace entry point highlighted in the reference layout, with quick
      access to active projects, recent files, and operational surfaces.
    </p>
  </section>

  <section class="of-card-grid md:grid-cols-2 xl:grid-cols-3">
    <a href="/" class="of-card">
      <div class="of-kicker">Workspace</div>
      <div class="of-heading-sm">Home</div>
      <p class="m-0 text-sm text-[var(--text-muted)]">
        Return to the main OpenFoundry workspace overview.
      </p>
    </a>

    <a href="/reports" class="of-card">
      <div class="of-kicker">Files</div>
      <div class="of-heading-sm">Browse artifacts</div>
      <p class="m-0 text-sm text-[var(--text-muted)]">
        Review generated files, reports, and published workspace assets.
      </p>
    </a>

    <a href="/dashboards" class="of-card">
      <div class="of-kicker">Recent</div>
      <div class="of-heading-sm">Reopen recent work</div>
      <p class="m-0 text-sm text-[var(--text-muted)]">
        Jump back into recent dashboards, views, and saved workstreams.
      </p>
    </a>
  </section>
</div>
