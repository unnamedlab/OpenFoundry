//! Log color coding contract — every emitted level survives the
//! round trip and the canonical mapping (Foundry doc § Live logs:
//! Info=blue, Warn=orange, Error/Fatal=red, Debug/Trace=gray) is
//! handled by the front-end. The backend's job is to expose the
//! canonical level strings; these are validated here.

use pipeline_build_service::domain::logs::LogLevel;

#[test]
fn level_strings_match_foundry_doc_vocabulary() {
    let mappings = [
        (LogLevel::Trace, "TRACE"),
        (LogLevel::Debug, "DEBUG"),
        (LogLevel::Info, "INFO"),
        (LogLevel::Warn, "WARN"),
        (LogLevel::Error, "ERROR"),
        (LogLevel::Fatal, "FATAL"),
    ];
    for (level, expected) in mappings {
        assert_eq!(level.as_str(), expected);
        assert_eq!(level, expected.parse::<LogLevel>().unwrap());
    }
}

#[test]
fn warning_alias_accepted() {
    // The runner integrations come from heterogeneous code paths
    // (Python bridges, tracing layers); accept "WARNING" as an
    // alias for WARN so emitters do not need to re-encode.
    assert_eq!("WARNING".parse::<LogLevel>().unwrap(), LogLevel::Warn);
}

#[test]
fn fatal_collapses_to_tracing_error() {
    // OpenTelemetry exporters cap at ERROR; the doc's FATAL must
    // not silently drop.
    assert_eq!(LogLevel::Fatal.to_tracing(), tracing::Level::ERROR);
}
