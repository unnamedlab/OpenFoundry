// Package pipelineexpression is the Pipeline Builder expression DSL —
// parser, type system, transform catalog and type inference.
//
// Mirrors libs/pipeline-expression from the Rust workspace verbatim:
// same lattice, same operator semantics, same nine canonical
// transforms (cast, title_case, clean_string, filter, join, union,
// group_by, window, pivot), same scalar helper catalog, same
// node-level validation entry point and the same preview engine
// shape.
//
// Pure-Go, no IO — meant to be embedded in pipeline-authoring-service
// and surfaced to the canvas through per-node validation status.
//
// Six surfaces:
//
//   - Types: the runtime type lattice (Boolean / Integer / Long /
//     Double / Decimal / String / Date / Timestamp / Array / Struct /
//     Geometry) plus promotion rules.
//   - Parser: hand-rolled lexer + Pratt-style parser for the
//     Pipeline Builder expression mini-language. No external dep.
//   - Catalog: typed signatures for the canonical transforms and the
//     scalar helpers visible inside expression bodies.
//   - Infer: visits an Expr under a column environment and returns
//     either the inferred PipelineType or a list of TypeError.
//   - Eval: row-level evaluator for parsed Expr trees, used by the
//     preview engine.
//   - NodeCheck: per-node validation entry point used by
//     pipeline-authoring-service to power
//     POST /api/v1/pipelines/{id}/validate.
//   - Preview: forward evaluator for the canonical transforms,
//     designed to be called from the preview handler.
package pipelineexpression
