// Package authmw is the Go port of libs/auth-middleware from the
// Rust workspace.
//
// What this package owns:
//
//   - [Claims] / [SessionScope] wire-compatible with the Rust crate.
//   - [JWTConfig] with HS256 + RS256 signing/verification, env-driven
//     [JWTConfig.WithEnvDefaults], and unattended secret resolution
//     ([LoadOrGenerate], [ResolveUnattended]).
//   - Token factory helpers ([BuildAccessClaims], [BuildRefreshClaims],
//     [BuildAPIKeyClaims], plus the *WithScope variants).
//   - HTTP middleware: [Middleware] (bearer extraction + JWT
//     validation + context injection) and [Required].
//   - RBAC middleware: [RequireRoles], [RequireAdmin],
//     [RequirePermissions] plus the well-known role constants.
//   - Marking enforcement: [CallerClearances],
//     [MarkingNameResolver], [StaticMarkingNameResolver],
//     [EnforceMarkings] backed by core-models/security.MarkingID.
//   - Row-level-security primitives: [RLSContext] and the
//     [RLSContext.OrgFilter] / [RLSContext.OwnerOrOrgFilter] SQL
//     fragment helpers.
//   - Tenant resolution: [TenantContext] / [TenantContextFromClaims]
//     with the same standard / team / enterprise quota tiers as Rust.
package authmw
