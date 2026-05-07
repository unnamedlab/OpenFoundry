// Package engine — distributed-worker pipeline executor.
//
// 1:1 port of `services/pipeline-build-service/src/domain/engine/dag_executor.rs`.
// Plans the DAG into stages of independent nodes (`executionStages`)
// and runs every stage with `min(workerBudget, len(stage))` goroutines
// in flight. Stages execute strictly in order; a failure inside a
// stage cuts the run short, mirroring the Rust `try_collect`.
//
// Output annotation: every result carries `worker_id` /
// `stage_index` and the JSON output gets the canonical `execution`
// envelope merged in (matches `annotate_output`).
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

func executePipelineDistributed(
	ctx context.Context,
	env *ExecutionEnvironment,
	nodes []PipelineNode,
	request *ExecutionRequest,
) ([]NodeResult, error) {
	stages, err := executionStages(nodes, request.StartFromNode)
	if err != nil {
		return nil, err
	}
	lookup := map[string]*PipelineNode{}
	for i := range nodes {
		lookup[nodes[i].ID] = &nodes[i]
	}
	maxAttempts := request.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	workerBudget := request.DistributedWorkerCount
	if workerBudget < 1 {
		workerBudget = 1
	}
	results := make([]NodeResult, 0, len(nodes))
	completed := map[string]string{}

	for stageIndex, stage := range stages {
		stageResults, err := runStage(ctx, env, stage, lookup, completed, request.SkipUnchanged,
			request.PriorNodeResults, maxAttempts, workerBudget, stageIndex)
		if err != nil {
			return nil, err
		}
		failedSeen := false
		for _, r := range stageResults {
			if fp, ok := fingerprintFromMetadata(r.Metadata); ok {
				completed[r.NodeID] = fp
			}
			results = append(results, r)
			if r.Status == "failed" {
				failedSeen = true
			}
		}
		if failedSeen {
			return results, nil
		}
	}
	return results, nil
}

type stageEntry struct {
	position int
	result   NodeResult
}

func runStage(
	ctx context.Context,
	env *ExecutionEnvironment,
	stage []string,
	lookup map[string]*PipelineNode,
	completed map[string]string,
	skipUnchanged bool,
	priors map[string]NodeResult,
	maxAttempts uint32,
	workerBudget int,
	stageIndex int,
) ([]NodeResult, error) {
	tasks := make(chan int, len(stage))
	for i := range stage {
		tasks <- i
	}
	close(tasks)

	out := make([]stageEntry, len(stage))
	var firstErr error
	var errMu sync.Mutex
	concurrency := workerBudget
	if concurrency > len(stage) {
		concurrency = len(stage)
	}
	if concurrency < 1 {
		concurrency = 1
	}

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pos := range tasks {
				nodeID := stage[pos]
				node, ok := lookup[nodeID]
				if !ok {
					errMu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("pipeline node '%s' not found", nodeID)
					}
					errMu.Unlock()
					return
				}
				var prior *NodeResult
				if priors != nil {
					if r, ok := priors[node.ID]; ok {
						p := r
						prior = &p
					}
				}
				result := executeNodeWithRetries(ctx, env, node, completed, skipUnchanged, prior, maxAttempts)
				slot := pos % workerBudget
				stageIdx := stageIndex
				workerID := fmt.Sprintf("pipeline-worker-%d", slot+1)
				result.StageIndex = &stageIdx
				result.WorkerID = &workerID
				annotateOutput(&result, stageIndex, slot)
				out[pos] = stageEntry{position: pos, result: result}
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(out, func(i, j int) bool { return out[i].position < out[j].position })
	results := make([]NodeResult, len(out))
	for i, e := range out {
		results[i] = e.result
	}
	return results, nil
}

func executeNodeWithRetries(
	ctx context.Context,
	env *ExecutionEnvironment,
	node *PipelineNode,
	dependencyFingerprints map[string]string,
	skipUnchanged bool,
	prior *NodeResult,
	maxAttempts uint32,
) NodeResult {
	var final NodeResult
	for attempt := uint32(1); attempt <= maxAttempts; attempt++ {
		r := executeNode(ctx, env, node, dependencyFingerprints, skipUnchanged, prior)
		r.Attempts = attempt
		final = r
		if r.Status == "completed" || r.Status == "skipped" || attempt == maxAttempts {
			break
		}
	}
	return final
}

// annotateOutput mirrors `fn annotate_output`. Merges the worker /
// stage info into the JSON output. When the output is a JSON object,
// merges in place; otherwise wraps the previous value under `result`.
func annotateOutput(result *NodeResult, stageIndex int, workerSlot int) {
	execution := map[string]any{
		"worker_id":   fmt.Sprintf("pipeline-worker-%d", workerSlot+1),
		"stage_index": stageIndex,
	}
	if len(result.Output) == 0 {
		out, _ := json.Marshal(map[string]any{"execution": execution})
		result.Output = out
		return
	}
	var asObj map[string]json.RawMessage
	if err := json.Unmarshal(result.Output, &asObj); err == nil {
		execJSON, _ := json.Marshal(execution)
		asObj["execution"] = execJSON
		out, _ := json.Marshal(asObj)
		result.Output = out
		return
	}
	wrapped, _ := json.Marshal(map[string]any{
		"result":    json.RawMessage(result.Output),
		"execution": execution,
	})
	result.Output = wrapped
}
