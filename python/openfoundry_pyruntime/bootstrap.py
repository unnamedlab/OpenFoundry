"""Bootstrap helpers injected into every user-script execution namespace.

Mirrors `PYTHON_RUNTIME_BOOTSTRAP` from
`services/ontology-actions-service/src/media_functions.rs` (Rust) at the
shape level. Only the helpers that user code can call are exposed; the
sidecar does not phone home to the ontology service from inside this
module — when the user calls `ontology.create_object(...)` etc. the
helpers issue HTTP requests against the URL passed in via the request
context (the same `serviceToken` and `ontologyServiceUrl` fields the
Rust path used).
"""

from __future__ import annotations

import json
import os
import pathlib
import urllib.error
import urllib.request
from typing import Any


def make_globals(input_payload: dict[str, Any]) -> dict[str, Any]:
    """Return the globals dict an inline-function user script runs in.

    The user script receives:
      * ``context``  — the full input envelope
      * ``action`` / ``target_object`` / ``parameters`` / ``object_set`` /
        ``linked_objects`` / ``justification``
      * ``policy`` — the FunctionCapabilities Rust struct (deny/allow lists)
      * ``service_token`` — bearer token to call ontology/AI services
      * ``ontology`` / ``ai`` — minimal HTTP client helpers
      * ``json`` / ``os`` / ``pathlib`` — standard library re-exports
    """
    context = input_payload.get("context") or {}
    service_token = input_payload.get("serviceToken")
    ontology_url = (input_payload.get("ontologyServiceUrl") or "").rstrip("/")
    ai_url = (input_payload.get("aiServiceUrl") or "").rstrip("/")

    g: dict[str, Any] = {
        "__builtins__": __builtins__,
        "json": json,
        "os": os,
        "pathlib": pathlib,
        "context": context,
        "action": context.get("action"),
        "target_object": context.get("targetObject"),
        "parameters": context.get("parameters") or {},
        "object_set": context.get("objectSet") or [],
        "linked_objects": context.get("linkedObjects") or [],
        "justification": context.get("justification"),
        "policy": input_payload.get("policy"),
        "function_package": input_payload.get("functionPackage"),
        "service_token": service_token,
        "ontology": _OntologyClient(ontology_url, service_token),
        "ai": _AIClient(ai_url, service_token),
    }
    # Sentinels the runtime inspects after the user script returns:
    g.setdefault("result", None)
    g.setdefault("object_patch", None)
    g.setdefault("link", None)
    g.setdefault("delete_object", None)
    return g


class _HTTPClient:
    def __init__(self, base_url: str, token: str | None) -> None:
        self._base = base_url
        self._token = token

    def _request(self, method: str, path: str, body: Any | None = None) -> Any:
        if not self._base:
            raise RuntimeError(f"{type(self).__name__}: base URL not provided")
        url = self._base + (path if path.startswith("/") else f"/{path}")
        data = None
        headers = {"Accept": "application/json"}
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"
        if self._token:
            headers["Authorization"] = f"Bearer {self._token}"
        request = urllib.request.Request(url, data=data, method=method, headers=headers)
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                payload = response.read()
        except urllib.error.HTTPError as error:
            raise RuntimeError(f"HTTP {error.code} from {url}: {error.read().decode('utf-8', 'replace')}") from error
        if not payload:
            return None
        return json.loads(payload)


class _OntologyClient(_HTTPClient):
    def get_object(self, object_id: str) -> Any:
        return self._request("GET", f"/api/v1/objects/{object_id}")

    def create_object(self, payload: dict[str, Any]) -> Any:
        return self._request("POST", "/api/v1/objects", payload)

    def update_object(self, object_id: str, patch: dict[str, Any]) -> Any:
        return self._request("PATCH", f"/api/v1/objects/{object_id}", patch)


class _AIClient(_HTTPClient):
    def chat(self, payload: dict[str, Any]) -> Any:
        return self._request("POST", "/api/v1/ai/chat/completions", payload)
