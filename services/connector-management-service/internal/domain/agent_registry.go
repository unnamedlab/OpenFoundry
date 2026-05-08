package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

type AgentRegistryStore interface {
	GetConnectorAgent(ctx context.Context, id uuid.UUID) (*models.ConnectorAgent, error)
}

type AgentRegistryClock interface {
	Now() time.Time
}

type AgentRegistryResolver struct {
	Store      AgentRegistryStore
	Clock      AgentRegistryClock
	StaleAfter time.Duration
}

func (r AgentRegistryResolver) ResolveAgentURL(ctx context.Context, connectionConfig json.RawMessage) (*string, error) {
	var cfg map[string]any
	_ = json.Unmarshal(connectionConfig, &cfg)
	if url, ok := cfg["agent_url"].(string); ok && strings.TrimSpace(url) != "" {
		trimmed := strings.TrimSpace(url)
		return &trimmed, nil
	}
	rawID, ok := cfg["agent_id"].(string)
	if !ok || strings.TrimSpace(rawID) == "" {
		return nil, nil
	}
	agentID, err := uuid.Parse(strings.TrimSpace(rawID))
	if err != nil {
		return nil, err
	}
	agent, err := r.Store.GetConnectorAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, fmt.Errorf("connector agent '%s' not found", agentID)
	}
	if agent.Status != "online" {
		return nil, fmt.Errorf("connector agent '%s' is not online (status: %s)", agent.Name, agent.Status)
	}
	if r.StaleAfter > 0 && agent.LastHeartbeatAt != nil {
		clock := r.Clock
		if clock == nil {
			clock = realRegistryClock{}
		}
		if agent.LastHeartbeatAt.Before(clock.Now().Add(-r.StaleAfter)) {
			return nil, fmt.Errorf("connector agent '%s' heartbeat is stale", agent.Name)
		}
	}
	url := agent.AgentURL
	return &url, nil
}

type realRegistryClock struct{}

func (realRegistryClock) Now() time.Time { return time.Now().UTC() }
