<script lang="ts">
  import { browser } from '$app/environment';
  import { onMount } from 'svelte';
  import Glyph from '$components/ui/Glyph.svelte';
  import {
    attachSharedPropertyType,
    createLinkType,
    createObjectType,
    createProperty,
    createSharedPropertyType,
    deleteProperty,
    deleteLinkType,
    deleteObjectType,
    deleteSharedPropertyType,
    detachSharedPropertyType,
    listLinkTypes,
    listObjectTypes,
    listProperties,
    listSharedPropertyTypes,
    listTypeSharedPropertyTypes,
    updateLinkType,
    updateObjectType,
    updateProperty,
    updateSharedPropertyType,
    type LinkType,
    type ObjectType,
    type Property,
    type SharedPropertyType
  } from '$lib/api/ontology';

  type StudioTab =
    | 'object-types'
    | 'properties'
    | 'shared-properties'
    | 'link-types'
    | 'value-types'
    | 'structs'
    | 'metadata'
    | 'derived'
    | 'groups'
    | 'marketplace';

  interface ValueTypeDraft {
    id: string;
    name: string;
    display_name: string;
    description: string;
    base_type: string;
    constraints_json: string;
    permissions_json: string;
    version_label: string;
  }

  interface StructFieldDraft {
    id: string;
    name: string;
    display_name: string;
    property_type: string;
    required: boolean;
  }

  interface StructTypeDraft {
    id: string;
    name: string;
    display_name: string;
    description: string;
    automapping_mode: 'manual' | 'dataset_like' | 'object_like';
    main_field_ids: string[];
    fields: StructFieldDraft[];
    shared_property_ids: string[];
  }

  interface TypeMetadataDraft {
    render_hints_json: string;
    statuses_json: string;
    type_classes_json: string;
  }

  interface DerivedPropertyDraft {
    id: string;
    name: string;
    display_name: string;
    description: string;
    result_type: string;
    source_expression: string;
    reducer: string;
    formatting_json: string;
  }

  interface ObjectTypeGroupDraft {
    id: string;
    name: string;
    description: string;
    object_type_ids: string[];
  }

  interface MarketplaceDraft {
    package_name: string;
    listing_title: string;
    summary: string;
    included_object_type_ids: string[];
    included_link_type_ids: string[];
    included_shared_property_type_ids: string[];
    included_value_type_ids: string[];
  }

  const tabs: { id: StudioTab; label: string; glyph: 'cube' | 'database' | 'link' | 'code' | 'object' | 'settings' | 'sparkles' }[] = [
    { id: 'object-types', label: 'Object types', glyph: 'cube' },
    { id: 'properties', label: 'Properties', glyph: 'database' },
    { id: 'shared-properties', label: 'Shared properties', glyph: 'database' },
    { id: 'link-types', label: 'Link types', glyph: 'link' },
    { id: 'value-types', label: 'Value types', glyph: 'code' },
    { id: 'structs', label: 'Structs', glyph: 'object' },
    { id: 'metadata', label: 'Metadata', glyph: 'settings' },
    { id: 'derived', label: 'Derived', glyph: 'sparkles' },
    { id: 'groups', label: 'Groups', glyph: 'cube' },
    { id: 'marketplace', label: 'Marketplace', glyph: 'sparkles' }
  ];

  const basePropertyTypes = [
    'string',
    'integer',
    'float',
    'boolean',
    'date',
    'timestamp',
    'json',
    'array',
    'vector',
    'reference',
    'geo_point',
    'media_reference'
  ];

  const cardinalityOptions = ['one_to_one', 'one_to_many', 'many_to_one', 'many_to_many'];

  let objectTypes = $state<ObjectType[]>([]);
  let properties = $state<Property[]>([]);
  let sharedPropertyTypes = $state<SharedPropertyType[]>([]);
  let attachedSharedPropertyTypes = $state<SharedPropertyType[]>([]);
  let linkTypes = $state<LinkType[]>([]);

  let valueTypes = $state<ValueTypeDraft[]>([]);
  let structTypes = $state<StructTypeDraft[]>([]);
  let typeMetadata = $state<Record<string, TypeMetadataDraft>>({});
  let derivedProperties = $state<Record<string, DerivedPropertyDraft[]>>({});
  let objectTypeGroups = $state<ObjectTypeGroupDraft[]>([]);
  let marketplaceDraft = $state<MarketplaceDraft>({
    package_name: 'ontology-core',
    listing_title: 'Ontology Core Types',
    summary: 'Reusable ontology types, links, value contracts, and structs.',
    included_object_type_ids: [],
    included_link_type_ids: [],
    included_shared_property_type_ids: [],
    included_value_type_ids: []
  });

  let loading = $state(true);
  let typeContextLoading = $state(false);
  let saveBusy = $state(false);
  let saveError = $state('');
  let saveSuccess = $state('');
  let activeTab = $state<StudioTab>('object-types');
  let selectedObjectTypeId = $state('');
  let selectedSharedPropertyTypeId = $state('');
  let selectedLinkTypeId = $state('');

  let objectTypeName = $state('');
  let objectTypeDisplayName = $state('');
  let objectTypeDescription = $state('');
  let objectTypePrimaryKeyProperty = $state('');
  let objectTypeIcon = $state('');
  let objectTypeColor = $state('#2458b8');

  let propertyName = $state('');
  let propertyDisplayName = $state('');
  let propertyDescription = $state('');
  let propertyType = $state('string');
  let propertyRequired = $state(false);
  let propertyUnique = $state(false);
  let propertyTimeDependent = $state(false);
  let propertyDefaultValueText = $state('null');
  let propertyValidationRulesText = $state('{}');

  let sharedPropertyName = $state('');
  let sharedPropertyDisplayName = $state('');
  let sharedPropertyDescription = $state('');
  let sharedPropertyBaseType = $state('string');
  let sharedPropertyRequired = $state(false);
  let sharedPropertyUnique = $state(false);
  let sharedPropertyTimeDependent = $state(false);
  let sharedPropertyDefaultValueText = $state('null');
  let sharedPropertyValidationRulesText = $state('{}');

  let linkName = $state('');
  let linkDisplayName = $state('');
  let linkDescription = $state('');
  let linkSourceTypeId = $state('');
  let linkTargetTypeId = $state('');
  let linkCardinality = $state('many_to_many');

  const selectedObjectType = $derived(objectTypes.find((item) => item.id === selectedObjectTypeId) ?? null);
  const selectedSharedPropertyType = $derived(sharedPropertyTypes.find((item) => item.id === selectedSharedPropertyTypeId) ?? null);
  const selectedLinkType = $derived(linkTypes.find((item) => item.id === selectedLinkTypeId) ?? null);
  const attachedSharedIds = $derived(attachedSharedPropertyTypes.map((item) => item.id));
  const metadataDraft = $derived(
    selectedObjectTypeId
      ? (typeMetadata[selectedObjectTypeId] ?? {
          render_hints_json: prettyJson({
            object_tile: { primary: 'name', secondary: 'status', accent_color: 'color' },
            table: { frozen: ['name'], visible: ['name', 'status', 'priority'] }
          }),
          statuses_json: prettyJson([
            { id: 'active', label: 'Active', color: '#2f6d35', icon: 'play' },
            { id: 'pending', label: 'Pending', color: '#b7791f', icon: 'clock' }
          ]),
          type_classes_json: prettyJson(['operational', 'tracked', 'editable'])
        })
      : null
  );
  const derivedDrafts = $derived(selectedObjectTypeId ? (derivedProperties[selectedObjectTypeId] ?? []) : []);

  function storageKey(name: string) {
    return `of.objectLinkTypes.${name}`;
  }

  function loadStored<T>(name: string, fallback: T): T {
    if (!browser) return fallback;
    try {
      const raw = window.localStorage.getItem(storageKey(name));
      return raw ? (JSON.parse(raw) as T) : fallback;
    } catch {
      return fallback;
    }
  }

  function persistStored(name: string, value: unknown) {
    if (!browser) return;
    window.localStorage.setItem(storageKey(name), JSON.stringify(value));
  }

  function prettyJson(value: unknown) {
    return JSON.stringify(value ?? null, null, 2);
  }

  function parseJson(source: string, label: string): unknown {
    try {
      return JSON.parse(source);
    } catch (error) {
      throw new Error(`${label}: ${error instanceof Error ? error.message : 'Invalid JSON'}`);
    }
  }

  function resetObjectTypeDraft() {
    objectTypeName = '';
    objectTypeDisplayName = '';
    objectTypeDescription = '';
    objectTypePrimaryKeyProperty = '';
    objectTypeIcon = '';
    objectTypeColor = '#2458b8';
  }

  function syncObjectTypeDraft(objectType: ObjectType | null) {
    if (!objectType) {
      resetObjectTypeDraft();
      return;
    }
    objectTypeName = objectType.name;
    objectTypeDisplayName = objectType.display_name;
    objectTypeDescription = objectType.description;
    objectTypePrimaryKeyProperty = objectType.primary_key_property ?? '';
    objectTypeIcon = objectType.icon ?? '';
    objectTypeColor = objectType.color ?? '#2458b8';
  }

  function resetPropertyDraft() {
    propertyName = '';
    propertyDisplayName = '';
    propertyDescription = '';
    propertyType = 'string';
    propertyRequired = false;
    propertyUnique = false;
    propertyTimeDependent = false;
    propertyDefaultValueText = 'null';
    propertyValidationRulesText = '{}';
  }

  function resetSharedPropertyDraft() {
    sharedPropertyName = '';
    sharedPropertyDisplayName = '';
    sharedPropertyDescription = '';
    sharedPropertyBaseType = 'string';
    sharedPropertyRequired = false;
    sharedPropertyUnique = false;
    sharedPropertyTimeDependent = false;
    sharedPropertyDefaultValueText = 'null';
    sharedPropertyValidationRulesText = '{}';
  }

  function resetLinkDraft() {
    linkName = '';
    linkDisplayName = '';
    linkDescription = '';
    linkSourceTypeId = selectedObjectTypeId || objectTypes[0]?.id || '';
    linkTargetTypeId = objectTypes[1]?.id || objectTypes[0]?.id || '';
    linkCardinality = 'many_to_many';
    selectedLinkTypeId = '';
  }

  async function loadTypeContext(typeId: string) {
    if (!typeId) {
      properties = [];
      attachedSharedPropertyTypes = [];
      return;
    }
    typeContextLoading = true;
    try {
      const [nextProperties, nextAttachedShared] = await Promise.all([
        listProperties(typeId),
        listTypeSharedPropertyTypes(typeId)
      ]);
      properties = nextProperties;
      attachedSharedPropertyTypes = nextAttachedShared;
      syncObjectTypeDraft(objectTypes.find((item) => item.id === typeId) ?? null);
      if (!typeMetadata[typeId]) {
        typeMetadata = {
          ...typeMetadata,
          [typeId]: {
            render_hints_json: prettyJson({
              object_tile: { primary: 'name', secondary: 'status', accent_color: 'color' },
              table: { frozen: ['name'], visible: ['name', 'status', 'priority'] }
            }),
            statuses_json: prettyJson([
              { id: 'active', label: 'Active', color: '#2f6d35', icon: 'play' },
              { id: 'pending', label: 'Pending', color: '#b7791f', icon: 'clock' }
            ]),
            type_classes_json: prettyJson(['operational', 'tracked', 'editable'])
          }
        };
        persistStored('typeMetadata', typeMetadata);
      }
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to load type context';
    } finally {
      typeContextLoading = false;
    }
  }

  async function loadPage() {
    loading = true;
    saveError = '';
    try {
      const [typeResponse, sharedResponse, linkResponse] = await Promise.all([
        listObjectTypes({ per_page: 200 }),
        listSharedPropertyTypes({ per_page: 200 }),
        listLinkTypes({ per_page: 200 })
      ]);
      objectTypes = typeResponse.data;
      sharedPropertyTypes = sharedResponse.data;
      linkTypes = linkResponse.data;

      valueTypes = loadStored<ValueTypeDraft[]>('valueTypes', []);
      structTypes = loadStored<StructTypeDraft[]>('structTypes', []);
      typeMetadata = loadStored<Record<string, TypeMetadataDraft>>('typeMetadata', {});
      derivedProperties = loadStored<Record<string, DerivedPropertyDraft[]>>('derivedProperties', {});
      objectTypeGroups = loadStored<ObjectTypeGroupDraft[]>('objectTypeGroups', []);
      marketplaceDraft = loadStored<MarketplaceDraft>('marketplaceDraft', marketplaceDraft);

      selectedObjectTypeId = selectedObjectTypeId || objectTypes[0]?.id || '';
      selectedSharedPropertyTypeId = selectedSharedPropertyTypeId || sharedPropertyTypes[0]?.id || '';
      selectedLinkTypeId = selectedLinkTypeId || linkTypes[0]?.id || '';
      await loadTypeContext(selectedObjectTypeId);
      resetLinkDraft();
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to load object and link types';
    } finally {
      loading = false;
    }
  }

  async function saveObjectType() {
    saveBusy = true;
    saveError = '';
    saveSuccess = '';
    try {
      if (selectedObjectTypeId) {
        await updateObjectType(selectedObjectTypeId, {
          display_name: objectTypeDisplayName.trim() || undefined,
          description: objectTypeDescription.trim() || undefined,
          primary_key_property: objectTypePrimaryKeyProperty.trim() || undefined,
          icon: objectTypeIcon.trim() || undefined,
          color: objectTypeColor.trim() || undefined
        });
        saveSuccess = 'Object type updated.';
      } else {
        if (!objectTypeName.trim()) throw new Error('Object type name is required.');
        await createObjectType({
          name: objectTypeName.trim(),
          display_name: objectTypeDisplayName.trim() || undefined,
          description: objectTypeDescription.trim() || undefined,
          primary_key_property: objectTypePrimaryKeyProperty.trim() || undefined,
          icon: objectTypeIcon.trim() || undefined,
          color: objectTypeColor.trim() || undefined
        });
        saveSuccess = 'Object type created.';
      }
      const response = await listObjectTypes({ per_page: 200 });
      objectTypes = response.data;
      const matched =
        objectTypes.find((item) => item.name === objectTypeName.trim()) ??
        objectTypes.find((item) => item.id === selectedObjectTypeId) ??
        objectTypes[0] ??
        null;
      selectedObjectTypeId = matched?.id ?? '';
      await loadTypeContext(selectedObjectTypeId);
      resetLinkDraft();
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to save object type';
    } finally {
      saveBusy = false;
    }
  }

  async function removeObjectType() {
    if (!selectedObjectTypeId) return;
    if (typeof window !== 'undefined' && !window.confirm('Delete this object type?')) return;
    saveBusy = true;
    saveError = '';
    saveSuccess = '';
    try {
      await deleteObjectType(selectedObjectTypeId);
      const removedId = selectedObjectTypeId;
      const response = await listObjectTypes({ per_page: 200 });
      objectTypes = response.data;
      selectedObjectTypeId = objectTypes[0]?.id ?? '';
      delete typeMetadata[removedId];
      delete derivedProperties[removedId];
      persistStored('typeMetadata', typeMetadata);
      persistStored('derivedProperties', derivedProperties);
      await loadTypeContext(selectedObjectTypeId);
      saveSuccess = 'Object type deleted.';
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to delete object type';
    } finally {
      saveBusy = false;
    }
  }

  async function copyObjectTypeConfiguration() {
    if (!selectedObjectType) return;
    saveBusy = true;
    saveError = '';
    saveSuccess = '';
    try {
      const cloneName = `${selectedObjectType.name}_copy_${Date.now().toString().slice(-4)}`;
      const created = await createObjectType({
        name: cloneName,
        display_name: `${selectedObjectType.display_name} Copy`,
        description: selectedObjectType.description,
        icon: selectedObjectType.icon ?? undefined,
        color: selectedObjectType.color ?? undefined
      });
      for (const property of properties) {
        await createProperty(created.id, {
          name: property.name,
          display_name: property.display_name,
          description: property.description,
          property_type: property.property_type,
          required: property.required,
          unique_constraint: property.unique_constraint,
          time_dependent: property.time_dependent,
          default_value: property.default_value,
          validation_rules: property.validation_rules
        });
      }
      for (const sharedProperty of attachedSharedPropertyTypes) {
        await attachSharedPropertyType(created.id, sharedProperty.id);
      }
      typeMetadata = {
        ...typeMetadata,
        [created.id]: typeMetadata[selectedObjectType.id] ?? {
          render_hints_json: '{}',
          statuses_json: '[]',
          type_classes_json: '[]'
        }
      };
      derivedProperties = {
        ...derivedProperties,
        [created.id]: (derivedProperties[selectedObjectType.id] ?? []).map((item) => ({
          ...item,
          id: crypto.randomUUID()
        }))
      };
      persistStored('typeMetadata', typeMetadata);
      persistStored('derivedProperties', derivedProperties);
      const response = await listObjectTypes({ per_page: 200 });
      objectTypes = response.data;
      selectedObjectTypeId = created.id;
      await loadTypeContext(created.id);
      saveSuccess = 'Object type configuration copied.';
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to copy object type configuration';
    } finally {
      saveBusy = false;
    }
  }

  async function createPropertyRecord() {
    if (!selectedObjectTypeId) return;
    saveBusy = true;
    saveError = '';
    saveSuccess = '';
    try {
      await createProperty(selectedObjectTypeId, {
        name: propertyName.trim(),
        display_name: propertyDisplayName.trim() || undefined,
        description: propertyDescription.trim() || undefined,
        property_type: propertyType,
        required: propertyRequired,
        unique_constraint: propertyUnique,
        time_dependent: propertyTimeDependent,
        default_value: parseJson(propertyDefaultValueText, 'Default value JSON'),
        validation_rules: parseJson(propertyValidationRulesText, 'Validation rules JSON')
      });
      resetPropertyDraft();
      await loadTypeContext(selectedObjectTypeId);
      saveSuccess = 'Property created.';
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to create property';
    } finally {
      saveBusy = false;
    }
  }

  async function togglePropertyFlag(property: Property, flag: 'required' | 'time_dependent') {
    if (!selectedObjectTypeId) return;
    try {
      await updateProperty(selectedObjectTypeId, property.id, {
        [flag]: !property[flag]
      });
      await loadTypeContext(selectedObjectTypeId);
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to update property';
    }
  }

  async function removePropertyRecord(property: Property) {
    if (!selectedObjectTypeId) return;
    if (typeof window !== 'undefined' && !window.confirm(`Delete property "${property.display_name}"?`)) return;
    try {
      await deleteProperty(selectedObjectTypeId, property.id);
      await loadTypeContext(selectedObjectTypeId);
      saveSuccess = 'Property deleted.';
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to delete property';
    }
  }

  async function createSharedPropertyRecord() {
    saveBusy = true;
    saveError = '';
    saveSuccess = '';
    try {
      await createSharedPropertyType({
        name: sharedPropertyName.trim(),
        display_name: sharedPropertyDisplayName.trim() || undefined,
        description: sharedPropertyDescription.trim() || undefined,
        property_type: sharedPropertyBaseType,
        required: sharedPropertyRequired,
        unique_constraint: sharedPropertyUnique,
        time_dependent: sharedPropertyTimeDependent,
        default_value: parseJson(sharedPropertyDefaultValueText, 'Default value JSON'),
        validation_rules: parseJson(sharedPropertyValidationRulesText, 'Validation rules JSON')
      });
      const response = await listSharedPropertyTypes({ per_page: 200 });
      sharedPropertyTypes = response.data;
      selectedSharedPropertyTypeId = sharedPropertyTypes[0]?.id ?? '';
      resetSharedPropertyDraft();
      saveSuccess = 'Shared property type created.';
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to create shared property type';
    } finally {
      saveBusy = false;
    }
  }

  async function updateSelectedSharedProperty() {
    if (!selectedSharedPropertyType) return;
    saveBusy = true;
    saveError = '';
    saveSuccess = '';
    try {
      await updateSharedPropertyType(selectedSharedPropertyType.id, {
        display_name: sharedPropertyDisplayName.trim() || undefined,
        description: sharedPropertyDescription.trim() || undefined,
        required: sharedPropertyRequired,
        unique_constraint: sharedPropertyUnique,
        time_dependent: sharedPropertyTimeDependent,
        default_value: parseJson(sharedPropertyDefaultValueText, 'Default value JSON'),
        validation_rules: parseJson(sharedPropertyValidationRulesText, 'Validation rules JSON')
      });
      const response = await listSharedPropertyTypes({ per_page: 200 });
      sharedPropertyTypes = response.data;
      await loadTypeContext(selectedObjectTypeId);
      saveSuccess = 'Shared property type updated.';
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to update shared property type';
    } finally {
      saveBusy = false;
    }
  }

  function seedSharedPropertyDraft(sharedProperty: SharedPropertyType | null) {
    if (!sharedProperty) {
      resetSharedPropertyDraft();
      return;
    }
    sharedPropertyName = sharedProperty.name;
    sharedPropertyDisplayName = sharedProperty.display_name;
    sharedPropertyDescription = sharedProperty.description;
    sharedPropertyBaseType = sharedProperty.property_type;
    sharedPropertyRequired = sharedProperty.required;
    sharedPropertyUnique = sharedProperty.unique_constraint;
    sharedPropertyTimeDependent = sharedProperty.time_dependent;
    sharedPropertyDefaultValueText = prettyJson(sharedProperty.default_value);
    sharedPropertyValidationRulesText = prettyJson(sharedProperty.validation_rules ?? {});
  }

  async function removeSharedPropertyTypeRecord() {
    if (!selectedSharedPropertyType) return;
    if (typeof window !== 'undefined' && !window.confirm('Delete this shared property type?')) return;
    saveBusy = true;
    saveError = '';
    saveSuccess = '';
    try {
      await deleteSharedPropertyType(selectedSharedPropertyType.id);
      const response = await listSharedPropertyTypes({ per_page: 200 });
      sharedPropertyTypes = response.data;
      selectedSharedPropertyTypeId = sharedPropertyTypes[0]?.id ?? '';
      seedSharedPropertyDraft(sharedPropertyTypes.find((item) => item.id === selectedSharedPropertyTypeId) ?? null);
      await loadTypeContext(selectedObjectTypeId);
      saveSuccess = 'Shared property type deleted.';
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to delete shared property type';
    } finally {
      saveBusy = false;
    }
  }

  async function toggleSharedPropertyAttachment(sharedPropertyTypeId: string) {
    if (!selectedObjectTypeId) return;
    try {
      if (attachedSharedIds.includes(sharedPropertyTypeId)) {
        await detachSharedPropertyType(selectedObjectTypeId, sharedPropertyTypeId);
      } else {
        await attachSharedPropertyType(selectedObjectTypeId, sharedPropertyTypeId);
      }
      await loadTypeContext(selectedObjectTypeId);
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to update shared property attachment';
    }
  }

  async function saveLinkType() {
    saveBusy = true;
    saveError = '';
    saveSuccess = '';
    try {
      if (selectedLinkTypeId) {
        await updateLinkType(selectedLinkTypeId, {
          display_name: linkDisplayName.trim() || undefined,
          description: linkDescription.trim() || undefined,
          cardinality: linkCardinality
        });
        saveSuccess = 'Link type updated.';
      } else {
        await createLinkType({
          name: linkName.trim(),
          display_name: linkDisplayName.trim() || undefined,
          description: linkDescription.trim() || undefined,
          source_type_id: linkSourceTypeId,
          target_type_id: linkTargetTypeId,
          cardinality: linkCardinality
        });
        saveSuccess = 'Link type created.';
      }
      const response = await listLinkTypes({ per_page: 200 });
      linkTypes = response.data;
      selectedLinkTypeId = linkTypes.find((item) => item.name === linkName.trim())?.id ?? selectedLinkTypeId;
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to save link type';
    } finally {
      saveBusy = false;
    }
  }

  async function removeLinkTypeRecord() {
    if (!selectedLinkTypeId) return;
    if (typeof window !== 'undefined' && !window.confirm('Delete this link type?')) return;
    saveBusy = true;
    saveError = '';
    saveSuccess = '';
    try {
      await deleteLinkType(selectedLinkTypeId);
      const response = await listLinkTypes({ per_page: 200 });
      linkTypes = response.data;
      selectedLinkTypeId = linkTypes[0]?.id ?? '';
      syncLinkDraft(linkTypes.find((item) => item.id === selectedLinkTypeId) ?? null);
      saveSuccess = 'Link type deleted.';
    } catch (error) {
      saveError = error instanceof Error ? error.message : 'Failed to delete link type';
    } finally {
      saveBusy = false;
    }
  }

  function syncLinkDraft(linkType: LinkType | null) {
    if (!linkType) {
      resetLinkDraft();
      return;
    }
    linkName = linkType.name;
    linkDisplayName = linkType.display_name;
    linkDescription = linkType.description;
    linkSourceTypeId = linkType.source_type_id;
    linkTargetTypeId = linkType.target_type_id;
    linkCardinality = linkType.cardinality;
    selectedLinkTypeId = linkType.id;
  }

  function saveValueTypeRecord() {
    const nextRecord: ValueTypeDraft = {
      id: crypto.randomUUID(),
      name: `value_type_${valueTypes.length + 1}`,
      display_name: `Value Type ${valueTypes.length + 1}`,
      description: 'Reusable value contract with explicit constraints and release semantics.',
      base_type: 'string',
      constraints_json: prettyJson({ regex: '^[A-Z0-9_-]+$', max_length: 64 }),
      permissions_json: prettyJson({ editable_by: ['editor'], readable_by: ['viewer', 'editor'] }),
      version_label: 'v1'
    };
    valueTypes = [nextRecord, ...valueTypes];
    persistStored('valueTypes', valueTypes);
    saveSuccess = 'Value type added to working state.';
  }

  function removeValueTypeRecord(id: string) {
    valueTypes = valueTypes.filter((item) => item.id !== id);
    persistStored('valueTypes', valueTypes);
  }

  function saveStructTypeRecord() {
    const nextRecord: StructTypeDraft = {
      id: crypto.randomUUID(),
      name: `struct_${structTypes.length + 1}`,
      display_name: `Struct ${structTypes.length + 1}`,
      description: 'Reusable structured field composed of typed sub-fields.',
      automapping_mode: 'manual',
      main_field_ids: [],
      fields: [
        {
          id: crypto.randomUUID(),
          name: 'line_1',
          display_name: 'Line 1',
          property_type: 'string',
          required: true
        },
        {
          id: crypto.randomUUID(),
          name: 'postal_code',
          display_name: 'Postal code',
          property_type: 'string',
          required: false
        }
      ],
      shared_property_ids: []
    };
    structTypes = [nextRecord, ...structTypes];
    persistStored('structTypes', structTypes);
    saveSuccess = 'Struct type added to working state.';
  }

  function removeStructTypeRecord(id: string) {
    structTypes = structTypes.filter((item) => item.id !== id);
    persistStored('structTypes', structTypes);
  }

  function updateMetadataDraft(field: keyof TypeMetadataDraft, value: string) {
    if (!selectedObjectTypeId || !metadataDraft) return;
    typeMetadata = {
      ...typeMetadata,
      [selectedObjectTypeId]: {
        ...metadataDraft,
        [field]: value
      }
    };
    persistStored('typeMetadata', typeMetadata);
  }

  function addDerivedPropertyRecord() {
    if (!selectedObjectTypeId) return;
    const nextRecord: DerivedPropertyDraft = {
      id: crypto.randomUUID(),
      name: `derived_${(derivedProperties[selectedObjectTypeId] ?? []).length + 1}`,
      display_name: 'Derived property',
      description: 'Computed field materialized from source properties.',
      result_type: 'string',
      source_expression: 'status + ":" + priority',
      reducer: 'latest_non_null',
      formatting_json: prettyJson({ badge: true, color_scale: 'status' })
    };
    derivedProperties = {
      ...derivedProperties,
      [selectedObjectTypeId]: [nextRecord, ...(derivedProperties[selectedObjectTypeId] ?? [])]
    };
    persistStored('derivedProperties', derivedProperties);
  }

  function removeDerivedPropertyRecord(id: string) {
    if (!selectedObjectTypeId) return;
    derivedProperties = {
      ...derivedProperties,
      [selectedObjectTypeId]: (derivedProperties[selectedObjectTypeId] ?? []).filter((item) => item.id !== id)
    };
    persistStored('derivedProperties', derivedProperties);
  }

  function addObjectTypeGroupRecord() {
    const nextRecord: ObjectTypeGroupDraft = {
      id: crypto.randomUUID(),
      name: `group_${objectTypeGroups.length + 1}`,
      description: 'Curated bundle of related ontology types.',
      object_type_ids: selectedObjectTypeId ? [selectedObjectTypeId] : []
    };
    objectTypeGroups = [nextRecord, ...objectTypeGroups];
    persistStored('objectTypeGroups', objectTypeGroups);
  }

  function removeObjectTypeGroupRecord(id: string) {
    objectTypeGroups = objectTypeGroups.filter((item) => item.id !== id);
    persistStored('objectTypeGroups', objectTypeGroups);
  }

  function updateMarketplaceSelections(field: keyof MarketplaceDraft, value: string[]) {
    marketplaceDraft = {
      ...marketplaceDraft,
      [field]: value
    };
    persistStored('marketplaceDraft', marketplaceDraft);
  }

  function marketplaceManifest() {
    return prettyJson({
      package_name: marketplaceDraft.package_name,
      listing_title: marketplaceDraft.listing_title,
      summary: marketplaceDraft.summary,
      object_types: objectTypes.filter((item) => marketplaceDraft.included_object_type_ids.includes(item.id)).map((item) => item.name),
      link_types: linkTypes.filter((item) => marketplaceDraft.included_link_type_ids.includes(item.id)).map((item) => item.name),
      shared_property_types: sharedPropertyTypes
        .filter((item) => marketplaceDraft.included_shared_property_type_ids.includes(item.id))
        .map((item) => item.name),
      value_types: valueTypes.filter((item) => marketplaceDraft.included_value_type_ids.includes(item.id)).map((item) => item.name)
    });
  }

  onMount(() => {
    void loadPage();
  });
</script>

<svelte:head>
  <title>OpenFoundry - Object and Link Types</title>
</svelte:head>

<div class="mx-auto flex max-w-7xl flex-col gap-6 px-4 py-6">
  <section class="overflow-hidden rounded-[2rem] border border-slate-200 bg-[radial-gradient(circle_at_top_left,_rgba(36,88,184,0.18),_transparent_35%),linear-gradient(135deg,_#f8fafc_0%,_#eef4ff_45%,_#f8fafc_100%)] p-6 shadow-sm">
    <div class="grid gap-6 lg:grid-cols-[1.45fr_1fr]">
      <div class="space-y-4">
        <div class="inline-flex items-center gap-2 rounded-full border border-sky-200 bg-white/80 px-3 py-1 text-xs font-semibold uppercase tracking-[0.24em] text-sky-700">
          <Glyph name="cube" size={14} />
          Define Ontologies / Object and Link Types
        </div>
        <div class="space-y-3">
          <h1 class="text-3xl font-semibold tracking-tight text-slate-950">Object and Link Types</h1>
          <p class="max-w-3xl text-sm leading-6 text-slate-600">
            Manage object types, link types, shared properties, value types, structs, metadata, derived properties, type groups, and Marketplace packaging from a dedicated ontology studio.
          </p>
        </div>
        <div class="flex flex-wrap gap-3 text-xs text-slate-500">
          <a href="/ontology-manager" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-sky-300 hover:text-sky-700">Ontology Manager</a>
          <a href="/interfaces" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-sky-300 hover:text-sky-700">Interfaces</a>
          <a href="/object-views" class="rounded-full border border-slate-200 bg-white px-3 py-1.5 hover:border-sky-300 hover:text-sky-700">Object Views</a>
        </div>
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Object types</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{objectTypes.length}</p>
          <p class="mt-1 text-sm text-slate-500">Core entities modeled in the ontology.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Link types</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{linkTypes.length}</p>
          <p class="mt-1 text-sm text-slate-500">Typed relationships and graph semantics.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Value types</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{valueTypes.length}</p>
          <p class="mt-1 text-sm text-slate-500">Reusable value contracts in working state.</p>
        </div>
        <div class="rounded-3xl border border-white/70 bg-white/80 p-4">
          <p class="text-xs uppercase tracking-[0.24em] text-slate-400">Struct types</p>
          <p class="mt-2 text-3xl font-semibold text-slate-950">{structTypes.length}</p>
          <p class="mt-1 text-sm text-slate-500">Nested reusable field groups and automapping candidates.</p>
        </div>
      </div>
    </div>
  </section>

  {#if saveError}
    <div class="rounded-3xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">{saveError}</div>
  {/if}
  {#if saveSuccess}
    <div class="rounded-3xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">{saveSuccess}</div>
  {/if}

  {#if loading}
    <div class="rounded-3xl border border-slate-200 bg-white px-5 py-10 text-center text-sm text-slate-500">
      Loading object and link type studio...
    </div>
  {:else}
    <section class="rounded-[2rem] border border-slate-200 bg-white p-4 shadow-sm">
      <div class="flex flex-wrap gap-2">
        {#each tabs as tab}
          <button
            class={`inline-flex items-center gap-2 rounded-full px-4 py-2 text-sm font-medium transition ${
              activeTab === tab.id
                ? 'bg-slate-950 text-white'
                : 'border border-slate-200 bg-white text-slate-600 hover:border-slate-300'
            }`}
            onclick={() => activeTab = tab.id}
          >
            <Glyph name={tab.glyph} size={16} />
            {tab.label}
          </button>
        {/each}
      </div>
    </section>

    {#if activeTab === 'object-types'}
      <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_380px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="flex items-start justify-between gap-4">
            <div>
              <p class="text-sm font-semibold text-slate-900">Object type catalog</p>
              <p class="mt-1 text-sm text-slate-500">Create, edit, copy, and curate the core entities in your ontology.</p>
            </div>
            <button class="rounded-full border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700" onclick={resetObjectTypeDraft}>New</button>
          </div>
          <div class="mt-4 grid gap-3">
            {#each objectTypes as objectType}
              <button
                class={`rounded-2xl border px-4 py-3 text-left transition ${selectedObjectTypeId === objectType.id ? 'border-sky-400 bg-sky-50' : 'border-slate-200 bg-white hover:border-slate-300'}`}
                onclick={() => {
                  selectedObjectTypeId = objectType.id;
                  void loadTypeContext(objectType.id);
                }}
              >
                <p class="text-sm font-semibold text-slate-900">{objectType.display_name}</p>
                <p class="mt-1 text-xs font-mono text-slate-500">{objectType.name}</p>
                <p class="mt-2 text-sm text-slate-500">{objectType.description || 'No description provided yet.'}</p>
              </button>
            {/each}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-sm font-semibold text-slate-900">{selectedObjectTypeId ? 'Edit object type' : 'Create object type'}</p>
          <div class="mt-4 space-y-4">
            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Name</span>
              <input class="w-full rounded-2xl border border-slate-300 px-4 py-3 font-mono text-sm outline-none transition focus:border-sky-500 disabled:bg-slate-100" type="text" bind:value={objectTypeName} disabled={Boolean(selectedObjectTypeId)} />
            </label>
            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Display name</span>
              <input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" type="text" bind:value={objectTypeDisplayName} />
            </label>
            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Description</span>
              <textarea rows="3" class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={objectTypeDescription}></textarea>
            </label>
            <div class="grid gap-4 sm:grid-cols-3">
              <label class="space-y-2 text-sm text-slate-700">
                <span class="font-medium">Primary key property</span>
                <input class="w-full rounded-2xl border border-slate-300 px-4 py-3 font-mono text-sm outline-none transition focus:border-sky-500" type="text" bind:value={objectTypePrimaryKeyProperty} placeholder="external_id" />
              </label>
              <label class="space-y-2 text-sm text-slate-700">
                <span class="font-medium">Icon</span>
                <input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" type="text" bind:value={objectTypeIcon} placeholder="cube" />
              </label>
              <label class="space-y-2 text-sm text-slate-700">
                <span class="font-medium">Color</span>
                <input class="h-12 w-full rounded-2xl border border-slate-300 px-2 py-2" type="color" bind:value={objectTypeColor} />
              </label>
            </div>
            <div class="flex flex-wrap gap-3">
              <button class="rounded-full bg-sky-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-sky-500 disabled:bg-sky-300" onclick={() => void saveObjectType()} disabled={saveBusy}>
                {saveBusy ? 'Saving...' : selectedObjectTypeId ? 'Save changes' : 'Create object type'}
              </button>
              {#if selectedObjectTypeId}
                <button class="rounded-full border border-slate-300 bg-white px-5 py-2.5 text-sm font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700 disabled:opacity-60" onclick={() => void copyObjectTypeConfiguration()} disabled={saveBusy}>Copy configuration</button>
                <button class="rounded-full border border-rose-200 bg-rose-50 px-5 py-2.5 text-sm font-medium text-rose-700 hover:border-rose-300 disabled:opacity-60" onclick={() => void removeObjectType()} disabled={saveBusy}>Delete</button>
              {/if}
            </div>
          </div>
        </section>
      </div>
    {:else if activeTab === 'properties'}
      <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_380px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <div class="flex items-start justify-between gap-4">
            <div>
              <p class="text-sm font-semibold text-slate-900">Properties on {selectedObjectType?.display_name ?? 'selected type'}</p>
              <p class="mt-1 text-sm text-slate-500">Base types, requiredness, default values, validation rules, edit-only patterns, and conditional formatting hooks.</p>
            </div>
            {#if typeContextLoading}
              <span class="text-xs text-slate-500">Refreshing...</span>
            {/if}
          </div>
          <div class="mt-4 space-y-3">
            {#each properties as property}
              <div class="rounded-2xl border border-slate-200 p-4">
                <div class="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">{property.display_name}</p>
                    <p class="mt-1 text-xs font-mono text-slate-500">{property.name}</p>
                  </div>
                  <div class="flex flex-wrap gap-2 text-[11px] text-slate-500">
                    <span class="rounded-full border border-slate-200 bg-slate-50 px-2 py-1">{property.property_type}</span>
                    {#if property.required}
                      <span class="rounded-full border border-amber-200 bg-amber-50 px-2 py-1 text-amber-700">Required</span>
                    {/if}
                    {#if property.time_dependent}
                      <span class="rounded-full border border-sky-200 bg-sky-50 px-2 py-1 text-sky-700">Time dependent</span>
                    {/if}
                  </div>
                </div>
                <p class="mt-3 text-sm text-slate-500">{property.description || 'No description provided.'}</p>
                <div class="mt-4 flex flex-wrap gap-2">
                  <button class="rounded-full border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700" onclick={() => void togglePropertyFlag(property, 'required')}>
                    {property.required ? 'Mark optional' : 'Mark required'}
                  </button>
                  <button class="rounded-full border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700" onclick={() => void togglePropertyFlag(property, 'time_dependent')}>
                    {property.time_dependent ? 'Disable time dependence' : 'Enable time dependence'}
                  </button>
                  <button class="rounded-full border border-rose-200 bg-rose-50 px-3 py-1.5 text-xs font-medium text-rose-700 hover:border-rose-300" onclick={() => void removePropertyRecord(property)}>
                    Delete property
                  </button>
                </div>
              </div>
            {/each}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-sm font-semibold text-slate-900">Create property</p>
          <div class="mt-4 space-y-4">
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Name</span><input class="w-full rounded-2xl border border-slate-300 px-4 py-3 font-mono text-sm outline-none transition focus:border-sky-500" type="text" bind:value={propertyName} /></label>
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Display name</span><input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" type="text" bind:value={propertyDisplayName} /></label>
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Description</span><input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" type="text" bind:value={propertyDescription} /></label>
            <label class="space-y-2 text-sm text-slate-700">
              <span class="font-medium">Base type</span>
              <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={propertyType}>
                {#each basePropertyTypes as type}
                  <option value={type}>{type}</option>
                {/each}
              </select>
            </label>
            <div class="grid gap-3 sm:grid-cols-3">
              <label class="flex items-center gap-2 rounded-2xl border border-slate-200 px-4 py-3 text-sm text-slate-700"><input type="checkbox" checked={propertyRequired} onchange={(event) => propertyRequired = (event.currentTarget as HTMLInputElement).checked} /> Required</label>
              <label class="flex items-center gap-2 rounded-2xl border border-slate-200 px-4 py-3 text-sm text-slate-700"><input type="checkbox" checked={propertyUnique} onchange={(event) => propertyUnique = (event.currentTarget as HTMLInputElement).checked} /> Unique</label>
              <label class="flex items-center gap-2 rounded-2xl border border-slate-200 px-4 py-3 text-sm text-slate-700"><input type="checkbox" checked={propertyTimeDependent} onchange={(event) => propertyTimeDependent = (event.currentTarget as HTMLInputElement).checked} /> Time dependent</label>
            </div>
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Default value JSON</span><textarea rows="4" class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-sky-500" bind:value={propertyDefaultValueText} spellcheck="false"></textarea></label>
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Validation rules JSON</span><textarea rows="4" class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-sky-500" bind:value={propertyValidationRulesText} spellcheck="false"></textarea></label>
            <button class="rounded-full bg-sky-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-sky-500 disabled:bg-sky-300" onclick={() => void createPropertyRecord()} disabled={saveBusy || !selectedObjectTypeId}>
              {saveBusy ? 'Creating...' : 'Create property'}
            </button>
          </div>
        </section>
      </div>
    {:else if activeTab === 'shared-properties'}
      <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_380px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-sm font-semibold text-slate-900">Shared property catalog</p>
          <div class="mt-4 space-y-3">
            {#each sharedPropertyTypes as sharedProperty}
              <button class={`w-full rounded-2xl border px-4 py-3 text-left transition ${selectedSharedPropertyTypeId === sharedProperty.id ? 'border-sky-400 bg-sky-50' : 'border-slate-200 bg-white hover:border-slate-300'}`} onclick={() => { selectedSharedPropertyTypeId = sharedProperty.id; seedSharedPropertyDraft(sharedProperty); }}>
                <p class="text-sm font-semibold text-slate-900">{sharedProperty.display_name}</p>
                <p class="mt-1 text-xs font-mono text-slate-500">{sharedProperty.name}</p>
                <p class="mt-2 text-sm text-slate-500">{sharedProperty.description || 'No description provided.'}</p>
              </button>
            {/each}
          </div>

          <div class="mt-6 rounded-3xl border border-slate-200 p-4">
            <p class="text-sm font-semibold text-slate-900">Attached to selected object type</p>
            <div class="mt-4 flex flex-wrap gap-2">
              {#each sharedPropertyTypes as sharedProperty}
                <button class={`rounded-full px-3 py-1.5 text-xs font-medium ${attachedSharedIds.includes(sharedProperty.id) ? 'bg-sky-100 text-sky-700' : 'border border-slate-200 bg-white text-slate-600'}`} onclick={() => void toggleSharedPropertyAttachment(sharedProperty.id)}>
                  {attachedSharedIds.includes(sharedProperty.id) ? 'Attached' : 'Attach'} · {sharedProperty.display_name}
                </button>
              {/each}
            </div>
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-sm font-semibold text-slate-900">{selectedSharedPropertyType ? 'Edit shared property type' : 'Create shared property type'}</p>
          <div class="mt-4 space-y-4">
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Name</span><input class="w-full rounded-2xl border border-slate-300 px-4 py-3 font-mono text-sm outline-none transition focus:border-sky-500 disabled:bg-slate-100" type="text" bind:value={sharedPropertyName} disabled={Boolean(selectedSharedPropertyType)} /></label>
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Display name</span><input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" type="text" bind:value={sharedPropertyDisplayName} /></label>
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Description</span><input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" type="text" bind:value={sharedPropertyDescription} /></label>
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Base type</span>
              <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={sharedPropertyBaseType}>
                {#each basePropertyTypes as type}
                  <option value={type}>{type}</option>
                {/each}
              </select>
            </label>
            <div class="grid gap-3 sm:grid-cols-3">
              <label class="flex items-center gap-2 rounded-2xl border border-slate-200 px-4 py-3 text-sm text-slate-700"><input type="checkbox" checked={sharedPropertyRequired} onchange={(event) => sharedPropertyRequired = (event.currentTarget as HTMLInputElement).checked} /> Required</label>
              <label class="flex items-center gap-2 rounded-2xl border border-slate-200 px-4 py-3 text-sm text-slate-700"><input type="checkbox" checked={sharedPropertyUnique} onchange={(event) => sharedPropertyUnique = (event.currentTarget as HTMLInputElement).checked} /> Unique</label>
              <label class="flex items-center gap-2 rounded-2xl border border-slate-200 px-4 py-3 text-sm text-slate-700"><input type="checkbox" checked={sharedPropertyTimeDependent} onchange={(event) => sharedPropertyTimeDependent = (event.currentTarget as HTMLInputElement).checked} /> Time dependent</label>
            </div>
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Default value JSON</span><textarea rows="4" class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-sky-500" bind:value={sharedPropertyDefaultValueText} spellcheck="false"></textarea></label>
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Validation rules JSON</span><textarea rows="4" class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-sky-500" bind:value={sharedPropertyValidationRulesText} spellcheck="false"></textarea></label>
            <div class="flex flex-wrap gap-3">
              <button class="rounded-full bg-sky-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-sky-500 disabled:bg-sky-300" onclick={() => void (selectedSharedPropertyType ? updateSelectedSharedProperty() : createSharedPropertyRecord())} disabled={saveBusy}>
                {saveBusy ? 'Saving...' : selectedSharedPropertyType ? 'Save changes' : 'Create shared property type'}
              </button>
              {#if selectedSharedPropertyType}
                <button class="rounded-full border border-rose-200 bg-rose-50 px-5 py-2.5 text-sm font-medium text-rose-700 hover:border-rose-300 disabled:opacity-60" onclick={() => void removeSharedPropertyTypeRecord()} disabled={saveBusy}>Delete</button>
              {/if}
            </div>
          </div>
        </section>
      </div>
    {:else if activeTab === 'link-types'}
      <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_380px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-sm font-semibold text-slate-900">Link type catalog</p>
          <div class="mt-4 space-y-3">
            {#each linkTypes as linkType}
              <button class={`w-full rounded-2xl border px-4 py-3 text-left transition ${selectedLinkTypeId === linkType.id ? 'border-sky-400 bg-sky-50' : 'border-slate-200 bg-white hover:border-slate-300'}`} onclick={() => syncLinkDraft(linkType)}>
                <p class="text-sm font-semibold text-slate-900">{linkType.display_name}</p>
                <p class="mt-1 text-xs font-mono text-slate-500">{linkType.name}</p>
                <p class="mt-2 text-sm text-slate-500">{linkType.description || 'No description provided.'}</p>
                <div class="mt-3 inline-flex rounded-full border border-slate-200 bg-slate-50 px-2 py-1 text-[11px] font-medium text-slate-600">{linkType.cardinality}</div>
              </button>
            {/each}
          </div>
        </section>

        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-sm font-semibold text-slate-900">{selectedLinkType ? 'Edit link type' : 'Create link type'}</p>
          <div class="mt-4 space-y-4">
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Name</span><input class="w-full rounded-2xl border border-slate-300 px-4 py-3 font-mono text-sm outline-none transition focus:border-sky-500 disabled:bg-slate-100" type="text" bind:value={linkName} disabled={Boolean(selectedLinkType)} /></label>
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Display name</span><input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" type="text" bind:value={linkDisplayName} /></label>
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Description</span><input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" type="text" bind:value={linkDescription} /></label>
            <div class="grid gap-4 sm:grid-cols-2">
              <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Source type</span>
                <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={linkSourceTypeId}>
                  {#each objectTypes as type}
                    <option value={type.id}>{type.display_name}</option>
                  {/each}
                </select>
              </label>
              <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Target type</span>
                <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={linkTargetTypeId}>
                  {#each objectTypes as type}
                    <option value={type.id}>{type.display_name}</option>
                  {/each}
                </select>
              </label>
            </div>
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Cardinality</span>
              <select class="w-full rounded-2xl border border-slate-300 bg-white px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={linkCardinality}>
                {#each cardinalityOptions as option}
                  <option value={option}>{option}</option>
                {/each}
              </select>
            </label>
            <div class="flex flex-wrap gap-3">
              <button class="rounded-full bg-sky-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-sky-500 disabled:bg-sky-300" onclick={() => void saveLinkType()} disabled={saveBusy}>{saveBusy ? 'Saving...' : selectedLinkType ? 'Save changes' : 'Create link type'}</button>
              {#if selectedLinkType}
                <button class="rounded-full border border-rose-200 bg-rose-50 px-5 py-2.5 text-sm font-medium text-rose-700 hover:border-rose-300 disabled:opacity-60" onclick={() => void removeLinkTypeRecord()} disabled={saveBusy}>Delete</button>
              {/if}
            </div>
          </div>
        </section>
      </div>
    {:else if activeTab === 'value-types'}
      <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
        <div class="flex items-center justify-between gap-3">
          <div>
            <p class="text-sm font-semibold text-slate-900">Value types</p>
            <p class="mt-1 text-sm text-slate-500">Reusable typed contracts with constraints, permissions, and version labels.</p>
          </div>
          <button class="rounded-full bg-sky-600 px-4 py-2 text-sm font-medium text-white hover:bg-sky-500" onclick={saveValueTypeRecord}>Add value type</button>
        </div>
        <div class="mt-4 grid gap-3 xl:grid-cols-2">
          {#each valueTypes as valueType}
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="flex items-start justify-between gap-3">
                <div>
                  <p class="text-sm font-semibold text-slate-900">{valueType.display_name}</p>
                  <p class="mt-1 text-xs font-mono text-slate-500">{valueType.name}</p>
                </div>
                <button class="rounded-full border border-rose-200 bg-rose-50 px-3 py-1.5 text-xs font-medium text-rose-700 hover:border-rose-300" onclick={() => removeValueTypeRecord(valueType.id)}>Delete</button>
              </div>
              <p class="mt-3 text-sm text-slate-500">{valueType.description}</p>
              <div class="mt-4 grid gap-3 md:grid-cols-2">
                <pre class="overflow-x-auto rounded-2xl bg-slate-950 p-4 text-[11px] text-slate-100">{valueType.constraints_json}</pre>
                <pre class="overflow-x-auto rounded-2xl bg-slate-950 p-4 text-[11px] text-slate-100">{valueType.permissions_json}</pre>
              </div>
            </div>
          {/each}
        </div>
      </section>
    {:else if activeTab === 'structs'}
      <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
        <div class="flex items-center justify-between gap-3">
          <div>
            <p class="text-sm font-semibold text-slate-900">Struct types</p>
            <p class="mt-1 text-sm text-slate-500">Nested field groups with automapping modes, designated main fields, and shared-property compatibility.</p>
          </div>
          <button class="rounded-full bg-sky-600 px-4 py-2 text-sm font-medium text-white hover:bg-sky-500" onclick={saveStructTypeRecord}>Add struct type</button>
        </div>
        <div class="mt-4 grid gap-3 xl:grid-cols-2">
          {#each structTypes as structType}
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="flex items-start justify-between gap-3">
                <div>
                  <p class="text-sm font-semibold text-slate-900">{structType.display_name}</p>
                  <p class="mt-1 text-xs font-mono text-slate-500">{structType.name}</p>
                </div>
                <button class="rounded-full border border-rose-200 bg-rose-50 px-3 py-1.5 text-xs font-medium text-rose-700 hover:border-rose-300" onclick={() => removeStructTypeRecord(structType.id)}>Delete</button>
              </div>
              <p class="mt-3 text-sm text-slate-500">{structType.description}</p>
              <div class="mt-4 grid gap-2">
                {#each structType.fields as field}
                  <div class="rounded-2xl border border-slate-200 px-3 py-2 text-sm text-slate-700">
                    {field.display_name} · {field.property_type}{field.required ? ' · required' : ''}
                  </div>
                {/each}
              </div>
            </div>
          {/each}
        </div>
      </section>
    {:else if activeTab === 'metadata'}
      <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
        <p class="text-sm font-semibold text-slate-900">Metadata for {selectedObjectType?.display_name ?? 'selected object type'}</p>
        <div class="mt-4 grid gap-4 xl:grid-cols-3">
          <label class="space-y-2 text-sm text-slate-700">
            <span class="font-medium">Render hints JSON</span>
            <textarea rows="14" class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-sky-500" value={metadataDraft?.render_hints_json ?? '{}'} oninput={(event) => updateMetadataDraft('render_hints_json', (event.currentTarget as HTMLTextAreaElement).value)} spellcheck="false"></textarea>
          </label>
          <label class="space-y-2 text-sm text-slate-700">
            <span class="font-medium">Statuses JSON</span>
            <textarea rows="14" class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-sky-500" value={metadataDraft?.statuses_json ?? '[]'} oninput={(event) => updateMetadataDraft('statuses_json', (event.currentTarget as HTMLTextAreaElement).value)} spellcheck="false"></textarea>
          </label>
          <label class="space-y-2 text-sm text-slate-700">
            <span class="font-medium">Type classes JSON</span>
            <textarea rows="14" class="w-full rounded-2xl border border-slate-300 bg-slate-950 px-4 py-3 font-mono text-xs text-slate-100 outline-none transition focus:border-sky-500" value={metadataDraft?.type_classes_json ?? '[]'} oninput={(event) => updateMetadataDraft('type_classes_json', (event.currentTarget as HTMLTextAreaElement).value)} spellcheck="false"></textarea>
          </label>
        </div>
      </section>
    {:else if activeTab === 'derived'}
      <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
        <div class="flex items-center justify-between gap-3">
          <div>
            <p class="text-sm font-semibold text-slate-900">Derived properties</p>
            <p class="mt-1 text-sm text-slate-500">Computed fields with reducer semantics, formatting metadata, and source expressions.</p>
          </div>
          <button class="rounded-full bg-sky-600 px-4 py-2 text-sm font-medium text-white hover:bg-sky-500" onclick={addDerivedPropertyRecord}>Add derived property</button>
        </div>
        <div class="mt-4 grid gap-3 xl:grid-cols-2">
          {#each derivedDrafts as derived}
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="flex items-start justify-between gap-3">
                <div>
                  <p class="text-sm font-semibold text-slate-900">{derived.display_name}</p>
                  <p class="mt-1 text-xs font-mono text-slate-500">{derived.name}</p>
                </div>
                <button class="rounded-full border border-rose-200 bg-rose-50 px-3 py-1.5 text-xs font-medium text-rose-700 hover:border-rose-300" onclick={() => removeDerivedPropertyRecord(derived.id)}>Delete</button>
              </div>
              <p class="mt-3 text-sm text-slate-500">{derived.description}</p>
              <div class="mt-4 grid gap-3 md:grid-cols-2">
                <div class="rounded-2xl border border-slate-200 px-3 py-2 text-sm text-slate-700">Reducer: {derived.reducer}</div>
                <div class="rounded-2xl border border-slate-200 px-3 py-2 text-sm text-slate-700">Result type: {derived.result_type}</div>
              </div>
              <pre class="mt-4 overflow-x-auto rounded-2xl bg-slate-950 p-4 text-[11px] text-slate-100">{derived.source_expression}</pre>
            </div>
          {/each}
        </div>
      </section>
    {:else if activeTab === 'groups'}
      <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
        <div class="flex items-center justify-between gap-3">
          <div>
            <p class="text-sm font-semibold text-slate-900">Object type groups</p>
            <p class="mt-1 text-sm text-slate-500">Curate bundles of related object types for reuse, packaging, and navigation.</p>
          </div>
          <button class="rounded-full bg-sky-600 px-4 py-2 text-sm font-medium text-white hover:bg-sky-500" onclick={addObjectTypeGroupRecord}>Add group</button>
        </div>
        <div class="mt-4 grid gap-3 xl:grid-cols-2">
          {#each objectTypeGroups as group}
            <div class="rounded-2xl border border-slate-200 p-4">
              <div class="flex items-start justify-between gap-3">
                <div>
                  <p class="text-sm font-semibold text-slate-900">{group.name}</p>
                  <p class="mt-1 text-sm text-slate-500">{group.description}</p>
                </div>
                <button class="rounded-full border border-rose-200 bg-rose-50 px-3 py-1.5 text-xs font-medium text-rose-700 hover:border-rose-300" onclick={() => removeObjectTypeGroupRecord(group.id)}>Delete</button>
              </div>
              <div class="mt-4 flex flex-wrap gap-2">
                {#each objectTypes.filter((item) => group.object_type_ids.includes(item.id)) as type}
                  <span class="rounded-full border border-slate-200 bg-slate-50 px-3 py-1 text-xs text-slate-600">{type.display_name}</span>
                {/each}
              </div>
            </div>
          {/each}
        </div>
      </section>
    {:else if activeTab === 'marketplace'}
      <div class="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_380px]">
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-sm font-semibold text-slate-900">Add ontology types to a Marketplace product</p>
          <div class="mt-4 space-y-4">
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Package name</span><input class="w-full rounded-2xl border border-slate-300 px-4 py-3 font-mono text-sm outline-none transition focus:border-sky-500" type="text" bind:value={marketplaceDraft.package_name} oninput={() => persistStored('marketplaceDraft', marketplaceDraft)} /></label>
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Listing title</span><input class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" type="text" bind:value={marketplaceDraft.listing_title} oninput={() => persistStored('marketplaceDraft', marketplaceDraft)} /></label>
            <label class="space-y-2 text-sm text-slate-700"><span class="font-medium">Summary</span><textarea rows="4" class="w-full rounded-2xl border border-slate-300 px-4 py-3 text-sm outline-none transition focus:border-sky-500" bind:value={marketplaceDraft.summary} oninput={() => persistStored('marketplaceDraft', marketplaceDraft)}></textarea></label>

            <div class="grid gap-4 md:grid-cols-2">
              <div class="rounded-3xl border border-slate-200 p-4">
                <p class="text-sm font-semibold text-slate-900">Object types</p>
                <div class="mt-3 space-y-2">
                  {#each objectTypes as item}
                    <label class="flex items-center gap-2 text-sm text-slate-700"><input type="checkbox" checked={marketplaceDraft.included_object_type_ids.includes(item.id)} onchange={(event) => updateMarketplaceSelections('included_object_type_ids', (event.currentTarget as HTMLInputElement).checked ? [...marketplaceDraft.included_object_type_ids, item.id] : marketplaceDraft.included_object_type_ids.filter((value) => value !== item.id))} /> {item.display_name}</label>
                  {/each}
                </div>
              </div>
              <div class="rounded-3xl border border-slate-200 p-4">
                <p class="text-sm font-semibold text-slate-900">Link types</p>
                <div class="mt-3 space-y-2">
                  {#each linkTypes as item}
                    <label class="flex items-center gap-2 text-sm text-slate-700"><input type="checkbox" checked={marketplaceDraft.included_link_type_ids.includes(item.id)} onchange={(event) => updateMarketplaceSelections('included_link_type_ids', (event.currentTarget as HTMLInputElement).checked ? [...marketplaceDraft.included_link_type_ids, item.id] : marketplaceDraft.included_link_type_ids.filter((value) => value !== item.id))} /> {item.display_name}</label>
                  {/each}
                </div>
              </div>
            </div>
          </div>
        </section>
        <section class="rounded-[2rem] border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-sm font-semibold text-slate-900">Marketplace manifest preview</p>
          <pre class="mt-4 overflow-x-auto rounded-2xl bg-slate-950 p-4 text-xs text-slate-100">{marketplaceManifest()}</pre>
        </section>
      </div>
    {/if}
  {/if}
</div>
