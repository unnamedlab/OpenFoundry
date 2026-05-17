import { describe, expect, it } from 'vitest';

import type { RetentionPolicy } from '@/lib/api/datasets';
import {
  classifyRetentionPolicy,
  filterRetentionPolicies,
  parseLegacyRetentionYaml,
  retentionScopeKind,
} from '@/lib/retention/policyClassification';

function policy(overrides: Partial<RetentionPolicy>): RetentionPolicy {
  return {
    id: overrides.id ?? 'policy-1',
    name: overrides.name ?? 'Default policy',
    scope: overrides.scope ?? 'namespace:analytics',
    target_kind: overrides.target_kind ?? 'dataset',
    retention_days: overrides.retention_days ?? 90,
    legal_hold: overrides.legal_hold ?? false,
    purge_mode: overrides.purge_mode ?? 'soft_delete',
    rules: overrides.rules ?? [],
    is_system: overrides.is_system ?? false,
    selector: overrides.selector ?? {},
    criteria: overrides.criteria ?? {},
    grace_period_minutes: overrides.grace_period_minutes ?? 60,
    created_at: overrides.created_at ?? '2026-01-01T00:00:00Z',
    updated_at: overrides.updated_at ?? '2026-01-01T00:00:00Z',
    active: overrides.active ?? true,
  };
}

describe('retention policy classification', () => {
  it('separates recommended, legacy, and custom policy views', () => {
    expect(classifyRetentionPolicy(policy({ is_system: true }))).toBe('recommended');
    expect(classifyRetentionPolicy(policy({ scope: 'legacy:warehouse', rules: ['yaml-import'] }))).toBe('legacy');
    expect(classifyRetentionPolicy(policy({ name: 'Namespace custom' }))).toBe('custom');
  });

  it('filters by view, scope, target kind, active state, and text', () => {
    const policies = [
      policy({ id: 'system', is_system: true, scope: 'space:finance', name: 'Recommended finance' }),
      policy({ id: 'custom', scope: 'namespace:supply-chain', target_kind: 'transaction', name: 'Cold transactions' }),
      policy({ id: 'paused', scope: 'namespace:supply-chain', active: false, name: 'Paused dataset policy' }),
    ];

    expect(filterRetentionPolicies(policies, { view: 'recommended' }).map((item) => item.id)).toEqual(['system']);
    expect(filterRetentionPolicies(policies, { scope: 'supply', targetKind: 'transaction' }).map((item) => item.id)).toEqual(['custom']);
    expect(filterRetentionPolicies(policies, { active: 'paused', search: 'dataset' }).map((item) => item.id)).toEqual(['paused']);
  });

  it('labels scoped policies for space and namespace views', () => {
    expect(retentionScopeKind('space:marketing')).toBe('space');
    expect(retentionScopeKind('namespace:ops')).toBe('namespace');
    expect(retentionScopeKind('')).toBe('global');
  });
});

describe('legacy retention YAML migration parser', () => {
  it('extracts read-only migration candidates from simple YAML-style policies', () => {
    const result = parseLegacyRetentionYaml(`
policies:
  - name: Finance archive
    scope: space:finance
    target_kind: dataset
    retention_days: 365
    purge_mode: archive
    legal_hold: true
  - name: Aborted transaction cleanup
    scope: namespace:ops
    target_kind: transaction
    retention_days: 30
`);

    expect(result.warnings).toEqual([]);
    expect(result.policies).toHaveLength(2);
    expect(result.policies[0]).toMatchObject({
      name: 'Finance archive',
      scope: 'space:finance',
      targetKind: 'dataset',
      retentionDays: 365,
      purgeMode: 'archive',
      legalHold: true,
    });
    expect(result.policies[1].targetKind).toBe('transaction');
  });

  it('keeps incomplete YAML as a migration input with review notes', () => {
    const result = parseLegacyRetentionYaml(`
- scope: legacy:unknown
  purge_mode: delete
`);

    expect(result.policies).toHaveLength(1);
    expect(result.policies[0].notes).toContain('Missing name; generated a migration label.');
    expect(result.policies[0].notes).toContain('Missing or invalid retention_days; review before migrating.');
  });
});
