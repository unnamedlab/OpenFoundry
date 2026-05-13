import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";

import {
  buildOntologyResourceRegistry,
  createObjectTypeGroup,
  deleteObjectTypeGroup,
  deriveOntologyArtifact,
  linkTypeCardinalityLabel,
  linkTypeEndpointLabels,
  linkTypeHasDatasourceMapping,
  listActionTypes,
  listInterfaces,
  listLinkTypes,
  listObjectTypes,
  listObjectTypeGroups,
  listObjectViews,
  listProjectResources,
  listProjects,
  listSharedPropertyTypes,
  updateObjectTypeGroup,
  type ActionType,
  type LinkType,
  type OntologyArtifact,
  type OntologyInterface,
  type OntologyObjectTypeGroup,
  type ObjectType,
  type ObjectViewDefinition,
  type OntologyProject,
  type OntologyProjectResourceBinding,
  type OntologyResourceRegistryEntry,
  type SharedPropertyType,
} from "@/lib/api/ontology";
import { Glyph } from "@/lib/components/ui/Glyph";
import { CreateObjectTypeWizard } from "@/lib/components/ontology/CreateObjectTypeWizard";
import { LinkEditor } from "@/lib/components/ontology/LinkEditor";
import { useAuth } from "@/lib/stores/auth";

type Section =
  | "overview"
  | "registry"
  | "types"
  | "links"
  | "actions"
  | "interfaces"
  | "shared"
  | "groups"
  | "views"
  | "usage"
  | "changes"
  | "history"
  | "importExport"
  | "cleanup"
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

export function OntologyManagerPage() {
  const navigate = useNavigate();
  const { user } = useAuth();
  const [section, setSection] = useState<Section>("overview");
  const [objectTypes, setObjectTypes] = useState<ObjectType[]>([]);
  const [actionTypes, setActionTypes] = useState<ActionType[]>([]);
  const [interfaces, setInterfaces] = useState<OntologyInterface[]>([]);
  const [shared, setShared] = useState<SharedPropertyType[]>([]);
  const [linkTypes, setLinkTypes] = useState<LinkType[]>([]);
  const [objectViews, setObjectViews] = useState<ObjectViewDefinition[]>([]);
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
  const [ontology, setOntology] = useState<OntologyArtifact>(() =>
    deriveOntologyArtifact({ projects: [] }),
  );
  const [search, setSearch] = useState("");
  const [resourceFilter, setResourceFilter] = useState<ResourceFilter>("all");
  const [error, setError] = useState("");
  const [newMenuOpen, setNewMenuOpen] = useState(false);
  const [branchMenuOpen, setBranchMenuOpen] = useState(false);
  const [branchName, setBranchName] = useState("Main");
  const [branchDialogOpen, setBranchDialogOpen] = useState(false);
  const [wizardOpen, setWizardOpen] = useState(false);
  const newMenuRef = useRef<HTMLDivElement>(null);
  const branchMenuRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);

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
      const [types, actions, ifs, sh, links, groups, views, prs] = await Promise.all([
        listObjectTypes({ per_page: 200, search: search || undefined }),
        listActionTypes({ per_page: 200, search: search || undefined }),
        listInterfaces({ per_page: 200, search: search || undefined }),
        listSharedPropertyTypes({ per_page: 200, search: search || undefined }),
        listLinkTypes({ per_page: 200 }),
        listObjectTypeGroups({ per_page: 200, search: groupSearch || undefined }),
        listObjectViews({ per_page: 200 }),
        listProjects({ per_page: 200 }),
      ]);
      setObjectTypes(types.data);
      setActionTypes(actions.data);
      setInterfaces(ifs.data);
      setShared(sh.data);
      setLinkTypes(links.data);
      setObjectTypeGroups(groups.data);
      setObjectViews(views.data);
      setProjects(prs.data);
      const primaryProject = prs.data[0];
      const resources = primaryProject
        ? await listProjectResources(primaryProject.id)
        : [];
      setProjectResources(resources);
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

  const shellNavItems = useMemo(
    () =>
      SHELL_NAV_BASE.map((item) => ({
        ...item,
        count: shellNavCount(item.id, {
          objectTypes,
          actionTypes,
          interfaces,
          shared,
          linkTypes,
          objectTypeGroups,
          objectViews,
          projects,
          projectResources,
          ontologyRegistry,
        }),
      })),
    [
      objectTypes,
      actionTypes,
      interfaces,
      shared,
      linkTypes,
      objectTypeGroups,
      objectViews,
      projects,
      projectResources,
      ontologyRegistry,
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
        linkTypes,
        objectTypeGroups,
        objectViews,
      }),
    [objectTypes, actionTypes, interfaces, shared, linkTypes, objectTypeGroups, objectViews],
  );

  const searchResults = useMemo(
    () =>
      buildSearchResults(search, {
        objectTypes,
        actionTypes,
        interfaces,
        shared,
        linkTypes,
        objectTypeGroups,
        objectViews,
        projects,
      }),
    [
      search,
      objectTypes,
      actionTypes,
      interfaces,
      shared,
      linkTypes,
      objectTypeGroups,
      objectViews,
      projects,
    ],
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
              Global search results ({searchResults.length})
            </p>
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
            <OntologyRegistryPanel registry={ontologyRegistry} />
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
            <section className="of-panel" style={{ padding: 16 }}>
              <p className="of-eyebrow">
                Shared property types ({shared.length})
              </p>
              <ul style={{ marginTop: 8, paddingLeft: 0, listStyle: "none" }}>
                {shared.map((s) => (
                  <li
                    key={s.id}
                    style={{
                      padding: 8,
                      borderBottom: "1px solid var(--border-subtle)",
                    }}
                  >
                    <strong>{s.display_name}</strong> · {s.name} ·{" "}
                    {s.property_type}
                  </li>
                ))}
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
            <ShellPlaceholder
              title="Usage"
              description="Usage aggregates dependents, object storage health, query usage, action usage, and application references from the Ontology Manager shell."
              bullets={[
                `${ontologyRegistry.length} first-class ontology registry entries available for usage impact review.`,
                `${objectTypes.length} object types and ${linkTypes.length} link types can display 30-day read/write usage once metrics are connected.`,
                `${actionTypes.length} action types available for observability handoff.`,
              ]}
            />
          )}

          {section === "changes" && (
            <ShellPlaceholder
              title="Unsaved changes"
              description="Branch-local ontology changes will be summarized here before save, proposal, or discard."
              bullets={[
                "No unsaved changes are currently loaded for this branch.",
                "The shell tracks branch context and project placement for future change records.",
                "Save/review workflows are reserved for OMOV.22–OMOV.24.",
              ]}
            />
          )}

          {section === "history" && (
            <ShellPlaceholder
              title="History"
              description="Saved ontology changes, restore points, and per-resource history will appear here."
              bullets={[
                "History navigation is present in the shell.",
                "Resource-specific restore actions will be wired after change records are first-class.",
              ]}
            />
          )}

          {section === "importExport" && (
            <ShellPlaceholder
              title="Import / export"
              description="Ontology bundle export, validation, review, and import workflows will use this shell surface."
              bullets={[
                "Export selected object types, links, actions, interfaces, shared properties, groups, and Object Views.",
                "Validate imported bundles before applying them as unsaved changes.",
                "Project/folder placement and organization visibility are retained in bundle metadata.",
              ]}
            />
          )}

          {section === "cleanup" && (
            <ShellPlaceholder
              title="Cleanup"
              description="Cleanup will identify stale, unused, deprecated, or broken ontology resources and propose safe remediation."
              bullets={
                shellWarnings.length
                  ? shellWarnings
                  : ["No cleanup warnings are currently detected by the shell."]
              }
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
                    <Link to={`/projects/${p.id}`}>
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
  linkTypes: LinkType[];
  objectTypeGroups: OntologyObjectTypeGroup[];
  objectViews: ObjectViewDefinition[];
  projects: OntologyProject[];
  projectResources: OntologyProjectResourceBinding[];
  ontologyRegistry?: OntologyResourceRegistryEntry[];
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
    case "groups":
      return input.objectTypeGroups.length;
    case "views":
      return input.objectViews.length;
    case "projects":
      return input.projects.length;
    case "changes":
      return 0;
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

function buildSearchResults(
  search: string,
  input: Omit<ShellCountsInput, "projectResources">,
): ShellSearchResult[] {
  const needle = search.trim().toLowerCase();
  if (!needle) return [];
  return [
    ...buildResourceSearchResults({
      objectTypes: input.objectTypes,
      actionTypes: input.actionTypes,
      interfaces: input.interfaces,
      shared: input.shared,
      linkTypes: input.linkTypes,
      objectTypeGroups: input.objectTypeGroups,
      objectViews: input.objectViews,
    }),
    ...input.projects.map((entry) =>
      resourceSearchResult(
        entry.id,
        "Project",
        entry.display_name || entry.slug,
        entry.slug,
        "projects",
        entry.updated_at,
      ),
    ),
  ].filter((entry) =>
    `${entry.kind} ${entry.label} ${entry.detail}`
      .toLowerCase()
      .includes(needle),
  );
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

function OntologyRegistryPanel({
  registry,
}: {
  registry: OntologyResourceRegistryEntry[];
}) {
  return (
    <section className="of-panel" style={{ padding: 16, overflowX: "auto" }}>
      <p className="of-eyebrow">
        Ontology resource registry ({registry.length})
      </p>
      <p className="of-text-muted" style={{ marginTop: 6, fontSize: 12 }}>
        First-class registry entries normalize type metadata, project/folder
        placement, visibility, branch state, usage count, backing datasource,
        and last edit metadata.
      </p>
      <table
        style={{
          width: "100%",
          borderCollapse: "collapse",
          marginTop: 12,
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
            <th style={registryCellStyle()}>Project / folder</th>
            <th style={registryCellStyle()}>Visibility</th>
            <th style={registryCellStyle()}>Status</th>
            <th style={registryCellStyle()}>Usage</th>
            <th style={registryCellStyle()}>Last edited</th>
          </tr>
        </thead>
        <tbody>
          {registry.map((entry) => (
            <tr
              key={entry.id}
              style={{ borderBottom: "1px solid var(--border-subtle)" }}
            >
              <td style={registryCellStyle()}>
                <strong>{entry.display_name}</strong>
                {entry.plural_display_name ? (
                  <div className="of-text-muted">
                    Plural: {entry.plural_display_name}
                  </div>
                ) : null}
                {entry.backing_datasource_id ? (
                  <div className="of-text-muted">
                    Datasource: {entry.backing_datasource_id}
                  </div>
                ) : null}
              </td>
              <td style={registryCellStyle()}>
                {resourceKindLabel(entry.resource_kind)}
              </td>
              <td style={registryCellStyle()}>{entry.api_name}</td>
              <td style={registryCellStyle()}>
                {entry.project_display_name}
                <div className="of-text-muted">{entry.folder_path}</div>
              </td>
              <td style={registryCellStyle()}>{entry.visibility}</td>
              <td style={registryCellStyle()}>
                {entry.status} · {entry.branch_state}
              </td>
              <td style={registryCellStyle()}>{entry.usage_count}</td>
              <td style={registryCellStyle()}>{entry.last_edited_at ?? "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {registry.length === 0 ? (
        <p className="of-text-muted" style={{ fontSize: 12 }}>
          No registry entries have been loaded.
        </p>
      ) : null}
    </section>
  );
}

function registryCellStyle(): React.CSSProperties {
  return { padding: "8px 10px", verticalAlign: "top" };
}

function ShellPlaceholder({
  title,
  description,
  bullets,
}: {
  title: string;
  description: string;
  bullets: string[];
}) {
  return (
    <section className="of-panel" style={{ padding: 16 }}>
      <p className="of-eyebrow">{title}</p>
      <p className="of-text-muted" style={{ marginTop: 8, fontSize: 13 }}>
        {description}
      </p>
      <ul style={{ marginTop: 10, paddingLeft: 18, fontSize: 12 }}>
        {bullets.map((bullet) => (
          <li key={bullet}>{bullet}</li>
        ))}
      </ul>
    </section>
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
