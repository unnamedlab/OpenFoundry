package plancomposer

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	pipelineplan "github.com/openfoundry/openfoundry-go/libs/pipeline-plan"
)

const LegacySQLUnsupportedMessage = "legacy SQL distributed config is unsupported by plan runner"

var ErrLegacySQLUnsupported = errors.New(LegacySQLUnsupportedMessage)

type Defaults struct {
	PipelineID string
	RunID      string
	Catalog    string
	Namespace  string
	WriteMode  pipelineplan.WriteMode
}

func Compose(raw json.RawMessage, defaults Defaults) (pipelineplan.Plan, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return pipelineplan.Plan{}, errors.New("distributed plan config is empty")
	}
	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return pipelineplan.Plan{}, fmt.Errorf("parse distributed plan config: %w", err)
	}
	if len(cfg.Plan) > 0 {
		var plan pipelineplan.Plan
		if err := json.Unmarshal(cfg.Plan, &plan); err != nil {
			return pipelineplan.Plan{}, fmt.Errorf("parse distributed plan config.plan: %w", err)
		}
		return finalize(plan, defaults)
	}
	if len(cfg.Ops) > 0 {
		var ops []pipelineplan.Op
		if err := json.Unmarshal(cfg.Ops, &ops); err != nil {
			return pipelineplan.Plan{}, fmt.Errorf("parse distributed plan config.ops: %w", err)
		}
		return finalize(pipelineplan.Plan{Ops: ops}, defaults)
	}
	if len(cfg.Input) > 0 || len(cfg.Output) > 0 || len(cfg.Steps) > 0 {
		plan, err := composeDeclarative(cfg, defaults)
		if err != nil {
			return pipelineplan.Plan{}, err
		}
		return finalize(plan, defaults)
	}
	if hasLegacySQL(cfg) {
		return pipelineplan.Plan{}, ErrLegacySQLUnsupported
	}
	return pipelineplan.Plan{}, errors.New("distributed plan config must include plan, ops, or input/output/steps")
}

type config struct {
	Plan      json.RawMessage `json:"plan"`
	Ops       json.RawMessage `json:"ops"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`
	Steps     json.RawMessage `json:"steps"`
	SQL       string          `json:"sql"`
	Statement string          `json:"statement"`
	Catalog   string          `json:"catalog"`
	Namespace string          `json:"namespace"`
}

type tableRef struct {
	Catalog   string `json:"catalog"`
	Namespace string `json:"namespace"`
	Table     string `json:"table"`
	Mode      string `json:"mode"`
}

type stepConfig struct {
	ID           string                         `json:"id"`
	Kind         pipelineplan.Kind              `json:"kind"`
	Expr         string                         `json:"expr"`
	Predicate    string                         `json:"predicate"`
	N            int64                          `json:"n"`
	Columns      []pipelineplan.ProjectColumn   `json:"columns"`
	Mapping      []pipelineplan.ColumnPair      `json:"mapping"`
	Casts        []pipelineplan.ColumnCast      `json:"casts"`
	GroupBy      []string                       `json:"group_by"`
	Aggregations []pipelineplan.AggregationFunc `json:"aggregations"`
}

func composeDeclarative(cfg config, defaults Defaults) (pipelineplan.Plan, error) {
	var input tableRef
	if len(cfg.Input) > 0 {
		if err := json.Unmarshal(cfg.Input, &input); err != nil {
			return pipelineplan.Plan{}, fmt.Errorf("parse distributed plan input: %w", err)
		}
	}
	var output tableRef
	if len(cfg.Output) > 0 {
		if err := json.Unmarshal(cfg.Output, &output); err != nil {
			return pipelineplan.Plan{}, fmt.Errorf("parse distributed plan output: %w", err)
		}
	}
	applyTableDefaults(&input, cfg, defaults)
	applyTableDefaults(&output, cfg, defaults)
	if strings.TrimSpace(input.Table) == "" {
		return pipelineplan.Plan{}, errors.New("distributed plan input.table must not be empty")
	}
	if strings.TrimSpace(output.Table) == "" {
		return pipelineplan.Plan{}, errors.New("distributed plan output.table must not be empty")
	}
	mode := pipelineplan.WriteMode(strings.TrimSpace(output.Mode))
	if mode == "" {
		mode = defaults.WriteMode
	}
	if mode == "" {
		mode = pipelineplan.WriteModeCreateOrReplace
	}

	ops := []pipelineplan.Op{{
		ID:   "read_input",
		Kind: pipelineplan.KindReadTable,
		ReadTable: &pipelineplan.ReadTable{
			Catalog: input.Catalog, Namespace: input.Namespace, Table: input.Table,
		},
	}}
	prev := "read_input"
	if len(cfg.Steps) > 0 {
		var steps []stepConfig
		if err := json.Unmarshal(cfg.Steps, &steps); err != nil {
			return pipelineplan.Plan{}, fmt.Errorf("parse distributed plan steps: %w", err)
		}
		for i, step := range steps {
			op, err := opFromStep(step, i, prev)
			if err != nil {
				return pipelineplan.Plan{}, err
			}
			ops = append(ops, op)
			prev = op.ID
		}
	}
	ops = append(ops, pipelineplan.Op{
		ID:     "write_output",
		Kind:   pipelineplan.KindWriteTable,
		Inputs: []string{prev},
		WriteTable: &pipelineplan.WriteTable{
			Catalog: output.Catalog, Namespace: output.Namespace, Table: output.Table, Mode: mode,
		},
	})
	return pipelineplan.Plan{Ops: ops}, nil
}

func opFromStep(step stepConfig, index int, input string) (pipelineplan.Op, error) {
	kind := pipelineplan.Kind(strings.TrimSpace(string(step.Kind)))
	if kind == "" {
		return pipelineplan.Op{}, fmt.Errorf("distributed plan steps[%d].kind must not be empty", index)
	}
	id := strings.TrimSpace(step.ID)
	if id == "" {
		id = fmt.Sprintf("%s_%d", kind, index+1)
	}
	op := pipelineplan.Op{ID: id, Kind: kind, Inputs: []string{input}}
	switch kind {
	case pipelineplan.KindFilter:
		expr := strings.TrimSpace(firstNonEmpty(step.Expr, step.Predicate))
		op.Filter = &pipelineplan.Filter{Expr: expr}
	case pipelineplan.KindLimit:
		op.Limit = &pipelineplan.Limit{N: step.N}
	case pipelineplan.KindProject:
		op.Project = &pipelineplan.Project{Columns: step.Columns}
	case pipelineplan.KindRename:
		op.Rename = &pipelineplan.Rename{Mapping: step.Mapping}
	case pipelineplan.KindCast:
		op.Cast = &pipelineplan.Cast{Casts: step.Casts}
	case pipelineplan.KindAggregate:
		op.Aggregate = &pipelineplan.Aggregate{GroupBy: step.GroupBy, Aggregations: step.Aggregations}
	case pipelineplan.KindUnion:
		return pipelineplan.Op{}, fmt.Errorf("distributed plan steps[%d].kind union requires explicit ops config", index)
	case pipelineplan.KindReadTable, pipelineplan.KindWriteTable:
		return pipelineplan.Op{}, fmt.Errorf("distributed plan steps[%d].kind %s is not valid inside steps", index, kind)
	default:
		return pipelineplan.Op{}, fmt.Errorf("distributed plan steps[%d].kind %q is unsupported", index, kind)
	}
	return op, nil
}

func applyTableDefaults(ref *tableRef, cfg config, defaults Defaults) {
	if strings.TrimSpace(ref.Catalog) == "" {
		ref.Catalog = firstNonEmpty(cfg.Catalog, defaults.Catalog, "lakekeeper")
	}
	if strings.TrimSpace(ref.Namespace) == "" {
		ref.Namespace = firstNonEmpty(cfg.Namespace, defaults.Namespace, "default")
	}
}

func finalize(plan pipelineplan.Plan, defaults Defaults) (pipelineplan.Plan, error) {
	if strings.TrimSpace(plan.PipelineID) == "" {
		plan.PipelineID = strings.TrimSpace(defaults.PipelineID)
	}
	if strings.TrimSpace(plan.RunID) == "" {
		plan.RunID = strings.TrimSpace(defaults.RunID)
	}
	if errs := plan.Validate(); errs != nil {
		return pipelineplan.Plan{}, fmt.Errorf("distributed plan invalid: %w", errs)
	}
	return plan, nil
}

func hasLegacySQL(cfg config) bool {
	return strings.TrimSpace(cfg.SQL) != "" || strings.TrimSpace(cfg.Statement) != ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
