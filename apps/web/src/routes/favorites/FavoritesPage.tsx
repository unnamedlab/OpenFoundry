import { type FormEvent, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';

import {
  createFavoriteGroup,
  deleteFavorite,
  listFavoritesWithGroups,
  resolveResourceLabels,
  updateFavoriteGroupsOrder,
  updateFavoriteOrder,
  type FavoriteGroup,
  type ResourceKind,
  type UserFavorite,
} from '@/lib/api/workspace';
import { workspaceResourceStablePath } from '@/lib/compass/stableResourceUrls';
import { Glyph, type GlyphName } from '@/lib/components/ui/Glyph';

const UNGROUPED_ID = '__ungrouped__';

interface FavoriteView extends UserFavorite {
  label: string;
}

interface FavoriteSection {
  id: string;
  name: string;
  displayOrder: number;
  favorites: FavoriteView[];
}

function resourceKindLabel(kind: ResourceKind): string {
  return kind.replace(/^ontology_/, '').replace(/_/g, ' ');
}

function glyphForResource(kind: ResourceKind): GlyphName {
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

function favoriteKey(entry: Pick<UserFavorite, 'resource_kind' | 'resource_id'>) {
  return `${entry.resource_kind}:${entry.resource_id}`;
}

function shortID(value: string) {
  if (value.length <= 18) return value;
  return `${value.slice(0, 8)}...${value.slice(-6)}`;
}

function byFavoriteOrder(a: UserFavorite, b: UserFavorite) {
  if (a.display_order !== b.display_order) return a.display_order - b.display_order;
  return Date.parse(a.created_at) - Date.parse(b.created_at);
}

function withDisplayOrder(items: UserFavorite[], groupID: string | null) {
  return items.map((entry, index) => ({
    resource_kind: entry.resource_kind,
    resource_id: entry.resource_id,
    group_id: groupID,
    display_order: (index + 1) * 1000,
  }));
}

export function FavoritesPage() {
  const [favorites, setFavorites] = useState<UserFavorite[]>([]);
  const [groups, setGroups] = useState<FavoriteGroup[]>([]);
  const [labels, setLabels] = useState<Map<string, string>>(new Map());
  const [newGroupName, setNewGroupName] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState('');
  const [error, setError] = useState('');

  async function loadFavorites() {
    setError('');
    const response = await listFavoritesWithGroups({ limit: 1000 });
    setFavorites(response.data);
    setGroups(response.groups ?? []);
    const items = response.data.map((entry) => ({
      resource_kind: entry.resource_kind,
      resource_id: entry.resource_id,
    }));
    if (items.length === 0) {
      setLabels(new Map());
      return;
    }
    const resolved = await resolveResourceLabels(items).catch(() => null);
    const nextLabels = new Map<string, string>();
    for (const entry of resolved?.data ?? []) {
      if (entry.label) nextLabels.set(favoriteKey(entry), entry.label);
    }
    setLabels(nextLabels);
  }

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    loadFavorites()
      .catch((cause: unknown) => {
        if (!cancelled) setError(cause instanceof Error ? cause.message : 'Failed to load favorites');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const sections = useMemo<FavoriteSection[]>(() => {
    const groupsByID = new Map(groups.map((group) => [group.id, group]));
    const labelFavorites = (entries: UserFavorite[]): FavoriteView[] =>
      entries
        .slice()
        .sort(byFavoriteOrder)
        .map((entry) => ({
          ...entry,
          label: labels.get(favoriteKey(entry)) ?? shortID(entry.resource_id),
        }));

    const ungrouped = labelFavorites(favorites.filter((entry) => !entry.group_id || !groupsByID.has(entry.group_id)));
    const grouped = groups
      .slice()
      .sort((a, b) => a.display_order - b.display_order || a.name.localeCompare(b.name))
      .map((group) => ({
        id: group.id,
        name: group.name,
        displayOrder: group.display_order,
        favorites: labelFavorites(favorites.filter((entry) => entry.group_id === group.id)),
      }));
    return [
      { id: UNGROUPED_ID, name: 'Ungrouped', displayOrder: -1, favorites: ungrouped },
      ...grouped,
    ];
  }, [favorites, groups, labels]);

  async function createGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = newGroupName.trim();
    if (!name) return;
    setSaving('create-group');
    setError('');
    try {
      await createFavoriteGroup({ name });
      setNewGroupName('');
      await loadFavorites();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to create group');
    } finally {
      setSaving('');
    }
  }

  async function moveFavorite(section: FavoriteSection, entry: UserFavorite, direction: -1 | 1) {
    const index = section.favorites.findIndex((item) => favoriteKey(item) === favoriteKey(entry));
    const target = index + direction;
    if (index < 0 || target < 0 || target >= section.favorites.length) return;
    const next = section.favorites.slice();
    [next[index], next[target]] = [next[target], next[index]];
    await persistFavoriteOrder(next, section.id === UNGROUPED_ID ? null : section.id);
  }

  async function moveToGroup(entry: UserFavorite, groupID: string) {
    const nextGroupID = groupID === UNGROUPED_ID ? null : groupID;
    const siblings = favorites
      .filter((item) => item.group_id === nextGroupID && favoriteKey(item) !== favoriteKey(entry))
      .sort(byFavoriteOrder);
    await persistFavoriteOrder([...siblings, { ...entry, group_id: nextGroupID }], nextGroupID);
  }

  async function persistFavoriteOrder(entries: UserFavorite[], groupID: string | null) {
    setSaving('favorites');
    setError('');
    try {
      await updateFavoriteOrder(withDisplayOrder(entries, groupID));
      await loadFavorites();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to update favorite order');
    } finally {
      setSaving('');
    }
  }

  async function moveGroup(group: FavoriteGroup, direction: -1 | 1) {
    const ordered = groups.slice().sort((a, b) => a.display_order - b.display_order || a.name.localeCompare(b.name));
    const index = ordered.findIndex((item) => item.id === group.id);
    const target = index + direction;
    if (index < 0 || target < 0 || target >= ordered.length) return;
    [ordered[index], ordered[target]] = [ordered[target], ordered[index]];
    setSaving('groups');
    setError('');
    try {
      await updateFavoriteGroupsOrder(ordered.map((item, itemIndex) => ({ id: item.id, display_order: (itemIndex + 1) * 1000 })));
      await loadFavorites();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to update group order');
    } finally {
      setSaving('');
    }
  }

  async function removeFavorite(entry: UserFavorite) {
    setSaving('favorites');
    setError('');
    try {
      await deleteFavorite(entry.resource_kind, entry.resource_id);
      await loadFavorites();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to remove favorite');
    } finally {
      setSaving('');
    }
  }

  const totalFavorites = favorites.length;

  return (
    <section style={{ padding: '24px 28px', display: 'grid', gap: 16 }}>
      <header style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
        <div style={{ display: 'grid', gap: 6 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <Glyph name="star" size={18} />
            <h1 style={{ margin: 0, fontSize: 20, fontWeight: 600 }}>Favorites</h1>
          </div>
          <p className="of-text-muted" style={{ margin: 0, maxWidth: 760 }}>
            Personal resource shortcuts are stored in your profile, so their groups and ordering follow you across devices.
          </p>
        </div>
        <form onSubmit={createGroup} style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <input
            className="of-input"
            value={newGroupName}
            onChange={(event) => setNewGroupName(event.target.value)}
            placeholder="New group"
            aria-label="New favorite group"
            style={{ minWidth: 220 }}
          />
          <button type="submit" className="of-button" disabled={!newGroupName.trim() || saving === 'create-group'}>
            <Glyph name="plus" size={14} />
            {saving === 'create-group' ? 'Creating...' : 'Group'}
          </button>
        </form>
      </header>

      {error ? (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 6, fontSize: 13 }}>
          {error}
        </div>
      ) : null}

      <div className="of-panel" style={{ padding: 0, overflow: 'hidden' }}>
        {loading ? (
          <p className="of-text-muted" style={{ padding: 28, textAlign: 'center', margin: 0 }}>Loading favorites...</p>
        ) : totalFavorites === 0 ? (
          <p className="of-text-muted" style={{ padding: 28, textAlign: 'center', margin: 0 }}>
            No favorites yet. Star resources from search or resource pages to keep them close.
          </p>
        ) : (
          <div style={{ display: 'grid' }}>
            {sections.map((section, sectionIndex) => {
              if (section.id === UNGROUPED_ID && section.favorites.length === 0) return null;
              const group = groups.find((item) => item.id === section.id);
              return (
                <section key={section.id} style={{ borderTop: sectionIndex === 0 ? 0 : '1px solid var(--border-subtle)' }}>
                  <header style={{ minHeight: 48, padding: '12px 16px', display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, background: '#f8fafc' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                      <Glyph name={section.id === UNGROUPED_ID ? 'star' : 'folder'} size={15} />
                      <strong style={{ fontSize: 13 }}>{section.name}</strong>
                      <span className="of-text-muted" style={{ fontSize: 12 }}>{section.favorites.length}</span>
                    </div>
                    {group ? (
                      <div style={{ display: 'flex', gap: 4 }}>
                        <button type="button" className="of-icon-button" title="Move group up" onClick={() => moveGroup(group, -1)} disabled={saving === 'groups'}>
                          <Glyph name="chevron-up" size={14} />
                        </button>
                        <button type="button" className="of-icon-button" title="Move group down" onClick={() => moveGroup(group, 1)} disabled={saving === 'groups'}>
                          <Glyph name="chevron-down" size={14} />
                        </button>
                      </div>
                    ) : null}
                  </header>
                  {section.favorites.length === 0 ? (
                    <p className="of-text-muted" style={{ margin: 0, padding: '14px 18px' }}>No favorites in this group.</p>
                  ) : (
                    <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid' }}>
                      {section.favorites.map((entry, index) => (
                        <li
                          key={favoriteKey(entry)}
                          style={{ minHeight: 58, padding: '10px 16px', display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) auto', gap: 12, alignItems: 'center', borderTop: index === 0 ? 0 : '1px solid var(--border-subtle)' }}
                        >
                          <Link
                            to={workspaceResourceStablePath(entry.resource_kind, entry.resource_id, entry.label)}
                            style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0, textDecoration: 'none', color: 'inherit' }}
                          >
                            <span style={{ width: 28, height: 28, display: 'inline-grid', placeItems: 'center', border: '1px solid var(--border-subtle)', borderRadius: 6, background: '#fff' }}>
                              <Glyph name={glyphForResource(entry.resource_kind)} size={14} />
                            </span>
                            <span style={{ display: 'grid', gap: 2, minWidth: 0 }}>
                              <span style={{ fontSize: 13, fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{entry.label}</span>
                              <span className="of-text-muted" style={{ fontSize: 12 }}>{resourceKindLabel(entry.resource_kind)} · {shortID(entry.resource_id)}</span>
                            </span>
                          </Link>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                            <select
                              className="of-input"
                              value={entry.group_id ?? UNGROUPED_ID}
                              onChange={(event) => moveToGroup(entry, event.target.value)}
                              aria-label={`Group for ${entry.label}`}
                              style={{ width: 160, height: 32, fontSize: 12 }}
                            >
                              <option value={UNGROUPED_ID}>Ungrouped</option>
                              {groups.map((item) => (
                                <option key={item.id} value={item.id}>{item.name}</option>
                              ))}
                            </select>
                            <button type="button" className="of-icon-button" title="Move up" onClick={() => moveFavorite(section, entry, -1)} disabled={index === 0 || saving === 'favorites'}>
                              <Glyph name="chevron-up" size={14} />
                            </button>
                            <button type="button" className="of-icon-button" title="Move down" onClick={() => moveFavorite(section, entry, 1)} disabled={index === section.favorites.length - 1 || saving === 'favorites'}>
                              <Glyph name="chevron-down" size={14} />
                            </button>
                            <button type="button" className="of-icon-button" title="Remove favorite" onClick={() => removeFavorite(entry)} disabled={saving === 'favorites'}>
                              <Glyph name="trash" size={14} />
                            </button>
                          </div>
                        </li>
                      ))}
                    </ul>
                  )}
                </section>
              );
            })}
          </div>
        )}
      </div>
    </section>
  );
}
