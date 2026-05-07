// Bootstrap is intentionally empty until S0.7.h wires the HTTP server.
// The submodules below are included so they participate in `cargo
// build`/`cargo check` even though they are not yet called from `main`.
#[allow(dead_code)]
mod audit_wiring;

// S8 / ADR-0030 (B14) — absorbed crates. Their domains land here as
// dead-code library namespaces until the consolidated binary's main
// is wired in a follow-up.
#[allow(dead_code)]
mod checkpoints_purpose;
#[allow(dead_code)]
mod cipher;
#[allow(dead_code)]
mod network_boundary;
#[allow(dead_code)]
mod security_governance;

fn main() {}
