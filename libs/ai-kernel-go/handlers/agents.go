package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/agents"
	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/llm"
	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/rag"
	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// AgentsHandlers exposes the agent-catalog CRUD surface mirroring
// libs/ai-kernel/src/handlers/agents.rs:
//   - GET   list_agents
//   - POST  create_agent
//   - PATCH update_agent
//   - POST  execute_agent  (planner/executor backed, with sensitive
//     approval enforced by per-tool policy checks;
//     see ExecuteAgent for purpose-checkpoint notes)
type AgentsHandlers struct {
	Pool              *pgxpool.Pool
	PurposeCheckpoint *authmw.PurposeCheckpointClient
}

const agentColumns = `id, name, description, status, system_prompt,
                      objective, tool_ids, planning_strategy,
                      max_iterations, memory, last_execution_at,
                      created_at, updated_at`

func scanAgent(s toolScanner) (models.AgentDefinition, error) {
	var a models.AgentDefinition
	var description, systemPrompt, objective, toolIDsRaw, memoryRaw []byte
	var lastExec *time.Time
	if err := s.Scan(
		&a.ID, &a.Name, &description, &a.Status, &systemPrompt,
		&objective, &toolIDsRaw, &a.PlanningStrategy, &a.MaxIterations,
		&memoryRaw, &lastExec, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return a, err
	}
	a.Description = string(description)
	a.SystemPrompt = string(systemPrompt)
	a.Objective = string(objective)
	if len(toolIDsRaw) > 0 {
		_ = json.Unmarshal(toolIDsRaw, &a.ToolIDs)
	}
	if a.ToolIDs == nil {
		a.ToolIDs = []uuid.UUID{}
	}
	if len(memoryRaw) > 0 {
		_ = json.Unmarshal(memoryRaw, &a.Memory)
	}
	a.LastExecutionAt = lastExec
	return a, nil
}

func (h *AgentsHandlers) loadAgent(ctx context.Context, id uuid.UUID) (*models.AgentDefinition, error) {
	row := h.Pool.QueryRow(ctx,
		`SELECT `+agentColumns+` FROM ai_agents WHERE id = $1`, id)
	a, err := scanAgent(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListAgents handles `GET /api/v1/agents`.
func (h *AgentsHandlers) ListAgents(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(),
		`SELECT `+agentColumns+` FROM ai_agents
          ORDER BY updated_at DESC, created_at DESC`)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	out := make([]models.AgentDefinition, 0)
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			dbError(w, err)
			return
		}
		out = append(out, a)
	}
	writeJSON(w, http.StatusOK, models.ListAgentsResponse{Data: out})
}

// CreateAgent handles `POST /api/v1/agents`.
func (h *AgentsHandlers) CreateAgent(w http.ResponseWriter, r *http.Request) {
	var body models.CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "agent name is required")
		return
	}

	description := derefString(body.Description, "")
	status := derefString(body.Status, models.DefaultAgentStatus)
	systemPrompt := derefString(body.SystemPrompt, "")
	objective := derefString(body.Objective, "")
	planningStrategy := derefString(body.PlanningStrategy, models.DefaultAgentPlanningStrategy)
	maxIterations := models.DefaultAgentMaxIterations
	if body.MaxIterations != nil {
		maxIterations = *body.MaxIterations
	}
	toolIDs := body.ToolIDs
	if toolIDs == nil {
		toolIDs = []uuid.UUID{}
	}
	toolIDsJSON, _ := json.Marshal(toolIDs)
	memoryJSON, _ := json.Marshal(models.AgentMemorySnapshot{})

	row := h.Pool.QueryRow(r.Context(),
		`INSERT INTO ai_agents
              (id, name, description, status, system_prompt, objective,
               tool_ids, planning_strategy, max_iterations, memory,
               last_execution_at)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULL)
            RETURNING `+agentColumns,
		uuid.New(), strings.TrimSpace(body.Name), description, status,
		systemPrompt, objective, toolIDsJSON, planningStrategy,
		maxIterations, memoryJSON)
	a, err := scanAgent(row)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// UpdateAgent handles `PATCH /api/v1/agents/{id}`.
func (h *AgentsHandlers) UpdateAgent(w http.ResponseWriter, r *http.Request, agentID uuid.UUID) {
	current, err := h.loadAgent(r.Context(), agentID)
	if err != nil {
		dbError(w, err)
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	var body models.UpdateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	name := derefString(body.Name, current.Name)
	desc := derefString(body.Description, current.Description)
	status := derefString(body.Status, current.Status)
	systemPrompt := derefString(body.SystemPrompt, current.SystemPrompt)
	objective := derefString(body.Objective, current.Objective)
	planningStrategy := derefString(body.PlanningStrategy, current.PlanningStrategy)
	maxIterations := current.MaxIterations
	if body.MaxIterations != nil {
		maxIterations = *body.MaxIterations
	}
	toolIDs := current.ToolIDs
	if body.ToolIDs != nil {
		toolIDs = *body.ToolIDs
	}
	if toolIDs == nil {
		toolIDs = []uuid.UUID{}
	}
	memory := current.Memory
	if body.Memory != nil {
		memory = *body.Memory
	}
	toolIDsJSON, _ := json.Marshal(toolIDs)
	memoryJSON, _ := json.Marshal(memory)

	row := h.Pool.QueryRow(r.Context(),
		`UPDATE ai_agents SET
            name = $2, description = $3, status = $4,
            system_prompt = $5, objective = $6, tool_ids = $7,
            planning_strategy = $8, max_iterations = $9, memory = $10,
            updated_at = NOW()
          WHERE id = $1
          RETURNING `+agentColumns,
		agentID, name, desc, status, systemPrompt, objective,
		toolIDsJSON, planningStrategy, maxIterations, memoryJSON)
	a, err := scanAgent(row)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// ExecuteAgent handles `POST /api/v1/agents/{id}/execute`. Mirrors
// fn execute_agent verbatim:
//
//  1. validate user_message + load agent + load tools.
//  2. retrieve knowledge hits when knowledge_base_id is set
//     (rag.Search over the KB's documents).
//  3. resolve objective (body.objective → agent.objective → user_message).
//  4. build the plan via agents.BuildPlan.
//  5. execute the plan via agents.ExecutePlan (covers all 11 tool
//     execution_modes including HTTP).
//  6. select an LlmProvider via the gateway, run llm.CompleteText
//     with the plan summary; on no-provider/no-runtime, fall back to
//     the last trace's observation.
//  7. update agent memory via agents.UpdateMemory and persist the
//     bumped memory + last_execution_at column.
//
// Sensitive tool execution now mirrors Rust enforce_purpose_checkpoint:
// when any tool requires approval, the configured purpose-checkpoint
// client must approve the request before planning or execution continues.
func (h *AgentsHandlers) ExecuteAgent(w http.ResponseWriter, r *http.Request, agentID uuid.UUID) {
	var body models.ExecuteAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.UserMessage) == "" {
		writeError(w, http.StatusBadRequest, "agent execution requires a user message")
		return
	}

	current, err := h.loadAgent(r.Context(), agentID)
	if err != nil {
		dbError(w, err)
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	tools, err := h.loadToolsForAgent(r.Context(), current.ToolIDs)
	if err != nil {
		dbError(w, err)
		return
	}
	if err := h.enforceAgentPurposeCheckpoint(r.Context(), current.ID, tools, body.PurposeJustification); err != nil {
		writePurposeCheckpointError(w, err)
		return
	}

	knowledgeHits := []models.KnowledgeSearchResult{}
	if body.KnowledgeBaseID != nil {
		docs, err := h.loadKnowledgeBaseDocuments(r.Context(), *body.KnowledgeBaseID)
		if err != nil {
			dbError(w, err)
			return
		}
		knowledgeHits = rag.Search(body.UserMessage, docs, 4, 0.55)
	}

	objective := derefString(body.Objective, "")
	if objective == "" {
		if strings.TrimSpace(current.Objective) == "" {
			objective = body.UserMessage
		} else {
			objective = current.Objective
		}
	}

	steps := agents.BuildPlan(*current, objective, tools, knowledgeHits)
	traces := agents.ExecutePlan(r.Context(), nil, steps, tools, body.UserMessage, objective, body.Context, r.Header, knowledgeHits)

	finalResponse, err := h.synthesiseFinalResponse(r.Context(), current, body.UserMessage, objective, traces, knowledgeHits)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	updatedMemory := agents.UpdateMemory(current.Memory, body.UserMessage, finalResponse, knowledgeHits)
	memoryJSON, _ := json.Marshal(updatedMemory)
	if _, err := h.Pool.Exec(r.Context(),
		`UPDATE ai_agents SET memory = $2, last_execution_at = NOW(), updated_at = NOW() WHERE id = $1`,
		agentID, memoryJSON); err != nil {
		dbError(w, err)
		return
	}

	usedToolNames := make([]string, 0)
	for _, trace := range traces {
		if trace.ToolName != nil {
			usedToolNames = append(usedToolNames, *trace.ToolName)
		}
	}

	writeJSON(w, http.StatusOK, models.AgentExecutionResponse{
		AgentID:       agentID,
		Steps:         steps,
		Traces:        traces,
		FinalResponse: finalResponse,
		UsedToolNames: usedToolNames,
		ExecutedAt:    time.Now().UTC(),
	})
}

func (h *AgentsHandlers) loadToolsForAgent(ctx context.Context, toolIDs []uuid.UUID) ([]models.ToolDefinition, error) {
	tools := make([]models.ToolDefinition, 0, len(toolIDs))
	for _, id := range toolIDs {
		row := h.Pool.QueryRow(ctx,
			`SELECT id, name, description, category, execution_mode,
                    execution_config, status, input_schema, output_schema,
                    tags, created_at, updated_at
              FROM ai_tools WHERE id = $1`, id)
		t, err := scanTool(row)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		tools = append(tools, t)
	}
	return tools, nil
}

func (h *AgentsHandlers) loadKnowledgeBaseDocuments(ctx context.Context, kbID uuid.UUID) ([]models.KnowledgeDocument, error) {
	rows, err := h.Pool.Query(ctx,
		`SELECT id, knowledge_base_id, title, content, source_uri,
                metadata, status, chunk_count, chunks, created_at, updated_at
          FROM ai_knowledge_documents
          WHERE knowledge_base_id = $1
          ORDER BY updated_at DESC`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.KnowledgeDocument, 0)
	for rows.Next() {
		d, err := scanKnowledgeDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// synthesiseFinalResponse mirrors the Rust final-completion section
// of execute_agent. Walks the loaded providers, picks one via the
// gateway routing/select pair, calls llm.CompleteText with the
// system prompt + summary of tool observations + knowledge hits, and
// returns the generated text. When no providers are configured or
// the runtime returns an error, falls back to the last trace's
// observation (matching Rust's "Agent execution completed without
// traces." fallback when no traces exist).
func (h *AgentsHandlers) synthesiseFinalResponse(
	ctx context.Context,
	agent *models.AgentDefinition,
	userMessage, objective string,
	traces []models.AgentExecutionTrace,
	knowledgeHits []models.KnowledgeSearchResult,
) (string, error) {
	providers, err := h.loadAllProviders(ctx)
	if err != nil {
		return "", err
	}
	routed := llm.RouteProviders(providers, nil, "agents", []string{"text"}, false, false)
	provider := llm.SelectProvider(routed, true)

	if provider == nil {
		return fallbackResponse(traces), nil
	}

	knowledgeSummary := strings.Builder{}
	for _, hit := range knowledgeHits {
		if knowledgeSummary.Len() > 0 {
			knowledgeSummary.WriteString("\n")
		}
		knowledgeSummary.WriteString("- ")
		knowledgeSummary.WriteString(hit.DocumentTitle)
		knowledgeSummary.WriteString(": ")
		knowledgeSummary.WriteString(hit.Excerpt)
	}
	toolSummary := strings.Builder{}
	for _, trace := range traces {
		if trace.ToolName == nil {
			continue
		}
		if toolSummary.Len() > 0 {
			toolSummary.WriteString("\n")
		}
		toolSummary.WriteString("- ")
		toolSummary.WriteString(trace.Title)
		toolSummary.WriteString(" => ")
		toolSummary.WriteString(string(trace.Output))
	}
	knowledgeText := "none"
	if knowledgeSummary.Len() > 0 {
		knowledgeText = knowledgeSummary.String()
	}
	toolText := "none"
	if toolSummary.Len() > 0 {
		toolText = toolSummary.String()
	}

	systemPrompt := agent.SystemPrompt
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = "You are an OpenFoundry execution agent. Summarize tool results clearly."
	}
	userPrompt := "Objective: " + objective + "\nUser message: " + userMessage +
		"\nKnowledge hits:\n" + knowledgeText +
		"\nTool observations:\n" + toolText +
		"\nRespond with a concise operator-facing answer."

	maxTokens := provider.MaxOutputTokens
	if maxTokens > 512 {
		maxTokens = 512
	}
	completion, err := llm.CompleteText(ctx, nil, provider, systemPrompt, userPrompt, nil, 0.2, maxTokens)
	if err != nil {
		return "", err
	}
	return completion.Text, nil
}

func (h *AgentsHandlers) loadAllProviders(ctx context.Context) ([]models.LlmProvider, error) {
	rows, err := h.Pool.Query(ctx,
		`SELECT id, name, provider_type, model_name, endpoint_url,
                api_mode, credential_reference, enabled,
                load_balance_weight, max_output_tokens, cost_tier,
                tags, route_rules, health_state, created_at, updated_at
          FROM ai_providers
          ORDER BY updated_at DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.LlmProvider, 0)
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func fallbackResponse(traces []models.AgentExecutionTrace) string {
	if len(traces) == 0 {
		return "Agent execution completed without traces."
	}
	return traces[len(traces)-1].Observation
}
