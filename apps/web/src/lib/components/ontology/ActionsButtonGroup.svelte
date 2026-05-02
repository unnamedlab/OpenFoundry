<!--
  TASK N — ActionsButtonGroup

  Configurable button-group widget that surfaces ontology actions in
  Object Explorer / Object Views / Workshop apps. Each button declares the
  action it triggers, optional default values to seed inputs, parameters to
  hide from the user, and whether the action is invoked immediately
  (skipping the modal form) when no required inputs remain.

  See `Use actions in the platform.md`.

  Usage:
    <ActionsButtonGroup
      typeId={objectType.id}
      objectId={selection.objectId}
      buttons={[
        { action_id: '…', label: 'Approve', color: 'emerald', immediate: true },
        { action_id: '…', label: 'Edit', color: 'sky', hidden_params: ['actor_id'] }
      ]}
      onExecuted={refresh}
    />
-->
<script lang="ts">
  import { executeAction, getActionType, type ActionType } from '$lib/api/ontology';
  import ActionExecutor from './ActionExecutor.svelte';

  export type ActionButtonConfig = {
    action_id: string;
    label?: string;
    /** Tailwind colour keyword: emerald | sky | rose | amber | slate */
    color?: 'emerald' | 'sky' | 'rose' | 'amber' | 'slate';
    /** Pre-filled parameter values when opening the executor. */
    default_values?: Record<string, unknown>;
    /** Names of parameters that should not be shown to the user. */
    hidden_params?: string[];
    /** Skip the modal entirely when the action has no remaining required input. */
    immediate?: boolean;
  };

  let {
    typeId,
    objectId,
    buttons,
    onExecuted
  }: {
    typeId: string;
    objectId?: string;
    buttons: ActionButtonConfig[];
    onExecuted?: () => void;
  } = $props();

  let openButton = $state<ActionButtonConfig | null>(null);
  let openAction = $state<ActionType | null>(null);
  let busy = $state('');
  let error = $state('');

  const colorClasses: Record<NonNullable<ActionButtonConfig['color']>, string> = {
    emerald: 'bg-emerald-600 hover:bg-emerald-500 text-white',
    sky: 'bg-sky-600 hover:bg-sky-500 text-white',
    rose: 'bg-rose-600 hover:bg-rose-500 text-white',
    amber: 'bg-amber-500 hover:bg-amber-400 text-slate-900',
    slate: 'bg-slate-200 hover:bg-slate-300 text-slate-900 dark:bg-slate-700 dark:text-slate-100'
  };

  function classFor(button: ActionButtonConfig): string {
    return colorClasses[button.color ?? 'sky'];
  }

  async function activate(button: ActionButtonConfig) {
    error = '';
    busy = button.action_id;
    try {
      const action = await getActionType(button.action_id);
      const visibleRequired = action.input_schema.filter(
        (field) =>
          field.required &&
          !(button.hidden_params ?? []).includes(field.name) &&
          (button.default_values?.[field.name] ?? field.default_value) === undefined
      );

      if (button.immediate && visibleRequired.length === 0) {
        await executeAction(button.action_id, {
          target_object_id: objectId,
          parameters: button.default_values ?? {}
        });
        onExecuted?.();
        return;
      }
      openAction = action;
      openButton = button;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = '';
    }
  }

  function close() {
    openAction = null;
    openButton = null;
  }

  function executed() {
    close();
    onExecuted?.();
  }
</script>

<div class="flex flex-wrap items-center gap-2">
  {#each buttons as button (button.action_id + (button.label ?? ''))}
    <button
      type="button"
      onclick={() => activate(button)}
      disabled={busy === button.action_id}
      class={`inline-flex items-center gap-2 rounded-xl px-3 py-1.5 text-sm font-semibold shadow-sm transition disabled:opacity-50 ${classFor(button)}`}
    >
      {button.label ?? 'Run action'}
    </button>
  {/each}
</div>

{#if error}
  <p class="mt-2 text-xs text-rose-600 dark:text-rose-300">{error}</p>
{/if}

{#if openAction && openButton}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4">
    <div class="max-h-[90vh] w-full max-w-3xl overflow-y-auto rounded-2xl bg-white p-6 shadow-xl dark:bg-slate-900">
      <div class="mb-4 flex items-center justify-between">
        <h2 class="text-lg font-semibold text-slate-900 dark:text-slate-100">
          {openButton.label ?? openAction.display_name ?? openAction.name}
        </h2>
        <button
          type="button"
          onclick={close}
          class="rounded-full bg-slate-100 px-3 py-1 text-sm text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-200"
        >
          Close
        </button>
      </div>
      <ActionExecutor
        action={openAction}
        parameters={{ ...(openButton.default_values ?? {}) }}
        on:executed={executed}
      />
      {#if (openButton.hidden_params ?? []).length > 0}
        <p class="mt-3 text-[11px] text-slate-500 dark:text-slate-400">
          Hidden parameters: {(openButton.hidden_params ?? []).join(', ')} are pre-filled by this
          button and not displayed in the form.
        </p>
      {/if}
    </div>
  </div>
{/if}
