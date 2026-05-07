// Package environment ports services/notebook-runtime-service/src/domain/environment.rs
// 1:1: workspace seeding + path safety helpers used by the notebook
// kernels (each notebook gets a sandboxed directory under DATA_DIR).
//
// The path normaliser is the security-relevant piece: it must reject
// `..`, absolute paths and Windows-style drive prefixes so a kernel
// cell can never write outside its notebook's workspace root.
package environment

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/notebook-runtime-service/internal/models"
)

// WorkspaceRoot returns the per-notebook workspace path under dataDir.
// Mirrors `notebook_workspace_root` in Rust.
func WorkspaceRoot(dataDir string, notebookID uuid.UUID) string {
	return filepath.Join(dataDir, "workspaces", notebookID.String())
}

// EnsureSeed makes sure the workspace exists and contains the seed
// README so a freshly-created notebook has a non-empty file tree the
// kernel can navigate.
func EnsureSeed(dataDir string, notebookID uuid.UUID) error {
	root := WorkspaceRoot(dataDir, notebookID)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	readme := filepath.Join(root, "README.md")
	if _, err := os.Stat(readme); errors.Is(err, os.ErrNotExist) {
		seed := "# Notebook Workspace\n\nNotebook `" + notebookID.String() +
			"` now has a persisted workspace.\n\nUse this area for helper scripts, prompts, notes, " +
			"and analysis artifacts that live next to your notebook cells.\n"
		if err := os.WriteFile(readme, []byte(seed), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ListWorkspaceFiles enumerates all files under the workspace, sorted
// by path for deterministic ordering (matches Rust).
func ListWorkspaceFiles(dataDir string, notebookID uuid.UUID) ([]models.NotebookWorkspaceFile, error) {
	if err := EnsureSeed(dataDir, notebookID); err != nil {
		return nil, err
	}
	root := WorkspaceRoot(dataDir, notebookID)
	var out []models.NotebookWorkspaceFile

	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := loadFile(root, path, info)
		if err != nil {
			return err
		}
		out = append(out, f)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// UpsertWorkspaceFile writes (or overwrites) a single file in the
// workspace. The path must be relative and stay inside the root.
func UpsertWorkspaceFile(dataDir string, notebookID uuid.UUID, relPath, content string) (models.NotebookWorkspaceFile, error) {
	if err := EnsureSeed(dataDir, notebookID); err != nil {
		return models.NotebookWorkspaceFile{}, err
	}
	clean, err := NormalizePath(relPath)
	if err != nil {
		return models.NotebookWorkspaceFile{}, err
	}
	root := WorkspaceRoot(dataDir, notebookID)
	abs := filepath.Join(root, clean)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return models.NotebookWorkspaceFile{}, err
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return models.NotebookWorkspaceFile{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return models.NotebookWorkspaceFile{}, err
	}
	return loadFile(root, abs, info)
}

// DeleteWorkspaceFile removes a file under the workspace. Returns
// (false, nil) when the file did not exist; true on success.
func DeleteWorkspaceFile(dataDir string, notebookID uuid.UUID, relPath string) (bool, error) {
	if err := EnsureSeed(dataDir, notebookID); err != nil {
		return false, err
	}
	clean, err := NormalizePath(relPath)
	if err != nil {
		return false, err
	}
	abs := filepath.Join(WorkspaceRoot(dataDir, notebookID), clean)
	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("workspace path is not a file")
	}
	if err := os.Remove(abs); err != nil {
		return false, err
	}
	return true, nil
}

// NormalizePath enforces that a workspace path is relative, free of
// `..` traversal and free of Windows-style absolute prefixes.
//
// Returns a cleaned forward-slash path on success.
func NormalizePath(p string) (string, error) {
	candidate := strings.TrimSpace(strings.ReplaceAll(p, `\`, `/`))
	if candidate == "" {
		return "", errors.New("workspace path is required")
	}
	if filepath.IsAbs(candidate) ||
		(len(candidate) >= 2 && candidate[1] == ':') /* C: */ {
		return "", errors.New("workspace paths must be relative")
	}
	parts := strings.Split(candidate, "/")
	var out []string
	for _, p := range parts {
		switch p {
		case "", ".":
			continue
		case "..":
			return "", errors.New("workspace paths cannot escape the notebook root")
		default:
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return "", errors.New("workspace path is required")
	}
	return strings.Join(out, "/"), nil
}

func loadFile(root, abs string, info os.FileInfo) (models.NotebookWorkspaceFile, error) {
	bytes, err := os.ReadFile(abs)
	if err != nil {
		return models.NotebookWorkspaceFile{}, err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return models.NotebookWorkspaceFile{}, err
	}
	mod := info.ModTime()
	if mod.IsZero() {
		mod = time.Now().UTC()
	}
	return models.NotebookWorkspaceFile{
		Path:      filepath.ToSlash(rel),
		Language:  inferLanguage(rel),
		Content:   string(bytes),
		SizeBytes: info.Size(),
		UpdatedAt: mod,
	}, nil
}

func inferLanguage(p string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(p), "."))
	switch ext {
	case "py":
		return "python"
	case "sql":
		return "sql"
	case "r":
		return "r"
	case "md":
		return "markdown"
	case "json":
		return "json"
	case "ts", "tsx":
		return "typescript"
	case "js", "jsx":
		return "javascript"
	case "toml":
		return "toml"
	default:
		return "text"
	}
}
