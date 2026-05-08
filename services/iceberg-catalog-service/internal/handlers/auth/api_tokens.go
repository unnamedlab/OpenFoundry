package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/audit"
	tokendomain "github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/domain/token"
)

// IssueAPITokenStore is the contract `CreateAPITokenHandler` needs from
// the data layer. Implementations live in `internal/repo`; tests
// substitute fakes.
type IssueAPITokenStore interface {
	IssueAPIToken(ctx context.Context, userID uuid.UUID, name string, scopes []string, expiresAt *time.Time) (*tokendomain.APIToken, string, error)
}

// CreateAPITokenRequest is the body of POST /v1/iceberg-clients/api-tokens.
type CreateAPITokenRequest struct {
	Name    string   `json:"name"`
	Scopes  []string `json:"scopes,omitempty"`
	TTLSecs *int64   `json:"ttl_secs,omitempty"`
}

// CreateAPITokenResponse is the response body. `RawToken` is shown to
// the caller exactly once — the catalog never recovers it.
type CreateAPITokenResponse struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	TokenHint string     `json:"token_hint"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	RawToken  string     `json:"raw_token"`
}

// CreateAPITokenHandler returns the chi-compatible HTTP handler for the
// POST endpoint. Authenticates as a Foundry user (regular Foundry JWT
// via the libs/auth-middleware chain) and mints an `ofty_*` token tied
// to that user.
func CreateAPITokenHandler(store IssueAPITokenStore, defaultTTLSecs int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caller, ok := authmw.FromContext(r.Context())
		if !ok {
			writeJSONErr(w, http.StatusUnauthorized, "authentication required")
			return
		}
		var body CreateAPITokenRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		if body.Name == "" {
			writeJSONErr(w, http.StatusBadRequest, "name is required")
			return
		}
		scopes := body.Scopes
		if len(scopes) == 0 {
			scopes = []string{"api:iceberg-read", "api:iceberg-write"}
		}
		ttl := defaultTTLSecs
		if body.TTLSecs != nil {
			ttl = *body.TTLSecs
		}
		var expiresAt *time.Time
		if ttl > 0 {
			t := time.Now().UTC().Add(time.Duration(ttl) * time.Second)
			expiresAt = &t
		}
		record, raw, err := store.IssueAPIToken(r.Context(), caller.Sub, body.Name, scopes, expiresAt)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		audit.APITokenCreated(caller.Sub, record.ID, scopes)
		writeJSON(w, http.StatusOK, CreateAPITokenResponse{
			ID:        record.ID,
			Name:      record.Name,
			TokenHint: record.TokenHint,
			Scopes:    record.Scopes,
			ExpiresAt: record.ExpiresAt,
			CreatedAt: record.CreatedAt,
			RawToken:  raw,
		})
	}
}

// writeJSON / writeJSONErr are shared by api_tokens.go + oauth.go in
// this package. Local copies (rather than reaching into internal/handlers)
// avoid an import cycle.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	typeName := "InternalServerException"
	switch status {
	case http.StatusBadRequest:
		typeName = "BadRequestException"
	case http.StatusUnauthorized:
		typeName = "AuthenticationException"
	case http.StatusForbidden:
		typeName = "ForbiddenException"
	case http.StatusNotFound:
		typeName = "NotFoundException"
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    typeName,
			"code":    status,
		},
	})
}
