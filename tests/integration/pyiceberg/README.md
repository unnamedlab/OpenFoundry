# PyIceberg integration suite

End-to-end tests that exercise `iceberg-catalog-service` with real
Iceberg clients (PyIceberg, Snowflake-style requests). The suite is
skipped automatically when the catalog isn't reachable so a developer
without Docker can still run `pytest --collect-only`.

## Run locally

```bash
docker compose -f docker-compose.dev.yml up -d \
    iceberg-catalog-service identity-federation-service \
    oauth-integration-service postgres minio
python -m pip install -r tests/integration/pyiceberg/requirements.txt
ICEBERG_CATALOG_URL=http://localhost:8197 \
    python -m pytest tests/integration/pyiceberg/
```

Override the credentials via:

* `ICEBERG_TEST_CLIENT_ID` (default `pyiceberg-suite`)
* `ICEBERG_TEST_CLIENT_SECRET` (default `dev-secret`)

## CI

The suite runs in `.github/workflows/iceberg-integration.yml` behind
a feature flag — see the workflow for the exact matrix.
