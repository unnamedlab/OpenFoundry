//! T6.2 — file format detection.
//!
//! Combines (a) extension-based hints with (b) magic-byte sniffing
//! and exposes the policy that drives the upload handler:
//!
//! * Parquet (`PAR1` footer) → infer schema on upload.
//! * Avro (`Obj\x01` magic)  → infer schema on upload.
//! * CSV / JSON / NDJSON / unknown text → DO NOT infer; the dataset
//!   is treated as **unstructured** until the user applies a schema
//!   via the "Apply schema" tab (see T6.3).
//!
//! The policy mirrors Foundry's recommendation: text uploads land as
//! unstructured datasets and downstream pipelines (or the Apply-schema
//! UI) attach the structured schema.

/// Canonical format identifiers used across the service.
pub const FORMAT_PARQUET: &str = "parquet";
pub const FORMAT_AVRO: &str = "avro";
pub const FORMAT_CSV: &str = "csv";
pub const FORMAT_JSON: &str = "json";
pub const FORMAT_TEXT: &str = "text";
pub const FORMAT_UNKNOWN: &str = "unknown";

/// Map a filename / extension to a canonical format.
pub fn detect_from_filename(filename: &str) -> &'static str {
    let ext = filename
        .rsplit('.')
        .next()
        .unwrap_or("")
        .to_ascii_lowercase();
    match ext.as_str() {
        "parquet" | "pq" => FORMAT_PARQUET,
        "avro" => FORMAT_AVRO,
        "csv" | "tsv" => FORMAT_CSV,
        "json" | "jsonl" | "ndjson" => FORMAT_JSON,
        "txt" => FORMAT_TEXT,
        _ => FORMAT_UNKNOWN,
    }
}

/// Sniff a format from the first bytes of a payload. Returns `None`
/// when the magic isn't recognised so callers can fall back to the
/// extension or the user-declared format.
pub fn detect_from_magic(bytes: &[u8]) -> Option<&'static str> {
    // Parquet writes "PAR1" as both header and footer (4 bytes).
    if bytes.len() >= 4 && &bytes[..4] == b"PAR1" {
        return Some(FORMAT_PARQUET);
    }
    // Avro Object Container Files start with "Obj\x01".
    if bytes.len() >= 4 && &bytes[..4] == b"Obj\x01" {
        return Some(FORMAT_AVRO);
    }
    None
}

/// Whether the upload pipeline should attempt automatic schema
/// inference. Text formats (CSV/JSON/etc.) opt out per Foundry
/// guidance — the dataset is stored unstructured and the user applies
/// a schema explicitly.
pub fn should_infer_schema_on_upload(format: &str) -> bool {
    matches!(format, FORMAT_PARQUET | FORMAT_AVRO)
}

/// Whether a dataset whose declared format is `format` is considered
/// "unstructured" until a schema is applied. Returns true for text
/// formats and for unknown blobs.
pub fn is_unstructured(format: &str) -> bool {
    !should_infer_schema_on_upload(format)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn extension_hints() {
        assert_eq!(detect_from_filename("data.parquet"), FORMAT_PARQUET);
        assert_eq!(detect_from_filename("data.PQ"), FORMAT_PARQUET);
        assert_eq!(detect_from_filename("rows.csv"), FORMAT_CSV);
        assert_eq!(detect_from_filename("rows.TSV"), FORMAT_CSV);
        assert_eq!(detect_from_filename("rows.ndjson"), FORMAT_JSON);
        assert_eq!(detect_from_filename("rows.avro"), FORMAT_AVRO);
        assert_eq!(detect_from_filename("notes.txt"), FORMAT_TEXT);
        assert_eq!(detect_from_filename("blob"), FORMAT_UNKNOWN);
    }

    #[test]
    fn magic_bytes_detect_parquet_and_avro() {
        assert_eq!(detect_from_magic(b"PAR1xxxx"), Some(FORMAT_PARQUET));
        assert_eq!(detect_from_magic(b"Obj\x01zzz"), Some(FORMAT_AVRO));
        assert_eq!(detect_from_magic(b"hello,world\n"), None);
        assert_eq!(detect_from_magic(b""), None);
    }

    #[test]
    fn schema_inference_policy_matches_foundry_guidance() {
        assert!(should_infer_schema_on_upload(FORMAT_PARQUET));
        assert!(should_infer_schema_on_upload(FORMAT_AVRO));
        assert!(!should_infer_schema_on_upload(FORMAT_CSV));
        assert!(!should_infer_schema_on_upload(FORMAT_JSON));
        assert!(!should_infer_schema_on_upload(FORMAT_TEXT));
        assert!(!should_infer_schema_on_upload(FORMAT_UNKNOWN));

        assert!(is_unstructured(FORMAT_CSV));
        assert!(!is_unstructured(FORMAT_PARQUET));
    }
}
