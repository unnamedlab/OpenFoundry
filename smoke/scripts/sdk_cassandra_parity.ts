// S1.9.b - TypeScript SDK parity check post-Cassandra migration.
//
// This smoke keeps the public runtime contract stable while the ontology hot
// path moves to Cassandra. The generated SDK does not expose convenience
// wrappers for the migrated runtime endpoints yet, so the script uses the
// client's public request() escape hatch instead of changing SDK contracts.

import { OpenFoundryClient } from '../../sdks/typescript/openfoundry-sdk/src/index.ts';

type Check = {
  name: string;
  endpoint: string;
  pass: boolean;
  details?: Record<string, unknown>;
  error?: string;
};

function envOrThrow(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(`${name} no definida`);
  }
  return value;
}

function objectIdOf(payload: unknown): string | undefined {
  if (!payload || typeof payload !== 'object') {
    return undefined;
  }
  const record = payload as Record<string, unknown>;
  const id = record.id ?? record.object_id;
  return typeof id === 'string' ? id : undefined;
}

function listItemsOf(payload: unknown): unknown[] | undefined {
  if (!payload || typeof payload !== 'object') {
    return undefined;
  }
  const record = payload as Record<string, unknown>;
  const items = record.items ?? record.objects;
  return Array.isArray(items) ? items : undefined;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

async function main() {
  const baseUrl = envOrThrow('OPENFOUNDRY_BASE_URL');
  const token = envOrThrow('OPENFOUNDRY_TOKEN');
  const tenant = envOrThrow('OPENFOUNDRY_TENANT');
  const objectId = envOrThrow('OPENFOUNDRY_OBJECT_ID');
  const typeId = envOrThrow('OPENFOUNDRY_TYPE_ID');
  const actionId = envOrThrow('OPENFOUNDRY_ACTION_ID');

  const client = new OpenFoundryClient({ baseUrl, token });
  const checks: Check[] = [];

  async function runCheck(
    name: string,
    endpoint: string,
    fn: () => Promise<Record<string, unknown>>,
  ) {
    try {
      const details = await fn();
      checks.push({ name, endpoint, pass: true, details });
    } catch (error) {
      checks.push({ name, endpoint, pass: false, error: errorMessage(error) });
    }
  }

  await runCheck(
    'read_by_id_strong',
    'GET /api/v1/ontology/objects/{tenant}/{object_id}',
    async () => {
      const payload = await client.request<unknown>(
        'GET',
        '/api/v1/ontology/objects/{tenant}/{object_id}',
        { tenant, object_id: objectId },
        undefined,
        undefined,
        { headers: { 'X-Consistency': 'strong' } },
      );
      const returnedId = objectIdOf(payload);
      if (returnedId !== objectId) {
        throw new Error(`unexpected by-id payload shape/id: ${JSON.stringify(payload).slice(0, 200)}`);
      }
      return { returned_id: returnedId };
    },
  );

  await runCheck(
    'read_by_id_eventual',
    'GET /api/v1/ontology/objects/{tenant}/{object_id}',
    async () => {
      const payload = await client.request<unknown>(
        'GET',
        '/api/v1/ontology/objects/{tenant}/{object_id}',
        { tenant, object_id: objectId },
        undefined,
        undefined,
        { headers: { 'X-Consistency': 'eventual' } },
      );
      const returnedId = objectIdOf(payload);
      if (!returnedId) {
        throw new Error(`unexpected eventual by-id payload: ${JSON.stringify(payload).slice(0, 200)}`);
      }
      return { returned_id: returnedId };
    },
  );

  await runCheck(
    'list_by_type',
    'GET /api/v1/ontology/objects/{tenant}/by-type/{type_id}',
    async () => {
      const payload = await client.request<unknown>(
        'GET',
        '/api/v1/ontology/objects/{tenant}/by-type/{type_id}',
        { tenant, type_id: typeId },
        { size: 25 },
        undefined,
      );
      const items = listItemsOf(payload);
      if (!items) {
        throw new Error(`list_by_type payload missing items[]/objects[]: ${JSON.stringify(payload).slice(0, 200)}`);
      }
      return { returned_items: items.length };
    },
  );

  await runCheck(
    'action_execute',
    'POST /api/v1/ontology/actions/{id}/execute',
    async () => {
      const payload = await client.request<unknown>(
        'POST',
        '/api/v1/ontology/actions/{id}/execute',
        { id: actionId },
        undefined,
        {
          target_object_id: objectId,
          parameters: { source: 'sdk-parity-ts' },
        },
      );
      if (!payload || typeof payload !== 'object') {
        throw new Error(`unexpected execute payload: ${JSON.stringify(payload).slice(0, 200)}`);
      }
      return { response_shape: 'object' };
    },
  );

  const pass = checks.every((check) => check.pass);
  console.log(JSON.stringify({ client: 'typescript', pass, checks }));
  if (!pass) {
    process.exit(1);
  }
}

main().catch((error) => {
  console.log(JSON.stringify({
    client: 'typescript',
    pass: false,
    checks: [],
    error: errorMessage(error),
  }));
  process.exit(1);
});
