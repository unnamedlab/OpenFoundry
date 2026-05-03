// Bootstrap is intentionally empty until S0.7.h wires the HTTP server.
// The submodules below are included so they participate in `cargo
// build`/`cargo check` even though they are not yet called from `main`.
#[allow(dead_code)]
mod audit_wiring;

fn main() {}
