// S1.9.b — TypeScript SDK parity check post-Cassandra migration.
//
// Ejerce los tres paths cubiertos por el bench S1.8:
//   1. Read by id  (GET /api/v1/ontology/objects/{tenant}/{id})
//   2. Read by type (GET …/{tenant}/by-type/{type_id})
//   3. Action execute (POST /api/v1/ontology/actions/{id}/execute)
//
// El SDK no cambia con la migración: mismo proto, mismas rutas. Si
// alguna llamada falla con un shape inesperado, S1.9.b reporta
// regresión de contrato.

import { OpenFoundryClient } from '../../sdks/typescript/openfoundry-sdk/src/index';

function envOrThrow(name: string): string {
  const v = process.env[name];
  if (!v) {
    throw new Error(`${name} no definida`);
  }
  return v;
}

async function main() {
  const baseUrl = envOrThrow('OPENFOUNDRY_BASE_URL');
  const token = envOrThrow('OPENFOUNDRY_TOKEN');
  const tenant = envOrThrow('OPENFOUNDRY_TENANT');
  const objectId = envOrThrow('OPENFOUNDRY_OBJECT_ID');
  const typeId = envOrThrow('OPENFOUNDRY_TYPE_ID');
  const actionId = envOrThrow('OPENFOUNDRY_ACTION_ID');

  const client = new OpenFoundryClient({ baseUrl, token });

  // 1. read by id (strong por defecto).
  const byId = await client.ontology.getObject(tenant, objectId);
  if (!byId || typeof byId !== 'object' || !('object_id' in byId)) {
    throw new Error(`unexpected by-id payload shape: ${JSON.stringify(byId).slice(0, 200)}`);
  }

  // 2. read by id eventual (cache path).
  const byIdEventual = await client.ontology.getObject(tenant, objectId, {
    consistency: 'eventual',
  });
  if (!byIdEventual || typeof byIdEventual !== 'object') {
    throw new Error('unexpected eventual by-id payload');
  }

  // 3. list by type.
  const page = await client.ontology.listObjectsByType(tenant, typeId, { limit: 25 });
  if (!Array.isArray(page.objects)) {
    throw new Error(`list_by_type payload missing objects[]: ${JSON.stringify(page).slice(0, 200)}`);
  }

  // 4. action execute (idempotente; el helper apply_object_with_outbox absorbe replay).
  const result = await client.ontology.executeAction(actionId, {
    tenant_id: tenant,
    target_object_id: objectId,
    payload: { source: 'sdk-parity-ts' },
  });
  if (!result || typeof result !== 'object') {
    throw new Error('unexpected execute payload');
  }

  console.log(JSON.stringify({ ok: true, byIdId: byId.object_id, listed: page.objects.length }));
}

main().catch((err) => {
  console.error(JSON.stringify({ ok: false, error: String(err && err.message || err) }));
  process.exit(1);
});
