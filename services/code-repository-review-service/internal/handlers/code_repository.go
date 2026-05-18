package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/code-repository-review-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/code-repository-review-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/code-repository-review-service/internal/templates"
)

func (h *Handlers) codeRepositoryRepo() *repo.CodeRepositoryRepo {
	if h.CodeRepositories != nil {
		return h.CodeRepositories
	}
	if h.Pool == nil {
		return nil
	}
	return &repo.CodeRepositoryRepo{Pool: h.Pool}
}

func (h *Handlers) ListCodeRepositoryTemplates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": templates.List()})
}

func (h *Handlers) ListCodeRepositories(w http.ResponseWriter, r *http.Request) {
	repositoryRepo := h.codeRepositoryRepo()
	if repositoryRepo == nil {
		writeError(w, http.StatusInternalServerError, "code repository repo is not configured")
		return
	}
	includeTrashed := r.URL.Query().Get("include_trashed") == "true"
	rows, err := repositoryRepo.List(r.Context(), includeTrashed)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handlers) CreateCodeRepository(w http.ResponseWriter, r *http.Request) {
	repositoryRepo := h.codeRepositoryRepo()
	if repositoryRepo == nil {
		writeError(w, http.StatusInternalServerError, "code repository repo is not configured")
		return
	}
	var body models.CreateCodeRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	body.LanguageTemplate = templates.NormalizeID(body.LanguageTemplate)
	if _, ok := templates.Get(body.LanguageTemplate); !ok {
		writeError(w, http.StatusBadRequest, "unsupported language_template")
		return
	}
	actor := h.requestActor(r)
	repository, err := repositoryRepo.Create(r.Context(), body, actor)
	if err != nil {
		if repo.IsUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "repository name, slug or rid already in use"})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	repository, err = h.ensureGitBackend(r, repositoryRepo, repository)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.seedRepositoryTemplate(r, repository); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, repository)
}

func (h *Handlers) GetCodeRepository(w http.ResponseWriter, r *http.Request) {
	repositoryRepo := h.codeRepositoryRepo()
	if repositoryRepo == nil {
		writeError(w, http.StatusInternalServerError, "code repository repo is not configured")
		return
	}
	id, ok := codeRepositoryID(w, r)
	if !ok {
		return
	}
	repository, err := repositoryRepo.Get(r.Context(), id, r.URL.Query().Get("include_trashed") == "true")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if repository == nil {
		writeError(w, http.StatusNotFound, "code repository not found")
		return
	}
	writeJSON(w, http.StatusOK, repository)
}

func (h *Handlers) UpdateCodeRepository(w http.ResponseWriter, r *http.Request) {
	repositoryRepo := h.codeRepositoryRepo()
	if repositoryRepo == nil {
		writeError(w, http.StatusInternalServerError, "code repository repo is not configured")
		return
	}
	id, ok := codeRepositoryID(w, r)
	if !ok {
		return
	}
	var body models.UpdateCodeRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	repository, err := repositoryRepo.Update(r.Context(), id, body)
	if err != nil {
		if repo.IsUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "repository name, slug or rid already in use"})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if repository == nil {
		writeError(w, http.StatusNotFound, "code repository not found")
		return
	}
	writeJSON(w, http.StatusOK, repository)
}

func (h *Handlers) TrashCodeRepository(w http.ResponseWriter, r *http.Request) {
	repositoryRepo := h.codeRepositoryRepo()
	if repositoryRepo == nil {
		writeError(w, http.StatusInternalServerError, "code repository repo is not configured")
		return
	}
	id, ok := codeRepositoryID(w, r)
	if !ok {
		return
	}
	repository, err := repositoryRepo.Trash(r.Context(), id, h.requestActor(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if repository == nil {
		writeError(w, http.StatusNotFound, "code repository not found")
		return
	}
	writeJSON(w, http.StatusOK, repository)
}

func (h *Handlers) RestoreCodeRepository(w http.ResponseWriter, r *http.Request) {
	repositoryRepo := h.codeRepositoryRepo()
	if repositoryRepo == nil {
		writeError(w, http.StatusInternalServerError, "code repository repo is not configured")
		return
	}
	id, ok := codeRepositoryID(w, r)
	if !ok {
		return
	}
	repository, err := repositoryRepo.Restore(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if repository == nil {
		writeError(w, http.StatusNotFound, "code repository not found")
		return
	}
	writeJSON(w, http.StatusOK, repository)
}

func (h *Handlers) MoveCodeRepository(w http.ResponseWriter, r *http.Request) {
	repositoryRepo := h.codeRepositoryRepo()
	if repositoryRepo == nil {
		writeError(w, http.StatusInternalServerError, "code repository repo is not configured")
		return
	}
	id, ok := codeRepositoryID(w, r)
	if !ok {
		return
	}
	var body models.MoveCodeRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	repository, err := repositoryRepo.Move(r.Context(), id, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if repository == nil {
		writeError(w, http.StatusNotFound, "code repository not found")
		return
	}
	writeJSON(w, http.StatusOK, repository)
}

func (h *Handlers) RenameCodeRepository(w http.ResponseWriter, r *http.Request) {
	repositoryRepo := h.codeRepositoryRepo()
	if repositoryRepo == nil {
		writeError(w, http.StatusInternalServerError, "code repository repo is not configured")
		return
	}
	id, ok := codeRepositoryID(w, r)
	if !ok {
		return
	}
	var body models.RenameCodeRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	repository, err := repositoryRepo.Rename(r.Context(), id, body)
	if err != nil {
		if repo.IsUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "repository name, slug or rid already in use"})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if repository == nil {
		writeError(w, http.StatusNotFound, "code repository not found")
		return
	}
	writeJSON(w, http.StatusOK, repository)
}

func (h *Handlers) ListCodeRepositoryBranches(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.loadCodeRepositoryForGit(w, r)
	if !ok {
		return
	}
	branches, err := h.GitStore.ListBranches(r.Context(), *repository)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": branches})
}

func (h *Handlers) CreateCodeRepositoryBranch(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.loadCodeRepositoryForGit(w, r)
	if !ok {
		return
	}
	var body models.CreateRepositoryBranchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	branch, err := h.GitStore.CreateBranch(r.Context(), *repository, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, branch)
}

func (h *Handlers) DeleteCodeRepositoryBranch(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.loadCodeRepositoryForGit(w, r)
	if !ok {
		return
	}
	var body models.DeleteRepositoryBranchRequest
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := h.GitStore.DeleteBranch(r.Context(), *repository, chi.URLParam(r, "branch"), body.Force); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": chi.URLParam(r, "branch")})
}

func (h *Handlers) MergeCodeRepositoryBranch(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.loadCodeRepositoryForGit(w, r)
	if !ok {
		return
	}
	var body models.MergeRepositoryBranchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body.AuthorName, body.AuthorEmail = h.requestGitAuthor(r)
	branch, err := h.GitStore.MergeBranch(r.Context(), *repository, chi.URLParam(r, "branch"), body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, branch)
}

func (h *Handlers) ListCodeRepositoryTags(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.loadCodeRepositoryForGit(w, r)
	if !ok {
		return
	}
	tags, err := h.GitStore.ListTags(r.Context(), *repository)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": tags})
}

func (h *Handlers) CreateCodeRepositoryTag(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.loadCodeRepositoryForGit(w, r)
	if !ok {
		return
	}
	var body models.CreateRepositoryTagRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Protected && !requestCanManageProtectedTags(r) {
		writeError(w, http.StatusForbidden, "protected tag creation requires code_repository:manage_protected_tags")
		return
	}
	body.TaggerName, body.TaggerEmail = h.requestGitAuthor(r)
	tag, err := h.GitStore.CreateTag(r.Context(), *repository, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tag)
}

func requestCanManageProtectedTags(r *http.Request) bool {
	claims, ok := authmw.FromContext(r.Context())
	return ok && claims.HasPermission("code_repository", "manage_protected_tags")
}

func (h *Handlers) ListCodeRepositoryCommits(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.loadCodeRepositoryForGit(w, r)
	if !ok {
		return
	}
	commits, err := h.GitStore.ListCommits(r.Context(), *repository, r.URL.Query().Get("branch"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": commits})
}

func (h *Handlers) CreateCodeRepositoryCommit(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.loadCodeRepositoryForGit(w, r)
	if !ok {
		return
	}
	var body models.CreateRepositoryCommitRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	actorName, actorEmail := h.requestGitAuthor(r)
	body.AuthorName = actorName
	body.AuthorEmail = actorEmail
	for i := range body.Files {
		body.Files[i].AuthorName = actorName
		body.Files[i].AuthorEmail = actorEmail
	}
	commit, err := h.GitStore.CreateCommit(r.Context(), *repository, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, commit)
}

func (h *Handlers) ListCodeRepositoryFiles(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.loadCodeRepositoryForGit(w, r)
	if !ok {
		return
	}
	if h.GitStore == nil {
		writeError(w, http.StatusInternalServerError, "git store is not configured")
		return
	}
	files, err := h.GitStore.ListFiles(r.Context(), *repository, r.URL.Query().Get("branch"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": files})
}

func (h *Handlers) MutateCodeRepositoryFile(w http.ResponseWriter, r *http.Request) {
	repository, ok := h.loadCodeRepositoryForGit(w, r)
	if !ok {
		return
	}
	if h.GitStore == nil {
		writeError(w, http.StatusInternalServerError, "git store is not configured")
		return
	}
	var body models.MutateRepositoryFileRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	authorName, authorEmail := h.requestGitAuthor(r)
	body.AuthorName = authorName
	body.AuthorEmail = authorEmail
	files, err := h.GitStore.MutateFile(r.Context(), *repository, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": files})
}

func (h *Handlers) loadCodeRepositoryForGit(w http.ResponseWriter, r *http.Request) (*models.CodeRepository, bool) {
	repositoryRepo := h.codeRepositoryRepo()
	if repositoryRepo == nil {
		writeError(w, http.StatusInternalServerError, "code repository repo is not configured")
		return nil, false
	}
	id, ok := codeRepositoryID(w, r)
	if !ok {
		return nil, false
	}
	repository, err := repositoryRepo.Get(r.Context(), id, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if repository == nil {
		writeError(w, http.StatusNotFound, "code repository not found")
		return nil, false
	}
	ensured, err := h.ensureGitBackend(r, repositoryRepo, *repository)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	return &ensured, true
}

func (h *Handlers) GetCodeRepositoryGit(w http.ResponseWriter, r *http.Request) {
	repositoryRepo := h.codeRepositoryRepo()
	if repositoryRepo == nil {
		writeError(w, http.StatusInternalServerError, "code repository repo is not configured")
		return
	}
	id, ok := codeRepositoryID(w, r)
	if !ok {
		return
	}
	repository, err := repositoryRepo.Get(r.Context(), id, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if repository == nil {
		writeError(w, http.StatusNotFound, "code repository not found")
		return
	}
	ensured, err := h.ensureGitBackend(r, repositoryRepo, *repository)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"repository_id":    ensured.ID,
		"storage_path":     ensured.GitStoragePath,
		"http_url":         ensured.GitHTTPURL,
		"ssh_url":          ensured.GitSSHURL,
		"ssh_enabled":      ensured.GitSSHEnabled,
		"default_branch":   ensured.DefaultBranch,
		"oidc_auth_scheme": "basic-password-or-bearer-token",
	})
}

func (h *Handlers) ensureGitBackend(r *http.Request, repositoryRepo *repo.CodeRepositoryRepo, repository models.CodeRepository) (models.CodeRepository, error) {
	if h.GitStore == nil {
		return repository, nil
	}
	meta, err := h.GitStore.Ensure(r.Context(), repository)
	if err != nil {
		return repository, err
	}
	updated, err := repositoryRepo.AttachGitBackend(r.Context(), repository.ID, meta.StoragePath, meta.HTTPURL, meta.SSHURL, meta.SSHEnabled)
	if err != nil {
		return repository, err
	}
	if updated == nil {
		return repository, nil
	}
	return *updated, nil
}

func (h *Handlers) seedRepositoryTemplate(r *http.Request, repository models.CodeRepository) error {
	if h.GitStore == nil {
		return nil
	}
	tmpl, ok := templates.Get(repository.LanguageTemplate)
	if !ok {
		return nil
	}
	return h.GitStore.SeedFiles(r.Context(), repository, tmpl.Files, "OpenFoundry Templates", "templates@openfoundry.local")
}

func codeRepositoryID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return uuid.Nil, false
	}
	return id, true
}
