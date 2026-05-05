<script lang="ts">
  import { goto } from '$app/navigation';

  import CreatePipelineModal from '$components/pipeline/CreatePipelineModal.svelte';

  let open = $state(true);

  function handleClose() {
    open = false;
    void goto('/pipelines');
  }

  function handleCreated(pipelineId: string) {
    open = false;
    void goto(`/pipelines/${pipelineId}/edit`);
  }
</script>

<div class="of-page">
  <CreatePipelineModal {open} onClose={handleClose} onCreated={handleCreated} />
</div>
