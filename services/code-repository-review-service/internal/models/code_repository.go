// Package models holds the wire-format types for code-repository-review-service.
package models

import (
	"time"

	"github.com/google/uuid"
)

// CodeRepository is the Compass first-class resource record for a Code Repository.
type CodeRepository struct {
	ID                 uuid.UUID      `json:"id"`
	RID                string         `json:"rid"`
	Name               string         `json:"name"`
	Slug               string         `json:"slug"`
	Description        string         `json:"description"`
	Owner              string         `json:"owner"`
	Organizations      []string       `json:"organizations"`
	Markings           []string       `json:"markings"`
	DefaultBranch      string         `json:"default_branch"`
	LanguageTemplate   string         `json:"language_template"`
	StorageBackendRID  string         `json:"storage_backend_rid"`
	StorageBackend     string         `json:"storage_backend"`
	ObjectStoreBackend string         `json:"object_store_backend"`
	GitStoragePath     string         `json:"git_storage_path"`
	GitHTTPURL         string         `json:"git_http_url"`
	GitSSHURL          string         `json:"git_ssh_url"`
	GitSSHEnabled      bool           `json:"git_ssh_enabled"`
	Visibility         string         `json:"visibility"`
	PackageKind        string         `json:"package_kind"`
	Tags               []string       `json:"tags"`
	Settings           map[string]any `json:"settings"`
	CompassProjectRID  string         `json:"compass_project_rid"`
	CompassFolderRID   string         `json:"compass_folder_rid"`
	ACL                map[string]any `json:"acl"`
	CreatedBy          string         `json:"created_by"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	TrashedAt          *time.Time     `json:"trashed_at"`
	TrashedBy          *string        `json:"trashed_by"`
}

// CreateCodeRepositoryRequest is POST /v1/code-repos/repositories.
type CreateCodeRepositoryRequest struct {
	Name               string         `json:"name"`
	Slug               string         `json:"slug"`
	Description        string         `json:"description"`
	Owner              string         `json:"owner"`
	Organizations      []string       `json:"organizations"`
	Markings           []string       `json:"markings"`
	DefaultBranch      string         `json:"default_branch"`
	LanguageTemplate   string         `json:"language_template"`
	StorageBackendRID  string         `json:"storage_backend_rid"`
	StorageBackend     string         `json:"storage_backend"`
	ObjectStoreBackend string         `json:"object_store_backend"`
	GitStoragePath     string         `json:"git_storage_path"`
	GitHTTPURL         string         `json:"git_http_url"`
	GitSSHURL          string         `json:"git_ssh_url"`
	GitSSHEnabled      bool           `json:"git_ssh_enabled"`
	Visibility         string         `json:"visibility"`
	PackageKind        string         `json:"package_kind"`
	Tags               []string       `json:"tags"`
	Settings           map[string]any `json:"settings"`
	CompassProjectRID  string         `json:"compass_project_rid"`
	CompassFolderRID   string         `json:"compass_folder_rid"`
	ACL                map[string]any `json:"acl"`
}

// UpdateCodeRepositoryRequest is PATCH /v1/code-repos/repositories/{id}.
type UpdateCodeRepositoryRequest struct {
	Name               *string         `json:"name"`
	Slug               *string         `json:"slug"`
	Description        *string         `json:"description"`
	Owner              *string         `json:"owner"`
	Organizations      *[]string       `json:"organizations"`
	Markings           *[]string       `json:"markings"`
	DefaultBranch      *string         `json:"default_branch"`
	LanguageTemplate   *string         `json:"language_template"`
	StorageBackendRID  *string         `json:"storage_backend_rid"`
	StorageBackend     *string         `json:"storage_backend"`
	ObjectStoreBackend *string         `json:"object_store_backend"`
	Visibility         *string         `json:"visibility"`
	PackageKind        *string         `json:"package_kind"`
	Tags               *[]string       `json:"tags"`
	Settings           *map[string]any `json:"settings"`
	CompassProjectRID  *string         `json:"compass_project_rid"`
	CompassFolderRID   *string         `json:"compass_folder_rid"`
	ACL                *map[string]any `json:"acl"`
}

// MoveCodeRepositoryRequest is POST /v1/code-repos/repositories/{id}/move.
type MoveCodeRepositoryRequest struct {
	CompassProjectRID string `json:"compass_project_rid"`
	CompassFolderRID  string `json:"compass_folder_rid"`
}

// RenameCodeRepositoryRequest is POST /v1/code-repos/repositories/{id}/rename.
type RenameCodeRepositoryRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// RepositoryFile is a tracked file materialized from the managed Git backend.
type RepositoryFile struct {
	ID            string `json:"id"`
	RepositoryID  string `json:"repository_id"`
	Path          string `json:"path"`
	BranchName    string `json:"branch_name"`
	Language      string `json:"language"`
	SizeBytes     int64  `json:"size_bytes"`
	Content       string `json:"content"`
	LastCommitSHA string `json:"last_commit_sha"`
}

// MutateRepositoryFileRequest routes editor/tree actions through Git commits.
type MutateRepositoryFileRequest struct {
	Action      string `json:"action"`
	Path        string `json:"path"`
	NewPath     string `json:"new_path"`
	Content     string `json:"content"`
	BranchName  string `json:"branch_name"`
	Message     string `json:"message"`
	AuthorName  string `json:"author_name"`
	AuthorEmail string `json:"author_email"`
}

// RepositoryCommit is a Git commit summary materialized from the managed Git backend.
type RepositoryCommit struct {
	ID           string  `json:"id"`
	RepositoryID string  `json:"repository_id"`
	BranchName   string  `json:"branch_name"`
	SHA          string  `json:"sha"`
	ParentSHA    *string `json:"parent_sha"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	AuthorName   string  `json:"author_name"`
	AuthorEmail  string  `json:"author_email"`
	FilesChanged int     `json:"files_changed"`
	Additions    int     `json:"additions"`
	Deletions    int     `json:"deletions"`
	CreatedAt    string  `json:"created_at"`
}

// CreateRepositoryCommitRequest commits an atomic set of editor file mutations.
type CreateRepositoryCommitRequest struct {
	BranchName  string                        `json:"branch_name"`
	Title       string                        `json:"title"`
	Description string                        `json:"description"`
	SignOff     bool                          `json:"sign_off"`
	AuthorName  string                        `json:"author_name"`
	AuthorEmail string                        `json:"author_email"`
	Files       []MutateRepositoryFileRequest `json:"files"`
}

// RepositoryBranch is a Git branch summary.
type RepositoryBranch struct {
	ID             string `json:"id"`
	RepositoryID   string `json:"repository_id"`
	Name           string `json:"name"`
	HeadSHA        string `json:"head_sha"`
	BaseBranch     string `json:"base_branch"`
	IsDefault      bool   `json:"is_default"`
	Protected      bool   `json:"protected"`
	AheadBy        int    `json:"ahead_by"`
	PendingReviews int    `json:"pending_reviews"`
	UpdatedAt      string `json:"updated_at"`
}

// RepositoryTag is an annotated Git release tag summary.
type RepositoryTag struct {
	ID           string `json:"id"`
	RepositoryID string `json:"repository_id"`
	Name         string `json:"name"`
	TargetSHA    string `json:"target_sha"`
	Message      string `json:"message"`
	Tagger       string `json:"tagger"`
	Protected    bool   `json:"protected"`
	CreatedAt    string `json:"created_at"`
}

// CreateRepositoryBranchRequest is POST /branches.
type CreateRepositoryBranchRequest struct {
	Name       string `json:"name"`
	BaseBranch string `json:"base_branch"`
	Protected  bool   `json:"protected"`
}

// DeleteRepositoryBranchRequest is DELETE /branches/{branch} body.
type DeleteRepositoryBranchRequest struct {
	Force bool `json:"force"`
}

// MergeRepositoryBranchRequest is POST /branches/{branch}/merge.
type MergeRepositoryBranchRequest struct {
	TargetBranch string `json:"target_branch"`
	Message      string `json:"message"`
	AuthorName   string `json:"author_name"`
	AuthorEmail  string `json:"author_email"`
}

// CreateRepositoryTagRequest is POST /tags.
type CreateRepositoryTagRequest struct {
	Name        string `json:"name"`
	Target      string `json:"target"`
	Message     string `json:"message"`
	TaggerName  string `json:"tagger_name"`
	TaggerEmail string `json:"tagger_email"`
	Protected   bool   `json:"protected"`
}
