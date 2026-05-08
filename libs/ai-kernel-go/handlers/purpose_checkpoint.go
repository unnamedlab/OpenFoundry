package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

func (h *AgentsHandlers) enforceAgentPurposeCheckpoint(ctx context.Context, agentID uuid.UUID, tools []models.ToolDefinition, justification *string) error {
	if h.PurposeCheckpoint == nil {
		return nil
	}
	sensitiveNames := sensitiveToolNames(tools)
	if len(sensitiveNames) == 0 {
		return nil
	}
	return h.PurposeCheckpoint.Enforce(ctx, authmw.PurposeCheckpointRequest{
		InteractionType:      "ai_agent_execution",
		ActorID:              actorIDFromContext(ctx),
		PurposeJustification: justification,
		RequiresApproval:     true,
		Tags:                 []string{"ai", "agent", "approval"},
		Evidence: mustJSONRaw(map[string]any{
			"agent_id":             agentID,
			"tool_count":           len(tools),
			"sensitive_tool_names": sensitiveNames,
		}),
	})
}

func (h *ChatHandlers) enforceChatPurposeCheckpoint(ctx context.Context, justification *string, requirePrivateNetwork bool, guardrail models.GuardrailVerdict, privacy *string) error {
	if h.PurposeCheckpoint == nil || (!requirePrivateNetwork && privacy == nil) {
		return nil
	}
	tags := []string{"ai", "chat"}
	if requirePrivateNetwork {
		tags = append(tags, "private-network")
	}
	if privacy != nil {
		tags = append(tags, "sensitive")
	}
	return h.PurposeCheckpoint.Enforce(ctx, authmw.PurposeCheckpointRequest{
		InteractionType:         "ai_chat_completion",
		ActorID:                 actorIDFromContext(ctx),
		PurposeJustification:    justification,
		RequestedPrivateNetwork: requirePrivateNetwork,
		Tags:                    tags,
		Evidence: mustJSONRaw(map[string]any{
			"privacy_reason":   privacy,
			"guardrail_status": guardrail.Status,
			"flag_count":       len(guardrail.Flags),
		}),
	})
}

func sensitiveToolNames(tools []models.ToolDefinition) []string {
	out := make([]string, 0)
	for _, tool := range tools {
		if toolRequiresApproval(tool) {
			out = append(out, tool.Name)
		}
	}
	return out
}

func toolRequiresApproval(tool models.ToolDefinition) bool {
	var cfg map[string]any
	if len(tool.ExecutionConfig) > 0 {
		_ = json.Unmarshal(tool.ExecutionConfig, &cfg)
	}
	if required, ok := cfg["requires_approval"].(bool); ok {
		return required
	}
	sensitivity, _ := cfg["sensitivity"].(string)
	switch sensitivity {
	case "high", "mutating", "admin":
		return true
	default:
		return false
	}
}

func actorIDFromContext(ctx context.Context) *uuid.UUID {
	claims, ok := authmw.FromContext(ctx)
	if !ok || claims == nil {
		return nil
	}
	id := claims.Sub
	return &id
}

func mustJSONRaw(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}

func writePurposeCheckpointError(w http.ResponseWriter, err error) {
	var denied *authmw.PurposeCheckpointDeniedError
	if errors.As(err, &denied) {
		writeError(w, http.StatusForbidden, denied.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}
