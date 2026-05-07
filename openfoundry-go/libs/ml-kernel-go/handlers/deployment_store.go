package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

type DeploymentStore interface {
	ListDeployments(ctx context.Context) ([]models.ModelDeployment, error)
	CreateDeployment(ctx context.Context, deployment models.ModelDeployment) (models.ModelDeployment, error)
	GetDeployment(ctx context.Context, id uuid.UUID) (*models.ModelDeployment, error)
	UpdateDeployment(ctx context.Context, deployment models.ModelDeployment) (models.ModelDeployment, error)
	SetActiveDeployment(ctx context.Context, modelID uuid.UUID, deploymentID *uuid.UUID) error
	ModelExists(ctx context.Context, modelID uuid.UUID) (bool, error)
	ModelVersionBelongs(ctx context.Context, modelID, versionID uuid.UUID) (bool, error)
}

func (h *DeploymentsHandlers) deploymentStore() DeploymentStore {
	if h.Store != nil {
		return h.Store
	}
	return &PGDeploymentStore{Pool: h.Pool}
}

type deploymentDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type PGDeploymentStore struct{ Pool deploymentDB }

func NewPGDeploymentStore(pool *pgxpool.Pool) *PGDeploymentStore {
	return &PGDeploymentStore{Pool: pool}
}

func (s *PGDeploymentStore) ListDeployments(ctx context.Context) ([]models.ModelDeployment, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+deploymentColumns+` FROM ml_deployments ORDER BY updated_at DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ModelDeployment, 0)
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *PGDeploymentStore) CreateDeployment(ctx context.Context, d models.ModelDeployment) (models.ModelDeployment, error) {
	splitsJSON, _ := json.Marshal(d.TrafficSplit)
	row := s.Pool.QueryRow(ctx, `INSERT INTO ml_deployments
              (id, model_id, name, status, strategy_type, endpoint_path, traffic_split, monitoring_window, baseline_dataset_id, drift_report)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL)
            RETURNING `+deploymentColumns,
		d.ID, d.ModelID, d.Name, d.Status, d.StrategyType, d.EndpointPath, splitsJSON, d.MonitoringWindow, d.BaselineDatasetID)
	return scanDeployment(row)
}

func (s *PGDeploymentStore) GetDeployment(ctx context.Context, id uuid.UUID) (*models.ModelDeployment, error) {
	row := s.Pool.QueryRow(ctx, `SELECT `+deploymentColumns+` FROM ml_deployments WHERE id = $1`, id)
	d, err := scanDeployment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *PGDeploymentStore) UpdateDeployment(ctx context.Context, d models.ModelDeployment) (models.ModelDeployment, error) {
	splitsJSON, _ := json.Marshal(d.TrafficSplit)
	driftJSON, _ := json.Marshal(d.DriftReport)
	row := s.Pool.QueryRow(ctx, `UPDATE ml_deployments SET
            name = $2, status = $3, strategy_type = $4, endpoint_path = $5,
            traffic_split = $6, monitoring_window = $7, baseline_dataset_id = $8,
            drift_report = $9, updated_at = NOW()
          WHERE id = $1
          RETURNING `+deploymentColumns,
		d.ID, d.Name, d.Status, d.StrategyType, d.EndpointPath, splitsJSON, d.MonitoringWindow, d.BaselineDatasetID, driftJSON)
	return scanDeployment(row)
}

func (s *PGDeploymentStore) SetActiveDeployment(ctx context.Context, modelID uuid.UUID, deploymentID *uuid.UUID) error {
	if deploymentID == nil {
		_, err := s.Pool.Exec(ctx, `UPDATE ml_models SET active_deployment_id = NULL, updated_at = NOW() WHERE id = $1`, modelID)
		return err
	}
	_, err := s.Pool.Exec(ctx, `UPDATE ml_models SET active_deployment_id = $2, updated_at = NOW() WHERE id = $1`, modelID, *deploymentID)
	return err
}

func (s *PGDeploymentStore) ModelExists(ctx context.Context, modelID uuid.UUID) (bool, error) {
	var exists bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ml_models WHERE id = $1)`, modelID).Scan(&exists)
	return exists, err
}

func (s *PGDeploymentStore) ModelVersionBelongs(ctx context.Context, modelID, versionID uuid.UUID) (bool, error) {
	var exists bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ml_model_versions WHERE id = $1 AND model_id = $2)`, versionID, modelID).Scan(&exists)
	return exists, err
}

type FakeDeploymentStore struct {
	mu          sync.RWMutex
	models      map[uuid.UUID]models.RegisteredModel
	versions    map[uuid.UUID]models.ModelVersion
	deployments map[uuid.UUID]models.ModelDeployment
}

func NewFakeDeploymentStore() *FakeDeploymentStore {
	return &FakeDeploymentStore{models: map[uuid.UUID]models.RegisteredModel{}, versions: map[uuid.UUID]models.ModelVersion{}, deployments: map[uuid.UUID]models.ModelDeployment{}}
}

func (s *FakeDeploymentStore) SeedModel(model models.RegisteredModel, versions ...models.ModelVersion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensure()
	if model.ID == uuid.Nil {
		model.ID = uuid.New()
	}
	now := time.Now().UTC()
	if model.CreatedAt.IsZero() {
		model.CreatedAt = now
	}
	model.UpdatedAt = now
	s.models[model.ID] = model
	for _, v := range versions {
		if v.ID == uuid.Nil {
			v.ID = uuid.New()
		}
		if v.ModelID == uuid.Nil {
			v.ModelID = model.ID
		}
		s.versions[v.ID] = v
	}
}

func (s *FakeDeploymentStore) ListDeployments(context.Context) ([]models.ModelDeployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.ModelDeployment, 0, len(s.deployments))
	for _, d := range s.deployments {
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}
func (s *FakeDeploymentStore) CreateDeployment(_ context.Context, d models.ModelDeployment) (models.ModelDeployment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensure()
	now := time.Now().UTC()
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	d.CreatedAt = now
	d.UpdatedAt = now
	s.deployments[d.ID] = d
	return d, nil
}
func (s *FakeDeploymentStore) GetDeployment(_ context.Context, id uuid.UUID) (*models.ModelDeployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.deployments[id]
	if !ok {
		return nil, nil
	}
	return &d, nil
}
func (s *FakeDeploymentStore) UpdateDeployment(_ context.Context, d models.ModelDeployment) (models.ModelDeployment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.deployments[d.ID]
	if !ok {
		return models.ModelDeployment{}, pgx.ErrNoRows
	}
	d.CreatedAt = current.CreatedAt
	d.UpdatedAt = time.Now().UTC()
	s.deployments[d.ID] = d
	return d, nil
}
func (s *FakeDeploymentStore) SetActiveDeployment(_ context.Context, modelID uuid.UUID, deploymentID *uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.models[modelID]
	if !ok {
		return pgx.ErrNoRows
	}
	m.ActiveDeploymentID = deploymentID
	m.UpdatedAt = time.Now().UTC()
	s.models[modelID] = m
	return nil
}
func (s *FakeDeploymentStore) ModelExists(_ context.Context, modelID uuid.UUID) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.models[modelID]
	return ok, nil
}
func (s *FakeDeploymentStore) ModelVersionBelongs(_ context.Context, modelID, versionID uuid.UUID) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.versions[versionID]
	return ok && v.ModelID == modelID, nil
}

func (s *FakeDeploymentStore) ensure() {
	if s.models == nil {
		s.models = map[uuid.UUID]models.RegisteredModel{}
	}
	if s.versions == nil {
		s.versions = map[uuid.UUID]models.ModelVersion{}
	}
	if s.deployments == nil {
		s.deployments = map[uuid.UUID]models.ModelDeployment{}
	}
}
