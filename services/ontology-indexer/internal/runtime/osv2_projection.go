package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// StorageProjector writes OSV2 primary object/link projections from the same
// Kafka events that feed lexical/vector search. It is deliberately separate
// from SearchBackend so deployments can enable row materialisation without
// changing the configured search backend.
type StorageProjector struct {
	Objects repos.ObjectStore
	Links   repos.LinkStore
}

// Enabled reports whether at least one OSV2 row store is configured.
func (p StorageProjector) Enabled() bool { return p.Objects != nil || p.Links != nil }

// ProcessMessageWithStores applies OSV2 row projections first and then applies
// the search projection. Both sides share the ProjectionIndex, so duplicate
// event ids and stale versions collapse before offsets are committed.
func ProcessMessageWithStores(ctx context.Context, backend repos.SearchBackend, stores StorageProjector, projector *ProjectionIndex, msg KafkaMessage, log *slog.Logger) (RecordOutcome, error) {
	if projector != nil && !projector.ShouldApplyEvent(eventIDFromMessage(msg)) {
		return OutcomeSkippedStale, nil
	}
	if stores.Enabled() {
		if outcome, err := stores.apply(ctx, projector, msg); err != nil || outcome == OutcomeDecodeError || outcome == OutcomeSkippedStale {
			return outcome, err
		}
	}
	outcome, err := ProcessMessageWithProjector(ctx, backend, projector, msg, log)
	if err == nil && projector != nil {
		projector.MarkEventApplied(eventIDFromMessage(msg))
	}
	return outcome, err
}

func (p StorageProjector) apply(ctx context.Context, projector *ProjectionIndex, msg KafkaMessage) (RecordOutcome, error) {
	switch msg.Topic {
	case TopicObjectChangedV1:
		return p.applyObject(ctx, projector, msg)
	case TopicLinkChangedV1:
		return p.applyLink(ctx, projector, msg)
	default:
		return OutcomeDecodeError, nil
	}
}

func (p StorageProjector) applyObject(ctx context.Context, projector *ProjectionIndex, msg KafkaMessage) (RecordOutcome, error) {
	if p.Objects == nil {
		return OutcomeIndexed, nil
	}
	var evt ObjectChangedV1
	if err := json.Unmarshal(msg.Value, &evt); err != nil || evt.Tenant == "" || evt.ID == "" || evt.TypeID == "" {
		return OutcomeDecodeError, nil
	}
	if !projector.ShouldApply(evt.Tenant, evt.ID, evt.Version) {
		return OutcomeSkippedStale, nil
	}
	current, err := p.Objects.Get(ctx, evt.Tenant, evt.ID, repos.Strong())
	if err != nil {
		return OutcomeIndexed, fmt.Errorf("load existing object projection: %w", err)
	}
	if current != nil && current.Version >= evt.Version {
		projector.MarkApplied(evt.Tenant, evt.ID, current.Version)
		return OutcomeSkippedStale, nil
	}
	if evt.Deleted {
		_, err := p.Objects.Delete(ctx, evt.Tenant, evt.ID)
		return OutcomeDeleted, err
	}
	expected := uint64(0)
	var expectedPtr *uint64
	if current != nil {
		expected = current.Version
		expectedPtr = &expected
	}
	payload := evt.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	now := msg.Time
	if now.IsZero() {
		now = time.Now().UTC()
	}
	obj := repos.Object{Tenant: evt.Tenant, ID: evt.ID, TypeID: evt.TypeID, Version: evt.Version, Payload: cloneRaw(payload), UpdatedAtMs: now.UnixMilli()}
	outcome, err := p.Objects.Put(ctx, obj, expectedPtr)
	if err != nil {
		return OutcomeIndexed, err
	}
	if outcome.Kind == repos.PutVersionConflict && outcome.ActualVersion != evt.Version {
		return OutcomeSkippedStale, nil
	}
	return OutcomeIndexed, nil
}

func (p StorageProjector) applyLink(ctx context.Context, projector *ProjectionIndex, msg KafkaMessage) (RecordOutcome, error) {
	if p.Links == nil {
		return OutcomeIndexed, nil
	}
	var evt LinkChangedV1
	if err := json.Unmarshal(msg.Value, &evt); err != nil {
		return OutcomeDecodeError, nil
	}
	normalizeLinkEvent(&evt)
	if evt.Tenant == "" || evt.LinkType == "" || evt.From == "" || evt.To == "" {
		return OutcomeDecodeError, nil
	}
	id := linkDocumentID(evt)
	if !projector.ShouldApply(evt.Tenant, id, evt.Version) {
		return OutcomeSkippedStale, nil
	}
	if evt.Deleted {
		_, err := p.Links.Delete(ctx, evt.Tenant, evt.LinkType, evt.From, evt.To)
		return OutcomeDeleted, err
	}
	payload := evt.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	createdAt := msg.Time
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	if err := p.Links.Put(ctx, repos.Link{Tenant: evt.Tenant, LinkType: evt.LinkType, From: evt.From, To: evt.To, Payload: cloneRaw(payload), CreatedAtMs: createdAt.UnixMilli()}); err != nil {
		return OutcomeIndexed, err
	}
	return OutcomeIndexed, nil
}

func eventIDFromMessage(msg KafkaMessage) string {
	switch msg.Topic {
	case TopicObjectChangedV1:
		var evt ObjectChangedV1
		if json.Unmarshal(msg.Value, &evt) == nil {
			return evt.EventID
		}
	case TopicLinkChangedV1:
		var evt LinkChangedV1
		if json.Unmarshal(msg.Value, &evt) == nil {
			return evt.EventID
		}
	}
	return ""
}
