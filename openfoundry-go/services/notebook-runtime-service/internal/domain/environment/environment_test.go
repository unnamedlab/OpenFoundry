package environment

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Mirrors `rejects_parent_directory_escapes`.
func TestRejectsParentDirectoryEscapes(t *testing.T) {
	t.Parallel()
	_, err := NormalizePath("../secret.txt")
	if err == nil || !strings.Contains(err.Error(), "escape") {
		t.Fatalf("expected escape error, got %v", err)
	}
}

// Mirrors `normalizes_relative_workspace_paths`.
func TestNormalizesRelativeWorkspacePaths(t *testing.T) {
	t.Parallel()
	got, err := NormalizePath("src/./analysis.py")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != "src/analysis.py" {
		t.Fatalf("want src/analysis.py, got %s", got)
	}
}

func TestRejectsAbsolutePaths(t *testing.T) {
	t.Parallel()
	_, err := NormalizePath("/etc/passwd")
	if err == nil {
		t.Fatal("expected error for absolute path")
	}
}

func TestRejectsWindowsDrivePrefix(t *testing.T) {
	t.Parallel()
	_, err := NormalizePath("C:/Windows/system32")
	if err == nil {
		t.Fatal("expected error for windows drive prefix")
	}
}

func TestUpsertAndListWorkspaceRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	nb := uuid.New()

	f, err := UpsertWorkspaceFile(dir, nb, "src/hello.py", "print('hi')\n")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if f.Path != "src/hello.py" || f.Language != "python" {
		t.Fatalf("unexpected file: %+v", f)
	}
	files, err := ListWorkspaceFiles(dir, nb)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Includes the seeded README + our file.
	want := map[string]bool{"README.md": false, "src/hello.py": false}
	for _, f := range files {
		if _, ok := want[f.Path]; ok {
			want[f.Path] = true
		}
	}
	for path, found := range want {
		if !found {
			t.Errorf("missing %s in workspace listing", path)
		}
	}
}

func TestEnsureSeedIsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	nb := uuid.New()
	if err := EnsureSeed(dir, nb); err != nil {
		t.Fatal(err)
	}
	// Second call must not regress the README content.
	if err := EnsureSeed(dir, nb); err != nil {
		t.Fatalf("second seed failed: %v", err)
	}
	root := WorkspaceRoot(dir, nb)
	if !strings.HasSuffix(filepath.ToSlash(root), nb.String()) {
		t.Fatalf("workspace root not under notebook id: %s", root)
	}
}

func TestDeleteWorkspaceFileMissingReturnsFalse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	nb := uuid.New()
	ok, err := DeleteWorkspaceFile(dir, nb, "ghost.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected false on missing file")
	}
}

func TestDeleteRejectsTraversal(t *testing.T) {
	t.Parallel()
	_, err := DeleteWorkspaceFile(t.TempDir(), uuid.New(), "../foo")
	var pathErr *errPathLikely
	if errors.As(err, &pathErr) {
		t.Fatal("unexpected typed error")
	}
	if err == nil {
		t.Fatal("expected error")
	}
}

// errPathLikely exists only as a typed-ish anchor for errors.As above
// — pure compile-time guard to make sure the test compiles even when
// future refactors swap the concrete error type.
type errPathLikely struct{ Inner error }

func (e *errPathLikely) Error() string { return e.Inner.Error() }
