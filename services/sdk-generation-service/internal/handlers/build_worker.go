package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/generator/ts"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/ontologyclient"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/repo"
)

// OntologyFetcher is the subset of ontologyclient.Client the worker
// uses. Extracted to an interface so tests can inject a fake without
// reaching for a real HTTP server.
type OntologyFetcher interface {
	GetOntologySnapshot(ctx context.Context, tenantID uuid.UUID, version string) (*ontologyclient.OntologySnapshot, error)
}

// BuildArtifactStore writes a tarball for a build and returns the URI
// the SDK consumer can fetch. The default implementation writes to a
// local directory; the integration tests use the same.
type BuildArtifactStore interface {
	Save(ctx context.Context, buildID uuid.UUID, contents []byte) (uri string, err error)
	Open(ctx context.Context, uri string) ([]byte, error)
}

// LocalArtifactStore writes tarballs to a flat directory. URI scheme
// is `file://<absolute path>` so the handler can mux storage backends
// later (s3:// in particular) without changing the wire surface.
type LocalArtifactStore struct {
	Dir string
}

// Save writes buildID.tgz under s.Dir.
func (s *LocalArtifactStore) Save(_ context.Context, buildID uuid.UUID, contents []byte) (string, error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir artifacts: %w", err)
	}
	path := filepath.Join(s.Dir, buildID.String()+".tgz")
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		return "", fmt.Errorf("write artifact: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return "file://" + abs, nil
}

// Open reads back a previously-saved artifact. Only the `file://` URI
// scheme is honored; anything else is an error so the handler stays
// fail-closed.
func (s *LocalArtifactStore) Open(_ context.Context, uri string) ([]byte, error) {
	if uri == "" {
		return nil, fmt.Errorf("empty artifact uri")
	}
	const prefix = "file://"
	if len(uri) < len(prefix) || uri[:len(prefix)] != prefix {
		return nil, fmt.Errorf("unsupported artifact uri scheme: %q", uri)
	}
	return os.ReadFile(uri[len(prefix):])
}

// BuildWorker turns queued SDKBuild rows into tarballs. It exposes
// ProcessBuild(buildID) — the handler fires it as a goroutine on
// enqueue, and the integration tests call it synchronously.
type BuildWorker struct {
	Repo            *repo.Repo
	Ontology        OntologyFetcher
	TSGenerator     *ts.Generator
	Artifacts       BuildArtifactStore
}

// ProcessBuild runs the full pipeline for one build id. Errors are
// persisted on the row so the GET endpoint can surface them; the
// returned error is for callers that want to log on the worker side.
func (w *BuildWorker) ProcessBuild(ctx context.Context, buildID uuid.UUID) error {
	build, err := w.Repo.GetBuild(ctx, buildID)
	if err != nil {
		return fmt.Errorf("load build: %w", err)
	}
	if build == nil {
		return fmt.Errorf("build %s not found", buildID)
	}
	if build.Status != domain.StatusQueued && build.Status != domain.StatusBuilding {
		return fmt.Errorf("build %s in terminal state %q", buildID, build.Status)
	}
	if build.Status == domain.StatusQueued {
		if err := w.Repo.FinishBuild(ctx, build.ID, domain.StatusBuilding, "", ""); err != nil {
			return fmt.Errorf("mark building: %w", err)
		}
	}

	snapshot, err := w.Ontology.GetOntologySnapshot(ctx, build.TenantID, build.OntologyVersion)
	if err != nil {
		w.fail(ctx, build.ID, "fetch snapshot: "+err.Error())
		return err
	}
	req := domain.SDKRequest{
		TenantID:        build.TenantID,
		OntologyVersion: build.OntologyVersion,
		Target:          build.Target,
		// Include filters are stored on the row but not threaded back
		// through GetBuild — the worker reads them from the queue
		// claim path. For ProcessBuild-called-directly we accept that
		// filters don't apply; that matches the dev path where the
		// stub returns the full catalog anyway.
	}

	files, err := w.TSGenerator.Generate(req, snapshot)
	if err != nil {
		w.fail(ctx, build.ID, "generate: "+err.Error())
		return err
	}
	tarball, err := ts.TarGz(files)
	if err != nil {
		w.fail(ctx, build.ID, "tar: "+err.Error())
		return err
	}
	uri, err := w.Artifacts.Save(ctx, build.ID, tarball)
	if err != nil {
		w.fail(ctx, build.ID, "save artifact: "+err.Error())
		return err
	}
	if err := w.Repo.FinishBuild(ctx, build.ID, domain.StatusSucceeded, uri, ""); err != nil {
		return fmt.Errorf("mark succeeded: %w", err)
	}
	return nil
}

func (w *BuildWorker) fail(ctx context.Context, id uuid.UUID, msg string) {
	if err := w.Repo.FinishBuild(ctx, id, domain.StatusFailed, "", msg); err != nil {
		slog.Error("mark failed", slog.String("build_id", id.String()), slog.String("error", err.Error()))
	}
}
