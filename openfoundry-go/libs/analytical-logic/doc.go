// Package analyticallogic owns the analytical_expressions and
// analytical_expression_versions tables that used to belong to the
// standalone analytical-logic-service. Mirrors libs/analytical-logic
// from the Rust workspace verbatim — same model + repo surface, same
// SQL shape, same error variants.
//
// Per ADR-0030 (S8 consolidation) and the S8 task notes for
// sql-bi-gateway-service:
//
//	Analytical-logic son expresiones reutilizables: deben ser una crate
//	interna, no rutas HTTP duplicadas.
//
// Consumers (today: sql-bi-gateway-service when it lands; tomorrow: any
// service that needs to look up or persist a saved expression) embed
// this package and call into AnalyticalExpressionRepo directly. There
// is no standalone HTTP surface — the previous /api/v1/analytical-logic
// routes were retired with the source service.
//
// The schema is the same one shipped by
// migrations/0001_analytical_expressions_foundation.sql in the Rust
// workspace (also installed into
// services/sql-bi-gateway-service/migrations/ so the gateway's
// pre-install Helm Job applies it as part of the consolidated bounded
// context).
package analyticallogic
