package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// Mirrors the Rust unit `parses_typescript_runtime_config`.
func TestParseInlineFunctionConfig_TypeScript(t *testing.T) {
	t.Parallel()
	body := []byte(`{"runtime":"typescript","source":"export default async function handler() { return { ok: true }; }"}`)
	cfg, err := ParseInlineFunctionConfig(body)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if cfg == nil || cfg.Kind != InlineFunctionTypeScript {
		t.Fatalf("expected TypeScript variant, got %+v", cfg)
	}
	if cfg.RuntimeName() != "typescript" {
		t.Fatalf("runtime drift: %s", cfg.RuntimeName())
	}
}

func TestParseInlineFunctionConfig_Python(t *testing.T) {
	t.Parallel()
	body := []byte(`{"runtime":"python","source":"def handler(c): return {}"}`)
	cfg, err := ParseInlineFunctionConfig(body)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if cfg == nil || cfg.Kind != InlineFunctionPython {
		t.Fatalf("expected Python variant, got %+v", cfg)
	}
}

func TestParseInlineFunctionConfig_NoRuntime(t *testing.T) {
	t.Parallel()
	cfg, err := ParseInlineFunctionConfig([]byte(`{"source":"x"}`))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected (nil, nil) when runtime field missing")
	}
}

func TestParseInlineFunctionConfig_RejectsEmptySource(t *testing.T) {
	t.Parallel()
	_, err := ParseInlineFunctionConfig([]byte(`{"runtime":"python","source":"   "}`))
	if err == nil {
		t.Fatal("expected error on empty python source")
	}
	_, err = ParseInlineFunctionConfig([]byte(`{"runtime":"typescript","source":""}`))
	if err == nil {
		t.Fatal("expected error on empty TypeScript source")
	}
}

func TestParseInlineFunctionConfig_RejectsUnknownRuntime(t *testing.T) {
	t.Parallel()
	_, err := ParseInlineFunctionConfig([]byte(`{"runtime":"ruby","source":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported function runtime") {
		t.Fatalf("expected unsupported runtime error, got %v", err)
	}
}

func TestValidateFunctionCapabilities_RejectsExceedingSource(t *testing.T) {
	t.Parallel()
	cfg := InlineFunctionConfig{
		Kind:       InlineFunctionTypeScript,
		TypeScript: &InlineTypeScriptFunctionConfig{Runtime: "typescript", Source: strings.Repeat("x", 100)},
	}
	caps := models.FunctionCapabilities{
		MaxSourceBytes: 50, TimeoutSeconds: 15,
	}
	if err := ValidateFunctionCapabilities(cfg, caps, nil); err == nil ||
		!strings.Contains(err.Error(), "exceeds max_source_bytes") {
		t.Fatalf("expected max_source_bytes failure, got %v", err)
	}
}

func TestValidateFunctionCapabilities_RejectsBadTimeout(t *testing.T) {
	t.Parallel()
	cfg := InlineFunctionConfig{
		Kind:       InlineFunctionTypeScript,
		TypeScript: &InlineTypeScriptFunctionConfig{Runtime: "typescript", Source: "x"},
	}
	caps := models.FunctionCapabilities{TimeoutSeconds: 0, MaxSourceBytes: 1024}
	if err := ValidateFunctionCapabilities(cfg, caps, nil); err == nil {
		t.Fatal("expected timeout=0 to fail")
	}
	caps.TimeoutSeconds = 301
	if err := ValidateFunctionCapabilities(cfg, caps, nil); err == nil {
		t.Fatal("expected timeout>300 to fail")
	}
}

func TestValidateFunctionCapabilities_RejectsBadEntrypoint(t *testing.T) {
	t.Parallel()
	cfg := InlineFunctionConfig{
		Kind:       InlineFunctionTypeScript,
		TypeScript: &InlineTypeScriptFunctionConfig{Runtime: "typescript", Source: "x"},
	}
	caps := models.FunctionCapabilities{TimeoutSeconds: 15, MaxSourceBytes: 1024}
	pkg := models.FunctionPackageSummary{Entrypoint: "main"}
	if err := ValidateFunctionCapabilities(cfg, caps, &pkg); err == nil ||
		!strings.Contains(err.Error(), "unsupported function package entrypoint") {
		t.Fatalf("expected entrypoint failure, got %v", err)
	}
	pkg.Entrypoint = "default"
	if err := ValidateFunctionCapabilities(cfg, caps, &pkg); err != nil {
		t.Fatalf("default entrypoint must be accepted: %v", err)
	}
}

// Mirrors the Rust unit `enriches_typescript_result_with_logs`.
func TestEnrichTypeScriptResultWithLogs(t *testing.T) {
	t.Parallel()
	got := enrichTypeScriptResult(
		json.RawMessage(`{"object_patch":{"status":"done"}}`),
		[]string{"hello"}, nil,
	)
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(got, &asMap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(string(asMap["stdout"]), "hello") {
		t.Fatalf("stdout drift: %s", asMap["stdout"])
	}
	var output map[string]any
	_ = json.Unmarshal(asMap["output"], &output)
	if stdout, ok := output["stdout"].([]any); !ok || len(stdout) != 1 || stdout[0] != "hello" {
		t.Fatalf("output.stdout drift: %v", output)
	}
}

// Mirrors `resolves_exact_versioned_package_reference`.
func TestSelectFunctionPackageVersion_ExactMatch(t *testing.T) {
	t.Parallel()
	packages := []models.FunctionPackage{
		{ID: uuid.Nil, Name: "triage", Version: "1.1.0"},
		{ID: uuid.Nil, Name: "triage", Version: "1.2.0"},
	}
	ref := versionedFunctionPackageReferenceConfig{Name: "triage", Version: "1.1.0"}
	pkg, err := selectFunctionPackageVersion(packages, ref)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if pkg == nil || pkg.Version != "1.1.0" {
		t.Fatalf("expected 1.1.0, got %+v", pkg)
	}
}

// Mirrors `resolves_latest_compatible_auto_upgrade_release`.
func TestSelectFunctionPackageVersion_AutoUpgradeLatest(t *testing.T) {
	t.Parallel()
	packages := []models.FunctionPackage{
		{Name: "triage", Version: "1.1.0"},
		{Name: "triage", Version: "1.3.2"},
		{Name: "triage", Version: "2.0.0"},
	}
	ref := versionedFunctionPackageReferenceConfig{
		Name: "triage", Version: "1.2.0", AutoUpgrade: true,
	}
	pkg, err := selectFunctionPackageVersion(packages, ref)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if pkg == nil {
		t.Fatal("expected a match")
	}
	if pkg.Version != "1.3.2" {
		t.Fatalf("expected latest 1.x.y >= 1.2.0 = 1.3.2, got %s", pkg.Version)
	}
}

// Mirrors `rejects_auto_upgrade_for_unstable_baseline`.
func TestSelectFunctionPackageVersion_RejectsUnstableAutoUpgrade(t *testing.T) {
	t.Parallel()
	packages := []models.FunctionPackage{{Name: "triage", Version: "0.3.0"}}
	ref := versionedFunctionPackageReferenceConfig{
		Name: "triage", Version: "0.3.0", AutoUpgrade: true,
	}
	_, err := selectFunctionPackageVersion(packages, ref)
	if err == nil || !strings.Contains(err.Error(), "stable baseline version 1.0.0 or above") {
		t.Fatalf("expected stable-baseline error, got %v", err)
	}
}

func TestExecuteInlinePythonFunction_ReturnsSentinel(t *testing.T) {
	t.Parallel()
	_, err := ExecuteInlinePythonFunction(nil, nil, nil, nil, nil, nil, nil, nil)
	if !errors.Is(err, ErrPythonRuntimeNotWired) {
		t.Fatalf("expected ErrPythonRuntimeNotWired, got %v", err)
	}
}

func TestObjectToJSON_RoundTrip(t *testing.T) {
	t.Parallel()
	props, _ := json.Marshal(map[string]any{"a": 1})
	obj := ObjectInstance{
		ID:           uuid.New(),
		ObjectTypeID: uuid.New(),
		Properties:   props,
		Marking:      "public",
		CreatedBy:    uuid.New(),
	}
	out := ObjectToJSON(obj)
	var asMap map[string]any
	if err := json.Unmarshal(out, &asMap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"id", "object_type_id", "marking", "properties", "created_by"} {
		if _, ok := asMap[key]; !ok {
			t.Errorf("missing key %q in object_to_json output", key)
		}
	}
}
