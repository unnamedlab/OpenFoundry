package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

var allowedBootstrapModes = map[string]struct{}{"schema-only": {}, "with-snapshot": {}}

func (r *PGXRepository) ListVersions(ctx context.Context, listingID uuid.UUID) ([]models.PackageVersion, error) {
	if _, err := r.getListingDefinition(ctx, listingID.String()); err != nil {
		return nil, err
	}
	return r.listVersions(ctx, listingID)
}

func (r *PGXRepository) IncludeActionInProduct(ctx context.Context, listingID uuid.UUID, req models.IncludeActionRequest) (*models.PackageVersion, error) {
	if _, err := r.getListingDefinition(ctx, listingID.String()); err != nil {
		return nil, err
	}
	versions, err := r.listVersions(ctx, listingID)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("%w: listing has no published versions; publish at least one before including action types", ErrValidation)
	}
	version := versions[0]
	if req.VersionID != nil {
		found := false
		for _, candidate := range versions {
			if candidate.ID == *req.VersionID {
				version = candidate
				found = true
				break
			}
		}
		if !found {
			return nil, ErrVersionNotFound
		}
	}
	if !json.Valid(req.ActionType) {
		return nil, fmt.Errorf("%w: action_type must be valid JSON", ErrValidation)
	}
	var manifest map[string]any
	if len(version.Manifest) == 0 || string(version.Manifest) == "null" {
		manifest = map[string]any{}
	} else if err := json.Unmarshal(version.Manifest, &manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	var action any
	if err := json.Unmarshal(req.ActionType, &action); err != nil {
		return nil, fmt.Errorf("%w: action_type must be valid JSON", ErrValidation)
	}
	artifacts, _ := manifest["artifacts"].([]any)
	artifacts = append(artifacts, map[string]any{"kind": "action_type", "action_type": action, "dependencies": req.Dependencies})
	manifest["artifacts"] = artifacts
	updatedManifest, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	if _, err := r.Pool.Exec(ctx, `UPDATE marketplace_package_versions SET manifest = $1::jsonb WHERE id = $2`, updatedManifest, version.ID); err != nil {
		return nil, err
	}
	versions, err = r.listVersions(ctx, listingID)
	if err != nil {
		return nil, err
	}
	for _, candidate := range versions {
		if candidate.ID == version.ID {
			return &candidate, nil
		}
	}
	return nil, ErrVersionNotFound
}

func (r *PGXRepository) CreateDatasetProduct(ctx context.Context, rid string, req models.CreateDatasetProductRequest) (*models.DatasetProduct, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	rid = strings.TrimSpace(rid)
	if rid == "" {
		return nil, fmt.Errorf("%w: dataset RID is required", ErrValidation)
	}
	if req.Version == "" {
		req.Version = "1.0.0"
	}
	if req.BootstrapMode == "" {
		req.BootstrapMode = "schema-only"
	}
	if _, ok := allowedBootstrapModes[req.BootstrapMode]; !ok {
		return nil, fmt.Errorf("%w: bootstrap_mode must be 'schema-only' or 'with-snapshot'", ErrValidation)
	}
	manifest := datasetManifest(req)
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	now := time.Now().UTC()
	row := r.Pool.QueryRow(ctx, `INSERT INTO marketplace_dataset_products (id, name, source_dataset_rid, entity_type, version, project_id, published_by, export_includes_data, include_schema, include_branches, include_retention, include_schedules, manifest, bootstrap_mode, published_at, created_at) VALUES ($1, $2, $3, 'dataset', $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14, $15) RETURNING id, name, source_dataset_rid, entity_type, version, project_id, published_by, export_includes_data, include_schema, include_branches, include_retention, include_schedules, manifest, bootstrap_mode, published_at, created_at`, id, req.Name, rid, req.Version, req.ProjectID, req.PublishedBy, req.ExportIncludesData, req.IncludeSchema, req.IncludeBranches, req.IncludeRetention, req.IncludeSchedules, manifestJSON, req.BootstrapMode, now, now)
	product, err := scanDatasetProduct(row)
	if err != nil {
		return nil, mapPGError(err)
	}
	return product, nil
}

func (r *PGXRepository) GetDatasetProduct(ctx context.Context, productID uuid.UUID) (*models.DatasetProduct, error) {
	product, err := scanDatasetProduct(r.Pool.QueryRow(ctx, `SELECT id, name, source_dataset_rid, entity_type, version, project_id, published_by, export_includes_data, include_schema, include_branches, include_retention, include_schedules, manifest, bootstrap_mode, published_at, created_at FROM marketplace_dataset_products WHERE id = $1`, productID))
	if err != nil {
		return nil, mapPGError(err)
	}
	return product, nil
}

func (r *PGXRepository) InstallDatasetProduct(ctx context.Context, productID uuid.UUID, req models.InstallDatasetProductRequest) (*models.DatasetProductInstall, error) {
	if strings.TrimSpace(req.TargetDatasetRID) == "" {
		return nil, fmt.Errorf("%w: target_dataset_rid is required", ErrValidation)
	}
	product, err := r.GetDatasetProduct(ctx, productID)
	if err != nil {
		return nil, err
	}
	bootstrapMode := product.BootstrapMode
	if req.BootstrapMode != nil {
		bootstrapMode = *req.BootstrapMode
	}
	if _, ok := allowedBootstrapModes[bootstrapMode]; !ok {
		return nil, fmt.Errorf("%w: bootstrap_mode must be 'schema-only' or 'with-snapshot'", ErrValidation)
	}
	details, err := json.Marshal(map[string]any{"manifest_replay": product.Manifest, "source_dataset_rid": product.SourceDatasetRID, "version": product.Version})
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	now := time.Now().UTC()
	install, err := scanDatasetProductInstall(r.Pool.QueryRow(ctx, `INSERT INTO marketplace_dataset_product_installs (id, product_id, target_project_id, target_dataset_rid, bootstrap_mode, status, details, installed_by, created_at, completed_at) VALUES ($1, $2, $3, $4, $5, 'pending', $6::jsonb, $7, $8, NULL) RETURNING id, product_id, target_project_id, target_dataset_rid, bootstrap_mode, status, details, installed_by, created_at, completed_at`, id, productID, req.TargetProjectID, req.TargetDatasetRID, bootstrapMode, details, req.InstalledBy, now))
	if err != nil {
		return nil, mapPGError(err)
	}
	return install, nil
}

func (r *PGXRepository) AddScheduleManifest(ctx context.Context, req models.AddScheduleManifestRequest) (*models.AddScheduleManifestResponse, error) {
	if strings.TrimSpace(req.Manifest.Name) == "" {
		return nil, fmt.Errorf("%w: manifest.name is required", ErrValidation)
	}
	manifestJSON, err := json.Marshal(req.Manifest)
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	var returned uuid.UUID
	if err := r.Pool.QueryRow(ctx, `INSERT INTO marketplace_schedule_manifests (id, product_version_id, name, manifest_json) VALUES ($1, $2, $3, $4::jsonb) ON CONFLICT (product_version_id, name) DO UPDATE SET manifest_json = EXCLUDED.manifest_json RETURNING id`, id, req.ProductVersionID, req.Manifest.Name, manifestJSON).Scan(&returned); err != nil {
		return nil, mapPGError(err)
	}
	return &models.AddScheduleManifestResponse{ID: returned, ProductVersionID: req.ProductVersionID, Name: req.Manifest.Name}, nil
}

func (r *PGXRepository) MaterialiseInstallSchedules(ctx context.Context, req models.InstallSchedulesRequest) (*models.InstallSchedulesResponse, error) {
	rows, err := r.Pool.Query(ctx, `SELECT manifest_json FROM marketplace_schedule_manifests WHERE product_version_id = $1 ORDER BY name ASC`, req.ProductVersionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	selected := map[string]struct{}{}
	for _, name := range req.ActivateManifests {
		selected[name] = struct{}{}
	}
	out := []models.MaterialisedSchedule{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var manifest models.ScheduleManifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			continue
		}
		if len(selected) > 0 {
			if _, ok := selected[manifest.Name]; !ok {
				continue
			}
		}
		manifest.Trigger = rewriteJSONRIDs(manifest.Trigger, req.RidMapping)
		manifest.Target = rewriteJSONRIDs(manifest.Target, req.RidMapping)
		out = append(out, models.MaterialisedSchedule{Name: manifest.Name, Trigger: manifest.Trigger, Target: manifest.Target, ScopeKind: manifest.ScopeKind, Defaults: manifest.Defaults})
	}
	return &models.InstallSchedulesResponse{ProductVersionID: req.ProductVersionID, Materialised: out}, rows.Err()
}

func datasetManifest(req models.CreateDatasetProductRequest) models.DatasetProductManifest {
	retention := req.Retention
	if len(retention) == 0 || !req.IncludeRetention {
		retention = json.RawMessage(`[]`)
	}
	schema := req.Schema
	if !req.IncludeSchema {
		schema = nil
	}
	branching := req.BranchingPolicy
	if !req.IncludeBranches {
		branching = nil
	}
	schedules := req.Schedules
	if !req.IncludeSchedules {
		schedules = []string{}
	}
	return models.DatasetProductManifest{Entity: "dataset", Version: req.Version, Schema: schema, Retention: retention, BranchingPolicy: branching, Schedules: schedules, Bootstrap: models.DatasetProductBootstrap{Mode: req.BootstrapMode}}
}

func scanDatasetProduct(row scanner) (*models.DatasetProduct, error) {
	var p models.DatasetProduct
	var manifest []byte
	if err := row.Scan(&p.ID, &p.Name, &p.SourceDatasetRID, &p.EntityType, &p.Version, &p.ProjectID, &p.PublishedBy, &p.ExportIncludesData, &p.IncludeSchema, &p.IncludeBranches, &p.IncludeRetention, &p.IncludeSchedules, &manifest, &p.BootstrapMode, &p.PublishedAt, &p.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(manifest, &p.Manifest); err != nil {
		return nil, err
	}
	return &p, nil
}

func scanDatasetProductInstall(row scanner) (*models.DatasetProductInstall, error) {
	var i models.DatasetProductInstall
	if err := row.Scan(&i.ID, &i.ProductID, &i.TargetProjectID, &i.TargetDatasetRID, &i.BootstrapMode, &i.Status, &i.Details, &i.InstalledBy, &i.CreatedAt, &i.CompletedAt); err != nil {
		return nil, err
	}
	return &i, nil
}

func rewriteJSONRIDs(raw json.RawMessage, mapping models.RidMapping) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	rewriteValue(&value, mapping)
	out, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return out
}

func rewriteValue(value *any, mapping models.RidMapping) {
	switch v := (*value).(type) {
	case string:
		if replacement, ok := mapping.Pipeline[v]; ok {
			*value = replacement
			return
		}
		if replacement, ok := mapping.Dataset[v]; ok {
			*value = replacement
		}
	case []any:
		for i := range v {
			rewriteValue(&v[i], mapping)
		}
	case map[string]any:
		for key := range v {
			child := v[key]
			rewriteValue(&child, mapping)
			v[key] = child
		}
	}
}
