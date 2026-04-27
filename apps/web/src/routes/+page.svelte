<script lang="ts">
  import { auth } from '$stores/auth';

  const isAuthenticated = auth.isAuthenticated;
</script>

<svelte:head>
  <title>OpenFoundry — Home</title>
</svelte:head>

<div class="max-w-5xl mx-auto">
  <h1 class="text-2xl font-bold mb-6">Welcome to OpenFoundry</h1>

  {#if $isAuthenticated}
    <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
      <a href="/apps" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🧩</div>
        <h2 class="font-semibold">Apps</h2>
        <p class="text-sm text-gray-500 mt-1">Build internal tools with widgets, data bindings, and published runtime views</p>
      </a>
      <a href="/ml" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🧠</div>
        <h2 class="font-semibold">ML Studio</h2>
        <p class="text-sm text-gray-500 mt-1">Track experiments, register models, serve predictions, and monitor drift</p>
      </a>
      <a href="/dashboards" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">📊</div>
        <h2 class="font-semibold">Dashboards</h2>
        <p class="text-sm text-gray-500 mt-1">Compose KPI cards, charts, and tables on a shareable grid</p>
      </a>
      <a href="/contour" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🧿</div>
        <h2 class="font-semibold">Contour</h2>
        <p class="text-sm text-gray-500 mt-1">Top-down analysis boards with transforms, paths, and dataset export</p>
      </a>
      <a href="/quiver" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">📈</div>
        <h2 class="font-semibold">Quiver</h2>
        <p class="text-sm text-gray-500 mt-1">Time-series and ontology analytics with reusable visual functions</p>
      </a>
      <a href="/control-panel" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🛠️</div>
        <h2 class="font-semibold">Control Panel</h2>
        <p class="text-sm text-gray-500 mt-1">Brand the platform, manage rollout posture, and govern enterprise operations</p>
      </a>
      <a href="/datasets" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🗄️</div>
        <h2 class="font-semibold">Datasets</h2>
        <p class="text-sm text-gray-500 mt-1">Connect, explore, and manage your data</p>
      </a>
      <a href="/queries" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🔍</div>
        <h2 class="font-semibold">Queries</h2>
        <p class="text-sm text-gray-500 mt-1">Write SQL and explore results</p>
      </a>
      <a href="/pipelines" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">⚙️</div>
        <h2 class="font-semibold">Pipelines</h2>
        <p class="text-sm text-gray-500 mt-1">Build and schedule data transformations</p>
      </a>
      <a href="/ontology" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🔗</div>
        <h2 class="font-semibold">Ontology</h2>
        <p class="text-sm text-gray-500 mt-1">Define object types and relationships</p>
      </a>
      <a href="/ontology-manager" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🗂️</div>
        <h2 class="font-semibold">Ontology Manager</h2>
        <p class="text-sm text-gray-500 mt-1">Review ontology working state, manage schema resources, import and export JSON, and scope permissions by project</p>
      </a>
      <a href="/ontologies" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🧭</div>
        <h2 class="font-semibold">Ontologies</h2>
        <p class="text-sm text-gray-500 mt-1">Operate ontology spaces with branch lifecycle, proposals, preview checks, migration flows, and shared collaboration posture</p>
      </a>
      <a href="/ontology-design" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🧪</div>
        <h2 class="font-semibold">Ontology Design Review</h2>
        <p class="text-sm text-gray-500 mt-1">Score ontology quality, detect anti-patterns, and turn best-practice guidance into concrete remediation work</p>
      </a>
      <a href="/ontology-indexing" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🗃️</div>
        <h2 class="font-semibold">Ontology Indexing</h2>
        <p class="text-sm text-gray-500 mt-1">Manage Funnel sources, health, runs, batch and streaming posture, replacement pipelines, and indexing restrictions</p>
      </a>
      <a href="/object-databases" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🛢️</div>
        <h2 class="font-semibold">Object Databases</h2>
        <p class="text-sm text-gray-500 mt-1">Inspect PostgreSQL-backed object rows, graph edges, search projections, Funnel hydration, and the indexed access paths behind ontology runtime</p>
      </a>
      <a href="/object-link-types" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🧱</div>
        <h2 class="font-semibold">Object and Link Types</h2>
        <p class="text-sm text-gray-500 mt-1">Model object types, link types, shared properties, value contracts, structs, metadata, and derived fields from one dedicated ontology studio</p>
      </a>
      <a href="/action-types" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🎛️</div>
        <h2 class="font-semibold">Action Types</h2>
        <p class="text-sm text-gray-500 mt-1">Author governed actions with parameter sections, side effects, inline edits, batch runs, and action-log monitoring</p>
      </a>
      <a href="/functions" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🧪</div>
        <h2 class="font-semibold">Functions</h2>
        <p class="text-sm text-gray-500 mt-1">Author reusable TypeScript and Python packages with testing, release workflows, SDK scaffolds, and function-run monitoring</p>
      </a>
      <a href="/interfaces" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🧬</div>
        <h2 class="font-semibold">Interfaces</h2>
        <p class="text-sm text-gray-500 mt-1">Manage reusable ontology contracts, canonical interface properties, and implementation bindings across object types</p>
      </a>
      <a href="/object-explorer" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🧭</div>
        <h2 class="font-semibold">Object Explorer</h2>
        <p class="text-sm text-gray-500 mt-1">Search ontology objects, pivot across links, compare lists, and reopen saved explorations</p>
      </a>
      <a href="/object-monitors" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🔔</div>
        <h2 class="font-semibold">Object Monitors</h2>
        <p class="text-sm text-gray-500 mt-1">Watch saved explorations, alert subscribers, and trigger remediation actions when ontology conditions change</p>
      </a>
      <a href="/object-views" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🪟</div>
        <h2 class="font-semibold">Object Views</h2>
        <p class="text-sm text-gray-500 mt-1">Configure standard and curated object views, manage versions, and preview full or panel form factors</p>
      </a>
      <a href="/dynamic-scheduling" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🗓️</div>
        <h2 class="font-semibold">Dynamic Scheduling</h2>
        <p class="text-sm text-gray-500 mt-1">Run a dedicated scheduling board with queue pressure, validation rules, and staged moves</p>
      </a>
      <a href="/foundry-rules" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">⚖️</div>
        <h2 class="font-semibold">Foundry Rules</h2>
        <p class="text-sm text-gray-500 mt-1">Author low-code rules, run workflow deployments, operate schedules, and generate custom rule pipelines</p>
      </a>
      <a href="/machinery" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🏭</div>
        <h2 class="font-semibold">Machinery</h2>
        <p class="text-sm text-gray-500 mt-1">Model processes, mine operational behavior, and monitor live interventions across workflows and queues</p>
      </a>
      <a href="/vertex" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">🕸️</div>
        <h2 class="font-semibold">Vertex</h2>
        <p class="text-sm text-gray-500 mt-1">Operate a dedicated graph product with reusable templates, event timelines, media overlays, and what-if digital twin scenarios</p>
      </a>
      <a href="/notebooks" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">📓</div>
        <h2 class="font-semibold">Notebooks</h2>
        <p class="text-sm text-gray-500 mt-1">Interactive Python and SQL notebooks</p>
      </a>
      <a href="/notepad" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">📝</div>
        <h2 class="font-semibold">Notepad</h2>
        <p class="text-sm text-gray-500 mt-1">Collaborative documents with embeds, exports, and AIP indexing</p>
      </a>
      <a href="/settings" class="block p-6 bg-white dark:bg-gray-900 rounded-xl border border-gray-200 dark:border-gray-800 hover:border-indigo-300 transition-colors">
        <div class="text-2xl mb-2">⚙️</div>
        <h2 class="font-semibold">Settings</h2>
        <p class="text-sm text-gray-500 mt-1">Users, roles, and platform configuration</p>
      </a>
    </div>
  {:else}
    <div class="text-center py-20">
      <p class="text-gray-500 mb-4">Sign in to access your data platform.</p>
      <a href="/auth/login" class="inline-block px-6 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-500 transition-colors">
        Sign In
      </a>
    </div>
  {/if}
</div>
