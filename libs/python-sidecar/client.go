package pythonsidecar

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	pb "github.com/openfoundry/openfoundry-go/libs/proto-gen/runtime"
)

// InlineFunctionResult is the structured response of an inline Python
// function execution.
type InlineFunctionResult struct {
	// ResultJSON is the enriched envelope (already includes stdout /
	// stderr arrays the caller can re-emit).
	ResultJSON []byte
	Stdout     string
	Stderr     string
}

// PipelineTransformResult mirrors PythonExecutionResult on the Rust
// side. RowsAffected is only meaningful when RowsAffectedSet is true.
type PipelineTransformResult struct {
	RowsAffected    int64
	RowsAffectedSet bool
	OutputJSON      []byte
	ResultRowsJSON  []byte
	Stdout          string
	// Stderr is currently empty because ExecutePipelineTransformResponse
	// has no stderr field; it is kept in the Go model so callers can map
	// the full stdout/stderr/error surface without another API change if
	// the sidecar contract grows one.
	Stderr string
}

// NotebookCellResult mirrors KernelExecutionResult on the Rust side.
type NotebookCellResult struct {
	OutputType  string
	ContentJSON []byte
	Stdout      string
	// Stderr is reserved for sidecars/protos that expose captured stderr.
	// The current notebook proto mirrors Rust's stdout-only Python path,
	// so Manager populates this as empty while tests/fakes can exercise
	// handler propagation decisions without changing the JSON contract.
	Stderr string
}

// ExecuteInline runs an inline ontology function. The caller is
// responsible for assembling `inputJSON` with the same envelope shape
// the Rust kernel used: { context, policy, functionPackage,
// serviceToken, ontologyServiceUrl, aiServiceUrl }.
func (m *Manager) ExecuteInline(ctx context.Context, source string, inputJSON []byte, timeoutSeconds uint32) (*InlineFunctionResult, error) {
	client := m.Client()
	if client == nil {
		return nil, errors.New("python-sidecar: not started")
	}
	callCtx, cancel := context.WithTimeout(ctx, m.cfg.HardCallTimeout)
	defer cancel()
	resp, err := client.ExecuteInlineFunction(callCtx, &pb.ExecuteInlineFunctionRequest{
		Source:         source,
		InputJson:      string(inputJSON),
		TimeoutSeconds: timeoutSeconds,
	})
	if err != nil {
		return nil, fmt.Errorf("execute inline function: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("inline function error: %s", resp.Error)
	}
	return &InlineFunctionResult{
		ResultJSON: []byte(resp.ResultJson),
		Stdout:     resp.Stdout,
		Stderr:     resp.Stderr,
	}, nil
}

// ExecutePipeline runs a Python pipeline transform.
func (m *Manager) ExecutePipeline(
	ctx context.Context,
	source string,
	configJSON []byte,
	preparedInputsJSON []byte,
	inputDatasetIDs []string,
	outputDatasetID string,
	timeoutSeconds uint32,
) (*PipelineTransformResult, error) {
	client := m.Client()
	if client == nil {
		return nil, errors.New("python-sidecar: not started")
	}
	callCtx, cancel := context.WithTimeout(ctx, m.cfg.HardCallTimeout)
	defer cancel()
	resp, err := client.ExecutePipelineTransform(callCtx, &pb.ExecutePipelineTransformRequest{
		Source:             source,
		ConfigJson:         string(configJSON),
		PreparedInputsJson: string(preparedInputsJSON),
		InputDatasetIds:    inputDatasetIDs,
		OutputDatasetId:    outputDatasetID,
		TimeoutSeconds:     timeoutSeconds,
	})
	if err != nil {
		return nil, fmt.Errorf("execute pipeline transform: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("pipeline transform error: %s", resp.Error)
	}
	return &PipelineTransformResult{
		RowsAffected:    resp.RowsAffected,
		RowsAffectedSet: resp.RowsAffectedSet,
		OutputJSON:      []byte(resp.OutputJson),
		ResultRowsJSON:  []byte(resp.ResultRowsJson),
		Stdout:          resp.Stdout,
	}, nil
}

// ExecuteNotebookCell runs one notebook cell against an optional
// session. Pass uuid.Nil to run statelessly.
func (m *Manager) ExecuteNotebookCell(
	ctx context.Context,
	sessionID uuid.UUID,
	notebookID uuid.UUID,
	source string,
	workspaceDir string,
	timeoutSeconds uint32,
) (*NotebookCellResult, error) {
	client := m.Client()
	if client == nil {
		return nil, errors.New("python-sidecar: not started")
	}
	callCtx, cancel := context.WithTimeout(ctx, m.cfg.HardCallTimeout)
	defer cancel()
	req := &pb.ExecuteNotebookCellRequest{
		Source:         source,
		WorkspaceDir:   workspaceDir,
		TimeoutSeconds: timeoutSeconds,
	}
	if sessionID != uuid.Nil {
		req.SessionId = sessionID[:]
	}
	if notebookID != uuid.Nil {
		req.NotebookId = notebookID[:]
	}
	resp, err := client.ExecuteNotebookCell(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("execute notebook cell: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("notebook cell error: %s", resp.Error)
	}
	return &NotebookCellResult{
		OutputType:  resp.OutputType,
		ContentJSON: []byte(resp.ContentJson),
		Stdout:      resp.Stdout,
	}, nil
}

// EnsureSession allocates a notebook session globals dict. No-op if it
// already exists.
func (m *Manager) EnsureSession(ctx context.Context, sessionID uuid.UUID) error {
	client := m.Client()
	if client == nil {
		return errors.New("python-sidecar: not started")
	}
	_, err := client.EnsureSession(ctx, &pb.EnsureSessionRequest{SessionId: sessionID[:]})
	return err
}

// DropSession removes a notebook session.
func (m *Manager) DropSession(ctx context.Context, sessionID uuid.UUID) error {
	client := m.Client()
	if client == nil {
		return errors.New("python-sidecar: not started")
	}
	_, err := client.DropSession(ctx, &pb.DropSessionRequest{SessionId: sessionID[:]})
	return err
}
