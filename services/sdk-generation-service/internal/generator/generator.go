// Package generator drives `tools/of-sdk-gen` as a subprocess and zips
// the resulting client tree. It is intentionally a thin orchestrator —
// the actual code emission lives in the language-specific generators
// (openapi-typescript-codegen, openapi-python-client) so we do not
// reinvent SDK generation in Go.
package generator

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Lang is the SDK target language understood by `of-sdk-gen`.
type Lang string

const (
	LangTypeScript Lang = "ts"
	LangPython     Lang = "py"
)

// ParseLang accepts the wire vocabulary used by POST /sdk/generate.
//
// Anything other than "typescript"/"ts"/"python"/"py" is an error.
func ParseLang(s string) (Lang, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ts", "typescript":
		return LangTypeScript, nil
	case "py", "python":
		return LangPython, nil
	default:
		return "", fmt.Errorf("unsupported language %q", s)
	}
}

// Driver runs `of-sdk-gen` on the host filesystem. Tests inject a
// fake by swapping the Bin field.
type Driver struct {
	// Bin is the path to the `of-sdk-gen` binary. Empty means look it
	// up on PATH at call time.
	Bin string
	// RepoRoot is forwarded to `of-sdk-gen --repo-root`. Empty means
	// the binary will walk up from its CWD.
	RepoRoot string
}

// Generate runs the underlying generator and returns the path to the
// populated output directory. The caller owns cleanup.
func (d *Driver) Generate(ctx context.Context, service string, lang Lang) (string, error) {
	if service == "" {
		return "", errors.New("service is required")
	}
	bin := d.Bin
	if bin == "" {
		found, err := exec.LookPath("of-sdk-gen")
		if err != nil {
			return "", fmt.Errorf("of-sdk-gen not on PATH: %w", err)
		}
		bin = found
	}
	out, err := os.MkdirTemp("", "of-sdk-gen-*")
	if err != nil {
		return "", err
	}
	args := []string{
		"--service", service,
		"--lang", string(lang),
		"--out", out,
	}
	if d.RepoRoot != "" {
		args = append(args, "--repo-root", d.RepoRoot)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(out)
		return "", fmt.Errorf("of-sdk-gen %s/%s failed: %w", service, lang, err)
	}
	return out, nil
}

// ZipDirectory walks root and writes every file into w as a zip
// archive. Paths inside the archive are relative to root. Empty
// directories are skipped (zip readers handle missing dir entries
// fine for SDK consumption).
func ZipDirectory(w io.Writer, root string) error {
	zw := zip.NewWriter(w)
	defer zw.Close()
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fh, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		fh.Name = filepath.ToSlash(rel)
		fh.Method = zip.Deflate
		entry, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(entry, src)
		return err
	})
}
