<!--
  PropertyTypeEditor — picker that lets the operator choose a property
  value type (string / integer / media_reference / …). H6 introduces
  the `media_reference` option, which surfaces a media-set selector
  whenever it is the active type.

  Controlled component: parent owns the `propertyType` and
  `backingMediaSetRid` state; this component fires `onChange` /
  `onBackingChange` callbacks. Mirrors `BranchPicker` / `MarkingBadge`
  conventions so the kernel-validation contract (`media_reference`
  + `default_value.media_set_rid`) is satisfied verbatim.
-->
<script lang="ts">
  import type { MediaSet } from '$lib/api/mediaSets';

  type PropertyType =
    | 'string'
    | 'integer'
    | 'float'
    | 'boolean'
    | 'date'
    | 'timestamp'
    | 'json'
    | 'array'
    | 'vector'
    | 'reference'
    | 'geo_point'
    | 'media_reference'
    | 'struct'
    | 'attachment';

  type Props = {
    propertyType: PropertyType;
    onChange: (value: PropertyType) => void;
    /**
     * Backing media-set RID surfaced when `propertyType ===
     * "media_reference"`. The kernel validator
     * (`domain::media_reference_validator`) rejects edits whose
     * backing set does not exist, so the picker only offers known
     * sets here.
     */
    backingMediaSetRid?: string | null;
    onBackingChange?: (rid: string | null) => void;
    /**
     * Catalog of media sets the project has access to. Loaded by the
     * caller via `listMediaSets` so this component stays presentational.
     */
    mediaSetCatalog?: MediaSet[];
  };

  let {
    propertyType,
    onChange,
    backingMediaSetRid = null,
    onBackingChange = () => {},
    mediaSetCatalog = [],
  }: Props = $props();

  // Foundry-aligned discriminator strings — same set the kernel's
  // `VALID_TYPES` array enforces. Order pinned for stable UI review.
  const PROPERTY_TYPE_OPTIONS: ReadonlyArray<{
    value: PropertyType;
    label: string;
  }> = [
    { value: 'string', label: 'String' },
    { value: 'integer', label: 'Integer' },
    { value: 'float', label: 'Float' },
    { value: 'boolean', label: 'Boolean' },
    { value: 'date', label: 'Date' },
    { value: 'timestamp', label: 'Timestamp' },
    { value: 'json', label: 'JSON' },
    { value: 'array', label: 'Array' },
    { value: 'vector', label: 'Vector (embedding)' },
    { value: 'reference', label: 'Object reference' },
    { value: 'geo_point', label: 'Geo point' },
    { value: 'media_reference', label: 'Media reference' },
    { value: 'struct', label: 'Struct' },
    { value: 'attachment', label: 'Attachment' },
  ];

  function handleType(event: Event) {
    const next = (event.currentTarget as HTMLSelectElement).value as PropertyType;
    onChange(next);
    // Clear the backing pointer whenever the type leaves
    // `media_reference` so a stale RID never persists into the row.
    if (next !== 'media_reference') {
      onBackingChange(null);
    }
  }

  function handleBacking(event: Event) {
    const value = (event.currentTarget as HTMLSelectElement).value;
    onBackingChange(value === '' ? null : value);
  }
</script>

<div
  class="space-y-3"
  data-testid="property-type-editor"
  data-property-type={propertyType}
>
  <label class="block text-sm">
    <span class="mb-1 block font-medium text-slate-700 dark:text-slate-200">
      Property type
    </span>
    <select
      class="w-full rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm dark:border-gray-700 dark:bg-gray-900"
      value={propertyType}
      onchange={handleType}
      data-testid="property-type-editor-select"
    >
      {#each PROPERTY_TYPE_OPTIONS as option (option.value)}
        <option value={option.value}>{option.label}</option>
      {/each}
    </select>
  </label>

  {#if propertyType === 'media_reference'}
    <label class="block text-sm" data-testid="property-type-editor-media-backing">
      <span class="mb-1 block font-medium text-slate-700 dark:text-slate-200">
        Backing media set
      </span>
      <select
        class="w-full rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm dark:border-gray-700 dark:bg-gray-900"
        value={backingMediaSetRid ?? ''}
        onchange={handleBacking}
        data-testid="property-type-editor-media-backing-select"
      >
        <option value="">— select a media set —</option>
        {#each mediaSetCatalog as set (set.rid)}
          <option value={set.rid}>{set.name} ({set.schema})</option>
        {/each}
      </select>
      <p class="mt-1 text-xs text-slate-500">
        Foundry strongly discourages binding a media-reference property
        to more than one backing set; the action editor surfaces a
        warning if multiple appear.
      </p>
    </label>
  {/if}
</div>
