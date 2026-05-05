pub mod compiler;
pub mod crud;
pub mod job_specs;
pub mod parameterized;
pub mod validate_node;
// Authoring service does NOT expose run / scheduler endpoints (Foundry
// separates Pipeline Builder from the Builds queue and Schedules apps).
// These modules stay declared so `pipeline-build-service` and
// `pipeline-schedule-service` can `#[path]`-include them; they are unused
// from the authoring binary itself.
#[allow(dead_code)]
pub mod execute;
#[allow(dead_code)]
pub mod runs;
