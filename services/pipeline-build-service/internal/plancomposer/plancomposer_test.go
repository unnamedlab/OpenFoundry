package plancomposer

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	pipelineplan "github.com/openfoundry/openfoundry-go/libs/pipeline-plan"
)

func TestComposePlanConfig(t *testing.T) {
	raw := json.RawMessage(`{"plan":{"pipeline_id":"p","run_id":"r","ops":[{"id":"src","kind":"read_table","read_table":{"catalog":"lakekeeper","namespace":"default","table":"source"}},{"id":"sink","kind":"write_table","inputs":["src"],"write_table":{"catalog":"lakekeeper","namespace":"default","table":"dest","mode":"append"}}]}}`)
	plan, err := Compose(raw, Defaults{PipelineID: "fallback-p", RunID: "fallback-r"})
	if err != nil {
		t.Fatalf("Compose returned error: %v", err)
	}
	if plan.PipelineID != "p" || plan.RunID != "r" {
		t.Fatalf("Compose should preserve explicit IDs, got %q/%q", plan.PipelineID, plan.RunID)
	}
	if errs := plan.Validate(); errs != nil {
		t.Fatalf("composed plan is invalid: %v", errs)
	}
}

func TestComposeOpsConfig(t *testing.T) {
	raw := json.RawMessage(`{"ops":[{"id":"src","kind":"read_table","read_table":{"catalog":"lakekeeper","namespace":"default","table":"source"}},{"id":"sink","kind":"write_table","inputs":["src"],"write_table":{"catalog":"lakekeeper","namespace":"default","table":"dest","mode":"create_or_replace"}}]}`)
	plan, err := Compose(raw, Defaults{PipelineID: "p", RunID: "r"})
	if err != nil {
		t.Fatalf("Compose returned error: %v", err)
	}
	if plan.PipelineID != "p" || plan.RunID != "r" {
		t.Fatalf("Compose should fill missing IDs, got %q/%q", plan.PipelineID, plan.RunID)
	}
	if len(plan.Ops) != 2 || plan.Ops[1].WriteTable.Mode != pipelineplan.WriteModeCreateOrReplace {
		t.Fatalf("unexpected ops plan: %+v", plan)
	}
}

func TestComposeDeclarativeInputOutputSteps(t *testing.T) {
	raw := json.RawMessage(`{"input":{"catalog":"lakekeeper","namespace":"default","table":"source_table"},"output":{"catalog":"lakekeeper","namespace":"default","table":"dest_table","mode":"create_or_replace"},"steps":[{"id":"filter_active","kind":"filter","expr":"status == \"active\""},{"id":"limit_100","kind":"limit","n":100}]}`)
	plan, err := Compose(raw, Defaults{PipelineID: "p", RunID: "r"})
	if err != nil {
		t.Fatalf("Compose returned error: %v", err)
	}
	if got, want := len(plan.Ops), 4; got != want {
		t.Fatalf("len(plan.Ops)=%d, want %d: %+v", got, want, plan.Ops)
	}
	if plan.Ops[0].Kind != pipelineplan.KindReadTable || plan.Ops[1].Kind != pipelineplan.KindFilter || plan.Ops[2].Kind != pipelineplan.KindLimit || plan.Ops[3].Kind != pipelineplan.KindWriteTable {
		t.Fatalf("unexpected op chain: %+v", plan.Ops)
	}
	if plan.Ops[3].Inputs[0] != "limit_100" {
		t.Fatalf("write op should consume final step, got inputs %+v", plan.Ops[3].Inputs)
	}
	if errs := plan.Validate(); errs != nil {
		t.Fatalf("composed plan is invalid: %v", errs)
	}
}

func TestComposeLegacySQLUnsupported(t *testing.T) {
	_, err := Compose(json.RawMessage(`{"sql":"SELECT * FROM table"}`), Defaults{PipelineID: "p", RunID: "r"})
	if !errors.Is(err, ErrLegacySQLUnsupported) {
		t.Fatalf("expected ErrLegacySQLUnsupported, got %v", err)
	}
	if !strings.Contains(err.Error(), LegacySQLUnsupportedMessage) {
		t.Fatalf("legacy SQL error should be explicit, got %q", err.Error())
	}
}

func TestComposeDeclarativeRequiresOutputTable(t *testing.T) {
	_, err := Compose(json.RawMessage(`{"input":{"table":"source"},"output":{"catalog":"lakekeeper"},"steps":[{"kind":"limit","n":1}]}`), Defaults{PipelineID: "p", RunID: "r"})
	if err == nil || !strings.Contains(err.Error(), "output.table") {
		t.Fatalf("expected output.table error, got %v", err)
	}
}
