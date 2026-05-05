"""Long-lived bearer tokens (`ofty_*`) are accepted in addition to
OAuth2-issued JWTs. The catalog mints them via
`POST /v1/iceberg-clients/api-tokens` and validates them against the
`iceberg_api_tokens` Postgres table.
"""

from __future__ import annotations

import httpx
import pytest


pytestmark = pytest.mark.integration


def _mint_long_lived(catalog_config, oauth_token: str) -> str:
    response = httpx.post(
        f"{catalog_config.base_url}/v1/iceberg-clients/api-tokens",
        headers={
            "authorization": f"Bearer {oauth_token}",
            "content-type": "application/json",
        },
        json={
            "name": "PyIceberg suite",
            "scopes": ["api:iceberg-read", "api:iceberg-write"],
        },
        timeout=5.0,
    )
    response.raise_for_status()
    return response.json()["raw_token"]


def test_long_lived_token_is_accepted_on_subsequent_calls(catalog_config, access_token):
    long_lived = _mint_long_lived(catalog_config, access_token)
    assert long_lived.startswith("ofty_")

    response = httpx.get(
        f"{catalog_config.base_url}/iceberg/v1/config",
        headers={"authorization": f"Bearer {long_lived}"},
        timeout=5.0,
    )
    assert response.status_code == 200
