import { describe, expect, it } from 'vitest';

import {
  capabilityLabel,
  filterCatalog,
  FALLBACK_CONNECTOR_CATALOG,
  type ConnectorCapability,
  type ConnectorCatalogEntry,
} from './data-connection';

describe('data-connection catalog helpers', () => {
  it('returns the full catalog when the query is empty or whitespace', () => {
    expect(filterCatalog(FALLBACK_CONNECTOR_CATALOG, '')).toHaveLength(
      FALLBACK_CONNECTOR_CATALOG.length,
    );
    expect(filterCatalog(FALLBACK_CONNECTOR_CATALOG, '   ')).toHaveLength(
      FALLBACK_CONNECTOR_CATALOG.length,
    );
  });

  it('matches by connector name case-insensitively', () => {
    const result = filterCatalog(FALLBACK_CONNECTOR_CATALOG, 'POST');
    expect(result.map((entry) => entry.type)).toEqual(['postgresql']);
  });

  it('matches by capability so a "virtual" search surfaces virtual-table connectors', () => {
    // Mirrors the Foundry docs example "search for virtual".
    const result = filterCatalog(FALLBACK_CONNECTOR_CATALOG, 'virtual');
    expect(result.map((entry) => entry.type)).toContain('snowflake');
    // S3 only has batch_sync / file_export / exploration so it must be excluded.
    expect(result.map((entry) => entry.type)).not.toContain('s3');
  });

  it('flags exactly the MVP connectors as available', () => {
    const available = FALLBACK_CONNECTOR_CATALOG.filter((entry) => entry.available).map(
      (entry) => entry.type,
    );
    expect(available.sort()).toEqual(['postgresql', 'rest_api', 's3']);
  });

  it('produces a human label for every capability tag in the catalog', () => {
    const seen = new Set<ConnectorCapability>();
    for (const entry of FALLBACK_CONNECTOR_CATALOG) {
      for (const cap of entry.capabilities) {
        seen.add(cap);
      }
    }
    for (const cap of seen) {
      const label = capabilityLabel(cap);
      expect(label.length).toBeGreaterThan(0);
      // Labels should be human-friendly (not raw snake_case identifiers).
      expect(label).not.toMatch(/_/);
    }
  });

  it('does not mutate the input catalog when filtering', () => {
    const snapshot: ConnectorCatalogEntry[] = JSON.parse(
      JSON.stringify(FALLBACK_CONNECTOR_CATALOG),
    );
    filterCatalog(FALLBACK_CONNECTOR_CATALOG, 'sync');
    expect(FALLBACK_CONNECTOR_CATALOG).toEqual(snapshot);
  });
});
