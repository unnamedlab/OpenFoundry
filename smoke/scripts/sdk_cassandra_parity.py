"""S1.9.b - Python SDK parity check post-Cassandra migration.

The generated Python SDK does not currently expose convenience wrappers for
the migrated ontology runtime endpoints. This smoke uses the existing client
request primitive to exercise the public HTTP contract without changing SDK
surface area.
"""

from __future__ import annotations

import json
import os
import sys
from typing import Any

from openfoundry_sdk import OpenFoundryClient  # type: ignore


Check = dict[str, Any]


def env_or_die(name: str) -> str:
    value = os.environ.get(name)
    if not value:
        raise RuntimeError(f"{name} no definida")
    return value


def object_id_of(payload: Any) -> str | None:
    if not isinstance(payload, dict):
        return None
    value = payload.get("id") or payload.get("object_id")
    return value if isinstance(value, str) else None


def list_items_of(payload: Any) -> list[Any] | None:
    if not isinstance(payload, dict):
        return None
    value = payload.get("items") or payload.get("objects")
    return value if isinstance(value, list) else None


def main() -> None:
    base_url = env_or_die("OPENFOUNDRY_BASE_URL")
    token = env_or_die("OPENFOUNDRY_TOKEN")
    tenant = env_or_die("OPENFOUNDRY_TENANT")
    object_id = env_or_die("OPENFOUNDRY_OBJECT_ID")
    type_id = env_or_die("OPENFOUNDRY_TYPE_ID")
    action_id = env_or_die("OPENFOUNDRY_ACTION_ID")

    client = OpenFoundryClient(base_url=base_url, token=token)
    checks: list[Check] = []

    def run_check(name: str, endpoint: str, fn: Any) -> None:
        try:
            details = fn()
            checks.append({"name": name, "endpoint": endpoint, "pass": True, "details": details})
        except Exception as exc:  # noqa: BLE001 - smoke script, keep going and aggregate.
            checks.append({"name": name, "endpoint": endpoint, "pass": False, "error": str(exc)})

    def read_by_id(consistency: str) -> dict[str, Any]:
        payload = client._request(  # noqa: SLF001 - no public generic request in Python SDK.
            "GET",
            "/api/v1/ontology/objects/{tenant}/{object_id}",
            {"tenant": tenant, "object_id": object_id},
            None,
            None,
            headers={"X-Consistency": consistency},
        )
        returned_id = object_id_of(payload)
        if consistency == "strong" and returned_id != object_id:
            raise RuntimeError(f"unexpected by-id payload shape/id: {str(payload)[:200]}")
        if not returned_id:
            raise RuntimeError(f"unexpected {consistency} by-id payload: {str(payload)[:200]}")
        return {"returned_id": returned_id}

    run_check(
        "read_by_id_strong",
        "GET /api/v1/ontology/objects/{tenant}/{object_id}",
        lambda: read_by_id("strong"),
    )
    run_check(
        "read_by_id_eventual",
        "GET /api/v1/ontology/objects/{tenant}/{object_id}",
        lambda: read_by_id("eventual"),
    )

    def list_by_type() -> dict[str, Any]:
        payload = client._request(  # noqa: SLF001 - smoke-only use of existing primitive.
            "GET",
            "/api/v1/ontology/objects/{tenant}/by-type/{type_id}",
            {"tenant": tenant, "type_id": type_id},
            {"size": 25},
            None,
        )
        items = list_items_of(payload)
        if items is None:
            raise RuntimeError(f"list_by_type missing items[]/objects[]: {str(payload)[:200]}")
        return {"returned_items": len(items)}

    run_check(
        "list_by_type",
        "GET /api/v1/ontology/objects/{tenant}/by-type/{type_id}",
        list_by_type,
    )

    def action_execute() -> dict[str, Any]:
        payload = client._request(  # noqa: SLF001 - smoke-only use of existing primitive.
            "POST",
            "/api/v1/ontology/actions/{id}/execute",
            {"id": action_id},
            None,
            {
                "target_object_id": object_id,
                "parameters": {"source": "sdk-parity-py"},
            },
        )
        if not isinstance(payload, dict):
            raise RuntimeError(f"unexpected execute payload: {str(payload)[:200]}")
        return {"response_shape": "object"}

    run_check(
        "action_execute",
        "POST /api/v1/ontology/actions/{id}/execute",
        action_execute,
    )

    pass_ = all(check["pass"] for check in checks)
    print(json.dumps({"client": "python", "pass": pass_, "checks": checks}))
    if not pass_:
        sys.exit(1)


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:  # noqa: BLE001 - smoke script, summarise & exit.
        print(json.dumps({"client": "python", "pass": False, "checks": [], "error": str(exc)}))
        sys.exit(1)
