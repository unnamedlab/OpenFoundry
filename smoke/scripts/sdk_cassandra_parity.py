"""S1.9.b — Python SDK parity check post-Cassandra migration.

Ejerce los tres paths cubiertos por el bench S1.8 (read-by-id,
read-by-type, action execute) usando el SDK Python publicado. El
contrato no cambia con la migración a Cassandra; cualquier diff de
shape detectado aquí es una regresión.

Salida: imprime un JSON `{ok, …}` por stdout. Exit 0 si todo pasa.
"""

from __future__ import annotations

import json
import os
import sys

from openfoundry_sdk import OpenFoundryClient  # type: ignore


def env_or_die(name: str) -> str:
    value = os.environ.get(name)
    if not value:
        raise SystemExit(f"{name} no definida")
    return value


def main() -> None:
    base_url = env_or_die("OPENFOUNDRY_BASE_URL")
    token = env_or_die("OPENFOUNDRY_TOKEN")
    tenant = env_or_die("OPENFOUNDRY_TENANT")
    object_id = env_or_die("OPENFOUNDRY_OBJECT_ID")
    type_id = env_or_die("OPENFOUNDRY_TYPE_ID")
    action_id = env_or_die("OPENFOUNDRY_ACTION_ID")

    client = OpenFoundryClient(base_url=base_url, token=token)

    # 1. read by id (strong default).
    by_id = client.ontology.get_object(tenant, object_id)
    if not isinstance(by_id, dict) or "object_id" not in by_id:
        raise SystemExit(f"unexpected by-id payload: {str(by_id)[:200]}")

    # 2. read by id eventual.
    by_id_eventual = client.ontology.get_object(tenant, object_id, consistency="eventual")
    if not isinstance(by_id_eventual, dict):
        raise SystemExit("unexpected eventual by-id payload")

    # 3. list by type.
    page = client.ontology.list_objects_by_type(tenant, type_id, limit=25)
    if not isinstance(getattr(page, "objects", None), list):
        raise SystemExit(f"list_by_type missing objects[]: {str(page)[:200]}")

    # 4. action execute.
    result = client.ontology.execute_action(
        action_id,
        tenant_id=tenant,
        target_object_id=object_id,
        payload={"source": "sdk-parity-py"},
    )
    if not isinstance(result, dict):
        raise SystemExit("unexpected execute payload")

    print(json.dumps({"ok": True, "by_id_id": by_id["object_id"], "listed": len(page.objects)}))


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:  # noqa: BLE001 — smoke script, summarise & exit.
        print(json.dumps({"ok": False, "error": str(exc)}), file=sys.stderr)
        sys.exit(1)
