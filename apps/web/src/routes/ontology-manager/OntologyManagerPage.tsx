import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";

import { projectStablePath } from "@/lib/compass/stableResourceUrls";
import {
  buildOntologyAuditEventLog,
  buildOntologyCleanupAssistant,
  buildOntologyHealthReport,
  buildOntologyUsageImpactAnalysis,
  buildCoreObjectViews,
  buildOntologyBundle,
  buildOntologyBranchProposalIntegration,
  buildOntologyHistory,
  buildOntologyPermissionAnalysis,
  buildOntologyResourceHistory,
  buildOntologyResourceRegistry,
  buildOntologyResourceSearchIndex,
  createOntologyCleanupStagedChanges,
  createOntologyRestoreChange,
  createSharedPropertyType,
  createValueType,
  createObjectTypeGroup,
  deleteObjectTypeGroup,
  deleteSharedPropertyType,
  deleteValueType,
  deriveOntologyArtifact,
  getProjectWorkingState,
  linkTypeCardinalityLabel,
  linkTypeEndpointLabels,
  linkTypeHasDatasourceMapping,
  listActionTypes,
  listInterfaces,
  listLinkTypes,
  listObjectTypes,
  listObjectTypeGroups,
  listObjectSets,
  listObjectViews,
  listProjectMemberships,
  listProjectSavedChanges,
  listProjectResources,
  listProjects,
  listSharedPropertyTypes,
  listValueTypes,
  ontologyResourceKey,
  parseOntologyBundleJSON,
  replaceProjectWorkingState,
  searchOntologyResourceIndex,
  sharedPropertyImpactWarning,
  sharedPropertyUsageSummary,
  updateObjectTypeGroup,
  updateSharedPropertyType,
  updateValueType,
  validateOntologyBundle,
  valueTypeUsageSummary,
  type ActionType,
  type LinkType,
  type OntologyBundleValidationResult,
  type OntologyUsageExternalSource,
  type OntologyUsageImpactAnalysis,
  type OntologyUsageProduct,
  type OntologyArtifact,
  type OntologyAuditEventCategory,
  type OntologyAuditEventLog,
  type OntologyAuditEventStatus,
  type OntologyCleanupAssistant,
  type OntologyCleanupCandidate,
  type OntologyCleanupCandidateKind,
  type OntologyHealthCategory,
  type OntologyHealthIssue,
  type OntologyHealthReport,
  type OntologyHealthSeverity,
  type OntologyHistoryDetailsFilter,
  type OntologyHistoryEntry,
  type OntologyHistoryResourceSummary,
  type OntologyHistoryVisibilityFilter,
  type OntologyGlobalBranchProposalIntegration,
  type OntologyInterface,
  type OntologyObjectTypeGroup,
  type OntologyPermissionAnalysis,
  type OntologyPermissionLevel,
  type OntologyResourceSearchIndex,
  type OntologyResourceSearchIndexKind,
  type OntologyResourceSearchPermissionFilter,
  type OntologyResourceSearchResultItem,
  type ObjectType,
  type ObjectSetDefinition,
  type ObjectViewDefinition,
  type OntologyValueType,
  type OntologyProject,
  type OntologyProjectMembership,
  type OntologyProjectWorkingState,
  type OntologyProjectResourceBinding,
  type OntologyResourceRegistryEntry,
  type OntologySavedChangeRecord,
  type SharedPropertyType,
  usageProductLabel,
} from "@/lib/api/ontology";
import { getApp, listApps, type AppDefinition } from "@/lib/api/apps";
import { listGlobalBranches, listGlobalBranchResources } from "@/lib/api/global-branches";
import { listListings, listVersions, type ListingDefinition, type PackageVersion } from "@/lib/api/marketplace";
import { listPipelines, type Pipeline } from "@/lib/api/pipelines";
import { Glyph } from "@/lib/components/ui/Glyph";
import { CreateObjectTypeWizard } from "@/lib/components/ontology/CreateObjectTypeWizard";
import { LinkEditor } from "@/lib/components/ontology/LinkEditor";
import { calculateLogicMetrics, type LogicRunHistoryRecord } from "@/lib/logic/blocks";
import { useAuth } from "@/lib/stores/auth";

type Section =
  | "overview"
  | "registry"
  | "types"
  | "links"
  | "actions"
  | "interfaces"
  | "shared"
  | "valueTypes"
  | "groups"
  | "views"
  | "usage"
  | "permissions"
  | "changes"
  | "history"
  | "importExport"
  | "cleanup"
  | "auditHealth"
  | "projects";

type ResourceFilter = "all" | "visible" | "hidden" | "experimental" | "issues";

interface ShellNavItem {
  id: Section;
  label: string;
  description: string;
  count?: number;
}

interface ShellSearchResult {
  id: string;
  kind: string;
  label: string;
  detail: string;
  section: Section;
}

const SHELL_NAV_BASE: Array<Omit<ShellNavItem, "count">> = [
  {
    id: "overview",
    label: "Discover",
    description: "Ontology home, metadata, warnings, and recent edits.",
  },
  {
    id: "registry",
    label: "Registry",
    description: "First-class resources, placement, usage, and edit metadata.",
  },
  {
    id: "types",
    label: "Object types",
    description: "Object schemas, properties, datasources, indexing state.",
  },
  {
    id: "links",
    label: "Link types",
    description: "Typed relationships and datasource mappings.",
  },
  {
    id: "actions",
    label: "Action types",
    description: "Writeback, forms, rules, and observability.",
  },
  {
    id: "interfaces",
    label: "Interfaces",
    description: "Polymorphic contracts and implementations.",
  },
  {
    id: "shared",
    label: "Shared properties",
    description: "Reusable property definitions.",
  },
  {
    id: "valueTypes",
    label: "Value types",
    description: "Space-scoped semantic types and constraints.",
  },
  {
    id: "groups",
    label: "Object type groups",
    description: "Curated type collections for discovery.",
  },
  {
    id: "views",
    label: "Object Views",
    description: "Core and custom object view surfaces.",
  },
  {
    id: "usage",
    label: "Usage",
    description: "Dependents, object storage, query, and action usage.",
  },
  {
    id: "permissions",
    label: "Permissions",
    description: "Project/folder access, ownership, and edit requirements.",
  },
  {
    id: "changes",
    label: "Unsaved changes",
    description: "Branch-local changes awaiting save or proposal.",
  },
  {
    id: "history",
    label: "History",
    description: "Saved ontology changes and restore points.",
  },
  {
    id: "importExport",
    label: "Import / export",
    description: "Bundle, validate, migrate, and review ontology resources.",
  },
  {
    id: "cleanup",
    label: "Cleanup",
    description: "Detect stale resources and issue remediation tasks.",
  },
  {
    id: "auditHealth",
    label: "Audit & health",
    description: "Audit timeline and operational health signals for the ontology.",
  },
  {
    id: "projects",
    label: "Projects",
    description: "Project placement, folders, and memberships.",
  },
];

const FILTERS: Array<{ id: ResourceFilter; label: string }> = [
  { id: "all", label: "All resources" },
  { id: "visible", label: "Visible" },
  { id: "hidden", label: "Hidden" },
  { id: "experimental", label: "Experimental" },
  { id: "issues", label: "Indexing / placement issues" },
];

const PROPERTY_BASE_TYPE_OPTIONS = [
  "string",
  "integer",
  "float",
  "decimal",
  "boolean",
  "date",
  "timestamp",
  "json",
  "array",
  "geopoint",
  "geojson",
  "media_reference",
];

export function OntologyManagerPage() {
  const navigate = useNavigate();
  const { user } = useAuth();
  const [section, setSection] = useState<Section>("overview");
  const [objectTypes, setObjectTypes] = useState<ObjectType[]>([]);
  const [actionTypes, setActionTypes] = useState<ActionType[]>([]);
  const [interfaces, setInterfaces] = useState<OntologyInterface[]>([]);
  const [shared, setShared] = useState<SharedPropertyType[]>([]);
  const [valueTypes, setValueTypes] = useState<OntologyValueType[]>([]);
  const [sharedDraft, setSharedDraft] = useState({ name: "", display_name: "", description: "", property_type: "string", value_type_id: "" });
  const [editingSharedId, setEditingSharedId] = useState<string | null>(null);
  const [valueTypeDraft, setValueTypeDraft] = useState({ name: "", display_name: "", description: "", base_type: "string", semantic_type: "", constraints_json: "{}", formatting_json: "{}" });
  const [editingValueTypeId, setEditingValueTypeId] = useState<string | null>(null);
  const [linkTypes, setLinkTypes] = useState<LinkType[]>([]);
  const [objectViews, setObjectViews] = useState<ObjectViewDefinition[]>([]);
  const [objectSets, setObjectSets] = useState<ObjectSetDefinition[]>([]);
  const [objectTypeGroups, setObjectTypeGroups] = useState<OntologyObjectTypeGroup[]>([]);
  const [groupSearch, setGroupSearch] = useState("");
  const [groupDraft, setGroupDraft] = useState({
    name: "",
    display_name: "",
    description: "",
    visibility: "normal",
    status: "active",
    project_id: "",
    object_type_ids: [] as string[],
  });
  const [editingGroupId, setEditingGroupId] = useState<string | null>(null);
  const [groupSaving, setGroupSaving] = useState(false);
  const [projects, setProjects] = useState<OntologyProject[]>([]);
  const [projectResources, setProjectResources] = useState<
    OntologyProjectResourceBinding[]
  >([]);
  const [projectMemberships, setProjectMemberships] = useState<OntologyProjectMembership[]>([]);
  const [workingState, setWorkingState] = useState<OntologyProjectWorkingState | null>(null);
  const [savedChanges, setSavedChanges] = useState<OntologySavedChangeRecord[]>([]);
  const [historyResourceKind, setHistoryResourceKind] = useState("all");
  const [historyAuthor, setHistoryAuthor] = useState("");
  const [historyFrom, setHistoryFrom] = useState("");
  const [historyTo, setHistoryTo] = useState("");
  const [historyVisibility, setHistoryVisibility] = useState<OntologyHistoryVisibilityFilter>("all");
  const [historyDetails, setHistoryDetails] = useState<OntologyHistoryDetailsFilter>("all");
  const [historyHideRestricted, setHistoryHideRestricted] = useState(false);
  const [selectedHistoryResource, setSelectedHistoryResource] = useState("");
  const [historyNotice, setHistoryNotice] = useState("");
  const [historyBusy, setHistoryBusy] = useState("");
  const [bundleSelection, setBundleSelection] = useState<string[]>([]);
  const [bundleText, setBundleText] = useState("");
  const [bundleValidation, setBundleValidation] = useState<OntologyBundleValidationResult | null>(null);
  const [bundleNotice, setBundleNotice] = useState("");
  const [bundleBusy, setBundleBusy] = useState("");
  const [usageExternalSources, setUsageExternalSources] = useState<OntologyUsageExternalSource[]>([]);
  const [usageNotice, setUsageNotice] = useState("");
  const [ontology, setOntology] = useState<OntologyArtifact>(() =>
    deriveOntologyArtifact({ projects: [] }),
  );
  const [search, setSearch] = useState("");
  const [resourceFilter, setResourceFilter] = useState<ResourceFilter>("all");
  const [error, setError] = useState("");
  const [newMenuOpen, setNewMenuOpen] = useState(false);
  const [branchMenuOpen, setBranchMenuOpen] = useState(false);
  const [branchName, setBranchName] = useState("Main");
  const [proposalExcludedResourceIds, setProposalExcludedResourceIds] = useState<string[]>([]);
  const [proposalExcludedIndexingChangeIds, setProposalExcludedIndexingChangeIds] = useState<string[]>([]);
  const [branchDialogOpen, setBranchDialogOpen] = useState(false);
  const [wizardOpen, setWizardOpen] = useState(false);
  const [cleanupSelection, setCleanupSelection] = useState<string[]>([]);
  const [cleanupConfirmed, setCleanupConfirmed] = useState(false);
  const [cleanupNotice, setCleanupNotice] = useState("");
  const [cleanupBusy, setCleanupBusy] = useState(false);
  const [auditCategoryFilter, setAuditCategoryFilter] = useState<OntologyAuditEventCategory | "all">("all");
  const [auditStatusFilter, setAuditStatusFilter] = useState<OntologyAuditEventStatus | "all">("all");
  const [auditActorFilter, setAuditActorFilter] = useState("");
  const [healthCategoryFilter, setHealthCategoryFilter] = useState<OntologyHealthCategory | "all">("all");
  const [healthSeverityFilter, setHealthSeverityFilter] = useState<OntologyHealthSeverity | "all">("all");
  const newMenuRef = useRef<HTMLDivElement>(null);
  const branchMenuRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const resourceSearchIndexRef = useRef<OntologyResourceSearchIndex | null>(null);

  useEffect(() => {
    if (!newMenuOpen && !branchMenuOpen) return;
    function onClickOutside(event: MouseEvent) {
      if (
        newMenuOpen &&
        newMenuRef.current &&
        !newMenuRef.current.contains(event.target as Node)
      )
        setNewMenuOpen(false);
      if (
        branchMenuOpen &&
        branchMenuRef.current &&
        !branchMenuRef.current.contains(event.target as Node)
      )
        setBranchMenuOpen(false);
    }
    window.addEventListener("mousedown", onClickOutside);
    return () => window.removeEventListener("mousedown", onClickOutside);
  }, [newMenuOpen, branchMenuOpen]);

  async function refresh() {
    setError("");
    try {
      const [types, actions, ifs, sh, valueTypeRes, links, groups, views, sets, prs] = await Promise.all([
        listObjectTypes({ per_page: 200, search: search || undefined }),
        listActionTypes({ per_page: 200, search: search || undefined }),
        listInterfaces({ per_page: 200, search: search || undefined }),
        listSharedPropertyTypes({ per_page: 200, search: search || undefined }),
        listValueTypes({ search: search || undefined }),
        listLinkTypes({ per_page: 200 }),
        listObjectTypeGroups({ per_page: 200, search: groupSearch || undefined }),
        listObjectViews({ per_page: 200 }),
        listObjectSets({ size: 200 }).catch(() => ({ data: [] as ObjectSetDefinition[] })),
        listProjects({ per_page: 200 }),
      ]);
      setObjectTypes(types.data);
      setActionTypes(actions.data);
      setInterfaces(ifs.data);
      setShared(sh.data);
      setValueTypes(valueTypeRes.data);
      const coreViews = buildCoreObjectViews({ objectTypes: types.data, linkTypes: links.data });
      setLinkTypes(links.data);
      setObjectTypeGroups(groups.data);
      setObjectViews([...coreViews, ...views.data]);
      setObjectSets(sets.data);
      setProjects(prs.data);
      const membershipLists = await Promise.all(
        prs.data.map((project) => listProjectMemberships(project.id).catch(() => [] as OntologyProjectMembership[])),
      );
      setProjectMemberships(membershipLists.flat());
      const primaryProject = prs.data[0];
      const [resources, nextWorkingState, nextSavedChanges]: [
        OntologyProjectResourceBinding[],
        OntologyProjectWorkingState | null,
        OntologySavedChangeRecord[],
      ] = primaryProject
        ? await Promise.all([
            listProjectResources(primaryProject.id),
            getProjectWorkingState(primaryProject.id).catch(() => null),
            listProjectSavedChanges(primaryProject.id).catch(() => []),
          ])
        : [[], null, [] as OntologySavedChangeRecord[]];
      setProjectResources(resources);
      setWorkingState(nextWorkingState);
      setSavedChanges(nextSavedChanges);
      setOntology(
        deriveOntologyArtifact({
          projects: prs.data,
          resourceBindings: resources,
          objectTypeCount: types.data.length,
          linkTypeCount: links.data.length,
          interfaceCount: ifs.data.length,
          sharedPropertyTypeCount: sh.data.length,
        }),
      );
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Failed to load");
    }
  }

  useEffect(() => {
    function onGlobalSearchShortcut(event: KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        searchInputRef.current?.focus();
      }
    }
    window.addEventListener("keydown", onGlobalSearchShortcut);
    return () => window.removeEventListener("keydown", onGlobalSearchShortcut);
  }, []);

  useEffect(() => {
    void refresh();
  }, []);

  useEffect(() => {
    let cancelled = false;
    async function loadUsageSources() {
      const { sources, failures } = await loadOntologyUsageSources();
      if (cancelled) return;
      setUsageExternalSources(sources);
      setUsageNotice(
        failures.length
          ? `Loaded ${sources.length} downstream usage sources; ${failures.join(", ")} unavailable.`
          : `Loaded ${sources.length} downstream usage sources.`,
      );
    }
    void loadUsageSources();
    return () => {
      cancelled = true;
    };
  }, []);

  function refreshLinkType(link: LinkType) {
    setLinkTypes((current) => {
      const index = current.findIndex((item) => item.id === link.id);
      if (index === -1) return [link, ...current];
      const next = [...current];
      next[index] = link;
      return next;
    });
  }

  function removeLinkType(id: string) {
    setLinkTypes((current) => current.filter((item) => item.id !== id));
  }

  function resetGroupDraft() {
    setEditingGroupId(null);
    setGroupDraft({
      name: "",
      display_name: "",
      description: "",
      visibility: "normal",
      status: "active",
      project_id: projects[0]?.id || "",
      object_type_ids: [],
    });
  }

  function editGroup(group: OntologyObjectTypeGroup) {
    setEditingGroupId(group.id);
    setGroupDraft({
      name: group.name,
      display_name: group.display_name,
      description: group.description || "",
      visibility: group.visibility || "normal",
      status: group.status || "active",
      project_id: group.project_id || "",
      object_type_ids: group.object_type_ids || [],
    });
  }

  async function saveGroup() {
    const name = groupDraft.name.trim() || slugifyGroupName(groupDraft.display_name);
    if (!name) {
      setError("Object type group API name is required.");
      return;
    }
    setGroupSaving(true);
    setError("");
    try {
      const body = {
        name,
        display_name: groupDraft.display_name.trim() || name,
        description: groupDraft.description,
        visibility: groupDraft.visibility,
        status: groupDraft.status,
        project_id: groupDraft.project_id || null,
        object_type_ids: groupDraft.object_type_ids,
      };
      const previousGroup = editingGroupId
        ? objectTypeGroups.find((group) => group.id === editingGroupId)
        : null;
      const saved = editingGroupId
        ? await updateObjectTypeGroup(editingGroupId, body)
        : await createObjectTypeGroup(body);
      setObjectTypeGroups((current) => {
        const index = current.findIndex((group) => group.id === saved.id);
        if (index === -1) return [saved, ...current];
        const next = [...current];
        next[index] = saved;
        return next;
      });
      setObjectTypes((current) =>
        current.map((type) => {
          const groupNames = new Set(type.group_names || []);
          if (previousGroup?.name) groupNames.delete(previousGroup.name);
          groupNames.delete(saved.name);
          if (saved.object_type_ids.includes(type.id)) groupNames.add(saved.name);
          return { ...type, group_names: Array.from(groupNames) };
        }),
      );
      resetGroupDraft();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Failed to save object type group");
    } finally {
      setGroupSaving(false);
    }
  }


  function resetSharedDraft() {
    setEditingSharedId(null);
    setSharedDraft({ name: "", display_name: "", description: "", property_type: "string", value_type_id: "" });
  }

  async function saveSharedProperty() {
    const name = sharedDraft.name.trim() || slugifyGroupName(sharedDraft.display_name);
    if (!name) {
      setError("Shared property API name is required.");
      return;
    }
    setError("");
    try {
      const body = {
        name,
        display_name: sharedDraft.display_name.trim() || name,
        description: sharedDraft.description,
        property_type: sharedDraft.property_type,
        value_type_id: sharedDraft.value_type_id || null,
      };
      const saved = editingSharedId
        ? await updateSharedPropertyType(editingSharedId, body)
        : await createSharedPropertyType(body);
      setShared((current) => {
        const index = current.findIndex((item) => item.id === saved.id);
        if (index === -1) return [saved, ...current];
        const next = [...current];
        next[index] = saved;
        return next;
      });
      resetSharedDraft();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Failed to save shared property type");
    }
  }

  function editSharedProperty(property: SharedPropertyType) {
    setEditingSharedId(property.id);
    setSharedDraft({
      name: property.name,
      display_name: property.display_name,
      description: property.description || "",
      property_type: property.property_type,
      value_type_id: property.value_type_id || "",
    });
  }

  async function removeSharedProperty(property: SharedPropertyType) {
    const usage = sharedPropertyUsageSummary(property.id, { objectTypes, interfaces });
    const warning = sharedPropertyImpactWarning(property, usage);
    if (typeof window !== "undefined" && !window.confirm(`${warning || `Delete ${property.display_name}?`}`)) return;
    try {
      await deleteSharedPropertyType(property.id);
      setShared((current) => current.filter((item) => item.id !== property.id));
      if (editingSharedId === property.id) resetSharedDraft();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Failed to delete shared property type");
    }
  }

  function resetValueTypeDraft() {
    setEditingValueTypeId(null);
    setValueTypeDraft({ name: "", display_name: "", description: "", base_type: "string", semantic_type: "", constraints_json: "{}", formatting_json: "{}" });
  }

  async function saveValueType() {
    const name = valueTypeDraft.name.trim() || slugifyGroupName(valueTypeDraft.display_name);
    if (!name) {
      setError("Value type API name is required.");
      return;
    }
    setError("");
    try {
      const constraints = JSON.parse(valueTypeDraft.constraints_json || "{}");
      const formatting = JSON.parse(valueTypeDraft.formatting_json || "{}");
      const body = {
        name,
        display_name: valueTypeDraft.display_name.trim() || name,
        description: valueTypeDraft.description,
        base_type: valueTypeDraft.base_type,
        semantic_type: valueTypeDraft.semantic_type || name,
        constraints,
        formatting,
      };
      const saved = editingValueTypeId
        ? await updateValueType(editingValueTypeId, { ...body, edit_kind: "breaking", note: "Ontology Manager edit" })
        : await createValueType(body);
      setValueTypes((current) => {
        const index = current.findIndex((item) => item.id === saved.id);
        if (index === -1) return [saved, ...current];
        const next = [...current];
        next[index] = saved;
        return next;
      });
      resetValueTypeDraft();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Failed to save value type");
    }
  }

  function editValueType(valueType: OntologyValueType) {
    setEditingValueTypeId(valueType.id);
    setValueTypeDraft({
      name: valueType.name,
      display_name: valueType.display_name,
      description: valueType.description || "",
      base_type: valueType.base_type,
      semantic_type: valueType.semantic_type || "",
      constraints_json: JSON.stringify(valueType.constraints || {}, null, 2),
      formatting_json: JSON.stringify(valueType.formatting || {}, null, 2),
    });
  }

  async function removeValueType(valueType: OntologyValueType) {
    const usage = valueTypeUsageSummary(valueType.id, { objectTypes, sharedPropertyTypes: shared, interfaces });
    if (usage.total > 0) {
      setError(`Cannot delete ${valueType.display_name}: it is used by ${usage.total} properties.`);
      return;
    }
    if (typeof window !== "undefined" && !window.confirm(`Delete value type "${valueType.display_name}"?`)) return;
    await deleteValueType(valueType.id, valueType.space_id);
    setValueTypes((current) => current.filter((item) => item.id !== valueType.id));
    if (editingValueTypeId === valueType.id) resetValueTypeDraft();
  }

  async function removeGroup(group: OntologyObjectTypeGroup) {
    if (typeof window !== "undefined" && !window.confirm(`Delete group "${group.display_name}"?`)) return;
    setGroupSaving(true);
    try {
      await deleteObjectTypeGroup(group.id);
      setObjectTypeGroups((current) => current.filter((item) => item.id !== group.id));
      setObjectTypes((current) =>
        current.map((type) => ({
          ...type,
          group_names: (type.group_names || []).filter((name) => name !== group.name),
        })),
      );
      if (editingGroupId === group.id) resetGroupDraft();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Failed to delete object type group");
    } finally {
      setGroupSaving(false);
    }
  }

  async function restoreHistoryResource(
    entry: { id: string; saved_at: string },
    resource: OntologyHistoryResourceSummary,
  ) {
    const primaryProject = projects[0];
    if (!primaryProject) {
      setError("Create or select an ontology project before restoring history.");
      return;
    }
    setHistoryBusy(`${entry.id}:${resource.kind}:${resource.id || ""}`);
    setHistoryNotice("");
    setError("");
    try {
      const fullEntry = historyEntries.find((candidate) => candidate.id === entry.id)
        || selectedResourceHistory.find((candidate) => candidate.id === entry.id);
      if (!fullEntry) throw new Error("History record is no longer loaded.");
      const restoreChange = createOntologyRestoreChange(fullEntry, resource, {
        current_user_id: currentUserId,
      });
      const nextWorkingState = await replaceProjectWorkingState(primaryProject.id, [
        ...workingChanges,
        restoreChange,
      ]);
      setWorkingState(nextWorkingState);
      setHistoryNotice(
        `${resource.label} was restored into unsaved changes. Save ontology changes for the restore to take effect.`,
      );
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Failed to stage restore change");
    } finally {
      setHistoryBusy("");
    }
  }

  function buildSelectedBundle() {
    return buildOntologyBundle({
      ontology,
      registry: ontologyRegistry,
      selectedResourceKeys: bundleSelection,
      objectTypes,
      linkTypes,
      actionTypes,
      interfaces,
      sharedPropertyTypes: shared,
      valueTypes,
      objectTypeGroups,
      objectViews,
      workingState,
      exportedBy: currentUserId,
    });
  }

  function exportBundle() {
    setBundleNotice("");
    setError("");
    if (bundleSelection.length === 0) {
      setError("Select at least one ontology resource to export.");
      return;
    }
    const bundle = buildSelectedBundle();
    const text = JSON.stringify(bundle, null, 2);
    setBundleText(text);
    setBundleValidation(validateOntologyBundle(bundle, {
      ontology,
      registry: ontologyRegistry,
      valueTypes,
      workingState,
      currentUserId,
    }));
    downloadTextFile(`openfoundry-ontology-bundle-${Date.now()}.json`, text);
    setBundleNotice(`Exported ${bundle.resources.length} resources into an OpenFoundry ontology bundle.`);
  }

  function validateBundleText() {
    setBundleNotice("");
    setError("");
    try {
      const bundle = parseOntologyBundleJSON(bundleText);
      const result = validateOntologyBundle(bundle, {
        ontology,
        registry: ontologyRegistry,
        valueTypes,
        workingState,
        currentUserId,
      });
      setBundleValidation(result);
      setBundleNotice(result.valid ? "Bundle validation passed." : "Bundle validation found blocking errors.");
      return { bundle, result };
    } catch (cause) {
      setBundleValidation(null);
      setError(cause instanceof Error ? cause.message : "Invalid ontology bundle JSON.");
      return null;
    }
  }

  async function importBundleAsWorkingState() {
    const primaryProject = projects[0];
    if (!primaryProject) {
      setError("Create or select an ontology project before importing a bundle.");
      return;
    }
    const parsed = validateBundleText();
    if (!parsed || !parsed.result.valid) return;
    setBundleBusy("import");
    try {
      const nextWorkingState = await replaceProjectWorkingState(primaryProject.id, [
        ...workingChanges,
        ...parsed.result.staged_changes,
      ]);
      setWorkingState(nextWorkingState);
      setBundleNotice(`Imported ${parsed.result.staged_changes.length} staged changes. Review and save them to apply the bundle.`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Failed to import ontology bundle");
    } finally {
      setBundleBusy("");
    }
  }

  async function loadBundleFile(file: File | null) {
    if (!file) return;
    setBundleBusy("file");
    setBundleNotice("");
    setError("");
    try {
      const text = await file.text();
      setBundleText(text);
      const bundle = parseOntologyBundleJSON(text);
      setBundleValidation(validateOntologyBundle(bundle, {
        ontology,
        registry: ontologyRegistry,
        valueTypes,
        workingState,
        currentUserId,
      }));
    } catch (cause) {
      setBundleValidation(null);
      setError(cause instanceof Error ? cause.message : "Failed to read ontology bundle");
    } finally {
      setBundleBusy("");
    }
  }

  const ontologyRegistry = useMemo(
    () =>
      buildOntologyResourceRegistry({
        ontology,
        projects,
        resourceBindings: projectResources,
        objectTypes,
        linkTypes,
        actionTypes,
        interfaces,
        sharedPropertyTypes: shared,
        objectTypeGroups,
        objectViews,
      }),
    [
      ontology,
      projects,
      projectResources,
      objectTypes,
      linkTypes,
      actionTypes,
      interfaces,
      shared,
      objectTypeGroups,
      objectViews,
    ],
  );

  const currentUserId = user?.id || user?.email || null;
  const canViewHiddenHistoryDetails = Boolean(
    user?.roles?.some((role) => ["admin", "owner", "ontology-admin"].includes(role)),
  );
  const workingChanges = workingState?.changes ?? [];
  const historyFilters = useMemo(
    () => ({
      resource_kind: historyResourceKind === "all" ? undefined : historyResourceKind,
      author: historyAuthor || undefined,
      from: historyFrom || undefined,
      to: historyTo || undefined,
      visibility: historyVisibility,
      details: historyDetails,
      hide_restricted_details: historyHideRestricted,
    }),
    [
      historyResourceKind,
      historyAuthor,
      historyFrom,
      historyTo,
      historyVisibility,
      historyDetails,
      historyHideRestricted,
    ],
  );
  const historyEntries = useMemo(
    () =>
      buildOntologyHistory(savedChanges, ontologyRegistry, historyFilters, {
        current_user_id: currentUserId,
        can_view_hidden_details: canViewHiddenHistoryDetails,
      }),
    [savedChanges, ontologyRegistry, historyFilters, currentUserId, canViewHiddenHistoryDetails],
  );
  const selectedHistoryResourceRef = parseHistoryResourceKey(selectedHistoryResource);
  const selectedResourceHistory = useMemo(
    () =>
      selectedHistoryResourceRef
        ? buildOntologyResourceHistory(
            savedChanges,
            ontologyRegistry,
            selectedHistoryResourceRef,
            historyFilters,
            {
              current_user_id: currentUserId,
              can_view_hidden_details: canViewHiddenHistoryDetails,
            },
          )
        : [],
    [
      savedChanges,
      ontologyRegistry,
      selectedHistoryResourceRef,
      historyFilters,
      currentUserId,
      canViewHiddenHistoryDetails,
    ],
  );
  const bundleResourceOptions = useMemo(
    () => buildBundleResourceOptions(ontologyRegistry, valueTypes),
    [ontologyRegistry, valueTypes],
  );
  const usageAnalysis = useMemo(
    () =>
      buildOntologyUsageImpactAnalysis({
        objectTypes,
        linkTypes,
        actionTypes,
        interfaces,
        sharedPropertyTypes: shared,
        valueTypes,
        objectViews,
        workingChanges,
        externalSources: usageExternalSources,
      }),
    [
      objectTypes,
      linkTypes,
      actionTypes,
      interfaces,
      shared,
      valueTypes,
      objectViews,
      workingChanges,
      usageExternalSources,
    ],
  );
  const permissionAnalysis = useMemo(
    () =>
      buildOntologyPermissionAnalysis({
        registry: ontologyRegistry,
        projects,
        projectMemberships,
        objectTypes,
        linkTypes,
        actionTypes,
        interfaces,
        sharedPropertyTypes: shared,
        valueTypes,
        objectViews,
        workingChanges,
        principal: {
          user_id: user?.id,
          email: user?.email,
          groups: user?.groups || [],
          roles: user?.roles || [],
          permissions: user?.permissions || [],
        },
      }),
    [
      ontologyRegistry,
      projects,
      projectMemberships,
      objectTypes,
      linkTypes,
      actionTypes,
      interfaces,
      shared,
      valueTypes,
      objectViews,
      workingChanges,
      user?.id,
      user?.email,
      user?.groups,
      user?.roles,
      user?.permissions,
    ],
  );
  const resourceSearchIndex = useMemo(
    () => {
      const nextIndex = buildOntologyResourceSearchIndex({
        registry: ontologyRegistry,
        objectTypes,
        linkTypes,
        interfaces,
        sharedPropertyTypes: shared,
        objectTypeGroups,
        objectViews,
        savedExplorations: objectSets,
        usageReferences: usageAnalysis.references,
        permissionAnalysis,
        principal: {
          user_id: user?.id,
          email: user?.email,
          groups: user?.groups || [],
          roles: user?.roles || [],
          permissions: user?.permissions || [],
        },
        previousIndex: resourceSearchIndexRef.current,
      });
      resourceSearchIndexRef.current = nextIndex;
      return nextIndex;
    },
    [
      ontologyRegistry,
      objectTypes,
      linkTypes,
      interfaces,
      shared,
      objectTypeGroups,
      objectViews,
      objectSets,
      usageAnalysis.references,
      permissionAnalysis,
      user?.id,
      user?.email,
      user?.groups,
      user?.roles,
      user?.permissions,
    ],
  );
  const branchProposalIntegration = useMemo(
    () =>
      buildOntologyBranchProposalIntegration({
        branchLabel: branchName,
        changes: workingChanges,
        objectTypes,
        linkTypes,
        actionTypes,
        interfaces,
        sharedPropertyTypes: shared,
        objectViews,
        mainObjectViews: objectViews.filter((view) => {
          const label = String(view.branch_label ?? view.config?.branch_label ?? "main").toLowerCase();
          return label === "main" || label === "default";
        }),
        propertiesByObjectType: Object.fromEntries(objectTypes.map((type) => [type.id, type.properties || []])),
        principal: {
          user_id: user?.id,
          email: user?.email,
          groups: user?.groups || [],
          roles: user?.roles || [],
          permissions: user?.permissions || [],
        },
        excludedResourceIds: proposalExcludedResourceIds,
        excludedIndexingChangeIds: proposalExcludedIndexingChangeIds,
      }),
    [
      branchName,
      workingChanges,
      objectTypes,
      linkTypes,
      actionTypes,
      interfaces,
      shared,
      objectViews,
      user?.id,
      user?.email,
      user?.groups,
      user?.roles,
      user?.permissions,
      proposalExcludedResourceIds,
      proposalExcludedIndexingChangeIds,
    ],
  );

  const cleanupAssistant = useMemo<OntologyCleanupAssistant>(
    () =>
      buildOntologyCleanupAssistant({
        objectTypes,
        linkTypes,
        actionTypes,
        interfaces,
        sharedPropertyTypes: shared,
        valueTypes,
        objectViews,
        objectTypeGroups,
        registry: ontologyRegistry,
        usageAnalysis,
        workingChanges,
      }),
    [
      objectTypes,
      linkTypes,
      actionTypes,
      interfaces,
      shared,
      valueTypes,
      objectViews,
      objectTypeGroups,
      ontologyRegistry,
      usageAnalysis,
      workingChanges,
    ],
  );
  const cleanupCandidatesById = useMemo(
    () => new Map(cleanupAssistant.candidates.map((candidate) => [candidate.id, candidate])),
    [cleanupAssistant],
  );
  useEffect(() => {
    setCleanupSelection((current) => current.filter((id) => cleanupCandidatesById.has(id)));
  }, [cleanupCandidatesById]);
  const cleanupCandidatesByKind = useMemo(() => {
    const map = new Map<OntologyCleanupCandidateKind, OntologyCleanupCandidate[]>();
    for (const candidate of cleanupAssistant.candidates) {
      const existing = map.get(candidate.kind) ?? [];
      existing.push(candidate);
      map.set(candidate.kind, existing);
    }
    return map;
  }, [cleanupAssistant]);

  const auditEventLog = useMemo<OntologyAuditEventLog>(
    () =>
      buildOntologyAuditEventLog({
        savedChanges,
        workingChanges,
        objectViews,
        filters: {
          category: auditCategoryFilter === "all" ? undefined : auditCategoryFilter,
          status: auditStatusFilter === "all" ? undefined : auditStatusFilter,
          actor: auditActorFilter || undefined,
        },
      }),
    [savedChanges, workingChanges, objectViews, auditCategoryFilter, auditStatusFilter, auditActorFilter],
  );
  const healthReport = useMemo<OntologyHealthReport>(
    () =>
      buildOntologyHealthReport({
        objectTypes,
        linkTypes,
        objectViews,
        valueTypes,
        permissionAnalysis,
      }),
    [objectTypes, linkTypes, objectViews, valueTypes, permissionAnalysis],
  );
  const filteredHealthIssues = useMemo(
    () =>
      healthReport.issues.filter((issue) => {
        if (healthCategoryFilter !== "all" && issue.category !== healthCategoryFilter) return false;
        if (healthSeverityFilter !== "all" && issue.severity !== healthSeverityFilter) return false;
        return true;
      }),
    [healthReport.issues, healthCategoryFilter, healthSeverityFilter],
  );

  async function stageCleanupSelection() {
    setCleanupNotice("");
    setError("");
    const primaryProject = projects[0];
    if (!primaryProject) {
      setError("Create or select an ontology project before staging cleanup changes.");
      return;
    }
    if (cleanupSelection.length === 0) {
      setError("Select at least one cleanup candidate before staging.");
      return;
    }
    if (!cleanupConfirmed) {
      setError("Confirm the cleanup impact review before staging changes.");
      return;
    }
    setCleanupBusy(true);
    try {
      const result = createOntologyCleanupStagedChanges({
        candidates: cleanupAssistant.candidates,
        selectedCandidateIds: cleanupSelection,
        confirmed: cleanupConfirmed,
        currentUserId,
      });
      if (result.errors.length > 0) {
        setError(result.errors.join(" "));
        return;
      }
      if (result.changes.length === 0) {
        setCleanupNotice("No supported cleanup actions were staged. Resolve manual candidates in their editors.");
        return;
      }
      const nextWorkingState = await replaceProjectWorkingState(primaryProject.id, [
        ...workingChanges,
        ...result.changes,
      ]);
      setWorkingState(nextWorkingState);
      const skipped = result.skipped.length;
      const skippedNote = skipped > 0 ? ` ${skipped} candidate${skipped === 1 ? '' : 's'} require manual resolution.` : "";
      setCleanupNotice(
        `Staged ${result.changes.length} cleanup deletion${result.changes.length === 1 ? '' : 's'} as unsaved changes. Review them in the Unsaved changes tab before saving.${skippedNote}`,
      );
      setCleanupSelection([]);
      setCleanupConfirmed(false);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Failed to stage cleanup actions");
    } finally {
      setCleanupBusy(false);
    }
  }

  const shellNavItems = useMemo(
    () =>
      SHELL_NAV_BASE.map((item) => ({
        ...item,
        count: shellNavCount(item.id, {
          objectTypes,
          actionTypes,
          interfaces,
          shared,
          valueTypes,
          linkTypes,
          objectTypeGroups,
          objectViews,
          projects,
          projectResources,
          ontologyRegistry,
          workingChanges,
          savedChanges,
          usageReferences: usageAnalysis.references,
          permissionResources: permissionAnalysis.resources,
          cleanupCandidates: cleanupAssistant.totals.candidates,
          auditEvents: auditEventLog.totals.events,
          healthIssues: healthReport.totals.issues,
        }),
      })),
    [
      objectTypes,
      actionTypes,
      interfaces,
      shared,
      valueTypes,
      linkTypes,
      objectTypeGroups,
      objectViews,
      projects,
      projectResources,
      ontologyRegistry,
      workingChanges,
      savedChanges,
      usageAnalysis.references,
      permissionAnalysis.resources,
      cleanupAssistant.totals.candidates,
      auditEventLog.totals.events,
      healthReport.totals.issues,
    ],
  );

  const visibleObjectTypes = useMemo(
    () =>
      objectTypes.filter((entry) =>
        matchesResourceFilter(entry, resourceFilter, projectResources),
      ),
    [objectTypes, resourceFilter, projectResources],
  );

  const visibleObjectTypeGroups = useMemo(() => {
    const needle = groupSearch.trim().toLowerCase();
    if (!needle) return objectTypeGroups;
    return objectTypeGroups.filter((group) =>
      [group.name, group.display_name, group.description, group.visibility, group.status]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(needle)),
    );
  }, [objectTypeGroups, groupSearch]);

  const recentResources = useMemo(
    () =>
      buildRecentResources({
        objectTypes,
        actionTypes,
        interfaces,
        shared,
        valueTypes,
        linkTypes,
        objectTypeGroups,
        objectViews,
      }),
    [objectTypes, actionTypes, interfaces, shared, linkTypes, objectTypeGroups, objectViews],
  );

  const indexedSearchResults = useMemo(
    () =>
      search.trim()
        ? searchOntologyResourceIndex(resourceSearchIndex, {
            query: search,
            page: 1,
            per_page: 8,
            permission_filter: "viewable",
          })
        : null,
    [resourceSearchIndex, search],
  );
  const searchResults = useMemo(
    () => indexedSearchResults?.data.map(searchDocumentToShellResult) ?? [],
    [indexedSearchResults],
  );

  const shellWarnings = useMemo(
    () =>
      buildShellWarnings({ ontology, objectTypes, projects, projectResources }),
    [ontology, objectTypes, projects, projectResources],
  );

  return (
    <section
      className="of-page"
      style={{ padding: 24, display: "grid", gap: 16 }}
    >
      <header
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "flex-start",
          gap: 12,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <span
            style={{
              display: "inline-flex",
              alignItems: "center",
              justifyContent: "center",
              width: 32,
              height: 32,
              borderRadius: 4,
              background: "rgba(45, 114, 210, 0.12)",
              color: "var(--status-info)",
            }}
          >
            <Glyph name="cube" size={18} tone="var(--status-info)" />
          </span>
          <div>
            <h1 className="of-heading-xl" style={{ margin: 0 }}>
              Ontology Manager
            </h1>
            <p
              className="of-text-muted"
              style={{ margin: "2px 0 0", fontSize: 12 }}
            >
              <Glyph name="folder" size={11} tone="#5c7080" />{" "}
              {ontology.owning_space_slug} · {ontology.display_name}
            </p>
          </div>
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <Link
            to="/ontology-manager/bindings"
            className="of-button"
            style={{ fontSize: 12 }}
          >
            <Glyph name="link" size={12} /> Bind dataset
          </Link>
          <div ref={branchMenuRef} style={{ position: "relative" }}>
            <button
              type="button"
              className="of-button"
              onClick={() => setBranchMenuOpen((open) => !open)}
            >
              <Glyph name="cube" size={12} tone="#5c7080" /> {branchName}{" "}
              <Glyph name="chevron-down" size={11} />
            </button>
            {branchMenuOpen ? (
              <div role="menu" style={popoverStyle()}>
                <input
                  className="of-input"
                  placeholder="Search branches..."
                  style={{ marginBottom: 6 }}
                />
                <p
                  className="of-text-muted"
                  style={{
                    margin: "4px 0 4px 6px",
                    fontSize: 11,
                    textTransform: "uppercase",
                    letterSpacing: "0.05em",
                  }}
                >
                  Create a new branch
                </p>
                <button
                  type="button"
                  onClick={() => {
                    setBranchMenuOpen(false);
                    setBranchDialogOpen(true);
                  }}
                  style={menuItemStyle()}
                >
                  <Glyph name="cube" size={12} tone="#5c7080" /> {branchName}
                  /new-branch
                </button>
                <p
                  className="of-text-muted"
                  style={{ margin: "6px 0 4px 6px", fontSize: 11 }}
                >
                  No results
                </p>
                <button
                  type="button"
                  disabled
                  style={{
                    ...menuItemStyle(),
                    justifyContent: "space-between",
                  }}
                >
                  Show more Ontology branches{" "}
                  <Glyph name="chevron-down" size={11} />
                </button>
                <div style={{ padding: 6 }}>
                  <button
                    type="button"
                    onClick={() => {
                      setBranchMenuOpen(false);
                      setBranchDialogOpen(true);
                    }}
                    style={{
                      width: "100%",
                      display: "inline-flex",
                      alignItems: "center",
                      justifyContent: "center",
                      gap: 6,
                      padding: "8px 14px",
                      border: 0,
                      borderRadius: 4,
                      background: "#15803d",
                      color: "#fff",
                      fontSize: 13,
                      fontWeight: 600,
                      cursor: "pointer",
                    }}
                  >
                    <Glyph name="plus" size={12} /> Create branch
                  </button>
                </div>
              </div>
            ) : null}
          </div>
          <div ref={newMenuRef} style={{ position: "relative" }}>
            <button
              type="button"
              className="of-button of-button--primary"
              onClick={() => setNewMenuOpen((open) => !open)}
            >
              New <Glyph name="chevron-down" size={11} />
            </button>
            {newMenuOpen ? (
              <div role="menu" style={popoverStyle({ minWidth: 320 })}>
                <NewMenuItem
                  glyph={
                    <Glyph name="cube" size={14} tone="var(--status-info)" />
                  }
                  label="Object type"
                  description="Map datasets and models to object types"
                  enabled
                  onClick={() => {
                    setNewMenuOpen(false);
                    setWizardOpen(true);
                  }}
                  highlighted
                />
                <NewMenuItem
                  glyph={<Glyph name="link" size={14} tone="#5c7080" />}
                  label="Link type"
                  description="Create relationships between object types"
                />
                <NewMenuItem
                  glyph={<Glyph name="run" size={14} tone="#7c5dd6" />}
                  label="Action type"
                  description="Allow users to writeback to their ontology"
                />
                <div
                  style={{
                    borderTop: "1px solid var(--border-subtle)",
                    margin: "4px 0",
                  }}
                />
                <NewMenuItem
                  glyph={<Glyph name="ontology" size={14} tone="#5c7080" />}
                  label="Shared property"
                  description="Create properties that can be shared across object types"
                />
                <NewMenuItem
                  glyph={<Glyph name="artifact" size={14} tone="#5c7080" />}
                  label="Interface"
                  description="Use interfaces to build against abstract types"
                />
                <NewMenuItem
                  glyph={
                    <span
                      style={{
                        fontFamily: "serif",
                        fontStyle: "italic",
                        fontSize: 14,
                        color: "#5c7080",
                      }}
                    >
                      fx
                    </span>
                  }
                  label="Function"
                  description="Define object modifications in code"
                />
              </div>
            ) : null}
          </div>
        </div>
      </header>

      {error && (
        <div
          className="of-status-danger"
          style={{
            padding: "10px 14px",
            borderRadius: "var(--radius-md)",
            fontSize: 13,
          }}
        >
          {error}
        </div>
      )}

      <section
        className="of-panel"
        style={{ padding: 16, display: "grid", gap: 12 }}
      >
        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            gap: 8,
            alignItems: "center",
          }}
        >
          <div style={{ flex: "1 1 360px", position: "relative" }}>
            <input
              ref={searchInputRef}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search ontology resources, properties, links, actions, interfaces, or projects…"
              className="of-input"
              aria-label="Search ontology resources"
            />
            <span
              className="of-text-muted"
              style={{ position: "absolute", right: 10, top: 8, fontSize: 11 }}
            >
              Ctrl/⌘ K
            </span>
          </div>
          <button
            type="button"
            onClick={() => void refresh()}
            className="of-button"
          >
            Apply
          </button>
        </div>
        <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
          {FILTERS.map((filter) => (
            <button
              key={filter.id}
              type="button"
              onClick={() => setResourceFilter(filter.id)}
              style={filterButtonStyle(resourceFilter === filter.id)}
            >
              {filter.label}
            </button>
          ))}
        </div>
        {search.trim() ? (
          <div
            style={{
              borderTop: "1px solid var(--border-subtle)",
              paddingTop: 10,
            }}
          >
            <p className="of-eyebrow" style={{ marginBottom: 8 }}>
              Global search results ({indexedSearchResults?.total ?? 0})
            </p>
            {indexedSearchResults?.hidden_results ? (
              <p className="of-text-muted" style={{ margin: "-4px 0 8px", fontSize: 12 }}>
                {indexedSearchResults.hidden_results} result{indexedSearchResults.hidden_results === 1 ? "" : "s"} hidden by resource permissions or visibility.
              </p>
            ) : null}
            <div style={{ display: "grid", gap: 6 }}>
              {searchResults.slice(0, 8).map((result) => (
                <button
                  key={result.id}
                  type="button"
                  onClick={() => setSection(result.section)}
                  style={searchResultStyle()}
                >
                  <strong>{result.label}</strong>
                  <span>
                    {result.kind} · {result.detail}
                  </span>
                </button>
              ))}
              {searchResults.length === 0 ? (
                <span className="of-text-muted" style={{ fontSize: 12 }}>
                  No matching ontology resources.
                </span>
              ) : null}
            </div>
          </div>
        ) : null}
      </section>

      <section
        style={{
          display: "grid",
          gridTemplateColumns: "280px minmax(0, 1fr)",
          gap: 16,
          alignItems: "start",
        }}
      >
        <aside
          className="of-panel"
          style={{ padding: 12, position: "sticky", top: 12 }}
        >
          <p className="of-eyebrow" style={{ margin: "4px 4px 10px" }}>
            Ontology Manager navigation
          </p>
          <nav
            aria-label="Ontology Manager sections"
            style={{ display: "grid", gap: 4 }}
          >
            {shellNavItems.map((item) => (
              <button
                key={item.id}
                type="button"
                onClick={() => setSection(item.id)}
                style={shellNavButtonStyle(section === item.id)}
              >
                <span
                  style={{
                    display: "flex",
                    justifyContent: "space-between",
                    gap: 8,
                  }}
                >
                  <strong>{item.label}</strong>
                  {typeof item.count === "number" ? (
                    <span>{item.count}</span>
                  ) : null}
                </span>
                <span>{item.description}</span>
              </button>
            ))}
          </nav>
          <div
            style={{
              borderTop: "1px solid var(--border-subtle)",
              marginTop: 12,
              paddingTop: 12,
            }}
          >
            <p className="of-eyebrow">Project / security context</p>
            <p style={{ margin: "6px 0 0", fontSize: 12 }}>
              <strong>Space:</strong> {ontology.owning_space_slug}
            </p>
            <p style={{ margin: "4px 0 0", fontSize: 12 }}>
              <strong>Project:</strong>{" "}
              {ontology.placement.project_display_name}
            </p>
            <p style={{ margin: "4px 0 0", fontSize: 12 }}>
              <strong>User:</strong> {user?.email || user?.name || "Anonymous"}
            </p>
            <p style={{ margin: "4px 0 0", fontSize: 12 }}>
              <strong>Roles:</strong>{" "}
              {user?.roles?.length ? user.roles.join(", ") : "viewer"}
            </p>
            <p style={{ margin: "4px 0 0", fontSize: 12 }}>
              <strong>Access:</strong>{" "}
              {ontology.access_mode === "shared" ? "shared" : "private"}
            </p>
          </div>
        </aside>
        <div style={{ display: "grid", gap: 16 }}>
          {shellWarnings.map((warning) => (
            <div
              key={warning}
              className="of-status-warning"
              style={{
                padding: "10px 14px",
                borderRadius: "var(--radius-md)",
                fontSize: 13,
              }}
            >
              {warning}
            </div>
          ))}

          {section === "overview" && (
            <section
              style={{
                display: "grid",
                gridTemplateColumns:
                  "minmax(280px, 1.2fr) minmax(260px, 0.8fr)",
                gap: 16,
              }}
            >
              <article
                className="of-panel"
                style={{ padding: 16, display: "grid", gap: 14 }}
              >
                <div>
                  <p className="of-eyebrow">Ontology metadata</p>
                  <h2 className="of-heading-lg" style={{ margin: "4px 0" }}>
                    {ontology.display_name}
                  </h2>
                  <p
                    className="of-text-muted"
                    style={{ margin: 0, fontSize: 13 }}
                  >
                    {ontology.description}
                  </p>
                </div>
                <dl
                  style={{
                    display: "grid",
                    gridTemplateColumns: "repeat(auto-fit, minmax(160px, 1fr))",
                    gap: 12,
                    margin: 0,
                  }}
                >
                  <OntologyMetadataTerm
                    label="Ontology ID"
                    value={ontology.id}
                  />
                  <OntologyMetadataTerm
                    label="API name"
                    value={ontology.api_name}
                  />
                  <OntologyMetadataTerm
                    label="Owning space"
                    value={ontology.owning_space_slug}
                  />
                  <OntologyMetadataTerm
                    label="Access"
                    value={
                      ontology.access_mode === "shared"
                        ? "Shared ontology"
                        : "Private ontology"
                    }
                  />
                  <OntologyMetadataTerm
                    label="Project placement"
                    value={ontology.placement.project_display_name}
                  />
                  <OntologyMetadataTerm
                    label="Folder"
                    value={ontology.placement.folder_path}
                  />
                </dl>
                <div
                  style={{
                    padding: 12,
                    borderRadius: 6,
                    background: "rgba(15, 118, 110, 0.08)",
                    border: "1px solid rgba(15, 118, 110, 0.18)",
                    fontSize: 12,
                  }}
                >
                  <strong>OpenFoundry access semantics:</strong> private
                  ontologies are visible through one organization marking on
                  their owning space; shared ontologies list multiple
                  organization markings so teams can share object types, links,
                  actions, and workflows safely.
                </div>
              </article>

              <article
                className="of-panel"
                style={{ padding: 16, display: "grid", gap: 14 }}
              >
                <div>
                  <p className="of-eyebrow">Organizations</p>
                  <div
                    style={{
                      display: "flex",
                      flexWrap: "wrap",
                      gap: 8,
                      marginTop: 8,
                    }}
                  >
                    {ontology.organizations.map((org) => (
                      <span key={org.id} style={pillStyle("#1d4ed8")}>
                        {org.display_name} · {org.marking}
                      </span>
                    ))}
                  </div>
                </div>
                <div>
                  <p className="of-eyebrow">Linked resources</p>
                  <ul
                    style={{
                      marginTop: 8,
                      paddingLeft: 0,
                      listStyle: "none",
                      display: "grid",
                      gap: 6,
                    }}
                  >
                    {ontology.linked_resources.map((resource) => (
                      <li
                        key={resource.resource_kind}
                        style={{
                          display: "flex",
                          justifyContent: "space-between",
                          fontSize: 12,
                          padding: "6px 0",
                          borderBottom: "1px solid var(--border-subtle)",
                        }}
                      >
                        <span>{resourceKindLabel(resource.resource_kind)}</span>
                        <strong>{resource.count}</strong>
                      </li>
                    ))}
                    {ontology.linked_resources.length === 0 ? (
                      <li className="of-text-muted" style={{ fontSize: 12 }}>
                        No linked ontology resources yet.
                      </li>
                    ) : null}
                  </ul>
                </div>
              </article>

              <article className="of-panel" style={{ padding: 16 }}>
                <p className="of-eyebrow">Stats</p>
                <ul style={{ marginTop: 8, paddingLeft: 18, fontSize: 12 }}>
                  <li>{ontologyRegistry.length} registry entries</li>
                  <li>{objectTypes.length} object types</li>
                  <li>{interfaces.length} interfaces</li>
                  <li>{shared.length} shared property types</li>
                  <li>{linkTypes.length} link types</li>
                  <li>{actionTypes.length} action types</li>
                  <li>{objectViews.length} Object Views</li>
                  <li>{objectSets.length} saved explorations / lists</li>
                  <li>{resourceSearchIndex.documents.length} indexed search documents</li>
                  <li>{projects.length} projects</li>
                  <li>{projectResources.length} project-bound resources</li>
                </ul>
              </article>
              <article className="of-panel" style={{ padding: 16 }}>
                <p className="of-eyebrow">Recently edited resources</p>
                <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: "none" }}>
                  {recentResources.map((resource) => (
                    <li
                      key={resource.id}
                      style={{
                        padding: "6px 0",
                        borderBottom: "1px solid var(--border-subtle)",
                        fontSize: 12,
                      }}
                    >
                      <strong>{resource.label}</strong> · {resource.kind}
                      <p
                        className="of-text-muted"
                        style={{ margin: 0, fontSize: 11 }}
                      >
                        {resource.detail}
                      </p>
                    </li>
                  ))}
                  {recentResources.length === 0 ? (
                    <li className="of-text-muted" style={{ fontSize: 12 }}>
                      No recently edited resources yet.
                    </li>
                  ) : null}
                </ul>
              </article>
              <article className="of-panel" style={{ padding: 16 }}>
                <p className="of-eyebrow">Related routes</p>
                <ul style={{ marginTop: 8, paddingLeft: 18, fontSize: 12 }}>
                  <li>
                    <Link to="/object-link-types">Object & link types →</Link>
                  </li>
                  <li>
                    <Link to="/interfaces">Interfaces →</Link>
                  </li>
                  <li>
                    <Link to="/ontologies">Ontology projects →</Link>
                  </li>
                  <li>
                    <Link to="/ontology-design">Ontology design →</Link>
                  </li>
                  <li>
                    <Link to="/ontology-indexing">
                      Ontology indexing (Funnel) →
                    </Link>
                  </li>
                  <li>
                    <Link to="/projects">Workspace projects →</Link>
                  </li>
                  <li>
                    <Link to="/ontology-manager/bindings">
                      Dataset → ObjectType bindings →
                    </Link>
                  </li>
                </ul>
              </article>
            </section>
          )}

          {section === "registry" && (
            <OntologyRegistryPanel
              registry={ontologyRegistry}
              searchIndex={resourceSearchIndex}
              initialQuery={search}
            />
          )}

          {section === "types" && (
            <section className="of-panel" style={{ padding: 16 }}>
              <p className="of-eyebrow">
                Object types ({visibleObjectTypes.length})
              </p>
              <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: "none" }}>
                {visibleObjectTypes.map((t) => (
                  <li
                    key={t.id}
                    style={{
                      padding: 8,
                      borderBottom: "1px solid var(--border-subtle)",
                    }}
                  >
                    <strong>{t.display_name}</strong> · {t.name} · pk:{" "}
                    {t.primary_key_property ?? "—"}
                  </li>
                ))}
              </ul>
            </section>
          )}

          {section === "actions" && (
            <section className="of-panel" style={{ padding: 16 }}>
              <p className="of-eyebrow">Action types ({actionTypes.length})</p>
              <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: "none" }}>
                {actionTypes.map((action) => (
                  <li key={action.id} style={resourceRowStyle()}>
                    <strong>{action.display_name}</strong> · {action.name} ·{" "}
                    {action.operation_kind}
                    {action.description ? (
                      <p
                        className="of-text-muted"
                        style={{ fontSize: 11, margin: 0 }}
                      >
                        {action.description}
                      </p>
                    ) : null}
                  </li>
                ))}
                {actionTypes.length === 0 ? (
                  <li className="of-text-muted" style={{ fontSize: 12 }}>
                    No action types have been authored yet.
                  </li>
                ) : null}
              </ul>
            </section>
          )}

          {section === "interfaces" && (
            <section className="of-panel" style={{ padding: 16 }}>
              <p className="of-eyebrow">Interfaces ({interfaces.length})</p>
              <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: "none" }}>
                {interfaces.map((i) => (
                  <li
                    key={i.id}
                    style={{
                      padding: 8,
                      borderBottom: "1px solid var(--border-subtle)",
                    }}
                  >
                    <strong>{i.display_name}</strong> · {i.name}
                    {i.description && (
                      <p
                        className="of-text-muted"
                        style={{ fontSize: 11, margin: 0 }}
                      >
                        {i.description}
                      </p>
                    )}
                  </li>
                ))}
              </ul>
            </section>
          )}

          {section === "shared" && (
            <section className="of-panel" style={{ padding: 16, display: "grid", gap: 14 }}>
              <div>
                <p className="of-eyebrow">Shared property types ({shared.length})</p>
                <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
                  Centralize reusable property metadata. Edits warn when the property appears in multiple object/interface bindings.
                </p>
              </div>
              <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))", gap: 8 }}>
                <input className="of-input" placeholder="API name" value={sharedDraft.name} onChange={(event) => setSharedDraft({ ...sharedDraft, name: event.target.value })} />
                <input className="of-input" placeholder="Display name" value={sharedDraft.display_name} onChange={(event) => setSharedDraft({ ...sharedDraft, display_name: event.target.value })} />
                <select className="of-input" value={sharedDraft.property_type} onChange={(event) => setSharedDraft({ ...sharedDraft, property_type: event.target.value })}>
                  {PROPERTY_BASE_TYPE_OPTIONS.map((option) => <option key={option} value={option}>{option}</option>)}
                </select>
                <select className="of-input" value={sharedDraft.value_type_id} onChange={(event) => setSharedDraft({ ...sharedDraft, value_type_id: event.target.value })}>
                  <option value="">No value type</option>
                  {valueTypes.map((valueType) => <option key={valueType.id} value={valueType.id}>{valueType.display_name}</option>)}
                </select>
              </div>
              <textarea className="of-input" placeholder="Description" value={sharedDraft.description} onChange={(event) => setSharedDraft({ ...sharedDraft, description: event.target.value })} rows={2} />
              <div style={{ display: "flex", gap: 8 }}>
                <button type="button" className="of-button of-button--primary" onClick={() => void saveSharedProperty()}>{editingSharedId ? "Save shared property" : "Create shared property"}</button>
                {editingSharedId ? <button type="button" className="of-button" onClick={resetSharedDraft}>Cancel</button> : null}
              </div>
              <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: "none", display: "grid", gap: 8 }}>
                {shared.map((s) => {
                  const usage = sharedPropertyUsageSummary(s.id, { objectTypes, interfaces });
                  const warning = sharedPropertyImpactWarning(s, usage);
                  return (
                    <li key={s.id} style={{ padding: 10, border: "1px solid var(--border-subtle)", borderRadius: 8, display: "grid", gap: 6 }}>
                      <div style={{ display: "flex", justifyContent: "space-between", gap: 8, alignItems: "flex-start" }}>
                        <div>
                          <strong>{s.display_name}</strong> <span className="of-text-muted">· {s.name} · {s.property_type}</span>
                          {s.value_type_id ? <p className="of-text-muted" style={{ margin: "3px 0 0", fontSize: 11 }}>Value type: {valueTypes.find((valueType) => valueType.id === s.value_type_id)?.display_name || s.value_type_id}</p> : null}
                        </div>
                        <span className="of-chip">{usage.total} usages</span>
                      </div>
                      {warning ? <div className="of-status-warning" style={{ padding: 8, borderRadius: 6, fontSize: 12 }}>{warning}</div> : null}
                      <div style={{ display: "flex", gap: 6 }}>
                        <button type="button" className="of-button" onClick={() => editSharedProperty(s)}>Edit</button>
                        <button type="button" className="of-button" onClick={() => void removeSharedProperty(s)} style={{ color: "#b91c1c" }}>Delete</button>
                      </div>
                    </li>
                  );
                })}
                {shared.length === 0 ? <li className="of-text-muted">No shared properties yet.</li> : null}
              </ul>
            </section>
          )}

          {section === "valueTypes" && (
            <section className="of-panel" style={{ padding: 16, display: "grid", gap: 14 }}>
              <div>
                <p className="of-eyebrow">Value types ({valueTypes.length})</p>
                <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
                  Define space-scoped semantic wrappers, reusable validation constraints, formatting, permissions, and breaking-version history.
                </p>
              </div>
              <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))", gap: 8 }}>
                <input className="of-input" placeholder="API name" value={valueTypeDraft.name} onChange={(event) => setValueTypeDraft({ ...valueTypeDraft, name: event.target.value })} />
                <input className="of-input" placeholder="Display name" value={valueTypeDraft.display_name} onChange={(event) => setValueTypeDraft({ ...valueTypeDraft, display_name: event.target.value })} />
                <select className="of-input" value={valueTypeDraft.base_type} onChange={(event) => setValueTypeDraft({ ...valueTypeDraft, base_type: event.target.value })}>
                  {PROPERTY_BASE_TYPE_OPTIONS.map((option) => <option key={option} value={option}>{option}</option>)}
                </select>
                <input className="of-input" placeholder="Semantic type (email, URL, currency...)" value={valueTypeDraft.semantic_type} onChange={(event) => setValueTypeDraft({ ...valueTypeDraft, semantic_type: event.target.value })} />
              </div>
              <textarea className="of-input" placeholder="Description" value={valueTypeDraft.description} onChange={(event) => setValueTypeDraft({ ...valueTypeDraft, description: event.target.value })} rows={2} />
              <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))", gap: 8 }}>
                <label style={{ fontSize: 12 }}>Validation constraints JSON
                  <textarea className="of-input" value={valueTypeDraft.constraints_json} onChange={(event) => setValueTypeDraft({ ...valueTypeDraft, constraints_json: event.target.value })} rows={5} style={{ marginTop: 4, fontFamily: "var(--font-mono)", fontSize: 11 }} />
                </label>
                <label style={{ fontSize: 12 }}>Formatting JSON
                  <textarea className="of-input" value={valueTypeDraft.formatting_json} onChange={(event) => setValueTypeDraft({ ...valueTypeDraft, formatting_json: event.target.value })} rows={5} style={{ marginTop: 4, fontFamily: "var(--font-mono)", fontSize: 11 }} />
                </label>
              </div>
              <div style={{ display: "flex", gap: 8 }}>
                <button type="button" className="of-button of-button--primary" onClick={() => void saveValueType()}>{editingValueTypeId ? "Save value type" : "Create value type"}</button>
                {editingValueTypeId ? <button type="button" className="of-button" onClick={resetValueTypeDraft}>Cancel</button> : null}
              </div>
              <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: "none", display: "grid", gap: 8 }}>
                {valueTypes.map((valueType) => {
                  const usage = valueTypeUsageSummary(valueType.id, { objectTypes, sharedPropertyTypes: shared, interfaces });
                  return (
                    <li key={valueType.id} style={{ padding: 10, border: "1px solid var(--border-subtle)", borderRadius: 8, display: "grid", gap: 6 }}>
                      <div style={{ display: "flex", justifyContent: "space-between", gap: 8 }}>
                        <div>
                          <strong>{valueType.display_name}</strong> <span className="of-text-muted">· {valueType.name} · {valueType.base_type}</span>
                          <p className="of-text-muted" style={{ margin: "3px 0 0", fontSize: 11 }}>{valueType.semantic_type} · v{valueType.version} · {valueType.status}</p>
                        </div>
                        <span className="of-chip">{usage.total} usages</span>
                      </div>
                      <div style={{ display: "flex", gap: 6 }}>
                        <button type="button" className="of-button" onClick={() => editValueType(valueType)}>Edit</button>
                        <button type="button" className="of-button" onClick={() => void removeValueType(valueType)} style={{ color: "#b91c1c" }}>Delete</button>
                      </div>
                    </li>
                  );
                })}
                {valueTypes.length === 0 ? <li className="of-text-muted">No value types yet.</li> : null}
              </ul>
            </section>
          )}

          {section === "links" && (
            <section
              className="of-panel"
              style={{
                padding: 16,
                display: "grid",
                gridTemplateColumns: "minmax(0, 1fr) 380px",
                gap: 16,
              }}
            >
              <div>
                <p className="of-eyebrow">Link types ({linkTypes.length})</p>
                <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
                  Create and review typed relationships between object types, including self-links, one-to-one, one-to-many, many-to-one, and many-to-many links with datasource key mappings.
                </p>
                <ul style={{ marginTop: 12, paddingLeft: 0, listStyle: "none" }}>
                  {linkTypes.map((l) => {
                    const labels = linkTypeEndpointLabels(l);
                    const source = objectTypes.find((type) => type.id === l.source_type_id);
                    const target = objectTypes.find((type) => type.id === l.target_type_id);
                    return (
                      <li
                        key={l.id}
                        style={{
                          padding: 10,
                          border: "1px solid var(--border-subtle)",
                          borderRadius: 8,
                          marginBottom: 8,
                        }}
                      >
                        <div style={{ display: "flex", justifyContent: "space-between", gap: 12 }}>
                          <div>
                            <strong>{l.display_name}</strong> · <code>{l.name}</code>
                            <p className="of-text-muted" style={{ margin: "4px 0 0", fontSize: 12 }}>
                              {source?.display_name || l.source_type_id} → {target?.display_name || l.target_type_id}
                              {l.source_type_id === l.target_type_id ? " · self-link" : ""}
                            </p>
                          </div>
                          <span className="of-badge">{linkTypeCardinalityLabel(l.cardinality)}</span>
                        </div>
                        <dl style={{ display: "grid", gridTemplateColumns: "repeat(3, minmax(0, 1fr))", gap: 8, margin: "10px 0 0", fontSize: 12 }}>
                          <div>
                            <dt className="of-text-muted">Forward label</dt>
                            <dd style={{ margin: 0 }}>{labels.forward}</dd>
                          </div>
                          <div>
                            <dt className="of-text-muted">Reverse label</dt>
                            <dd style={{ margin: 0 }}>{labels.reverse}</dd>
                          </div>
                          <div>
                            <dt className="of-text-muted">Visibility</dt>
                            <dd style={{ margin: 0 }}>{l.visibility || "normal"}</dd>
                          </div>
                        </dl>
                        {l.cardinality === "many_to_many" && (
                          <p className="of-text-muted" style={{ margin: "8px 0 0", fontSize: 12 }}>
                            Datasource: {l.link_datasource_mapping?.datasource_id || "not mapped"} · source key {l.link_datasource_mapping?.source_key || "—"} · target key {l.link_datasource_mapping?.target_key || "—"}
                            {!linkTypeHasDatasourceMapping(l) ? " · missing required mapping" : ""}
                          </p>
                        )}
                      </li>
                    );
                  })}
                </ul>
              </div>
              <LinkEditor onCreated={refreshLinkType} onUpdated={refreshLinkType} onDeleted={removeLinkType} />
            </section>
          )}

          {section === "groups" && (
            <section
              className="of-panel"
              style={{
                padding: 16,
                display: "grid",
                gridTemplateColumns: "minmax(0, 1fr) 380px",
                gap: 16,
              }}
            >
              <div>
                <p className="of-eyebrow">Object type groups ({visibleObjectTypeGroups.length})</p>
                <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
                  Create searchable, permissionable groups and assign object types for Ontology Manager and Object Explorer discovery.
                </p>
                <input
                  className="of-input"
                  value={groupSearch}
                  onChange={(event) => setGroupSearch(event.target.value)}
                  onBlur={() => void refresh()}
                  placeholder="Search groups…"
                  style={{ marginTop: 10 }}
                />
                <ul style={{ marginTop: 12, paddingLeft: 0, listStyle: "none" }}>
                  {visibleObjectTypeGroups.map((group) => (
                    <li
                      key={group.id}
                      style={{
                        padding: 10,
                        border: "1px solid var(--border-subtle)",
                        borderRadius: 8,
                        marginBottom: 8,
                      }}
                    >
                      <div style={{ display: "flex", justifyContent: "space-between", gap: 12 }}>
                        <div>
                          <strong>{group.display_name}</strong> · <code>{group.name}</code>
                          {group.description ? (
                            <p className="of-text-muted" style={{ margin: "4px 0 0", fontSize: 12 }}>
                              {group.description}
                            </p>
                          ) : null}
                        </div>
                        <span className="of-badge">{group.object_type_count} types</span>
                      </div>
                      <p className="of-text-muted" style={{ margin: "8px 0 0", fontSize: 12 }}>
                        {group.visibility || "normal"} · {group.status || "active"} · project {group.project_id || "unbound"}
                      </p>
                      <p className="of-text-muted" style={{ margin: "8px 0 0", fontSize: 12 }}>
                        Members: {objectTypes.filter((type) => group.object_type_ids.includes(type.id)).map((type) => type.display_name).join(", ") || "none"}
                      </p>
                      <div style={{ display: "flex", gap: 6, marginTop: 8 }}>
                        <button type="button" className="of-button" onClick={() => editGroup(group)} style={{ fontSize: 11 }}>
                          Edit
                        </button>
                        <button type="button" className="of-button" onClick={() => void removeGroup(group)} disabled={groupSaving} style={{ fontSize: 11, color: "#fca5a5", borderColor: "#7f1d1d" }}>
                          Delete
                        </button>
                      </div>
                    </li>
                  ))}
                </ul>
              </div>
              <article style={{ display: "flex", flexDirection: "column", gap: 10 }}>
                <h3 style={{ margin: 0, fontSize: 14 }}>{editingGroupId ? "Edit group" : "New group"}</h3>
                <label style={{ fontSize: 12 }}>
                  Display name
                  <input className="of-input" value={groupDraft.display_name} onChange={(event) => setGroupDraft({ ...groupDraft, display_name: event.target.value })} style={{ marginTop: 4 }} />
                </label>
                <label style={{ fontSize: 12 }}>
                  API name
                  <input className="of-input" value={groupDraft.name} onChange={(event) => setGroupDraft({ ...groupDraft, name: event.target.value })} style={{ marginTop: 4, fontFamily: "var(--font-mono)" }} placeholder={slugifyGroupName(groupDraft.display_name) || "asset_group"} />
                </label>
                <label style={{ fontSize: 12 }}>
                  Description
                  <textarea className="of-input" rows={2} value={groupDraft.description} onChange={(event) => setGroupDraft({ ...groupDraft, description: event.target.value })} style={{ marginTop: 4 }} />
                </label>
                <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
                  <label style={{ fontSize: 12 }}>
                    Visibility
                    <select className="of-input" value={groupDraft.visibility} onChange={(event) => setGroupDraft({ ...groupDraft, visibility: event.target.value })} style={{ marginTop: 4 }}>
                      <option value="normal">normal</option>
                      <option value="hidden">hidden</option>
                      <option value="experimental">experimental</option>
                    </select>
                  </label>
                  <label style={{ fontSize: 12 }}>
                    Status
                    <select className="of-input" value={groupDraft.status} onChange={(event) => setGroupDraft({ ...groupDraft, status: event.target.value })} style={{ marginTop: 4 }}>
                      <option value="active">active</option>
                      <option value="experimental">experimental</option>
                      <option value="deprecated">deprecated</option>
                    </select>
                  </label>
                </div>
                <label style={{ fontSize: 12 }}>
                  Permission project
                  <select className="of-input" value={groupDraft.project_id} onChange={(event) => setGroupDraft({ ...groupDraft, project_id: event.target.value })} style={{ marginTop: 4 }}>
                    <option value="">Unbound</option>
                    {projects.map((project) => (
                      <option key={project.id} value={project.id}>{project.display_name}</option>
                    ))}
                  </select>
                </label>
                <fieldset style={{ border: "1px solid var(--border-subtle)", borderRadius: 8, padding: 10 }}>
                  <legend style={{ padding: "0 4px", fontSize: 12 }}>Object types</legend>
                  <div style={{ display: "grid", gap: 6, maxHeight: 220, overflow: "auto" }}>
                    {objectTypes.map((type) => {
                      const checked = groupDraft.object_type_ids.includes(type.id);
                      return (
                        <label key={type.id} style={{ display: "flex", gap: 8, alignItems: "center", fontSize: 12 }}>
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={(event) =>
                              setGroupDraft((draft) => ({
                                ...draft,
                                object_type_ids: event.target.checked
                                  ? [...draft.object_type_ids, type.id]
                                  : draft.object_type_ids.filter((id) => id !== type.id),
                              }))
                            }
                          />
                          {type.display_name} <span className="of-text-muted">({type.name})</span>
                        </label>
                      );
                    })}
                  </div>
                </fieldset>
                <div style={{ display: "flex", gap: 6 }}>
                  <button type="button" className="of-button of-button--primary" onClick={() => void saveGroup()} disabled={groupSaving}>
                    {groupSaving ? "Saving…" : editingGroupId ? "Update group" : "Create group"}
                  </button>
                  {editingGroupId ? (
                    <button type="button" className="of-button" onClick={resetGroupDraft} disabled={groupSaving}>Cancel</button>
                  ) : null}
                </div>
              </article>
            </section>
          )}

          {section === "views" && (
            <section className="of-panel" style={{ padding: 16 }}>
              <p className="of-eyebrow">Object Views ({objectViews.length})</p>
              <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: "none" }}>
                {objectViews.map((view) => (
                  <li key={view.id} style={resourceRowStyle()}>
                    <strong>{view.display_name}</strong> · {view.name} ·{" "}
                    {view.form_factor} · {view.mode}
                    {view.description ? (
                      <p
                        className="of-text-muted"
                        style={{ fontSize: 11, margin: 0 }}
                      >
                        {view.description}
                      </p>
                    ) : null}
                  </li>
                ))}
                {objectViews.length === 0 ? (
                  <li className="of-text-muted" style={{ fontSize: 12 }}>
                    Core Object Views will be generated after object type view
                    parity lands.
                  </li>
                ) : null}
              </ul>
            </section>
          )}

          {section === "usage" && (
            <OntologyUsagePanel
              analysis={usageAnalysis}
              notice={usageNotice}
            />
          )}

          {section === "permissions" && (
            <OntologyPermissionsPanel
              analysis={permissionAnalysis}
              userLabel={user?.email || user?.name || currentUserId || "Anonymous"}
            />
          )}

          {section === "changes" && (
            <section className="of-panel" style={{ padding: 16 }}>
              <p className="of-eyebrow">Unsaved changes ({workingChanges.length})</p>
              <p className="of-text-muted" style={{ marginTop: 6, fontSize: 12 }}>
                Restore actions from history appear here as staged ontology changes. They do not take effect until saved from the project working state.
              </p>
              {usageAnalysis.warnings.length > 0 ? (
                <div className={usageAnalysis.totals.errors > 0 ? "of-status-danger" : "of-status-warning"} style={{ marginTop: 10, padding: 10, borderRadius: 6, fontSize: 12 }}>
                  {usageAnalysis.warnings.length} downstream usage warning{usageAnalysis.warnings.length === 1 ? "" : "s"} detected. Review the Usage tab before saving these changes.
                </div>
              ) : null}
              {permissionAnalysis.blocked_changes > 0 ? (
                <div className="of-status-danger" style={{ marginTop: 10, padding: 10, borderRadius: 6, fontSize: 12 }}>
                  {permissionAnalysis.blocked_changes} staged change{permissionAnalysis.blocked_changes === 1 ? "" : "s"} require additional ontology resource edit permissions. Review the Permissions tab before saving.
                </div>
              ) : null}
              <OntologyBranchProposalPreviewPanel
                integration={branchProposalIntegration}
                onToggleResource={(resourceId, included) =>
                  setProposalExcludedResourceIds((current) =>
                    included ? current.filter((id) => id !== resourceId) : [...new Set([...current, resourceId])],
                  )
                }
                onToggleIndexing={(changeId, included) =>
                  setProposalExcludedIndexingChangeIds((current) =>
                    included ? current.filter((id) => id !== changeId) : [...new Set([...current, changeId])],
                  )
                }
              />
              {workingChanges.length === 0 ? (
                <p className="of-text-muted" style={{ marginTop: 10, fontSize: 12 }}>
                  No unsaved changes are currently loaded for this project.
                </p>
              ) : (
                <div style={{ overflowX: "auto", marginTop: 12 }}>
                  <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 12 }}>
                    <thead>
                      <tr style={{ textAlign: "left", borderBottom: "1px solid var(--border-default)" }}>
                        <th style={registryCellStyle()}>Change</th>
                        <th style={registryCellStyle()}>Resource</th>
                        <th style={registryCellStyle()}>Author</th>
                        <th style={registryCellStyle()}>Created</th>
                        <th style={registryCellStyle()}>Warnings</th>
                      </tr>
                    </thead>
                    <tbody>
                      {workingChanges.map((change) => (
                        <tr key={change.id} style={{ borderBottom: "1px solid var(--border-subtle)" }}>
                          <td style={registryCellStyle()}>
                            <strong>{change.label}</strong>
                            <div className="of-text-muted">{change.action} · {change.source}</div>
                          </td>
                          <td style={registryCellStyle()}>{change.kind} · {change.targetId || "—"}</td>
                          <td style={registryCellStyle()}>{change.author || change.createdBy || change.updatedBy || "unknown"}</td>
                          <td style={registryCellStyle()}>{formatDateTime(change.createdAt)}</td>
                          <td style={registryCellStyle()}>{change.warnings?.join("; ") || "—"}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
          )}

          {section === "history" && (
            <OntologyHistoryPanel
              registry={ontologyRegistry}
              entries={historyEntries}
              resourceEntries={selectedResourceHistory}
              selectedResource={selectedHistoryResource}
              onSelectedResourceChange={setSelectedHistoryResource}
              resourceKind={historyResourceKind}
              onResourceKindChange={setHistoryResourceKind}
              author={historyAuthor}
              onAuthorChange={setHistoryAuthor}
              from={historyFrom}
              onFromChange={setHistoryFrom}
              to={historyTo}
              onToChange={setHistoryTo}
              visibility={historyVisibility}
              onVisibilityChange={setHistoryVisibility}
              details={historyDetails}
              onDetailsChange={setHistoryDetails}
              hideRestricted={historyHideRestricted}
              onHideRestrictedChange={setHistoryHideRestricted}
              notice={historyNotice}
              busyKey={historyBusy}
              onRestore={(entry, resource) => void restoreHistoryResource(entry, resource)}
            />
          )}

          {section === "importExport" && (
            <OntologyBundlePanel
              resources={bundleResourceOptions}
              selected={bundleSelection}
              onSelectedChange={setBundleSelection}
              bundleText={bundleText}
              onBundleTextChange={(text) => {
                setBundleText(text);
                setBundleValidation(null);
              }}
              validation={bundleValidation}
              notice={bundleNotice}
              busy={bundleBusy}
              onExport={exportBundle}
              onValidate={validateBundleText}
              onImport={() => void importBundleAsWorkingState()}
              onFile={(file) => void loadBundleFile(file)}
            />
          )}

          {section === "cleanup" && (
            <CleanupAssistantPanel
              assistant={cleanupAssistant}
              candidatesByKind={cleanupCandidatesByKind}
              selectedIds={cleanupSelection}
              onToggle={(id) =>
                setCleanupSelection((current) =>
                  current.includes(id)
                    ? current.filter((entry) => entry !== id)
                    : [...current, id],
                )
              }
              onSelectAll={() =>
                setCleanupSelection(
                  cleanupAssistant.candidates
                    .filter((candidate) => candidate.delete_supported)
                    .map((candidate) => candidate.id),
                )
              }
              onClear={() => {
                setCleanupSelection([]);
                setCleanupConfirmed(false);
              }}
              confirmed={cleanupConfirmed}
              onConfirmChange={setCleanupConfirmed}
              busy={cleanupBusy}
              notice={cleanupNotice}
              proposalIntegration={branchProposalIntegration}
              onStage={() => void stageCleanupSelection()}
            />
          )}

          {section === "auditHealth" && (
            <AuditHealthPanel
              auditLog={auditEventLog}
              healthReport={healthReport}
              healthIssues={filteredHealthIssues}
              auditCategoryFilter={auditCategoryFilter}
              auditStatusFilter={auditStatusFilter}
              auditActorFilter={auditActorFilter}
              onAuditCategoryChange={setAuditCategoryFilter}
              onAuditStatusChange={setAuditStatusFilter}
              onAuditActorChange={setAuditActorFilter}
              healthCategoryFilter={healthCategoryFilter}
              healthSeverityFilter={healthSeverityFilter}
              onHealthCategoryChange={setHealthCategoryFilter}
              onHealthSeverityChange={setHealthSeverityFilter}
            />
          )}

          {section === "projects" && (
            <section className="of-panel" style={{ padding: 16 }}>
              <p className="of-eyebrow">Projects ({projects.length})</p>
              <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: "none" }}>
                {projects.map((p) => (
                  <li
                    key={p.id}
                    style={{
                      padding: 8,
                      borderBottom: "1px solid var(--border-subtle)",
                    }}
                  >
                    <Link to={projectStablePath(p)}>
                      <strong>{p.display_name || p.slug}</strong>
                    </Link>{" "}
                    · {p.id}
                  </li>
                ))}
              </ul>
            </section>
          )}
        </div>
      </section>

      <CreateObjectTypeWizard
        open={wizardOpen}
        onClose={() => setWizardOpen(false)}
        onCreated={(objectType) => {
          setWizardOpen(false);
          void refresh();
          navigate(`/ontology/${objectType.id}`);
        }}
      />

      {branchDialogOpen ? (
        <CreateBranchDialog
          onClose={() => setBranchDialogOpen(false)}
          onCreate={(name) => {
            setBranchName(name);
            setBranchDialogOpen(false);
          }}
        />
      ) : null}
    </section>
  );
}

function slugifyGroupName(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
}

interface ShellCountsInput {
  objectTypes: ObjectType[];
  actionTypes: ActionType[];
  interfaces: OntologyInterface[];
  shared: SharedPropertyType[];
  valueTypes: OntologyValueType[];
  linkTypes: LinkType[];
  objectTypeGroups: OntologyObjectTypeGroup[];
  objectViews: ObjectViewDefinition[];
  projects: OntologyProject[];
  projectResources: OntologyProjectResourceBinding[];
  ontologyRegistry?: OntologyResourceRegistryEntry[];
  workingChanges?: unknown[];
  savedChanges?: unknown[];
  usageReferences?: unknown[];
  permissionResources?: unknown[];
  cleanupCandidates?: number;
  auditEvents?: number;
  healthIssues?: number;
}

function shellNavCount(section: Section, input: ShellCountsInput) {
  switch (section) {
    case "registry":
      return input.ontologyRegistry?.length ?? 0;
    case "types":
      return input.objectTypes.length;
    case "links":
      return input.linkTypes.length;
    case "actions":
      return input.actionTypes.length;
    case "interfaces":
      return input.interfaces.length;
    case "shared":
      return input.shared.length;
    case "valueTypes":
      return input.valueTypes.length;
    case "groups":
      return input.objectTypeGroups.length;
    case "views":
      return input.objectViews.length;
    case "projects":
      return input.projects.length;
    case "changes":
      return input.workingChanges?.length ?? 0;
    case "history":
      return input.savedChanges?.length ?? 0;
    case "usage":
      return input.usageReferences?.length ?? 0;
    case "permissions":
      return input.permissionResources?.length ?? 0;
    case "cleanup":
      return input.cleanupCandidates ?? 0;
    case "auditHealth":
      return (input.auditEvents ?? 0) + (input.healthIssues ?? 0);
    case "overview":
      return input.projectResources.length;
    default:
      return undefined;
  }
}

function matchesResourceFilter(
  objectType: ObjectType,
  filter: ResourceFilter,
  bindings: OntologyProjectResourceBinding[],
) {
  if (filter === "all") return true;
  if (filter === "visible") return objectType.visibility !== "hidden";
  if (filter === "hidden") return objectType.visibility === "hidden";
  if (filter === "experimental") return objectType.status === "experimental";
  const isProjectBound = bindings.some(
    (binding) =>
      binding.resource_kind === "object_type" &&
      binding.resource_id === objectType.id,
  );
  return !isProjectBound || !objectType.backing_dataset_id;
}

function buildRecentResources(
  input: Omit<ShellCountsInput, "projects" | "projectResources">,
): ShellSearchResult[] {
  return buildResourceSearchResults(input)
    .sort((a, b) => b.detail.localeCompare(a.detail))
    .slice(0, 6);
}

function buildResourceSearchResults(
  input: Omit<ShellCountsInput, "projects" | "projectResources">,
): ShellSearchResult[] {
  return [
    ...input.objectTypes.map((entry) =>
      resourceSearchResult(
        entry.id,
        "Object type",
        entry.display_name,
        entry.name,
        "types",
        entry.updated_at,
      ),
    ),
    ...input.linkTypes.map((entry) =>
      resourceSearchResult(
        entry.id,
        "Link type",
        entry.display_name,
        entry.name,
        "links",
        entry.updated_at,
      ),
    ),
    ...input.actionTypes.map((entry) =>
      resourceSearchResult(
        entry.id,
        "Action type",
        entry.display_name,
        entry.name,
        "actions",
        entry.updated_at,
      ),
    ),
    ...input.interfaces.map((entry) =>
      resourceSearchResult(
        entry.id,
        "Interface",
        entry.display_name,
        entry.name,
        "interfaces",
        entry.updated_at,
      ),
    ),
    ...input.shared.map((entry) =>
      resourceSearchResult(
        entry.id,
        "Shared property",
        entry.display_name,
        entry.name,
        "shared",
        entry.updated_at,
      ),
    ),
    ...input.valueTypes.map((entry) =>
      resourceSearchResult(
        entry.id,
        "Value type",
        entry.display_name,
        `${entry.name} · ${entry.base_type}`,
        "valueTypes",
        entry.updated_at,
      ),
    ),
    ...input.objectTypeGroups.map((entry) =>
      resourceSearchResult(
        entry.id,
        "Object type group",
        entry.display_name,
        entry.name,
        "groups",
        entry.updated_at || "",
      ),
    ),
    ...input.objectViews.map((entry) =>
      resourceSearchResult(
        entry.id,
        "Object View",
        entry.display_name || entry.name,
        entry.name,
        "views",
        entry.updated_at || "",
      ),
    ),
  ];
}

function resourceSearchResult(
  id: string,
  kind: string,
  label: string,
  detail: string,
  section: Section,
  updatedAt: string,
): ShellSearchResult {
  return {
    id: `${kind}:${id}`,
    kind,
    label,
    detail: detail || updatedAt || "—",
    section,
  };
}

function searchDocumentToShellResult(document: OntologyResourceSearchResultItem): ShellSearchResult {
  return {
    id: document.id,
    kind: resourceKindLabel(document.resource_kind),
    label: document.display_name,
    detail: [
      document.api_name,
      document.project_display_name,
      document.permission.schema_only ? "schema only" : "",
      document.match_reason,
    ].filter(Boolean).join(" · "),
    section: sectionForSearchDocument(document),
  };
}

function sectionForSearchDocument(document: OntologyResourceSearchResultItem): Section {
  switch (document.resource_kind) {
    case "object_type":
    case "property":
    case "datasource_registration":
      return "types";
    case "link_type":
      return "links";
    case "action_type":
      return "actions";
    case "interface":
      return "interfaces";
    case "shared_property_type":
      return "shared";
    case "value_type":
      return "valueTypes";
    case "object_type_group":
      return "groups";
    case "core_object_view":
    case "custom_object_view":
      return "views";
    case "usage_edge":
    case "saved_exploration":
    case "saved_list":
      return "usage";
    default:
      return "registry";
  }
}

function buildShellWarnings({
  ontology,
  objectTypes,
  projects,
  projectResources,
}: {
  ontology: OntologyArtifact;
  objectTypes: ObjectType[];
  projects: OntologyProject[];
  projectResources: OntologyProjectResourceBinding[];
}) {
  const warnings: string[] = [];
  if (projects.length === 0)
    warnings.push(
      "No ontology project is available for placement; resources are shown in sandbox context.",
    );
  if (ontology.access_mode === "shared")
    warnings.push(
      "This ontology is shared across organizations; verify markings before publishing changes.",
    );
  const boundObjectTypes = new Set(
    projectResources
      .filter((entry) => entry.resource_kind === "object_type")
      .map((entry) => entry.resource_id),
  );
  const unplacedObjectTypes = objectTypes.filter(
    (entry) => !boundObjectTypes.has(entry.id),
  ).length;
  if (unplacedObjectTypes > 0)
    warnings.push(
      `${unplacedObjectTypes} object type${unplacedObjectTypes === 1 ? " is" : "s are"} not yet bound to a project resource.`,
    );
  return warnings;
}

async function loadOntologyUsageSources(): Promise<{
  sources: OntologyUsageExternalSource[];
  failures: string[];
}> {
  const sources: OntologyUsageExternalSource[] = [];
  const failures: string[] = [];

  try {
    const apps = await listApps({ per_page: 50 });
    const appResults = await Promise.allSettled(
      apps.data.slice(0, 50).map((app) => getApp(app.id)),
    );
    for (const result of appResults) {
      if (result.status === "fulfilled") sources.push(workshopUsageSource(result.value));
    }
  } catch {
    failures.push("Workshop");
  }

  try {
    const pipelines = await listPipelines({ per_page: 100 });
    sources.push(...pipelines.data.map(pipelineUsageSource));
  } catch {
    failures.push("Pipeline Builder");
  }

  try {
    const objectSets = await listObjectSets({ size: 200 });
    for (const objectSet of objectSets.data) {
      sources.push(objectExplorerUsageSource(objectSet));
      sources.push(savedExplorationUsageSource(objectSet));
    }
  } catch {
    failures.push("Object Explorer");
  }

  try {
    const branches = await listGlobalBranches();
    const branchResults = await Promise.allSettled(
      branches.slice(0, 50).map(async (branch) => ({
        branch,
        links: await listGlobalBranchResources(branch.id),
      })),
    );
    for (const result of branchResults) {
      if (result.status !== "fulfilled") continue;
      for (const link of result.value.links) {
        sources.push({
          product: "global_branching",
          consumer_id: `${result.value.branch.id}:${link.resource_rid}`,
          consumer_label: result.value.branch.name,
          consumer_kind: "Global branch resource link",
          surface: link.status,
          detail: `${link.resource_type} linked to ${link.branch_rid}.`,
          payload: { branch: result.value.branch, link },
          last_used_at: link.last_synced_at,
          actor: result.value.branch.created_by,
        });
      }
    }
  } catch {
    failures.push("Global Branching");
  }

  try {
    const listings = await listListings();
    const versionResults = await Promise.allSettled(
      listings.items.slice(0, 50).map(async (listing) => ({
        listing,
        versions: (await listVersions(listing.id)).items,
      })),
    );
    for (const result of versionResults) {
      if (result.status !== "fulfilled") continue;
      for (const version of result.value.versions.slice(0, 3)) {
        sources.push(marketplaceUsageSource(result.value.listing, version));
      }
    }
  } catch {
    failures.push("Marketplace");
  }

  return { sources, failures };
}

function workshopUsageSource(app: AppDefinition): OntologyUsageExternalSource {
  return {
    product: "workshop",
    consumer_id: app.id,
    consumer_label: app.name,
    consumer_kind: "Workshop app",
    surface: `${app.pages.length} pages / ${countAppWidgets(app.pages)} widgets`,
    detail: app.description || "Workshop app definition references ontology resources through variables, widgets, events, and actions.",
    payload: app,
    last_used_at: app.updated_at,
    actor: app.created_by,
  };
}

function pipelineUsageSource(pipeline: Pipeline): OntologyUsageExternalSource {
  return {
    product: "pipeline_builder",
    consumer_id: pipeline.id,
    consumer_label: pipeline.name,
    consumer_kind: "Pipeline",
    surface: pipeline.lifecycle || pipeline.status,
    detail: pipeline.description || "Pipeline DAG and IR reference ontology resources.",
    payload: pipeline,
    last_used_at: pipeline.updated_at,
    actor: pipeline.owner_id,
  };
}

function objectExplorerUsageSource(objectSet: ObjectSetDefinition): OntologyUsageExternalSource {
  return {
    product: "object_explorer",
    consumer_id: objectSet.id,
    consumer_label: objectSet.name,
    consumer_kind: "Object Explorer object set",
    surface: objectSet.materialized_at ? "materialized" : "live",
    detail: "Object Explorer can evaluate, filter, traverse, and materialize this object set.",
    payload: objectSet,
    last_used_at: objectSet.updated_at,
    actor: objectSet.owner_id,
  };
}

function savedExplorationUsageSource(objectSet: ObjectSetDefinition): OntologyUsageExternalSource {
  return {
    product: "saved_exploration",
    consumer_id: objectSet.id,
    consumer_label: objectSet.name,
    consumer_kind: "Saved object exploration",
    surface: objectSet.materialized_at ? "saved materialized set" : "saved live set",
    detail: objectSet.description || "Saved Object Explorer object set.",
    payload: objectSet,
    last_used_at: objectSet.updated_at,
    actor: objectSet.owner_id,
  };
}

function marketplaceUsageSource(
  listing: ListingDefinition,
  version: PackageVersion,
): OntologyUsageExternalSource {
  return {
    product: "marketplace",
    consumer_id: `${listing.id}:${version.id}`,
    consumer_label: `${listing.name} ${version.version}`,
    consumer_kind: "Marketplace package version",
    surface: version.release_channel,
    detail: `${version.packaged_resources.length} packaged resources in ${listing.package_kind}.`,
    payload: { listing, version },
    last_used_at: version.published_at || listing.updated_at,
    actor: listing.publisher,
  };
}

function countAppWidgets(pages: AppDefinition["pages"]) {
  return pages.reduce((count, page) => count + countWidgets(page.widgets) + countSections(page.sections || []), 0);
}

function countSections(sections: NonNullable<AppDefinition["pages"][number]["sections"]>): number {
  return sections.reduce(
    (count, section) =>
      count + countWidgets(section.widgets || []) + countSections(section.sections || []),
    0,
  );
}

function countWidgets(widgets: AppDefinition["pages"][number]["widgets"]): number {
  return widgets.reduce((count, widget) => count + 1 + countWidgets(widget.children || []), 0);
}

const USAGE_PRODUCT_ORDER: OntologyUsageProduct[] = [
  "workshop",
  "functions",
  "pipeline_builder",
  "object_explorer",
  "saved_exploration",
  "global_branching",
  "marketplace",
  "object_views",
];

function ontologyLogicMetricRun(
  id: string,
  status: "succeeded" | "failed",
  minutesAgo: number,
  durationMs: number,
  invocationSurface: string,
  errorMessage?: string,
): LogicRunHistoryRecord {
  const now = new Date();
  const started = new Date(now.getTime() - minutesAgo * 60 * 1000);
  return {
    id,
    actorId: "ontology-viewer",
    actorName: "Ontology viewer",
    executionMode: "project_scoped",
    status,
    invocationSurface,
    startedAtIso: started.toISOString(),
    retentionExpiresAtIso: new Date(now.getTime() + 30 * 24 * 60 * 60 * 1000).toISOString(),
    durationMs,
    errorMessage,
  };
}

const USAGE_RESOURCE_KIND_OPTIONS = [
  "all",
  "object_type",
  "property",
  "link_type",
  "interface",
  "action_type",
  "object_view",
];

function OntologyUsagePanel({
  analysis,
  notice,
}: {
  analysis: OntologyUsageImpactAnalysis;
  notice: string;
}) {
  const [productFilter, setProductFilter] = useState<OntologyUsageProduct | "all">("all");
  const [kindFilter, setKindFilter] = useState("all");
  const visibleSummaries = analysis.summaries.filter((summary) => {
    if (kindFilter !== "all" && summary.resource_kind !== kindFilter) return false;
    if (productFilter !== "all" && !summary.products.includes(productFilter)) return false;
    return true;
  });
  const logicMetrics = useMemo(
    () => calculateLogicMetrics([
      ontologyLogicMetricRun("logic-run-om-1", "succeeded", 18, 142, "workshop"),
      ontologyLogicMetricRun("logic-run-om-2", "succeeded", 87, 176, "action_workflow"),
      ontologyLogicMetricRun("logic-run-om-3", "failed", 136, 231, "automate", "Permission denied for Customer.creditHold"),
      ontologyLogicMetricRun("logic-run-om-4", "succeeded", 420, 128, "function_on_objects"),
    ], "30d"),
    [],
  );

  return (
    <section className="of-panel" style={{ padding: 16, display: "grid", gap: 16 }}>
      <div>
        <p className="of-eyebrow">Usage and impact</p>
        <p className="of-text-muted" style={{ marginTop: 6, fontSize: 12 }}>
          Downstream references across Workshop, Functions, Pipeline Builder, Object Explorer, saved explorations, Global Branching, Marketplace products, and Object Views.
        </p>
        {notice ? <p className="of-text-muted" style={{ marginTop: 6, fontSize: 12 }}>{notice}</p> : null}
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(150px, 1fr))", gap: 8 }}>
        <UsageMetric label="Resources used" value={analysis.totals.resources} />
        <UsageMetric label="References" value={analysis.totals.references} />
        <UsageMetric label="Reads" value={analysis.totals.reads} />
        <UsageMetric label="Writes" value={analysis.totals.writes} />
        <UsageMetric label="Active users" value={analysis.totals.active_users} />
        <UsageMetric label="Impact warnings" value={analysis.warnings.length} tone={analysis.totals.errors > 0 ? "#b91c1c" : "#b45309"} />
      </div>

      <section style={{ border: "1px solid var(--border-subtle)", borderRadius: 6, padding: 12, display: "grid", gap: 10 }}>
        <div style={{ display: "flex", justifyContent: "space-between", gap: 10, alignItems: "center", flexWrap: "wrap" }}>
          <div>
            <p className="of-eyebrow">Logic resource metrics</p>
            <strong>Customer triage logic</strong>
          </div>
          <span className="of-chip">viewer permission required</span>
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(130px, 1fr))", gap: 8 }}>
          <UsageMetric label="Succeeded" value={logicMetrics.successCount} tone="#047857" />
          <UsageMetric label="Failed" value={logicMetrics.failureCount} tone={logicMetrics.failureCount > 0 ? "#b91c1c" : "#047857"} />
          <UsageMetric label="P95 ms" value={logicMetrics.p95DurationMs ?? 0} />
          <UsageMetric label="Recent runs" value={logicMetrics.recentRuns.length} />
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "minmax(180px, 0.7fr) minmax(260px, 1fr)", gap: 10, fontSize: 12 }}>
          <div>
            <p className="of-eyebrow">Failure categories</p>
            {logicMetrics.failureCategories.length === 0 ? (
              <p className="of-text-muted" style={{ margin: 0 }}>No failures in the 30-day window.</p>
            ) : logicMetrics.failureCategories.map((category) => (
              <div key={category.category} style={{ display: "flex", justifyContent: "space-between", gap: 8 }}>
                <span>{category.category.replaceAll("_", " ")}</span>
                <strong>{category.count}</strong>
              </div>
            ))}
          </div>
          <div>
            <p className="of-eyebrow">Recent run history</p>
            <div style={{ display: "grid", gap: 4 }}>
              {logicMetrics.recentRuns.slice(0, 4).map((run) => (
                <div key={run.id} style={{ display: "grid", gridTemplateColumns: "1fr auto auto", gap: 8 }}>
                  <span>{run.invocationSurface}</span>
                  <span className={run.status === "failed" ? "of-status-warning" : "of-status-success"} style={{ padding: "1px 6px", borderRadius: 4 }}>{run.status}</span>
                  <span className="of-text-muted">{run.durationMs} ms</span>
                </div>
              ))}
            </div>
          </div>
        </div>
      </section>

      <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
        {USAGE_PRODUCT_ORDER.map((product) => (
          <button
            key={product}
            type="button"
            className="of-button"
            onClick={() => setProductFilter(productFilter === product ? "all" : product)}
            style={{
              fontSize: 12,
              borderColor: productFilter === product ? "var(--status-info)" : undefined,
              background: productFilter === product ? "rgba(45, 114, 210, 0.08)" : undefined,
            }}
          >
            {usageProductLabel(product)} · {analysis.product_counts[product] || 0}
          </button>
        ))}
      </div>

      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
        <select className="of-input" value={kindFilter} onChange={(event) => setKindFilter(event.target.value)} style={{ maxWidth: 240 }}>
          {USAGE_RESOURCE_KIND_OPTIONS.map((kind) => (
            <option key={kind} value={kind}>{kind === "all" ? "All resource kinds" : resourceKindLabel(kind)}</option>
          ))}
        </select>
        <button type="button" className="of-button" onClick={() => { setProductFilter("all"); setKindFilter("all"); }}>
          Clear filters
        </button>
      </div>

      {analysis.warnings.length > 0 ? (
        <section style={{ display: "grid", gap: 8 }}>
          <p className="of-eyebrow">Warnings before save ({analysis.warnings.length})</p>
          {analysis.warnings.map((warning) => (
            <div
              key={`${warning.change_id}:${warning.resource_kind}:${warning.resource_id}`}
              className={warning.severity === "error" ? "of-status-danger" : "of-status-warning"}
              style={{ padding: 10, borderRadius: 6, fontSize: 12 }}
            >
              <strong>{warning.resource_label}</strong> · {warning.code}
              <div>{warning.message}</div>
            </div>
          ))}
        </section>
      ) : null}

      <div style={{ overflowX: "auto" }}>
        <table style={{ width: "100%", minWidth: 980, borderCollapse: "collapse", fontSize: 12 }}>
          <thead>
            <tr style={{ textAlign: "left", borderBottom: "1px solid var(--border-default)" }}>
              <th style={registryCellStyle()}>Resource</th>
              <th style={registryCellStyle()}>Risk</th>
              <th style={registryCellStyle()}>30-day usage model</th>
              <th style={registryCellStyle()}>Products</th>
              <th style={registryCellStyle()}>Where used</th>
            </tr>
          </thead>
          <tbody>
            {visibleSummaries.map((summary) => (
              <tr key={summary.resource_key} style={{ borderBottom: "1px solid var(--border-subtle)", verticalAlign: "top" }}>
                <td style={registryCellStyle()}>
                  <strong>{summary.resource_label}</strong>
                  <div className="of-text-muted">{resourceKindLabel(summary.resource_kind)} · {summary.resource_id}</div>
                </td>
                <td style={registryCellStyle()}>
                  <span className={`of-chip ${summary.risk_level === "high" ? "of-status-danger" : summary.risk_level === "medium" ? "of-status-warning" : "of-status-success"}`}>
                    {summary.risk_level}
                  </span>
                </td>
                <td style={registryCellStyle()}>
                  {summary.interactions} interactions
                  <div className="of-text-muted">{summary.read_count} reads · {summary.write_count} writes · {summary.active_users} users</div>
                  <div className="of-text-muted">Last: {formatDateTime(summary.last_used_at)}</div>
                </td>
                <td style={registryCellStyle()}>
                  <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                    {summary.products.map((product) => (
                      <span key={product} className="of-chip">{usageProductLabel(product)}</span>
                    ))}
                  </div>
                </td>
                <td style={registryCellStyle()}>
                  <div style={{ display: "grid", gap: 6 }}>
                    {summary.references.slice(0, 5).map((reference) => (
                      <div key={reference.id}>
                        <strong>{reference.consumer_label}</strong>
                        <div className="of-text-muted">{reference.product_label} · {reference.consumer_kind} · {reference.surface}</div>
                        <div className="of-text-muted">{reference.detail}</div>
                      </div>
                    ))}
                    {summary.references.length > 5 ? (
                      <span className="of-text-muted">+{summary.references.length - 5} more references</span>
                    ) : null}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {visibleSummaries.length === 0 ? (
          <p className="of-text-muted" style={{ marginTop: 10, fontSize: 12 }}>
            No downstream references matched the current filters.
          </p>
        ) : null}
      </div>
    </section>
  );
}

function UsageMetric({ label, value, tone = "#1d4ed8" }: { label: string; value: number; tone?: string }) {
  return (
    <div style={{ border: "1px solid var(--border-subtle)", borderRadius: 6, padding: 10 }}>
      <p className="of-text-muted" style={{ margin: 0, fontSize: 11 }}>{label}</p>
      <strong style={{ color: tone, fontSize: 20 }}>{value.toLocaleString()}</strong>
    </div>
  );
}

function OntologyPermissionsPanel({
  analysis,
  userLabel,
}: {
  analysis: OntologyPermissionAnalysis;
  userLabel: string;
}) {
  const [levelFilter, setLevelFilter] = useState<OntologyPermissionLevel | "all">("all");
  const [showBlockedOnly, setShowBlockedOnly] = useState(false);
  const visibleResources = analysis.resources.filter((resource) => {
    if (levelFilter !== "all" && resource.effective_level !== levelFilter) return false;
    if (showBlockedOnly && resource.can_edit) return false;
    return true;
  });
  const blockedChecks = analysis.change_checks.filter((check) => !check.allowed);

  return (
    <section className="of-panel" style={{ padding: 16, display: "grid", gap: 16 }}>
      <div>
        <p className="of-eyebrow">Ontology resource permissions</p>
        <p className="of-text-muted" style={{ marginTop: 6, fontSize: 12 }}>
          Project/folder-managed resource access for {userLabel}. Object type definitions and object instance data are evaluated separately.
        </p>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(150px, 1fr))", gap: 8 }}>
        <UsageMetric label="Resources" value={analysis.totals.resources} />
        <UsageMetric label="Definitions viewable" value={analysis.totals.viewable_definitions} />
        <UsageMetric label="Objects viewable" value={analysis.totals.viewable_instances} />
        <UsageMetric label="Editable" value={analysis.totals.editable} />
        <UsageMetric label="Manageable" value={analysis.totals.manageable} />
        <UsageMetric label="Blocked changes" value={analysis.blocked_changes} tone={analysis.blocked_changes > 0 ? "#b91c1c" : "#15803d"} />
      </div>

      {analysis.change_checks.length > 0 ? (
        <section style={{ display: "grid", gap: 8 }}>
          <p className="of-eyebrow">Edit permission checks ({analysis.change_checks.length})</p>
          {analysis.change_checks.map((check) => (
            <div
              key={check.change_id}
              className={check.allowed ? "of-status-success" : "of-status-danger"}
              style={{ padding: 10, borderRadius: 6, fontSize: 12 }}
            >
              <strong>{check.change_label}</strong> · {check.allowed ? "Allowed" : "Blocked"}
              <div style={{ display: "grid", gap: 4, marginTop: 6 }}>
                {check.requirements.map((requirement) => (
                  <div key={`${check.change_id}:${requirement.resource_key}`}>
                    {requirement.resource_label}: needs {requirement.required_level}, has {requirement.effective_level}
                    <span className="of-text-muted"> · {requirement.reason}</span>
                  </div>
                ))}
              </div>
            </div>
          ))}
          {blockedChecks.length === 0 ? (
            <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>No staged changes are blocked by ontology resource permissions.</p>
          ) : null}
        </section>
      ) : null}

      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
        <select className="of-input" value={levelFilter} onChange={(event) => setLevelFilter(event.target.value as OntologyPermissionLevel | "all")} style={{ maxWidth: 220 }}>
          <option value="all">All levels</option>
          {(["none", "view", "edit", "manage", "owner"] as const).map((level) => (
            <option key={level} value={level}>{level}</option>
          ))}
        </select>
        <label style={{ display: "flex", gap: 6, alignItems: "center", fontSize: 12 }}>
          <input type="checkbox" checked={showBlockedOnly} onChange={(event) => setShowBlockedOnly(event.target.checked)} />
          Show resources without edit access
        </label>
      </div>

      <div style={{ overflowX: "auto" }}>
        <table style={{ width: "100%", minWidth: 1040, borderCollapse: "collapse", fontSize: 12 }}>
          <thead>
            <tr style={{ textAlign: "left", borderBottom: "1px solid var(--border-default)" }}>
              <th style={registryCellStyle()}>Resource</th>
              <th style={registryCellStyle()}>Project / folder</th>
              <th style={registryCellStyle()}>Owner</th>
              <th style={registryCellStyle()}>Definition</th>
              <th style={registryCellStyle()}>Objects</th>
              <th style={registryCellStyle()}>Edit / manage</th>
              <th style={registryCellStyle()}>Reason</th>
            </tr>
          </thead>
          <tbody>
            {visibleResources.map((resource) => (
              <tr key={resource.resource_key} style={{ borderBottom: "1px solid var(--border-subtle)", verticalAlign: "top" }}>
                <td style={registryCellStyle()}>
                  <strong>{resource.display_name}</strong>
                  <div className="of-text-muted">{resourceKindLabel(resource.resource_kind)} · {resource.resource_id}</div>
                </td>
                <td style={registryCellStyle()}>
                  {resource.project_display_name}
                  <div className="of-text-muted">{resource.folder_path}</div>
                </td>
                <td style={registryCellStyle()}>{resource.owner_id || "—"}</td>
                <td style={registryCellStyle()}>
                  <span className={`of-chip ${resource.can_view_definition ? "of-status-success" : "of-status-danger"}`}>
                    {resource.can_view_definition ? "View" : "No view"}
                  </span>
                </td>
                <td style={registryCellStyle()}>
                  {resource.object_instance_access === "not_applicable" ? (
                    <span className="of-text-muted">Not object data</span>
                  ) : (
                    <>
                      <span className={`of-chip ${resource.can_view_instances ? "of-status-success" : "of-status-warning"}`}>
                        {resource.can_view_instances ? "Object data view" : "Schema only"}
                      </span>
                      <div className="of-text-muted">{objectAccessLabel(resource.object_instance_access)}</div>
                    </>
                  )}
                </td>
                <td style={registryCellStyle()}>
                  <span style={permissionPillStyle(resource.effective_level)}>{resource.effective_level}</span>
                  <div className="of-text-muted">
                    {resource.can_edit ? "edit" : "no edit"} · {resource.can_manage ? "manage" : "no manage"}
                  </div>
                </td>
                <td style={registryCellStyle()}>{resource.reasons.join("; ") || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {visibleResources.length === 0 ? (
          <p className="of-text-muted" style={{ marginTop: 10, fontSize: 12 }}>No permission records matched the current filters.</p>
        ) : null}
      </div>
    </section>
  );
}

function OntologyBranchProposalPreviewPanel({
  integration,
  onToggleResource,
  onToggleIndexing,
}: {
  integration: OntologyGlobalBranchProposalIntegration;
  onToggleResource: (resourceId: string, included: boolean) => void;
  onToggleIndexing: (changeId: string, included: boolean) => void;
}) {
  const previewTone =
    integration.preview.status === "blocked"
      ? "of-status-danger"
      : integration.preview.status === "pending"
        ? "of-status-warning"
        : "of-status-success";
  const resources = integration.resources.slice(0, 8);
  const indexingChanges = integration.indexing_changes.slice(0, 6);
  return (
    <section style={{ marginTop: 12, border: "1px solid var(--border-subtle)", borderRadius: 6, padding: 12, display: "grid", gap: 10 }}>
      <div style={{ display: "flex", justifyContent: "space-between", gap: 10, alignItems: "flex-start", flexWrap: "wrap" }}>
        <div>
          <p className="of-eyebrow">Global Branching proposal preview</p>
          <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
            Branch {integration.branch_label} includes {integration.preview.resource_count} ontology/Object View resources, {integration.preview.indexing_change_count} indexing changes, and {integration.proposal_tasks.length} review tasks.
          </p>
        </div>
        <span className={`of-chip ${previewTone}`}>{integration.preview.status}</span>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(120px, 1fr))", gap: 8 }}>
        <UsageMetric label="Ready" value={integration.preview.ready_count} tone="#047857" />
        <UsageMetric label="Pending" value={integration.preview.pending_count} tone="#b45309" />
        <UsageMetric label="Blocked" value={integration.preview.blocked_count} tone={integration.preview.blocked_count > 0 ? "#b91c1c" : "#047857"} />
      </div>

      {integration.checks.length > 0 ? (
        <div style={{ display: "grid", gap: 6 }}>
          {integration.checks.map((check) => (
            <div
              key={check.id}
              className={check.status === "failed" ? "of-status-danger" : check.status === "warning" ? "of-status-warning" : "of-status-success"}
              style={{ padding: "8px 10px", borderRadius: 6, fontSize: 12 }}
            >
              <strong>{check.label}:</strong> {check.message}
            </div>
          ))}
        </div>
      ) : null}

      {resources.length > 0 ? (
        <div style={{ display: "grid", gap: 6 }}>
          <p className="of-eyebrow">Proposed resources</p>
          {resources.map((resource) => (
            <article key={resource.id} className="of-panel-muted" style={{ padding: 10, display: "flex", justifyContent: "space-between", gap: 10, alignItems: "flex-start", flexWrap: "wrap" }}>
              <div>
                <strong>{resource.label}</strong>
                <p className="of-text-muted" style={{ margin: "2px 0 0", fontSize: 11 }}>
                  {resourceKindLabel(resource.kind)} · {resource.action} · {resource.included ? "included" : "removed"}
                </p>
                {[...resource.errors, ...resource.warnings].slice(0, 1).map((message) => (
                  <p key={message} className="of-text-muted" style={{ margin: "4px 0 0", fontSize: 11 }}>{message}</p>
                ))}
              </div>
              <button
                type="button"
                className="of-button"
                onClick={() => onToggleResource(resource.id, !resource.included)}
                disabled={resource.included && !resource.removable}
                style={{ fontSize: 11 }}
              >
                {resource.included ? "Remove from proposal" : "Restore to proposal"}
              </button>
            </article>
          ))}
        </div>
      ) : null}

      {indexingChanges.length > 0 ? (
        <div style={{ display: "grid", gap: 6 }}>
          <p className="of-eyebrow">Indexing changes</p>
          {indexingChanges.map((change) => (
            <article key={change.id} className="of-panel-muted" style={{ padding: 10, display: "flex", justifyContent: "space-between", gap: 10, alignItems: "flex-start", flexWrap: "wrap" }}>
              <div>
                <strong>{change.label}</strong>
                <p className="of-text-muted" style={{ margin: "2px 0 0", fontSize: 11 }}>
                  {change.required ? "required" : "optional"} · {change.status} · {change.reason}
                </p>
              </div>
              <button
                type="button"
                className="of-button"
                onClick={() => onToggleIndexing(change.id, !change.included)}
                disabled={change.included && !change.removable}
                style={{ fontSize: 11 }}
              >
                {change.included ? "Remove indexing" : "Restore indexing"}
              </button>
            </article>
          ))}
        </div>
      ) : null}

      {integration.warnings.length > 0 ? (
        <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>{integration.warnings[0]}</p>
      ) : null}
    </section>
  );
}

function objectAccessLabel(mode: string) {
  switch (mode) {
    case "definition_not_viewable":
      return "Object type definition is not viewable.";
    case "object_policy":
      return "Object security policy grants data separately from schema.";
    case "object_policy_required":
      return "Object security policy visibility is still required.";
    case "datasource_required":
      return "Backing datasource or object-data permission is still required.";
    case "datasource_granted":
      return "Backing datasource or object-data permission is present.";
    default:
      return "Object instance access is not applicable.";
  }
}

function permissionPillStyle(level: OntologyPermissionLevel): React.CSSProperties {
  const tones: Record<OntologyPermissionLevel, string> = {
    none: "#991b1b",
    view: "#1d4ed8",
    edit: "#047857",
    manage: "#7c3aed",
    owner: "#a16207",
  };
  return pillStyle(tones[level]);
}

interface BundleResourceOption {
  key: string;
  kind: string;
  id: string;
  api_name: string;
  display_name: string;
  detail: string;
}

function buildBundleResourceOptions(
  registry: OntologyResourceRegistryEntry[],
  valueTypes: OntologyValueType[],
): BundleResourceOption[] {
  const registryOptions = registry.map((entry) => ({
    key: ontologyResourceKey(entry.resource_kind, entry.resource_id),
    kind: entry.resource_kind,
    id: entry.resource_id,
    api_name: entry.api_name,
    display_name: entry.display_name,
    detail: entry.project_display_name || entry.folder_path || "Ontology resource",
  }));
  const valueTypeOptions = valueTypes.map((valueType) => ({
    key: ontologyResourceKey("value_type", valueType.id),
    kind: "value_type",
    id: valueType.id,
    api_name: valueType.name,
    display_name: valueType.display_name,
    detail: `${valueType.base_type} · ${valueType.status}`,
  }));
  return [...registryOptions, ...valueTypeOptions].sort((left, right) =>
    `${left.kind}:${left.display_name}`.localeCompare(`${right.kind}:${right.display_name}`),
  );
}

function uniqueStrings(values: string[]) {
  return [...new Set(values)];
}

function downloadTextFile(filename: string, text: string) {
  if (typeof document === "undefined") return;
  const blob = new Blob([text], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.click();
  URL.revokeObjectURL(url);
}

function OntologyBundlePanel({
  resources,
  selected,
  onSelectedChange,
  bundleText,
  onBundleTextChange,
  validation,
  notice,
  busy,
  onExport,
  onValidate,
  onImport,
  onFile,
}: {
  resources: BundleResourceOption[];
  selected: string[];
  onSelectedChange: (next: string[]) => void;
  bundleText: string;
  onBundleTextChange: (text: string) => void;
  validation: OntologyBundleValidationResult | null;
  notice: string;
  busy: string;
  onExport: () => void;
  onValidate: () => void;
  onImport: () => void;
  onFile: (file: File | null) => void;
}) {
  const selectedSet = new Set(selected);
  const allSelected = resources.length > 0 && selected.length === resources.length;

  function toggle(key: string, checked: boolean) {
    onSelectedChange(checked ? uniqueStrings([...selected, key]) : selected.filter((item) => item !== key));
  }

  return (
    <section className="of-panel" style={{ padding: 16, display: "grid", gap: 16 }}>
      <div>
        <p className="of-eyebrow">Import / export</p>
        <p className="of-text-muted" style={{ marginTop: 6, fontSize: 12 }}>
          Bundle selected ontology resources as editable JSON, validate changed bundles, and import valid entries as unsaved changes.
        </p>
      </div>

      {notice ? (
        <div className={validation && !validation.valid ? "of-status-warning" : "of-status-success"} style={{ padding: "10px 12px", borderRadius: 6, fontSize: 12 }}>
          {notice}
        </div>
      ) : null}

      <section style={{ display: "grid", gap: 10 }}>
        <div style={{ display: "flex", justifyContent: "space-between", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
          <p className="of-eyebrow">Export resources ({selected.length})</p>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            <button type="button" className="of-button" onClick={() => onSelectedChange(allSelected ? [] : resources.map((resource) => resource.key))}>
              {allSelected ? "Clear" : "Select all"}
            </button>
            <button type="button" className="of-button of-button--primary" onClick={onExport} disabled={selected.length === 0}>
              Export JSON
            </button>
          </div>
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))", gap: 8, maxHeight: 280, overflow: "auto", paddingRight: 2 }}>
          {resources.map((resource) => (
            <label
              key={resource.key}
              style={{
                display: "grid",
                gridTemplateColumns: "auto 1fr",
                gap: 8,
                alignItems: "start",
                padding: 8,
                border: "1px solid var(--border-subtle)",
                borderRadius: 6,
                fontSize: 12,
              }}
            >
              <input
                type="checkbox"
                checked={selectedSet.has(resource.key)}
                onChange={(event) => toggle(resource.key, event.target.checked)}
                style={{ marginTop: 2 }}
              />
              <span>
                <strong>{resource.display_name}</strong>
                <span className="of-text-muted"> · {resourceKindLabel(resource.kind)}</span>
                <span className="of-text-muted" style={{ display: "block", marginTop: 2 }}>
                  {resource.api_name} · {resource.detail}
                </span>
              </span>
            </label>
          ))}
          {resources.length === 0 ? (
            <p className="of-text-muted" style={{ fontSize: 12 }}>No resources are available to export.</p>
          ) : null}
        </div>
      </section>

      <section style={{ display: "grid", gap: 10 }}>
        <div style={{ display: "flex", justifyContent: "space-between", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
          <p className="of-eyebrow">Edited bundle</p>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            <label className="of-button" style={{ cursor: "pointer" }}>
              {busy === "file" ? "Reading…" : "Open JSON"}
              <input
                type="file"
                accept="application/json,.json"
                onChange={(event) => onFile(event.target.files?.[0] || null)}
                style={{ display: "none" }}
              />
            </label>
            <button type="button" className="of-button" onClick={onValidate} disabled={!bundleText.trim()}>
              Validate
            </button>
            <button type="button" className="of-button of-button--primary" onClick={onImport} disabled={!bundleText.trim() || !validation?.valid || busy === "import"}>
              {busy === "import" ? "Importing…" : "Import as unsaved changes"}
            </button>
          </div>
        </div>
        <textarea
          className="of-input"
          value={bundleText}
          onChange={(event) => onBundleTextChange(event.target.value)}
          rows={16}
          spellCheck={false}
          style={{ fontFamily: "var(--font-mono)", fontSize: 11, lineHeight: 1.45 }}
        />
      </section>

      {validation ? (
        <section style={{ display: "grid", gap: 8 }}>
          <p className="of-eyebrow">
            Validation · {validation.errors} errors · {validation.warnings} warnings · {validation.staged_changes.length} staged changes
          </p>
          {validation.issues.length === 0 ? (
            <p className="of-text-muted" style={{ fontSize: 12 }}>No validation issues.</p>
          ) : (
            <ul style={{ listStyle: "none", paddingLeft: 0, margin: 0, display: "grid", gap: 6 }}>
              {validation.issues.map((issue, index) => (
                <li
                  key={`${issue.code}-${issue.resource_key || "bundle"}-${index}`}
                  className={issue.severity === "error" ? "of-status-danger" : "of-status-warning"}
                  style={{ padding: 8, borderRadius: 6, fontSize: 12 }}
                >
                  <strong>{issue.code}</strong>
                  {issue.resource_key ? <span> · {issue.resource_key}</span> : null}
                  <div>{issue.message}</div>
                </li>
              ))}
            </ul>
          )}
        </section>
      ) : null}
    </section>
  );
}

const HISTORY_RESOURCE_KIND_OPTIONS = [
  "object_type",
  "link_type",
  "action_type",
  "interface",
  "shared_property_type",
  "object_type_group",
  "core_object_view",
  "custom_object_view",
  "datasource_registration",
];

function OntologyHistoryPanel({
  registry,
  entries,
  resourceEntries,
  selectedResource,
  onSelectedResourceChange,
  resourceKind,
  onResourceKindChange,
  author,
  onAuthorChange,
  from,
  onFromChange,
  to,
  onToChange,
  visibility,
  onVisibilityChange,
  details,
  onDetailsChange,
  hideRestricted,
  onHideRestrictedChange,
  notice,
  busyKey,
  onRestore,
}: {
  registry: OntologyResourceRegistryEntry[];
  entries: OntologyHistoryEntry[];
  resourceEntries: OntologyHistoryEntry[];
  selectedResource: string;
  onSelectedResourceChange: (value: string) => void;
  resourceKind: string;
  onResourceKindChange: (value: string) => void;
  author: string;
  onAuthorChange: (value: string) => void;
  from: string;
  onFromChange: (value: string) => void;
  to: string;
  onToChange: (value: string) => void;
  visibility: OntologyHistoryVisibilityFilter;
  onVisibilityChange: (value: OntologyHistoryVisibilityFilter) => void;
  details: OntologyHistoryDetailsFilter;
  onDetailsChange: (value: OntologyHistoryDetailsFilter) => void;
  hideRestricted: boolean;
  onHideRestrictedChange: (value: boolean) => void;
  notice: string;
  busyKey: string;
  onRestore: (entry: OntologyHistoryEntry, resource: OntologyHistoryResourceSummary) => void;
}) {
  const resourceOptions = registry
    .filter((entry) => entry.resource_id)
    .sort((left, right) => left.display_name.localeCompare(right.display_name));

  return (
    <section className="of-panel" style={{ padding: 16, display: "grid", gap: 16 }}>
      <div>
        <p className="of-eyebrow">Ontology history ({entries.length})</p>
        <p className="of-text-muted" style={{ marginTop: 6, fontSize: 12 }}>
          Saved change records are shown globally and can be narrowed to one ontology resource. Restores are staged back into unsaved changes.
        </p>
      </div>

      {notice ? (
        <div className="of-status-success" style={{ padding: "10px 12px", borderRadius: 6, fontSize: 12 }}>
          {notice}
        </div>
      ) : null}

      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))", gap: 8 }}>
        <label style={historyFilterLabelStyle()}>
          Resource type
          <select className="of-input" value={resourceKind} onChange={(event) => onResourceKindChange(event.target.value)} style={{ marginTop: 4 }}>
            <option value="all">All resource types</option>
            {HISTORY_RESOURCE_KIND_OPTIONS.map((kind) => (
              <option key={kind} value={kind}>{resourceKindLabel(kind)}</option>
            ))}
          </select>
        </label>
        <label style={historyFilterLabelStyle()}>
          Author
          <input className="of-input" value={author} onChange={(event) => onAuthorChange(event.target.value)} placeholder="User ID or email" style={{ marginTop: 4 }} />
        </label>
        <label style={historyFilterLabelStyle()}>
          From
          <input className="of-input" type="datetime-local" value={from} onChange={(event) => onFromChange(event.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={historyFilterLabelStyle()}>
          To
          <input className="of-input" type="datetime-local" value={to} onChange={(event) => onToChange(event.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={historyFilterLabelStyle()}>
          Visibility
          <select className="of-input" value={visibility} onChange={(event) => onVisibilityChange(event.target.value as OntologyHistoryVisibilityFilter)} style={{ marginTop: 4 }}>
            <option value="all">All visibility</option>
            <option value="visible">Visible resources</option>
            <option value="hidden">Hidden resources</option>
          </select>
        </label>
        <label style={historyFilterLabelStyle()}>
          Details
          <select className="of-input" value={details} onChange={(event) => onDetailsChange(event.target.value as OntologyHistoryDetailsFilter)} style={{ marginTop: 4 }}>
            <option value="all">All detail access</option>
            <option value="viewable">Can view details</option>
            <option value="restricted">Restricted details</option>
          </select>
        </label>
      </div>

      <label style={{ display: "flex", gap: 8, alignItems: "center", fontSize: 12 }}>
        <input type="checkbox" checked={hideRestricted} onChange={(event) => onHideRestrictedChange(event.target.checked)} />
        Hide records where every resource has restricted details
      </label>

      <div style={{ display: "grid", gap: 8 }}>
        <label style={historyFilterLabelStyle()}>
          Resource history
          <select className="of-input" value={selectedResource} onChange={(event) => onSelectedResourceChange(event.target.value)} style={{ marginTop: 4 }}>
            <option value="">Select a resource</option>
            {resourceOptions.map((entry) => (
              <option key={historyRegistryKey(entry)} value={historyRegistryKey(entry)}>
                {entry.display_name} · {resourceKindLabel(entry.resource_kind)}
              </option>
            ))}
          </select>
        </label>
        {selectedResource ? (
          <HistoryRecordsTable
            entries={resourceEntries}
            busyKey={busyKey}
            onRestore={onRestore}
            emptyText="No saved changes matched this resource and filter set."
          />
        ) : null}
      </div>

      <div>
        <p className="of-eyebrow">Global saved changes</p>
        <HistoryRecordsTable
          entries={entries}
          busyKey={busyKey}
          onRestore={onRestore}
          emptyText="No saved change records matched the current filters."
        />
      </div>
    </section>
  );
}

function HistoryRecordsTable({
  entries,
  busyKey,
  onRestore,
  emptyText,
}: {
  entries: OntologyHistoryEntry[];
  busyKey: string;
  onRestore: (entry: OntologyHistoryEntry, resource: OntologyHistoryResourceSummary) => void;
  emptyText: string;
}) {
  if (entries.length === 0) {
    return <p className="of-text-muted" style={{ marginTop: 8, fontSize: 12 }}>{emptyText}</p>;
  }
  return (
    <div style={{ overflowX: "auto", marginTop: 8 }}>
      <table style={{ width: "100%", minWidth: 980, borderCollapse: "collapse", fontSize: 12 }}>
        <thead>
          <tr style={{ textAlign: "left", borderBottom: "1px solid var(--border-default)" }}>
            <th style={registryCellStyle()}>Saved at</th>
            <th style={registryCellStyle()}>Author</th>
            <th style={registryCellStyle()}>Resources</th>
            <th style={registryCellStyle()}>Details</th>
            <th style={registryCellStyle()}>Branch / proposal</th>
            <th style={registryCellStyle()}>Status</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((entry) => (
            <tr key={entry.id} style={{ borderBottom: "1px solid var(--border-subtle)" }}>
              <td style={registryCellStyle()}>
                {formatDateTime(entry.saved_at)}
                {entry.note ? <div className="of-text-muted">{entry.note}</div> : null}
              </td>
              <td style={registryCellStyle()}>{entry.author}</td>
              <td style={registryCellStyle()}>
                <div style={{ display: "grid", gap: 6 }}>
                  {entry.resources.map((resource) => {
                    const key = historyBusyKey(entry, resource);
                    return (
                      <div key={`${entry.id}:${resource.kind}:${resource.id || resource.label}`} style={{ display: "grid", gap: 3 }}>
                        <strong>{resource.label}</strong>
                        <span className="of-text-muted">{resource.kind} · {resource.id || "unidentified"} · {resource.visibility}</span>
                        <button
                          type="button"
                          className="of-button"
                          disabled={!resource.can_view_details || !resource.change || busyKey === key}
                          onClick={() => onRestore(entry, resource)}
                          style={{ width: "fit-content", fontSize: 11 }}
                        >
                          {busyKey === key ? "Staging…" : "Restore"}
                        </button>
                      </div>
                    );
                  })}
                </div>
              </td>
              <td style={registryCellStyle()}>
                {entry.restricted_details_count > 0 ? (
                  <span className="of-chip of-status-warning">{entry.restricted_details_count} restricted</span>
                ) : (
                  <span className="of-chip of-status-success">Viewable</span>
                )}
                <ul style={{ margin: "6px 0 0", paddingLeft: 16 }}>
                  {entry.resources.slice(0, 3).map((resource) => (
                    <li key={`${entry.id}:detail:${resource.kind}:${resource.id || resource.label}`}>
                      {resource.can_view_details && resource.change
                        ? `${resource.change.action} · ${historyPayloadSummary(resource.change.payload)}`
                        : `${resource.label}: details restricted`}
                    </li>
                  ))}
                </ul>
              </td>
              <td style={registryCellStyle()}>{entry.record.branch_id || "main"} / {entry.record.proposal_id || "—"}</td>
              <td style={registryCellStyle()}>
                <span className={`of-chip ${entry.status === "failed" ? "of-status-danger" : "of-status-success"}`}>{entry.status}</span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function historyFilterLabelStyle(): React.CSSProperties {
  return { display: "grid", gap: 2, fontSize: 12, color: "var(--text-muted)" };
}

function historyRegistryKey(entry: OntologyResourceRegistryEntry) {
  return `${entry.resource_kind}:${entry.resource_id}`;
}

function historyBusyKey(entry: OntologyHistoryEntry, resource: OntologyHistoryResourceSummary) {
  return `${entry.id}:${resource.kind}:${resource.id || ""}`;
}

function parseHistoryResourceKey(value: string) {
  if (!value) return null;
  const [kind, ...idParts] = value.split(":");
  const id = idParts.join(":");
  if (!kind || !id) return null;
  return { kind, id };
}

function historyPayloadSummary(payload: Record<string, unknown>) {
  const keys = Object.keys(payload || {}).filter((key) => !key.startsWith("restored_from_"));
  if (keys.length === 0) return "metadata";
  return keys.slice(0, 4).join(", ") + (keys.length > 4 ? ` +${keys.length - 4}` : "");
}

function formatDateTime(value: string | null | undefined) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

function OntologyRegistryPanel({
  registry,
  searchIndex,
  initialQuery,
}: {
  registry: OntologyResourceRegistryEntry[];
  searchIndex: OntologyResourceSearchIndex;
  initialQuery: string;
}) {
  const [query, setQuery] = useState(initialQuery);
  const [kind, setKind] = useState<OntologyResourceSearchIndexKind | "all">("all");
  const [project, setProject] = useState("all");
  const [group, setGroup] = useState("all");
  const [apiOnly, setApiOnly] = useState(false);
  const [permissionFilter, setPermissionFilter] = useState<OntologyResourceSearchPermissionFilter>("viewable");
  const [page, setPage] = useState(1);

  useEffect(() => {
    setQuery(initialQuery);
    setPage(1);
  }, [initialQuery]);

  const result = useMemo(
    () =>
      searchOntologyResourceIndex(searchIndex, {
        query,
        api_name_only: apiOnly,
        resource_kinds: kind === "all" ? undefined : [kind],
        project_ids: project === "all" ? undefined : [project],
        group_ids: group === "all" ? undefined : [group],
        permission_filter: permissionFilter,
        page,
        per_page: 25,
      }),
    [apiOnly, group, kind, page, permissionFilter, project, query, searchIndex],
  );

  return (
    <section className="of-panel" style={{ padding: 16, overflowX: "auto", display: "grid", gap: 12 }}>
      <div style={{ display: "flex", justifyContent: "space-between", gap: 12, alignItems: "flex-start" }}>
        <div>
          <p className="of-eyebrow">
            Ontology resource search index ({result.total} / {searchIndex.documents.length})
          </p>
          <p className="of-text-muted" style={{ marginTop: 6, fontSize: 12 }}>
            The index expands {registry.length} registry entries into searchable properties, usage edges, Object Views, groups, and saved explorations, then hides rows the current user cannot view.
          </p>
        </div>
        <div style={{ display: "flex", flexWrap: "wrap", justifyContent: "flex-end", gap: 6 }}>
          <span className="of-chip">{searchIndex.incremental.reused_documents} reused</span>
          <span className="of-chip">{searchIndex.incremental.upserted_documents} upserted</span>
          <span className="of-chip">{searchIndex.incremental.removed_documents} removed</span>
        </div>
      </div>
      <p className="of-text-muted" style={{ marginTop: 6, fontSize: 12 }}>
        First-class registry entries normalize type metadata, project/folder placement, visibility, branch state, usage count, backing datasource, and last edit metadata.
      </p>
      <div style={{ display: "grid", gridTemplateColumns: "minmax(220px, 1.6fr) repeat(4, minmax(150px, 1fr))", gap: 8 }}>
        <input
          className="of-input"
          value={query}
          onChange={(event) => {
            setQuery(event.target.value);
            setPage(1);
          }}
          placeholder="Search display name, API name, properties, usage, saved explorations..."
        />
        <select
          className="of-input"
          value={kind}
          onChange={(event) => {
            setKind(event.target.value as OntologyResourceSearchIndexKind | "all");
            setPage(1);
          }}
        >
          <option value="all">All resource types</option>
          {searchIndex.facets.resource_kinds.map((facet) => (
            <option key={facet.id} value={facet.id}>
              {facet.label} ({facet.count})
            </option>
          ))}
        </select>
        <select
          className="of-input"
          value={project}
          onChange={(event) => {
            setProject(event.target.value);
            setPage(1);
          }}
        >
          <option value="all">All projects</option>
          {searchIndex.facets.projects.map((facet) => (
            <option key={facet.id} value={facet.id}>
              {facet.label} ({facet.count})
            </option>
          ))}
        </select>
        <select
          className="of-input"
          value={group}
          onChange={(event) => {
            setGroup(event.target.value);
            setPage(1);
          }}
        >
          <option value="all">All groups</option>
          {searchIndex.facets.groups.map((facet) => (
            <option key={facet.id} value={facet.id}>
              {facet.label} ({facet.count})
            </option>
          ))}
        </select>
        <select
          className="of-input"
          value={permissionFilter}
          onChange={(event) => {
            setPermissionFilter(event.target.value as OntologyResourceSearchPermissionFilter);
            setPage(1);
          }}
        >
          <option value="viewable">Viewable only</option>
          <option value="hidden">Permission-hidden</option>
          <option value="all">All indexed rows</option>
        </select>
      </div>
      <label style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12, color: "var(--text-muted)" }}>
        <input
          type="checkbox"
          checked={apiOnly}
          onChange={(event) => {
            setApiOnly(event.target.checked);
            setPage(1);
          }}
        />
        API-name / resource-id search only
      </label>
      {result.hidden_results > 0 && permissionFilter === "viewable" ? (
        <div className="of-status-warning" style={{ padding: 8, borderRadius: 6, fontSize: 12 }}>
          {result.hidden_results} matching indexed row{result.hidden_results === 1 ? "" : "s"} hidden by permissions or resource visibility.
        </div>
      ) : null}
      <table
        style={{
          width: "100%",
          borderCollapse: "collapse",
          fontSize: 12,
        }}
      >
        <thead>
          <tr
            style={{
              textAlign: "left",
              borderBottom: "1px solid var(--border-default)",
            }}
          >
            <th style={registryCellStyle()}>Resource</th>
            <th style={registryCellStyle()}>Kind</th>
            <th style={registryCellStyle()}>API name</th>
            <th style={registryCellStyle()}>Project / group</th>
            <th style={registryCellStyle()}>Visibility</th>
            <th style={registryCellStyle()}>Status</th>
            <th style={registryCellStyle()}>Usage / links</th>
            <th style={registryCellStyle()}>Indexed</th>
          </tr>
        </thead>
        <tbody>
          {result.data.map((entry) => (
            <tr
              key={entry.id}
              style={{ borderBottom: "1px solid var(--border-subtle)" }}
            >
              <td style={registryCellStyle()}>
                <strong>{entry.display_name}</strong>
                {entry.parent_resource_id ? (
                  <div className="of-text-muted">
                    Parent: {entry.parent_resource_kind} / {entry.parent_resource_id}
                  </div>
                ) : null}
                {entry.permission.schema_only ? (
                  <div className="of-text-muted">
                    Schema-only result
                  </div>
                ) : null}
              </td>
              <td style={registryCellStyle()}>
                {resourceKindLabel(entry.resource_kind)}
              </td>
              <td style={registryCellStyle()}>{entry.api_name}</td>
              <td style={registryCellStyle()}>
                {entry.project_display_name}
                <div className="of-text-muted">{entry.group_names.join(", ") || entry.folder_path}</div>
              </td>
              <td style={registryCellStyle()}>{entry.visibility}</td>
              <td style={registryCellStyle()}>
                {entry.status} · {entry.branch_state}
              </td>
              <td style={registryCellStyle()}>{entry.usage_count} / {entry.linked_resource_count}</td>
              <td style={registryCellStyle()}>
                {formatDateTime(entry.indexed_at)}
                <div className="of-text-muted">score {entry.score}</div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {result.total === 0 ? (
        <p className="of-text-muted" style={{ fontSize: 12 }}>
          No indexed ontology resources match the current filters.
        </p>
      ) : null}
      <div style={{ display: "flex", justifyContent: "space-between", gap: 8, alignItems: "center" }}>
        <span className="of-text-muted" style={{ fontSize: 12 }}>
          Page {result.page} of {result.total_pages} · {result.total} matching rows
        </span>
        <div style={{ display: "flex", gap: 6 }}>
          <button type="button" className="of-button" disabled={result.page <= 1} onClick={() => setPage((value) => Math.max(1, value - 1))}>
            Previous
          </button>
          <button type="button" className="of-button" disabled={result.page >= result.total_pages} onClick={() => setPage((value) => value + 1)}>
            Next
          </button>
        </div>
      </div>
    </section>
  );
}

function registryCellStyle(): React.CSSProperties {
  return { padding: "8px 10px", verticalAlign: "top" };
}

const ONTOLOGY_CLEANUP_KIND_LABELS: Record<OntologyCleanupCandidateKind, string> = {
  object_type: "Object types",
  property: "Properties",
  link_type: "Link types",
  interface: "Interfaces",
  shared_property_type: "Shared property types",
  value_type: "Value types",
  object_type_group: "Object type groups",
  object_view: "Object Views",
  legacy_object_view_fragment: "Legacy Object View fragments",
  workshop_module: "Workshop modules",
};

function CleanupAssistantPanel({
  assistant,
  candidatesByKind,
  selectedIds,
  onToggle,
  onSelectAll,
  onClear,
  confirmed,
  onConfirmChange,
  busy,
  notice,
  proposalIntegration,
  onStage,
}: {
  assistant: OntologyCleanupAssistant;
  candidatesByKind: Map<OntologyCleanupCandidateKind, OntologyCleanupCandidate[]>;
  selectedIds: string[];
  onToggle: (id: string) => void;
  onSelectAll: () => void;
  onClear: () => void;
  confirmed: boolean;
  onConfirmChange: (value: boolean) => void;
  busy: boolean;
  notice: string;
  proposalIntegration: OntologyGlobalBranchProposalIntegration;
  onStage: () => void;
}) {
  const selectedCandidates = selectedIds
    .map((id) => assistant.candidates.find((candidate) => candidate.id === id))
    .filter((candidate): candidate is OntologyCleanupCandidate => Boolean(candidate));
  const selectedSupported = selectedCandidates.filter((candidate) => candidate.delete_supported);
  const manualSelectedCount = selectedCandidates.length - selectedSupported.length;
  const orderedKinds = Array.from(candidatesByKind.keys()).sort(
    (a, b) =>
      ONTOLOGY_CLEANUP_KIND_LABELS[a].localeCompare(ONTOLOGY_CLEANUP_KIND_LABELS[b]),
  );
  return (
    <section className="of-panel" style={{ display: "grid", gap: 14, padding: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 12, flexWrap: "wrap" }}>
        <div>
          <p className="of-eyebrow">Ontology cleanup assistant</p>
          <h2 className="of-heading-md" style={{ marginTop: 4 }}>
            {assistant.totals.candidates === 0
              ? "No unused ontology resources detected."
              : `${assistant.totals.candidates} cleanup candidate${assistant.totals.candidates === 1 ? "" : "s"}`}
          </h2>
          <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
            Generated {new Date(assistant.generated_at).toLocaleString()}. Cleanup converts confirmed actions into unsaved
            ontology changes; review them in Unsaved changes or fold them into a branch proposal before saving.
          </p>
        </div>
        <div style={{ display: "flex", flexWrap: "wrap", gap: 6, justifyContent: "flex-end" }}>
          <span className="of-chip">High {assistant.totals.high}</span>
          <span className="of-chip">Warnings {assistant.totals.warning}</span>
          <span className="of-chip">Info {assistant.totals.info}</span>
          <span className="of-chip">Auto-delete {assistant.totals.delete_supported}</span>
        </div>
      </div>

      {notice ? (
        <div className="of-status-success" style={{ padding: "9px 10px", borderRadius: "var(--radius-sm)", fontSize: 12 }}>
          {notice}
        </div>
      ) : null}

      {assistant.totals.candidates === 0 ? (
        <p className="of-text-muted" style={{ fontSize: 13 }}>
          All ontology resources have recorded usage or are protected (core Object Views, published views, primary keys,
          title properties). Re-run cleanup after editing the ontology.
        </p>
      ) : (
        <>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 8, alignItems: "center" }}>
            <button type="button" className="of-button" onClick={onSelectAll} disabled={busy}>
              Select all auto-deletable ({assistant.totals.delete_supported})
            </button>
            <button type="button" className="of-button" onClick={onClear} disabled={busy || selectedIds.length === 0}>
              Clear selection
            </button>
            <span className="of-chip" style={{ marginLeft: "auto" }}>
              {selectedSupported.length} selected · {manualSelectedCount} manual
            </span>
          </div>

          <div style={{ display: "grid", gap: 12 }}>
            {orderedKinds.map((kind) => {
              const candidates = candidatesByKind.get(kind) ?? [];
              return (
                <div key={kind} className="of-panel-muted" style={{ display: "grid", gap: 8, padding: 12 }}>
                  <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
                    <strong style={{ fontSize: 13 }}>{ONTOLOGY_CLEANUP_KIND_LABELS[kind]}</strong>
                    <span className="of-chip">{candidates.length}</span>
                  </div>
                  <ul style={{ margin: 0, padding: 0, listStyle: "none", display: "grid", gap: 6 }}>
                    {candidates.map((candidate) => {
                      const selected = selectedIds.includes(candidate.id);
                      return (
                        <li
                          key={candidate.id}
                          className="of-panel"
                          style={{ padding: 10, display: "grid", gap: 6, opacity: busy ? 0.7 : 1 }}
                        >
                          <label style={{ display: "flex", alignItems: "flex-start", gap: 8, fontSize: 13 }}>
                            <input
                              type="checkbox"
                              checked={selected}
                              disabled={busy || !candidate.delete_supported}
                              onChange={() => onToggle(candidate.id)}
                              style={{ marginTop: 3 }}
                            />
                            <span style={{ display: "grid", gap: 2 }}>
                              <span style={{ fontWeight: 600 }}>{candidate.label}</span>
                              <span className="of-text-muted" style={{ fontSize: 12 }}>
                                {candidate.reason}
                              </span>
                            </span>
                          </label>
                          <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                            <span
                              className={`of-chip ${
                                candidate.severity === "high"
                                  ? "of-status-danger"
                                  : candidate.severity === "warning"
                                  ? "of-status-warning"
                                  : ""
                              }`}
                            >
                              Severity: {candidate.severity}
                            </span>
                            <span className="of-chip">Usage refs: {candidate.usage_count}</span>
                            {candidate.delete_supported ? (
                              <span className="of-chip of-status-success">Auto-delete supported</span>
                            ) : (
                              <span className="of-chip of-status-warning">Manual resolution required</span>
                            )}
                          </div>
                          {candidate.reference_summary.length > 0 ? (
                            <ul style={{ margin: 0, paddingLeft: 16, fontSize: 12 }}>
                              {candidate.reference_summary.map((entry, index) => (
                                <li key={`${candidate.id}-ref-${index}`}>{entry}</li>
                              ))}
                            </ul>
                          ) : null}
                          {candidate.warnings.length > 0 ? (
                            <ul style={{ margin: 0, paddingLeft: 16, fontSize: 12, color: "var(--text-muted)" }}>
                              {candidate.warnings.map((warning, index) => (
                                <li key={`${candidate.id}-warn-${index}`}>{warning}</li>
                              ))}
                            </ul>
                          ) : null}
                        </li>
                      );
                    })}
                  </ul>
                </div>
              );
            })}
          </div>

          <div className="of-panel-muted" style={{ display: "grid", gap: 8, padding: 12 }}>
            <strong style={{ fontSize: 13 }}>Confirm impact review</strong>
            <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
              Cleanup actions are staged as deletion changes. They affect downstream Object Views, actions, and external
              products listed above. Review usage references and warnings before staging.
            </p>
            <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12, fontWeight: 600 }}>
              <input
                type="checkbox"
                checked={confirmed}
                onChange={(event) => onConfirmChange(event.target.checked)}
                disabled={busy}
              />
              I have reviewed the downstream usage impact for each selected candidate.
            </label>
            <div style={{ display: "flex", flexWrap: "wrap", gap: 8, alignItems: "center" }}>
              <button
                type="button"
                className="of-button of-button--primary"
                onClick={onStage}
                disabled={
                  busy || !confirmed || selectedIds.length === 0 || selectedSupported.length === 0
                }
              >
                {busy ? "Staging..." : "Stage as unsaved changes"}
              </button>
              <span className="of-text-muted" style={{ fontSize: 12 }}>
                Staged changes flow into the active branch ({proposalIntegration.branch_label || "main"}) proposal —{" "}
                {proposalIntegration.resources.length} pending resource
                {proposalIntegration.resources.length === 1 ? "" : "s"}.
              </span>
            </div>
          </div>
        </>
      )}
    </section>
  );
}

const ONTOLOGY_AUDIT_CATEGORY_OPTIONS: Array<{ id: OntologyAuditEventCategory; label: string }> = [
  { id: "resource_crud", label: "Resource CRUD" },
  { id: "datasource_mapping", label: "Datasource mapping" },
  { id: "object_view_edit", label: "Object View edit" },
  { id: "object_view_publish", label: "Object View publish" },
  { id: "import", label: "Bundle import" },
  { id: "export", label: "Bundle export" },
  { id: "restore", label: "History restore" },
  { id: "branch_rebase", label: "Branch rebase" },
  { id: "marketplace_packaging", label: "Marketplace packaging" },
  { id: "permission_change", label: "Permission change" },
];

const ONTOLOGY_AUDIT_STATUS_OPTIONS: Array<{ id: OntologyAuditEventStatus; label: string }> = [
  { id: "saved", label: "Saved" },
  { id: "pending", label: "Pending" },
  { id: "failed", label: "Failed" },
  { id: "info", label: "Info" },
];

const ONTOLOGY_HEALTH_CATEGORY_OPTIONS: Array<{ id: OntologyHealthCategory; label: string }> = [
  { id: "stale_datasource", label: "Stale datasources" },
  { id: "broken_link", label: "Broken links" },
  { id: "widget_load_failure", label: "Widget load failures" },
  { id: "inaccessible_backing_data", label: "Inaccessible backing data" },
  { id: "indexing_lag", label: "Indexing lag" },
  { id: "missing_value_type", label: "Missing value type validation" },
  { id: "permission_mismatch", label: "Permission mismatches" },
];

function AuditHealthPanel({
  auditLog,
  healthReport,
  healthIssues,
  auditCategoryFilter,
  auditStatusFilter,
  auditActorFilter,
  onAuditCategoryChange,
  onAuditStatusChange,
  onAuditActorChange,
  healthCategoryFilter,
  healthSeverityFilter,
  onHealthCategoryChange,
  onHealthSeverityChange,
}: {
  auditLog: OntologyAuditEventLog;
  healthReport: OntologyHealthReport;
  healthIssues: OntologyHealthIssue[];
  auditCategoryFilter: OntologyAuditEventCategory | "all";
  auditStatusFilter: OntologyAuditEventStatus | "all";
  auditActorFilter: string;
  onAuditCategoryChange: (value: OntologyAuditEventCategory | "all") => void;
  onAuditStatusChange: (value: OntologyAuditEventStatus | "all") => void;
  onAuditActorChange: (value: string) => void;
  healthCategoryFilter: OntologyHealthCategory | "all";
  healthSeverityFilter: OntologyHealthSeverity | "all";
  onHealthCategoryChange: (value: OntologyHealthCategory | "all") => void;
  onHealthSeverityChange: (value: OntologyHealthSeverity | "all") => void;
}) {
  return (
    <div style={{ display: "grid", gap: 16 }}>
      <section className="of-panel" style={{ display: "grid", gap: 14, padding: 16 }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 12, flexWrap: "wrap" }}>
          <div>
            <p className="of-eyebrow">Audit timeline</p>
            <h2 className="of-heading-md" style={{ marginTop: 4 }}>
              {auditLog.totals.events === 0
                ? "No audit events match the current filters."
                : `${auditLog.totals.events} audit event${auditLog.totals.events === 1 ? "" : "s"}`}
            </h2>
            <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
              Synthesized from saved ontology changes, pending unsaved changes, Object View publish history, branch
              rebases, marketplace packaging outputs, and permission-bearing change payloads.
            </p>
          </div>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 6, justifyContent: "flex-end" }}>
            <span className="of-chip">Saved {auditLog.totals.by_status.saved}</span>
            <span className="of-chip">Pending {auditLog.totals.by_status.pending}</span>
            <span className="of-chip">Failed {auditLog.totals.by_status.failed}</span>
            <span className="of-chip">Info {auditLog.totals.by_status.info}</span>
            <span className="of-chip">Actors {auditLog.totals.unique_actors}</span>
          </div>
        </div>

        <div style={{ display: "flex", flexWrap: "wrap", gap: 10, alignItems: "flex-end" }}>
          <label style={{ display: "grid", gap: 4, fontSize: 12, fontWeight: 600 }}>
            Category
            <select
              className="of-input"
              value={auditCategoryFilter}
              onChange={(event) => onAuditCategoryChange(event.target.value as OntologyAuditEventCategory | "all")}
              style={{ minWidth: 180 }}
            >
              <option value="all">All categories</option>
              {ONTOLOGY_AUDIT_CATEGORY_OPTIONS.map((option) => (
                <option key={option.id} value={option.id}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
          <label style={{ display: "grid", gap: 4, fontSize: 12, fontWeight: 600 }}>
            Status
            <select
              className="of-input"
              value={auditStatusFilter}
              onChange={(event) => onAuditStatusChange(event.target.value as OntologyAuditEventStatus | "all")}
              style={{ minWidth: 120 }}
            >
              <option value="all">All statuses</option>
              {ONTOLOGY_AUDIT_STATUS_OPTIONS.map((option) => (
                <option key={option.id} value={option.id}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
          <label style={{ display: "grid", gap: 4, fontSize: 12, fontWeight: 600, flex: "1 1 200px" }}>
            Actor contains
            <input
              className="of-input"
              value={auditActorFilter}
              onChange={(event) => onAuditActorChange(event.target.value)}
              placeholder="alice@example.com"
            />
          </label>
        </div>

        {auditLog.events.length === 0 ? (
          <p className="of-text-muted" style={{ fontSize: 13 }}>
            No audit events captured yet. Save changes, publish Object Views, or import bundles to populate the timeline.
          </p>
        ) : (
          <ul style={{ margin: 0, padding: 0, listStyle: "none", display: "grid", gap: 8 }}>
            {auditLog.events.slice(0, 60).map((event) => (
              <li
                key={event.id}
                className="of-panel-muted"
                style={{ display: "grid", gap: 4, padding: 10, fontSize: 13 }}
              >
                <div style={{ display: "flex", flexWrap: "wrap", gap: 6, alignItems: "center" }}>
                  <span
                    className={`of-chip ${
                      event.status === "failed"
                        ? "of-status-danger"
                        : event.status === "pending"
                        ? "of-status-warning"
                        : event.status === "saved"
                        ? "of-status-success"
                        : ""
                    }`}
                  >
                    {event.status}
                  </span>
                  <span className="of-chip">{event.category_label}</span>
                  <span className="of-chip">{event.action}</span>
                  <span style={{ fontWeight: 600 }}>{event.resource_label}</span>
                  <span className="of-text-muted" style={{ fontSize: 12, marginLeft: "auto" }}>
                    {event.actor} · {new Date(event.timestamp).toLocaleString()}
                  </span>
                </div>
                <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
                  {event.summary}
                </p>
                <p className="of-text-muted" style={{ margin: 0, fontSize: 11 }}>
                  source: {event.source} · resource: {event.resource_kind}/{event.resource_id || "—"}
                </p>
              </li>
            ))}
            {auditLog.events.length > 60 ? (
              <li className="of-text-muted" style={{ fontSize: 12 }}>
                +{auditLog.events.length - 60} more events. Refine filters to see them.
              </li>
            ) : null}
          </ul>
        )}
      </section>

      <section className="of-panel" style={{ display: "grid", gap: 14, padding: 16 }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 12, flexWrap: "wrap" }}>
          <div>
            <p className="of-eyebrow">Operational health</p>
            <h2 className="of-heading-md" style={{ marginTop: 4 }}>
              {healthReport.totals.issues === 0
                ? "No operational health issues detected."
                : `${healthReport.totals.issues} health issue${healthReport.totals.issues === 1 ? "" : "s"}`}
            </h2>
            <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
              Generated {new Date(healthReport.generated_at).toLocaleString()}. Includes stale datasources, broken links,
              widget load failures, inaccessible backing data, indexing lag, missing value type validation, and permission
              mismatches.
            </p>
          </div>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 6, justifyContent: "flex-end" }}>
            <span className={`of-chip ${healthReport.totals.critical > 0 ? "of-status-danger" : ""}`}>
              Critical {healthReport.totals.critical}
            </span>
            <span className={`of-chip ${healthReport.totals.warning > 0 ? "of-status-warning" : ""}`}>
              Warning {healthReport.totals.warning}
            </span>
            <span className="of-chip">Info {healthReport.totals.info}</span>
          </div>
        </div>

        <div style={{ display: "grid", gap: 8, gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))" }}>
          {healthReport.by_category.map((summary) => (
            <button
              key={summary.category}
              type="button"
              className="of-panel-muted"
              style={{
                display: "grid",
                gap: 4,
                padding: 12,
                textAlign: "left",
                cursor: "pointer",
                border:
                  healthCategoryFilter === summary.category
                    ? "1px solid #1d4ed8"
                    : "1px solid var(--border-subtle)",
              }}
              onClick={() =>
                onHealthCategoryChange(healthCategoryFilter === summary.category ? "all" : summary.category)
              }
            >
              <strong style={{ fontSize: 12 }}>{summary.label}</strong>
              <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
                <span className="of-chip">Total {summary.total}</span>
                {summary.critical > 0 ? <span className="of-chip of-status-danger">{summary.critical} crit</span> : null}
                {summary.warning > 0 ? <span className="of-chip of-status-warning">{summary.warning} warn</span> : null}
                {summary.info > 0 ? <span className="of-chip">{summary.info} info</span> : null}
              </div>
            </button>
          ))}
        </div>

        <div style={{ display: "flex", flexWrap: "wrap", gap: 10, alignItems: "flex-end" }}>
          <label style={{ display: "grid", gap: 4, fontSize: 12, fontWeight: 600 }}>
            Category
            <select
              className="of-input"
              value={healthCategoryFilter}
              onChange={(event) => onHealthCategoryChange(event.target.value as OntologyHealthCategory | "all")}
              style={{ minWidth: 180 }}
            >
              <option value="all">All categories</option>
              {ONTOLOGY_HEALTH_CATEGORY_OPTIONS.map((option) => (
                <option key={option.id} value={option.id}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
          <label style={{ display: "grid", gap: 4, fontSize: 12, fontWeight: 600 }}>
            Severity
            <select
              className="of-input"
              value={healthSeverityFilter}
              onChange={(event) => onHealthSeverityChange(event.target.value as OntologyHealthSeverity | "all")}
              style={{ minWidth: 120 }}
            >
              <option value="all">All severities</option>
              <option value="critical">Critical</option>
              <option value="warning">Warning</option>
              <option value="info">Info</option>
            </select>
          </label>
        </div>

        {healthIssues.length === 0 ? (
          <p className="of-text-muted" style={{ fontSize: 13 }}>
            No issues match the current filters.
          </p>
        ) : (
          <ul style={{ margin: 0, padding: 0, listStyle: "none", display: "grid", gap: 8 }}>
            {healthIssues.slice(0, 80).map((issue) => (
              <li
                key={issue.id}
                className="of-panel-muted"
                style={{ display: "grid", gap: 4, padding: 10, fontSize: 13 }}
              >
                <div style={{ display: "flex", flexWrap: "wrap", gap: 6, alignItems: "center" }}>
                  <span
                    className={`of-chip ${
                      issue.severity === "critical"
                        ? "of-status-danger"
                        : issue.severity === "warning"
                        ? "of-status-warning"
                        : ""
                    }`}
                  >
                    {issue.severity}
                  </span>
                  <span className="of-chip">{issue.category_label}</span>
                  <span style={{ fontWeight: 600 }}>{issue.resource_label}</span>
                  <span className="of-text-muted" style={{ fontSize: 12, marginLeft: "auto" }}>
                    {issue.resource_kind}/{issue.resource_id}
                  </span>
                </div>
                <p style={{ margin: 0, fontSize: 13 }}>{issue.message}</p>
                <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
                  Remediation: {issue.remediation}
                </p>
              </li>
            ))}
            {healthIssues.length > 80 ? (
              <li className="of-text-muted" style={{ fontSize: 12 }}>
                +{healthIssues.length - 80} more issues. Refine filters to see them.
              </li>
            ) : null}
          </ul>
        )}
      </section>
    </div>
  );
}

function filterButtonStyle(active: boolean): React.CSSProperties {
  return {
    border: `1px solid ${active ? "#1d4ed8" : "var(--border-default)"}`,
    borderRadius: 999,
    background: active ? "rgba(29, 78, 216, 0.08)" : "transparent",
    color: active ? "#1d4ed8" : "var(--text-muted)",
    padding: "5px 10px",
    fontSize: 12,
    cursor: "pointer",
  };
}

function searchResultStyle(): React.CSSProperties {
  return {
    display: "grid",
    gap: 2,
    textAlign: "left",
    padding: "8px 10px",
    border: "1px solid var(--border-subtle)",
    borderRadius: 6,
    background: "#fff",
    cursor: "pointer",
    fontSize: 12,
  };
}

function shellNavButtonStyle(active: boolean): React.CSSProperties {
  return {
    display: "grid",
    gap: 3,
    textAlign: "left",
    border: active ? "1px solid #1d4ed8" : "1px solid transparent",
    borderRadius: 6,
    background: active ? "rgba(29, 78, 216, 0.08)" : "transparent",
    color: active ? "#1d4ed8" : "var(--text-default)",
    padding: "8px 10px",
    cursor: "pointer",
    fontSize: 12,
  };
}

function resourceRowStyle(): React.CSSProperties {
  return {
    padding: 8,
    borderBottom: "1px solid var(--border-subtle)",
  };
}

function OntologyMetadataTerm({
  label,
  value,
}: {
  label: string;
  value: string;
}) {
  return (
    <div>
      <dt className="of-eyebrow" style={{ marginBottom: 4 }}>
        {label}
      </dt>
      <dd
        style={{
          margin: 0,
          fontSize: 13,
          color: "var(--text-default)",
          overflowWrap: "anywhere",
        }}
      >
        {value}
      </dd>
    </div>
  );
}

function resourceKindLabel(kind: string) {
  return kind
    .split("_")
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function pillStyle(tone: string): React.CSSProperties {
  return {
    display: "inline-flex",
    alignItems: "center",
    borderRadius: 999,
    border: `1px solid ${tone}33`,
    background: `${tone}12`,
    color: tone,
    padding: "4px 8px",
    fontSize: 12,
    fontWeight: 600,
  };
}

function NewMenuItem({
  glyph,
  label,
  description,
  enabled,
  highlighted,
  onClick,
}: {
  glyph: React.ReactNode;
  label: string;
  description: string;
  enabled?: boolean;
  highlighted?: boolean;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      onClick={enabled ? onClick : undefined}
      disabled={!enabled}
      style={{
        display: "flex",
        alignItems: "flex-start",
        gap: 10,
        width: "100%",
        padding: "10px 12px",
        border: 0,
        background: highlighted ? "rgba(45, 114, 210, 0.06)" : "transparent",
        cursor: enabled ? "pointer" : "not-allowed",
        opacity: enabled ? 1 : 0.6,
        textAlign: "left",
        borderRadius: 4,
      }}
      onMouseEnter={(event) => {
        if (enabled)
          event.currentTarget.style.background = "rgba(45, 114, 210, 0.08)";
      }}
      onMouseLeave={(event) =>
        (event.currentTarget.style.background = highlighted
          ? "rgba(45, 114, 210, 0.06)"
          : "transparent")
      }
    >
      <span style={{ display: "inline-flex", marginTop: 2 }}>{glyph}</span>
      <span style={{ display: "grid", gap: 2 }}>
        <strong
          style={{
            fontSize: 13,
            color: highlighted ? "var(--status-info)" : "var(--text-strong)",
          }}
        >
          {label}
        </strong>
        <span style={{ fontSize: 12, color: "var(--text-muted)" }}>
          {description}
        </span>
      </span>
    </button>
  );
}

function popoverStyle(extra: React.CSSProperties = {}): React.CSSProperties {
  return {
    position: "absolute",
    top: "calc(100% + 4px)",
    right: 0,
    background: "#fff",
    border: "1px solid var(--border-default)",
    borderRadius: 6,
    boxShadow: "0 8px 24px rgba(15, 23, 42, 0.16)",
    padding: 6,
    minWidth: 240,
    zIndex: 30,
    ...extra,
  };
}

function menuItemStyle(): React.CSSProperties {
  return {
    display: "flex",
    alignItems: "center",
    gap: 8,
    width: "100%",
    padding: "6px 10px",
    border: 0,
    background: "transparent",
    cursor: "pointer",
    fontSize: 13,
    textAlign: "left",
    borderRadius: 4,
  };
}

function CreateBranchDialog({
  onClose,
  onCreate,
}: {
  onClose: () => void;
  onCreate: (name: string) => void;
}) {
  const [name, setName] = useState("username/e2espeedrun");
  const [indexing, setIndexing] = useState(true);
  const [datasetBranch, setDatasetBranch] = useState("");

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="create-branch-title"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
      style={{
        position: "fixed",
        inset: 0,
        zIndex: 90,
        background: "rgba(17, 24, 39, 0.42)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: 24,
      }}
    >
      <section
        style={{
          width: "100%",
          maxWidth: 720,
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
            justifyContent: "space-between",
            alignItems: "center",
            padding: "12px 18px",
            borderBottom: "1px solid var(--border-subtle)",
          }}
        >
          <h2
            id="create-branch-title"
            style={{ margin: 0, fontSize: 15, fontWeight: 600 }}
          >
            Create branch
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
        <div style={{ padding: 18, display: "grid", gap: 14 }}>
          <p style={{ margin: 0, fontSize: 13, color: "var(--text-muted)" }}>
            This will create a Branch with your Ontology changes and a draft
            Proposal to review those changes. Open the Branch to view and add
            edits. Once all edits are approved, release the Proposal.
          </p>
          <label style={{ display: "grid", gap: 4 }}>
            <span style={{ fontSize: 13, fontWeight: 600 }}>Branch name</span>
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
              autoFocus
              style={{
                padding: "8px 10px",
                border: "1px solid var(--border-default)",
                borderRadius: 4,
                fontSize: 13,
              }}
            />
          </label>
          <div
            style={{
              background: "#f4f6f9",
              border: "1px solid var(--border-subtle)",
              borderRadius: 6,
              padding: 12,
              display: "grid",
              gap: 10,
            }}
          >
            <label
              style={{
                display: "flex",
                alignItems: "flex-start",
                gap: 12,
                cursor: "pointer",
              }}
            >
              <input
                type="checkbox"
                checked={indexing}
                onChange={(event) => setIndexing(event.target.checked)}
                style={{ accentColor: "var(--status-info)", marginTop: 2 }}
              />
              <span style={{ display: "grid", gap: 2 }}>
                <strong style={{ fontSize: 13 }}>
                  <Glyph name="eye" size={12} tone="var(--status-info)" />{" "}
                  Enable indexing on this branch
                </strong>
                <span style={{ fontSize: 12, color: "var(--text-muted)" }}>
                  Enable indexing if you need to preview schema changes for
                  modified entities. This will use additional storage and
                  compute. Only supported by Object Storage V2.
                </span>
              </span>
            </label>
            <label style={{ display: "grid", gap: 4 }}>
              <span style={{ fontSize: 13, fontWeight: 600 }}>
                Dataset branch name (Optional)
              </span>
              <span style={{ fontSize: 12, color: "var(--text-muted)" }}>
                Set this if you want to preview data or schema changes for
                modified entities from a branch other than master.
              </span>
              <input
                value={datasetBranch}
                onChange={(event) => setDatasetBranch(event.target.value)}
                style={{
                  padding: "8px 10px",
                  border: "1px solid var(--border-default)",
                  borderRadius: 4,
                  fontSize: 13,
                }}
              />
            </label>
          </div>
        </div>
        <footer
          style={{
            display: "flex",
            justifyContent: "flex-end",
            gap: 8,
            padding: 12,
            borderTop: "1px solid var(--border-subtle)",
          }}
        >
          <button type="button" onClick={onClose} className="of-button">
            Cancel
          </button>
          <button
            type="button"
            onClick={() => onCreate(name.trim() || "new-branch")}
            disabled={!name.trim()}
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: 6,
              padding: "8px 14px",
              border: 0,
              borderRadius: 4,
              background: "#15803d",
              color: "#fff",
              fontSize: 13,
              fontWeight: 600,
              cursor: name.trim() ? "pointer" : "not-allowed",
              opacity: name.trim() ? 1 : 0.6,
            }}
          >
            <Glyph name="plus" size={12} /> Create branch
          </button>
        </footer>
      </section>
    </div>
  );
}
