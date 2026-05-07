// Package engine — node fingerprint + metadata helpers + transform
// runtime stubs.
//
// Phase A delivers the deterministic node fingerprint (used by
// `skip_unchanged`), the metadata builder, and the dispatch surface
// every transform runtime will call into. Each transform-kind helper
// returns a `transform_runtime_not_wired:<kind>` failure today so the
// engine orchestration is testable without paying for the runtime
// wiring; the per-kind implementations land in:
//
//   - SQL → DataFusion-Go (Apache Arrow-Go) phase
//   - Python → libs/python-sidecar phase
//   - LLM → ai-service HTTP phase
//   - WASM → wasmtime-go phase
//   - Spark/PySpark → spark-on-k8s dispatch phase
//   - External/Remote → connector-management-service HTTP phase
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"

	"github.com/google/uuid"
)

// nodeFingerprint mirrors `pub fn node_fingerprint`. Hashes the node
// definition + dependency fingerprints + sorted input metadata so
// `skip_unchanged` can collapse re-runs whose effective inputs are
// unchanged.
func nodeFingerprint(
	node *PipelineNode,
	inputs []LoadedDataset,
	dependencyFingerprints map[string]string,
) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(node.ID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(node.Label))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(node.TransformType))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(node.Config)
	_, _ = h.Write([]byte{0})

	type inputKey struct {
		datasetID   string
		version     int32
		sizeBytes   int64
		storagePath string
	}
	keys := make([]inputKey, 0, len(inputs))
	for _, in := range inputs {
		keys = append(keys, inputKey{
			datasetID:   in.Metadata.DatasetID.String(),
			version:     in.Metadata.Version,
			sizeBytes:   in.Metadata.SizeBytes,
			storagePath: in.StoragePath,
		})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].datasetID < keys[j].datasetID })
	for _, k := range keys {
		_, _ = fmt.Fprintf(h, "%s|%d|%d|%s|", k.datasetID, k.version, k.sizeBytes, k.storagePath)
	}
	_, _ = h.Write([]byte{0})

	type depKey struct{ name, fp string }
	deps := make([]depKey, 0, len(node.DependsOn))
	for _, name := range node.DependsOn {
		deps = append(deps, depKey{name: name, fp: dependencyFingerprints[name]})
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].name < deps[j].name })
	for _, d := range deps {
		_, _ = fmt.Fprintf(h, "%s=%s|", d.name, d.fp)
	}

	return fmt.Sprintf("%016x", h.Sum64())
}

// buildMetadata mirrors `pub fn build_metadata`. Encodes the
// `NodeExecutionMetadata` struct into the canonical JSON blob the
// runner stores on the NodeResult.
func buildMetadata(
	fingerprint string,
	skipped bool,
	inputs []LoadedDataset,
	outputDatasetID *uuid.UUID,
	outputDatasetVersion *int32,
) json.RawMessage {
	datasets := make([]DatasetInputMetadata, 0, len(inputs))
	for _, in := range inputs {
		datasets = append(datasets, in.Metadata)
	}
	out, _ := json.Marshal(NodeExecutionMetadata{
		Fingerprint:          fingerprint,
		Skipped:              skipped,
		InputDatasets:        datasets,
		OutputDatasetID:      outputDatasetID,
		OutputDatasetVersion: outputDatasetVersion,
	})
	return out
}

// fingerprintFromMetadata mirrors `pub fn fingerprint_from_metadata`.
func fingerprintFromMetadata(metadata json.RawMessage) (string, bool) {
	if len(metadata) == 0 {
		return "", false
	}
	var m NodeExecutionMetadata
	if err := json.Unmarshal(metadata, &m); err != nil {
		return "", false
	}
	return m.Fingerprint, true
}

// outputDatasetVersionFromMetadata mirrors
// `pub fn output_dataset_version_from_metadata`.
func outputDatasetVersionFromMetadata(metadata json.RawMessage) *int32 {
	if len(metadata) == 0 {
		return nil
	}
	var m NodeExecutionMetadata
	if err := json.Unmarshal(metadata, &m); err != nil {
		return nil
	}
	return m.OutputDatasetVersion
}

// ── Transform runtime dispatch (Phase A stubs) ─────────────────────

// loadNodeInputs mirrors `runtime::load_node_inputs`. The Phase A
// version returns an empty list — the dataset-versioning client +
// storage-fetch wiring belong to a follow-up. This is enough for the
// orchestration layer to drive forward when no node depends on
// physical inputs (passthrough nodes, tests).
func loadNodeInputs(_ context.Context, _ any, _ uuid.UUID, _ *PipelineNode) ([]LoadedDataset, error) {
	return []LoadedDataset{}, nil
}

// transformRuntimeError is the canonical Phase A failure — every
// runtime helper surfaces this so callers can route around the
// missing wiring without confusing it for a real transform error.
func transformRuntimeError(kind string) error {
	return fmt.Errorf("transform_runtime_not_wired:%s", kind)
}

// executeSQLTransform — SQL/DataFusion path lands in its own phase.
func executeSQLTransform(_ context.Context, _ any, _ *PipelineNode, _ []LoadedDataset) (TransformResult, error) {
	return TransformResult{}, transformRuntimeError("sql")
}

// executePythonTransform — libs/python-sidecar dispatch lands in its
// own phase.
func executePythonTransform(_ context.Context, _ any, _ *PipelineNode, _ []LoadedDataset) (TransformResult, error) {
	return TransformResult{}, transformRuntimeError("python")
}

// executeLLMTransform — ai-service HTTP dispatch lands in its own phase.
func executeLLMTransform(_ context.Context, _ any, _ *PipelineNode, _ []LoadedDataset) (TransformResult, error) {
	return TransformResult{}, transformRuntimeError("llm")
}

// executeWASMTransform — wasmtime-go sandbox lands in its own phase.
func executeWASMTransform(_ *PipelineNode) (*uint64, json.RawMessage, error) {
	return nil, nil, transformRuntimeError("wasm")
}

// executePassthroughTransform — passthrough copy lands in its own
// phase (uses the dataset client to clone the input version).
func executePassthroughTransform(_ context.Context, _ any, _ *PipelineNode, _ []LoadedDataset) (*uint64, json.RawMessage, *int32, error) {
	return nil, nil, nil, transformRuntimeError("passthrough")
}

// executeDistributedComputeTransform — spark-on-k8s submission lands
// in its own phase (requires the Kubernetes client wired by Phase B).
func executeDistributedComputeTransform(_ context.Context, _ any, _ *PipelineNode, _ []LoadedDataset) (TransformResult, error) {
	return TransformResult{}, transformRuntimeError("distributed")
}

// executeRemoteComputeTransform — connector-management-service
// HTTP dispatch lands in its own phase.
func executeRemoteComputeTransform(_ context.Context, _ any, _ *PipelineNode, _ []LoadedDataset, _ string) (TransformResult, error) {
	return TransformResult{}, transformRuntimeError("remote")
}
