CREATE TABLE IF NOT EXISTS tabular_analysis_jobs (
    id UUID PRIMARY KEY,
    dataset_id UUID NOT NULL,
    analysis_kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    options JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tabular_jobs_dataset_id ON tabular_analysis_jobs(dataset_id);
CREATE INDEX IF NOT EXISTS idx_tabular_jobs_status ON tabular_analysis_jobs(status);

CREATE TABLE IF NOT EXISTS tabular_analysis_results (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES tabular_analysis_jobs(id) ON DELETE CASCADE,
    result_kind TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tabular_results_job_id ON tabular_analysis_results(job_id);
