package storageabstraction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// SnapshotScope identifies whether a snapshot captures main type storage or a
// branch overlay for a type.
type SnapshotScope string

const (
	SnapshotScopeMain          SnapshotScope = "main"
	SnapshotScopeBranchOverlay SnapshotScope = "branch_overlay"
)

// SnapshotScheduleSpec is the data-health/retention layer scheduling contract
// for periodic OSV2.19 per-type snapshots.
type SnapshotScheduleSpec struct {
	Tenant          TenantId       `json:"tenant"`
	TypeID          TypeId         `json:"type_id"`
	Branch          BranchID       `json:"branch,omitempty"`
	Scope           SnapshotScope  `json:"scope"`
	Interval        time.Duration  `json:"interval"`
	RetentionCount  int            `json:"retention_count,omitempty"`
	RetentionWindow time.Duration  `json:"retention_window,omitempty"`
	Labels          map[string]any `json:"labels,omitempty"`
}

// SnapshotMetadata is the verifiable descriptor returned for OSV2 snapshots.
type SnapshotMetadata struct {
	ID              string        `json:"id"`
	Tenant          TenantId      `json:"tenant"`
	TypeID          TypeId        `json:"type_id"`
	Branch          BranchID      `json:"branch,omitempty"`
	Scope           SnapshotScope `json:"scope"`
	ObjectCount     int           `json:"object_count"`
	IndexRowCount   int           `json:"index_row_count"`
	ContentHash     string        `json:"content_hash"`
	CreatedAtMs     int64         `json:"created_at_ms"`
	ScheduledBy     string        `json:"scheduled_by,omitempty"`
	RetentionPolicy string        `json:"retention_policy,omitempty"`
}

// SnapshotDependencyWarning lists downstream consumers that may be affected by a
// restore before the caller commits OSV2.20 restore-to-snapshot.
type SnapshotDependencyWarning struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	Impact     string `json:"impact"`
	RequiredBy string `json:"required_by,omitempty"`
}

// SnapshotRestorePlan is the pre-commit warning surface for restore.
type SnapshotRestorePlan struct {
	Snapshot SnapshotMetadata            `json:"snapshot"`
	Warnings []SnapshotDependencyWarning `json:"warnings"`
}

// SnapshotRestoreRequest restores a main type or a branch overlay to a snapshot.
type SnapshotRestoreRequest struct {
	SnapshotID                  string                      `json:"snapshot_id"`
	Actor                       string                      `json:"actor"`
	Branch                      BranchID                    `json:"branch,omitempty"`
	Scope                       SnapshotScope               `json:"scope"`
	AcknowledgedDependencyKinds []string                    `json:"acknowledged_dependency_kinds,omitempty"`
	Dependencies                []SnapshotDependencyWarning `json:"dependencies,omitempty"`
}

// SnapshotRestoreResult records whether the restore committed and which warning
// set had to be acknowledged first.
type SnapshotRestoreResult struct {
	SnapshotID  string                      `json:"snapshot_id"`
	Committed   bool                        `json:"committed"`
	ContentHash string                      `json:"content_hash"`
	Warnings    []SnapshotDependencyWarning `json:"warnings,omitempty"`
}

// SnapshotStore is the OSV2.19/OSV2.20 snapshot and restore contract.
type SnapshotStore interface {
	ScheduleSnapshot(ctx context.Context, spec SnapshotScheduleSpec) (SnapshotMetadata, error)
	CreateTypeSnapshot(ctx context.Context, tenant TenantId, typeID TypeId, objects ObjectStore, scheduledBy string) (SnapshotMetadata, error)
	CreateBranchOverlaySnapshot(ctx context.Context, branch BranchID, tenant TenantId, typeID TypeId, main ObjectStore, scheduledBy string) (SnapshotMetadata, error)
	PlanRestoreSnapshot(ctx context.Context, snapshotID string, dependencies []SnapshotDependencyWarning) (SnapshotRestorePlan, error)
	RestoreSnapshot(ctx context.Context, req SnapshotRestoreRequest, objects ObjectStore, actions ActionLogStore) (SnapshotRestoreResult, error)
}

type snapshotRecord struct {
	metadata SnapshotMetadata
	payload  snapshotPayload
}

type snapshotPayload struct {
	Objects    []Object                  `json:"objects"`
	Spatial    []snapshotSpatialRow      `json:"spatial,omitempty"`
	TimeSeries []TimeSeriesSample        `json:"time_series,omitempty"`
	BranchRows []snapshotBranchObjectRow `json:"branch_rows,omitempty"`
}

type snapshotSpatialRow struct {
	ObjectID ObjectId `json:"object_id"`
	Property string   `json:"property"`
	Point    GeoPoint `json:"point"`
	Version  uint64   `json:"version"`
}

type snapshotBranchObjectRow struct {
	Object  Object `json:"object"`
	Deleted bool   `json:"deleted"`
}

var _ SnapshotStore = (*InMemoryOSV2AdvancedStore)(nil)

func (s *InMemoryOSV2AdvancedStore) ScheduleSnapshot(ctx context.Context, spec SnapshotScheduleSpec) (SnapshotMetadata, error) {
	if spec.Scope == "" {
		spec.Scope = SnapshotScopeMain
	}
	scheduledBy := "data-health-retention"
	if spec.Labels != nil {
		if v, ok := spec.Labels["scheduled_by"].(string); ok && v != "" {
			scheduledBy = v
		}
	}
	switch spec.Scope {
	case SnapshotScopeMain:
		return s.CreateTypeSnapshot(ctx, spec.Tenant, spec.TypeID, nil, scheduledBy)
	case SnapshotScopeBranchOverlay:
		return s.CreateBranchOverlaySnapshot(ctx, spec.Branch, spec.Tenant, spec.TypeID, nil, scheduledBy)
	default:
		return SnapshotMetadata{}, Invalidf("unknown snapshot scope %q", spec.Scope)
	}
}

func (s *InMemoryOSV2AdvancedStore) CreateTypeSnapshot(ctx context.Context, tenant TenantId, typeID TypeId, objects ObjectStore, scheduledBy string) (SnapshotMetadata, error) {
	var rows []Object
	if objects != nil {
		page, err := objects.ListByType(ctx, tenant, typeID, Page{Size: 100_000}, Strong())
		if err != nil {
			return SnapshotMetadata{}, err
		}
		rows = append(rows, page.Items...)
	}
	payload := snapshotPayload{Objects: cloneObjectsForSnapshot(rows)}
	s.addIndexRowsToSnapshot(tenant, typeID, "", &payload)
	return s.storeSnapshot(tenant, typeID, "", SnapshotScopeMain, scheduledBy, payload)
}

func (s *InMemoryOSV2AdvancedStore) CreateBranchOverlaySnapshot(ctx context.Context, branch BranchID, tenant TenantId, typeID TypeId, main ObjectStore, scheduledBy string) (SnapshotMetadata, error) {
	_ = ctx
	_ = main
	payload := snapshotPayload{}
	s.mu.Lock()
	for key, overlay := range s.branchObjects {
		if key.branch != branch || key.tenant != tenant {
			continue
		}
		if !overlay.deleted && overlay.object.TypeID != typeID {
			continue
		}
		payload.BranchRows = append(payload.BranchRows, snapshotBranchObjectRow{Object: cloneObject(overlay.object), Deleted: overlay.deleted})
	}
	s.mu.Unlock()
	sort.SliceStable(payload.BranchRows, func(i, j int) bool { return payload.BranchRows[i].Object.ID < payload.BranchRows[j].Object.ID })
	s.addIndexRowsToSnapshot(tenant, typeID, branch, &payload)
	return s.storeSnapshot(tenant, typeID, branch, SnapshotScopeBranchOverlay, scheduledBy, payload)
}

func (s *InMemoryOSV2AdvancedStore) PlanRestoreSnapshot(_ context.Context, snapshotID string, dependencies []SnapshotDependencyWarning) (SnapshotRestorePlan, error) {
	s.mu.Lock()
	record, ok := s.snapshots[snapshotID]
	s.mu.Unlock()
	if !ok {
		return SnapshotRestorePlan{}, NotFound("snapshot " + snapshotID)
	}
	warnings := append([]SnapshotDependencyWarning(nil), dependencies...)
	if len(warnings) == 0 {
		warnings = defaultSnapshotWarnings(record.metadata)
	}
	return SnapshotRestorePlan{Snapshot: record.metadata, Warnings: warnings}, nil
}

func (s *InMemoryOSV2AdvancedStore) RestoreSnapshot(ctx context.Context, req SnapshotRestoreRequest, objects ObjectStore, actions ActionLogStore) (SnapshotRestoreResult, error) {
	s.mu.Lock()
	record, ok := s.snapshots[req.SnapshotID]
	s.mu.Unlock()
	if !ok {
		return SnapshotRestoreResult{}, NotFound("snapshot " + req.SnapshotID)
	}
	warnings := append([]SnapshotDependencyWarning(nil), req.Dependencies...)
	if len(warnings) == 0 {
		warnings = defaultSnapshotWarnings(record.metadata)
	}
	if missing := unacknowledgedWarnings(warnings, req.AcknowledgedDependencyKinds); len(missing) > 0 {
		return SnapshotRestoreResult{SnapshotID: req.SnapshotID, ContentHash: record.metadata.ContentHash, Warnings: missing}, nil
	}
	scope := req.Scope
	if scope == "" {
		scope = record.metadata.Scope
	}
	if scope == SnapshotScopeBranchOverlay || record.metadata.Scope == SnapshotScopeBranchOverlay {
		branch := req.Branch
		if branch == "" {
			branch = record.metadata.Branch
		}
		if branch == "" {
			return SnapshotRestoreResult{}, Invalid("branch overlay restore requires branch id")
		}
		s.restoreBranchPayload(branch, record.metadata.Tenant, record.metadata.TypeID, record.payload)
	} else {
		if objects == nil {
			return SnapshotRestoreResult{}, Invalid("main snapshot restore requires object store")
		}
		if err := restoreMainObjects(ctx, objects, record.metadata.Tenant, record.metadata.TypeID, record.payload.Objects); err != nil {
			return SnapshotRestoreResult{}, err
		}
	}
	if actions != nil {
		if err := appendSnapshotAudit(ctx, actions, req.Actor, record.metadata, scope, warnings); err != nil {
			return SnapshotRestoreResult{}, err
		}
	}
	return SnapshotRestoreResult{SnapshotID: req.SnapshotID, Committed: true, ContentHash: record.metadata.ContentHash, Warnings: warnings}, nil
}

func (s *InMemoryOSV2AdvancedStore) addIndexRowsToSnapshot(tenant TenantId, typeID TypeId, branch BranchID, payload *snapshotPayload) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.spatial {
		if entry.tenant == tenant && entry.typeID == typeID {
			payload.Spatial = append(payload.Spatial, snapshotSpatialRow{ObjectID: entry.objectID, Property: entry.property, Point: entry.point, Version: entry.version})
		}
	}
	for key, samples := range s.timeSeries {
		if key.tenant == tenant && key.typeID == typeID {
			payload.TimeSeries = append(payload.TimeSeries, samples...)
		}
	}
	sort.SliceStable(payload.Spatial, func(i, j int) bool {
		if payload.Spatial[i].ObjectID != payload.Spatial[j].ObjectID {
			return payload.Spatial[i].ObjectID < payload.Spatial[j].ObjectID
		}
		return payload.Spatial[i].Property < payload.Spatial[j].Property
	})
	sort.SliceStable(payload.TimeSeries, func(i, j int) bool {
		if payload.TimeSeries[i].ObjectID != payload.TimeSeries[j].ObjectID {
			return payload.TimeSeries[i].ObjectID < payload.TimeSeries[j].ObjectID
		}
		if payload.TimeSeries[i].Property != payload.TimeSeries[j].Property {
			return payload.TimeSeries[i].Property < payload.TimeSeries[j].Property
		}
		return payload.TimeSeries[i].TimestampMs < payload.TimeSeries[j].TimestampMs
	})
	_ = branch
}

func (s *InMemoryOSV2AdvancedStore) storeSnapshot(tenant TenantId, typeID TypeId, branch BranchID, scope SnapshotScope, scheduledBy string, payload snapshotPayload) (SnapshotMetadata, error) {
	canonical := canonicalSnapshotPayload(payload)
	sum := sha256.Sum256(canonical)
	hash := "sha256:" + hex.EncodeToString(sum[:])
	idSeed := fmt.Sprintf("%s/%s/%s/%s/%s", tenant, typeID, branch, scope, hash)
	idSum := sha256.Sum256([]byte(idSeed))
	id := hex.EncodeToString(idSum[:16])
	meta := SnapshotMetadata{
		ID:              id,
		Tenant:          tenant,
		TypeID:          typeID,
		Branch:          branch,
		Scope:           scope,
		ObjectCount:     len(payload.Objects) + len(payload.BranchRows),
		IndexRowCount:   len(payload.Spatial) + len(payload.TimeSeries),
		ContentHash:     hash,
		CreatedAtMs:     time.Now().UTC().UnixMilli(),
		ScheduledBy:     scheduledBy,
		RetentionPolicy: "data-health-retention",
	}
	s.mu.Lock()
	s.snapshots[id] = snapshotRecord{metadata: meta, payload: payload}
	s.mu.Unlock()
	return meta, nil
}

func (s *InMemoryOSV2AdvancedStore) restoreBranchPayload(branch BranchID, tenant TenantId, typeID TypeId, payload snapshotPayload) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.branchObjects {
		if key.branch == branch && key.tenant == tenant {
			delete(s.branchObjects, key)
		}
	}
	for _, row := range payload.BranchRows {
		if !row.Deleted && row.Object.TypeID != typeID {
			continue
		}
		obj := cloneObject(row.Object)
		if obj.Tenant == "" {
			obj.Tenant = tenant
		}
		s.branchObjects[branchObjectKey{branch: branch, tenant: tenant, id: obj.ID}] = branchObjectOverlay{object: obj, deleted: row.Deleted}
	}
}

func restoreMainObjects(ctx context.Context, objects ObjectStore, tenant TenantId, typeID TypeId, snapshotObjects []Object) error {
	current, err := objects.ListByType(ctx, tenant, typeID, Page{Size: 100_000}, Strong())
	if err != nil {
		return err
	}
	wanted := map[ObjectId]Object{}
	for _, obj := range snapshotObjects {
		wanted[obj.ID] = cloneObject(obj)
	}
	for _, obj := range current.Items {
		if _, ok := wanted[obj.ID]; !ok {
			if _, err := objects.Delete(ctx, tenant, obj.ID); err != nil {
				return err
			}
		}
	}
	for _, obj := range snapshotObjects {
		currentObj, err := objects.Get(ctx, tenant, obj.ID, Strong())
		if err != nil {
			return err
		}
		var expected *uint64
		if currentObj != nil {
			v := currentObj.Version
			expected = &v
		}
		toPut := cloneObject(obj)
		_, err = objects.Put(ctx, toPut, expected)
		if err != nil {
			return err
		}
	}
	return nil
}

func appendSnapshotAudit(ctx context.Context, actions ActionLogStore, actor string, meta SnapshotMetadata, scope SnapshotScope, warnings []SnapshotDependencyWarning) error {
	payload, err := json.Marshal(map[string]any{
		"snapshot_id":  meta.ID,
		"content_hash": meta.ContentHash,
		"tenant":       string(meta.Tenant),
		"type_id":      string(meta.TypeID),
		"branch_id":    string(meta.Branch),
		"scope":        string(scope),
		"warnings":     warnings,
	})
	if err != nil {
		return err
	}
	eventID := "osv2-snapshot-restore:" + meta.ID + ":" + string(scope)
	return actions.Append(ctx, ActionLogEntry{Tenant: meta.Tenant, EventID: &eventID, ActionID: eventID, Kind: "osv2.snapshot_restored", Subject: actor, Payload: payload, RecordedAtMs: time.Now().UTC().UnixMilli()})
}

func canonicalSnapshotPayload(payload snapshotPayload) []byte {
	payload.Objects = cloneObjectsForSnapshot(payload.Objects)
	sort.SliceStable(payload.Objects, func(i, j int) bool { return payload.Objects[i].ID < payload.Objects[j].ID })
	b, _ := json.Marshal(payload)
	return b
}

func cloneObjectsForSnapshot(in []Object) []Object {
	out := make([]Object, 0, len(in))
	for _, obj := range in {
		out = append(out, cloneObject(obj))
	}
	return out
}

func cloneObject(obj Object) Object {
	out := obj
	out.Payload = append(json.RawMessage(nil), obj.Payload...)
	out.Markings = append([]MarkingId(nil), obj.Markings...)
	if obj.OrganizationID != nil {
		v := *obj.OrganizationID
		out.OrganizationID = &v
	}
	if obj.CreatedAtMs != nil {
		v := *obj.CreatedAtMs
		out.CreatedAtMs = &v
	}
	if obj.Owner != nil {
		v := *obj.Owner
		out.Owner = &v
	}
	return out
}

func defaultSnapshotWarnings(meta SnapshotMetadata) []SnapshotDependencyWarning {
	base := string(meta.TypeID)
	return []SnapshotDependencyWarning{
		{Kind: "action", ID: base + ":actions", Impact: "Actions may observe restored object state on their next execution.", RequiredBy: "restore"},
		{Kind: "dashboard", ID: base + ":dashboards", Impact: "Dashboards and Workshop modules may refresh to restored values.", RequiredBy: "restore"},
		{Kind: "osdk", ID: base + ":osdk", Impact: "OSDK subscribers may receive restore change events or stale cursor invalidations.", RequiredBy: "restore"},
	}
}

func unacknowledgedWarnings(warnings []SnapshotDependencyWarning, acknowledged []string) []SnapshotDependencyWarning {
	if len(warnings) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, kind := range acknowledged {
		seen[kind] = true
	}
	missing := []SnapshotDependencyWarning{}
	for _, warning := range warnings {
		if !seen[warning.Kind] {
			missing = append(missing, warning)
		}
	}
	return missing
}
