package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type composeFile struct {
	Services map[string]service `yaml:"services"`
}

type service struct {
	Build buildConfig `yaml:"build"`
}

type buildConfig struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

func (b *buildConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		b.Context = value.Value
		return nil
	}
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("unsupported build node kind %d", value.Kind)
	}
	var raw struct {
		Context    string `yaml:"context"`
		Dockerfile string `yaml:"dockerfile"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	b.Context = raw.Context
	b.Dockerfile = raw.Dockerfile
	return nil
}

func TestComposeBuildContextsResolveDockerfiles(t *testing.T) {
	repoRoot := repoRootFromTest(t)
	composePaths := []string{
		"infra/compose/docker-compose.yml",
		"infra/compose/docker-compose.dev.yml",
	}

	for _, composePath := range composePaths {
		composePath := composePath
		t.Run(composePath, func(t *testing.T) {
			checkComposeBuildContexts(t, repoRoot, composePath)
		})
	}
}

func repoRootFromTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	root, err := filepath.Abs(filepath.Join(wd, "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return filepath.Clean(root)
}

func checkComposeBuildContexts(t *testing.T, repoRoot, composePath string) {
	t.Helper()
	composeFilePath := filepath.Join(repoRoot, composePath)
	contents, err := os.ReadFile(composeFilePath)
	if err != nil {
		t.Fatalf("read %s: %v", composePath, err)
	}

	var parsed composeFile
	if err := yaml.Unmarshal(contents, &parsed); err != nil {
		t.Fatalf("parse %s: %v", composePath, err)
	}

	composeDir := filepath.Dir(composeFilePath)
	for serviceName, service := range parsed.Services {
		build := service.Build
		if build.Context == "" && build.Dockerfile == "" {
			continue
		}
		if build.Context == "" || build.Dockerfile == "" {
			t.Fatalf("%s: service %s must set both build.context and build.dockerfile", composePath, serviceName)
		}

		contextPath := resolvePath(composeDir, build.Context)
		if stat, err := os.Stat(contextPath); err != nil {
			t.Fatalf("%s: service %s resolves build context to %s, but that directory does not exist: %v", composePath, serviceName, rel(t, repoRoot, contextPath), err)
		} else if !stat.IsDir() {
			t.Fatalf("%s: service %s resolves build context to %s, but it is not a directory", composePath, serviceName, rel(t, repoRoot, contextPath))
		}

		dockerfilePath := resolvePath(contextPath, build.Dockerfile)
		if stat, err := os.Stat(dockerfilePath); err != nil {
			t.Fatalf("%s: service %s resolves Dockerfile to %s, but that file does not exist: %v", composePath, serviceName, rel(t, repoRoot, dockerfilePath), err)
		} else if stat.IsDir() {
			t.Fatalf("%s: service %s resolves Dockerfile to %s, but it is a directory", composePath, serviceName, rel(t, repoRoot, dockerfilePath))
		}

		if composePath == "infra/compose/docker-compose.yml" && isRepoRootDockerfile(build.Dockerfile) && contextPath != repoRoot {
			t.Fatalf("%s: service %s expected build context to resolve to repo root for %s, got %s", composePath, serviceName, build.Dockerfile, rel(t, repoRoot, contextPath))
		}
	}
}

func resolvePath(base, raw string) string {
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	return filepath.Clean(filepath.Join(base, raw))
}

func isRepoRootDockerfile(dockerfile string) bool {
	clean := filepath.ToSlash(filepath.Clean(dockerfile))
	return strings.HasPrefix(clean, "services/") || strings.HasPrefix(clean, "apps/")
}

func rel(t *testing.T, repoRoot, path string) string {
	t.Helper()
	relative, err := filepath.Rel(repoRoot, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return path
	}
	return filepath.ToSlash(relative)
}
