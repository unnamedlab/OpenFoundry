package pipelineplan_test

import (
	"errors"
	"strings"
	"testing"

	pp "github.com/openfoundry/openfoundry-go/libs/pipeline-plan"
)

func assertErrContains(t *testing.T, errs pp.ValidationErrors, substr string) {
	t.Helper()
	if errs == nil {
		t.Fatalf("expected error containing %q, got none", substr)
	}
	if !strings.Contains(errs.Error(), substr) {
		t.Errorf("expected error containing %q, got: %v", substr, errs)
	}
}

func TestValidate_emptyPlan(t *testing.T) {
	t.Parallel()
	errs := pp.Plan{}.Validate()
	if errs == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(errs, pp.ErrEmptyPlan) {
		t.Errorf("expected ErrEmptyPlan, got %v", errs)
	}
}

func TestValidate_missingSourceOrTerminal(t *testing.T) {
	t.Parallel()
	// no source — single project op pointing nowhere
	plan := pp.Plan{
		Ops: []pp.Op{{
			ID: "p", Kind: pp.KindProject,
			Project: &pp.Project{Columns: []pp.ProjectColumn{{Name: "a"}}},
		}},
	}
	assertErrContains(t, plan.Validate(), "no source op")

	// no terminal — source only
	plan = pp.Plan{
		Ops: []pp.Op{{
			ID: "s", Kind: pp.KindReadTable,
			ReadTable: &pp.ReadTable{Catalog: "c", Namespace: "n", Table: "t"},
		}},
	}
	assertErrContains(t, plan.Validate(), "no terminal op")
}

func TestValidate_duplicateOpID(t *testing.T) {
	t.Parallel()
	plan := pp.Plan{
		Ops: []pp.Op{
			{ID: "x", Kind: pp.KindReadTable, ReadTable: &pp.ReadTable{Catalog: "c", Namespace: "n", Table: "t"}},
			{ID: "x", Kind: pp.KindWriteTable, Inputs: []string{"x"},
				WriteTable: &pp.WriteTable{Catalog: "c", Namespace: "n", Table: "t", Mode: pp.WriteModeAppend}},
		},
	}
	assertErrContains(t, plan.Validate(), "duplicate op id")
}

func TestValidate_emptyOpID(t *testing.T) {
	t.Parallel()
	plan := pp.Plan{
		Ops: []pp.Op{{
			ID: " ", Kind: pp.KindReadTable,
			ReadTable: &pp.ReadTable{Catalog: "c", Namespace: "n", Table: "t"},
		}, {
			ID: "w", Kind: pp.KindWriteTable, Inputs: []string{},
			WriteTable: &pp.WriteTable{Catalog: "c", Namespace: "n", Table: "t", Mode: pp.WriteModeAppend},
		}},
	}
	assertErrContains(t, plan.Validate(), "empty id")
}

func TestValidate_kindConfigMismatch(t *testing.T) {
	t.Parallel()
	// Kind=filter but Filter is nil, and a sibling (Project) is populated.
	plan := pp.Plan{
		Ops: []pp.Op{
			{ID: "s", Kind: pp.KindReadTable,
				ReadTable: &pp.ReadTable{Catalog: "c", Namespace: "n", Table: "t"}},
			{ID: "bad", Kind: pp.KindFilter, Inputs: []string{"s"},
				Project: &pp.Project{Columns: []pp.ProjectColumn{{Name: "a"}}}},
			{ID: "w", Kind: pp.KindWriteTable, Inputs: []string{"bad"},
				WriteTable: &pp.WriteTable{Catalog: "c", Namespace: "n", Table: "t", Mode: pp.WriteModeAppend}},
		},
	}
	errs := plan.Validate()
	if errs == nil {
		t.Fatal("expected errors")
	}
	got := errs.Error()
	if !strings.Contains(got, "config for declared kind is missing") ||
		!strings.Contains(got, `config for non-declared kind "project"`) {
		t.Errorf("expected both 'missing' and 'non-declared' errors, got: %v", errs)
	}
}

func TestValidate_inputsRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		plan   pp.Plan
		substr string
	}{
		{
			name: "source with inputs",
			plan: pp.Plan{Ops: []pp.Op{
				{ID: "s", Kind: pp.KindReadTable, Inputs: []string{"x"},
					ReadTable: &pp.ReadTable{Catalog: "c", Namespace: "n", Table: "t"}},
				{ID: "w", Kind: pp.KindWriteTable, Inputs: []string{"s"},
					WriteTable: &pp.WriteTable{Catalog: "c", Namespace: "n", Table: "t", Mode: pp.WriteModeAppend}},
			}},
			substr: "source op must have zero inputs",
		},
		{
			name: "non-source non-union without exactly one input",
			plan: pp.Plan{Ops: []pp.Op{
				{ID: "s", Kind: pp.KindReadTable,
					ReadTable: &pp.ReadTable{Catalog: "c", Namespace: "n", Table: "t"}},
				{ID: "f", Kind: pp.KindFilter, Inputs: []string{},
					Filter: &pp.Filter{Expr: "true"}},
				{ID: "w", Kind: pp.KindWriteTable, Inputs: []string{"f"},
					WriteTable: &pp.WriteTable{Catalog: "c", Namespace: "n", Table: "t", Mode: pp.WriteModeAppend}},
			}},
			substr: "requires exactly one input",
		},
		{
			name: "union with one input",
			plan: pp.Plan{Ops: []pp.Op{
				{ID: "s", Kind: pp.KindReadTable,
					ReadTable: &pp.ReadTable{Catalog: "c", Namespace: "n", Table: "t"}},
				{ID: "u", Kind: pp.KindUnion, Inputs: []string{"s"}, Union: &pp.Union{}},
				{ID: "w", Kind: pp.KindWriteTable, Inputs: []string{"u"},
					WriteTable: &pp.WriteTable{Catalog: "c", Namespace: "n", Table: "t", Mode: pp.WriteModeAppend}},
			}},
			substr: "union requires at least two inputs",
		},
		{
			name: "unknown input reference",
			plan: pp.Plan{Ops: []pp.Op{
				{ID: "s", Kind: pp.KindReadTable,
					ReadTable: &pp.ReadTable{Catalog: "c", Namespace: "n", Table: "t"}},
				{ID: "f", Kind: pp.KindFilter, Inputs: []string{"missing"},
					Filter: &pp.Filter{Expr: "true"}},
				{ID: "w", Kind: pp.KindWriteTable, Inputs: []string{"f"},
					WriteTable: &pp.WriteTable{Catalog: "c", Namespace: "n", Table: "t", Mode: pp.WriteModeAppend}},
			}},
			substr: `unknown op id "missing"`,
		},
		{
			name: "self-reference",
			plan: pp.Plan{Ops: []pp.Op{
				{ID: "s", Kind: pp.KindReadTable,
					ReadTable: &pp.ReadTable{Catalog: "c", Namespace: "n", Table: "t"}},
				{ID: "f", Kind: pp.KindFilter, Inputs: []string{"f"},
					Filter: &pp.Filter{Expr: "true"}},
				{ID: "w", Kind: pp.KindWriteTable, Inputs: []string{"f"},
					WriteTable: &pp.WriteTable{Catalog: "c", Namespace: "n", Table: "t", Mode: pp.WriteModeAppend}},
			}},
			substr: "self-reference",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertErrContains(t, tc.plan.Validate(), tc.substr)
		})
	}
}

func TestValidate_cycle(t *testing.T) {
	t.Parallel()
	// s → a → b → a (cycle a↔b)
	plan := pp.Plan{
		Ops: []pp.Op{
			{ID: "s", Kind: pp.KindReadTable,
				ReadTable: &pp.ReadTable{Catalog: "c", Namespace: "n", Table: "t"}},
			{ID: "a", Kind: pp.KindFilter, Inputs: []string{"b"},
				Filter: &pp.Filter{Expr: "true"}},
			{ID: "b", Kind: pp.KindFilter, Inputs: []string{"a"},
				Filter: &pp.Filter{Expr: "true"}},
			{ID: "w", Kind: pp.KindWriteTable, Inputs: []string{"a"},
				WriteTable: &pp.WriteTable{Catalog: "c", Namespace: "n", Table: "t", Mode: pp.WriteModeAppend}},
		},
	}
	assertErrContains(t, plan.Validate(), "cycle detected")
}

func TestValidate_perOpInvariants(t *testing.T) {
	t.Parallel()
	build := func(opMid pp.Op) pp.Plan {
		opMid.Inputs = []string{"s"}
		return pp.Plan{Ops: []pp.Op{
			{ID: "s", Kind: pp.KindReadTable,
				ReadTable: &pp.ReadTable{Catalog: "c", Namespace: "n", Table: "t"}},
			opMid,
			{ID: "w", Kind: pp.KindWriteTable, Inputs: []string{opMid.ID},
				WriteTable: &pp.WriteTable{Catalog: "c", Namespace: "n", Table: "t", Mode: pp.WriteModeAppend}},
		}}
	}

	t.Run("filter empty expr", func(t *testing.T) {
		t.Parallel()
		assertErrContains(t, build(pp.Op{ID: "f", Kind: pp.KindFilter,
			Filter: &pp.Filter{Expr: " "}}).Validate(), "filter.expr")
	})

	t.Run("project no columns", func(t *testing.T) {
		t.Parallel()
		assertErrContains(t, build(pp.Op{ID: "p", Kind: pp.KindProject,
			Project: &pp.Project{}}).Validate(), "at least one column")
	})

	t.Run("project duplicate column", func(t *testing.T) {
		t.Parallel()
		assertErrContains(t, build(pp.Op{ID: "p", Kind: pp.KindProject,
			Project: &pp.Project{Columns: []pp.ProjectColumn{{Name: "x"}, {Name: "x"}}}}).Validate(),
			`duplicate column name "x"`)
	})

	t.Run("aggregate unknown function", func(t *testing.T) {
		t.Parallel()
		assertErrContains(t, build(pp.Op{ID: "a", Kind: pp.KindAggregate,
			Aggregate: &pp.Aggregate{Aggregations: []pp.AggregationFunc{
				{Function: "median", SourceColumn: "x", TargetColumn: "m"},
			}}}).Validate(), `unknown function "median"`)
	})

	t.Run("aggregate source_column required except for count", func(t *testing.T) {
		t.Parallel()
		// sum without source_column → error
		assertErrContains(t, build(pp.Op{ID: "a", Kind: pp.KindAggregate,
			Aggregate: &pp.Aggregate{Aggregations: []pp.AggregationFunc{
				{Function: "sum", TargetColumn: "s"},
			}}}).Validate(), "requires source_column")

		// count without source_column → OK
		plan := build(pp.Op{ID: "a", Kind: pp.KindAggregate,
			Aggregate: &pp.Aggregate{Aggregations: []pp.AggregationFunc{
				{Function: "count", TargetColumn: "n"},
			}}})
		if errs := plan.Validate(); errs != nil {
			t.Errorf("count without source_column should be valid, got: %v", errs)
		}
	})

	t.Run("aggregate duplicate target", func(t *testing.T) {
		t.Parallel()
		assertErrContains(t, build(pp.Op{ID: "a", Kind: pp.KindAggregate,
			Aggregate: &pp.Aggregate{Aggregations: []pp.AggregationFunc{
				{Function: "sum", SourceColumn: "x", TargetColumn: "v"},
				{Function: "avg", SourceColumn: "y", TargetColumn: "v"},
			}}}).Validate(), `duplicate target column "v"`)
	})

	t.Run("limit non-positive", func(t *testing.T) {
		t.Parallel()
		assertErrContains(t, build(pp.Op{ID: "l", Kind: pp.KindLimit,
			Limit: &pp.Limit{N: 0}}).Validate(), "must be > 0")
	})

	t.Run("write_table unknown mode", func(t *testing.T) {
		t.Parallel()
		plan := pp.Plan{Ops: []pp.Op{
			{ID: "s", Kind: pp.KindReadTable,
				ReadTable: &pp.ReadTable{Catalog: "c", Namespace: "n", Table: "t"}},
			{ID: "w", Kind: pp.KindWriteTable, Inputs: []string{"s"},
				WriteTable: &pp.WriteTable{Catalog: "c", Namespace: "n", Table: "t", Mode: "merge"}},
		}}
		assertErrContains(t, plan.Validate(), `unknown mode "merge"`)
	})

	t.Run("read_table missing catalog", func(t *testing.T) {
		t.Parallel()
		plan := pp.Plan{Ops: []pp.Op{
			{ID: "s", Kind: pp.KindReadTable, ReadTable: &pp.ReadTable{Namespace: "n", Table: "t"}},
			{ID: "w", Kind: pp.KindWriteTable, Inputs: []string{"s"},
				WriteTable: &pp.WriteTable{Catalog: "c", Namespace: "n", Table: "t", Mode: pp.WriteModeAppend}},
		}}
		assertErrContains(t, plan.Validate(), "read_table.catalog")
	})

	t.Run("cast empty column", func(t *testing.T) {
		t.Parallel()
		assertErrContains(t, build(pp.Op{ID: "c", Kind: pp.KindCast,
			Cast: &pp.Cast{Casts: []pp.ColumnCast{{Column: ""}}}}).Validate(),
			"cast.casts[0].column")
	})

	t.Run("rename empty mapping pair", func(t *testing.T) {
		t.Parallel()
		assertErrContains(t, build(pp.Op{ID: "r", Kind: pp.KindRename,
			Rename: &pp.Rename{Mapping: []pp.ColumnPair{{From: "a", To: ""}}}}).Validate(),
			"rename.mapping[0]")
	})
}

func TestValidationErrors_AggregateString(t *testing.T) {
	t.Parallel()
	errs := pp.ValidationErrors{
		{OpID: "x", Field: "f", Message: "boom"},
		{Message: "global"},
	}
	got := errs.Error()
	if !strings.Contains(got, "boom") || !strings.Contains(got, "global") {
		t.Errorf("Error() = %q, missing parts", got)
	}
}

func TestIsAggregation(t *testing.T) {
	t.Parallel()
	for _, fn := range pp.AllAggregations() {
		if !pp.IsAggregation(fn) {
			t.Errorf("IsAggregation(%q) false but in AllAggregations", fn)
		}
	}
	if pp.IsAggregation("median") {
		t.Error("median should not be recognised in v1")
	}
}

func TestIsWriteMode(t *testing.T) {
	t.Parallel()
	if !pp.IsWriteMode(pp.WriteModeCreateOrReplace) {
		t.Error("create_or_replace should be valid")
	}
	if !pp.IsWriteMode(pp.WriteModeAppend) {
		t.Error("append should be valid")
	}
	if pp.IsWriteMode("merge") {
		t.Error("merge should not be valid")
	}
}
