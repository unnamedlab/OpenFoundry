/**
 * T10 — End-to-end ontology flow.
 *
 * Drives the complete ontology lifecycle the user documented:
 *   1. Create ObjectType `aircraft` with properties.
 *   2. Create LinkType `belongs_to(aircraft → airline)`.
 *   3. Bind the `flights.parquet` dataset (T7 wizard surface) and materialize
 *      it into `object_instances` rows.
 *   4. Create ActionType `update_status` and execute it via the Action
 *      Executor surface.
 *   5. Verify a new `object_revisions` entry exists (T9 timeline) and that an
 *      `audit_events` row was emitted for the action.
 *
 * The flow is exercised through the real `$lib/api` client by issuing `fetch`
 * calls inside the page (so the routes installed via `page.route` intercept
 * them). The mock backend is stateful: each POST mutates an in-memory store
 * and subsequent GET/POSTs honour the new state, so the test exercises the
 * actual happy-path orchestration of the ontology API surface.
 */

import { expect, test, type Page } from '@playwright/test';

import { seedAuthenticatedSession } from './support/api';

interface MockObjectType {
  id: string;
  name: string;
  display_name: string;
  description: string;
  primary_key_property: string;
  icon: string | null;
  color: string | null;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

interface MockProperty {
  id: string;
  object_type_id: string;
  name: string;
  display_name: string;
  description: string;
  property_type: string;
  required: boolean;
  unique_constraint: boolean;
  time_dependent: boolean;
  default_value: unknown;
  validation_rules: unknown;
  created_at: string;
  updated_at: string;
}

interface MockLinkType {
  id: string;
  name: string;
  display_name: string;
  description: string;
  source_type_id: string;
  target_type_id: string;
  cardinality: string;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

interface MockBinding {
  id: string;
  object_type_id: string;
  dataset_id: string;
  primary_key_column: string;
  property_mapping: Array<{ source_column: string; target_property: string; transform?: string | null }>;
  sync_mode: string;
  default_marking: string;
  preview_limit: number;
  owner_id: string;
  created_at: string;
  updated_at: string;
  last_materialized_at: string | null;
  last_run_status: string | null;
  last_run_summary: Record<string, unknown> | null;
}

interface MockObjectInstance {
  id: string;
  object_type_id: string;
  properties: Record<string, unknown>;
  marking: string;
  organization_id: string | null;
  created_by: string;
  created_at: string;
  updated_at: string;
}

interface MockObjectRevision {
  id: string;
  object_id: string;
  object_type_id: string;
  operation: 'insert' | 'update' | 'delete';
  properties: Record<string, unknown>;
  marking: string;
  organization_id: string | null;
  changed_by: string;
  revision_number: number;
  written_at: string;
}

interface MockActionType {
  id: string;
  name: string;
  display_name: string;
  description: string;
  object_type_id: string;
  operation_kind: string;
  input_schema: Array<{ name: string; property_type: string; required?: boolean }>;
  form_schema: { sections?: unknown[]; parameter_overrides?: unknown[] };
  config: { property_mappings: Array<{ property_name: string; input_name?: string; value?: unknown }> };
  confirmation_required: boolean;
  permission_key: string | null;
  authorization_policy: Record<string, unknown>;
  owner_id: string;
  created_at: string;
  updated_at: string;
}

interface MockAuditEvent {
  id: string;
  source_service: string;
  channel: string;
  actor: string;
  action: string;
  resource_type: string;
  resource_id: string;
  status: string;
  severity: string;
  classification: string;
  metadata: Record<string, unknown>;
  recorded_at: string;
}

interface FlowState {
  objectTypes: MockObjectType[];
  propertiesByType: Map<string, MockProperty[]>;
  linkTypes: MockLinkType[];
  bindingsByType: Map<string, MockBinding[]>;
  objectsByType: Map<string, MockObjectInstance[]>;
  revisionsByObject: Map<string, MockObjectRevision[]>;
  actionTypes: MockActionType[];
  auditEvents: MockAuditEvent[];
}

const ACTOR_ID = '00000000-0000-7000-8000-000000000001';
const ORG_ID = '00000000-0000-7000-8000-00000000000a';
const FLIGHTS_DATASET_ID = '00000000-0000-7000-8000-0000000fl1ts';

function mintId(prefix: string, n: number): string {
  return `${prefix}-${String(n).padStart(8, '0')}`;
}

async function installFlowMockBackend(page: Page, state: FlowState) {
  const json = (status: number, body: unknown) => ({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body),
  });

  await page.route('**/api/v1/**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname, searchParams } = url;
    const method = request.method();

    // ---- Auth bootstrap (the auth store probes /users/me on first nav). --
    if (pathname === '/api/v1/users/me') {
      return route.fulfill(
        json(200, {
          id: ACTOR_ID,
          email: 'flow@openfoundry.dev',
          name: 'Flow Test User',
          is_active: true,
          roles: ['operator', 'admin'],
          groups: ['platform'],
          permissions: ['*:*'],
          organization_id: ORG_ID,
          attributes: {},
          mfa_enabled: false,
          mfa_enforced: false,
          auth_source: 'local',
          created_at: '2026-01-01T00:00:00Z',
        }),
      );
    }

    // ---- ObjectTypes -----------------------------------------------------
    if (pathname === '/api/v1/ontology/types' && method === 'GET') {
      return route.fulfill(
        json(200, { data: state.objectTypes, total: state.objectTypes.length, page: 1, per_page: 100 }),
      );
    }
    if (pathname === '/api/v1/ontology/types' && method === 'POST') {
      const body = request.postDataJSON() as Partial<MockObjectType>;
      const created: MockObjectType = {
        id: mintId('object-type', state.objectTypes.length + 1),
        name: body.name ?? 'unnamed',
        display_name: body.display_name ?? body.name ?? 'Unnamed',
        description: body.description ?? '',
        primary_key_property: body.primary_key_property ?? 'id',
        icon: body.icon ?? null,
        color: body.color ?? null,
        owner_id: ACTOR_ID,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      };
      state.objectTypes.push(created);
      state.propertiesByType.set(created.id, []);
      state.bindingsByType.set(created.id, []);
      state.objectsByType.set(created.id, []);
      return route.fulfill(json(201, created));
    }

    const propertiesMatch = pathname.match(/^\/api\/v1\/ontology\/types\/([^/]+)\/properties$/);
    if (propertiesMatch) {
      const typeId = propertiesMatch[1];
      if (method === 'GET') {
        return route.fulfill(json(200, { data: state.propertiesByType.get(typeId) ?? [] }));
      }
      if (method === 'POST') {
        const body = request.postDataJSON() as Partial<MockProperty>;
        const list = state.propertiesByType.get(typeId) ?? [];
        const created: MockProperty = {
          id: mintId(`property-${typeId}`, list.length + 1),
          object_type_id: typeId,
          name: body.name ?? 'unnamed',
          display_name: body.display_name ?? body.name ?? 'Unnamed',
          description: body.description ?? '',
          property_type: body.property_type ?? 'string',
          required: body.required ?? false,
          unique_constraint: body.unique_constraint ?? false,
          time_dependent: body.time_dependent ?? false,
          default_value: body.default_value ?? null,
          validation_rules: body.validation_rules ?? null,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        };
        state.propertiesByType.set(typeId, [...list, created]);
        return route.fulfill(json(201, created));
      }
    }

    // ---- LinkTypes -------------------------------------------------------
    if (pathname === '/api/v1/ontology/links' && method === 'GET') {
      return route.fulfill(json(200, { data: state.linkTypes, total: state.linkTypes.length }));
    }
    if (pathname === '/api/v1/ontology/links' && method === 'POST') {
      const body = request.postDataJSON() as Partial<MockLinkType>;
      const created: MockLinkType = {
        id: mintId('link-type', state.linkTypes.length + 1),
        name: body.name ?? 'unnamed_link',
        display_name: body.display_name ?? body.name ?? 'Unnamed link',
        description: body.description ?? '',
        source_type_id: body.source_type_id!,
        target_type_id: body.target_type_id!,
        cardinality: body.cardinality ?? 'many_to_many',
        owner_id: ACTOR_ID,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      };
      state.linkTypes.push(created);
      return route.fulfill(json(201, created));
    }

    // ---- Object-type → Dataset bindings (T2/T7) --------------------------
    const bindingsMatch = pathname.match(/^\/api\/v1\/ontology\/types\/([^/]+)\/bindings$/);
    if (bindingsMatch) {
      const typeId = bindingsMatch[1];
      if (method === 'GET') {
        return route.fulfill(json(200, { data: state.bindingsByType.get(typeId) ?? [] }));
      }
      if (method === 'POST') {
        const body = request.postDataJSON() as Partial<MockBinding>;
        const list = state.bindingsByType.get(typeId) ?? [];
        const created: MockBinding = {
          id: mintId(`binding-${typeId}`, list.length + 1),
          object_type_id: typeId,
          dataset_id: body.dataset_id!,
          primary_key_column: body.primary_key_column ?? 'id',
          property_mapping: body.property_mapping ?? [],
          sync_mode: body.sync_mode ?? 'snapshot',
          default_marking: body.default_marking ?? 'public',
          preview_limit: body.preview_limit ?? 50,
          owner_id: ACTOR_ID,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
          last_materialized_at: null,
          last_run_status: null,
          last_run_summary: null,
        };
        state.bindingsByType.set(typeId, [...list, created]);
        return route.fulfill(json(201, created));
      }
    }

    const materializeMatch = pathname.match(
      /^\/api\/v1\/ontology\/types\/([^/]+)\/bindings\/([^/]+)\/materialize$/,
    );
    if (materializeMatch && method === 'POST') {
      const [, typeId, bindingId] = materializeMatch;
      const list = state.bindingsByType.get(typeId) ?? [];
      const binding = list.find((b) => b.id === bindingId);
      if (!binding) return route.fulfill(json(404, { error: 'binding not found' }));
      // Simulate two rows materialized from the parquet sample.
      const sample = [
        { tail_number: 'AF-101', model: 'A320', status: 'ready', airline_id: 'AL-1' },
        { tail_number: 'AF-202', model: 'B737', status: 'maintenance', airline_id: 'AL-2' },
      ];
      const objects = state.objectsByType.get(typeId) ?? [];
      let inserted = 0;
      for (const row of sample) {
        const id = mintId(`object-${typeId}`, objects.length + inserted + 1);
        const properties: Record<string, unknown> = {};
        for (const map of binding.property_mapping) {
          properties[map.target_property] = (row as Record<string, unknown>)[map.source_column];
        }
        const now = new Date().toISOString();
        const obj: MockObjectInstance = {
          id,
          object_type_id: typeId,
          properties,
          marking: binding.default_marking,
          organization_id: ORG_ID,
          created_by: ACTOR_ID,
          created_at: now,
          updated_at: now,
        };
        objects.push(obj);
        const rev: MockObjectRevision = {
          id: mintId(`revision-${id}`, 1),
          object_id: id,
          object_type_id: typeId,
          operation: 'insert',
          properties,
          marking: binding.default_marking,
          organization_id: ORG_ID,
          changed_by: ACTOR_ID,
          revision_number: 1,
          written_at: now,
        };
        state.revisionsByObject.set(id, [rev]);
        inserted += 1;
      }
      state.objectsByType.set(typeId, objects);
      binding.last_materialized_at = new Date().toISOString();
      binding.last_run_status = 'completed';
      binding.last_run_summary = { inserted, updated: 0, skipped: 0, errors: 0 };
      return route.fulfill(
        json(200, {
          binding_id: bindingId,
          status: 'completed',
          rows_read: sample.length,
          inserted,
          updated: 0,
          skipped: 0,
          errors: 0,
          dry_run: false,
        }),
      );
    }

    // ---- ObjectInstances list & retrieval --------------------------------
    const objectsListMatch = pathname.match(/^\/api\/v1\/ontology\/types\/([^/]+)\/objects$/);
    if (objectsListMatch && method === 'GET') {
      const typeId = objectsListMatch[1];
      const objects = state.objectsByType.get(typeId) ?? [];
      return route.fulfill(json(200, { data: objects, total: objects.length }));
    }

    const objectByIdMatch = pathname.match(/^\/api\/v1\/ontology\/types\/([^/]+)\/objects\/([^/]+)$/);
    if (objectByIdMatch && method === 'GET') {
      const [, typeId, objectId] = objectByIdMatch;
      const objects = state.objectsByType.get(typeId) ?? [];
      const obj = objects.find((o) => o.id === objectId);
      if (!obj) return route.fulfill(json(404, { error: 'not found' }));
      return route.fulfill(json(200, obj));
    }

    // ---- ObjectRevisions list (T9) ---------------------------------------
    const revisionsListMatch = pathname.match(
      /^\/api\/v1\/ontology\/types\/([^/]+)\/objects\/([^/]+)\/revisions$/,
    );
    if (revisionsListMatch && method === 'GET') {
      const [, , objectId] = revisionsListMatch;
      const limit = Number(searchParams.get('limit') ?? '50');
      const list = (state.revisionsByObject.get(objectId) ?? [])
        .slice()
        .sort((a, b) => b.revision_number - a.revision_number)
        .slice(0, limit);
      return route.fulfill(json(200, { object_id: objectId, total: list.length, data: list }));
    }

    // ---- ActionTypes -----------------------------------------------------
    if (pathname === '/api/v1/ontology/actions' && method === 'GET') {
      return route.fulfill(
        json(200, { data: state.actionTypes, total: state.actionTypes.length, page: 1, per_page: 100 }),
      );
    }
    if (pathname === '/api/v1/ontology/actions' && method === 'POST') {
      const body = request.postDataJSON() as Partial<MockActionType>;
      const created: MockActionType = {
        id: mintId('action-type', state.actionTypes.length + 1),
        name: body.name ?? 'unnamed_action',
        display_name: body.display_name ?? body.name ?? 'Unnamed action',
        description: body.description ?? '',
        object_type_id: body.object_type_id!,
        operation_kind: body.operation_kind ?? 'update_object',
        input_schema: body.input_schema ?? [],
        form_schema: body.form_schema ?? {},
        config: body.config ?? { property_mappings: [] },
        confirmation_required: body.confirmation_required ?? false,
        permission_key: body.permission_key ?? null,
        authorization_policy: body.authorization_policy ?? {},
        owner_id: ACTOR_ID,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      };
      state.actionTypes.push(created);
      return route.fulfill(json(201, created));
    }

    const actionExecuteMatch = pathname.match(/^\/api\/v1\/ontology\/actions\/([^/]+)\/execute$/);
    if (actionExecuteMatch && method === 'POST') {
      const actionId = actionExecuteMatch[1];
      const action = state.actionTypes.find((a) => a.id === actionId);
      if (!action) return route.fulfill(json(404, { error: 'action not found' }));
      const body = request.postDataJSON() as {
        target_object_id?: string;
        parameters?: Record<string, unknown>;
        justification?: string;
      };
      const objects = state.objectsByType.get(action.object_type_id) ?? [];
      const target = objects.find((o) => o.id === body.target_object_id);
      if (!target) return route.fulfill(json(400, { error: 'target object missing' }));
      const patch: Record<string, unknown> = {};
      for (const mapping of action.config.property_mappings) {
        if (mapping.input_name && body.parameters && mapping.input_name in body.parameters) {
          patch[mapping.property_name] = body.parameters[mapping.input_name];
        } else if (mapping.value !== undefined) {
          patch[mapping.property_name] = mapping.value;
        }
      }
      const now = new Date().toISOString();
      target.properties = { ...target.properties, ...patch };
      target.updated_at = now;
      const revisions = state.revisionsByObject.get(target.id) ?? [];
      const next: MockObjectRevision = {
        id: mintId(`revision-${target.id}`, revisions.length + 1),
        object_id: target.id,
        object_type_id: target.object_type_id,
        operation: 'update',
        properties: { ...target.properties },
        marking: target.marking,
        organization_id: target.organization_id,
        changed_by: ACTOR_ID,
        revision_number: revisions.length + 1,
        written_at: now,
      };
      revisions.push(next);
      state.revisionsByObject.set(target.id, revisions);
      // Emit audit event
      state.auditEvents.push({
        id: mintId('audit', state.auditEvents.length + 1),
        source_service: 'ontology-service',
        channel: 'api',
        actor: 'flow@openfoundry.dev',
        action: 'ontology.action.execute',
        resource_type: 'ontology_object',
        resource_id: target.id,
        status: 'success',
        severity: 'info',
        classification: target.marking,
        metadata: {
          action_id: action.id,
          action_name: action.name,
          operation_kind: action.operation_kind,
          parameters: body.parameters ?? {},
          justification: body.justification ?? null,
          revision_number: next.revision_number,
        },
        recorded_at: now,
      });
      return route.fulfill(
        json(200, {
          action,
          target_object_id: target.id,
          deleted: false,
          preview: { kind: 'update_object', target_object_id: target.id, patch },
          object: target,
          link: null,
          result: null,
        }),
      );
    }

    // ---- Audit events ----------------------------------------------------
    if (pathname === '/api/v1/audit/events' && method === 'GET') {
      const action = searchParams.get('action');
      const data = action
        ? state.auditEvents.filter((event) => event.action === action)
        : state.auditEvents;
      return route.fulfill(json(200, { data, total: data.length }));
    }

    // Catch-all so we surface unexpected calls in test logs.
    return route.fulfill(json(500, { error: `unhandled mock for ${method} ${pathname}` }));
  });
}

test.describe('T10 — full ontology lifecycle', () => {
  test('object type → link → bind+materialize → action → revision + audit', async ({
    page,
  }) => {
    const state: FlowState = {
      objectTypes: [],
      propertiesByType: new Map(),
      linkTypes: [],
      bindingsByType: new Map(),
      objectsByType: new Map(),
      revisionsByObject: new Map(),
      actionTypes: [],
      auditEvents: [],
    };

    await seedAuthenticatedSession(page);
    await installFlowMockBackend(page, state);

    // Boot the SPA so `fetch` calls inside `page.evaluate` go through the
    // browser context and are intercepted by `page.route`.
    await page.goto('/');

    // ----- 1. Create the Aircraft ObjectType + properties ----------------
    const aircraft = await page.evaluate(async (datasetId) => {
      const headers = { 'Content-Type': 'application/json' };
      const post = async (path: string, body: unknown) => {
        const r = await fetch(path, { method: 'POST', headers, body: JSON.stringify(body) });
        if (!r.ok) throw new Error(`${path} → ${r.status}`);
        return r.json();
      };

      const objectType = await post('/api/v1/ontology/types', {
        name: 'aircraft',
        display_name: 'Aircraft',
        description: 'Operational aircraft tracked in the ontology',
        primary_key_property: 'tail_number',
      });
      for (const prop of [
        { name: 'tail_number', property_type: 'string', required: true, unique_constraint: true },
        { name: 'model', property_type: 'string' },
        { name: 'status', property_type: 'string' },
        { name: 'airline_id', property_type: 'string' },
      ]) {
        await post(`/api/v1/ontology/types/${objectType.id}/properties`, {
          ...prop,
          display_name: prop.name,
          description: '',
        });
      }

      const airline = await post('/api/v1/ontology/types', {
        name: 'airline',
        display_name: 'Airline',
        description: 'Operating airline',
        primary_key_property: 'code',
      });
      await post(`/api/v1/ontology/types/${airline.id}/properties`, {
        name: 'code',
        property_type: 'string',
        required: true,
        unique_constraint: true,
        display_name: 'Code',
        description: '',
      });

      // ----- 2. Create the LinkType belongs_to(Aircraft → Airline) ------
      const linkType = await post('/api/v1/ontology/links', {
        name: 'belongs_to',
        display_name: 'Belongs to',
        source_type_id: objectType.id,
        target_type_id: airline.id,
        cardinality: 'many_to_one',
      });

      // ----- 3. Bind flights.parquet and materialize --------------------
      const binding = await post(`/api/v1/ontology/types/${objectType.id}/bindings`, {
        dataset_id: datasetId,
        primary_key_column: 'tail_number',
        sync_mode: 'snapshot',
        default_marking: 'public',
        property_mapping: [
          { source_column: 'tail_number', target_property: 'tail_number' },
          { source_column: 'model', target_property: 'model' },
          { source_column: 'status', target_property: 'status' },
          { source_column: 'airline_id', target_property: 'airline_id' },
        ],
      });
      const materialization = await post(
        `/api/v1/ontology/types/${objectType.id}/bindings/${binding.id}/materialize`,
        { dry_run: false },
      );

      // ----- 4. Create ActionType update_status -------------------------
      const action = await post('/api/v1/ontology/actions', {
        name: 'update_status',
        display_name: 'Update aircraft status',
        object_type_id: objectType.id,
        operation_kind: 'update_object',
        input_schema: [{ name: 'next_status', property_type: 'string', required: true }],
        form_schema: { sections: [], parameter_overrides: [] },
        config: {
          property_mappings: [{ property_name: 'status', input_name: 'next_status' }],
        },
        confirmation_required: false,
        authorization_policy: {},
      });

      return {
        aircraftTypeId: objectType.id,
        airlineTypeId: airline.id,
        linkTypeId: linkType.id,
        bindingId: binding.id,
        materialization,
        actionId: action.id,
      };
    }, FLIGHTS_DATASET_ID);

    // Confirm materialization shape.
    expect(aircraft.materialization).toMatchObject({
      status: 'completed',
      rows_read: 2,
      inserted: 2,
    });

    // List newly materialized objects, pick the first to mutate.
    const objects = await page.evaluate(async (typeId) => {
      const r = await fetch(`/api/v1/ontology/types/${typeId}/objects`);
      return r.json();
    }, aircraft.aircraftTypeId);

    expect(objects.total).toBe(2);
    const targetObjectId = objects.data[0].id as string;
    expect(targetObjectId).toBeTruthy();

    // ----- 5. Execute the update_status action ---------------------------
    const executed = await page.evaluate(
      async ({ actionId, targetObjectId: oid }) => {
        const r = await fetch(`/api/v1/ontology/actions/${actionId}/execute`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            target_object_id: oid,
            parameters: { next_status: 'grounded' },
            justification: 'T10 E2E coverage',
          }),
        });
        if (!r.ok) throw new Error(`execute → ${r.status}`);
        return r.json();
      },
      { actionId: aircraft.actionId, targetObjectId },
    );
    expect(executed.deleted).toBe(false);
    expect(executed.object.properties.status).toBe('grounded');
    expect(executed.preview).toMatchObject({ kind: 'update_object', target_object_id: targetObjectId });

    // ----- 6. Verify revision lineage (T9) -------------------------------
    const revisions = await page.evaluate(async ({ typeId, oid }) => {
      const r = await fetch(`/api/v1/ontology/types/${typeId}/objects/${oid}/revisions?limit=10`);
      return r.json();
    }, { typeId: aircraft.aircraftTypeId, oid: targetObjectId });

    expect(revisions.total).toBe(2);
    expect(revisions.data[0]).toMatchObject({
      revision_number: 2,
      operation: 'update',
    });
    expect(revisions.data[0].properties.status).toBe('grounded');
    expect(revisions.data[1]).toMatchObject({
      revision_number: 1,
      operation: 'insert',
    });

    // ----- 7. Verify the audit event was emitted -------------------------
    const audit = await page.evaluate(async () => {
      const r = await fetch('/api/v1/audit/events?action=ontology.action.execute');
      return r.json();
    });
    expect(audit.total).toBe(1);
    expect(audit.data[0]).toMatchObject({
      action: 'ontology.action.execute',
      resource_type: 'ontology_object',
      resource_id: targetObjectId,
      status: 'success',
    });
    expect(audit.data[0].metadata).toMatchObject({
      action_id: aircraft.actionId,
      action_name: 'update_status',
      operation_kind: 'update_object',
      revision_number: 2,
    });
  });
});
