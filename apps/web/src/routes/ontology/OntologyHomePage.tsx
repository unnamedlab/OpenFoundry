import { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';

import { ObjectExplorer } from '@/lib/components/ontology/ObjectExplorer';
import { OntologySearch } from '@/lib/components/ontology/OntologySearch';
import { Glyph, type GlyphName } from '@/lib/components/ui/Glyph';
import {
  listObjects,
  listObjectTypes,
  searchOntology,
  type ObjectType,
  type SearchResult,
} from '@/lib/api/ontology';

type Tab = 'overview' | 'objects' | 'object-types' | 'artifacts';
type ViewMode = 'list' | 'graph';
type FilterId = 'all' | 'explorations' | string;
type ScopeKind = 'all' | 'object_type' | 'object_instance' | 'action_type' | 'link_type' | 'shared_property_type';

const SEARCH_SCOPES: { value: ScopeKind; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'object_type', label: 'Object types' },
  { value: 'object_instance', label: 'Objects' },
  { value: 'action_type', label: 'Actions' },
  { value: 'link_type', label: 'Links' },
  { value: 'shared_property_type', label: 'Shared properties' },
];

interface SavedItem {
  id: string;
  name: string;
  kind: 'list' | 'exploration';
}

function compactNumber(value: number) {
  if (!Number.isFinite(value)) return '0';
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2).replace(/\.?0+$/, '')}b`;
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2).replace(/\.?0+$/, '')}m`;
  if (value >= 10_000) return `${(value / 1_000).toFixed(2).replace(/\.?0+$/, '')}k`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(2).replace(/\.?0+$/, '')}k`;
  return value.toLocaleString();
}

function typeLabel(type: ObjectType) {
  return type.display_name || type.name || type.id;
}

function deriveGroup(type: ObjectType): string {
  const explicitGroup = type.group_names?.find((group) => group.trim());
  if (explicitGroup) return explicitGroup;
  const display = typeLabel(type).trim();
  const bracket = /^\[([^\]]+)\]/.exec(display);
  if (bracket) return bracket[1].trim();
  if (type.owner_id) return `Owner ${type.owner_id.slice(0, 6)}`;
  return 'Ungrouped';
}

function stripGroupPrefix(label: string): string {
  return label.replace(/^\[[^\]]+\]\s*/, '').trim();
}

function iconChar(type: ObjectType) {
  const icon = (type.icon ?? '').trim();
  if (icon) return icon.slice(0, 2);
  const stripped = stripGroupPrefix(typeLabel(type));
  return stripped.slice(0, 1).toUpperCase() || '·';
}

function resultIcon(kind: string): GlyphName {
  switch (kind) {
    case 'object_type':
      return 'cube';
    case 'object_instance':
    case 'object':
      return 'object';
    case 'action_type':
    case 'action':
      return 'run';
    case 'link_type':
    case 'link':
      return 'link';
    case 'shared_property_type':
      return 'tag';
    case 'interface':
      return 'ontology';
    default:
      return 'search';
  }
}

interface TypeGroup {
  id: string;
  label: string;
  types: ObjectType[];
}

export function OntologyHomePage() {
  const navigate = useNavigate();

  const [tab, setTab] = useState<Tab>('overview');
  const [view, setView] = useState<ViewMode>('list');
  const [activeFilter, setActiveFilter] = useState<FilterId>('all');
  const [groupPage, setGroupPage] = useState(0);

  const [scope, setScope] = useState<ScopeKind>('all');
  const [scopeOpen, setScopeOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchResults, setSearchResults] = useState<SearchResult[]>([]);
  const [searchLoading, setSearchLoading] = useState(false);
  const [searchError, setSearchError] = useState('');
  const [advancedSearchOpen, setAdvancedSearchOpen] = useState(false);

  const [types, setTypes] = useState<ObjectType[]>([]);
  const [counts, setCounts] = useState<Record<string, number>>({});
  const [favorites, setFavorites] = useState<Set<string>>(() => new Set());
  const [savedItems] = useState<SavedItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');

  const searchBoxRef = useRef<HTMLDivElement | null>(null);
  const scopeBoxRef = useRef<HTMLDivElement | null>(null);
  const searchDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Load object types once on mount.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoading(true);
      setLoadError('');
      try {
        const res = await listObjectTypes({ per_page: 200 });
        if (cancelled) return;
        setTypes(res.data);
      } catch (cause) {
        if (cancelled) return;
        setLoadError(cause instanceof Error ? cause.message : 'Failed to load ontology');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Lazily fetch object counts in small parallel batches once the type list loads.
  useEffect(() => {
    if (types.length === 0) return;
    let cancelled = false;
    const queue = types.map((type) => type.id).filter((id) => !(id in counts));
    if (queue.length === 0) return;

    async function pump(pending: string[]) {
      const next: Record<string, number> = {};
      const concurrency = 6;
      for (let cursor = 0; cursor < pending.length; cursor += concurrency) {
        const slice = pending.slice(cursor, cursor + concurrency);
        const settled = await Promise.allSettled(
          slice.map((id) => listObjects(id, { per_page: 1 }).then((r) => [id, r.total ?? 0] as const)),
        );
        if (cancelled) return;
        for (const item of settled) {
          if (item.status === 'fulfilled') {
            const [id, total] = item.value;
            next[id] = total;
          }
        }
      }
      if (!cancelled && Object.keys(next).length > 0) {
        setCounts((prev) => ({ ...prev, ...next }));
      }
    }
    void pump(queue);
    return () => {
      cancelled = true;
    };
    // We intentionally only re-run when the set of type ids changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [types]);

  // Outside-click handlers for search and scope popovers.
  useEffect(() => {
    function onDown(event: MouseEvent) {
      const target = event.target as Node | null;
      if (searchBoxRef.current && target && !searchBoxRef.current.contains(target)) {
        setSearchOpen(false);
      }
      if (scopeBoxRef.current && target && !scopeBoxRef.current.contains(target)) {
        setScopeOpen(false);
      }
    }
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, []);

  // Debounced search.
  useEffect(() => {
    if (searchDebounceRef.current) {
      clearTimeout(searchDebounceRef.current);
      searchDebounceRef.current = null;
    }
    const trimmed = query.trim();
    if (!trimmed) {
      setSearchResults([]);
      setSearchError('');
      setSearchLoading(false);
      return;
    }
    setSearchOpen(true);
    setSearchLoading(true);
    setSearchError('');
    searchDebounceRef.current = setTimeout(async () => {
      try {
        const res = await searchOntology({
          query: trimmed,
          kind: scope === 'all' ? undefined : scope,
          limit: 30,
        });
        setSearchResults(res.data);
      } catch (cause) {
        setSearchError(cause instanceof Error ? cause.message : 'Search failed');
      } finally {
        setSearchLoading(false);
      }
    }, 220);
    return () => {
      if (searchDebounceRef.current) clearTimeout(searchDebounceRef.current);
    };
  }, [query, scope]);

  const filteredTypes = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return types;
    return types.filter((type) =>
      [
        type.display_name,
        type.name,
        type.description,
        type.primary_key_property,
        ...(type.group_names || []),
      ]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(needle)),
    );
  }, [types, query]);

  const groups = useMemo<TypeGroup[]>(() => {
    const map = new Map<string, ObjectType[]>();
    for (const type of filteredTypes) {
      const key = deriveGroup(type);
      const list = map.get(key) ?? [];
      list.push(type);
      map.set(key, list);
    }
    const entries = Array.from(map.entries())
      .map(([label, list]) => ({
        id: label,
        label,
        types: list.sort((a, b) => typeLabel(a).localeCompare(typeLabel(b))),
      }))
      .sort((a, b) => a.label.localeCompare(b.label));
    return entries;
  }, [filteredTypes]);

  const visibleGroups = useMemo(() => {
    if (activeFilter === 'all') return groups;
    if (activeFilter === 'explorations') return [];
    return groups.filter((group) => group.id === activeFilter);
  }, [groups, activeFilter]);

  const favoriteTypes = useMemo(
    () => filteredTypes.filter((type) => favorites.has(type.id)),
    [filteredTypes, favorites],
  );

  const selectedGroup = activeFilter !== 'all' && activeFilter !== 'explorations' ? activeFilter : null;

  const sidebarGroups = useMemo(() => {
    const items = groups.map((group) => ({ id: group.id, label: group.label, count: group.types.length }));
    const start = groupPage * 10;
    const end = start + 10;
    return {
      slice: items.slice(start, end),
      hasPrev: start > 0,
      hasNext: end < items.length,
      total: items.length,
    };
  }, [groups, groupPage]);

  function toggleFavorite(typeId: string) {
    setFavorites((prev) => {
      const next = new Set(prev);
      if (next.has(typeId)) next.delete(typeId);
      else next.add(typeId);
      return next;
    });
  }

  const summaryStats = useMemo(
    () => [
      { label: 'Object types', value: filteredTypes.length },
      { label: 'Groups', value: groups.length },
      { label: 'Favorites', value: favorites.size },
    ],
    [filteredTypes.length, groups.length, favorites.size],
  );

  return (
    <section className="of-page" style={{ padding: 18, display: 'grid', gap: 14 }}>
      <header style={{ display: 'flex', flexWrap: 'wrap', gap: 12, justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <div
          style={{
            display: 'flex',
            gap: 0,
            background: 'var(--bg-panel)',
            border: '1px solid var(--border-default)',
            borderRadius: 'var(--radius-md)',
            overflow: 'hidden',
          }}
        >
          <button
            type="button"
            onClick={() => navigate('/object-explorer')}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              padding: '6px 12px',
              background: 'var(--bg-panel)',
              border: 0,
              borderRight: '1px solid var(--border-subtle)',
              color: 'var(--text-strong)',
              fontSize: 12,
              fontWeight: 600,
            }}
          >
            <Glyph name="search" size={13} />
            New exploration
          </button>
          <button
            type="button"
            onClick={() => navigate('/object-explorer')}
            title="Open another exploration"
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              padding: '6px 10px',
              background: 'var(--bg-panel)',
              border: 0,
              color: 'var(--text-muted)',
            }}
          >
            <Glyph name="plus" size={13} />
          </button>
        </div>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
          <button
            type="button"
            className="of-button"
            onClick={() => navigate('/object-explorer')}
            style={{ background: 'var(--bg-chip-active)', color: 'var(--status-info)', borderColor: 'var(--border-default)' }}
          >
            <Glyph name="search" size={14} />
            Explorations
            <Glyph name="chevron-down" size={13} />
          </button>
          <button
            type="button"
            className="of-button"
            onClick={() => navigate('/ontology/object-sets')}
            style={{ borderColor: 'var(--border-default)', background: 'var(--bg-panel)' }}
          >
            <Glyph name="list" size={14} />
            Lists
            <Glyph name="chevron-down" size={13} />
          </button>
        </div>
      </header>

      <div style={{ maxWidth: 1280 }}>
        <h1 className="of-heading-xl" style={{ margin: 0, fontSize: 28 }}>
          Explore your data
        </h1>
        <p className="of-text-muted" style={{ margin: '6px 0 0', fontSize: 13 }}>
          Select an object type from the list below to{' '}
          <button
            type="button"
            className="of-button of-button--ghost"
            onClick={() => navigate('/object-explorer')}
            style={{ minHeight: 0, padding: 0, color: 'var(--text-link)', fontWeight: 600, textDecoration: 'underline', textDecorationStyle: 'dotted' }}
          >
            <Glyph name="graph" size={13} />
            explore
          </button>{' '}
          or view{' '}
          <button
            type="button"
            className="of-button of-button--ghost"
            onClick={() => navigate('/ontology/object-sets')}
            style={{ minHeight: 0, padding: 0, color: 'var(--text-link)', fontWeight: 600, textDecoration: 'underline', textDecorationStyle: 'dotted' }}
          >
            <Glyph name="list" size={13} />
            results
          </button>
          .
        </p>
      </div>

      <div ref={searchBoxRef} style={{ position: 'relative' }}>
        <div
          className="of-panel"
          style={{
            display: 'flex',
            alignItems: 'center',
            padding: 0,
            border: '1px solid var(--border-default)',
            borderRadius: 'var(--radius-md)',
            background: 'var(--bg-panel)',
            minHeight: 44,
          }}
        >
          <div ref={scopeBoxRef} style={{ position: 'relative', display: 'flex', alignItems: 'center', borderRight: '1px solid var(--border-subtle)' }}>
            <button
              type="button"
              onClick={() => setScopeOpen((open) => !open)}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 6,
                padding: '0 14px',
                height: 42,
                background: 'transparent',
                border: 0,
                color: 'var(--text-strong)',
                fontWeight: 600,
                fontSize: 13,
              }}
            >
              {SEARCH_SCOPES.find((option) => option.value === scope)?.label ?? 'All'}
              <Glyph name="chevron-down" size={13} />
            </button>
            {scopeOpen ? (
              <div
                role="menu"
                style={{
                  position: 'absolute',
                  top: '100%',
                  left: 0,
                  marginTop: 4,
                  minWidth: 200,
                  background: 'var(--bg-panel)',
                  border: '1px solid var(--border-default)',
                  borderRadius: 'var(--radius-md)',
                  boxShadow: 'var(--shadow-popover)',
                  zIndex: 30,
                  overflow: 'hidden',
                }}
              >
                {SEARCH_SCOPES.map((option) => (
                  <button
                    key={option.value}
                    type="button"
                    onClick={() => {
                      setScope(option.value);
                      setScopeOpen(false);
                    }}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 8,
                      width: '100%',
                      padding: '8px 12px',
                      background: option.value === scope ? 'var(--bg-chip-active)' : 'transparent',
                      color: option.value === scope ? 'var(--status-info)' : 'var(--text-default)',
                      border: 0,
                      fontSize: 12,
                      textAlign: 'left',
                      fontWeight: option.value === scope ? 700 : 500,
                    }}
                  >
                    {option.value === scope ? <Glyph name="check" size={12} /> : <span style={{ width: 12 }} />}
                    {option.label}
                  </button>
                ))}
              </div>
            ) : null}
          </div>

          <Glyph name="search" size={15} tone="var(--text-muted)" />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onFocus={() => {
              if (query.trim()) setSearchOpen(true);
            }}
            placeholder="Search object types and properties…"
            style={{
              flex: 1,
              border: 0,
              outline: 'none',
              background: 'transparent',
              padding: '0 10px',
              height: 42,
              fontSize: 14,
              color: 'var(--text-strong)',
            }}
          />
          {query ? (
            <button
              type="button"
              onClick={() => {
                setQuery('');
                setSearchResults([]);
                setSearchOpen(false);
              }}
              style={{ padding: '0 10px', background: 'transparent', border: 0, color: 'var(--text-muted)', fontWeight: 600 }}
            >
              Clear
            </button>
          ) : null}
          <button
            type="button"
            onClick={() => setAdvancedSearchOpen(true)}
            title="Open advanced search"
            style={{ padding: '0 12px', background: 'transparent', border: 0, color: 'var(--text-muted)' }}
          >
            <Glyph name="help" size={15} />
          </button>
        </div>

        {searchOpen && query.trim() ? (
          <div
            role="listbox"
            style={{
              position: 'absolute',
              top: '100%',
              left: 0,
              right: 0,
              marginTop: 4,
              background: 'var(--bg-panel)',
              border: '1px solid var(--border-default)',
              borderRadius: 'var(--radius-md)',
              boxShadow: 'var(--shadow-popover)',
              zIndex: 25,
              maxHeight: 460,
              overflow: 'auto',
            }}
            className="of-scrollbar"
          >
            <div
              style={{
                padding: '10px 14px',
                background: 'var(--bg-panel-muted)',
                borderBottom: '1px solid var(--border-subtle)',
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                fontSize: 13,
                color: 'var(--text-default)',
              }}
            >
              <Glyph name="search" size={14} tone="var(--status-info)" />
              <span>
                Search for <strong>“{query.trim()}”</strong>
              </span>
              {searchLoading ? <span className="of-text-soft" style={{ marginLeft: 'auto', fontSize: 11 }}>Searching…</span> : null}
            </div>

            {searchError ? (
              <div className="of-status-danger" style={{ margin: 10, padding: '8px 12px', borderRadius: 'var(--radius-md)', fontSize: 12 }}>
                {searchError}
              </div>
            ) : null}

            {!searchLoading && !searchError && searchResults.length === 0 ? (
              <div style={{ padding: 18, textAlign: 'center', color: 'var(--text-muted)', fontSize: 12 }}>No results.</div>
            ) : null}

            {searchResults.map((result) => {
              const isExperimental = (result.metadata as Record<string, unknown> | undefined)?.['experimental'] === true;
              const matchedType = result.object_type_id
                ? types.find((type) => type.id === result.object_type_id) ?? null
                : null;
              return (
                <button
                  key={`${result.kind}-${result.id}`}
                  type="button"
                  onClick={() => {
                    navigate(result.route);
                    setSearchOpen(false);
                  }}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 10,
                    width: '100%',
                    padding: '8px 14px',
                    background: 'transparent',
                    border: 0,
                    textAlign: 'left',
                    fontSize: 13,
                    color: 'var(--text-default)',
                  }}
                >
                  <span
                    aria-hidden="true"
                    style={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      width: 24,
                      height: 24,
                      borderRadius: 'var(--radius-sm)',
                      background: matchedType?.color || '#2d72d2',
                      color: '#fff',
                      fontWeight: 700,
                      fontSize: 11,
                      flexShrink: 0,
                    }}
                  >
                    <Glyph name={resultIcon(result.kind)} size={13} tone="#ffffff" />
                  </span>
                  <span style={{ display: 'flex', flexDirection: 'column', minWidth: 0 }}>
                    <span style={{ fontWeight: 600, color: 'var(--text-strong)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                      {result.title || result.id}
                    </span>
                    {result.subtitle ? (
                      <span className="of-text-soft" style={{ fontSize: 11 }}>
                        {result.subtitle}
                      </span>
                    ) : null}
                  </span>
                  {isExperimental ? (
                    <span
                      className="of-chip"
                      style={{ marginLeft: 'auto', minHeight: 0, padding: '0 6px', background: '#fff3df', color: '#9a5b00', fontSize: 10 }}
                    >
                      Experimental
                    </span>
                  ) : (
                    <span className="of-chip" style={{ marginLeft: 'auto', minHeight: 0, padding: '0 6px', fontSize: 10 }}>
                      {result.kind.replace(/_/g, ' ')}
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        ) : null}
      </div>

      <div className="of-tabbar" style={{ marginTop: 4 }}>
        {(
          [
            ['overview', 'Overview'],
            ['objects', 'Objects'],
            ['object-types', 'Object types'],
            ['artifacts', 'Artifacts'],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            type="button"
            className={`of-tab ${tab === id ? 'of-tab-active' : ''}`}
            onClick={() => setTab(id)}
          >
            {label}
          </button>
        ))}
      </div>

      {loadError ? (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {loadError}
        </div>
      ) : null}

      {tab === 'overview' ? (
        <div style={{ display: 'grid', gridTemplateColumns: 'minmax(220px, 240px) minmax(0, 1fr)', gap: 14, alignItems: 'start' }}>
          <aside style={{ display: 'grid', gap: 4 }}>
            <SidebarItem
              label="All"
              active={activeFilter === 'all'}
              onClick={() => {
                setActiveFilter('all');
                setGroupPage(0);
              }}
            />
            <SidebarItem
              label="My explorations & lists"
              count={savedItems.length}
              active={activeFilter === 'explorations'}
              onClick={() => setActiveFilter('explorations')}
            />

            <p
              className="of-eyebrow"
              style={{ marginTop: 14, marginBottom: 4, paddingLeft: 8, fontSize: 11, color: 'var(--text-muted)' }}
            >
              Object type groups
            </p>

            {sidebarGroups.slice.map((group) => (
              <SidebarItem
                key={group.id}
                label={group.label}
                count={group.count}
                active={activeFilter === group.id}
                onClick={() => setActiveFilter(group.id)}
              />
            ))}

            {sidebarGroups.total === 0 && !loading ? (
              <p className="of-text-soft" style={{ padding: '6px 8px', fontSize: 11, margin: 0 }}>
                No groups yet.
              </p>
            ) : null}

            {sidebarGroups.total > 10 ? (
              <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 6, gap: 6 }}>
                <button
                  type="button"
                  className="of-button"
                  disabled={!sidebarGroups.hasPrev}
                  onClick={() => setGroupPage((value) => Math.max(0, value - 1))}
                  style={{ flex: 1 }}
                >
                  <Glyph name="chevron-left" size={12} />
                  Prev.
                </button>
                <button
                  type="button"
                  className="of-button"
                  disabled={!sidebarGroups.hasNext}
                  onClick={() => setGroupPage((value) => value + 1)}
                  style={{ flex: 1 }}
                >
                  Next
                  <Glyph name="chevron-right" size={12} />
                </button>
              </div>
            ) : null}
          </aside>

          <div style={{ display: 'grid', gap: 16 }}>
            {activeFilter === 'all' ? (
              <SectionHeader
                title="My explorations & lists"
                count={savedItems.length}
                hint="Saved object sets and explorations"
              />
            ) : null}

            {activeFilter === 'all' || activeFilter === 'explorations' ? (
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 10 }}>
                {savedItems.length === 0 ? (
                  <div
                    className="of-panel"
                    style={{
                      padding: 16,
                      borderStyle: 'dashed',
                      color: 'var(--text-muted)',
                      fontSize: 12,
                      textAlign: 'center',
                    }}
                  >
                    No saved explorations yet. Open an object type and pin it to see it here.
                  </div>
                ) : (
                  savedItems.map((item) => (
                    <button
                      key={item.id}
                      type="button"
                      className="of-card"
                      onClick={() => navigate(`/object-explorer?id=${item.id}`)}
                      style={{ textAlign: 'left', padding: 14 }}
                    >
                      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                        <span
                          aria-hidden="true"
                          style={{
                            display: 'inline-flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            width: 32,
                            height: 32,
                            borderRadius: 'var(--radius-sm)',
                            background: item.kind === 'list' ? '#1f5ea8' : '#7c3aed',
                            color: '#fff',
                          }}
                        >
                          <Glyph name={item.kind === 'list' ? 'list' : 'cube'} size={16} tone="#ffffff" />
                        </span>
                        <strong style={{ color: 'var(--text-strong)', fontSize: 14 }}>{item.name}</strong>
                      </div>
                    </button>
                  ))
                )}
              </div>
            ) : null}

            {activeFilter === 'all' ? <SectionDivider label="Object types" /> : null}

            {activeFilter !== 'explorations' ? (
              <>
                {favoriteTypes.length > 0 && activeFilter === 'all' ? (
                  <GroupBlock
                    label="Favorites"
                    types={favoriteTypes}
                    counts={counts}
                    favorites={favorites}
                    onToggleFavorite={toggleFavorite}
                    onOpen={(type) => navigate(`/ontology/${type.id}`)}
                    view={view}
                    onChangeView={setView}
                  />
                ) : null}

                {visibleGroups.length === 0 && !loading ? (
                  <div
                    className="of-panel"
                    style={{
                      padding: 18,
                      textAlign: 'center',
                      color: 'var(--text-muted)',
                      fontSize: 13,
                    }}
                  >
                    {selectedGroup
                      ? `No object types in ${selectedGroup}.`
                      : 'No object types match the current search.'}
                  </div>
                ) : null}

                {visibleGroups.map((group) => (
                  <GroupBlock
                    key={group.id}
                    label={group.label}
                    types={group.types}
                    counts={counts}
                    favorites={favorites}
                    onToggleFavorite={toggleFavorite}
                    onOpen={(type) => navigate(`/ontology/${type.id}`)}
                    view={view}
                    onChangeView={setView}
                  />
                ))}

                {loading ? (
                  <div
                    className="of-panel"
                    style={{ padding: 18, textAlign: 'center', color: 'var(--text-muted)', fontSize: 12 }}
                  >
                    Loading object types…
                  </div>
                ) : null}
              </>
            ) : null}
          </div>
        </div>
      ) : null}

      {tab === 'objects' ? (
        <div className="of-panel" style={{ padding: 14, display: 'grid', gap: 12 }}>
          <SectionHeader title="Objects" hint="Pick an object type from the overview to inspect its objects." />
          {filteredTypes.length === 0 ? (
            <p className="of-text-muted" style={{ margin: 0 }}>No object types loaded.</p>
          ) : (
            <ObjectExplorer
              key={filteredTypes[0].id}
              typeId={filteredTypes[0].id}
              pageSize={25}
              onSelect={(object) => navigate(`/ontology/${filteredTypes[0].id}#object-${object.id}`)}
            />
          )}
        </div>
      ) : null}

      {tab === 'object-types' ? (
        <section className="of-panel" style={{ overflow: 'hidden' }}>
          <div
            className="of-toolbar"
            style={{ borderRadius: 0, borderLeft: 0, borderRight: 0, borderTop: 0, padding: '10px 12px', justifyContent: 'space-between', flexWrap: 'wrap' }}
          >
            <div>
              <p className="of-heading-sm" style={{ margin: 0 }}>
                Object types
              </p>
              <p className="of-text-muted" style={{ margin: '2px 0 0', fontSize: 11 }}>
                {filteredTypes.length.toLocaleString()} of {types.length.toLocaleString()} loaded
              </p>
            </div>
            <Link to="/ontology/types" className="of-button of-button--primary">
              <Glyph name="plus" size={14} />
              New object type
            </Link>
          </div>

          <div className="of-scrollbar" style={{ overflowX: 'auto' }}>
            <table className="of-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Group</th>
                  <th>Primary key</th>
                  <th>Updated</th>
                  <th style={{ width: 110 }}>Objects</th>
                  <th style={{ width: 140 }}>Actions</th>
                </tr>
              </thead>
              <tbody>
                {!loading && filteredTypes.length === 0 ? (
                  <tr>
                    <td colSpan={6} style={{ padding: 20, textAlign: 'center' }}>
                      <span className="of-text-muted">No object types found.</span>
                    </td>
                  </tr>
                ) : null}

                {filteredTypes.map((type) => (
                  <tr key={type.id}>
                    <td>
                      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8 }}>
                        <TypeIcon type={type} size={26} />
                        <div>
                          <Link to={`/ontology/${type.id}`} className="of-link">
                            {typeLabel(type)}
                          </Link>
                          {type.description ? (
                            <div className="of-text-muted" style={{ marginTop: 2, fontSize: 11, maxWidth: 540 }}>
                              {type.description}
                            </div>
                          ) : null}
                        </div>
                      </div>
                    </td>
                    <td className="of-text-muted">{deriveGroup(type)}</td>
                    <td className="of-text-muted">{type.primary_key_property || '-'}</td>
                    <td className="of-text-muted">
                      {type.updated_at
                        ? new Intl.DateTimeFormat('en-US', { dateStyle: 'medium' }).format(new Date(type.updated_at))
                        : '-'}
                    </td>
                    <td className="of-text-muted">
                      {type.id in counts ? compactNumber(counts[type.id]) : '…'}
                    </td>
                    <td>
                      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                        <Link to={`/ontology/${type.id}`} className="of-button">
                          Open
                        </Link>
                        <button
                          type="button"
                          className="of-button of-button--ghost"
                          onClick={() => toggleFavorite(type.id)}
                          aria-pressed={favorites.has(type.id)}
                          title={favorites.has(type.id) ? 'Remove from favorites' : 'Add to favorites'}
                        >
                          <Glyph name="bookmark" size={14} tone={favorites.has(type.id) ? '#f59e0b' : undefined} />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      ) : null}

      {tab === 'artifacts' ? (
        <div
          className="of-panel"
          style={{ padding: 22, textAlign: 'center', color: 'var(--text-muted)', fontSize: 13 }}
        >
          Artifacts (saved searches, dashboards, time series) appear here once saved. Connect a saved query or
          object set to populate this tab.
        </div>
      ) : null}

      <footer
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 14,
          flexWrap: 'wrap',
          paddingTop: 8,
          borderTop: '1px solid var(--border-subtle)',
          color: 'var(--text-muted)',
          fontSize: 11,
        }}
      >
        {summaryStats.map((item) => (
          <span key={item.label} style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
            <span style={{ fontWeight: 700, color: 'var(--text-strong)' }}>{item.value.toLocaleString()}</span>
            {item.label}
          </span>
        ))}
        <span style={{ marginLeft: 'auto' }}>
          ONT-001 · /ontology
        </span>
      </footer>

      <OntologySearch open={advancedSearchOpen} onClose={() => setAdvancedSearchOpen(false)} initialQuery={query} />
    </section>
  );
}

interface SidebarItemProps {
  label: string;
  count?: number;
  active?: boolean;
  onClick: () => void;
}

function SidebarItem({ label, count, active, onClick }: SidebarItemProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        gap: 8,
        width: '100%',
        padding: '7px 10px',
        background: active ? 'var(--bg-chip-active)' : 'transparent',
        border: 0,
        borderRadius: 'var(--radius-sm)',
        color: active ? 'var(--status-info)' : 'var(--text-default)',
        fontSize: 12,
        fontWeight: active ? 700 : 500,
        textAlign: 'left',
      }}
    >
      <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{label}</span>
      {typeof count === 'number' ? (
        <span
          className="of-badge"
          style={{
            fontSize: 10,
            minWidth: 18,
            minHeight: 18,
            padding: '0 6px',
            background: active ? '#cfe0fb' : 'var(--bg-chip)',
            color: active ? 'var(--status-info)' : 'var(--text-muted)',
          }}
        >
          {count}
        </span>
      ) : null}
    </button>
  );
}

interface SectionHeaderProps {
  title: string;
  count?: number;
  hint?: string;
}

function SectionHeader({ title, count, hint }: SectionHeaderProps) {
  return (
    <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, paddingBottom: 6 }}>
      <p
        className="of-eyebrow"
        style={{ margin: 0, color: 'var(--text-muted)', fontSize: 11, letterSpacing: '0.06em' }}
      >
        {title.toUpperCase()}
      </p>
      {typeof count === 'number' ? (
        <span className="of-badge" style={{ background: 'var(--bg-chip)', color: 'var(--text-muted)' }}>
          {count}
        </span>
      ) : null}
      {hint ? <span className="of-text-soft" style={{ fontSize: 11 }}>{hint}</span> : null}
    </div>
  );
}

function SectionDivider({ label }: { label: string }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 10, color: 'var(--text-muted)' }}>
      <span style={{ flex: 1, height: 1, background: 'var(--border-subtle)' }} />
      <span style={{ fontSize: 11, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em' }}>
        {label}
      </span>
      <span style={{ flex: 1, height: 1, background: 'var(--border-subtle)' }} />
    </div>
  );
}

interface GroupBlockProps {
  label: string;
  types: ObjectType[];
  counts: Record<string, number>;
  favorites: Set<string>;
  onToggleFavorite: (typeId: string) => void;
  onOpen: (type: ObjectType) => void;
  view: ViewMode;
  onChangeView: (view: ViewMode) => void;
}

function GroupBlock({ label, types, counts, favorites, onToggleFavorite, onOpen, view, onChangeView }: GroupBlockProps) {
  return (
    <section style={{ display: 'grid', gap: 8 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <p
            className="of-eyebrow"
            style={{ margin: 0, color: 'var(--text-muted)', fontSize: 11, letterSpacing: '0.06em' }}
          >
            {label.toUpperCase()}
          </p>
          <span className="of-badge" style={{ background: 'var(--bg-chip)', color: 'var(--text-muted)' }}>
            {types.length}
          </span>
        </div>
        <ViewToggle view={view} onChange={onChangeView} />
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 10 }}>
        {types.map((type) => (
          <TypeCard
            key={type.id}
            type={type}
            count={counts[type.id]}
            favorited={favorites.has(type.id)}
            onToggleFavorite={() => onToggleFavorite(type.id)}
            onClick={() => onOpen(type)}
          />
        ))}
      </div>
    </section>
  );
}

interface TypeCardProps {
  type: ObjectType;
  count?: number;
  favorited: boolean;
  onClick: () => void;
  onToggleFavorite: () => void;
}

function TypeCard({ type, count, favorited, onClick, onToggleFavorite }: TypeCardProps) {
  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          onClick();
        }
      }}
      className="of-card"
      style={{ flexDirection: 'row', alignItems: 'center', gap: 10, padding: '10px 12px', cursor: 'pointer', minHeight: 56 }}
    >
      <TypeIcon type={type} size={32} />
      <div style={{ minWidth: 0, flex: 1 }}>
        <div
          style={{
            color: 'var(--text-strong)',
            fontWeight: 600,
            fontSize: 13,
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
          }}
          title={typeLabel(type)}
        >
          {typeLabel(type)}
        </div>
        {type.description ? (
          <div
            className="of-text-muted"
            style={{
              fontSize: 11,
              lineHeight: 1.35,
              display: '-webkit-box',
              WebkitLineClamp: 2,
              WebkitBoxOrient: 'vertical',
              overflow: 'hidden',
            }}
          >
            {type.description}
          </div>
        ) : null}
      </div>
      <span className="of-badge" style={{ background: 'var(--bg-chip)', color: 'var(--text-muted)' }}>
        {count === undefined ? '…' : compactNumber(count)}
      </span>
      <button
        type="button"
        onClick={(event) => {
          event.stopPropagation();
          onToggleFavorite();
        }}
        aria-pressed={favorited}
        title={favorited ? 'Remove from favorites' : 'Add to favorites'}
        style={{
          padding: 4,
          background: 'transparent',
          border: 0,
          color: favorited ? '#f59e0b' : 'var(--text-soft)',
        }}
      >
        <Glyph name="bookmark" size={14} tone={favorited ? '#f59e0b' : undefined} />
      </button>
    </div>
  );
}

function TypeIcon({ type, size = 32 }: { type: ObjectType; size?: number }) {
  return (
    <span
      aria-hidden="true"
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: size,
        height: size,
        flexShrink: 0,
        borderRadius: 'var(--radius-sm)',
        background: type.color || '#2d72d2',
        color: '#fff',
        fontSize: size <= 24 ? 11 : 13,
        fontWeight: 700,
      }}
    >
      {iconChar(type)}
    </span>
  );
}

function ViewToggle({ view, onChange }: { view: ViewMode; onChange: (view: ViewMode) => void }) {
  return (
    <div className="of-pill-toggle" style={{ display: 'inline-flex', gap: 0 }}>
      <button
        type="button"
        data-active={view === 'list'}
        onClick={() => onChange('list')}
        style={{ display: 'inline-flex', alignItems: 'center', gap: 4, padding: '4px 10px', fontSize: 12 }}
      >
        <Glyph name="list" size={13} />
        List
      </button>
      <button
        type="button"
        data-active={view === 'graph'}
        onClick={() => onChange('graph')}
        style={{ display: 'inline-flex', alignItems: 'center', gap: 4, padding: '4px 10px', fontSize: 12 }}
      >
        <Glyph name="graph" size={13} />
        Graph
      </button>
    </div>
  );
}
