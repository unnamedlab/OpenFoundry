// S8.1.b — `widget-registry-service` consolidated into this service.
// `widgets` re-exports the widget-catalog model still owned by the
// `app-builder-service` source tree (until app-builder is retired in
// its own R-prompt).

pub mod composition;
pub mod widgets;
