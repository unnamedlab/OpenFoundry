// S8.1.a — `health-check-service` consolidated into this service.
// `health_checks` exposes the health-check CRUD endpoints
// previously owned by the deleted `health-check-service`.

pub mod telemetry;
pub mod health_checks;
