"""A token minted with only `api:iceberg-read` cannot create / drop /
write — the catalog rejects with 403.
"""

from __future__ import annotations

import httpx
import pytest


pytestmark = pytest.mark.integration


@pytest.fixture()
def read_only_token(catalog_config) -> str:
    body = {
        "grant_type": "client_credentials",
        "client_id": catalog_config.client_id,
        "client_secret": catalog_config.client_secret,
        "scope": "api:iceberg-read",
    }
    response = httpx.post(
        catalog_config.oauth_token_uri,
        data=body,
        headers={"content-type": "application/x-www-form-urlencoded"},
        timeout=5.0,
    )
    response.raise_for_status()
    return response.json()["access_token"]


def test_read_only_token_cannot_create_namespace(catalog_config, read_only_token):
    response = httpx.post(
        f"{catalog_config.base_url}/iceberg/v1/namespaces",
        headers={
            "authorization": f"Bearer {read_only_token}",
            "content-type": "application/json",
        },
        json={"namespace": ["scope_check"], "properties": {}},
        timeout=5.0,
    )
    assert response.status_code == 403, response.text


def test_read_only_token_can_get_config(catalog_config, read_only_token):
    response = httpx.get(
        f"{catalog_config.base_url}/iceberg/v1/config",
        headers={"authorization": f"Bearer {read_only_token}"},
        timeout=5.0,
    )
    assert response.status_code == 200
    body = response.json()
    assert "warehouse" in body["defaults"]
