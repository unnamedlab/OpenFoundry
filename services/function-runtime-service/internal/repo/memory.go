package repo

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
)

// MemoryStore is an in-process Store. Used for tests and the optional
// dev-only fallback when DATABASE_URL is unset.
type MemoryStore struct {
	mu        sync.RWMutex
	functions map[uuid.UUID]*models.FunctionDefinition
	versions  map[uuid.UUID][]*models.FunctionVersion // keyed by function id, ordered
	runs      map[uuid.UUID]*models.FunctionRun
	now       func() time.Time
}

// NewMemoryStore returns an empty MemoryStore using time.Now for clock.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		functions: map[uuid.UUID]*models.FunctionDefinition{},
		versions:  map[uuid.UUID][]*models.FunctionVersion{},
		runs:      map[uuid.UUID]*models.FunctionRun{},
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// WithClock overrides the clock used to stamp rows. Returns the
// receiver for chaining.
func (s *MemoryStore) WithClock(now func() time.Time) *MemoryStore {
	s.now = now
	return s
}

func (s *MemoryStore) CreateFunction(_ context.Context, fn *models.FunctionDefinition) error {
	if fn == nil {
		return errors.New("nil function")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.functions {
		if existing.TenantID == fn.TenantID && existing.Namespace == fn.Namespace && existing.Name == fn.Name {
			return domain.ErrAlreadyExists
		}
	}
	if fn.ID == uuid.Nil {
		fn.ID = ids.New()
	}
	now := s.now()
	fn.CreatedAt = now
	fn.UpdatedAt = now
	if fn.Status == "" {
		fn.Status = models.StatusDraft
	}
	clone := *fn
	s.functions[fn.ID] = &clone
	return nil
}

func (s *MemoryStore) GetFunction(_ context.Context, tenantID, id uuid.UUID) (*models.FunctionDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn, ok := s.functions[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if tenantID != uuid.Nil && fn.TenantID != tenantID {
		return nil, domain.ErrNotFound
	}
	out := *fn
	return &out, nil
}

func (s *MemoryStore) ListFunctions(_ context.Context, f ListFunctionsFilter) ([]models.FunctionDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.FunctionDefinition, 0, len(s.functions))
	for _, fn := range s.functions {
		if f.TenantID != uuid.Nil && fn.TenantID != f.TenantID {
			continue
		}
		if f.Namespace != "" && fn.Namespace != f.Namespace {
			continue
		}
		if f.Status != "" && fn.Status != f.Status {
			continue
		}
		if f.Runtime != "" && fn.Runtime != f.Runtime {
			continue
		}
		out = append(out, *fn)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (s *MemoryStore) UpdateFunctionStatus(_ context.Context, tenantID, id uuid.UUID, status models.Status, activeVersion *int) (*models.FunctionDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn, ok := s.functions[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if tenantID != uuid.Nil && fn.TenantID != tenantID {
		return nil, domain.ErrNotFound
	}
	fn.Status = status
	fn.UpdatedAt = s.now()
	if activeVersion != nil {
		v := *activeVersion
		fn.ActiveVersion = &v
		at := s.now()
		fn.ActivatedAt = &at
	}
	out := *fn
	return &out, nil
}

func (s *MemoryStore) AppendVersion(_ context.Context, tenantID, fnID uuid.UUID, sourceURI, entryPoint string) (*models.FunctionVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn, ok := s.functions[fnID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if tenantID != uuid.Nil && fn.TenantID != tenantID {
		return nil, domain.ErrNotFound
	}
	fn.LatestVersion++
	fn.UpdatedAt = s.now()
	v := &models.FunctionVersion{
		ID:         ids.New(),
		FunctionID: fnID,
		Version:    fn.LatestVersion,
		SourceURI:  sourceURI,
		EntryPoint: entryPoint,
		CreatedAt:  s.now(),
	}
	s.versions[fnID] = append(s.versions[fnID], v)
	out := *v
	return &out, nil
}

func (s *MemoryStore) GetVersion(_ context.Context, fnID uuid.UUID, version int) (*models.FunctionVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.versions[fnID] {
		if v.Version == version {
			out := *v
			return &out, nil
		}
	}
	return nil, domain.ErrVersionNotFound
}

func (s *MemoryStore) ListVersions(_ context.Context, fnID uuid.UUID) ([]models.FunctionVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src := s.versions[fnID]
	out := make([]models.FunctionVersion, 0, len(src))
	for _, v := range src {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

func (s *MemoryStore) CreateRun(_ context.Context, run *models.FunctionRun) error {
	if run == nil {
		return errors.New("nil run")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.ID == uuid.Nil {
		run.ID = ids.New()
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = s.now()
	}
	clone := *run
	s.runs[run.ID] = &clone
	return nil
}

func (s *MemoryStore) GetRun(_ context.Context, id uuid.UUID) (*models.FunctionRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	out := *r
	return &out, nil
}

func (s *MemoryStore) ListRuns(_ context.Context, f ListRunsFilter) ([]models.FunctionRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.FunctionRun, 0, len(s.runs))
	for _, r := range s.runs {
		if f.TenantID != uuid.Nil && r.TenantID != f.TenantID {
			continue
		}
		if f.FunctionID != uuid.Nil && r.FunctionID != f.FunctionID {
			continue
		}
		if f.Status != "" && r.Status != f.Status {
			continue
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (s *MemoryStore) FinishRun(_ context.Context, id uuid.UUID, upd RunUpdate) (*models.FunctionRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	r.Status = upd.Status
	r.Output = append(r.Output[:0], upd.Output...)
	r.Error = upd.Error
	r.DurationMs = upd.DurationMs
	finished := s.now()
	r.FinishedAt = &finished
	out := *r
	return &out, nil
}
