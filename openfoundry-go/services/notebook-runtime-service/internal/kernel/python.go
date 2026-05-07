// Package kernel wires the notebook service to the openfoundry-pyruntime
// sidecar via libs/python-sidecar. The HTTP handler layer is still
// substrate-only; this file provides the Manager-backed runtime so the
// ExecuteCell port can drop in without revisiting wiring.
package kernel

import (
	"context"

	"github.com/google/uuid"

	pythonsidecar "github.com/openfoundry/openfoundry-go/libs/python-sidecar"
)

// PythonKernel is the slice of the sidecar contract notebook handlers
// need: stateful cell execution against a session id.
type PythonKernel interface {
	EnsureSession(ctx context.Context, sessionID uuid.UUID) error
	ExecuteCell(ctx context.Context, sessionID, notebookID uuid.UUID, source, workspaceDir string, timeoutSeconds uint32) (*pythonsidecar.NotebookCellResult, error)
	DropSession(ctx context.Context, sessionID uuid.UUID) error
}

// SidecarKernel adapts *pythonsidecar.Manager to PythonKernel.
type SidecarKernel struct{ Mgr *pythonsidecar.Manager }

func (s SidecarKernel) EnsureSession(ctx context.Context, sessionID uuid.UUID) error {
	return s.Mgr.EnsureSession(ctx, sessionID)
}

func (s SidecarKernel) ExecuteCell(ctx context.Context, sessionID, notebookID uuid.UUID, source, workspaceDir string, timeoutSeconds uint32) (*pythonsidecar.NotebookCellResult, error) {
	return s.Mgr.ExecuteNotebookCell(ctx, sessionID, notebookID, source, workspaceDir, timeoutSeconds)
}

func (s SidecarKernel) DropSession(ctx context.Context, sessionID uuid.UUID) error {
	return s.Mgr.DropSession(ctx, sessionID)
}
