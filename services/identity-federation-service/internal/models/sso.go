package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SsoProvider mirrors `models::sso::SsoProvider`. Persistence-shaped
// row used by the OIDC + SAML SSO flows. Slice 5a wired the OIDC
// surface; slice 5b adds SAML configuration columns
// (`saml_metadata_url`, `saml_entity_id`, `saml_sso_url`,
// `saml_certificate`).
//
// `AttributeMapping` is the JSON column that maps SAML attribute
// names → canonical claim slots (`subject`, `email`, `name`). The
// SAML domain package reads it via `gjson`-style lookups against the
// raw bytes — empty / missing keys fall back to defaults
// (`NameID`, `email`, `name`).
type SsoProvider struct {
	ID                uuid.UUID       `json:"id"`
	Slug              string          `json:"slug"`
	Name              string          `json:"name"`
	ProviderType      string          `json:"provider_type"`
	Enabled           bool            `json:"enabled"`
	ClientID          *string         `json:"client_id,omitempty"`
	ClientSecret      *string         `json:"client_secret,omitempty"`
	IssuerURL         *string         `json:"issuer_url,omitempty"`
	AuthorizationURL  *string         `json:"authorization_url,omitempty"`
	TokenURL          *string         `json:"token_url,omitempty"`
	UserinfoURL       *string         `json:"userinfo_url,omitempty"`
	Scopes            []string        `json:"scopes"`
	SamlMetadataURL   *string         `json:"saml_metadata_url,omitempty"`
	SamlEntityID      *string         `json:"saml_entity_id,omitempty"`
	SamlSsoURL        *string         `json:"saml_sso_url,omitempty"`
	SamlCertificate   *string         `json:"saml_certificate,omitempty"`
	AttributeMapping  json.RawMessage `json:"attribute_mapping,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// SsoProviderResponse mirrors `models::sso::SsoProviderResponse` — the
// public-safe view that drops `client_secret` and replaces it with
// `client_secret_configured`.
type SsoProviderResponse struct {
	ID                       uuid.UUID       `json:"id"`
	Slug                     string          `json:"slug"`
	Name                     string          `json:"name"`
	ProviderType             string          `json:"provider_type"`
	Enabled                  bool            `json:"enabled"`
	ClientID                 *string         `json:"client_id,omitempty"`
	ClientSecretConfigured   bool            `json:"client_secret_configured"`
	IssuerURL                *string         `json:"issuer_url,omitempty"`
	AuthorizationURL         *string         `json:"authorization_url,omitempty"`
	TokenURL                 *string         `json:"token_url,omitempty"`
	UserinfoURL              *string         `json:"userinfo_url,omitempty"`
	Scopes                   []string        `json:"scopes"`
	SamlMetadataURL          *string         `json:"saml_metadata_url,omitempty"`
	SamlEntityID             *string         `json:"saml_entity_id,omitempty"`
	SamlSsoURL               *string         `json:"saml_sso_url,omitempty"`
	AttributeMapping         json.RawMessage `json:"attribute_mapping,omitempty"`
	CreatedAt                time.Time       `json:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
}

// IntoResponse mirrors `SsoProvider::into_response` — strips the
// secret while preserving every other field.
func (p *SsoProvider) IntoResponse() SsoProviderResponse {
	return SsoProviderResponse{
		ID:                     p.ID,
		Slug:                   p.Slug,
		Name:                   p.Name,
		ProviderType:           p.ProviderType,
		Enabled:                p.Enabled,
		ClientID:               p.ClientID,
		ClientSecretConfigured: p.ClientSecret != nil && *p.ClientSecret != "",
		IssuerURL:              p.IssuerURL,
		AuthorizationURL:       p.AuthorizationURL,
		TokenURL:               p.TokenURL,
		UserinfoURL:            p.UserinfoURL,
		Scopes:                 p.Scopes,
		SamlMetadataURL:        p.SamlMetadataURL,
		SamlEntityID:           p.SamlEntityID,
		SamlSsoURL:             p.SamlSsoURL,
		AttributeMapping:       p.AttributeMapping,
		CreatedAt:              p.CreatedAt,
		UpdatedAt:              p.UpdatedAt,
	}
}
