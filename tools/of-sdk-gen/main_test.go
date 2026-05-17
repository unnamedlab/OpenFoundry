package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFlagsRequiresLang(t *testing.T) {
	_, err := parseFlags([]string{"--service", "x", "--out", "/tmp/out"})
	if err == nil || !strings.Contains(err.Error(), "--lang") {
		t.Fatalf("expected --lang error, got %v", err)
	}
}

func TestParseFlagsRequiresOut(t *testing.T) {
	_, err := parseFlags([]string{"--lang", "ts", "--service", "x"})
	if err == nil || !strings.Contains(err.Error(), "--out") {
		t.Fatalf("expected --out error, got %v", err)
	}
}

func TestParseFlagsRequiresServiceOrSpec(t *testing.T) {
	_, err := parseFlags([]string{"--lang", "ts", "--out", "/tmp/out"})
	if err == nil || !strings.Contains(err.Error(), "--service") {
		t.Fatalf("expected service/spec error, got %v", err)
	}
}

func TestParseFlagsHappy(t *testing.T) {
	o, err := parseFlags([]string{"--lang", "py", "--service", "audit-compliance-service", "--out", "/tmp/out"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if o.lang != "py" || o.service != "audit-compliance-service" || o.out != "/tmp/out" {
		t.Fatalf("unexpected opts: %+v", o)
	}
}

func TestResolveSpecExplicitWins(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.yaml")
	if err := os.WriteFile(spec, []byte("openapi: 3.0.3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSpec(options{spec: spec, lang: "ts", out: dir})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != spec {
		t.Fatalf("want %s, got %s", spec, got)
	}
}

func TestResolveSpecFromRepoLayout(t *testing.T) {
	repo := t.TempDir()
	// Synthesise a tiny repo layout: go.mod marker + services/foo spec.
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	specDir := filepath.Join(repo, "services", "foo", "internal", "openapi")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(specDir, "openapi.yaml")
	if err := os.WriteFile(specPath, []byte("openapi: 3.0.3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSpec(options{service: "foo", repoRoot: repo, lang: "ts", out: repo})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != specPath {
		t.Fatalf("want %s, got %s", specPath, got)
	}
}

func TestResolveSpecMissing(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := resolveSpec(options{service: "missing", repoRoot: repo, lang: "ts", out: repo})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRunUnsupportedLang(t *testing.T) {
	err := run(options{lang: "rb", out: t.TempDir(), spec: "/dev/null"}, os.Stdout, os.Stderr)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, err) { // sanity: error type doesn't matter, only that it surfaced
		t.Fatal("unreachable")
	}
}
