CREATE TABLE IF NOT EXISTS ai_knowledge_bases (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    embedding_provider TEXT NOT NULL DEFAULT 'deterministic-hash',
    chunking_strategy TEXT NOT NULL DEFAULT 'balanced',
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    document_count BIGINT NOT NULL DEFAULT 0,
    chunk_count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_knowledge_documents (
    id UUID PRIMARY KEY,
    knowledge_base_id UUID NOT NULL REFERENCES ai_knowledge_bases(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    source_uri TEXT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'indexed',
    chunk_count INTEGER NOT NULL DEFAULT 0,
    chunks JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_knowledge_bases_updated_at ON ai_knowledge_bases(updated_at DESC, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_knowledge_bases_status ON ai_knowledge_bases(status);
CREATE INDEX IF NOT EXISTS idx_ai_knowledge_documents_kb_id ON ai_knowledge_documents(knowledge_base_id);
CREATE INDEX IF NOT EXISTS idx_ai_knowledge_documents_kb_updated_at ON ai_knowledge_documents(knowledge_base_id, updated_at DESC, created_at DESC);
