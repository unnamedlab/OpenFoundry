package gitstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/code-repository-review-service/internal/models"
)

const defaultGitBinary = "git"

// Metadata is the durable backing-store information attached to a Code Repository.
type Metadata struct {
	StoragePath string
	HTTPURL     string
	SSHURL      string
	SSHEnabled  bool
}

// BareRepositoryStore hosts one bare Git repository per Code Repository.
type BareRepositoryStore struct {
	Root        string
	HTTPBaseURL string
	SSHBaseURL  string
	SSHEnabled  bool
	GitBinary   string
}

// NewBareRepositoryStore returns a store with sane local defaults.
func NewBareRepositoryStore(root, httpBaseURL, sshBaseURL string, sshEnabled bool) *BareRepositoryStore {
	return &BareRepositoryStore{
		Root:        root,
		HTTPBaseURL: httpBaseURL,
		SSHBaseURL:  sshBaseURL,
		SSHEnabled:  sshEnabled,
		GitBinary:   defaultGitBinary,
	}
}

// Ensure creates the bare repository for repo if it does not already exist.
func (s *BareRepositoryStore) Ensure(ctx context.Context, repo models.CodeRepository) (Metadata, error) {
	if s == nil {
		return Metadata{}, errors.New("git store is not configured")
	}
	if strings.TrimSpace(s.Root) == "" {
		return Metadata{}, errors.New("git store root is required")
	}
	if repo.ID.String() == "00000000-0000-0000-0000-000000000000" {
		return Metadata{}, errors.New("repository id is required")
	}
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return Metadata{}, fmt.Errorf("create git root: %w", err)
	}
	path := s.repositoryPath(repo.ID.String())
	if _, err := os.Stat(filepath.Join(path, "HEAD")); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return Metadata{}, fmt.Errorf("stat bare repository: %w", err)
		}
		if err := s.runGit(ctx, "init", "--bare", "--initial-branch", defaultBranch(repo.DefaultBranch), path); err != nil {
			return Metadata{}, err
		}
	}
	if err := s.runGit(ctx, "--git-dir", path, "config", "http.receivepack", "true"); err != nil {
		return Metadata{}, err
	}
	if err := s.runGit(ctx, "--git-dir", path, "config", "uploadpack.allowAnySHA1InWant", "true"); err != nil {
		return Metadata{}, err
	}
	if err := s.runGit(ctx, "--git-dir", path, "update-server-info"); err != nil {
		return Metadata{}, err
	}
	return Metadata{
		StoragePath: path,
		HTTPURL:     s.httpURL(repo.ID.String()),
		SSHURL:      s.sshURL(repo.ID.String()),
		SSHEnabled:  s.SSHEnabled,
	}, nil
}

// ListBranches returns local branch refs from the bare repository.
func (s *BareRepositoryStore) ListBranches(ctx context.Context, repo models.CodeRepository) ([]models.RepositoryBranch, error) {
	if _, err := s.Ensure(ctx, repo); err != nil {
		return nil, err
	}
	path := s.repositoryPath(repo.ID.String())
	stdout, err := s.gitOutput(ctx, "--git-dir", path, "for-each-ref", "--format=%(refname:short)%09%(objectname)%09%(committerdate:iso8601)", "refs/heads")
	if err != nil {
		return nil, err
	}
	branches := make([]models.RepositoryBranch, 0)
	for _, line := range splitGitLines(stdout) {
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		ahead := 0
		if name != defaultBranch(repo.DefaultBranch) {
			if count, err := s.gitOutput(ctx, "--git-dir", path, "rev-list", "--count", defaultBranch(repo.DefaultBranch)+".."+name); err == nil {
				_, _ = fmt.Sscanf(strings.TrimSpace(count), "%d", &ahead)
			}
		}
		updatedAt := ""
		if len(parts) > 2 {
			updatedAt = parts[2]
		}
		branches = append(branches, models.RepositoryBranch{
			ID:           repo.ID.String() + ":" + name,
			RepositoryID: repo.ID.String(),
			Name:         name,
			HeadSHA:      parts[1],
			BaseBranch:   defaultBranch(repo.DefaultBranch),
			IsDefault:    name == defaultBranch(repo.DefaultBranch),
			Protected:    name == defaultBranch(repo.DefaultBranch),
			AheadBy:      ahead,
			UpdatedAt:    updatedAt,
		})
	}
	return branches, nil
}

func (s *BareRepositoryStore) CreateBranch(ctx context.Context, repo models.CodeRepository, req models.CreateRepositoryBranchRequest) (models.RepositoryBranch, error) {
	if _, err := s.Ensure(ctx, repo); err != nil {
		return models.RepositoryBranch{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return models.RepositoryBranch{}, errors.New("branch name is required")
	}
	base := defaultBranch(firstNonEmpty(req.BaseBranch, repo.DefaultBranch))
	path := s.repositoryPath(repo.ID.String())
	if err := s.runGit(ctx, "--git-dir", path, "show-ref", "--verify", "--quiet", "refs/heads/"+base); err != nil {
		return models.RepositoryBranch{}, fmt.Errorf("base branch %q not found", base)
	}
	if err := s.runGit(ctx, "--git-dir", path, "branch", name, base); err != nil {
		return models.RepositoryBranch{}, err
	}
	branches, err := s.ListBranches(ctx, repo)
	if err != nil {
		return models.RepositoryBranch{}, err
	}
	for _, branch := range branches {
		if branch.Name == name {
			branch.Protected = req.Protected || branch.Protected
			branch.BaseBranch = base
			return branch, nil
		}
	}
	return models.RepositoryBranch{}, fmt.Errorf("created branch %q not found", name)
}

func (s *BareRepositoryStore) DeleteBranch(ctx context.Context, repo models.CodeRepository, name string, force bool) error {
	if strings.TrimSpace(name) == defaultBranch(repo.DefaultBranch) {
		return errors.New("cannot delete default branch")
	}
	if _, err := s.Ensure(ctx, repo); err != nil {
		return err
	}
	flag := "-d"
	if force {
		flag = "-D"
	}
	return s.runGit(ctx, "--git-dir", s.repositoryPath(repo.ID.String()), "branch", flag, strings.TrimSpace(name))
}

func (s *BareRepositoryStore) MergeBranch(ctx context.Context, repo models.CodeRepository, source string, req models.MergeRepositoryBranchRequest) (models.RepositoryBranch, error) {
	if _, err := s.Ensure(ctx, repo); err != nil {
		return models.RepositoryBranch{}, err
	}
	target := defaultBranch(firstNonEmpty(req.TargetBranch, repo.DefaultBranch))
	worktree, err := os.MkdirTemp("", "openfoundry-code-merge-*")
	if err != nil {
		return models.RepositoryBranch{}, err
	}
	defer func() { _ = os.RemoveAll(worktree) }()
	if err := s.runGit(ctx, "clone", "--branch", target, s.repositoryPath(repo.ID.String()), worktree); err != nil {
		return models.RepositoryBranch{}, err
	}
	if err := s.runGitIn(ctx, worktree, "config", "user.name", firstNonEmpty(req.AuthorName, "OpenFoundry Merge")); err != nil {
		return models.RepositoryBranch{}, err
	}
	if err := s.runGitIn(ctx, worktree, "config", "user.email", firstNonEmpty(req.AuthorEmail, "merge@openfoundry.local")); err != nil {
		return models.RepositoryBranch{}, err
	}
	message := firstNonEmpty(req.Message, "Merge "+source+" into "+target)
	if err := s.runGitIn(ctx, worktree, "merge", "--no-ff", "-m", message, "origin/"+strings.TrimSpace(source)); err != nil {
		return models.RepositoryBranch{}, err
	}
	if err := s.runGitIn(ctx, worktree, "push", "origin", "HEAD:"+target); err != nil {
		return models.RepositoryBranch{}, err
	}
	branches, err := s.ListBranches(ctx, repo)
	if err != nil {
		return models.RepositoryBranch{}, err
	}
	for _, branch := range branches {
		if branch.Name == target {
			return branch, nil
		}
	}
	return models.RepositoryBranch{}, fmt.Errorf("target branch %q not found", target)
}

func (s *BareRepositoryStore) ListTags(ctx context.Context, repo models.CodeRepository) ([]models.RepositoryTag, error) {
	if _, err := s.Ensure(ctx, repo); err != nil {
		return nil, err
	}
	stdout, err := s.gitOutput(ctx, "--git-dir", s.repositoryPath(repo.ID.String()), "for-each-ref", "--format=%(refname:short)%09%(objectname)%09%(contents:subject)%09%(taggername)%09%(taggerdate:iso8601)", "refs/tags")
	if err != nil {
		return nil, err
	}
	tags := make([]models.RepositoryTag, 0)
	for _, line := range splitGitLines(stdout) {
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		tag := models.RepositoryTag{ID: repo.ID.String() + ":" + parts[0], RepositoryID: repo.ID.String(), Name: parts[0], TargetSHA: parts[1]}
		if len(parts) > 2 {
			tag.Message = parts[2]
		}
		if len(parts) > 3 {
			tag.Tagger = parts[3]
		}
		if len(parts) > 4 {
			tag.CreatedAt = parts[4]
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

func (s *BareRepositoryStore) CreateTag(ctx context.Context, repo models.CodeRepository, req models.CreateRepositoryTagRequest) (models.RepositoryTag, error) {
	if _, err := s.Ensure(ctx, repo); err != nil {
		return models.RepositoryTag{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return models.RepositoryTag{}, errors.New("tag name is required")
	}
	target := firstNonEmpty(req.Target, defaultBranch(repo.DefaultBranch))
	path := s.repositoryPath(repo.ID.String())
	if err := s.runGit(ctx, "--git-dir", path, "config", "user.name", firstNonEmpty(req.TaggerName, "OpenFoundry Release")); err != nil {
		return models.RepositoryTag{}, err
	}
	if err := s.runGit(ctx, "--git-dir", path, "config", "user.email", firstNonEmpty(req.TaggerEmail, "releases@openfoundry.local")); err != nil {
		return models.RepositoryTag{}, err
	}
	if err := s.runGit(ctx, "--git-dir", path, "tag", "-a", name, target, "-m", firstNonEmpty(req.Message, "Release "+name)); err != nil {
		return models.RepositoryTag{}, err
	}
	tags, err := s.ListTags(ctx, repo)
	if err != nil {
		return models.RepositoryTag{}, err
	}
	for _, tag := range tags {
		if tag.Name == name {
			tag.Protected = req.Protected
			return tag, nil
		}
	}
	return models.RepositoryTag{}, fmt.Errorf("created tag %q not found", name)
}

// ListFiles reads the repository tree from branch and returns file contents for editor use.
func (s *BareRepositoryStore) ListFiles(ctx context.Context, repo models.CodeRepository, branch string) ([]models.RepositoryFile, error) {
	if _, err := s.Ensure(ctx, repo); err != nil {
		return nil, err
	}
	branch = defaultBranch(firstNonEmpty(branch, repo.DefaultBranch))
	path := s.repositoryPath(repo.ID.String())
	if empty, err := s.IsEmpty(ctx, repo); err != nil || empty {
		return []models.RepositoryFile{}, err
	}
	stdout, err := s.gitOutput(ctx, "--git-dir", path, "ls-tree", "-r", "--name-only", branch)
	if err != nil {
		return nil, err
	}
	paths := splitGitLines(stdout)
	files := make([]models.RepositoryFile, 0, len(paths))
	for _, filePath := range paths {
		content, err := s.gitOutput(ctx, "--git-dir", path, "show", branch+":"+filePath)
		if err != nil {
			return nil, err
		}
		lastSHA, _ := s.gitOutput(ctx, "--git-dir", path, "log", "-n", "1", "--format=%H", branch, "--", filePath)
		files = append(files, models.RepositoryFile{
			ID:            repo.ID.String() + ":" + filePath,
			RepositoryID:  repo.ID.String(),
			Path:          filePath,
			BranchName:    branch,
			Language:      languageForPath(filePath),
			SizeBytes:     int64(len(content)),
			Content:       content,
			LastCommitSHA: strings.TrimSpace(lastSHA),
		})
	}
	return files, nil
}

// MutateFile applies a file-tree/editor action and commits it to the branch.
func (s *BareRepositoryStore) MutateFile(ctx context.Context, repo models.CodeRepository, req models.MutateRepositoryFileRequest) ([]models.RepositoryFile, error) {
	if _, err := s.Ensure(ctx, repo); err != nil {
		return nil, err
	}
	branch := defaultBranch(firstNonEmpty(req.BranchName, repo.DefaultBranch))
	worktree, err := os.MkdirTemp("", "openfoundry-code-edit-*")
	if err != nil {
		return nil, fmt.Errorf("create editor worktree: %w", err)
	}
	defer func() { _ = os.RemoveAll(worktree) }()
	if empty, _ := s.IsEmpty(ctx, repo); empty {
		if err := s.runGitIn(ctx, worktree, "init", "--initial-branch", branch); err != nil {
			return nil, err
		}
		if err := s.runGitIn(ctx, worktree, "remote", "add", "origin", s.repositoryPath(repo.ID.String())); err != nil {
			return nil, err
		}
	} else if err := s.runGit(ctx, "clone", "--branch", branch, s.repositoryPath(repo.ID.String()), worktree); err != nil {
		return nil, err
	}
	authorName := firstNonEmpty(req.AuthorName, "OpenFoundry Editor")
	authorEmail := firstNonEmpty(req.AuthorEmail, "editor@openfoundry.local")
	if err := s.runGitIn(ctx, worktree, "config", "user.name", authorName); err != nil {
		return nil, err
	}
	if err := s.runGitIn(ctx, worktree, "config", "user.email", authorEmail); err != nil {
		return nil, err
	}
	if err := applyFileMutation(worktree, req); err != nil {
		return nil, err
	}
	if err := s.runGitIn(ctx, worktree, "add", "-A"); err != nil {
		return nil, err
	}
	changed, err := s.hasStagedChanges(ctx, worktree)
	if err != nil {
		return nil, err
	}
	if changed {
		message := firstNonEmpty(req.Message, defaultFileMutationMessage(req))
		if err := s.runGitIn(ctx, worktree, "commit", "-m", message); err != nil {
			return nil, err
		}
		if err := s.runGitIn(ctx, worktree, "push", "origin", "HEAD:"+branch); err != nil {
			return nil, err
		}
		if err := s.runGit(ctx, "--git-dir", s.repositoryPath(repo.ID.String()), "update-server-info"); err != nil {
			return nil, err
		}
	}
	return s.ListFiles(ctx, repo, branch)
}

// ListCommits returns recent Git commits for the requested branch.
func (s *BareRepositoryStore) ListCommits(ctx context.Context, repo models.CodeRepository, branch string) ([]models.RepositoryCommit, error) {
	if _, err := s.Ensure(ctx, repo); err != nil {
		return nil, err
	}
	branch = defaultBranch(firstNonEmpty(branch, repo.DefaultBranch))
	path := s.repositoryPath(repo.ID.String())
	stdout, err := s.gitOutput(ctx, "--git-dir", path, "log", "--max-count=50", "--date=iso8601-strict", "--format=%H%x09%P%x09%an%x09%ae%x09%ad%x09%s", branch)
	if err != nil {
		if strings.Contains(err.Error(), "unknown revision") || strings.Contains(err.Error(), "ambiguous argument") || strings.Contains(err.Error(), "bad revision") {
			return []models.RepositoryCommit{}, nil
		}
		return nil, err
	}
	commits := make([]models.RepositoryCommit, 0)
	for _, line := range splitGitLines(stdout) {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 6 {
			continue
		}
		sha := parts[0]
		parentSHA := firstParentSHA(parts[1])
		description, _ := s.gitOutput(ctx, "--git-dir", path, "log", "-n", "1", "--format=%b", sha)
		stat, _ := s.gitOutput(ctx, "--git-dir", path, "show", "--numstat", "--format=", sha)
		filesChanged, additions, deletions := parseNumstat(stat)
		commits = append(commits, models.RepositoryCommit{
			ID:           repo.ID.String() + ":" + sha,
			RepositoryID: repo.ID.String(),
			BranchName:   branch,
			SHA:          sha,
			ParentSHA:    parentSHA,
			Title:        parts[5],
			Description:  strings.TrimSpace(description),
			AuthorName:   parts[2],
			AuthorEmail:  parts[3],
			FilesChanged: filesChanged,
			Additions:    additions,
			Deletions:    deletions,
			CreatedAt:    parts[4],
		})
	}
	return commits, nil
}

// CreateCommit applies multiple editor file mutations as one atomic Git commit.
func (s *BareRepositoryStore) CreateCommit(ctx context.Context, repo models.CodeRepository, req models.CreateRepositoryCommitRequest) (models.RepositoryCommit, error) {
	if _, err := s.Ensure(ctx, repo); err != nil {
		return models.RepositoryCommit{}, err
	}
	if len(req.Files) == 0 {
		return models.RepositoryCommit{}, errors.New("at least one file mutation is required")
	}
	branch := defaultBranch(firstNonEmpty(req.BranchName, repo.DefaultBranch))
	worktree, err := os.MkdirTemp("", "openfoundry-code-commit-*")
	if err != nil {
		return models.RepositoryCommit{}, fmt.Errorf("create commit worktree: %w", err)
	}
	defer func() { _ = os.RemoveAll(worktree) }()
	if empty, _ := s.IsEmpty(ctx, repo); empty {
		if err := s.runGitIn(ctx, worktree, "init", "--initial-branch", branch); err != nil {
			return models.RepositoryCommit{}, err
		}
		if err := s.runGitIn(ctx, worktree, "remote", "add", "origin", s.repositoryPath(repo.ID.String())); err != nil {
			return models.RepositoryCommit{}, err
		}
	} else if err := s.runGit(ctx, "clone", "--branch", branch, s.repositoryPath(repo.ID.String()), worktree); err != nil {
		return models.RepositoryCommit{}, err
	}
	authorName := firstNonEmpty(req.AuthorName, "OpenFoundry Editor")
	authorEmail := firstNonEmpty(req.AuthorEmail, "editor@openfoundry.local")
	if err := s.runGitIn(ctx, worktree, "config", "user.name", authorName); err != nil {
		return models.RepositoryCommit{}, err
	}
	if err := s.runGitIn(ctx, worktree, "config", "user.email", authorEmail); err != nil {
		return models.RepositoryCommit{}, err
	}
	for _, file := range req.Files {
		if strings.TrimSpace(file.BranchName) == "" {
			file.BranchName = branch
		}
		if err := applyFileMutation(worktree, file); err != nil {
			return models.RepositoryCommit{}, err
		}
	}
	if err := s.runGitIn(ctx, worktree, "add", "-A"); err != nil {
		return models.RepositoryCommit{}, err
	}
	changed, err := s.hasStagedChanges(ctx, worktree)
	if err != nil {
		return models.RepositoryCommit{}, err
	}
	if !changed {
		return models.RepositoryCommit{}, errors.New("no file changes to commit")
	}
	message := commitMessage(req, authorName, authorEmail)
	if err := s.runGitIn(ctx, worktree, "commit", "-m", message); err != nil {
		return models.RepositoryCommit{}, err
	}
	sha, err := s.gitOutputIn(ctx, worktree, "rev-parse", "HEAD")
	if err != nil {
		return models.RepositoryCommit{}, err
	}
	if err := s.runGitIn(ctx, worktree, "push", "origin", "HEAD:"+branch); err != nil {
		return models.RepositoryCommit{}, err
	}
	if err := s.runGit(ctx, "--git-dir", s.repositoryPath(repo.ID.String()), "update-server-info"); err != nil {
		return models.RepositoryCommit{}, err
	}
	commits, err := s.ListCommits(ctx, repo, branch)
	if err != nil {
		return models.RepositoryCommit{}, err
	}
	trimmedSHA := strings.TrimSpace(sha)
	for _, commit := range commits {
		if commit.SHA == trimmedSHA {
			return commit, nil
		}
	}
	return models.RepositoryCommit{}, fmt.Errorf("created commit %q not found", trimmedSHA)
}

// IsEmpty reports whether the bare repository has no HEAD commit yet.
func (s *BareRepositoryStore) IsEmpty(ctx context.Context, repo models.CodeRepository) (bool, error) {
	if s == nil {
		return false, errors.New("git store is not configured")
	}
	path := s.repositoryPath(repo.ID.String())
	cmd := exec.CommandContext(ctx, s.gitBinary(), "--git-dir", path, "rev-parse", "--verify", "HEAD")
	if err := cmd.Run(); err != nil {
		return true, nil
	}
	return false, nil
}

// SeedFiles creates the initial commit for an empty bare repository.
func (s *BareRepositoryStore) SeedFiles(ctx context.Context, repo models.CodeRepository, files map[string]string, authorName, authorEmail string) error {
	if len(files) == 0 {
		return nil
	}
	if _, err := s.Ensure(ctx, repo); err != nil {
		return err
	}
	empty, err := s.IsEmpty(ctx, repo)
	if err != nil {
		return err
	}
	if !empty {
		return nil
	}
	worktree, err := os.MkdirTemp("", "openfoundry-code-template-*")
	if err != nil {
		return fmt.Errorf("create template worktree: %w", err)
	}
	defer func() { _ = os.RemoveAll(worktree) }()
	branch := defaultBranch(repo.DefaultBranch)
	if err := s.runGitIn(ctx, worktree, "init", "--initial-branch", branch); err != nil {
		return err
	}
	if strings.TrimSpace(authorName) == "" {
		authorName = "OpenFoundry Templates"
	}
	if strings.TrimSpace(authorEmail) == "" {
		authorEmail = "templates@openfoundry.local"
	}
	if err := s.runGitIn(ctx, worktree, "config", "user.name", authorName); err != nil {
		return err
	}
	if err := s.runGitIn(ctx, worktree, "config", "user.email", authorEmail); err != nil {
		return err
	}
	for path, content := range files {
		cleanPath, err := cleanTemplatePath(path)
		if err != nil {
			return err
		}
		fullPath := filepath.Join(worktree, cleanPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return fmt.Errorf("create template directory %s: %w", filepath.Dir(cleanPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write template file %s: %w", cleanPath, err)
		}
	}
	if err := s.runGitIn(ctx, worktree, "add", "."); err != nil {
		return err
	}
	if err := s.runGitIn(ctx, worktree, "commit", "-m", "Seed repository template"); err != nil {
		return err
	}
	if err := s.runGitIn(ctx, worktree, "remote", "add", "origin", s.repositoryPath(repo.ID.String())); err != nil {
		return err
	}
	if err := s.runGitIn(ctx, worktree, "push", "origin", "HEAD:"+branch); err != nil {
		return err
	}
	return s.runGit(ctx, "--git-dir", s.repositoryPath(repo.ID.String()), "update-server-info")
}

// ServeHTTP proxies authenticated Smart HTTP Git traffic to git-http-backend.
func (s *BareRepositoryStore) ServeHTTP(w http.ResponseWriter, r *http.Request, gitPath string, remoteUser string) {
	if s == nil || strings.TrimSpace(s.Root) == "" {
		http.Error(w, "git store is not configured", http.StatusInternalServerError)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasPrefix(gitPath, "/") {
		gitPath = "/" + gitPath
	}
	cmd := exec.CommandContext(r.Context(), s.gitBinary(), "http-backend")
	cmd.Env = append(os.Environ(),
		"GIT_PROJECT_ROOT="+s.Root,
		"GIT_HTTP_EXPORT_ALL=1",
		"PATH_INFO="+gitPath,
		"REQUEST_METHOD="+r.Method,
		"QUERY_STRING="+r.URL.RawQuery,
		"CONTENT_TYPE="+r.Header.Get("Content-Type"),
		"REMOTE_USER="+remoteUser,
	)
	cmd.Stdin = r.Body
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		http.Error(w, strings.TrimSpace(stderr.String()), http.StatusBadGateway)
		return
	}
	writeCGIResponse(w, stdout.Bytes())
}

func (s *BareRepositoryStore) repositoryPath(id string) string {
	return filepath.Join(s.Root, id+".git")
}

func (s *BareRepositoryStore) httpURL(id string) string {
	path := "/v1/code-repos/git/" + id + ".git"
	base := strings.TrimRight(strings.TrimSpace(s.HTTPBaseURL), "/")
	if base == "" {
		return path
	}
	return base + path
}

func (s *BareRepositoryStore) sshURL(id string) string {
	if !s.SSHEnabled {
		return ""
	}
	base := strings.TrimRight(strings.TrimSpace(s.SSHBaseURL), "/")
	if base == "" {
		return ""
	}
	return base + "/" + id + ".git"
}

func (s *BareRepositoryStore) runGit(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, s.gitBinary(), args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (s *BareRepositoryStore) gitOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, s.gitBinary(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (s *BareRepositoryStore) gitOutputIn(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, s.gitBinary(), args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (s *BareRepositoryStore) hasStagedChanges(ctx context.Context, dir string) (bool, error) {
	cmd := exec.CommandContext(ctx, s.gitBinary(), "diff", "--cached", "--quiet")
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return true, nil
	}
	return false, err
}

func applyFileMutation(worktree string, req models.MutateRepositoryFileRequest) error {
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "save"
	}
	path, err := cleanTemplatePath(req.Path)
	if err != nil && action != "new" {
		return err
	}
	switch action {
	case "save", "new":
		if action == "new" {
			path, err = cleanTemplatePath(firstNonEmpty(req.NewPath, req.Path))
			if err != nil {
				return err
			}
		}
		fullPath := filepath.Join(worktree, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(fullPath, []byte(req.Content), 0o644)
	case "delete":
		return os.RemoveAll(filepath.Join(worktree, path))
	case "rename", "move":
		newPath, err := cleanTemplatePath(req.NewPath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(worktree, newPath)), 0o755); err != nil {
			return err
		}
		return os.Rename(filepath.Join(worktree, path), filepath.Join(worktree, newPath))
	default:
		return fmt.Errorf("unsupported file action %q", req.Action)
	}
}

func defaultFileMutationMessage(req models.MutateRepositoryFileRequest) string {
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "new":
		return "Create " + firstNonEmpty(req.NewPath, req.Path)
	case "rename":
		return "Rename " + req.Path + " to " + req.NewPath
	case "move":
		return "Move " + req.Path + " to " + req.NewPath
	case "delete":
		return "Delete " + req.Path
	default:
		return "Update " + req.Path
	}
}

func commitMessage(req models.CreateRepositoryCommitRequest, authorName, authorEmail string) string {
	title := firstNonEmpty(req.Title, "Update repository files")
	body := strings.TrimSpace(req.Description)
	if req.SignOff {
		signoff := fmt.Sprintf("Signed-off-by: %s <%s>", authorName, authorEmail)
		if body != "" {
			body += "\n\n" + signoff
		} else {
			body = signoff
		}
	}
	if body == "" {
		return title
	}
	return title + "\n\n" + body
}

func firstParentSHA(parents string) *string {
	fields := strings.Fields(parents)
	if len(fields) == 0 {
		return nil
	}
	return &fields[0]
}

func parseNumstat(stat string) (filesChanged, additions, deletions int) {
	for _, line := range splitGitLines(stat) {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		filesChanged++
		var add, del int
		if parts[0] != "-" {
			_, _ = fmt.Sscanf(parts[0], "%d", &add)
		}
		if parts[1] != "-" {
			_, _ = fmt.Sscanf(parts[1], "%d", &del)
		}
		additions += add
		deletions += del
	}
	return filesChanged, additions, deletions
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func splitGitLines(stdout string) []string {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func languageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".sql":
		return "sql"
	case ".r":
		return "r"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".json":
		return "json"
	case ".md", ".markdown":
		return "markdown"
	case ".toml":
		return "toml"
	case ".yml", ".yaml":
		return "yaml"
	default:
		return "text"
	}
}

func (s *BareRepositoryStore) runGitIn(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, s.gitBinary(), args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func cleanTemplatePath(path string) (string, error) {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "." || strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("invalid template path %q", path)
	}
	return cleaned, nil
}

func (s *BareRepositoryStore) gitBinary() string {
	if strings.TrimSpace(s.GitBinary) == "" {
		return defaultGitBinary
	}
	return s.GitBinary
}

func defaultBranch(branch string) string {
	if strings.TrimSpace(branch) == "" {
		return "main"
	}
	return strings.TrimSpace(branch)
}

func writeCGIResponse(w http.ResponseWriter, raw []byte) {
	header, body := splitCGIResponse(raw)
	status := http.StatusOK
	for _, line := range strings.Split(header, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if strings.EqualFold(name, "Status") {
			var parsed int
			if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil && parsed > 0 {
				status = parsed
			}
			continue
		}
		w.Header().Add(name, value)
	}
	w.WriteHeader(status)
	_, _ = io.Copy(w, bytes.NewReader(body))
}

func splitCGIResponse(raw []byte) (string, []byte) {
	if idx := bytes.Index(raw, []byte("\r\n\r\n")); idx >= 0 {
		return string(raw[:idx]), raw[idx+4:]
	}
	if idx := bytes.Index(raw, []byte("\n\n")); idx >= 0 {
		return string(raw[:idx]), raw[idx+2:]
	}
	return "", raw
}
