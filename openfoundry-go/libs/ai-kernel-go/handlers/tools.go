package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

// ToolsHandlers wraps a pgx pool and exposes the tool-registry
// CRUD surface. Mirrors Rust libs/ai-kernel/src/handlers/tools.rs:
//   - GET    list_tools
//   - POST   create_tool   (validates name + execution_mode)
//   - PATCH  update_tool   (validates execution_mode if provided)
type ToolsHandlers struct {
	Pool *pgxpool.Pool
}

const toolColumns = `id, name, description, category, execution_mode,
                     execution_config, status, input_schema, output_schema,
                     tags, created_at, updated_at`

type toolScanner interface{ Scan(...any) error }

func scanTool(s toolScanner) (models.ToolDefinition, error) {
	var t models.ToolDefinition
	var execConfig, inputSchema, outputSchema, tagsRaw []byte
	if err := s.Scan(
		&t.ID, &t.Name, &t.Description, &t.Category, &t.ExecutionMode,
		&execConfig, &t.Status, &inputSchema, &outputSchema,
		&tagsRaw, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return t, err
	}
	t.ExecutionConfig = execConfig
	t.InputSchema = inputSchema
	t.OutputSchema = outputSchema
	if len(tagsRaw) > 0 {
		_ = json.Unmarshal(tagsRaw, &t.Tags)
	}
	if t.Tags == nil {
		t.Tags = []string{}
	}
	return t, nil
}

// ListTools handles `GET /api/v1/tools`. Matches Rust list_tools.
func (h *ToolsHandlers) ListTools(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(),
		`SELECT `+toolColumns+` FROM ai_tools
          ORDER BY updated_at DESC, created_at DESC`)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	tools := make([]models.ToolDefinition, 0)
	for rows.Next() {
		t, err := scanTool(rows)
		if err != nil {
			dbError(w, err)
			return
		}
		tools = append(tools, t)
	}
	writeJSON(w, http.StatusOK, models.ListToolsResponse{Data: tools})
}

// CreateTool handles `POST /api/v1/tools`. Validates name + execution_mode.
func (h *ToolsHandlers) CreateTool(w http.ResponseWriter, r *http.Request) {
	var body models.CreateToolRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "tool name is required")
		return
	}

	// Apply defaults from Rust serde annotations.
	description := derefString(body.Description, "")
	category := derefString(body.Category, models.DefaultToolCategory)
	executionMode := derefString(body.ExecutionMode, models.DefaultToolExecutionMode)
	status := derefString(body.Status, models.DefaultToolStatus)

	if !models.ValidateExecutionMode(executionMode) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported tool execution_mode '%s'", executionMode))
		return
	}

	executionConfig := jsonOrEmptyObject(body.ExecutionConfig)
	inputSchema := jsonOrEmptyObject(body.InputSchema)
	outputSchema := jsonOrEmptyObject(body.OutputSchema)
	tags := body.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, _ := json.Marshal(tags)

	row := h.Pool.QueryRow(r.Context(),
		`INSERT INTO ai_tools
              (id, name, description, category, execution_mode,
               execution_config, status, input_schema, output_schema, tags)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
            RETURNING `+toolColumns,
		uuid.New(), strings.TrimSpace(body.Name), description, category,
		executionMode, executionConfig, status, inputSchema, outputSchema, tagsJSON)
	t, err := scanTool(row)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// UpdateTool handles `PATCH /api/v1/tools/{id}`. Loads current row
// to fall back to its values for unspecified fields (matches Rust
// `body.X.unwrap_or(tool.X)` semantics).
func (h *ToolsHandlers) UpdateTool(w http.ResponseWriter, r *http.Request, toolID uuid.UUID) {
	current, err := h.loadTool(r.Context(), toolID)
	if errors.Is(err, pgx.ErrNoRows) || current == nil {
		writeError(w, http.StatusNotFound, "tool not found")
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}

	var body models.UpdateToolRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.ExecutionMode != nil && !models.ValidateExecutionMode(*body.ExecutionMode) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported tool execution_mode '%s'", *body.ExecutionMode))
		return
	}

	name := derefString(body.Name, current.Name)
	desc := derefString(body.Description, current.Description)
	category := derefString(body.Category, current.Category)
	executionMode := derefString(body.ExecutionMode, current.ExecutionMode)
	status := derefString(body.Status, current.Status)
	executionConfig := jsonOrFallback(body.ExecutionConfig, current.ExecutionConfig)
	inputSchema := jsonOrFallback(body.InputSchema, current.InputSchema)
	outputSchema := jsonOrFallback(body.OutputSchema, current.OutputSchema)
	tags := current.Tags
	if body.Tags != nil {
		tags = *body.Tags
	}
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, _ := json.Marshal(tags)

	row := h.Pool.QueryRow(r.Context(),
		`UPDATE ai_tools SET
            name = $2, description = $3, category = $4,
            execution_mode = $5, execution_config = $6, status = $7,
            input_schema = $8, output_schema = $9, tags = $10,
            updated_at = NOW()
          WHERE id = $1
          RETURNING `+toolColumns,
		toolID, name, desc, category, executionMode, executionConfig,
		status, inputSchema, outputSchema, tagsJSON)
	t, err := scanTool(row)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (h *ToolsHandlers) loadTool(ctx context.Context, toolID uuid.UUID) (*models.ToolDefinition, error) {
	row := h.Pool.QueryRow(ctx,
		`SELECT `+toolColumns+` FROM ai_tools WHERE id = $1`, toolID)
	t, err := scanTool(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// --- helpers shared with other handler files ----------------------------

func derefString(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}

func jsonOrEmptyObject(p *json.RawMessage) json.RawMessage {
	if p == nil || len(*p) == 0 || string(*p) == "null" {
		return json.RawMessage("{}")
	}
	return *p
}

func jsonOrFallback(p *json.RawMessage, fallback json.RawMessage) json.RawMessage {
	if p == nil {
		if len(fallback) == 0 {
			return json.RawMessage("{}")
		}
		return fallback
	}
	return *p
}
