package cedarauthz

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// engineCtxKey carries the [*AuthzEngine] through the request context.
// Internal — callers use [WithEngine] / [EngineFromContext].
type engineCtxKey struct{}

// WithEngine returns a copy of ctx that carries `engine` so downstream
// middleware + handlers can read it via [EngineFromContext]. Mirrors
// the Rust `Router::layer(Extension(engine))` pattern.
func WithEngine(ctx context.Context, engine *AuthzEngine) context.Context {
	return context.WithValue(ctx, engineCtxKey{}, engine)
}

// EngineFromContext extracts the engine attached by [WithEngine].
// Returns false when no engine is present (handler must 500).
func EngineFromContext(ctx context.Context) (*AuthzEngine, bool) {
	e, ok := ctx.Value(engineCtxKey{}).(*AuthzEngine)
	return e, ok
}

// EngineMiddleware returns a chi-compatible middleware that injects the
// engine into every request context. Mount once at the router root,
// after auth-middleware so [authmw.Claims] is also present.
func EngineMiddleware(engine *AuthzEngine) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(WithEngine(r.Context(), engine)))
		})
	}
}

// ResourceFunc extracts the (resource UID, related entities) pair from
// a request. The returned entities should include the resource itself
// AND any related entities the policy may walk (inherited markings,
// parent groups, …) — the Rust [AuthzResource] trait collapses these
// into one method.
//
// Returning an error short-circuits the guard with 400 Bad Request.
type ResourceFunc func(r *http.Request) (cedar.EntityUID, []cedar.Entity, error)

// guardErrorBody is the JSON shape used for guard rejections. Mirrors
// the auth-middleware error envelope so API clients see a single
// schema across the auth chain.
type guardErrorBody struct {
	Error string `json:"error"`
}

func writeGuardError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(guardErrorBody{Error: msg})
}

// Guard returns a chi middleware that enforces a single Cedar action
// against the resource extracted from the request.
//
// Wiring (matches the Rust `AuthzGuard<Action, Resource>` extractor):
//
//  1. Mount [authmw.Middleware] so claims are in context.
//  2. Mount [EngineMiddleware] so the engine is in context.
//  3. Mount Guard(action, resourceFn) on the routes that need it.
//
// Behaviour:
//   - 401 Unauthorized when no [authmw.Claims] are in context.
//   - 500 Internal Server Error when no engine is in context.
//   - 400 Bad Request when `resourceFn` returns an error.
//   - 403 Forbidden when the engine denies.
//   - Pass-through (next.ServeHTTP) when the engine allows; the outcome
//     is also stashed in context so the wrapped handler can inspect
//     it via [OutcomeFromContext].
func Guard(action cedar.EntityUID, resourceFn ResourceFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := authmw.FromContext(r.Context())
			if !ok {
				writeGuardError(w, http.StatusUnauthorized, "missing Claims")
				return
			}
			engine, ok := EngineFromContext(r.Context())
			if !ok {
				slog.Error("authz guard: no AuthzEngine in context — wire EngineMiddleware")
				writeGuardError(w, http.StatusInternalServerError, "AuthzEngine not configured")
				return
			}

			resourceUID, resourceEntities, err := resourceFn(r)
			if err != nil {
				writeGuardError(w, http.StatusBadRequest, err.Error())
				return
			}

			principalEntity := PrincipalEntityFromClaims(claims)
			ents := cedar.EntityMap{principalEntity.UID: principalEntity}
			for _, e := range resourceEntities {
				ents[e.UID] = e
			}

			outcome, err := engine.Authorize(
				r.Context(),
				principalEntity.UID,
				action,
				resourceUID,
				cedar.NewRecord(cedar.RecordMap{}),
				ents,
			)
			if err != nil {
				slog.Error("authz guard: engine error", slog.String("error", err.Error()))
				writeGuardError(w, http.StatusInternalServerError, "authz error: "+err.Error())
				return
			}
			if !outcome.IsAllow() {
				writeGuardError(w, http.StatusForbidden, "forbidden")
				return
			}
			ctx := withOutcome(r.Context(), outcome)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// outcomeCtxKey carries the [*AuthorizeOutcome] forward so the handler
// can inspect matching policy ids / diagnostics without re-running the
// engine.
type outcomeCtxKey struct{}

func withOutcome(ctx context.Context, o *AuthorizeOutcome) context.Context {
	return context.WithValue(ctx, outcomeCtxKey{}, o)
}

// OutcomeFromContext returns the engine outcome stashed by [Guard].
// Useful when the handler needs to log policy ids alongside its own
// audit trail.
func OutcomeFromContext(ctx context.Context) (*AuthorizeOutcome, bool) {
	o, ok := ctx.Value(outcomeCtxKey{}).(*AuthorizeOutcome)
	return o, ok
}

// PrincipalEntityFromClaims builds the canonical Cedar User entity
// from a JWT claims set, mirroring the Rust impl of the same name:
//
//   - UID type:   `User`
//   - UID id:     `Claims.Sub` (UUID v7)
//   - tenant:     `Claims.OrgID` (or "" if absent)
//   - roles:      `Claims.Roles`
//   - clearances: `Claims.SessionScope.AllowedMarkings`, materialised
//     as `Marking::"<id>"` entity references.
//
// Locked: the schema's `User` entity declares `tenant` (String),
// `roles` (Set<String>), `clearances` (Set<Marking>) — these three
// attributes MUST match exactly or schema validation fails on every
// request.
func PrincipalEntityFromClaims(claims *authmw.Claims) cedar.Entity {
	userUID := MustEntityUID("User", claims.Sub.String())

	tenant := ""
	if claims.OrgID != nil {
		tenant = claims.OrgID.String()
	}

	clearances := make([]cedar.Value, 0)
	if claims.SessionScope != nil {
		for _, m := range claims.SessionScope.AllowedMarkings {
			clearances = append(clearances, MustEntityUID("Marking", m))
		}
	}
	roles := make([]cedar.Value, 0, len(claims.Roles))
	for _, r := range claims.Roles {
		roles = append(roles, cedar.String(r))
	}

	return cedar.Entity{
		UID: userUID,
		Attributes: cedar.NewRecord(cedar.RecordMap{
			"tenant":     cedar.String(tenant),
			"clearances": cedar.NewSet(clearances...),
			"roles":      cedar.NewSet(roles...),
		}),
	}
}

// MustEntityUID builds an [cedar.EntityUID] from `(typeName, id)`.
//
// Both arguments are statically known in every call site we have today;
// we panic on malformed input rather than thread an error up the stack.
// The Rust helper does the same.
func MustEntityUID(typeName, id string) cedar.EntityUID {
	if typeName == "" {
		panic("cedarauthz: entity type name is empty")
	}
	if _, err := uuid.Parse(id); err == nil {
		// uuid string is always a valid id; fast path.
		return types.NewEntityUID(types.EntityType(typeName), types.String(id))
	}
	if id == "" {
		panic("cedarauthz: entity id is empty")
	}
	return types.NewEntityUID(types.EntityType(typeName), types.String(id))
}

// errMissingResource is the canonical error a [ResourceFunc] returns
// when the request URL does not carry the expected path/query parameter.
// Tests use it; production callers can return their own.
var errMissingResource = errors.New("missing resource parameter")

// ErrMissingResource is the exported sentinel for [errMissingResource].
var ErrMissingResource = errMissingResource
