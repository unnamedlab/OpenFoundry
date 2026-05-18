package repo

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/domain/function"
)

// invocationStore is embedded into MemoryRepository to keep the
// CRUD-side of the repo readable. All exported methods on
// MemoryRepository forward into the helpers defined here.
type invocationStore struct {
	items map[uuid.UUID]*function.FunctionInvocation
}

func newInvocationStore() invocationStore {
	return invocationStore{items: make(map[uuid.UUID]*function.FunctionInvocation)}
}

// CreateInvocation implements Repository.
func (r *MemoryRepository) CreateInvocation(_ context.Context, in function.FunctionInvocation) (*function.FunctionInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if in.ID == uuid.Nil {
		in.ID = r.uuidFn()
	}
	if in.ScheduledAt.IsZero() {
		in.ScheduledAt = r.nowFn()
	}
	if in.Status == "" {
		in.Status = function.StatusQueued
	}
	if len(in.Payload) == 0 {
		in.Payload = json.RawMessage("null")
	}
	cp := (&in).Clone()
	r.invocations.items[in.ID] = cp
	return cp.Clone(), nil
}

// MarkInvocationRunning implements Repository.
func (r *MemoryRepository) MarkInvocationRunning(_ context.Context, id uuid.UUID) (*function.FunctionInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.invocations.items[id]
	if !ok {
		return nil, function.ErrInvocationNotFound
	}
	if row.Status.IsTerminal() {
		return nil, function.ErrInvocationTerminal
	}
	now := r.nowFn()
	row.Status = function.StatusRunning
	row.StartedAt = &now
	return row.Clone(), nil
}

// CompleteInvocation implements Repository.
func (r *MemoryRepository) CompleteInvocation(_ context.Context, id uuid.UUID, update InvocationCompletion) (*function.FunctionInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.invocations.items[id]
	if !ok {
		return nil, function.ErrInvocationNotFound
	}
	if !update.Status.IsValid() || !update.Status.IsTerminal() {
		return nil, function.ErrInvocationTerminal
	}
	if row.Status.IsTerminal() {
		return row.Clone(), nil
	}
	now := r.nowFn()
	if row.StartedAt == nil {
		started := row.ScheduledAt
		if started.IsZero() {
			started = now
		}
		row.StartedAt = &started
	}
	row.Status = update.Status
	row.FinishedAt = &now
	if len(update.Result) > 0 {
		row.Result = append(json.RawMessage(nil), update.Result...)
	}
	row.ErrorMessage = update.ErrorMessage
	row.CostUnits = update.CostUnits
	return row.Clone(), nil
}

// CancelInvocation implements Repository.
func (r *MemoryRepository) CancelInvocation(_ context.Context, id, _ uuid.UUID) (*function.FunctionInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.invocations.items[id]
	if !ok {
		return nil, function.ErrInvocationNotFound
	}
	if row.Status.IsTerminal() {
		return nil, function.ErrInvocationTerminal
	}
	now := r.nowFn()
	row.Status = function.StatusCancelled
	row.FinishedAt = &now
	return row.Clone(), nil
}

// GetInvocation implements Repository.
func (r *MemoryRepository) GetInvocation(_ context.Context, id uuid.UUID) (*function.FunctionInvocation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	row, ok := r.invocations.items[id]
	if !ok {
		return nil, function.ErrInvocationNotFound
	}
	return row.Clone(), nil
}

// ListInvocations implements Repository.
func (r *MemoryRepository) ListInvocations(_ context.Context, filter InvocationFilter, page Page) (InvocationListResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	limit := page.Limit
	if limit == 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	all := make([]*function.FunctionInvocation, 0, len(r.invocations.items))
	for _, row := range r.invocations.items {
		if !matchesInvocationFilter(row, filter) {
			continue
		}
		all = append(all, row)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].ScheduledAt.Equal(all[j].ScheduledAt) {
			return all[i].ID.String() < all[j].ID.String()
		}
		return all[i].ScheduledAt.Before(all[j].ScheduledAt)
	})

	startIdx := 0
	if page.Cursor != nil && *page.Cursor != "" {
		cursorID, err := uuid.Parse(*page.Cursor)
		if err == nil {
			for i, row := range all {
				if row.ID == cursorID {
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
	out := InvocationListResult{Items: make([]*function.FunctionInvocation, 0, len(page2))}
	for _, row := range page2 {
		out.Items = append(out.Items, row.Clone())
	}
	if end < len(all) && len(out.Items) > 0 {
		last := out.Items[len(out.Items)-1].ID.String()
		out.NextCursor = &last
	}
	return out, nil
}

func matchesInvocationFilter(row *function.FunctionInvocation, f InvocationFilter) bool {
	if f.ModuleID != nil && row.ModuleID != *f.ModuleID {
		return false
	}
	if f.TenantID != nil && row.TenantID != *f.TenantID {
		return false
	}
	if f.Status != nil && row.Status != *f.Status {
		return false
	}
	return true
}
