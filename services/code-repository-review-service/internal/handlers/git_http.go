package handlers

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

const gitHTTPPrefix = "/v1/code-repos/git/"

// ServeGitHTTP authenticates Git Smart HTTP requests with OIDC JWTs supplied
// either as Authorization: Bearer <token> or as the Basic-auth password used by
// standard Git credential helpers for HTTPS remotes.
func (h *Handlers) ServeGitHTTP(jwt *authmw.JWTConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := gitClaimsFromRequest(r, jwt)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="OpenFoundry Code Repositories"`)
			writeError(w, http.StatusUnauthorized, "missing or invalid OIDC token")
			return
		}
		if h.GitStore == nil {
			writeError(w, http.StatusInternalServerError, "git store is not configured")
			return
		}
		gitPath, ok := gitPathFromRequest(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "git repository path not found")
			return
		}
		repositoryID, ok := gitRepositoryIDFromPath(gitPath)
		if !ok {
			writeError(w, http.StatusNotFound, "git repository path not found")
			return
		}
		repositoryRepo := h.codeRepositoryRepo()
		if repositoryRepo == nil {
			writeError(w, http.StatusInternalServerError, "code repository repo is not configured")
			return
		}
		repository, err := repositoryRepo.Get(r.Context(), repositoryID, false)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if repository == nil {
			writeError(w, http.StatusNotFound, "code repository not found")
			return
		}
		if _, err := h.ensureGitBackend(r, repositoryRepo, *repository); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		ctx := authmw.ContextWithClaims(r.Context(), claims)
		h.GitStore.ServeHTTP(w, r.WithContext(ctx), gitPath, claims.Sub.String())
	}
}

func gitClaimsFromRequest(r *http.Request, jwt *authmw.JWTConfig) (*authmw.Claims, bool) {
	if jwt == nil {
		return nil, false
	}
	if token, ok := bearerToken(r.Header.Get("Authorization")); ok {
		claims, err := authmw.DecodeToken(jwt, token)
		return claims, err == nil
	}
	if token, ok := basicPasswordToken(r.Header.Get("Authorization")); ok {
		claims, err := authmw.DecodeToken(jwt, token)
		return claims, err == nil
	}
	return nil, false
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}

func basicPasswordToken(header string) (string, bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header[len(prefix):]))
	if err != nil {
		return "", false
	}
	_, password, ok := strings.Cut(string(decoded), ":")
	password = strings.TrimSpace(password)
	return password, ok && password != ""
}

func gitPathFromRequest(path string) (string, bool) {
	if !strings.HasPrefix(path, gitHTTPPrefix) {
		return "", false
	}
	gitPath := strings.TrimPrefix(path, gitHTTPPrefix)
	if gitPath == "" || !strings.Contains(gitPath, ".git") || strings.Contains(gitPath, "..") {
		return "", false
	}
	return "/" + gitPath, true
}

func gitRepositoryIDFromPath(gitPath string) (uuid.UUID, bool) {
	trimmed := strings.TrimPrefix(gitPath, "/")
	repositoryPart, _, _ := strings.Cut(trimmed, "/")
	idText, ok := strings.CutSuffix(repositoryPart, ".git")
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(idText)
	return id, err == nil
}
