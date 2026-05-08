package lineage

import (
	"context"
	"crypto/sha1" //nolint:gosec // Used as a deterministic hash, not for security.
	"encoding/binary"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
)

// RelationWriteInput captures the input shape for [PersistRelation].
type RelationWriteInput struct {
	SourceID         uuid.UUID
	SourceKind       models.NodeKind
	TargetID         uuid.UUID
	TargetKind       models.NodeKind
	RelationKind     string
	ProducerKey      string
	PipelineID       *uuid.UUID
	WorkflowID       *uuid.UUID
	NodeID           *string
	StepID           *string
	ExplicitMarking  *string
	Metadata         json.RawMessage
}

// PersistRelation ports `persist_relation`. Computes the effective
// marking from the source/target overlays + any explicit override,
// derives a deterministic relation id from the producer key, and
// hands off to the runtime store.
func PersistRelation(ctx context.Context, state *AppState, in RelationWriteInput) error {
	source, err := GetNodeRecord(ctx, state.DB, in.SourceID, in.SourceKind)
	if err != nil {
		return err
	}
	target, err := GetNodeRecord(ctx, state.DB, in.TargetID, in.TargetKind)
	if err != nil {
		return err
	}

	values := []*string{}
	if source != nil {
		s := source.Marking
		values = append(values, &s)
	}
	if target != nil {
		t := target.Marking
		values = append(values, &t)
	}
	if in.ExplicitMarking != nil {
		values = append(values, in.ExplicitMarking)
	}
	effective := MaxMarkings(values)

	metadata := in.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}

	return state.Store.RecordRelation(ctx, models.LineageRelationRecord{
		ID:               relationIDFromProducerKey(in.ProducerKey),
		SourceID:         in.SourceID,
		SourceKind:       in.SourceKind.String(),
		TargetID:         in.TargetID,
		TargetKind:       in.TargetKind.String(),
		RelationKind:     in.RelationKind,
		PipelineID:       in.PipelineID,
		WorkflowID:       in.WorkflowID,
		NodeID:           in.NodeID,
		StepID:           in.StepID,
		EffectiveMarking: effective,
		Metadata:         metadata,
		CreatedAt:        time.Now().UTC(),
	})
}

// relationIDFromProducerKey ports the Rust `relation_id_from_producer_key`.
//
// The Rust impl runs the producer key through two `DefaultHasher`
// instances (with distinct salts) and concatenates the resulting
// 16 bytes into a UUID. We use SHA-1 of `salt || producer_key` for
// the same shape: a 16-byte deterministic identifier per producer
// key. The two outputs are not byte-equal across runtimes, but each
// is **deterministic within its own runtime** which is the only
// invariant the runtime store relies on (the `producer_key` itself
// is the de-dup mechanism — the UUID is just an opaque handle).
func relationIDFromProducerKey(producerKey string) uuid.UUID {
	first := sha1.Sum([]byte("lineage-runtime:0|" + producerKey))   //nolint:gosec
	second := sha1.Sum([]byte("lineage-runtime:1|" + producerKey)) //nolint:gosec
	var bytes [16]byte
	binary.BigEndian.PutUint64(bytes[:8], binary.BigEndian.Uint64(first[:8]))
	binary.BigEndian.PutUint64(bytes[8:], binary.BigEndian.Uint64(second[:8]))
	return uuid.UUID(bytes)
}
