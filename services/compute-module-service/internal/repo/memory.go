package repo

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/models"
)

// MemoryRepository is the in-memory implementation used by tests and
// the smoke-mode binary (no DATABASE_URL). It is safe for concurrent
// use.
type MemoryRepository struct {
	mu          sync.RWMutex
	items       map[uuid.UUID]*models.ComputeModule
	invocations invocationStore
	nowFn       func() time.Time
	uuidFn      func() uuid.UUID
}

// NewMemoryRepository returns an empty store backed by wall-clock and
// google/uuid. Tests override nowFn/uuidFn via the With* helpers.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		items:       make(map[uuid.UUID]*models.ComputeModule),
		invocations: newInvocationStore(),
		nowFn:       func() time.Time { return time.Now().UTC() },
		uuidFn:      uuid.New,
	}
}

// WithClock returns the same repo with a deterministic clock — used by
// tests to assert on CreatedAt/UpdatedAt.
func (r *MemoryRepository) WithClock(now func() time.Time) *MemoryRepository {
	r.nowFn = now
	return r
}

// WithIDGen overrides the UUID generator. Tests use this for stable
// snapshots.
func (r *MemoryRepository) WithIDGen(gen func() uuid.UUID) *MemoryRepository {
	r.uuidFn = gen
	return r
}

// Create assigns a fresh ID, stamps timestamps, and persists the
// module. ErrNameConflict is returned when another active module in
// the same project+folder already uses the requested name.
func (r *MemoryRepository) Create(_ context.Context, p models.CreateParams) (*models.ComputeModule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nameTakenLocked(p.ProjectID, p.FolderID, p.Name, uuid.Nil) {
		return nil, ErrNameConflict
	}

	now := r.nowFn()
	m := &models.ComputeModule{
		ID:            r.uuidFn(),
		Name:          p.Name,
		Description:   p.Description,
		ProjectID:     p.ProjectID,
		FolderID:      p.FolderID,
		ExecutionMode: p.ExecutionMode,
		State:         models.LifecycleActive,
		Labels:        copyLabels(p.Labels),
		CreatedAt:     now,
		UpdatedAt:     now,
		CreatedBy:     p.Actor,
		UpdatedBy:     p.Actor,
	}
	r.items[m.ID] = m
	return cloneModule(m), nil
}

// Get returns the module by ID; archived modules are returned too so
// callers can drive Restore.
func (r *MemoryRepository) Get(_ context.Context, id uuid.UUID) (*models.ComputeModule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneModule(m), nil
}

// List paginates modules deterministically by (created_at, id). The
// cursor encodes the last seen ID so callers can resume.
func (r *MemoryRepository) List(_ context.Context, filter ListFilter, page Page) (ListResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	limit := page.Limit
	if limit == 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	all := make([]*models.ComputeModule, 0, len(r.items))
	for _, m := range r.items {
		if !matchesFilter(m, filter) {
			continue
		}
		all = append(all, m)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID.String() < all[j].ID.String()
		}
		return all[i].CreatedAt.Before(all[j].CreatedAt)
	})

	startIdx := 0
	if page.Cursor != nil && *page.Cursor != "" {
		cursorID, err := uuid.Parse(*page.Cursor)
		if err != nil {
			startIdx = 0
		} else {
			for i, m := range all {
				if m.ID == cursorID {
					startIdx = i + 1
					break
				}
			}
		}
	}

	end := startIdx + int(limit)
	if end > len(all) {
		end = len(all)
	}
	page2 := all[startIdx:end]

	out := ListResult{Items: make([]*models.ComputeModule, 0, len(page2))}
	for _, m := range page2 {
		out.Items = append(out.Items, cloneModule(m))
	}
	if end < len(all) && len(out.Items) > 0 {
		last := out.Items[len(out.Items)-1].ID.String()
		out.NextCursor = &last
	}
	return out, nil
}

// UpdateMetadata applies the patch. Name changes are checked for
// folder-scoped uniqueness against active siblings.
func (r *MemoryRepository) UpdateMetadata(_ context.Context, id uuid.UUID, p models.UpdateMetadataParams) (*models.ComputeModule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}

	if p.Name != nil && *p.Name != m.Name {
		if r.nameTakenLocked(m.ProjectID, m.FolderID, *p.Name, m.ID) {
			return nil, ErrNameConflict
		}
		m.Name = *p.Name
	}
	if p.Description != nil {
		m.Description = *p.Description
	}
	if p.Labels != nil {
		m.Labels = copyLabels(*p.Labels)
	}
	m.UpdatedAt = r.nowFn()
	m.UpdatedBy = p.Actor
	return cloneModule(m), nil
}

// Move re-homes a module under a new project/folder, checking for
// folder-scoped name collisions in the destination.
func (r *MemoryRepository) Move(_ context.Context, id uuid.UUID, p models.MoveParams) (*models.ComputeModule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	if sameLocation(m, p) {
		return cloneModule(m), nil
	}
	if r.nameTakenLocked(p.ProjectID, p.FolderID, m.Name, m.ID) {
		return nil, ErrNameConflict
	}
	m.ProjectID = p.ProjectID
	m.FolderID = p.FolderID
	m.UpdatedAt = r.nowFn()
	m.UpdatedBy = p.Actor
	return cloneModule(m), nil
}

// Duplicate clones metadata into a new active module, optionally in a
// different project/folder. The execution mode and labels carry over.
func (r *MemoryRepository) Duplicate(_ context.Context, id uuid.UUID, p models.DuplicateParams) (*models.ComputeModule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	src, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	projectID := src.ProjectID
	if p.ProjectID != nil {
		projectID = *p.ProjectID
	}
	folderID := src.FolderID
	if p.FolderID != nil {
		folderID = p.FolderID
	}
	if r.nameTakenLocked(projectID, folderID, p.NewName, uuid.Nil) {
		return nil, ErrNameConflict
	}
	now := r.nowFn()
	cp := &models.ComputeModule{
		ID:            r.uuidFn(),
		Name:          p.NewName,
		Description:   src.Description,
		ProjectID:     projectID,
		FolderID:      folderID,
		ExecutionMode: src.ExecutionMode,
		State:         models.LifecycleActive,
		Labels:        copyLabels(src.Labels),
		CreatedAt:     now,
		UpdatedAt:     now,
		CreatedBy:     p.Actor,
		UpdatedBy:     p.Actor,
	}
	r.items[cp.ID] = cp
	return cloneModule(cp), nil
}

// Archive moves a module out of the active state. Re-archiving an
// already-archived module returns ErrAlreadyArchived to make the
// idempotency boundary explicit to callers/audit.
func (r *MemoryRepository) Archive(_ context.Context, id, actor uuid.UUID) (*models.ComputeModule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	if m.IsArchived() {
		return nil, ErrAlreadyArchived
	}
	now := r.nowFn()
	m.State = models.LifecycleArchived
	m.ArchivedAt = &now
	archivedBy := actor
	m.ArchivedBy = &archivedBy
	m.UpdatedAt = now
	m.UpdatedBy = actor
	return cloneModule(m), nil
}

// Restore returns an archived module to active state. The original
// name must still be free in the module's project/folder.
func (r *MemoryRepository) Restore(_ context.Context, id, actor uuid.UUID) (*models.ComputeModule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	if !m.IsArchived() {
		return nil, ErrNotArchived
	}
	if r.nameTakenLocked(m.ProjectID, m.FolderID, m.Name, m.ID) {
		return nil, ErrNameConflict
	}
	now := r.nowFn()
	m.State = models.LifecycleActive
	m.ArchivedAt = nil
	m.ArchivedBy = nil
	m.UpdatedAt = now
	m.UpdatedBy = actor
	return cloneModule(m), nil
}

// SetPipelineIOConfig stamps a pipeline I/O config onto a pipeline-mode
// module. Function-mode modules are rejected with
// ErrExecutionModeMismatch wrapping the underlying policy sentinel.
func (r *MemoryRepository) SetPipelineIOConfig(_ context.Context, id uuid.UUID, cfg models.PipelineIOConfig, actor uuid.UUID) (*models.ComputeModule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	if m.ExecutionMode != models.ExecutionModePipeline {
		return nil, ErrExecutionModeMismatch
	}
	clone := clonePipelineIOConfig(&cfg)
	m.PipelineIOConfig = clone
	m.UpdatedAt = r.nowFn()
	m.UpdatedBy = actor
	return cloneModule(m), nil
}

// ClearPipelineIOConfig removes a previously persisted pipeline I/O
// config. Function-mode modules are rejected with
// ErrExecutionModeMismatch (mirroring SetPipelineIOConfig) so the
// guard surface is symmetric.
func (r *MemoryRepository) ClearPipelineIOConfig(_ context.Context, id, actor uuid.UUID) (*models.ComputeModule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	if m.ExecutionMode != models.ExecutionModePipeline {
		return nil, ErrExecutionModeMismatch
	}
	m.PipelineIOConfig = nil
	m.UpdatedAt = r.nowFn()
	m.UpdatedBy = actor
	return cloneModule(m), nil
}

// SetContainerImage stores the supplied image reference (and any
// pre-computed findings) on the module record.
func (r *MemoryRepository) SetContainerImage(_ context.Context, id uuid.UUID, img models.ContainerImage, actor uuid.UUID) (*models.ComputeModule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	clone := cloneContainerImage(&img)
	m.ContainerImage = clone
	m.UpdatedAt = r.nowFn()
	m.UpdatedBy = actor
	return cloneModule(m), nil
}

// ClearContainerImage removes any image reference from the module.
func (r *MemoryRepository) ClearContainerImage(_ context.Context, id, actor uuid.UUID) (*models.ComputeModule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	m.ContainerImage = nil
	m.UpdatedAt = r.nowFn()
	m.UpdatedBy = actor
	return cloneModule(m), nil
}

// SetRuntimeConfig persists the per-container runtime configuration
// produced by the runtime policy (CM.4).
func (r *MemoryRepository) SetRuntimeConfig(_ context.Context, id uuid.UUID, cfg models.RuntimeConfig, actor uuid.UUID) (*models.ComputeModule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	m.RuntimeConfig = cloneRuntimeConfig(&cfg)
	m.UpdatedAt = r.nowFn()
	m.UpdatedBy = actor
	return cloneModule(m), nil
}

// ClearRuntimeConfig drops the previously persisted runtime config.
func (r *MemoryRepository) ClearRuntimeConfig(_ context.Context, id, actor uuid.UUID) (*models.ComputeModule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	m.RuntimeConfig = nil
	m.UpdatedAt = r.nowFn()
	m.UpdatedBy = actor
	return cloneModule(m), nil
}

// Delete removes the module record outright. Callers should normally
// archive first; hard delete is gated by audit/permission at the
// handler layer.
func (r *MemoryRepository) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[id]; !ok {
		return ErrNotFound
	}
	delete(r.items, id)
	return nil
}

// nameTakenLocked returns true if another *active* module in the same
// project+folder already uses `name` (case-insensitive). exceptID is
// skipped so updates that keep the same name pass.
func (r *MemoryRepository) nameTakenLocked(projectID uuid.UUID, folderID *uuid.UUID, name string, exceptID uuid.UUID) bool {
	target := strings.ToLower(name)
	for _, m := range r.items {
		if m.ID == exceptID {
			continue
		}
		if m.State != models.LifecycleActive {
			continue
		}
		if m.ProjectID != projectID {
			continue
		}
		if !sameFolderPtr(m.FolderID, folderID) {
			continue
		}
		if strings.ToLower(m.Name) == target {
			return true
		}
	}
	return false
}

func matchesFilter(m *models.ComputeModule, f ListFilter) bool {
	if f.State != nil {
		if m.State != *f.State {
			return false
		}
	} else if !f.IncludeArchived && m.IsArchived() {
		return false
	}
	if f.ProjectID != nil && m.ProjectID != *f.ProjectID {
		return false
	}
	if f.FolderID != nil && !sameFolderPtr(m.FolderID, f.FolderID) {
		return false
	}
	if f.ExecutionMode != nil && m.ExecutionMode != *f.ExecutionMode {
		return false
	}
	return true
}

func sameLocation(m *models.ComputeModule, p models.MoveParams) bool {
	return m.ProjectID == p.ProjectID && sameFolderPtr(m.FolderID, p.FolderID)
}

func sameFolderPtr(a, b *uuid.UUID) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}

func copyLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneModule(m *models.ComputeModule) *models.ComputeModule {
	if m == nil {
		return nil
	}
	cp := *m
	cp.Labels = copyLabels(m.Labels)
	if m.FolderID != nil {
		f := *m.FolderID
		cp.FolderID = &f
	}
	if m.ArchivedAt != nil {
		t := *m.ArchivedAt
		cp.ArchivedAt = &t
	}
	if m.ArchivedBy != nil {
		u := *m.ArchivedBy
		cp.ArchivedBy = &u
	}
	cp.PipelineIOConfig = clonePipelineIOConfig(m.PipelineIOConfig)
	cp.ContainerImage = cloneContainerImage(m.ContainerImage)
	cp.RuntimeConfig = cloneRuntimeConfig(m.RuntimeConfig)
	return &cp
}

func cloneRuntimeConfig(in *models.RuntimeConfig) *models.RuntimeConfig {
	if in == nil {
		return nil
	}
	out := *in
	if len(in.Command) > 0 {
		out.Command = append([]string(nil), in.Command...)
	}
	if len(in.Args) > 0 {
		out.Args = append([]string(nil), in.Args...)
	}
	if len(in.Env) > 0 {
		out.Env = make([]models.EnvVar, len(in.Env))
		for i, ev := range in.Env {
			out.Env[i] = ev
			if ev.ValueFromSecret != nil {
				v := *ev.ValueFromSecret
				out.Env[i].ValueFromSecret = &v
			}
		}
	}
	if len(in.Ports) > 0 {
		out.Ports = append([]models.ContainerPort(nil), in.Ports...)
	}
	if in.Resources != nil {
		r := *in.Resources
		out.Resources = &r
	}
	if in.Logging != nil {
		l := *in.Logging
		if len(in.Logging.FilePaths) > 0 {
			l.FilePaths = append([]string(nil), in.Logging.FilePaths...)
		}
		out.Logging = &l
	}
	if in.Health != nil {
		h := *in.Health
		out.Health = &h
	}
	if len(in.SecretBindings) > 0 {
		out.SecretBindings = append([]models.SecretBinding(nil), in.SecretBindings...)
	}
	if len(in.Findings) > 0 {
		out.Findings = append([]models.CompatibilityFinding(nil), in.Findings...)
	}
	return &out
}

func cloneContainerImage(in *models.ContainerImage) *models.ContainerImage {
	if in == nil {
		return nil
	}
	out := *in
	if len(in.ExposedPorts) > 0 {
		out.ExposedPorts = append([]int(nil), in.ExposedPorts...)
	}
	if len(in.Labels) > 0 {
		out.Labels = make(map[string]string, len(in.Labels))
		for k, v := range in.Labels {
			out.Labels[k] = v
		}
	}
	if in.Provenance != nil {
		p := *in.Provenance
		out.Provenance = &p
	}
	if len(in.Findings) > 0 {
		out.Findings = append([]models.CompatibilityFinding(nil), in.Findings...)
	}
	return &out
}

func clonePipelineIOConfig(in *models.PipelineIOConfig) *models.PipelineIOConfig {
	if in == nil {
		return nil
	}
	out := &models.PipelineIOConfig{}
	if len(in.Inputs) > 0 {
		out.Inputs = append([]models.PipelineIO(nil), in.Inputs...)
	}
	if len(in.Outputs) > 0 {
		out.Outputs = append([]models.PipelineIO(nil), in.Outputs...)
	}
	return out
}
