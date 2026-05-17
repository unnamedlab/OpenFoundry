package pipelineruntime

import (
	"context"
	"errors"
	"fmt"
	"iter"

	pp "github.com/openfoundry/openfoundry-go/libs/pipeline-plan"
)

// Row is one record flowing through the pipeline. Maps column name to
// JSON-marshalable value. Same shape as Phase A's indexer used for
// apache/iceberg-go reads.
type Row = map[string]any

// RowStream yields rows lazily. Implementations must respect ctx
// cancellation by yielding an error and returning.
type RowStream = iter.Seq2[Row, error]

// Reader is the seam over Iceberg table reads. Production
// implementation wraps apache/iceberg-go (Phase A path) and ships
// in Phase C.5; tests inject a memory-backed fake.
type Reader interface {
	// Scan returns the RowStream for `catalog.namespace.table` at the
	// current snapshot. The implementation may stream lazily or
	// materialise — the interpreter does not assume either.
	Scan(ctx context.Context, catalog, namespace, table string) (RowStream, error)
}

// Writer is the seam over Iceberg table writes. Production
// implementation posts to iceberg-catalog-service's
// `/openfoundry/iceberg/v1/append` (Phase B pattern) and ships in
// Phase C.5; tests inject a memory-backed fake.
type Writer interface {
	// Write commits `rows` to `catalog.namespace.table` under the
	// requested write mode. The implementation publishes exactly one
	// Iceberg snapshot per call so the upstream caller sees atomic
	// table updates that match the Spark `createOrReplace` semantic.
	Write(ctx context.Context, catalog, namespace, table string, mode pp.WriteMode, rows []Row) error
}

// Executor runs a [pp.Plan] against a Reader/Writer pair.
type Executor struct {
	Reader Reader
	Writer Writer
}

// Run validates `plan` and then executes it. Returns nil on success.
// Errors fall into three buckets and are wrapped accordingly:
//
//   - Plan validation — surfaced as the underlying ValidationErrors.
//   - Per-op build (e.g. unparseable expression) — wrapped with the
//     op id.
//   - Per-row evaluation (e.g. type error, missing column) — wrapped
//     with the op id of the failing operator.
func (e *Executor) Run(ctx context.Context, plan pp.Plan) error {
	if e.Reader == nil {
		return errors.New("pipelineruntime: Executor.Reader is nil")
	}
	if e.Writer == nil {
		return errors.New("pipelineruntime: Executor.Writer is nil")
	}
	if errs := plan.Validate(); errs != nil {
		return fmt.Errorf("plan invalid: %w", errs)
	}

	order, err := topoSort(plan)
	if err != nil {
		return err
	}

	// streams[opID] = RowStream produced by that op. Terminal ops
	// (write_table) are never read from `streams`; they consume their
	// input directly through Writer.Write and return nil from build.
	streams := make(map[string]RowStream, len(plan.Ops))
	byID := make(map[string]pp.Op, len(plan.Ops))
	for _, op := range plan.Ops {
		byID[op.ID] = op
	}

	for _, id := range order {
		op := byID[id]
		switch op.Kind {
		case pp.KindReadTable:
			s, err := e.Reader.Scan(ctx, op.ReadTable.Catalog, op.ReadTable.Namespace, op.ReadTable.Table)
			if err != nil {
				return fmt.Errorf("op %q (read_table %s): %w", op.ID, op.ReadTable.FullyQualified(), err)
			}
			streams[op.ID] = s

		case pp.KindWriteTable:
			upstream, err := upstreamSingle(streams, op)
			if err != nil {
				return err
			}
			rows, err := drain(ctx, upstream)
			if err != nil {
				return fmt.Errorf("op %q (write_table %s): %w", op.ID, op.WriteTable.FullyQualified(), err)
			}
			if err := e.Writer.Write(ctx, op.WriteTable.Catalog, op.WriteTable.Namespace, op.WriteTable.Table, op.WriteTable.Mode, rows); err != nil {
				return fmt.Errorf("op %q (write_table %s): %w", op.ID, op.WriteTable.FullyQualified(), err)
			}

		default:
			upstreams, err := upstreamMulti(streams, op)
			if err != nil {
				return err
			}
			out, err := buildOp(ctx, op, upstreams)
			if err != nil {
				return fmt.Errorf("op %q (%s): %w", op.ID, op.Kind, err)
			}
			streams[op.ID] = out
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

// upstreamSingle returns the single upstream stream for ops that take
// exactly one input. Caller has already passed plan.Validate(), so
// `op.Inputs` arity is guaranteed; we only handle the "missing in
// the map" case (which only happens when topoSort is broken).
func upstreamSingle(streams map[string]RowStream, op pp.Op) (RowStream, error) {
	if len(op.Inputs) != 1 {
		return nil, fmt.Errorf("op %q: expected exactly 1 input, got %d", op.ID, len(op.Inputs))
	}
	s, ok := streams[op.Inputs[0]]
	if !ok {
		return nil, fmt.Errorf("op %q: upstream %q has no produced stream", op.ID, op.Inputs[0])
	}
	return s, nil
}

func upstreamMulti(streams map[string]RowStream, op pp.Op) ([]RowStream, error) {
	out := make([]RowStream, 0, len(op.Inputs))
	for _, in := range op.Inputs {
		s, ok := streams[in]
		if !ok {
			return nil, fmt.Errorf("op %q: upstream %q has no produced stream", op.ID, in)
		}
		out = append(out, s)
	}
	return out, nil
}

// drain pulls every row from `s` into a slice. Returns the first
// non-nil error if iteration produces one.
func drain(ctx context.Context, s RowStream) ([]Row, error) {
	var out []Row
	for row, err := range s {
		if err != nil {
			return nil, err
		}
		out = append(out, row)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// errStream returns a RowStream that yields a single error and stops.
// Used by per-op builders to defer parse / setup failures into the
// iteration.
func errStream(err error) RowStream {
	return func(yield func(Row, error) bool) {
		yield(nil, err)
	}
}

// topoSort returns op ids in a topological order: every op appears
// after all of its inputs. Plan.Validate() already guarantees no
// cycles; we re-run a defensive check.
func topoSort(plan pp.Plan) ([]string, error) {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	colour := make(map[string]int, len(plan.Ops))
	byID := make(map[string]pp.Op, len(plan.Ops))
	for _, op := range plan.Ops {
		byID[op.ID] = op
	}
	out := make([]string, 0, len(plan.Ops))

	var visit func(id string) error
	visit = func(id string) error {
		switch colour[id] {
		case gray:
			return fmt.Errorf("topoSort: cycle detected through op %q", id)
		case black:
			return nil
		}
		colour[id] = gray
		op := byID[id]
		for _, in := range op.Inputs {
			if err := visit(in); err != nil {
				return err
			}
		}
		colour[id] = black
		out = append(out, id)
		return nil
	}

	for _, op := range plan.Ops {
		if err := visit(op.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}
