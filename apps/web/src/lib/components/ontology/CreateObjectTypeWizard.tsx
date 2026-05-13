import { useEffect, useMemo, useState } from "react";

import { Glyph } from "@/lib/components/ui/Glyph";
import {
  bindProjectResource,
  createActionType,
  createObjectType,
  createObjectTypeBinding,
  createProperty,
  listProjects,
  propertyTypeMetadata,
  type ObjectType,
  type OntologyProject,
} from "@/lib/api/ontology";
import { listDatasets, previewDataset, type Dataset } from "@/lib/api/datasets";

interface CreateObjectTypeWizardProps {
  open: boolean;
  onClose: () => void;
  onCreated: (objectType: ObjectType) => void;
}

type StepId = "datasource" | "metadata" | "properties" | "location" | "actions";

interface PropertyRow {
  id: string;
  source_column: string;
  property_name: string;
  display_name: string;
  property_type: string;
  raw_type: string;
  supported: boolean;
  include: boolean;
  required: boolean;
  deleted?: boolean;
}

const STEPS: Array<{ id: StepId; label: string }> = [
  { id: "datasource", label: "Datasource" },
  { id: "metadata", label: "Metadata" },
  { id: "properties", label: "Properties" },
  { id: "location", label: "Save location" },
  { id: "actions", label: "Actions" },
];

const PROPERTY_TYPES = [
  "STRING",
  "TEXT",
  "INTEGER",
  "LONG",
  "FLOAT",
  "DOUBLE",
  "DECIMAL",
  "BOOLEAN",
  "DATE",
  "TIME",
  "TIMESTAMP",
  "GEOHASH",
  "GEOPOINT",
  "GEOSHAPE",
  "MEDIA_REFERENCE",
  "TIME_SERIES",
  "ATTACHMENT",
  "BINARY",
  "OBJECT_REFERENCE",
  "ARRAY<STRING>",
];
const RESERVED_API_NAMES = new Set([
  "action",
  "and",
  "as",
  "class",
  "delete",
  "from",
  "interface",
  "link",
  "object",
  "ontology",
  "or",
  "property",
  "select",
  "type",
  "where",
]);
const UNSUPPORTED_SOURCE_TYPES = [
  "ARRAY",
  "MAP",
  "STRUCT",
  "BINARY",
  "GEOMETRY",
  "JSON",
  "VARIANT",
];

function snakeToTitle(value: string): string {
  return value
    .replace(/[_-]+/g, " ")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/\b\w/g, (m) => m.toUpperCase());
}

function snakeIdent(value: string): string {
  return value
    .toLowerCase()
    .replace(/([a-z0-9])([A-Z])/g, "$1_$2")
    .replace(/[^a-z0-9_]+/g, "_")
    .replace(/^_+|_+$/g, "");
}

function isUnsupportedSourceType(rawType: string | undefined): boolean {
  if (!rawType) return false;
  const upper = rawType.toUpperCase();
  return UNSUPPORTED_SOURCE_TYPES.some((type) => upper.includes(type));
}

function inferPropertyType(rawType: string | undefined): string {
  if (!rawType) return "STRING";
  const upper = rawType.toUpperCase();
  if (upper.includes("TIMESTAMP") || upper.includes("DATETIME")) return "TIMESTAMP";
  if (upper === "TIME" || upper.includes("TIME(")) return "TIME";
  if (upper.includes("DATE")) return "DATE";
  if (upper.includes("INT") && upper.includes("64")) return "LONG";
  if (upper.includes("LONG")) return "LONG";
  if (upper.includes("INT")) return "INTEGER";
  if (
    upper.includes("DOUBLE") ||
    upper.includes("FLOAT") ||
    upper.includes("DECIMAL")
  )
    return upper.includes("DECIMAL") || upper.includes("NUMERIC")
      ? "DECIMAL"
      : "DOUBLE";
  if (upper.includes("BOOL")) return "BOOLEAN";
  if (upper.includes("GEOHASH")) return "GEOHASH";
  if (upper.includes("GEOPOINT") || upper.includes("GEO_POINT"))
    return "GEOPOINT";
  if (upper.includes("GEOSHAPE") || upper.includes("GEOJSON"))
    return "GEOSHAPE";
  if (upper.includes("BINARY") || upper.includes("BYTES")) return "BINARY";
  return "STRING";
}

function isValidAPIName(value: string): boolean {
  return /^[a-z][a-z0-9_]*$/.test(value);
}

function isPrimaryKeyType(propertyType: string): boolean {
  return propertyTypeMetadata({ property_type: propertyType }).primary_key_eligible;
}

function isTitleKeyType(propertyType: string): boolean {
  return propertyTypeMetadata({ property_type: propertyType }).title_key_eligible;
}

function rowForColumn(column: { name: string; type: string }): PropertyRow {
  const supported = !isUnsupportedSourceType(column.type);
  return {
    id: `column:${column.name}`,
    source_column: column.name,
    property_name: snakeIdent(column.name),
    display_name: snakeToTitle(column.name),
    property_type: inferPropertyType(column.type),
    raw_type: column.type,
    supported,
    include: supported,
    required: false,
  };
}

function metadataIssues(displayName: string): string[] {
  const apiName = snakeIdent(displayName);
  if (!displayName.trim()) return ["Object type name is required."];
  if (!isValidAPIName(apiName)) {
    return [
      "Object type API name must start with a lowercase letter and contain only lowercase letters, numbers, and underscores.",
    ];
  }
  if (RESERVED_API_NAMES.has(apiName)) {
    return [`Object type API name "${apiName}" is reserved.`];
  }
  return [];
}

function propertyEditorIssues(
  rows: PropertyRow[],
  primaryKeyRowId: string,
  titleRowId: string,
  hasDatasource: boolean,
): string[] {
  const issues: string[] = [];
  const includedRows = rows.filter((row) => row.include && !row.deleted);
  const propertyCounts = new Map<string, number>();
  const sourceCounts = new Map<string, number>();

  for (const row of includedRows) {
    const propertyName = row.property_name.trim();
    if (!propertyName) {
      issues.push(
        `Property "${row.display_name || row.source_column || "New property"}" needs an API name.`,
      );
      continue;
    }
    if (!isValidAPIName(propertyName)) {
      issues.push(
        `Property API name "${propertyName}" must start with a lowercase letter and contain only lowercase letters, numbers, and underscores.`,
      );
    }
    if (RESERVED_API_NAMES.has(propertyName)) {
      issues.push(`Property API name "${propertyName}" is reserved.`);
    }
    propertyCounts.set(
      propertyName,
      (propertyCounts.get(propertyName) ?? 0) + 1,
    );

    if (hasDatasource) {
      if (!row.source_column.trim() && row.required) {
        issues.push(
          `Required property "${propertyName}" must be mapped to a datasource column.`,
        );
      }
      if (row.source_column.trim()) {
        sourceCounts.set(
          row.source_column,
          (sourceCounts.get(row.source_column) ?? 0) + 1,
        );
      }
    }
  }

  for (const [propertyName, count] of propertyCounts.entries()) {
    if (count > 1) {
      issues.push(`Duplicate property API name "${propertyName}".`);
    }
  }
  for (const [sourceColumn, count] of sourceCounts.entries()) {
    if (count > 1) {
      issues.push(
        `Datasource column "${sourceColumn}" is mapped more than once.`,
      );
    }
  }

  const primaryRow = includedRows.find((row) => row.id === primaryKeyRowId);
  if (!primaryRow) {
    issues.push("Select an included property as the primary key.");
  } else if (!isPrimaryKeyType(primaryRow.property_type)) {
    issues.push("Primary key must use a STRING, INTEGER, or LONG property type.");
  } else if (hasDatasource && !primaryRow.source_column.trim()) {
    issues.push("Primary key must be mapped to a datasource column.");
  }

  const titleRow = titleRowId
    ? includedRows.find((row) => row.id === titleRowId)
    : null;
  if (titleRow && !isTitleKeyType(titleRow.property_type)) {
    issues.push("Title key must use a STRING property type.");
  }

  return Array.from(new Set(issues));
}

export function CreateObjectTypeWizard({
  open,
  onClose,
  onCreated,
}: CreateObjectTypeWizardProps) {
  const [step, setStep] = useState<StepId>("datasource");
  const [datasourceMode, setDatasourceMode] = useState<"use" | "continue">(
    "use",
  );
  const [datasets, setDatasets] = useState<Dataset[]>([]);
  const [projects, setProjects] = useState<OntologyProject[]>([]);
  const [datasetSearch, setDatasetSearch] = useState("");
  const [datasetPickerOpen, setDatasetPickerOpen] = useState(false);
  const [selectedDataset, setSelectedDataset] = useState<Dataset | null>(null);
  const [columns, setColumns] = useState<Array<{ name: string; type: string }>>(
    [],
  );

  const [name, setName] = useState("");
  const [pluralName, setPluralName] = useState("");
  const [description, setDescription] = useState("");
  const [selectedGroupNames, setSelectedGroupNames] = useState("");

  const [propertyRows, setPropertyRows] = useState<PropertyRow[]>([]);
  const [primaryKeyColumn, setPrimaryKeyColumn] = useState("");
  const [titleColumn, setTitleColumn] = useState("");

  const [selectedProjectId, setSelectedProjectId] = useState("");
  const [folderPath, setFolderPath] = useState("/ontology/object-types");
  const [generatedPermissionsDataset, setGeneratedPermissionsDataset] =
    useState("");

  const [actions, setActions] = useState<{
    create: boolean;
    modify: boolean;
    delete: boolean;
  }>({
    create: false,
    modify: false,
    delete: false,
  });

  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    setStep("datasource");
    setDatasourceMode("use");
    setSelectedDataset(null);
    setDatasets([]);
    setProjects([]);
    setColumns([]);
    setName("");
    setPluralName("");
    setDescription("");
    setSelectedGroupNames("");
    setPropertyRows([]);
    setPrimaryKeyColumn("");
    setTitleColumn("");
    setSelectedProjectId("");
    setFolderPath("/ontology/object-types");
    setGeneratedPermissionsDataset("");
    setActions({ create: false, modify: false, delete: false });
    setError("");
  }, [open]);

  useEffect(() => {
    if (!open || datasets.length > 0) return;
    void listDatasets({ per_page: 200 })
      .then((response) => setDatasets(response.data))
      .catch(() => setDatasets([]));
  }, [open, datasets.length]);

  useEffect(() => {
    if (!open || projects.length > 0) return;
    void listProjects({ per_page: 200 })
      .then((response) => {
        setProjects(response.data);
        setSelectedProjectId(
          (current) => current || response.data[0]?.id || "",
        );
      })
      .catch(() => setProjects([]));
  }, [open, projects.length]);

  useEffect(() => {
    if (!selectedDataset) {
      setColumns([]);
      return;
    }
    let cancelled = false;
    previewDataset(selectedDataset.id, { limit: 1 })
      .then((response) => {
        if (cancelled) return;
        const cols =
          response.columns?.map((column) => ({
            name: column.name,
            type: column.field_type ?? column.data_type ?? "string",
          })) ?? [];
        setColumns(cols);
        const rows: PropertyRow[] = cols.map(rowForColumn);
        setPropertyRows(rows);
        const idCandidate = cols.find((column) =>
          /(^|_)id$/i.test(column.name),
        );
        if (idCandidate) setPrimaryKeyColumn(`column:${idCandidate.name}`);
        const titleCandidate = cols.find((column) =>
          /name|title/i.test(column.name),
        );
        if (titleCandidate) setTitleColumn(`column:${titleCandidate.name}`);
      })
      .catch(() => {
        if (!cancelled) setColumns([]);
      });
    return () => {
      cancelled = true;
    };
  }, [selectedDataset]);

  useEffect(() => {
    if (!open || datasourceMode !== "continue") return;
    setSelectedDataset(null);
    setColumns([]);
    setGeneratedPermissionsDataset(
      (current) =>
        current ||
        `${snakeIdent(name) || "object_type"}_permissions_placeholder`,
    );
    if (propertyRows.length === 0) {
      setPropertyRows([
        {
          id: "placeholder:id",
          source_column: "id",
          property_name: "id",
          display_name: "ID",
          property_type: "STRING",
          raw_type: "placeholder",
          supported: true,
          include: true,
          required: true,
        },
        {
          id: "placeholder:title",
          source_column: "title",
          property_name: "title",
          display_name: "Title",
          property_type: "STRING",
          raw_type: "placeholder",
          supported: true,
          include: true,
          required: false,
        },
      ]);
      setPrimaryKeyColumn("placeholder:id");
      setTitleColumn("placeholder:title");
    }
  }, [datasourceMode, name, open, propertyRows.length]);

  const filteredDatasets = useMemo(() => {
    const q = datasetSearch.trim().toLowerCase();
    if (!q) return datasets;
    return datasets.filter((entry) => entry.name.toLowerCase().includes(q));
  }, [datasetSearch, datasets]);

  if (!open) return null;

  const visiblePropertyRows = propertyRows.filter((row) => !row.deleted);
  const includedPropertyRows = visiblePropertyRows.filter((row) => row.include);
  const objectTypeIssues = metadataIssues(name);
  const propertyIssues = propertyEditorIssues(
    propertyRows,
    primaryKeyColumn,
    titleColumn,
    Boolean(selectedDataset),
  );
  const unmappedColumns = columns.filter(
    (column) =>
      !visiblePropertyRows.some((row) => row.source_column === column.name),
  );
  const currentStepIndex = STEPS.findIndex((entry) => entry.id === step);
  const canGoNext = (() => {
    if (step === "datasource")
      return datasourceMode === "continue" || Boolean(selectedDataset);
    if (step === "metadata") return objectTypeIssues.length === 0;
    if (step === "properties")
      return includedPropertyRows.length > 0 && propertyIssues.length === 0;
    if (step === "location")
      return datasourceMode === "continue"
        ? Boolean(generatedPermissionsDataset.trim())
        : true;
    return true;
  })();

  function patchRow(index: number, patch: Partial<PropertyRow>) {
    setPropertyRows((current) =>
      current.map((row, i) => (i === index ? { ...row, ...patch } : row)),
    );
  }

  function addCustomProperty() {
    const id = `custom:${Date.now()}:${propertyRows.length}`;
    setPropertyRows((current) => [
      ...current,
      {
        id,
        source_column: "",
        property_name: "new_property",
        display_name: "New property",
        property_type: "STRING",
        raw_type: selectedDataset ? "unmapped" : "manual",
        supported: true,
        include: true,
        required: false,
      },
    ]);
  }

  function addAllUnmappedColumns() {
    setPropertyRows((current) => [
      ...current,
      ...unmappedColumns.map(rowForColumn),
    ]);
  }

  function deleteRow(row: PropertyRow) {
    setPropertyRows((current) => current.filter((entry) => entry.id !== row.id));
    if (primaryKeyColumn === row.id) setPrimaryKeyColumn("");
    if (titleColumn === row.id) setTitleColumn("");
  }

  async function submit() {
    setSubmitting(true);
    setError("");
    try {
      const submitIssues = propertyEditorIssues(
        propertyRows,
        primaryKeyColumn,
        titleColumn,
        Boolean(selectedDataset),
      );
      if (submitIssues.length > 0) {
        setError(submitIssues.join(" "));
        return;
      }
      const includedRows = propertyRows.filter(
        (row) => row.include && row.supported && !row.deleted,
      );
      const primaryRow = includedRows.find((row) => row.id === primaryKeyColumn);
      const titleRow = includedRows.find((row) => row.id === titleColumn);
      const objectApiName = snakeIdent(name) || "new_object_type";
      const created = await createObjectType({
        name: objectApiName,
        display_name: name.trim() || undefined,
        description:
          [
            description.trim(),
            selectedGroupNames.trim()
              ? `Groups: ${selectedGroupNames.trim()}`
              : "",
            titleRow ? `Title key: ${titleRow.property_name}` : "",
            datasourceMode === "continue"
              ? `Placeholder permissions dataset: ${generatedPermissionsDataset.trim()}`
              : "",
            folderPath.trim() ? `Save location: ${folderPath.trim()}` : "",
          ]
            .filter(Boolean)
            .join("\n") || undefined,
        primary_key_property: primaryRow?.property_name,
        title_property: titleRow?.property_name,
        plural_display_name: pluralName.trim() || undefined,
        group_names: selectedGroupNames
          .split(",")
          .map((entry) => entry.trim())
          .filter(Boolean),
        object_display_preferences: titleRow
          ? { title_property: titleRow.property_name }
          : undefined,
      });

      const propertyResults = await Promise.allSettled(
        includedRows.map((row) =>
          createProperty(created.id, {
            name: row.property_name,
            display_name: row.display_name,
            property_type: row.property_type,
            required:
              row.required || row.property_name === primaryRow?.property_name,
            unique_constraint: row.property_name === primaryRow?.property_name,
          }),
        ),
      );
      const propertyFailures = propertyResults.filter(
        (result) => result.status === "rejected",
      ).length;

      const followupResults = await Promise.allSettled([
        selectedProjectId
          ? bindProjectResource(selectedProjectId, {
              resource_kind: "object_type",
              resource_id: created.id,
            })
          : Promise.resolve(null),
        selectedDataset && primaryRow
          ? createObjectTypeBinding(created.id, {
              dataset_id: selectedDataset.id,
              dataset_branch: selectedDataset.active_branch || undefined,
              primary_key_column: primaryRow.source_column,
              property_mapping: includedRows
                .filter((row) => row.source_column.trim())
                .map((row) => ({
                  source_field: row.source_column,
                  target_property: row.property_name,
                })),
              sync_mode: "snapshot",
              default_marking: "default",
              preview_limit: 1000,
            })
          : Promise.resolve(null),
        actions.create
          ? createActionType({
              name: `${objectApiName}_create`,
              display_name: `Create ${name || "object"}`,
              description: `Generated by the object type creation helper for ${name || objectApiName}.`,
              object_type_id: created.id,
              operation_kind: "create_object",
              input_schema: includedRows.map((row) => ({
                name: row.property_name,
                display_name: row.display_name,
                property_type: row.property_type,
                required: row.property_name === primaryRow?.property_name,
              })),
              config: { generated_by: "object_type_creation_helper" },
            })
          : Promise.resolve(null),
        actions.modify
          ? createActionType({
              name: `${objectApiName}_modify`,
              display_name: `Modify ${name || "object"}`,
              description: `Generated by the object type creation helper for ${name || objectApiName}.`,
              object_type_id: created.id,
              operation_kind: "update_object",
              input_schema: includedRows
                .filter(
                  (row) => row.property_name !== primaryRow?.property_name,
                )
                .map((row) => ({
                  name: row.property_name,
                  display_name: row.display_name,
                  property_type: row.property_type,
                  required: false,
                })),
              config: { generated_by: "object_type_creation_helper" },
            })
          : Promise.resolve(null),
        actions.delete
          ? createActionType({
              name: `${objectApiName}_delete`,
              display_name: `Delete ${name || "object"}`,
              description: `Generated by the object type creation helper for ${name || objectApiName}.`,
              object_type_id: created.id,
              operation_kind: "delete_object",
              input_schema: primaryRow
                ? [
                    {
                      name: primaryRow.property_name,
                      display_name: primaryRow.display_name,
                      property_type: primaryRow.property_type,
                      required: true,
                    },
                  ]
                : [],
              confirmation_required: true,
              config: { generated_by: "object_type_creation_helper" },
            })
          : Promise.resolve(null),
      ]);
      const followupFailures = followupResults.filter(
        (result) => result.status === "rejected",
      ).length;
      if (propertyFailures > 0 || followupFailures > 0) {
        setError(
          `Object type created, but ${propertyFailures} property write(s) and ${followupFailures} follow-up task(s) failed. You can close this dialog and continue editing the type.`,
        );
        onCreated(created);
        return;
      }
      onCreated(created);
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Create failed");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="otw-title"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !submitting) onClose();
      }}
      style={{
        position: "fixed",
        inset: 0,
        zIndex: 95,
        background: "rgba(17, 24, 39, 0.4)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: 32,
      }}
    >
      <section
        style={{
          width: "100%",
          maxWidth: 880,
          height: "min(640px, calc(100vh - 64px))",
          background: "#fff",
          borderRadius: 6,
          boxShadow: "0 20px 60px rgba(15, 23, 42, 0.18)",
          display: "grid",
          gridTemplateRows: "auto 1fr auto",
          overflow: "hidden",
        }}
      >
        <header
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            padding: "12px 18px",
            borderBottom: "1px solid var(--border-subtle)",
          }}
        >
          <h2
            id="otw-title"
            style={{ margin: 0, fontSize: 15, fontWeight: 600 }}
          >
            Create a new object type
          </h2>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="of-button of-button--ghost"
            style={{ padding: 4 }}
          >
            <Glyph name="x" size={14} />
          </button>
        </header>

        <div
          style={{
            display: "grid",
            gridTemplateColumns: "220px minmax(0, 1fr)",
            minHeight: 0,
          }}
        >
          <aside
            style={{
              borderRight: "1px solid var(--border-subtle)",
              padding: 12,
              display: "grid",
              gap: 4,
              alignContent: "start",
            }}
          >
            {STEPS.map((entry, index) => {
              const active = entry.id === step;
              const reachable = index <= currentStepIndex;
              return (
                <button
                  key={entry.id}
                  type="button"
                  onClick={() => reachable && setStep(entry.id)}
                  disabled={!reachable}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 10,
                    padding: "8px 10px",
                    border: 0,
                    background: active
                      ? "rgba(45, 114, 210, 0.06)"
                      : "transparent",
                    color: active
                      ? "var(--status-info)"
                      : reachable
                        ? "var(--text-strong)"
                        : "var(--text-muted)",
                    fontWeight: active ? 600 : 500,
                    fontSize: 13,
                    borderRadius: 4,
                    cursor: reachable ? "pointer" : "not-allowed",
                    textAlign: "left",
                  }}
                >
                  <span
                    style={{
                      display: "inline-flex",
                      alignItems: "center",
                      justifyContent: "center",
                      width: 24,
                      height: 24,
                      borderRadius: 999,
                      fontSize: 12,
                      fontWeight: 600,
                      background: active ? "var(--status-info)" : "#aab4c0",
                      color: "#fff",
                    }}
                  >
                    {index + 1}
                  </span>
                  {entry.label}
                </button>
              );
            })}
          </aside>

          <main style={{ overflowY: "auto", padding: "20px 24px" }}>
            {step === "datasource" ? (
              <div style={{ display: "grid", gap: 18 }}>
                <div>
                  <p
                    className="of-text-muted"
                    style={{
                      margin: 0,
                      fontSize: 12,
                      textTransform: "uppercase",
                      letterSpacing: "0.06em",
                    }}
                  >
                    Step 1
                  </p>
                  <h3
                    style={{
                      margin: "4px 0 12px",
                      fontSize: 18,
                      fontWeight: 600,
                    }}
                  >
                    Object type backing
                  </h3>
                  <p
                    style={{
                      margin: "0 0 10px",
                      fontSize: 13,
                      fontWeight: 600,
                    }}
                  >
                    Datasource
                  </p>
                  <div
                    style={{
                      display: "grid",
                      gridTemplateColumns: "1fr 1fr",
                      gap: 10,
                    }}
                  >
                    <button
                      type="button"
                      onClick={() => {
                        setDatasourceMode("continue");
                        setDatasetPickerOpen(false);
                        setSelectedDataset(null);
                        setPropertyRows([]);
                        setPrimaryKeyColumn("");
                        setTitleColumn("");
                      }}
                      style={cardStyle(datasourceMode === "continue")}
                    >
                      <Glyph name="document" size={18} tone="#5c7080" />
                      <div
                        style={{ display: "grid", gap: 2, textAlign: "left" }}
                      >
                        <strong style={{ fontSize: 13 }}>
                          Continue without datasource
                        </strong>
                        <span
                          style={{ fontSize: 12, color: "var(--text-muted)" }}
                        >
                          Generate a dataset for permissions purposes
                        </span>
                      </div>
                    </button>
                    <button
                      type="button"
                      onClick={() => setDatasourceMode("use")}
                      style={cardStyle(datasourceMode === "use")}
                    >
                      <Glyph name="database" size={18} tone="#2d72d2" />
                      <div
                        style={{ display: "grid", gap: 2, textAlign: "left" }}
                      >
                        <strong style={{ fontSize: 13 }}>
                          Use existing datasource
                        </strong>
                        <span
                          style={{ fontSize: 12, color: "var(--text-muted)" }}
                        >
                          Select a preexisting Foundry datasource
                        </span>
                      </div>
                      {datasourceMode === "use" ? (
                        <Glyph
                          name="check"
                          size={14}
                          tone="var(--status-info)"
                        />
                      ) : null}
                    </button>
                  </div>
                </div>

                {datasourceMode === "continue" ? (
                  <div
                    style={{
                      padding: 12,
                      border: "1px solid var(--border-subtle)",
                      borderRadius: 6,
                      background: "rgba(245, 158, 11, 0.08)",
                      fontSize: 12,
                    }}
                  >
                    This object type will be created with OpenFoundry
                    placeholder properties and a generated permissions dataset
                    reference. You can attach a real datasource later from the
                    bindings wizard.
                  </div>
                ) : null}

                {datasourceMode === "use" ? (
                  <div>
                    <p
                      style={{
                        margin: "0 0 6px",
                        fontSize: 13,
                        fontWeight: 600,
                      }}
                    >
                      Select a datasource to back this object type
                    </p>
                    {selectedDataset ? (
                      <div
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: 10,
                          padding: "10px 12px",
                          border: "1px solid var(--border-default)",
                          borderRadius: 4,
                        }}
                      >
                        <Glyph name="database" size={14} tone="#2d72d2" />
                        <div style={{ flex: 1, display: "grid" }}>
                          <span style={{ fontSize: 13, fontWeight: 600 }}>
                            {selectedDataset.name}
                          </span>
                          <span
                            style={{ fontSize: 11, color: "var(--text-muted)" }}
                          >
                            {selectedDataset.storage_path || selectedDataset.id}{" "}
                            · {columns.length} column(s)
                          </span>
                        </div>
                        <button
                          type="button"
                          className="of-button"
                          onClick={() => setDatasetPickerOpen(true)}
                          style={{ fontSize: 12 }}
                        >
                          <Glyph name="pencil" size={12} /> Replace
                        </button>
                      </div>
                    ) : (
                      <button
                        type="button"
                        className="of-button"
                        onClick={() => setDatasetPickerOpen(true)}
                        style={{ fontSize: 13 }}
                      >
                        <Glyph name="database" size={13} /> Select datasource
                      </button>
                    )}
                    {datasetPickerOpen ? (
                      <div
                        style={{
                          marginTop: 8,
                          border: "1px solid var(--border-default)",
                          borderRadius: 4,
                          background: "#fff",
                          padding: 8,
                          maxHeight: 300,
                          overflowY: "auto",
                        }}
                      >
                        <input
                          autoFocus
                          value={datasetSearch}
                          onChange={(event) =>
                            setDatasetSearch(event.target.value)
                          }
                          placeholder="Search datasets..."
                          style={{
                            width: "100%",
                            padding: "6px 10px",
                            border: "1px solid var(--border-default)",
                            borderRadius: 4,
                            fontSize: 13,
                            marginBottom: 6,
                          }}
                        />
                        {filteredDatasets.map((dataset) => (
                          <button
                            key={dataset.id}
                            type="button"
                            onClick={() => {
                              setSelectedDataset(dataset);
                              setDatasetPickerOpen(false);
                            }}
                            style={{
                              display: "flex",
                              alignItems: "center",
                              gap: 8,
                              width: "100%",
                              padding: "6px 8px",
                              border: 0,
                              background: "transparent",
                              cursor: "pointer",
                              textAlign: "left",
                              fontSize: 13,
                            }}
                          >
                            <Glyph name="database" size={13} tone="#2d72d2" />
                            <span>{dataset.name}</span>
                          </button>
                        ))}
                        {filteredDatasets.length === 0 ? (
                          <p
                            className="of-text-muted"
                            style={{ margin: 8, fontSize: 12 }}
                          >
                            No datasets.
                          </p>
                        ) : null}
                      </div>
                    ) : null}
                  </div>
                ) : null}
              </div>
            ) : null}

            {step === "metadata" ? (
              <div style={{ display: "grid", gap: 14 }}>
                <div>
                  <p
                    className="of-text-muted"
                    style={{
                      margin: 0,
                      fontSize: 12,
                      textTransform: "uppercase",
                      letterSpacing: "0.06em",
                    }}
                  >
                    Step 2
                  </p>
                  <h3
                    style={{ margin: "4px 0 0", fontSize: 18, fontWeight: 600 }}
                  >
                    Configure object type metadata
                  </h3>
                </div>
                <div
                  style={{
                    display: "grid",
                    gridTemplateColumns: "60px 1fr 1fr",
                    gap: 12,
                  }}
                >
                  <Field label="Icon">
                    <button
                      type="button"
                      style={{
                        width: 40,
                        height: 40,
                        border: "1px solid var(--border-default)",
                        borderRadius: 4,
                        background: "#f4f6f9",
                        cursor: "pointer",
                      }}
                    >
                      <Glyph name="cube" size={18} tone="#2d72d2" />
                    </button>
                  </Field>
                  <Field label="Name">
                    <input
                      value={name}
                      onChange={(event) => {
                        setName(event.target.value);
                        if (!pluralName.trim())
                          setPluralName(`${event.target.value}s`);
                      }}
                      style={inputStyle()}
                      placeholder="Order"
                    />
                  </Field>
                  <Field label="Plural name">
                    <input
                      value={pluralName}
                      onChange={(event) => setPluralName(event.target.value)}
                      style={inputStyle()}
                      placeholder="Orders"
                    />
                  </Field>
                </div>
                {objectTypeIssues.length > 0 ? (
                  <div
                    className="of-status-warning"
                    style={{
                      padding: "8px 12px",
                      borderRadius: 4,
                      fontSize: 12,
                    }}
                  >
                    {objectTypeIssues.join(" ")}
                  </div>
                ) : null}
                <Field label="Description">
                  <input
                    value={description}
                    onChange={(event) => setDescription(event.target.value)}
                    style={inputStyle()}
                    placeholder="Enter optional description…"
                  />
                </Field>
                <Field label="Groups">
                  <input
                    value={selectedGroupNames}
                    onChange={(event) =>
                      setSelectedGroupNames(event.target.value)
                    }
                    style={inputStyle()}
                    placeholder="Trail operations, Customer 360…"
                  />
                </Field>
              </div>
            ) : null}

            {step === "properties" ? (
              <div style={{ display: "grid", gap: 14 }}>
                <div
                  style={{
                    display: "flex",
                    justifyContent: "space-between",
                    alignItems: "flex-end",
                  }}
                >
                  <div>
                    <p
                      className="of-text-muted"
                      style={{
                        margin: 0,
                        fontSize: 12,
                        textTransform: "uppercase",
                        letterSpacing: "0.06em",
                      }}
                    >
                      Step 3
                    </p>
                    <h3
                      style={{
                        margin: "4px 0 0",
                        fontSize: 18,
                        fontWeight: 600,
                      }}
                    >
                      Properties
                    </h3>
                  </div>
                  <div style={{ display: "flex", gap: 8 }}>
                    <button
                      type="button"
                      className="of-button"
                      onClick={addCustomProperty}
                      style={{ fontSize: 12 }}
                    >
                      <Glyph name="plus" size={12} tone="var(--status-info)" />{" "}
                      Add property
                    </button>
                    {selectedDataset ? (
                      <button
                        type="button"
                        className="of-button of-button--ghost"
                        onClick={addAllUnmappedColumns}
                        disabled={unmappedColumns.length === 0}
                        style={{ fontSize: 12 }}
                      >
                        Add all unmapped columns
                      </button>
                    ) : null}
                  </div>
                </div>
                {visiblePropertyRows.some((row) => !row.supported) ? (
                  <div
                    className="of-status-warning"
                    style={{
                      padding: "8px 12px",
                      borderRadius: 4,
                      fontSize: 12,
                    }}
                  >
                    {visiblePropertyRows.filter((row) => !row.supported).length}{" "}
                    datasource column(s) use unsupported types and will not be
                    generated as object properties unless remapped to a
                    supported type.
                  </div>
                ) : null}
                {propertyIssues.length > 0 ? (
                  <div
                    className="of-status-warning"
                    style={{
                      padding: "8px 12px",
                      borderRadius: 4,
                      fontSize: 12,
                    }}
                  >
                    <strong>Fix before continuing:</strong>{" "}
                    {propertyIssues.join(" ")}
                  </div>
                ) : null}
                <div
                  style={{
                    display: "grid",
                    gridTemplateColumns: selectedDataset
                      ? "58px 70px minmax(130px, 1fr) auto minmax(210px, 1.5fr) 132px 42px"
                      : "58px 70px minmax(210px, 1.5fr) 132px 42px",
                    gap: 8,
                    fontSize: 12,
                    color: "var(--text-muted)",
                  }}
                >
                  <span>Show</span>
                  <span>Required</span>
                  {selectedDataset ? <span>Datasource column</span> : null}
                  {selectedDataset ? <span /> : null}
                  <span>Property</span>
                  <span>Type</span>
                  <span />
                </div>
                <div
                  style={{
                    display: "grid",
                    gap: 6,
                    maxHeight: 280,
                    overflowY: "auto",
                  }}
                >
                  {visiblePropertyRows.length === 0 ? (
                    <p className="of-text-muted" style={{ fontSize: 12 }}>
                      No properties yet. Pick a datasource in Step 1 or continue
                      without one to generate placeholder properties.
                    </p>
                  ) : (
                    visiblePropertyRows.map((row, index) => (
                      <div
                        key={row.id}
                        style={{
                          display: "grid",
                          gridTemplateColumns: selectedDataset
                            ? "58px 70px minmax(130px, 1fr) auto minmax(210px, 1.5fr) 132px 42px"
                            : "58px 70px minmax(210px, 1.5fr) 132px 42px",
                          gap: 8,
                          alignItems: "start",
                          opacity: row.include ? 1 : 0.65,
                        }}
                      >
                        <label
                          style={{
                            display: "flex",
                            alignItems: "center",
                            gap: 6,
                            fontSize: 12,
                            paddingTop: 8,
                          }}
                        >
                          <input
                            type="checkbox"
                            checked={row.include}
                            onChange={(event) =>
                              patchRow(index, { include: event.target.checked })
                            }
                            disabled={!row.supported}
                          />
                          {row.include ? "Yes" : "No"}
                        </label>
                        <label
                          style={{
                            display: "flex",
                            alignItems: "center",
                            gap: 6,
                            fontSize: 12,
                            paddingTop: 8,
                          }}
                        >
                          <input
                            type="checkbox"
                            checked={row.required}
                            onChange={(event) =>
                              patchRow(index, { required: event.target.checked })
                            }
                            disabled={!row.include}
                          />
                          Req
                        </label>
                        {selectedDataset ? (
                          <span style={{ display: "grid", gap: 3 }}>
                            <select
                              value={row.source_column}
                              onChange={(event) => {
                                const column = columns.find(
                                  (entry) => entry.name === event.target.value,
                                );
                                patchRow(index, {
                                  source_column: event.target.value,
                                  raw_type: column?.type ?? "unmapped",
                                  property_type: column
                                    ? inferPropertyType(column.type)
                                    : row.property_type,
                                  supported: column
                                    ? !isUnsupportedSourceType(column.type)
                                    : true,
                                  include: column
                                    ? !isUnsupportedSourceType(column.type)
                                    : row.include,
                                });
                              }}
                              style={inputStyle()}
                            >
                              <option value="">No datasource mapping</option>
                              {columns.map((column) => (
                                <option key={column.name} value={column.name}>
                                  {column.name}
                                </option>
                              ))}
                            </select>
                            <span
                              className="of-text-muted"
                              style={{ fontSize: 11 }}
                            >
                              {row.raw_type}
                              {row.supported ? "" : " · unsupported"}
                            </span>
                          </span>
                        ) : null}
                        {selectedDataset ? (
                          <Glyph
                            name="chevron-right"
                            size={12}
                            tone="#5c7080"
                          />
                        ) : null}
                        <span style={{ display: "grid", gap: 4 }}>
                          <input
                            value={row.display_name}
                            onChange={(event) =>
                              patchRow(index, {
                                display_name: event.target.value,
                                property_name: snakeIdent(event.target.value),
                              })
                            }
                            style={inputStyle()}
                            disabled={!row.include}
                            placeholder="Display name"
                          />
                          <input
                            value={row.property_name}
                            onChange={(event) =>
                              patchRow(index, {
                                property_name: snakeIdent(event.target.value),
                              })
                            }
                            style={inputStyle()}
                            disabled={!row.include}
                            aria-label={`${row.display_name} API name`}
                            placeholder="api_name"
                          />
                        </span>
                        <select
                          value={row.property_type}
                          onChange={(event) =>
                            patchRow(index, {
                              property_type: event.target.value,
                              supported: true,
                              include: true,
                            })
                          }
                          style={inputStyle()}
                        >
                          {PROPERTY_TYPES.map((type) => (
                            <option key={type} value={type}>
                              {type}
                            </option>
                          ))}
                        </select>
                        <button
                          type="button"
                          className="of-button of-button--ghost"
                          onClick={() => deleteRow(row)}
                          aria-label={`Delete ${row.display_name}`}
                          style={{ padding: 7 }}
                        >
                          <Glyph
                            name="trash"
                            size={12}
                            tone="var(--status-danger)"
                          />
                        </button>
                      </div>
                    ))
                  )}
                </div>
                <div
                  style={{
                    display: "grid",
                    gridTemplateColumns: "1fr 1fr",
                    gap: 14,
                  }}
                >
                  <Field label="Primary key">
                    <select
                      value={primaryKeyColumn}
                      onChange={(event) =>
                        setPrimaryKeyColumn(event.target.value)
                      }
                      style={inputStyle()}
                    >
                      <option value="">Select primary key</option>
                      {visiblePropertyRows
                        .filter((row) => row.include && row.supported)
                        .map((row) => (
                          <option key={row.id} value={row.id}>
                            {row.display_name} ({row.property_name})
                          </option>
                        ))}
                    </select>
                  </Field>
                  <Field label="Title">
                    <select
                      value={titleColumn}
                      onChange={(event) => setTitleColumn(event.target.value)}
                      style={inputStyle()}
                    >
                      <option value="">Select title column</option>
                      {visiblePropertyRows
                        .filter((row) => row.include && row.supported)
                        .map((row) => (
                          <option key={row.id} value={row.id}>
                            {row.display_name} ({row.property_name})
                          </option>
                        ))}
                    </select>
                  </Field>
                </div>
              </div>
            ) : null}

            {step === "location" ? (
              <div style={{ display: "grid", gap: 14 }}>
                <div>
                  <p
                    className="of-text-muted"
                    style={{
                      margin: 0,
                      fontSize: 12,
                      textTransform: "uppercase",
                      letterSpacing: "0.06em",
                    }}
                  >
                    Step 4
                  </p>
                  <h3
                    style={{ margin: "4px 0 0", fontSize: 18, fontWeight: 600 }}
                  >
                    Choose save location
                  </h3>
                </div>
                <Field label="Project">
                  <select
                    value={selectedProjectId}
                    onChange={(event) =>
                      setSelectedProjectId(event.target.value)
                    }
                    style={inputStyle()}
                  >
                    <option value="">No project binding</option>
                    {projects.map((project) => (
                      <option key={project.id} value={project.id}>
                        {project.display_name || project.slug}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field label="Folder path">
                  <input
                    value={folderPath}
                    onChange={(event) => setFolderPath(event.target.value)}
                    style={inputStyle()}
                    placeholder="/ontology/object-types"
                  />
                </Field>
                {datasourceMode === "continue" ? (
                  <Field label="Generated permissions dataset / placeholder">
                    <input
                      value={generatedPermissionsDataset}
                      onChange={(event) =>
                        setGeneratedPermissionsDataset(event.target.value)
                      }
                      style={inputStyle()}
                      placeholder="customer_permissions_placeholder"
                    />
                  </Field>
                ) : null}
                <div
                  style={{
                    padding: 12,
                    border: "1px solid var(--border-subtle)",
                    borderRadius: 6,
                    background: "#f8fafc",
                    fontSize: 12,
                  }}
                >
                  The helper will bind the object type to the selected project
                  after creation. Folder path and group selections are preserved
                  in the generated description until first-class folder/group
                  metadata APIs land.
                </div>
              </div>
            ) : null}

            {step === "actions" ? (
              <div style={{ display: "grid", gap: 14 }}>
                <div>
                  <p
                    className="of-text-muted"
                    style={{
                      margin: 0,
                      fontSize: 12,
                      textTransform: "uppercase",
                      letterSpacing: "0.06em",
                    }}
                  >
                    Step 5
                  </p>
                  <h3
                    style={{ margin: "4px 0 0", fontSize: 18, fontWeight: 600 }}
                  >
                    Generate action types
                  </h3>
                </div>
                <p
                  className="of-text-muted"
                  style={{ margin: 0, fontSize: 13 }}
                >
                  Select action types to generate
                </p>
                <div
                  style={{
                    border: "1px solid var(--border-subtle)",
                    borderRadius: 6,
                  }}
                >
                  <ActionTypeRow
                    icon="object"
                    title={`Create ${name || "object"}`}
                    description={`Set ${
                      propertyRows
                        .slice(0, 3)
                        .map((row) => row.display_name)
                        .join(", ") || "—"
                    }, and ${Math.max(propertyRows.length - 3, 0)} more properties`}
                    checked={actions.create}
                    onToggle={() =>
                      setActions((current) => ({
                        ...current,
                        create: !current.create,
                      }))
                    }
                  />
                  <ActionTypeRow
                    icon="pencil"
                    title={`Modify ${name || "object"}`}
                    description={`Modify ${
                      propertyRows
                        .slice(0, 3)
                        .map((row) => row.display_name)
                        .join(", ") || "—"
                    }, and ${Math.max(propertyRows.length - 3, 0)} more properties`}
                    checked={actions.modify}
                    onToggle={() =>
                      setActions((current) => ({
                        ...current,
                        modify: !current.modify,
                      }))
                    }
                  />
                  <ActionTypeRow
                    icon="object"
                    title={`Delete ${name || "object"}`}
                    description="Allows deleting object instances and all of their properties"
                    checked={actions.delete}
                    onToggle={() =>
                      setActions((current) => ({
                        ...current,
                        delete: !current.delete,
                      }))
                    }
                  />
                </div>
                <Field label="Select who can execute these action types">
                  <input
                    className="of-input"
                    disabled
                    placeholder="Search users or groups..."
                    style={inputStyle()}
                  />
                </Field>
              </div>
            ) : null}

            {error ? (
              <div
                role="alert"
                className="of-status-danger"
                style={{
                  marginTop: 14,
                  padding: "8px 12px",
                  fontSize: 12,
                  borderRadius: 4,
                }}
              >
                {error}
              </div>
            ) : null}
          </main>
        </div>

        <footer
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            padding: "12px 18px",
            borderTop: "1px solid var(--border-subtle)",
          }}
        >
          <button
            type="button"
            onClick={() => {
              const previous = STEPS[currentStepIndex - 1];
              if (previous) setStep(previous.id);
            }}
            className="of-button"
            disabled={currentStepIndex === 0 || submitting}
          >
            Back
          </button>
          {step === "actions" ? (
            <button
              type="button"
              onClick={() => void submit()}
              disabled={submitting}
              style={{
                padding: "8px 14px",
                border: 0,
                borderRadius: 4,
                background: "#15803d",
                color: "#fff",
                fontSize: 13,
                fontWeight: 600,
                cursor: submitting ? "not-allowed" : "pointer",
              }}
            >
              {submitting ? "Creating..." : "Create"}
            </button>
          ) : (
            <button
              type="button"
              onClick={() => {
                const next = STEPS[currentStepIndex + 1];
                if (next) setStep(next.id);
              }}
              disabled={!canGoNext}
              style={{
                padding: "8px 14px",
                border: 0,
                borderRadius: 4,
                background: "#2d72d2",
                color: "#fff",
                fontSize: 13,
                fontWeight: 600,
                cursor: canGoNext ? "pointer" : "not-allowed",
                opacity: canGoNext ? 1 : 0.6,
              }}
            >
              Next
            </button>
          )}
        </footer>
      </section>
    </div>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label style={{ display: "grid", gap: 4 }}>
      <span style={{ fontSize: 12, color: "var(--text-muted)" }}>{label}</span>
      {children}
    </label>
  );
}

function ActionTypeRow({
  icon,
  title,
  description,
  checked,
  onToggle,
}: {
  icon: "object" | "pencil";
  title: string;
  description: string;
  checked: boolean;
  onToggle: () => void;
}) {
  return (
    <label
      style={{
        display: "flex",
        alignItems: "flex-start",
        gap: 12,
        padding: "12px 14px",
        borderBottom: "1px solid var(--border-subtle)",
        cursor: "pointer",
      }}
    >
      <input
        type="checkbox"
        checked={checked}
        onChange={onToggle}
        style={{ accentColor: "var(--status-info)", marginTop: 2 }}
      />
      <span
        style={{
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          width: 28,
          height: 28,
          borderRadius: 4,
          background: "rgba(45, 114, 210, 0.12)",
          color: "var(--status-info)",
        }}
      >
        <Glyph name={icon} size={14} tone="var(--status-info)" />
      </span>
      <span style={{ display: "grid", gap: 2 }}>
        <strong style={{ fontSize: 13 }}>{title}</strong>
        <span style={{ fontSize: 12, color: "var(--text-muted)" }}>
          {description}
        </span>
      </span>
    </label>
  );
}

function cardStyle(active: boolean): React.CSSProperties {
  return {
    display: "flex",
    alignItems: "center",
    gap: 12,
    padding: "14px 16px",
    border: active
      ? "2px solid var(--status-info)"
      : "1px solid var(--border-default)",
    borderRadius: 6,
    background: active ? "rgba(45, 114, 210, 0.04)" : "#fff",
    cursor: "pointer",
    textAlign: "left",
    width: "100%",
  };
}

function inputStyle(): React.CSSProperties {
  return {
    padding: "6px 10px",
    border: "1px solid var(--border-default)",
    borderRadius: 4,
    background: "#fff",
    fontSize: 13,
    color: "var(--text-strong)",
    width: "100%",
  };
}
