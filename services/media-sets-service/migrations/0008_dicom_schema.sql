-- H7 — Foundry DICOM media-set schema (`Add a DICOM media set.md`).
--
-- The original `media_sets.schema` CHECK constraint pinned the column
-- to the six pre-DICOM kinds. DICOM is the seventh, gated by Foundry
-- on its own per-page doc + a dedicated access pattern
-- (`render_dicom_image_layer`, 75 cs/GB) and a viewer with
-- window/level + series/instance navigation. We keep DICOM on the
-- same `media_set` row shape — only the CHECK constraint changes.

ALTER TABLE media_sets DROP CONSTRAINT IF EXISTS media_sets_schema_check;
ALTER TABLE media_sets ADD CONSTRAINT media_sets_schema_check
    CHECK (schema IN (
        'IMAGE',
        'AUDIO',
        'VIDEO',
        'DOCUMENT',
        'SPREADSHEET',
        'EMAIL',
        'DICOM'
    ));
