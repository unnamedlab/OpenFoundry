import api from "./client";

// ────────────────────────────────────────────────────────────────
// Ontology API — slices grow as routes are migrated. Today: object
// types + properties listing (used by /queries). Full ontology
// surface (links, indexing, design) gets added with /ontology* routes.
// ────────────────────────────────────────────────────────────────

export interface ObjectType {
  id: string;
  rid?: string;
  name: string;
  api_name?: string;
  display_name: string;
  plural_display_name?: string | null;
  description: string;
  primary_key_property: string | null;
  primary_key?: string;
  title_property?: string | null;
  icon: string | null;
  color: string | null;
  status?: "active" | "experimental" | "deprecated" | string;
  visibility?: "normal" | "hidden" | string;
  group_names?: string[];
  object_display_preferences?: Record<string, unknown>;
  editable?: boolean;
  backing_dataset_id?: string | null;
  backing_dataset_rid?: string | null;
  backing_datasource_type?: "dataset" | "restricted_view" | string | null;
  backing_restricted_view_id?: string | null;
  restricted_view_id?: string | null;
  restricted_view_policy?: RestrictedViewRowPolicy | null;
  restricted_view_policy_version?: number | null;
  restricted_view_registered_policy_version?: number | null;
  restricted_view_indexed_policy_version?: number | null;
  restricted_view_storage_mode?: RestrictedViewStorageMode | null;
  restricted_view_policy_updated_at?: string | null;
  restricted_view_registered_at?: string | null;
  restricted_view_indexed_at?: string | null;
  object_security_policy_id?: string | null;
  object_security_policy?: ObjectSecurityPolicyDefinition | null;
  object_security_policy_support?: ObjectSecurityPolicyPrimitiveSupport | null;
  object_security_policy_test_fixtures?: boolean | null;
  security_policy_evaluation_supported?: boolean | null;
  security_policy_mode?: string | null;
  property_security_policies?: PropertySecurityPolicyDefinition[];
  pipeline_rid?: string | null;
  managed_by?: string | null;
  properties?: Property[];
  property_count?: number;
  searchable_property_names?: string[];
  geopoint_property_names?: string[];
  geoshape_property_names?: string[];
  owner_id: string;
  created_at: string;
  updated_at: string;
}

export type PropertyDisplayMode = "hidden" | "normal" | "prominent" | string;

export interface PropertyValueFormatting {
  style?: "plain" | "number" | "currency" | "percent" | "date" | "time" | "datetime" | string;
  locale?: string;
  currency?: string;
  minimum_fraction_digits?: number;
  maximum_fraction_digits?: number;
  prefix?: string;
  suffix?: string;
  empty_value?: string;
}


export type ValueTypeEditKind = "non_breaking" | "breaking";

export interface ValueTypeConstraints {
  required?: boolean;
  min?: number;
  max?: number;
  min_length?: number;
  max_length?: number;
  regex?: string;
  allowed_values?: unknown[];
}

export interface ValueTypeVersion {
  version: number;
  edit_kind: ValueTypeEditKind;
  note: string;
  created_by: string;
  created_at: string;
}

export interface ValueTypePermissions {
  viewers?: string[];
  appliers?: string[];
  editors?: string[];
}

export interface OntologyValueType {
  id: string;
  name: string;
  display_name: string;
  description: string;
  space_id: string;
  base_type: string;
  semantic_type: string;
  constraints: ValueTypeConstraints;
  formatting?: PropertyValueFormatting | Record<string, unknown>;
  permissions: ValueTypePermissions;
  version: number;
  versions: ValueTypeVersion[];
  status: "active" | "deprecated" | string;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

export interface PropertyConditionalFormattingRule {
  operator?: "eq" | "neq" | "gt" | "gte" | "lt" | "lte" | "contains" | "empty" | "not_empty" | string;
  value?: unknown;
  color?: string;
  background?: string;
  font_weight?: string;
  label?: string;
}

export interface PropertyReducerMetadata {
  reducer?: "count" | "sum" | "avg" | "min" | "max" | "latest" | "earliest" | string;
  source_property?: string;
  window?: string;
  label?: string;
}

export interface PropertyInlineEditConfig {
  action_type_id?: string;
  input_name?: string | null;
  enabled?: boolean;
  permission_keys?: string[];
}

export interface ObjectSecurityPropertyDecision {
  property_name: string;
  policy_id: string | null;
  can_read: boolean;
  can_edit_property: boolean;
  can_edit_policy_property: boolean;
  policy_property: boolean;
  blocked: boolean;
  reason: string;
}

export interface ObjectSecurityPolicyEvaluation {
  enforcement_state: ObjectSecurityPolicyEnforcementState;
  blocked: boolean;
  can_read_object: boolean;
  reason: string;
  warnings: string[];
  object_policy_id: string | null;
  property_decisions: ObjectSecurityPropertyDecision[];
}

export interface Property {
  id: string;
  object_type_id: string;
  name: string;
  display_name: string;
  description: string;
  property_type: string;
  value_type_id?: string | null;
  value_type?: OntologyValueType | null;
  shared_property_type_id?: string | null;
  base_type?: string;
  type_family?: string;
  type_display_name?: string;
  value_shape?: string;
  is_array?: boolean;
  array_item_type?: string | null;
  array_allowed?: boolean;
  searchable?: boolean;
  filterable?: boolean;
  sortable?: boolean;
  aggregatable?: boolean;
  primary_key_eligible?: boolean;
  title_key_eligible?: boolean;
  formatting_eligible?: boolean;
  object_security_eligible?: boolean;
  security_policy_id?: string | null;
  security_policy_property?: boolean;
  prominent_eligible?: boolean;
  semantic_hints?: string[];
  display_mode?: PropertyDisplayMode;
  value_formatting?: PropertyValueFormatting | Record<string, unknown>;
  conditional_formatting?: PropertyConditionalFormattingRule[];
  reducer_metadata?: PropertyReducerMetadata | Record<string, unknown>;
  required: boolean;
  unique_constraint: boolean;
  time_dependent: boolean;
  default_value: unknown;
  validation_rules: unknown;
  inline_edit_config?: PropertyInlineEditConfig | null;
  created_at: string;
  updated_at: string;
}

export function objectTypeRID(
  objectType: Pick<ObjectType, "id"> & Partial<ObjectType>,
) {
  return objectType.rid || `ri.ontology.main.object-type.${objectType.id}`;
}

export function objectTypeAPIName(
  objectType: Pick<ObjectType, "name"> & Partial<ObjectType>,
) {
  return objectType.api_name || objectType.name;
}

export function objectTypePluralDisplayName(
  objectType: Pick<ObjectType, "display_name" | "name"> & Partial<ObjectType>,
) {
  if (objectType.plural_display_name?.trim())
    return objectType.plural_display_name;
  const base = objectType.display_name || objectType.name || "Objects";
  return base.toLowerCase().endsWith("s") ? base : `${base}s`;
}

export function objectTypePrimaryKey(
  objectType: Partial<ObjectType> | null | undefined,
) {
  return objectType?.primary_key || objectType?.primary_key_property || "id";
}

export function objectTypeTitleProperty(
  objectType: Partial<ObjectType> | null | undefined,
) {
  return objectType?.title_property || objectTypePrimaryKey(objectType);
}

export function objectTypeProperties(
  objectType: Partial<ObjectType> | null | undefined,
) {
  return Array.isArray(objectType?.properties) ? objectType.properties : [];
}

export function objectTypeSearchablePropertyNames(
  objectType: Partial<ObjectType> | null | undefined,
) {
  if (
    Array.isArray(objectType?.searchable_property_names) &&
    objectType.searchable_property_names.length > 0
  ) {
    return objectType.searchable_property_names;
  }
  const names = [
    objectTypeTitleProperty(objectType),
    objectTypePrimaryKey(objectType),
  ];
  for (const property of objectTypeProperties(objectType)) {
    if (isStringLikePropertyType(property.property_type))
      names.push(property.name);
  }
  return uniqueNonEmpty(names);
}

export function objectTypeGeoPointPropertyNames(
  objectType: Partial<ObjectType> | null | undefined,
) {
  if (Array.isArray(objectType?.geopoint_property_names))
    return objectType.geopoint_property_names;
  return objectTypeProperties(objectType)
    .filter((property) => isGeoPointPropertyType(property.property_type))
    .map((property) => property.name);
}

export function objectTypeGeoShapePropertyNames(
  objectType: Partial<ObjectType> | null | undefined,
) {
  if (Array.isArray(objectType?.geoshape_property_names))
    return objectType.geoshape_property_names;
  return objectTypeProperties(objectType)
    .filter((property) => isGeoShapePropertyType(property.property_type))
    .map((property) => property.name);
}

export interface PropertyTypeMetadata {
  base_type: string;
  type_family: string;
  type_display_name: string;
  value_shape: string;
  is_array: boolean;
  array_item_type: string | null;
  array_allowed: boolean;
  searchable: boolean;
  filterable: boolean;
  sortable: boolean;
  aggregatable: boolean;
  primary_key_eligible: boolean;
  title_key_eligible: boolean;
  formatting_eligible: boolean;
  object_security_eligible: boolean;
  prominent_eligible: boolean;
  semantic_hints: string[];
}

export function propertyTypeMetadata(
  property: Pick<Property, "property_type"> & Partial<Property>,
): PropertyTypeMetadata {
  const derived = derivePropertyTypeMetadata(property.property_type);
  return {
    base_type: property.base_type || derived.base_type,
    type_family: property.type_family || derived.type_family,
    type_display_name: property.type_display_name || derived.type_display_name,
    value_shape: property.value_shape || derived.value_shape,
    is_array: property.is_array ?? derived.is_array,
    array_item_type: property.array_item_type ?? derived.array_item_type,
    array_allowed: property.array_allowed ?? derived.array_allowed,
    searchable: property.searchable ?? derived.searchable,
    filterable: property.filterable ?? derived.filterable,
    sortable: property.sortable ?? derived.sortable,
    aggregatable: property.aggregatable ?? derived.aggregatable,
    primary_key_eligible:
      property.primary_key_eligible ?? derived.primary_key_eligible,
    title_key_eligible: property.title_key_eligible ?? derived.title_key_eligible,
    formatting_eligible:
      property.formatting_eligible ?? derived.formatting_eligible,
    object_security_eligible:
      property.object_security_eligible ?? derived.object_security_eligible,
    prominent_eligible: property.prominent_eligible ?? derived.prominent_eligible,
    semantic_hints: property.semantic_hints ?? derived.semantic_hints,
  };
}

export function canonicalPropertyBaseType(propertyType: string) {
  const parsedArray = parseArrayPropertyType(propertyType);
  if (parsedArray.isArray && parsedArray.itemType)
    return canonicalPropertyBaseType(parsedArray.itemType);
  const normalized = normalizedPropertyType(propertyType);
  if (["string", "str", "text"].includes(normalized)) return "string";
  if (["integer", "int", "short", "byte"].includes(normalized))
    return "integer";
  if (normalized === "long") return "long";
  if (["float", "double"].includes(normalized)) return "float";
  if (["decimal", "number", "numeric"].includes(normalized))
    return "decimal";
  if (["boolean", "bool"].includes(normalized)) return "boolean";
  if (normalized === "date") return "date";
  if (normalized === "time") return "time";
  if (["timestamp", "datetime"].includes(normalized)) return "timestamp";
  if (normalized === "json") return "json";
  if (normalized === "array") return "array";
  if (["vector", "embedding"].includes(normalized)) return "vector";
  if (["reference", "object_reference", "object_ref"].includes(normalized))
    return "reference";
  if (normalized === "geohash") return "geohash";
  if (isGeoPointPropertyType(normalized)) return "geopoint";
  if (isGeoShapePropertyType(normalized)) return "geoshape";
  if (["media_reference", "media", "mediaref"].includes(normalized))
    return "media_reference";
  if (["attachment", "file", "file_reference"].includes(normalized))
    return "attachment";
  if (["binary", "bytes"].includes(normalized)) return "binary";
  if (
    ["time_series", "timeseries", "time_series_reference"].includes(normalized)
  )
    return "time_series";
  if (normalized === "struct") return "struct";
  return normalized;
}

function derivePropertyTypeMetadata(
  propertyType: string,
): PropertyTypeMetadata {
  const parsedArray = parseArrayPropertyType(propertyType);
  const itemBaseType = parsedArray.itemType
    ? canonicalPropertyBaseType(parsedArray.itemType)
    : null;
  const baseType = parsedArray.isArray
    ? itemBaseType || "array"
    : canonicalPropertyBaseType(propertyType);
  const base = basePropertyTypeMetadata(baseType);
  if (!parsedArray.isArray) return base;
  return {
    ...base,
    base_type: "array",
    type_family: "collection",
    type_display_name: "Array",
    value_shape: "array",
    is_array: true,
    array_item_type: itemBaseType,
    searchable: base.searchable && baseType === "string",
    filterable: true,
    sortable: false,
    aggregatable: false,
    primary_key_eligible: false,
    title_key_eligible: false,
    object_security_eligible: false,
    prominent_eligible: true,
    semantic_hints: uniqueNonEmpty([
      "array",
      itemBaseType || "",
      ...base.semantic_hints,
    ]),
  };
}

function basePropertyTypeMetadata(baseType: string): PropertyTypeMetadata {
  switch (baseType) {
    case "string":
      return metadata(baseType, "primitive", "String", "string", true, true, true, false, true, true, true, true, true, true, ["string"]);
    case "integer":
      return metadata(baseType, "numeric", "Integer", "integer", true, true, true, true, false, true, false, true, true, true, ["numeric"]);
    case "long":
      return metadata(baseType, "numeric", "Long", "integer", true, true, true, true, false, true, false, true, true, true, ["numeric"]);
    case "float":
      return metadata(baseType, "numeric", "Float", "number", true, true, true, true, false, false, false, true, false, true, ["numeric"]);
    case "decimal":
      return metadata(baseType, "numeric", "Decimal", "decimal", true, true, true, true, false, false, false, true, false, true, ["numeric", "decimal"]);
    case "boolean":
      return metadata(baseType, "primitive", "Boolean", "boolean", true, true, true, false, false, false, false, true, true, true, ["boolean"]);
    case "date":
      return metadata(baseType, "temporal", "Date", "date-string", true, true, true, false, false, false, false, true, true, true, ["temporal"]);
    case "time":
      return metadata(baseType, "temporal", "Time", "time-string", true, true, true, false, false, false, false, true, true, true, ["temporal", "time"]);
    case "timestamp":
      return metadata(baseType, "temporal", "Timestamp", "timestamp-string", true, true, true, false, false, false, false, true, true, true, ["temporal"]);
    case "json":
      return metadata(baseType, "structured", "JSON", "json", true, false, false, false, false, false, false, true, false, false, ["json"]);
    case "array":
      return metadata(baseType, "collection", "Array", "array", true, true, false, false, false, false, false, true, false, false, ["array"]);
    case "vector":
      return metadata(baseType, "semantic", "Vector", "numeric-array", false, false, false, false, false, false, false, false, false, false, ["vector", "embedding"]);
    case "reference":
      return metadata(baseType, "reference", "Object reference", "string", true, true, true, false, true, false, true, true, true, true, ["reference", "object_reference"]);
    case "geohash":
      return metadata(baseType, "geospatial", "Geohash", "string", true, true, true, false, true, false, true, true, true, true, ["geospatial", "geohash"]);
    case "geopoint":
      return metadata(baseType, "geospatial", "Geopoint", "lat-lon-object", true, true, false, false, false, false, false, true, false, true, ["geospatial", "point"]);
    case "geoshape":
      return metadata(baseType, "geospatial", "Geoshape", "geojson-object-or-string", true, true, false, false, false, false, false, true, false, true, ["geospatial", "shape"]);
    case "media_reference":
      return metadata(baseType, "media", "Media reference", "media-reference", true, true, false, false, false, false, false, true, false, true, ["media"]);
    case "attachment":
      return metadata(baseType, "file", "Attachment", "attachment-reference", true, true, false, false, false, false, false, true, false, true, ["attachment", "file"]);
    case "binary":
      return metadata(baseType, "file", "Binary", "base64-string", false, false, false, false, false, false, false, false, false, false, ["binary", "file"]);
    case "time_series":
      return metadata(baseType, "timeseries", "Time series", "time-series", false, false, false, false, false, false, false, false, false, false, ["time_series", "temporal"]);
    case "struct":
      return metadata(baseType, "structured", "Struct", "object", true, false, false, false, false, false, false, true, false, false, ["struct"]);
    default:
      return metadata(baseType, "unknown", baseType || "Unknown", "unknown", true, false, false, false, false, false, false, false, false, false, []);
  }
}

function metadata(
  baseType: string,
  family: string,
  displayName: string,
  valueShape: string,
  arrayAllowed: boolean,
  filterable: boolean,
  sortable: boolean,
  aggregatable: boolean,
  searchable: boolean,
  primaryKeyEligible: boolean,
  titleKeyEligible: boolean,
  formattingEligible: boolean,
  objectSecurityEligible: boolean,
  prominentEligible: boolean,
  semanticHints: string[],
): PropertyTypeMetadata {
  return {
    base_type: baseType,
    type_family: family,
    type_display_name: displayName,
    value_shape: valueShape,
    is_array: false,
    array_item_type: null,
    array_allowed: arrayAllowed,
    searchable,
    filterable,
    sortable,
    aggregatable,
    primary_key_eligible: primaryKeyEligible,
    title_key_eligible: titleKeyEligible,
    formatting_eligible: formattingEligible,
    object_security_eligible: objectSecurityEligible,
    prominent_eligible: prominentEligible,
    semantic_hints: uniqueNonEmpty(semanticHints),
  };
}

function parseArrayPropertyType(propertyType: string) {
  const normalized = normalizedPropertyType(propertyType);
  if (normalized === "array")
    return { isArray: true, itemType: null as string | null };
  if (normalized.endsWith("[]"))
    return { isArray: true, itemType: normalized.slice(0, -2) };
  if (normalized.startsWith("array<"))
    return {
      isArray: true,
      itemType: normalized.replace(/^array</, "").replace(/>$/, "") || null,
    };
  if (normalized.startsWith("array_of_"))
    return {
      isArray: true,
      itemType: normalized.replace(/^array_of_/, "") || null,
    };
  return { isArray: false, itemType: null as string | null };
}

function normalizedPropertyType(kind: string) {
  return kind
    .trim()
    .toLowerCase()
    .replace(/[-\s]+/g, "_");
}

function isStringLikePropertyType(kind: string) {
  const normalized = normalizedPropertyType(kind);
  return (
    ["string", "str", "text", "uuid", "rid", "object_id"].includes(
      normalized,
    ) ||
    normalized.includes("string") ||
    normalized.includes("text")
  );
}

function isGeoPointPropertyType(kind: string) {
  const normalized = normalizedPropertyType(kind);
  return (
    [
      "geopoint",
      "geo_point",
      "point",
      "latlon",
      "lat_lon",
      "latitude_longitude",
    ].includes(normalized) ||
    normalized.includes("geopoint") ||
    normalized.includes("geo_point")
  );
}

function isGeoShapePropertyType(kind: string) {
  const normalized = normalizedPropertyType(kind);
  return (
    [
      "geoshape",
      "geo_shape",
      "geometry",
      "geojson",
      "linestring",
      "line_string",
      "polygon",
      "multipolygon",
    ].includes(normalized) ||
    normalized.includes("geoshape") ||
    normalized.includes("geo_shape") ||
    normalized.includes("geometry") ||
    normalized.includes("geojson")
  );
}


function uniqueNonEmpty(values: string[]) {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const value of values) {
    const trimmed = value.trim();
    if (!trimmed) continue;
    const key = trimmed.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(trimmed);
  }
  return out;
}

export function propertyDisplayMode(property: Partial<Property>) {
  const mode = String(property.display_mode || "normal").toLowerCase();
  return mode === "hidden" || mode === "prominent" ? mode : "normal";
}

export function objectViewVisibleProperties(properties: Property[]) {
  return properties
    .filter((property) => propertyDisplayMode(property) !== "hidden")
    .sort((left, right) => {
      const leftProminent = propertyDisplayMode(left) === "prominent" ? 0 : 1;
      const rightProminent = propertyDisplayMode(right) === "prominent" ? 0 : 1;
      return leftProminent - rightProminent;
    });
}

export function objectViewProminentProperties(properties: Property[]) {
  return properties.filter(
    (property) => propertyDisplayMode(property) === "prominent",
  );
}

export type ProminentPropertyPresentation =
  | "media"
  | "time_series"
  | "map"
  | "card";

export function prominentPropertyPresentation(
  property: Partial<Property>,
): ProminentPropertyPresentation {
  const metadata = propertyTypeMetadata({
    property_type: property.property_type || "string",
    base_type: property.base_type,
    type_family: property.type_family,
    value_shape: property.value_shape,
  });
  const hints = new Set([
    metadata.base_type,
    metadata.type_family,
    metadata.value_shape,
    ...(metadata.semantic_hints || []),
  ].map((hint) => String(hint || "").toLowerCase()));
  if (hints.has("media_reference") || hints.has("media") || hints.has("attachment-reference")) {
    return "media";
  }
  if (hints.has("time_series") || hints.has("timeseries") || hints.has("time-series")) {
    return "time_series";
  }
  if (
    hints.has("geospatial") ||
    hints.has("geopoint") ||
    hints.has("geoshape") ||
    hints.has("geojson-object-or-string") ||
    hints.has("lat-lon-object") ||
    hints.has("geohash")
  ) {
    return "map";
  }
  return "card";
}

export function formatPropertyValue(property: Partial<Property>, value: unknown) {
  const formatting = (property.value_formatting || property.value_type?.formatting || {}) as PropertyValueFormatting;
  if (value === null || value === undefined || value === "") {
    return formatting.empty_value ?? "—";
  }
  const style = formatting.style || propertyTypeMetadata({
    property_type: property.property_type || "string",
  }).value_shape;
  let rendered: string;
  if (["number", "currency", "percent", "decimal"].includes(style)) {
    const numeric = typeof value === "number" ? value : Number(value);
    if (Number.isFinite(numeric)) {
      rendered = new Intl.NumberFormat(formatting.locale, {
        style: style === "currency" ? "currency" : style === "percent" ? "percent" : "decimal",
        currency: formatting.currency || "USD",
        minimumFractionDigits: formatting.minimum_fraction_digits,
        maximumFractionDigits: formatting.maximum_fraction_digits,
      }).format(numeric);
    } else {
      rendered = String(value);
    }
  } else if (["date", "time", "datetime", "date-string", "time-string", "timestamp-string"].includes(style)) {
    const date = new Date(String(value));
    if (!Number.isNaN(date.getTime())) {
      if (style === "date" || style === "date-string") {
        rendered = new Intl.DateTimeFormat(formatting.locale).format(date);
      } else if (style === "time" || style === "time-string") {
        rendered = new Intl.DateTimeFormat(formatting.locale, { timeStyle: "short" }).format(date);
      } else {
        rendered = new Intl.DateTimeFormat(formatting.locale, { dateStyle: "medium", timeStyle: "short" }).format(date);
      }
    } else {
      rendered = String(value);
    }
  } else if (typeof value === "object") {
    rendered = JSON.stringify(value);
  } else {
    rendered = String(value);
  }
  return `${formatting.prefix || ""}${rendered}${formatting.suffix || ""}`;
}

function compareConditionalValue(operator: string, actual: unknown, expected: unknown) {
  switch (operator) {
    case "eq":
      return actual === expected;
    case "neq":
      return actual !== expected;
    case "gt":
      return Number(actual) > Number(expected);
    case "gte":
      return Number(actual) >= Number(expected);
    case "lt":
      return Number(actual) < Number(expected);
    case "lte":
      return Number(actual) <= Number(expected);
    case "contains":
      return String(actual ?? "").includes(String(expected ?? ""));
    case "empty":
      return actual === null || actual === undefined || actual === "";
    case "not_empty":
      return actual !== null && actual !== undefined && actual !== "";
    default:
      return false;
  }
}

export function propertyConditionalStyle(property: Partial<Property>, value: unknown) {
  const rules = Array.isArray(property.conditional_formatting)
    ? property.conditional_formatting
    : [];
  const match = rules.find((rule) =>
    compareConditionalValue(rule.operator || "eq", value, rule.value),
  );
  if (!match) return {};
  return {
    color: match.color,
    background: match.background,
    fontWeight: match.font_weight,
  };
}


function normalizeValueTypeName(value: string) {
  return value
    .trim()
    .replace(/[^a-zA-Z0-9_]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .replace(/_{2,}/g, "_")
    .toLowerCase();
}

function localValueTypesKey(spaceId = "default") {
  return `of:ontology:value-types:${spaceId || "default"}`;
}

function readLocalValueTypes(spaceId = "default"): OntologyValueType[] {
  if (typeof window === "undefined") return [];
  try {
    const raw = window.localStorage.getItem(localValueTypesKey(spaceId));
    const parsed = raw ? (JSON.parse(raw) as OntologyValueType[]) : [];
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function writeLocalValueTypes(items: OntologyValueType[], spaceId = "default") {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(localValueTypesKey(spaceId), JSON.stringify(items));
}

export function listValueTypes(params?: { space_id?: string; search?: string; status?: string }) {
  const spaceId = params?.space_id || "default";
  const query = (params?.search || "").trim().toLowerCase();
  const data = readLocalValueTypes(spaceId).filter((valueType) => {
    const matchesQuery = !query || [valueType.name, valueType.display_name, valueType.description, valueType.semantic_type]
      .some((value) => String(value || "").toLowerCase().includes(query));
    const matchesStatus = !params?.status || valueType.status === params.status;
    return matchesQuery && matchesStatus;
  });
  return Promise.resolve({ data, total: data.length, page: 1, per_page: data.length || 100 });
}

export function createValueType(body: {
  name: string;
  display_name?: string;
  description?: string;
  space_id?: string;
  base_type: string;
  semantic_type?: string;
  constraints?: ValueTypeConstraints;
  formatting?: PropertyValueFormatting | Record<string, unknown>;
  permissions?: ValueTypePermissions;
}) {
  const now = new Date().toISOString();
  const spaceId = body.space_id || "default";
  const name = normalizeValueTypeName(body.name || body.display_name || "value_type");
  const next: OntologyValueType = {
    id: `value-type:${spaceId}:${name}`,
    name,
    display_name: body.display_name || body.name || name,
    description: body.description || "",
    space_id: spaceId,
    base_type: body.base_type,
    semantic_type: body.semantic_type || name,
    constraints: body.constraints || {},
    formatting: body.formatting || {},
    permissions: body.permissions || { viewers: [], appliers: [], editors: [] },
    version: 1,
    versions: [{ version: 1, edit_kind: "non_breaking", note: "Initial version", created_by: "local", created_at: now }],
    status: "active",
    owner_id: "local",
    created_at: now,
    updated_at: now,
  };
  const existing = readLocalValueTypes(spaceId).filter((item) => item.id !== next.id && item.name !== next.name);
  writeLocalValueTypes([next, ...existing], spaceId);
  return Promise.resolve(next);
}

export function updateValueType(
  id: string,
  body: Partial<Omit<OntologyValueType, "id" | "versions" | "created_at">> & { edit_kind?: ValueTypeEditKind; note?: string },
) {
  const spaceId = body.space_id || id.split(":")[1] || "default";
  const items = readLocalValueTypes(spaceId);
  const index = items.findIndex((item) => item.id === id);
  if (index < 0) return Promise.reject(new Error("value type not found"));
  const now = new Date().toISOString();
  const current = items[index];
  const editKind = body.edit_kind || "non_breaking";
  const nextVersion = editKind === "breaking" ? current.version + 1 : current.version;
  const next: OntologyValueType = {
    ...current,
    ...body,
    id: current.id,
    version: nextVersion,
    versions: editKind === "breaking"
      ? [{ version: nextVersion, edit_kind: editKind, note: body.note || "Breaking edit", created_by: "local", created_at: now }, ...current.versions]
      : current.versions,
    updated_at: now,
  };
  items[index] = next;
  writeLocalValueTypes(items, spaceId);
  return Promise.resolve(next);
}

export function deleteValueType(id: string, spaceId = "default") {
  writeLocalValueTypes(readLocalValueTypes(spaceId).filter((item) => item.id !== id), spaceId);
  return Promise.resolve();
}

export function validateValueAgainstValueType(value: unknown, valueType?: OntologyValueType | null): string[] {
  if (!valueType) return [];
  const constraints = valueType.constraints || {};
  const errors: string[] = [];
  if (constraints.required && (value === null || value === undefined || value === "")) errors.push(`${valueType.display_name} is required.`);
  if (value === null || value === undefined || value === "") return errors;
  const numeric = Number(value);
  if (constraints.min !== undefined && Number.isFinite(numeric) && numeric < constraints.min) errors.push(`${valueType.display_name} must be at least ${constraints.min}.`);
  if (constraints.max !== undefined && Number.isFinite(numeric) && numeric > constraints.max) errors.push(`${valueType.display_name} must be at most ${constraints.max}.`);
  const text = String(value);
  if (constraints.min_length !== undefined && text.length < constraints.min_length) errors.push(`${valueType.display_name} must be at least ${constraints.min_length} characters.`);
  if (constraints.max_length !== undefined && text.length > constraints.max_length) errors.push(`${valueType.display_name} must be at most ${constraints.max_length} characters.`);
  if (constraints.regex) {
    try {
      if (!new RegExp(constraints.regex).test(text)) errors.push(`${valueType.display_name} does not match ${constraints.regex}.`);
    } catch {
      errors.push(`${valueType.display_name} has an invalid regular expression constraint.`);
    }
  }
  if (Array.isArray(constraints.allowed_values) && constraints.allowed_values.length > 0) {
    const allowed = constraints.allowed_values.some((candidate) => String(candidate) === text);
    if (!allowed) errors.push(`${valueType.display_name} must be one of ${constraints.allowed_values.join(", ")}.`);
  }
  return errors;
}

export function validatePropertyValueType(property: Partial<Property>, value: unknown) {
  return validateValueAgainstValueType(value, property.value_type);
}

export function valueTypeUsageSummary(
  valueTypeId: string,
  input: { objectTypes?: ObjectType[]; sharedPropertyTypes?: SharedPropertyType[]; interfaces?: OntologyInterface[] },
) {
  const objectProperties = (input.objectTypes || []).flatMap((objectType) =>
    (objectType.properties || [])
      .filter((property) => property.value_type_id === valueTypeId)
      .map((property) => ({ object_type_id: objectType.id, property_name: property.name })),
  );
  const sharedProperties = (input.sharedPropertyTypes || []).filter((property) => property.value_type_id === valueTypeId);
  const interfaceProperties = (input.interfaces || []).flatMap((iface) =>
    (iface.properties || [])
      .filter((property) => property.value_type_id === valueTypeId)
      .map((property) => ({ interface_id: iface.id, property_name: property.name })),
  );
  return {
    object_properties: objectProperties,
    shared_properties: sharedProperties,
    interface_properties: interfaceProperties,
    total: objectProperties.length + sharedProperties.length + interfaceProperties.length,
  };
}

export function sharedPropertyUsageSummary(
  sharedPropertyId: string,
  input: { objectTypes?: ObjectType[]; interfaces?: OntologyInterface[] },
) {
  const objectTypeIds = (input.objectTypes || [])
    .filter((objectType) =>
      (objectType.properties || []).some((property) => property.shared_property_type_id === sharedPropertyId) ||
      (objectType as ObjectType & { shared_property_type_ids?: string[] }).shared_property_type_ids?.includes(sharedPropertyId),
    )
    .map((objectType) => objectType.id);
  const interfaceIds = (input.interfaces || [])
    .filter((iface) => (iface.properties || []).some((property) => property.shared_property_type_id === sharedPropertyId))
    .map((iface) => iface.id);
  return { object_type_ids: objectTypeIds, interface_ids: interfaceIds, total: objectTypeIds.length + interfaceIds.length };
}

export function sharedPropertyImpactWarning(sharedProperty: SharedPropertyType, usage: { total: number }) {
  if (usage.total <= 1) return "";
  return `Editing ${sharedProperty.display_name || sharedProperty.name} affects ${usage.total} object/interface bindings.`;
}

export function listObjectTypes(params?: {
  page?: number;
  per_page?: number;
  search?: string;
}) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  if (params?.search) qs.set("search", params.search);
  return api.get<{
    data: ObjectType[];
    total: number;
    page: number;
    per_page: number;
  }>(`/ontology/types${qs.toString() ? `?${qs}` : ""}`);
}

export function listProperties(typeId: string) {
  return api
    .get<{ data: Property[] }>(`/ontology/types/${typeId}/properties`)
    .then((response) => response.data);
}

// ────────────────────────────────────────────────────────────────
// Object instances + ontology graph + Quiver visual functions
// (used by /quiver). Other ontology surfaces ship with their routes.
// ────────────────────────────────────────────────────────────────

export interface ObjectInstance {
  id: string;
  object_type_id: string;
  properties: Record<string, unknown>;
  created_by: string;
  organization_id?: string | null;
  marking?: string;
  created_at: string;
  updated_at: string;
  object_view_access?: ObjectInstanceViewPolicy;
  object_security_access?: ObjectSecurityPolicyEvaluation;
}

export interface GraphNode {
  id: string;
  kind: string;
  label: string;
  secondary_label: string | null;
  color: string | null;
  route: string | null;
  metadata: Record<string, unknown>;
}

export interface GraphEdge {
  id: string;
  kind: string;
  source: string;
  target: string;
  label: string;
  metadata: Record<string, unknown>;
}

export interface GraphSummary {
  scope: string;
  node_kinds: Record<string, number>;
  edge_kinds: Record<string, number>;
  object_types: Record<string, number>;
  markings: Record<string, number>;
  root_neighbor_count: number;
  max_hops_reached: number;
  boundary_crossings: number;
  sensitive_objects: number;
  sensitive_markings: string[];
}

export interface GraphResponse {
  mode: string;
  root_object_id: string | null;
  root_type_id: string | null;
  depth: number;
  total_nodes: number;
  total_edges: number;
  summary: GraphSummary;
  nodes: GraphNode[];
  edges: GraphEdge[];
}

export type QuiverChartKind = "line" | "area" | "bar" | "point";

export interface QuiverVisualFunction {
  id: string;
  name: string;
  description: string;
  primary_type_id: string;
  secondary_type_id: string | null;
  join_field: string;
  secondary_join_field: string;
  date_field: string;
  metric_field: string;
  group_field: string;
  selected_group: string | null;
  chart_kind: QuiverChartKind;
  shared: boolean;
  vega_spec: Record<string, unknown>;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

export function getObjectType(id: string) {
  return api.get<ObjectType>(`/ontology/types/${id}`);
}

export function executeInlineEdit(
  typeId: string,
  objectId: string,
  propertyId: string,
  body: { value: unknown; justification?: string },
) {
  return api.post<ExecuteActionResponse>(
    `/ontology/types/${typeId}/properties/${propertyId}/objects/${objectId}/inline-edit`,
    body,
  );
}

export interface ObjectQueryFilter {
  property_name: string;
  operator?:
    | "equals"
    | "not_equals"
    | "contains"
    | "gte"
    | "lte"
    | "gt"
    | "lt"
    | "in"
    | "is_empty"
    | "is_not_empty"
    | string;
  value?: unknown;
}

export interface ObjectQuerySort {
  property_name: string;
  direction?: "asc" | "desc" | string;
}

export type ObjectSetAggregationFunction =
  | "count"
  | "sum"
  | "avg"
  | "average"
  | "min"
  | "max"
  | "distinct_count"
  | "approx_distinct"
  | string;

export interface ObjectSetAggregationSpec {
  id?: string;
  alias?: string;
  function: ObjectSetAggregationFunction;
  property_name?: string;
}

export interface ObjectSetAggregationResult {
  id: string;
  alias?: string;
  function: string;
  property_name?: string;
  value: number | null;
  count: number;
}

export interface ObjectQueryBody {
  equals?: Record<string, unknown>;
  filters?: ObjectQueryFilter[];
  limit?: number;
  page?: number;
  per_page?: number;
  sort?: ObjectQuerySort[];
  include_count?: boolean;
  aggregations?: ObjectSetAggregationSpec[];
  selected_object_ids?: string[];
  search_around?: ObjectSearchAroundQuery;
  knn?: ObjectKnnQuery;
}

export interface ObjectSearchAroundQuery {
  source_object_ids: string[];
  link_type_id?: string;
  link_type_ids?: string[];
  direction?: "outgoing" | "incoming" | "both" | string;
  depth?: number;
  target_object_type_id?: string;
}

export interface LinkedObjectEdge {
  link_id: string;
  link_type_id: string;
  source_object_id: string;
  target_object_id: string;
  direction?: "outgoing" | "incoming" | "both" | string;
  depth?: number;
  properties?: Record<string, unknown>;
}

export interface ObjectKnnQuery {
  property_name: string;
  vector: number[];
  k?: number;
  k_value?: number;
  metric?: "cosine" | "euclidean" | "dot" | string;
}

export interface ObjectKnnResult {
  object_id: string;
  rank: number;
  score: number;
  distance: number;
  metric: string;
  property_name: string;
}

export interface ObjectQueryResponse {
  data: ObjectInstance[];
  total: number;
  count?: number;
  page?: number;
  per_page?: number;
  aggregations?: ObjectSetAggregationResult[];
  linked_edges?: LinkedObjectEdge[];
  knn_results?: ObjectKnnResult[];
  object_set?: {
    object_type_id: string;
    filters?: ObjectQueryFilter[];
    sort?: ObjectQuerySort[];
    limit?: number;
    selected_object_ids?: string[];
    search_around?: ObjectSearchAroundQuery | null;
    knn?: ObjectKnnQuery | null;
  };
}

export function queryObjects(typeId: string, body: ObjectQueryBody) {
  return api.post<ObjectQueryResponse>(
    `/ontology/types/${typeId}/objects/query`,
    body,
  );
}

export type SubmissionUserAttr =
  | "user_id"
  | "email"
  | "organization_id"
  | "roles"
  | "permissions"
  | "auth_methods";

export type SubmissionOperator =
  | "is"
  | "is_not"
  | "matches"
  | "lt"
  | "lte"
  | "gt"
  | "gte"
  | "includes"
  | "includes_any"
  | "is_included_in"
  | "each_is"
  | "each_is_not"
  | "is_empty"
  | "is_not_empty";

export type SubmissionOperand =
  | { kind: "static"; value: unknown }
  | { kind: "param"; name: string }
  | { kind: "user"; attr: SubmissionUserAttr };

export type SubmissionNode =
  | {
      type: "leaf";
      left: SubmissionOperand;
      op: SubmissionOperator;
      right?: SubmissionOperand;
    }
  | { type: "all"; children: SubmissionNode[] }
  | { type: "any"; children: SubmissionNode[] };

export interface ObjectRevision {
  id: string;
  object_id: string;
  object_type_id: string;
  operation: "insert" | "update" | "delete" | string;
  properties: Record<string, unknown>;
  marking: string;
  organization_id?: string | null;
  changed_by: string;
  revision_number: number;
  written_at: string;
}

export interface ListObjectRevisionsResponse {
  object_id: string;
  total: number;
  data: ObjectRevision[];
}

export interface RestoreObjectRevisionResponse {
  object: ObjectInstance;
  restored_from_revision_number: number;
  new_revision_number: number;
}

export function listObjectRevisions(
  typeId: string,
  objectId: string,
  params?: { limit?: number },
) {
  const qs = new URLSearchParams();
  if (params?.limit !== undefined) qs.set("limit", String(params.limit));
  const tail = qs.toString() ? `?${qs}` : "";
  return api.get<ListObjectRevisionsResponse>(
    `/ontology/types/${typeId}/objects/${objectId}/revisions${tail}`,
  );
}

export function restoreObjectRevision(
  typeId: string,
  objectId: string,
  revisionNumber: number,
) {
  return api.post<RestoreObjectRevisionResponse>(
    `/ontology/types/${typeId}/objects/${objectId}/revisions/${revisionNumber}/restore`,
    {},
  );
}

export interface InlineEditBatchEntry {
  property_id: string;
  object_id: string;
  value: unknown;
  justification?: string;
}

export interface InlineEditBatchResponse {
  total: number;
  succeeded: number;
  failed: number;
  results: {
    property_id: string;
    object_id: string;
    status: "success" | "failure";
    error?: string;
  }[];
}

export function executeInlineEditBatch(
  typeId: string,
  edits: InlineEditBatchEntry[],
) {
  return api.post<InlineEditBatchResponse>(
    `/ontology/types/${typeId}/inline-edit-batch`,
    { edits },
  );
}

export interface RevertActionExecutionResponse {
  execution_id: string;
  reverted: boolean;
  message: string | null;
}

export function revertActionExecution(executionId: string) {
  return api.post<RevertActionExecutionResponse>(
    `/ontology/actions/executions/${executionId}/revert`,
    {},
  );
}

export function createObject(
  typeId: string,
  body: { properties: Record<string, unknown> },
) {
  return api.post<ObjectInstance>(`/ontology/types/${typeId}/objects`, body);
}

export function deleteObject(typeId: string, objectId: string) {
  return api.delete(`/ontology/types/${typeId}/objects/${objectId}`);
}

export function listObjects(
  typeId: string,
  params?: { page?: number; per_page?: number },
) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  return api.get<{
    data: ObjectInstance[];
    total: number;
    page?: number;
    per_page?: number;
  }>(`/ontology/types/${typeId}/objects${qs.toString() ? `?${qs}` : ""}`);
}

export function getObject(typeId: string, objectId: string) {
  return api.get<ObjectInstance>(
    `/ontology/types/${typeId}/objects/${objectId}`,
  );
}

export function getOntologyGraph(params?: {
  root_object_id?: string;
  root_type_id?: string;
  depth?: number;
  limit?: number;
}) {
  const qs = new URLSearchParams();
  if (params?.root_object_id) qs.set("root_object_id", params.root_object_id);
  if (params?.root_type_id) qs.set("root_type_id", params.root_type_id);
  if (params?.depth) qs.set("depth", String(params.depth));
  if (params?.limit) qs.set("limit", String(params.limit));
  return api.get<GraphResponse>(
    `/ontology/graph${qs.toString() ? `?${qs}` : ""}`,
  );
}

export function listQuiverVisualFunctions(params?: {
  page?: number;
  per_page?: number;
  search?: string;
  include_shared?: boolean;
}) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  if (params?.search) qs.set("search", params.search);
  if (typeof params?.include_shared === "boolean") {
    qs.set("include_shared", params.include_shared ? "true" : "false");
  }
  return api.get<{
    data: QuiverVisualFunction[];
    total: number;
    page: number;
    per_page: number;
  }>(`/ontology/quiver/visual-functions${qs.toString() ? `?${qs}` : ""}`);
}

export function createQuiverVisualFunction(body: {
  name: string;
  description?: string;
  primary_type_id: string;
  secondary_type_id?: string | null;
  join_field: string;
  secondary_join_field?: string;
  date_field: string;
  metric_field: string;
  group_field: string;
  selected_group?: string | null;
  chart_kind?: QuiverChartKind;
  shared?: boolean;
}) {
  return api.post<QuiverVisualFunction>(
    "/ontology/quiver/visual-functions",
    body,
  );
}

export function updateQuiverVisualFunction(
  id: string,
  body: Partial<{
    name: string;
    description: string;
    primary_type_id: string;
    secondary_type_id: string | null;
    join_field: string;
    secondary_join_field: string;
    date_field: string;
    metric_field: string;
    group_field: string;
    selected_group: string | null;
    chart_kind: QuiverChartKind;
    shared: boolean;
  }>,
) {
  return api.patch<QuiverVisualFunction>(
    `/ontology/quiver/visual-functions/${id}`,
    body,
  );
}

export function deleteQuiverVisualFunction(id: string) {
  return api.delete(`/ontology/quiver/visual-functions/${id}`);
}

// ────────────────────────────────────────────────────────────────
// Vertex slice — search, neighbors, scenario simulation. Used by
// /vertex; future ontology routes can extend the same module.
// ────────────────────────────────────────────────────────────────

function compactObjectId(id: string, length = 12) {
  return id.length > length ? `${id.slice(0, length)}...` : id;
}

export function objectViewTitle(object: ObjectInstance | null | undefined, objectType?: ObjectType | null) {
  if (!object) return "Object detail";
  const titleProperty = objectType?.title_property || objectType?.primary_key_property;
  if (titleProperty && object.properties?.[titleProperty] !== undefined && object.properties[titleProperty] !== null && object.properties[titleProperty] !== "") {
    return String(object.properties[titleProperty]);
  }
  for (const key of ["name", "title", "display_name", "label"]) {
    const value = object.properties?.[key];
    if (typeof value === "string" && value.trim()) return value;
  }
  return compactObjectId(object.id);
}

export function objectViewPrimaryKey(object: ObjectInstance | null | undefined, objectType?: ObjectType | null) {
  if (!object) return "-";
  const primaryKeyProperty = objectType?.primary_key_property;
  if (primaryKeyProperty && object.properties?.[primaryKeyProperty] !== undefined && object.properties[primaryKeyProperty] !== null) {
    return String(object.properties[primaryKeyProperty]);
  }
  return object.id;
}

export function objectViewFullHref(objectOrTypeId: ObjectInstance | string, objectId?: string) {
  const typeId = typeof objectOrTypeId === "string" ? objectOrTypeId : objectOrTypeId.object_type_id;
  const id = typeof objectOrTypeId === "string" ? objectId : objectOrTypeId.id;
  return id
    ? `/ontology/${encodeURIComponent(typeId)}?object=${encodeURIComponent(id)}`
    : `/ontology/${encodeURIComponent(typeId)}`;
}

export function objectViewPrimaryKeyProperty(objectType?: ObjectType | null) {
  return objectType?.primary_key_property || objectType?.primary_key || null;
}

export function objectViewPrimaryKeyValue(object: ObjectInstance | null | undefined, objectType?: ObjectType | null) {
  if (!object) return null;
  const property = objectViewPrimaryKeyProperty(objectType);
  if (!property) return null;
  const value = object.properties?.[property];
  if (value === null || value === undefined || value === "") return null;
  return String(value);
}

export interface NeighborLink {
  direction: "inbound" | "outbound";
  link_id: string;
  link_type_id: string;
  link_name: string;
  object: ObjectInstance;
}

export interface LinkedObjectGroup {
  link_type_id: string;
  link_name: string;
  link_type?: LinkType;
  outbound: NeighborLink[];
  inbound: NeighborLink[];
  items: NeighborLink[];
}

export function groupLinkedObjectsByLinkType(
  neighbors: NeighborLink[],
  linkTypes: LinkType[] = [],
): LinkedObjectGroup[] {
  const linkTypeByID = new Map(linkTypes.map((linkType) => [linkType.id, linkType]));
  const groups = new Map<string, LinkedObjectGroup>();
  for (const neighbor of neighbors) {
    const linkType = linkTypeByID.get(neighbor.link_type_id);
    if (linkType?.visibility === "hidden") continue;
    const existing = groups.get(neighbor.link_type_id) ?? {
      link_type_id: neighbor.link_type_id,
      link_name: linkType?.display_name || neighbor.link_name || neighbor.link_type_id,
      link_type: linkType,
      outbound: [],
      inbound: [],
      items: [],
    };
    existing.items.push(neighbor);
    if (neighbor.direction === "outbound") existing.outbound.push(neighbor);
    else existing.inbound.push(neighbor);
    groups.set(neighbor.link_type_id, existing);
  }
  return Array.from(groups.values()).sort((left, right) =>
    left.link_name.localeCompare(right.link_name),
  );
}

export interface SearchResult {
  kind: string;
  id: string;
  object_type_id: string | null;
  title: string;
  subtitle: string | null;
  snippet: string;
  score: number;
  route: string;
  metadata: Record<string, unknown>;
  score_breakdown?: {
    fusion_strategy: string;
    lexical_rank: number | null;
    semantic_rank: number | null;
    lexical_score: number;
    semantic_score: number;
    title_bonus: number;
  };
}

export interface ScenarioSimulationOperation {
  label?: string | null;
  target_object_id?: string | null;
  action_id?: string | null;
  action_parameters?: Record<string, unknown>;
  properties_patch?: Record<string, unknown>;
}

export interface ScenarioSimulationCandidate {
  name: string;
  description?: string | null;
  operations: ScenarioSimulationOperation[];
}

export interface ScenarioSummary {
  impacted_object_count: number;
  changed_object_count: number;
  deleted_object_count: number;
  automatic_rule_matches: number;
  automatic_rule_applications: number;
  advisory_rule_matches: number;
  schedule_count: number;
  impacted_types: string[];
  changed_properties: string[];
  boundary_crossings: number;
  sensitive_objects: number;
  failed_constraints: number;
  achieved_goals: number;
  total_goals: number;
  goal_score: number;
}

export interface ScenarioSimulationResult {
  scenario_id: string;
  name: string;
  description: string | null;
  graph: GraphResponse;
  summary: ScenarioSummary;
}

export interface ObjectScenarioSimulationResponse {
  root_object_id: string;
  root_type_id: string;
  compared_at: string;
  baseline: ScenarioSimulationResult | null;
  scenarios: ScenarioSimulationResult[];
}

export function searchOntology(body: {
  query: string;
  kind?: string;
  object_type_id?: string;
  limit?: number;
  semantic?: boolean;
  hybrid_strategy?: "rrf" | "weighted";
  embedding_provider?: string;
  semantic_candidate_limit?: number;
}) {
  return api.post<{ query: string; total: number; data: SearchResult[] }>(
    "/ontology/search",
    body,
  );
}

export function listNeighbors(typeId: string, objectId: string) {
  return api
    .get<{
      data: NeighborLink[];
    }>(`/ontology/types/${typeId}/objects/${objectId}/neighbors`)
    .then((response) => response.data);
}

export function simulateObjectScenarios(
  typeId: string,
  objectId: string,
  body: {
    scenarios: ScenarioSimulationCandidate[];
    depth?: number;
    include_baseline?: boolean;
  },
) {
  return api.post<ObjectScenarioSimulationResponse>(
    `/ontology/types/${typeId}/objects/${objectId}/scenarios/simulate`,
    body,
  );
}

// ────────────────────────────────────────────────────────────────
// Storage insights — used by /object-databases. Reflects PostgreSQL
// table metrics, index definitions, search projections, and runtime
// activity timestamps from the ontology runtime.
// ────────────────────────────────────────────────────────────────

export interface OntologyStorageTableMetric {
  key: string;
  table_name: string;
  label: string;
  role: string;
  record_count: number;
}

export interface OntologyStorageIndexDefinition {
  table_name: string;
  index_name: string;
  index_definition: string;
}

export interface OntologyStorageDistributionMetric {
  id: string;
  label: string;
  count: number;
}

export interface OntologyStorageSearchKindMetric {
  kind: string;
  count: number;
}

export interface OntologyStorageInsights {
  database_backend: string;
  access_driver: string;
  graph_projection: string;
  search_projection: string;
  funnel_runtime: string;
  table_metrics: OntologyStorageTableMetric[];
  index_definitions: OntologyStorageIndexDefinition[];
  object_type_distribution: OntologyStorageDistributionMetric[];
  link_type_distribution: OntologyStorageDistributionMetric[];
  search_documents_total: number;
  search_documents_by_kind: OntologyStorageSearchKindMetric[];
  latest_object_write_at: string | null;
  latest_link_write_at: string | null;
  latest_funnel_run_at: string | null;
}

export function getOntologyStorageInsights() {
  return api.get<OntologyStorageInsights>("/ontology/storage/insights");
}

// ────────────────────────────────────────────────────────────────
// Link types, interfaces, action types, and projects — used by
// /ontology-design (and incoming routes that touch the broader
// ontology surface).
// ────────────────────────────────────────────────────────────────

export type OntologyAccessMode = "private" | "shared";

export interface OntologyOrganizationVisibility {
  id: string;
  display_name: string;
  marking: string;
}

export interface OntologyLinkedResourceSummary {
  resource_kind: string;
  count: number;
}

export interface OntologySpacePlacement {
  space_slug: string;
  project_id: string | null;
  project_display_name: string;
  folder_path: string;
}

export interface OntologyArtifact {
  id: string;
  api_name: string;
  display_name: string;
  description: string;
  owning_space_slug: string;
  access_mode: OntologyAccessMode;
  organizations: OntologyOrganizationVisibility[];
  placement: OntologySpacePlacement;
  linked_resources: OntologyLinkedResourceSummary[];
  created_at: string | null;
  updated_at: string | null;
}

export interface DeriveOntologyArtifactInput {
  projects: OntologyProject[];
  resourceBindings?: OntologyProjectResourceBinding[];
  objectTypeCount?: number;
  linkTypeCount?: number;
  interfaceCount?: number;
  sharedPropertyTypeCount?: number;
}

export function deriveOntologyArtifact(
  input: DeriveOntologyArtifactInput,
): OntologyArtifact {
  const projects = input.projects;
  const primaryProject = projects[0] ?? null;
  const spaceSlug = primaryProject?.workspace_slug?.trim() || "sandbox";
  const organizations = deriveOntologyOrganizations(projects, spaceSlug);
  const linkedResources = summarizeOntologyLinkedResources(input);

  return {
    id: `ontology.${spaceSlug}`,
    api_name: spaceSlug.replace(/[^a-zA-Z0-9_]/g, "_"),
    display_name:
      primaryProject?.display_name?.trim() ||
      `${titleCaseSlug(spaceSlug)} Ontology`,
    description:
      primaryProject?.description?.trim() ||
      "Space-scoped ontology artifact generated from OpenFoundry project placement and resource bindings.",
    owning_space_slug: spaceSlug,
    access_mode: organizations.length > 1 ? "shared" : "private",
    organizations,
    placement: {
      space_slug: spaceSlug,
      project_id: primaryProject?.id ?? null,
      project_display_name:
        primaryProject?.display_name ||
        primaryProject?.slug ||
        "Sandbox project",
      folder_path: `/${spaceSlug}/ontology`,
    },
    linked_resources: linkedResources,
    created_at: primaryProject?.created_at ?? null,
    updated_at: primaryProject?.updated_at ?? null,
  };
}

export function summarizeOntologyLinkedResources(
  input: DeriveOntologyArtifactInput,
): OntologyLinkedResourceSummary[] {
  const counts = new Map<string, number>();
  const add = (kind: string, count: number | undefined) => {
    if (!count || count < 1) return;
    counts.set(kind, (counts.get(kind) ?? 0) + count);
  };

  add("object_type", input.objectTypeCount);
  add("link_type", input.linkTypeCount);
  add("interface", input.interfaceCount);
  add("shared_property_type", input.sharedPropertyTypeCount);
  for (const binding of input.resourceBindings ?? [])
    add(binding.resource_kind, 1);

  return [...counts.entries()]
    .map(([resource_kind, count]) => ({ resource_kind, count }))
    .sort((a, b) => a.resource_kind.localeCompare(b.resource_kind));
}

function deriveOntologyOrganizations(
  projects: OntologyProject[],
  fallbackSpaceSlug: string,
): OntologyOrganizationVisibility[] {
  const slugs = uniqueNonEmpty(
    projects.map((project) => project.workspace_slug || fallbackSpaceSlug),
  );
  const effectiveSlugs = slugs.length > 0 ? slugs : [fallbackSpaceSlug];
  return effectiveSlugs.map((slug) => ({
    id: `org.${slug}`,
    display_name: `${titleCaseSlug(slug)} organization`,
    marking: slug,
  }));
}

function titleCaseSlug(slug: string) {
  return (
    slug
      .split(/[-_]/g)
      .filter(Boolean)
      .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
      .join(" ") || "Sandbox"
  );
}

export type OntologyResourceRegistryKind =
  | "object_type"
  | "link_type"
  | "action_type"
  | "interface"
  | "shared_property_type"
  | "object_type_group"
  | "core_object_view"
  | "custom_object_view"
  | "datasource_registration";

export interface OntologyObjectTypeGroupSummary {
  id: string;
  name: string;
  display_name: string;
  description?: string;
  visibility?: string;
  status?: string;
  owner_id?: string;
  created_at?: string;
  updated_at?: string;
  object_type_ids?: string[];
  object_type_count?: number;
  project_id?: string | null;
}

export interface OntologyObjectTypeGroup extends OntologyObjectTypeGroupSummary {
  description: string;
  visibility: string;
  status: string;
  owner_id: string;
  created_at: string;
  updated_at: string;
  object_type_ids: string[];
  object_type_count: number;
}

export interface OntologyResourceRegistryEntry {
  id: string;
  resource_kind: OntologyResourceRegistryKind;
  resource_id: string;
  api_name: string;
  display_name: string;
  plural_display_name?: string | null;
  description: string;
  project_id: string | null;
  project_display_name: string;
  folder_path: string;
  visibility: string;
  status: string;
  branch_state: string;
  usage_count: number;
  last_edited_at: string | null;
  last_edited_by: string | null;
  backing_datasource_id?: string | null;
  linked_resource_count: number;
}

export interface BuildOntologyResourceRegistryInput {
  ontology: OntologyArtifact;
  projects: OntologyProject[];
  resourceBindings?: OntologyProjectResourceBinding[];
  objectTypes: ObjectType[];
  linkTypes: LinkType[];
  actionTypes: ActionType[];
  interfaces: OntologyInterface[];
  sharedPropertyTypes: SharedPropertyType[];
  objectTypeGroups?: OntologyObjectTypeGroupSummary[];
  objectViews: ObjectViewDefinition[];
}

export function buildOntologyResourceRegistry(
  input: BuildOntologyResourceRegistryInput,
): OntologyResourceRegistryEntry[] {
  const projectByID = new Map(
    input.projects.map((project) => [project.id, project]),
  );
  const bindingByResource = new Map(
    (input.resourceBindings ?? []).map((binding) => [
      registryBindingKey(binding.resource_kind, binding.resource_id),
      binding,
    ]),
  );
  const objectTypeByID = new Map(
    input.objectTypes.map((objectType) => [objectType.id, objectType]),
  );

  const entries: OntologyResourceRegistryEntry[] = [];
  for (const objectType of input.objectTypes) {
    entries.push(
      registryEntry({
        ontology: input.ontology,
        projectByID,
        binding: bindingByResource.get(
          registryBindingKey("object_type", objectType.id),
        ),
        kind: "object_type",
        resourceID: objectType.id,
        apiName: objectTypeAPIName(objectType),
        displayName: objectType.display_name,
        pluralDisplayName: objectTypePluralDisplayName(objectType),
        description: objectType.description,
        visibility: objectType.visibility,
        status: objectType.status,
        lastEditedAt: objectType.updated_at,
        lastEditedBy: objectType.owner_id,
        usageCount: countObjectTypeUsage(objectType.id, input),
        linkedResourceCount: input.linkTypes.filter(
          (link) =>
            link.source_type_id === objectType.id ||
            link.target_type_id === objectType.id,
        ).length,
        backingDatasourceID:
          objectType.backing_dataset_id ||
          objectType.backing_dataset_rid ||
          null,
      }),
    );
    const datasourceID =
      objectType.backing_dataset_id || objectType.backing_dataset_rid;
    if (datasourceID) {
      entries.push(
        registryEntry({
          ontology: input.ontology,
          projectByID,
          binding: bindingByResource.get(
            registryBindingKey("object_type", objectType.id),
          ),
          kind: "datasource_registration",
          resourceID: `${objectType.id}:${datasourceID}`,
          apiName: `${objectTypeAPIName(objectType)}Datasource`,
          displayName: `${objectType.display_name} datasource`,
          pluralDisplayName: null,
          description: `Backing datasource registration for ${objectType.display_name}.`,
          visibility: objectType.visibility,
          status: objectType.status,
          lastEditedAt: objectType.updated_at,
          lastEditedBy: objectType.owner_id,
          usageCount: 1,
          linkedResourceCount: 1,
          backingDatasourceID: datasourceID,
        }),
      );
    }
  }

  for (const linkType of input.linkTypes) {
    entries.push(
      registryEntry({
        ontology: input.ontology,
        projectByID,
        binding: bindingByResource.get(
          registryBindingKey("link_type", linkType.id),
        ),
        kind: "link_type",
        resourceID: linkType.id,
        apiName: linkType.name,
        displayName: linkType.display_name,
        pluralDisplayName: null,
        description: linkType.description,
        visibility: linkType.visibility || "normal",
        status: "active",
        lastEditedAt: linkType.updated_at,
        lastEditedBy: linkType.owner_id,
        usageCount: 0,
        linkedResourceCount: [
          linkType.source_type_id,
          linkType.target_type_id,
        ].filter((id) => objectTypeByID.has(id)).length,
      }),
    );
  }

  for (const actionType of input.actionTypes) {
    entries.push(
      registryEntry({
        ontology: input.ontology,
        projectByID,
        binding: bindingByResource.get(
          registryBindingKey("action_type", actionType.id),
        ),
        kind: "action_type",
        resourceID: actionType.id,
        apiName: actionType.name,
        displayName: actionType.display_name,
        pluralDisplayName: null,
        description: actionType.description,
        visibility: "normal",
        status: "active",
        lastEditedAt: actionType.updated_at,
        lastEditedBy: actionType.owner_id,
        usageCount: 0,
        linkedResourceCount: actionType.object_type_id ? 1 : 0,
      }),
    );
  }

  for (const iface of input.interfaces) {
    entries.push(
      registryEntry({
        ontology: input.ontology,
        projectByID,
        binding: bindingByResource.get(
          registryBindingKey("interface", iface.id),
        ),
        kind: "interface",
        resourceID: iface.id,
        apiName: iface.name,
        displayName: iface.display_name,
        pluralDisplayName: null,
        description: iface.description,
        visibility: "normal",
        status: "active",
        lastEditedAt: iface.updated_at,
        lastEditedBy: iface.owner_id,
        usageCount: 0,
        linkedResourceCount: 0,
      }),
    );
  }

  for (const shared of input.sharedPropertyTypes) {
    entries.push(
      registryEntry({
        ontology: input.ontology,
        projectByID,
        binding: bindingByResource.get(
          registryBindingKey("shared_property_type", shared.id),
        ),
        kind: "shared_property_type",
        resourceID: shared.id,
        apiName: shared.name,
        displayName: shared.display_name,
        pluralDisplayName: null,
        description: shared.description,
        visibility: "normal",
        status: "active",
        lastEditedAt: shared.updated_at,
        lastEditedBy: shared.owner_id,
        usageCount: 0,
        linkedResourceCount: 0,
      }),
    );
  }

  for (const group of input.objectTypeGroups ?? []) {
    entries.push(
      registryEntry({
        ontology: input.ontology,
        projectByID,
        binding: bindingByResource.get(
          registryBindingKey("object_type_group", group.id),
        ),
        kind: "object_type_group",
        resourceID: group.id,
        apiName: group.name,
        displayName: group.display_name,
        pluralDisplayName: null,
        description: group.description || "",
        visibility: group.visibility,
        status: group.status,
        lastEditedAt: group.updated_at || group.created_at || null,
        lastEditedBy: group.owner_id || null,
        usageCount: group.object_type_count ?? group.object_type_ids?.length ?? 0,
        linkedResourceCount: group.object_type_count ?? group.object_type_ids?.length ?? 0,
      }),
    );
  }

  for (const view of input.objectViews) {
    const kind: OntologyResourceRegistryKind =
      view.mode === "standard" ? "core_object_view" : "custom_object_view";
    entries.push(
      registryEntry({
        ontology: input.ontology,
        projectByID,
        binding: bindingByResource.get(registryBindingKey(kind, view.id)),
        kind,
        resourceID: view.id,
        apiName: view.name,
        displayName: view.display_name || view.name,
        pluralDisplayName: null,
        description: view.description || "",
        visibility: view.published === false ? "hidden" : "normal",
        status: view.status || (view.published === false ? "draft" : "active"),
        lastEditedAt: view.updated_at || view.created_at || null,
        lastEditedBy: view.owner_id || view.created_by || null,
        usageCount: 0,
        linkedResourceCount: view.object_type_id ? 1 : 0,
      }),
    );
  }

  return entries.sort(
    (a, b) =>
      a.resource_kind.localeCompare(b.resource_kind) ||
      a.display_name.localeCompare(b.display_name),
  );
}

function registryBindingKey(kind: string, id: string) {
  return `${kind}:${id}`;
}

function countObjectTypeUsage(
  objectTypeID: string,
  input: BuildOntologyResourceRegistryInput,
) {
  return (
    input.linkTypes.filter(
      (link) =>
        link.source_type_id === objectTypeID ||
        link.target_type_id === objectTypeID,
    ).length +
    input.actionTypes.filter((action) => action.object_type_id === objectTypeID)
      .length +
    input.objectViews.filter((view) => view.object_type_id === objectTypeID)
      .length
  );
}

function registryEntry({
  ontology,
  projectByID,
  binding,
  kind,
  resourceID,
  apiName,
  displayName,
  pluralDisplayName,
  description,
  visibility,
  status,
  lastEditedAt,
  lastEditedBy,
  usageCount,
  linkedResourceCount,
  backingDatasourceID,
}: {
  ontology: OntologyArtifact;
  projectByID: Map<string, OntologyProject>;
  binding?: OntologyProjectResourceBinding;
  kind: OntologyResourceRegistryKind;
  resourceID: string;
  apiName: string;
  displayName: string;
  pluralDisplayName?: string | null;
  description?: string;
  visibility?: string | null;
  status?: string | null;
  lastEditedAt?: string | null;
  lastEditedBy?: string | null;
  usageCount: number;
  linkedResourceCount: number;
  backingDatasourceID?: string | null;
}): OntologyResourceRegistryEntry {
  const project = binding ? projectByID.get(binding.project_id) : null;
  return {
    id: registryBindingKey(kind, resourceID),
    resource_kind: kind,
    resource_id: resourceID,
    api_name: apiName,
    display_name: displayName,
    plural_display_name: pluralDisplayName,
    description: description || "",
    project_id: binding?.project_id ?? ontology.placement.project_id,
    project_display_name:
      project?.display_name ||
      project?.slug ||
      ontology.placement.project_display_name,
    folder_path: ontology.placement.folder_path,
    visibility: visibility || "normal",
    status: status || "active",
    branch_state: "main",
    usage_count: usageCount,
    last_edited_at: lastEditedAt || null,
    last_edited_by: lastEditedBy || null,
    backing_datasource_id: backingDatasourceID,
    linked_resource_count: linkedResourceCount,
  };
}

export type LinkTypeCardinality =
  | "one_to_one"
  | "one_to_many"
  | "many_to_one"
  | "many_to_many";

export interface LinkTypeDatasourceMapping {
  datasource_id?: string | null;
  source_key?: string | null;
  target_key?: string | null;
  source_property?: string | null;
  target_property?: string | null;
}

export interface LinkType {
  id: string;
  name: string;
  display_name: string;
  description: string;
  source_type_id: string;
  target_type_id: string;
  cardinality: LinkTypeCardinality | string;
  label?: string | null;
  reverse_label?: string | null;
  visibility?: string | null;
  link_datasource_mapping?: LinkTypeDatasourceMapping | null;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

export function linkTypeCardinalityLabel(cardinality: string) {
  switch (cardinality) {
    case "one_to_one":
      return "One-to-one";
    case "one_to_many":
      return "One-to-many";
    case "many_to_one":
      return "Many-to-one";
    case "many_to_many":
      return "Many-to-many";
    default:
      return cardinality || "Many-to-many";
  }
}

export function linkTypeEndpointLabels(linkType: LinkType) {
  return {
    forward: linkType.label || linkType.display_name || linkType.name,
    reverse:
      linkType.reverse_label ||
      (linkType.label
        ? `${linkType.label} (reverse)`
        : `Reverse ${linkType.display_name || linkType.name}`),
  };
}

export function linkTypeRequiresDatasourceMapping(
  linkType: Pick<LinkType, "cardinality">,
) {
  return linkType.cardinality === "many_to_many";
}

export function linkTypeHasDatasourceMapping(
  linkType: Pick<LinkType, "cardinality" | "link_datasource_mapping">,
) {
  if (!linkTypeRequiresDatasourceMapping(linkType)) return true;
  const mapping = linkType.link_datasource_mapping;
  return Boolean(
    mapping?.datasource_id && mapping?.source_key && mapping?.target_key,
  );
}

export interface OntologyInterface {
  id: string;
  name: string;
  display_name: string;
  description: string;
  owner_id: string;
  created_at: string;
  updated_at: string;
  properties?: InterfaceProperty[];
}

export type OntologyProjectRole = "discoverer" | "viewer" | "editor" | "owner";

export interface OntologyProject {
  id: string;
  rid?: string;
  slug: string;
  display_name: string;
  description: string;
  workspace_slug: string | null;
  owner_id: string;
  default_role?: OntologyProjectRole;
  point_of_contact_user_id?: string | null;
  point_of_contact_email?: string | null;
  references?: Array<{ kind: string; id: string; label?: string }>;
  marking_rids?: string[];
  propagate_view_requirements_enabled?: boolean;
  propagate_view_requirements_disabled_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface OntologyProjectMembership {
  project_id: string;
  user_id: string;
  role: OntologyProjectRole;
  created_at: string;
  updated_at: string;
}

export type ActionOperationKind =
  | "create_object"
  | "update_object"
  | "modify_object"
  | "create_or_modify_object"
  | "create_link"
  | "delete_link"
  | "delete_object"
  | "invoke_function"
  | "invoke_webhook"
  | "create_interface"
  | "modify_interface"
  | "delete_interface"
  | "create_interface_link"
  | "delete_interface_link";

export interface ActionInputField {
  name: string;
  display_name?: string | null;
  description?: string | null;
  property_type: string;
  value_type_id?: string | null;
  value_type?: OntologyValueType | null;
  shared_property_type_id?: string | null;
  value_formatting?: PropertyValueFormatting | Record<string, unknown>;
  conditional_formatting?: PropertyConditionalFormattingRule[];
  required: boolean;
  default_value?: unknown;
  struct_fields?: ActionInputField[];
}

export interface ActionFormCondition {
  left: string;
  operator: string;
  right?: unknown;
}

export interface ActionFormSectionOverride {
  conditions?: ActionFormCondition[];
  hidden?: boolean;
  columns?: number;
  title?: string | null;
  description?: string | null;
}

export interface ActionFormSection {
  id: string;
  title?: string | null;
  description?: string | null;
  columns?: number;
  collapsible?: boolean;
  visible?: boolean;
  parameter_names?: string[];
  overrides?: ActionFormSectionOverride[];
}

export interface ActionFormParameterOverride {
  parameter_name: string;
  conditions?: ActionFormCondition[];
  hidden?: boolean;
  required?: boolean;
  default_value?: unknown;
  display_name?: string | null;
  description?: string | null;
}

export interface ActionFormSchema {
  sections?: ActionFormSection[];
  parameter_overrides?: ActionFormParameterOverride[];
}

export interface ActionAuthorizationPolicy {
  required_permission_keys?: string[];
  any_role?: string[];
  all_roles?: string[];
  attribute_equals?: Record<string, unknown>;
  allowed_markings?: string[];
  minimum_clearance?: string | null;
  deny_guest_sessions?: boolean;
}

export interface ActionType {
  id: string;
  name: string;
  display_name: string;
  description: string;
  object_type_id: string;
  interface_id?: string | null;
  operation_kind: ActionOperationKind;
  input_schema: ActionInputField[];
  form_schema: ActionFormSchema;
  config: unknown;
  confirmation_required: boolean;
  permission_key: string | null;
  authorization_policy: ActionAuthorizationPolicy;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

export function listLinkTypes(params?: {
  object_type_id?: string;
  page?: number;
  per_page?: number;
}) {
  const qs = new URLSearchParams();
  if (params?.object_type_id) qs.set("object_type_id", params.object_type_id);
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  return api.get<{ data: LinkType[]; total: number }>(`/ontology/links?${qs}`);
}

export function listInterfaces(params?: {
  page?: number;
  per_page?: number;
  search?: string;
}) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  if (params?.search) qs.set("search", params.search);
  return api.get<{
    data: OntologyInterface[];
    total: number;
    page: number;
    per_page: number;
  }>(`/ontology/interfaces?${qs}`);
}


export function actionInterfaceId(action: Partial<ActionType>) {
  const config = action.config && typeof action.config === "object" ? action.config as Record<string, unknown> : {};
  const candidate = action.interface_id ?? config.interface_id ?? config.target_interface_id;
  return typeof candidate === "string" && candidate.trim() ? candidate : null;
}

export function isInterfaceAction(action: Partial<ActionType>) {
  return Boolean(actionInterfaceId(action)) || String(action.operation_kind || "").includes("interface");
}

export function interfaceActionAppliesToObjectType(action: ActionType, implementedInterfaces: OntologyInterface[]) {
  const interfaceId = actionInterfaceId(action);
  return implementedInterfaces.some((iface) =>
    iface.id === interfaceId || (isInterfaceAction(action) && action.object_type_id === iface.id),
  );
}

export function mergeApplicableInterfaceActions(
  objectActions: ActionType[],
  allActions: ActionType[],
  implementedInterfaces: OntologyInterface[],
) {
  const seen = new Set(objectActions.map((action) => action.id));
  const inherited = allActions.filter((action) => interfaceActionAppliesToObjectType(action, implementedInterfaces) && !seen.has(action.id));
  return [...objectActions, ...inherited];
}

function actionConfigTargets(action: ActionType) {
  const config = action.config && typeof action.config === "object" ? action.config as Record<string, unknown> : {};
  const raw = [config.property_name, config.target_property, config.property, config.properties, config.modified_properties, config.changed_properties];
  const directTargets = raw.flatMap((entry) => Array.isArray(entry) ? entry : entry ? [entry] : []).map(String);
  const mappingTargets = Array.isArray(config.property_mappings)
    ? config.property_mappings.flatMap((entry) => {
        if (!entry || typeof entry !== "object") return [];
        const mapping = entry as Record<string, unknown>;
        return typeof mapping.property_name === "string" ? [mapping.property_name] : [];
      })
    : [];
  const staticPatchTargets = config.static_patch && typeof config.static_patch === "object" && !Array.isArray(config.static_patch)
    ? Object.keys(config.static_patch as Record<string, unknown>)
    : [];
  return [...directTargets, ...mappingTargets, ...staticPatchTargets];
}

export function validateInterfaceActionRestrictions(
  action: ActionType,
  input: { interfaceProperties?: InterfaceProperty[]; objectType?: ObjectType | null },
) {
  const warnings: string[] = [];
  const errors: string[] = [];
  if (!isInterfaceAction(action)) return { valid: true, warnings, errors };
  const primaryKey = input.objectType?.primary_key_property || input.objectType?.primary_key || "id";
  const modified = new Set([...action.input_schema.map((field) => field.name), ...actionConfigTargets(action)]);
  if ([...modified].some((name) => name === primaryKey || name.endsWith(`.${primaryKey}`))) {
    errors.push(`Interface action ${action.display_name || action.name} may modify primary-key property ${primaryKey}.`);
  }
  const allowedProperties = new Set((input.interfaceProperties || []).map((property) => property.name));
  const broadModify = action.operation_kind === "modify_interface" || action.operation_kind === "create_interface";
  if (broadModify) {
    for (const name of modified) {
      if (allowedProperties.size > 0 && !allowedProperties.has(name) && name !== primaryKey) {
        warnings.push(`${name} is not declared on the target interface.`);
      }
    }
  }
  return { valid: errors.length === 0, warnings, errors };
}

export function listActionTypes(params?: {
  object_type_id?: string;
  page?: number;
  per_page?: number;
  search?: string;
}) {
  const qs = new URLSearchParams();
  if (params?.object_type_id) qs.set("object_type_id", params.object_type_id);
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  if (params?.search) qs.set("search", params.search);
  return api.get<{
    data: ActionType[];
    total: number;
    page: number;
    per_page: number;
  }>(`/ontology/actions?${qs}`);
}

export interface CreateActionTypeBody {
  name: string;
  display_name?: string;
  description?: string;
  object_type_id: string;
  interface_id?: string | null;
  operation_kind: ActionOperationKind;
  input_schema?: ActionInputField[];
  form_schema?: ActionFormSchema;
  config?: unknown;
  confirmation_required?: boolean;
  permission_key?: string;
  authorization_policy?: ActionAuthorizationPolicy;
}

export interface UpdateActionTypeBody {
  display_name?: string;
  description?: string;
  operation_kind?: ActionOperationKind;
  input_schema?: ActionInputField[];
  form_schema?: ActionFormSchema;
  config?: unknown;
  confirmation_required?: boolean;
  permission_key?: string;
  authorization_policy?: ActionAuthorizationPolicy;
}

export interface ActionWhatIfBranch {
  id: string;
  action_id: string;
  target_object_id: string | null;
  name: string;
  description: string;
  parameters: Record<string, unknown>;
  preview: Record<string, unknown>;
  before_object: Record<string, unknown> | null;
  after_object: Record<string, unknown> | null;
  deleted: boolean;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

export interface ActionExecutionContext {
  surface?: "workshop" | "workshop_action_execution" | "branch_preview" | "approved_automation" | string;
  branch_name?: string;
  preview?: boolean;
  action_execution_id?: string;
  approved_automation_proposal_id?: string;
  source?: string;
}

export interface ValidateActionResponse {
  valid: boolean;
  errors: string[];
  preview: unknown;
}

export interface ExecuteActionResponse {
  action: ActionType;
  target_object_id: string | null;
  deleted: boolean;
  preview: unknown;
  object: unknown | null;
  link: unknown | null;
  result: unknown | null;
}

export interface ExecuteBatchActionResponse {
  action: ActionType;
  total: number;
  succeeded: number;
  failed: number;
  results: Array<Record<string, unknown>>;
}

export interface ActionMetricsResponse {
  action_id: string;
  window: string;
  success_count: number;
  failure_count: number;
  p95_duration_ms: number | null;
  failure_categories: Record<string, number>;
}

export function createActionType(body: CreateActionTypeBody) {
  return api.post<ActionType>("/ontology/actions", body);
}

export function updateActionType(id: string, body: UpdateActionTypeBody) {
  return api.patch<ActionType>(`/ontology/actions/${id}`, body);
}

export function deleteActionType(id: string) {
  return api.delete(`/ontology/actions/${id}`);
}

export function validateAction(
  id: string,
  body: {
    target_object_id?: string;
    parameters?: Record<string, unknown>;
    execution_context?: ActionExecutionContext;
  },
) {
  return api.post<ValidateActionResponse>(
    `/ontology/actions/${id}/validate`,
    body,
  );
}

export function executeAction(
  id: string,
  body: {
    target_object_id?: string;
    parameters?: Record<string, unknown>;
    justification?: string;
    execution_context?: ActionExecutionContext;
  },
) {
  return api.post<ExecuteActionResponse>(
    `/ontology/actions/${id}/execute`,
    body,
  );
}

export function executeActionBatch(
  id: string,
  body: {
    target_object_ids: string[];
    parameters?: Record<string, unknown>;
    justification?: string;
    execution_context?: ActionExecutionContext;
  },
) {
  return api.post<ExecuteBatchActionResponse>(
    `/ontology/actions/${id}/execute-batch`,
    body,
  );
}

export function getActionMetrics(
  actionId: string,
  params?: { window?: string },
) {
  const qs = new URLSearchParams();
  if (params?.window) qs.set("window", params.window);
  const tail = qs.toString();
  return api.get<ActionMetricsResponse>(
    `/ontology/actions/${actionId}/metrics${tail ? `?${tail}` : ""}`,
  );
}

export function listActionWhatIfBranches(
  id: string,
  params?: { target_object_id?: string; page?: number; per_page?: number },
) {
  const qs = new URLSearchParams();
  if (params?.target_object_id)
    qs.set("target_object_id", params.target_object_id);
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  return api.get<{
    data: ActionWhatIfBranch[];
    total: number;
    page: number;
    per_page: number;
  }>(`/ontology/actions/${id}/what-if?${qs}`);
}

export function createActionWhatIfBranch(
  id: string,
  body: {
    target_object_id?: string;
    parameters?: Record<string, unknown>;
    name?: string;
    description?: string;
  },
) {
  return api.post<ActionWhatIfBranch>(`/ontology/actions/${id}/what-if`, body);
}

export function deleteActionWhatIfBranch(id: string, branchId: string) {
  return api.delete(`/ontology/actions/${id}/what-if/${branchId}`);
}

export function listProjects(params?: {
  page?: number;
  per_page?: number;
  search?: string;
}) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  if (params?.search) qs.set("search", params.search);
  return api.get<{
    data: OntologyProject[];
    total: number;
    page: number;
    per_page: number;
  }>(`/ontology/projects?${qs}`);
}

export interface ProjectTemplate {
  id: string;
  key: string;
  name: string;
  description: string;
  space_slug?: string | null;
  default_role?: OntologyProjectRole;
  variables?: Array<{
    key: string;
    label?: string;
    description?: string;
    default_value?: string | null;
    required: boolean;
  }>;
  folder_structure?: Array<{ key?: string; name: string; description?: string; parent_key?: string | null }>;
  generated_groups?: Array<{
    role: OntologyProjectRole;
    slug_suffix?: string;
    display_name_template?: string;
    requestable?: boolean;
  }>;
  markings?: Array<{ display_name: string; marking_id?: string; marking_rid?: string; create_if_missing?: boolean }>;
  constraints?: Array<{ name: string; mode?: string }>;
  governance_tags?: string[];
}

export function listProjectTemplates() {
  return api
    .get<{ data: ProjectTemplate[] }>("/ontology/projects/templates")
    .then((response) => response.data);
}

export function listProjectMemberships(id: string) {
  return api
    .get<{
      data: OntologyProjectMembership[];
    }>(`/ontology/projects/${id}/memberships`)
    .then((response) => response.data);
}

export function createProject(body: {
  slug: string;
  display_name?: string;
  description?: string;
  workspace_slug?: string;
  default_role?: OntologyProjectRole;
  template_key?: string;
  template_variables?: Record<string, string>;
  file_access_preset_id?: string;
  marking_rids?: string[];
  propagate_view_requirements_enabled?: boolean;
  folders?: Array<{
    name: string;
    description?: string;
    parent_folder_id?: string | null;
    parent_folder_rid?: string | null;
    propagate_view_requirements_enabled?: boolean;
    view_requirement_marking_rids?: string[];
  }>;
}) {
  return api.post<OntologyProject>("/ontology/projects", body);
}

export function getProject(id: string) {
  return api.get<OntologyProject>(`/ontology/projects/${id}`);
}

export function updateProject(
  id: string,
  body: {
    display_name?: string;
    description?: string;
    workspace_slug?: string | null;
    propagate_view_requirements_enabled?: boolean;
  },
) {
  return api.patch<OntologyProject>(`/ontology/projects/${id}`, {
    ...body,
    workspace_slug:
      typeof body.workspace_slug === "undefined"
        ? undefined
        : body.workspace_slug,
  });
}

export function deleteProject(id: string) {
  return api.delete(`/ontology/projects/${id}`);
}

export function listProjectFolders(id: string) {
  return api
    .get<{ data: OntologyProjectFolder[] }>(`/ontology/projects/${id}/folders`)
    .then((response) => response.data);
}

export function createProjectFolder(
  id: string,
  body: {
    name: string;
    description?: string;
    parent_folder_id?: string | null;
    parent_folder_rid?: string | null;
    propagate_view_requirements_enabled?: boolean;
    view_requirement_marking_rids?: string[];
  },
) {
  return api.post<OntologyProjectFolder>(
    `/ontology/projects/${id}/folders`,
    body,
  );
}

export function updateProjectFolderPropagation(
  id: string,
  folderId: string,
  body: {
    enabled: boolean;
    view_requirement_marking_rids?: string[];
  },
) {
  return api.patch<OntologyProjectFolder>(
    `/ontology/projects/${id}/folders/${folderId}/propagate-view-requirements`,
    body,
  );
}

export function bindProjectResource(
  id: string,
  body: { resource_kind: string; resource_id: string },
) {
  return api.post<OntologyProjectResourceBinding>(
    `/ontology/projects/${id}/resources`,
    body,
  );
}

export function unbindProjectResource(
  id: string,
  resourceKind: string,
  resourceId: string,
) {
  return api.delete(
    `/ontology/projects/${id}/resources/${resourceKind}/${resourceId}`,
  );
}

export function upsertProjectMembership(
  id: string,
  body: { user_id: string; role: OntologyProjectRole },
) {
  return api.post<OntologyProjectMembership>(
    `/ontology/projects/${id}/memberships`,
    body,
  );
}

export function deleteProjectMembership(id: string, userId: string) {
  return api.delete(`/ontology/projects/${id}/memberships/${userId}`);
}

export function replaceProjectWorkingState(
  id: string,
  changes: OntologyStagedChange[],
) {
  return api.put<OntologyProjectWorkingState>(
    `/ontology/projects/${id}/working-state`,
    { changes },
  );
}

export function getActionType(id: string) {
  return api.get<ActionType>(`/ontology/actions/${id}`);
}

export function getFunctionPackage(id: string) {
  return api.get<FunctionPackage>(`/ontology/functions/${id}`);
}

export function getFunctionPackageMetricsForId(id: string) {
  return api.get<FunctionPackageMetrics>(`/ontology/functions/${id}/metrics`);
}

export function getOntologyFunnelSource(id: string) {
  return api.get<OntologyFunnelSource>(`/ontology/funnel/sources/${id}`);
}

export function getObjectInstance(typeId: string, objectId: string) {
  return api.get<ObjectInstance>(
    `/ontology/types/${typeId}/objects/${objectId}`,
  );
}

// ────────────────────────────────────────────────────────────────
// Object-type bindings (dataset → object type materialization)
// ────────────────────────────────────────────────────────────────

export type ObjectTypeBindingSyncMode = "snapshot" | "incremental" | "view";

export interface ObjectTypeBindingPropertyMapping {
  source_field: string;
  target_property: string;
  transform?: string | null;
  datasource_id?: string | null;
  restricted_view_id?: string | null;
  null_when_inaccessible?: boolean;
}

export interface ObjectTypeBinding {
  id: string;
  object_type_id: string;
  dataset_id: string;
  dataset_branch?: string | null;
  dataset_version?: number | null;
  primary_key_column: string;
  property_mapping: ObjectTypeBindingPropertyMapping[];
  sync_mode: ObjectTypeBindingSyncMode;
  default_marking: string;
  preview_limit: number;
  owner_id: string;
  last_materialized_at?: string | null;
  last_run_status?: string | null;
  last_run_summary?: Record<string, unknown> | null;
  created_at: string;
  updated_at: string;
}

export interface MaterializeBindingResponse {
  binding_id: string;
  status: string;
  rows_read: number;
  inserted: number;
  updated: number;
  skipped: number;
  errors: number;
  dry_run: boolean;
  error_details?: unknown[];
}


export function bindingDatasourceProvenance(bindings: ObjectTypeBinding[]) {
  const provenance = new Map<string, Array<{ binding_id: string; dataset_id: string; source_field: string; restricted_view_id?: string | null; null_when_inaccessible?: boolean }>>();
  for (const binding of bindings) {
    for (const mapping of binding.property_mapping) {
      const entries = provenance.get(mapping.target_property) ?? [];
      entries.push({
        binding_id: binding.id,
        dataset_id: mapping.datasource_id || binding.dataset_id,
        source_field: mapping.source_field,
        restricted_view_id: mapping.restricted_view_id,
        null_when_inaccessible: mapping.null_when_inaccessible,
      });
      provenance.set(mapping.target_property, entries);
    }
  }
  return provenance;
}

export function validateMultiDatasourcePrimaryKeys(bindings: ObjectTypeBinding[]) {
  const columns = Array.from(new Set(bindings.map((binding) => binding.primary_key_column).filter(Boolean)));
  return {
    valid: columns.length <= 1,
    primary_key_columns: columns,
    errors: columns.length > 1 ? [`Primary key columns must be consistent across datasources: ${columns.join(", ")}.`] : [],
    unsupported: ["Row-wise MDO semantics are unsupported; use restricted views for row-level filtering."],
  };
}

export function maskObjectPropertiesForDatasourceAccess(
  object: ObjectInstance,
  bindings: ObjectTypeBinding[],
  accessibleDatasourceIds: string[],
) {
  const accessible = new Set(accessibleDatasourceIds);
  const provenance = bindingDatasourceProvenance(bindings);
  const properties = { ...(object.properties || {}) };
  for (const [propertyName, sources] of provenance) {
    const allInaccessible = sources.length > 0 && sources.every((source) => !accessible.has(source.dataset_id));
    const shouldNull = sources.some((source) => source.null_when_inaccessible);
    if (allInaccessible && shouldNull) properties[propertyName] = null;
  }
  return { ...object, properties };
}

export function listObjectTypeBindings(typeId: string) {
  return api.get<{ data: ObjectTypeBinding[] }>(
    `/ontology/types/${typeId}/bindings`,
  );
}

export function createObjectTypeBinding(
  typeId: string,
  body: {
    dataset_id: string;
    dataset_branch?: string;
    dataset_version?: number;
    primary_key_column: string;
    property_mapping: ObjectTypeBindingPropertyMapping[];
    sync_mode: ObjectTypeBindingSyncMode;
    default_marking?: string;
    preview_limit?: number;
  },
) {
  return api.post<ObjectTypeBinding>(
    `/ontology/types/${typeId}/bindings`,
    body,
  );
}

export function updateObjectTypeBinding(
  typeId: string,
  bindingId: string,
  body: Partial<{
    dataset_branch: string;
    dataset_version: number;
    primary_key_column: string;
    property_mapping: ObjectTypeBindingPropertyMapping[];
    sync_mode: ObjectTypeBindingSyncMode;
    default_marking: string;
    preview_limit: number;
  }>,
) {
  return api.patch<ObjectTypeBinding>(
    `/ontology/types/${typeId}/bindings/${bindingId}`,
    body,
  );
}

export function deleteObjectTypeBinding(typeId: string, bindingId: string) {
  return api.delete(`/ontology/types/${typeId}/bindings/${bindingId}`);
}

export function materializeObjectTypeBinding(
  typeId: string,
  bindingId: string,
  body?: {
    dataset_branch?: string;
    dataset_version?: number;
    limit?: number;
    dry_run?: boolean;
  },
) {
  return api.post<MaterializeBindingResponse>(
    `/ontology/types/${typeId}/bindings/${bindingId}/materialize`,
    body ?? {},
  );
}

// ────────────────────────────────────────────────────────────────
// Rules + machinery — used by /dynamic-scheduling.
// ────────────────────────────────────────────────────────────────

export type RuleEvaluationMode = "advisory" | "automatic";

export interface RuleTriggerSpec {
  equals?: Record<string, unknown>;
  numeric_gte?: Record<string, number>;
  numeric_lte?: Record<string, number>;
  exists?: string[];
  changed_properties?: string[];
  markings?: string[];
}

export interface RuleAlertSpec {
  severity: "low" | "medium" | "high" | "critical";
  title: string;
  message?: string | null;
}

export interface RuleScheduleSpec {
  property_name: string;
  offset_hours: number;
  priority_score?: number;
  estimated_duration_minutes?: number;
  required_capability?: string | null;
  constraint_tags?: string[];
  hard_deadline_hours?: number | null;
}

export interface RuleEffectSpec {
  object_patch?: Record<string, unknown> | null;
  schedule?: RuleScheduleSpec | null;
  alert?: RuleAlertSpec | null;
}

export interface OntologyRule {
  id: string;
  name: string;
  display_name: string;
  description: string;
  object_type_id: string;
  evaluation_mode: RuleEvaluationMode;
  trigger_spec: RuleTriggerSpec;
  effect_spec: RuleEffectSpec;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

export interface MachineryInsight {
  rule_id: string;
  name: string;
  display_name: string;
  evaluation_mode: RuleEvaluationMode;
  matched_runs: number;
  total_runs: number;
  pending_schedules: number;
  overdue_schedules: number;
  avg_schedule_lead_hours: number | null;
  dynamic_pressure: string;
  last_matched_at: string | null;
  last_object_id: string | null;
}

export interface MachineryQueueItem {
  id: string;
  rule_id: string;
  rule_run_id: string;
  object_id: string;
  rule_name: string;
  rule_display_name: string;
  object_type_id: string;
  status: string;
  scheduled_for: string;
  priority_score: number;
  estimated_duration_minutes: number;
  required_capability: string | null;
  constraint_snapshot: Record<string, unknown>;
  created_by: string;
  created_at: string;
  updated_at: string;
  started_at: string | null;
  completed_at: string | null;
}

export interface MachineryCapabilityLoad {
  capability: string;
  pending_count: number;
  total_estimated_minutes: number;
}

export interface MachineryQueueRecommendation {
  generated_at: string;
  strategy: string;
  queue_depth: number;
  overdue_count: number;
  total_estimated_minutes: number;
  next_due_at: string | null;
  recommended_order: string[];
  capability_load: MachineryCapabilityLoad[];
}

export interface MachineryQueueResponse {
  object_type_id: string | null;
  data: MachineryQueueItem[];
  recommendation: MachineryQueueRecommendation;
}

export function listRules(params?: {
  object_type_id?: string;
  search?: string;
  page?: number;
  per_page?: number;
}) {
  const qs = new URLSearchParams();
  if (params?.object_type_id) qs.set("object_type_id", params.object_type_id);
  if (params?.search) qs.set("search", params.search);
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  return api.get<{
    data: OntologyRule[];
    total: number;
    page: number;
    per_page: number;
  }>(`/ontology/rules?${qs}`);
}

export function getMachineryInsights(params?: { object_type_id?: string }) {
  const qs = new URLSearchParams();
  if (params?.object_type_id) qs.set("object_type_id", params.object_type_id);
  return api.get<{ object_type_id: string | null; data: MachineryInsight[] }>(
    `/ontology/rules/insights?${qs}`,
  );
}

export function getMachineryQueue(params?: { object_type_id?: string }) {
  const qs = new URLSearchParams();
  if (params?.object_type_id) qs.set("object_type_id", params.object_type_id);
  return api.get<MachineryQueueResponse>(
    `/ontology/rules/machinery/queue?${qs}`,
  );
}

export function updateMachineryQueueItem(id: string, body: { status: string }) {
  return api.patch<MachineryQueueItem>(
    `/ontology/rules/machinery/queue/${id}`,
    body,
  );
}

// ────────────────────────────────────────────────────────────────
// Interface mutations + bindings — used by /interfaces.
// ────────────────────────────────────────────────────────────────

export interface InterfaceProperty {
  id: string;
  interface_id: string;
  name: string;
  display_name: string;
  description: string;
  property_type: string;
  value_type_id?: string | null;
  value_type?: OntologyValueType | null;
  shared_property_type_id?: string | null;
  value_formatting?: PropertyValueFormatting | Record<string, unknown>;
  conditional_formatting?: PropertyConditionalFormattingRule[];
  required: boolean;
  unique_constraint: boolean;
  time_dependent: boolean;
  default_value: unknown;
  validation_rules: unknown;
  created_at: string;
  updated_at: string;
}


export interface InterfaceExtensionBinding {
  interface_id: string;
  extends_interface_id: string;
  created_at: string;
}

export interface InterfacePropertyMapping {
  interface_property_id: string;
  interface_property_name: string;
  object_property_name: string;
}

export interface InterfaceLinkImplementationMapping {
  constraint_id: string;
  link_type_id: string;
}

export interface InterfaceImplementationDetail {
  interface_id: string;
  object_type_id: string;
  property_mappings: InterfacePropertyMapping[];
  link_mappings: InterfaceLinkImplementationMapping[];
  updated_at: string;
}

export type InterfaceLinkTargetKind = "object_type" | "interface";
export type InterfaceLinkConstraintCardinality = "one" | "many";

export interface InterfaceLinkConstraint {
  id: string;
  interface_id: string;
  api_name: string;
  display_name: string;
  description: string;
  target_kind: InterfaceLinkTargetKind;
  target_id: string;
  cardinality: InterfaceLinkConstraintCardinality;
  required: boolean;
  created_at: string;
  updated_at: string;
}

export interface ObjectTypeInterfaceBinding {
  object_type_id: string;
  interface_id: string;
  created_at: string;
}


function interfaceLocalKey(kind: string, id: string) {
  return `of:ontology:interface:${kind}:${id}`;
}

function readInterfaceLocalList<T>(kind: string, id: string): T[] {
  if (typeof window === "undefined") return [];
  try {
    const raw = window.localStorage.getItem(interfaceLocalKey(kind, id));
    const parsed = raw ? (JSON.parse(raw) as T[]) : [];
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function writeInterfaceLocalList<T>(kind: string, id: string, items: T[]) {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(interfaceLocalKey(kind, id), JSON.stringify(items));
}

export function listInterfaceExtensions(interfaceId: string) {
  return Promise.resolve(readInterfaceLocalList<InterfaceExtensionBinding>("extensions", interfaceId));
}

export function addInterfaceExtension(interfaceId: string, extendsInterfaceId: string) {
  const now = new Date().toISOString();
  const existing = readInterfaceLocalList<InterfaceExtensionBinding>("extensions", interfaceId)
    .filter((entry) => entry.extends_interface_id !== extendsInterfaceId);
  const next = { interface_id: interfaceId, extends_interface_id: extendsInterfaceId, created_at: now };
  writeInterfaceLocalList("extensions", interfaceId, [next, ...existing]);
  return Promise.resolve(next);
}

export function removeInterfaceExtension(interfaceId: string, extendsInterfaceId: string) {
  writeInterfaceLocalList(
    "extensions",
    interfaceId,
    readInterfaceLocalList<InterfaceExtensionBinding>("extensions", interfaceId)
      .filter((entry) => entry.extends_interface_id !== extendsInterfaceId),
  );
  return Promise.resolve();
}

export function listInterfaceLinkConstraints(interfaceId: string) {
  return Promise.resolve(readInterfaceLocalList<InterfaceLinkConstraint>("link-constraints", interfaceId));
}

export function createInterfaceLinkConstraint(interfaceId: string, body: Omit<InterfaceLinkConstraint, "id" | "interface_id" | "created_at" | "updated_at">) {
  const now = new Date().toISOString();
  const constraint: InterfaceLinkConstraint = {
    ...body,
    id: `interface-link:${interfaceId}:${body.api_name}`,
    interface_id: interfaceId,
    created_at: now,
    updated_at: now,
  };
  const existing = readInterfaceLocalList<InterfaceLinkConstraint>("link-constraints", interfaceId)
    .filter((entry) => entry.id !== constraint.id && entry.api_name !== constraint.api_name);
  writeInterfaceLocalList("link-constraints", interfaceId, [constraint, ...existing]);
  return Promise.resolve(constraint);
}

export function deleteInterfaceLinkConstraint(interfaceId: string, constraintId: string) {
  writeInterfaceLocalList(
    "link-constraints",
    interfaceId,
    readInterfaceLocalList<InterfaceLinkConstraint>("link-constraints", interfaceId).filter((entry) => entry.id !== constraintId),
  );
  return Promise.resolve();
}

export function listInterfaceImplementationDetails(interfaceId: string) {
  return Promise.resolve(readInterfaceLocalList<InterfaceImplementationDetail>("implementations", interfaceId));
}

export function upsertInterfaceImplementationDetail(detail: InterfaceImplementationDetail) {
  const next = { ...detail, updated_at: new Date().toISOString() };
  const existing = readInterfaceLocalList<InterfaceImplementationDetail>("implementations", detail.interface_id)
    .filter((entry) => entry.object_type_id !== detail.object_type_id);
  writeInterfaceLocalList("implementations", detail.interface_id, [next, ...existing]);
  return Promise.resolve(next);
}

export function validateInterfaceImplementation(input: {
  properties: InterfaceProperty[];
  linkConstraints: InterfaceLinkConstraint[];
  implementation?: InterfaceImplementationDetail | null;
}) {
  const propertyMappings = input.implementation?.property_mappings ?? [];
  const linkMappings = input.implementation?.link_mappings ?? [];
  const missingProperties = input.properties
    .filter((property) => property.required)
    .filter((property) => !propertyMappings.some((mapping) => mapping.interface_property_id === property.id && mapping.object_property_name.trim()));
  const missingLinks = input.linkConstraints
    .filter((constraint) => constraint.required)
    .filter((constraint) => !linkMappings.some((mapping) => mapping.constraint_id === constraint.id && mapping.link_type_id.trim()));
  return {
    valid: missingProperties.length === 0 && missingLinks.length === 0,
    missing_properties: missingProperties,
    missing_link_constraints: missingLinks,
  };
}

export function createInterface(body: {
  name: string;
  display_name?: string;
  description?: string;
}) {
  return api.post<OntologyInterface>("/ontology/interfaces", body);
}

export function getInterface(id: string) {
  return api.get<OntologyInterface>(`/ontology/interfaces/${id}`);
}

export function updateInterface(
  id: string,
  body: { display_name?: string; description?: string },
) {
  return api.patch<OntologyInterface>(`/ontology/interfaces/${id}`, body);
}

export function deleteInterface(id: string) {
  return api.delete(`/ontology/interfaces/${id}`);
}

export function listInterfaceProperties(id: string) {
  return api
    .get<{ data: InterfaceProperty[] }>(`/ontology/interfaces/${id}/properties`)
    .then((response) => response.data);
}

export function createInterfaceProperty(
  id: string,
  body: {
    name: string;
    display_name?: string;
    description?: string;
    property_type: string;
    value_type_id?: string | null;
    required?: boolean;
    unique_constraint?: boolean;
    time_dependent?: boolean;
    default_value?: unknown;
    validation_rules?: unknown;
  },
) {
  return api.post<InterfaceProperty>(
    `/ontology/interfaces/${id}/properties`,
    body,
  );
}

export function updateInterfaceProperty(
  id: string,
  propertyId: string,
  body: {
    display_name?: string;
    description?: string;
    value_type_id?: string | null;
    required?: boolean;
    unique_constraint?: boolean;
    time_dependent?: boolean;
    default_value?: unknown;
    validation_rules?: unknown;
  },
) {
  return api.patch<InterfaceProperty>(
    `/ontology/interfaces/${id}/properties/${propertyId}`,
    body,
  );
}

export function deleteInterfaceProperty(id: string, propertyId: string) {
  return api.delete(`/ontology/interfaces/${id}/properties/${propertyId}`);
}

export function listTypeInterfaces(typeId: string) {
  return api
    .get<{ data: OntologyInterface[] }>(`/ontology/types/${typeId}/interfaces`)
    .then((response) => response.data);
}

export function implementInterface(
  interfaceId: string,
  body: { object_type_id: string },
) {
  return api.post<ObjectTypeInterfaceBinding>(
    `/ontology/interfaces/${interfaceId}/implementations`,
    body,
  );
}

export function attachInterfaceToType(typeId: string, interfaceId: string) {
  return implementInterface(interfaceId, { object_type_id: typeId });
}

export function detachInterfaceFromType(typeId: string, interfaceId: string) {
  return api.delete(`/ontology/types/${typeId}/interfaces/${interfaceId}`);
}

export function updateObject(
  typeId: string,
  objectId: string,
  body: {
    properties: Record<string, unknown>;
    replace?: boolean;
    marking?: string;
  },
) {
  return api.patch<ObjectInstance>(
    `/ontology/types/${typeId}/objects/${objectId}`,
    body,
  );
}

// ────────────────────────────────────────────────────────────────
// Object views, rules, simulations — used by /object-views,
// /object-explorer, /foundry-rules, /machinery, etc.
// ────────────────────────────────────────────────────────────────

export interface RuleMatchResponse {
  rule_id: string;
  matched: boolean;
  trigger_payload: Record<string, unknown>;
  effect_preview: Record<string, unknown> | null;
}

export interface OntologyRuleRun {
  id: string;
  rule_id: string;
  object_id: string;
  matched: boolean;
  simulated: boolean;
  trigger_payload: Record<string, unknown>;
  effect_preview: Record<string, unknown> | null;
  created_by: string;
  created_at: string;
}

export interface ObjectViewResponse {
  object: ObjectInstance;
  summary: Record<string, unknown>;
  neighbors: NeighborLink[];
  graph: GraphResponse;
  applicable_actions: ActionType[];
  matching_rules: RuleMatchResponse[];
  recent_rule_runs: OntologyRuleRun[];
  timeline: Array<Record<string, unknown>>;
}

export function getObjectView(typeId: string, objectId: string) {
  return api.get<ObjectViewResponse>(
    `/ontology/types/${typeId}/objects/${objectId}/view`,
  );
}

export function objectCommentThreadKey(
  objectTypeID: string,
  objectID: string,
  surface: Exclude<ObjectCommentSurface, "workshop_widget"> = "object_view",
) {
  return `object-comments:${surface}:${objectTypeID}:${objectID}`;
}

export function extractObjectCommentMentions(body: string): ObjectCommentMention[] {
  const matches = body.matchAll(/(^|\s)@([A-Za-z0-9._-]{2,80})/g);
  const byHandle = new Map<string, ObjectCommentMention>();
  for (const match of matches) {
    const handle = match[2].trim();
    const normalized = handle.toLowerCase();
    if (byHandle.has(normalized)) continue;
    byHandle.set(normalized, {
      id: normalized,
      handle,
      display_name: handle.replace(/[._-]+/g, " "),
    });
  }
  return Array.from(byHandle.values());
}

export function normalizeObjectCommentAttachments(
  attachments: Array<Partial<ObjectCommentAttachment> & { name: string }>,
): ObjectCommentAttachment[] {
  return attachments
    .filter((attachment) => attachment.name.trim().length > 0)
    .map((attachment, index) => {
      const name = attachment.name.trim();
      const lower = name.toLowerCase();
      const mimeType = attachment.mime_type || (
        /\.(png|jpg|jpeg|gif|webp|svg)$/.test(lower) ? `image/${lower.split(".").pop() === "jpg" ? "jpeg" : lower.split(".").pop()}` : "application/octet-stream"
      );
      const kind: ObjectCommentAttachmentKind = attachment.kind || (mimeType.startsWith("image/") ? "image" : "file");
      return {
        id: attachment.id || `attachment-${index + 1}-${safeObjectCommentIDPart(name)}`,
        kind,
        name,
        mime_type: mimeType,
        size_bytes: Math.max(0, Math.round(Number(attachment.size_bytes ?? 0))),
        url: attachment.url ?? null,
      };
    });
}

export function buildObjectCommentPermissionDecision(input: {
  objectType?: ObjectType | null;
  object?: ObjectInstance | null;
  accessPolicy?: ObjectInstanceViewPolicy | null;
  principal?: OntologyPermissionPrincipal | null;
  commentsEnabled?: boolean;
  surface?: ObjectCommentSurface;
}): ObjectCommentPermissionDecision {
  const principal = input.principal || {};
  const policy = input.accessPolicy || buildObjectInstanceViewPolicy({
    objectType: input.objectType,
    principal,
  });
  const commentsEnabled = input.commentsEnabled !== false;
  const isWorkshopWidget = input.surface === "workshop_widget";
  const canManage = objectCommentCanManage(principal);
  const canView = commentsEnabled && !isWorkshopWidget && policy.can_view_instances;
  const canComment = canView && (policy.can_view_instances || canManage);
  return {
    can_view: canView,
    can_comment: canComment,
    can_edit_own: canComment,
    can_delete_own: canComment,
    can_manage: canManage,
    reason: !commentsEnabled
      ? "Object comments are disabled for this Object View."
      : isWorkshopWidget
      ? "Workshop Comment widgets use a separate comment surface from Object Explorer object comments."
      : policy.can_view_instances
      ? "Object comments are available for users who can view this object instance."
      : policy.reason,
    object_explorer_distinct_from_workshop: !isWorkshopWidget,
  };
}

export function buildObjectCommentThread(input: {
  objectType?: ObjectType | null;
  object?: ObjectInstance | null;
  objectId?: string | null;
  comments?: ObjectCommentEntry[];
  activity?: ObjectCommentActivityEvent[];
  notifications?: ObjectCommentNotification[];
  principal?: OntologyPermissionPrincipal | null;
  accessPolicy?: ObjectInstanceViewPolicy | null;
  commentsEnabled?: boolean;
  surface?: Exclude<ObjectCommentSurface, "workshop_widget">;
  now?: string;
}): ObjectCommentThread {
  const objectTypeID = input.objectType?.id || input.object?.object_type_id || "";
  const objectID = input.object?.id || input.objectId || "";
  const surface = input.surface || "object_view";
  const now = input.now || new Date().toISOString();
  const id = objectCommentThreadKey(objectTypeID, objectID, surface);
  const permissions = buildObjectCommentPermissionDecision({
    objectType: input.objectType,
    object: input.object,
    accessPolicy: input.accessPolicy,
    principal: input.principal,
    commentsEnabled: input.commentsEnabled,
    surface,
  });
  const activity = input.activity && input.activity.length > 0
    ? input.activity
    : [{
        id: `${id}:activity:thread-created`,
        kind: "thread_created" as const,
        actor_id: "system",
        comment_id: null,
        timestamp: now,
        message: "Object-scoped comment thread initialized.",
      }];
  return {
    id,
    object_type_id: objectTypeID,
    object_id: objectID,
    scope: "object",
    surface,
    comments: input.comments || [],
    activity,
    notifications: input.notifications || [],
    permissions,
    created_at: activity[0]?.timestamp || now,
    updated_at: activity[activity.length - 1]?.timestamp || now,
    workshop_widget_thread_id: `workshop-comments:${objectTypeID}:${objectID}`,
  };
}

export function objectCommentEntryPermissions(
  thread: ObjectCommentThread,
  comment: ObjectCommentEntry,
  principal?: OntologyPermissionPrincipal | null,
) {
  const actorID = objectCommentActorID(principal || {});
  const own = comment.author_id === actorID;
  const active = !comment.deleted_at;
  return {
    can_edit: active && thread.permissions.can_edit_own && own,
    can_delete: active && ((thread.permissions.can_delete_own && own) || thread.permissions.can_manage),
  };
}

export function appendObjectComment(
  thread: ObjectCommentThread,
  input: {
    body: string;
    principal?: OntologyPermissionPrincipal | null;
    authorDisplayName?: string | null;
    attachments?: Array<Partial<ObjectCommentAttachment> & { name: string }>;
    now?: string;
  },
): ObjectCommentMutationResult {
  if (!thread.permissions.can_comment) {
    return { thread, notifications: [], error: thread.permissions.reason };
  }
  const body = input.body.trim();
  if (!body) return { thread, notifications: [], error: "Comment body is required." };
  const now = input.now || new Date().toISOString();
  const actorID = objectCommentActorID(input.principal || {});
  const mentions = extractObjectCommentMentions(body);
  const attachments = normalizeObjectCommentAttachments(input.attachments || []);
  const comment: ObjectCommentEntry = {
    id: `${thread.id}:comment:${thread.comments.length + 1}`,
    thread_id: thread.id,
    object_type_id: thread.object_type_id,
    object_id: thread.object_id,
    body,
    author_id: actorID,
    author_display_name: input.authorDisplayName || actorID,
    mentions,
    attachments,
    source_surface: thread.surface,
    created_at: now,
    updated_at: now,
    edited_at: null,
    deleted_at: null,
    deleted_by: null,
  };
  const notifications = objectCommentNotifications(thread, comment);
  const activity = [...thread.activity];
  const pushActivity = (kind: ObjectCommentActivityKind, message: string) => {
    activity.push(objectCommentActivity({ ...thread, activity }, kind, actorID, now, comment.id, message));
  };
  pushActivity("comment_created", "Comment added from Object View comments helper.");
  for (const mention of mentions) pushActivity("mention_added", `Mentioned @${mention.handle}.`);
  for (const attachment of attachments) pushActivity("attachment_added", `Attached ${attachment.kind} ${attachment.name}.`);
  for (const notification of notifications) pushActivity("notification_queued", `Queued notification for ${notification.recipient_id}.`);
  return {
    thread: {
      ...thread,
      comments: [...thread.comments, comment],
      notifications: [...thread.notifications, ...notifications],
      activity,
      updated_at: now,
    },
    comment,
    notifications,
  };
}

export function editObjectComment(
  thread: ObjectCommentThread,
  input: {
    commentId: string;
    body: string;
    principal?: OntologyPermissionPrincipal | null;
    now?: string;
  },
): ObjectCommentMutationResult {
  const now = input.now || new Date().toISOString();
  const actorID = objectCommentActorID(input.principal || {});
  const body = input.body.trim();
  if (!body) return { thread, notifications: [], error: "Comment body is required." };
  const comment = thread.comments.find((entry) => entry.id === input.commentId);
  if (!comment) return { thread, notifications: [], error: "Comment not found." };
  const permissions = objectCommentEntryPermissions(thread, comment, input.principal);
  if (!permissions.can_edit) return { thread, notifications: [], error: "Only the author can edit this active comment." };
  const mentions = extractObjectCommentMentions(body);
  const updatedComment = {
    ...comment,
    body,
    mentions,
    updated_at: now,
    edited_at: now,
  };
  return {
    thread: {
      ...thread,
      comments: thread.comments.map((entry) => entry.id === input.commentId ? updatedComment : entry),
      activity: [
        ...thread.activity,
        objectCommentActivity(thread, "comment_edited", actorID, now, input.commentId, "Comment edited."),
      ],
      updated_at: now,
    },
    comment: updatedComment,
    notifications: [],
  };
}

export function deleteObjectComment(
  thread: ObjectCommentThread,
  input: {
    commentId: string;
    principal?: OntologyPermissionPrincipal | null;
    now?: string;
  },
): ObjectCommentMutationResult {
  const now = input.now || new Date().toISOString();
  const actorID = objectCommentActorID(input.principal || {});
  const comment = thread.comments.find((entry) => entry.id === input.commentId);
  if (!comment) return { thread, notifications: [], error: "Comment not found." };
  const permissions = objectCommentEntryPermissions(thread, comment, input.principal);
  if (!permissions.can_delete) return { thread, notifications: [], error: "Only the author or comment manager can delete this active comment." };
  const deletedComment = {
    ...comment,
    body: "",
    attachments: [],
    mentions: [],
    updated_at: now,
    deleted_at: now,
    deleted_by: actorID,
  };
  return {
    thread: {
      ...thread,
      comments: thread.comments.map((entry) => entry.id === input.commentId ? deletedComment : entry),
      activity: [
        ...thread.activity,
        objectCommentActivity(thread, "comment_deleted", actorID, now, input.commentId, "Comment deleted."),
      ],
      updated_at: now,
    },
    comment: deletedComment,
    notifications: [],
  };
}

export type ObjectViewMode = "standard" | "configured";
export type ObjectViewFormFactor = "full" | "panel";
export type ObjectCommentSurface = "object_view" | "object_explorer" | "workshop_widget";
export type ObjectCommentAttachmentKind = "file" | "image";
export type ObjectCommentActivityKind =
  | "thread_created"
  | "comment_created"
  | "comment_edited"
  | "comment_deleted"
  | "mention_added"
  | "attachment_added"
  | "notification_queued";

export interface ObjectCommentMention {
  id: string;
  handle: string;
  display_name: string;
}

export interface ObjectCommentAttachment {
  id: string;
  kind: ObjectCommentAttachmentKind;
  name: string;
  mime_type: string;
  size_bytes: number;
  url?: string | null;
}

export interface ObjectCommentEntry {
  id: string;
  thread_id: string;
  object_type_id: string;
  object_id: string;
  body: string;
  author_id: string;
  author_display_name: string;
  mentions: ObjectCommentMention[];
  attachments: ObjectCommentAttachment[];
  source_surface: ObjectCommentSurface;
  created_at: string;
  updated_at: string;
  edited_at?: string | null;
  deleted_at?: string | null;
  deleted_by?: string | null;
}

export interface ObjectCommentActivityEvent {
  id: string;
  kind: ObjectCommentActivityKind;
  actor_id: string;
  comment_id?: string | null;
  timestamp: string;
  message: string;
}

export interface ObjectCommentNotification {
  id: string;
  recipient_id: string;
  channel: "in_app" | "email";
  title: string;
  body: string;
  object_type_id: string;
  object_id: string;
  thread_id: string;
  comment_id: string;
}

export interface ObjectCommentPermissionDecision {
  can_view: boolean;
  can_comment: boolean;
  can_edit_own: boolean;
  can_delete_own: boolean;
  can_manage: boolean;
  reason: string;
  object_explorer_distinct_from_workshop: boolean;
}

export interface ObjectCommentThread {
  id: string;
  object_type_id: string;
  object_id: string;
  scope: "object";
  surface: Exclude<ObjectCommentSurface, "workshop_widget">;
  comments: ObjectCommentEntry[];
  activity: ObjectCommentActivityEvent[];
  notifications: ObjectCommentNotification[];
  permissions: ObjectCommentPermissionDecision;
  created_at: string;
  updated_at: string;
  workshop_widget_thread_id: string | null;
}

export interface ObjectCommentMutationResult {
  thread: ObjectCommentThread;
  comment?: ObjectCommentEntry;
  notifications: ObjectCommentNotification[];
  error?: string;
}

export type ObjectViewSectionKind =
  | "summary"
  | "properties"
  | "links"
  | "timeline"
  | "actions"
  | "graph"
  | "comments"
  | "apps";

export interface ObjectViewSectionDefinition {
  id: string;
  title: string;
  kind: ObjectViewSectionKind;
  description: string;
}

export interface ObjectViewSidebarLinkDefinition {
  id: string;
  label: string;
  href: string;
}

export type ObjectViewTabVisibility = "visible" | "hidden" | "conditional";

export interface ObjectViewWorkshopWidgetDefinition {
  id: string;
  kind: ObjectViewSectionKind | string;
  title: string;
  description?: string;
  binding: string;
  config?: Record<string, unknown>;
}

export interface ObjectViewWorkshopModuleDefinition {
  id: string;
  name: string;
  display_name: string;
  version: number;
  form_factor: ObjectViewFormFactor;
  object_context_parameter: string;
  source: "generated_default" | "user_managed";
  widgets: ObjectViewWorkshopWidgetDefinition[];
  updated_at: string;
}

export interface ObjectViewTabDefinition {
  id: string;
  title: string;
  order: number;
  visibility: ObjectViewTabVisibility;
  module: ObjectViewWorkshopModuleDefinition;
  hidden_in_runtime_when_single?: boolean;
}

export interface ObjectViewRuntimeTabDefinition extends ObjectViewTabDefinition {
  runtime_title_visible: boolean;
}

export type ObjectViewPanelHost =
  | "object_explorer"
  | "workshop"
  | "map"
  | "vertex"
  | "gaia"
  | "object_detail_drawer"
  | "action_success_toast"
  | string;

export type ObjectViewPanelDensity = "comfortable" | "compact";

export interface ObjectViewPanelHostConfiguration {
  host: ObjectViewPanelHost;
  enabled: boolean;
  surface: "side_panel" | "compact" | "workshop_widget";
  selected_object_parameter: string;
  supports_open_full_view: boolean;
}

export interface ObjectViewPanelWorkshopWidgetConfiguration {
  enabled: boolean;
  widget_id: string;
  selected_object_parameter: string;
  height_px: number;
}

export interface ObjectViewPanelConfiguration {
  title_template: string;
  property_names: string[];
  section_kinds: ObjectViewSectionKind[];
  density: ObjectViewPanelDensity;
  max_properties: number;
  max_link_groups: number;
  show_title: boolean;
  show_open_full_view: boolean;
  hosts: ObjectViewPanelHostConfiguration[];
  workshop_widget: ObjectViewPanelWorkshopWidgetConfiguration;
}

export interface ObjectViewPanelRuntimeConfig {
  host: ObjectViewPanelHost;
  title: string;
  open_full_view_href: string;
  selected_object_parameter: string;
  property_names: string[];
  section_kinds: ObjectViewSectionKind[];
  density: ObjectViewPanelDensity;
  show_title: boolean;
  show_open_full_view: boolean;
  embed_supported: boolean;
  workshop_widget: ObjectViewPanelWorkshopWidgetConfiguration;
}

export type ObjectViewEmbedHost =
  | "object_views"
  | "object_explorer"
  | "workshop"
  | "map"
  | "vertex"
  | "gaia"
  | "object_detail_drawer"
  | "action_success_toast"
  | "generated_deep_link"
  | "external_iframe"
  | string;

export interface ObjectViewEmbedPolicy {
  host: ObjectViewEmbedHost;
  allowed: boolean;
  hides_workspace_chrome: boolean;
  reason: string;
}

export interface ObjectViewUrlInput {
  objectTypeId?: string | null;
  objectType?: ObjectType | null;
  object?: ObjectInstance | null;
  objectId?: string | null;
  primaryKeyProperty?: string | null;
  primaryKeyValue?: unknown;
  mode?: ObjectViewMode;
  formFactor?: ObjectViewFormFactor;
  branchLabel?: string | null;
  tabId?: string | null;
  embedded?: boolean;
  embedHost?: ObjectViewEmbedHost;
  preferPrimaryKey?: boolean;
}

export interface ObjectViewUrlState {
  object_type_id: string;
  object_id: string | null;
  primary_key_property: string | null;
  primary_key_value: string | null;
  mode: ObjectViewMode;
  form_factor: ObjectViewFormFactor;
  branch_label: string | null;
  tab_id: string | null;
  embedded: boolean;
}

export interface ObjectViewUrlVariants {
  by_object_id: string | null;
  by_primary_key: string | null;
  embedded_by_object_id: string | null;
  embedded_by_primary_key: string | null;
  selected: string;
  selected_locator: "object_id" | "primary_key" | "type_preview";
  embed_policy: ObjectViewEmbedPolicy;
  warnings: string[];
}

export type ObjectViewDefaultSyncState = "synced" | "manual";

export interface ObjectViewDefaultSyncMetadata {
  enabled: boolean;
  state: ObjectViewDefaultSyncState;
  source: "object_type_metadata";
  metadata_signature: string;
  synchronized_at: string;
  generated_from_object_type_updated_at?: string | null;
  property_names: string[];
  prominent_property_names: string[];
  panel_property_names: string[];
  link_type_ids: string[];
}

export type ObjectViewCompatibilityMode = "native" | "datasource_derived";

export interface ObjectViewConfig {
  mode: ObjectViewMode;
  form_factor: ObjectViewFormFactor;
  title_template: string;
  subtitle_property: string;
  prominent_properties: string[];
  panel_properties: string[];
  sections: ObjectViewSectionDefinition[];
  sidebar_links: ObjectViewSidebarLinkDefinition[];
  comments_enabled: boolean;
  branch_label: string;
  auto_publish: boolean;
  compatibility_mode?: ObjectViewCompatibilityMode;
  input_datasource_ids?: string[];
  object_view_version?: number;
  workshop_module_version?: number;
  selected_tab_id?: string;
  tabs?: ObjectViewTabDefinition[];
  panel_config?: ObjectViewPanelConfiguration;
  published_version?: number;
  last_saved_by?: string;
  last_saved_at?: string;
  last_change_summary?: string;
  rollback_target_version?: number;
  restored_from_version?: number;
  version_history?: ObjectViewVersionRecord[];
  default_sync?: ObjectViewDefaultSyncMetadata;
  runtime_budgets?: ObjectViewRuntimeBudgets;
  metadata?: {
    title_property?: string | null;
    primary_key_property?: string | null;
    prominent_property_names?: string[];
    panel_property_names?: string[];
    normal_properties?: string[];
    linked_object_type_ids?: string[];
    link_type_ids?: string[];
    default_custom?: boolean;
    generated?: boolean;
    compatibility_mode?: ObjectViewCompatibilityMode;
    input_datasource_ids?: string[];
    branch_resource_id?: string;
    branch_rebased_at?: string;
    branch_rebased_ontology_signature?: string;
    legacy_fields_modified?: boolean;
    legacy_builder?: boolean;
  };
}

export interface ObjectViewRuntimeBudgetLimits {
  max_queries: number;
  max_linked_object_loads: number;
  max_media_loads: number;
  max_map_loads: number;
  max_time_series_loads: number;
  max_workshop_widget_executions: number;
  max_function_backed_display_values: number;
}

export interface ObjectViewRuntimeBudgets {
  enabled: boolean;
  per_render: ObjectViewRuntimeBudgetLimits;
  per_tab?: Partial<ObjectViewRuntimeBudgetLimits>;
  per_panel?: Partial<ObjectViewRuntimeBudgetLimits>;
}

export type ObjectViewRuntimeBudgetMetric =
  | "queries"
  | "linked_object_loads"
  | "media_loads"
  | "map_loads"
  | "time_series_loads"
  | "workshop_widget_executions"
  | "function_backed_display_values";

export interface ObjectViewRuntimeUsageBreakdown {
  queries: number;
  linked_object_loads: number;
  media_loads: number;
  map_loads: number;
  time_series_loads: number;
  workshop_widget_executions: number;
  function_backed_display_values: number;
}

export interface ObjectViewRuntimeUsageTabBreakdown extends ObjectViewRuntimeUsageBreakdown {
  tab_id: string;
  tab_title: string;
}

export interface ObjectViewRuntimeUsage extends ObjectViewRuntimeUsageBreakdown {
  per_tab: ObjectViewRuntimeUsageTabBreakdown[];
  per_panel: ObjectViewRuntimeUsageBreakdown | null;
}

export type ObjectViewRuntimeBudgetScope = "render" | "tab" | "panel";

export interface ObjectViewRuntimeBudgetWarning {
  scope: ObjectViewRuntimeBudgetScope;
  scope_id?: string;
  scope_label?: string;
  metric: ObjectViewRuntimeBudgetMetric;
  metric_label: string;
  budget: number;
  observed: number;
  overage: number;
  message: string;
}

export interface ObjectViewRuntimeBudgetEvaluation {
  enabled: boolean;
  budgets: ObjectViewRuntimeBudgets;
  usage: ObjectViewRuntimeUsage;
  warnings: ObjectViewRuntimeBudgetWarning[];
  exceeded: boolean;
}

export type ObjectViewVersionPublishState = "draft" | "published" | "previously_published";

export interface ObjectViewVersionRecord {
  id: string;
  object_view_version: number;
  workshop_module_version: number;
  author: string;
  timestamp: string;
  change_summary: string;
  publish_state: ObjectViewVersionPublishState;
  published: boolean;
  published_at?: string;
  rollback_target_version?: number;
  restored_from_version?: number;
  tab_ids: string[];
  module_ids: string[];
  snapshot: Omit<ObjectViewConfig, "version_history">;
}

export interface ObjectViewDefinition {
  id: string;
  name: string;
  display_name?: string;
  description?: string;
  object_type_id: string;
  mode: ObjectViewMode;
  form_factor: ObjectViewFormFactor;
  config?: ObjectViewConfig;
  branch_label?: string | null;
  published?: boolean;
  status?: string;
  created_by?: string;
  owner_id?: string;
  created_at?: string;
  updated_at?: string;
}

export type ObjectViewToggleHost =
  | "object_views"
  | "object_explorer"
  | "workshop"
  | "map"
  | "vertex"
  | "gaia"
  | "object_detail_drawer"
  | "action_success_toast"
  | "generated_deep_link"
  | string;

export interface ObjectViewToggleHostPolicy {
  host: ObjectViewToggleHost;
  supports_toggle: boolean;
  limitation?: string;
}

export interface ObjectViewModeToggleOption {
  mode: ObjectViewMode;
  label: string;
  view: ObjectViewDefinition | null;
  enabled: boolean;
  selected: boolean;
  default: boolean;
  reason: string;
}

export interface ObjectViewModeToggleResolution {
  host: ObjectViewToggleHost;
  form_factor: ObjectViewFormFactor;
  supports_toggle: boolean;
  limitation?: string;
  requested_mode?: ObjectViewMode;
  requested_mode_ignored: boolean;
  default_mode: ObjectViewMode;
  selected_mode: ObjectViewMode;
  custom_is_default: boolean;
  core_view: ObjectViewDefinition | null;
  custom_view: ObjectViewDefinition | null;
  active_view: ObjectViewDefinition | null;
  options: ObjectViewModeToggleOption[];
}

const OBJECT_VIEW_TOGGLE_HOST_POLICIES: Record<string, ObjectViewToggleHostPolicy> = {
  object_views: { host: "object_views", supports_toggle: true },
  object_explorer: { host: "object_explorer", supports_toggle: true },
  map: { host: "map", supports_toggle: true },
  vertex: { host: "vertex", supports_toggle: true },
  gaia: { host: "gaia", supports_toggle: true },
  object_detail_drawer: { host: "object_detail_drawer", supports_toggle: true },
  generated_deep_link: { host: "generated_deep_link", supports_toggle: true },
  action_success_toast: {
    host: "action_success_toast",
    supports_toggle: false,
    limitation: "Action success toasts render Object View deep links; the toast itself does not expose a core/custom toggle.",
  },
  workshop: {
    host: "workshop",
    supports_toggle: false,
    limitation: "Workshop Object View widgets use the default Object View; core/custom toggling is not implemented locally.",
  },
};

export function objectViewToggleHostPolicy(host: ObjectViewToggleHost): ObjectViewToggleHostPolicy {
  return OBJECT_VIEW_TOGGLE_HOST_POLICIES[host] ?? {
    host,
    supports_toggle: false,
    limitation: "This host has no local core/custom Object View toggle.",
  };
}

function objectViewTimestamp(view: ObjectViewDefinition) {
  const timestamp = Date.parse(view.updated_at || view.created_at || "");
  return Number.isFinite(timestamp) ? timestamp : 0;
}

function objectViewCustomRank(view: ObjectViewDefinition) {
  if (view.published === true || view.status === "published") return 0;
  if (view.config?.default_sync?.state === "manual") return 1;
  if (view.status === "default_synced" || view.config?.metadata?.default_custom) return 2;
  return 3;
}

function chooseObjectViewCandidate(views: ObjectViewDefinition[]) {
  return [...views].sort((left, right) => {
    const rank = objectViewCustomRank(left) - objectViewCustomRank(right);
    if (rank !== 0) return rank;
    return objectViewTimestamp(right) - objectViewTimestamp(left);
  })[0] ?? null;
}

export function resolveObjectViewModeToggle(input: {
  views: ObjectViewDefinition[];
  formFactor: ObjectViewFormFactor;
  host: ObjectViewToggleHost;
  objectTypeId?: string;
  requestedMode?: ObjectViewMode;
}): ObjectViewModeToggleResolution {
  const policy = objectViewToggleHostPolicy(input.host);
  const candidates = input.views.filter((view) =>
    view.form_factor === input.formFactor &&
    (!input.objectTypeId || view.object_type_id === input.objectTypeId),
  );
  const coreView = chooseObjectViewCandidate(candidates.filter((view) => view.mode === "standard"));
  const customView = chooseObjectViewCandidate(candidates.filter((view) => view.mode === "configured"));
  const defaultMode: ObjectViewMode = customView ? "configured" : "standard";
  const requestedView = input.requestedMode === "configured" ? customView : input.requestedMode === "standard" ? coreView : null;
  const selectedMode: ObjectViewMode = policy.supports_toggle && input.requestedMode && requestedView
    ? input.requestedMode
    : defaultMode;
  const activeView = selectedMode === "configured" ? customView : coreView;
  const requestedModeIgnored = Boolean(input.requestedMode && input.requestedMode !== selectedMode);
  const optionFor = (mode: ObjectViewMode): ObjectViewModeToggleOption => {
    const view = mode === "configured" ? customView : coreView;
    const hasView = Boolean(view);
    const selected = selectedMode === mode;
    const enabled = hasView && (policy.supports_toggle || selected);
    const label = mode === "configured" ? "Custom" : "Core";
    const missingReason = mode === "configured"
      ? "No configured Object View is available for this form factor."
      : "No core Object View is available for this form factor.";
    return {
      mode,
      label,
      view,
      enabled,
      selected,
      default: defaultMode === mode,
      reason: hasView ? (!policy.supports_toggle && !selected ? policy.limitation ?? "" : "") : missingReason,
    };
  };
  return {
    host: input.host,
    form_factor: input.formFactor,
    supports_toggle: policy.supports_toggle,
    limitation: policy.limitation,
    requested_mode: input.requestedMode,
    requested_mode_ignored: requestedModeIgnored,
    default_mode: defaultMode,
    selected_mode: selectedMode,
    custom_is_default: Boolean(customView),
    core_view: coreView,
    custom_view: customView,
    active_view: activeView,
    options: [optionFor("configured"), optionFor("standard")],
  };
}

function coreObjectViewSection(
  id: string,
  title: string,
  kind: ObjectViewSectionKind,
  description: string,
): ObjectViewSectionDefinition {
  return { id, title, kind, description };
}

function objectViewLinksForType(objectType: Pick<ObjectType, "id">, linkTypes: LinkType[]) {
  return linkTypes.filter(
    (link) =>
      link.visibility !== "hidden" &&
      (link.source_type_id === objectType.id || link.target_type_id === objectType.id),
  );
}

function objectViewLinkedObjectTypeIds(
  objectType: Pick<ObjectType, "id">,
  linkTypes: LinkType[],
) {
  return uniqueNonEmpty(
    linkTypes.flatMap((link) => [link.source_type_id, link.target_type_id]),
  ).filter(
    (id) =>
      id !== objectType.id ||
      linkTypes.some((link) => link.source_type_id === objectType.id && link.target_type_id === objectType.id),
  );
}

function objectViewStableToken(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "") || "object-view";
}

function objectViewPascalToken(value: string) {
  return objectViewStableToken(value)
    .split("-")
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join("") || "Tab";
}

function objectViewUniqueTabId(title: string, tabs: ObjectViewTabDefinition[]) {
  const base = objectViewStableToken(title);
  const existing = new Set(tabs.map((tab) => tab.id));
  if (!existing.has(base)) return base;
  let index = 2;
  while (existing.has(`${base}-${index}`)) index += 1;
  return `${base}-${index}`;
}

function objectViewReorderTabs(tabs: ObjectViewTabDefinition[]) {
  return [...tabs]
    .sort((left, right) => left.order - right.order)
    .map((tab, index) => ({ ...tab, order: index }));
}

function objectViewAssignTabOrder(tabs: ObjectViewTabDefinition[]) {
  return tabs.map((tab, index) => ({ ...tab, order: index }));
}

function objectViewWorkshopWidgetsFromSections(
  config: ObjectViewConfig,
  formFactor: ObjectViewFormFactor,
): ObjectViewWorkshopWidgetDefinition[] {
  return config.sections.map((section, index) => {
    const binding = section.kind === "properties"
      ? "selectedObject.properties"
      : section.kind === "links"
      ? "selectedObject.links"
      : section.kind === "actions"
      ? "selectedObject.actions"
      : "selectedObject";
    const widgetConfig: Record<string, unknown> = {};
    if (section.kind === "properties" || section.kind === "summary") {
      widgetConfig.properties = formFactor === "panel"
        ? config.panel_properties
        : config.prominent_properties;
    }
    if (section.kind === "links") {
      widgetConfig.link_type_ids = config.metadata?.link_type_ids ?? [];
    }
    return {
      id: `widget-${objectViewStableToken(section.id || section.kind)}-${index + 1}`,
      kind: section.kind,
      title: section.title,
      description: section.description,
      binding,
      config: widgetConfig,
    };
  });
}

function objectViewDefaultTab(input: {
  objectType: ObjectType;
  config: ObjectViewConfig;
  formFactor: ObjectViewFormFactor;
  now: string;
}): ObjectViewTabDefinition {
  const tabId = input.formFactor === "full" ? "overview" : "panel";
  const moduleName = `${objectTypeAPIName(input.objectType)}${input.formFactor === "full" ? "Overview" : "Panel"}Module`;
  return {
    id: tabId,
    title: input.formFactor === "full" ? "Overview" : "Panel",
    order: 0,
    visibility: "visible",
    hidden_in_runtime_when_single: input.formFactor === "full",
    module: {
      id: `workshop-module:${input.objectType.id}:${input.formFactor}:${tabId}`,
      name: moduleName,
      display_name: `${input.objectType.display_name} ${input.formFactor === "full" ? "overview" : "panel"} module`,
      version: input.config.workshop_module_version ?? 1,
      form_factor: input.formFactor,
      object_context_parameter: "selectedObject",
      source: input.config.default_sync?.state === "synced" ? "generated_default" : "user_managed",
      widgets: objectViewWorkshopWidgetsFromSections(input.config, input.formFactor),
      updated_at: input.now,
    },
  };
}

export function createObjectViewTab(input: {
  objectType: ObjectType;
  config: ObjectViewConfig;
  title?: string;
  now?: string;
}): ObjectViewTabDefinition {
  const now = input.now ?? new Date().toISOString();
  const formFactor = "full";
  const shell = ensureObjectViewEditorShell({
    objectType: input.objectType,
    config: input.config,
    formFactor,
    now,
  });
  const tabs = objectViewReorderTabs(shell.tabs ?? []);
  const title = input.title?.trim() || `Tab ${tabs.length + 1}`;
  const id = objectViewUniqueTabId(title, tabs);
  const moduleToken = objectViewPascalToken(title);
  return {
    id,
    title,
    order: tabs.length,
    visibility: "visible",
    hidden_in_runtime_when_single: true,
    module: {
      id: `workshop-module:${input.objectType.id}:full:${id}`,
      name: `${objectTypeAPIName(input.objectType)}${moduleToken}Module`,
      display_name: `${input.objectType.display_name} ${title} module`,
      version: 1,
      form_factor: formFactor,
      object_context_parameter: "selectedObject",
      source: "user_managed",
      widgets: objectViewWorkshopWidgetsFromSections(shell, formFactor),
      updated_at: now,
    },
  };
}

export function ensureObjectViewEditorShell(input: {
  objectType: ObjectType;
  config: ObjectViewConfig;
  formFactor?: ObjectViewFormFactor;
  now?: string;
}): ObjectViewConfig {
  const formFactor = input.formFactor ?? input.config.form_factor;
  const now = input.now ?? new Date().toISOString();
  const baseVersion = Number.isFinite(input.config.object_view_version)
    ? Math.max(1, Number(input.config.object_view_version))
    : 1;
  const tabs = input.config.tabs && input.config.tabs.length > 0
    ? input.config.tabs
        .map((tab, index) => ({
          ...tab,
          order: Number.isFinite(tab.order) ? tab.order : index,
          visibility: tab.visibility || "visible",
          hidden_in_runtime_when_single: tab.hidden_in_runtime_when_single ?? (formFactor === "full"),
          module: {
            ...tab.module,
            form_factor: tab.module.form_factor || formFactor,
            object_context_parameter: tab.module.object_context_parameter || "selectedObject",
            version: Math.max(1, Number(tab.module.version) || 1),
            widgets: tab.module.widgets && tab.module.widgets.length > 0
              ? tab.module.widgets
              : objectViewWorkshopWidgetsFromSections(input.config, formFactor),
            updated_at: tab.module.updated_at || now,
          },
        }))
        .sort((left, right) => left.order - right.order)
    : [objectViewDefaultTab({ objectType: input.objectType, config: input.config, formFactor, now })];
  const selectedTabId = tabs.some((tab) => tab.id === input.config.selected_tab_id)
    ? input.config.selected_tab_id
    : tabs[0]?.id;
  const activeModule = tabs.find((tab) => tab.id === selectedTabId)?.module ?? tabs[0]?.module;
  return {
    ...input.config,
    form_factor: formFactor,
    object_view_version: baseVersion,
    workshop_module_version: activeModule?.version ?? input.config.workshop_module_version ?? 1,
    selected_tab_id: selectedTabId,
    tabs,
  };
}

export function addObjectViewTab(input: {
  objectType: ObjectType;
  config: ObjectViewConfig;
  title?: string;
  now?: string;
}): ObjectViewConfig {
  const shell = ensureObjectViewEditorShell({
    objectType: input.objectType,
    config: input.config,
    formFactor: "full",
    now: input.now,
  });
  const tab = createObjectViewTab({
    objectType: input.objectType,
    config: shell,
    title: input.title,
    now: input.now,
  });
  return {
    ...shell,
    selected_tab_id: tab.id,
    workshop_module_version: tab.module.version,
    tabs: objectViewReorderTabs([...(shell.tabs ?? []), tab]),
  };
}

export function renameObjectViewTab(input: {
  objectType: ObjectType;
  config: ObjectViewConfig;
  tabId: string;
  title: string;
  now?: string;
}): ObjectViewConfig {
  const shell = ensureObjectViewEditorShell({
    objectType: input.objectType,
    config: input.config,
    formFactor: "full",
    now: input.now,
  });
  const title = input.title.trim();
  if (!title) return shell;
  return {
    ...shell,
    tabs: objectViewReorderTabs((shell.tabs ?? []).map((tab) =>
      tab.id === input.tabId
        ? {
            ...tab,
            title,
            module: {
              ...tab.module,
              display_name: `${input.objectType.display_name} ${title} module`,
              source: "user_managed",
              updated_at: input.now ?? tab.module.updated_at,
            },
          }
        : tab,
    )),
  };
}

export function setObjectViewTabVisibility(input: {
  objectType: ObjectType;
  config: ObjectViewConfig;
  tabId: string;
  visibility: ObjectViewTabVisibility;
  hiddenInRuntimeWhenSingle?: boolean;
  now?: string;
}): ObjectViewConfig {
  const shell = ensureObjectViewEditorShell({
    objectType: input.objectType,
    config: input.config,
    formFactor: "full",
    now: input.now,
  });
  return {
    ...shell,
    tabs: objectViewReorderTabs((shell.tabs ?? []).map((tab) =>
      tab.id === input.tabId
        ? {
            ...tab,
            visibility: input.visibility,
            hidden_in_runtime_when_single: input.hiddenInRuntimeWhenSingle ?? tab.hidden_in_runtime_when_single,
          }
        : tab,
    )),
  };
}

export function moveObjectViewTab(input: {
  objectType: ObjectType;
  config: ObjectViewConfig;
  tabId: string;
  direction: "up" | "down";
  now?: string;
}): ObjectViewConfig {
  const shell = ensureObjectViewEditorShell({
    objectType: input.objectType,
    config: input.config,
    formFactor: "full",
    now: input.now,
  });
  const tabs = objectViewReorderTabs(shell.tabs ?? []);
  const index = tabs.findIndex((tab) => tab.id === input.tabId);
  if (index < 0) return shell;
  const nextIndex = input.direction === "up" ? index - 1 : index + 1;
  if (nextIndex < 0 || nextIndex >= tabs.length) return shell;
  const reordered = [...tabs];
  const [tab] = reordered.splice(index, 1);
  reordered.splice(nextIndex, 0, tab);
  return {
    ...shell,
    tabs: objectViewAssignTabOrder(reordered),
  };
}

export function deleteObjectViewTab(input: {
  objectType: ObjectType;
  config: ObjectViewConfig;
  tabId: string;
  now?: string;
}): ObjectViewConfig {
  const shell = ensureObjectViewEditorShell({
    objectType: input.objectType,
    config: input.config,
    formFactor: "full",
    now: input.now,
  });
  const tabs = objectViewReorderTabs(shell.tabs ?? []);
  if (tabs.length <= 1) return shell;
  const deletedIndex = tabs.findIndex((tab) => tab.id === input.tabId);
  if (deletedIndex < 0) return shell;
  const nextTabs = objectViewReorderTabs(tabs.filter((tab) => tab.id !== input.tabId));
  const selectedTabId = shell.selected_tab_id === input.tabId
    ? nextTabs[Math.min(deletedIndex, nextTabs.length - 1)]?.id
    : shell.selected_tab_id;
  const selectedModule = nextTabs.find((tab) => tab.id === selectedTabId)?.module;
  return {
    ...shell,
    selected_tab_id: selectedTabId,
    workshop_module_version: selectedModule?.version ?? shell.workshop_module_version,
    tabs: nextTabs,
  };
}

export function objectViewRuntimeTabs(
  config: ObjectViewConfig,
  options?: { editMode?: boolean },
): ObjectViewRuntimeTabDefinition[] {
  const visibleTabs = objectViewReorderTabs(config.tabs ?? [])
    .filter((tab) => tab.visibility !== "hidden");
  const hideOnlyTitle = !options?.editMode &&
    visibleTabs.length === 1 &&
    visibleTabs[0]?.hidden_in_runtime_when_single === true;
  return visibleTabs.map((tab) => ({
    ...tab,
    runtime_title_visible: !hideOnlyTitle,
  }));
}

function objectViewConfigSnapshot(config: ObjectViewConfig): Omit<ObjectViewConfig, "version_history"> {
  const { version_history: _versionHistory, ...snapshot } = config;
  return JSON.parse(JSON.stringify(snapshot)) as Omit<ObjectViewConfig, "version_history">;
}

export function objectViewVersionHistory(config: ObjectViewConfig): ObjectViewVersionRecord[] {
  return [...(config.version_history ?? [])].sort((left, right) => right.object_view_version - left.object_view_version);
}

export function saveObjectViewConfigVersion(input: {
  objectType: ObjectType;
  config: ObjectViewConfig;
  published?: boolean;
  author?: string;
  changeSummary?: string;
  now?: string;
}): ObjectViewConfig {
  const now = input.now ?? new Date().toISOString();
  const shell = ensurePanelObjectViewConfiguration({
    objectType: input.objectType,
    config: ensureObjectViewEditorShell({
      objectType: input.objectType,
      config: input.config,
      formFactor: input.config.form_factor,
      now,
    }),
  });
  const shouldPublish = input.published ?? shell.auto_publish;
  const author = input.author?.trim() || "platform-ui";
  const summary = input.changeSummary?.trim() || (shouldPublish ? "Saved and published Object View edits." : "Saved Object View draft.");
  const nextObjectViewVersion = Math.max(1, Number(shell.object_view_version) || 1) + 1;
  const selectedTabId = shell.selected_tab_id ?? shell.tabs?.[0]?.id;
  const tabs = (shell.tabs ?? []).map((tab) =>
    tab.id === selectedTabId
      ? {
          ...tab,
          module: {
            ...tab.module,
            version: Math.max(1, Number(tab.module.version) || 1) + 1,
            source: "user_managed" as const,
            updated_at: now,
          },
        }
      : tab,
  );
  const activeModule = tabs.find((tab) => tab.id === selectedTabId)?.module ?? tabs[0]?.module;
  const history = (shell.version_history ?? []).map((entry) =>
    shouldPublish && entry.publish_state === "published"
      ? { ...entry, publish_state: "previously_published" as const, published: false }
      : entry,
  );
  const baseConfig = markObjectViewConfigManuallyEdited(
    {
      ...shell,
      mode: "configured",
      object_view_version: nextObjectViewVersion,
      workshop_module_version: activeModule?.version ?? shell.workshop_module_version ?? 1,
      published_version: shouldPublish ? nextObjectViewVersion : shell.published_version,
      last_saved_by: author,
      last_saved_at: now,
      last_change_summary: summary,
      rollback_target_version: shell.rollback_target_version,
      restored_from_version: shell.restored_from_version,
      tabs,
    },
    now,
  );
  const record: ObjectViewVersionRecord = {
    id: `object-view-version:${input.objectType.id}:${baseConfig.form_factor}:${nextObjectViewVersion}`,
    object_view_version: nextObjectViewVersion,
    workshop_module_version: baseConfig.workshop_module_version ?? 1,
    author,
    timestamp: now,
    change_summary: summary,
    publish_state: shouldPublish ? "published" : "draft",
    published: shouldPublish,
    published_at: shouldPublish ? now : undefined,
    rollback_target_version: shell.rollback_target_version,
    restored_from_version: shell.restored_from_version,
    tab_ids: tabs.map((tab) => tab.id),
    module_ids: tabs.map((tab) => tab.module.id),
    snapshot: objectViewConfigSnapshot(baseConfig),
  };
  return {
    ...baseConfig,
    rollback_target_version: undefined,
    restored_from_version: undefined,
    version_history: [record, ...history].slice(0, 50),
  };
}

export function restoreObjectViewConfigVersion(input: {
  objectType: ObjectType;
  config: ObjectViewConfig;
  version: number;
  author?: string;
  now?: string;
}): ObjectViewConfig {
  const now = input.now ?? new Date().toISOString();
  const history = objectViewVersionHistory(input.config);
  const record = history.find((entry) => entry.object_view_version === input.version);
  if (!record) return input.config;
  const restored = {
    ...record.snapshot,
    object_view_version: input.config.object_view_version,
    workshop_module_version: input.config.workshop_module_version,
    published_version: input.config.published_version,
    last_saved_by: input.author?.trim() || input.config.last_saved_by,
    last_saved_at: input.config.last_saved_at,
    last_change_summary: `Restored draft from version ${record.object_view_version}.`,
    rollback_target_version: record.object_view_version,
    restored_from_version: record.object_view_version,
    version_history: input.config.version_history,
  } satisfies ObjectViewConfig;
  return markObjectViewConfigManuallyEdited(
    ensurePanelObjectViewConfiguration({
      objectType: input.objectType,
      config: ensureObjectViewEditorShell({
        objectType: input.objectType,
        config: restored,
        formFactor: restored.form_factor,
        now,
      }),
    }),
    now,
  );
}

const OBJECT_VIEW_RUNTIME_BUDGET_METRIC_LABELS: Record<ObjectViewRuntimeBudgetMetric, string> = {
  queries: "queries",
  linked_object_loads: "linked object loads",
  media_loads: "media loads",
  map_loads: "map loads",
  time_series_loads: "time-series loads",
  workshop_widget_executions: "Workshop widget executions",
  function_backed_display_values: "function-backed display values",
};

const OBJECT_VIEW_RUNTIME_BUDGET_LIMIT_KEYS: Record<ObjectViewRuntimeBudgetMetric, keyof ObjectViewRuntimeBudgetLimits> = {
  queries: "max_queries",
  linked_object_loads: "max_linked_object_loads",
  media_loads: "max_media_loads",
  map_loads: "max_map_loads",
  time_series_loads: "max_time_series_loads",
  workshop_widget_executions: "max_workshop_widget_executions",
  function_backed_display_values: "max_function_backed_display_values",
};

export function defaultObjectViewRuntimeBudgets(): ObjectViewRuntimeBudgets {
  return {
    enabled: true,
    per_render: {
      max_queries: 16,
      max_linked_object_loads: 50,
      max_media_loads: 8,
      max_map_loads: 2,
      max_time_series_loads: 4,
      max_workshop_widget_executions: 12,
      max_function_backed_display_values: 24,
    },
    per_tab: {
      max_queries: 6,
      max_linked_object_loads: 20,
      max_workshop_widget_executions: 6,
      max_function_backed_display_values: 12,
    },
    per_panel: {
      max_queries: 4,
      max_linked_object_loads: 10,
      max_workshop_widget_executions: 3,
      max_function_backed_display_values: 6,
    },
  };
}

function mergeObjectViewBudgetLimits(
  base: ObjectViewRuntimeBudgetLimits,
  override?: Partial<ObjectViewRuntimeBudgetLimits> | null,
): ObjectViewRuntimeBudgetLimits {
  if (!override) return { ...base };
  const next: ObjectViewRuntimeBudgetLimits = { ...base };
  for (const key of Object.keys(base) as Array<keyof ObjectViewRuntimeBudgetLimits>) {
    const value = override[key];
    if (typeof value === "number" && Number.isFinite(value) && value >= 0) {
      next[key] = Math.floor(value);
    }
  }
  return next;
}

export function objectViewRuntimeBudgets(
  config?: ObjectViewConfig | null,
): ObjectViewRuntimeBudgets {
  const defaults = defaultObjectViewRuntimeBudgets();
  const candidate = config?.runtime_budgets;
  if (!candidate) return defaults;
  const enabled = candidate.enabled !== false;
  return {
    enabled,
    per_render: mergeObjectViewBudgetLimits(defaults.per_render, candidate.per_render),
    per_tab: candidate.per_tab ? { ...candidate.per_tab } : { ...defaults.per_tab },
    per_panel: candidate.per_panel ? { ...candidate.per_panel } : { ...defaults.per_panel },
  };
}

export function setObjectViewRuntimeBudgets(input: {
  config: ObjectViewConfig;
  budgets: ObjectViewRuntimeBudgets;
}): ObjectViewConfig {
  const next = objectViewRuntimeBudgets({ ...input.config, runtime_budgets: input.budgets });
  return { ...input.config, runtime_budgets: next };
}

function emptyObjectViewRuntimeUsageBreakdown(): ObjectViewRuntimeUsageBreakdown {
  return {
    queries: 0,
    linked_object_loads: 0,
    media_loads: 0,
    map_loads: 0,
    time_series_loads: 0,
    workshop_widget_executions: 0,
    function_backed_display_values: 0,
  };
}

function isMediaPropertyType(kind: string) {
  const normalized = normalizedPropertyType(kind);
  return (
    [
      "media_reference",
      "media",
      "attachment",
      "image",
      "image_reference",
      "file",
      "file_reference",
    ].includes(normalized) ||
    normalized.includes("media") ||
    normalized.includes("attachment") ||
    normalized.includes("image")
  );
}

function isTimeSeriesPropertyType(kind: string) {
  const normalized = normalizedPropertyType(kind);
  return (
    [
      "time_series",
      "timeseries",
      "time_series_reference",
      "series",
    ].includes(normalized) ||
    normalized.includes("time_series") ||
    normalized.includes("timeseries")
  );
}

function isFunctionBackedProperty(property: Property): boolean {
  const formatting = property.value_formatting as Record<string, unknown> | undefined;
  if (formatting && typeof formatting === "object") {
    const source = String((formatting as { source?: unknown }).source ?? "").toLowerCase();
    if (source === "function" || source === "computed" || source === "derived") return true;
    const kind = String((formatting as { kind?: unknown }).kind ?? "").toLowerCase();
    if (kind === "function" || kind === "computed" || kind === "derived") return true;
    if ((formatting as { function_id?: unknown }).function_id) return true;
    if ((formatting as { function_rid?: unknown }).function_rid) return true;
  }
  const semanticHints = property.semantic_hints ?? [];
  if (semanticHints.some((hint) => /function|computed|derived/i.test(String(hint)))) return true;
  const reducer = property.reducer_metadata as Record<string, unknown> | undefined;
  if (reducer && typeof reducer === "object" && (reducer as { source?: unknown }).source === "function") return true;
  return false;
}

function objectViewMediaPropertyNames(properties: Property[]): string[] {
  return properties
    .filter((property) => isMediaPropertyType(property.property_type))
    .map((property) => property.name);
}

function objectViewTimeSeriesPropertyNames(properties: Property[]): string[] {
  return properties
    .filter((property) => isTimeSeriesPropertyType(property.property_type))
    .map((property) => property.name);
}

function objectViewFunctionBackedPropertyNames(properties: Property[]): string[] {
  return properties.filter(isFunctionBackedProperty).map((property) => property.name);
}

function objectViewMapPropertyNames(properties: Property[]): string[] {
  return properties
    .filter(
      (property) =>
        isGeoPointPropertyType(property.property_type) ||
        isGeoShapePropertyType(property.property_type),
    )
    .map((property) => property.name);
}

function widgetUsageMetrics(
  widget: ObjectViewWorkshopWidgetDefinition,
  context: {
    visibleProperties: Set<string>;
    mediaProperties: Set<string>;
    mapProperties: Set<string>;
    timeSeriesProperties: Set<string>;
    functionBackedProperties: Set<string>;
    linkedObjectCount: number;
    functionBackedCount: number;
  },
): ObjectViewRuntimeUsageBreakdown {
  const usage = emptyObjectViewRuntimeUsageBreakdown();
  usage.workshop_widget_executions = 1;
  const kind = String(widget.kind || "").toLowerCase();
  if (kind === "links" || kind === "graph") {
    usage.queries += 1;
    usage.linked_object_loads += context.linkedObjectCount;
  } else if (kind === "timeline" || kind === "comments" || kind === "apps") {
    usage.queries += 1;
  } else if (kind === "actions") {
    usage.queries += 1;
  } else if (kind === "properties" || kind === "summary") {
    let mediaCount = 0;
    let mapCount = 0;
    let timeSeriesCount = 0;
    let functionCount = 0;
    for (const name of context.visibleProperties) {
      if (context.mediaProperties.has(name)) mediaCount += 1;
      if (context.mapProperties.has(name)) mapCount += 1;
      if (context.timeSeriesProperties.has(name)) timeSeriesCount += 1;
      if (context.functionBackedProperties.has(name)) functionCount += 1;
    }
    usage.media_loads += mediaCount;
    usage.map_loads += mapCount;
    usage.time_series_loads += timeSeriesCount;
    usage.function_backed_display_values += functionCount;
  }
  return usage;
}

function addBreakdowns(
  base: ObjectViewRuntimeUsageBreakdown,
  delta: ObjectViewRuntimeUsageBreakdown,
): ObjectViewRuntimeUsageBreakdown {
  return {
    queries: base.queries + delta.queries,
    linked_object_loads: base.linked_object_loads + delta.linked_object_loads,
    media_loads: base.media_loads + delta.media_loads,
    map_loads: base.map_loads + delta.map_loads,
    time_series_loads: base.time_series_loads + delta.time_series_loads,
    workshop_widget_executions: base.workshop_widget_executions + delta.workshop_widget_executions,
    function_backed_display_values:
      base.function_backed_display_values + delta.function_backed_display_values,
  };
}

export interface MeasureObjectViewRuntimeUsageInput {
  config: ObjectViewConfig;
  properties?: Property[];
  response?: ObjectViewResponse | null;
  formFactor: ObjectViewFormFactor;
}

export function measureObjectViewRuntimeUsage(
  input: MeasureObjectViewRuntimeUsageInput,
): ObjectViewRuntimeUsage {
  const properties = input.properties ?? [];
  const visibleProperties = new Set(
    objectViewVisibleProperties(properties).map((property) => property.name),
  );
  const mediaProperties = new Set(objectViewMediaPropertyNames(properties));
  const mapProperties = new Set(objectViewMapPropertyNames(properties));
  const timeSeriesProperties = new Set(objectViewTimeSeriesPropertyNames(properties));
  const functionBackedProperties = new Set(objectViewFunctionBackedPropertyNames(properties));

  const linkedObjectCount = input.response?.neighbors?.length ?? 0;
  const baseQueries = input.response ? 1 : 0;
  const matchingRulesQueries = input.response?.matching_rules?.length ? 1 : 0;
  const recentRuleRunsQueries = input.response?.recent_rule_runs?.length ? 1 : 0;
  const timelineQueries = input.response?.timeline?.length ? 1 : 0;

  const widgetContext = {
    visibleProperties,
    mediaProperties,
    mapProperties,
    timeSeriesProperties,
    functionBackedProperties,
    linkedObjectCount,
    functionBackedCount: functionBackedProperties.size,
  };

  const perTab: ObjectViewRuntimeUsageTabBreakdown[] = [];
  let perPanel: ObjectViewRuntimeUsageBreakdown | null = null;
  let totalWidgetExecutions = 0;
  const tabKinds = new Set<string>();

  if (input.formFactor === "full") {
    const tabs = objectViewRuntimeTabs(input.config, { editMode: true });
    for (const tab of tabs) {
      const tabBreakdown = emptyObjectViewRuntimeUsageBreakdown();
      for (const widget of tab.module.widgets) {
        const widgetUsage = widgetUsageMetrics(widget, widgetContext);
        const merged = addBreakdowns(tabBreakdown, widgetUsage);
        Object.assign(tabBreakdown, merged);
        tabKinds.add(String(widget.kind || "").toLowerCase());
      }
      perTab.push({
        tab_id: tab.id,
        tab_title: tab.title,
        ...tabBreakdown,
      });
      totalWidgetExecutions += tabBreakdown.workshop_widget_executions;
    }
  } else {
    const panelConfig = input.config.panel_config;
    const sectionKinds = panelConfig?.section_kinds ?? input.config.sections.map((section) => section.kind);
    const panelBreakdown = emptyObjectViewRuntimeUsageBreakdown();
    for (const kind of sectionKinds) {
      const widget: ObjectViewWorkshopWidgetDefinition = {
        id: `panel-${kind}`,
        kind,
        title: String(kind),
        binding: "selectedObject",
      };
      const widgetUsage = widgetUsageMetrics(widget, widgetContext);
      Object.assign(panelBreakdown, addBreakdowns(panelBreakdown, widgetUsage));
      tabKinds.add(String(kind).toLowerCase());
    }
    perPanel = panelBreakdown;
    totalWidgetExecutions = panelBreakdown.workshop_widget_executions;
  }

  const anyLinkRenderer = tabKinds.has("links") || tabKinds.has("graph");
  const renderUsage: ObjectViewRuntimeUsage = {
    queries: baseQueries + matchingRulesQueries + recentRuleRunsQueries + timelineQueries,
    linked_object_loads: anyLinkRenderer ? linkedObjectCount : 0,
    media_loads: 0,
    map_loads: 0,
    time_series_loads: 0,
    workshop_widget_executions: totalWidgetExecutions,
    function_backed_display_values: 0,
    per_tab: perTab,
    per_panel: perPanel,
  };

  if (input.response && input.response.object?.properties) {
    const presentProperties = Object.keys(input.response.object.properties).filter(
      (name) => visibleProperties.has(name),
    );
    for (const name of presentProperties) {
      if (mediaProperties.has(name)) renderUsage.media_loads += 1;
      if (mapProperties.has(name)) renderUsage.map_loads += 1;
      if (timeSeriesProperties.has(name)) renderUsage.time_series_loads += 1;
      if (functionBackedProperties.has(name)) renderUsage.function_backed_display_values += 1;
    }
  }

  return renderUsage;
}

function compareObjectViewBudget(
  scope: ObjectViewRuntimeBudgetScope,
  scopeId: string | undefined,
  scopeLabel: string | undefined,
  metric: ObjectViewRuntimeBudgetMetric,
  budget: number,
  observed: number,
): ObjectViewRuntimeBudgetWarning | null {
  if (!Number.isFinite(budget) || budget <= 0) return null;
  if (observed <= budget) return null;
  const metricLabel = OBJECT_VIEW_RUNTIME_BUDGET_METRIC_LABELS[metric];
  const overage = observed - budget;
  const scopeText =
    scope === "tab"
      ? `Tab "${scopeLabel ?? scopeId ?? "(unknown)"}"`
      : scope === "panel"
      ? "Side panel"
      : "Render";
  return {
    scope,
    scope_id: scopeId,
    scope_label: scopeLabel,
    metric,
    metric_label: metricLabel,
    budget,
    observed,
    overage,
    message: `${scopeText} uses ${observed} ${metricLabel} but the configured budget is ${budget}.`,
  };
}

export interface EvaluateObjectViewRuntimeBudgetsInput {
  config: ObjectViewConfig;
  usage: ObjectViewRuntimeUsage;
  formFactor: ObjectViewFormFactor;
  editorMode?: boolean;
}

export function evaluateObjectViewRuntimeBudgets(
  input: EvaluateObjectViewRuntimeBudgetsInput,
): ObjectViewRuntimeBudgetEvaluation {
  const budgets = objectViewRuntimeBudgets(input.config);
  if (!budgets.enabled) {
    return {
      enabled: false,
      budgets,
      usage: input.usage,
      warnings: [],
      exceeded: false,
    };
  }
  const warnings: ObjectViewRuntimeBudgetWarning[] = [];
  const renderLimits = budgets.per_render;
  const metrics: ObjectViewRuntimeBudgetMetric[] = [
    "queries",
    "linked_object_loads",
    "media_loads",
    "map_loads",
    "time_series_loads",
    "workshop_widget_executions",
    "function_backed_display_values",
  ];
  for (const metric of metrics) {
    const limitKey = OBJECT_VIEW_RUNTIME_BUDGET_LIMIT_KEYS[metric];
    const limit = renderLimits[limitKey];
    const observed = input.usage[metric];
    const warning = compareObjectViewBudget("render", undefined, undefined, metric, limit, observed);
    if (warning) warnings.push(warning);
  }
  if (input.formFactor === "full" && budgets.per_tab) {
    for (const tab of input.usage.per_tab) {
      for (const metric of metrics) {
        const limitKey = OBJECT_VIEW_RUNTIME_BUDGET_LIMIT_KEYS[metric];
        const limit = budgets.per_tab[limitKey];
        if (typeof limit !== "number") continue;
        const observed = tab[metric];
        const warning = compareObjectViewBudget("tab", tab.tab_id, tab.tab_title, metric, limit, observed);
        if (warning) warnings.push(warning);
      }
    }
  }
  if (input.formFactor === "panel" && budgets.per_panel && input.usage.per_panel) {
    for (const metric of metrics) {
      const limitKey = OBJECT_VIEW_RUNTIME_BUDGET_LIMIT_KEYS[metric];
      const limit = budgets.per_panel[limitKey];
      if (typeof limit !== "number") continue;
      const observed = input.usage.per_panel[metric];
      const warning = compareObjectViewBudget("panel", undefined, undefined, metric, limit, observed);
      if (warning) warnings.push(warning);
    }
  }
  return {
    enabled: true,
    budgets,
    usage: input.usage,
    warnings,
    exceeded: warnings.length > 0,
  };
}

export interface ObjectViewSafeMetadata {
  object_view_id: string;
  object_type_id: string;
  form_factor: ObjectViewFormFactor;
  branch_label?: string | null;
  tab_count: number;
  panel_section_count: number;
  prominent_property_names: string[];
  panel_property_names: string[];
  link_type_ids: string[];
  section_kinds: ObjectViewSectionKind[];
  budgets: ObjectViewRuntimeBudgets;
  permission_context_key: string;
}

export interface ObjectViewMetadataCacheEntry {
  metadata: ObjectViewSafeMetadata;
  cached_at: string;
}

export interface ObjectViewMetadataCache {
  entries: Record<string, ObjectViewMetadataCacheEntry>;
}

export function emptyObjectViewMetadataCache(): ObjectViewMetadataCache {
  return { entries: {} };
}

export function objectViewPermissionContextKey(
  principal?: OntologyPermissionPrincipal | null,
): string {
  if (!principal) return "anonymous";
  const groups = [...(principal.groups || [])].sort();
  const roles = [...(principal.roles || [])].sort();
  const permissions = [...(principal.permissions || [])].sort();
  return [
    principal.user_id || "",
    principal.email || "",
    groups.join(","),
    roles.join(","),
    permissions.join(","),
  ].join("|");
}

function objectViewMetadataCacheKey(
  objectViewId: string,
  formFactor: ObjectViewFormFactor,
  contextKey: string,
): string {
  return `${objectViewId}::${formFactor}::${contextKey}`;
}

export function buildObjectViewSafeMetadata(input: {
  view: ObjectViewDefinition;
  config: ObjectViewConfig;
  principal?: OntologyPermissionPrincipal | null;
}): ObjectViewSafeMetadata {
  const config = input.config;
  const budgets = objectViewRuntimeBudgets(config);
  return {
    object_view_id: input.view.id,
    object_type_id: input.view.object_type_id,
    form_factor: input.view.form_factor,
    branch_label: input.view.branch_label ?? config.branch_label ?? null,
    tab_count: config.tabs?.length ?? 0,
    panel_section_count: config.panel_config?.section_kinds?.length ?? 0,
    prominent_property_names: [...(config.prominent_properties ?? [])],
    panel_property_names: [...(config.panel_properties ?? [])],
    link_type_ids: [...(config.metadata?.link_type_ids ?? [])],
    section_kinds: config.sections.map((section) => section.kind),
    budgets,
    permission_context_key: objectViewPermissionContextKey(input.principal),
  };
}

export function cacheObjectViewSafeMetadata(input: {
  cache: ObjectViewMetadataCache;
  metadata: ObjectViewSafeMetadata;
  now?: string;
}): ObjectViewMetadataCache {
  const key = objectViewMetadataCacheKey(
    input.metadata.object_view_id,
    input.metadata.form_factor,
    input.metadata.permission_context_key,
  );
  return {
    entries: {
      ...input.cache.entries,
      [key]: {
        metadata: input.metadata,
        cached_at: input.now ?? new Date().toISOString(),
      },
    },
  };
}

export function getObjectViewSafeMetadata(input: {
  cache: ObjectViewMetadataCache;
  objectViewId: string;
  formFactor: ObjectViewFormFactor;
  principal?: OntologyPermissionPrincipal | null;
}): ObjectViewSafeMetadata | null {
  const key = objectViewMetadataCacheKey(
    input.objectViewId,
    input.formFactor,
    objectViewPermissionContextKey(input.principal),
  );
  const entry = input.cache.entries[key];
  return entry ? entry.metadata : null;
}

export function invalidateObjectViewMetadataCache(input: {
  cache: ObjectViewMetadataCache;
  objectViewId?: string;
  principal?: OntologyPermissionPrincipal | null;
}): ObjectViewMetadataCache {
  if (!input.objectViewId && !input.principal) {
    return emptyObjectViewMetadataCache();
  }
  const contextKey = input.principal ? objectViewPermissionContextKey(input.principal) : null;
  const entries: Record<string, ObjectViewMetadataCacheEntry> = {};
  for (const [key, entry] of Object.entries(input.cache.entries)) {
    if (input.objectViewId && entry.metadata.object_view_id === input.objectViewId) continue;
    if (contextKey && entry.metadata.permission_context_key === contextKey) continue;
    entries[key] = entry;
  }
  return { entries };
}

const PANEL_OBJECT_VIEW_HOSTS: ObjectViewPanelHostConfiguration[] = [
  {
    host: "object_explorer",
    enabled: true,
    surface: "side_panel",
    selected_object_parameter: "selectedObject",
    supports_open_full_view: true,
  },
  {
    host: "workshop",
    enabled: true,
    surface: "workshop_widget",
    selected_object_parameter: "selectedObject",
    supports_open_full_view: true,
  },
  {
    host: "map",
    enabled: true,
    surface: "compact",
    selected_object_parameter: "selectedMapObject",
    supports_open_full_view: true,
  },
  {
    host: "vertex",
    enabled: true,
    surface: "compact",
    selected_object_parameter: "selectedVertexObject",
    supports_open_full_view: true,
  },
  {
    host: "gaia",
    enabled: true,
    surface: "compact",
    selected_object_parameter: "selectedGaiaObject",
    supports_open_full_view: true,
  },
  {
    host: "object_detail_drawer",
    enabled: true,
    surface: "side_panel",
    selected_object_parameter: "selectedObject",
    supports_open_full_view: true,
  },
  {
    host: "action_success_toast",
    enabled: true,
    surface: "compact",
    selected_object_parameter: "createdObject",
    supports_open_full_view: true,
  },
];

function objectViewPanelSectionKinds(config: ObjectViewConfig) {
  const compactKinds: ObjectViewSectionKind[] = ["summary", "properties", "links", "actions"];
  const configuredKinds = config.sections
    .map((section) => section.kind)
    .filter((kind): kind is ObjectViewSectionKind => compactKinds.includes(kind as ObjectViewSectionKind));
  const kinds = configuredKinds.length > 0 ? configuredKinds : ["summary", "properties"];
  return uniqueNonEmpty(kinds).slice(0, 4) as ObjectViewSectionKind[];
}

function mergeObjectViewPanelHosts(
  hosts: ObjectViewPanelHostConfiguration[] | undefined,
) {
  const byHost = new Map<string, ObjectViewPanelHostConfiguration>();
  for (const host of PANEL_OBJECT_VIEW_HOSTS) byHost.set(host.host, host);
  for (const host of hosts ?? []) {
    const fallback = byHost.get(host.host);
    byHost.set(host.host, {
      host: host.host,
      enabled: host.enabled,
      surface: host.surface ?? fallback?.surface ?? "side_panel",
      selected_object_parameter:
        host.selected_object_parameter || fallback?.selected_object_parameter || "selectedObject",
      supports_open_full_view: host.supports_open_full_view ?? fallback?.supports_open_full_view ?? true,
    });
  }
  return Array.from(byHost.values());
}

const OBJECT_VIEW_EMBED_HOST_POLICIES: Record<string, ObjectViewEmbedPolicy> = {
  object_views: {
    host: "object_views",
    allowed: true,
    hides_workspace_chrome: true,
    reason: "Object Views supports embedded full-view rendering.",
  },
  object_explorer: {
    host: "object_explorer",
    allowed: true,
    hides_workspace_chrome: true,
    reason: "Object Explorer can open Object Views without surrounding workspace chrome.",
  },
  workshop: {
    host: "workshop",
    allowed: true,
    hides_workspace_chrome: true,
    reason: "Workshop Object View widgets can embed full Object Views in modals or iframes.",
  },
  map: {
    host: "map",
    allowed: true,
    hides_workspace_chrome: true,
    reason: "Map hosts can open full Object Views from compact panels.",
  },
  vertex: {
    host: "vertex",
    allowed: true,
    hides_workspace_chrome: true,
    reason: "Vertex hosts can open full Object Views from compact panels.",
  },
  gaia: {
    host: "gaia",
    allowed: true,
    hides_workspace_chrome: true,
    reason: "Gaia hosts can open full Object Views from compact panels.",
  },
  object_detail_drawer: {
    host: "object_detail_drawer",
    allowed: true,
    hides_workspace_chrome: true,
    reason: "Detail drawers can embed an Object View without workspace chrome.",
  },
  external_iframe: {
    host: "external_iframe",
    allowed: true,
    hides_workspace_chrome: true,
    reason: "External iframe embeds can request embedded Object View chrome.",
  },
};

export function objectViewEmbedPolicy(host: ObjectViewEmbedHost = "object_views"): ObjectViewEmbedPolicy {
  return OBJECT_VIEW_EMBED_HOST_POLICIES[host] ?? {
    host,
    allowed: false,
    hides_workspace_chrome: false,
    reason: "This host has no local Object View embed policy.",
  };
}

function objectViewUrlObjectTypeId(input: ObjectViewUrlInput) {
  return input.objectTypeId || input.objectType?.id || input.object?.object_type_id || "";
}

function objectViewUrlPrimaryKey(input: ObjectViewUrlInput) {
  const property = input.primaryKeyProperty || objectViewPrimaryKeyProperty(input.objectType);
  const value = input.primaryKeyValue ?? objectViewPrimaryKeyValue(input.object, input.objectType);
  if (!property || value === null || value === undefined || value === "") {
    return { property: null, value: null };
  }
  return { property, value: String(value) };
}

export function objectViewConfiguredHref(input: ObjectViewUrlInput) {
  const objectTypeId = objectViewUrlObjectTypeId(input);
  const params = new URLSearchParams();
  params.set("type", objectTypeId);
  const objectId = input.objectId ?? input.object?.id ?? null;
  const primaryKey = objectViewUrlPrimaryKey(input);
  if (objectId) {
    params.set("object", objectId);
  } else if (primaryKey.property && primaryKey.value !== null) {
    params.set(primaryKey.property, primaryKey.value);
    params.set("primaryKey", primaryKey.property);
  }
  params.set("mode", input.mode ?? "configured");
  params.set("factor", input.formFactor ?? "full");
  if (input.branchLabel && input.branchLabel !== "default") params.set("branch", input.branchLabel);
  if (input.tabId) params.set("tab", input.tabId);
  if (input.embedded) params.set("embedded", "true");
  return `/object-views?${params.toString()}`;
}

export function buildObjectViewUrlVariants(input: ObjectViewUrlInput): ObjectViewUrlVariants {
  const objectTypeId = objectViewUrlObjectTypeId(input);
  const objectId = input.objectId ?? input.object?.id ?? null;
  const primaryKey = objectViewUrlPrimaryKey(input);
  const embedPolicy = objectViewEmbedPolicy(input.embedHost ?? "object_views");
  const base = {
    objectTypeId,
    mode: input.mode ?? "configured",
    formFactor: input.formFactor ?? "full",
    branchLabel: input.branchLabel,
    tabId: input.tabId,
  } satisfies ObjectViewUrlInput;
  const byObjectId = objectId ? objectViewConfiguredHref({ ...base, objectId }) : null;
  const byPrimaryKey = primaryKey.property && primaryKey.value !== null
    ? objectViewConfiguredHref({
        ...base,
        primaryKeyProperty: primaryKey.property,
        primaryKeyValue: primaryKey.value,
      })
    : null;
  const embeddedByObjectId = embedPolicy.allowed && objectId
    ? objectViewConfiguredHref({ ...base, objectId, embedded: true })
    : null;
  const embeddedByPrimaryKey = embedPolicy.allowed && primaryKey.property && primaryKey.value !== null
    ? objectViewConfiguredHref({
        ...base,
        primaryKeyProperty: primaryKey.property,
        primaryKeyValue: primaryKey.value,
        embedded: true,
      })
    : null;
  const selectedLocator =
    input.preferPrimaryKey && byPrimaryKey ? "primary_key" : byObjectId ? "object_id" : byPrimaryKey ? "primary_key" : "type_preview";
  const selected =
    selectedLocator === "primary_key" ? byPrimaryKey! : selectedLocator === "object_id" ? byObjectId! : objectViewConfiguredHref(base);
  const warnings: string[] = [];
  if (!objectTypeId) warnings.push("Object View URL is missing an object type ID.");
  if (!byObjectId) warnings.push("Object ID URL is unavailable until an object ID is known.");
  if (!byPrimaryKey) warnings.push("Primary-key URL is unavailable until the primary-key property and value are known.");
  if (!embedPolicy.allowed) warnings.push(embedPolicy.reason);
  return {
    by_object_id: byObjectId,
    by_primary_key: byPrimaryKey,
    embedded_by_object_id: embeddedByObjectId,
    embedded_by_primary_key: embeddedByPrimaryKey,
    selected,
    selected_locator: selectedLocator,
    embed_policy: embedPolicy,
    warnings,
  };
}

export type ObjectViewApplicationHost =
  | "object_explorer"
  | "workshop"
  | "map"
  | "vertex"
  | "gaia"
  | "object_detail_drawer"
  | "action_success_toast"
  | "generated_deep_link"
  | string;

export type ObjectViewApplicationDelivery =
  | "embedded"
  | "modal"
  | "host_panel"
  | "workshop_widget"
  | "deep_link"
  | "unsupported";

export type ObjectViewApplicationHeaderMode = "object_view" | "host_owned" | "hidden";

export type ObjectViewApplicationEmbeddingFallbackKind =
  | "host_header"
  | "toggle_unavailable"
  | "open_full_view"
  | "deep_link_only"
  | "unsupported";

export interface ObjectViewApplicationEmbeddingFallback {
  kind: ObjectViewApplicationEmbeddingFallbackKind;
  message: string;
  href?: string | null;
}

export interface ObjectViewApplicationEmbeddingHostPolicy {
  host: ObjectViewApplicationHost;
  label: string;
  full_delivery: ObjectViewApplicationDelivery;
  panel_delivery: ObjectViewApplicationDelivery;
  default_form_factor: ObjectViewFormFactor;
  supports_embedded_mode: boolean;
  supports_core_custom_toggle: boolean;
  header_mode: ObjectViewApplicationHeaderMode;
  reason: string;
}

export interface ObjectViewApplicationEmbeddingEntry {
  host: ObjectViewApplicationHost;
  label: string;
  full_delivery: ObjectViewApplicationDelivery;
  panel_delivery: ObjectViewApplicationDelivery;
  full_supported: boolean;
  panel_supported: boolean;
  default_form_factor: ObjectViewFormFactor;
  selected_mode: ObjectViewMode;
  custom_is_default: boolean;
  supports_core_custom_toggle: boolean;
  header_mode: ObjectViewApplicationHeaderMode;
  uses_host_header: boolean;
  embed_href: string | null;
  full_href: string | null;
  panel_href: string | null;
  fallbacks: ObjectViewApplicationEmbeddingFallback[];
  warnings: string[];
}

export interface ObjectViewApplicationEmbeddingMatrix {
  entries: ObjectViewApplicationEmbeddingEntry[];
  summary: {
    hosts: number;
    full_supported: number;
    panel_supported: number;
    toggle_supported: number;
    host_header_fallbacks: number;
    generated_deep_links: number;
  };
}

export interface ObjectViewActionSuccessToastLink {
  object_type_id: string;
  object_id: string;
  href: string;
  label: string;
  full_href: string | null;
  panel_href: string | null;
  fallback_kind: ObjectViewApplicationEmbeddingFallbackKind | null;
}

const OBJECT_VIEW_APPLICATION_HOSTS: ObjectViewApplicationHost[] = [
  "object_explorer",
  "workshop",
  "map",
  "vertex",
  "gaia",
  "object_detail_drawer",
  "action_success_toast",
  "generated_deep_link",
];

const OBJECT_VIEW_APPLICATION_EMBEDDING_POLICIES: Record<string, ObjectViewApplicationEmbeddingHostPolicy> = {
  object_explorer: {
    host: "object_explorer",
    label: "Object Explorer",
    full_delivery: "embedded",
    panel_delivery: "host_panel",
    default_form_factor: "panel",
    supports_embedded_mode: true,
    supports_core_custom_toggle: true,
    header_mode: "host_owned",
    reason: "Object Explorer can render a compact panel and open the full Object View.",
  },
  workshop: {
    host: "workshop",
    label: "Workshop",
    full_delivery: "modal",
    panel_delivery: "workshop_widget",
    default_form_factor: "panel",
    supports_embedded_mode: true,
    supports_core_custom_toggle: false,
    header_mode: "host_owned",
    reason: "Workshop Object View widgets embed panels and can open full views in modal-style surfaces.",
  },
  map: {
    host: "map",
    label: "Map",
    full_delivery: "modal",
    panel_delivery: "host_panel",
    default_form_factor: "panel",
    supports_embedded_mode: true,
    supports_core_custom_toggle: true,
    header_mode: "host_owned",
    reason: "Map surfaces use compact selected-object panels and open full Object Views from the title.",
  },
  vertex: {
    host: "vertex",
    label: "Vertex",
    full_delivery: "modal",
    panel_delivery: "host_panel",
    default_form_factor: "panel",
    supports_embedded_mode: true,
    supports_core_custom_toggle: true,
    header_mode: "host_owned",
    reason: "Vertex-like graph surfaces use compact panels and modal full Object Views.",
  },
  gaia: {
    host: "gaia",
    label: "Gaia",
    full_delivery: "modal",
    panel_delivery: "host_panel",
    default_form_factor: "panel",
    supports_embedded_mode: true,
    supports_core_custom_toggle: true,
    header_mode: "host_owned",
    reason: "Gaia-like object selection surfaces use panel Object Views with a full-view handoff.",
  },
  object_detail_drawer: {
    host: "object_detail_drawer",
    label: "Object detail drawer",
    full_delivery: "deep_link",
    panel_delivery: "host_panel",
    default_form_factor: "panel",
    supports_embedded_mode: true,
    supports_core_custom_toggle: true,
    header_mode: "host_owned",
    reason: "Detail drawers own their chrome while embedding panel content and linking to the full Object View.",
  },
  action_success_toast: {
    host: "action_success_toast",
    label: "Action success toast",
    full_delivery: "deep_link",
    panel_delivery: "deep_link",
    default_form_factor: "full",
    supports_embedded_mode: false,
    supports_core_custom_toggle: false,
    header_mode: "hidden",
    reason: "Action success toasts expose generated Object View links after create or modify actions.",
  },
  generated_deep_link: {
    host: "generated_deep_link",
    label: "Generated deep link",
    full_delivery: "deep_link",
    panel_delivery: "deep_link",
    default_form_factor: "full",
    supports_embedded_mode: false,
    supports_core_custom_toggle: true,
    header_mode: "object_view",
    reason: "Generated links preserve object locator, branch, form factor, tab, and core/custom mode.",
  },
};

export function objectViewApplicationEmbeddingHostPolicy(
  host: ObjectViewApplicationHost,
): ObjectViewApplicationEmbeddingHostPolicy {
  return OBJECT_VIEW_APPLICATION_EMBEDDING_POLICIES[host] ?? {
    host,
    label: host,
    full_delivery: "unsupported",
    panel_delivery: "unsupported",
    default_form_factor: "full",
    supports_embedded_mode: false,
    supports_core_custom_toggle: false,
    header_mode: "host_owned",
    reason: "This host has no local Object View application embedding contract.",
  };
}

function objectViewApplicationDeliverySupported(delivery: ObjectViewApplicationDelivery) {
  return delivery !== "unsupported";
}

function objectViewApplicationSelectedMode(
  resolution: ObjectViewModeToggleResolution,
  requestedMode: ObjectViewMode | undefined,
) {
  if (requestedMode && !resolution.core_view && !resolution.custom_view) return requestedMode;
  return resolution.selected_mode;
}

export function buildObjectViewApplicationEmbeddingMatrix(input: {
  objectType?: ObjectType | null;
  objectTypeId?: string | null;
  object?: ObjectInstance | null;
  objectId?: string | null;
  primaryKeyProperty?: string | null;
  primaryKeyValue?: unknown;
  views?: ObjectViewDefinition[];
  mode?: ObjectViewMode;
  formFactor?: ObjectViewFormFactor;
  branchLabel?: string | null;
  tabId?: string | null;
  hosts?: ObjectViewApplicationHost[];
}): ObjectViewApplicationEmbeddingMatrix {
  const objectTypeId = input.objectType?.id || input.objectTypeId || input.object?.object_type_id || "";
  const objectId = input.objectId ?? input.object?.id ?? null;
  const views = input.views ?? [];
  const hosts = input.hosts && input.hosts.length > 0 ? input.hosts : OBJECT_VIEW_APPLICATION_HOSTS;
  const commonUrlInput = {
    objectType: input.objectType ?? undefined,
    objectTypeId,
    object: input.object ?? undefined,
    objectId,
    primaryKeyProperty: input.primaryKeyProperty,
    primaryKeyValue: input.primaryKeyValue,
    branchLabel: input.branchLabel,
    tabId: input.tabId,
  } satisfies ObjectViewUrlInput;

  const entries = hosts.map((host) => {
    const policy = objectViewApplicationEmbeddingHostPolicy(host);
    const fullResolution = resolveObjectViewModeToggle({
      views,
      formFactor: "full",
      host,
      objectTypeId,
      requestedMode: input.mode,
    });
    const panelResolution = resolveObjectViewModeToggle({
      views,
      formFactor: "panel",
      host,
      objectTypeId,
      requestedMode: input.mode,
    });
    const defaultFormFactor = input.formFactor ?? policy.default_form_factor;
    const selectedResolution = defaultFormFactor === "panel" ? panelResolution : fullResolution;
    const fullMode = objectViewApplicationSelectedMode(fullResolution, input.mode);
    const panelMode = objectViewApplicationSelectedMode(panelResolution, input.mode);
    const selectedMode = defaultFormFactor === "panel" ? panelMode : fullMode;
    const fullSupported = objectViewApplicationDeliverySupported(policy.full_delivery);
    const panelSupported = objectViewApplicationDeliverySupported(policy.panel_delivery);
    const fullHref = fullSupported
      ? objectViewConfiguredHref({
          ...commonUrlInput,
          mode: fullMode,
          formFactor: "full",
        })
      : null;
    const panelHref = panelSupported
      ? objectViewConfiguredHref({
          ...commonUrlInput,
          mode: panelMode,
          formFactor: "panel",
        })
      : null;
    const selectedHref = defaultFormFactor === "panel" ? panelHref : fullHref;
    const embedHref = selectedHref
      ? objectViewConfiguredHref({
          ...commonUrlInput,
          mode: selectedMode,
          formFactor: defaultFormFactor,
          embedded: policy.supports_embedded_mode,
        })
      : null;
    const togglePolicy = objectViewToggleHostPolicy(host);
    const fallbacks: ObjectViewApplicationEmbeddingFallback[] = [];
    if (policy.header_mode === "host_owned") {
      fallbacks.push({
        kind: "host_header",
        message: `${policy.label} owns the surrounding header; Object View header controls are suppressed or mapped into host chrome.`,
      });
    }
    if (!policy.supports_core_custom_toggle || !togglePolicy.supports_toggle) {
      fallbacks.push({
        kind: "toggle_unavailable",
        message: togglePolicy.limitation || `${policy.label} uses the default Object View because core/custom toggling is not available locally.`,
      });
    }
    if (defaultFormFactor === "panel" && fullHref) {
      fallbacks.push({
        kind: "open_full_view",
        message: `${policy.label} embeds the panel and exposes an open-full-view handoff.`,
        href: fullHref,
      });
    }
    if (policy.full_delivery === "deep_link" || policy.panel_delivery === "deep_link") {
      fallbacks.push({
        kind: "deep_link_only",
        message: `${policy.label} hands off through generated Object View URLs rather than hosting full workspace chrome.`,
        href: fullHref ?? panelHref,
      });
    }
    if (!fullSupported || !panelSupported) {
      fallbacks.push({
        kind: "unsupported",
        message: `${policy.label} does not support ${!fullSupported && !panelSupported ? "full or panel" : !fullSupported ? "full" : "panel"} Object View embedding locally.`,
      });
    }
    return {
      host,
      label: policy.label,
      full_delivery: policy.full_delivery,
      panel_delivery: policy.panel_delivery,
      full_supported: fullSupported,
      panel_supported: panelSupported,
      default_form_factor: defaultFormFactor,
      selected_mode: selectedMode,
      custom_is_default: selectedResolution.custom_is_default,
      supports_core_custom_toggle: policy.supports_core_custom_toggle && togglePolicy.supports_toggle,
      header_mode: policy.header_mode,
      uses_host_header: policy.header_mode === "host_owned",
      embed_href: embedHref,
      full_href: fullHref,
      panel_href: panelHref,
      fallbacks,
      warnings: fallbacks.map((fallback) => fallback.message),
    } satisfies ObjectViewApplicationEmbeddingEntry;
  });

  return {
    entries,
    summary: {
      hosts: entries.length,
      full_supported: entries.filter((entry) => entry.full_supported).length,
      panel_supported: entries.filter((entry) => entry.panel_supported).length,
      toggle_supported: entries.filter((entry) => entry.supports_core_custom_toggle).length,
      host_header_fallbacks: entries.filter((entry) => entry.uses_host_header).length,
      generated_deep_links: entries.filter((entry) => entry.full_delivery === "deep_link" || entry.panel_delivery === "deep_link").length,
    },
  };
}

function objectViewActionObjectId(value: unknown): string | null {
  if (!value || typeof value !== "object") return null;
  const record = value as Record<string, unknown>;
  const direct = record.id ?? record.object_id ?? record.objectId ?? record.target_object_id ?? record.targetObjectId;
  if (typeof direct === "string" && direct.trim()) return direct;
  return objectViewActionObjectId(record.object) ?? objectViewActionObjectId(record.result) ?? objectViewActionObjectId(record.preview);
}

function objectViewActionObjectTypeId(value: unknown): string | null {
  if (!value || typeof value !== "object") return null;
  const record = value as Record<string, unknown>;
  const direct = record.object_type_id ?? record.objectTypeId ?? record.type_id ?? record.typeId;
  if (typeof direct === "string" && direct.trim()) return direct;
  return objectViewActionObjectTypeId(record.object) ?? objectViewActionObjectTypeId(record.result) ?? objectViewActionObjectTypeId(record.preview);
}

export function buildObjectViewActionSuccessToastLink(input: {
  result?: ExecuteActionResponse | ExecuteBatchActionResponse | null;
  objectTypes?: ObjectType[];
  branchLabel?: string | null;
  mode?: ObjectViewMode;
}): ObjectViewActionSuccessToastLink | null {
  const result = input.result;
  if (!result) return null;
  const batchResults = "results" in result && Array.isArray(result.results) ? result.results : [];
  const firstBatchObject = batchResults.find((entry) => objectViewActionObjectId(entry));
  const objectId = "target_object_id" in result && result.target_object_id
    ? result.target_object_id
    : objectViewActionObjectId("object" in result ? result.object : null)
      ?? objectViewActionObjectId("result" in result ? result.result : null)
      ?? objectViewActionObjectId("preview" in result ? result.preview : null)
      ?? objectViewActionObjectId(firstBatchObject);
  const objectTypeId = result.action?.object_type_id
    || objectViewActionObjectTypeId("object" in result ? result.object : null)
    || objectViewActionObjectTypeId(firstBatchObject)
    || "";
  if (!objectTypeId || !objectId || result.action?.operation_kind === "delete_object") return null;
  const objectType = input.objectTypes?.find((candidate) => candidate.id === objectTypeId) ?? null;
  const entry = buildObjectViewApplicationEmbeddingMatrix({
    objectType,
    objectTypeId,
    objectId,
    mode: input.mode ?? "configured",
    branchLabel: input.branchLabel,
    hosts: ["action_success_toast"],
  }).entries[0];
  const href = entry.full_href ?? entry.panel_href ?? entry.embed_href;
  if (!href) return null;
  return {
    object_type_id: objectTypeId,
    object_id: objectId,
    href,
    label: `Open ${objectType?.display_name || objectType?.name || objectTypeId} Object View`,
    full_href: entry.full_href,
    panel_href: entry.panel_href,
    fallback_kind: entry.fallbacks[0]?.kind ?? null,
  };
}

export type ObjectViewGlobalBranchResourceKind = "ov_managed_module" | "full_object_view_tabs";
export type ObjectViewGlobalBranchResourceStatus = "added" | "modified" | "removed" | "unchanged";
export type ObjectViewGlobalBranchPreviewStatus = "not_requested" | "pending" | "ready" | "blocked";
export type ObjectViewGlobalBranchMergeState = "clean" | "needs_rebase" | "needs_approval" | "blocked" | "merged";
export type ObjectViewGlobalBranchCheckStatus = "passed" | "warning" | "failed";
export type ObjectViewGlobalBranchOperationKind = "add" | "remove" | "preview" | "rebase" | "check" | "approve" | "merge";

export interface ObjectViewGlobalBranchCheck {
  id: string;
  label: string;
  status: ObjectViewGlobalBranchCheckStatus;
  message: string;
  resource_ids: string[];
}

export interface ObjectViewGlobalBranchResource {
  id: string;
  rid: string;
  kind: ObjectViewGlobalBranchResourceKind;
  label: string;
  object_type_id: string;
  object_view_id: string;
  object_view_name: string;
  form_factor: ObjectViewFormFactor;
  branch_label: string;
  main_version: number;
  branch_version: number;
  status: ObjectViewGlobalBranchResourceStatus;
  preview_status: ObjectViewGlobalBranchPreviewStatus;
  merge_state: ObjectViewGlobalBranchMergeState;
  tab_id?: string;
  module_id?: string;
  parent_resource_id?: string;
  associated_resource_ids: string[];
  dependency_resource_ids: string[];
  href: string;
  resource_signature: string;
  latest_ontology_signature: string;
  rebased_ontology_signature: string | null;
  render_ontology_signature: string | null;
  requires_rebase: boolean;
  approved: boolean;
  auto_approved: boolean;
  unsupported_legacy_fields: boolean;
  permission_allowed: boolean;
  permission_reason: string;
  removed_with_resource_id?: string;
}

export interface ObjectViewGlobalBranchPreviewSummary {
  status: ObjectViewGlobalBranchPreviewStatus;
  ready_count: number;
  blocked_count: number;
  pending_count: number;
  render_ontology_signature: string;
}

export interface ObjectViewGlobalBranchAdapterState {
  branch_label: string;
  main_branch_label: string;
  latest_ontology_signature: string;
  resources: ObjectViewGlobalBranchResource[];
  checks: ObjectViewGlobalBranchCheck[];
  preview: ObjectViewGlobalBranchPreviewSummary;
  mergeable: boolean;
  auto_approved: boolean;
  approved_resource_count: number;
}

export interface ObjectViewGlobalBranchAdapterOperation {
  kind: ObjectViewGlobalBranchOperationKind;
  resource_id?: string;
  actor_id?: string;
  now?: string;
  comment?: string;
}

export interface ObjectViewGlobalBranchAdapterOperationResult {
  operation: ObjectViewGlobalBranchOperationKind;
  state: ObjectViewGlobalBranchAdapterState;
  warnings: string[];
  errors: string[];
}

export type ObjectViewGlobalBranchRebaseResolutionChoice = "main" | "branch" | "custom";
export type ObjectViewGlobalBranchRebaseDisposition =
  | "unchanged"
  | "auto_accepted"
  | "conflict"
  | "manual_resolution";

export interface ObjectViewGlobalBranchRebaseResourceState {
  resource_id: string;
  kind: ObjectViewGlobalBranchResourceKind;
  label: string;
  branch_label: string;
  version: number;
  signature: string;
  summary: string;
  tab_id?: string;
  module_id?: string;
  href: string;
  details: Record<string, unknown>;
}

export interface ObjectViewGlobalBranchRebaseRow {
  resource_id: string;
  kind: ObjectViewGlobalBranchResourceKind;
  label: string;
  main_state: ObjectViewGlobalBranchRebaseResourceState | null;
  branch_state: ObjectViewGlobalBranchRebaseResourceState | null;
  proposed_state: ObjectViewGlobalBranchRebaseResourceState | null;
  disposition: ObjectViewGlobalBranchRebaseDisposition;
  auto_accepted: boolean;
  requires_manual_resolution: boolean;
  conflict_fields: string[];
  resolution_choice: ObjectViewGlobalBranchRebaseResolutionChoice | null;
  resolution_options: ObjectViewGlobalBranchRebaseResolutionChoice[];
  message: string;
}

export interface ObjectViewGlobalBranchRebaseModel {
  branch_label: string;
  latest_ontology_signature: string;
  rows: ObjectViewGlobalBranchRebaseRow[];
  auto_accepted_count: number;
  conflict_count: number;
  unresolved_conflict_count: number;
  manual_resolution_count: number;
  can_finish: boolean;
  deployability_checks_after_rebase: ObjectViewGlobalBranchCheck[];
  warnings: string[];
}

function normalizeObjectViewBranchLabel(value: string | null | undefined) {
  const label = (value || "").trim();
  if (!label) return "main";
  if (["main", "default", "core"].includes(label.toLowerCase())) return "main";
  return label;
}

function objectViewGlobalBranchRID(...parts: string[]) {
  return `ri.object-view-branch.${parts.map((part) => objectViewStableToken(part)).join(".")}`;
}

function objectViewBranchVersion(value: unknown, fallback = 1) {
  const numeric = Number(value);
  return Number.isFinite(numeric) && numeric > 0 ? numeric : fallback;
}

function objectViewBranchResourceSignature(value: unknown) {
  return JSON.stringify(value ?? null);
}

function objectViewBranchLatestOntologySignature(input: {
  objectType: ObjectType | null;
  properties: Property[];
  linkTypes: LinkType[];
}) {
  if (!input.objectType) return "missing-object-type";
  return objectViewDefaultMetadataSignature({
    objectType: input.objectType,
    properties: input.properties,
    linkTypes: input.linkTypes.filter((link) =>
      link.source_type_id === input.objectType?.id || link.target_type_id === input.objectType?.id,
    ),
    formFactor: "full",
  });
}

function objectViewBranchUnsupportedLegacyFields(config: ObjectViewConfig | undefined) {
  const metadata = config?.metadata as (ObjectViewConfig["metadata"] & Record<string, unknown>) | undefined;
  return Boolean(
    metadata?.legacy_fields_modified ||
    metadata?.legacy_builder ||
    (config as unknown as Record<string, unknown> | undefined)?.legacy_fields_modified ||
    (config as unknown as Record<string, unknown> | undefined)?.legacy_builder,
  );
}

function objectViewBranchResourceHref(view: ObjectViewDefinition, resource: {
  branch_label: string;
  form_factor: ObjectViewFormFactor;
  tab_id?: string;
}) {
  return objectViewConfiguredHref({
    objectTypeId: view.object_type_id,
    mode: view.mode,
    formFactor: resource.form_factor,
    branchLabel: resource.branch_label,
    tabId: resource.tab_id,
  });
}

function objectViewBranchResourcePermission(input: {
  objectType: ObjectType | null;
  objectView: ObjectViewDefinition;
  config?: ObjectViewConfig;
  principal?: OntologyPermissionPrincipal | null;
}) {
  if (!input.principal || !input.objectType) {
    return {
      allowed: true,
      reason: input.objectType ? "Permission check deferred until branch proposal review." : "Object type is missing for permission evaluation.",
    };
  }
  return buildObjectViewEditPermissionDecision({
    objectType: input.objectType,
    objectView: input.objectView,
    config: input.config,
    principal: input.principal,
  });
}

function objectViewBranchBaseResource(input: {
  kind: ObjectViewGlobalBranchResourceKind;
  label: string;
  objectView: ObjectViewDefinition;
  config: ObjectViewConfig;
  objectType: ObjectType | null;
  properties: Property[];
  linkTypes: LinkType[];
  branchLabel: string;
  resourceId: string;
  rid: string;
  mainVersion: number;
  branchVersion: number;
  tabId?: string;
  moduleId?: string;
  parentResourceId?: string;
  associatedResourceIds?: string[];
  dependencies?: string[];
  principal?: OntologyPermissionPrincipal | null;
}): ObjectViewGlobalBranchResource {
  const latestOntologySignature = objectViewBranchLatestOntologySignature({
    objectType: input.objectType,
    properties: input.properties,
    linkTypes: input.linkTypes,
  });
  const rebasedOntologySignature = input.config.metadata?.branch_rebased_ontology_signature ?? null;
  const branchLabel = normalizeObjectViewBranchLabel(input.branchLabel);
  const requiresRebase = branchLabel !== "main" && rebasedOntologySignature !== latestOntologySignature;
  const permission = objectViewBranchResourcePermission({
    objectType: input.objectType,
    objectView: input.objectView,
    config: input.config,
    principal: input.principal,
  });
  const unsupportedLegacyFields = objectViewBranchUnsupportedLegacyFields(input.config);
  return {
    id: input.resourceId,
    rid: input.rid,
    kind: input.kind,
    label: input.label,
    object_type_id: input.objectView.object_type_id,
    object_view_id: input.objectView.id,
    object_view_name: input.objectView.display_name || input.objectView.name,
    form_factor: input.objectView.form_factor,
    branch_label: branchLabel,
    main_version: input.mainVersion,
    branch_version: input.branchVersion,
    status: branchLabel === "main" ? "unchanged" : input.mainVersion > 0 ? "modified" : "added",
    preview_status: branchLabel === "main" ? "ready" : "pending",
    merge_state: requiresRebase ? "needs_rebase" : "clean",
    tab_id: input.tabId,
    module_id: input.moduleId,
    parent_resource_id: input.parentResourceId,
    associated_resource_ids: input.associatedResourceIds ?? [],
    dependency_resource_ids: uniqueNonEmpty([
      `object_type:${input.objectView.object_type_id}`,
      ...(input.dependencies ?? []),
    ]),
    href: objectViewBranchResourceHref(input.objectView, {
      branch_label: branchLabel,
      form_factor: input.objectView.form_factor,
      tab_id: input.tabId,
    }),
    resource_signature: objectViewBranchResourceSignature({
      kind: input.kind,
      config: input.config,
      tab_id: input.tabId,
      module_id: input.moduleId,
    }),
    latest_ontology_signature: latestOntologySignature,
    rebased_ontology_signature: rebasedOntologySignature,
    render_ontology_signature: null,
    requires_rebase: requiresRebase,
    approved: true,
    auto_approved: true,
    unsupported_legacy_fields: unsupportedLegacyFields,
    permission_allowed: permission.allowed,
    permission_reason: permission.reason,
  };
}

export function buildObjectViewGlobalBranchResources(input: {
  branchLabel?: string | null;
  objectViews: ObjectViewDefinition[];
  mainObjectViews?: ObjectViewDefinition[];
  objectTypes: ObjectType[];
  propertiesByObjectType?: Record<string, Property[]>;
  linkTypes?: LinkType[];
  principal?: OntologyPermissionPrincipal | null;
}): ObjectViewGlobalBranchResource[] {
  const branchLabel = normalizeObjectViewBranchLabel(input.branchLabel);
  const mainById = new Map((input.mainObjectViews ?? []).map((view) => [view.id, view]));
  const typeById = new Map(input.objectTypes.map((type) => [type.id, type]));
  const linkTypes = input.linkTypes ?? [];
  const byId = new Map<string, ObjectViewGlobalBranchResource>();

  for (const view of input.objectViews) {
    if (view.mode !== "configured") continue;
    const config = view.config;
    if (!config) continue;
    const resourceBranchLabel = normalizeObjectViewBranchLabel(view.branch_label ?? config.branch_label ?? branchLabel);
    if (branchLabel !== "main" && resourceBranchLabel !== branchLabel) continue;
    const objectType = typeById.get(view.object_type_id) ?? null;
    const properties = input.propertiesByObjectType?.[view.object_type_id] ?? objectType?.properties ?? [];
    const mainView = mainById.get(view.id);
    const mainVersion = objectViewBranchVersion(mainView?.config?.object_view_version, mainView ? 1 : 0);
    const branchVersion = objectViewBranchVersion(config.object_view_version, 1);
    const tabsResourceId = `object_view_tabs:${view.object_type_id}:${view.id}`;
    const tabsModuleResourceIds = (view.form_factor === "full" ? config.tabs ?? [] : [])
      .map((tab) => `ov_managed_module:${view.object_type_id}:${view.id}:${tab.module.id}`);

    if (view.form_factor === "full") {
      byId.set(tabsResourceId, objectViewBranchBaseResource({
        kind: "full_object_view_tabs",
        label: `${view.display_name || view.name} tabs`,
        objectView: view,
        config,
        objectType,
        properties,
        linkTypes,
        branchLabel: resourceBranchLabel,
        resourceId: tabsResourceId,
        rid: objectViewGlobalBranchRID("tabs", view.object_type_id, view.id),
        mainVersion,
        branchVersion,
        associatedResourceIds: tabsModuleResourceIds,
        dependencies: tabsModuleResourceIds,
        principal: input.principal,
      }));
    }

    for (const tab of view.form_factor === "full" ? config.tabs ?? [] : []) {
      const resourceId = `ov_managed_module:${view.object_type_id}:${view.id}:${tab.module.id}`;
      byId.set(resourceId, objectViewBranchBaseResource({
        kind: "ov_managed_module",
        label: `${tab.title} module`,
        objectView: view,
        config,
        objectType,
        properties,
        linkTypes,
        branchLabel: resourceBranchLabel,
        resourceId,
        rid: objectViewGlobalBranchRID("module", view.object_type_id, tab.module.id),
        mainVersion: objectViewBranchVersion(mainView?.config?.workshop_module_version, mainView ? 1 : 0),
        branchVersion: objectViewBranchVersion(tab.module.version, config.workshop_module_version ?? 1),
        tabId: tab.id,
        moduleId: tab.module.id,
        parentResourceId: tabsResourceId,
        dependencies: [tabsResourceId],
        principal: input.principal,
      }));
    }

    const panelWidget = config.panel_config?.workshop_widget;
    if (panelWidget?.enabled) {
      const moduleId = `${panelWidget.widget_id}:object-instance-panel`;
      const resourceId = `ov_managed_module:${view.object_type_id}:${view.id}:${moduleId}`;
      byId.set(resourceId, objectViewBranchBaseResource({
        kind: "ov_managed_module",
        label: `${view.display_name || view.name} object instance panel module`,
        objectView: { ...view, form_factor: "panel" },
        config,
        objectType,
        properties,
        linkTypes,
        branchLabel: resourceBranchLabel,
        resourceId,
        rid: objectViewGlobalBranchRID("module", view.object_type_id, moduleId),
        mainVersion: objectViewBranchVersion(mainView?.config?.workshop_module_version, mainView ? 1 : 0),
        branchVersion: objectViewBranchVersion(config.workshop_module_version, 1),
        tabId: "panel:object-instance",
        moduleId,
        dependencies: [],
        principal: input.principal,
      }));
    }
  }

  return Array.from(byId.values()).sort((left, right) => {
    const kind = left.kind.localeCompare(right.kind);
    if (kind !== 0) return kind;
    return left.label.localeCompare(right.label);
  });
}

function objectViewGlobalBranchChecks(resources: ObjectViewGlobalBranchResource[]): ObjectViewGlobalBranchCheck[] {
  const active = resources.filter((resource) => resource.status !== "removed");
  const checks: ObjectViewGlobalBranchCheck[] = [];
  checks.push({
    id: "resources-present",
    label: "Resources tracked",
    status: active.length > 0 ? "passed" : "warning",
    message: active.length > 0
      ? `${active.length} Object View branch resources are tracked.`
      : "No Object View resources are tracked on this branch.",
    resource_ids: active.map((resource) => resource.id),
  });

  const needsRebase = active.filter((resource) => resource.requires_rebase);
  checks.push({
    id: "rebased-with-main",
    label: "Rebased with main",
    status: needsRebase.length === 0 ? "passed" : "failed",
    message: needsRebase.length === 0
      ? "Object View resources are rebased with the latest ontology state."
      : `${needsRebase.length} Object View resources must be rebased before merge.`,
    resource_ids: needsRebase.map((resource) => resource.id),
  });

  const legacy = active.filter((resource) => resource.unsupported_legacy_fields);
  checks.push({
    id: "no-legacy-fields",
    label: "No legacy Object View fields modified",
    status: legacy.length === 0 ? "passed" : "failed",
    message: legacy.length === 0
      ? "No unsupported legacy Object View fields are modified."
      : "Unsupported legacy Object View fields were modified on this branch.",
    resource_ids: legacy.map((resource) => resource.id),
  });

  const permissionBlocked = active.filter((resource) => !resource.permission_allowed);
  checks.push({
    id: "publish-permissions",
    label: "Publish permissions",
    status: permissionBlocked.length === 0 ? "passed" : "failed",
    message: permissionBlocked.length === 0
      ? "Object View publish/edit permissions are satisfied or deferred to proposal review."
      : `${permissionBlocked.length} resources are missing Object View publish/edit permission.`,
    resource_ids: permissionBlocked.map((resource) => resource.id),
  });

  const stalePreview = active.filter((resource) =>
    resource.preview_status === "ready" && resource.render_ontology_signature !== resource.latest_ontology_signature,
  );
  checks.push({
    id: "preview-latest-ontology",
    label: "Preview uses branch ontology",
    status: stalePreview.length === 0 ? "passed" : "failed",
    message: stalePreview.length === 0
      ? "Ready previews render against the latest ontology state on the same branch."
      : "Some previews were rendered against an older ontology state.",
    resource_ids: stalePreview.map((resource) => resource.id),
  });

  const unapproved = active.filter((resource) => !resource.approved && !resource.auto_approved);
  checks.push({
    id: "approved",
    label: "Approvals",
    status: unapproved.length === 0 ? "passed" : "failed",
    message: unapproved.length === 0
      ? "Object View branch resources are approved; local Object View approvals are auto-approved until manual approvals land."
      : `${unapproved.length} resources still need approval.`,
    resource_ids: unapproved.map((resource) => resource.id),
  });

  return checks;
}

function objectViewGlobalBranchPreviewSummary(
  resources: ObjectViewGlobalBranchResource[],
  latestOntologySignature: string,
): ObjectViewGlobalBranchPreviewSummary {
  const active = resources.filter((resource) => resource.status !== "removed");
  const readyCount = active.filter((resource) => resource.preview_status === "ready").length;
  const blockedCount = active.filter((resource) => resource.preview_status === "blocked").length;
  const pendingCount = active.filter((resource) => resource.preview_status === "pending" || resource.preview_status === "not_requested").length;
  return {
    status: blockedCount > 0 ? "blocked" : pendingCount > 0 ? "pending" : "ready",
    ready_count: readyCount,
    blocked_count: blockedCount,
    pending_count: pendingCount,
    render_ontology_signature: latestOntologySignature,
  };
}

function buildObjectViewGlobalBranchStateFromResources(input: {
  branchLabel: string;
  mainBranchLabel?: string;
  latestOntologySignature: string;
  resources: ObjectViewGlobalBranchResource[];
}): ObjectViewGlobalBranchAdapterState {
  const checks = objectViewGlobalBranchChecks(input.resources);
  const active = input.resources.filter((resource) => resource.status !== "removed");
  return {
    branch_label: normalizeObjectViewBranchLabel(input.branchLabel),
    main_branch_label: input.mainBranchLabel ?? "main",
    latest_ontology_signature: input.latestOntologySignature,
    resources: input.resources,
    checks,
    preview: objectViewGlobalBranchPreviewSummary(input.resources, input.latestOntologySignature),
    mergeable: active.length > 0 && checks.every((check) => check.status !== "failed"),
    auto_approved: active.every((resource) => resource.auto_approved),
    approved_resource_count: active.filter((resource) => resource.approved || resource.auto_approved).length,
  };
}

export function buildObjectViewGlobalBranchAdapterState(input: {
  branchLabel?: string | null;
  mainBranchLabel?: string;
  objectViews: ObjectViewDefinition[];
  mainObjectViews?: ObjectViewDefinition[];
  objectTypes: ObjectType[];
  propertiesByObjectType?: Record<string, Property[]>;
  linkTypes?: LinkType[];
  principal?: OntologyPermissionPrincipal | null;
}): ObjectViewGlobalBranchAdapterState {
  const branchLabel = normalizeObjectViewBranchLabel(input.branchLabel);
  const resources = buildObjectViewGlobalBranchResources({
    branchLabel,
    objectViews: input.objectViews,
    mainObjectViews: input.mainObjectViews,
    objectTypes: input.objectTypes,
    propertiesByObjectType: input.propertiesByObjectType,
    linkTypes: input.linkTypes,
    principal: input.principal,
  });
  const latestOntologySignature = resources[0]?.latest_ontology_signature ?? "no-object-view-ontology-state";
  return buildObjectViewGlobalBranchStateFromResources({
    branchLabel,
    mainBranchLabel: input.mainBranchLabel,
    latestOntologySignature,
    resources,
  });
}

export function applyObjectViewGlobalBranchAdapterOperation(
  state: ObjectViewGlobalBranchAdapterState,
  operation: ObjectViewGlobalBranchAdapterOperation,
): ObjectViewGlobalBranchAdapterOperationResult {
  const now = operation.now ?? new Date().toISOString();
  const warnings: string[] = [];
  const errors: string[] = [];
  const selected = operation.resource_id
    ? new Set([operation.resource_id])
    : new Set(state.resources.map((resource) => resource.id));
  let resources = state.resources.map((resource) => ({ ...resource }));

  const hasSelected = !operation.resource_id || resources.some((resource) => resource.id === operation.resource_id);
  if (!hasSelected) {
    errors.push(`Object View branch resource not found: ${operation.resource_id}`);
  }

  if (operation.kind === "add") {
    resources = resources.map((resource) =>
      selected.has(resource.id)
        ? {
            ...resource,
            status: resource.status === "unchanged" ? "modified" : resource.status,
            preview_status: "pending",
            merge_state: resource.requires_rebase ? "needs_rebase" : "clean",
          }
        : resource,
    );
  } else if (operation.kind === "remove") {
    const cascade = new Set<string>();
    for (const resource of resources) {
      if (!selected.has(resource.id)) continue;
      if (resource.kind === "full_object_view_tabs") {
        for (const childId of resource.associated_resource_ids) cascade.add(childId);
      }
    }
    resources = resources.map((resource) =>
      selected.has(resource.id) || cascade.has(resource.id)
        ? {
            ...resource,
            status: "removed",
            preview_status: "not_requested",
            merge_state: "clean",
            removed_with_resource_id: selected.has(resource.id) ? undefined : operation.resource_id,
          }
        : resource,
    );
    if (cascade.size > 0) {
      warnings.push("Removing a full Object View tabs resource also removes its associated OV-managed tab modules from the branch.");
    }
  } else if (operation.kind === "preview") {
    resources = resources.map((resource) => {
      if (!selected.has(resource.id) || resource.status === "removed") return resource;
      const blocked = resource.unsupported_legacy_fields || !resource.permission_allowed;
      return {
        ...resource,
        preview_status: blocked ? "blocked" : "ready",
        render_ontology_signature: state.latest_ontology_signature,
        merge_state: blocked ? "blocked" : resource.merge_state,
      };
    });
  } else if (operation.kind === "rebase") {
    resources = resources.map((resource) =>
      selected.has(resource.id) && resource.status !== "removed"
        ? {
            ...resource,
            requires_rebase: false,
            rebased_ontology_signature: state.latest_ontology_signature,
            preview_status: "pending",
            merge_state: "clean",
            dependency_resource_ids: uniqueNonEmpty(resource.dependency_resource_ids),
          }
        : resource,
    );
    warnings.push(`Rebased Object View branch resources at ${now}; deployability checks should be rerun.`);
  } else if (operation.kind === "approve") {
    resources = resources.map((resource) =>
      selected.has(resource.id) && resource.status !== "removed"
        ? {
            ...resource,
            approved: true,
            auto_approved: resource.auto_approved || !operation.actor_id,
            merge_state: resource.requires_rebase ? "needs_rebase" : "clean",
          }
        : resource,
    );
  } else if (operation.kind === "merge") {
    const failed = state.checks.filter((check) => check.status === "failed");
    if (failed.length > 0) {
      errors.push("Cannot merge Object View branch resources until deployability checks pass.");
      resources = resources.map((resource) =>
        selected.has(resource.id) && resource.status !== "removed"
          ? { ...resource, merge_state: "blocked" }
          : resource,
      );
    } else {
      resources = resources.map((resource) =>
        selected.has(resource.id)
          ? {
              ...resource,
              status: resource.status === "removed" ? "removed" : "unchanged",
              branch_label: state.main_branch_label,
              preview_status: resource.status === "removed" ? "not_requested" : "ready",
              merge_state: "merged",
              requires_rebase: false,
              render_ontology_signature: state.latest_ontology_signature,
            }
          : resource,
      );
    }
  }

  const nextState = buildObjectViewGlobalBranchStateFromResources({
    branchLabel: state.branch_label,
    mainBranchLabel: state.main_branch_label,
    latestOntologySignature: state.latest_ontology_signature,
    resources,
  });
  return {
    operation: operation.kind,
    state: nextState,
    warnings,
    errors,
  };
}

function objectViewRebaseTabDetails(config: ObjectViewConfig) {
  const tabs = objectViewReorderTabs(config.tabs ?? []);
  return {
    tab_count: tabs.length,
    tab_order: tabs.map((tab) => tab.id),
    tab_titles: Object.fromEntries(tabs.map((tab) => [tab.id, tab.title])),
    tab_visibility: Object.fromEntries(tabs.map((tab) => [tab.id, tab.visibility])),
    tab_modules: Object.fromEntries(tabs.map((tab) => [tab.id, tab.module.id])),
    selected_tab_id: config.selected_tab_id ?? tabs[0]?.id ?? null,
  };
}

function objectViewRebaseTabSummary(details: Record<string, unknown>) {
  const order = Array.isArray(details.tab_order) ? details.tab_order as string[] : [];
  const titles = details.tab_titles && typeof details.tab_titles === "object" ? details.tab_titles as Record<string, unknown> : {};
  const visibility = details.tab_visibility && typeof details.tab_visibility === "object" ? details.tab_visibility as Record<string, unknown> : {};
  if (order.length === 0) return "No full Object View tabs.";
  return order.map((id) => `${String(titles[id] ?? id)} (${String(visibility[id] ?? "visible")})`).join(", ");
}

function objectViewRebaseModuleDetails(module: ObjectViewWorkshopModuleDefinition, tab?: ObjectViewTabDefinition) {
  return {
    module_id: module.id,
    module_name: module.name,
    module_display_name: module.display_name,
    module_version: module.version,
    object_context_parameter: module.object_context_parameter,
    widget_count: module.widgets.length,
    widget_ids: module.widgets.map((widget) => widget.id),
    widget_kinds: module.widgets.map((widget) => widget.kind),
    tab_id: tab?.id ?? null,
    tab_title: tab?.title ?? null,
    tab_visibility: tab?.visibility ?? null,
  };
}

function objectViewRebaseModuleSummary(details: Record<string, unknown>) {
  const title = details.tab_title ? `${details.tab_title} ` : "";
  return `${title}module v${String(details.module_version ?? "1")} with ${String(details.widget_count ?? 0)} widgets`;
}

function objectViewRebaseDetailMap(views: ObjectViewDefinition[]) {
  const details = new Map<string, { summary: string; details: Record<string, unknown> }>();
  for (const view of views) {
    if (view.mode !== "configured" || !view.config) continue;
    const config = view.config;
    if (view.form_factor === "full") {
      const tabsResourceId = `object_view_tabs:${view.object_type_id}:${view.id}`;
      const tabDetails = objectViewRebaseTabDetails(config);
      details.set(tabsResourceId, {
        summary: objectViewRebaseTabSummary(tabDetails),
        details: tabDetails,
      });
      for (const tab of config.tabs ?? []) {
        const moduleDetails = objectViewRebaseModuleDetails(tab.module, tab);
        details.set(`ov_managed_module:${view.object_type_id}:${view.id}:${tab.module.id}`, {
          summary: objectViewRebaseModuleSummary(moduleDetails),
          details: moduleDetails,
        });
      }
    }

    const panelWidget = config.panel_config?.workshop_widget;
    if (panelWidget?.enabled) {
      const moduleId = `${panelWidget.widget_id}:object-instance-panel`;
      const panelModule: ObjectViewWorkshopModuleDefinition = {
        id: moduleId,
        name: `${objectTypeAPIName({ id: view.object_type_id, name: view.object_type_id, display_name: view.object_type_id })}PanelModule`,
        display_name: `${view.display_name || view.name} panel module`,
        version: config.workshop_module_version ?? 1,
        form_factor: "panel",
        object_context_parameter: panelWidget.selected_object_parameter,
        source: "user_managed",
        widgets: [],
        updated_at: config.last_saved_at || view.updated_at || view.created_at || "",
      };
      const moduleDetails = {
        ...objectViewRebaseModuleDetails(panelModule),
        widget_id: panelWidget.widget_id,
        height_px: panelWidget.height_px,
      };
      details.set(`ov_managed_module:${view.object_type_id}:${view.id}:${moduleId}`, {
        summary: `Object instance panel module v${String(moduleDetails.module_version)} (${panelWidget.height_px}px)`,
        details: moduleDetails,
      });
    }
  }
  return details;
}

function objectViewRebaseStates(input: {
  branchLabel: string;
  objectViews: ObjectViewDefinition[];
  objectTypes: ObjectType[];
  propertiesByObjectType?: Record<string, Property[]>;
  linkTypes?: LinkType[];
  principal?: OntologyPermissionPrincipal | null;
}) {
  const resources = buildObjectViewGlobalBranchResources({
    branchLabel: input.branchLabel,
    objectViews: input.objectViews,
    objectTypes: input.objectTypes,
    propertiesByObjectType: input.propertiesByObjectType,
    linkTypes: input.linkTypes,
    principal: input.principal,
  });
  const detailMap = objectViewRebaseDetailMap(input.objectViews);
  return new Map(resources.map((resource) => {
    const detail = detailMap.get(resource.id) ?? { summary: resource.label, details: {} };
    return [resource.id, {
      resource_id: resource.id,
      kind: resource.kind,
      label: resource.label,
      branch_label: resource.branch_label,
      version: resource.branch_version,
      signature: resource.resource_signature,
      summary: detail.summary,
      tab_id: resource.tab_id,
      module_id: resource.module_id,
      href: resource.href,
      details: detail.details,
    } satisfies ObjectViewGlobalBranchRebaseResourceState];
  }));
}

function objectViewRebaseChangedFields(
  kind: ObjectViewGlobalBranchResourceKind,
  mainState: ObjectViewGlobalBranchRebaseResourceState,
  branchState: ObjectViewGlobalBranchRebaseResourceState,
) {
  const fields = kind === "full_object_view_tabs"
    ? ["tab_count", "tab_order", "tab_titles", "tab_visibility", "selected_tab_id", "tab_modules"]
    : ["module_display_name", "module_version", "object_context_parameter", "widget_count", "widget_ids", "widget_kinds", "tab_visibility", "height_px"];
  return fields.filter((field) =>
    JSON.stringify(mainState.details[field] ?? null) !== JSON.stringify(branchState.details[field] ?? null),
  );
}

function objectViewRebaseHasConflict(
  kind: ObjectViewGlobalBranchResourceKind,
  mainState: ObjectViewGlobalBranchRebaseResourceState | null,
  branchState: ObjectViewGlobalBranchRebaseResourceState | null,
  changedFields: string[],
) {
  if (!mainState || !branchState || changedFields.length === 0) return false;
  if (kind === "ov_managed_module") return true;
  const mainOrder = Array.isArray(mainState.details.tab_order) ? mainState.details.tab_order as string[] : [];
  const branchOrder = Array.isArray(branchState.details.tab_order) ? branchState.details.tab_order as string[] : [];
  const sharedTabs = mainOrder.filter((id) => branchOrder.includes(id));
  if (sharedTabs.length === 0) return false;
  const mainTitles = mainState.details.tab_titles as Record<string, unknown> | undefined;
  const branchTitles = branchState.details.tab_titles as Record<string, unknown> | undefined;
  const mainVisibility = mainState.details.tab_visibility as Record<string, unknown> | undefined;
  const branchVisibility = branchState.details.tab_visibility as Record<string, unknown> | undefined;
  return sharedTabs.some((id) =>
    JSON.stringify(mainTitles?.[id] ?? null) !== JSON.stringify(branchTitles?.[id] ?? null) ||
    JSON.stringify(mainVisibility?.[id] ?? null) !== JSON.stringify(branchVisibility?.[id] ?? null),
  );
}

function objectViewRebaseProposedState(input: {
  mainState: ObjectViewGlobalBranchRebaseResourceState | null;
  branchState: ObjectViewGlobalBranchRebaseResourceState | null;
  choice: ObjectViewGlobalBranchRebaseResolutionChoice | null;
  conflict: boolean;
}) {
  if (!input.mainState) return input.branchState;
  if (!input.branchState) return input.mainState;
  if (input.conflict) {
    if (input.choice === "main") return input.mainState;
    if (input.choice === "branch" || input.choice === "custom") return input.branchState;
    return {
      ...input.branchState,
      summary: `Pending manual choice. Branch preview: ${input.branchState.summary}`,
    };
  }
  if (input.mainState.signature === input.branchState.signature) return input.branchState;
  return {
    ...input.branchState,
    signature: objectViewBranchResourceSignature({
      main: input.mainState.signature,
      branch: input.branchState.signature,
    }),
    summary: `Auto-accepted result: ${input.branchState.summary}; main-only non-conflicting changes are retained where applicable.`,
  };
}

export function buildObjectViewGlobalBranchRebaseModel(input: {
  branchLabel?: string | null;
  mainObjectViews: ObjectViewDefinition[];
  branchObjectViews: ObjectViewDefinition[];
  objectTypes: ObjectType[];
  propertiesByObjectType?: Record<string, Property[]>;
  linkTypes?: LinkType[];
  principal?: OntologyPermissionPrincipal | null;
  resolutions?: Record<string, ObjectViewGlobalBranchRebaseResolutionChoice>;
}): ObjectViewGlobalBranchRebaseModel {
  const branchLabel = normalizeObjectViewBranchLabel(input.branchLabel);
  const mainStates = objectViewRebaseStates({
    branchLabel: "main",
    objectViews: input.mainObjectViews,
    objectTypes: input.objectTypes,
    propertiesByObjectType: input.propertiesByObjectType,
    linkTypes: input.linkTypes,
    principal: input.principal,
  });
  const branchStates = objectViewRebaseStates({
    branchLabel,
    objectViews: input.branchObjectViews,
    objectTypes: input.objectTypes,
    propertiesByObjectType: input.propertiesByObjectType,
    linkTypes: input.linkTypes,
    principal: input.principal,
  });
  const resourceIds = uniqueNonEmpty([...mainStates.keys(), ...branchStates.keys()]).sort();
  const latestOntologySignature = buildObjectViewGlobalBranchAdapterState({
    branchLabel,
    objectViews: input.branchObjectViews,
    mainObjectViews: input.mainObjectViews,
    objectTypes: input.objectTypes,
    propertiesByObjectType: input.propertiesByObjectType,
    linkTypes: input.linkTypes,
    principal: input.principal,
  }).latest_ontology_signature;

  const rows = resourceIds.map((resourceId) => {
    const mainState = mainStates.get(resourceId) ?? null;
    const branchState = branchStates.get(resourceId) ?? null;
    const kind = branchState?.kind ?? mainState?.kind ?? "ov_managed_module";
    const label = branchState?.label ?? mainState?.label ?? resourceId;
    const changedFields = mainState && branchState
      ? objectViewRebaseChangedFields(kind, mainState, branchState)
      : [];
    const conflict = objectViewRebaseHasConflict(kind, mainState, branchState, changedFields);
    const choice = input.resolutions?.[resourceId] ?? null;
    const proposedState = objectViewRebaseProposedState({
      mainState,
      branchState,
      choice,
      conflict,
    });
    const autoAccepted = !conflict && Boolean(mainState || branchState) && mainState?.signature !== branchState?.signature;
    const requiresManualResolution = conflict && !choice;
    const disposition: ObjectViewGlobalBranchRebaseDisposition =
      conflict && choice ? "manual_resolution" : conflict ? "conflict" : autoAccepted ? "auto_accepted" : "unchanged";
    return {
      resource_id: resourceId,
      kind,
      label,
      main_state: mainState,
      branch_state: branchState,
      proposed_state: proposedState,
      disposition,
      auto_accepted: autoAccepted,
      requires_manual_resolution: requiresManualResolution,
      conflict_fields: changedFields,
      resolution_choice: choice,
      resolution_options: ["main", "branch", "custom"],
      message: requiresManualResolution
        ? "Choose main, branch, or custom before completing rebase."
        : conflict
          ? `Manual resolution keeps ${choice} state.`
          : autoAccepted
            ? "Non-conflicting change is automatically accepted in the proposed result."
            : "No rebase difference for this resource.",
    } satisfies ObjectViewGlobalBranchRebaseRow;
  });

  const simulatedResources = buildObjectViewGlobalBranchAdapterState({
    branchLabel,
    objectViews: input.branchObjectViews,
    mainObjectViews: input.mainObjectViews,
    objectTypes: input.objectTypes,
    propertiesByObjectType: input.propertiesByObjectType,
    linkTypes: input.linkTypes,
    principal: input.principal,
  }).resources.map((resource) => ({
    ...resource,
    requires_rebase: false,
    rebased_ontology_signature: latestOntologySignature,
    preview_status: "pending" as const,
    merge_state: "clean" as const,
  }));
  const deployabilityChecks = objectViewGlobalBranchChecks(simulatedResources);
  const unresolvedConflictCount = rows.filter((row) => row.requires_manual_resolution).length;
  return {
    branch_label: branchLabel,
    latest_ontology_signature: latestOntologySignature,
    rows,
    auto_accepted_count: rows.filter((row) => row.auto_accepted).length,
    conflict_count: rows.filter((row) => row.disposition === "conflict" || row.disposition === "manual_resolution").length,
    unresolved_conflict_count: unresolvedConflictCount,
    manual_resolution_count: rows.filter((row) => row.disposition === "manual_resolution").length,
    can_finish: unresolvedConflictCount === 0,
    deployability_checks_after_rebase: deployabilityChecks,
    warnings: unresolvedConflictCount > 0
      ? [`${unresolvedConflictCount} Object View rebase conflicts require manual resolution.`]
      : ["Deployability checks will be rerun after completing Object View rebase."],
  };
}

export function completeObjectViewGlobalBranchRebase(input: {
  state: ObjectViewGlobalBranchAdapterState;
  rebaseModel: ObjectViewGlobalBranchRebaseModel;
  now?: string;
}): ObjectViewGlobalBranchAdapterOperationResult {
  if (!input.rebaseModel.can_finish) {
    return {
      operation: "rebase",
      state: input.state,
      warnings: input.rebaseModel.warnings,
      errors: ["Resolve Object View rebase conflicts before finishing rebase."],
    };
  }
  const result = applyObjectViewGlobalBranchAdapterOperation(input.state, {
    kind: "rebase",
    now: input.now,
  });
  return {
    ...result,
    warnings: [
      ...result.warnings,
      "Object View deployability checks were rerun after successful rebase.",
    ],
  };
}

export type OntologyGlobalBranchProposalResourceKind =
  | "object_type"
  | "link_type"
  | "action_type"
  | "interface"
  | "shared_property"
  | "object_view"
  | "object_view_tabs"
  | "object_view_module";

export type OntologyGlobalBranchProposalPreviewStatus = "ready" | "pending" | "blocked";
export type OntologyGlobalBranchIndexingChangeStatus = "pending" | "ready" | "removed" | "blocked";
export type OntologyGlobalBranchProposalCheckStatus = ObjectViewGlobalBranchCheckStatus;

export interface OntologyGlobalBranchProposalResource {
  id: string;
  logical_key: string;
  kind: OntologyGlobalBranchProposalResourceKind;
  label: string;
  description: string;
  branch_label: string;
  action: string;
  source_change_id?: string;
  target_id?: string | null;
  object_type_id?: string | null;
  href?: string;
  included: boolean;
  removable: boolean;
  preview_status: OntologyGlobalBranchProposalPreviewStatus;
  merge_blocking: boolean;
  indexing_change: boolean;
  dependency_resource_ids: string[];
  warnings: string[];
  errors: string[];
}

export interface OntologyGlobalBranchIndexingChange {
  id: string;
  resource_id: string;
  resource_key: string;
  label: string;
  reason: string;
  required: boolean;
  included: boolean;
  removable: boolean;
  status: OntologyGlobalBranchIndexingChangeStatus;
  warnings: string[];
  errors: string[];
}

export interface OntologyGlobalBranchProposalCheck {
  id: string;
  label: string;
  status: OntologyGlobalBranchProposalCheckStatus;
  message: string;
  resource_ids: string[];
}

export interface OntologyGlobalBranchPreviewState {
  branch_label: string;
  status: OntologyGlobalBranchProposalPreviewStatus;
  resource_count: number;
  ready_count: number;
  pending_count: number;
  blocked_count: number;
  indexing_change_count: number;
  object_view_preview_status?: ObjectViewGlobalBranchPreviewStatus;
}

export interface OntologyGlobalBranchProposalIntegration {
  branch_label: string;
  resources: OntologyGlobalBranchProposalResource[];
  indexing_changes: OntologyGlobalBranchIndexingChange[];
  checks: OntologyGlobalBranchProposalCheck[];
  proposal_tasks: OntologyProposalTask[];
  preview: OntologyGlobalBranchPreviewState;
  mergeable: boolean;
  warnings: string[];
}

export interface OntologyGlobalBranchProposalIntegrationInput {
  branchLabel?: string | null;
  branch?: Pick<OntologyBranch, "name" | "changes" | "enable_indexing"> | null;
  changes?: OntologyStagedChange[];
  objectTypes?: ObjectType[];
  linkTypes?: LinkType[];
  actionTypes?: ActionType[];
  interfaces?: OntologyInterface[];
  sharedPropertyTypes?: SharedPropertyType[];
  objectViews?: ObjectViewDefinition[];
  mainObjectViews?: ObjectViewDefinition[];
  propertiesByObjectType?: Record<string, Property[]>;
  principal?: OntologyPermissionPrincipal | null;
  enableIndexing?: boolean;
  excludedResourceIds?: string[];
  excludedIndexingChangeIds?: string[];
}

function ontologyProposalBranchLabel(input: OntologyGlobalBranchProposalIntegrationInput) {
  return normalizeObjectViewBranchLabel(input.branchLabel ?? input.branch?.name ?? "main");
}

function ontologyProposalResourceKindForChange(kind: string): OntologyGlobalBranchProposalResourceKind | null {
  switch (kind) {
    case "object_type":
      return "object_type";
    case "link_type":
      return "link_type";
    case "action_type":
      return "action_type";
    case "interface":
      return "interface";
    case "shared_property":
    case "shared_property_type":
      return "shared_property";
    case "object_view":
    case "custom_object_view":
    case "core_object_view":
      return "object_view";
    default:
      return null;
  }
}

function ontologyProposalTargetId(change: OntologyStagedChange) {
  const payloadId = change.payload?.id;
  const payloadName = change.payload?.name || change.payload?.api_name;
  return change.targetId || (typeof payloadId === "string" ? payloadId : "") || (typeof payloadName === "string" ? payloadName : "");
}

function ontologyProposalResourceLookup(input: OntologyGlobalBranchProposalIntegrationInput) {
  return {
    objectTypes: new Map((input.objectTypes || []).map((entry) => [entry.id, entry])),
    objectTypesByName: new Map((input.objectTypes || []).flatMap((entry) =>
      uniqueNonEmpty([entry.name, entry.api_name || ""]).map((name) => [name, entry] as const),
    )),
    linkTypes: new Map((input.linkTypes || []).map((entry) => [entry.id, entry])),
    actionTypes: new Map((input.actionTypes || []).map((entry) => [entry.id, entry])),
    interfaces: new Map((input.interfaces || []).map((entry) => [entry.id, entry])),
    sharedPropertyTypes: new Map((input.sharedPropertyTypes || []).map((entry) => [entry.id, entry])),
    objectViews: new Map((input.objectViews || []).map((entry) => [entry.id, entry])),
  };
}

function ontologyProposalKnownResourceLabel(
  kind: OntologyGlobalBranchProposalResourceKind,
  id: string,
  lookup: ReturnType<typeof ontologyProposalResourceLookup>,
) {
  if (kind === "object_type") {
    const objectType = lookup.objectTypes.get(id) || lookup.objectTypesByName.get(id);
    return objectType?.display_name || objectType?.name;
  }
  if (kind === "link_type") {
    const linkType = lookup.linkTypes.get(id);
    return linkType?.display_name || linkType?.name;
  }
  if (kind === "action_type") {
    const actionType = lookup.actionTypes.get(id);
    return actionType?.display_name || actionType?.name;
  }
  if (kind === "interface") {
    const ontologyInterface = lookup.interfaces.get(id);
    return ontologyInterface?.display_name || ontologyInterface?.name;
  }
  if (kind === "shared_property") {
    const sharedProperty = lookup.sharedPropertyTypes.get(id);
    return sharedProperty?.display_name || sharedProperty?.name;
  }
  if (kind === "object_view") {
    const objectView = lookup.objectViews.get(id);
    return objectView?.display_name || objectView?.name;
  }
  return undefined;
}

function ontologyProposalObjectTypeIdForChange(
  kind: OntologyGlobalBranchProposalResourceKind,
  id: string,
  change: OntologyStagedChange,
  lookup: ReturnType<typeof ontologyProposalResourceLookup>,
) {
  if (kind === "object_type") return id || null;
  if (kind === "link_type") {
    const linkType = lookup.linkTypes.get(id);
    return linkType?.source_type_id || (typeof change.payload?.source_type_id === "string" ? change.payload.source_type_id : null);
  }
  if (kind === "action_type") {
    const actionType = lookup.actionTypes.get(id);
    return actionType?.object_type_id || (typeof change.payload?.object_type_id === "string" ? change.payload.object_type_id : null);
  }
  if (kind === "object_view") {
    const objectView = lookup.objectViews.get(id);
    return objectView?.object_type_id || (typeof change.payload?.object_type_id === "string" ? change.payload.object_type_id : null);
  }
  return typeof change.payload?.object_type_id === "string" ? change.payload.object_type_id : null;
}

function ontologyProposalDependenciesForChange(
  kind: OntologyGlobalBranchProposalResourceKind,
  id: string,
  change: OntologyStagedChange,
  lookup: ReturnType<typeof ontologyProposalResourceLookup>,
) {
  const dependencies: string[] = [];
  if (kind === "link_type") {
    const linkType = lookup.linkTypes.get(id);
    const source = linkType?.source_type_id || (typeof change.payload?.source_type_id === "string" ? change.payload.source_type_id : "");
    const target = linkType?.target_type_id || (typeof change.payload?.target_type_id === "string" ? change.payload.target_type_id : "");
    dependencies.push(...uniqueNonEmpty([source, target]).map((typeId) => `object_type:${typeId}`));
  }
  if (kind === "action_type") {
    const actionType = lookup.actionTypes.get(id);
    const objectTypeId = actionType?.object_type_id || (typeof change.payload?.object_type_id === "string" ? change.payload.object_type_id : "");
    const interfaceId = actionType?.interface_id || (typeof change.payload?.interface_id === "string" ? change.payload.interface_id : "");
    if (objectTypeId) dependencies.push(`object_type:${objectTypeId}`);
    if (interfaceId) dependencies.push(`interface:${interfaceId}`);
  }
  if (kind === "object_view") {
    const objectView = lookup.objectViews.get(id);
    const objectTypeId = objectView?.object_type_id || (typeof change.payload?.object_type_id === "string" ? change.payload.object_type_id : "");
    if (objectTypeId) dependencies.push(`object_type:${objectTypeId}`);
  }
  return uniqueNonEmpty(dependencies);
}

function ontologyProposalPreviewStatusFromChange(change: OntologyStagedChange): OntologyGlobalBranchProposalPreviewStatus {
  if (change.errors?.length) return "blocked";
  if (change.warnings?.length) return "pending";
  return "ready";
}

function ontologyProposalResourceFromChange(input: {
  change: OntologyStagedChange;
  kind: OntologyGlobalBranchProposalResourceKind;
  branchLabel: string;
  excludedResourceIds: Set<string>;
  lookup: ReturnType<typeof ontologyProposalResourceLookup>;
}): OntologyGlobalBranchProposalResource {
  const targetId = ontologyProposalTargetId(input.change);
  const logicalKey = `${input.kind}:${targetId || input.change.id}`;
  const label = input.change.label || ontologyProposalKnownResourceLabel(input.kind, targetId, input.lookup) || targetId || input.kind;
  const previewStatus = ontologyProposalPreviewStatusFromChange(input.change);
  return {
    id: `change:${input.change.id}`,
    logical_key: logicalKey,
    kind: input.kind,
    label,
    description: input.change.description || `${input.change.action} ${input.kind}`,
    branch_label: input.branchLabel,
    action: input.change.action,
    source_change_id: input.change.id,
    target_id: targetId || null,
    object_type_id: ontologyProposalObjectTypeIdForChange(input.kind, targetId, input.change, input.lookup),
    included: !input.excludedResourceIds.has(`change:${input.change.id}`) && !input.excludedResourceIds.has(logicalKey),
    removable: true,
    preview_status: previewStatus,
    merge_blocking: previewStatus === "blocked",
    indexing_change: false,
    dependency_resource_ids: ontologyProposalDependenciesForChange(input.kind, targetId, input.change, input.lookup),
    warnings: input.change.warnings || [],
    errors: input.change.errors || [],
  };
}

function ontologyProposalIndexingRequired(change: OntologyStagedChange) {
  const action = change.action.toLowerCase();
  const payload = change.payload || {};
  if (payload.indexing_required === true || payload.requires_indexing === true) return true;
  if (action === "delete" || action === "remove") return false;
  return Boolean(
    payload.primary_key_property ||
    payload.backing_dataset_id ||
    payload.backing_dataset_rid ||
    payload.backing_restricted_view_id ||
    payload.restricted_view_id ||
    payload.link_datasource_mapping ||
    payload.datasource_mapping ||
    payload.property_mapping,
  );
}

function ontologyProposalNeedsIndexingResource(resource: OntologyGlobalBranchProposalResource) {
  return resource.kind === "object_type" || resource.kind === "link_type";
}

function ontologyProposalIndexingReason(resource: OntologyGlobalBranchProposalResource, change?: OntologyStagedChange) {
  if (change && ontologyProposalIndexingRequired(change)) {
    return "Schema, datasource, restricted-view, or link mapping edits require an indexing check before merge.";
  }
  if (resource.kind === "object_type") {
    return "Object type changes can create or update indexed object-search metadata.";
  }
  return "Link type changes can update linked-object traversal indexes.";
}

function ontologyProposalIndexingChangeForResource(input: {
  resource: OntologyGlobalBranchProposalResource;
  change?: OntologyStagedChange;
  branchIndexingEnabled: boolean;
  excludedIndexingChangeIds: Set<string>;
}): OntologyGlobalBranchIndexingChange | null {
  if (!input.resource.included || !ontologyProposalNeedsIndexingResource(input.resource)) return null;
  const required = input.change ? ontologyProposalIndexingRequired(input.change) : false;
  const id = `index:${input.resource.id}`;
  const included = !input.excludedIndexingChangeIds.has(id) && !input.excludedIndexingChangeIds.has(input.resource.logical_key);
  const errors: string[] = [];
  const warnings: string[] = [];
  if (required && !included) errors.push("Required indexing change cannot be removed from the proposal.");
  if (required && !input.branchIndexingEnabled) errors.push("Branch indexing is disabled for a resource that requires indexing before merge.");
  if (!required && !included) warnings.push("Optional indexing update was removed from the merge proposal.");
  return {
    id,
    resource_id: input.resource.id,
    resource_key: input.resource.logical_key,
    label: `${input.resource.label} indexing`,
    reason: ontologyProposalIndexingReason(input.resource, input.change),
    required,
    included,
    removable: !required,
    status: errors.length > 0 ? "blocked" : included ? "pending" : "removed",
    warnings,
    errors,
  };
}

function ontologyProposalResourceFromObjectViewBranchResource(input: {
  resource: ObjectViewGlobalBranchResource;
  branchLabel: string;
  excludedResourceIds: Set<string>;
}): OntologyGlobalBranchProposalResource {
  const kind: OntologyGlobalBranchProposalResourceKind =
    input.resource.kind === "full_object_view_tabs" ? "object_view_tabs" : "object_view_module";
  const id = `object-view:${input.resource.id}`;
  const logicalKey = `${kind}:${input.resource.id}`;
  const errors = [
    ...(!input.resource.permission_allowed ? [input.resource.permission_reason] : []),
    ...(input.resource.unsupported_legacy_fields ? ["Unsupported legacy Object View fields were modified."] : []),
  ];
  const warnings = [
    ...(input.resource.requires_rebase ? ["Object View resource must be rebased with main before merge."] : []),
  ];
  return {
    id,
    logical_key: logicalKey,
    kind,
    label: input.resource.label,
    description: input.resource.kind === "full_object_view_tabs"
      ? "Full Object View tab configuration branch resource."
      : "OV-managed Workshop module branch resource.",
    branch_label: input.branchLabel,
    action: input.resource.status,
    target_id: input.resource.object_view_id,
    object_type_id: input.resource.object_type_id,
    href: input.resource.href,
    included: !input.excludedResourceIds.has(id) && !input.excludedResourceIds.has(logicalKey),
    removable: true,
    preview_status: input.resource.preview_status === "blocked"
      ? "blocked"
      : input.resource.preview_status === "ready"
        ? "ready"
        : "pending",
    merge_blocking:
      input.resource.merge_state === "blocked" ||
      input.resource.requires_rebase ||
      errors.length > 0 ||
      input.resource.preview_status === "blocked",
    indexing_change: false,
    dependency_resource_ids: uniqueNonEmpty([
      `object_type:${input.resource.object_type_id}`,
      `object_view:${input.resource.object_view_id}`,
      ...input.resource.dependency_resource_ids,
    ]),
    warnings,
    errors,
  };
}

function ontologyProposalDependencyErrors(resources: OntologyGlobalBranchProposalResource[]) {
  const allKeys = new Set(resources.map((resource) => resource.logical_key));
  const includedKeys = new Set(resources.filter((resource) => resource.included).map((resource) => resource.logical_key));
  const removedDependencies = new Map<string, string[]>();
  for (const resource of resources.filter((entry) => entry.included)) {
    const missing = resource.dependency_resource_ids.filter((dependency) =>
      allKeys.has(dependency) && !includedKeys.has(dependency),
    );
    if (missing.length > 0) removedDependencies.set(resource.id, missing);
  }
  return removedDependencies;
}

function ontologyProposalChecks(input: {
  resources: OntologyGlobalBranchProposalResource[];
  indexingChanges: OntologyGlobalBranchIndexingChange[];
  objectViewChecks?: ObjectViewGlobalBranchCheck[];
}) {
  const active = input.resources.filter((resource) => resource.included);
  const dependencyErrors = ontologyProposalDependencyErrors(input.resources);
  const blockingResources = active.filter((resource) =>
    resource.merge_blocking ||
    resource.errors.length > 0 ||
    dependencyErrors.has(resource.id),
  );
  const checks: OntologyGlobalBranchProposalCheck[] = [
    {
      id: "proposal-resources",
      label: "Proposal resources",
      status: active.length > 0 ? "passed" : "warning",
      message: active.length > 0
        ? `${active.length} ontology and Object View resources will participate in the Global Branching proposal.`
        : "No ontology resources are included in the proposal.",
      resource_ids: active.map((resource) => resource.id),
    },
    {
      id: "resource-dependencies",
      label: "Resource dependencies",
      status: dependencyErrors.size === 0 ? "passed" : "failed",
      message: dependencyErrors.size === 0
        ? "Included resources do not depend on resources removed from the proposal."
        : `${dependencyErrors.size} included resources depend on removed proposal resources.`,
      resource_ids: [...dependencyErrors.keys()],
    },
    {
      id: "resource-mergeability",
      label: "Resource mergeability",
      status: blockingResources.length === 0 ? "passed" : "failed",
      message: blockingResources.length === 0
        ? "Resource-level proposal checks are passing or pending review."
        : `${blockingResources.length} resources have blocking errors, stale previews, or rebase requirements.`,
      resource_ids: blockingResources.map((resource) => resource.id),
    },
  ];

  const activeIndexing = input.indexingChanges.filter((change) => change.included);
  const blockedIndexing = input.indexingChanges.filter((change) => change.errors.length > 0);
  const removedOptionalIndexing = input.indexingChanges.filter((change) => !change.included && !change.required);
  checks.push({
    id: "indexing-changes",
    label: "Indexing changes",
    status: blockedIndexing.length > 0 ? "failed" : removedOptionalIndexing.length > 0 ? "warning" : "passed",
    message: blockedIndexing.length > 0
      ? `${blockedIndexing.length} required indexing changes are missing or blocked.`
      : removedOptionalIndexing.length > 0
        ? `${removedOptionalIndexing.length} optional indexing changes were removed from the proposal.`
        : `${activeIndexing.length} indexing changes are included for merge validation.`,
    resource_ids: [...blockedIndexing, ...removedOptionalIndexing].map((change) => change.resource_id),
  });

  for (const check of input.objectViewChecks || []) {
    checks.push({
      id: `object-view:${check.id}`,
      label: `Object View ${check.label}`,
      status: check.status,
      message: check.message,
      resource_ids: check.resource_ids.map((id) => `object-view:${id}`),
    });
  }
  return checks;
}

function ontologyProposalPreviewState(input: {
  branchLabel: string;
  resources: OntologyGlobalBranchProposalResource[];
  indexingChanges: OntologyGlobalBranchIndexingChange[];
  checks: OntologyGlobalBranchProposalCheck[];
  objectViewPreviewStatus?: ObjectViewGlobalBranchPreviewStatus;
}): OntologyGlobalBranchPreviewState {
  const activeResources = input.resources.filter((resource) => resource.included);
  const blockedCount = activeResources.filter((resource) =>
    resource.preview_status === "blocked" ||
    resource.merge_blocking ||
    resource.errors.length > 0,
  ).length + input.indexingChanges.filter((change) => change.errors.length > 0).length;
  const readyCount = activeResources.filter((resource) => resource.preview_status === "ready" && !resource.merge_blocking && resource.errors.length === 0).length;
  const pendingCount = Math.max(0, activeResources.length - readyCount - blockedCount);
  const failedChecks = input.checks.filter((check) => check.status === "failed").length;
  return {
    branch_label: input.branchLabel,
    status: failedChecks > 0 || blockedCount > 0 ? "blocked" : pendingCount > 0 ? "pending" : "ready",
    resource_count: activeResources.length,
    ready_count: readyCount,
    pending_count: pendingCount,
    blocked_count: blockedCount,
    indexing_change_count: input.indexingChanges.filter((change) => change.included).length,
    object_view_preview_status: input.objectViewPreviewStatus,
  };
}

function ontologyProposalTasks(resources: OntologyGlobalBranchProposalResource[]): OntologyProposalTask[] {
  return resources
    .filter((resource) => resource.included)
    .map((resource) => ({
      id: `proposal-task:${resource.id}`,
      change_id: resource.source_change_id || resource.id,
      title: resource.label,
      description: resource.description,
      status: "pending",
      reviewer_id: null,
      comments: [],
    }));
}

function buildOntologyProposalIntegrationFromParts(input: {
  branchLabel: string;
  resources: OntologyGlobalBranchProposalResource[];
  indexingChanges: OntologyGlobalBranchIndexingChange[];
  objectViewChecks?: ObjectViewGlobalBranchCheck[];
  objectViewPreviewStatus?: ObjectViewGlobalBranchPreviewStatus;
  warnings?: string[];
}): OntologyGlobalBranchProposalIntegration {
  const checks = ontologyProposalChecks({
    resources: input.resources,
    indexingChanges: input.indexingChanges,
    objectViewChecks: input.objectViewChecks,
  });
  const preview = ontologyProposalPreviewState({
    branchLabel: input.branchLabel,
    resources: input.resources,
    indexingChanges: input.indexingChanges,
    checks,
    objectViewPreviewStatus: input.objectViewPreviewStatus,
  });
  return {
    branch_label: input.branchLabel,
    resources: input.resources,
    indexing_changes: input.indexingChanges,
    checks,
    proposal_tasks: ontologyProposalTasks(input.resources),
    preview,
    mergeable: preview.status !== "blocked" && checks.every((check) => check.status !== "failed"),
    warnings: input.warnings || [],
  };
}

export function buildOntologyBranchProposalIntegration(
  input: OntologyGlobalBranchProposalIntegrationInput,
): OntologyGlobalBranchProposalIntegration {
  const branchLabel = ontologyProposalBranchLabel(input);
  const changes = input.branch?.changes ?? input.changes ?? [];
  const excludedResourceIds = new Set(input.excludedResourceIds || []);
  const excludedIndexingChangeIds = new Set(input.excludedIndexingChangeIds || []);
  const lookup = ontologyProposalResourceLookup(input);
  const resources: OntologyGlobalBranchProposalResource[] = [];
  const warnings: string[] = [];

  for (const change of changes) {
    const kind = ontologyProposalResourceKindForChange(change.kind);
    if (!kind) continue;
    resources.push(ontologyProposalResourceFromChange({
      change,
      kind,
      branchLabel,
      excludedResourceIds,
      lookup,
    }));
  }

  const branchObjectViews = branchLabel === "main"
    ? []
    : (input.objectViews || []).filter((view) =>
        normalizeObjectViewBranchLabel(view.branch_label ?? view.config?.branch_label ?? branchLabel) === branchLabel,
      );
  const objectViewAdapter = branchObjectViews.length > 0
    ? buildObjectViewGlobalBranchAdapterState({
        branchLabel,
        objectViews: branchObjectViews,
        mainObjectViews: input.mainObjectViews,
        objectTypes: input.objectTypes || [],
        propertiesByObjectType: input.propertiesByObjectType,
        linkTypes: input.linkTypes,
        principal: input.principal,
      })
    : null;
  for (const resource of objectViewAdapter?.resources || []) {
    resources.push(ontologyProposalResourceFromObjectViewBranchResource({
      resource,
      branchLabel,
      excludedResourceIds,
    }));
  }

  const branchIndexingEnabled = input.enableIndexing ?? input.branch?.enable_indexing ?? true;
  const changesById = new Map(changes.map((change) => [`change:${change.id}`, change]));
  const indexingChanges = resources
    .map((resource) =>
      ontologyProposalIndexingChangeForResource({
        resource,
        change: resource.source_change_id ? changesById.get(`change:${resource.source_change_id}`) : undefined,
        branchIndexingEnabled,
        excludedIndexingChangeIds,
      }),
    )
    .filter((change): change is OntologyGlobalBranchIndexingChange => Boolean(change));

  if (changes.length > resources.filter((resource) => resource.source_change_id).length) {
    warnings.push("Some staged changes are not branch-proposal resources and remain in the project working state.");
  }

  return buildOntologyProposalIntegrationFromParts({
    branchLabel,
    resources,
    indexingChanges,
    objectViewChecks: objectViewAdapter?.resources.length ? objectViewAdapter.checks : [],
    objectViewPreviewStatus: objectViewAdapter?.resources.length ? objectViewAdapter.preview.status : undefined,
    warnings,
  });
}

export function removeOntologyBranchProposalResources(
  integration: OntologyGlobalBranchProposalIntegration,
  resourceIds: string[],
): OntologyGlobalBranchProposalIntegration {
  const ids = new Set(resourceIds);
  const warnings: string[] = [...integration.warnings];
  const resources = integration.resources.map((resource) => {
    if (!ids.has(resource.id) && !ids.has(resource.logical_key)) return resource;
    if (!resource.removable) {
      warnings.push(`${resource.label} cannot be removed from the branch proposal.`);
      return resource;
    }
    return { ...resource, included: false };
  });
  const indexingChanges = integration.indexing_changes.map((change) => {
    if (!ids.has(change.id) && !ids.has(change.resource_key)) return change;
    if (!change.removable) {
      warnings.push(`${change.label} is required and cannot be removed from the branch proposal.`);
      return {
        ...change,
        errors: uniqueNonEmpty([...change.errors, "Required indexing change cannot be removed from the proposal."]),
        status: "blocked" as const,
      };
    }
    return {
      ...change,
      included: false,
      status: "removed" as const,
      warnings: uniqueNonEmpty([...change.warnings, "Optional indexing update was removed from the merge proposal."]),
    };
  });
  return buildOntologyProposalIntegrationFromParts({
    branchLabel: integration.branch_label,
    resources,
    indexingChanges,
    objectViewPreviewStatus: integration.preview.object_view_preview_status,
    warnings,
  });
}

export type ObjectViewMarketplaceDependencyKind =
  | "object_type"
  | "workshop_module"
  | "workshop_widget"
  | "function"
  | "action_type"
  | "data_resource";

export type ObjectViewMarketplaceIssueSeverity = "warning" | "error";

export interface ObjectViewMarketplaceDependency {
  kind: ObjectViewMarketplaceDependencyKind;
  id: string;
  label: string;
  resource_ref: string;
  required: boolean;
  available: boolean | null;
  source_tab_id?: string;
  source_widget_id?: string;
  reason: string;
}

export interface ObjectViewMarketplacePackagingIssue {
  severity: ObjectViewMarketplaceIssueSeverity;
  code: string;
  message: string;
  tab_id?: string;
  dependency_ref?: string;
}

export interface ObjectViewMarketplacePackagedResource {
  kind: "marketplace_object_view_output";
  name: string;
  resource_ref: string;
  source_branch?: string | null;
  required: boolean;
}

export interface ObjectViewMarketplaceOutputTabManifest {
  tab_id: string;
  title: string;
  visibility: ObjectViewTabVisibility;
  module_id: string;
  module_name: string;
  module_version: number;
  widget_ids: string[];
}

export interface ObjectViewMarketplaceOutputManifestEntry {
  kind: "marketplace_object_view_output";
  object_view_id: string;
  object_view_name: string;
  object_type_id: string;
  form_factor: ObjectViewFormFactor;
  selected_tab_ids: string[];
  tabs: ObjectViewMarketplaceOutputTabManifest[];
  dependency_refs: string[];
  module_dependency_refs: string[];
  permission_model: "object_type_managed" | "standalone_module";
  custom_view_default: boolean;
  workshop_tab_builder_only: boolean;
  legacy_builder_compatibility: boolean;
}

export interface ObjectViewMarketplaceOutputManifest {
  schema_version: 1;
  object_view_outputs: ObjectViewMarketplaceOutputManifestEntry[];
}

export interface ObjectViewMarketplacePackagingResult {
  valid: boolean;
  output: ObjectViewMarketplacePackagedResource | null;
  packaged_resources: ObjectViewMarketplacePackagedResource[];
  manifest: ObjectViewMarketplaceOutputManifest;
  dependencies: ObjectViewMarketplaceDependency[];
  issues: ObjectViewMarketplacePackagingIssue[];
}

export interface ObjectViewMarketplacePackagingInput {
  objectView: ObjectViewDefinition;
  selectedTabIds: string[];
  objectType?: ObjectType | null;
  objectTypes?: ObjectType[];
  actionTypes?: ActionType[];
  availableObjectTypeIds?: string[];
  availableWorkshopModuleIds?: string[];
  availableWidgetIds?: string[];
  availableFunctionIds?: string[];
  availableActionTypeIds?: string[];
  availableDataResourceIds?: string[];
  allowLegacyBuilderCompatibility?: boolean;
  required?: boolean;
  sourceBranch?: string | null;
}

function objectViewMarketplaceOutputName(view: ObjectViewDefinition, tabs: ObjectViewTabDefinition[]) {
  const viewName = view.display_name || view.name || view.id;
  if (tabs.length === 1) return `${viewName} - ${tabs[0].title}`;
  return `${viewName} - ${tabs.length} tabs`;
}

function objectViewMarketplaceResourceRef(view: ObjectViewDefinition, tabIds: string[]) {
  return `object_view:${view.id}:tabs:${tabIds.map((id) => objectViewStableToken(id)).join(",")}`;
}

function normalizedIdSet(values?: string[]) {
  if (!values) return null;
  return new Set(values.map((value) => value.trim()).filter(Boolean));
}

function objectViewMarketplaceKnownIds(input: ObjectViewMarketplacePackagingInput) {
  const objectTypeIds = uniqueNonEmpty([
    ...(input.objectTypes?.map((type) => type.id) ?? []),
    input.objectType?.id ?? "",
    ...(input.availableObjectTypeIds ?? []),
  ]);
  const actionTypeIds = uniqueNonEmpty([
    ...(input.actionTypes?.map((action) => action.id) ?? []),
    ...(input.availableActionTypeIds ?? []),
  ]);
  const selectedTabs = (input.objectView.config?.tabs || []).filter((tab) => input.selectedTabIds.includes(tab.id));
  return {
    objectTypes: objectTypeIds.length > 0 ? new Set(objectTypeIds) : normalizedIdSet(input.availableObjectTypeIds),
    actionTypes: actionTypeIds.length > 0 ? new Set(actionTypeIds) : normalizedIdSet(input.availableActionTypeIds),
    workshopModules: normalizedIdSet([
      ...selectedTabs.map((tab) => tab.module?.id ?? ""),
      ...(input.availableWorkshopModuleIds ?? []),
    ]),
    widgets: normalizedIdSet([
      ...selectedTabs.flatMap((tab) => tab.module?.widgets?.map((widget) => widget.id) ?? []),
      ...(input.availableWidgetIds ?? []),
    ]),
    functions: normalizedIdSet(input.availableFunctionIds),
    dataResources: normalizedIdSet([
      input.objectType?.backing_dataset_id ?? "",
      input.objectType?.backing_dataset_rid ?? "",
      input.objectType?.backing_restricted_view_id ?? "",
      input.objectType?.restricted_view_id ?? "",
      ...(input.objectView.config?.input_datasource_ids ?? []),
      ...(input.objectView.config?.metadata?.input_datasource_ids ?? []),
      ...(input.availableDataResourceIds ?? []),
    ]),
  };
}

function objectViewMarketplaceDependencyAvailability(
  kind: ObjectViewMarketplaceDependencyKind,
  id: string,
  known: ReturnType<typeof objectViewMarketplaceKnownIds>,
) {
  const set =
    kind === "object_type" ? known.objectTypes
      : kind === "action_type" ? known.actionTypes
        : kind === "workshop_module" ? known.workshopModules
          : kind === "workshop_widget" ? known.widgets
            : kind === "function" ? known.functions
              : known.dataResources;
  return set ? set.has(id) : null;
}

function pushObjectViewMarketplaceDependency(
  dependencies: ObjectViewMarketplaceDependency[],
  input: Omit<ObjectViewMarketplaceDependency, "resource_ref" | "available">,
  known: ReturnType<typeof objectViewMarketplaceKnownIds>,
) {
  const id = input.id.trim();
  if (!id) return;
  const resourceRef = `${input.kind}:${id}`;
  if (dependencies.some((dependency) =>
    dependency.resource_ref === resourceRef &&
    dependency.source_tab_id === input.source_tab_id &&
    dependency.source_widget_id === input.source_widget_id,
  )) return;
  dependencies.push({
    ...input,
    id,
    resource_ref: resourceRef,
    available: objectViewMarketplaceDependencyAvailability(input.kind, id, known),
  });
}

const OBJECT_VIEW_MARKETPLACE_DEPENDENCY_KEYS: Record<ObjectViewMarketplaceDependencyKind, string[]> = {
  object_type: ["object_type_id", "objectTypeId", "target_object_type_id", "source_object_type_id"],
  workshop_module: [],
  workshop_widget: ["widget_id", "widgetId"],
  function: ["function_id", "functionId", "function_package_id", "functionPackageId", "function_rid", "logic_function_id", "query_function_id", "display_function_id"],
  action_type: ["action_type_id", "actionTypeId", "action_id", "actionId"],
  data_resource: ["dataset_id", "datasetId", "datasource_id", "datasourceId", "data_resource_id", "dataResourceId", "resource_rid", "media_set_id", "mediaSetId"],
};

function collectObjectViewMarketplaceDependencies(
  value: unknown,
  dependencies: ObjectViewMarketplaceDependency[],
  input: {
    tab: ObjectViewTabDefinition;
    widget?: ObjectViewWorkshopWidgetDefinition;
    known: ReturnType<typeof objectViewMarketplaceKnownIds>;
    depth?: number;
  },
) {
  const depth = input.depth ?? 0;
  if (depth > 8 || value === null || value === undefined) return;
  if (Array.isArray(value)) {
    value.forEach((entry) =>
      collectObjectViewMarketplaceDependencies(entry, dependencies, { ...input, depth: depth + 1 }),
    );
    return;
  }
  if (typeof value !== "object") return;
  for (const [key, nested] of Object.entries(value as Record<string, unknown>)) {
    for (const [kind, keys] of Object.entries(OBJECT_VIEW_MARKETPLACE_DEPENDENCY_KEYS) as Array<[ObjectViewMarketplaceDependencyKind, string[]]>) {
      if (!keys.includes(key)) continue;
      const values = Array.isArray(nested) ? nested : [nested];
      for (const item of values) {
        if (typeof item !== "string") continue;
        pushObjectViewMarketplaceDependency(dependencies, {
          kind,
          id: item,
          label: item,
          required: true,
          source_tab_id: input.tab.id,
          source_widget_id: input.widget?.id,
          reason: `Referenced by ${input.widget?.title || input.tab.title}.`,
        }, input.known);
      }
    }
    collectObjectViewMarketplaceDependencies(nested, dependencies, { ...input, depth: depth + 1 });
  }
}

function objectViewMarketplaceIsLegacyTab(tab: ObjectViewTabDefinition, view: ObjectViewDefinition) {
  const tabRecord = tab as unknown as Record<string, unknown>;
  const moduleRecord = tab.module as unknown as Record<string, unknown>;
  return Boolean(
    view.config?.metadata?.legacy_builder ||
    view.config?.metadata?.legacy_fields_modified ||
    tabRecord.legacy_builder ||
    tabRecord.builder === "legacy" ||
    moduleRecord.legacy_builder ||
    moduleRecord.builder === "legacy",
  );
}

function objectViewMarketplaceTabHasWorkshopModule(tab: ObjectViewTabDefinition) {
  const module = tab.module;
  return Boolean(
    module &&
    module.id &&
    module.name &&
    module.object_context_parameter &&
    Array.isArray(module.widgets),
  );
}

function objectViewMarketplaceIssuesForDependencies(
  dependencies: ObjectViewMarketplaceDependency[],
): ObjectViewMarketplacePackagingIssue[] {
  return dependencies.flatMap((dependency): ObjectViewMarketplacePackagingIssue[] => {
    if (dependency.available === false) {
      return [{
        severity: "error" as const,
        code: "missing_dependency",
        message: `${dependency.label} (${dependency.resource_ref}) is required by the packaged Object View output but is not available.`,
        tab_id: dependency.source_tab_id,
        dependency_ref: dependency.resource_ref,
      }];
    }
    if (dependency.available === null && (dependency.kind === "function" || dependency.kind === "data_resource")) {
      return [{
        severity: "warning" as const,
        code: "unverified_dependency",
        message: `${dependency.label} (${dependency.resource_ref}) could not be verified against a local registry before packaging.`,
        tab_id: dependency.source_tab_id,
        dependency_ref: dependency.resource_ref,
      }];
    }
    return [];
  });
}

export function buildObjectViewMarketplaceOutput(
  input: ObjectViewMarketplacePackagingInput,
): ObjectViewMarketplacePackagingResult {
  const config = input.objectView.config;
  const issues: ObjectViewMarketplacePackagingIssue[] = [];
  const selectedIds = uniqueNonEmpty(input.selectedTabIds);
  const tabs = (config?.tabs || []).filter((tab) => selectedIds.includes(tab.id));
  const known = objectViewMarketplaceKnownIds(input);
  const dependencies: ObjectViewMarketplaceDependency[] = [];

  if (input.objectView.form_factor !== "full") {
    issues.push({
      severity: "error",
      code: "unsupported_form_factor",
      message: "Marketplace Object View outputs only package full Object View tabs.",
    });
  }
  if (input.objectView.mode !== "configured") {
    issues.push({
      severity: "error",
      code: "unsupported_object_view_mode",
      message: "Marketplace Object View outputs require a configured/custom Object View.",
    });
  }
  if (selectedIds.length === 0) {
    issues.push({
      severity: "error",
      code: "no_selected_tabs",
      message: "Select at least one Object View tab to include in the product output.",
    });
  }
  for (const tabId of selectedIds) {
    if (!tabs.some((tab) => tab.id === tabId)) {
      issues.push({
        severity: "error",
        code: "missing_selected_tab",
        message: `Selected Object View tab ${tabId} is not present in the current configuration.`,
        tab_id: tabId,
      });
    }
  }

  const objectTypeID = input.objectType?.id || input.objectView.object_type_id;
  pushObjectViewMarketplaceDependency(dependencies, {
    kind: "object_type",
    id: objectTypeID,
    label: input.objectType?.display_name || objectTypeID,
    required: true,
    reason: "Packaged Object Views must remap to an object type during install.",
  }, known);

  for (const dataResourceID of uniqueNonEmpty([
    input.objectType?.backing_dataset_id ?? "",
    input.objectType?.backing_dataset_rid ?? "",
    input.objectType?.backing_restricted_view_id ?? "",
    input.objectType?.restricted_view_id ?? "",
    ...config?.input_datasource_ids ?? [],
    ...config?.metadata?.input_datasource_ids ?? [],
  ])) {
    pushObjectViewMarketplaceDependency(dependencies, {
      kind: "data_resource",
      id: dataResourceID,
      label: dataResourceID,
      required: true,
      reason: "Object View runtime data access depends on this backing datasource or restricted view.",
    }, known);
  }

  for (const tab of tabs) {
    if (!objectViewMarketplaceTabHasWorkshopModule(tab)) {
      issues.push({
        severity: "error",
        code: "unsupported_tab_builder",
        message: `${tab.title} is not backed by a Workshop tab module and cannot be packaged.`,
        tab_id: tab.id,
      });
      continue;
    }
    if (objectViewMarketplaceIsLegacyTab(tab, input.objectView)) {
      if (input.allowLegacyBuilderCompatibility) {
        issues.push({
          severity: "warning",
          code: "legacy_builder_compatibility_enabled",
          message: `${tab.title} uses legacy Object View builder metadata and is packaged through explicit local compatibility mode.`,
          tab_id: tab.id,
        });
      } else {
        issues.push({
          severity: "error",
          code: "unsupported_legacy_builder",
          message: `${tab.title} uses legacy Object View builder metadata. Rebuild the tab with the Workshop tab builder before packaging.`,
          tab_id: tab.id,
        });
      }
    }
    pushObjectViewMarketplaceDependency(dependencies, {
      kind: "workshop_module",
      id: tab.module.id,
      label: tab.module.display_name || tab.module.name,
      required: true,
      source_tab_id: tab.id,
      reason: "Selected Object View tab is backed by this Workshop module.",
    }, known);
    for (const widget of tab.module.widgets) {
      pushObjectViewMarketplaceDependency(dependencies, {
        kind: "workshop_widget",
        id: widget.id,
        label: widget.title || widget.id,
        required: true,
        source_tab_id: tab.id,
        source_widget_id: widget.id,
        reason: "Workshop tab content includes this widget.",
      }, known);
      collectObjectViewMarketplaceDependencies(widget.config, dependencies, { tab, widget, known });
      collectObjectViewMarketplaceDependencies({ binding: widget.binding, kind: widget.kind }, dependencies, { tab, widget, known });
    }
  }

  issues.push(...objectViewMarketplaceIssuesForDependencies(dependencies));
  const valid = issues.every((issue) => issue.severity !== "error");
  const selectedTabIds = tabs.map((tab) => tab.id);
  const output: ObjectViewMarketplacePackagedResource | null = tabs.length > 0
    ? {
        kind: "marketplace_object_view_output",
        name: objectViewMarketplaceOutputName(input.objectView, tabs),
        resource_ref: objectViewMarketplaceResourceRef(input.objectView, selectedTabIds),
        source_branch: input.sourceBranch ?? input.objectView.branch_label ?? config?.branch_label ?? null,
        required: input.required ?? true,
      }
    : null;
  const manifestEntry: ObjectViewMarketplaceOutputManifestEntry = {
    kind: "marketplace_object_view_output",
    object_view_id: input.objectView.id,
    object_view_name: input.objectView.display_name || input.objectView.name,
    object_type_id: objectTypeID,
    form_factor: input.objectView.form_factor,
    selected_tab_ids: selectedTabIds,
    tabs: tabs.map((tab) => ({
      tab_id: tab.id,
      title: tab.title,
      visibility: tab.visibility,
      module_id: tab.module.id,
      module_name: tab.module.display_name || tab.module.name,
      module_version: tab.module.version,
      widget_ids: tab.module.widgets.map((widget) => widget.id),
    })),
    dependency_refs: uniqueNonEmpty(dependencies.map((dependency) => dependency.resource_ref)),
    module_dependency_refs: uniqueNonEmpty(dependencies
      .filter((dependency) => dependency.kind === "workshop_module")
      .map((dependency) => dependency.resource_ref)),
    permission_model: "object_type_managed",
    custom_view_default: input.objectView.mode === "configured" && input.objectView.form_factor === "full",
    workshop_tab_builder_only: !input.allowLegacyBuilderCompatibility,
    legacy_builder_compatibility: Boolean(input.allowLegacyBuilderCompatibility),
  };

  return {
    valid,
    output,
    packaged_resources: output ? [output] : [],
    manifest: {
      schema_version: 1,
      object_view_outputs: [manifestEntry],
    },
    dependencies,
    issues,
  };
}

export type ObjectViewMarketplaceInstallFailureCode =
  | "invalid_manifest"
  | "missing_object_type"
  | "unsupported_tab_builder"
  | "missing_workshop_module"
  | "unavailable_widget"
  | "missing_function"
  | "missing_action";

export interface ObjectViewMarketplaceInstallFailure {
  code: ObjectViewMarketplaceInstallFailureCode;
  message: string;
  output_ref?: string;
  tab_id?: string;
  dependency_ref?: string;
}

export interface ObjectViewMarketplaceObjectTypeRemap {
  source_object_type_id: string;
  target_object_type_id: string | null;
  target_object_type_name: string | null;
  strategy: "explicit" | "same_id" | "api_name" | "missing";
}

export interface ObjectViewMarketplaceInstalledOutput {
  output_ref: string;
  object_view_id: string;
  object_view_name: string;
  source_object_type_id: string;
  target_object_type_id: string;
  selected_tab_ids: string[];
  module_dependency_refs: string[];
  permission_model: "object_type_managed" | "standalone_module";
  custom_view_default: boolean;
  installed_view: ObjectViewDefinition;
}

export interface ObjectViewMarketplaceInstallPlan {
  valid: boolean;
  outputs: ObjectViewMarketplaceInstalledOutput[];
  remaps: ObjectViewMarketplaceObjectTypeRemap[];
  failures: ObjectViewMarketplaceInstallFailure[];
  preserved: {
    selected_tabs: number;
    module_dependencies: number;
    permissions: "object_type_managed" | "mixed" | "standalone_module";
    custom_view_default_count: number;
  };
}

export interface ObjectViewMarketplaceInstallPlanInput {
  manifest: unknown;
  packagedResources?: Array<{
    kind: string;
    name: string;
    resource_ref: string;
    source_branch?: string | null;
    required: boolean;
  }>;
  targetObjectTypes: ObjectType[];
  existingObjectViews?: ObjectViewDefinition[];
  objectTypeRemap?: Record<string, string>;
  availableWorkshopModuleIds?: string[];
  availableWidgetIds?: string[];
  availableFunctionIds?: string[];
  availableActionTypeIds?: string[];
  allowLegacyBuilderCompatibility?: boolean;
  installedBy?: string | null;
  now?: string;
}

function objectViewMarketplaceManifest(value: unknown): ObjectViewMarketplaceOutputManifest {
  if (!value || typeof value !== "object") return { schema_version: 1, object_view_outputs: [] };
  const record = value as Record<string, unknown>;
  const outputs = Array.isArray(record.object_view_outputs) ? record.object_view_outputs : [];
  return {
    schema_version: 1,
    object_view_outputs: outputs.filter((entry): entry is ObjectViewMarketplaceOutputManifestEntry => {
      if (!entry || typeof entry !== "object") return false;
      const output = entry as Partial<ObjectViewMarketplaceOutputManifestEntry>;
      return output.kind === "marketplace_object_view_output" &&
        typeof output.object_view_id === "string" &&
        typeof output.object_type_id === "string" &&
        Array.isArray(output.selected_tab_ids) &&
        Array.isArray(output.tabs);
    }),
  };
}

function objectViewMarketplaceOutputRef(entry: Pick<ObjectViewMarketplaceOutputManifestEntry, "object_view_id" | "selected_tab_ids">) {
  return objectViewMarketplaceResourceRef(
    {
      id: entry.object_view_id,
      name: entry.object_view_id,
      object_type_id: "",
      mode: "configured",
      form_factor: "full",
    },
    entry.selected_tab_ids,
  );
}

function objectViewMarketplaceResolveObjectType(
  sourceObjectTypeID: string,
  targetObjectTypes: ObjectType[],
  remap?: Record<string, string>,
): ObjectViewMarketplaceObjectTypeRemap {
  const explicit = remap?.[sourceObjectTypeID];
  if (explicit) {
    const target = targetObjectTypes.find((type) => type.id === explicit || type.name === explicit || type.api_name === explicit);
    return {
      source_object_type_id: sourceObjectTypeID,
      target_object_type_id: target?.id ?? null,
      target_object_type_name: target?.display_name ?? null,
      strategy: target ? "explicit" : "missing",
    };
  }
  const sameId = targetObjectTypes.find((type) => type.id === sourceObjectTypeID);
  if (sameId) {
    return {
      source_object_type_id: sourceObjectTypeID,
      target_object_type_id: sameId.id,
      target_object_type_name: sameId.display_name,
      strategy: "same_id",
    };
  }
  const apiName = targetObjectTypes.find((type) => type.name === sourceObjectTypeID || type.api_name === sourceObjectTypeID);
  return {
    source_object_type_id: sourceObjectTypeID,
    target_object_type_id: apiName?.id ?? null,
    target_object_type_name: apiName?.display_name ?? null,
    strategy: apiName ? "api_name" : "missing",
  };
}

function objectViewMarketplaceDependencyRefs(entry: ObjectViewMarketplaceOutputManifestEntry, kind: ObjectViewMarketplaceDependencyKind) {
  const prefix = `${kind}:`;
  return (entry.dependency_refs || [])
    .filter((ref) => ref.startsWith(prefix))
    .map((ref) => ref.slice(prefix.length));
}

function objectViewMarketplaceInstallDependencyFailures(input: {
  entry: ObjectViewMarketplaceOutputManifestEntry;
  outputRef: string;
  availableWorkshopModuleIds?: Set<string>;
  availableWidgetIds?: Set<string>;
  availableFunctionIds?: Set<string>;
  availableActionTypeIds?: Set<string>;
}): ObjectViewMarketplaceInstallFailure[] {
  const failures: ObjectViewMarketplaceInstallFailure[] = [];
  const failMissing = (
    code: ObjectViewMarketplaceInstallFailureCode,
    kind: ObjectViewMarketplaceDependencyKind,
    id: string,
    message: string,
    available?: Set<string>,
  ) => {
    if (!available || available.has(id)) return;
    failures.push({
      code,
      message,
      output_ref: input.outputRef,
      dependency_ref: `${kind}:${id}`,
    });
  };

  for (const tab of input.entry.tabs) {
    if (!tab.module_id) {
      failures.push({
        code: "missing_workshop_module",
        message: `${tab.title} does not declare a Workshop module dependency.`,
        output_ref: input.outputRef,
        tab_id: tab.tab_id,
      });
    }
    failMissing(
      "missing_workshop_module",
      "workshop_module",
      tab.module_id,
      `${tab.title} requires Workshop module ${tab.module_id}, which is not installed or packaged.`,
      input.availableWorkshopModuleIds,
    );
    for (const widgetId of tab.widget_ids) {
      failMissing(
        "unavailable_widget",
        "workshop_widget",
        widgetId,
        `${tab.title} uses widget ${widgetId}, which is unavailable in the target workspace.`,
        input.availableWidgetIds,
      );
    }
  }

  for (const functionId of objectViewMarketplaceDependencyRefs(input.entry, "function")) {
    failMissing(
      "missing_function",
      "function",
      functionId,
      `Function ${functionId} is required by the packaged Object View but is unavailable in the target workspace.`,
      input.availableFunctionIds,
    );
  }
  for (const actionId of objectViewMarketplaceDependencyRefs(input.entry, "action_type")) {
    failMissing(
      "missing_action",
      "action_type",
      actionId,
      `Action type ${actionId} is required by the packaged Object View but is unavailable in the target workspace.`,
      input.availableActionTypeIds,
    );
  }
  return failures;
}

function objectViewMarketplaceInstalledConfig(input: {
  entry: ObjectViewMarketplaceOutputManifestEntry;
  targetObjectType: ObjectType;
  installedBy?: string | null;
  now: string;
}): ObjectViewConfig {
  const tabs: ObjectViewTabDefinition[] = input.entry.tabs.map((tab, index) => ({
    id: tab.tab_id,
    title: tab.title,
    order: index,
    visibility: tab.visibility,
    hidden_in_runtime_when_single: true,
    module: {
      id: tab.module_id,
      name: objectViewStableToken(tab.module_name || tab.module_id),
      display_name: tab.module_name || tab.title,
      version: tab.module_version,
      form_factor: "full",
      object_context_parameter: "selectedObject",
      source: "user_managed",
      widgets: tab.widget_ids.map((widgetId) => ({
        id: widgetId,
        kind: "apps",
        title: widgetId,
        binding: "selectedObject",
        description: "Marketplace-installed Workshop widget dependency.",
      })),
      updated_at: input.now,
    },
  }));
  const propertyNames = (input.targetObjectType.properties || [])
    .filter((property) => propertyDisplayMode(property) !== "hidden")
    .map((property) => property.name)
    .slice(0, 8);
  return {
    mode: "configured",
    form_factor: "full",
    title_template: "{{name}}",
    subtitle_property: "",
    prominent_properties: propertyNames.slice(0, 4),
    panel_properties: propertyNames.slice(0, 6),
    sections: [],
    sidebar_links: [],
    comments_enabled: true,
    branch_label: "main",
    auto_publish: true,
    object_view_version: 1,
    workshop_module_version: Math.max(1, ...tabs.map((tab) => tab.module.version)),
    selected_tab_id: tabs[0]?.id,
    tabs,
    published_version: 1,
    last_saved_by: input.installedBy || "marketplace-install",
    last_saved_at: input.now,
    last_change_summary: "Installed from Marketplace Object View output.",
    metadata: {
      default_custom: input.entry.custom_view_default,
      generated: false,
      linked_object_type_ids: [],
      link_type_ids: [],
    },
  };
}

export function buildObjectViewMarketplaceInstallPlan(
  input: ObjectViewMarketplaceInstallPlanInput,
): ObjectViewMarketplaceInstallPlan {
  const manifest = objectViewMarketplaceManifest(input.manifest);
  const failures: ObjectViewMarketplaceInstallFailure[] = [];
  const outputs: ObjectViewMarketplaceInstalledOutput[] = [];
  const remaps: ObjectViewMarketplaceObjectTypeRemap[] = [];
  const now = input.now ?? new Date().toISOString();
  const packagedOutputRefs = new Set((input.packagedResources || [])
    .filter((resource) => resource.kind === "marketplace_object_view_output")
    .map((resource) => resource.resource_ref));
  const availableWorkshopModuleIds = input.availableWorkshopModuleIds ? new Set(input.availableWorkshopModuleIds) : null;
  const availableWidgetIds = input.availableWidgetIds ? new Set(input.availableWidgetIds) : null;
  const availableFunctionIds = input.availableFunctionIds ? new Set(input.availableFunctionIds) : null;
  const availableActionTypeIds = input.availableActionTypeIds ? new Set(input.availableActionTypeIds) : null;

  if (manifest.object_view_outputs.length === 0 && packagedOutputRefs.size > 0) {
    failures.push({
      code: "invalid_manifest",
      message: "Package declares Object View outputs but the version manifest does not contain object_view_outputs metadata.",
    });
  }

  for (const entry of manifest.object_view_outputs) {
    const outputRef = objectViewMarketplaceOutputRef(entry);
    if (packagedOutputRefs.size > 0 && !packagedOutputRefs.has(outputRef)) {
      failures.push({
        code: "invalid_manifest",
        message: `${outputRef} is present in the manifest but not in packaged_resources.`,
        output_ref: outputRef,
      });
    }
    if (entry.form_factor !== "full" || !entry.workshop_tab_builder_only || entry.legacy_builder_compatibility && !input.allowLegacyBuilderCompatibility) {
      failures.push({
        code: "unsupported_tab_builder",
        message: `${entry.object_view_name} contains Object View tabs that are not supported by Workshop-tab-only Marketplace install.`,
        output_ref: outputRef,
      });
    }
    const remap = objectViewMarketplaceResolveObjectType(entry.object_type_id, input.targetObjectTypes, input.objectTypeRemap);
    remaps.push(remap);
    if (!remap.target_object_type_id) {
      failures.push({
        code: "missing_object_type",
        message: `${entry.object_view_name} requires object type ${entry.object_type_id}; provide an install remap or install the object type first.`,
        output_ref: outputRef,
        dependency_ref: `object_type:${entry.object_type_id}`,
      });
    }
    failures.push(...objectViewMarketplaceInstallDependencyFailures({
      entry,
      outputRef,
      availableWorkshopModuleIds: availableWorkshopModuleIds ?? undefined,
      availableWidgetIds: availableWidgetIds ?? undefined,
      availableFunctionIds: availableFunctionIds ?? undefined,
      availableActionTypeIds: availableActionTypeIds ?? undefined,
    }));
    const targetObjectType = input.targetObjectTypes.find((type) => type.id === remap.target_object_type_id);
    if (!targetObjectType) continue;
    const config = objectViewMarketplaceInstalledConfig({
      entry,
      targetObjectType,
      installedBy: input.installedBy,
      now,
    });
    const installedViewID = `marketplace:${entry.object_view_id}:${targetObjectType.id}`;
    outputs.push({
      output_ref: outputRef,
      object_view_id: installedViewID,
      object_view_name: entry.object_view_name,
      source_object_type_id: entry.object_type_id,
      target_object_type_id: targetObjectType.id,
      selected_tab_ids: entry.selected_tab_ids,
      module_dependency_refs: entry.module_dependency_refs || [],
      permission_model: entry.permission_model,
      custom_view_default: entry.custom_view_default,
      installed_view: {
        id: installedViewID,
        name: objectViewPascalToken(`${entry.object_view_name} ${targetObjectType.name}`),
        display_name: entry.object_view_name,
        description: "Installed from Marketplace Object View output.",
        object_type_id: targetObjectType.id,
        mode: "configured",
        form_factor: "full",
        config,
        branch_label: "main",
        published: true,
        status: entry.custom_view_default ? "default_custom" : "published",
        owner_id: input.installedBy || undefined,
        created_at: now,
        updated_at: now,
      },
    });
  }

  const permissionModels = uniqueNonEmpty(outputs.map((output) => output.permission_model));
  return {
    valid: failures.length === 0,
    outputs,
    remaps,
    failures,
    preserved: {
      selected_tabs: outputs.reduce((sum, output) => sum + output.selected_tab_ids.length, 0),
      module_dependencies: outputs.reduce((sum, output) => sum + output.module_dependency_refs.length, 0),
      permissions: permissionModels.length === 1
        ? permissionModels[0] as "object_type_managed" | "standalone_module"
        : permissionModels.length > 1 ? "mixed" : "object_type_managed",
      custom_view_default_count: outputs.filter((output) => output.custom_view_default).length,
    },
  };
}

export function parseObjectViewUrlSearch(
  search: string | URLSearchParams,
  objectType?: ObjectType | null,
): ObjectViewUrlState {
  const params = typeof search === "string" ? new URLSearchParams(search.startsWith("?") ? search.slice(1) : search) : search;
  const objectTypeID = params.get("type") || params.get("object_type_id") || objectType?.id || "";
  const objectID = params.get("object") || params.get("objectId") || params.get("object_id");
  const explicitPrimaryKey = params.get("primaryKey") || params.get("primary_key") || params.get("pk") || objectViewPrimaryKeyProperty(objectType);
  const primaryKeyValue = explicitPrimaryKey ? params.get(explicitPrimaryKey) ?? params.get("primaryKeyValue") ?? params.get("pkValue") : null;
  const mode = params.get("mode") === "standard" ? "standard" : "configured";
  const formFactor = params.get("factor") === "panel" || params.get("formFactor") === "panel" ? "panel" : "full";
  const embeddedParam = params.get("embedded");
  return {
    object_type_id: objectTypeID,
    object_id: objectID,
    primary_key_property: explicitPrimaryKey,
    primary_key_value: primaryKeyValue,
    mode,
    form_factor: formFactor,
    branch_label: params.get("branch") || params.get("branchLabel"),
    tab_id: params.get("tab") || params.get("tabId"),
    embedded: embeddedParam === "true" || embeddedParam === "1",
  };
}

export function ensurePanelObjectViewConfiguration(input: {
  objectType: ObjectType;
  config: ObjectViewConfig;
}): ObjectViewConfig {
  const existing = input.config.panel_config;
  const propertyNames = uniqueNonEmpty(
    (existing?.property_names && existing.property_names.length > 0
      ? existing.property_names
      : input.config.panel_properties.length > 0
      ? input.config.panel_properties
      : input.config.prominent_properties
    ).filter(Boolean),
  ).slice(0, existing?.max_properties ?? 6);
  const sectionKinds = existing?.section_kinds && existing.section_kinds.length > 0
    ? existing.section_kinds
    : objectViewPanelSectionKinds(input.config);
  const workshopWidget = existing?.workshop_widget ?? {
    enabled: true,
    widget_id: `object-view-widget:${input.objectType.id}:panel`,
    selected_object_parameter: "selectedObject",
    height_px: 420,
  };

  return {
    ...input.config,
    panel_properties: propertyNames,
    panel_config: {
      title_template: existing?.title_template || input.config.title_template || `{{${objectTypeTitleProperty(input.objectType)}}}`,
      property_names: propertyNames,
      section_kinds: sectionKinds,
      density: existing?.density ?? "compact",
      max_properties: Math.max(1, Math.min(12, existing?.max_properties ?? 6)),
      max_link_groups: Math.max(0, Math.min(8, existing?.max_link_groups ?? 2)),
      show_title: existing?.show_title ?? true,
      show_open_full_view: existing?.show_open_full_view ?? true,
      hosts: mergeObjectViewPanelHosts(existing?.hosts),
      workshop_widget: {
        enabled: workshopWidget.enabled ?? true,
        widget_id: workshopWidget.widget_id || `object-view-widget:${input.objectType.id}:panel`,
        selected_object_parameter: workshopWidget.selected_object_parameter || "selectedObject",
        height_px: Math.max(240, Math.min(900, workshopWidget.height_px || 420)),
      },
    },
  };
}

function objectViewRenderConfigTemplate(
  template: string,
  object: ObjectInstance | null | undefined,
  summary: Record<string, unknown> | undefined,
  fallback: string,
) {
  if (!object) return fallback;
  const rendered = template.replace(/\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}/g, (_match, key: string) => {
    const value = summary?.[key] ?? object.properties?.[key] ?? object.id;
    if (value === null || value === undefined) return "";
    if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
    try {
      return JSON.stringify(value);
    } catch {
      return String(value);
    }
  });
  return rendered.trim() || fallback;
}

export function buildPanelObjectViewRuntimeConfig(input: {
  objectType: ObjectType;
  config: ObjectViewConfig;
  object?: ObjectInstance | null;
  summary?: Record<string, unknown>;
  objectId?: string | null;
  host?: ObjectViewPanelHost;
}): ObjectViewPanelRuntimeConfig {
  const config = ensurePanelObjectViewConfiguration({
    objectType: input.objectType,
    config: input.config,
  });
  const panel = config.panel_config;
  const host = input.host ?? "object_explorer";
  const hostConfig = panel?.hosts.find((entry) => entry.host === host);
  const objectId = input.object?.id ?? input.objectId ?? null;
  const fallbackTitle = input.object
    ? objectViewTitle(input.object, input.objectType)
    : `${input.objectType.display_name || input.objectType.name} panel`;

  return {
    host,
    title: objectViewRenderConfigTemplate(
      panel?.title_template || config.title_template,
      input.object,
      input.summary,
      fallbackTitle,
    ),
    open_full_view_href: objectViewConfiguredHref({
      objectTypeId: input.objectType.id,
      objectId,
      mode: "configured",
      formFactor: "full",
      branchLabel: config.branch_label,
      tabId: config.form_factor === "full" ? config.selected_tab_id : null,
    }),
    selected_object_parameter:
      hostConfig?.selected_object_parameter || panel?.workshop_widget.selected_object_parameter || "selectedObject",
    property_names: panel?.property_names ?? config.panel_properties,
    section_kinds: panel?.section_kinds ?? objectViewPanelSectionKinds(config),
    density: panel?.density ?? "compact",
    show_title: panel?.show_title ?? true,
    show_open_full_view: Boolean(panel?.show_open_full_view && (hostConfig?.supports_open_full_view ?? true)),
    embed_supported: hostConfig?.enabled ?? false,
    workshop_widget: panel?.workshop_widget ?? {
      enabled: true,
      widget_id: `object-view-widget:${input.objectType.id}:panel`,
      selected_object_parameter: "selectedObject",
      height_px: 420,
    },
  };
}

function coreObjectViewConfig(
  objectType: ObjectType,
  properties: Property[],
  linkTypes: LinkType[],
  formFactor: ObjectViewFormFactor,
): ObjectViewConfig {
  const visible = objectViewVisibleProperties(properties);
  const prominent = objectViewProminentProperties(properties);
  const prominentNames = prominent.map((property) => property.name);
  const normalNames = visible
    .filter((property) => !prominentNames.includes(property.name))
    .map((property) => property.name);
  const panelNames = [
    ...prominentNames,
    ...normalNames,
  ].slice(0, 6);
  const linkedObjectTypeIds = objectViewLinkedObjectTypeIds(objectType, linkTypes);
  return {
    mode: "standard",
    form_factor: formFactor,
    title_template: `{{${objectType.title_property || objectType.primary_key_property || "id"}}}`,
    subtitle_property: objectType.primary_key_property || "",
    prominent_properties: prominentNames,
    panel_properties: formFactor === "panel" ? panelNames : visible.map((property) => property.name),
    sections: formFactor === "full"
      ? [
          coreObjectViewSection("summary", "Summary", "summary", "Title, primary key, prominent properties, and metadata."),
          coreObjectViewSection("properties", "Properties", "properties", "Normal non-hidden properties from the object type."),
          coreObjectViewSection("links", "Linked objects", "links", "Non-hidden linked objects grouped by link type."),
          coreObjectViewSection("actions", "Actions", "actions", "Actions available for this object type."),
          coreObjectViewSection("timeline", "Timeline", "timeline", "Object activity and metadata timeline."),
          coreObjectViewSection("graph", "Graph", "graph", "Object graph neighborhood."),
        ]
      : [
          coreObjectViewSection("summary", "Summary", "summary", "Title, primary key, and prominent properties."),
          coreObjectViewSection("properties", "Properties", "properties", "Compact non-hidden properties."),
          coreObjectViewSection("links", "Linked objects", "links", "Compact linked object previews."),
          coreObjectViewSection("actions", "Actions", "actions", "Primary actions."),
        ],
    sidebar_links: [],
    comments_enabled: false,
    branch_label: "core",
    auto_publish: true,
    metadata: {
      title_property: objectType.title_property || objectType.primary_key_property || null,
      primary_key_property: objectType.primary_key_property || null,
      normal_properties: normalNames,
      linked_object_type_ids: linkedObjectTypeIds,
      link_type_ids: linkTypes.map((link) => link.id),
      generated: true,
    },
  };
}

export function buildCoreObjectViews(input: {
  objectTypes: ObjectType[];
  propertiesByObjectType?: Record<string, Property[]>;
  linkTypes?: LinkType[];
}): ObjectViewDefinition[] {
  const linkTypes = input.linkTypes ?? [];
  return input.objectTypes.flatMap((objectType) => {
    const properties = input.propertiesByObjectType?.[objectType.id] ?? objectType.properties ?? [];
    const visibleLinks = linkTypes.filter(
      (link) =>
        link.visibility !== "hidden" &&
        (link.source_type_id === objectType.id || link.target_type_id === objectType.id),
    );
    return (["full", "panel"] as const).map((formFactor) => ({
      id: `core:${objectType.id}:${formFactor}`,
      name: `${objectTypeAPIName(objectType)}Core${formFactor === "full" ? "Full" : "Panel"}View`,
      display_name: `${objectType.display_name} core ${formFactor === "full" ? "full" : "panel"} view`,
      description: `Automatically generated core ${formFactor} Object View for ${objectType.display_name}.`,
      object_type_id: objectType.id,
      mode: "standard",
      form_factor: formFactor,
      config: coreObjectViewConfig(objectType, properties, visibleLinks, formFactor),
      branch_label: "core",
      published: true,
      status: "core",
      owner_id: "platform",
      created_by: "platform",
      created_at: objectType.created_at,
      updated_at: objectType.updated_at,
    }));
  });
}

function objectViewDefaultMetadataSignature(input: {
  objectType: ObjectType;
  properties: Property[];
  linkTypes: LinkType[];
  formFactor: ObjectViewFormFactor;
}) {
  const properties = [...input.properties]
    .sort((left, right) => left.name.localeCompare(right.name))
    .map((property) => ({
      id: property.id,
      name: property.name,
      display_name: property.display_name,
      display_mode: propertyDisplayMode(property),
      property_type: property.property_type,
      required: property.required,
      searchable: property.searchable ?? false,
      updated_at: property.updated_at,
    }));
  const linkTypes = [...input.linkTypes]
    .sort((left, right) => left.id.localeCompare(right.id))
    .map((link) => ({
      id: link.id,
      name: link.name,
      display_name: link.display_name,
      source_type_id: link.source_type_id,
      target_type_id: link.target_type_id,
      visibility: link.visibility ?? "normal",
      updated_at: link.updated_at,
    }));
  return JSON.stringify({
    form_factor: input.formFactor,
    object_type: {
      id: input.objectType.id,
      name: input.objectType.name,
      api_name: objectTypeAPIName(input.objectType),
      display_name: input.objectType.display_name,
      primary_key_property: objectTypePrimaryKey(input.objectType),
      title_property: objectTypeTitleProperty(input.objectType),
      updated_at: input.objectType.updated_at,
    },
    properties,
    link_types: linkTypes,
  });
}

function defaultCustomObjectViewPanelProperties(
  objectType: ObjectType,
  visibleProperties: Property[],
  prominentNames: string[],
) {
  const visibleNames = new Set(visibleProperties.map((property) => property.name));
  const baseNames = prominentNames.length > 0
    ? prominentNames
    : visibleProperties.map((property) => property.name);
  const candidates = [
    objectTypeTitleProperty(objectType),
    objectTypePrimaryKey(objectType),
    ...baseNames,
    ...visibleProperties
      .filter((property) => property.required || property.searchable)
      .map((property) => property.name),
  ].filter((name) => visibleNames.has(name));
  return uniqueNonEmpty(candidates).slice(0, 6);
}

export function buildDefaultCustomObjectViewConfig(input: {
  objectType: ObjectType;
  properties?: Property[];
  linkTypes?: LinkType[];
  formFactor: ObjectViewFormFactor;
  now?: string;
}): ObjectViewConfig {
  const properties = input.properties ?? input.objectType.properties ?? [];
  const visibleProperties = objectViewVisibleProperties(properties);
  const prominentNames = objectViewProminentProperties(properties).map((property) => property.name);
  const fullPropertyNames = prominentNames.length > 0
    ? prominentNames
    : visibleProperties.map((property) => property.name);
  const panelNames = defaultCustomObjectViewPanelProperties(
    input.objectType,
    visibleProperties,
    fullPropertyNames,
  );
  const linkTypes = objectViewLinksForType(input.objectType, input.linkTypes ?? []);
  const linkTypeIds = linkTypes.map((link) => link.id);
  const linkedObjectTypeIds = objectViewLinkedObjectTypeIds(input.objectType, linkTypes);
  const titleProperty = objectTypeTitleProperty(input.objectType);
  const primaryKeyProperty = objectTypePrimaryKey(input.objectType);
  const synchronizedAt = input.now ?? new Date().toISOString();
  const defaultSync: ObjectViewDefaultSyncMetadata = {
    enabled: true,
    state: "synced",
    source: "object_type_metadata",
    metadata_signature: objectViewDefaultMetadataSignature({
      objectType: input.objectType,
      properties,
      linkTypes,
      formFactor: input.formFactor,
    }),
    synchronized_at: synchronizedAt,
    generated_from_object_type_updated_at: input.objectType.updated_at ?? null,
    property_names: fullPropertyNames,
    prominent_property_names: prominentNames,
    panel_property_names: panelNames,
    link_type_ids: linkTypeIds,
  };

  const config: ObjectViewConfig = {
    mode: "configured",
    form_factor: input.formFactor,
    title_template: `{{${titleProperty}}}`,
    subtitle_property: primaryKeyProperty === titleProperty ? "" : primaryKeyProperty,
    prominent_properties: fullPropertyNames,
    panel_properties: panelNames,
    sections: input.formFactor === "full"
      ? [
          coreObjectViewSection("default-summary", "Summary", "summary", "Title, primary key, and default property list content."),
          coreObjectViewSection("default-properties", "Properties", "properties", "Prominent properties, or all non-hidden properties when none are prominent."),
          ...(linkTypes.length > 0
            ? [coreObjectViewSection("default-links", "Linked objects", "links", "Visible links for this object type.")]
            : []),
        ]
      : [
          coreObjectViewSection("default-summary", "Summary", "summary", "Compact title and key property list content."),
          coreObjectViewSection("default-properties", "Properties", "properties", "Critical non-hidden properties for compact panels."),
        ],
    sidebar_links: [],
    comments_enabled: false,
    branch_label: "default",
    auto_publish: true,
    default_sync: defaultSync,
    metadata: {
      title_property: titleProperty,
      primary_key_property: primaryKeyProperty,
      prominent_property_names: prominentNames,
      panel_property_names: panelNames,
      normal_properties: visibleProperties
        .filter((property) => !fullPropertyNames.includes(property.name))
        .map((property) => property.name),
      linked_object_type_ids: linkedObjectTypeIds,
      link_type_ids: linkTypeIds,
      default_custom: true,
      generated: true,
    },
  };
  const panelAwareConfig = ensurePanelObjectViewConfiguration({
    objectType: input.objectType,
    config,
  });
  return ensureObjectViewEditorShell({
    objectType: input.objectType,
    config: panelAwareConfig,
    formFactor: input.formFactor,
    now: synchronizedAt,
  });
}

export function markObjectViewConfigManuallyEdited(
  config: ObjectViewConfig,
  editedAt = new Date().toISOString(),
): ObjectViewConfig {
  return {
    ...config,
    default_sync: config.default_sync
      ? {
          ...config.default_sync,
          enabled: false,
          state: "manual",
          synchronized_at: editedAt,
        }
      : undefined,
    metadata: config.metadata
      ? {
          ...config.metadata,
          generated: false,
          default_custom: config.metadata.default_custom,
        }
      : config.metadata,
  };
}

export function buildDefaultCustomObjectViews(input: {
  objectTypes: ObjectType[];
  propertiesByObjectType?: Record<string, Property[]>;
  linkTypes?: LinkType[];
  existingViews?: ObjectViewDefinition[];
  now?: string;
  ownerId?: string;
}): ObjectViewDefinition[] {
  const existingViews = input.existingViews ?? [];
  const replacements = new Map<string, ObjectViewDefinition>();
  const generated: ObjectViewDefinition[] = [];
  const linkTypes = input.linkTypes ?? [];
  const now = input.now ?? new Date().toISOString();

  for (const objectType of input.objectTypes) {
    const properties = input.propertiesByObjectType?.[objectType.id] ?? objectType.properties ?? [];
    const visibleLinks = objectViewLinksForType(objectType, linkTypes);

    for (const formFactor of ["full", "panel"] as const) {
      const existingForFormFactor = existingViews.filter(
        (view) =>
          view.object_type_id === objectType.id &&
          view.mode === "configured" &&
          view.form_factor === formFactor,
      );
      const syncedDefault = existingForFormFactor.find(
        (view) => view.config?.default_sync?.enabled === true && view.config.default_sync.state === "synced",
      );
      const manualView = existingForFormFactor.find(
        (view) => view.config?.default_sync?.state === "manual" || !view.config?.default_sync,
      );
      const config = buildDefaultCustomObjectViewConfig({
        objectType,
        properties,
        linkTypes: visibleLinks,
        formFactor,
        now,
      });

      if (syncedDefault) {
        replacements.set(syncedDefault.id, {
          ...syncedDefault,
          config,
          branch_label: syncedDefault.branch_label ?? config.branch_label,
          published: syncedDefault.published ?? true,
          status: "default_synced",
          updated_at: now,
        });
        continue;
      }

      if (manualView) continue;

      generated.push({
        id: `custom-default:${objectType.id}:${formFactor}`,
        name: `${objectTypeAPIName(objectType)}Default${formFactor === "full" ? "Full" : "Panel"}View`,
        display_name: `${objectType.display_name} default ${formFactor === "full" ? "full" : "panel"} view`,
        description: `Automatically generated configured ${formFactor} Object View for ${objectType.display_name}.`,
        object_type_id: objectType.id,
        mode: "configured",
        form_factor: formFactor,
        config,
        branch_label: "default",
        published: true,
        status: "default_synced",
        owner_id: input.ownerId ?? "platform",
        created_by: input.ownerId ?? "platform",
        created_at: objectType.created_at,
        updated_at: now,
      });
    }
  }

  return [
    ...existingViews.map((view) => replacements.get(view.id) ?? view),
    ...generated,
  ];
}

export interface CreateObjectViewBody {
  name: string;
  display_name?: string;
  description?: string;
  object_type_id: string;
  mode?: ObjectViewMode;
  form_factor?: ObjectViewFormFactor;
  config?: ObjectViewConfig;
  branch_label?: string;
  published?: boolean;
}

export function listObjectViews(params?: {
  object_type_id?: string;
  form_factor?: ObjectViewFormFactor;
  page?: number;
  per_page?: number;
}) {
  const qs = new URLSearchParams();
  if (params?.object_type_id) qs.set("object_type_id", params.object_type_id);
  if (params?.form_factor) qs.set("form_factor", params.form_factor);
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  const tail = qs.toString();
  return api.get<{
    data: ObjectViewDefinition[];
    total: number;
    page?: number;
    per_page?: number;
  }>(`/object-views${tail ? `?${tail}` : ""}`);
}

export function createObjectView(body: CreateObjectViewBody) {
  return api.post<ObjectViewDefinition>("/object-views", body);
}

// ────────────────────────────────────────────────────────────────
// Object sets — used by /object-explorer.
// ────────────────────────────────────────────────────────────────

export interface ObjectSetPolicy {
  allowed_markings: string[];
  minimum_clearance: string | null;
  deny_guest_sessions: boolean;
  required_restricted_view_id: string | null;
}

export interface ObjectSetFilter {
  field: string;
  operator: string;
  value: unknown;
}

export interface ObjectSetTraversal {
  direction: "outbound" | "inbound" | "both";
  link_type_id: string | null;
  target_object_type_id: string | null;
  max_hops: number;
}

export interface ObjectSetJoin {
  secondary_object_type_id: string;
  left_field: string;
  right_field: string;
  join_kind: "inner" | "left";
}

export type ObjectExplorerSavedArtifactKind = "exploration" | "list";
export type ObjectExplorerSavedArtifactPrivacy = "private" | "public";

export interface ObjectExplorerSavedQueryState {
  query?: string;
  search_mode?: "lexical" | "semantic" | string;
  search_kind?: string;
  object_type_id?: string;
  property_filters?: ObjectQueryFilter[];
  linked_filter?: Record<string, unknown> | null;
  search_around?: ObjectSearchAroundQuery | null;
  selected_object_ids?: string[];
  exploration_context?: Record<string, unknown> | null;
}

export interface ObjectExplorerSavedLayout {
  view?: "table" | "cards" | "split" | string;
  columns?: string[];
  preview_panel?: boolean;
  density?: "compact" | "comfortable" | string;
  sort?: ObjectQuerySort[];
}

export interface ObjectSetDefinition {
  id: string;
  name: string;
  description: string;
  base_object_type_id: string;
  filters: ObjectSetFilter[];
  traversals: ObjectSetTraversal[];
  join: ObjectSetJoin | null;
  projections: string[];
  what_if_label: string | null;
  policy: ObjectSetPolicy;
  kind?: ObjectExplorerSavedArtifactKind | string;
  query_state?: ObjectExplorerSavedQueryState | Record<string, unknown> | null;
  layout?: ObjectExplorerSavedLayout | Record<string, unknown> | null;
  privacy?: ObjectExplorerSavedArtifactPrivacy | string;
  project_id?: string | null;
  folder_path?: string;
  share_slug?: string;
  selected_object_ids?: string[];
  materialized_snapshot: unknown[] | null;
  materialized_at: string | null;
  materialized_row_count: number;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

export interface ObjectSetEvaluationResponse {
  object_set: ObjectSetDefinition;
  total_base_matches: number;
  total_rows: number;
  traversal_neighbor_count: number;
  rows: Record<string, unknown>[];
  generated_at: string;
  materialized: boolean;
}

export interface ObjectExplorerTypeGroup {
  id: string;
  name: string;
  display_name: string;
  description: string;
  object_types: ObjectType[];
  object_type_ids: string[];
  synthetic?: boolean;
}

export function listObjectSets(params?: { size?: number; token?: string }) {
  const qs = new URLSearchParams();
  if (params?.size) qs.set("size", String(params.size));
  if (params?.token) qs.set("token", params.token);
  return api.get<{ data: ObjectSetDefinition[]; next_token?: string }>(
    `/ontology/object-sets${qs.toString() ? `?${qs}` : ""}`,
  );
}

export function createObjectSet(body: {
  name: string;
  description?: string;
  base_object_type_id: string;
  filters?: ObjectSetFilter[];
  traversals?: ObjectSetTraversal[];
  join?: ObjectSetJoin | null;
  projections?: string[];
  what_if_label?: string | null;
  policy?: Partial<ObjectSetPolicy>;
  kind?: ObjectExplorerSavedArtifactKind | string;
  query_state?: ObjectExplorerSavedQueryState | Record<string, unknown> | null;
  layout?: ObjectExplorerSavedLayout | Record<string, unknown> | null;
  privacy?: ObjectExplorerSavedArtifactPrivacy | string;
  project_id?: string | null;
  folder_path?: string;
  share_slug?: string;
  selected_object_ids?: string[];
}) {
  return api.post<ObjectSetDefinition>("/ontology/object-sets", body);
}

export function updateObjectSet(
  id: string,
  body: Partial<{
    name: string;
    description: string;
    base_object_type_id: string;
    filters: ObjectSetFilter[];
    traversals: ObjectSetTraversal[];
    join: ObjectSetJoin | null;
    projections: string[];
    what_if_label: string | null;
    policy: ObjectSetPolicy;
    kind: ObjectExplorerSavedArtifactKind | string;
    query_state: ObjectExplorerSavedQueryState | Record<string, unknown> | null;
    layout: ObjectExplorerSavedLayout | Record<string, unknown> | null;
    privacy: ObjectExplorerSavedArtifactPrivacy | string;
    project_id: string | null;
    folder_path: string;
    share_slug: string;
    selected_object_ids: string[];
  }>,
) {
  return api.patch<ObjectSetDefinition>(`/ontology/object-sets/${id}`, body);
}

export function deleteObjectSet(id: string) {
  return api.delete(`/ontology/object-sets/${id}`);
}

export function evaluateObjectSet(id: string, body?: { limit?: number }) {
  return api.post<ObjectSetEvaluationResponse>(
    `/ontology/object-sets/${id}/evaluate`,
    body ?? {},
  );
}

export function materializeObjectSet(id: string, body?: { limit?: number }) {
  return api.post<ObjectSetEvaluationResponse>(
    `/ontology/object-sets/${id}/materialize`,
    body ?? {},
  );
}

export function objectExplorerSavedArtifactKind(objectSet: Partial<ObjectSetDefinition>): ObjectExplorerSavedArtifactKind {
  return objectSet.kind === "list" ? "list" : "exploration";
}

export function objectExplorerSavedArtifactPrivacy(objectSet: Partial<ObjectSetDefinition>): ObjectExplorerSavedArtifactPrivacy {
  return objectSet.privacy === "public" ? "public" : "private";
}

function objectExplorerSlug(text: string) {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 48) || "exploration";
}

export function objectExplorerShareSlug(objectSet: Pick<ObjectSetDefinition, "id" | "name"> & Partial<ObjectSetDefinition>) {
  return objectSet.share_slug || `${objectExplorerSlug(objectSet.name)}-${objectSet.id.slice(0, 8)}`;
}

export function objectExplorerShareLink(
  objectSet: Pick<ObjectSetDefinition, "id" | "name"> & Partial<ObjectSetDefinition>,
  origin = "",
) {
  const base = origin.replace(/\/$/, "");
  return `${base}/object-explorer?exploration=${encodeURIComponent(objectSet.id)}&slug=${encodeURIComponent(objectExplorerShareSlug(objectSet))}`;
}

export function buildObjectExplorerSavedQueryState(input: {
  query?: string;
  search_mode?: string;
  search_kind?: string;
  object_type_id?: string;
  property_filters?: ObjectQueryFilter[];
  linked_filter?: Record<string, unknown> | null;
  search_around?: ObjectSearchAroundQuery | null;
  selected_object_ids?: string[];
  exploration_context?: Record<string, unknown> | null;
}): ObjectExplorerSavedQueryState {
  return {
    query: input.query?.trim() || "",
    search_mode: input.search_mode || "lexical",
    search_kind: input.search_kind || "object_instance",
    object_type_id: input.object_type_id || "",
    property_filters: input.property_filters || [],
    linked_filter: input.linked_filter ?? null,
    search_around: input.search_around ?? null,
    selected_object_ids: uniqueNonEmpty(input.selected_object_ids || []),
    exploration_context: input.exploration_context ?? null,
  };
}

export function buildObjectExplorerSavedLayout(input?: Partial<ObjectExplorerSavedLayout> | null): ObjectExplorerSavedLayout {
  return {
    view: input?.view || "split",
    columns: uniqueNonEmpty(input?.columns || []),
    preview_panel: input?.preview_panel ?? true,
    density: input?.density || "comfortable",
    sort: input?.sort || [],
  };
}

export function objectExplorerSavedArtifactAccess(
  objectSet: ObjectSetDefinition,
  objectType: ObjectType | null | undefined,
  principal?: OntologyPermissionPrincipal | null,
) {
  const objectAccess = buildObjectInstanceViewPolicy({ objectType, principal });
  const effectivePrincipal = (principal || { roles: [], permissions: [] }) as OntologyPermissionPrincipal;
  const userID = String(effectivePrincipal.user_id || "");
  const ownArtifact = Boolean(userID && objectSet.owner_id === userID);
  const privacy = objectExplorerSavedArtifactPrivacy(objectSet);
  const publicArtifact = privacy === "public";
  const elevated = hasAny(normalizedPrincipalRoles(effectivePrincipal), ["admin", "owner", "ontology-admin", "ontology_admin", "ontology_manager"]) ||
    hasAny(normalizedPrincipalPermissions(effectivePrincipal), ["ontology:manage", "projects:manage", "resources:manage"]);
  const canViewMetadata = objectAccess.can_view_definition && (ownArtifact || publicArtifact || elevated);
  return {
    can_view_metadata: canViewMetadata,
    can_view_objects: canViewMetadata && objectAccess.can_view_instances,
    schema_only: canViewMetadata && !objectAccess.can_view_instances,
    privacy,
    own_artifact: ownArtifact,
    share_link: objectExplorerShareLink(objectSet),
    reason: canViewMetadata
      ? objectAccess.reason
      : publicArtifact
        ? "Saved exploration metadata is public, but object type definition access is required."
        : "Saved exploration is private to its owner.",
  };
}

export function objectExplorerVisibleObjectTypes(
  objectTypes: ObjectType[],
  principal?: OntologyPermissionPrincipal | null,
) {
  return objectTypes.filter((objectType) => {
    if (objectType.visibility === "hidden") return false;
    return buildObjectInstanceViewPolicy({ objectType, principal }).can_view_definition;
  });
}

export function objectExplorerVisibleObjectSets(
  objectSets: ObjectSetDefinition[],
  objectTypes: ObjectType[],
  principal?: OntologyPermissionPrincipal | null,
) {
  const typeByID = new Map(objectTypes.map((objectType) => [objectType.id, objectType]));
  return objectSets.filter((objectSet) => {
    const objectType = typeByID.get(objectSet.base_object_type_id);
    if (!objectType || objectType.visibility === "hidden") return false;
    return objectExplorerSavedArtifactAccess(objectSet, objectType, principal).can_view_metadata;
  });
}

export type ObjectExplorerOpenInTarget =
  | "object_views"
  | "graph"
  | "map"
  | "workshop"
  | "reports";

export interface ObjectExplorerProductConfig {
  max_action_selection_count?: number;
  max_export_selection_count?: number;
  export_enabled?: boolean;
  clipboard_export_enabled?: boolean;
  open_in_targets?: ObjectExplorerOpenInTarget[];
}

export interface ObjectExplorerActionContext {
  object_type_id: string;
  object_type?: ObjectType | null;
  selected_object_ids: string[];
  object_set_id?: string | null;
  object_set_name?: string | null;
  can_view_objects?: boolean;
}

export interface ObjectExplorerActionPrefill {
  initial_parameters: Record<string, unknown>;
  hidden_params: string[];
  target_object_id: string | null;
  batch_target_object_ids: string[];
  prefilled_parameter_names: string[];
  selection_count: number;
  blocked_reason: string;
  warning: string;
}

export interface ObjectExplorerOpenInAffordance {
  id: ObjectExplorerOpenInTarget;
  label: string;
  href: string;
  enabled: boolean;
  reason: string;
}

export interface ObjectExplorerExportAffordance {
  id: "copy_ids" | "csv" | "json";
  label: string;
  enabled: boolean;
  reason: string;
  file_name: string;
}

export const DEFAULT_OBJECT_EXPLORER_PRODUCT_CONFIG: Required<ObjectExplorerProductConfig> = {
  max_action_selection_count: 1000,
  max_export_selection_count: 10000,
  export_enabled: true,
  clipboard_export_enabled: true,
  open_in_targets: ["object_views", "graph", "map", "workshop", "reports"],
};

export function normalizeObjectExplorerProductConfig(config?: ObjectExplorerProductConfig | null): Required<ObjectExplorerProductConfig> {
  return {
    ...DEFAULT_OBJECT_EXPLORER_PRODUCT_CONFIG,
    ...(config || {}),
    open_in_targets: config?.open_in_targets?.length ? config.open_in_targets : DEFAULT_OBJECT_EXPLORER_PRODUCT_CONFIG.open_in_targets,
  };
}

function actionObjectExplorerConfig(action: Partial<ActionType>) {
  if (!action.config || typeof action.config !== "object" || Array.isArray(action.config)) return {};
  return action.config as Record<string, unknown>;
}

function actionFieldTypeClasses(field: ActionInputField) {
  const record = field as ActionInputField & {
    type_classes?: Array<string | { kind?: string; name?: string }>;
    typeClasses?: Array<string | { kind?: string; name?: string }>;
  };
  const raw = record.type_classes || record.typeClasses || [];
  return raw.flatMap((entry) => {
    if (typeof entry === "string") return [entry];
    if (!entry || typeof entry !== "object") return [];
    return [`${entry.kind || ""}:${entry.name || ""}`, entry.kind || "", entry.name || ""].filter(Boolean);
  });
}

function actionFieldMatchesTypeClass(field: ActionInputField, pattern: string) {
  return actionFieldTypeClasses(field).some((entry) => entry === pattern || entry.includes(pattern));
}

export function objectExplorerActionHiddenInExplorer(action: ActionType) {
  const config = actionObjectExplorerConfig(action);
  if (config.object_explorer_hidden === true || config.hide_in_object_explorer === true) return true;
  return action.input_schema.some((field) => actionFieldMatchesTypeClass(field, "hubble-oe:hide-action"));
}

function objectExplorerConfiguredField(action: ActionType, names: string[]) {
  const config = actionObjectExplorerConfig(action);
  const operation = config.operation && typeof config.operation === "object" && !Array.isArray(config.operation)
    ? config.operation as Record<string, unknown>
    : {};
  for (const key of names) {
    const value = typeof config[key] === "string" ? config[key] : typeof operation[key] === "string" ? operation[key] : "";
    if (!value) continue;
    const field = action.input_schema.find((entry) => entry.name === value);
    if (field) return field;
  }
  return null;
}

function isObjectReferenceParameter(field: ActionInputField) {
  return ["object_reference", "object_ref", "reference"].includes(field.property_type.toLowerCase());
}

function isObjectReferenceListParameter(field: ActionInputField) {
  return [
    "object_set",
    "object_reference_list",
    "object_reference[]",
    "object_reference_array",
    "array<object_reference>",
  ].includes(field.property_type.toLowerCase());
}

function uniqueObjectExplorerIDs(ids: string[]) {
  return uniqueNonEmpty(ids.map((id) => id.trim()));
}

export function objectExplorerApplicableActionsForContext(
  actions: ActionType[],
  context: Pick<ObjectExplorerActionContext, "object_type_id">,
) {
  return actions.filter((action) => {
    if (objectExplorerActionHiddenInExplorer(action)) return false;
    if (action.object_type_id === context.object_type_id) return true;
    return action.input_schema.some((field) =>
      actionFieldMatchesTypeClass(field, `hubble-oe-object-set-rid:${context.object_type_id}`) ||
      actionFieldMatchesTypeClass(field, `hubble-oe-object-type:${context.object_type_id}`),
    );
  });
}

export function buildObjectExplorerActionPrefill(
  action: ActionType,
  context: ObjectExplorerActionContext,
  config?: ObjectExplorerProductConfig | null,
): ObjectExplorerActionPrefill {
  const product = normalizeObjectExplorerProductConfig(config);
  const ids = uniqueObjectExplorerIDs(context.selected_object_ids);
  const initialParameters: Record<string, unknown> = {};
  const hiddenParams: string[] = [];
  const objectField = objectExplorerConfiguredField(action, ["target_object_input_name", "object_input_name", "source_input_name"]) ||
    action.input_schema.filter(isObjectReferenceParameter)[0] || null;
  const objectListField = objectExplorerConfiguredField(action, ["object_set_input_name", "object_reference_list_input_name", "target_objects_input_name"]) ||
    action.input_schema.filter(isObjectReferenceListParameter)[0] || null;
  const multipleObjectFields = action.input_schema.filter(isObjectReferenceParameter).length > 1;
  const multipleListFields = action.input_schema.filter(isObjectReferenceListParameter).length > 1;
  let targetObjectID: string | null = null;
  let batchTargetObjectIDs: string[] = [];
  let warning = "";
  let blockedReason = "";

  if (!context.can_view_objects) {
    blockedReason = "Object data is restricted for this selection.";
  } else if (ids.length > product.max_action_selection_count) {
    blockedReason = `Select ${product.max_action_selection_count} objects or fewer to run actions.`;
  } else if (ids.length === 0) {
    warning = "Run an exploration or select an object before applying actions.";
  }

  if (!blockedReason && ids.length > 0) {
    if (ids.length > 1 && objectListField && !multipleListFields) {
      initialParameters[objectListField.name] = ids;
      hiddenParams.push(objectListField.name);
    } else if (ids.length === 1 && objectField && !multipleObjectFields) {
      targetObjectID = ids[0];
      initialParameters[objectField.name] = ids[0];
      hiddenParams.push(objectField.name);
    } else if (ids.length === 1) {
      targetObjectID = ids[0];
      if (multipleObjectFields) warning = "Multiple object-reference parameters are available, so Object Explorer leaves the prefill visible.";
    } else {
      batchTargetObjectIDs = ids;
      if (multipleListFields) warning = "Multiple object-list parameters are available, so Object Explorer uses batch execution instead of prefilling one.";
    }
  }

  return {
    initial_parameters: initialParameters,
    hidden_params: hiddenParams,
    target_object_id: targetObjectID,
    batch_target_object_ids: batchTargetObjectIDs,
    prefilled_parameter_names: hiddenParams,
    selection_count: ids.length,
    blocked_reason: blockedReason,
    warning,
  };
}

function objectExplorerSelectionQuery(context: ObjectExplorerActionContext, limit = 100) {
  const params = new URLSearchParams();
  params.set("object_type_id", context.object_type_id);
  if (context.object_set_id) params.set("object_set_id", context.object_set_id);
  const ids = uniqueObjectExplorerIDs(context.selected_object_ids).slice(0, limit);
  if (ids.length > 0) params.set("object_ids", ids.join(","));
  return params.toString();
}

export function buildObjectExplorerOpenInAffordances(
  context: ObjectExplorerActionContext,
  config?: ObjectExplorerProductConfig | null,
): ObjectExplorerOpenInAffordance[] {
  const product = normalizeObjectExplorerProductConfig(config);
  const enabled = new Set(product.open_in_targets);
  const count = uniqueObjectExplorerIDs(context.selected_object_ids).length;
  const query = objectExplorerSelectionQuery(context);
  const firstObjectID = uniqueObjectExplorerIDs(context.selected_object_ids)[0];
  const objectViewHref = firstObjectID
    ? objectViewConfiguredHref({
        objectType: context.object_type,
        objectTypeId: context.object_type_id,
        objectId: firstObjectID,
        mode: "configured",
        formFactor: "full",
      })
    : objectViewConfiguredHref({
        objectType: context.object_type,
        objectTypeId: context.object_type_id,
        mode: "configured",
        formFactor: "full",
      });
  const hasGeo = Boolean(context.object_type && (
    objectTypeGeoPointPropertyNames(context.object_type).length > 0 ||
    objectTypeGeoShapePropertyNames(context.object_type).length > 0
  ));
  const dataAvailable = context.can_view_objects !== false && count > 0;

  const entries: ObjectExplorerOpenInAffordance[] = [
    { id: "object_views", label: firstObjectID ? "Object View" : "Object View type preview", href: objectViewHref, enabled: enabled.has("object_views"), reason: "" },
    { id: "graph", label: "Ontology graph", href: `/ontology/graph?${query}`, enabled: enabled.has("graph") && dataAvailable, reason: dataAvailable ? "" : "Open a result set first." },
    { id: "map", label: "Map workspace", href: `/geospatial?${query}`, enabled: enabled.has("map") && dataAvailable && hasGeo, reason: hasGeo ? (dataAvailable ? "" : "Open a result set first.") : "Object type has no geospatial properties." },
    { id: "workshop", label: "Workshop apps", href: `/apps?${query}`, enabled: enabled.has("workshop") && dataAvailable, reason: dataAvailable ? "" : "Open a result set first." },
    { id: "reports", label: "Reports", href: `/reports?${query}`, enabled: enabled.has("reports") && dataAvailable, reason: dataAvailable ? "" : "Open a result set first." },
  ];
  return entries.filter((entry) => enabled.has(entry.id));
}

export function buildObjectExplorerExportAffordances(
  context: ObjectExplorerActionContext,
  config?: ObjectExplorerProductConfig | null,
): ObjectExplorerExportAffordance[] {
  const product = normalizeObjectExplorerProductConfig(config);
  const ids = uniqueObjectExplorerIDs(context.selected_object_ids);
  const baseName = (context.object_set_name || context.object_type?.display_name || context.object_type_id || "object-explorer")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "") || "object-explorer";
  const commonReason = !product.export_enabled
    ? "Exports are disabled by Object Explorer configuration."
    : context.can_view_objects === false
      ? "Object data is restricted for this selection."
      : ids.length === 0
        ? "Open a result set first."
        : ids.length > product.max_export_selection_count
          ? `Export ${product.max_export_selection_count} objects or fewer.`
          : "";
  const enabled = commonReason === "";
  return [
    { id: "copy_ids", label: "Copy object IDs", enabled: enabled && product.clipboard_export_enabled, reason: product.clipboard_export_enabled ? commonReason : "Clipboard export is disabled.", file_name: `${baseName}-ids.txt` },
    { id: "csv", label: "Download CSV", enabled, reason: commonReason, file_name: `${baseName}.csv` },
    { id: "json", label: "Download JSON", enabled, reason: commonReason, file_name: `${baseName}.json` },
  ];
}

export function buildObjectExplorerTypeGroups(
  groups: OntologyObjectTypeGroup[],
  objectTypes: ObjectType[],
): ObjectExplorerTypeGroup[] {
  const visibleTypes = objectTypes.filter((objectType) => objectType.visibility !== "hidden");
  const typeByID = new Map(visibleTypes.map((objectType) => [objectType.id, objectType]));
  const out: ObjectExplorerTypeGroup[] = [];
  const assigned = new Set<string>();
  for (const group of groups.filter((entry) => entry.visibility !== "hidden" && entry.status !== "deprecated")) {
    const groupTypes = (group.object_type_ids || [])
      .map((id) => typeByID.get(id))
      .filter((entry): entry is ObjectType => Boolean(entry));
    if (groupTypes.length === 0) continue;
    for (const objectType of groupTypes) assigned.add(objectType.id);
    out.push({
      id: group.id,
      name: group.name,
      display_name: group.display_name || group.name,
      description: group.description || "",
      object_types: groupTypes,
      object_type_ids: groupTypes.map((objectType) => objectType.id),
    });
  }

  const knownGroupNames = new Set(out.map((group) => group.name.toLowerCase()));
  const legacyByName = new Map<string, ObjectType[]>();
  for (const objectType of visibleTypes) {
    if (assigned.has(objectType.id)) continue;
    for (const groupName of objectType.group_names || []) {
      const normalized = groupName.trim();
      if (!normalized || knownGroupNames.has(normalized.toLowerCase())) continue;
      const entries = legacyByName.get(normalized) || [];
      entries.push(objectType);
      legacyByName.set(normalized, entries);
      assigned.add(objectType.id);
    }
  }
  for (const [name, entries] of legacyByName) {
    out.push({
      id: `legacy:${name}`,
      name,
      display_name: name,
      description: "Legacy object type group metadata",
      object_types: entries,
      object_type_ids: entries.map((objectType) => objectType.id),
      synthetic: true,
    });
  }

  const ungrouped = visibleTypes.filter((objectType) => !assigned.has(objectType.id));
  if (ungrouped.length > 0 && out.length > 0) {
    out.push({
      id: "other",
      name: "other",
      display_name: "Other",
      description: "Visible object types not assigned to a configured group.",
      object_types: ungrouped,
      object_type_ids: ungrouped.map((objectType) => objectType.id),
      synthetic: true,
    });
  }
  return out.sort((left, right) => left.display_name.localeCompare(right.display_name));
}

export interface ObjectExplorerLinkContext {
  link_type: LinkType;
  source_object_type_id: string;
  target_object_type_id: string;
  direction: NonNullable<ObjectSearchAroundQuery["direction"]>;
  reverse_direction: NonNullable<ObjectSearchAroundQuery["direction"]>;
  traversal_direction: ObjectSetTraversal["direction"];
  reverse_traversal_direction: ObjectSetTraversal["direction"];
}

export interface ObjectExplorerPivotQuery {
  target_object_type_id: string;
  search_around: ObjectSearchAroundQuery;
  context: ObjectExplorerLinkContext;
}

function reverseSearchAroundDirection(direction: NonNullable<ObjectSearchAroundQuery["direction"]>): NonNullable<ObjectSearchAroundQuery["direction"]> {
  if (direction === "outgoing") return "incoming";
  if (direction === "incoming") return "outgoing";
  return "both";
}

function traversalDirectionFromSearchAround(direction: NonNullable<ObjectSearchAroundQuery["direction"]>): ObjectSetTraversal["direction"] {
  if (direction === "outgoing") return "outbound";
  if (direction === "incoming") return "inbound";
  return "both";
}

export function objectExplorerLinkedTargetForType(
  linkType: LinkType,
  sourceObjectTypeId: string,
  targetObjectTypeId?: string | null,
): ObjectExplorerLinkContext | null {
  if (!sourceObjectTypeId) return null;
  const isSelfLink = linkType.source_type_id === sourceObjectTypeId && linkType.target_type_id === sourceObjectTypeId;
  if (isSelfLink) {
    return {
      link_type: linkType,
      source_object_type_id: sourceObjectTypeId,
      target_object_type_id: sourceObjectTypeId,
      direction: "both",
      reverse_direction: "both",
      traversal_direction: "both",
      reverse_traversal_direction: "both",
    };
  }
  if (linkType.source_type_id === sourceObjectTypeId) {
    if (targetObjectTypeId && targetObjectTypeId !== linkType.target_type_id) return null;
    return {
      link_type: linkType,
      source_object_type_id: sourceObjectTypeId,
      target_object_type_id: linkType.target_type_id,
      direction: "outgoing",
      reverse_direction: "incoming",
      traversal_direction: "outbound",
      reverse_traversal_direction: "inbound",
    };
  }
  if (linkType.target_type_id === sourceObjectTypeId) {
    if (targetObjectTypeId && targetObjectTypeId !== linkType.source_type_id) return null;
    return {
      link_type: linkType,
      source_object_type_id: sourceObjectTypeId,
      target_object_type_id: linkType.source_type_id,
      direction: "incoming",
      reverse_direction: "outgoing",
      traversal_direction: "inbound",
      reverse_traversal_direction: "outbound",
    };
  }
  return null;
}

export function objectExplorerLinksForType(
  linkTypes: LinkType[],
  objectTypeId: string,
  visibleObjectTypeIds?: Set<string>,
) {
  return linkTypes.filter((linkType) => {
    if (linkType.visibility === "hidden") return false;
    const context = objectExplorerLinkedTargetForType(linkType, objectTypeId);
    if (!context) return false;
    return !visibleObjectTypeIds || visibleObjectTypeIds.has(context.target_object_type_id);
  });
}

export function buildObjectExplorerPivotQuery(input: {
  source_object_type_id: string;
  source_object_ids: string[];
  link_type: LinkType;
  target_object_type_id?: string | null;
  depth?: number;
}): ObjectExplorerPivotQuery | null {
  const context = objectExplorerLinkedTargetForType(input.link_type, input.source_object_type_id, input.target_object_type_id);
  if (!context) return null;
  const sourceObjectIds = uniqueNonEmpty(input.source_object_ids);
  return {
    target_object_type_id: context.target_object_type_id,
    context,
    search_around: {
      source_object_ids: sourceObjectIds,
      link_type_id: input.link_type.id,
      link_type_ids: [input.link_type.id],
      direction: context.direction,
      depth: Math.max(1, Math.min(4, input.depth ?? 1)),
      target_object_type_id: context.target_object_type_id,
    },
  };
}

export function buildObjectExplorerLinkedFilterQuery(input: {
  base_object_type_id: string;
  anchor_object_ids: string[];
  link_type: LinkType;
  depth?: number;
}): ObjectExplorerPivotQuery | null {
  const context = objectExplorerLinkedTargetForType(input.link_type, input.base_object_type_id);
  if (!context) return null;
  const reverseDirection = reverseSearchAroundDirection(context.direction);
  return {
    target_object_type_id: input.base_object_type_id,
    context: {
      ...context,
      direction: reverseDirection,
      reverse_direction: context.direction,
      traversal_direction: traversalDirectionFromSearchAround(reverseDirection),
      reverse_traversal_direction: context.traversal_direction,
    },
    search_around: {
      source_object_ids: uniqueNonEmpty(input.anchor_object_ids),
      link_type_id: input.link_type.id,
      link_type_ids: [input.link_type.id],
      direction: reverseDirection,
      depth: Math.max(1, Math.min(4, input.depth ?? 1)),
      target_object_type_id: input.base_object_type_id,
    },
  };
}

export function buildObjectExplorerPivotObjectSetDraft(input: {
  result_object_ids: string[];
  result_object_type_id: string;
  source_object_type_id: string;
  source_object_ids?: string[];
  link_type: LinkType;
  depth?: number;
}) {
  const pivot = input.source_object_ids ? buildObjectExplorerPivotQuery({
    source_object_type_id: input.source_object_type_id,
    source_object_ids: input.source_object_ids,
    link_type: input.link_type,
    target_object_type_id: input.result_object_type_id,
    depth: input.depth,
  }) : null;
  const context = objectExplorerLinkedTargetForType(input.link_type, input.source_object_type_id, input.result_object_type_id);
  const resultIds = uniqueNonEmpty(input.result_object_ids);
  return {
    filters: resultIds.length > 0 ? [{ field: "id", operator: "in", value: resultIds } satisfies ObjectSetFilter] : [],
    traversals: context ? [{
      direction: context.reverse_traversal_direction,
      link_type_id: input.link_type.id,
      target_object_type_id: input.source_object_type_id,
      max_hops: Math.max(1, Math.min(4, input.depth ?? 1)),
    } satisfies ObjectSetTraversal] : [],
    search_around: pivot?.search_around ?? null,
  };
}

// ────────────────────────────────────────────────────────────────
// Ontology funnel sources / runs / health — used by /ontology-indexing.
// ────────────────────────────────────────────────────────────────

export interface OntologyFunnelPropertyMapping {
  source_field: string;
  target_property: string;
}

export interface OntologyFunnelSource {
  id: string;
  name: string;
  description: string;
  object_type_id: string;
  dataset_id: string;
  pipeline_id: string | null;
  dataset_branch: string | null;
  dataset_version: number | null;
  preview_limit: number;
  default_marking: string;
  status: string;
  property_mappings: OntologyFunnelPropertyMapping[];
  trigger_context: Record<string, unknown>;
  owner_id: string;
  last_run_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface OntologyFunnelRun {
  id: string;
  source_id: string;
  object_type_id: string;
  dataset_id: string;
  pipeline_id: string | null;
  pipeline_run_id: string | null;
  status: string;
  trigger_type: string;
  started_by: string | null;
  rows_read: number;
  inserted_count: number;
  updated_count: number;
  skipped_count: number;
  error_count: number;
  details: Record<string, unknown>;
  error_message: string | null;
  started_at: string;
  finished_at: string | null;
}

export interface OntologyFunnelSourceHealth {
  source: OntologyFunnelSource;
  health_status: string;
  health_reason: string;
  total_runs: number;
  successful_runs: number;
  failed_runs: number;
  warning_runs: number;
  success_rate: number;
  avg_duration_ms: number | null;
  p95_duration_ms: number | null;
  max_duration_ms: number | null;
  latest_run_status: string | null;
  last_run_at: string | null;
  last_success_at: string | null;
  last_failure_at: string | null;
  last_warning_at: string | null;
  rows_read: number;
  inserted_count: number;
  updated_count: number;
  skipped_count: number;
  error_count: number;
}

export interface OntologyFunnelHealthSummary {
  stale_after_hours: number;
  total_sources: number;
  active_sources: number;
  paused_sources: number;
  healthy_sources: number;
  degraded_sources: number;
  failing_sources: number;
  stale_sources: number;
  never_run_sources: number;
  total_runs: number;
  successful_runs: number;
  failed_runs: number;
  warning_runs: number;
  success_rate: number;
  rows_read: number;
  inserted_count: number;
  updated_count: number;
  skipped_count: number;
  error_count: number;
  last_run_at: string | null;
  sources: OntologyFunnelSourceHealth[];
}

export function getOntologyFunnelHealth(params?: {
  object_type_id?: string;
  stale_after_hours?: number;
}) {
  const qs = new URLSearchParams();
  if (params?.object_type_id) qs.set("object_type_id", params.object_type_id);
  if (params?.stale_after_hours)
    qs.set("stale_after_hours", String(params.stale_after_hours));
  return api.get<OntologyFunnelHealthSummary>(`/ontology/funnel/health?${qs}`);
}

export function listOntologyFunnelSources(params?: {
  object_type_id?: string;
  status?: string;
  page?: number;
  per_page?: number;
}) {
  const qs = new URLSearchParams();
  if (params?.object_type_id) qs.set("object_type_id", params.object_type_id);
  if (params?.status) qs.set("status", params.status);
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  return api.get<{
    data: OntologyFunnelSource[];
    total: number;
    page: number;
    per_page: number;
  }>(`/ontology/funnel/sources?${qs}`);
}

export function createOntologyFunnelSource(body: {
  name: string;
  description?: string;
  object_type_id: string;
  dataset_id: string;
  pipeline_id?: string | null;
  dataset_branch?: string | null;
  dataset_version?: number | null;
  preview_limit?: number;
  default_marking?: string;
  status?: string;
  property_mappings?: OntologyFunnelPropertyMapping[];
  trigger_context?: Record<string, unknown>;
}) {
  return api.post<OntologyFunnelSource>("/ontology/funnel/sources", body);
}

export function updateOntologyFunnelSource(
  id: string,
  body: {
    name?: string;
    description?: string;
    pipeline_id?: string | null;
    dataset_branch?: string | null;
    dataset_version?: number | null;
    preview_limit?: number;
    default_marking?: string;
    status?: string;
    property_mappings?: OntologyFunnelPropertyMapping[];
    trigger_context?: Record<string, unknown>;
  },
) {
  return api.patch<OntologyFunnelSource>(
    `/ontology/funnel/sources/${id}`,
    body,
  );
}

export function deleteOntologyFunnelSource(id: string) {
  return api.delete(`/ontology/funnel/sources/${id}`);
}

export function triggerOntologyFunnelRun(
  id: string,
  body?: {
    limit?: number;
    dataset_branch?: string;
    dataset_version?: number;
    skip_pipeline?: boolean;
    dry_run?: boolean;
    trigger_context?: Record<string, unknown>;
  },
) {
  return api.post<OntologyFunnelRun>(
    `/ontology/funnel/sources/${id}/run`,
    body ?? {},
  );
}

export interface OntologyReindexRequest {
  page_size?: number;
  trigger_context?: Record<string, unknown>;
}

export interface OntologyReindexResponse {
  job_id?: string;
  request_id?: string;
  type_id?: string;
  status?: string;
  message?: string;
  [key: string]: unknown;
}

export function reindexOntologyType(
  typeId: string,
  body?: OntologyReindexRequest,
) {
  return api.post<OntologyReindexResponse>(
    `/ontology/types/${typeId}/reindex`,
    body ?? {},
  );
}

export function listOntologyFunnelRuns(
  id: string,
  params?: { page?: number; per_page?: number },
) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  return api.get<{
    data: OntologyFunnelRun[];
    total: number;
    page: number;
    per_page: number;
  }>(`/ontology/funnel/sources/${id}/runs?${qs}`);
}

// ────────────────────────────────────────────────────────────────
// Project branches/proposals/migrations + shared property types —
// used by /ontologies, /ontology-manager, /ontology.
// ────────────────────────────────────────────────────────────────

export interface SharedPropertyType {
  id: string;
  name: string;
  display_name: string;
  description: string;
  property_type: string;
  value_type_id?: string | null;
  value_type?: OntologyValueType | null;
  value_formatting?: PropertyValueFormatting | Record<string, unknown>;
  conditional_formatting?: PropertyConditionalFormattingRule[];
  required: boolean;
  unique_constraint: boolean;
  time_dependent: boolean;
  default_value: unknown;
  validation_rules: unknown;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

export interface OntologyProjectResourceBinding {
  project_id: string;
  resource_kind: string;
  resource_id: string;
  bound_by: string;
  view_requirement_marking_rids?: string[];
  created_at: string;
}

export interface OntologyProjectMigrationResource {
  resource_kind: string;
  resource_id: string;
  label?: string;
}

export interface OntologyProjectMigration {
  id: string;
  project_id: string;
  source_project_id: string;
  target_project_id: string;
  resources: OntologyProjectMigrationResource[];
  submitted_at: string;
  status: string;
  note: string;
  submitted_by: string;
}

export interface OntologyStagedChange {
  id: string;
  kind: string;
  action: string;
  label: string;
  description: string;
  targetId?: string;
  parentRef?: {
    kind: string;
    id?: string;
    name?: string;
    slug?: string;
  };
  payload: Record<string, unknown>;
  warnings: string[];
  errors: string[];
  source: string;
  author?: string;
  createdBy?: string;
  updatedBy?: string;
  createdAt: string;
}

export type OntologyChangeValidationSeverity = "warning" | "error";

export interface OntologyChangeValidationIssue {
  severity: OntologyChangeValidationSeverity;
  code: string;
  message: string;
}

export interface OntologyUnsavedChangeReview {
  change: OntologyStagedChange;
  resource_kind: string;
  resource_id: string | null;
  author: string;
  timestamp: string;
  diff_summary: string;
  validation_issues: OntologyChangeValidationIssue[];
  validation_status: "valid" | "warning" | "error";
  save_ready: boolean;
  owned_by_current_user: boolean;
}

export interface OntologyUnsavedChangesReviewSummary {
  reviews: OntologyUnsavedChangeReview[];
  total: number;
  errors: number;
  warnings: number;
  save_ready: boolean;
  current_user_owned: number;
}

export interface OntologySavedChangeRecord {
  id: string;
  project_id: string;
  change_ids: string[];
  resources: Array<{ kind: string; id?: string | null; label?: string }>;
  changes: OntologyStagedChange[];
  branch_id?: string | null;
  proposal_id?: string | null;
  status: "saved" | "failed";
  validation_errors: OntologyChangeValidationIssue[];
  error_details?: unknown;
  note?: string;
  saved_by: string;
  saved_at: string;
}

export interface SaveOntologyChangesResponse {
  record: OntologySavedChangeRecord;
  working_state: OntologyProjectWorkingState;
}

export interface OntologyProjectWorkingState {
  project_id: string;
  changes: OntologyStagedChange[];
  updated_by: string;
  updated_at: string;
}

export type OntologyHistoryVisibilityFilter = "all" | "visible" | "hidden";
export type OntologyHistoryDetailsFilter = "all" | "viewable" | "restricted";

export interface OntologyHistoryFilters {
  resource_kind?: string;
  author?: string;
  from?: string;
  to?: string;
  visibility?: OntologyHistoryVisibilityFilter;
  details?: OntologyHistoryDetailsFilter;
  hide_restricted_details?: boolean;
}

export interface OntologyHistoryResourceSummary {
  kind: string;
  id: string | null;
  label: string;
  visibility: string;
  can_view_details: boolean;
  registry_entry?: OntologyResourceRegistryEntry;
  change?: OntologyStagedChange;
}

export interface OntologyHistoryEntry {
  id: string;
  record: OntologySavedChangeRecord;
  saved_at: string;
  author: string;
  status: OntologySavedChangeRecord["status"];
  note?: string;
  resources: OntologyHistoryResourceSummary[];
  visible_resources: OntologyHistoryResourceSummary[];
  changes_count: number;
  can_view_details: boolean;
  restricted_details_count: number;
}

export const ONTOLOGY_BUNDLE_SCHEMA_VERSION = "openfoundry.ontology.bundle.v1";

export type OntologyBundleResourceKind =
  | OntologyResourceRegistryKind
  | "value_type";
export type OntologyBundleResourceAction = "upsert" | "delete";

export interface OntologyBundleDependency {
  kind: OntologyBundleResourceKind;
  id: string;
  required?: boolean;
}

export interface OntologyBundleResource {
  kind: OntologyBundleResourceKind;
  id: string;
  api_name: string;
  display_name: string;
  action: OntologyBundleResourceAction;
  visibility?: string;
  payload: Record<string, unknown>;
  dependencies: OntologyBundleDependency[];
}

export interface OntologyBundle {
  schema_version: typeof ONTOLOGY_BUNDLE_SCHEMA_VERSION;
  bundle_id: string;
  exported_at: string;
  exported_by?: string | null;
  ontology: Pick<OntologyArtifact, "id" | "api_name" | "display_name" | "owning_space_slug">;
  resources: OntologyBundleResource[];
  working_state?: OntologyStagedChange[];
  metadata: {
    resource_count: number;
    includes_working_state: boolean;
    generated_by: "openfoundry";
  };
}

export type OntologyBundleValidationSeverity = "error" | "warning";

export interface OntologyBundleValidationIssue {
  severity: OntologyBundleValidationSeverity;
  code: string;
  message: string;
  resource_key?: string;
}

export interface OntologyBundleValidationResult {
  valid: boolean;
  errors: number;
  warnings: number;
  issues: OntologyBundleValidationIssue[];
  resources: OntologyBundleResource[];
  staged_changes: OntologyStagedChange[];
}

export type OntologyPermissionLevel = "none" | "view" | "edit" | "manage" | "owner";
export type OntologyObjectInstanceAccessMode =
  | "not_applicable"
  | "definition_not_viewable"
  | "object_policy"
  | "object_policy_blocked"
  | "object_policy_required"
  | "restricted_view"
  | "restricted_view_required"
  | "datasource_required"
  | "datasource_granted";

export interface OntologyPermissionPrincipal {
  user_id?: string | null;
  email?: string | null;
  groups?: string[];
  roles?: string[];
  permissions?: string[];
}

export type RestrictedViewStorageMode = "foundry_object_storage" | "local_storage" | "local_index" | "remote" | "none" | string;

export interface RestrictedViewRowPolicyRule {
  id?: string;
  description?: string;
  property?: string;
  operator?: "equals" | "not_equals" | "in" | "not_in" | "contains" | "exists" | "marking_in" | string;
  value?: unknown;
  values?: unknown[];
  allow?: boolean;
  allowed_user_ids?: string[];
  allowed_groups?: string[];
  allowed_roles?: string[];
  required_permissions?: string[];
  required_markings?: string[];
  denied_markings?: string[];
}

export interface RestrictedViewRowPolicy {
  mode?: "allow_all" | "deny_all" | "any_rule" | "all_rules" | "rules" | string;
  rules?: RestrictedViewRowPolicyRule[];
  row_rules?: RestrictedViewRowPolicyRule[];
  allowed_user_ids?: string[];
  allowed_groups?: string[];
  allowed_roles?: string[];
  required_permissions?: string[];
  required_markings?: string[];
  denied_markings?: string[];
}

export interface RestrictedViewBackingConfig {
  restricted_view_id: string;
  backing_dataset_id?: string | null;
  storage_mode?: RestrictedViewStorageMode | null;
  policy?: RestrictedViewRowPolicy | null;
  policy_version?: number | null;
  registered_policy_version?: number | null;
  indexed_policy_version?: number | null;
  policy_updated_at?: string | null;
  registered_at?: string | null;
  indexed_at?: string | null;
  require_reregistration_on_policy_change?: boolean;
  require_reindex_on_policy_change?: boolean;
}

export interface RestrictedViewPolicyOutcome {
  restricted_view_id: string;
  allowed: boolean;
  reason: string;
  matched_rules: string[];
  requires_runtime_evaluation?: boolean;
}

export interface RestrictedViewPropagationStatus {
  restricted_view_id: string | null;
  storage_mode: RestrictedViewStorageMode | null;
  policy_version: number;
  registered_policy_version: number;
  indexed_policy_version: number;
  requires_reregistration: boolean;
  requires_reindex: boolean;
  warnings: string[];
}

export type ObjectSecurityPolicyOperation = "read" | "edit_property" | "edit_policy_property";
export type ObjectSecurityPolicyEnforcementState = "not_configured" | "enforced" | "blocked";

export interface ObjectSecurityPolicyPrimitiveSupport {
  object_attribute_evaluation?: boolean;
  property_policy_evaluation?: boolean;
  edit_policy_evaluation?: boolean;
  test_fixtures?: boolean;
}

export interface ObjectSecurityPolicyDefinition extends RestrictedViewRowPolicy {
  id?: string;
  name?: string;
  display_name?: string;
  read_policy?: RestrictedViewRowPolicy;
  granular_policy?: RestrictedViewRowPolicy;
  edit_property_policy?: RestrictedViewRowPolicy;
  edit_policy_property_policy?: RestrictedViewRowPolicy;
}

export interface PropertySecurityPolicyDefinition extends ObjectSecurityPolicyDefinition {
  property_names?: string[];
  properties?: string[];
}

export interface ObjectSecurityPolicySupportStatus {
  enforcement_state: ObjectSecurityPolicyEnforcementState;
  blocked: boolean;
  configured: boolean;
  requires_attribute_evaluation: boolean;
  supports_attribute_evaluation: boolean;
  supports_property_policies: boolean;
  supports_edit_policies: boolean;
  has_test_fixtures: boolean;
  warnings: string[];
}

export interface ObjectInstanceViewPolicy {
  object_type_id: string;
  access_mode: OntologyObjectInstanceAccessMode;
  can_view_definition: boolean;
  can_view_instances: boolean;
  schema_only: boolean;
  reason: string;
  restricted_view_id?: string | null;
  restricted_view_policy_outcome?: RestrictedViewPolicyOutcome | null;
}

export type ObjectViewPermissionRequirementKind =
  | "object_type_or_resource_edit"
  | "object_view_admin"
  | "input_datasource_editor";

export interface ObjectViewPermissionRequirement {
  id: string;
  kind: ObjectViewPermissionRequirementKind;
  label: string;
  resource_id?: string | null;
  required_level?: OntologyPermissionLevel;
  effective_level?: OntologyPermissionLevel;
  allowed: boolean;
  reason: string;
}

export interface ObjectViewEditPermissionDecision {
  object_type_id: string;
  object_view_id: string | null;
  compatibility_mode: ObjectViewCompatibilityMode;
  allowed: boolean;
  can_edit_object_type: boolean;
  can_edit_object_view_resource: boolean;
  can_manage_object_view: boolean;
  can_edit_input_datasources: boolean;
  input_datasource_ids: string[];
  editable_input_datasource_ids: string[];
  requirements: ObjectViewPermissionRequirement[];
  warnings: string[];
  reason: string;
}

export interface ObjectViewRuntimePermissionDecision {
  object_type_id: string;
  can_view_definition: boolean;
  can_view_instances: boolean;
  schema_only: boolean;
  object_policy: ObjectInstanceViewPolicy;
  redacted_property_names: string[];
  reason: string;
}

export interface OntologyResourcePermissionDecision {
  resource_key: string;
  resource_kind: string;
  resource_id: string;
  display_name: string;
  project_id: string | null;
  project_display_name: string;
  folder_path: string;
  owner_id: string | null;
  effective_level: OntologyPermissionLevel;
  can_view_definition: boolean;
  can_view_instances: boolean;
  can_edit: boolean;
  can_manage: boolean;
  is_owner: boolean;
  object_instance_access: OntologyObjectInstanceAccessMode;
  reasons: string[];
}

export interface OntologyPermissionRequirement {
  resource_key: string;
  resource_kind: string;
  resource_id: string;
  resource_label: string;
  required_level: OntologyPermissionLevel;
  effective_level: OntologyPermissionLevel;
  allowed: boolean;
  reason: string;
}

export interface OntologyPermissionChangeCheck {
  change_id: string;
  change_label: string;
  change_kind: string;
  allowed: boolean;
  requirements: OntologyPermissionRequirement[];
}

export interface OntologyPermissionAnalysis {
  resources: OntologyResourcePermissionDecision[];
  change_checks: OntologyPermissionChangeCheck[];
  blocked_changes: number;
  totals: {
    resources: number;
    viewable_definitions: number;
    viewable_instances: number;
    editable: number;
    manageable: number;
    owned: number;
  };
}

const API_NAME_RE = /^[A-Za-z][A-Za-z0-9_]*$/;

function stagedPayload(change: OntologyStagedChange) {
  return change.payload && typeof change.payload === "object" ? change.payload as Record<string, unknown> : {};
}

export function ontologyChangeAuthor(change: OntologyStagedChange, fallback = "unknown") {
  return change.author || change.createdBy || change.updatedBy || fallback;
}

export function ontologyChangeResource(change: OntologyStagedChange) {
  const payload = stagedPayload(change);
  return {
    kind: change.kind || change.parentRef?.kind || "ontology_resource",
    id: change.targetId || (typeof payload.id === "string" ? payload.id : null),
  };
}

export function ontologyChangeDiffSummary(change: OntologyStagedChange) {
  const payload = stagedPayload(change);
  const keys = Object.keys(payload).filter((key) => payload[key] !== undefined);
  const changedFields = keys.length > 0 ? keys.slice(0, 6).join(", ") : "metadata";
  const suffix = keys.length > 6 ? ` +${keys.length - 6} more` : "";
  return `${change.action || "update"} ${change.kind || "resource"}: ${changedFields}${suffix}`;
}

function pushExistingIssues(change: OntologyStagedChange, issues: OntologyChangeValidationIssue[]) {
  for (const message of change.warnings || []) {
    issues.push({ severity: "warning", code: "existing_warning", message });
  }
  for (const message of change.errors || []) {
    issues.push({ severity: "error", code: "existing_error", message });
  }
}

export function validateOntologyStagedChange(change: OntologyStagedChange): OntologyChangeValidationIssue[] {
  const issues: OntologyChangeValidationIssue[] = [];
  const payload = stagedPayload(change);
  pushExistingIssues(change, issues);

  const apiName = payload.name ?? payload.api_name;
  if (typeof apiName === "string" && apiName.trim() && !API_NAME_RE.test(apiName.trim())) {
    issues.push({ severity: "error", code: "invalid_api_name", message: `API name ${apiName} must start with a letter and contain only letters, numbers, and underscores.` });
  }

  if ((change.kind === "link_type" || payload.source_object_type_id || payload.target_object_type_id) && change.action !== "delete") {
    if (!payload.source_object_type_id && !payload.source_type_id) {
      issues.push({ severity: "error", code: "missing_link_source", message: "Link changes must include a source object type." });
    }
    if (!payload.target_object_type_id && !payload.target_type_id) {
      issues.push({ severity: "error", code: "missing_link_target", message: "Link changes must include a target object type." });
    }
  }

  const primaryKey = payload.primary_key_property ?? payload.primary_key_column;
  if ((change.kind === "object_type" || change.kind === "object_type_binding") && change.action !== "delete" && typeof primaryKey === "string" && !primaryKey.trim()) {
    issues.push({ severity: "error", code: "missing_primary_key", message: "Object type and datasource mapping changes must define a primary key." });
  }

  if (Array.isArray(payload.property_mapping)) {
    const seen = new Set<string>();
    for (const entry of payload.property_mapping) {
      if (!entry || typeof entry !== "object") continue;
      const mapping = entry as Record<string, unknown>;
      const target = typeof mapping.target_property === "string" ? mapping.target_property : "";
      if (!mapping.source_field) issues.push({ severity: "error", code: "missing_mapping_source", message: "Datasource mappings must include source_field for each mapped property." });
      if (!target) issues.push({ severity: "error", code: "missing_mapping_target", message: "Datasource mappings must include target_property for each mapped property." });
      if (target && seen.has(target)) issues.push({ severity: "error", code: "duplicate_mapping_target", message: `Datasource mapping targets ${target} more than once.` });
      if (target) seen.add(target);
    }
  }

  if (change.kind === "interface_implementation") {
    if (Array.isArray(payload.missing_properties) && payload.missing_properties.length > 0) {
      issues.push({ severity: "error", code: "missing_interface_properties", message: "Interface implementation is missing required property mappings." });
    }
    if (Array.isArray(payload.missing_link_constraints) && payload.missing_link_constraints.length > 0) {
      issues.push({ severity: "error", code: "missing_interface_links", message: "Interface implementation is missing required link mappings." });
    }
  }

  if (change.kind === "object_view" && Array.isArray(payload.action_ids) && payload.action_ids.some((id) => !id)) {
    issues.push({ severity: "error", code: "invalid_action_reference", message: "Object View action references must point at concrete action type IDs." });
  }

  if ((change.kind === "action_type" || payload.operation_kind) && !payload.permission_key) {
    issues.push({ severity: "warning", code: "missing_permission_requirement", message: "Action changes should declare a permission key or authorization policy before save." });
  }

  if (["object_type", "property", "link_type"].includes(change.kind)) {
    issues.push({ severity: "warning", code: "object_view_impact", message: "Review downstream Object View impacts before saving this schema change." });
  }

  return issues;
}

export function reviewUnsavedOntologyChanges(
  changes: OntologyStagedChange[],
  currentUserId?: string | null,
): OntologyUnsavedChangesReviewSummary {
  const reviews = changes.map((change) => {
    const resource = ontologyChangeResource(change);
    const issues = validateOntologyStagedChange(change);
    const hasError = issues.some((issue) => issue.severity === "error");
    const hasWarning = issues.some((issue) => issue.severity === "warning");
    const author = ontologyChangeAuthor(change, currentUserId || "unknown");
    return {
      change,
      resource_kind: resource.kind,
      resource_id: resource.id,
      author,
      timestamp: change.createdAt,
      diff_summary: ontologyChangeDiffSummary(change),
      validation_issues: issues,
      validation_status: hasError ? "error" as const : hasWarning ? "warning" as const : "valid" as const,
      save_ready: !hasError,
      owned_by_current_user: Boolean(currentUserId && author === currentUserId),
    };
  });
  const errors = reviews.reduce((count, review) => count + review.validation_issues.filter((issue) => issue.severity === "error").length, 0);
  const warnings = reviews.reduce((count, review) => count + review.validation_issues.filter((issue) => issue.severity === "warning").length, 0);
  return {
    reviews,
    total: reviews.length,
    errors,
    warnings,
    save_ready: reviews.length > 0 && errors === 0,
    current_user_owned: reviews.filter((review) => review.owned_by_current_user).length,
  };
}

export function discardOntologyChange(changes: OntologyStagedChange[], changeId: string) {
  return changes.filter((change) => change.id !== changeId);
}

export function discardOntologyChangesOwnedBy(changes: OntologyStagedChange[], userId: string) {
  return changes.filter((change) => ontologyChangeAuthor(change) !== userId);
}

export function buildOntologyHistory(
  records: OntologySavedChangeRecord[],
  registry: OntologyResourceRegistryEntry[] = [],
  filters: OntologyHistoryFilters = {},
  options: { current_user_id?: string | null; can_view_hidden_details?: boolean } = {},
): OntologyHistoryEntry[] {
  const normalizedAuthor = filters.author?.trim().toLowerCase() || "";
  const from = filters.from ? new Date(filters.from) : null;
  const to = filters.to ? new Date(filters.to) : null;
  const visibility = filters.visibility || "all";
  const details = filters.details || "all";

  return records
    .map((record) => ontologyHistoryEntry(record, registry, options))
    .filter((entry) => {
      const savedAt = new Date(entry.saved_at);
      if (normalizedAuthor && !entry.author.toLowerCase().includes(normalizedAuthor)) return false;
      if (from && !Number.isNaN(from.getTime()) && savedAt < from) return false;
      if (to && !Number.isNaN(to.getTime()) && savedAt > to) return false;
      if (filters.resource_kind && !entry.resources.some((resource) => resource.kind === filters.resource_kind)) return false;
      if (visibility === "visible" && !entry.resources.some((resource) => resource.visibility !== "hidden")) return false;
      if (visibility === "hidden" && !entry.resources.some((resource) => resource.visibility === "hidden")) return false;
      if (details === "viewable" && !entry.resources.some((resource) => resource.can_view_details)) return false;
      if (details === "restricted" && !entry.resources.some((resource) => !resource.can_view_details)) return false;
      if (filters.hide_restricted_details && entry.resources.every((resource) => !resource.can_view_details)) return false;
      return true;
    });
}

export function buildOntologyResourceHistory(
  records: OntologySavedChangeRecord[],
  registry: OntologyResourceRegistryEntry[] = [],
  resource: { kind: string; id: string },
  filters: Omit<OntologyHistoryFilters, "resource_kind"> = {},
  options: { current_user_id?: string | null; can_view_hidden_details?: boolean } = {},
) {
  const { resource_kind: _ignoredResourceKind, ...resourceFilters } = filters as OntologyHistoryFilters;
  void _ignoredResourceKind;
  return buildOntologyHistory(records, registry, resourceFilters, options)
    .map((entry) => {
      const resources = entry.resources.filter((item) => item.kind === resource.kind && item.id === resource.id);
      const visibleResources = resources.filter((item) => item.can_view_details);
      return {
        ...entry,
        resources,
        visible_resources: visibleResources,
        can_view_details: resources.length > 0 && resources.every((item) => item.can_view_details),
        restricted_details_count: resources.filter((item) => !item.can_view_details).length,
      };
    })
    .filter((entry) => entry.resources.length > 0);
}

export function createOntologyRestoreChange(
  entry: OntologyHistoryEntry,
  resource: OntologyHistoryResourceSummary,
  options: { current_user_id?: string | null; now?: string } = {},
): OntologyStagedChange {
  if (!resource.can_view_details) {
    throw new Error("Cannot restore a resource whose historical details are restricted.");
  }
  const sourceChange = resource.change;
  if (!sourceChange) {
    throw new Error("No historical change payload is available for this resource.");
  }
  const payload = stagedPayload(sourceChange);
  const resourceID = resource.id || ontologyChangeResource(sourceChange).id || "";
  const createdAt = options.now || new Date().toISOString();
  const sourceAuthor = ontologyChangeAuthor(sourceChange, entry.author);
  const restoreChange: OntologyStagedChange = {
    id: `restore:${resource.kind}:${resourceID}:${entry.record.id}:${Date.parse(createdAt) || Date.now()}`,
    kind: resource.kind,
    action: "restore",
    label: `Restore ${resource.label}`,
    description: `Restore ${resource.label} to the version saved on ${entry.saved_at} by ${sourceAuthor}.`,
    payload: {
      ...payload,
      id: resourceID || payload.id,
      restored_from_record_id: entry.record.id,
      restored_from_change_id: sourceChange.id,
      restored_from_saved_at: entry.saved_at,
      restore_kind: "ontology_history_restore",
    },
    warnings: [
      "Restore is staged as an unsaved ontology change. Save ontology changes for it to take effect.",
    ],
    errors: [],
    source: "ontology_history_restore",
    author: options.current_user_id || "local",
    createdAt,
  };
  if (resourceID) restoreChange.targetId = resourceID;
  if (sourceChange.parentRef) restoreChange.parentRef = sourceChange.parentRef;
  return restoreChange;
}

function ontologyHistoryEntry(
  record: OntologySavedChangeRecord,
  registry: OntologyResourceRegistryEntry[],
  options: { current_user_id?: string | null; can_view_hidden_details?: boolean },
): OntologyHistoryEntry {
  const resources = normalizeHistoryResources(record, registry, options);
  const visibleResources = resources.filter((resource) => resource.can_view_details);
  return {
    id: record.id,
    record,
    saved_at: record.saved_at,
    author: record.saved_by,
    status: record.status,
    note: record.note,
    resources,
    visible_resources: visibleResources,
    changes_count: record.changes?.length ?? record.change_ids?.length ?? resources.length,
    can_view_details: resources.length > 0 && resources.every((resource) => resource.can_view_details),
    restricted_details_count: resources.filter((resource) => !resource.can_view_details).length,
  };
}

function normalizeHistoryResources(
  record: OntologySavedChangeRecord,
  registry: OntologyResourceRegistryEntry[],
  options: { current_user_id?: string | null; can_view_hidden_details?: boolean },
): OntologyHistoryResourceSummary[] {
  const byResourceKey = new Map(registry.map((entry) => [`${entry.resource_kind}:${entry.resource_id}`, entry]));
  const changes = Array.isArray(record.changes) ? record.changes : [];
  const resourceRows = Array.isArray(record.resources) && record.resources.length > 0
    ? record.resources
    : changes.map((change) => {
        const resource = ontologyChangeResource(change);
        return { kind: resource.kind, id: resource.id, label: change.label };
      });
  const out: OntologyHistoryResourceSummary[] = [];
  const seen = new Set<string>();
  for (const rawResource of resourceRows) {
    const kind = rawResource.kind || "ontology_resource";
    const id = rawResource.id || null;
    const key = `${kind}:${id || ""}`;
    if (seen.has(key)) continue;
    seen.add(key);
    const registryEntry = id ? byResourceKey.get(`${kind}:${id}`) : undefined;
    const change = changes.find((candidate) => {
      const candidateResource = ontologyChangeResource(candidate);
      return candidateResource.kind === kind && (!id || candidateResource.id === id || candidate.id === id);
    });
    const visibility = registryEntry?.visibility || visibilityFromChange(change) || "normal";
    const ownRecord = Boolean(options.current_user_id && record.saved_by === options.current_user_id);
    const canViewDetails = visibility !== "hidden" || Boolean(options.can_view_hidden_details) || ownRecord;
    out.push({
      kind,
      id,
      label: rawResource.label || change?.label || registryEntry?.display_name || [kind, id].filter(Boolean).join(":"),
      visibility,
      can_view_details: canViewDetails,
      registry_entry: registryEntry,
      change,
    });
  }
  return out;
}

function visibilityFromChange(change?: OntologyStagedChange) {
  if (!change) return "";
  const payload = stagedPayload(change);
  return typeof payload.visibility === "string" ? payload.visibility : "";
}

export function buildOntologyBundle(input: {
  ontology: OntologyArtifact;
  registry: OntologyResourceRegistryEntry[];
  selectedResourceKeys: string[];
  objectTypes?: ObjectType[];
  linkTypes?: LinkType[];
  actionTypes?: ActionType[];
  interfaces?: OntologyInterface[];
  sharedPropertyTypes?: SharedPropertyType[];
  valueTypes?: OntologyValueType[];
  objectTypeGroups?: OntologyObjectTypeGroup[];
  objectViews?: ObjectViewDefinition[];
  workingState?: OntologyProjectWorkingState | null;
  exportedBy?: string | null;
  now?: string;
}): OntologyBundle {
  const selected = new Set(input.selectedResourceKeys);
  const resourceMap = ontologyBundlePayloadMap(input);
  const resources = input.registry
    .filter((entry) => selected.has(ontologyResourceKey(entry.resource_kind, entry.resource_id)))
    .map((entry) => {
      const payload = clonePlainObject(resourceMap.get(ontologyResourceKey(entry.resource_kind, entry.resource_id)) || {
        id: entry.resource_id,
        name: entry.api_name,
        display_name: entry.display_name,
      });
      return ontologyBundleResourceFromRegistry(entry, payload);
    });
  for (const valueType of input.valueTypes || []) {
    const key = ontologyResourceKey("value_type", valueType.id);
    if (!selected.has(key)) continue;
    resources.push({
      kind: "value_type",
      id: valueType.id,
      api_name: valueType.name,
      display_name: valueType.display_name,
      action: "upsert",
      visibility: valueType.status === "deprecated" ? "hidden" : "normal",
      payload: clonePlainObject(valueType as unknown as Record<string, unknown>),
      dependencies: [],
    });
  }
  const exportedAt = input.now || new Date().toISOString();
  return {
    schema_version: ONTOLOGY_BUNDLE_SCHEMA_VERSION,
    bundle_id: `ontology-bundle:${input.ontology.id}:${Date.parse(exportedAt) || Date.now()}`,
    exported_at: exportedAt,
    exported_by: input.exportedBy || null,
    ontology: {
      id: input.ontology.id,
      api_name: input.ontology.api_name,
      display_name: input.ontology.display_name,
      owning_space_slug: input.ontology.owning_space_slug,
    },
    resources,
    working_state: input.workingState?.changes || [],
    metadata: {
      resource_count: resources.length,
      includes_working_state: Boolean(input.workingState?.changes?.length),
      generated_by: "openfoundry",
    },
  };
}

export function parseOntologyBundleJSON(text: string): OntologyBundle {
  const parsed = JSON.parse(text) as OntologyBundle;
  if (!parsed || typeof parsed !== "object") {
    throw new Error("Ontology bundle JSON must be an object.");
  }
  return parsed;
}

export function validateOntologyBundle(
  bundle: OntologyBundle,
  context: {
    ontology: OntologyArtifact;
    registry: OntologyResourceRegistryEntry[];
    valueTypes?: OntologyValueType[];
    workingState?: OntologyProjectWorkingState | null;
    currentUserId?: string | null;
  },
): OntologyBundleValidationResult {
  const issues: OntologyBundleValidationIssue[] = [];
  const resources = Array.isArray(bundle.resources) ? bundle.resources : [];
  if (bundle.schema_version !== ONTOLOGY_BUNDLE_SCHEMA_VERSION) {
    issues.push({ severity: "error", code: "unsupported_schema_version", message: `Unsupported bundle schema ${String(bundle.schema_version || "unknown")}.` });
  }
  if (resources.length === 0) {
    issues.push({ severity: "error", code: "empty_bundle", message: "Bundle must include at least one ontology resource." });
  }
  if (bundle.ontology?.id && bundle.ontology.id !== context.ontology.id && bundleHasConditionalFormatting(resources)) {
    issues.push({ severity: "error", code: "conditional_formatting_scope", message: "Conditional formatting rule metadata cannot be imported into a different ontology." });
  }

  const existingByName = new Map<string, OntologyResourceRegistryEntry>();
  const existingValueTypeNames = new Map<string, string>();
  const existingByID = new Set<string>();
  for (const entry of context.registry) {
    existingByName.set(ontologyNameKey(entry.resource_kind, entry.api_name), entry);
    existingByID.add(ontologyResourceKey(entry.resource_kind, entry.resource_id));
  }
  for (const valueType of context.valueTypes || []) {
    existingByID.add(ontologyResourceKey("value_type", valueType.id));
    existingValueTypeNames.set(ontologyNameKey("value_type", valueType.name), valueType.id);
  }
  const bundleNameToID = new Map<string, string>();
  const bundleResourceKeys = new Set(resources.map((resource) => ontologyResourceKey(resource.kind, resource.id)));
  for (const resource of resources) {
    const resourceKey = ontologyResourceKey(resource.kind, resource.id);
    const nameKey = ontologyNameKey(resource.kind, resource.api_name);
    const previousBundleID = bundleNameToID.get(nameKey);
    if (previousBundleID && previousBundleID !== resource.id) {
      issues.push({ severity: "error", code: "duplicate_bundle_api_name", message: `${resourceKindLabelForBundle(resource.kind)} API name ${resource.api_name} appears more than once in this bundle.`, resource_key: resourceKey });
    }
    bundleNameToID.set(nameKey, resource.id);
    const existing = existingByName.get(nameKey);
    if (existing && existing.resource_id !== resource.id) {
      issues.push({ severity: "error", code: "api_name_conflict", message: `${resourceKindLabelForBundle(resource.kind)} API name ${resource.api_name} already belongs to ${existing.display_name}.`, resource_key: resourceKey });
    }
    const existingValueTypeID = existingValueTypeNames.get(nameKey);
    if (resource.kind === "value_type" && existingValueTypeID && existingValueTypeID !== resource.id) {
      issues.push({ severity: "error", code: "api_name_conflict", message: `Value Type API name ${resource.api_name} already belongs to another value type.`, resource_key: resourceKey });
    }
    if (!API_NAME_RE.test(resource.api_name || "")) {
      issues.push({ severity: "error", code: "invalid_api_name", message: `${resource.display_name || resource.id} has an invalid API name.`, resource_key: resourceKey });
    }
    if (hasUnsupportedPrivateFields(resource.payload)) {
      issues.push({ severity: "error", code: "unsupported_private_fields", message: `${resource.display_name || resource.id} contains unsupported private fields.`, resource_key: resourceKey });
    }
    if (resource.action === "delete") {
      const existingResource = context.registry.find((entry) => entry.resource_kind === resource.kind && entry.resource_id === resource.id);
      if (!existingResource) {
        issues.push({ severity: "warning", code: "delete_missing_resource", message: `${resource.display_name || resource.id} is marked for delete but does not exist in this ontology.`, resource_key: resourceKey });
      } else if (existingResource.usage_count > 0 || existingResource.linked_resource_count > 0) {
        issues.push({ severity: "error", code: "unsafe_delete", message: `${existingResource.display_name} is used by ${existingResource.usage_count} resources and has ${existingResource.linked_resource_count} linked resources.`, resource_key: resourceKey });
      }
    }
    for (const dependency of ontologyBundleDependencies(resource)) {
      const dependencyKey = ontologyResourceKey(dependency.kind, dependency.id);
      if (existingByID.has(dependencyKey) || bundleResourceKeys.has(dependencyKey)) continue;
      issues.push({ severity: "error", code: "missing_dependency", message: `${resource.display_name || resource.id} depends on missing ${resourceKindLabelForBundle(dependency.kind)} ${dependency.id}.`, resource_key: resourceKey });
    }
    if (resource.kind === "action_type") {
      if (!hasBundlePermissionRequirement(resource.payload)) {
        issues.push({ severity: "warning", code: "missing_permission_requirement", message: `${resource.display_name || resource.id} should declare a permission key or authorization policy before save.`, resource_key: resourceKey });
      }
    }
  }

  const stagedChanges = ontologyBundleToStagedChanges(bundle, {
    currentUserId: context.currentUserId,
  });
  const errors = issues.filter((issue) => issue.severity === "error").length;
  const warnings = issues.filter((issue) => issue.severity === "warning").length;
  return {
    valid: errors === 0,
    errors,
    warnings,
    issues,
    resources,
    staged_changes: stagedChanges,
  };
}

export function ontologyBundleToStagedChanges(
  bundle: Pick<OntologyBundle, "bundle_id" | "exported_at" | "resources">,
  options: { currentUserId?: string | null; now?: string } = {},
): OntologyStagedChange[] {
  const createdAt = options.now || new Date().toISOString();
  return (bundle.resources || []).map((resource, index) => ({
    id: `import:${resource.kind}:${resource.id}:${bundle.bundle_id}:${index}`,
    kind: stagedChangeKindForBundleResource(resource.kind),
    action: resource.action === "delete" ? "delete" : "import",
    label: `${resource.action === "delete" ? "Delete" : "Import"} ${resource.display_name || resource.api_name || resource.id}`,
    description: `Imported from ontology bundle ${bundle.bundle_id}.`,
    targetId: resource.id,
    payload: {
      ...resource.payload,
      id: resource.id,
      imported_from_bundle_id: bundle.bundle_id,
      imported_from_exported_at: bundle.exported_at,
      imported_resource_kind: resource.kind,
    },
    warnings: resource.action === "delete"
      ? ["Imported delete must be reviewed carefully before save."]
      : ["Imported bundle resource is staged as an unsaved ontology change."],
    errors: [],
    source: "ontology_bundle_import",
    author: options.currentUserId || "local",
    createdAt,
  }));
}

function ontologyBundlePayloadMap(input: {
  objectTypes?: ObjectType[];
  linkTypes?: LinkType[];
  actionTypes?: ActionType[];
  interfaces?: OntologyInterface[];
  sharedPropertyTypes?: SharedPropertyType[];
  objectTypeGroups?: OntologyObjectTypeGroup[];
  objectViews?: ObjectViewDefinition[];
}) {
  const map = new Map<string, Record<string, unknown>>();
  for (const objectType of input.objectTypes || []) map.set(ontologyResourceKey("object_type", objectType.id), objectType as unknown as Record<string, unknown>);
  for (const linkType of input.linkTypes || []) map.set(ontologyResourceKey("link_type", linkType.id), linkType as unknown as Record<string, unknown>);
  for (const actionType of input.actionTypes || []) map.set(ontologyResourceKey("action_type", actionType.id), actionType as unknown as Record<string, unknown>);
  for (const iface of input.interfaces || []) map.set(ontologyResourceKey("interface", iface.id), iface as unknown as Record<string, unknown>);
  for (const shared of input.sharedPropertyTypes || []) map.set(ontologyResourceKey("shared_property_type", shared.id), shared as unknown as Record<string, unknown>);
  for (const group of input.objectTypeGroups || []) map.set(ontologyResourceKey("object_type_group", group.id), group as unknown as Record<string, unknown>);
  for (const view of input.objectViews || []) {
    const kind = view.mode === "standard" ? "core_object_view" : "custom_object_view";
    map.set(ontologyResourceKey(kind, view.id), view as unknown as Record<string, unknown>);
  }
  return map;
}

function ontologyBundleResourceFromRegistry(
  entry: OntologyResourceRegistryEntry,
  payload: Record<string, unknown>,
): OntologyBundleResource {
  return {
    kind: entry.resource_kind,
    id: entry.resource_id,
    api_name: entry.api_name,
    display_name: entry.display_name,
    action: "upsert",
    visibility: entry.visibility,
    payload,
    dependencies: ontologyBundleDependencies({ kind: entry.resource_kind, payload }),
  };
}

function ontologyBundleDependencies(resource: Pick<OntologyBundleResource, "kind" | "payload">): OntologyBundleDependency[] {
  const payload = resource.payload || {};
  const dependencies: OntologyBundleDependency[] = [];
  const add = (kind: OntologyBundleResourceKind, value: unknown, required = true) => {
    if (typeof value === "string" && value.trim()) dependencies.push({ kind, id: value, required });
  };
  if (resource.kind === "link_type") {
    add("object_type", payload.source_type_id || payload.source_object_type_id);
    add("object_type", payload.target_type_id || payload.target_object_type_id);
  }
  if (resource.kind === "action_type") {
    add("object_type", payload.object_type_id, false);
    add("interface", payload.interface_id, false);
  }
  if (resource.kind === "core_object_view" || resource.kind === "custom_object_view") {
    add("object_type", payload.object_type_id);
  }
  if (resource.kind === "object_type_group" && Array.isArray(payload.object_type_ids)) {
    for (const objectTypeID of payload.object_type_ids) add("object_type", objectTypeID, false);
  }
  if (resource.kind === "datasource_registration") {
    const [objectTypeID] = String(payload.id || "").split(":");
    add("object_type", payload.object_type_id || objectTypeID);
  }
  if (resource.kind === "object_type" && Array.isArray(payload.properties)) {
    for (const property of payload.properties) {
      if (!property || typeof property !== "object") continue;
      const typed = property as Record<string, unknown>;
      add("shared_property_type", typed.shared_property_type_id, false);
      add("value_type", typed.value_type_id, false);
    }
  }
  if (resource.kind === "shared_property_type") {
    add("value_type", payload.value_type_id, false);
  }
  return dedupeBundleDependencies(dependencies);
}

function dedupeBundleDependencies(dependencies: OntologyBundleDependency[]) {
  const seen = new Set<string>();
  return dependencies.filter((dependency) => {
    const key = ontologyResourceKey(dependency.kind, dependency.id);
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

function bundleHasConditionalFormatting(resources: OntologyBundleResource[]) {
  return resources.some((resource) => JSON.stringify(resource.payload || {}).includes("conditional_formatting"));
}

function hasUnsupportedPrivateFields(value: unknown): boolean {
  if (!value || typeof value !== "object") return false;
  if (Array.isArray(value)) return value.some(hasUnsupportedPrivateFields);
  for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
    const normalized = key.toLowerCase();
    if (
      normalized.startsWith("_") ||
      normalized.startsWith("internal_") ||
      normalized.startsWith("private_") ||
      normalized.startsWith("palantir_") ||
      normalized === "tenant_rid"
    ) {
      return true;
    }
    if (hasUnsupportedPrivateFields(child)) return true;
  }
  return false;
}

function hasBundlePermissionRequirement(payload: Record<string, unknown>) {
  if (typeof payload.permission_key === "string" && payload.permission_key.trim()) {
    return true;
  }
  const policy = payload.authorization_policy;
  if (!policy || typeof policy !== "object") return false;
  return Object.values(policy as Record<string, unknown>).some((value) => {
    if (Array.isArray(value)) return value.length > 0;
    if (value && typeof value === "object") return Object.keys(value).length > 0;
    return value !== null && value !== undefined && value !== false && value !== "";
  });
}

function clonePlainObject(value: Record<string, unknown>) {
  return JSON.parse(JSON.stringify(value)) as Record<string, unknown>;
}

function stagedChangeKindForBundleResource(kind: OntologyBundleResourceKind) {
  if (kind === "core_object_view" || kind === "custom_object_view") return "object_view";
  if (kind === "datasource_registration") return "object_type_binding";
  return kind;
}

export function ontologyResourceKey(kind: string, id: string) {
  return `${String(kind || "unknown")}:${String(id || "")}`;
}

function ontologyNameKey(kind: string, apiName: string) {
  return `${String(kind || "unknown")}:${String(apiName || "").trim().toLowerCase()}`;
}

function resourceKindLabelForBundle(kind: string) {
  return kind
    .split("_")
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

export type OntologyUsageProduct =
  | "workshop"
  | "functions"
  | "pipeline_builder"
  | "object_explorer"
  | "saved_exploration"
  | "global_branching"
  | "marketplace"
  | "object_views";

export type OntologyUsageResourceKind =
  | "object_type"
  | "property"
  | "link_type"
  | "interface"
  | "action_type"
  | "object_view"
  | "shared_property_type"
  | "value_type";

export interface OntologyUsageExternalSource {
  product: OntologyUsageProduct;
  product_label?: string;
  consumer_id: string;
  consumer_label: string;
  consumer_kind: string;
  surface?: string;
  detail?: string;
  payload: unknown;
  last_used_at?: string | null;
  actor?: string | null;
  read_count?: number;
  write_count?: number;
  active_users?: number;
}

export interface OntologyUsageReference {
  id: string;
  product: OntologyUsageProduct;
  product_label: string;
  consumer_id: string;
  consumer_label: string;
  consumer_kind: string;
  surface: string;
  detail: string;
  resource_kind: OntologyUsageResourceKind;
  resource_id: string;
  resource_label: string;
  read_count: number;
  write_count: number;
  active_users: number;
  last_used_at: string | null;
}

export interface OntologyUsageResourceSummary {
  resource_key: string;
  resource_kind: OntologyUsageResourceKind;
  resource_id: string;
  resource_label: string;
  references: OntologyUsageReference[];
  products: OntologyUsageProduct[];
  read_count: number;
  write_count: number;
  interactions: number;
  active_users: number;
  last_used_at: string | null;
  risk_level: "none" | "low" | "medium" | "high";
}

export interface OntologyUsageImpactWarning {
  severity: "warning" | "error";
  code: string;
  message: string;
  change_id: string;
  resource_kind: OntologyUsageResourceKind;
  resource_id: string;
  resource_label: string;
  affected_reference_count: number;
  affected_products: OntologyUsageProduct[];
}

export interface OntologyUsageImpactAnalysis {
  references: OntologyUsageReference[];
  summaries: OntologyUsageResourceSummary[];
  warnings: OntologyUsageImpactWarning[];
  product_counts: Record<OntologyUsageProduct, number>;
  totals: {
    resources: number;
    references: number;
    warnings: number;
    errors: number;
    reads: number;
    writes: number;
    active_users: number;
  };
}

interface UsageResourceCatalogEntry {
  kind: OntologyUsageResourceKind;
  id: string;
  label: string;
  aliases: string[];
  object_type_id?: string;
  property_name?: string;
}

interface StringToken {
  key: string;
  value: string;
  path: string;
}

export function buildOntologyUsageImpactAnalysis(input: {
  objectTypes?: ObjectType[];
  linkTypes?: LinkType[];
  actionTypes?: ActionType[];
  interfaces?: OntologyInterface[];
  sharedPropertyTypes?: SharedPropertyType[];
  valueTypes?: OntologyValueType[];
  objectViews?: ObjectViewDefinition[];
  workingChanges?: OntologyStagedChange[];
  externalSources?: OntologyUsageExternalSource[];
}): OntologyUsageImpactAnalysis {
  const catalog = buildUsageResourceCatalog(input);
  const references = dedupeUsageReferences([
    ...buildObjectViewUsageReferences(catalog, input.objectViews || [], input.actionTypes || []),
    ...buildFunctionBackedActionReferences(catalog, input.actionTypes || []),
    ...(input.externalSources || []).flatMap((source) => usageReferencesFromExternalSource(source, catalog)),
  ]);
  const summaries = buildUsageSummaries(catalog, references);
  const warnings = buildUsageImpactWarnings(input.workingChanges || [], summaries);
  const productCounts = usageProductCounts(references);
  return {
    references,
    summaries,
    warnings,
    product_counts: productCounts,
    totals: {
      resources: summaries.length,
      references: references.length,
      warnings: warnings.filter((warning) => warning.severity === "warning").length,
      errors: warnings.filter((warning) => warning.severity === "error").length,
      reads: references.reduce((sum, reference) => sum + reference.read_count, 0),
      writes: references.reduce((sum, reference) => sum + reference.write_count, 0),
      active_users: summaries.reduce((sum, summary) => sum + summary.active_users, 0),
    },
  };
}

function buildUsageResourceCatalog(input: {
  objectTypes?: ObjectType[];
  linkTypes?: LinkType[];
  actionTypes?: ActionType[];
  interfaces?: OntologyInterface[];
  sharedPropertyTypes?: SharedPropertyType[];
  valueTypes?: OntologyValueType[];
  objectViews?: ObjectViewDefinition[];
}): UsageResourceCatalogEntry[] {
  const entries: UsageResourceCatalogEntry[] = [];
  const add = (entry: UsageResourceCatalogEntry) => {
    entries.push({
      ...entry,
      aliases: uniqueNonEmpty([entry.id, entry.label, ...entry.aliases]),
    });
  };
  for (const objectType of input.objectTypes || []) {
    const apiName = objectTypeAPIName(objectType);
    add({
      kind: "object_type",
      id: objectType.id,
      label: objectType.display_name || apiName,
      aliases: [objectType.id, apiName, objectType.display_name, objectTypeRID(objectType), `object_type:${objectType.id}`].filter(Boolean) as string[],
    });
    for (const property of objectType.properties || []) {
      add({
        kind: "property",
        id: `${objectType.id}.${property.name}`,
        label: `${objectType.display_name || apiName}.${property.display_name || property.name}`,
        object_type_id: objectType.id,
        property_name: property.name,
        aliases: [
          `${objectType.id}.${property.name}`,
          `${apiName}.${property.name}`,
          property.name,
          property.display_name,
          property.id,
        ].filter(Boolean) as string[],
      });
    }
  }
  for (const linkType of input.linkTypes || []) {
    add({
      kind: "link_type",
      id: linkType.id,
      label: linkType.display_name || linkType.name,
      aliases: [linkType.id, linkType.name, linkType.display_name, `link_type:${linkType.id}`, `ri.ontology.main.link-type.${linkType.id}`].filter(Boolean) as string[],
    });
  }
  for (const actionType of input.actionTypes || []) {
    add({
      kind: "action_type",
      id: actionType.id,
      label: actionType.display_name || actionType.name,
      aliases: [actionType.id, actionType.name, actionType.display_name, `action_type:${actionType.id}`, `ri.ontology.main.action-type.${actionType.id}`].filter(Boolean) as string[],
    });
  }
  for (const iface of input.interfaces || []) {
    add({
      kind: "interface",
      id: iface.id,
      label: iface.display_name || iface.name,
      aliases: [iface.id, iface.name, iface.display_name, `interface:${iface.id}`, `ri.ontology.main.interface.${iface.id}`].filter(Boolean) as string[],
    });
  }
  for (const shared of input.sharedPropertyTypes || []) {
    add({
      kind: "shared_property_type",
      id: shared.id,
      label: shared.display_name || shared.name,
      aliases: [shared.id, shared.name, shared.display_name, `shared_property_type:${shared.id}`].filter(Boolean) as string[],
    });
  }
  for (const valueType of input.valueTypes || []) {
    add({
      kind: "value_type",
      id: valueType.id,
      label: valueType.display_name || valueType.name,
      aliases: [valueType.id, valueType.name, valueType.display_name, valueType.semantic_type, `value_type:${valueType.id}`].filter(Boolean) as string[],
    });
  }
  for (const view of input.objectViews || []) {
    add({
      kind: "object_view",
      id: view.id,
      label: view.display_name || view.name,
      aliases: [view.id, view.name, view.display_name, `object_view:${view.id}`, `ri.ontology.main.object-view.${view.id}`].filter(Boolean) as string[],
    });
  }
  return entries;
}

function buildObjectViewUsageReferences(
  catalog: UsageResourceCatalogEntry[],
  views: ObjectViewDefinition[],
  actions: ActionType[],
): OntologyUsageReference[] {
  const refs: OntologyUsageReference[] = [];
  for (const view of views) {
    const viewLabel = view.display_name || view.name;
    const source: Omit<OntologyUsageReference, "id" | "resource_kind" | "resource_id" | "resource_label"> = {
      product: "object_views",
      product_label: usageProductLabel("object_views"),
      consumer_id: view.id,
      consumer_label: viewLabel,
      consumer_kind: `${view.mode} ${view.form_factor} Object View`,
      surface: view.form_factor,
      detail: view.config?.metadata?.generated ? "Core Object View generated from ontology metadata." : "Configured Object View definition.",
      read_count: view.published === false ? 0 : 1,
      write_count: 0,
      active_users: view.owner_id || view.created_by ? 1 : 0,
      last_used_at: view.updated_at || view.created_at || null,
    };
    pushUsageRef(refs, source, catalog, "object_view", view.id);
    pushUsageRef(refs, source, catalog, "object_type", view.object_type_id);
    for (const propertyName of objectViewPropertyNames(view)) {
      pushUsageRef(refs, source, catalog, "property", `${view.object_type_id}.${propertyName}`);
    }
    for (const linkTypeID of view.config?.metadata?.link_type_ids || []) {
      pushUsageRef(refs, source, catalog, "link_type", linkTypeID);
    }
    if (view.config?.sections?.some((section) => section.kind === "actions")) {
      for (const action of actions.filter((candidate) => candidate.object_type_id === view.object_type_id)) {
        pushUsageRef(refs, source, catalog, "action_type", action.id);
      }
    }
  }
  return refs;
}

function buildFunctionBackedActionReferences(
  catalog: UsageResourceCatalogEntry[],
  actions: ActionType[],
): OntologyUsageReference[] {
  const refs: OntologyUsageReference[] = [];
  for (const action of actions) {
    const configText = JSON.stringify(action.config || {});
    const functionBacked =
      action.operation_kind === "invoke_function" ||
      /function_(rid|id|package)|functionRid|functionPackage/i.test(configText);
    if (!functionBacked) continue;
    const source = {
      product: "functions" as const,
      product_label: usageProductLabel("functions"),
      consumer_id: action.id,
      consumer_label: action.display_name || action.name,
      consumer_kind: "Function-backed Action",
      surface: "Action type implementation",
      detail: "Action delegates execution to a Function-backed implementation.",
      read_count: 0,
      write_count: 1,
      active_users: action.owner_id ? 1 : 0,
      last_used_at: action.updated_at || action.created_at || null,
    };
    pushUsageRef(refs, source, catalog, "action_type", action.id);
    pushUsageRef(refs, source, catalog, "object_type", action.object_type_id);
    if (action.interface_id) pushUsageRef(refs, source, catalog, "interface", action.interface_id);
  }
  return refs;
}

function objectViewPropertyNames(view: ObjectViewDefinition) {
  return uniqueNonEmpty([
    view.config?.subtitle_property || "",
    ...(view.config?.prominent_properties || []),
    ...(view.config?.panel_properties || []),
    ...(view.config?.metadata?.normal_properties || []),
    view.config?.metadata?.title_property || "",
    view.config?.metadata?.primary_key_property || "",
  ]);
}

function usageReferencesFromExternalSource(
  source: OntologyUsageExternalSource,
  catalog: UsageResourceCatalogEntry[],
): OntologyUsageReference[] {
  const refs: OntologyUsageReference[] = [];
  const tokens = extractStringTokens(source.payload);
  const matchedObjectTypeIDs = new Set(
    catalog
      .filter((entry) => entry.kind === "object_type" && tokens.some((token) => usageTokenMatchesEntry(token, entry)))
      .map((entry) => entry.id),
  );
  for (const entry of catalog) {
    const matchingTokens = tokens.filter((token) => {
      if (entry.kind === "property") {
        if (!isPropertyReferenceKey(token.key) && !isPropertyReferencePath(token.path)) return false;
        if (entry.object_type_id && matchedObjectTypeIDs.size > 0 && !matchedObjectTypeIDs.has(entry.object_type_id)) return false;
      }
      return usageTokenMatchesEntry(token, entry);
    });
    if (matchingTokens.length === 0) continue;
    refs.push({
      id: usageReferenceID(source.product, source.consumer_id, entry.kind, entry.id),
      product: source.product,
      product_label: source.product_label || usageProductLabel(source.product),
      consumer_id: source.consumer_id,
      consumer_label: source.consumer_label,
      consumer_kind: source.consumer_kind,
      surface: source.surface || source.consumer_kind,
      detail: source.detail || `Matched ${matchingTokens.slice(0, 3).map((token) => token.path).join(", ")}.`,
      resource_kind: entry.kind,
      resource_id: entry.id,
      resource_label: entry.label,
      read_count: source.read_count ?? defaultReadCount(source.product),
      write_count: source.write_count ?? defaultWriteCount(source.product),
      active_users: source.active_users ?? (source.actor ? 1 : 0),
      last_used_at: source.last_used_at || null,
    });
  }
  return refs;
}

function pushUsageRef(
  refs: OntologyUsageReference[],
  source: Omit<OntologyUsageReference, "id" | "resource_kind" | "resource_id" | "resource_label">,
  catalog: UsageResourceCatalogEntry[],
  kind: OntologyUsageResourceKind,
  id: string | null | undefined,
) {
  if (!id) return;
  const entry = catalog.find((candidate) => candidate.kind === kind && candidate.id === id);
  if (!entry) return;
  refs.push({
    ...source,
    id: usageReferenceID(source.product, source.consumer_id, kind, id),
    resource_kind: kind,
    resource_id: id,
    resource_label: entry.label,
  });
}

function extractStringTokens(value: unknown, path = "$", key = "", depth = 0): StringToken[] {
  if (depth > 9) return [];
  if (typeof value === "string") return [{ key, value, path }];
  if (typeof value === "number" || typeof value === "boolean") return [{ key, value: String(value), path }];
  if (!value || typeof value !== "object") return [];
  if (Array.isArray(value)) {
    return value.flatMap((item, index) => extractStringTokens(item, `${path}[${index}]`, key, depth + 1));
  }
  return Object.entries(value as Record<string, unknown>).flatMap(([childKey, child]) =>
    extractStringTokens(child, `${path}.${childKey}`, childKey, depth + 1),
  );
}

function usageTokenMatchesEntry(token: StringToken, entry: UsageResourceCatalogEntry) {
  return entry.aliases.some((alias) => usageValueMatchesAlias(token.value, alias));
}

function usageValueMatchesAlias(value: string, alias: string) {
  const left = normalizeUsageValue(value);
  const right = normalizeUsageValue(alias);
  if (!left || !right) return false;
  return left === right || left.endsWith(`:${right}`) || left.endsWith(`/${right}`) || left.endsWith(`.${right}`);
}

function normalizeUsageValue(value: string) {
  return value.trim().toLowerCase();
}

function isPropertyReferenceKey(key: string) {
  const normalized = key.toLowerCase();
  return (
    normalized === "field" ||
    normalized.includes("property") ||
    normalized.includes("column") ||
    normalized.includes("projection") ||
    normalized.includes("sort") ||
    normalized.includes("filter") ||
    normalized.includes("group")
  );
}

function isPropertyReferencePath(path: string) {
  const normalized = path.toLowerCase();
  return normalized.includes("properties") || normalized.includes("projections") || normalized.includes("filters");
}

function dedupeUsageReferences(references: OntologyUsageReference[]) {
  const seen = new Set<string>();
  return references.filter((reference) => {
    if (seen.has(reference.id)) return false;
    seen.add(reference.id);
    return true;
  });
}

function buildUsageSummaries(
  catalog: UsageResourceCatalogEntry[],
  references: OntologyUsageReference[],
): OntologyUsageResourceSummary[] {
  return catalog
    .map((entry) => {
      const resourceReferences = references.filter(
        (reference) => reference.resource_kind === entry.kind && reference.resource_id === entry.id,
      );
      const products = uniqueUsageProducts(resourceReferences.map((reference) => reference.product));
      const lastUsedAt = latestUsageTimestamp(resourceReferences.map((reference) => reference.last_used_at));
      const readCount = resourceReferences.reduce((sum, reference) => sum + reference.read_count, 0);
      const writeCount = resourceReferences.reduce((sum, reference) => sum + reference.write_count, 0);
      const activeUsers = resourceReferences.reduce((sum, reference) => sum + reference.active_users, 0);
      return {
        resource_key: ontologyResourceKey(entry.kind, entry.id),
        resource_kind: entry.kind,
        resource_id: entry.id,
        resource_label: entry.label,
        references: resourceReferences,
        products,
        read_count: readCount,
        write_count: writeCount,
        interactions: readCount + writeCount,
        active_users: activeUsers,
        last_used_at: lastUsedAt,
        risk_level: usageRiskLevel(resourceReferences.length, products.length, writeCount),
      };
    })
    .filter((summary) => summary.references.length > 0)
    .sort((left, right) => {
      const riskRank = { high: 3, medium: 2, low: 1, none: 0 };
      return riskRank[right.risk_level] - riskRank[left.risk_level] || right.references.length - left.references.length;
    });
}

function buildUsageImpactWarnings(
  changes: OntologyStagedChange[],
  summaries: OntologyUsageResourceSummary[],
): OntologyUsageImpactWarning[] {
  const summaryByKey = new Map(summaries.map((summary) => [summary.resource_key, summary]));
  const warnings: OntologyUsageImpactWarning[] = [];
  for (const change of changes) {
    for (const target of usageTargetsForChange(change)) {
      const summary = summaryByKey.get(ontologyResourceKey(target.kind, target.id));
      if (!summary) continue;
      const destructive = isDestructiveOntologyChange(change);
      warnings.push({
        severity: destructive || summary.risk_level === "high" ? "error" : "warning",
        code: destructive ? "breaking_downstream_usage" : "downstream_usage_review",
        message: `${change.label || change.action} touches ${summary.resource_label}, which is used by ${summary.references.length} downstream reference${summary.references.length === 1 ? "" : "s"} across ${summary.products.map(usageProductLabel).join(", ")}.`,
        change_id: change.id,
        resource_kind: target.kind,
        resource_id: target.id,
        resource_label: summary.resource_label,
        affected_reference_count: summary.references.length,
        affected_products: summary.products,
      });
    }
  }
  return warnings;
}

function usageTargetsForChange(change: OntologyStagedChange): Array<{ kind: OntologyUsageResourceKind; id: string }> {
  const resource = ontologyChangeResource(change);
  const payload = stagedPayload(change);
  const targets: Array<{ kind: OntologyUsageResourceKind; id: string }> = [];
  const add = (kind: OntologyUsageResourceKind, id: unknown) => {
    if (typeof id !== "string" || !id.trim()) return;
    const candidate = { kind, id: id.trim() };
    if (!targets.some((target) => target.kind === candidate.kind && target.id === candidate.id)) targets.push(candidate);
  };
  switch (resource.kind) {
    case "object_type":
      add("object_type", resource.id || payload.object_type_id);
      break;
    case "property":
      add("property", resource.id || propertyTargetIDFromPayload(payload, change.parentRef?.id));
      break;
    case "link_type":
      add("link_type", resource.id);
      break;
    case "interface":
      add("interface", resource.id);
      break;
    case "action_type":
      add("action_type", resource.id);
      break;
    case "object_view":
      add("object_view", resource.id);
      break;
    default:
      if (typeof resource.id === "string") add(resource.kind as OntologyUsageResourceKind, resource.id);
  }
  add("object_type", payload.object_type_id);
  add("interface", payload.interface_id);
  add("link_type", payload.link_type_id);
  add("action_type", payload.action_type_id);
  const objectTypeID = typeof payload.object_type_id === "string" ? payload.object_type_id : change.parentRef?.kind === "object_type" ? change.parentRef.id : null;
  for (const propertyName of propertyNamesFromChangePayload(payload)) {
    if (objectTypeID) add("property", `${objectTypeID}.${propertyName}`);
  }
  return targets;
}

function propertyTargetIDFromPayload(payload: Record<string, unknown>, parentObjectTypeID?: string) {
  const propertyName = payload.name || payload.property_name || payload.target_property;
  if (typeof parentObjectTypeID === "string" && typeof propertyName === "string") return `${parentObjectTypeID}.${propertyName}`;
  return typeof propertyName === "string" ? propertyName : null;
}

function propertyNamesFromChangePayload(payload: Record<string, unknown>) {
  const names: string[] = [];
  for (const key of ["property_name", "target_property", "primary_key_property", "title_property"]) {
    if (typeof payload[key] === "string") names.push(payload[key] as string);
  }
  for (const key of ["removed_property_names", "deleted_property_names", "renamed_property_names", "property_names"]) {
    if (Array.isArray(payload[key])) {
      for (const value of payload[key]) if (typeof value === "string") names.push(value);
    }
  }
  if (Array.isArray(payload.property_mapping)) {
    for (const mapping of payload.property_mapping) {
      if (mapping && typeof mapping === "object") {
        const target = (mapping as Record<string, unknown>).target_property;
        if (typeof target === "string") names.push(target);
      }
    }
  }
  return uniqueNonEmpty(names);
}

function isDestructiveOntologyChange(change: OntologyStagedChange) {
  const payload = stagedPayload(change);
  if (["delete", "remove", "deprecate"].includes(String(change.action || "").toLowerCase())) return true;
  return ["removed_property_names", "deleted_property_names", "primary_key_property", "primary_key"].some((key) => payload[key] !== undefined);
}

function usageRiskLevel(referenceCount: number, productCount: number, writeCount: number): OntologyUsageResourceSummary["risk_level"] {
  if (referenceCount === 0) return "none";
  if (writeCount > 0 || productCount >= 4 || referenceCount >= 8) return "high";
  if (productCount >= 2 || referenceCount >= 3) return "medium";
  return "low";
}

function usageProductCounts(references: OntologyUsageReference[]): Record<OntologyUsageProduct, number> {
  const counts = {
    workshop: 0,
    functions: 0,
    pipeline_builder: 0,
    object_explorer: 0,
    saved_exploration: 0,
    global_branching: 0,
    marketplace: 0,
    object_views: 0,
  } satisfies Record<OntologyUsageProduct, number>;
  for (const product of uniqueUsageProducts(references.map((reference) => reference.product))) {
    counts[product] = references.filter((reference) => reference.product === product).length;
  }
  return counts;
}

function uniqueUsageProducts(products: OntologyUsageProduct[]) {
  return [...new Set(products)];
}

function latestUsageTimestamp(values: Array<string | null | undefined>) {
  let best: string | null = null;
  let bestTime = -Infinity;
  for (const value of values) {
    if (!value) continue;
    const time = Date.parse(value);
    if (Number.isNaN(time) || time <= bestTime) continue;
    best = value;
    bestTime = time;
  }
  return best;
}

function usageReferenceID(product: OntologyUsageProduct, consumerID: string, kind: OntologyUsageResourceKind, id: string) {
  return `${product}:${consumerID}:${kind}:${id}`;
}

export function usageProductLabel(product: OntologyUsageProduct) {
  switch (product) {
    case "workshop":
      return "Workshop";
    case "functions":
      return "Functions";
    case "pipeline_builder":
      return "Pipeline Builder";
    case "object_explorer":
      return "Object Explorer";
    case "saved_exploration":
      return "Saved explorations";
    case "global_branching":
      return "Global Branching";
    case "marketplace":
      return "Marketplace";
    case "object_views":
      return "Object Views";
  }
}

export type OntologyCleanupCandidateKind =
  | "object_type"
  | "property"
  | "link_type"
  | "interface"
  | "shared_property_type"
  | "value_type"
  | "object_type_group"
  | "object_view"
  | "legacy_object_view_fragment"
  | "workshop_module";

export type OntologyCleanupSeverity = "info" | "warning" | "high";

export interface OntologyCleanupCandidate {
  id: string;
  kind: OntologyCleanupCandidateKind;
  resource_id: string;
  parent_resource_id?: string;
  label: string;
  reason: string;
  severity: OntologyCleanupSeverity;
  usage_count: number;
  reference_summary: string[];
  delete_supported: boolean;
  staged_change_kind: string;
  warnings: string[];
  metadata: Record<string, unknown>;
}

export interface OntologyCleanupAssistant {
  generated_at: string;
  candidates: OntologyCleanupCandidate[];
  counts: Record<OntologyCleanupCandidateKind, number>;
  totals: {
    candidates: number;
    high: number;
    warning: number;
    info: number;
    delete_supported: number;
  };
}

export interface BuildOntologyCleanupAssistantInput {
  objectTypes?: ObjectType[];
  linkTypes?: LinkType[];
  actionTypes?: ActionType[];
  interfaces?: OntologyInterface[];
  sharedPropertyTypes?: SharedPropertyType[];
  valueTypes?: OntologyValueType[];
  objectViews?: ObjectViewDefinition[];
  objectTypeGroups?: OntologyObjectTypeGroup[];
  registry?: OntologyResourceRegistryEntry[];
  usageAnalysis?: OntologyUsageImpactAnalysis;
  workingChanges?: OntologyStagedChange[];
  now?: string;
}

const ONTOLOGY_CLEANUP_KIND_COUNT_KEYS: OntologyCleanupCandidateKind[] = [
  "object_type",
  "property",
  "link_type",
  "interface",
  "shared_property_type",
  "value_type",
  "object_type_group",
  "object_view",
  "legacy_object_view_fragment",
  "workshop_module",
];

function ontologyCleanupReferenceLabel(reference: OntologyUsageReference): string {
  const product = reference.product_label || usageProductLabel(reference.product);
  return `${product}: ${reference.consumer_label}`;
}

function ontologyCleanupSummarizeReferences(
  references: OntologyUsageReference[],
  limit = 4,
): string[] {
  return references.slice(0, limit).map(ontologyCleanupReferenceLabel);
}

function ontologyCleanupChangePending(
  changes: OntologyStagedChange[] | undefined,
  kind: string,
  resourceId: string,
): boolean {
  if (!changes || changes.length === 0) return false;
  return changes.some(
    (change) =>
      change.kind === kind &&
      (change.targetId === resourceId || change.id.includes(resourceId)),
  );
}

export function buildOntologyCleanupAssistant(
  input: BuildOntologyCleanupAssistantInput,
): OntologyCleanupAssistant {
  const now = input.now ?? new Date().toISOString();
  const objectTypes = input.objectTypes ?? [];
  const linkTypes = input.linkTypes ?? [];
  const interfaces = input.interfaces ?? [];
  const sharedPropertyTypes = input.sharedPropertyTypes ?? [];
  const valueTypes = input.valueTypes ?? [];
  const objectViews = input.objectViews ?? [];
  const objectTypeGroups = input.objectTypeGroups ?? [];
  const workingChanges = input.workingChanges ?? [];

  const usageAnalysis = input.usageAnalysis ?? buildOntologyUsageImpactAnalysis({
    objectTypes,
    linkTypes,
    actionTypes: input.actionTypes ?? [],
    interfaces,
    sharedPropertyTypes,
    valueTypes,
    objectViews,
    workingChanges,
  });

  const summariesByKey = new Map<string, OntologyUsageResourceSummary>();
  for (const summary of usageAnalysis.summaries) {
    summariesByKey.set(summary.resource_key, summary);
  }
  const referencesByKey = new Map<string, OntologyUsageReference[]>();
  for (const reference of usageAnalysis.references) {
    const key = ontologyResourceKey(reference.resource_kind, reference.resource_id);
    const list = referencesByKey.get(key) ?? [];
    list.push(reference);
    referencesByKey.set(key, list);
  }

  const candidates: OntologyCleanupCandidate[] = [];

  function usageForResource(kind: OntologyUsageResourceKind, id: string) {
    const key = ontologyResourceKey(kind, id);
    const summary = summariesByKey.get(key) ?? null;
    const references = referencesByKey.get(key) ?? [];
    return { summary, references };
  }

  for (const objectType of objectTypes) {
    const { summary, references } = usageForResource("object_type", objectType.id);
    if ((summary?.references.length ?? 0) > 0) continue;
    if (ontologyCleanupChangePending(workingChanges, "object_type", objectType.id)) continue;
    const linkedLinkTypes = linkTypes.filter(
      (link) => link.source_type_id === objectType.id || link.target_type_id === objectType.id,
    );
    if (linkedLinkTypes.length > 0) continue;
    candidates.push({
      id: `cleanup:object_type:${objectType.id}`,
      kind: "object_type",
      resource_id: objectType.id,
      label: objectType.display_name || objectType.name || objectType.id,
      reason: "No Object Views, actions, or external products reference this object type.",
      severity: "warning",
      usage_count: references.length,
      reference_summary: ontologyCleanupSummarizeReferences(references),
      delete_supported: true,
      staged_change_kind: "object_type",
      warnings: ["Deleting an object type removes its properties, link bindings, and Object Views."],
      metadata: { api_name: objectType.api_name ?? objectType.name },
    });
  }

  for (const objectType of objectTypes) {
    const primaryKey = objectTypePrimaryKey(objectType);
    const titleProperty = objectTypeTitleProperty(objectType);
    for (const property of objectType.properties ?? []) {
      if (property.name === primaryKey) continue;
      if (property.name === titleProperty) continue;
      const propertyId = `${objectType.id}.${property.name}`;
      const { summary, references } = usageForResource("property", propertyId);
      if ((summary?.references.length ?? 0) > 0) continue;
      if (ontologyCleanupChangePending(workingChanges, "property", propertyId)) continue;
      candidates.push({
        id: `cleanup:property:${propertyId}`,
        kind: "property",
        resource_id: propertyId,
        parent_resource_id: objectType.id,
        label: `${objectType.display_name || objectType.name}.${property.display_name || property.name}`,
        reason: "Property is not referenced by any Object View, action, or external product.",
        severity: "info",
        usage_count: references.length,
        reference_summary: ontologyCleanupSummarizeReferences(references),
        delete_supported: true,
        staged_change_kind: "property",
        warnings: [],
        metadata: {
          property_type: property.property_type,
          display_mode: property.display_mode || "normal",
        },
      });
    }
  }

  for (const linkType of linkTypes) {
    const { summary, references } = usageForResource("link_type", linkType.id);
    if ((summary?.references.length ?? 0) > 0) continue;
    if (ontologyCleanupChangePending(workingChanges, "link_type", linkType.id)) continue;
    candidates.push({
      id: `cleanup:link_type:${linkType.id}`,
      kind: "link_type",
      resource_id: linkType.id,
      label: linkType.display_name || linkType.name || linkType.id,
      reason: "Link type has no Object View, action, or external product references.",
      severity: "info",
      usage_count: references.length,
      reference_summary: ontologyCleanupSummarizeReferences(references),
      delete_supported: true,
      staged_change_kind: "link_type",
      warnings: ["Verify that no marketplace or saved exploration relies on this link before deleting."],
      metadata: {
        source_type_id: linkType.source_type_id,
        target_type_id: linkType.target_type_id,
      },
    });
  }

  for (const iface of interfaces) {
    const { summary, references } = usageForResource("interface", iface.id);
    if ((summary?.references.length ?? 0) > 0) continue;
    if (ontologyCleanupChangePending(workingChanges, "interface", iface.id)) continue;
    candidates.push({
      id: `cleanup:interface:${iface.id}`,
      kind: "interface",
      resource_id: iface.id,
      label: iface.display_name || iface.name || iface.id,
      reason: "Interface has no recorded implementations or external references.",
      severity: "info",
      usage_count: references.length,
      reference_summary: ontologyCleanupSummarizeReferences(references),
      delete_supported: true,
      staged_change_kind: "interface",
      warnings: ["Verify no object type implementations rely on this interface before deleting."],
      metadata: {},
    });
  }

  for (const shared of sharedPropertyTypes) {
    const { summary, references } = usageForResource("shared_property_type", shared.id);
    if ((summary?.references.length ?? 0) > 0) continue;
    const consumers = objectTypes.filter((objectType) =>
      (objectType.properties ?? []).some((property) => property.shared_property_type_id === shared.id),
    );
    if (consumers.length > 0) continue;
    if (ontologyCleanupChangePending(workingChanges, "shared_property_type", shared.id)) continue;
    candidates.push({
      id: `cleanup:shared_property_type:${shared.id}`,
      kind: "shared_property_type",
      resource_id: shared.id,
      label: shared.display_name || shared.name || shared.id,
      reason: "Shared property type is not used by any object type or external product.",
      severity: "info",
      usage_count: references.length,
      reference_summary: ontologyCleanupSummarizeReferences(references),
      delete_supported: true,
      staged_change_kind: "shared_property_type",
      warnings: [],
      metadata: {},
    });
  }

  for (const valueType of valueTypes) {
    const { summary, references } = usageForResource("value_type", valueType.id);
    if ((summary?.references.length ?? 0) > 0) continue;
    const consumers = objectTypes.filter((objectType) =>
      (objectType.properties ?? []).some((property) => property.value_type_id === valueType.id),
    );
    if (consumers.length > 0) continue;
    if (ontologyCleanupChangePending(workingChanges, "value_type", valueType.id)) continue;
    candidates.push({
      id: `cleanup:value_type:${valueType.id}`,
      kind: "value_type",
      resource_id: valueType.id,
      label: valueType.display_name || valueType.name || valueType.id,
      reason: "Value type is not bound to any property or external product.",
      severity: "info",
      usage_count: references.length,
      reference_summary: ontologyCleanupSummarizeReferences(references),
      delete_supported: true,
      staged_change_kind: "value_type",
      warnings: [],
      metadata: {},
    });
  }

  for (const group of objectTypeGroups) {
    if ((group.object_type_ids ?? []).length > 0) continue;
    if (ontologyCleanupChangePending(workingChanges, "object_type_group", group.id)) continue;
    candidates.push({
      id: `cleanup:object_type_group:${group.id}`,
      kind: "object_type_group",
      resource_id: group.id,
      label: group.display_name || group.name || group.id,
      reason: "Object type group has no members.",
      severity: "info",
      usage_count: 0,
      reference_summary: [],
      delete_supported: true,
      staged_change_kind: "object_type_group",
      warnings: [],
      metadata: {},
    });
  }

  const visibleObjectTypeIds = new Set(objectTypes.map((type) => type.id));
  for (const view of objectViews) {
    const { references } = usageForResource("object_view", view.id);
    const orphanType = !visibleObjectTypeIds.has(view.object_type_id);
    const isPublished = view.published === true || view.status === "published";
    const isCore = view.mode === "standard" || view.status === "core";
    if (isPublished) continue;
    if (isCore) continue;
    if (ontologyCleanupChangePending(workingChanges, "custom_object_view", view.id)) continue;
    if (orphanType) {
      candidates.push({
        id: `cleanup:object_view:${view.id}`,
        kind: "object_view",
        resource_id: view.id,
        parent_resource_id: view.object_type_id,
        label: view.display_name || view.name || view.id,
        reason: "Object View references an object type that no longer exists in the ontology.",
        severity: "high",
        usage_count: references.length,
        reference_summary: ontologyCleanupSummarizeReferences(references),
        delete_supported: true,
        staged_change_kind: "custom_object_view",
        warnings: ["Deleting an orphan Object View also removes its tabs and embedded Workshop modules."],
        metadata: { form_factor: view.form_factor, missing_object_type_id: view.object_type_id },
      });
      continue;
    }
    if (references.length === 0 && view.mode === "configured") {
      candidates.push({
        id: `cleanup:object_view:${view.id}`,
        kind: "object_view",
        resource_id: view.id,
        parent_resource_id: view.object_type_id,
        label: view.display_name || view.name || view.id,
        reason: "Custom Object View has no recorded consumer references.",
        severity: "info",
        usage_count: 0,
        reference_summary: [],
        delete_supported: true,
        staged_change_kind: "custom_object_view",
        warnings: [],
        metadata: { form_factor: view.form_factor },
      });
    }
  }

  for (const view of objectViews) {
    const metadata = view.config?.metadata;
    if (!metadata) continue;
    if (metadata.legacy_builder === true || metadata.legacy_fields_modified === true) {
      candidates.push({
        id: `cleanup:legacy_object_view_fragment:${view.id}`,
        kind: "legacy_object_view_fragment",
        resource_id: view.id,
        parent_resource_id: view.object_type_id,
        label: view.display_name || view.name || view.id,
        reason: metadata.legacy_builder === true
          ? "Object View still uses the legacy builder and should be migrated to the configured Object View shell."
          : "Object View carries legacy_fields_modified metadata that should be reconciled.",
        severity: "warning",
        usage_count: 0,
        reference_summary: [],
        delete_supported: false,
        staged_change_kind: "custom_object_view",
        warnings: ["Resolve the legacy fields before deleting or migrating."],
        metadata: { form_factor: view.form_factor, ...metadata },
      });
    }
  }

  for (const view of objectViews) {
    const tabs = view.config?.tabs ?? [];
    for (const tab of tabs) {
      const widgetCount = tab.module?.widgets?.length ?? 0;
      const isHidden = tab.visibility === "hidden";
      const isUserManaged = tab.module?.source === "user_managed";
      if (widgetCount === 0 && isUserManaged) {
        candidates.push({
          id: `cleanup:workshop_module:${view.id}:${tab.module.id}`,
          kind: "workshop_module",
          resource_id: tab.module.id,
          parent_resource_id: view.id,
          label: `${view.display_name || view.name}: ${tab.module.display_name || tab.module.name}`,
          reason: "Workshop module has no widgets and is user-managed but produces no rendered content.",
          severity: "info",
          usage_count: 0,
          reference_summary: [],
          delete_supported: false,
          staged_change_kind: "custom_object_view",
          warnings: ["Edit the Object View to delete the tab; cleanup only marks the module for review."],
          metadata: { object_view_id: view.id, tab_id: tab.id, hidden: isHidden },
        });
      } else if (isHidden && isUserManaged && widgetCount === 0) {
        candidates.push({
          id: `cleanup:workshop_module:${view.id}:${tab.module.id}`,
          kind: "workshop_module",
          resource_id: tab.module.id,
          parent_resource_id: view.id,
          label: `${view.display_name || view.name}: ${tab.module.display_name || tab.module.name}`,
          reason: "Hidden Workshop module no longer has widgets attached.",
          severity: "info",
          usage_count: 0,
          reference_summary: [],
          delete_supported: false,
          staged_change_kind: "custom_object_view",
          warnings: [],
          metadata: { object_view_id: view.id, tab_id: tab.id, hidden: true },
        });
      }
    }
  }

  const counts = ONTOLOGY_CLEANUP_KIND_COUNT_KEYS.reduce(
    (acc, kind) => {
      acc[kind] = 0;
      return acc;
    },
    {} as Record<OntologyCleanupCandidateKind, number>,
  );
  let high = 0;
  let warning = 0;
  let info = 0;
  let deleteSupported = 0;
  for (const candidate of candidates) {
    counts[candidate.kind] += 1;
    if (candidate.severity === "high") high += 1;
    else if (candidate.severity === "warning") warning += 1;
    else info += 1;
    if (candidate.delete_supported) deleteSupported += 1;
  }
  return {
    generated_at: now,
    candidates,
    counts,
    totals: {
      candidates: candidates.length,
      high,
      warning,
      info,
      delete_supported: deleteSupported,
    },
  };
}

export interface CreateOntologyCleanupStagedChangesInput {
  candidates: OntologyCleanupCandidate[];
  selectedCandidateIds: string[];
  confirmed: boolean;
  currentUserId?: string | null;
  now?: string;
}

export interface CreateOntologyCleanupStagedChangesResult {
  changes: OntologyStagedChange[];
  skipped: Array<{ candidate: OntologyCleanupCandidate; reason: string }>;
  errors: string[];
  confirmation_required: boolean;
}

export function createOntologyCleanupStagedChanges(
  input: CreateOntologyCleanupStagedChangesInput,
): CreateOntologyCleanupStagedChangesResult {
  const errors: string[] = [];
  if (!input.confirmed) {
    errors.push("Cleanup actions require explicit confirmation before they can be staged.");
  }
  const selected = new Set(input.selectedCandidateIds);
  const skipped: Array<{ candidate: OntologyCleanupCandidate; reason: string }> = [];
  const changes: OntologyStagedChange[] = [];
  const now = input.now ?? new Date().toISOString();
  let index = 0;
  for (const candidate of input.candidates) {
    if (!selected.has(candidate.id)) continue;
    if (!candidate.delete_supported) {
      skipped.push({ candidate, reason: "This candidate cannot be auto-deleted; resolve it in the affected resource editor." });
      continue;
    }
    if (!input.confirmed) {
      skipped.push({ candidate, reason: "Cleanup must be confirmed before changes are staged." });
      continue;
    }
    changes.push({
      id: `cleanup:${candidate.kind}:${candidate.resource_id}:${index}`,
      kind: candidate.staged_change_kind,
      action: "delete",
      label: `Cleanup: delete ${candidate.label}`,
      description: candidate.reason,
      targetId: candidate.resource_id,
      parentRef: candidate.parent_resource_id
        ? { kind: "object_type", id: candidate.parent_resource_id }
        : undefined,
      payload: {
        id: candidate.resource_id,
        cleanup_action: true,
        cleanup_reason: candidate.reason,
        cleanup_kind: candidate.kind,
      },
      warnings: [
        "Cleanup deletion — review impact and downstream usage before saving.",
        ...candidate.warnings,
      ],
      errors: [],
      source: "ontology_cleanup_assistant",
      author: input.currentUserId || "local",
      createdAt: now,
    });
    index += 1;
  }
  return {
    changes,
    skipped,
    errors,
    confirmation_required: !input.confirmed,
  };
}

export type OntologyAuditEventCategory =
  | "resource_crud"
  | "datasource_mapping"
  | "object_view_edit"
  | "object_view_publish"
  | "import"
  | "export"
  | "restore"
  | "branch_rebase"
  | "marketplace_packaging"
  | "permission_change";

export type OntologyAuditEventStatus = "saved" | "pending" | "failed" | "info";

export interface OntologyAuditEvent {
  id: string;
  category: OntologyAuditEventCategory;
  category_label: string;
  action: string;
  resource_kind: string;
  resource_id: string;
  resource_label: string;
  actor: string;
  timestamp: string;
  status: OntologyAuditEventStatus;
  summary: string;
  source: string;
  metadata: Record<string, unknown>;
}

export interface OntologyAuditEventFilters {
  category?: OntologyAuditEventCategory;
  actor?: string;
  resource_kind?: string;
  status?: OntologyAuditEventStatus;
  from?: string;
  to?: string;
}

export interface OntologyAuditEventLog {
  events: OntologyAuditEvent[];
  totals: {
    events: number;
    by_category: Record<OntologyAuditEventCategory, number>;
    by_status: Record<OntologyAuditEventStatus, number>;
    unique_actors: number;
  };
  generated_at: string;
}

const ONTOLOGY_AUDIT_CATEGORY_LABELS: Record<OntologyAuditEventCategory, string> = {
  resource_crud: "Resource CRUD",
  datasource_mapping: "Datasource mapping",
  object_view_edit: "Object View edit",
  object_view_publish: "Object View publish",
  import: "Bundle import",
  export: "Bundle export",
  restore: "History restore",
  branch_rebase: "Branch rebase",
  marketplace_packaging: "Marketplace packaging",
  permission_change: "Permission change",
};

function ontologyAuditCategoryLabel(category: OntologyAuditEventCategory) {
  return ONTOLOGY_AUDIT_CATEGORY_LABELS[category];
}

function ontologyAuditPayloadString(value: unknown) {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  if (value === null || value === undefined) return "";
  try {
    return JSON.stringify(value);
  } catch {
    return "";
  }
}

function ontologyAuditCategoryForChange(change: OntologyStagedChange): OntologyAuditEventCategory {
  const action = (change.action || "").toLowerCase();
  const source = (change.source || "").toLowerCase();
  const payload = stagedPayload(change);
  if (source === "ontology_history_restore" || action === "restore") return "restore";
  if (source === "ontology_bundle_import") return "import";
  if (change.kind === "marketplace_object_view_output") return "marketplace_packaging";
  if (change.kind === "object_type_binding") return "datasource_mapping";
  if (change.kind === "core_object_view" || change.kind === "custom_object_view" || change.kind === "object_view") {
    return "object_view_edit";
  }
  if (payload.permission_key !== undefined || payload.authorization_policy !== undefined || payload.role_permissions !== undefined) {
    return "permission_change";
  }
  return "resource_crud";
}

function ontologyAuditActionLabel(change: OntologyStagedChange) {
  const action = (change.action || "").toLowerCase();
  switch (action) {
    case "create":
    case "import":
      return "create";
    case "update":
    case "edit":
    case "upsert":
      return "update";
    case "delete":
    case "remove":
      return "delete";
    case "restore":
      return "restore";
    case "publish":
      return "publish";
    default:
      return action || "update";
  }
}

function ontologyAuditSummaryForChange(change: OntologyStagedChange, category: OntologyAuditEventCategory) {
  if (change.description) return change.description;
  const label = change.label || `${change.kind || "resource"} ${change.targetId || ""}`.trim();
  return `${ontologyAuditCategoryLabel(category)}: ${label}.`;
}

function ontologyAuditEmptyCategoryTotals(): Record<OntologyAuditEventCategory, number> {
  return {
    resource_crud: 0,
    datasource_mapping: 0,
    object_view_edit: 0,
    object_view_publish: 0,
    import: 0,
    export: 0,
    restore: 0,
    branch_rebase: 0,
    marketplace_packaging: 0,
    permission_change: 0,
  };
}

function ontologyAuditEmptyStatusTotals(): Record<OntologyAuditEventStatus, number> {
  return {
    saved: 0,
    pending: 0,
    failed: 0,
    info: 0,
  };
}

export interface BuildOntologyAuditEventLogInput {
  savedChanges?: OntologySavedChangeRecord[];
  workingChanges?: OntologyStagedChange[];
  objectViews?: ObjectViewDefinition[];
  marketplacePackagings?: Array<{
    id: string;
    label: string;
    object_view_id?: string;
    object_type_id?: string;
    actor?: string;
    packaged_at: string;
    output_id?: string;
    notes?: string;
  }>;
  filters?: OntologyAuditEventFilters;
  now?: string;
}

export function buildOntologyAuditEventLog(input: BuildOntologyAuditEventLogInput): OntologyAuditEventLog {
  const filters = input.filters ?? {};
  const events: OntologyAuditEvent[] = [];

  for (const record of input.savedChanges ?? []) {
    const changes = Array.isArray(record.changes) ? record.changes : [];
    for (const change of changes) {
      const category = ontologyAuditCategoryForChange(change);
      const resource = ontologyChangeResource(change);
      events.push({
        id: `audit:saved:${record.id}:${change.id}`,
        category,
        category_label: ontologyAuditCategoryLabel(category),
        action: ontologyAuditActionLabel(change),
        resource_kind: resource.kind,
        resource_id: resource.id ?? change.targetId ?? "",
        resource_label: change.label || resource.id || resource.kind,
        actor: ontologyChangeAuthor(change, record.saved_by || "unknown"),
        timestamp: record.saved_at,
        status: record.status === "failed" ? "failed" : "saved",
        summary: ontologyAuditSummaryForChange(change, category),
        source: change.source || "ontology_change",
        metadata: {
          record_id: record.id,
          change_id: change.id,
          change_kind: change.kind,
          change_action: change.action,
          project_id: record.project_id,
          branch_id: record.branch_id ?? null,
          proposal_id: record.proposal_id ?? null,
        },
      });
    }
  }

  for (const change of input.workingChanges ?? []) {
    const category = ontologyAuditCategoryForChange(change);
    const resource = ontologyChangeResource(change);
    events.push({
      id: `audit:pending:${change.id}`,
      category,
      category_label: ontologyAuditCategoryLabel(category),
      action: ontologyAuditActionLabel(change),
      resource_kind: resource.kind,
      resource_id: resource.id ?? change.targetId ?? "",
      resource_label: change.label || resource.id || resource.kind,
      actor: ontologyChangeAuthor(change, "unknown"),
      timestamp: change.createdAt,
      status: "pending",
      summary: ontologyAuditSummaryForChange(change, category),
      source: change.source || "ontology_change",
      metadata: {
        change_id: change.id,
        change_kind: change.kind,
        change_action: change.action,
        warnings: change.warnings ?? [],
      },
    });
  }

  for (const view of input.objectViews ?? []) {
    const config = view.config;
    if (!config) continue;
    for (const version of config.version_history ?? []) {
      const isPublish = version.publish_state === "published" || version.published === true;
      if (!isPublish) continue;
      const timestamp = version.published_at || version.timestamp;
      events.push({
        id: `audit:publish:${view.id}:${version.id}`,
        category: "object_view_publish",
        category_label: ontologyAuditCategoryLabel("object_view_publish"),
        action: "publish",
        resource_kind: view.mode === "standard" ? "core_object_view" : "custom_object_view",
        resource_id: view.id,
        resource_label: view.display_name || view.name || view.id,
        actor: version.author || "unknown",
        timestamp,
        status: "saved",
        summary: `Published Object View ${view.display_name || view.name} v${version.object_view_version}. ${version.change_summary}`.trim(),
        source: "object_view_version_history",
        metadata: {
          object_view_version: version.object_view_version,
          workshop_module_version: version.workshop_module_version,
          rollback_target_version: version.rollback_target_version,
          restored_from_version: version.restored_from_version,
          tab_ids: version.tab_ids,
        },
      });
    }
    const rebasedAt = config.metadata?.branch_rebased_at;
    if (typeof rebasedAt === "string" && rebasedAt) {
      events.push({
        id: `audit:rebase:${view.id}:${rebasedAt}`,
        category: "branch_rebase",
        category_label: ontologyAuditCategoryLabel("branch_rebase"),
        action: "rebase",
        resource_kind: view.mode === "standard" ? "core_object_view" : "custom_object_view",
        resource_id: view.id,
        resource_label: view.display_name || view.name || view.id,
        actor: config.last_saved_by || "unknown",
        timestamp: rebasedAt,
        status: "saved",
        summary: `Object View ${view.display_name || view.name} rebased against ontology signature ${ontologyAuditPayloadString(config.metadata?.branch_rebased_ontology_signature) || "(unknown)"}.`,
        source: "object_view_branch_rebase",
        metadata: {
          branch_rebased_ontology_signature: config.metadata?.branch_rebased_ontology_signature,
          branch_resource_id: config.metadata?.branch_resource_id,
        },
      });
    }
  }

  for (const packaging of input.marketplacePackagings ?? []) {
    events.push({
      id: `audit:marketplace:${packaging.id}`,
      category: "marketplace_packaging",
      category_label: ontologyAuditCategoryLabel("marketplace_packaging"),
      action: "package",
      resource_kind: "marketplace_object_view_output",
      resource_id: packaging.output_id || packaging.id,
      resource_label: packaging.label,
      actor: packaging.actor || "unknown",
      timestamp: packaging.packaged_at,
      status: "info",
      summary: packaging.notes ? `${packaging.label}: ${packaging.notes}` : `Marketplace packaging: ${packaging.label}.`,
      source: "marketplace_packaging",
      metadata: {
        object_view_id: packaging.object_view_id,
        object_type_id: packaging.object_type_id,
      },
    });
  }

  const filtered = events
    .filter((event) => {
      if (filters.category && event.category !== filters.category) return false;
      if (filters.status && event.status !== filters.status) return false;
      if (filters.resource_kind && event.resource_kind !== filters.resource_kind) return false;
      if (filters.actor) {
        const needle = filters.actor.trim().toLowerCase();
        if (needle && !event.actor.toLowerCase().includes(needle)) return false;
      }
      if (filters.from) {
        const fromTime = Date.parse(filters.from);
        if (Number.isFinite(fromTime) && Date.parse(event.timestamp) < fromTime) return false;
      }
      if (filters.to) {
        const toTime = Date.parse(filters.to);
        if (Number.isFinite(toTime) && Date.parse(event.timestamp) > toTime) return false;
      }
      return true;
    })
    .sort((left, right) => {
      const leftTime = Date.parse(left.timestamp);
      const rightTime = Date.parse(right.timestamp);
      const leftValue = Number.isFinite(leftTime) ? leftTime : 0;
      const rightValue = Number.isFinite(rightTime) ? rightTime : 0;
      return rightValue - leftValue;
    });

  const byCategory = ontologyAuditEmptyCategoryTotals();
  const byStatus = ontologyAuditEmptyStatusTotals();
  const actors = new Set<string>();
  for (const event of filtered) {
    byCategory[event.category] += 1;
    byStatus[event.status] += 1;
    if (event.actor) actors.add(event.actor);
  }

  return {
    events: filtered,
    totals: {
      events: filtered.length,
      by_category: byCategory,
      by_status: byStatus,
      unique_actors: actors.size,
    },
    generated_at: input.now ?? new Date().toISOString(),
  };
}

export type OntologyHealthCategory =
  | "stale_datasource"
  | "broken_link"
  | "widget_load_failure"
  | "inaccessible_backing_data"
  | "indexing_lag"
  | "missing_value_type"
  | "permission_mismatch";

export type OntologyHealthSeverity = "info" | "warning" | "critical";

export interface OntologyHealthIssue {
  id: string;
  category: OntologyHealthCategory;
  category_label: string;
  severity: OntologyHealthSeverity;
  resource_kind: string;
  resource_id: string;
  resource_label: string;
  detected_at: string;
  message: string;
  remediation: string;
  metadata: Record<string, unknown>;
}

export interface OntologyHealthCategorySummary {
  category: OntologyHealthCategory;
  label: string;
  total: number;
  critical: number;
  warning: number;
  info: number;
}

export interface OntologyHealthReport {
  generated_at: string;
  issues: OntologyHealthIssue[];
  totals: { issues: number; critical: number; warning: number; info: number };
  by_category: OntologyHealthCategorySummary[];
}

const ONTOLOGY_HEALTH_CATEGORY_LABELS: Record<OntologyHealthCategory, string> = {
  stale_datasource: "Stale datasources",
  broken_link: "Broken links",
  widget_load_failure: "Widget load failures",
  inaccessible_backing_data: "Inaccessible backing data",
  indexing_lag: "Indexing lag",
  missing_value_type: "Missing value type validation",
  permission_mismatch: "Permission mismatches",
};

export interface BuildOntologyHealthReportInput {
  objectTypes?: ObjectType[];
  linkTypes?: LinkType[];
  objectViews?: ObjectViewDefinition[];
  valueTypes?: OntologyValueType[];
  permissionAnalysis?: OntologyPermissionAnalysis;
  widgetFailures?: Array<{
    id?: string;
    object_view_id: string;
    object_view_label?: string;
    tab_id?: string;
    widget_id?: string;
    message: string;
    detected_at?: string;
  }>;
  staleDatasourceThresholdHours?: number;
  now?: string;
}

export function buildOntologyHealthReport(input: BuildOntologyHealthReportInput): OntologyHealthReport {
  const now = input.now ?? new Date().toISOString();
  const nowTime = Date.parse(now);
  const thresholdHours = input.staleDatasourceThresholdHours ?? 72;
  const thresholdMs = Math.max(1, thresholdHours) * 60 * 60 * 1000;
  const issues: OntologyHealthIssue[] = [];
  const objectTypes = input.objectTypes ?? [];
  const linkTypes = input.linkTypes ?? [];
  const objectViews = input.objectViews ?? [];
  const valueTypes = input.valueTypes ?? [];

  const objectTypeById = new Map(objectTypes.map((type) => [type.id, type]));
  const valueTypeIds = new Set(valueTypes.map((valueType) => valueType.id));

  for (const objectType of objectTypes) {
    if (!objectType.backing_dataset_id) continue;
    const updatedAt = Date.parse(objectType.updated_at || "");
    if (!Number.isFinite(updatedAt) || !Number.isFinite(nowTime)) continue;
    const ageMs = nowTime - updatedAt;
    if (ageMs < thresholdMs) continue;
    issues.push({
      id: `health:stale_datasource:${objectType.id}`,
      category: "stale_datasource",
      category_label: ONTOLOGY_HEALTH_CATEGORY_LABELS.stale_datasource,
      severity: ageMs > thresholdMs * 3 ? "critical" : "warning",
      resource_kind: "object_type",
      resource_id: objectType.id,
      resource_label: objectType.display_name || objectType.name,
      detected_at: now,
      message: `Backing dataset for ${objectType.display_name || objectType.name} has not synchronized in ${Math.round(ageMs / (60 * 60 * 1000))}h.`,
      remediation: "Re-run the dataset pipeline or refresh the schema mapping in the object type editor.",
      metadata: {
        backing_dataset_id: objectType.backing_dataset_id,
        age_hours: Math.round(ageMs / (60 * 60 * 1000)),
      },
    });
  }

  for (const linkType of linkTypes) {
    const missingSource = !objectTypeById.has(linkType.source_type_id);
    const missingTarget = !objectTypeById.has(linkType.target_type_id);
    if (!missingSource && !missingTarget) continue;
    const missingNames: string[] = [];
    if (missingSource) missingNames.push(`source object type ${linkType.source_type_id}`);
    if (missingTarget) missingNames.push(`target object type ${linkType.target_type_id}`);
    issues.push({
      id: `health:broken_link:${linkType.id}`,
      category: "broken_link",
      category_label: ONTOLOGY_HEALTH_CATEGORY_LABELS.broken_link,
      severity: "critical",
      resource_kind: "link_type",
      resource_id: linkType.id,
      resource_label: linkType.display_name || linkType.name,
      detected_at: now,
      message: `Link ${linkType.display_name || linkType.name} references missing ${missingNames.join(" and ")}.`,
      remediation: "Restore the missing object types, repoint the link, or delete the link via Cleanup.",
      metadata: {
        source_type_id: linkType.source_type_id,
        target_type_id: linkType.target_type_id,
        missing_source: missingSource,
        missing_target: missingTarget,
      },
    });
  }

  for (const view of objectViews) {
    const tabs = view.config?.tabs ?? [];
    for (const tab of tabs) {
      if (tab.visibility === "hidden") continue;
      const widgetCount = tab.module?.widgets?.length ?? 0;
      if (widgetCount > 0) continue;
      issues.push({
        id: `health:widget_load_failure:empty_module:${view.id}:${tab.id}`,
        category: "widget_load_failure",
        category_label: ONTOLOGY_HEALTH_CATEGORY_LABELS.widget_load_failure,
        severity: "warning",
        resource_kind: view.mode === "standard" ? "core_object_view" : "custom_object_view",
        resource_id: view.id,
        resource_label: view.display_name || view.name || view.id,
        detected_at: now,
        message: `Tab "${tab.title}" has no widgets attached but is set to visible — viewers will see an empty surface.`,
        remediation: "Add widgets to the tab in the Object View editor, hide the tab, or delete it.",
        metadata: { object_view_id: view.id, tab_id: tab.id },
      });
    }
    if (view.config?.metadata?.legacy_builder === true) {
      issues.push({
        id: `health:widget_load_failure:legacy_builder:${view.id}`,
        category: "widget_load_failure",
        category_label: ONTOLOGY_HEALTH_CATEGORY_LABELS.widget_load_failure,
        severity: "warning",
        resource_kind: view.mode === "standard" ? "core_object_view" : "custom_object_view",
        resource_id: view.id,
        resource_label: view.display_name || view.name || view.id,
        detected_at: now,
        message: `Object View ${view.display_name || view.name} still uses the legacy builder; widget loads may fail in the runtime.`,
        remediation: "Migrate the Object View to the configured Workshop module builder.",
        metadata: { legacy_builder: true },
      });
    }
  }

  for (const failure of input.widgetFailures ?? []) {
    issues.push({
      id: failure.id || `health:widget_load_failure:runtime:${failure.object_view_id}:${failure.widget_id ?? failure.tab_id ?? "unknown"}`,
      category: "widget_load_failure",
      category_label: ONTOLOGY_HEALTH_CATEGORY_LABELS.widget_load_failure,
      severity: "critical",
      resource_kind: "custom_object_view",
      resource_id: failure.object_view_id,
      resource_label: failure.object_view_label || failure.object_view_id,
      detected_at: failure.detected_at || now,
      message: failure.message,
      remediation: "Check the Workshop module bindings and reload; if the error persists, file an Object View bug.",
      metadata: { tab_id: failure.tab_id, widget_id: failure.widget_id },
    });
  }

  for (const objectType of objectTypes) {
    const supportsRestricted = Boolean(objectType.restricted_view_id || objectType.backing_restricted_view_id || objectType.restricted_view_storage_mode);
    if (!supportsRestricted) continue;
    const policy = objectType.restricted_view_policy_version ?? 0;
    const registered = objectType.restricted_view_registered_policy_version ?? 0;
    const indexed = objectType.restricted_view_indexed_policy_version ?? 0;
    if (policy === indexed) continue;
    issues.push({
      id: `health:indexing_lag:${objectType.id}`,
      category: "indexing_lag",
      category_label: ONTOLOGY_HEALTH_CATEGORY_LABELS.indexing_lag,
      severity: policy - indexed > 1 ? "critical" : "warning",
      resource_kind: "object_type",
      resource_id: objectType.id,
      resource_label: objectType.display_name || objectType.name,
      detected_at: now,
      message: `Restricted-view policy v${policy} is ahead of indexed v${indexed} (registered v${registered}); row-level outcomes may be stale.`,
      remediation: "Trigger a re-index for the object type's restricted view or wait for the indexer to catch up.",
      metadata: {
        policy_version: policy,
        registered_policy_version: registered,
        indexed_policy_version: indexed,
      },
    });
  }

  for (const objectType of objectTypes) {
    if (!objectType.backing_dataset_id && !objectType.backing_restricted_view_id) {
      issues.push({
        id: `health:inaccessible_backing_data:no_binding:${objectType.id}`,
        category: "inaccessible_backing_data",
        category_label: ONTOLOGY_HEALTH_CATEGORY_LABELS.inaccessible_backing_data,
        severity: "warning",
        resource_kind: "object_type",
        resource_id: objectType.id,
        resource_label: objectType.display_name || objectType.name,
        detected_at: now,
        message: `Object type ${objectType.display_name || objectType.name} has no backing dataset or restricted view; instance queries will return empty results.`,
        remediation: "Attach a dataset or restricted view binding in the object type editor.",
        metadata: {},
      });
    }
  }

  if (input.permissionAnalysis) {
    for (const decision of input.permissionAnalysis.resources) {
      if (decision.is_owner && !decision.can_edit) {
        issues.push({
          id: `health:permission_mismatch:owner_cannot_edit:${decision.resource_key}`,
          category: "permission_mismatch",
          category_label: ONTOLOGY_HEALTH_CATEGORY_LABELS.permission_mismatch,
          severity: "warning",
          resource_kind: decision.resource_kind,
          resource_id: decision.resource_id,
          resource_label: decision.display_name,
          detected_at: now,
          message: `${decision.display_name} lists you as owner but the effective permission level (${decision.effective_level}) does not allow edits.`,
          remediation: "Update project-role bindings or escalate ownership to a principal with edit rights.",
          metadata: { effective_level: decision.effective_level, reasons: decision.reasons },
        });
      }
      if (decision.can_view_definition && !decision.can_view_instances) {
        issues.push({
          id: `health:inaccessible_backing_data:redacted:${decision.resource_key}`,
          category: "inaccessible_backing_data",
          category_label: ONTOLOGY_HEALTH_CATEGORY_LABELS.inaccessible_backing_data,
          severity: "info",
          resource_kind: decision.resource_kind,
          resource_id: decision.resource_id,
          resource_label: decision.display_name,
          detected_at: now,
          message: `${decision.display_name} schema is visible but instance data is inaccessible to the current principal — Object Views render in schema-only mode.`,
          remediation: "Grant a data-viewing role on the backing dataset or restricted view if the principal needs instance data.",
          metadata: { reasons: decision.reasons },
        });
      }
    }
    for (const check of input.permissionAnalysis.change_checks) {
      if (check.allowed) continue;
      issues.push({
        id: `health:permission_mismatch:change_blocked:${check.change_id}`,
        category: "permission_mismatch",
        category_label: ONTOLOGY_HEALTH_CATEGORY_LABELS.permission_mismatch,
        severity: "critical",
        resource_kind: check.change_kind,
        resource_id: check.change_id,
        resource_label: check.change_label,
        detected_at: now,
        message: `${check.change_label} is blocked by missing permissions on ${check.requirements.filter((requirement) => !requirement.allowed).map((requirement) => requirement.resource_label).join(", ")}.`,
        remediation: "Resolve the missing project permissions before saving the staged change.",
        metadata: {
          requirements: check.requirements,
        },
      });
    }
  }

  for (const objectType of objectTypes) {
    for (const property of objectType.properties ?? []) {
      if (!property.value_type_id) continue;
      if (valueTypeIds.has(property.value_type_id)) continue;
      issues.push({
        id: `health:missing_value_type:${objectType.id}.${property.name}`,
        category: "missing_value_type",
        category_label: ONTOLOGY_HEALTH_CATEGORY_LABELS.missing_value_type,
        severity: "warning",
        resource_kind: "property",
        resource_id: `${objectType.id}.${property.name}`,
        resource_label: `${objectType.display_name || objectType.name}.${property.display_name || property.name}`,
        detected_at: now,
        message: `Property references value type ${property.value_type_id} that is not present in this ontology; validation will not run.`,
        remediation: "Re-import the value type or repoint the property to a defined value type.",
        metadata: { value_type_id: property.value_type_id },
      });
    }
  }

  const byCategoryMap = new Map<OntologyHealthCategory, OntologyHealthCategorySummary>();
  for (const category of Object.keys(ONTOLOGY_HEALTH_CATEGORY_LABELS) as OntologyHealthCategory[]) {
    byCategoryMap.set(category, {
      category,
      label: ONTOLOGY_HEALTH_CATEGORY_LABELS[category],
      total: 0,
      critical: 0,
      warning: 0,
      info: 0,
    });
  }
  let critical = 0;
  let warning = 0;
  let info = 0;
  for (const issue of issues) {
    const summary = byCategoryMap.get(issue.category)!;
    summary.total += 1;
    if (issue.severity === "critical") {
      summary.critical += 1;
      critical += 1;
    } else if (issue.severity === "warning") {
      summary.warning += 1;
      warning += 1;
    } else {
      summary.info += 1;
      info += 1;
    }
  }
  const sortedIssues = [...issues].sort((left, right) => {
    const severityRank = { critical: 0, warning: 1, info: 2 } as const;
    if (severityRank[left.severity] !== severityRank[right.severity]) {
      return severityRank[left.severity] - severityRank[right.severity];
    }
    return left.category.localeCompare(right.category);
  });
  return {
    generated_at: now,
    issues: sortedIssues,
    totals: { issues: sortedIssues.length, critical, warning, info },
    by_category: Array.from(byCategoryMap.values()),
  };
}

export type OntologyResourceSearchIndexKind =
  | OntologyResourceRegistryKind
  | "property"
  | "usage_edge"
  | "saved_exploration"
  | "saved_list"
  | "value_type";

export type OntologyResourceSearchPermissionFilter = "viewable" | "hidden" | "all";

export interface OntologyResourceSearchPermission {
  can_view: boolean;
  can_view_details: boolean;
  schema_only: boolean;
  reason: string;
}

export interface OntologyResourceSearchDocument {
  id: string;
  resource_kind: OntologyResourceSearchIndexKind;
  resource_id: string;
  parent_resource_kind?: string | null;
  parent_resource_id?: string | null;
  api_name: string;
  display_name: string;
  description: string;
  project_id: string | null;
  project_display_name: string;
  folder_path: string;
  group_ids: string[];
  group_names: string[];
  visibility: string;
  status: string;
  branch_state: string;
  usage_count: number;
  linked_resource_count: number;
  last_edited_at: string | null;
  last_edited_by: string | null;
  href: string;
  tokens: string[];
  token_text: string;
  permission: OntologyResourceSearchPermission;
  indexed_at: string;
  source_updated_at: string | null;
  source_signature: string;
  rank_boost: number;
}

export interface OntologyResourceSearchFacet {
  id: string;
  label: string;
  count: number;
}

export interface OntologyResourceSearchIndex {
  documents: OntologyResourceSearchDocument[];
  revision: string;
  built_at: string;
  incremental: {
    previous_revision: string | null;
    reused_documents: number;
    upserted_documents: number;
    removed_documents: number;
    changed_resource_keys: string[];
    source_counts: Record<string, number>;
  };
  facets: {
    resource_kinds: OntologyResourceSearchFacet[];
    projects: OntologyResourceSearchFacet[];
    groups: OntologyResourceSearchFacet[];
  };
}

export interface BuildOntologyResourceSearchIndexInput {
  registry: OntologyResourceRegistryEntry[];
  objectTypes?: ObjectType[];
  linkTypes?: LinkType[];
  interfaces?: OntologyInterface[];
  sharedPropertyTypes?: SharedPropertyType[];
  objectTypeGroups?: OntologyObjectTypeGroupSummary[];
  objectViews?: ObjectViewDefinition[];
  savedExplorations?: ObjectSetDefinition[];
  usageReferences?: OntologyUsageReference[];
  permissionAnalysis?: Pick<OntologyPermissionAnalysis, "resources"> | null;
  principal?: OntologyPermissionPrincipal | null;
  previousIndex?: OntologyResourceSearchIndex | null;
  now?: string;
}

export interface OntologyResourceSearchQuery {
  query?: string;
  api_name?: string;
  api_name_only?: boolean;
  resource_kinds?: OntologyResourceSearchIndexKind[];
  project_ids?: string[];
  group_ids?: string[];
  visibility?: string;
  status?: string;
  permission_filter?: OntologyResourceSearchPermissionFilter;
  page?: number;
  per_page?: number;
}

export interface OntologyResourceSearchResultItem extends OntologyResourceSearchDocument {
  score: number;
  matched_terms: string[];
  match_reason: string;
}

export interface OntologyResourceSearchResults {
  data: OntologyResourceSearchResultItem[];
  total: number;
  page: number;
  per_page: number;
  total_pages: number;
  hidden_results: number;
  query: string;
  facets: {
    resource_kinds: Record<string, number>;
    projects: Record<string, number>;
    groups: Record<string, number>;
    visibility: Record<string, number>;
  };
}

export function buildOntologyResourceSearchIndex(
  input: BuildOntologyResourceSearchIndexInput,
): OntologyResourceSearchIndex {
  const builtAt = input.now || new Date().toISOString();
  const previousByID = new Map((input.previousIndex?.documents || []).map((document) => [document.id, document]));
  const groupByObjectTypeID = ontologySearchGroupsByObjectType(input.objectTypeGroups || []);
  const registryByKey = new Map(input.registry.map((entry) => [ontologySearchRegistryKey(entry.resource_kind, entry.resource_id), entry]));
  const registryByResourceID = new Map(input.registry.map((entry) => [entry.resource_id, entry]));
  const permissionByKey = new Map((input.permissionAnalysis?.resources || []).map((decision) => [decision.resource_key, decision]));
  const objectTypeByID = new Map((input.objectTypes || []).map((objectType) => [objectType.id, objectType]));
  const documents: OntologyResourceSearchDocument[] = [];

  const push = (document: OntologyResourceSearchDocument) => {
    const previous = previousByID.get(document.id);
    documents.push(previous && previous.source_signature === document.source_signature
      ? previous
      : document);
  };

  for (const entry of input.registry) {
    const objectTypeID = entry.resource_kind === "object_type"
      ? entry.resource_id
      : entry.resource_kind === "core_object_view" || entry.resource_kind === "custom_object_view"
        ? input.objectViews?.find((view) => view.id === entry.resource_id)?.object_type_id
        : null;
    const groups = objectTypeID ? groupByObjectTypeID.get(objectTypeID) || [] : [];
    push(ontologySearchDocument({
      builtAt,
      resource_kind: entry.resource_kind,
      resource_id: entry.resource_id,
      api_name: entry.api_name,
      display_name: entry.display_name,
      description: entry.description,
      project_id: entry.project_id,
      project_display_name: entry.project_display_name,
      folder_path: entry.folder_path,
      group_ids: groups.map((group) => group.id),
      group_names: groups.map((group) => group.display_name || group.name),
      visibility: entry.visibility,
      status: entry.status,
      branch_state: entry.branch_state,
      usage_count: entry.usage_count,
      linked_resource_count: entry.linked_resource_count,
      last_edited_at: entry.last_edited_at,
      last_edited_by: entry.last_edited_by,
      href: ontologySearchHrefForRegistryEntry(entry),
      tokens: [
        entry.resource_kind,
        entry.resource_id,
        entry.api_name,
        entry.display_name,
        entry.plural_display_name || "",
        entry.description,
        entry.project_display_name,
        entry.folder_path,
        entry.backing_datasource_id || "",
        ...groups.flatMap((group) => [group.name, group.display_name]),
      ],
      permission: ontologySearchPermissionForResource({
        key: ontologyResourceKey(entry.resource_kind, entry.resource_id),
        fallbackVisibility: entry.visibility,
        permissionByKey,
      }),
      source_updated_at: entry.last_edited_at,
      rank_boost: ontologySearchRankBoost(entry.resource_kind),
    }));
  }

  for (const objectType of input.objectTypes || []) {
    const parent = registryByKey.get(ontologySearchRegistryKey("object_type", objectType.id));
    const groups = groupByObjectTypeID.get(objectType.id) || [];
    for (const property of objectType.properties || []) {
      const resourceID = `${objectType.id}.${property.name}`;
      const visibility = propertyDisplayMode(property) === "hidden" || objectType.visibility === "hidden"
        ? "hidden"
        : objectType.visibility || "normal";
      push(ontologySearchDocument({
        builtAt,
        resource_kind: "property",
        resource_id: resourceID,
        parent_resource_kind: "object_type",
        parent_resource_id: objectType.id,
        api_name: `${objectTypeAPIName(objectType)}.${property.name}`,
        display_name: `${objectType.display_name || objectTypeAPIName(objectType)}.${property.display_name || property.name}`,
        description: property.description || `Property ${property.name} on ${objectType.display_name || objectType.name}.`,
        project_id: parent?.project_id ?? null,
        project_display_name: parent?.project_display_name || "Unplaced ontology resource",
        folder_path: parent?.folder_path || "/",
        group_ids: groups.map((group) => group.id),
        group_names: groups.map((group) => group.display_name || group.name),
        visibility,
        status: objectType.status || "active",
        branch_state: parent?.branch_state || "main",
        usage_count: 0,
        linked_resource_count: property.value_type_id || property.shared_property_type_id ? 1 : 0,
        last_edited_at: property.updated_at || objectType.updated_at || null,
        last_edited_by: objectType.owner_id || null,
        href: `/ontology/${encodeURIComponent(objectType.id)}?property=${encodeURIComponent(property.name)}`,
        tokens: [
          "property",
          resourceID,
          property.id,
          property.name,
          property.display_name,
          property.description,
          property.property_type,
          property.value_type_id || "",
          property.shared_property_type_id || "",
          objectType.id,
          objectTypeAPIName(objectType),
          objectType.display_name,
          ...groups.flatMap((group) => [group.name, group.display_name]),
        ],
        permission: ontologySearchPermissionForResource({
          key: ontologyResourceKey("object_type", objectType.id),
          fallbackVisibility: visibility,
          permissionByKey,
          schemaOnly: false,
        }),
        source_updated_at: property.updated_at || objectType.updated_at || null,
        rank_boost: 26,
      }));
    }
  }

  for (const reference of input.usageReferences || []) {
    const targetRegistry = ontologySearchRegistryEntryForUsage(reference, registryByKey, registryByResourceID);
    const targetObjectTypeID = reference.resource_kind === "property"
      ? reference.resource_id.split(".")[0]
      : reference.resource_kind === "object_type"
        ? reference.resource_id
        : undefined;
    const groups = targetObjectTypeID ? groupByObjectTypeID.get(targetObjectTypeID) || [] : [];
    const key = targetRegistry
      ? ontologyResourceKey(targetRegistry.resource_kind, targetRegistry.resource_id)
      : ontologyResourceKey(reference.resource_kind, reference.resource_id);
    push(ontologySearchDocument({
      builtAt,
      resource_kind: "usage_edge",
      resource_id: reference.id,
      parent_resource_kind: reference.resource_kind,
      parent_resource_id: reference.resource_id,
      api_name: `${reference.product}.${reference.consumer_id}.${reference.resource_kind}.${reference.resource_id}`,
      display_name: `${reference.consumer_label} -> ${reference.resource_label}`,
      description: reference.detail,
      project_id: targetRegistry?.project_id ?? null,
      project_display_name: targetRegistry?.project_display_name || "Cross-product usage",
      folder_path: targetRegistry?.folder_path || "/usage",
      group_ids: groups.map((group) => group.id),
      group_names: groups.map((group) => group.display_name || group.name),
      visibility: targetRegistry?.visibility || "normal",
      status: reference.write_count > 0 ? "write_reference" : "read_reference",
      branch_state: targetRegistry?.branch_state || "main",
      usage_count: reference.read_count + reference.write_count,
      linked_resource_count: 2,
      last_edited_at: reference.last_used_at,
      last_edited_by: null,
      href: "/ontology-manager?section=usage",
      tokens: [
        "usage",
        "usage_edge",
        reference.product,
        usageProductLabel(reference.product),
        reference.consumer_id,
        reference.consumer_label,
        reference.consumer_kind,
        reference.surface,
        reference.detail,
        reference.resource_kind,
        reference.resource_id,
        reference.resource_label,
        ...groups.flatMap((group) => [group.name, group.display_name]),
      ],
      permission: ontologySearchPermissionForResource({
        key,
        fallbackVisibility: targetRegistry?.visibility || "normal",
        permissionByKey,
      }),
      source_updated_at: reference.last_used_at,
      rank_boost: 10,
    }));
  }

  for (const saved of input.savedExplorations || []) {
    const objectType = objectTypeByID.get(saved.base_object_type_id);
    const parent = saved.base_object_type_id
      ? registryByKey.get(ontologySearchRegistryKey("object_type", saved.base_object_type_id))
      : undefined;
    const groups = saved.base_object_type_id ? groupByObjectTypeID.get(saved.base_object_type_id) || [] : [];
    const access = objectType
      ? objectExplorerSavedArtifactAccess(saved, objectType, input.principal)
      : null;
    const kind = objectExplorerSavedArtifactKind(saved) === "list" ? "saved_list" : "saved_exploration";
    push(ontologySearchDocument({
      builtAt,
      resource_kind: kind,
      resource_id: saved.id,
      parent_resource_kind: "object_type",
      parent_resource_id: saved.base_object_type_id,
      api_name: saved.share_slug || saved.id,
      display_name: saved.name,
      description: saved.description || `${kind === "saved_list" ? "Saved object list" : "Saved exploration"} for ${objectType?.display_name || saved.base_object_type_id}.`,
      project_id: saved.project_id ?? parent?.project_id ?? null,
      project_display_name: parent?.project_display_name || "Saved explorations",
      folder_path: saved.folder_path || parent?.folder_path || "/object-explorer",
      group_ids: groups.map((group) => group.id),
      group_names: groups.map((group) => group.display_name || group.name),
      visibility: objectExplorerSavedArtifactPrivacy(saved),
      status: saved.materialized_at ? "materialized" : "live",
      branch_state: "main",
      usage_count: saved.materialized_row_count || saved.selected_object_ids?.length || 0,
      linked_resource_count: saved.projections.length + saved.filters.length + saved.traversals.length,
      last_edited_at: saved.updated_at || saved.created_at || null,
      last_edited_by: saved.owner_id,
      href: objectExplorerShareLink(saved),
      tokens: [
        kind,
        saved.id,
        saved.name,
        saved.description,
        saved.share_slug || "",
        saved.base_object_type_id,
        objectType?.display_name || "",
        objectType ? objectTypeAPIName(objectType) : "",
        JSON.stringify(saved.query_state || {}),
        JSON.stringify(saved.layout || {}),
        ...groups.flatMap((group) => [group.name, group.display_name]),
      ],
      permission: access
        ? {
            can_view: access.can_view_metadata,
            can_view_details: access.can_view_objects,
            schema_only: access.schema_only,
            reason: access.reason,
          }
        : {
            can_view: false,
            can_view_details: false,
            schema_only: false,
            reason: "Saved exploration base object type is not available.",
          },
      source_updated_at: saved.updated_at || saved.created_at || null,
      rank_boost: kind === "saved_exploration" ? 18 : 16,
    }));
  }

  const previousIDs = new Set(previousByID.keys());
  const nextIDs = new Set(documents.map((document) => document.id));
  const reusedDocuments = documents.filter((document) => previousByID.get(document.id) === document).length;
  const changedResourceKeys = documents
    .filter((document) => previousByID.get(document.id) !== document)
    .map((document) => document.id);
  const removedDocuments = [...previousIDs].filter((id) => !nextIDs.has(id)).length;
  const sourceCounts = documents.reduce<Record<string, number>>((counts, document) => {
    counts[document.resource_kind] = (counts[document.resource_kind] || 0) + 1;
    return counts;
  }, {});
  const revision = `${builtAt}:${documents.length}:${changedResourceKeys.length}:${removedDocuments}`;
  return {
    documents: documents.sort((left, right) =>
      left.resource_kind.localeCompare(right.resource_kind) ||
      left.display_name.localeCompare(right.display_name),
    ),
    revision,
    built_at: builtAt,
    incremental: {
      previous_revision: input.previousIndex?.revision || null,
      reused_documents: reusedDocuments,
      upserted_documents: changedResourceKeys.length,
      removed_documents: removedDocuments,
      changed_resource_keys: changedResourceKeys,
      source_counts: sourceCounts,
    },
    facets: {
      resource_kinds: ontologySearchFacet(documents, (document) => document.resource_kind, resourceKindLabelForBundle),
      projects: ontologySearchFacet(documents, (document) => document.project_id || "unplaced", (id) =>
        documents.find((document) => (document.project_id || "unplaced") === id)?.project_display_name || id,
      ),
      groups: ontologySearchFacet(
        documents.flatMap((document) => document.group_ids.map((id, index) => ({
          ...document,
          group_id: id,
          group_name: document.group_names[index] || id,
        }))),
        (document) => (document as OntologyResourceSearchDocument & { group_id: string }).group_id,
        (id) => {
          const match = documents.find((document) => document.group_ids.includes(id));
          const index = match?.group_ids.indexOf(id) ?? -1;
          return index >= 0 ? match?.group_names[index] || id : id;
        },
      ),
    },
  };
}

export function searchOntologyResourceIndex(
  index: OntologyResourceSearchIndex,
  query: OntologyResourceSearchQuery = {},
): OntologyResourceSearchResults {
  const page = Math.max(1, Math.trunc(query.page || 1));
  const perPage = Math.max(1, Math.min(100, Math.trunc(query.per_page || 25)));
  const normalizedQuery = normalizeOntologySearchText(query.query || "");
  const normalizedAPIName = normalizeOntologySearchText(query.api_name || "");
  const terms = uniqueNonEmpty(normalizedQuery.split(/\s+/g));
  const apiNameOnly = query.api_name_only || Boolean(normalizedAPIName && terms.length === 0);
  const permissionFilter = query.permission_filter || "viewable";
  const kindFilter = new Set(query.resource_kinds || []);
  const projectFilter = new Set((query.project_ids || []).filter(Boolean));
  const groupFilter = new Set((query.group_ids || []).filter(Boolean));

  const structurallyFiltered = index.documents.filter((document) => {
    if (kindFilter.size > 0 && !kindFilter.has(document.resource_kind)) return false;
    if (projectFilter.size > 0 && !projectFilter.has(document.project_id || "unplaced")) return false;
    if (groupFilter.size > 0 && !document.group_ids.some((groupID) => groupFilter.has(groupID))) return false;
    if (query.visibility && query.visibility !== "all" && document.visibility !== query.visibility) return false;
    if (query.status && query.status !== "all" && document.status !== query.status) return false;
    if (normalizedAPIName && !normalizeOntologySearchText(`${document.api_name} ${document.resource_id}`).includes(normalizedAPIName)) return false;
    return true;
  });

  const scored = structurallyFiltered
    .map((document) => ontologySearchScore(document, terms, apiNameOnly))
    .filter((entry) => entry.score > 0 || terms.length === 0);
  const hiddenResults = scored.filter((entry) => !entry.document.permission.can_view).length;
  const visible = scored.filter((entry) => {
    if (permissionFilter === "all") return true;
    if (permissionFilter === "hidden") return !entry.document.permission.can_view;
    return entry.document.permission.can_view;
  });
  visible.sort((left, right) =>
    right.score - left.score ||
    ontologySearchTimestamp(right.document.source_updated_at) - ontologySearchTimestamp(left.document.source_updated_at) ||
    left.document.display_name.localeCompare(right.document.display_name),
  );
  const total = visible.length;
  const totalPages = Math.max(1, Math.ceil(total / perPage));
  const safePage = Math.min(page, totalPages);
  const offset = (safePage - 1) * perPage;
  const pageRows = visible.slice(offset, offset + perPage).map(({ document, score, matched_terms, match_reason }) => ({
    ...document,
    score,
    matched_terms,
    match_reason,
  }));
  return {
    data: pageRows,
    total,
    page: safePage,
    per_page: perPage,
    total_pages: totalPages,
    hidden_results: hiddenResults,
    query: query.query || query.api_name || "",
    facets: ontologySearchFacetCounts(scored.map((entry) => entry.document)),
  };
}

function ontologySearchRegistryKey(kind: string, id: string) {
  return `${kind}:${id}`;
}

function ontologySearchDocument(input: Omit<
  OntologyResourceSearchDocument,
  "id" | "token_text" | "indexed_at" | "source_signature"
> & { builtAt: string }): OntologyResourceSearchDocument {
  const { builtAt, ...rest } = input;
  const tokens = uniqueNonEmpty(input.tokens.map(String));
  const tokenText = normalizeOntologySearchText([
    input.resource_kind,
    input.resource_id,
    input.parent_resource_kind || "",
    input.parent_resource_id || "",
    input.api_name,
    input.display_name,
    input.description,
    input.project_display_name,
    input.folder_path,
    input.visibility,
    input.status,
    ...input.group_ids,
    ...input.group_names,
    ...tokens,
  ].join(" "));
  const document: OntologyResourceSearchDocument = {
    ...rest,
    id: ontologyResourceKey(input.resource_kind, input.resource_id),
    tokens,
    token_text: tokenText,
    indexed_at: builtAt,
    source_signature: "",
  };
  document.source_signature = ontologySearchDocumentSignature(document);
  return document;
}

function ontologySearchDocumentSignature(document: OntologyResourceSearchDocument) {
  return JSON.stringify({
    resource_kind: document.resource_kind,
    resource_id: document.resource_id,
    parent_resource_kind: document.parent_resource_kind,
    parent_resource_id: document.parent_resource_id,
    api_name: document.api_name,
    display_name: document.display_name,
    description: document.description,
    project_id: document.project_id,
    folder_path: document.folder_path,
    group_ids: document.group_ids,
    visibility: document.visibility,
    status: document.status,
    branch_state: document.branch_state,
    usage_count: document.usage_count,
    linked_resource_count: document.linked_resource_count,
    last_edited_at: document.last_edited_at,
    permission: document.permission,
    source_updated_at: document.source_updated_at,
    token_text: document.token_text,
  });
}

function ontologySearchGroupsByObjectType(groups: OntologyObjectTypeGroupSummary[]) {
  const out = new Map<string, OntologyObjectTypeGroupSummary[]>();
  for (const group of groups) {
    for (const objectTypeID of group.object_type_ids || []) {
      const list = out.get(objectTypeID) || [];
      list.push(group);
      out.set(objectTypeID, list);
    }
  }
  return out;
}

function ontologySearchPermissionForResource(input: {
  key: string;
  fallbackVisibility?: string | null;
  permissionByKey: Map<string, OntologyResourcePermissionDecision>;
  schemaOnly?: boolean;
}): OntologyResourceSearchPermission {
  const hiddenByVisibility = input.fallbackVisibility === "hidden";
  const decision = input.permissionByKey.get(input.key);
  if (decision) {
    const canView = decision.can_view_definition && !hiddenByVisibility;
    return {
      can_view: canView,
      can_view_details: canView && !input.schemaOnly,
      schema_only: Boolean(input.schemaOnly || (canView && !decision.can_view_instances && decision.resource_kind === "object_type")),
      reason: canView
        ? decision.reasons.join(" ") || "Resource definition is viewable."
        : hiddenByVisibility
          ? "Resource is hidden from ontology search results."
          : decision.reasons.join(" ") || "Resource definition is not viewable.",
    };
  }
  return {
    can_view: !hiddenByVisibility,
    can_view_details: !hiddenByVisibility && !input.schemaOnly,
    schema_only: Boolean(input.schemaOnly),
    reason: hiddenByVisibility ? "Resource is hidden from ontology search results." : "No explicit resource permission restriction was found.",
  };
}

function ontologySearchHrefForRegistryEntry(entry: OntologyResourceRegistryEntry) {
  switch (entry.resource_kind) {
    case "object_type":
      return `/ontology/${encodeURIComponent(entry.resource_id)}`;
    case "link_type":
      return `/object-link-types?linkType=${encodeURIComponent(entry.resource_id)}`;
    case "action_type":
      return `/action-types?action=${encodeURIComponent(entry.resource_id)}`;
    case "interface":
      return `/interfaces?interface=${encodeURIComponent(entry.resource_id)}`;
    case "object_type_group":
      return `/ontology-manager?section=groups&group=${encodeURIComponent(entry.resource_id)}`;
    case "core_object_view":
    case "custom_object_view":
      return `/object-views?view=${encodeURIComponent(entry.resource_id)}`;
    default:
      return "/ontology-manager";
  }
}

function ontologySearchRegistryEntryForUsage(
  reference: OntologyUsageReference,
  registryByKey: Map<string, OntologyResourceRegistryEntry>,
  registryByResourceID: Map<string, OntologyResourceRegistryEntry>,
) {
  if (reference.resource_kind === "object_view") {
    return registryByResourceID.get(reference.resource_id);
  }
  const direct = registryByKey.get(ontologySearchRegistryKey(reference.resource_kind, reference.resource_id));
  if (direct) return direct;
  if (reference.resource_kind === "property") {
    const objectTypeID = reference.resource_id.split(".")[0];
    return registryByKey.get(ontologySearchRegistryKey("object_type", objectTypeID));
  }
  return undefined;
}

function ontologySearchRankBoost(kind: string) {
  switch (kind) {
    case "object_type":
      return 50;
    case "property":
      return 26;
    case "link_type":
    case "action_type":
    case "interface":
      return 32;
    case "object_type_group":
      return 28;
    case "core_object_view":
    case "custom_object_view":
      return 24;
    case "usage_edge":
      return 10;
    case "saved_exploration":
    case "saved_list":
      return 18;
    default:
      return 12;
  }
}

function ontologySearchFacet<T>(
  documents: T[],
  idFor: (document: T) => string,
  labelFor: (id: string) => string,
): OntologyResourceSearchFacet[] {
  const counts = new Map<string, number>();
  for (const document of documents) {
    const id = idFor(document);
    if (!id) continue;
    counts.set(id, (counts.get(id) || 0) + 1);
  }
  return [...counts.entries()]
    .map(([id, count]) => ({ id, label: labelFor(id), count }))
    .sort((left, right) => right.count - left.count || left.label.localeCompare(right.label));
}

function ontologySearchFacetCounts(documents: OntologyResourceSearchDocument[]) {
  const counts = {
    resource_kinds: {} as Record<string, number>,
    projects: {} as Record<string, number>,
    groups: {} as Record<string, number>,
    visibility: {} as Record<string, number>,
  };
  for (const document of documents) {
    counts.resource_kinds[document.resource_kind] = (counts.resource_kinds[document.resource_kind] || 0) + 1;
    const projectID = document.project_id || "unplaced";
    counts.projects[projectID] = (counts.projects[projectID] || 0) + 1;
    counts.visibility[document.visibility] = (counts.visibility[document.visibility] || 0) + 1;
    for (const groupID of document.group_ids) counts.groups[groupID] = (counts.groups[groupID] || 0) + 1;
  }
  return counts;
}

function ontologySearchScore(
  document: OntologyResourceSearchDocument,
  terms: string[],
  apiNameOnly: boolean,
) {
  if (terms.length === 0) {
    return {
      document,
      score: document.rank_boost,
      matched_terms: [] as string[],
      match_reason: "No query filter.",
    };
  }
  let score = document.rank_boost;
  const matched: string[] = [];
  const reasons: string[] = [];
  for (const term of terms) {
    const termScore = ontologySearchTermScore(document, term, apiNameOnly);
    if (termScore.score <= 0) {
      return { document, score: 0, matched_terms: [], match_reason: "No match." };
    }
    score += termScore.score;
    matched.push(term);
    reasons.push(termScore.reason);
  }
  return {
    document,
    score,
    matched_terms: matched,
    match_reason: uniqueNonEmpty(reasons).slice(0, 3).join("; "),
  };
}

function ontologySearchTermScore(
  document: OntologyResourceSearchDocument,
  term: string,
  apiNameOnly: boolean,
) {
  const apiName = normalizeOntologySearchText(document.api_name);
  const resourceID = normalizeOntologySearchText(document.resource_id);
  if (apiName === term || resourceID === term) return { score: 100, reason: "Exact API name or resource ID match." };
  if (apiName.startsWith(term) || resourceID.startsWith(term)) return { score: 76, reason: "API name prefix match." };
  if (apiName.includes(term) || resourceID.includes(term)) return { score: 58, reason: "API name contains match." };
  if (apiNameOnly) return { score: 0, reason: "" };
  const displayName = normalizeOntologySearchText(document.display_name);
  if (displayName === term) return { score: 72, reason: "Exact display name match." };
  if (displayName.includes(term)) return { score: 48, reason: "Display name contains match." };
  for (const token of document.tokens.map(normalizeOntologySearchText)) {
    if (!token) continue;
    if (token === term) return { score: 42, reason: "Token match." };
    if (token.startsWith(term)) return { score: 28, reason: "Token prefix match." };
    if (token.includes(term)) return { score: 18, reason: "Token contains match." };
    if (ontologySearchFuzzyMatch(token, term)) return { score: 9, reason: "Fuzzy token match." };
  }
  return ontologySearchFuzzyMatch(document.token_text, term)
    ? { score: 6, reason: "Fuzzy document match." }
    : { score: 0, reason: "" };
}

function normalizeOntologySearchText(value: string) {
  return value
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[\u0300-\u036f]/g, "")
    .replace(/[^a-z0-9_.:-]+/g, " ")
    .trim();
}

function ontologySearchFuzzyMatch(value: string, term: string) {
  if (term.length < 3 || value.length < 3) return false;
  if (value.includes(term)) return true;
  const tokens = value.split(/\s+/g).filter(Boolean);
  return tokens.some((token) => {
    if (token.length < 3) return false;
    const threshold = term.length <= 5 ? 1 : 2;
    return ontologySearchLevenshtein(token.slice(0, Math.max(token.length, term.length)), term) <= threshold ||
      ontologySearchSubsequence(term, token);
  });
}

function ontologySearchSubsequence(needle: string, haystack: string) {
  let offset = 0;
  for (const char of needle) {
    offset = haystack.indexOf(char, offset);
    if (offset < 0) return false;
    offset += 1;
  }
  return true;
}

function ontologySearchLevenshtein(left: string, right: string) {
  const rows = left.length + 1;
  const cols = right.length + 1;
  const matrix = Array.from({ length: rows }, (_, row) =>
    Array.from({ length: cols }, (_, col) => (row === 0 ? col : col === 0 ? row : 0)),
  );
  for (let row = 1; row < rows; row += 1) {
    for (let col = 1; col < cols; col += 1) {
      const cost = left[row - 1] === right[col - 1] ? 0 : 1;
      matrix[row][col] = Math.min(
        matrix[row - 1][col] + 1,
        matrix[row][col - 1] + 1,
        matrix[row - 1][col - 1] + cost,
      );
    }
  }
  return matrix[left.length][right.length];
}

function ontologySearchTimestamp(value: string | null | undefined) {
  if (!value) return 0;
  const timestamp = Date.parse(value);
  return Number.isNaN(timestamp) ? 0 : timestamp;
}

function defaultReadCount(product: OntologyUsageProduct) {
  return product === "global_branching" ? 0 : 1;
}

function defaultWriteCount(product: OntologyUsageProduct) {
  return product === "functions" || product === "global_branching" ? 1 : 0;
}

const ONTOLOGY_PERMISSION_RANK: Record<OntologyPermissionLevel, number> = {
  none: 0,
  view: 1,
  edit: 2,
  manage: 3,
  owner: 4,
};

export function buildOntologyPermissionAnalysis(input: {
  registry: OntologyResourceRegistryEntry[];
  projects?: OntologyProject[];
  projectMemberships?: OntologyProjectMembership[];
  objectTypes?: ObjectType[];
  linkTypes?: LinkType[];
  actionTypes?: ActionType[];
  interfaces?: OntologyInterface[];
  sharedPropertyTypes?: SharedPropertyType[];
  valueTypes?: OntologyValueType[];
  objectViews?: ObjectViewDefinition[];
  workingChanges?: OntologyStagedChange[];
  principal?: OntologyPermissionPrincipal | null;
}): OntologyPermissionAnalysis {
  const resources = buildOntologyResourcePermissions(input);
  const decisionsByKey = new Map(resources.map((decision) => [decision.resource_key, decision]));
  const changeChecks = (input.workingChanges || []).map((change) =>
    evaluateOntologyChangePermission(change, decisionsByKey, input),
  );
  return {
    resources,
    change_checks: changeChecks,
    blocked_changes: changeChecks.filter((check) => !check.allowed).length,
    totals: {
      resources: resources.length,
      viewable_definitions: resources.filter((resource) => resource.can_view_definition).length,
      viewable_instances: resources.filter((resource) => resource.can_view_instances).length,
      editable: resources.filter((resource) => resource.can_edit).length,
      manageable: resources.filter((resource) => resource.can_manage).length,
      owned: resources.filter((resource) => resource.is_owner).length,
    },
  };
}

export function buildOntologyResourcePermissions(input: {
  registry: OntologyResourceRegistryEntry[];
  projects?: OntologyProject[];
  projectMemberships?: OntologyProjectMembership[];
  objectTypes?: ObjectType[];
  linkTypes?: LinkType[];
  actionTypes?: ActionType[];
  interfaces?: OntologyInterface[];
  sharedPropertyTypes?: SharedPropertyType[];
  valueTypes?: OntologyValueType[];
  objectViews?: ObjectViewDefinition[];
  principal?: OntologyPermissionPrincipal | null;
}): OntologyResourcePermissionDecision[] {
  const out = input.registry.map((entry) => {
    const ownerID = ownerIDForRegistryEntry(entry, input);
    return permissionDecisionForResource({
      resource_key: ontologyResourceKey(entry.resource_kind, entry.resource_id),
      resource_kind: entry.resource_kind,
      resource_id: entry.resource_id,
      display_name: entry.display_name,
      project_id: entry.project_id,
      project_display_name: entry.project_display_name,
      folder_path: entry.folder_path,
      owner_id: ownerID,
      backing_datasource_id: entry.backing_datasource_id,
      object_type: entry.resource_kind === "object_type" ? input.objectTypes?.find((type) => type.id === entry.resource_id) : undefined,
    }, input);
  });
  for (const valueType of input.valueTypes || []) {
    out.push(permissionDecisionForResource({
      resource_key: ontologyResourceKey("value_type", valueType.id),
      resource_kind: "value_type",
      resource_id: valueType.id,
      display_name: valueType.display_name || valueType.name,
      project_id: null,
      project_display_name: valueType.space_id || "Value type space",
      folder_path: `/${valueType.space_id || "default"}/value-types`,
      owner_id: valueType.owner_id,
      backing_datasource_id: null,
    }, input));
  }
  return out.sort((left, right) => `${left.resource_kind}:${left.display_name}`.localeCompare(`${right.resource_kind}:${right.display_name}`));
}

const RESTRICTED_VIEW_CONFIG_PREFIX = "of:ontology:restricted-view:";

export function readRestrictedViewBackingConfig(typeId: string): RestrictedViewBackingConfig | null {
  if (!typeId || typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(`${RESTRICTED_VIEW_CONFIG_PREFIX}${typeId}`);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as RestrictedViewBackingConfig;
    return parsed?.restricted_view_id ? parsed : null;
  } catch {
    return null;
  }
}

export function saveRestrictedViewBackingConfig(typeId: string, config: RestrictedViewBackingConfig | null) {
  if (!typeId || typeof window === "undefined") return;
  try {
    if (!config?.restricted_view_id) {
      window.localStorage.removeItem(`${RESTRICTED_VIEW_CONFIG_PREFIX}${typeId}`);
      return;
    }
    window.localStorage.setItem(`${RESTRICTED_VIEW_CONFIG_PREFIX}${typeId}`, JSON.stringify(config));
  } catch {
    /* local storage is a best-effort implementation detail */
  }
}

export function restrictedViewBackingConfigForObjectType(
  objectType?: ObjectType | null,
  localConfig: RestrictedViewBackingConfig | null = objectType?.id ? readRestrictedViewBackingConfig(objectType.id) : null,
): RestrictedViewBackingConfig | null {
  if (!objectType && !localConfig?.restricted_view_id) return null;
  const payload = (objectType || {}) as ObjectType & Record<string, unknown>;
  const id =
    stringValue(localConfig?.restricted_view_id) ||
    stringValue(payload.backing_restricted_view_id) ||
    stringValue(payload.restricted_view_id) ||
    stringValue(payload.restrictedViewId);
  const declaredRestrictedView =
    payload.backing_datasource_type === "restricted_view" ||
    typeof payload.backing_restricted_view_id === "string" ||
    typeof payload.restricted_view_id === "string" ||
    Boolean(localConfig?.restricted_view_id);
  if (!id || !declaredRestrictedView) return null;
  return {
    restricted_view_id: id,
    backing_dataset_id: stringValue(localConfig?.backing_dataset_id) || objectType?.backing_dataset_id || null,
    storage_mode: localConfig?.storage_mode ?? objectType?.restricted_view_storage_mode ?? "remote",
    policy: localConfig?.policy ?? objectType?.restricted_view_policy ?? null,
    policy_version: numberValue(localConfig?.policy_version ?? objectType?.restricted_view_policy_version) ?? 0,
    registered_policy_version: numberValue(localConfig?.registered_policy_version ?? objectType?.restricted_view_registered_policy_version) ?? 0,
    indexed_policy_version: numberValue(localConfig?.indexed_policy_version ?? objectType?.restricted_view_indexed_policy_version) ?? 0,
    policy_updated_at: localConfig?.policy_updated_at ?? objectType?.restricted_view_policy_updated_at ?? null,
    registered_at: localConfig?.registered_at ?? objectType?.restricted_view_registered_at ?? null,
    indexed_at: localConfig?.indexed_at ?? objectType?.restricted_view_indexed_at ?? null,
    require_reregistration_on_policy_change: localConfig?.require_reregistration_on_policy_change ?? true,
    require_reindex_on_policy_change: localConfig?.require_reindex_on_policy_change ?? true,
  };
}

export function objectTypeWithRestrictedViewConfig(
  objectType: ObjectType,
  config: RestrictedViewBackingConfig | null,
): ObjectType {
  if (!config?.restricted_view_id) {
    return objectType;
  }
  return {
    ...objectType,
    backing_datasource_type: "restricted_view",
    backing_restricted_view_id: config.restricted_view_id,
    restricted_view_id: config.restricted_view_id,
    backing_dataset_id: config.backing_dataset_id ?? objectType.backing_dataset_id ?? null,
    restricted_view_policy: config.policy ?? null,
    restricted_view_policy_version: config.policy_version ?? 0,
    restricted_view_registered_policy_version: config.registered_policy_version ?? 0,
    restricted_view_indexed_policy_version: config.indexed_policy_version ?? 0,
    restricted_view_storage_mode: config.storage_mode ?? "remote",
    restricted_view_policy_updated_at: config.policy_updated_at ?? null,
    restricted_view_registered_at: config.registered_at ?? null,
    restricted_view_indexed_at: config.indexed_at ?? null,
  };
}

export function restrictedViewPolicyPropagationStatus(
  config: RestrictedViewBackingConfig | null | undefined,
): RestrictedViewPropagationStatus {
  const policyVersion = numberValue(config?.policy_version) ?? 0;
  const registeredVersion = numberValue(config?.registered_policy_version) ?? 0;
  const indexedVersion = numberValue(config?.indexed_policy_version) ?? 0;
  const storageMode = config?.storage_mode ?? null;
  const localMode = isLocalRestrictedViewStorageMode(storageMode);
  const requiresReregistration = Boolean(
    config?.restricted_view_id &&
    localMode &&
    config.require_reregistration_on_policy_change !== false &&
    policyVersion > registeredVersion,
  );
  const requiresReindex = Boolean(
    config?.restricted_view_id &&
    localMode &&
    config.require_reindex_on_policy_change !== false &&
    policyVersion > indexedVersion,
  );
  const warnings: string[] = [];
  if (requiresReregistration) {
    warnings.push("Restricted-view policy changed after registration; re-register this object type before relying on local storage reads.");
  }
  if (requiresReindex) {
    warnings.push("Restricted-view policy changed after indexing; re-index local objects so row-level outcomes match the latest policy.");
  }
  return {
    restricted_view_id: config?.restricted_view_id ?? null,
    storage_mode: storageMode,
    policy_version: policyVersion,
    registered_policy_version: registeredVersion,
    indexed_policy_version: indexedVersion,
    requires_reregistration: requiresReregistration,
    requires_reindex: requiresReindex,
    warnings,
  };
}

export function evaluateRestrictedViewRowPolicy(input: {
  object: ObjectInstance;
  objectType?: ObjectType | null;
  config?: RestrictedViewBackingConfig | null;
  principal?: OntologyPermissionPrincipal | null;
  partial?: boolean;
}): RestrictedViewPolicyOutcome {
  const config = input.config ?? restrictedViewBackingConfigForObjectType(input.objectType);
  if (!config?.restricted_view_id) {
    return { restricted_view_id: "", allowed: true, reason: "Object type is not backed by a restricted view.", matched_rules: [] };
  }
  const principal = input.principal || {};
  if (!hasRestrictedViewViewPermission(principal, config)) {
    return {
      restricted_view_id: config.restricted_view_id,
      allowed: false,
      reason: `Restricted view ${config.restricted_view_id} visibility is required before object data can be shown.`,
      matched_rules: [],
    };
  }

  const policy = config.policy ?? null;
  if (!policy) {
    return {
      restricted_view_id: config.restricted_view_id,
      allowed: true,
      reason: "Restricted view grants row access; no local row policy is configured.",
      matched_rules: [],
    };
  }
  const principalGate = principalAllowedByRestrictedViewPolicy(policy, principal);
  if (!principalGate.allowed) {
    return {
      restricted_view_id: config.restricted_view_id,
      allowed: false,
      reason: principalGate.reason,
      matched_rules: [],
    };
  }
  const markingGate = markingAllowedByRestrictedViewPolicy(policy, input.object.marking);
  if (!markingGate.allowed) {
    return {
      restricted_view_id: config.restricted_view_id,
      allowed: false,
      reason: markingGate.reason,
      matched_rules: [],
    };
  }

  const mode = (policy.mode || "any_rule").toLowerCase();
  if (mode === "deny_all") {
    return { restricted_view_id: config.restricted_view_id, allowed: false, reason: "Restricted view row policy denies all rows.", matched_rules: [] };
  }
  const rules = restrictedViewPolicyRules(policy);
  if (mode === "allow_all" || rules.length === 0) {
    return { restricted_view_id: config.restricted_view_id, allowed: true, reason: "Restricted view row policy allows this row.", matched_rules: [] };
  }

  const matches = rules.map((rule, index) => restrictedViewRuleMatches(rule, input.object, principal, Boolean(input.partial), index));
  const runtimeEvaluation = matches.some((match) => match.requires_runtime_evaluation);
  const denyMatch = matches.find((match) => match.matches && match.allow === false);
  if (denyMatch) {
    return {
      restricted_view_id: config.restricted_view_id,
      allowed: false,
      reason: denyMatch.reason || "Restricted view row policy denies this row.",
      matched_rules: [denyMatch.id],
      requires_runtime_evaluation: runtimeEvaluation,
    };
  }
  if (mode === "all_rules") {
    const positive = matches.filter((match) => match.allow !== false);
    const allowed = positive.every((match) => match.matches);
    return {
      restricted_view_id: config.restricted_view_id,
      allowed,
      reason: allowed ? "Restricted view row policy matched all required rules." : "Restricted view row policy did not match every required rule.",
      matched_rules: positive.filter((match) => match.matches).map((match) => match.id),
      requires_runtime_evaluation: runtimeEvaluation,
    };
  }
  const allowMatches = matches.filter((match) => match.allow !== false && match.matches);
  return {
    restricted_view_id: config.restricted_view_id,
    allowed: allowMatches.length > 0,
    reason: allowMatches.length > 0 ? "Restricted view row policy matched an allow rule." : "Restricted view row policy did not allow this row.",
    matched_rules: allowMatches.map((match) => match.id),
    requires_runtime_evaluation: runtimeEvaluation,
  };
}

export function redactObjectInstanceForRestrictedViewPolicy(
  object: ObjectInstance,
  objectType?: ObjectType | null,
  principal?: OntologyPermissionPrincipal | null,
): ObjectInstance {
  const config = restrictedViewBackingConfigForObjectType(objectType);
  if (!config) return redactObjectInstanceForSecurityPolicies(object, { objectType, principal });
  const typePolicy = buildObjectInstanceViewPolicy({ objectType, principal });
  if (!typePolicy.can_view_instances) return redactObjectInstanceForPolicy(object, typePolicy);
  const outcome = evaluateRestrictedViewRowPolicy({ object, objectType, config, principal });
  const policy: ObjectInstanceViewPolicy = {
    ...typePolicy,
    can_view_instances: outcome.allowed,
    schema_only: !outcome.allowed,
    reason: outcome.allowed ? typePolicy.reason : outcome.reason,
    restricted_view_id: config.restricted_view_id,
    restricted_view_policy_outcome: outcome,
  };
  return outcome.allowed
    ? redactObjectInstanceForSecurityPolicies({ ...object, object_view_access: policy }, { objectType, principal })
    : redactObjectInstanceForPolicy(object, policy);
}

export function filterObjectsForRestrictedViewPolicy(
  objects: ObjectInstance[],
  input: {
    objectType?: ObjectType | null;
    objectTypes?: ObjectType[];
    principal?: OntologyPermissionPrincipal | null;
  } = {},
) {
  const typeByID = new Map((input.objectTypes || []).map((objectType) => [objectType.id, objectType]));
  return objects
    .map((object) => {
      const objectType = input.objectType || typeByID.get(object.object_type_id) || null;
      return redactObjectInstanceForRestrictedViewPolicy(object, objectType, input.principal);
    })
    .filter((object) => !object.object_view_access?.schema_only);
}

export function redactObjectViewResponseForRestrictedView(
  response: ObjectViewResponse,
  input: {
    objectType?: ObjectType | null;
    objectTypes?: ObjectType[];
    principal?: OntologyPermissionPrincipal | null;
  } = {},
): ObjectViewResponse {
  const typeByID = new Map((input.objectTypes || []).map((objectType) => [objectType.id, objectType]));
  const objectType = input.objectType || typeByID.get(response.object.object_type_id) || null;
  const object = redactObjectInstanceForRestrictedViewPolicy(response.object, objectType, input.principal);
  if (object.object_view_access?.schema_only) {
    return {
      ...response,
      object,
      summary: {},
      neighbors: [],
      graph: emptySchemaOnlyGraph(response.object.object_type_id, response.object.id),
      applicable_actions: [],
      matching_rules: [],
      recent_rule_runs: [],
      timeline: [],
    };
  }
  const denied = new Set(
    object.object_security_access?.property_decisions
      .filter((decision) => !decision.can_read)
      .map((decision) => decision.property_name) || [],
  );
  const summary = { ...response.summary };
  for (const propertyName of denied) if (propertyName in summary) summary[propertyName] = null;
  return {
    ...response,
    object,
    summary,
    neighbors: redactNeighborLinksForObjectAccess({
      neighbors: response.neighbors,
      objectTypes: input.objectTypes,
      principal: input.principal,
    }),
  };
}

export function redactSearchResultForRestrictedViewAccess(
  result: SearchResult,
  objectType?: ObjectType | null,
  principal?: OntologyPermissionPrincipal | null,
): SearchResult {
  if (result.kind !== "object_instance") return result;
  const config = restrictedViewBackingConfigForObjectType(objectType);
  if (!config) return result;
  const policy = buildObjectInstanceViewPolicy({ objectType, principal });
  if (!policy.can_view_instances) return redactSearchResultForObjectAccess(result, policy);
  const syntheticObject: ObjectInstance = {
    id: result.id,
    object_type_id: result.object_type_id || objectType?.id || "",
    properties: { ...(result.metadata || {}) },
    created_by: "",
    marking: typeof result.metadata?.marking === "string" ? result.metadata.marking : undefined,
    created_at: "",
    updated_at: "",
  };
  const outcome = evaluateRestrictedViewRowPolicy({ object: syntheticObject, objectType, config, principal, partial: true });
  if (outcome.allowed && !outcome.requires_runtime_evaluation) return result;
  return {
    ...result,
    title: "Restricted object",
    subtitle: outcome.requires_runtime_evaluation ? "Restricted view policy requires object-row evaluation" : outcome.reason,
    snippet: "",
    route: result.object_type_id ? `/ontology/${encodeURIComponent(result.object_type_id)}` : "",
    metadata: {},
    score_breakdown: undefined,
  };
}

export function objectSecurityPolicySupportStatus(
  objectType?: ObjectType | null,
): ObjectSecurityPolicySupportStatus {
  const configured = hasAttributeObjectSecurityPolicy(objectType) || propertySecurityPoliciesForObjectType(objectType).length > 0;
  const payload = (objectType || {}) as ObjectType & Record<string, unknown>;
  const support = (payload.object_security_policy_support || payload.security_policy_support || {}) as ObjectSecurityPolicyPrimitiveSupport;
  const supportsAttributeEvaluation = Boolean(payload.security_policy_evaluation_supported || support.object_attribute_evaluation);
  const supportsPropertyPolicies = Boolean(payload.security_policy_evaluation_supported || support.property_policy_evaluation);
  const supportsEditPolicies = Boolean(payload.security_policy_evaluation_supported || support.edit_policy_evaluation);
  const hasFixtures = Boolean(payload.object_security_policy_test_fixtures || payload.security_policy_test_fixtures || support.test_fixtures);
  const warnings: string[] = [];
  const requiresAttributeEvaluation = configured;
  if (configured && !supportsAttributeEvaluation) {
    warnings.push("Object and property security policy enforcement is blocked until OpenFoundry has object-attribute policy evaluation primitives.");
  }
  if (configured && !hasFixtures) {
    warnings.push("Object and property security policy enforcement is blocked until compatible policy fixtures are available.");
  }
  if (propertySecurityPoliciesForObjectType(objectType).length > 0 && !supportsPropertyPolicies) {
    warnings.push("Property security policies are configured, but property-level evaluation is not supported yet.");
  }
  return {
    enforcement_state: configured ? warnings.length > 0 ? "blocked" : "enforced" : "not_configured",
    blocked: configured && warnings.length > 0,
    configured,
    requires_attribute_evaluation: requiresAttributeEvaluation,
    supports_attribute_evaluation: supportsAttributeEvaluation,
    supports_property_policies: supportsPropertyPolicies,
    supports_edit_policies: supportsEditPolicies,
    has_test_fixtures: hasFixtures,
    warnings,
  };
}

export function evaluateObjectAndPropertySecurityPolicies(input: {
  object: ObjectInstance;
  objectType?: ObjectType | null;
  properties?: Property[];
  principal?: OntologyPermissionPrincipal | null;
}): ObjectSecurityPolicyEvaluation {
  const objectPolicy = objectSecurityPolicyDefinitionForObjectType(input.objectType);
  const propertyPolicies = propertySecurityPoliciesForObjectType(input.objectType);
  const support = objectSecurityPolicySupportStatus(input.objectType);
  const warnings = [...support.warnings, ...propertySecurityPolicyConfigurationWarnings(input.objectType, input.properties)];
  if (!objectPolicy && propertyPolicies.length === 0) {
    return {
      enforcement_state: "not_configured",
      blocked: false,
      can_read_object: true,
      reason: "No object or property security policies are configured.",
      warnings,
      object_policy_id: null,
      property_decisions: [],
    };
  }

  const principal = input.principal || {};
  const objectPolicyID = objectPolicy?.id || objectSecurityPolicyID(input.objectType) || null;
  const objectDecision = objectPolicy
    ? evaluatePolicyDefinitionForOperation(objectPolicy, "read", input.object, principal)
    : { allowed: true, reason: "No object security policy is configured.", matched_rules: [] as string[] };
  const canReadObject = support.blocked && objectPolicy ? false : objectDecision.allowed;
  const protectedProperties = propertyPolicies.flatMap((policy) =>
    propertySecurityPolicyNames(policy).map((propertyName) => ({ policy, propertyName })),
  );
  const protectedByName = new Map<string, PropertySecurityPolicyDefinition>();
  for (const entry of protectedProperties) {
    if (!protectedByName.has(entry.propertyName)) protectedByName.set(entry.propertyName, entry.policy);
  }
  const configuredProperties = input.properties?.length
    ? input.properties.map((property) => property.name)
    : Array.from(new Set([...Object.keys(input.object.properties || {}), ...protectedByName.keys()]));
  const propertyDecisions = configuredProperties.map((propertyName) => {
    const propertyPolicy = protectedByName.get(propertyName);
    const policyProperty = isPolicyProperty(input.objectType, propertyName, propertyPolicies);
    if (!propertyPolicy) {
      const editDecision = objectPolicy
        ? evaluatePolicyDefinitionForOperation(objectPolicy, "edit_property", input.object, principal)
        : { allowed: true, reason: "No object edit policy is configured.", matched_rules: [] as string[] };
      const policyEditDecision = objectPolicy
        ? evaluatePolicyDefinitionForOperation(objectPolicy, "edit_policy_property", input.object, principal)
        : { allowed: true, reason: "No object policy-property edit policy is configured.", matched_rules: [] as string[] };
      return {
        property_name: propertyName,
        policy_id: null,
        can_read: canReadObject,
        can_edit_property: canReadObject && !policyProperty && editDecision.allowed,
        can_edit_policy_property: canReadObject && policyProperty && policyEditDecision.allowed,
        policy_property: policyProperty,
        blocked: false,
        reason: canReadObject ? "Property is governed by the object security policy only." : objectDecision.reason,
      };
    }
    if (support.blocked) {
      return {
        property_name: propertyName,
        policy_id: propertyPolicy.id || propertyPolicy.name || null,
        can_read: false,
        can_edit_property: false,
        can_edit_policy_property: false,
        policy_property: policyProperty,
        blocked: true,
        reason: "Property security policy enforcement is blocked until OpenFoundry can evaluate policy attributes with fixtures.",
      };
    }
    const readDecision = evaluatePolicyDefinitionForOperation(propertyPolicy, "read", input.object, principal);
    const editDecision = evaluatePolicyDefinitionForOperation(propertyPolicy, "edit_property", input.object, principal);
    const policyEditDecision = evaluatePolicyDefinitionForOperation(propertyPolicy, "edit_policy_property", input.object, principal);
    return {
      property_name: propertyName,
      policy_id: propertyPolicy.id || propertyPolicy.name || null,
      can_read: canReadObject && readDecision.allowed,
      can_edit_property: canReadObject && readDecision.allowed && editDecision.allowed,
      can_edit_policy_property: canReadObject && readDecision.allowed && policyEditDecision.allowed,
      policy_property: policyProperty,
      blocked: false,
      reason: readDecision.allowed ? "Property security policy allows this property." : readDecision.reason,
    };
  });

  return {
    enforcement_state: support.enforcement_state,
    blocked: support.blocked,
    can_read_object: canReadObject,
    reason: support.blocked
      ? support.warnings[0] || "Object security policy enforcement is blocked."
      : objectDecision.reason,
    warnings,
    object_policy_id: objectPolicyID,
    property_decisions: propertyDecisions,
  };
}

export function redactObjectInstanceForSecurityPolicies(
  object: ObjectInstance,
  input: {
    objectType?: ObjectType | null;
    properties?: Property[];
    principal?: OntologyPermissionPrincipal | null;
  } = {},
): ObjectInstance {
  const evaluation = evaluateObjectAndPropertySecurityPolicies({
    object,
    objectType: input.objectType,
    properties: input.properties,
    principal: input.principal,
  });
  if (evaluation.enforcement_state === "not_configured") return object;
  if (!evaluation.can_read_object) {
    const policy: ObjectInstanceViewPolicy = {
      object_type_id: object.object_type_id,
      access_mode: evaluation.blocked ? "object_policy_blocked" : "object_policy_required",
      can_view_definition: true,
      can_view_instances: false,
      schema_only: true,
      reason: evaluation.reason,
    };
    return {
      ...redactObjectInstanceForPolicy(object, policy),
      object_security_access: evaluation,
    };
  }
  const protectedProperties = new Map(evaluation.property_decisions.map((decision) => [decision.property_name, decision]));
  const properties = { ...(object.properties || {}) };
  for (const [propertyName, decision] of protectedProperties) {
    if (!decision.can_read) properties[propertyName] = null;
  }
  return {
    ...object,
    properties,
    object_security_access: evaluation,
  };
}

export function redactObjectViewResponseForSecurityPolicies(
  response: ObjectViewResponse,
  input: {
    objectType?: ObjectType | null;
    properties?: Property[];
    principal?: OntologyPermissionPrincipal | null;
  } = {},
): ObjectViewResponse {
  const object = redactObjectInstanceForSecurityPolicies(response.object, input);
  if (object.object_view_access?.schema_only) {
    return {
      ...response,
      object,
      summary: {},
      neighbors: [],
      graph: emptySchemaOnlyGraph(response.object.object_type_id, response.object.id),
      applicable_actions: [],
      matching_rules: [],
      recent_rule_runs: [],
      timeline: [],
    };
  }
  const denied = new Set(
    object.object_security_access?.property_decisions
      .filter((decision) => !decision.can_read)
      .map((decision) => decision.property_name) || [],
  );
  if (denied.size === 0) return { ...response, object };
  const summary = { ...response.summary };
  for (const propertyName of denied) if (propertyName in summary) summary[propertyName] = null;
  return { ...response, object, summary };
}

export function redactSearchResultForObjectSecurityAccess(
  result: SearchResult,
  objectType?: ObjectType | null,
  principal?: OntologyPermissionPrincipal | null,
): SearchResult {
  if (result.kind !== "object_instance") return result;
  const syntheticObject: ObjectInstance = {
    id: result.id,
    object_type_id: result.object_type_id || objectType?.id || "",
    properties: { ...(result.metadata || {}) },
    created_by: "",
    marking: typeof result.metadata?.marking === "string" ? result.metadata.marking : undefined,
    created_at: "",
    updated_at: "",
  };
  const evaluation = evaluateObjectAndPropertySecurityPolicies({ object: syntheticObject, objectType, principal });
  if (evaluation.enforcement_state === "not_configured") return result;
  if (!evaluation.can_read_object || evaluation.blocked) {
    return {
      ...result,
      title: "Restricted object",
      subtitle: evaluation.reason,
      snippet: "",
      route: result.object_type_id ? `/ontology/${encodeURIComponent(result.object_type_id)}` : "",
      metadata: {},
      score_breakdown: undefined,
    };
  }
  const denied = new Set(evaluation.property_decisions.filter((decision) => !decision.can_read).map((decision) => decision.property_name));
  if (denied.size === 0) return result;
  const titleProperty = objectTypeTitleProperty(objectType);
  return {
    ...result,
    title: denied.has(titleProperty) ? "Restricted object" : result.title,
    subtitle: "Property values restricted",
    snippet: "",
    metadata: Object.fromEntries(Object.entries(result.metadata || {}).filter(([key]) => !denied.has(key))),
    score_breakdown: undefined,
  };
}

function objectSecurityPolicyDefinitionForObjectType(
  objectType?: ObjectType | null,
): ObjectSecurityPolicyDefinition | null {
  const payload = (objectType || {}) as ObjectType & Record<string, unknown>;
  const policy = payload.object_security_policy || payload.security_policy;
  return policy && typeof policy === "object" ? policy as ObjectSecurityPolicyDefinition : null;
}

function objectSecurityPolicyID(objectType?: ObjectType | null) {
  const payload = (objectType || {}) as ObjectType & Record<string, unknown>;
  return stringValue(payload.object_security_policy_id) || stringValue(payload.security_policy_id);
}

function propertySecurityPoliciesForObjectType(
  objectType?: ObjectType | null,
): PropertySecurityPolicyDefinition[] {
  const payload = (objectType || {}) as ObjectType & Record<string, unknown>;
  const raw = payload.property_security_policies || payload.property_security_policy || payload.property_policies;
  if (Array.isArray(raw)) {
    return raw.filter((entry): entry is PropertySecurityPolicyDefinition => Boolean(entry && typeof entry === "object"));
  }
  return raw && typeof raw === "object" ? [raw as PropertySecurityPolicyDefinition] : [];
}

function hasAttributeObjectSecurityPolicy(objectType?: ObjectType | null) {
  const policy = objectSecurityPolicyDefinitionForObjectType(objectType);
  if (!policy) return false;
  return Boolean(
    policy.granular_policy ||
    policy.read_policy ||
    policy.edit_property_policy ||
    policy.edit_policy_property_policy ||
    restrictedViewPolicyRules(policy).length > 0 ||
    normalizedPolicyList(policy.required_markings, policy.denied_markings, policy.allowed_groups, policy.allowed_roles, policy.required_permissions).length > 0,
  );
}

function propertySecurityPolicyNames(policy: PropertySecurityPolicyDefinition) {
  return normalizedPropertyNameList(policy.property_names, policy.properties);
}

function normalizedPropertyNameList(...values: unknown[]) {
  return values.flatMap((value) => {
    if (Array.isArray(value)) {
      return value.filter((entry): entry is string => typeof entry === "string");
    }
    return typeof value === "string" ? [value] : [];
  }).map((value) => value.trim()).filter(Boolean);
}

function propertySecurityPolicyConfigurationWarnings(
  objectType?: ObjectType | null,
  properties: Property[] = objectTypeProperties(objectType),
) {
  const warnings: string[] = [];
  const policies = propertySecurityPoliciesForObjectType(objectType);
  if (policies.length === 0) return warnings;
  const objectPolicy = objectSecurityPolicyDefinitionForObjectType(objectType) || objectSecurityPolicyID(objectType);
  if (!objectPolicy) warnings.push("Property security policies require an object security policy.");
  const primaryKey = objectTypePrimaryKey(objectType);
  const ownership = new Map<string, string>();
  for (const policy of policies) {
    const policyID = policy.id || policy.name || "property-policy";
    for (const propertyName of propertySecurityPolicyNames(policy)) {
      if (propertyName === primaryKey) {
        warnings.push(`Primary key property ${propertyName} cannot be guarded by a property security policy.`);
      }
      const existing = ownership.get(propertyName);
      if (existing && existing !== policyID) {
        warnings.push(`Property ${propertyName} is assigned to multiple property security policies.`);
      }
      ownership.set(propertyName, policyID);
    }
  }
  for (const propertyName of ownership.keys()) {
    if (properties.length > 0 && !properties.some((property) => property.name === propertyName)) {
      warnings.push(`Property security policy references unknown property ${propertyName}.`);
    }
  }
  return warnings;
}

function isPolicyProperty(
  objectType: ObjectType | null | undefined,
  propertyName: string,
  propertyPolicies: PropertySecurityPolicyDefinition[],
) {
  const properties = objectTypeProperties(objectType);
  const property = properties.find((entry) => entry.name === propertyName);
  if (property?.security_policy_property) return true;
  const objectPolicy = objectSecurityPolicyDefinitionForObjectType(objectType);
  const policyReferenced = objectPolicy ? policyReferencesProperty(objectPolicy, propertyName) : false;
  return policyReferenced || propertyPolicies.some((policy) => policyReferencesProperty(policy, propertyName));
}

function policyReferencesProperty(policy: ObjectSecurityPolicyDefinition, propertyName: string): boolean {
  const policies = [
    policy,
    policy.read_policy,
    policy.granular_policy,
    policy.edit_property_policy,
    policy.edit_policy_property_policy,
  ].filter((entry): entry is RestrictedViewRowPolicy => Boolean(entry));
  return policies.some((entry) =>
    restrictedViewPolicyRules(entry).some((rule) => rule.property === propertyName),
  );
}

function evaluatePolicyDefinitionForOperation(
  policy: ObjectSecurityPolicyDefinition,
  operation: ObjectSecurityPolicyOperation,
  object: ObjectInstance,
  principal: OntologyPermissionPrincipal,
) {
  const operationPolicy = policyForSecurityOperation(policy, operation);
  return evaluatePolicyLikeRowPolicy(operationPolicy, object, principal);
}

function policyForSecurityOperation(
  policy: ObjectSecurityPolicyDefinition,
  operation: ObjectSecurityPolicyOperation,
): RestrictedViewRowPolicy {
  if (operation === "edit_property") {
    return policy.edit_property_policy || policy.read_policy || policy.granular_policy || policy;
  }
  if (operation === "edit_policy_property") {
    return policy.edit_policy_property_policy || policy.edit_property_policy || policy.read_policy || policy.granular_policy || policy;
  }
  return policy.read_policy || policy.granular_policy || policy;
}

function evaluatePolicyLikeRowPolicy(
  policy: RestrictedViewRowPolicy | null | undefined,
  object: ObjectInstance,
  principal: OntologyPermissionPrincipal,
) {
  if (!policy) return { allowed: true, reason: "No security policy is configured.", matched_rules: [] as string[] };
  const principalGate = principalAllowedByRestrictedViewPolicy(policy, principal);
  if (!principalGate.allowed) return { allowed: false, reason: principalGate.reason, matched_rules: [] as string[] };
  const markingGate = markingAllowedByRestrictedViewPolicy(policy, object.marking);
  if (!markingGate.allowed) return { allowed: false, reason: markingGate.reason, matched_rules: [] as string[] };
  const mode = (policy.mode || "any_rule").toLowerCase();
  if (mode === "deny_all") return { allowed: false, reason: "Security policy denies all rows.", matched_rules: [] as string[] };
  const rules = restrictedViewPolicyRules(policy);
  if (mode === "allow_all" || rules.length === 0) return { allowed: true, reason: "Security policy allows this object.", matched_rules: [] as string[] };
  const matches = rules.map((rule, index) => restrictedViewRuleMatches(rule, object, principal, false, index));
  const denyMatch = matches.find((match) => match.matches && match.allow === false);
  if (denyMatch) return { allowed: false, reason: denyMatch.reason || "Security policy denies this object.", matched_rules: [denyMatch.id] };
  if (mode === "all_rules") {
    const positive = matches.filter((match) => match.allow !== false);
    const allowed = positive.every((match) => match.matches);
    return {
      allowed,
      reason: allowed ? "Security policy matched all required rules." : "Security policy did not match every required rule.",
      matched_rules: positive.filter((match) => match.matches).map((match) => match.id),
    };
  }
  const allowMatches = matches.filter((match) => match.allow !== false && match.matches);
  return {
    allowed: allowMatches.length > 0,
    reason: allowMatches.length > 0 ? "Security policy matched an allow rule." : "Security policy did not allow this object.",
    matched_rules: allowMatches.map((match) => match.id),
  };
}

function stringValue(value: unknown) {
  return typeof value === "string" && value.trim().length > 0 ? value.trim() : null;
}

function numberValue(value: unknown) {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && value.trim()) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
}

function isLocalRestrictedViewStorageMode(mode: RestrictedViewStorageMode | null | undefined) {
  const normalized = String(mode || "").toLowerCase();
  return normalized.includes("local") || normalized.includes("object_storage") || normalized.includes("indexed");
}

function hasRestrictedViewViewPermission(
  principal: OntologyPermissionPrincipal,
  config: RestrictedViewBackingConfig,
) {
  if (hasObjectDataPermission(principal, config.restricted_view_id)) return true;
  const id = config.restricted_view_id.toLowerCase();
  const permissions = normalizedPrincipalPermissions(principal);
  return hasAny(permissions, [
    `restricted-view:${id}:view`,
    `restricted_view:${id}:view`,
    `rv:${id}:view`,
    `restricted-view:${id}:read`,
  ]);
}

function principalAllowedByRestrictedViewPolicy(
  policy: RestrictedViewRowPolicy,
  principal: OntologyPermissionPrincipal,
) {
  const identities = principalIdentities(principal);
  const groups = normalizedPrincipalGroups(principal);
  const roles = normalizedPrincipalRoles(principal);
  const permissions = normalizedPrincipalPermissions(principal);
  const users = normalizedPolicyList(policy.allowed_user_ids);
  if (users.length > 0 && !users.some((user) => identities.has(user))) {
    return { allowed: false, reason: "Restricted view row policy does not include this user." };
  }
  const allowedGroups = normalizedPolicyList(policy.allowed_groups);
  if (allowedGroups.length > 0 && !allowedGroups.some((group) => groups.includes(group))) {
    return { allowed: false, reason: "Restricted view row policy does not include this user's groups." };
  }
  const allowedRoles = normalizedPolicyList(policy.allowed_roles);
  if (allowedRoles.length > 0 && !allowedRoles.some((role) => roles.includes(role))) {
    return { allowed: false, reason: "Restricted view row policy does not include this user's roles." };
  }
  const requiredPermissions = normalizedPolicyList(policy.required_permissions);
  if (requiredPermissions.length > 0 && !requiredPermissions.every((permission) => permissions.includes(permission))) {
    return { allowed: false, reason: "Restricted view row policy requires additional permissions." };
  }
  return { allowed: true, reason: "" };
}

function markingAllowedByRestrictedViewPolicy(
  policy: RestrictedViewRowPolicy | RestrictedViewRowPolicyRule,
  marking: string | null | undefined,
) {
  const normalizedMarking = String(marking || "").toLowerCase();
  const denied = normalizedPolicyList(policy.denied_markings);
  if (normalizedMarking && denied.includes(normalizedMarking)) {
    return { allowed: false, reason: `Restricted view row policy denies ${marking} rows.` };
  }
  const required = normalizedPolicyList(policy.required_markings);
  if (required.length > 0 && !required.includes(normalizedMarking)) {
    return { allowed: false, reason: "Restricted view row policy requires a different row marking." };
  }
  return { allowed: true, reason: "" };
}

function restrictedViewPolicyRules(policy: RestrictedViewRowPolicy) {
  return [...(policy.row_rules || []), ...(policy.rules || [])];
}

function restrictedViewRuleMatches(
  rule: RestrictedViewRowPolicyRule,
  object: ObjectInstance,
  principal: OntologyPermissionPrincipal,
  partial: boolean,
  index: number,
) {
  const id = rule.id || `rule-${index + 1}`;
  const principalGate = principalAllowedByRestrictedViewPolicy(rule, principal);
  if (!principalGate.allowed) return { id, matches: false, allow: rule.allow, reason: principalGate.reason, requires_runtime_evaluation: false };
  const markingGate = markingAllowedByRestrictedViewPolicy(rule, object.marking);
  if (!markingGate.allowed) return { id, matches: false, allow: rule.allow, reason: markingGate.reason, requires_runtime_evaluation: false };
  if (rule.operator === "marking_in") {
    const values = normalizedPolicyList(rule.values, rule.value);
    return {
      id,
      matches: values.includes(String(object.marking || "").toLowerCase()),
      allow: rule.allow,
      reason: "",
      requires_runtime_evaluation: false,
    };
  }
  if (!rule.property) {
    return { id, matches: true, allow: rule.allow, reason: "", requires_runtime_evaluation: false };
  }
  const properties = object.properties || {};
  if (!(rule.property in properties)) {
    return {
      id,
      matches: false,
      allow: rule.allow,
      reason: "",
      requires_runtime_evaluation: partial,
    };
  }
  const actual = properties[rule.property];
  return {
    id,
    matches: restrictedViewPropertyMatches(actual, rule),
    allow: rule.allow,
    reason: "",
    requires_runtime_evaluation: false,
  };
}

function restrictedViewPropertyMatches(actual: unknown, rule: RestrictedViewRowPolicyRule) {
  const operator = (rule.operator || "equals").toLowerCase();
  const values = rule.values ?? (rule.value !== undefined ? [rule.value] : []);
  switch (operator) {
    case "exists":
      return actual !== undefined && actual !== null;
    case "not_equals":
    case "neq":
      return !restrictedViewValuesEqual(actual, rule.value);
    case "in":
      return values.some((value) => restrictedViewValuesEqual(actual, value));
    case "not_in":
      return values.every((value) => !restrictedViewValuesEqual(actual, value));
    case "contains":
      return Array.isArray(actual)
        ? actual.some((value) => values.some((candidate) => restrictedViewValuesEqual(value, candidate)))
        : values.some((value) => String(actual ?? "").toLowerCase().includes(String(value ?? "").toLowerCase()));
    case "equals":
    case "eq":
    default:
      return restrictedViewValuesEqual(actual, rule.value);
  }
}

function restrictedViewValuesEqual(left: unknown, right: unknown) {
  if (typeof left === "string" || typeof right === "string") {
    return String(left ?? "").toLowerCase() === String(right ?? "").toLowerCase();
  }
  return left === right;
}

export function buildObjectInstanceViewPolicy(input: {
  objectType?: ObjectType | null;
  resourceDecision?: Pick<OntologyResourcePermissionDecision, "effective_level" | "can_view_definition"> | null;
  principal?: OntologyPermissionPrincipal | null;
}): ObjectInstanceViewPolicy {
  const objectType = input.objectType || null;
  if (!objectType) {
    return {
      object_type_id: "",
      access_mode: "definition_not_viewable",
      can_view_definition: false,
      can_view_instances: false,
      schema_only: false,
      reason: "Object type metadata is unavailable.",
    };
  }
  const principal = input.principal || {};
  const level = input.resourceDecision?.effective_level || inferredObjectTypePermissionLevel(objectType, principal);
  const canViewDefinition = input.resourceDecision?.can_view_definition ?? permissionAtLeast(level, "view");
  const accessMode = objectInstanceAccessMode(
    objectType,
    objectType.backing_dataset_id || objectType.backing_dataset_rid || null,
    canViewDefinition ? level : "none",
    principal,
  );
  const canViewInstances = canViewDefinition && (
    accessMode === "datasource_granted" ||
    accessMode === "object_policy" ||
    accessMode === "restricted_view"
  );
  const restrictedView = restrictedViewBackingConfigForObjectType(objectType);
  return {
    object_type_id: objectType.id,
    access_mode: accessMode,
    can_view_definition: canViewDefinition,
    can_view_instances: canViewInstances,
    schema_only: canViewDefinition && !canViewInstances,
    reason: objectInstanceAccessReason(accessMode),
    restricted_view_id: restrictedView?.restricted_view_id ?? null,
  };
}

export function schemaOnlyObjectInstance(
  objectTypeOrID: Pick<ObjectType, "id"> | string,
  objectId?: string | null,
  policy?: ObjectInstanceViewPolicy | null,
): ObjectInstance {
  const objectTypeID = typeof objectTypeOrID === "string" ? objectTypeOrID : objectTypeOrID.id;
  const access = policy || {
    object_type_id: objectTypeID,
    access_mode: "datasource_required" as const,
    can_view_definition: true,
    can_view_instances: false,
    schema_only: true,
    reason: objectInstanceAccessReason("datasource_required"),
  };
  const now = new Date().toISOString();
  return {
    id: objectId || `schema-only:${objectTypeID}`,
    object_type_id: objectTypeID,
    properties: {},
    created_by: "",
    organization_id: null,
    marking: "schema-only",
    created_at: now,
    updated_at: now,
    object_view_access: access,
  };
}

export function redactObjectInstanceForPolicy(
  object: ObjectInstance,
  policy: ObjectInstanceViewPolicy,
): ObjectInstance {
  if (policy.can_view_instances) return { ...object, object_view_access: policy };
  return {
    ...object,
    properties: {},
    created_by: "",
    organization_id: null,
    marking: policy.schema_only ? "schema-only" : "restricted",
    object_view_access: policy,
  };
}

export function schemaOnlyObjectViewResponse(input: {
  objectType: ObjectType | string;
  objectId?: string | null;
  policy?: ObjectInstanceViewPolicy | null;
}): ObjectViewResponse {
  const objectTypeID = typeof input.objectType === "string" ? input.objectType : input.objectType.id;
  const object = schemaOnlyObjectInstance(objectTypeID, input.objectId, input.policy);
  return {
    object,
    summary: {},
    neighbors: [],
    graph: emptySchemaOnlyGraph(objectTypeID, object.id),
    applicable_actions: [],
    matching_rules: [],
    recent_rule_runs: [],
    timeline: [],
  };
}

export function redactObjectViewResponseForPolicy(
  response: ObjectViewResponse,
  policy: ObjectInstanceViewPolicy,
): ObjectViewResponse {
  if (policy.can_view_instances) {
    return {
      ...response,
      object: { ...response.object, object_view_access: policy },
    };
  }
  return {
    ...response,
    object: redactObjectInstanceForPolicy(response.object, policy),
    summary: {},
    neighbors: [],
    graph: emptySchemaOnlyGraph(response.object.object_type_id, response.object.id),
    applicable_actions: [],
    matching_rules: [],
    recent_rule_runs: [],
    timeline: [],
  };
}

export function buildObjectViewEditPermissionDecision(input: {
  objectType?: ObjectType | null;
  objectView?: ObjectViewDefinition | null;
  config?: ObjectViewConfig | null;
  objectTypeDecision?: Pick<OntologyResourcePermissionDecision, "effective_level" | "can_edit" | "reasons"> | null;
  objectViewDecision?: Pick<OntologyResourcePermissionDecision, "effective_level" | "can_edit" | "can_manage" | "reasons"> | null;
  projectMemberships?: OntologyProjectMembership[];
  principal?: OntologyPermissionPrincipal | null;
}): ObjectViewEditPermissionDecision {
  const objectType = input.objectType || null;
  const objectView = input.objectView || null;
  const config = input.config || objectView?.config || null;
  const principal = input.principal || {};
  const compatibilityMode = objectViewCompatibilityMode(config, objectType);
  const objectTypeLevel = input.objectTypeDecision?.effective_level || (
    objectType ? inferredObjectTypePermissionLevel(objectType, principal) : "none"
  );
  const canEditObjectType = input.objectTypeDecision?.can_edit ?? permissionAtLeast(objectTypeLevel, "edit");
  const objectViewLevel = effectiveObjectViewPermissionLevel({
    objectView,
    config,
    objectViewDecision: input.objectViewDecision,
    projectMemberships: input.projectMemberships || [],
    principal,
  });
  const canEditObjectViewResource = input.objectViewDecision?.can_edit ?? permissionAtLeast(objectViewLevel.level, "edit");
  const hasAdmin = hasObjectViewAdminPermission(principal, objectView?.id || null);
  const canManageObjectView = hasAdmin || (input.objectViewDecision?.can_manage ?? permissionAtLeast(objectViewLevel.level, "manage"));
  const inputDatasourceIds = objectViewInputDatasourceIds(objectType, config);
  const editableInputDatasourceIds = inputDatasourceIds.filter((datasourceID) => hasDatasourceEditPermission(principal, datasourceID));
  const hasDatasourceEditor = inputDatasourceIds.length === 0
    ? hasDatasourceEditPermission(principal, "")
    : editableInputDatasourceIds.length > 0;
  const canEditBase = canEditObjectType || canEditObjectViewResource;
  const requirements: ObjectViewPermissionRequirement[] = [
    {
      id: "object-type-or-object-view-edit",
      kind: "object_type_or_resource_edit",
      label: "Edit object type or Object View resource",
      resource_id: objectType?.id || objectView?.id || null,
      required_level: "edit",
      effective_level: permissionAtLeast(objectTypeLevel, objectViewLevel.level) ? objectTypeLevel : objectViewLevel.level,
      allowed: canEditBase,
      reason: canEditBase
        ? "Ontology roles or project/resource permissions grant Object View editing."
        : "Edit permission on the object type or Object View resource is required.",
    },
  ];

  if (compatibilityMode === "datasource_derived") {
    requirements.push({
      id: "object-view-admin",
      kind: "object_view_admin",
      label: "Object View Admin application permission",
      resource_id: objectView?.id || null,
      required_level: "manage",
      effective_level: objectViewLevel.level,
      allowed: canManageObjectView,
      reason: canManageObjectView
        ? "Object View Admin or manage permission is present."
        : "Datasource-derived Object Views require Object View Admin or manage permission.",
    });
    requirements.push({
      id: "input-datasource-editor",
      kind: "input_datasource_editor",
      label: inputDatasourceIds.length > 0 ? "Editor on at least one input datasource" : "Input datasource editor permission",
      resource_id: inputDatasourceIds.join(",") || null,
      required_level: "edit",
      effective_level: hasDatasourceEditor ? "edit" : "none",
      allowed: hasDatasourceEditor,
      reason: hasDatasourceEditor
        ? "Datasource editor permission is present for an input datasource."
        : inputDatasourceIds.length > 0
        ? "Datasource-derived Object Views require editor permission on at least one input datasource."
        : "Datasource-derived Object Views require a known input datasource or global datasource edit permission.",
    });
  }

  const allowed = requirements.every((requirement) => requirement.allowed);
  const warnings: string[] = [];
  if (compatibilityMode === "datasource_derived" && inputDatasourceIds.length === 0) {
    warnings.push("No input datasource IDs are declared on the Object View config or object type; only global datasource edit permission can satisfy datasource-derived editing.");
  }
  if (!objectType) warnings.push("Object type metadata is unavailable; edit permission cannot be fully evaluated.");

  return {
    object_type_id: objectType?.id || objectView?.object_type_id || "",
    object_view_id: objectView?.id || null,
    compatibility_mode: compatibilityMode,
    allowed,
    can_edit_object_type: canEditObjectType,
    can_edit_object_view_resource: canEditObjectViewResource,
    can_manage_object_view: canManageObjectView,
    can_edit_input_datasources: hasDatasourceEditor,
    input_datasource_ids: inputDatasourceIds,
    editable_input_datasource_ids: editableInputDatasourceIds,
    requirements,
    warnings,
    reason: allowed
      ? "Object View edit permission requirements are satisfied."
      : requirements.find((requirement) => !requirement.allowed)?.reason || "Object View edit permission requirements are not satisfied.",
  };
}

export function redactObjectViewResponseForObjectViewPermissions(
  response: ObjectViewResponse,
  input: {
    objectType?: ObjectType | null;
    objectTypes?: ObjectType[];
    resourceDecision?: Pick<OntologyResourcePermissionDecision, "effective_level" | "can_view_definition"> | null;
    principal?: OntologyPermissionPrincipal | null;
    objectTypeBindings?: ObjectTypeBinding[];
    accessibleDatasourceIds?: string[];
  } = {},
): ObjectViewResponse {
  const typeByID = new Map((input.objectTypes || []).map((objectType) => [objectType.id, objectType]));
  const objectType = input.objectType || typeByID.get(response.object.object_type_id) || null;
  const principal = input.principal || {};
  const policy = buildObjectInstanceViewPolicy({
    objectType,
    resourceDecision: input.resourceDecision,
    principal,
  });
  let next = redactObjectViewResponseForPolicy(response, policy);
  if (next.object.object_view_access?.schema_only) return next;

  next = redactObjectViewResponseForRestrictedView(next, {
    objectType,
    objectTypes: input.objectTypes,
    principal,
  });
  if (next.object.object_view_access?.schema_only) return next;

  next = redactObjectViewResponseForSecurityPolicies(next, {
    objectType,
    properties: objectType?.properties,
    principal,
  });
  if (next.object.object_view_access?.schema_only) return next;

  const bindings = input.objectTypeBindings || [];
  if (bindings.length > 0) {
    const accessibleDatasourceIds = input.accessibleDatasourceIds || Array.from(new Set(
      bindings.flatMap((binding) => [
        binding.dataset_id,
        ...binding.property_mapping.map((mapping) => mapping.datasource_id || binding.dataset_id),
      ]).filter((value): value is string => typeof value === "string" && value.trim().length > 0),
    )).filter((datasourceID) => hasObjectDataPermission(principal, datasourceID));
    const object = maskObjectPropertiesForDatasourceAccess(next.object, bindings, accessibleDatasourceIds);
    const summary = { ...next.summary };
    for (const [propertyName, value] of Object.entries(object.properties || {})) {
      if (value === null && propertyName in summary) summary[propertyName] = null;
    }
    next = { ...next, object, summary };
  }

  return {
    ...next,
    neighbors: redactNeighborLinksForObjectAccess({
      neighbors: next.neighbors,
      objectTypes: input.objectTypes,
      principal,
    }),
  };
}

export function buildObjectViewRuntimePermissionDecision(input: {
  response?: ObjectViewResponse | null;
  objectType?: ObjectType | null;
  objectTypes?: ObjectType[];
  resourceDecision?: Pick<OntologyResourcePermissionDecision, "effective_level" | "can_view_definition"> | null;
  principal?: OntologyPermissionPrincipal | null;
  objectTypeBindings?: ObjectTypeBinding[];
  accessibleDatasourceIds?: string[];
}): ObjectViewRuntimePermissionDecision {
  const response = input.response || null;
  const typeByID = new Map((input.objectTypes || []).map((objectType) => [objectType.id, objectType]));
  const objectType = input.objectType || (response ? typeByID.get(response.object.object_type_id) : null) || null;
  const principal = input.principal || {};
  if (!response) {
    const policy = buildObjectInstanceViewPolicy({
      objectType,
      resourceDecision: input.resourceDecision,
      principal,
    });
    return {
      object_type_id: objectType?.id || "",
      can_view_definition: policy.can_view_definition,
      can_view_instances: policy.can_view_instances,
      schema_only: policy.schema_only,
      object_policy: policy,
      redacted_property_names: [],
      reason: policy.reason,
    };
  }
  const redacted = redactObjectViewResponseForObjectViewPermissions(response, input);
  const policy = redacted.object.object_view_access || buildObjectInstanceViewPolicy({
    objectType,
    resourceDecision: input.resourceDecision,
    principal,
  });
  const originalProperties = response.object.properties || {};
  const redactedProperties = redacted.object.properties || {};
  const redactedPropertyNames = Object.keys(originalProperties).filter((propertyName) => {
    if (!(propertyName in redactedProperties)) return true;
    return originalProperties[propertyName] !== null && redactedProperties[propertyName] === null;
  });
  return {
    object_type_id: objectType?.id || response.object.object_type_id,
    can_view_definition: policy.can_view_definition,
    can_view_instances: policy.can_view_instances,
    schema_only: Boolean(policy.schema_only),
    object_policy: policy,
    redacted_property_names: redactedPropertyNames,
    reason: policy.reason,
  };
}

export function redactNeighborLinksForObjectAccess(input: {
  neighbors: NeighborLink[];
  objectTypes?: ObjectType[];
  principal?: OntologyPermissionPrincipal | null;
}) {
  const objectTypeByID = new Map((input.objectTypes || []).map((objectType) => [objectType.id, objectType]));
  return input.neighbors.map((neighbor) => {
    const objectType = objectTypeByID.get(neighbor.object.object_type_id);
    const policy = buildObjectInstanceViewPolicy({ objectType, principal: input.principal });
    const redacted = redactObjectInstanceForPolicy(neighbor.object, policy);
    return {
      ...neighbor,
      object: redacted.object_view_access?.schema_only
        ? redacted
        : redactObjectInstanceForRestrictedViewPolicy(redacted, objectType, input.principal),
    };
  });
}

export function redactSearchResultForObjectAccess(
  result: SearchResult,
  policy: ObjectInstanceViewPolicy,
): SearchResult {
  if (result.kind !== "object_instance" || policy.can_view_instances) return result;
  return {
    ...result,
    title: policy.can_view_definition ? "Schema-only object" : "Restricted object",
    subtitle: policy.can_view_definition ? "Object values restricted" : "Object type not viewable",
    snippet: "",
    route: policy.can_view_definition && result.object_type_id
      ? `/ontology/${encodeURIComponent(result.object_type_id)}`
      : "",
    metadata: {},
    score_breakdown: undefined,
  };
}

function permissionDecisionForResource(
  resource: {
    resource_key: string;
    resource_kind: string;
    resource_id: string;
    display_name: string;
    project_id: string | null;
    project_display_name: string;
    folder_path: string;
    owner_id: string | null;
    backing_datasource_id?: string | null;
    object_type?: ObjectType;
  },
  input: {
    projectMemberships?: OntologyProjectMembership[];
    principal?: OntologyPermissionPrincipal | null;
  },
): OntologyResourcePermissionDecision {
  const principal = input.principal || {};
  const reasons: string[] = [];
  const level = effectiveOntologyPermissionLevel({
    projectID: resource.project_id,
    ownerID: resource.owner_id,
    memberships: input.projectMemberships || [],
    principal,
    reasons,
  });
  const objectAccess = objectInstanceAccessMode(resource.object_type, resource.backing_datasource_id, level, principal);
  const canViewDefinition = permissionAtLeast(level, "view");
  const canViewInstances =
    objectAccess === "object_policy" ||
    objectAccess === "restricted_view" ||
    objectAccess === "datasource_granted";
  return {
    ...resource,
    effective_level: level,
    can_view_definition: canViewDefinition,
    can_view_instances: canViewInstances,
    can_edit: permissionAtLeast(level, "edit"),
    can_manage: permissionAtLeast(level, "manage"),
    is_owner: permissionAtLeast(level, "owner"),
    object_instance_access: objectAccess,
    reasons,
  };
}

function effectiveOntologyPermissionLevel(input: {
  projectID: string | null;
  ownerID: string | null;
  memberships: OntologyProjectMembership[];
  principal: OntologyPermissionPrincipal;
  reasons: string[];
}): OntologyPermissionLevel {
  const identities = principalIdentities(input.principal);
  const roles = normalizedPrincipalRoles(input.principal);
  const permissions = normalizedPrincipalPermissions(input.principal);
  if (hasAny(roles, ["admin", "owner", "ontology-admin"]) || hasAny(permissions, ["ontology:manage", "projects:manage", "resources:manage"])) {
    input.reasons.push("Global manage role or permission");
    return "owner";
  }
  if (input.ownerID && identities.has(input.ownerID.toLowerCase())) {
    input.reasons.push("Resource owner");
    return "owner";
  }
  const membership = input.projectID
    ? input.memberships.find((entry) => entry.project_id === input.projectID && identities.has(entry.user_id.toLowerCase()))
    : null;
  if (membership) {
    const level = projectRolePermissionLevel(membership.role);
    input.reasons.push(`Project ${membership.role} on ${input.projectID}`);
    return level;
  }
  if (hasAny(roles, ["ontology-editor", "editor"]) || hasAny(permissions, ["ontology:edit", "resources:edit"])) {
    input.reasons.push("Global edit role or permission");
    return "edit";
  }
  if (hasAny(roles, ["ontology-viewer", "viewer"]) || hasAny(permissions, ["ontology:view", "resources:view"])) {
    input.reasons.push("Global view role or permission");
    return "view";
  }
  input.reasons.push("No project membership or direct permission");
  return "none";
}

function objectInstanceAccessMode(
  objectType: ObjectType | undefined,
  backingDatasourceID: string | null | undefined,
  level: OntologyPermissionLevel,
  principal: OntologyPermissionPrincipal,
): OntologyObjectInstanceAccessMode {
  if (!objectType) return "not_applicable";
  if (!permissionAtLeast(level, "view")) return "definition_not_viewable";
  const payload = objectType as ObjectType & {
    object_security_policy?: unknown;
    object_security_policy_id?: string | null;
    security_policy_mode?: string | null;
  };
  if (payload.object_security_policy || payload.object_security_policy_id || payload.security_policy_mode === "object_policy") {
    const support = objectSecurityPolicySupportStatus(objectType);
    if (support.blocked && support.requires_attribute_evaluation) return "object_policy_blocked";
    return hasObjectSecurityPolicyPermission(principal, objectType)
      ? "object_policy"
      : "object_policy_required";
  }
  const restrictedView = restrictedViewBackingConfigForObjectType(objectType);
  if (restrictedView) {
    return hasRestrictedViewViewPermission(principal, restrictedView)
      ? "restricted_view"
      : "restricted_view_required";
  }
  if (!backingDatasourceID && !objectType.backing_dataset_id && !objectType.backing_dataset_rid) {
    return hasObjectDataPermission(principal, "")
      ? "object_policy"
      : "object_policy_required";
  }
  return hasObjectDataPermission(principal, backingDatasourceID || objectType.backing_dataset_id || objectType.backing_dataset_rid || "")
    ? "datasource_granted"
    : "datasource_required";
}

function objectInstanceAccessReason(mode: OntologyObjectInstanceAccessMode) {
  switch (mode) {
    case "definition_not_viewable":
      return "Object type definition is not viewable.";
    case "object_policy":
      return "Object security policy grants access to object data.";
    case "object_policy_blocked":
      return "Object and property security policy enforcement is blocked until OpenFoundry supports object-attribute policy evaluation and compatible fixtures.";
    case "object_policy_required":
      return "Object security policy visibility is required before object data can be shown.";
    case "restricted_view":
      return "Restricted view visibility grants object rows; row-level policy still applies per object.";
    case "restricted_view_required":
      return "Restricted view visibility is required before object data can be shown.";
    case "datasource_required":
      return "Backing datasource view permission is required before object data can be shown.";
    case "datasource_granted":
      return "Backing datasource or object-data permission is present.";
    default:
      return "Object instance data is not applicable for this resource.";
  }
}

function emptySchemaOnlyGraph(objectTypeID: string, objectID: string): GraphResponse {
  return {
    mode: "schema_only",
    root_object_id: objectID,
    root_type_id: objectTypeID,
    depth: 0,
    total_nodes: 0,
    total_edges: 0,
    summary: {
      scope: "schema_only",
      node_kinds: {},
      edge_kinds: {},
      object_types: {},
      markings: {},
      root_neighbor_count: 0,
      max_hops_reached: 0,
      boundary_crossings: 0,
      sensitive_objects: 0,
      sensitive_markings: [],
    },
    nodes: [],
    edges: [],
  };
}

function evaluateOntologyChangePermission(
  change: OntologyStagedChange,
  decisionsByKey: Map<string, OntologyResourcePermissionDecision>,
  context: {
    linkTypes?: LinkType[];
    actionTypes?: ActionType[];
  },
): OntologyPermissionChangeCheck {
  const targets = ontologyEditPermissionTargets(change, context);
  const requirements = targets.map((target) => {
    const key = ontologyResourceKey(target.kind, target.id);
    const decision = decisionsByKey.get(key) || (
      target.kind === "object_view"
        ? [...decisionsByKey.values()].find((candidate) => candidate.resource_id === target.id && candidate.resource_kind.endsWith("object_view"))
        : undefined
    );
    const effectiveLevel = decision?.effective_level || "none";
    return {
      resource_key: decision?.resource_key || key,
      resource_kind: target.kind,
      resource_id: target.id,
      resource_label: decision?.display_name || target.label || target.id,
      required_level: "edit" as const,
      effective_level: effectiveLevel,
      allowed: permissionAtLeast(effectiveLevel, "edit"),
      reason: decision
        ? decision.reasons.join("; ")
        : "Resource is not project-managed or is missing from the permission index",
    };
  });
  return {
    change_id: change.id,
    change_label: change.label,
    change_kind: change.kind,
    allowed: requirements.length > 0 && requirements.every((requirement) => requirement.allowed),
    requirements,
  };
}

function ontologyEditPermissionTargets(
  change: OntologyStagedChange,
  context: { linkTypes?: LinkType[]; actionTypes?: ActionType[] },
): Array<{ kind: string; id: string; label?: string }> {
  const payload = stagedPayload(change);
  const resource = ontologyChangeResource(change);
  const targets: Array<{ kind: string; id: string; label?: string }> = [];
  const add = (kind: string, id: unknown, label?: string) => {
    if (typeof id !== "string" || !id.trim()) return;
    if (targets.some((target) => target.kind === kind && target.id === id.trim())) return;
    targets.push({ kind, id: id.trim(), label });
  };

  if (change.kind === "link_type") {
    const link = context.linkTypes?.find((entry) => entry.id === resource.id);
    add("link_type", resource.id || payload.id, "Link type");
    add("object_type", payload.source_type_id || payload.source_object_type_id || link?.source_type_id, "Source object type");
    add("object_type", payload.target_type_id || payload.target_object_type_id || link?.target_type_id, "Target object type");
    return targets;
  }

  if (change.kind === "action_type") {
    const action = context.actionTypes?.find((entry) => entry.id === resource.id);
    const operationKind = String(payload.operation_kind || action?.operation_kind || "");
    add("action_type", resource.id || payload.id, "Action type");
    add("object_type", payload.object_type_id || action?.object_type_id, "Edited object type");
    add("interface", payload.interface_id || action?.interface_id, "Edited interface");
    if (operationKind.includes("link")) add("link_type", payload.link_type_id || payload.linkTypeId, "Edited link type");
    for (const edited of editedResourceTargets(payload)) add(edited.kind, edited.id, "Edited resource");
    return targets;
  }

  if (change.kind === "property") {
    const propertyTargetID = typeof resource.id === "string" ? resource.id : "";
    const objectTypeID =
      payload.object_type_id ||
      (change.parentRef?.kind === "object_type" ? change.parentRef.id : null) ||
      (propertyTargetID.includes(".") ? propertyTargetID.split(".")[0] : null);
    add("object_type", objectTypeID, "Parent object type");
    return targets;
  }

  if (change.kind === "object_type_binding" || change.kind === "datasource_registration") {
    const bindingID = resource.id || payload.id;
    const objectTypeID =
      payload.object_type_id ||
      payload.objectTypeId ||
      (typeof bindingID === "string" && bindingID.includes(":") ? bindingID.split(":")[0] : null);
    add("datasource_registration", bindingID, "Datasource registration");
    add("object_type", objectTypeID, "Mapped object type");
    return targets;
  }

  if (change.kind === "object_view") {
    add("object_view", resource.id || payload.id, "Object View");
    add("object_type", payload.object_type_id, "Viewed object type");
    return targets;
  }

  add(change.kind, resource.id || payload.id, resource.kind);
  add("object_type", payload.object_type_id, "Object type");
  return targets;
}

function editedResourceTargets(payload: Record<string, unknown>) {
  const raw = payload.edited_resources || payload.edited_resource_targets || payload.required_edit_resources;
  if (!Array.isArray(raw)) return [];
  return raw
    .map((entry) => {
      if (!entry || typeof entry !== "object") return null;
      const typed = entry as Record<string, unknown>;
      const kind = typed.kind || typed.resource_kind;
      const id = typed.id || typed.resource_id;
      return typeof kind === "string" && typeof id === "string" ? { kind, id } : null;
    })
    .filter((entry): entry is { kind: string; id: string } => Boolean(entry));
}

function ownerIDForRegistryEntry(
  entry: OntologyResourceRegistryEntry,
  input: {
    objectTypes?: ObjectType[];
    linkTypes?: LinkType[];
    actionTypes?: ActionType[];
    interfaces?: OntologyInterface[];
    sharedPropertyTypes?: SharedPropertyType[];
    objectViews?: ObjectViewDefinition[];
  },
) {
  switch (entry.resource_kind) {
    case "object_type":
    case "datasource_registration":
      return input.objectTypes?.find((item) => item.id === entry.resource_id || entry.resource_id.startsWith(`${item.id}:`))?.owner_id || entry.last_edited_by;
    case "link_type":
      return input.linkTypes?.find((item) => item.id === entry.resource_id)?.owner_id || entry.last_edited_by;
    case "action_type":
      return input.actionTypes?.find((item) => item.id === entry.resource_id)?.owner_id || entry.last_edited_by;
    case "interface":
      return input.interfaces?.find((item) => item.id === entry.resource_id)?.owner_id || entry.last_edited_by;
    case "shared_property_type":
      return input.sharedPropertyTypes?.find((item) => item.id === entry.resource_id)?.owner_id || entry.last_edited_by;
    case "core_object_view":
    case "custom_object_view":
      return input.objectViews?.find((item) => item.id === entry.resource_id)?.owner_id || entry.last_edited_by;
    default:
      return entry.last_edited_by;
  }
}

function objectCommentActorID(principal: OntologyPermissionPrincipal) {
  return principal.user_id || principal.email || "anonymous";
}

function objectCommentCanManage(principal: OntologyPermissionPrincipal) {
  const roles = normalizedPrincipalRoles(principal);
  const permissions = normalizedPrincipalPermissions(principal);
  return hasAny(roles, ["admin", "owner", "ontology-admin", "object-comment-admin"]) ||
    hasAny(permissions, [
      "object-comments:manage",
      "object-comments:delete",
      "ontology:manage",
      "resources:manage",
    ]);
}

function objectCommentNotifications(
  thread: ObjectCommentThread,
  comment: ObjectCommentEntry,
): ObjectCommentNotification[] {
  return comment.mentions
    .filter((mention) => mention.id !== comment.author_id.toLowerCase())
    .map((mention) => ({
      id: `${comment.id}:notification:${mention.id}`,
      recipient_id: mention.id,
      channel: "in_app" as const,
      title: `Mention on ${thread.object_type_id}`,
      body: `${comment.author_display_name} mentioned you on object ${thread.object_id}.`,
      object_type_id: thread.object_type_id,
      object_id: thread.object_id,
      thread_id: thread.id,
      comment_id: comment.id,
    }));
}

function objectCommentActivity(
  thread: ObjectCommentThread,
  kind: ObjectCommentActivityKind,
  actorID: string,
  timestamp: string,
  commentID: string | null,
  message: string,
): ObjectCommentActivityEvent {
  return {
    id: `${thread.id}:activity:${thread.activity.length + 1}:${kind}`,
    kind,
    actor_id: actorID,
    comment_id: commentID,
    timestamp,
    message,
  };
}

function safeObjectCommentIDPart(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 32) || "file";
}

function objectViewCompatibilityMode(
  config?: ObjectViewConfig | null,
  objectType?: ObjectType | null,
): ObjectViewCompatibilityMode {
  const configRecord = (config || {}) as ObjectViewConfig & Record<string, unknown>;
  const metadataRecord = (config?.metadata || {}) as Record<string, unknown>;
  const typeRecord = (objectType || {}) as ObjectType & Record<string, unknown>;
  const raw =
    config?.compatibility_mode ||
    metadataRecord.compatibility_mode ||
    configRecord.permission_model ||
    configRecord.authorization_model ||
    typeRecord.object_view_compatibility_mode ||
    typeRecord.permission_model ||
    typeRecord.authorization_model;
  const normalized = typeof raw === "string" ? raw.trim().toLowerCase().replace(/[-\s]+/g, "_") : "";
  return normalized === "datasource_derived" || normalized === "datasource" || normalized === "legacy_datasource"
    ? "datasource_derived"
    : "native";
}

function objectViewInputDatasourceIds(
  objectType?: ObjectType | null,
  config?: ObjectViewConfig | null,
) {
  const configRecord = (config || {}) as ObjectViewConfig & Record<string, unknown>;
  const metadataRecord = (config?.metadata || {}) as Record<string, unknown>;
  const typeRecord = (objectType || {}) as ObjectType & Record<string, unknown>;
  return Array.from(new Set([
    ...stringArrayFromUnknown(config?.input_datasource_ids),
    ...stringArrayFromUnknown(metadataRecord.input_datasource_ids),
    ...stringArrayFromUnknown(configRecord.input_datasources),
    ...stringArrayFromUnknown(configRecord.datasource_ids),
    stringFromUnknown(configRecord.input_datasource_id),
    stringFromUnknown(configRecord.datasource_id),
    objectType?.backing_dataset_id,
    objectType?.backing_dataset_rid,
    objectType?.backing_restricted_view_id,
    objectType?.restricted_view_id,
    stringFromUnknown(typeRecord.backing_datasource_id),
    stringFromUnknown(typeRecord.backing_datasource_rid),
  ].filter((value): value is string => typeof value === "string" && value.trim().length > 0)
    .map((value) => value.trim())));
}

function effectiveObjectViewPermissionLevel(input: {
  objectView?: ObjectViewDefinition | null;
  config?: ObjectViewConfig | null;
  objectViewDecision?: Pick<OntologyResourcePermissionDecision, "effective_level" | "reasons"> | null;
  projectMemberships: OntologyProjectMembership[];
  principal: OntologyPermissionPrincipal;
}) {
  if (input.objectViewDecision?.effective_level) {
    return {
      level: input.objectViewDecision.effective_level,
      reasons: input.objectViewDecision.reasons || [],
    };
  }
  const viewRecord = (input.objectView || {}) as ObjectViewDefinition & Record<string, unknown>;
  const configRecord = (input.config || {}) as ObjectViewConfig & Record<string, unknown>;
  const reasons: string[] = [];
  const level = effectiveOntologyPermissionLevel({
    projectID: stringFromUnknown(viewRecord.project_id) || stringFromUnknown(configRecord.project_id) || null,
    ownerID: input.objectView?.owner_id || stringFromUnknown(viewRecord.created_by) || null,
    memberships: input.projectMemberships,
    principal: input.principal,
    reasons,
  });
  return { level, reasons };
}

function hasObjectViewAdminPermission(
  principal: OntologyPermissionPrincipal,
  objectViewID?: string | null,
) {
  const roles = normalizedPrincipalRoles(principal);
  const permissions = normalizedPrincipalPermissions(principal);
  const id = objectViewID?.trim().toLowerCase();
  const scopedPermissions = id ? [
    `object-view:${id}:manage`,
    `object-view:${id}:admin`,
    `object-views:${id}:manage`,
  ] : [];
  return hasAny(roles, ["admin", "owner", "ontology-admin", "object-view-admin", "object-views-admin"]) ||
    hasAny(permissions, [
      "object-views:admin",
      "object-views:manage",
      "object-view:admin",
      "object-view:manage",
      "ontology:manage",
      "resources:manage",
      ...scopedPermissions,
    ]);
}

function hasDatasourceEditPermission(principal: OntologyPermissionPrincipal, datasourceID: string) {
  const roles = normalizedPrincipalRoles(principal);
  const permissions = normalizedPrincipalPermissions(principal);
  if (hasAny(roles, ["admin", "owner", "data-editor", "datasource-editor", "dataset-editor"])) return true;
  const id = datasourceID.trim().toLowerCase();
  const scopedPermissions = id ? [
    `datasource:${id}:edit`,
    `datasource:${id}:manage`,
    `dataset:${id}:edit`,
    `dataset:${id}:manage`,
    `data-source:${id}:edit`,
    `data-source:${id}:manage`,
  ] : [];
  return hasAny(permissions, [
    "datasources:edit",
    "datasources:manage",
    "datasets:edit",
    "datasets:manage",
    "data-sources:edit",
    "data-sources:manage",
    "resources:edit",
    "resources:manage",
    ...scopedPermissions,
  ]);
}

function stringArrayFromUnknown(value: unknown) {
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is string => typeof item === "string" && item.trim().length > 0);
}

function stringFromUnknown(value: unknown) {
  return typeof value === "string" && value.trim().length > 0 ? value.trim() : null;
}

function inferredObjectTypePermissionLevel(
  objectType: ObjectType,
  principal: OntologyPermissionPrincipal,
): OntologyPermissionLevel {
  const identities = principalIdentities(principal);
  const roles = normalizedPrincipalRoles(principal);
  const permissions = normalizedPrincipalPermissions(principal);
  if (hasAny(roles, ["admin", "owner", "ontology-admin"]) || hasAny(permissions, ["ontology:manage", "projects:manage", "resources:manage"])) {
    return "owner";
  }
  if (objectType.owner_id && identities.has(objectType.owner_id.toLowerCase())) {
    return "owner";
  }
  if (hasAny(roles, ["ontology-editor", "editor"]) || hasAny(permissions, ["ontology:edit", "resources:edit"])) {
    return "edit";
  }
  if (
    objectType.visibility !== "hidden" ||
    hasAny(roles, ["ontology-viewer", "viewer"]) ||
    hasAny(permissions, ["ontology:view", "resources:view"])
  ) {
    return "view";
  }
  return "none";
}

function projectRolePermissionLevel(role: string): OntologyPermissionLevel {
  switch (role) {
    case "owner":
      return "manage";
    case "editor":
      return "edit";
    case "viewer":
      return "view";
    default:
      return "none";
  }
}

function permissionAtLeast(actual: OntologyPermissionLevel, required: OntologyPermissionLevel) {
  return ONTOLOGY_PERMISSION_RANK[actual] >= ONTOLOGY_PERMISSION_RANK[required];
}

function principalIdentities(principal: OntologyPermissionPrincipal) {
  return new Set(
    [principal.user_id, principal.email]
      .filter((value): value is string => typeof value === "string" && value.trim().length > 0)
      .map((value) => value.trim().toLowerCase()),
  );
}

function normalizedPrincipalRoles(principal: OntologyPermissionPrincipal) {
  return (principal.roles || []).map((role) => role.trim().toLowerCase());
}

function normalizedPrincipalGroups(principal: OntologyPermissionPrincipal) {
  return (principal.groups || []).map((group) => group.trim().toLowerCase());
}

function normalizedPrincipalPermissions(principal: OntologyPermissionPrincipal) {
  return (principal.permissions || []).map((permission) => permission.trim().toLowerCase());
}

function hasAny(values: string[], candidates: string[]) {
  return candidates.some((candidate) => values.includes(candidate));
}

function hasObjectDataPermission(principal: OntologyPermissionPrincipal, datasourceID: string) {
  const roles = normalizedPrincipalRoles(principal);
  const permissions = normalizedPrincipalPermissions(principal);
  if (hasAny(roles, ["admin", "owner", "data-viewer", "object-viewer"])) return true;
  const datasourcePermission = datasourceID ? `datasource:${datasourceID.toLowerCase()}:view` : "";
  return hasAny(permissions, [
    "objects:view",
    "object-instances:view",
    "ontology:objects:view",
    "datasources:view",
    datasourcePermission,
  ]);
}

function hasObjectSecurityPolicyPermission(
  principal: OntologyPermissionPrincipal,
  objectType: ObjectType,
) {
  if (hasObjectDataPermission(principal, "")) return true;
  const payload = objectType as ObjectType & {
    object_security_policy?: unknown;
    object_security_policy_id?: string | null;
    security_policy_id?: string | null;
    restricted_view_id?: string | null;
  };
  const permissions = normalizedPrincipalPermissions(principal);
  const policyIDs = [
    payload.object_security_policy_id,
    payload.security_policy_id,
    payload.restricted_view_id,
  ].filter((value): value is string => typeof value === "string" && value.trim().length > 0);
  for (const id of policyIDs) {
    const normalized = id.trim().toLowerCase();
    if (hasAny(permissions, [
      `object-policy:${normalized}:view`,
      `object-security-policy:${normalized}:view`,
      `restricted-view:${normalized}:view`,
      `policy:${normalized}:view`,
    ])) {
      return true;
    }
  }
  return objectSecurityPolicyAllowsPrincipal(payload.object_security_policy, principal);
}

function objectSecurityPolicyAllowsPrincipal(
  policy: unknown,
  principal: OntologyPermissionPrincipal,
) {
  if (!policy || typeof policy !== "object") return false;
  const record = policy as Record<string, unknown>;
  const identities = principalIdentities(principal);
  const roles = normalizedPrincipalRoles(principal);
  const groups = normalizedPrincipalGroups(principal);
  const permissions = normalizedPrincipalPermissions(principal);
  const userValues = normalizedPolicyList(record.viewers, record.viewer_ids, record.users, record.user_ids, record.allowed_users);
  if (userValues.some((value) => identities.has(value))) return true;
  const roleValues = normalizedPolicyList(record.roles, record.role_ids, record.allowed_roles);
  if (roleValues.some((value) => roles.includes(value))) return true;
  const groupValues = normalizedPolicyList(record.groups, record.group_ids, record.allowed_groups);
  if (groupValues.some((value) => groups.includes(value))) return true;
  const permissionValues = normalizedPolicyList(record.permissions, record.permission_keys, record.allowed_permissions);
  return permissionValues.some((value) => permissions.includes(value));
}

function normalizedPolicyList(...values: unknown[]) {
  return values.flatMap((value) => {
    if (Array.isArray(value)) {
      return value.filter((item): item is string => typeof item === "string");
    }
    return typeof value === "string" ? [value] : [];
  }).map((value) => value.trim().toLowerCase()).filter(Boolean);
}

export interface OntologyBranch {
  id: string;
  project_id: string;
  name: string;
  description: string;
  status: "main" | "draft" | "in_review" | "rebasing" | "merged" | "closed";
  proposal_id: string | null;
  changes: OntologyStagedChange[];
  conflict_resolutions: Record<string, "main" | "branch" | "custom">;
  enable_indexing: boolean;
  created_by: string;
  created_at: string;
  updated_at: string;
  latest_rebased_at: string;
}

export interface OntologyProposalTask {
  id: string;
  change_id: string;
  title: string;
  description: string;
  status: "pending" | "approved" | "rejected";
  reviewer_id: string | null;
  comments: string[];
}

export interface OntologyProposalComment {
  id: string;
  author: string;
  body: string;
  created_at: string;
}

export interface OntologyProposal {
  id: string;
  project_id: string;
  branch_id: string;
  title: string;
  description: string;
  status: "draft" | "in_review" | "approved" | "merged" | "closed";
  reviewer_ids: string[];
  tasks: OntologyProposalTask[];
  comments: OntologyProposalComment[];
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface OntologyProjectFolder {
  id: string;
  rid: string;
  project_id: string;
  project_rid: string;
  parent_folder_id: string | null;
  parent_folder_rid: string;
  space_rid: string;
  type: "FOLDER";
  trash_status: "DIRECTLY_TRASHED" | "ANCESTOR_TRASHED" | "NOT_TRASHED";
  inherits_project_policies: boolean;
  policy_overrides_allowed: boolean;
  propagate_view_requirements_enabled?: boolean;
  propagate_view_requirements_disabled_at?: string | null;
  view_requirement_marking_rids?: string[];
  name: string;
  slug: string;
  description: string;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export function listSharedPropertyTypes(params?: {
  page?: number;
  per_page?: number;
  search?: string;
}) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  if (params?.search) qs.set("search", params.search);
  return api.get<{
    data: SharedPropertyType[];
    total: number;
    page: number;
    per_page: number;
  }>(`/ontology/shared-property-types?${qs}`);
}

export function listObjectTypeGroups(params?: {
  page?: number;
  per_page?: number;
  search?: string;
}) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  if (params?.search) qs.set("search", params.search);
  return api.get<{
    data: OntologyObjectTypeGroup[];
    total: number;
    page: number;
    per_page: number;
  }>(`/ontology/object-type-groups?${qs}`);
}

export function createObjectTypeGroup(body: {
  name: string;
  display_name?: string;
  description?: string;
  visibility?: string;
  status?: string;
  project_id?: string | null;
  object_type_ids?: string[];
}) {
  return api.post<OntologyObjectTypeGroup>("/ontology/object-type-groups", body);
}

export function updateObjectTypeGroup(
  id: string,
  body: {
    name?: string;
    display_name?: string;
    description?: string;
    visibility?: string;
    status?: string;
    project_id?: string | null;
    object_type_ids?: string[];
  },
) {
  return api.patch<OntologyObjectTypeGroup>(
    `/ontology/object-type-groups/${id}`,
    body,
  );
}

export function deleteObjectTypeGroup(id: string) {
  return api.delete(`/ontology/object-type-groups/${id}`);
}

export function addObjectTypeToGroup(groupId: string, objectTypeId: string) {
  return api.post<OntologyObjectTypeGroup>(
    `/ontology/object-type-groups/${groupId}/object-types/${objectTypeId}`,
    {},
  );
}

export function removeObjectTypeFromGroup(groupId: string, objectTypeId: string) {
  return api.delete(
    `/ontology/object-type-groups/${groupId}/object-types/${objectTypeId}`,
  );
}

export function listProjectResources(id: string) {
  return api
    .get<{
      data: OntologyProjectResourceBinding[];
    }>(`/ontology/projects/${id}/resources`)
    .then((r) => r.data);
}

export function getProjectWorkingState(id: string) {
  return api.get<OntologyProjectWorkingState>(
    `/ontology/projects/${id}/working-state`,
  );
}

export function listProjectSavedChanges(id: string) {
  return api
    .get<{ data: OntologySavedChangeRecord[] }>(
      `/ontology/projects/${id}/saved-changes`,
    )
    .then((r) => r.data);
}

export function saveProjectOntologyChanges(
  id: string,
  body: {
    change_ids?: string[];
    branch_id?: string | null;
    proposal_id?: string | null;
    note?: string;
  } = {},
) {
  return api.post<SaveOntologyChangesResponse>(
    `/ontology/projects/${id}/save-changes`,
    body,
  );
}

export function listProjectBranches(id: string) {
  return api
    .get<{ data: OntologyBranch[] }>(`/ontology/projects/${id}/branches`)
    .then((r) => r.data);
}

export function createProjectBranch(
  id: string,
  body: {
    name: string;
    description?: string;
    changes: OntologyStagedChange[];
    enable_indexing?: boolean;
  },
) {
  return api.post<OntologyBranch>(`/ontology/projects/${id}/branches`, body);
}

export function updateProjectBranch(
  id: string,
  branchId: string,
  body: {
    description?: string;
    status?: OntologyBranch["status"];
    proposal_id?: string | null;
    changes?: OntologyStagedChange[];
    conflict_resolutions?: Record<string, "main" | "branch" | "custom">;
    enable_indexing?: boolean;
    latest_rebased_at?: string;
  },
) {
  return api.patch<OntologyBranch>(
    `/ontology/projects/${id}/branches/${branchId}`,
    body,
  );
}

export function listProjectProposals(id: string) {
  return api
    .get<{ data: OntologyProposal[] }>(`/ontology/projects/${id}/proposals`)
    .then((r) => r.data);
}

export function createProjectProposal(
  id: string,
  body: {
    branch_id: string;
    title: string;
    description?: string;
    reviewer_ids?: string[];
    tasks: OntologyProposalTask[];
    comments?: OntologyProposalComment[];
  },
) {
  return api.post<OntologyProposal>(`/ontology/projects/${id}/proposals`, body);
}

export function updateProjectProposal(
  id: string,
  proposalId: string,
  body: {
    title?: string;
    description?: string;
    status?: OntologyProposal["status"];
    reviewer_ids?: string[];
    tasks?: OntologyProposalTask[];
    comments?: OntologyProposalComment[];
  },
) {
  return api.patch<OntologyProposal>(
    `/ontology/projects/${id}/proposals/${proposalId}`,
    body,
  );
}

export function listProjectMigrations(id: string) {
  return api
    .get<{
      data: OntologyProjectMigration[];
    }>(`/ontology/projects/${id}/migrations`)
    .then((r) => r.data);
}

export function createProjectMigration(
  id: string,
  body: {
    source_project_id: string;
    target_project_id: string;
    resources: OntologyProjectMigration["resources"];
    note?: string;
  },
) {
  return api.post<OntologyProjectMigration>(
    `/ontology/projects/${id}/migrations`,
    body,
  );
}

// ────────────────────────────────────────────────────────────────
// Function packages — used by /functions, /object-monitors, /ml.
// ────────────────────────────────────────────────────────────────

export interface FunctionCapabilities {
  allow_ontology_read: boolean;
  allow_ontology_write: boolean;
  allow_ai: boolean;
  allow_network: boolean;
  timeout_seconds: number;
  max_source_bytes: number;
}

export interface FunctionPackageSummary {
  id: string;
  name: string;
  version: string;
  display_name: string;
  runtime: string;
  entrypoint: string;
  capabilities: FunctionCapabilities;
}

export interface FunctionPackage {
  id: string;
  name: string;
  version: string;
  display_name: string;
  description: string;
  runtime: string;
  source: string;
  entrypoint: string;
  capabilities: FunctionCapabilities;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

export interface FunctionAuthoringTemplate {
  id: string;
  runtime: string;
  display_name: string;
  description: string;
  entrypoint: string;
  starter_source: string;
  default_capabilities: FunctionCapabilities;
  recommended_use_cases: string[];
  cli_scaffold_template: string | null;
  sdk_packages: string[];
}

export interface FunctionSDKPackageReference {
  language: string;
  path: string;
  package_name: string;
  generated_by: string;
}

export interface FunctionAuthoringSurface {
  templates: FunctionAuthoringTemplate[];
  sdk_packages: FunctionSDKPackageReference[];
  cli_commands: string[];
}

export interface FunctionPackageRun {
  id: string;
  function_package_id: string;
  function_package_name: string;
  function_package_version: string;
  runtime: string;
  status: "success" | "failure" | string;
  invocation_kind: "simulation" | "action" | string;
  action_id: string | null;
  action_name: string | null;
  object_type_id: string | null;
  target_object_id: string | null;
  actor_id: string;
  duration_ms: number;
  error_message: string | null;
  started_at: string;
  completed_at: string;
}

export interface FunctionPackageMetrics {
  package: FunctionPackageSummary;
  total_runs: number;
  successful_runs: number;
  failed_runs: number;
  simulation_runs: number;
  action_runs: number;
  success_rate: number;
  avg_duration_ms: number | null;
  p95_duration_ms: number | null;
  max_duration_ms: number | null;
  last_run_at: string | null;
  last_success_at: string | null;
  last_failure_at: string | null;
}

export interface FunctionPackageInvocationBody {
  object_type_id?: string;
  target_object_id?: string;
  parameters?: Record<string, unknown>;
  justification?: string;
}

export interface ValidateFunctionPackageResponse {
  valid: boolean;
  package: FunctionPackageSummary;
  preview: unknown;
  errors: string[];
}

export interface SimulateFunctionPackageResponse {
  package: FunctionPackageSummary;
  preview: unknown;
  result: unknown;
}

export interface LogicFunctionInvocationResponse {
  function: {
    id: string;
    function_rid: string;
    name: string;
    published_version_id: string;
  };
  invocation_surface: string;
  status: string;
  inputs: Record<string, unknown>;
  outputs: unknown;
}

export interface LogicRunHistoryEntry {
  id: string;
  logic_file_id: string;
  published_version_id: string;
  function_rid: string;
  actor_id: string;
  execution_mode: "user_scoped" | "project_scoped" | string;
  permission_subject_kind: "user" | "project" | string;
  permission_subject_id: string;
  invocation_surface: string;
  status: "succeeded" | "failed" | string;
  inputs: Record<string, unknown>;
  outputs: unknown;
  error_message?: string | null;
  logs: unknown[];
  duration_ms: number;
  retention_expires_at: string;
  created_at: string;
  completed_at: string;
}

export interface LogicFailureCategoryMetric {
  category: string;
  count: number;
}

export interface LogicMetricsResponse {
  logic_file_id: string;
  window: "24h" | "7d" | "30d" | "90d" | string;
  window_start: string;
  window_end: string;
  success_count: number;
  failure_count: number;
  failure_categories: LogicFailureCategoryMetric[];
  recent_runs: LogicRunHistoryEntry[];
  p95_duration_ms: number | null;
  viewer_permission_required: boolean;
}

export function isLogicFunctionPackageId(id: string) {
  return id.trim().startsWith("logic.");
}

function logicFunctionSummary(fn: LogicFunctionInvocationResponse["function"]): FunctionPackageSummary {
  return {
    id: fn.function_rid,
    name: fn.name || fn.function_rid,
    display_name: fn.name || fn.function_rid,
    version: fn.published_version_id,
    runtime: "logic",
    entrypoint: fn.function_rid,
    capabilities: {
      allow_ontology_read: true,
      allow_ontology_write: true,
      allow_ai: true,
      allow_network: false,
      timeout_seconds: 60,
      max_source_bytes: 0,
    },
  };
}

export function invokeLogicFunction(
  functionRid: string,
  body: {
    inputs?: Record<string, unknown>;
    invocation_surface?: string;
    justification?: string;
  },
) {
  return api.post<LogicFunctionInvocationResponse>(
    `/agent-runtime/logic/functions/${encodeURIComponent(functionRid)}/invoke`,
    body,
  );
}

export function listLogicRuns(logicFileId: string) {
  return api.get<LogicRunHistoryEntry[]>(
    `/agent-runtime/logic/files/${encodeURIComponent(logicFileId)}/runs`,
  );
}

export function getLogicMetrics(logicFileId: string, params?: { window?: string }) {
  const qs = new URLSearchParams();
  if (params?.window) qs.set("window", params.window);
  const tail = qs.toString();
  return api.get<LogicMetricsResponse>(
    `/agent-runtime/logic/files/${encodeURIComponent(logicFileId)}/metrics${tail ? `?${tail}` : ""}`,
  );
}

export function getFunctionAuthoringSurface() {
  return api.get<FunctionAuthoringSurface>(
    "/ontology/functions/authoring-surface",
  );
}

export function listFunctionPackages(params?: {
  runtime?: string;
  search?: string;
  page?: number;
  per_page?: number;
}) {
  const qs = new URLSearchParams();
  if (params?.runtime) qs.set("runtime", params.runtime);
  if (params?.search) qs.set("search", params.search);
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  return api.get<{
    data: FunctionPackage[];
    total: number;
    page: number;
    per_page: number;
  }>(`/ontology/functions?${qs}`);
}

export function createFunctionPackage(body: {
  name: string;
  version?: string;
  display_name?: string;
  description?: string;
  runtime: string;
  source: string;
  entrypoint?: string;
  capabilities?: Partial<FunctionCapabilities>;
}) {
  return api.post<FunctionPackage>("/ontology/functions", body);
}

export function updateFunctionPackage(
  id: string,
  body: {
    display_name?: string;
    description?: string;
    source?: string;
    entrypoint?: string;
    capabilities?: Partial<FunctionCapabilities>;
  },
) {
  return api.patch<FunctionPackage>(`/ontology/functions/${id}`, body);
}

export function deleteFunctionPackage(id: string) {
  return api.delete(`/ontology/functions/${id}`);
}

export function listFunctionPackageRuns(
  id: string,
  params?: {
    invocation_kind?: string;
    status?: string;
    page?: number;
    per_page?: number;
  },
) {
  const qs = new URLSearchParams();
  if (params?.invocation_kind)
    qs.set("invocation_kind", params.invocation_kind);
  if (params?.status) qs.set("status", params.status);
  if (params?.page) qs.set("page", String(params.page));
  if (params?.per_page) qs.set("per_page", String(params.per_page));
  const tail = qs.toString() ? `?${qs}` : "";
  return api.get<{
    data: FunctionPackageRun[];
    total: number;
    page: number;
    per_page: number;
  }>(`/ontology/functions/${id}/runs${tail}`);
}

export function listFunctionPackageMetrics(id: string) {
  return api.get<FunctionPackageMetrics>(`/ontology/functions/${id}/metrics`);
}

export function validateFunctionPackage(
  id: string,
  body: FunctionPackageInvocationBody,
) {
  return api.post<ValidateFunctionPackageResponse>(
    `/ontology/functions/${id}/validate`,
    body,
  );
}

export function simulateFunctionPackage(
  id: string,
  body: FunctionPackageInvocationBody & { object_type_id: string },
) {
  return api.post<SimulateFunctionPackageResponse>(
    `/ontology/functions/${id}/simulate`,
    body,
  );
}

export function executeFunctionPackage(
  id: string,
  body: FunctionPackageInvocationBody & { object_type_id: string },
) {
  if (isLogicFunctionPackageId(id)) {
    return invokeLogicFunction(id, {
      inputs: body.parameters ?? {},
      invocation_surface: "workshop",
      justification: body.justification,
    }).then((response): SimulateFunctionPackageResponse => ({
      package: logicFunctionSummary(response.function),
      preview: response.outputs,
      result: response.outputs,
    }));
  }
  return simulateFunctionPackage(id, body);
}

// ────────────────────────────────────────────────────────────────
// Object/Link/Property/SharedProperty mutations — used by
// /object-link-types, /ontology-manager, /ontology, /action-types.
// ────────────────────────────────────────────────────────────────

export interface PropertyInlineEditConfig {
  action_type_id?: string;
  input_name?: string | null;
}

export function createObjectType(body: {
  name: string;
  display_name?: string;
  plural_display_name?: string | null;
  description?: string;
  primary_key_property?: string;
  title_property?: string | null;
  icon?: string;
  color?: string;
  status?: string;
  visibility?: string;
    group_names?: string[];
    object_display_preferences?: Record<string, unknown>;
    backing_dataset_id?: string | null;
    backing_dataset_rid?: string | null;
    backing_datasource_type?: "dataset" | "restricted_view" | string | null;
    backing_restricted_view_id?: string | null;
    restricted_view_id?: string | null;
    restricted_view_policy?: RestrictedViewRowPolicy | Record<string, unknown> | null;
    restricted_view_policy_version?: number | null;
    restricted_view_registered_policy_version?: number | null;
    restricted_view_indexed_policy_version?: number | null;
    restricted_view_storage_mode?: RestrictedViewStorageMode | null;
  }) {
  return api.post<ObjectType>("/ontology/types", body);
}

export function updateObjectType(
  id: string,
  body: {
    name?: string;
    display_name?: string;
    plural_display_name?: string | null;
    description?: string;
    primary_key_property?: string;
    title_property?: string | null;
    icon?: string;
    color?: string;
    status?: string;
    visibility?: string;
    group_names?: string[];
    object_display_preferences?: Record<string, unknown>;
    backing_dataset_id?: string | null;
    backing_dataset_rid?: string | null;
    backing_datasource_type?: "dataset" | "restricted_view" | string | null;
    backing_restricted_view_id?: string | null;
    restricted_view_id?: string | null;
    restricted_view_policy?: RestrictedViewRowPolicy | Record<string, unknown> | null;
    restricted_view_policy_version?: number | null;
    restricted_view_registered_policy_version?: number | null;
    restricted_view_indexed_policy_version?: number | null;
    restricted_view_storage_mode?: RestrictedViewStorageMode | null;
    restricted_view_policy_updated_at?: string | null;
    restricted_view_registered_at?: string | null;
    restricted_view_indexed_at?: string | null;
  },
) {
  return api.patch<ObjectType>(`/ontology/types/${id}`, body);
}

export function deleteObjectType(id: string) {
  return api.delete(`/ontology/types/${id}`);
}

export function createProperty(
  typeId: string,
  body: {
    name: string;
    display_name?: string;
    description?: string;
    property_type: string;
    value_type_id?: string | null;
    required?: boolean;
    unique_constraint?: boolean;
    time_dependent?: boolean;
    default_value?: unknown;
    validation_rules?: unknown;
    display_mode?: PropertyDisplayMode;
    value_formatting?: PropertyValueFormatting | Record<string, unknown>;
    conditional_formatting?: PropertyConditionalFormattingRule[];
    reducer_metadata?: PropertyReducerMetadata | Record<string, unknown>;
    inline_edit_config?: PropertyInlineEditConfig | null;
  },
) {
  return api.post<Property>(`/ontology/types/${typeId}/properties`, body);
}

export function updateProperty(
  typeId: string,
  propertyId: string,
  body: {
    display_name?: string;
    description?: string;
    value_type_id?: string | null;
    required?: boolean;
    unique_constraint?: boolean;
    time_dependent?: boolean;
    default_value?: unknown;
    validation_rules?: unknown;
    display_mode?: PropertyDisplayMode;
    value_formatting?: PropertyValueFormatting | Record<string, unknown>;
    conditional_formatting?: PropertyConditionalFormattingRule[];
    reducer_metadata?: PropertyReducerMetadata | Record<string, unknown>;
    inline_edit_config?: PropertyInlineEditConfig | null;
  },
) {
  return api.patch<Property>(
    `/ontology/types/${typeId}/properties/${propertyId}`,
    body,
  );
}

export function deleteProperty(typeId: string, propertyId: string) {
  return api.delete(`/ontology/types/${typeId}/properties/${propertyId}`);
}

export function createSharedPropertyType(body: {
  name: string;
  display_name?: string;
  description?: string;
  property_type: string;
  value_type_id?: string | null;
  value_formatting?: PropertyValueFormatting | Record<string, unknown>;
  conditional_formatting?: PropertyConditionalFormattingRule[];
  required?: boolean;
  unique_constraint?: boolean;
  time_dependent?: boolean;
  default_value?: unknown;
  validation_rules?: unknown;
}) {
  return api.post<SharedPropertyType>("/ontology/shared-property-types", body);
}

export function updateSharedPropertyType(
  id: string,
  body: {
    display_name?: string;
    description?: string;
    value_type_id?: string | null;
    value_formatting?: PropertyValueFormatting | Record<string, unknown>;
    conditional_formatting?: PropertyConditionalFormattingRule[];
    required?: boolean;
    unique_constraint?: boolean;
    time_dependent?: boolean;
    default_value?: unknown;
    validation_rules?: unknown;
  },
) {
  return api.patch<SharedPropertyType>(
    `/ontology/shared-property-types/${id}`,
    body,
  );
}

export function deleteSharedPropertyType(id: string) {
  return api.delete(`/ontology/shared-property-types/${id}`);
}

export function listTypeSharedPropertyTypes(typeId: string) {
  return api
    .get<{
      data: SharedPropertyType[];
    }>(`/ontology/types/${typeId}/shared-property-types`)
    .then((response) => response.data);
}

export function attachSharedPropertyType(
  typeId: string,
  sharedPropertyTypeId: string,
) {
  return api.post<{ object_type_id: string; shared_property_type_id: string }>(
    `/ontology/types/${typeId}/shared-property-types/${sharedPropertyTypeId}`,
    {},
  );
}

export function detachSharedPropertyType(
  typeId: string,
  sharedPropertyTypeId: string,
) {
  return api.delete(
    `/ontology/types/${typeId}/shared-property-types/${sharedPropertyTypeId}`,
  );
}

export function createLinkType(body: {
  name: string;
  display_name?: string;
  description?: string;
  source_type_id: string;
  target_type_id: string;
  cardinality?: LinkTypeCardinality | string;
  label?: string;
  reverse_label?: string;
  visibility?: string;
  link_datasource_mapping?: LinkTypeDatasourceMapping | null;
}) {
  return api.post<LinkType>("/ontology/links", body);
}

export function updateLinkType(
  id: string,
  body: {
    display_name?: string;
    description?: string;
    cardinality?: LinkTypeCardinality | string;
    label?: string;
    reverse_label?: string;
    visibility?: string;
    link_datasource_mapping?: LinkTypeDatasourceMapping | null;
  },
) {
  return api.patch<LinkType>(`/ontology/links/${id}`, body);
}

export function deleteLinkType(id: string) {
  return api.delete(`/ontology/links/${id}`);
}

// Rule mutations + simulation
export function createRule(body: {
  name: string;
  display_name?: string;
  description?: string;
  object_type_id: string;
  evaluation_mode?: RuleEvaluationMode;
  trigger_spec?: RuleTriggerSpec;
  effect_spec?: RuleEffectSpec;
}) {
  return api.post<OntologyRule>("/ontology/rules", body);
}

export function updateRule(
  id: string,
  body: {
    display_name?: string;
    description?: string;
    evaluation_mode?: RuleEvaluationMode;
    trigger_spec?: RuleTriggerSpec;
    effect_spec?: RuleEffectSpec;
  },
) {
  return api.patch<OntologyRule>(`/ontology/rules/${id}`, body);
}

export function simulateRule(
  id: string,
  body: { object_id: string; properties_patch?: Record<string, unknown> },
) {
  return api.post<{
    rule: OntologyRule;
    matched: boolean;
    trigger_payload: Record<string, unknown>;
    effect_preview: Record<string, unknown> | null;
    object: ObjectInstance;
  }>(`/ontology/rules/${id}/simulate`, body);
}

export function applyRule(
  id: string,
  body: { object_id: string; properties_patch?: Record<string, unknown> },
) {
  return api.post<{
    rule: OntologyRule;
    matched: boolean;
    trigger_payload: Record<string, unknown>;
    effect_preview: Record<string, unknown> | null;
    object: ObjectInstance;
  }>(`/ontology/rules/${id}/apply`, body);
}

export function deleteRule(id: string) {
  return api.delete(`/ontology/rules/${id}`);
}
