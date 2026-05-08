package workers

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

type AutoRegistrationStore interface {
	ListConnections(ctx context.Context, ownerID *uuid.UUID) ([]models.Connection, error)
	UpsertRegistration(ctx context.Context, sourceID uuid.UUID, source models.DiscoveredSource, mode string, autoSync bool, updateDetection bool, targetDatasetID *uuid.UUID, metadata json.RawMessage) (*models.ConnectionRegistration, error)
	GetRegistrationSignature(ctx context.Context, sourceID uuid.UUID, selector string) (*string, error)
	RecordRegistrationSignature(ctx context.Context, sourceID uuid.UUID, selector string, signature *string) error
}

type DiscoveryFunc func(context.Context, *models.Connection) ([]models.DiscoveredSource, error)

type AutoRegistrationWorker struct {
	Store    AutoRegistrationStore
	Clock    Clock
	Discover DiscoveryFunc
	Recorder *AutoRegistrationRecorder
}

type ConnectionTickRecord struct {
	ConnectionID     uuid.UUID      `json:"connection_id"`
	StartedAt        time.Time      `json:"started_at"`
	FinishedAt       time.Time      `json:"finished_at"`
	Discovered       int            `json:"discovered"`
	Registered       int            `json:"registered"`
	Errors           int            `json:"errors"`
	UpdateBreakdown  map[string]int `json:"update_breakdown"`
	ChangedSelectors []string       `json:"changed_selectors"`
	LastError        *string        `json:"last_error,omitempty"`
}

type AutoRegistrationRecorder struct {
	mu   sync.RWMutex
	runs map[uuid.UUID]ConnectionTickRecord
}

func NewAutoRegistrationRecorder() *AutoRegistrationRecorder {
	return &AutoRegistrationRecorder{runs: map[uuid.UUID]ConnectionTickRecord{}}
}

var DefaultAutoRegistrationRecorder = NewAutoRegistrationRecorder()

func (r *AutoRegistrationRecorder) Record(record ConnectionTickRecord) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[record.ConnectionID] = record
}

func (r *AutoRegistrationRecorder) LastRun(id uuid.UUID) *ConnectionTickRecord {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if v, ok := r.runs[id]; ok {
		return &v
	}
	return nil
}

type AutoRegistrationSummary struct {
	Scanned    int
	Registered int
	Errors     int
}

type AutoRegistrationSettings struct {
	Enabled          bool
	IntervalSeconds  uint64
	RegistrationMode string
	AutoSync         bool
	UpdateDetection  bool
	Selectors        []string
}

type AutoRegistrationSettingsView struct {
	Enabled          bool     `json:"enabled"`
	RegistrationMode string   `json:"registration_mode"`
	AutoSync         bool     `json:"auto_sync"`
	UpdateDetection  bool     `json:"update_detection"`
	Selectors        []string `json:"selectors"`
}

func AutoRegistrationSettingsFromConfig(config json.RawMessage) (*AutoRegistrationSettings, bool) {
	var cfg map[string]json.RawMessage
	if json.Unmarshal(config, &cfg) != nil {
		return nil, false
	}
	raw, ok := cfg["auto_registration"]
	if !ok {
		return nil, false
	}
	var block struct {
		Enabled          bool     `json:"enabled"`
		IntervalSeconds  uint64   `json:"interval_secs"`
		RegistrationMode string   `json:"registration_mode"`
		AutoSync         bool     `json:"auto_sync"`
		UpdateDetection  *bool    `json:"update_detection"`
		Selectors        []string `json:"selectors"`
	}
	if json.Unmarshal(raw, &block) != nil {
		return nil, false
	}
	settings := &AutoRegistrationSettings{Enabled: block.Enabled, IntervalSeconds: block.IntervalSeconds, RegistrationMode: block.RegistrationMode, AutoSync: block.AutoSync, Selectors: block.Selectors}
	if settings.RegistrationMode == "" {
		settings.RegistrationMode = "sync"
	}
	settings.UpdateDetection = true
	if block.UpdateDetection != nil {
		settings.UpdateDetection = *block.UpdateDetection
	}
	return settings, true
}

func AutoRegistrationSettingsViewFromConfig(config json.RawMessage) *AutoRegistrationSettingsView {
	settings, ok := AutoRegistrationSettingsFromConfig(config)
	if !ok {
		return nil
	}
	return &AutoRegistrationSettingsView{Enabled: settings.Enabled, RegistrationMode: settings.RegistrationMode, AutoSync: settings.AutoSync, UpdateDetection: settings.UpdateDetection, Selectors: settings.Selectors}
}

func (w *AutoRegistrationWorker) RunOnce(ctx context.Context) (AutoRegistrationSummary, error) {
	var summary AutoRegistrationSummary
	connections, err := w.Store.ListConnections(ctx, nil)
	if err != nil {
		return summary, err
	}
	clock := w.Clock
	if clock == nil {
		clock = RealClock{}
	}
	discover := w.Discover
	if discover == nil {
		discover = func(_ context.Context, c *models.Connection) ([]models.DiscoveredSource, error) {
			return domain.DiscoverConnectionSources(c), nil
		}
	}
	recorder := w.Recorder
	if recorder == nil {
		recorder = DefaultAutoRegistrationRecorder
	}
	for i := range connections {
		conn := &connections[i]
		settings, ok := AutoRegistrationSettingsFromConfig(conn.Config)
		if !ok || !settings.Enabled {
			continue
		}
		summary.Scanned++
		started := clock.Now()
		record := ConnectionTickRecord{ConnectionID: conn.ID, StartedAt: started, UpdateBreakdown: map[string]int{}, ChangedSelectors: []string{}}
		discovered, err := discover(ctx, conn)
		if err != nil {
			summary.Errors++
			record.Errors = 1
			msg := err.Error()
			record.LastError = &msg
			record.FinishedAt = clock.Now()
			recorder.Record(record)
			continue
		}
		record.Discovered = len(discovered)
		modePtr := settings.RegistrationMode
		mode, err := domain.NormalizeRegistrationMode(&modePtr)
		if err != nil {
			summary.Errors++
			record.Errors = 1
			msg := err.Error()
			record.LastError = &msg
			record.FinishedAt = clock.Now()
			recorder.Record(record)
			continue
		}
		for _, source := range domain.SelectSources(discovered, settings.Selectors) {
			previous, err := w.Store.GetRegistrationSignature(ctx, conn.ID, source.Selector)
			var outcome domain.UpdateOutcome
			if err != nil {
				msg := err.Error()
				record.LastError = &msg
				outcome = domain.UpdateOutcome{Selector: source.Selector, State: domain.UpdateStateUnknown, CurrentSignature: source.SourceSignature}
			} else {
				outcome = domain.EvaluateUpdate(source.Selector, previous, source.SourceSignature)
			}
			record.UpdateBreakdown[string(outcome.State)]++
			if settings.UpdateDetection && outcome.State == domain.UpdateStateUnchanged {
				continue
			}
			metadata, _ := json.Marshal(map[string]any{"origin": "auto_registration_scheduler", "update_state": string(outcome.State), "previous_signature": outcome.PreviousSignature, "current_signature": outcome.CurrentSignature})
			if _, err := w.Store.UpsertRegistration(ctx, conn.ID, source, mode, settings.AutoSync, settings.UpdateDetection, nil, metadata); err != nil {
				summary.Errors++
				record.Errors++
				msg := err.Error()
				record.LastError = &msg
				continue
			}
			summary.Registered++
			record.Registered++
			if outcome.State == domain.UpdateStateChanged {
				record.ChangedSelectors = append(record.ChangedSelectors, source.Selector)
			}
			_ = w.Store.RecordRegistrationSignature(ctx, conn.ID, source.Selector, source.SourceSignature)
		}
		record.FinishedAt = clock.Now()
		recorder.Record(record)
	}
	return summary, nil
}

func (w *AutoRegistrationWorker) RunLoop(ctx context.Context, interval time.Duration) error {
	return sleepLoop(ctx, w.Clock, interval, func(ctx context.Context) error { _, err := w.RunOnce(ctx); return err })
}
