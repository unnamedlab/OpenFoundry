package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// NetworkBoundaryPolicy: ingress/egress allow/block list policy.
type NetworkBoundaryPolicy struct {
	ID                   uuid.UUID       `json:"id"`
	Name                 string          `json:"name"`
	Direction            string          `json:"direction"`
	BoundaryKind         string          `json:"boundary_kind"`
	AllowedHosts         json.RawMessage `json:"allowed_hosts"`
	BlockedHosts         json.RawMessage `json:"blocked_hosts"`
	AllowPrivateNetworks bool            `json:"allow_private_networks"`
	AllowInsecureHTTP    bool            `json:"allow_insecure_http"`
	ProxyMode            string          `json:"proxy_mode"`
	PrivateLinkEnabled   bool            `json:"private_link_enabled"`
	UpdatedBy            string          `json:"updated_by"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

// CreateNetworkBoundaryPolicyRequest is POST /api/v1/network-boundary-policies.
type CreateNetworkBoundaryPolicyRequest struct {
	Name                 string          `json:"name"`
	Direction            string          `json:"direction"`
	BoundaryKind         string          `json:"boundary_kind"`
	AllowedHosts         json.RawMessage `json:"allowed_hosts,omitempty"`
	BlockedHosts         json.RawMessage `json:"blocked_hosts,omitempty"`
	AllowPrivateNetworks *bool           `json:"allow_private_networks,omitempty"`
	AllowInsecureHTTP    *bool           `json:"allow_insecure_http,omitempty"`
	ProxyMode            *string         `json:"proxy_mode,omitempty"`
	PrivateLinkEnabled   *bool           `json:"private_link_enabled,omitempty"`
}

// UpdateNetworkBoundaryPolicyRequest mirrors PATCH semantics.
type UpdateNetworkBoundaryPolicyRequest struct {
	AllowedHosts         json.RawMessage `json:"allowed_hosts,omitempty"`
	BlockedHosts         json.RawMessage `json:"blocked_hosts,omitempty"`
	AllowPrivateNetworks *bool           `json:"allow_private_networks,omitempty"`
	AllowInsecureHTTP    *bool           `json:"allow_insecure_http,omitempty"`
	ProxyMode            *string         `json:"proxy_mode,omitempty"`
	PrivateLinkEnabled   *bool           `json:"private_link_enabled,omitempty"`
}

// NetworkPrivateLink: pinned-host private-link target.
type NetworkPrivateLink struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	TargetHost string    `json:"target_host"`
	Transport  string    `json:"transport"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// CreateNetworkPrivateLinkRequest is POST /api/v1/network-private-links.
type CreateNetworkPrivateLinkRequest struct {
	Name       string  `json:"name"`
	TargetHost string  `json:"target_host"`
	Transport  string  `json:"transport"`
	Enabled    *bool   `json:"enabled,omitempty"`
}

// UpdateNetworkPrivateLinkRequest mirrors PATCH semantics.
type UpdateNetworkPrivateLinkRequest struct {
	TargetHost *string `json:"target_host,omitempty"`
	Transport  *string `json:"transport,omitempty"`
	Enabled    *bool   `json:"enabled,omitempty"`
}

// NetworkProxyDefinition: configured egress proxy endpoint.
type NetworkProxyDefinition struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	ProxyURL  string    `json:"proxy_url"`
	Mode      string    `json:"mode"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateNetworkProxyDefinitionRequest is POST /api/v1/network-proxy-definitions.
type CreateNetworkProxyDefinitionRequest struct {
	Name     string `json:"name"`
	ProxyURL string `json:"proxy_url"`
	Mode     string `json:"mode"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

// UpdateNetworkProxyDefinitionRequest mirrors PATCH semantics.
type UpdateNetworkProxyDefinitionRequest struct {
	ProxyURL *string `json:"proxy_url,omitempty"`
	Mode     *string `json:"mode,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
}
