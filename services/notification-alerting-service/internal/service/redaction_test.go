package service

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/notification-alerting-service/internal/models"
)

func TestRenderEmailForDeliveryStrictRedactsToPlatformLink(t *testing.T) {
	t.Parallel()
	metadata := json.RawMessage(`{"in_platform_url":"/workspace/module/view/latest/task-1"}`)
	n := &models.NotificationRecord{ID: uuid.New(), Title: "Sensitive action payload", Body: "customer secret", Metadata: metadata}

	rendered := RenderEmailForDelivery(n, "analyst@example.com", EmailRedactionConfig{Mode: "strict"})

	require.True(t, rendered.Redacted)
	require.Equal(t, "OpenFoundry notification", rendered.Subject)
	require.NotContains(t, rendered.Body, "customer secret")
	require.Contains(t, rendered.Body, "/workspace/module/view/latest/task-1")
}

func TestRenderEmailForDeliverySelectedAllowlistRequiresRiskAck(t *testing.T) {
	t.Parallel()
	n := &models.NotificationRecord{ID: uuid.New(), Title: "Full", Body: "payload", Metadata: json.RawMessage(`{}`)}
	cfg := EmailRedactionConfig{Mode: "selected_users_only", AllowlistDomains: []string{"@example.com"}}

	require.True(t, RenderEmailForDelivery(n, "user@example.com", cfg).Redacted)
	cfg.RiskAcknowledged = true
	rendered := RenderEmailForDelivery(n, "user@example.com", cfg)
	require.False(t, rendered.Redacted)
	require.Equal(t, "payload", rendered.Body)
	require.True(t, RenderEmailForDelivery(n, "user@other.test", cfg).Redacted)
}
