package pipelineplan

// Plan is the wire-format root: a pipeline_id + run_id (for log
// scoping and event correlation) plus the operator graph itself.
type Plan struct {
	PipelineID string `json:"pipeline_id"`
	RunID      string `json:"run_id"`
	Ops        []Op   `json:"ops"`
}

// Kind discriminates the per-op config field. Pinned wire constants
// — renaming any of them is a breaking change to every consumer.
type Kind string

const (
	KindReadTable  Kind = "read_table"
	KindFilter     Kind = "filter"
	KindProject    Kind = "project"
	KindRename     Kind = "rename"
	KindCast       Kind = "cast"
	KindAggregate  Kind = "aggregate"
	KindUnion      Kind = "union"
	KindLimit      Kind = "limit"
	KindWriteTable Kind = "write_table"
)

// AllKinds returns every supported [Kind]. Stable order — used by
// validation tables and by the catalog surface that lists v1 ops.
func AllKinds() []Kind {
	return []Kind{
		KindReadTable, KindFilter, KindProject, KindRename, KindCast,
		KindAggregate, KindUnion, KindLimit, KindWriteTable,
	}
}

// IsKind returns true when k is a recognised operator kind.
func IsKind(k Kind) bool {
	for _, v := range AllKinds() {
		if v == k {
			return true
		}
	}
	return false
}

// Op is one operator node. Exactly one per-kind field is populated;
// which one is gated by [Kind]. Inputs reference upstream [Op.ID]
// values — read_table has zero inputs (it is a source), every other
// op has one input by default, union accepts two or more.
type Op struct {
	ID     string   `json:"id"`
	Kind   Kind     `json:"kind"`
	Inputs []string `json:"inputs,omitempty"`

	ReadTable  *ReadTable  `json:"read_table,omitempty"`
	Filter     *Filter     `json:"filter,omitempty"`
	Project    *Project    `json:"project,omitempty"`
	Rename     *Rename     `json:"rename,omitempty"`
	Cast       *Cast       `json:"cast,omitempty"`
	Aggregate  *Aggregate  `json:"aggregate,omitempty"`
	Union      *Union      `json:"union,omitempty"`
	Limit      *Limit      `json:"limit,omitempty"`
	WriteTable *WriteTable `json:"write_table,omitempty"`
}

// Source returns true for operators that have no inputs and instead
// pull data from outside the plan (today: only read_table).
func (o Op) Source() bool { return o.Kind == KindReadTable }

// Terminal returns true for operators that produce no downstream row
// stream and instead commit results outside the plan (today: only
// write_table).
func (o Op) Terminal() bool { return o.Kind == KindWriteTable }
