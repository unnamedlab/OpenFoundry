-- S8 / ADR-0030: schema absorbed from the retired `spreadsheet-computation-service`.
-- The source was a `tools/scaffold_p59_p85.py` placeholder (`fn main() {}`
-- stub, generic CRUD); tables preserved on `notebook-pg` for future
-- spreadsheet-computation work in `notebook-runtime-service`.
CREATE TABLE IF NOT EXISTS spreadsheet_sheets (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_spreadsheet_sheets_created_at ON spreadsheet_sheets(created_at);

CREATE TABLE IF NOT EXISTS spreadsheet_recalcs (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES spreadsheet_sheets(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_spreadsheet_recalcs_parent_id ON spreadsheet_recalcs(parent_id);
