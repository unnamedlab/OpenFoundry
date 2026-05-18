package handler

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/domain/executor"
	dispatchpkg "github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/dispatch"
)

// TestSparkFlinkDistributedRunnerSurfacesPlanCompositionTODO asserts
// the C.4.a transitional behaviour: the dispatcher refuses to submit
// any Spark-like node because the composer that builds a
// pipelineplan.Plan from the DAG node config has not landed yet
// (Phase C.4.b). The two previous tests verified the SparkApplication
// CR fields (InlineSQL, Catalog, ApplicationType, ExecutorInstances)
// which have all been removed from PipelineRunInput.
func TestSparkFlinkDistributedRunnerSurfacesPlanCompositionTODO(t *testing.T) {
	fake := &fakeSparkClient{submittedName: "should-not-submit", status: &dispatchpkg.RunStatusReport{Status: dispatchpkg.RunSucceeded}}
	runner := NewSparkFlinkDistributedRunner(DistributedRuntimeConfig{
		SparkClientProvider: func() (dispatchpkg.Client, bool) { return fake, true },
		Namespace:           "cluster-ns",
		RunnerImage:         "runner:unit",
		PollInterval:        time.Nanosecond,
		Timeout:             time.Second,
	})

	_, err := runner.RunDistributedTransform(context.Background(), DistributedTransformRequest{
		Node: executor.NodeContext{
			BuildID: uuid.MustParse("11111111-2222-3333-4444-555555555555"),
			Node: executor.Node{
				ID:        "spark-output",
				DependsOn: []string{"ri.dataset.main.trails"},
				Outputs:   []executor.OutputTransaction{{DatasetRID: "ri.dataset.main.out"}},
			},
		},
		Payload:       json.RawMessage(`{"engine":"spark","sql":"SELECT * FROM trails"}`),
		TransformType: "output_dataset",
		Engine:        "spark",
	})

	require.ErrorIs(t, err, ErrPlanCompositionNotImplemented)
	require.Equal(t, "", fake.submitted.PipelineID, "no Job should have been submitted")
}

func TestSparkFlinkDistributedRunnerFlinkIsExplicitlyAdapterGated(t *testing.T) {
	runner := NewSparkFlinkDistributedRunner(DistributedRuntimeConfig{})

	_, err := runner.RunDistributedTransform(context.Background(), DistributedTransformRequest{
		Node:          executor.NodeContext{BuildID: uuid.New(), Node: executor.Node{ID: "flink"}},
		TransformType: "flink",
		Engine:        "flink",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "flink_runtime_not_configured")
}

func TestDistributedPipelineClusterSmokeGated(t *testing.T) {
	if os.Getenv("OPENFOUNDRY_DISTRIBUTED_CLUSTER_SMOKE") != "1" {
		t.Skip("set OPENFOUNDRY_DISTRIBUTED_CLUSTER_SMOKE=1 with Kubernetes/Spark config to run this optional cluster smoke")
	}
	inputRID := os.Getenv("OPENFOUNDRY_DISTRIBUTED_INPUT_DATASET_RID")
	outputRID := os.Getenv("OPENFOUNDRY_DISTRIBUTED_OUTPUT_DATASET_RID")
	require.NotEmpty(t, inputRID, "OPENFOUNDRY_DISTRIBUTED_INPUT_DATASET_RID is required")
	require.NotEmpty(t, outputRID, "OPENFOUNDRY_DISTRIBUTED_OUTPUT_DATASET_RID is required")

	runner := NewSparkFlinkDistributedRunner(DistributedRuntimeConfig{
		PollInterval: time.Second,
		Timeout:      5 * time.Minute,
	})
	result, err := runner.RunDistributedTransform(context.Background(), DistributedTransformRequest{
		Node: executor.NodeContext{
			BuildID: uuid.New(),
			Node: executor.Node{
				ID:      "cluster-output",
				Outputs: []executor.OutputTransaction{{DatasetRID: outputRID}},
				Metadata: map[string]any{
					"input_dataset_ids": []string{inputRID},
				},
			},
		},
		Payload:       json.RawMessage(`{"engine":"spark","sql":"SELECT * FROM input_table"}`),
		TransformType: "output_dataset",
		Engine:        "spark",
	})
	require.NoError(t, err)
	require.Equal(t, "distributed", result.Metadata["runtime"])
}
