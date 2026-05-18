import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';

import { ApiError } from '@/lib/api/client';
import { searchOntology, type SearchResult } from '@/lib/api/ontology';
import {
  createFavorite,
  listFavorites,
  listRecents,
  recordAccess,
  resolveResourceLabels,
  searchCompass,
  type CompassSearchResult,
  type RecentEntry,
  type ResourceKind,
  type UserFavorite,
} from '@/lib/api/workspace';
import {
  getResourceTypeDefinition,
  openURLForCompassResource,
} from '@/lib/compass/resourceTypeRegistry';
import { workspaceResourceStablePath } from '@/lib/compass/stableResourceUrls';
import { Glyph, type GlyphName } from '@/lib/components/ui/Glyph';
import { OpenWithMenu } from '@/lib/components/workspace/OpenWithMenu';

// ──────────────────────────────────────────────────────────────────
// SEARCH-001 — Quicksearch full-results page (jump-to + tabs).
// Replicates the Compass Quicksearch UX shown in the docs:
//   - Top bar: search input, scope chip, Clear, X close
//   - Pill tabs: Top / Apps / Objects / Datasets / Files (with counts)
//   - Idle (no query): recent-searches list (jump-to mode)
//   - Top tab: 4-quadrant grid of top samples per family
//   - Per-family tab: left filters sidebar + result rows
//   - Hotkeys footer
// Backend: parallel calls to POST /ontology/search per kind.
// ──────────────────────────────────────────────────────────────────

type TabId = 'top' | 'apps' | 'objects' | 'datasets' | 'files';

interface SearchFamilyState {
  data: SearchResult[];
  total: number;
}

interface CompassSearchFamilyState {
  data: CompassSearchResult[];
  total: number;
  nextCursor: string | null;
}

interface FamilyResults {
  apps: SearchFamilyState;
  objectTypes: SearchFamilyState;
  objects: SearchFamilyState;
  datasets: SearchFamilyState;
  files: SearchFamilyState;
  resources: CompassSearchFamilyState;
}

interface ResourceShortcut {
  key: string;
  label: string;
  subtitle: string;
  href: string;
  icon: GlyphName;
  resourceKind: ResourceKind;
  resourceId: string;
}

type SearchStatus = 'idle' | 'loading' | 'success' | 'empty' | 'error' | 'permission_denied';

const TABS: { id: TabId; label: string }[] = [
  { id: 'top', label: 'Top' },
  { id: 'apps', label: 'Apps' },
  { id: 'objects', label: 'Objects' },
  { id: 'datasets', label: 'Datasets' },
  { id: 'files', label: 'Files' },
];

const RECENT_SEARCHES_KEY = 'of_qs_recent_searches';
const RECENT_SEARCHES_MAX = 8;

const FILE_KINDS = new Set([
  'dashboard',
  'interface',
  'interface_binding',
  'link_type',
  'action_type',
  'shared_property_type',
]);

const KIND_ICON: Record<string, GlyphName> = {
  app: 'app',
  dashboard: 'spreadsheet',
  dataset: 'database',
  object_type: 'cube',
  object_instance: 'object',
  link_type: 'link',
  action_type: 'run',
  interface: 'document',
  interface_binding: 'document',
  shared_property_type: 'document',
};

const KIND_CHIP: Record<string, string> = {
  app: 'Module',
  dashboard: 'Dashboard',
  dataset: 'Dataset',
  object_type: 'Object type',
  object_instance: 'Object',
  link_type: 'Link type',
  action_type: 'Action',
  interface: 'Interface',
  interface_binding: 'Interface',
  shared_property_type: 'Property',
};

function emptyFamily(): SearchFamilyState {
  return { data: [], total: 0 };
}

function emptyCompassFamily(): CompassSearchFamilyState {
  return { data: [], total: 0, nextCursor: null };
}

function emptyFamilies(): FamilyResults {
  return {
    apps: emptyFamily(),
    objectTypes: emptyFamily(),
    objects: emptyFamily(),
    datasets: emptyFamily(),
    files: emptyFamily(),
    resources: emptyCompassFamily(),
  };
}

function tabFromParam(value: string | null): TabId {
  return TABS.find((t) => t.id === value)?.id ?? 'top';
}

function formatCount(n: number): string {
  if (!Number.isFinite(n) || n < 0) return '0';
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n >= 10_000_000 ? 0 : 1)}M`;
  if (n >= 10_000) return `${Math.round(n / 1000)}K`;
  if (n >= 1000) return `${(n / 1000).toFixed(1)}K`;
  return String(n);
}

function loadRecentSearches(): string[] {
  if (typeof window === 'undefined') return [];
  try {
    const raw = window.localStorage.getItem(RECENT_SEARCHES_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed.filter((v): v is string => typeof v === 'string') : [];
  } catch {
    return [];
  }
}

function persistRecentSearches(items: string[]) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(RECENT_SEARCHES_KEY, JSON.stringify(items.slice(0, RECENT_SEARCHES_MAX)));
  } catch {
    // ignore quota errors
  }
}

function pushRecentSearch(items: string[], query: string): string[] {
  const trimmed = query.trim();
  if (!trimmed) return items;
  const next = [trimmed, ...items.filter((q) => q.toLowerCase() !== trimmed.toLowerCase())];
  return next.slice(0, RECENT_SEARCHES_MAX);
}

function splitFilterTokens(value: string): string[] {
  return value
    .split(/[,\s]+/)
    .map((token) => token.trim())
    .filter(Boolean);
}

function shortToken(value: string, limit = 12): string {
  if (value.length <= limit) return value;
  return `${value.slice(0, Math.max(4, limit - 5))}...${value.slice(-4)}`;
}

function ridLocator(value: string): string {
  const parts = value.split('.');
  if (parts.length >= 5 && parts[0] === 'ri') return parts.slice(4).join('.');
  return value;
}

function looksLikeUUID(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value);
}

function compactRID(value: string): string {
  const parts = value.split('.');
  if (parts.length >= 5 && parts[0] === 'ri') {
    return `${parts[1]}.${parts[3]}.${shortToken(parts.slice(4).join('.'), 10)}`;
  }
  return shortToken(value, 14);
}

function formatDateTime(value: string): string {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function resourceKindForCompassType(type: string): ResourceKind {
  if (type === 'project') return 'ontology_project';
  if (type === 'folder') return 'ontology_folder';
  if (type === 'dataset') return 'dataset';
  if (type === 'pipeline') return 'pipeline';
  if (type === 'notebook') return 'notebook';
  if (type === 'app') return 'app';
  if (type === 'dashboard') return 'dashboard';
  if (type === 'report') return 'report';
  if (type === 'model') return 'model';
  if (type === 'workflow') return 'workflow';
  return 'other';
}

function recordCompassAccess(result: CompassSearchResult) {
  const resourceKind = resourceKindForCompassType(result.type);
  const resourceId = ridLocator(result.rid);
  recordAccess({ resource_kind: resourceKind, resource_id: resourceId }).catch(() => {});
}

function resourceKindLabel(kind: ResourceKind): string {
  return kind.replace(/^ontology_/, '').replace(/_/g, ' ');
}

function hrefForWorkspaceResource(kind: ResourceKind, id: string): string {
  return workspaceResourceStablePath(kind, id);
}

function glyphForWorkspaceResource(kind: ResourceKind): GlyphName {
  if (kind === 'ontology_project') return 'project';
  if (kind === 'ontology_folder') return 'folder';
  if (kind === 'dataset') return 'database';
  if (kind === 'pipeline') return 'graph';
  if (kind === 'notebook') return 'code';
  if (kind === 'app') return 'app';
  if (kind === 'dashboard') return 'spreadsheet';
  if (kind === 'report') return 'document';
  if (kind === 'model') return 'cube';
  if (kind === 'workflow') return 'run';
  return 'object';
}

function shortcutFromFavorite(entry: UserFavorite, labels: Map<string, string>): ResourceShortcut {
  const key = `${entry.resource_kind}:${entry.resource_id}`;
  const label = labels.get(key) ?? shortToken(entry.resource_id, 18);
  return {
    key: `favorite:${key}`,
    label,
    subtitle: `Favorite ${resourceKindLabel(entry.resource_kind)}`,
    href: hrefForWorkspaceResource(entry.resource_kind, entry.resource_id),
    icon: glyphForWorkspaceResource(entry.resource_kind),
    resourceKind: entry.resource_kind,
    resourceId: entry.resource_id,
  };
}

function shortcutFromRecent(entry: RecentEntry, labels: Map<string, string>): ResourceShortcut {
  const key = `${entry.resource_kind}:${entry.resource_id}`;
  const label = labels.get(key) ?? shortToken(entry.resource_id, 18);
  const openedAt = formatDateTime(entry.last_accessed_at);
  return {
    key: `recent:${key}`,
    label,
    subtitle: openedAt ? `${resourceKindLabel(entry.resource_kind)} · ${openedAt}` : resourceKindLabel(entry.resource_kind),
    href: hrefForWorkspaceResource(entry.resource_kind, entry.resource_id),
    icon: glyphForWorkspaceResource(entry.resource_kind),
    resourceKind: entry.resource_kind,
    resourceId: entry.resource_id,
  };
}

export function SearchPage() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();

  const [query, setQuery] = useState(() => searchParams.get('q') ?? '');
  const [submittedQuery, setSubmittedQuery] = useState(() => searchParams.get('q') ?? '');
  const [tab, setTab] = useState<TabId>(() => tabFromParam(searchParams.get('tab')));
  const [families, setFamilies] = useState<FamilyResults>(() => emptyFamilies());
  const [status, setStatus] = useState<SearchStatus>(() => (searchParams.get('q')?.trim() ? 'loading' : 'idle'));
  const [error, setError] = useState('');
  const [recent, setRecent] = useState<string[]>(() => loadRecentSearches());
  const [favoriteShortcuts, setFavoriteShortcuts] = useState<ResourceShortcut[]>([]);
  const [recentResourceShortcuts, setRecentResourceShortcuts] = useState<ResourceShortcut[]>([]);

  // Per-tab filter state
  const [appsTypeFilter, setAppsTypeFilter] = useState<Set<string>>(new Set());
  const [objectsTypeFilter, setObjectsTypeFilter] = useState<Set<string>>(new Set());
  const [filesKindFilter, setFilesKindFilter] = useState<Set<string>>(new Set());
  const [showCatalogOnly, setShowCatalogOnly] = useState(false);
  const [createdBy, setCreatedBy] = useState('');
  const [tagFilter, setTagFilter] = useState('');
  const [markingFilter, setMarkingFilter] = useState('');
  const [pathFilter, setPathFilter] = useState('');
  const [projectFilter, setProjectFilter] = useState('');
  const [includeTrash, setIncludeTrash] = useState(false);

  const requestRef = useRef(0);
  const inputRef = useRef<HTMLInputElement | null>(null);

  // Sync URL → state
  useEffect(() => {
    const nextQuery = searchParams.get('q') ?? '';
    const nextTab = tabFromParam(searchParams.get('tab'));
    setQuery(nextQuery);
    setSubmittedQuery(nextQuery);
    setTab(nextTab);
  }, [searchParams]);

  // Hotkeys: Cmd/Ctrl + J → focus input. ESC closes the page.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const cmd = e.metaKey || e.ctrlKey;
      if (cmd && e.key.toLowerCase() === 'j') {
        e.preventDefault();
        inputRef.current?.focus();
        inputRef.current?.select();
        return;
      }
      if (e.key === 'Escape') {
        navigate(-1);
      }
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [navigate]);

  // Auto-focus on mount
  useEffect(() => {
    const id = setTimeout(() => inputRef.current?.focus(), 30);
    return () => clearTimeout(id);
  }, []);

  // Jump-to personalization: resource shortcuts are permission-filtered by the
  // backend endpoints that own favorites/recents.
  useEffect(() => {
    let cancelled = false;
    Promise.all([
      listFavorites({ limit: 8 }).catch(() => [] as UserFavorite[]),
      listRecents({ limit: 8 }).catch(() => [] as RecentEntry[]),
    ])
      .then(async ([favorites, recents]) => {
        const items = [
          ...favorites.map((entry) => ({ resource_kind: entry.resource_kind, resource_id: entry.resource_id })),
          ...recents.map((entry) => ({ resource_kind: entry.resource_kind, resource_id: entry.resource_id })),
        ];
        const labels = new Map<string, string>();
        if (items.length > 0) {
          const response = await resolveResourceLabels(items).catch(() => null);
          for (const entry of response?.data ?? []) {
            if (entry.label) labels.set(`${entry.resource_kind}:${entry.resource_id}`, entry.label);
          }
        }
        if (cancelled) return;
        setFavoriteShortcuts(favorites.map((entry) => shortcutFromFavorite(entry, labels)));
        setRecentResourceShortcuts(recents.map((entry) => shortcutFromRecent(entry, labels)));
      })
      .catch(() => {
        if (!cancelled) {
          setFavoriteShortcuts([]);
          setRecentResourceShortcuts([]);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Run a parallel multi-kind search whenever the submitted query or Compass
  // project/marking facets change.
  useEffect(() => {
    const trimmed = submittedQuery.trim();
    if (!trimmed) {
      requestRef.current += 1;
      setFamilies(emptyFamilies());
      setStatus('idle');
      setError('');
      return;
    }

    const requestId = requestRef.current + 1;
    requestRef.current = requestId;
    setStatus('loading');
    setError('');

    const fetchFamily = async (kind: string | undefined, limit: number) => {
      try {
        const res = await searchOntology({ query: trimmed, kind, limit, semantic: true, hybrid_strategy: 'rrf' });
        return { data: res.data, total: res.total };
      } catch (cause) {
        throw cause;
      }
    };

    const compassProject = projectFilter.trim();
    const compassMarkings = splitFilterTokens(markingFilter);

    Promise.all([
      fetchFamily('app', 12),
      fetchFamily('object_type', 12),
      fetchFamily('object_instance', 25),
      fetchFamily('dataset', 25),
      fetchFamily(undefined, 50),
      searchCompass({
        q: trimmed,
        project: compassProject || undefined,
        marking: compassMarkings.length > 0 ? compassMarkings : undefined,
        limit: 50,
      }),
    ])
      .then(([apps, objectTypes, objects, datasets, all, resources]) => {
        if (requestRef.current !== requestId) return;
        const filesData = all.data.filter((r) => FILE_KINDS.has(r.kind));
        const filesTotal = filesData.length;
        const next: FamilyResults = {
          apps,
          objectTypes,
          objects,
          datasets,
          files: { data: filesData, total: filesTotal },
          resources: {
            data: resources.data,
            total: resources.data.length,
            nextCursor: resources.next_cursor ?? null,
          },
        };
        setFamilies(next);
        const hasAny = apps.total + objectTypes.total + objects.total + datasets.total + filesTotal + resources.data.length > 0;
        setStatus(hasAny ? 'success' : 'empty');
      })
      .catch((cause) => {
        if (requestRef.current !== requestId) return;
        setFamilies(emptyFamilies());
        setError(cause instanceof Error ? cause.message : 'Search failed');
        setStatus(cause instanceof ApiError && (cause.status === 401 || cause.status === 403) ? 'permission_denied' : 'error');
      });
  }, [markingFilter, projectFilter, submittedQuery]);

  const tabCounts = useMemo(
    () => ({
      top: 0,
      apps: families.apps.total,
      objects: families.objectTypes.total + families.objects.total,
      datasets: families.datasets.total,
      files: families.files.total + families.resources.total,
    }),
    [families],
  );

  const goTab = useCallback(
    (next: TabId) => {
      setTab(next);
      const params = new URLSearchParams(searchParams);
      if (next === 'top') params.delete('tab');
      else params.set('tab', next);
      setSearchParams(params, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  const hasQuery = submittedQuery.trim().length > 0;

  const submit = useCallback(
    (rawQuery: string) => {
      const trimmed = rawQuery.trim();
      const params = new URLSearchParams(searchParams);
      if (trimmed) params.set('q', trimmed);
      else params.delete('q');
      setSearchParams(params, { replace: false });
      setSubmittedQuery(trimmed);
      if (trimmed) {
        const nextRecent = pushRecentSearch(recent, trimmed);
        setRecent(nextRecent);
        persistRecentSearches(nextRecent);
      }
    },
    [recent, searchParams, setSearchParams],
  );

  function onFormSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    submit(query);
  }

  function clearSearch() {
    setQuery('');
    setSubmittedQuery('');
    const params = new URLSearchParams(searchParams);
    params.delete('q');
    setSearchParams(params, { replace: true });
    inputRef.current?.focus();
  }

  function closePage() {
    navigate(-1);
  }

  const showFilterChip = tab !== 'top' && hasQuery;
  const visibleTabs = hasQuery ? TABS : TABS.filter((t) => t.id !== 'top');

  return (
    <section className="of-quicksearch" aria-label="Quicksearch">
      <header className="of-quicksearch__header">
        <span className="of-quicksearch__searchIcon">
          <Glyph name="search" size={18} />
        </span>

        {showFilterChip && (
          <span className="of-quicksearch__filterChip">
            {TABS.find((t) => t.id === tab)?.label}
            <button
              type="button"
              aria-label="Clear filter"
              className="of-quicksearch__filterChipClose"
              onClick={() => goTab('top')}
            >
              <Glyph name="x" size={12} />
            </button>
          </span>
        )}

        <form onSubmit={onFormSubmit} className="of-quicksearch__inputWrap" role="search">
          <input
            ref={inputRef}
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search for anything in your enterprise…"
            className="of-quicksearch__input"
            aria-label="Quicksearch input"
            autoComplete="off"
          />
        </form>

        {(query || hasQuery) && (
          <button type="button" className="of-quicksearch__clear" onClick={clearSearch}>
            Clear
          </button>
        )}

        <button type="button" className="of-quicksearch__close" aria-label="Close" onClick={closePage}>
          <Glyph name="x" size={16} />
        </button>
      </header>

      <div className="of-quicksearch__tabsRow">
        <div className="of-quicksearch__tabs" role="tablist">
          {!hasQuery && <span className="of-quicksearch__filterLead">Filter</span>}
          {visibleTabs.map((t) => {
            const count = tabCounts[t.id];
            return (
              <button
                key={t.id}
                type="button"
                role="tab"
                aria-selected={tab === t.id}
                data-active={tab === t.id ? 'true' : undefined}
                onClick={() => goTab(t.id)}
                className="of-quicksearch__tab"
              >
                <span>{t.label}</span>
                {t.id !== 'top' && status === 'success' && (
                  <span className="of-quicksearch__tabCount">{formatCount(count)}</span>
                )}
              </button>
            );
          })}
        </div>
        <div className="of-quicksearch__topRight">
          {!hasQuery && (
            <div className="of-quicksearch__topActions">
              <Link to="/projects" className="of-quicksearch__topAction">
                <span className="of-quicksearch__topActionBadge of-quicksearch__topActionBadge--catalog">
                  <Glyph name="check" size={11} tone="#ffffff" />
                </span>
                <span>Data Catalog</span>
              </Link>
              <Link to="/object-explorer" className="of-quicksearch__topAction">
                <span className="of-quicksearch__topActionBadge of-quicksearch__topActionBadge--explorer">
                  <Glyph name="search" size={11} />
                </span>
                <span>Object Explorer</span>
              </Link>
            </div>
          )}
        </div>
      </div>

      <div className="of-quicksearch__layout">
        {tab !== 'top' && (
          <aside className="of-quicksearch__sidebar">
            <SidebarFilters
              tab={tab}
              families={families}
              showCatalogOnly={showCatalogOnly}
              setShowCatalogOnly={setShowCatalogOnly}
              appsTypeFilter={appsTypeFilter}
              setAppsTypeFilter={setAppsTypeFilter}
              objectsTypeFilter={objectsTypeFilter}
              setObjectsTypeFilter={setObjectsTypeFilter}
              filesKindFilter={filesKindFilter}
              setFilesKindFilter={setFilesKindFilter}
              createdBy={createdBy}
              setCreatedBy={setCreatedBy}
              tagFilter={tagFilter}
              setTagFilter={setTagFilter}
              markingFilter={markingFilter}
              setMarkingFilter={setMarkingFilter}
              pathFilter={pathFilter}
              setPathFilter={setPathFilter}
              projectFilter={projectFilter}
              setProjectFilter={setProjectFilter}
              includeTrash={includeTrash}
              setIncludeTrash={setIncludeTrash}
            />
          </aside>
        )}

        <main className="of-quicksearch__content">
          {status === 'idle' && (
            <RecentSearchesView
              recent={recent}
              favorites={favoriteShortcuts}
              resources={recentResourceShortcuts}
              onPick={(q) => {
                setQuery(q);
                submit(q);
              }}
              onClear={() => {
                setRecent([]);
                persistRecentSearches([]);
              }}
            />
          )}

          {status === 'loading' && <LoadingSkeleton />}

          {(status === 'error' || status === 'permission_denied') && (
            <div className="of-quicksearch__inlineError">
              {error || (status === 'permission_denied' ? 'Permission denied for this search.' : 'Search failed.')}
            </div>
          )}

          {status === 'empty' && (
            <div className="of-quicksearch__emptyBlock">
              <p style={{ margin: 0, fontSize: 14, color: 'var(--qs-text-strong)' }}>No matches</p>
              <p style={{ margin: '6px 0 0' }}>
                Nothing matched <strong>"{submittedQuery}"</strong>. Try a different keyword or check the
                {' '}
                <Link to="/object-explorer" style={{ color: 'var(--qs-link)' }}>Object Explorer</Link>.
              </p>
            </div>
          )}

          {status === 'success' && tab === 'top' && (
            <TopTabView
              query={submittedQuery}
              families={families}
              onSeeMore={(next) => goTab(next)}
            />
          )}

          {status === 'success' && tab === 'apps' && <AppsTabView families={families} appsTypeFilter={appsTypeFilter} />}
          {status === 'success' && tab === 'objects' && (
            <ObjectsTabView families={families} objectsTypeFilter={objectsTypeFilter} />
          )}
          {status === 'success' && tab === 'datasets' && <DatasetsTabView families={families} />}
          {status === 'success' && tab === 'files' && (
            <FilesTabView families={families} filesKindFilter={filesKindFilter} tagFilter={tagFilter} />
          )}
        </main>
      </div>

      <footer className="of-quicksearch__hotkeys">
        <span>Hotkeys</span>
        <span className="of-quicksearch__hotkey">
          <span className="of-quicksearch__kbd">⌘</span>
          <span className="of-quicksearch__hotkeyJoin">+</span>
          <span className="of-quicksearch__kbd">J</span>
          <span>Open Quicksearch</span>
        </span>
        {hasQuery && (
          <>
            <span className="of-quicksearch__hotkey">
              <span className="of-quicksearch__kbd">↑</span>
              <span className="of-quicksearch__kbd">↓</span>
              <span>Move in list</span>
            </span>
            <span className="of-quicksearch__hotkey">
              <span className="of-quicksearch__kbd">⏎</span>
              <span>select item</span>
            </span>
          </>
        )}
        {hasQuery && (
          <span className="of-quicksearch__experience">
            <span>You are using the new search experience.</span>
            <button type="button" className="of-quicksearch__experienceBtn">Turn off</button>
          </span>
        )}
      </footer>
    </section>
  );
}

// ──────────────────────────────────────────────────────────────────
// Recent searches (jump-to mode)
// ──────────────────────────────────────────────────────────────────

interface RecentSearchesViewProps {
  recent: string[];
  favorites: ResourceShortcut[];
  resources: ResourceShortcut[];
  onPick: (q: string) => void;
  onClear: () => void;
}

function RecentSearchesView({ recent, favorites, resources, onPick, onClear }: RecentSearchesViewProps) {
  return (
    <div className="of-quicksearch__recent">
      <div className="of-quicksearch__recentHeader">
        <span>Recent Searches</span>
        {recent.length > 0 && (
          <button type="button" className="of-quicksearch__recentClear" onClick={onClear}>
            Clear recent
          </button>
        )}
      </div>

      {recent.length === 0 ? (
        <div className="of-quicksearch__recentEmpty">
          Start typing to search across apps, objects, datasets, and files.
        </div>
      ) : (
        <ul className="of-quicksearch__recentList">
          {recent.map((q) => (
            <li key={q}>
              <button type="button" className="of-quicksearch__recentRow" onClick={() => onPick(q)}>
                <Glyph name="search" size={15} tone="#79b8ff" />
                <span>{q}</span>
              </button>
            </li>
          ))}
        </ul>
      )}

      <ResourceShortcutSection title="Favorites" rows={favorites} />
      <ResourceShortcutSection title="Recent resources" rows={resources} />
    </div>
  );
}

function ResourceShortcutSection({ title, rows }: { title: string; rows: ResourceShortcut[] }) {
  if (rows.length === 0) return null;
  return (
    <>
      <div className="of-quicksearch__recentHeader">
        <span>{title}</span>
      </div>
      <ul className="of-quicksearch__recentList of-quicksearch__recentList--resources">
        {rows.map((row) => (
          <li key={row.key}>
            <Link
              to={row.href}
              className="of-quicksearch__recentRow of-quicksearch__recentResourceRow"
              onClick={() => recordAccess({ resource_kind: row.resourceKind, resource_id: row.resourceId }).catch(() => {})}
            >
              <span className="of-quicksearch__rowIcon of-quicksearch__recentResourceIcon">
                <Glyph name={row.icon} size={14} />
              </span>
              <span className="of-quicksearch__recentResourceText">
                <span>{row.label}</span>
                <span>{row.subtitle}</span>
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </>
  );
}

// ──────────────────────────────────────────────────────────────────
// Top tab — 4-quadrant grid
// ──────────────────────────────────────────────────────────────────

interface TopTabViewProps {
  query: string;
  families: FamilyResults;
  onSeeMore: (tab: TabId) => void;
}

function TopTabView({ query, families, onSeeMore }: TopTabViewProps) {
  return (
    <div>
      <div className="of-quicksearch__topSamples">
        Top samples from your results for{' '}
        <strong>"{query}"</strong>
      </div>
      <div className="of-quicksearch__quadGrid">
        <Quadrant
          title="Apps"
          rows={families.apps.data.slice(0, 4)}
          allLabel="All apps"
          onSeeMore={() => onSeeMore('apps')}
          query={query}
        />
        <Quadrant
          title="Objects"
          rows={[...families.objectTypes.data.slice(0, 2), ...families.objects.data.slice(0, 3)]}
          allLabel="All objects"
          onSeeMore={() => onSeeMore('objects')}
          query={query}
        />
        <Quadrant
          title="Datasets"
          rows={families.datasets.data.slice(0, 4)}
          allLabel="All datasets"
          onSeeMore={() => onSeeMore('datasets')}
          query={query}
        />
        <FilesQuadrant
          title="Files"
          rows={families.files.data.slice(0, Math.max(0, 4 - Math.min(2, families.resources.data.length)))}
          resources={families.resources.data.slice(0, 2)}
          allLabel="All files"
          onSeeMore={() => onSeeMore('files')}
          query={query}
        />
      </div>
    </div>
  );
}

interface QuadrantProps {
  title: string;
  rows: SearchResult[];
  allLabel: string;
  onSeeMore: () => void;
  query: string;
}

function Quadrant({ title, rows, allLabel, onSeeMore, query }: QuadrantProps) {
  return (
    <section className="of-quicksearch__quad">
      <header className="of-quicksearch__quadHeader">{title}</header>
      <div className="of-quicksearch__quadBody">
        {rows.length === 0 ? (
          <div className="of-quicksearch__quadEmpty">No matches for "{query}"</div>
        ) : (
          rows.map((row) => <CompactResultRow key={`${row.kind}-${row.id}`} result={row} />)
        )}
      </div>
      <button type="button" className="of-quicksearch__quadAll" onClick={onSeeMore}>
        {allLabel}
      </button>
    </section>
  );
}

interface FilesQuadrantProps extends QuadrantProps {
  resources: CompassSearchResult[];
}

function FilesQuadrant({ title, rows, resources, allLabel, onSeeMore, query }: FilesQuadrantProps) {
  const hasRows = rows.length > 0 || resources.length > 0;
  return (
    <section className="of-quicksearch__quad">
      <header className="of-quicksearch__quadHeader">{title}</header>
      <div className="of-quicksearch__quadBody">
        {!hasRows ? (
          <div className="of-quicksearch__quadEmpty">No matches for "{query}"</div>
        ) : (
          <>
            {resources.map((row) => <CompactResourceRow key={row.rid} result={row} />)}
            {rows.map((row) => <CompactResultRow key={`${row.kind}-${row.id}`} result={row} />)}
          </>
        )}
      </div>
      <button type="button" className="of-quicksearch__quadAll" onClick={onSeeMore}>
        {allLabel}
      </button>
    </section>
  );
}

function CompactResultRow({ result }: { result: SearchResult }) {
  const icon = KIND_ICON[result.kind] ?? 'document';
  return (
    <Link to={result.route} className="of-quicksearch__row" style={{ textDecoration: 'none' }}>
      <span className="of-quicksearch__rowIcon">
        <Glyph name={icon} size={14} />
      </span>
      <div className="of-quicksearch__rowMain">
        <div className="of-quicksearch__rowTitleLine">
          <span className="of-quicksearch__rowTitle">{result.title || result.id}</span>
        </div>
        {result.subtitle && <span className="of-quicksearch__rowMeta">{result.subtitle}</span>}
      </div>
    </Link>
  );
}

function CompactResourceRow({ result }: { result: CompassSearchResult }) {
  const definition = getResourceTypeDefinition(result.type);
  const href = openURLForCompassResource(result);
  return (
    <Link
      to={href}
      className="of-quicksearch__row"
      style={{ textDecoration: 'none' }}
      onClick={() => recordCompassAccess(result)}
    >
      <span className="of-quicksearch__rowIcon">
        <Glyph name={definition.defaultIcon} size={14} />
      </span>
      <div className="of-quicksearch__rowMain">
        <div className="of-quicksearch__rowTitleLine">
          <span className="of-quicksearch__rowTitle">{result.display_name || compactRID(result.rid)}</span>
        </div>
        <span className="of-quicksearch__rowMeta">{definition.displayName} · {compactRID(result.rid)}</span>
      </div>
    </Link>
  );
}

// ──────────────────────────────────────────────────────────────────
// Apps tab
// ──────────────────────────────────────────────────────────────────

function AppsTabView({ families, appsTypeFilter }: { families: FamilyResults; appsTypeFilter: Set<string> }) {
  const all = families.apps.data;
  const filtered = appsTypeFilter.size === 0 ? all : all.filter((r) => appsTypeFilter.has(r.kind));
  if (filtered.length === 0) return <EmptyState label="No apps matched these filters." />;
  return (
    <div>
      {filtered.map((r) => (
        <AppOrFileRow key={`${r.kind}-${r.id}`} result={r} chipLabel="Module" />
      ))}
    </div>
  );
}

// ──────────────────────────────────────────────────────────────────
// Objects tab
// ──────────────────────────────────────────────────────────────────

function ObjectsTabView({ families, objectsTypeFilter }: { families: FamilyResults; objectsTypeFilter: Set<string> }) {
  const types = families.objectTypes.data;
  const objs = families.objects.data;
  const filteredObjs = objectsTypeFilter.size === 0 ? objs : objs.filter((r) => objectsTypeFilter.has(r.object_type_id ?? ''));

  if (types.length === 0 && filteredObjs.length === 0) {
    return <EmptyState label="No object types or objects matched." />;
  }

  return (
    <div>
      {types.length > 0 && (
        <>
          <header className="of-quicksearch__sectionHead">
            Object types
            <span className="of-quicksearch__sectionCount">{formatCount(families.objectTypes.total)}</span>
          </header>
          <div className="of-quicksearch__typeRow">
            {types.slice(0, 4).map((t) => (
              <Link key={t.id} to={t.route} className="of-quicksearch__typeCard">
                <span className="of-quicksearch__typeCardIcon">
                  <Glyph name="cube" size={12} />
                </span>
                <span className="of-quicksearch__typeCardLink">{t.title || t.id}</span>
                {typeof (t.metadata?.linked_count) === 'number' && (
                  <span style={{ color: 'var(--qs-text-muted)', fontSize: 12 }}>
                    <Glyph name="link" size={12} /> {(t.metadata?.linked_count) as number}
                  </span>
                )}
              </Link>
            ))}
          </div>
          {types.length > 4 && (
            <div style={{ textAlign: 'center', margin: '0 0 18px' }}>
              <button type="button" className="of-quicksearch__filterMore">Show more</button>
            </div>
          )}
        </>
      )}

      {filteredObjs.length > 0 && (
        <>
          <header className="of-quicksearch__sectionHead">
            Objects
            <span className="of-quicksearch__sectionCount">{formatCount(families.objects.total)}</span>
          </header>
          {filteredObjs.map((r) => (
            <ObjectRow key={`${r.kind}-${r.id}`} result={r} />
          ))}
        </>
      )}
    </div>
  );
}

function ObjectRow({ result }: { result: SearchResult }) {
  const icon = KIND_ICON[result.kind] ?? 'object';
  const typeLabel = (result.metadata?.object_type_name as string | undefined) ?? result.subtitle ?? '';
  const description = (result.metadata?.description as string | undefined) ?? '';
  const projectName = (result.metadata?.project_name as string | undefined) ?? '';

  return (
    <Link to={result.route} className="of-quicksearch__row" style={{ textDecoration: 'none' }}>
      <span className="of-quicksearch__rowIcon">
        <Glyph name={icon} size={14} />
      </span>
      <div className="of-quicksearch__rowMain">
        <div className="of-quicksearch__rowTitleLine">
          <span className="of-quicksearch__rowTitle">{result.title || result.id}</span>
        </div>
        <div className="of-quicksearch__rowKv">
          {typeLabel && (
            <span><strong>Object type:</strong> {typeLabel}</span>
          )}
          {description && (
            <span><strong>Description:</strong> {description}</span>
          )}
          {projectName && (
            <span><strong>Project Name:</strong> {projectName}</span>
          )}
        </div>
      </div>
      <div className="of-quicksearch__rowSide">
        <span className="of-quicksearch__rowChip">{KIND_CHIP[result.kind] ?? 'Object'}</span>
      </div>
    </Link>
  );
}

// ──────────────────────────────────────────────────────────────────
// Datasets tab
// ──────────────────────────────────────────────────────────────────

function DatasetsTabView({ families }: { families: FamilyResults }) {
  const rows = families.datasets.data;
  if (rows.length === 0) return <EmptyState label="No datasets matched." />;
  return (
    <div>
      {rows.map((r) => (
        <DatasetRow key={`${r.kind}-${r.id}`} result={r} />
      ))}
    </div>
  );
}

function DatasetRow({ result }: { result: SearchResult }) {
  const branchCount = (result.metadata?.branch_count as number | undefined) ?? 1;
  const fileCount = (result.metadata?.file_count as number | undefined) ?? 1;
  const linkCount = (result.metadata?.link_count as number | undefined) ?? 0;
  return (
    <Link to={result.route} className="of-quicksearch__row" style={{ textDecoration: 'none' }}>
      <span className="of-quicksearch__rowIcon">
        <Glyph name="database" size={14} />
      </span>
      <div className="of-quicksearch__rowMain">
        <div className="of-quicksearch__rowTitleLine">
          <span className="of-quicksearch__rowTitle">{result.title || result.id}</span>
          {result.subtitle && <span className="of-quicksearch__rowMeta">• {result.subtitle}</span>}
        </div>
        {result.snippet && <span className="of-quicksearch__rowMeta">{result.snippet}</span>}
      </div>
      <div className="of-quicksearch__rowSide">
        <span className="of-quicksearch__rowStat" title="Branches">
          <Glyph name="graph" size={13} /> {branchCount}
        </span>
        <span className="of-quicksearch__rowStat" title="Files">
          <Glyph name="document" size={13} /> {fileCount}
        </span>
        <span className="of-quicksearch__rowStat" title="Links">
          <Glyph name="link" size={13} /> {linkCount}
        </span>
        <button type="button" className="of-quicksearch__rowAction">Raw dataset</button>
      </div>
    </Link>
  );
}

// ──────────────────────────────────────────────────────────────────
// Files tab
// ──────────────────────────────────────────────────────────────────

function FilesTabView({
  families,
  filesKindFilter,
  tagFilter,
}: {
  families: FamilyResults;
  filesKindFilter: Set<string>;
  tagFilter: string;
}) {
  const rows = families.files.data;
  const filtered = filesKindFilter.size === 0 ? rows : rows.filter((r) => filesKindFilter.has(r.kind));
  const normalizedTag = tagFilter.trim().toLowerCase();
  const resourcesByType = filesKindFilter.size === 0
    ? families.resources.data
    : families.resources.data.filter((r) => filesKindFilter.has(r.type));
  const resources = normalizedTag
    ? resourcesByType.filter((r) => r.tags.some((tag) => tag.toLowerCase().includes(normalizedTag)))
    : resourcesByType;

  if (filtered.length === 0 && resources.length === 0) return <EmptyState label="No files matched these filters." />;
  return (
    <div>
      {resources.length > 0 && (
        <>
          <header className="of-quicksearch__sectionHead">
            Resources
            <span className="of-quicksearch__sectionCount">{formatCount(families.resources.total)}</span>
          </header>
          {resources.map((r) => (
            <CompassResourceRow key={r.rid} result={r} />
          ))}
        </>
      )}
      {filtered.length > 0 && resources.length > 0 && (
        <header className="of-quicksearch__sectionHead of-quicksearch__sectionHead--spaced">
          Files
          <span className="of-quicksearch__sectionCount">{formatCount(families.files.total)}</span>
        </header>
      )}
      {filtered.map((r) => (
        <AppOrFileRow key={`${r.kind}-${r.id}`} result={r} chipLabel={KIND_CHIP[r.kind] ?? 'File'} />
      ))}
    </div>
  );
}

function CompassResourceRow({ result }: { result: CompassSearchResult }) {
  const definition = getResourceTypeDefinition(result.type);
  const href = openURLForCompassResource(result);
  const markings = result.marking_rids.slice(0, 4);
  const hiddenMarkings = result.marking_rids.length - markings.length;
  const tags = result.tags.slice(0, 3);
  const [favoriteState, setFavoriteState] = useState<'idle' | 'saving' | 'saved'>('idle');
  const favoriteResourceID = ridLocator(result.rid);
  const canFavorite = looksLikeUUID(favoriteResourceID);

  function favoriteResult() {
    const resourceKind = resourceKindForCompassType(result.type);
    if (!canFavorite) return;
    setFavoriteState('saving');
    createFavorite({ resource_kind: resourceKind, resource_id: favoriteResourceID })
      .then(() => setFavoriteState('saved'))
      .catch(() => setFavoriteState('idle'));
  }

  return (
    <article className="of-quicksearch__row of-quicksearch__row--resource">
      <span className="of-quicksearch__rowIcon">
        <Glyph name={definition.defaultIcon} size={14} />
      </span>
      <div className="of-quicksearch__rowMain">
        <div className="of-quicksearch__rowTitleLine">
          <Link to={href} className="of-quicksearch__rowTitle" onClick={() => recordCompassAccess(result)}>
            {result.display_name || compactRID(result.rid)}
          </Link>
          <span className="of-quicksearch__rowMeta">· {definition.displayName}</span>
          {result.is_deleted && <span className="of-quicksearch__rowChip of-quicksearch__rowChip--danger">Trash</span>}
        </div>
        {result.summary && <span className="of-quicksearch__rowMeta">{result.summary}</span>}
        <span className="of-quicksearch__rowPath">{result.open_url || href}</span>
        <div className="of-quicksearch__badgeRow">
          {markings.map((marking) => (
            <span key={marking} className="of-quicksearch__markingBadge" title={marking}>
              <Glyph name="shield" size={12} />
              {compactRID(marking)}
            </span>
          ))}
          {hiddenMarkings > 0 && <span className="of-quicksearch__markingBadge">+{hiddenMarkings}</span>}
          {tags.map((tag) => (
            <span key={tag} className="of-quicksearch__tagBadge" title={tag}>
              <Glyph name="tag" size={12} />
              {tag}
            </span>
          ))}
        </div>
      </div>
      <div className="of-quicksearch__rowSide of-quicksearch__rowSide--resource">
        <span className="of-quicksearch__rowStat" title="Last modified">
          <Glyph name="history" size={13} /> {formatDateTime(result.last_modified_at)}
        </span>
        <span className="of-quicksearch__rowChip">{definition.displayName}</span>
        <button
          type="button"
          className="of-icon-button"
          title={canFavorite ? (favoriteState === 'saved' ? 'Saved to favorites' : 'Add to favorites') : 'Resource cannot be favorited'}
          aria-label={canFavorite ? (favoriteState === 'saved' ? 'Saved to favorites' : 'Add to favorites') : 'Resource cannot be favorited'}
          onClick={favoriteResult}
          disabled={!canFavorite || favoriteState === 'saving' || favoriteState === 'saved'}
        >
          <Glyph name={favoriteState === 'saved' ? 'star-filled' : 'star'} size={14} />
        </button>
        <OpenWithMenu
          resourceKind={result.type}
          resourceId={ridLocator(result.rid) || result.rid}
          resourceRid={result.rid}
          projectRid={result.owning_project_rid}
          projectId={result.owning_project_id}
          openUrl={result.open_url}
          onOpen={() => recordCompassAccess(result)}
        />
      </div>
    </article>
  );
}

function AppOrFileRow({ result, chipLabel }: { result: SearchResult; chipLabel: string }) {
  const icon = KIND_ICON[result.kind] ?? 'document';
  const path = (result.metadata?.path as string | undefined) ?? '';
  const updated = (result.metadata?.updated_at as string | undefined) ?? '';
  const branchCount = (result.metadata?.branch_count as number | undefined) ?? 1;
  const linkCount = (result.metadata?.link_count as number | undefined) ?? 0;

  return (
    <Link to={result.route} className="of-quicksearch__row" style={{ textDecoration: 'none' }}>
      <span className="of-quicksearch__rowIcon">
        <Glyph name={icon} size={14} />
      </span>
      <div className="of-quicksearch__rowMain">
        <div className="of-quicksearch__rowTitleLine">
          <span className="of-quicksearch__rowTitle">{result.title || result.id}</span>
          {updated && <span className="of-quicksearch__rowMeta">• {updated}</span>}
        </div>
        {path && <span className="of-quicksearch__rowPath">{path}</span>}
        {!path && result.subtitle && <span className="of-quicksearch__rowMeta">{result.subtitle}</span>}
      </div>
      <div className="of-quicksearch__rowSide">
        <span className="of-quicksearch__rowStat" title="Branches">
          <Glyph name="graph" size={13} /> {branchCount}
        </span>
        <span className="of-quicksearch__rowStat" title="Links">
          <Glyph name="link" size={13} /> {linkCount}
        </span>
        <span className="of-quicksearch__rowChip">{chipLabel}</span>
      </div>
    </Link>
  );
}

// ──────────────────────────────────────────────────────────────────
// Sidebar filters (Apps/Objects/Datasets/Files)
// ──────────────────────────────────────────────────────────────────

interface SidebarFiltersProps {
  tab: TabId;
  families: FamilyResults;
  showCatalogOnly: boolean;
  setShowCatalogOnly: (v: boolean) => void;
  appsTypeFilter: Set<string>;
  setAppsTypeFilter: (v: Set<string>) => void;
  objectsTypeFilter: Set<string>;
  setObjectsTypeFilter: (v: Set<string>) => void;
  filesKindFilter: Set<string>;
  setFilesKindFilter: (v: Set<string>) => void;
  createdBy: string;
  setCreatedBy: (v: string) => void;
  tagFilter: string;
  setTagFilter: (v: string) => void;
  markingFilter: string;
  setMarkingFilter: (v: string) => void;
  pathFilter: string;
  setPathFilter: (v: string) => void;
  projectFilter: string;
  setProjectFilter: (v: string) => void;
  includeTrash: boolean;
  setIncludeTrash: (v: boolean) => void;
}

function SidebarFilters(props: SidebarFiltersProps) {
  const {
    tab,
    families,
    showCatalogOnly,
    setShowCatalogOnly,
    appsTypeFilter,
    setAppsTypeFilter,
    objectsTypeFilter,
    setObjectsTypeFilter,
    filesKindFilter,
    setFilesKindFilter,
    createdBy,
    setCreatedBy,
    tagFilter,
    setTagFilter,
    markingFilter,
    setMarkingFilter,
    pathFilter,
    setPathFilter,
    projectFilter,
    setProjectFilter,
    includeTrash,
    setIncludeTrash,
  } = props;

  function toggleSet(set: Set<string>, value: string, setter: (next: Set<string>) => void) {
    const next = new Set(set);
    if (next.has(value)) next.delete(value);
    else next.add(value);
    setter(next);
  }

  // Buckets per tab
  const buckets = useMemo(() => {
    if (tab === 'apps') {
      const totalsByKind = new Map<string, number>();
      for (const r of families.apps.data) totalsByKind.set(r.kind, (totalsByKind.get(r.kind) ?? 0) + 1);
      return Array.from(totalsByKind.entries()).map(([kind, count]) => ({
        key: kind,
        label: KIND_CHIP[kind] ?? kind,
        count,
        icon: KIND_ICON[kind] ?? ('document' as GlyphName),
      }));
    }
    if (tab === 'objects') {
      const byType = new Map<string, { label: string; count: number }>();
      for (const r of families.objects.data) {
        const id = r.object_type_id ?? r.kind;
        const label = (r.metadata?.object_type_name as string | undefined) ?? id;
        const cur = byType.get(id);
        byType.set(id, { label, count: (cur?.count ?? 0) + 1 });
      }
      for (const t of families.objectTypes.data) {
        const id = t.id;
        const label = t.title || id;
        if (!byType.has(id)) byType.set(id, { label, count: 0 });
      }
      return Array.from(byType.entries()).map(([key, v]) => ({
        key,
        label: v.label,
        count: v.count,
        icon: 'cube' as GlyphName,
      }));
    }
    if (tab === 'files') {
      const byKind = new Map<string, number>();
      for (const r of families.files.data) byKind.set(r.kind, (byKind.get(r.kind) ?? 0) + 1);
      for (const r of families.resources.data) byKind.set(r.type, (byKind.get(r.type) ?? 0) + 1);
      return Array.from(byKind.entries()).map(([kind, count]) => ({
        key: kind,
        label: KIND_CHIP[kind] ?? getResourceTypeDefinition(kind).displayName,
        count,
        icon: KIND_ICON[kind] ?? getResourceTypeDefinition(kind).defaultIcon,
      }));
    }
    return [];
  }, [tab, families]);

  const maxCount = Math.max(1, ...buckets.map((b) => b.count));

  return (
    <div>
      {(tab === 'apps' || tab === 'datasets' || tab === 'files') && (
        <div className="of-quicksearch__filterGroup">
          <span className="of-quicksearch__filterLabel">Only show</span>
          <label className="of-quicksearch__filterRow">
            <input
              type="checkbox"
              checked={showCatalogOnly}
              onChange={(e) => setShowCatalogOnly(e.target.checked)}
            />
            <span className="of-quicksearch__filterIcon">
              <Glyph name="check" size={12} tone="#a78bfa" />
            </span>
            <span className="of-quicksearch__filterText">Catalog items</span>
            <span className="of-quicksearch__filterCount">0</span>
            <span className="of-quicksearch__filterBar">
              <span className="of-quicksearch__filterBarFill" style={{ width: '0%' }} />
            </span>
          </label>
        </div>
      )}

      {(tab === 'apps' || tab === 'files') && buckets.length > 0 && (
        <div className="of-quicksearch__filterGroup">
          <span className="of-quicksearch__filterLabel">Type</span>
          {buckets.map((b) => {
            const active = (tab === 'apps' ? appsTypeFilter : filesKindFilter).has(b.key);
            const setter = tab === 'apps' ? setAppsTypeFilter : setFilesKindFilter;
            const current = tab === 'apps' ? appsTypeFilter : filesKindFilter;
            return (
              <label key={b.key} className="of-quicksearch__filterRow">
                <input
                  type="checkbox"
                  checked={active}
                  onChange={() => toggleSet(current, b.key, setter)}
                />
                <span className="of-quicksearch__filterIcon">
                  <Glyph name={b.icon} size={13} />
                </span>
                <span className="of-quicksearch__filterText">{b.label}</span>
                <span className="of-quicksearch__filterCount">{b.count}</span>
                <span className="of-quicksearch__filterBar">
                  <span
                    className="of-quicksearch__filterBarFill"
                    style={{ width: `${Math.round((b.count / maxCount) * 100)}%` }}
                  />
                </span>
              </label>
            );
          })}
        </div>
      )}

      {tab === 'objects' && buckets.length > 0 && (
        <div className="of-quicksearch__filterGroup">
          <span className="of-quicksearch__filterLabel">Object type</span>
          {buckets.slice(0, 6).map((b) => (
            <label key={b.key} className="of-quicksearch__filterRow">
              <input
                type="checkbox"
                checked={objectsTypeFilter.has(b.key)}
                onChange={() => toggleSet(objectsTypeFilter, b.key, setObjectsTypeFilter)}
              />
              <span className="of-quicksearch__filterIcon">
                <Glyph name={b.icon} size={13} />
              </span>
              <span className="of-quicksearch__filterText">{b.label}</span>
              <span className="of-quicksearch__filterCount">{formatCount(b.count)}</span>
              <span className="of-quicksearch__filterBar">
                <span
                  className="of-quicksearch__filterBarFill"
                  style={{ width: `${Math.round((b.count / maxCount) * 100)}%` }}
                />
              </span>
            </label>
          ))}
          {buckets.length > 6 && (
            <button type="button" className="of-quicksearch__filterMore">Show more</button>
          )}
        </div>
      )}

      {tab !== 'objects' && (
        <>
          <div className="of-quicksearch__filterGroup">
            <span className="of-quicksearch__filterLabel">Created by</span>
            <div className="of-quicksearch__filterFieldRow">
              <select
                className="of-quicksearch__filterField"
                value={createdBy}
                onChange={(e) => setCreatedBy(e.target.value)}
              >
                <option value="">Select a user…</option>
                <option value="me">Me</option>
              </select>
              {createdBy && (
                <button
                  type="button"
                  className="of-quicksearch__close"
                  onClick={() => setCreatedBy('')}
                  aria-label="Clear creator"
                >
                  <Glyph name="x" size={13} />
                </button>
              )}
            </div>
          </div>

          <div className="of-quicksearch__filterGroup">
            <span className="of-quicksearch__filterLabel">Tags</span>
            <input
              type="text"
              className="of-quicksearch__filterField"
              placeholder="🏷  Click to search for tags…"
              value={tagFilter}
              onChange={(e) => setTagFilter(e.target.value)}
            />
          </div>

          <div className="of-quicksearch__filterGroup">
            <span className="of-quicksearch__filterLabel">Markings</span>
            <input
              type="text"
              className="of-quicksearch__filterField"
              placeholder="Enter marking RID..."
              value={markingFilter}
              onChange={(e) => setMarkingFilter(e.target.value)}
            />
          </div>

          <div className="of-quicksearch__filterGroup">
            <span className="of-quicksearch__filterLabel">In paths</span>
            <input
              type="text"
              className="of-quicksearch__filterField"
              placeholder="Click to enter path…"
              value={pathFilter}
              onChange={(e) => setPathFilter(e.target.value)}
            />
          </div>

          <div className="of-quicksearch__filterGroup">
            <span className="of-quicksearch__filterLabel">In projects</span>
            <input
              type="text"
              className="of-quicksearch__filterField"
              placeholder="Project RID or UUID..."
              value={projectFilter}
              onChange={(e) => setProjectFilter(e.target.value)}
            />
          </div>

          <label className="of-quicksearch__filterRow" style={{ marginTop: 16 }}>
            <input
              type="checkbox"
              checked={includeTrash}
              onChange={(e) => setIncludeTrash(e.target.checked)}
            />
            <span className="of-quicksearch__filterIcon">
              <Glyph name="trash" size={13} />
            </span>
            <span className="of-quicksearch__filterText">In trash</span>
          </label>
        </>
      )}
    </div>
  );
}

// ──────────────────────────────────────────────────────────────────
// Reusable bits
// ──────────────────────────────────────────────────────────────────

function LoadingSkeleton() {
  return (
    <div>
      <div className="of-quicksearch__quadGrid">
        {[0, 1, 2, 3].map((i) => (
          <section key={i} className="of-quicksearch__quad">
            <header className="of-quicksearch__quadHeader">Loading…</header>
            <div className="of-quicksearch__quadBody" style={{ padding: 16 }}>
              <div className="of-quicksearch__skeleton" style={{ width: '60%' }} />
              <div className="of-quicksearch__skeleton" style={{ width: '90%' }} />
              <div className="of-quicksearch__skeleton" style={{ width: '70%' }} />
              <div className="of-quicksearch__skeleton" style={{ width: '85%' }} />
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}

function EmptyState({ label }: { label: string }) {
  return <div className="of-quicksearch__emptyBlock">{label}</div>;
}
