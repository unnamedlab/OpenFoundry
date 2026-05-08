package audittrail

// middleware.go ports libs/audit-trail/src/middleware.rs.
//
// HTTP middleware that emits one structured log record per request
// for the audit-compliance collector. The Rust crate uses
// `tracing::info!(target = "audit", …)`; Go has no notion of a
// tracing target, so we tag records with `category=audit` — the
// collector's slog handler subscribes to records carrying that key.
//
// Middleware shape is the chi-compatible `func(http.Handler) http.Handler`
// already used by `auth-middleware`. Errors from inner handlers
// propagate unchanged so retry / fallback middleware above keep
// seeing them (matches the Rust contract).

import (
	"log/slog"
	"net/http"
	"time"
)

// Middleware returns a chi-compatible middleware that emits a
// structured `request handled` log record on the default logger
// when each request completes. Mirrors `audit_layer` /
// `AuditLayer` in the Rust crate.
//
// Mount once per Router:
//
//	r := chi.NewRouter()
//	r.Use(audittrail.Middleware())
func Middleware() func(http.Handler) http.Handler {
	return MiddlewareWithLogger(nil)
}

// MiddlewareWithLogger is the explicit-logger variant. Pass nil to
// fall back to slog.Default(). Useful in tests that need to capture
// the emitted record into a buffer.
func MiddlewareWithLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			method := r.Method
			path := r.URL.Path

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			lg := logger
			if lg == nil {
				lg = slog.Default()
			}
			lg.LogAttrs(r.Context(), slog.LevelInfo, "request handled",
				slog.String("category", "audit"),
				slog.String("http_method", method),
				slog.String("http_path", path),
				slog.Int("http_status", rec.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		})
	}
}

// statusRecorder wraps http.ResponseWriter to capture the status
// code emitted by the inner handler. Defaults to 200 when the
// handler writes a body without calling WriteHeader explicitly,
// matching `Response::status()` on the Rust side which also
// defaults to 200.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}
