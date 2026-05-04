//! Foundry "Set up a media set sync" filter contract.
//!
//! Validates the four filter knobs the docs call out
//! (`exclude_already_synced`, `path_glob`, `file_size_limit`,
//! `ignore_unmatched_schema`) against the per-file decision function in
//! `connector_management_service::media_set_sync`.
//!
//! Reusing the lib surface (no Postgres / HTTP) keeps this fast.

use std::collections::HashSet;

use connector_management_service::media_set_sync::{
    MediaSetSyncConfig, MediaSetSyncFilters, MediaSetSyncKind, SourceFile, SyncDecision,
    classify_batch, decide,
};

fn cfg(filters: MediaSetSyncFilters, kind: MediaSetSyncKind) -> MediaSetSyncConfig {
    MediaSetSyncConfig {
        kind,
        target_media_set_rid: "ri.foundry.main.media_set.018f0000-aaaa-bbbb-cccc-000000000001"
            .to_string(),
        subfolder: String::new(),
        filters,
        schedule_cron: None,
    }
}

fn file(path: &str, size: u64, mime: &str) -> SourceFile {
    SourceFile {
        path: path.into(),
        size_bytes: size,
        mime_type: mime.into(),
    }
}

#[test]
fn config_validates_target_rid_and_glob_and_cron() {
    let bad_rid = MediaSetSyncConfig {
        target_media_set_rid: "not-a-rid".into(),
        ..cfg(
            MediaSetSyncFilters::default(),
            MediaSetSyncKind::MediaSetSync,
        )
    };
    let errs = bad_rid.validate();
    assert!(
        errs.iter().any(|e| e.contains("target_media_set_rid")),
        "{errs:?}"
    );

    let bad_glob = MediaSetSyncConfig {
        filters: MediaSetSyncFilters {
            path_glob: Some("[unclosed".into()),
            ..Default::default()
        },
        ..cfg(
            MediaSetSyncFilters::default(),
            MediaSetSyncKind::MediaSetSync,
        )
    };
    let errs = bad_glob.validate();
    assert!(errs.iter().any(|e| e.contains("path_glob")), "{errs:?}");

    let bad_cron = MediaSetSyncConfig {
        schedule_cron: Some("not a cron".into()),
        ..cfg(
            MediaSetSyncFilters::default(),
            MediaSetSyncKind::MediaSetSync,
        )
    };
    let errs = bad_cron.validate();
    assert!(errs.iter().any(|e| e.contains("schedule_cron")), "{errs:?}");

    let zero_limit = MediaSetSyncConfig {
        filters: MediaSetSyncFilters {
            file_size_limit: Some(0),
            ..Default::default()
        },
        ..cfg(
            MediaSetSyncFilters::default(),
            MediaSetSyncKind::MediaSetSync,
        )
    };
    let errs = zero_limit.validate();
    assert!(
        errs.iter().any(|e| e.contains("file_size_limit")),
        "{errs:?}"
    );

    let ok = cfg(
        MediaSetSyncFilters {
            path_glob: Some("**/*.png".into()),
            file_size_limit: Some(10_000_000),
            ..Default::default()
        },
        MediaSetSyncKind::MediaSetSync,
    );
    assert!(ok.validate().is_empty(), "{:?}", ok.validate());
}

#[test]
fn exclude_already_synced_skips_known_paths() {
    let cfg = cfg(
        MediaSetSyncFilters {
            exclude_already_synced: true,
            ..Default::default()
        },
        MediaSetSyncKind::MediaSetSync,
    );
    let mut already = HashSet::new();
    already.insert("logo.png".to_string());

    let matcher = cfg.matcher().unwrap();
    let allowed = vec!["image/png".to_string()];

    assert_eq!(
        decide(
            &cfg,
            &file("logo.png", 1, "image/png"),
            &already,
            &allowed,
            matcher.as_ref()
        ),
        SyncDecision::Skip,
        "already-synced file should be skipped"
    );
    assert_eq!(
        decide(
            &cfg,
            &file("new.png", 1, "image/png"),
            &already,
            &allowed,
            matcher.as_ref()
        ),
        SyncDecision::Accept,
        "fresh file should be accepted"
    );
}

#[test]
fn path_glob_filters_by_pattern() {
    let cfg = cfg(
        MediaSetSyncFilters {
            path_glob: Some("**/*.png".into()),
            ..Default::default()
        },
        MediaSetSyncKind::MediaSetSync,
    );
    let matcher = cfg.matcher().unwrap();

    assert_eq!(
        decide(
            &cfg,
            &file("a/b/c.png", 1, "image/png"),
            &HashSet::new(),
            &[],
            matcher.as_ref()
        ),
        SyncDecision::Accept
    );
    assert_eq!(
        decide(
            &cfg,
            &file("a/b/c.jpg", 1, "image/jpeg"),
            &HashSet::new(),
            &[],
            matcher.as_ref()
        ),
        SyncDecision::Skip,
        "jpg should miss the **/*.png glob"
    );
}

#[test]
fn file_size_limit_skips_oversize_files() {
    let cfg = cfg(
        MediaSetSyncFilters {
            file_size_limit: Some(1024),
            ..Default::default()
        },
        MediaSetSyncKind::MediaSetSync,
    );
    let matcher = cfg.matcher().unwrap();

    assert_eq!(
        decide(
            &cfg,
            &file("ok.png", 1024, "image/png"),
            &HashSet::new(),
            &[],
            matcher.as_ref()
        ),
        SyncDecision::Accept,
        "exactly at the limit is allowed"
    );
    assert_eq!(
        decide(
            &cfg,
            &file("oversize.png", 1025, "image/png"),
            &HashSet::new(),
            &[],
            matcher.as_ref()
        ),
        SyncDecision::Skip
    );
}

#[test]
fn ignore_unmatched_schema_toggles_skip_vs_error() {
    // ignore = true → silently skip schema-mismatched files.
    let lenient = cfg(
        MediaSetSyncFilters {
            ignore_unmatched_schema: true,
            ..Default::default()
        },
        MediaSetSyncKind::MediaSetSync,
    );
    let matcher = lenient.matcher().unwrap();
    let allowed_image_only = vec!["image/png".to_string()];
    assert_eq!(
        decide(
            &lenient,
            &file("audio.mp3", 1, "audio/mp3"),
            &HashSet::new(),
            &allowed_image_only,
            matcher.as_ref(),
        ),
        SyncDecision::Skip,
        "with ignore_unmatched_schema=true a non-IMAGE MIME is skipped silently"
    );

    // ignore = false → surface as schema mismatch (which the executor
    // converts into a per-file error record).
    let strict = cfg(
        MediaSetSyncFilters {
            ignore_unmatched_schema: false,
            ..Default::default()
        },
        MediaSetSyncKind::MediaSetSync,
    );
    let matcher = strict.matcher().unwrap();
    assert_eq!(
        decide(
            &strict,
            &file("audio.mp3", 1, "audio/mp3"),
            &HashSet::new(),
            &allowed_image_only,
            matcher.as_ref(),
        ),
        SyncDecision::SchemaMismatch
    );
}

#[test]
fn classify_batch_aggregates_decisions_into_stats() {
    // Glob deliberately includes `.mp4` so a video file *passes* the
    // path filter and reaches the MIME check — that is the only way to
    // exercise SchemaMismatch (the path-glob check fires before the
    // MIME check, so a non-glob-matching file is just Skip).
    let cfg = cfg(
        MediaSetSyncFilters {
            exclude_already_synced: true,
            path_glob: Some("**/*.{png,jpg,mp4}".into()),
            file_size_limit: Some(5_000_000),
            ignore_unmatched_schema: false,
        },
        MediaSetSyncKind::VirtualMediaSetSync,
    );
    let mut already = HashSet::new();
    already.insert("logo.png".to_string());
    let allowed = vec!["image/png".to_string(), "image/jpeg".to_string()];

    let files = vec![
        file("logo.png", 1, "image/png"),     // skipped: already synced
        file("hero.jpg", 1024, "image/jpeg"), // accepted
        file("docs/big.png", 9_000_000, "image/png"), // skipped: oversize
        file("data/clip.mp4", 1024, "video/mp4"), // schema mismatch: glob ok, MIME not allowed
        file("docs/manual.pdf", 1024, "application/pdf"), // skipped: glob mismatch
        file("ok.png", 2048, "image/png"),    // accepted
    ];

    let (per_file, stats) = classify_batch(&cfg, &files, &already, &allowed).unwrap();
    assert_eq!(stats.accepted, 2, "{stats:?}");
    assert_eq!(stats.schema_mismatched, 1, "{stats:?}");
    // All other 3 fall into "skipped" (already-synced, oversize, glob mismatch).
    assert_eq!(stats.skipped, 3, "{stats:?}");
    assert_eq!(per_file.len(), files.len());
}
