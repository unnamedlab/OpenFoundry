package outbox

// lineage_event.go ports libs/outbox/src/lineage_event.rs.
//
// OpenLineage v1 event helper for `lineage.events.v1`.
//
// Producers (pipeline-build, pipeline-schedule, workflow-automation,
// ontology-actions) build a *LineageEvent with NewLineageEvent and
// hand it to EnqueueLineageEvent, which serialises an OpenLineage 1.x
// RunEvent JSON payload, derives a deterministic event id, attaches
// the canonical `ol-*` headers and forwards to Enqueue inside the
// caller's transaction.
//
// Reference: https://openlineage.io/spec/1-0-5/OpenLineage.json
//
// The Iceberg materialisation in `lineage-service::runtime::decode_event`
// (now lineage-service/internal/runtime/decode_event.go) consumes
// this exact shape: eventType, eventTime, producer, schemaURL,
// run.runId, job.namespace, job.name, inputs[], outputs[].

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TopicLineageEvents is the Kafka topic lineage-service consumes for
// Iceberg materialisation.
const TopicLineageEvents = "lineage.events.v1"

// LineageSchemaURL is the OpenLineage spec URL referenced in every
// emitted event.
const LineageSchemaURL = "https://openlineage.io/spec/1-0-5/OpenLineage.json"

// LineageProducer is the producer URI announced in the OL `producer`
// field. Pinned to the repository so consumers can attribute events
// to OpenFoundry without guessing.
const LineageProducer = "https://github.com/open-foundry/openfoundry"

// LineageAggregate is the value written to outbox.events.aggregate
// for lineage records. Kept separate from the per-service aggregates
// ("build", "schedule_run", "workflow_run", "ontology_object") so
// consumers can filter the lineage stream without parsing payloads.
const LineageAggregate = "lineage_run"

// lineageEventIDNamespace is the v5 UUID namespace for deterministic
// OL event ids. Same bytes as the Rust EVENT_ID_NAMESPACE — flipping
// any of (run_id, event_type, event_time, namespace, job) produces a
// new id while replays of the same call collapse via the outbox PK.
// DO NOT CHANGE without coordinating a lineage-service migration.
var lineageEventIDNamespace = uuid.UUID{
	0x6e, 0x18, 0x5a, 0x0b, 0x0c, 0x77, 0x5c, 0x6d,
	0x9d, 0x77, 0x4e, 0x14, 0xc1, 0x2a, 0x71, 0x83,
}

// LineageEventType is the OpenLineage event-type wire form. The
// Iceberg consumer normalises to upper-case before storing, so the
// constants are upper-case strings — the wire form *is* the type.
type LineageEventType string

// OpenLineage event-type constants.
const (
	LineageStart    LineageEventType = "START"
	LineageRunning  LineageEventType = "RUNNING"
	LineageComplete LineageEventType = "COMPLETE"
	LineageFail     LineageEventType = "FAIL"
	LineageAbort    LineageEventType = "ABORT"
)

// IsTerminal reports whether the event terminates the run in
// OpenLineage's state machine. The lineage-service sets
// `completed_at` from the event time when this returns true.
func (t LineageEventType) IsTerminal() bool {
	switch t {
	case LineageComplete, LineageFail, LineageAbort:
		return true
	}
	return false
}

// LineageDataset is a minimal OpenLineage dataset reference. Facets
// is free-form JSON (e.g. schema, dataSource) and may be omitted on
// first emit.
type LineageDataset struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Facets    any    `json:"facets,omitempty"`
}

// NewLineageDataset is the value-style constructor. Use WithFacets
// to attach an optional facet payload.
func NewLineageDataset(namespace, name string) LineageDataset {
	return LineageDataset{Namespace: namespace, Name: name}
}

// WithFacets returns a copy with `facets` attached.
func (d LineageDataset) WithFacets(facets any) LineageDataset {
	d.Facets = facets
	return d
}

// LineageEvent is the in-memory builder for an OpenLineage RunEvent.
// Hand to EnqueueLineageEvent once populated.
type LineageEvent struct {
	EventType    LineageEventType
	EventTime    time.Time
	RunID        uuid.UUID
	ParentRunID  *uuid.UUID
	JobNamespace string
	JobName      string
	Inputs       []LineageDataset
	Outputs      []LineageDataset
	RunFacets    map[string]any
	JobFacets    map[string]any
}

// NewLineageEvent constructs a LineageEvent with EventTime set to
// time.Now().UTC(). Use At to override.
func NewLineageEvent(
	eventType LineageEventType,
	runID uuid.UUID,
	jobNamespace, jobName string,
) *LineageEvent {
	return &LineageEvent{
		EventType:    eventType,
		EventTime:    time.Now().UTC(),
		RunID:        runID,
		JobNamespace: jobNamespace,
		JobName:      jobName,
		Inputs:       []LineageDataset{},
		Outputs:      []LineageDataset{},
		RunFacets:    map[string]any{},
		JobFacets:    map[string]any{},
	}
}

// At overrides the event time. Useful when replaying historical
// events or stitching across services that already captured a
// timestamp.
func (e *LineageEvent) At(t time.Time) *LineageEvent {
	e.EventTime = t.UTC()
	return e
}

// WithParent sets the parent run id, which expands into a `parent`
// run facet in the rendered payload.
func (e *LineageEvent) WithParent(parentRunID uuid.UUID) *LineageEvent {
	id := parentRunID
	e.ParentRunID = &id
	return e
}

// WithInput appends `dataset` to Inputs.
func (e *LineageEvent) WithInput(dataset LineageDataset) *LineageEvent {
	e.Inputs = append(e.Inputs, dataset)
	return e
}

// WithOutput appends `dataset` to Outputs.
func (e *LineageEvent) WithOutput(dataset LineageDataset) *LineageEvent {
	e.Outputs = append(e.Outputs, dataset)
	return e
}

// WithRunFacet attaches a free-form JSON facet to the run.
func (e *LineageEvent) WithRunFacet(name string, facet any) *LineageEvent {
	if e.RunFacets == nil {
		e.RunFacets = map[string]any{}
	}
	e.RunFacets[name] = facet
	return e
}

// WithJobFacet attaches a free-form JSON facet to the job.
func (e *LineageEvent) WithJobFacet(name string, facet any) *LineageEvent {
	if e.JobFacets == nil {
		e.JobFacets = map[string]any{}
	}
	e.JobFacets[name] = facet
	return e
}

// ToPayload renders the OpenLineage 1.x JSON payload that downstream
// consumers (and the lineage-service Iceberg writer) decode.
func (e *LineageEvent) ToPayload() (json.RawMessage, error) {
	run := map[string]any{
		"runId": e.RunID.String(),
	}
	switch {
	case e.ParentRunID != nil:
		run["facets"] = mergeRunFacets(e.RunFacets, *e.ParentRunID, e.JobNamespace, e.JobName)
	case len(e.RunFacets) > 0:
		run["facets"] = e.RunFacets
	}

	job := map[string]any{
		"namespace": e.JobNamespace,
		"name":      e.JobName,
	}
	if len(e.JobFacets) > 0 {
		job["facets"] = e.JobFacets
	}

	payload := map[string]any{
		"eventType": string(e.EventType),
		"eventTime": formatLineageEventTime(e.EventTime),
		"producer":  LineageProducer,
		"schemaURL": LineageSchemaURL,
		"run":       run,
		"job":       job,
		"inputs":    e.Inputs,
		"outputs":   e.Outputs,
	}
	return json.Marshal(payload)
}

// formatLineageEventTime renders the time in the same RFC3339 form
// that chrono's DateTime<Utc>::to_rfc3339 emits — UTC offset spelled
// `+00:00` (not `Z`), nanosecond precision with trailing-zero
// stripping.
func formatLineageEventTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.999999999-07:00")
}

func (e *LineageEvent) deriveEventID() uuid.UUID {
	key := fmt.Sprintf("%s|%s|%d|%s|%s",
		e.RunID.String(),
		string(e.EventType),
		e.EventTime.UnixMicro(),
		e.JobNamespace,
		e.JobName,
	)
	return uuid.NewSHA1(lineageEventIDNamespace, []byte(key))
}

// mergeRunFacets clones `existing` and adds the canonical OL
// `parent` facet. Mirrors the Rust merge_run_facets helper.
func mergeRunFacets(existing map[string]any, parentRunID uuid.UUID, parentNamespace, parentName string) map[string]any {
	out := make(map[string]any, len(existing)+1)
	for k, v := range existing {
		out[k] = v
	}
	out["parent"] = map[string]any{
		"_producer":  LineageProducer,
		"_schemaURL": "https://openlineage.io/spec/facets/1-0-0/ParentRunFacet.json",
		"run": map[string]any{
			"runId": parentRunID.String(),
		},
		"job": map[string]any{
			"namespace": parentNamespace,
			"name":      parentName,
		},
	}
	return out
}

// EnqueueLineageEvent appends `event` to outbox.events with topic
// `lineage.events.v1`. Adds the canonical `ol-*` headers
// (`ol-run-id`, `ol-namespace`, `ol-job`, plus `ol-parent-run-id`
// when present) so consumers can filter without deserialising the
// payload.
//
// The caller still owns the surrounding transaction — pair with the
// primary write so a single tx.Commit() atomically publishes both.
func EnqueueLineageEvent(ctx context.Context, tx pgx.Tx, event *LineageEvent) error {
	eventID := event.deriveEventID()
	payload, err := event.ToPayload()
	if err != nil {
		return fmt.Errorf("render lineage payload: %w", err)
	}
	ev := New(eventID, LineageAggregate, event.RunID.String(), TopicLineageEvents, payload).
		WithHeader("ol-run-id", event.RunID.String()).
		WithHeader("ol-namespace", event.JobNamespace).
		WithHeader("ol-job", event.JobName)
	if event.ParentRunID != nil {
		ev.WithHeader("ol-parent-run-id", event.ParentRunID.String())
	}
	return Enqueue(ctx, tx, ev)
}
