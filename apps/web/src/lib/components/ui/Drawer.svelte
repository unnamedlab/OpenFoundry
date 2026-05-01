<script lang="ts">
  /**
   * `Drawer` — panel lateral genérico (right/left/bottom). Svelte 5 runes.
   *
   * Contrato:
   * - `open: boolean`                        — bindable.
   * - `title?: string`
   * - `side?: 'right' | 'left' | 'bottom'`   — default `right`.
   * - `width?: string`                       — CSS width when right/left.
   * - `onclose?: () => void`
   * - `children: Snippet`                    — contenido.
   */
  import type { Snippet } from 'svelte';

  type Props = {
    open: boolean;
    title?: string;
    side?: 'right' | 'left' | 'bottom';
    width?: string;
    onclose?: () => void;
    children: Snippet;
  };

  let {
    open = $bindable(),
    title = '',
    side = 'right',
    width = '480px',
    onclose,
    children,
  }: Props = $props();

  function close() {
    open = false;
    onclose?.();
  }

  function onkeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && open) close();
  }
</script>

<svelte:window on:keydown={onkeydown} />

{#if open}
  <div class="overlay" onclick={close} role="presentation"></div>
  <!-- svelte-ignore a11y_no_noninteractive_element_to_interactive_role -->
  <aside
    class="drawer"
    data-side={side}
    style:--w={width}
    role="dialog"
    aria-modal="true"
    aria-label={title}
  >
    <header>
      <strong>{title}</strong>
      <button type="button" class="close" aria-label="Close" onclick={close}>×</button>
    </header>
    <div class="body">
      {@render children()}
    </div>
  </aside>
{/if}

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(2, 6, 23, 0.55);
    z-index: 80;
  }
  .drawer {
    position: fixed;
    background: #0f172a;
    border: 1px solid #1e293b;
    box-shadow: -4px 0 16px rgba(0, 0, 0, 0.4);
    z-index: 90;
    display: flex;
    flex-direction: column;
    color: #e2e8f0;
  }
  .drawer[data-side='right'] { top: 0; right: 0; bottom: 0; width: var(--w); }
  .drawer[data-side='left']  { top: 0; left: 0;  bottom: 0; width: var(--w); }
  .drawer[data-side='bottom']{ left: 0; right: 0; bottom: 0; height: var(--w); }
  header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 0.75rem 1rem; border-bottom: 1px solid #1e293b;
  }
  .close {
    background: transparent; border: none; color: #94a3b8;
    font-size: 1.4rem; line-height: 1; cursor: pointer; padding: 0 0.25rem;
  }
  .body { flex: 1; overflow: auto; padding: 1rem; }
</style>
