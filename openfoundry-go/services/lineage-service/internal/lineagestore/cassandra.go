package lineagestore

import (
	"context"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
)

// Keyspace is the Cassandra keyspace owning the lineage runtime tables.
const Keyspace = "lineage_runtime"

const runtimePartition = "runtime"

// DDL strings — verbatim ports of tracker.rs so on-disk schemas line
// up across runtimes. Run at boot via [CassandraStore.Migrate].
const (
	keyspaceDDL = `CREATE KEYSPACE IF NOT EXISTS lineage_runtime
		WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}`

	relationsBySourceDDL = `CREATE TABLE IF NOT EXISTS lineage_runtime.relations_by_source (
		source_kind         text,
		source_id           uuid,
		relation_id         uuid,
		source_kind_copy    text,
		target_id           uuid,
		target_kind         text,
		relation_kind       text,
		pipeline_id         uuid,
		workflow_id         uuid,
		node_id             text,
		step_id             text,
		effective_marking   text,
		metadata            text,
		created_at          timestamp,
		PRIMARY KEY ((source_kind, source_id), relation_id)
	)`

	relationsByTargetDDL = `CREATE TABLE IF NOT EXISTS lineage_runtime.relations_by_target (
		target_kind         text,
		target_id           uuid,
		relation_id         uuid,
		source_id           uuid,
		source_kind         text,
		target_kind_copy    text,
		relation_kind       text,
		pipeline_id         uuid,
		workflow_id         uuid,
		node_id             text,
		step_id             text,
		effective_marking   text,
		metadata            text,
		created_at          timestamp,
		PRIMARY KEY ((target_kind, target_id), relation_id)
	)`

	relationsAllDDL = `CREATE TABLE IF NOT EXISTS lineage_runtime.relations_all (
		partition_key       text,
		relation_id         uuid,
		source_id           uuid,
		source_kind         text,
		target_id           uuid,
		target_kind         text,
		relation_kind       text,
		pipeline_id         uuid,
		workflow_id         uuid,
		node_id             text,
		step_id             text,
		effective_marking   text,
		metadata            text,
		created_at          timestamp,
		PRIMARY KEY ((partition_key), relation_id)
	)`

	relationsByWorkflowDDL = `CREATE TABLE IF NOT EXISTS lineage_runtime.relations_by_workflow (
		workflow_id         uuid,
		relation_id         uuid,
		source_id           uuid,
		source_kind         text,
		target_id           uuid,
		target_kind         text,
		relation_kind       text,
		pipeline_id         uuid,
		node_id             text,
		step_id             text,
		effective_marking   text,
		metadata            text,
		created_at          timestamp,
		PRIMARY KEY ((workflow_id), relation_id)
	)`

	columnRelationsByDatasetDDL = `CREATE TABLE IF NOT EXISTS lineage_runtime.column_relations_by_dataset (
		dataset_id          uuid,
		relation_id         uuid,
		source_dataset_id   uuid,
		source_column       text,
		target_dataset_id   uuid,
		target_column       text,
		pipeline_id         uuid,
		node_id             text,
		created_at          timestamp,
		PRIMARY KEY ((dataset_id), relation_id)
	)`
)

// CassandraStore persists lineage relations to Cassandra/Scylla.
type CassandraStore struct {
	session *gocql.Session
}

// NewCassandraStore wraps an existing gocql session.
func NewCassandraStore(session *gocql.Session) *CassandraStore {
	return &CassandraStore{session: session}
}

// Migrate runs every CREATE statement (idempotent).
func (s *CassandraStore) Migrate(ctx context.Context) error {
	for _, ddl := range []string{
		keyspaceDDL,
		relationsBySourceDDL,
		relationsByTargetDDL,
		relationsAllDDL,
		relationsByWorkflowDDL,
		columnRelationsByDatasetDDL,
	} {
		if err := s.session.Query(ddl).WithContext(ctx).Exec(); err != nil {
			return fmt.Errorf("lineage runtime DDL failed: %w", err)
		}
	}
	return nil
}

func (s *CassandraStore) RecordRelation(ctx context.Context, r models.LineageRelationRecord) error {
	metadata, err := jsonForCassandra(r.Metadata)
	if err != nil {
		return err
	}

	if err := s.session.Query(
		`INSERT INTO lineage_runtime.relations_by_source
		 (source_kind, source_id, relation_id, source_kind_copy, target_id, target_kind,
		  relation_kind, pipeline_id, workflow_id, node_id, step_id, effective_marking,
		  metadata, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.SourceKind, r.SourceID, r.ID, r.SourceKind, r.TargetID, r.TargetKind,
		r.RelationKind, optUUID(r.PipelineID), optUUID(r.WorkflowID),
		optStr(r.NodeID), optStr(r.StepID), r.EffectiveMarking,
		metadata, r.CreatedAt,
	).WithContext(ctx).Exec(); err != nil {
		return err
	}

	if err := s.session.Query(
		`INSERT INTO lineage_runtime.relations_by_target
		 (target_kind, target_id, relation_id, source_id, source_kind, target_kind_copy,
		  relation_kind, pipeline_id, workflow_id, node_id, step_id, effective_marking,
		  metadata, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TargetKind, r.TargetID, r.ID, r.SourceID, r.SourceKind, r.TargetKind,
		r.RelationKind, optUUID(r.PipelineID), optUUID(r.WorkflowID),
		optStr(r.NodeID), optStr(r.StepID), r.EffectiveMarking,
		metadata, r.CreatedAt,
	).WithContext(ctx).Exec(); err != nil {
		return err
	}

	if err := s.session.Query(
		`INSERT INTO lineage_runtime.relations_all
		 (partition_key, relation_id, source_id, source_kind, target_id, target_kind,
		  relation_kind, pipeline_id, workflow_id, node_id, step_id, effective_marking,
		  metadata, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runtimePartition, r.ID, r.SourceID, r.SourceKind, r.TargetID, r.TargetKind,
		r.RelationKind, optUUID(r.PipelineID), optUUID(r.WorkflowID),
		optStr(r.NodeID), optStr(r.StepID), r.EffectiveMarking,
		metadata, r.CreatedAt,
	).WithContext(ctx).Exec(); err != nil {
		return err
	}

	if r.WorkflowID != nil {
		if err := s.session.Query(
			`INSERT INTO lineage_runtime.relations_by_workflow
			 (workflow_id, relation_id, source_id, source_kind, target_id, target_kind,
			  relation_kind, pipeline_id, node_id, step_id, effective_marking, metadata,
			  created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			*r.WorkflowID, r.ID, r.SourceID, r.SourceKind, r.TargetID, r.TargetKind,
			r.RelationKind, optUUID(r.PipelineID),
			optStr(r.NodeID), optStr(r.StepID), r.EffectiveMarking,
			metadata, r.CreatedAt,
		).WithContext(ctx).Exec(); err != nil {
			return err
		}
	}

	if edge, ok := columnEdgeFromRelation(r); ok {
		for _, datasetID := range []uuid.UUID{edge.SourceDatasetID, edge.TargetDatasetID} {
			if err := s.session.Query(
				`INSERT INTO lineage_runtime.column_relations_by_dataset
				 (dataset_id, relation_id, source_dataset_id, source_column,
				  target_dataset_id, target_column, pipeline_id, node_id, created_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				datasetID, edge.ID, edge.SourceDatasetID, edge.SourceColumn,
				edge.TargetDatasetID, edge.TargetColumn, optUUID(edge.PipelineID),
				optStr(edge.NodeID), r.CreatedAt,
			).WithContext(ctx).Exec(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *CassandraStore) AdjacentRelations(ctx context.Context, node models.NodeKey) ([]models.LineageRelationRecord, error) {
	merged := map[uuid.UUID]models.LineageRelationRecord{}

	srcIter := s.session.Query(
		`SELECT relation_id, source_id, source_kind_copy, target_id, target_kind,
		        relation_kind, pipeline_id, workflow_id, node_id, step_id,
		        effective_marking, metadata, created_at
		   FROM lineage_runtime.relations_by_source
		  WHERE source_kind = ? AND source_id = ?`,
		node.Kind.String(), node.ID,
	).WithContext(ctx).Iter()
	if err := scanRelationsInto(srcIter, merged); err != nil {
		return nil, fmt.Errorf("decode source lineage: %w", err)
	}

	tgtIter := s.session.Query(
		`SELECT relation_id, source_id, source_kind, target_id, target_kind_copy,
		        relation_kind, pipeline_id, workflow_id, node_id, step_id,
		        effective_marking, metadata, created_at
		   FROM lineage_runtime.relations_by_target
		  WHERE target_kind = ? AND target_id = ?`,
		node.Kind.String(), node.ID,
	).WithContext(ctx).Iter()
	if err := scanRelationsInto(tgtIter, merged); err != nil {
		return nil, fmt.Errorf("decode target lineage: %w", err)
	}

	out := make([]models.LineageRelationRecord, 0, len(merged))
	for _, r := range merged {
		out = append(out, r)
	}
	return out, nil
}

func (s *CassandraStore) AllRelations(ctx context.Context) ([]models.LineageRelationRecord, error) {
	iter := s.session.Query(
		`SELECT relation_id, source_id, source_kind, target_id, target_kind, relation_kind,
		        pipeline_id, workflow_id, node_id, step_id, effective_marking, metadata,
		        created_at
		   FROM lineage_runtime.relations_all
		  WHERE partition_key = ?`,
		runtimePartition,
	).WithContext(ctx).Iter()
	merged := map[uuid.UUID]models.LineageRelationRecord{}
	if err := scanRelationsInto(iter, merged); err != nil {
		return nil, fmt.Errorf("decode full lineage: %w", err)
	}
	out := make([]models.LineageRelationRecord, 0, len(merged))
	for _, r := range merged {
		out = append(out, r)
	}
	return out, nil
}

func (s *CassandraStore) DeleteWorkflowRelations(ctx context.Context, workflowID uuid.UUID) error {
	iter := s.session.Query(
		`SELECT relation_id, source_id, source_kind, target_id, target_kind, relation_kind,
		        pipeline_id, node_id, step_id, effective_marking, metadata, created_at
		   FROM lineage_runtime.relations_by_workflow
		  WHERE workflow_id = ?`,
		workflowID,
	).WithContext(ctx).Iter()

	type wf struct {
		id, sid, tid uuid.UUID
		skind, tkind string
	}
	var rows []wf
	var (
		relID, srcID, tgtID                   gocql.UUID
		srcKind, tgtKind, relKind, effMarking string
		pipelineID                            *gocql.UUID
		nodeID, stepID                        *string
		metadata                              string
		createdAt                             time.Time
	)
	_ = relKind
	_ = effMarking
	_ = pipelineID
	_ = nodeID
	_ = stepID
	_ = metadata
	_ = createdAt
	for iter.Scan(&relID, &srcID, &srcKind, &tgtID, &tgtKind, &relKind,
		&pipelineID, &nodeID, &stepID, &effMarking, &metadata, &createdAt) {
		rows = append(rows, wf{
			id:    uuid.UUID(relID),
			sid:   uuid.UUID(srcID),
			tid:   uuid.UUID(tgtID),
			skind: srcKind, tkind: tgtKind,
		})
	}
	if err := iter.Close(); err != nil {
		return fmt.Errorf("scan workflow relations: %w", err)
	}

	for _, row := range rows {
		if err := s.session.Query(
			`DELETE FROM lineage_runtime.relations_by_source
			  WHERE source_kind = ? AND source_id = ? AND relation_id = ?`,
			row.skind, row.sid, row.id,
		).WithContext(ctx).Exec(); err != nil {
			return err
		}
		if err := s.session.Query(
			`DELETE FROM lineage_runtime.relations_by_target
			  WHERE target_kind = ? AND target_id = ? AND relation_id = ?`,
			row.tkind, row.tid, row.id,
		).WithContext(ctx).Exec(); err != nil {
			return err
		}
		if err := s.session.Query(
			`DELETE FROM lineage_runtime.relations_all
			  WHERE partition_key = ? AND relation_id = ?`,
			runtimePartition, row.id,
		).WithContext(ctx).Exec(); err != nil {
			return err
		}
		if err := s.session.Query(
			`DELETE FROM lineage_runtime.relations_by_workflow
			  WHERE workflow_id = ? AND relation_id = ?`,
			workflowID, row.id,
		).WithContext(ctx).Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (s *CassandraStore) DatasetColumnLineage(ctx context.Context, datasetID uuid.UUID) ([]models.ColumnLineageEdge, error) {
	iter := s.session.Query(
		`SELECT relation_id, source_dataset_id, source_column, target_dataset_id,
		        target_column, pipeline_id, node_id, created_at
		   FROM lineage_runtime.column_relations_by_dataset
		  WHERE dataset_id = ?`,
		datasetID,
	).WithContext(ctx).Iter()

	dedup := map[uuid.UUID]models.ColumnLineageEdge{}
	var (
		relID, srcID, tgtID gocql.UUID
		srcCol, tgtCol      string
		pipelineID          *gocql.UUID
		nodeID              *string
		createdAt           time.Time
	)
	for iter.Scan(&relID, &srcID, &srcCol, &tgtID, &tgtCol, &pipelineID, &nodeID, &createdAt) {
		id := uuid.UUID(relID)
		dedup[id] = models.ColumnLineageEdge{
			ID:              id,
			SourceDatasetID: uuid.UUID(srcID),
			SourceColumn:    srcCol,
			TargetDatasetID: uuid.UUID(tgtID),
			TargetColumn:    tgtCol,
			PipelineID:      ptrUUID(pipelineID),
			NodeID:          nodeID,
			CreatedAt:       createdAt,
		}
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("scan column lineage: %w", err)
	}

	out := make([]models.ColumnLineageEdge, 0, len(dedup))
	for _, e := range dedup {
		out = append(out, e)
	}
	// Sort by created_at desc to match the Rust impl.
	sortColumnEdgesDesc(out)
	return out, nil
}
