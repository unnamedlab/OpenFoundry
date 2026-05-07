// S8 / ADR-0030 (B15) — absorbed crates. Their domains land here as
// dead-code library namespaces until the consolidated binary's main
// is wired in a follow-up.
#[allow(dead_code)]
mod lineage_deletion;
#[allow(dead_code)]
mod retention_policy;
#[allow(dead_code)]
mod sds;

fn main() {}
