"""Snowflake-style requests against the Iceberg REST Catalog.

Snowflake's external Iceberg integration uses the same REST endpoints
as PyIceberg / Spark; the only meaningful difference is the
`User-Agent` and a stricter expectation on the `config.warehouse`
field. We replay the exact request shape Snowflake's connector emits
to confirm contract conformance.
"""

from __future__ import annotations

import httpx
import pytest


pytestmark = pytest.mark.integration

SNOWFLAKE_USER_AGENT = "Snowflake/Iceberg-REST-Connector"


def test_config_endpoint_returns_warehouse_default(catalog_config, access_token):
    response = httpx.get(
        f"{catalog_config.base_url}/iceberg/v1/config",
        headers={
            "authorization": f"Bearer {access_token}",
            "user-agent": SNOWFLAKE_USER_AGENT,
        },
        timeout=5.0,
    )
    assert response.status_code == 200
    body = response.json()
    assert body["defaults"].get("warehouse")


def test_namespaces_listing_works_for_snowflake_ua(catalog_config, access_token):
    response = httpx.get(
        f"{catalog_config.base_url}/iceberg/v1/namespaces",
        headers={
            "authorization": f"Bearer {access_token}",
            "user-agent": SNOWFLAKE_USER_AGENT,
        },
        timeout=5.0,
    )
    assert response.status_code == 200
    body = response.json()
    assert "namespaces" in body
