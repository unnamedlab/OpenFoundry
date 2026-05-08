// Package engine — top-level pipeline orchestrator.
//
// 1:1 port of `services/pipeline-build-service/src/domain/engine/mod.rs`.
// `ExecutePipeline` dispatches to the sequential or distributed-worker
// driver based on `request.DistributedWorkerCount`. Both walk the DAG
// in topological order, attach fingerprints to the per-node metadata,
// honour the `skip_unchanged` shortcut by comparing previous
// metadata fingerprints, and bail out on the first failed node.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
)

// ExecutePipeline mirrors `pub async fn execute_pipeline` in the
// Rust crate. Returns the per-node results in the order they were
// executed; the slice is truncated at the first failed node so the
// failure cascade matches the Rust impl.
func ExecutePipeline(
	ctx context.Context,
	env *ExecutionEnvironment,
	nodes []PipelineNode,
	request *ExecutionRequest,
) ([]NodeResult, error) {
	if request == nil {
		def := DefaultExecutionRequest()
		request = &def
	}
	if request.DistributedWorkerCount > 1 {
		return executePipelineDistributed(ctx, env, nodes, request)
	}
	return executePipelineSequential(ctx, env, nodes, request)
}

func executePipelineSequential(
	ctx context.Context,
	env *ExecutionEnvironment,
	nodes []PipelineNode,
	request *ExecutionRequest,
) ([]NodeResult, error) {
	order, err := executionOrder(nodes, request.StartFromNode)
	if err != nil {
		return nil, err
	}
	results := make([]NodeResult, 0, len(order))
	dependencyFingerprints := map[string]string{}
	lookup := map[string]*PipelineNode{}
	for i := range nodes {
		lookup[nodes[i].ID] = &nodes[i]
	}
	maxAttempts := request.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	for _, id := range order {
		node, ok := lookup[id]
		if !ok {
			return nil, fmt.Errorf("pipeline node '%s' not found", id)
		}
		var prior *NodeResult
		if request.PriorNodeResults != nil {
			if r, ok := request.PriorNodeResults[node.ID]; ok {
				p := r
				prior = &p
			}
		}
		var final NodeResult
		for attempt := uint32(1); attempt <= maxAttempts; attempt++ {
			r := executeNode(ctx, env, node, dependencyFingerprints, request.SkipUnchanged, prior)
			r.Attempts = attempt
			final = r
			terminal := r.Status == "completed" || r.Status == "skipped" || attempt == maxAttempts
			if terminal {
				break
			}
		}
		if fp, ok := fingerprintFromMetadata(final.Metadata); ok {
			dependencyFingerprints[node.ID] = fp
		}
		failed := final.Status == "failed"
		results = append(results, final)
		if failed {
			break
		}
	}
	return results, nil
}

// executeNode mirrors `pub(crate) async fn execute_node`. Loads
// inputs, computes the fingerprint, honours the skip-unchanged
// shortcut, and dispatches on `transform_type`.
func executeNode(
	ctx context.Context,
	env *ExecutionEnvironment,
	node *PipelineNode,
	dependencyFingerprints map[string]string,
	skipUnchanged bool,
	priorNodeResult *NodeResult,
) NodeResult {
	inputs, err := loadNodeInputs(ctx, env.State, env.ActorID, node)
	if err != nil {
		return failedResult(node, err.Error())
	}
	fingerprint := nodeFingerprint(node, inputs, dependencyFingerprints)

	if skipUnchanged && priorNodeResult != nil {
		if prevFP, ok := fingerprintFromMetadata(priorNodeResult.Metadata); ok && prevFP == fingerprint {
			output := priorNodeResult.Output
			if len(output) == 0 {
				output, _ = json.Marshal(map[string]any{
					"message": "node skipped because inputs did not change",
				})
			}
			version := outputDatasetVersionFromMetadata(priorNodeResult.Metadata)
			return NodeResult{
				NodeID:        node.ID,
				Label:         node.Label,
				TransformType: node.TransformType,
				Status:        "skipped",
				RowsAffected:  priorNodeResult.RowsAffected,
				Attempts:      1,
				Output:        output,
				Metadata:      buildMetadata(fingerprint, true, inputs, node.OutputDatasetID, version),
			}
		}
	}

	switch node.TransformType {
	case "sql":
		result, err := executeSQLTransform(ctx, env.State, node, inputs)
		if err != nil {
			return failedResult(node, err.Error())
		}
		return successResult(node, result.RowsAffected, result.Output,
			buildMetadata(fingerprint, false, inputs, node.OutputDatasetID, result.OutputDatasetVersion))
	case "python":
		result, err := executePythonTransform(ctx, env.State, node, inputs)
		if err != nil {
			return failedResult(node, err.Error())
		}
		return successResult(node, result.RowsAffected, result.Output,
			buildMetadata(fingerprint, false, inputs, node.OutputDatasetID, result.OutputDatasetVersion))
	case "llm":
		result, err := executeLLMTransform(ctx, env.State, node, inputs)
		if err != nil {
			return failedResult(node, err.Error())
		}
		return successResult(node, result.RowsAffected, result.Output,
			buildMetadata(fingerprint, false, inputs, node.OutputDatasetID, result.OutputDatasetVersion))
	case "wasm":
		rows, output, err := executeWASMTransform(node)
		if err != nil {
			return failedResult(node, err.Error())
		}
		return successResult(node, rows, output,
			buildMetadata(fingerprint, false, inputs, node.OutputDatasetID, nil))
	case "passthrough":
		rows, output, version, err := executePassthroughTransform(ctx, env.State, node, inputs)
		if err != nil {
			return failedResult(node, err.Error())
		}
		return successResult(node, rows, output,
			buildMetadata(fingerprint, false, inputs, node.OutputDatasetID, version))
	case "spark", "pyspark":
		result, err := executeDistributedComputeTransform(ctx, env.State, node, inputs)
		if err != nil {
			return failedResult(node, err.Error())
		}
		return successResult(node, result.RowsAffected, result.Output,
			buildMetadata(fingerprint, false, inputs, node.OutputDatasetID, result.OutputDatasetVersion))
	case "external", "remote":
		result, err := executeRemoteComputeTransform(ctx, env.State, node, inputs, "external")
		if err != nil {
			return failedResult(node, err.Error())
		}
		return successResult(node, result.RowsAffected, result.Output,
			buildMetadata(fingerprint, false, inputs, node.OutputDatasetID, result.OutputDatasetVersion))
	default:
		return failedResult(node, fmt.Sprintf("unsupported transform type: %s", node.TransformType))
	}
}

func successResult(node *PipelineNode, rowsAffected *uint64, output, metadata json.RawMessage) NodeResult {
	return NodeResult{
		NodeID:        node.ID,
		Label:         node.Label,
		TransformType: node.TransformType,
		Status:        "completed",
		RowsAffected:  rowsAffected,
		Attempts:      1,
		Output:        output,
		Metadata:      metadata,
	}
}

func failedResult(node *PipelineNode, message string) NodeResult {
	msg := message
	return NodeResult{
		NodeID:        node.ID,
		Label:         node.Label,
		TransformType: node.TransformType,
		Status:        "failed",
		Attempts:      1,
		Error:         &msg,
	}
}
