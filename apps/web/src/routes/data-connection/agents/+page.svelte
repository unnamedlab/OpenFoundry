<script lang="ts">
  // Agent worker is in the legacy phase per the Foundry docs; the MVP exposes
  // the screen but explicitly does not allow registering agents. This avoids
  // promising functionality we don't ship while keeping the navigation
  // structure consistent with the docs.
</script>

<svelte:head>
  <title>Agents · Data Connection</title>
</svelte:head>

<div class="space-y-6">
  <div>
    <a href="/data-connection" class="text-xs text-blue-600 hover:underline dark:text-blue-400"
      >← Back to sources</a
    >
    <h1 class="mt-1 text-2xl font-bold">Data Connection agents</h1>
    <p class="mt-1 max-w-2xl text-sm text-gray-500">
      An agent is a downloadable program installed within your organisational network. Agents power
      connections leveraging agent proxy policies and agent worker connections.
    </p>
  </div>

  <div
    class="rounded-2xl border border-amber-300 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-700 dark:bg-amber-950/30 dark:text-amber-200"
  >
    <p class="font-medium">Agent worker is in the legacy phase</p>
    <p class="mt-1 text-xs">
      For new sources, prefer Foundry worker — it benefits from containerised, scalable job
      execution with no agent maintenance overhead. Agent worker support is not included in the
      Data Connection MVP and the download below is intentionally disabled.
    </p>
  </div>

  <div class="overflow-hidden rounded-2xl border border-gray-200 dark:border-gray-800">
    <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-gray-800">
      <thead class="bg-gray-50 dark:bg-gray-900">
        <tr>
          <th class="px-4 py-2 text-left font-medium text-gray-600 dark:text-gray-300">Agent</th>
          <th class="px-4 py-2 text-left font-medium text-gray-600 dark:text-gray-300">Host</th>
          <th class="px-4 py-2 text-left font-medium text-gray-600 dark:text-gray-300">Status</th>
          <th class="px-4 py-2 text-left font-medium text-gray-600 dark:text-gray-300"
            >Last heartbeat</th
          >
        </tr>
      </thead>
      <tbody>
        <tr>
          <td class="px-4 py-6 text-center text-xs text-gray-500" colspan="4">
            No agents registered.
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <button
    type="button"
    disabled
    class="cursor-not-allowed rounded-xl bg-gray-300 px-4 py-2 text-sm font-medium text-gray-600 dark:bg-gray-800 dark:text-gray-500"
    title="Agent worker is out of MVP scope"
  >
    Download agent (unavailable)
  </button>
</div>
