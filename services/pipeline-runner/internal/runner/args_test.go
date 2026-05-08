package runner

import (
	"testing"
)

func TestParseArgsHonoursAllFlags(t *testing.T) {
	t.Parallel()
	args, err := ParseArgs([]string{
		"--pipeline-id", "p1",
		"--run-id", "r1",
		"--input-dataset", "cat.ns.in",
		"--output-dataset", "cat.ns.out",
		"--catalog", "lakekeeper",
		"--catalog-uri", "https://lkk",
		"--pipeline-build-url", "http://pb",
		"--smoke",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args.PipelineID != "p1" || args.RunID != "r1" || !args.Smoke {
		t.Fatalf("missing fields: %+v", args)
	}
	if args.PipelineBuildURL != "http://pb" {
		t.Fatalf("expected explicit url, got %s", args.PipelineBuildURL)
	}
}

func TestParseArgsRejectsUnknown(t *testing.T) {
	t.Parallel()
	_, err := ParseArgs([]string{"--bogus", "x"})
	if err == nil {
		t.Fatal("expected error on unknown flag")
	}
}

func TestParseArgsRequiresAllMandatory(t *testing.T) {
	t.Parallel()
	_, err := ParseArgs([]string{"--pipeline-id", "p1"})
	if err == nil {
		t.Fatal("expected error when mandatory flags missing")
	}
}

func TestParseArgsFlagWithoutValue(t *testing.T) {
	t.Parallel()
	_, err := ParseArgs([]string{"--pipeline-id"})
	if err == nil {
		t.Fatal("expected error for dangling flag")
	}
}

func TestParseArgsDefaultsBuildURL(t *testing.T) {
	// Cannot t.Parallel: t.Setenv mutates process env.
	t.Setenv("OF_PIPELINE_BUILD_URL", "")
	args, err := ParseArgs([]string{
		"--pipeline-id", "p", "--run-id", "r",
		"--input-dataset", "i", "--output-dataset", "o",
		"--catalog", "c", "--catalog-uri", "u",
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if args.PipelineBuildURL != DefaultPipelineBuildURL {
		t.Fatalf("expected default URL, got %s", args.PipelineBuildURL)
	}
}

func TestSmokeSpecEscapesQuotes(t *testing.T) {
	t.Parallel()
	spec := SmokeSpec(Args{RunID: "r'1"})
	want := "SELECT CAST('r''1' AS STRING) AS run_id"
	if spec.SQL == "" || !contains(spec.SQL, want) {
		t.Fatalf("smoke SQL did not escape quote: %s", spec.SQL)
	}
	if spec.Format != "iceberg" {
		t.Fatalf("expected iceberg format, got %q", spec.Format)
	}
}

func TestParseSpecDefaultsFormat(t *testing.T) {
	t.Parallel()
	spec, err := parseSpec([]byte(`{"sql":"SELECT 1"}`))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if spec.Format != "iceberg" {
		t.Fatalf("expected iceberg, got %q", spec.Format)
	}
}

func TestParseSpecRejectsMissingSQL(t *testing.T) {
	t.Parallel()
	_, err := parseSpec([]byte(`{"format":"iceberg"}`))
	if err == nil {
		t.Fatal("expected error when sql missing")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
