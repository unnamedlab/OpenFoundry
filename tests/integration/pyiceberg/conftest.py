"""Shared fixtures for the PyIceberg ↔ iceberg-catalog-service e2e suite.

The suite runs against a `docker-compose.dev.yml` stack that boots:

  * `iceberg-catalog-service` (this repo, P3 build)
  * `identity-federation-service` (for OAuth2 client_credentials)
  * `oauth-integration-service` (the catalog calls into `/v1/oauth-clients/validate`)
  * MinIO (S3-compatible warehouse)
  * Postgres (catalog metadata)

Tests skip themselves when the catalog URL is not reachable so the
suite stays opt-in for developers without Docker.
"""

from __future__ import annotations

import os
import time
from dataclasses import dataclass

import httpx
import pytest


CATALOG_URL_ENV = "ICEBERG_CATALOG_URL"
DEFAULT_CATALOG_URL = "http://localhost:8197"


@dataclass(frozen=True)
class CatalogConfig:
    """Connection info reused across tests."""

    base_url: str
    client_id: str
    client_secret: str

    @property
    def oauth_token_uri(self) -> str:
        return f"{self.base_url}/iceberg/v1/oauth/tokens"


def _catalog_reachable(url: str) -> bool:
    try:
        response = httpx.get(f"{url}/health", timeout=2.0)
        return response.status_code == 200
    except Exception:
        return False


@pytest.fixture(scope="session")
def catalog_config() -> CatalogConfig:
    base_url = os.environ.get(CATALOG_URL_ENV, DEFAULT_CATALOG_URL).rstrip("/")
    if not _catalog_reachable(base_url):
        pytest.skip(
            f"iceberg-catalog-service is not reachable at {base_url}; "
            "boot docker-compose.dev.yml to enable this suite",
        )
    return CatalogConfig(
        base_url=base_url,
        client_id=os.environ.get("ICEBERG_TEST_CLIENT_ID", "pyiceberg-suite"),
        client_secret=os.environ.get("ICEBERG_TEST_CLIENT_SECRET", "dev-secret"),
    )


@pytest.fixture(scope="session")
def access_token(catalog_config: CatalogConfig) -> str:
    """Mint an OAuth2 access token via the iceberg-flavoured endpoint."""

    body = {
        "grant_type": "client_credentials",
        "client_id": catalog_config.client_id,
        "client_secret": catalog_config.client_secret,
        "scope": "api:iceberg-read api:iceberg-write",
    }
    response = httpx.post(
        catalog_config.oauth_token_uri,
        data=body,
        headers={"content-type": "application/x-www-form-urlencoded"},
        timeout=5.0,
    )
    response.raise_for_status()
    payload = response.json()
    assert payload["token_type"] == "bearer"
    return payload["access_token"]


@pytest.fixture()
def pyiceberg_catalog(catalog_config: CatalogConfig, access_token: str):
    """Return a configured PyIceberg REST catalog handle.

    PyIceberg is imported lazily so `pytest --collect-only` works on
    machines without the dependency installed.
    """

    pyiceberg = pytest.importorskip("pyiceberg.catalog")
    return pyiceberg.load_catalog(
        "foundry",
        **{
            "uri": f"{catalog_config.base_url}/iceberg",
            "token": access_token,
            "warehouse": "test-warehouse",
        },
    )


@pytest.fixture()
def unique_namespace() -> str:
    """Per-test namespace so parallel runs don't collide."""

    return f"pytest_{int(time.time() * 1000)}"
