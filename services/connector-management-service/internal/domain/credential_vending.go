package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

type VendedCredentials struct {
	Entries     map[string]any
	ExpiresAtMS int64
}

func VendCredentials(connection *models.Connection, ttlSecs int64, now time.Time) VendedCredentials {
	if ttlSecs <= 0 {
		ttlSecs = 900
	}
	expiresAtMS := now.UTC().UnixMilli() + ttlSecs*1000
	entries := map[string]any{"expires-at-ms": fmt.Sprintf("%d", expiresAtMS)}

	cfg := map[string]any{}
	_ = json.Unmarshal(connection.Config, &cfg)
	switch connection.ConnectorType {
	case "s3":
		if region := stringField(cfg, "region"); region != "" {
			entries["s3.region"] = region
			entries["client.region"] = region
		}
		if endpoint := stringField(cfg, "endpoint"); endpoint != "" {
			entries["s3.endpoint"] = endpoint
		}
		if boolField(cfg, "path_style") {
			entries["s3.path-style-access"] = "true"
		}
		// Rust tries STS AssumeRole first when assume_role_arn is present, then
		// falls back to static credentials on error. The Go parity layer preserves
		// the same safe fallback until the service wires an AWS SDK client.
		if key := stringField(cfg, "access_key_id"); key != "" {
			entries["s3.access-key-id"] = key
		}
		if secret := stringField(cfg, "secret_access_key"); secret != "" {
			entries["s3.secret-access-key"] = secret
		}
		if token := stringField(cfg, "session_token"); token != "" {
			entries["s3.session-token"] = token
		}
	case "azure_blob", "adls", "onelake":
		account := stringField(cfg, "account_name")
		if account == "" {
			break
		}
		entries["adls.account-name"] = account
		if key := stringField(cfg, "account_key"); key != "" {
			perms := stringField(cfg, "sas_permissions")
			if perms == "" {
				perms = "rl"
			}
			container := stringField(cfg, "container_name")
			var sas string
			var err error
			if container != "" {
				sas, err = GenerateServiceSASContainer(account, key, container, perms, expiresAtMS)
			} else {
				sas, err = GenerateAccountSAS(account, key, perms, expiresAtMS)
			}
			if err == nil {
				entries["adls.sas-token"] = sas
				if container != "" {
					entries["adls.container"] = container
				}
				break
			}
		}
		if sas := stringField(cfg, "sas_token"); sas != "" {
			entries["adls.sas-token"] = sas
		}
	case "gcs", "google_cloud_storage":
		if token := stringField(cfg, "access_token"); token != "" {
			entries["gcs.oauth2.token"] = token
		}
		if project := stringField(cfg, "project_id"); project != "" {
			entries["gcs.project-id"] = project
		}
	}
	return VendedCredentials{Entries: entries, ExpiresAtMS: expiresAtMS}
}

func GenerateAccountSAS(account, accountKeyB64, permissions string, expiresAtMS int64) (string, error) {
	expiry := time.UnixMilli(expiresAtMS).UTC()
	if expiry.IsZero() {
		return "", fmt.Errorf("invalid expiry timestamp: %d", expiresAtMS)
	}
	version := "2022-11-02"
	services := "bf"
	resourceTypes := "co"
	protocol := "https"
	expiryText := expiry.Format("2006-01-02T15:04:05Z")
	stringToSign := fmt.Sprintf("%s\n%s\n%s\n%s\n\n%s\n\n%s\n%s\n\n\n\n\n", account, permissions, services, resourceTypes, expiryText, protocol, version)
	sig, err := sasSignature(accountKeyB64, stringToSign)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sv=%s&ss=%s&srt=%s&sp=%s&se=%s&spr=%s&sig=%s", version, services, resourceTypes, urlEncode(permissions), urlEncode(expiryText), protocol, urlEncode(sig)), nil
}

func GenerateServiceSASContainer(account, accountKeyB64, container, permissions string, expiresAtMS int64) (string, error) {
	expiry := time.UnixMilli(expiresAtMS).UTC()
	if expiry.IsZero() {
		return "", fmt.Errorf("invalid expiry timestamp: %d", expiresAtMS)
	}
	version := "2022-11-02"
	resource := "c"
	protocol := "https"
	expiryText := expiry.Format("2006-01-02T15:04:05Z")
	canonical := fmt.Sprintf("/blob/%s/%s", account, container)
	stringToSign := fmt.Sprintf("%s\n\n%s\n%s\n\n\n\n%s\n%s\n%s\n\n\n\n\n\n", permissions, expiryText, canonical, protocol, version, resource)
	sig, err := sasSignature(accountKeyB64, stringToSign)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sv=%s&sr=%s&sp=%s&se=%s&spr=%s&sig=%s", version, resource, urlEncode(permissions), urlEncode(expiryText), protocol, urlEncode(sig)), nil
}

func sasSignature(accountKeyB64, stringToSign string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(accountKeyB64)
	if err != nil {
		return "", fmt.Errorf("account_key is not base64: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func urlEncode(input string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(input))
	for i := 0; i < len(input); i++ {
		c := input[i]
		unreserved := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~'
		if unreserved {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func boolField(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}
