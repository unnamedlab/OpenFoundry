CREATE TABLE IF NOT EXISTS model_adapters (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    adapter_kind TEXT NOT NULL,
    artifact_uri TEXT NOT NULL,
    sidecar_image TEXT,
    framework TEXT,
    model_id UUID,
    status TEXT NOT NULL DEFAULT 'registered',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_model_adapters_model_id ON model_adapters(model_id);
CREATE INDEX IF NOT EXISTS idx_model_adapters_kind ON model_adapters(adapter_kind);

CREATE TABLE IF NOT EXISTS inference_contracts (
    id UUID PRIMARY KEY,
    adapter_id UUID NOT NULL REFERENCES model_adapters(id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    input_schema JSONB NOT NULL,
    output_schema JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (adapter_id, version)
);

CREATE INDEX IF NOT EXISTS idx_inference_contracts_adapter_id ON inference_contracts(adapter_id);
