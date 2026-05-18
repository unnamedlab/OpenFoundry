package service

import (
	"encoding/json"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/notification-alerting-service/internal/models"
)

const redactedEmailBody = "Sensitive notification content is available in OpenFoundry. Use the in-platform link to view the full notification with your current permissions."

type EmailRedactionConfig struct {
	Mode             string
	AllowlistDomains []string
	AllowlistUsers   []string
	RiskAcknowledged bool
	PlatformBaseURL  string
}

type RenderedEmail struct {
	Subject  string
	Body     string
	Redacted bool
	Reason   string
}

func (c EmailRedactionConfig) normalizedMode() string {
	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case "disabled", "off", "none":
		return "disabled"
	case "selected_users_only", "selected-users-only", "selected":
		return "selected_users_only"
	case "group", "group_redaction":
		return "group_redaction"
	case "strict", "everyone", "":
		return "strict"
	default:
		return "strict"
	}
}

func RenderEmailForDelivery(notification *models.NotificationRecord, recipient string, cfg EmailRedactionConfig) RenderedEmail {
	if notification == nil {
		return RenderedEmail{Subject: "OpenFoundry notification", Body: redactedEmailBody, Redacted: true, Reason: "missing_notification"}
	}
	mode := cfg.normalizedMode()
	if mode == "disabled" && cfg.RiskAcknowledged {
		return RenderedEmail{Subject: notification.Title, Body: notification.Body}
	}
	if mode == "selected_users_only" && cfg.RiskAcknowledged && recipientAllowed(recipient, cfg) {
		return RenderedEmail{Subject: notification.Title, Body: notification.Body}
	}
	link := notificationLink(notification, cfg)
	body := redactedEmailBody
	if link != "" {
		body += "\n\nView in OpenFoundry: " + link
	}
	reason := mode
	if mode == "disabled" || mode == "selected_users_only" {
		reason = "risk_acknowledgement_required"
	}
	return RenderedEmail{Subject: "OpenFoundry notification", Body: body, Redacted: true, Reason: reason}
}

func recipientAllowed(recipient string, cfg EmailRedactionConfig) bool {
	recipient = strings.ToLower(strings.TrimSpace(recipient))
	if recipient == "" {
		return false
	}
	for _, user := range cfg.AllowlistUsers {
		if strings.ToLower(strings.TrimSpace(user)) == recipient {
			return true
		}
	}
	at := strings.LastIndex(recipient, "@")
	if at < 0 {
		return false
	}
	domain := recipient[at:]
	bare := strings.TrimPrefix(domain, "@")
	for _, allowed := range cfg.AllowlistDomains {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if allowed == "" {
			continue
		}
		if !strings.HasPrefix(allowed, "@") {
			allowed = "@" + allowed
		}
		allowedBare := strings.TrimPrefix(allowed, "@")
		if domain == allowed || strings.HasSuffix(bare, "."+allowedBare) {
			return true
		}
	}
	return false
}

func notificationLink(notification *models.NotificationRecord, cfg EmailRedactionConfig) string {
	var metadata map[string]any
	if len(notification.Metadata) > 0 {
		_ = json.Unmarshal(notification.Metadata, &metadata)
	}
	for _, key := range []string{"in_platform_url", "platform_url", "link", "url"} {
		if raw, ok := metadata[key].(string); ok && strings.TrimSpace(raw) != "" {
			return strings.TrimSpace(raw)
		}
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.PlatformBaseURL), "/")
	if base == "" {
		return ""
	}
	return base + "/notifications/" + notification.ID.String()
}
