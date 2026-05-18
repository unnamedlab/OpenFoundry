package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/code-repository-review-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/code-repository-review-service/internal/templates"
)

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// CodeRepositoryRepo persists the Compass resource facet of Code Repositories.
type CodeRepositoryRepo struct {
	Pool *pgxpool.Pool
}

func (r *CodeRepositoryRepo) Create(ctx context.Context, req models.CreateCodeRepositoryRequest, actor string) (models.CodeRepository, error) {
	normalized := normalizeCreateCodeRepository(req, actor)
	settings, err := marshalJSON(normalized.Settings)
	if err != nil {
		return models.CodeRepository{}, err
	}
	acl, err := marshalJSON(normalized.ACL)
	if err != nil {
		return models.CodeRepository{}, err
	}
	row := r.Pool.QueryRow(ctx, `
		INSERT INTO repositories (
			id, name, slug, description, owner, organizations, markings,
			default_branch, language_template, storage_backend_rid,
			object_store_backend, visibility, package_kind, tags, settings,
			compass_project_rid, compass_folder_rid, acl, created_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14, $15::jsonb,
			$16, $17, $18::jsonb, $19
		)
		RETURNING `+codeRepositoryColumns,
		uuid.New(), normalized.Name, normalized.Slug, normalized.Description, normalized.Owner,
		normalized.Organizations, normalized.Markings, normalized.DefaultBranch,
		normalized.LanguageTemplate, normalized.StorageBackendRID, normalized.StorageBackend,
		normalized.Visibility, normalized.PackageKind, normalized.Tags, string(settings),
		normalized.CompassProjectRID, normalized.CompassFolderRID, string(acl), actor,
	)
	return scanCodeRepository(row)
}

func (r *CodeRepositoryRepo) List(ctx context.Context, includeTrashed bool) ([]models.CodeRepository, error) {
	where := "WHERE trashed_at IS NULL"
	if includeTrashed {
		where = ""
	}
	rows, err := r.Pool.Query(ctx, `
		SELECT `+codeRepositoryColumns+` FROM repositories `+where+`
		ORDER BY updated_at DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CodeRepository, 0)
	for rows.Next() {
		repository, err := scanCodeRepository(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, repository)
	}
	return out, rows.Err()
}

func (r *CodeRepositoryRepo) Get(ctx context.Context, id uuid.UUID, includeTrashed bool) (*models.CodeRepository, error) {
	where := "id = $1 AND trashed_at IS NULL"
	if includeTrashed {
		where = "id = $1"
	}
	row := r.Pool.QueryRow(ctx, `
		SELECT `+codeRepositoryColumns+` FROM repositories WHERE `+where, id)
	repository, err := scanCodeRepository(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &repository, nil
}

func (r *CodeRepositoryRepo) Update(ctx context.Context, id uuid.UUID, req models.UpdateCodeRepositoryRequest) (*models.CodeRepository, error) {
	current, err := r.Get(ctx, id, true)
	if err != nil || current == nil {
		return current, err
	}
	updated := applyCodeRepositoryUpdate(*current, req)
	settings, err := marshalJSON(updated.Settings)
	if err != nil {
		return nil, err
	}
	acl, err := marshalJSON(updated.ACL)
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(ctx, `
		UPDATE repositories SET
			name = $2, slug = $3, description = $4, owner = $5,
			organizations = $6, markings = $7, default_branch = $8,
			language_template = $9, storage_backend_rid = $10,
			object_store_backend = $11, visibility = $12, package_kind = $13,
			tags = $14, settings = $15::jsonb, compass_project_rid = $16,
			compass_folder_rid = $17, acl = $18::jsonb, updated_at = NOW()
		WHERE id = $1
		RETURNING `+codeRepositoryColumns,
		id, updated.Name, updated.Slug, updated.Description, updated.Owner,
		updated.Organizations, updated.Markings, updated.DefaultBranch, updated.LanguageTemplate,
		updated.StorageBackendRID, updated.StorageBackend, updated.Visibility, updated.PackageKind,
		updated.Tags, string(settings), updated.CompassProjectRID, updated.CompassFolderRID, string(acl),
	)
	next, err := scanCodeRepository(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &next, err
}

func (r *CodeRepositoryRepo) Trash(ctx context.Context, id uuid.UUID, actor string) (*models.CodeRepository, error) {
	row := r.Pool.QueryRow(ctx, `
		UPDATE repositories
		   SET trashed_at = COALESCE(trashed_at, NOW()), trashed_by = COALESCE(trashed_by, $2), updated_at = NOW()
		 WHERE id = $1
		 RETURNING `+codeRepositoryColumns,
		id, actor,
	)
	repository, err := scanCodeRepository(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &repository, err
}

func (r *CodeRepositoryRepo) Restore(ctx context.Context, id uuid.UUID) (*models.CodeRepository, error) {
	row := r.Pool.QueryRow(ctx, `
		UPDATE repositories
		   SET trashed_at = NULL, trashed_by = NULL, updated_at = NOW()
		 WHERE id = $1
		 RETURNING `+codeRepositoryColumns,
		id,
	)
	repository, err := scanCodeRepository(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &repository, err
}

func (r *CodeRepositoryRepo) AttachGitBackend(ctx context.Context, id uuid.UUID, storagePath, httpURL, sshURL string, sshEnabled bool) (*models.CodeRepository, error) {
	row := r.Pool.QueryRow(ctx, `
		UPDATE repositories
		   SET git_storage_path = $2, git_http_url = $3, git_ssh_url = $4,
		       git_ssh_enabled = $5, updated_at = NOW()
		 WHERE id = $1
		 RETURNING `+codeRepositoryColumns,
		id, storagePath, httpURL, sshURL, sshEnabled,
	)
	repository, err := scanCodeRepository(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &repository, err
}

func (r *CodeRepositoryRepo) Move(ctx context.Context, id uuid.UUID, req models.MoveCodeRepositoryRequest) (*models.CodeRepository, error) {
	return r.Update(ctx, id, models.UpdateCodeRepositoryRequest{
		CompassProjectRID: &req.CompassProjectRID,
		CompassFolderRID:  &req.CompassFolderRID,
	})
}

func (r *CodeRepositoryRepo) Rename(ctx context.Context, id uuid.UUID, req models.RenameCodeRepositoryRequest) (*models.CodeRepository, error) {
	slug := req.Slug
	if strings.TrimSpace(slug) == "" {
		slug = slugify(req.Name)
	}
	return r.Update(ctx, id, models.UpdateCodeRepositoryRequest{Name: &req.Name, Slug: &slug})
}

const codeRepositoryColumns = `
	id, rid, name, slug, description, owner, organizations, markings,
	default_branch, language_template, storage_backend_rid, object_store_backend,
	git_storage_path, git_http_url, git_ssh_url, git_ssh_enabled,
	visibility, package_kind, tags, settings, compass_project_rid, compass_folder_rid,
	acl, created_by, created_at, updated_at, trashed_at, trashed_by`

func normalizeCreateCodeRepository(req models.CreateCodeRepositoryRequest, actor string) models.CreateCodeRepositoryRequest {
	req.Name = strings.TrimSpace(req.Name)
	if strings.TrimSpace(req.Slug) == "" {
		req.Slug = slugify(req.Name)
	} else {
		req.Slug = slugify(req.Slug)
	}
	if strings.TrimSpace(req.Owner) == "" {
		req.Owner = actor
	}
	if strings.TrimSpace(req.DefaultBranch) == "" {
		req.DefaultBranch = "main"
	}
	if strings.TrimSpace(req.LanguageTemplate) == "" {
		req.LanguageTemplate = templates.DefaultID
	} else {
		req.LanguageTemplate = templates.NormalizeID(req.LanguageTemplate)
	}
	if strings.TrimSpace(req.StorageBackend) == "" {
		req.StorageBackend = req.ObjectStoreBackend
	}
	if strings.TrimSpace(req.StorageBackend) == "" {
		req.StorageBackend = "local"
	}
	if strings.TrimSpace(req.StorageBackendRID) == "" {
		req.StorageBackendRID = "ri.openfoundry.main.storage-backend." + req.StorageBackend
	}
	if strings.TrimSpace(req.Visibility) == "" {
		req.Visibility = "private"
	}
	if strings.TrimSpace(req.PackageKind) == "" {
		req.PackageKind = languageTemplateToPackageKind(req.LanguageTemplate)
	}
	if req.Settings == nil {
		req.Settings = map[string]any{}
	}
	if req.ACL == nil {
		req.ACL = map[string]any{"owners": []any{req.Owner}, "viewers": []any{}, "editors": []any{}}
	}
	return req
}

func applyCodeRepositoryUpdate(current models.CodeRepository, req models.UpdateCodeRepositoryRequest) models.CodeRepository {
	if req.Name != nil {
		current.Name = strings.TrimSpace(*req.Name)
	}
	if req.Slug != nil {
		current.Slug = slugify(*req.Slug)
	}
	if req.Description != nil {
		current.Description = *req.Description
	}
	if req.Owner != nil {
		current.Owner = strings.TrimSpace(*req.Owner)
	}
	if req.Organizations != nil {
		current.Organizations = *req.Organizations
	}
	if req.Markings != nil {
		current.Markings = *req.Markings
	}
	if req.DefaultBranch != nil {
		current.DefaultBranch = strings.TrimSpace(*req.DefaultBranch)
	}
	if req.LanguageTemplate != nil {
		current.LanguageTemplate = strings.TrimSpace(*req.LanguageTemplate)
	}
	if req.StorageBackendRID != nil {
		current.StorageBackendRID = strings.TrimSpace(*req.StorageBackendRID)
	}
	if req.StorageBackend != nil {
		current.StorageBackend = strings.TrimSpace(*req.StorageBackend)
	}
	if req.ObjectStoreBackend != nil {
		current.StorageBackend = strings.TrimSpace(*req.ObjectStoreBackend)
	}
	if req.Visibility != nil {
		current.Visibility = strings.TrimSpace(*req.Visibility)
	}
	if req.PackageKind != nil {
		current.PackageKind = strings.TrimSpace(*req.PackageKind)
	}
	if req.Tags != nil {
		current.Tags = *req.Tags
	}
	if req.Settings != nil {
		current.Settings = *req.Settings
	}
	if req.CompassProjectRID != nil {
		current.CompassProjectRID = strings.TrimSpace(*req.CompassProjectRID)
	}
	if req.CompassFolderRID != nil {
		current.CompassFolderRID = strings.TrimSpace(*req.CompassFolderRID)
	}
	if req.ACL != nil {
		current.ACL = *req.ACL
	}
	return current
}

func scanCodeRepository(s rowScanner) (models.CodeRepository, error) {
	var repository models.CodeRepository
	var settingsRaw, aclRaw []byte
	err := s.Scan(
		&repository.ID, &repository.RID, &repository.Name, &repository.Slug,
		&repository.Description, &repository.Owner, &repository.Organizations,
		&repository.Markings, &repository.DefaultBranch, &repository.LanguageTemplate,
		&repository.StorageBackendRID, &repository.StorageBackend, &repository.GitStoragePath,
		&repository.GitHTTPURL, &repository.GitSSHURL, &repository.GitSSHEnabled, &repository.Visibility,
		&repository.PackageKind, &repository.Tags, &settingsRaw, &repository.CompassProjectRID,
		&repository.CompassFolderRID, &aclRaw, &repository.CreatedBy, &repository.CreatedAt,
		&repository.UpdatedAt, &repository.TrashedAt, &repository.TrashedBy,
	)
	if err != nil {
		return repository, err
	}
	repository.ObjectStoreBackend = repository.StorageBackend
	if err := json.Unmarshal(settingsRaw, &repository.Settings); err != nil {
		return repository, fmt.Errorf("unmarshal repository settings: %w", err)
	}
	if err := json.Unmarshal(aclRaw, &repository.ACL); err != nil {
		return repository, fmt.Errorf("unmarshal repository acl: %w", err)
	}
	return repository, nil
}

func marshalJSON(value any) ([]byte, error) {
	if value == nil {
		value = map[string]any{}
	}
	return json.Marshal(value)
}

func slugify(in string) string {
	slug := strings.Trim(nonSlugChars.ReplaceAllString(strings.ToLower(strings.TrimSpace(in)), "-"), "-")
	if slug == "" {
		return "repository"
	}
	return slug
}

func languageTemplateToPackageKind(template string) string {
	switch strings.ToLower(strings.TrimSpace(template)) {
	case "typescript-function", "foundry-functions-typescript":
		return "function"
	case "java-transform", "python-transform", "sql-transform", "r-transform":
		return "transform"
	case "model", "model-development":
		return "ml_model"
	default:
		return "transform"
	}
}
