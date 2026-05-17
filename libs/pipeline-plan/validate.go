package pipelineplan

import (
	"errors"
	"fmt"
	"strings"
)

// ValidationError carries one problem found in a [Plan]. Validate
// returns a slice of these so the authoring UI can highlight every
// broken node in one pass rather than one error at a time.
//
// Cause is the optional sentinel the failure wraps; it is omitted
// from JSON serialisation so the wire shape stays stable across
// future sentinels.
type ValidationError struct {
	OpID    string `json:"op_id,omitempty"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	Cause   error  `json:"-"`
}

func (e ValidationError) Error() string {
	switch {
	case e.OpID != "" && e.Field != "":
		return fmt.Sprintf("op %q field %q: %s", e.OpID, e.Field, e.Message)
	case e.OpID != "":
		return fmt.Sprintf("op %q: %s", e.OpID, e.Message)
	default:
		return e.Message
	}
}

// Unwrap exposes Cause so errors.Is / errors.As can match against
// the sentinel (e.g. ErrEmptyPlan).
func (e ValidationError) Unwrap() error { return e.Cause }

// ValidationErrors aggregates multiple ValidationError into a single
// returned error so callers that want a flat error path can use
// `errors.Is`; the slice form remains the canonical shape.
type ValidationErrors []ValidationError

func (es ValidationErrors) Error() string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "; ")
}

// Unwrap exposes the first error so errors.Is / errors.As traversal
// reaches the underlying ValidationError values.
func (es ValidationErrors) Unwrap() []error {
	out := make([]error, len(es))
	for i := range es {
		err := es[i]
		out[i] = err
	}
	return out
}

// ErrEmptyPlan is returned (wrapped in ValidationErrors) when a Plan
// has no operators.
var ErrEmptyPlan = errors.New("plan has no ops")

// Validate runs every structural check on p and returns a slice of
// findings; nil means the plan is well-formed.
//
// Checks performed:
//  1. Plan has at least one op.
//  2. Every Op.ID is non-empty and unique.
//  3. Every Op.Kind is a recognised constant.
//  4. Exactly the per-kind config field for the declared Kind is
//     populated; siblings must be nil.
//  5. Op.Inputs reference existing Op.IDs.
//  6. Source ops (read_table) have zero inputs.
//  7. Non-source, non-union ops have exactly one input.
//  8. Union ops have at least two inputs.
//  9. The graph has at least one terminal op (write_table) and at
//     least one source op (read_table).
//  10. The graph has no cycles.
//  11. Per-kind invariants (e.g. limit n > 0, aggregate function
//      tokens are known, write mode is valid).
//
// Validate is meant to run at authoring time. The runtime may rely
// on Validate having passed before it starts executing — it does not
// re-check structural invariants.
func (p Plan) Validate() ValidationErrors {
	var errs ValidationErrors
	if len(p.Ops) == 0 {
		errs = append(errs, ValidationError{Message: ErrEmptyPlan.Error(), Cause: ErrEmptyPlan})
		return errs
	}

	ids := make(map[string]int, len(p.Ops))
	for i, op := range p.Ops {
		if strings.TrimSpace(op.ID) == "" {
			errs = append(errs, ValidationError{Field: "id", Message: fmt.Sprintf("op at index %d has empty id", i)})
			continue
		}
		if _, dup := ids[op.ID]; dup {
			errs = append(errs, ValidationError{OpID: op.ID, Field: "id", Message: "duplicate op id"})
			continue
		}
		ids[op.ID] = i
	}

	hasSource, hasTerminal := false, false
	for i := range p.Ops {
		op := p.Ops[i]
		if _, known := ids[op.ID]; !known {
			continue // already flagged
		}
		validateKindAndConfig(op, &errs)
		validateInputs(op, ids, &errs)
		validateOpInvariants(op, &errs)
		if op.Source() {
			hasSource = true
		}
		if op.Terminal() {
			hasTerminal = true
		}
	}

	if !hasSource {
		errs = append(errs, ValidationError{Message: "plan has no source op (read_table)"})
	}
	if !hasTerminal {
		errs = append(errs, ValidationError{Message: "plan has no terminal op (write_table)"})
	}

	if len(errs) == 0 {
		if cycle := findCycle(p.Ops, ids); cycle != "" {
			errs = append(errs, ValidationError{OpID: cycle, Message: "cycle detected"})
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}

func validateKindAndConfig(op Op, errs *ValidationErrors) {
	if !IsKind(op.Kind) {
		*errs = append(*errs, ValidationError{OpID: op.ID, Field: "kind", Message: fmt.Sprintf("unknown kind %q", op.Kind)})
		return
	}
	// Build a (kind → present?) map by inspecting the pointer fields.
	present := map[Kind]bool{
		KindReadTable:  op.ReadTable != nil,
		KindFilter:     op.Filter != nil,
		KindProject:    op.Project != nil,
		KindRename:     op.Rename != nil,
		KindCast:       op.Cast != nil,
		KindAggregate:  op.Aggregate != nil,
		KindUnion:      op.Union != nil,
		KindLimit:      op.Limit != nil,
		KindWriteTable: op.WriteTable != nil,
	}
	if !present[op.Kind] {
		*errs = append(*errs, ValidationError{OpID: op.ID, Field: string(op.Kind), Message: "config for declared kind is missing"})
	}
	for k, ok := range present {
		if ok && k != op.Kind {
			*errs = append(*errs, ValidationError{OpID: op.ID, Field: string(k), Message: fmt.Sprintf("config for non-declared kind %q is populated", k)})
		}
	}
}

func validateInputs(op Op, ids map[string]int, errs *ValidationErrors) {
	switch {
	case op.Source():
		if len(op.Inputs) != 0 {
			*errs = append(*errs, ValidationError{OpID: op.ID, Field: "inputs", Message: "source op must have zero inputs"})
		}
	case op.Kind == KindUnion:
		if len(op.Inputs) < 2 {
			*errs = append(*errs, ValidationError{OpID: op.ID, Field: "inputs", Message: "union requires at least two inputs"})
		}
	default:
		if len(op.Inputs) != 1 {
			*errs = append(*errs, ValidationError{OpID: op.ID, Field: "inputs", Message: fmt.Sprintf("kind %q requires exactly one input, got %d", op.Kind, len(op.Inputs))})
		}
	}
	for _, in := range op.Inputs {
		if _, ok := ids[in]; !ok {
			*errs = append(*errs, ValidationError{OpID: op.ID, Field: "inputs", Message: fmt.Sprintf("references unknown op id %q", in)})
		}
		if in == op.ID {
			*errs = append(*errs, ValidationError{OpID: op.ID, Field: "inputs", Message: "self-reference"})
		}
	}
}

func validateOpInvariants(op Op, errs *ValidationErrors) {
	switch op.Kind {
	case KindReadTable:
		if op.ReadTable == nil {
			return
		}
		mustNonEmpty(op.ID, "read_table.catalog", op.ReadTable.Catalog, errs)
		mustNonEmpty(op.ID, "read_table.namespace", op.ReadTable.Namespace, errs)
		mustNonEmpty(op.ID, "read_table.table", op.ReadTable.Table, errs)

	case KindFilter:
		if op.Filter == nil {
			return
		}
		mustNonEmpty(op.ID, "filter.expr", op.Filter.Expr, errs)

	case KindProject:
		if op.Project == nil {
			return
		}
		if len(op.Project.Columns) == 0 {
			*errs = append(*errs, ValidationError{OpID: op.ID, Field: "project.columns", Message: "must list at least one column"})
		}
		seen := map[string]struct{}{}
		for i, c := range op.Project.Columns {
			if strings.TrimSpace(c.Name) == "" {
				*errs = append(*errs, ValidationError{OpID: op.ID, Field: fmt.Sprintf("project.columns[%d].name", i), Message: "must be non-empty"})
				continue
			}
			if _, dup := seen[c.Name]; dup {
				*errs = append(*errs, ValidationError{OpID: op.ID, Field: fmt.Sprintf("project.columns[%d].name", i), Message: fmt.Sprintf("duplicate column name %q", c.Name)})
			}
			seen[c.Name] = struct{}{}
		}

	case KindRename:
		if op.Rename == nil {
			return
		}
		if len(op.Rename.Mapping) == 0 {
			*errs = append(*errs, ValidationError{OpID: op.ID, Field: "rename.mapping", Message: "must list at least one mapping"})
		}
		for i, m := range op.Rename.Mapping {
			if strings.TrimSpace(m.From) == "" || strings.TrimSpace(m.To) == "" {
				*errs = append(*errs, ValidationError{OpID: op.ID, Field: fmt.Sprintf("rename.mapping[%d]", i), Message: "from and to must be non-empty"})
			}
		}

	case KindCast:
		if op.Cast == nil {
			return
		}
		if len(op.Cast.Casts) == 0 {
			*errs = append(*errs, ValidationError{OpID: op.ID, Field: "cast.casts", Message: "must list at least one cast"})
		}
		for i, c := range op.Cast.Casts {
			if strings.TrimSpace(c.Column) == "" {
				*errs = append(*errs, ValidationError{OpID: op.ID, Field: fmt.Sprintf("cast.casts[%d].column", i), Message: "must be non-empty"})
			}
			if c.To.Kind == "" {
				*errs = append(*errs, ValidationError{OpID: op.ID, Field: fmt.Sprintf("cast.casts[%d].to.kind", i), Message: "target type kind must be non-empty"})
			}
		}

	case KindAggregate:
		if op.Aggregate == nil {
			return
		}
		if len(op.Aggregate.Aggregations) == 0 {
			*errs = append(*errs, ValidationError{OpID: op.ID, Field: "aggregate.aggregations", Message: "must list at least one aggregation"})
		}
		targets := map[string]struct{}{}
		for i, a := range op.Aggregate.Aggregations {
			if !IsAggregation(a.Function) {
				*errs = append(*errs, ValidationError{OpID: op.ID, Field: fmt.Sprintf("aggregate.aggregations[%d].function", i), Message: fmt.Sprintf("unknown function %q", a.Function)})
			}
			if a.Function != "count" && strings.TrimSpace(a.SourceColumn) == "" {
				*errs = append(*errs, ValidationError{OpID: op.ID, Field: fmt.Sprintf("aggregate.aggregations[%d].source_column", i), Message: fmt.Sprintf("function %q requires source_column", a.Function)})
			}
			if strings.TrimSpace(a.TargetColumn) == "" {
				*errs = append(*errs, ValidationError{OpID: op.ID, Field: fmt.Sprintf("aggregate.aggregations[%d].target_column", i), Message: "target_column must be non-empty"})
				continue
			}
			if _, dup := targets[a.TargetColumn]; dup {
				*errs = append(*errs, ValidationError{OpID: op.ID, Field: fmt.Sprintf("aggregate.aggregations[%d].target_column", i), Message: fmt.Sprintf("duplicate target column %q", a.TargetColumn)})
			}
			targets[a.TargetColumn] = struct{}{}
		}
		groupKeys := map[string]struct{}{}
		for i, g := range op.Aggregate.GroupBy {
			if strings.TrimSpace(g) == "" {
				*errs = append(*errs, ValidationError{OpID: op.ID, Field: fmt.Sprintf("aggregate.group_by[%d]", i), Message: "must be non-empty"})
				continue
			}
			if _, dup := groupKeys[g]; dup {
				*errs = append(*errs, ValidationError{OpID: op.ID, Field: fmt.Sprintf("aggregate.group_by[%d]", i), Message: fmt.Sprintf("duplicate group key %q", g)})
			}
			groupKeys[g] = struct{}{}
		}

	case KindLimit:
		if op.Limit == nil {
			return
		}
		if op.Limit.N <= 0 {
			*errs = append(*errs, ValidationError{OpID: op.ID, Field: "limit.n", Message: "must be > 0"})
		}

	case KindWriteTable:
		if op.WriteTable == nil {
			return
		}
		mustNonEmpty(op.ID, "write_table.catalog", op.WriteTable.Catalog, errs)
		mustNonEmpty(op.ID, "write_table.namespace", op.WriteTable.Namespace, errs)
		mustNonEmpty(op.ID, "write_table.table", op.WriteTable.Table, errs)
		if !IsWriteMode(op.WriteTable.Mode) {
			*errs = append(*errs, ValidationError{OpID: op.ID, Field: "write_table.mode", Message: fmt.Sprintf("unknown mode %q", op.WriteTable.Mode)})
		}
	}
}

func mustNonEmpty(opID, field, value string, errs *ValidationErrors) {
	if strings.TrimSpace(value) == "" {
		*errs = append(*errs, ValidationError{OpID: opID, Field: field, Message: "must be non-empty"})
	}
}

// findCycle returns the ID of one op that participates in a cycle, or
// "" when the graph is acyclic. Standard 3-colour DFS.
func findCycle(ops []Op, ids map[string]int) string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	colour := make(map[string]int, len(ops))

	var visit func(id string) string
	visit = func(id string) string {
		if colour[id] == gray {
			return id
		}
		if colour[id] == black {
			return ""
		}
		colour[id] = gray
		op := ops[ids[id]]
		for _, in := range op.Inputs {
			if _, ok := ids[in]; !ok {
				continue
			}
			if cycle := visit(in); cycle != "" {
				return cycle
			}
		}
		colour[id] = black
		return ""
	}

	for _, op := range ops {
		if colour[op.ID] != white {
			continue
		}
		if cycle := visit(op.ID); cycle != "" {
			return cycle
		}
	}
	return ""
}
