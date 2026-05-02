<!--
  TASK P — Reusable file picker that uploads through the ontology-actions
  upload endpoint and emits the resulting attachment_rid metadata back to
  the parent. Mirrors the simple file-picker UX described in
  `Upload attachments.md` and `Upload media.md`.

  Usage:
    <FileUpload
      label="Choose attachment"
      onUploaded={(attachment) => updateField(field.name, attachment.attachment_rid)}
    />
-->
<script lang="ts">
  type AttachmentMetadata = {
    attachment_rid: string;
    filename: string;
    content_type: string | null;
    size_bytes: number;
    storage_uri: string;
  };

  let {
    label = 'Choose file',
    accept,
    onUploaded
  }: {
    label?: string;
    accept?: string;
    onUploaded: (attachment: AttachmentMetadata) => void;
  } = $props();

  let busy = $state(false);
  let error = $state('');

  async function handleChange(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;

    busy = true;
    error = '';
    try {
      const buffer = await file.arrayBuffer();
      // Encode small payloads inline; production callers will swap this for
      // a presigned upload to object storage. We keep the inline path so
      // local development and tests work without external dependencies.
      const base64 = btoa(String.fromCharCode(...new Uint8Array(buffer)));
      const response = await fetch('/api/v1/ontology/actions/uploads', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
          filename: file.name,
          content_type: file.type || null,
          size_bytes: file.size,
          content_base64: base64
        })
      });
      if (!response.ok) {
        error = `${response.status}: ${await response.text()}`;
        return;
      }
      const attachment = (await response.json()) as AttachmentMetadata;
      onUploaded(attachment);
      input.value = '';
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }
</script>

<div class="space-y-1">
  <label class="inline-flex cursor-pointer items-center gap-2 rounded-xl border border-slate-300 px-3 py-2 text-sm text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-200 dark:hover:bg-slate-800">
    {label}
    <input type="file" class="hidden" {accept} disabled={busy} onchange={handleChange} />
  </label>
  {#if busy}
    <p class="text-[11px] text-slate-500">Uploading…</p>
  {/if}
  {#if error}
    <p class="text-[11px] text-rose-600 dark:text-rose-300">{error}</p>
  {/if}
</div>
