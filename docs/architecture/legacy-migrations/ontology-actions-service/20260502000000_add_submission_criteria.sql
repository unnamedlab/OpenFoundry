-- TASK C — Submission criteria AST persisted on action_types.
--
-- Adds the typed AST authored in `libs/ontology-kernel/src/models/submission_criteria.rs`
-- as a JSONB column on `action_types`. NULL is stored as `'null'::jsonb` so existing rows
-- mean "no submission criteria configured" and the kernel evaluator skips evaluation.
-- Foundry's docs (`Action types/Submission criteria.md`) treat criteria as optional.

ALTER TABLE action_types
ADD COLUMN IF NOT EXISTS submission_criteria JSONB NOT NULL DEFAULT 'null'::jsonb;

COMMENT ON COLUMN action_types.submission_criteria IS
    'Typed SubmissionNode AST evaluated server-side after auth checks and before plan_action commits. Null disables evaluation.';
