package gitstore_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/code-repository-review-service/internal/gitstore"
	"github.com/openfoundry/openfoundry-go/services/code-repository-review-service/internal/models"
)

func TestEnsureCreatesBareRepositoryAndCloneURLs(t *testing.T) {
	store := gitstore.NewBareRepositoryStore(t.TempDir(), "https://foundry.example", "ssh://git@foundry.example/code-repos", true)
	repositoryID := uuid.New()
	meta, err := store.Ensure(context.Background(), models.CodeRepository{
		ID:            repositoryID,
		DefaultBranch: "main",
	})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(store.Root, repositoryID.String()+".git"), meta.StoragePath)
	require.Equal(t, "https://foundry.example/v1/code-repos/git/"+repositoryID.String()+".git", meta.HTTPURL)
	require.Equal(t, "ssh://git@foundry.example/code-repos/"+repositoryID.String()+".git", meta.SSHURL)
	require.True(t, meta.SSHEnabled)
	require.FileExists(t, filepath.Join(meta.StoragePath, "HEAD"))

	head, err := os.ReadFile(filepath.Join(meta.StoragePath, "HEAD"))
	require.NoError(t, err)
	require.Contains(t, string(head), "refs/heads/main")
}

func TestEnsureRejectsMissingRoot(t *testing.T) {
	store := gitstore.NewBareRepositoryStore("", "", "", false)
	_, err := store.Ensure(context.Background(), models.CodeRepository{ID: uuid.New()})
	require.ErrorContains(t, err, "git store root is required")
}

func TestServeHTTPExposesSmartHTTPInfoRefs(t *testing.T) {
	store := gitstore.NewBareRepositoryStore(t.TempDir(), "", "", false)
	repositoryID := uuid.New()
	meta, err := store.Ensure(context.Background(), models.CodeRepository{ID: repositoryID, DefaultBranch: "main"})
	require.NoError(t, err)
	require.DirExists(t, meta.StoragePath)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/git/"+repositoryID.String()+".git/info/refs?service=git-upload-pack", nil)
	store.ServeHTTP(rec, req, "/"+repositoryID.String()+".git/info/refs", "alice")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/x-git-upload-pack-advertisement")
}

func TestSeedFilesCreatesInitialTemplateCommit(t *testing.T) {
	store := gitstore.NewBareRepositoryStore(t.TempDir(), "", "", false)
	repositoryID := uuid.New()
	repository := models.CodeRepository{ID: repositoryID, DefaultBranch: "main"}

	require.NoError(t, store.SeedFiles(context.Background(), repository, map[string]string{
		"README.md":  "# seeded\n",
		"src/app.py": "print('ok')\n",
	}, "Template Bot", "templates@example.com"))

	worktree := t.TempDir()
	cmd := exec.Command("git", "clone", filepath.Join(store.Root, repositoryID.String()+".git"), worktree)
	require.NoError(t, cmd.Run())
	require.FileExists(t, filepath.Join(worktree, "README.md"))
	require.FileExists(t, filepath.Join(worktree, "src/app.py"))
}

func TestCreateCommitAppliesAtomicMultiFileCommitWithActorSignoff(t *testing.T) {
	store := gitstore.NewBareRepositoryStore(t.TempDir(), "", "", false)
	repositoryID := uuid.New()
	repository := models.CodeRepository{ID: repositoryID, DefaultBranch: "main"}
	require.NoError(t, store.SeedFiles(context.Background(), repository, map[string]string{"README.md": "# seeded\n", "app.py": "print('old')\n"}, "Template Bot", "templates@example.com"))

	commit, err := store.CreateCommit(context.Background(), repository, models.CreateRepositoryCommitRequest{
		BranchName:  "main",
		Title:       "Update files atomically",
		Description: "Commit body from the web dialog.",
		SignOff:     true,
		AuthorName:  "Ada Lovelace",
		AuthorEmail: "ada@example.com",
		Files: []models.MutateRepositoryFileRequest{
			{Action: "save", Path: "README.md", Content: "# changed\n"},
			{Action: "save", Path: "app.py", Content: "print('new')\n"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "Update files atomically", commit.Title)
	require.Equal(t, "Ada Lovelace", commit.AuthorName)
	require.Equal(t, "ada@example.com", commit.AuthorEmail)
	require.Equal(t, 2, commit.FilesChanged)
	require.Contains(t, commit.Description, "Commit body from the web dialog.")
	require.Contains(t, commit.Description, "Signed-off-by: Ada Lovelace <ada@example.com>")

	files, err := store.ListFiles(context.Background(), repository, "main")
	require.NoError(t, err)
	require.Len(t, files, 2)
	contents := map[string]string{}
	for _, file := range files {
		contents[file.Path] = file.Content
	}
	require.Equal(t, "# changed\n", contents["README.md"])
	require.Equal(t, "print('new')\n", contents["app.py"])
}

func TestMutateFileUpdatesTreeThroughGitCommit(t *testing.T) {
	store := gitstore.NewBareRepositoryStore(t.TempDir(), "", "", false)
	repositoryID := uuid.New()
	repository := models.CodeRepository{ID: repositoryID, DefaultBranch: "main"}
	require.NoError(t, store.SeedFiles(context.Background(), repository, map[string]string{"README.md": "# seeded\n"}, "Template Bot", "templates@example.com"))

	files, err := store.MutateFile(context.Background(), repository, models.MutateRepositoryFileRequest{
		Action:  "save",
		Path:    "README.md",
		Content: "# changed\n",
	})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "# changed\n", files[0].Content)
	require.NotEmpty(t, files[0].LastCommitSHA)

	files, err = store.MutateFile(context.Background(), repository, models.MutateRepositoryFileRequest{
		Action:  "rename",
		Path:    "README.md",
		NewPath: "docs/README.md",
	})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, "docs/README.md", files[0].Path)
}

func TestBranchAndTagLifecycleBlocksDefaultDelete(t *testing.T) {
	store := gitstore.NewBareRepositoryStore(t.TempDir(), "", "", false)
	repositoryID := uuid.New()
	repository := models.CodeRepository{ID: repositoryID, DefaultBranch: "main"}
	require.NoError(t, store.SeedFiles(context.Background(), repository, map[string]string{"README.md": "# seeded\n"}, "Template Bot", "templates@example.com"))

	branch, err := store.CreateBranch(context.Background(), repository, models.CreateRepositoryBranchRequest{Name: "release/test", BaseBranch: "main"})
	require.NoError(t, err)
	require.Equal(t, "release/test", branch.Name)

	tag, err := store.CreateTag(context.Background(), repository, models.CreateRepositoryTagRequest{Name: "v0.1.0", Target: "main", Message: "Release v0.1.0"})
	require.NoError(t, err)
	require.Equal(t, "v0.1.0", tag.Name)
	require.Contains(t, tag.Message, "Release")

	require.ErrorContains(t, store.DeleteBranch(context.Background(), repository, "main", false), "cannot delete default branch")
	require.NoError(t, store.DeleteBranch(context.Background(), repository, "release/test", true))
}
