// Package models hosts the persistent shape used by the notebook
// runtime: notebooks, cells, sessions, notepad documents, presence,
// and the workspace file projection. Every struct uses the same
// JSON shape as the Rust origin so existing notebook frontends
// (apps/web-react/src/lib/components/notebook/...) keep round-tripping.
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Notebook is the top-level container that holds cells.
type Notebook struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	OwnerID       uuid.UUID `json:"owner_id"`
	DefaultKernel string    `json:"default_kernel"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CreateNotebookRequest struct {
	Name          string  `json:"name"`
	Description   *string `json:"description,omitempty"`
	DefaultKernel *string `json:"default_kernel,omitempty"`
}

type UpdateNotebookRequest struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	DefaultKernel *string `json:"default_kernel,omitempty"`
}

// Cell is one entry inside a Notebook (markdown or executable).
type Cell struct {
	ID             uuid.UUID       `json:"id"`
	NotebookID     uuid.UUID       `json:"notebook_id"`
	CellType       string          `json:"cell_type"`
	Kernel         string          `json:"kernel"`
	Source         string          `json:"source"`
	Position       int32           `json:"position"`
	LastOutput     json.RawMessage `json:"last_output,omitempty"`
	ExecutionCount *int32          `json:"execution_count,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type CreateCellRequest struct {
	CellType *string `json:"cell_type,omitempty"`
	Kernel   *string `json:"kernel,omitempty"`
	Source   *string `json:"source,omitempty"`
	Position *int32  `json:"position,omitempty"`
}

type UpdateCellRequest struct {
	Source   *string `json:"source,omitempty"`
	CellType *string `json:"cell_type,omitempty"`
	Kernel   *string `json:"kernel,omitempty"`
	Position *int32  `json:"position,omitempty"`
}

type ExecuteCellRequest struct {
	SessionID *uuid.UUID `json:"session_id,omitempty"`
}

// CellOutput is the response shape every kernel returns.
type CellOutput struct {
	OutputType     string          `json:"output_type"`
	Content        json.RawMessage `json:"content"`
	ExecutionCount int32           `json:"execution_count"`
}

// Session is a kernel session (one per active notebook+kernel pair).
type Session struct {
	ID           uuid.UUID `json:"id"`
	NotebookID   uuid.UUID `json:"notebook_id"`
	Kernel       string    `json:"kernel"`
	Status       string    `json:"status"`
	StartedBy    uuid.UUID `json:"started_by"`
	CreatedAt    time.Time `json:"created_at"`
	LastActivity time.Time `json:"last_activity"`
}

type CreateSessionRequest struct {
	Kernel *string `json:"kernel,omitempty"`
}

// NotepadDocument is the live-collaboration document shape.
type NotepadDocument struct {
	ID            uuid.UUID       `json:"id"`
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	OwnerID       uuid.UUID       `json:"owner_id"`
	Content       string          `json:"content"`
	TemplateKey   *string         `json:"template_key,omitempty"`
	Widgets       json.RawMessage `json:"widgets"`
	LastIndexedAt *time.Time      `json:"last_indexed_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type NotepadPresence struct {
	ID          uuid.UUID `json:"id"`
	DocumentID  uuid.UUID `json:"document_id"`
	UserID      uuid.UUID `json:"user_id"`
	SessionID   string    `json:"session_id"`
	DisplayName string    `json:"display_name"`
	CursorLabel string    `json:"cursor_label"`
	Color       string    `json:"color"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

type CreateNotepadDocumentRequest struct {
	Title       string          `json:"title"`
	Description *string         `json:"description,omitempty"`
	Content     *string         `json:"content,omitempty"`
	TemplateKey *string         `json:"template_key,omitempty"`
	Widgets     json.RawMessage `json:"widgets,omitempty"`
}

type UpdateNotepadDocumentRequest struct {
	Title         *string         `json:"title,omitempty"`
	Description   *string         `json:"description,omitempty"`
	Content       *string         `json:"content,omitempty"`
	TemplateKey   *string         `json:"template_key,omitempty"`
	Widgets       json.RawMessage `json:"widgets,omitempty"`
	LastIndexedAt *time.Time      `json:"last_indexed_at,omitempty"`
}

type UpsertNotepadPresenceRequest struct {
	SessionID   string  `json:"session_id"`
	DisplayName string  `json:"display_name"`
	CursorLabel *string `json:"cursor_label,omitempty"`
	Color       *string `json:"color,omitempty"`
}

// NotepadExportPayload mirrors the Rust struct returned by the
// notepad export endpoint.
type NotepadExportPayload struct {
	FileName       string `json:"file_name"`
	MimeType       string `json:"mime_type"`
	Title          string `json:"title"`
	HTML           string `json:"html"`
	PreviewExcerpt string `json:"preview_excerpt"`
}

// NotebookWorkspaceFile mirrors the file projection returned by the
// /workspace endpoints.
type NotebookWorkspaceFile struct {
	Path      string    `json:"path"`
	Language  string    `json:"language"`
	Content   string    `json:"content"`
	SizeBytes int64     `json:"size_bytes"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UpsertNotebookWorkspaceFileRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
