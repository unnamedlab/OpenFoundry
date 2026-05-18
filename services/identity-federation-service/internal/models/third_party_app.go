package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	ThirdPartyClientTypeConfidential = "confidential"
	ThirdPartyClientTypePublic       = "public"

	ThirdPartyGrantAuthorizationCode = "authorization_code"
	ThirdPartyGrantClientCredentials = "client_credentials"

	ThirdPartyManagementDeveloperConsole = "developer_console"
	ThirdPartyManagementControlPanel     = "control_panel_fallback"
)

const (
	OAuthGrantAuthorizationCode = "authorization_code"
	OAuthGrantRefreshToken      = "refresh_token"
	OAuthGrantClientCredentials = "client_credentials"
)

const (
	ThirdPartyServiceUserGrantScopeProject  = "project"
	ThirdPartyServiceUserGrantScopeResource = "resource"

	ThirdPartyServiceUserAuditCreated             = "service_user.created"
	ThirdPartyServiceUserAuditClientGrantEnabled  = "service_user.client_credentials_enabled"
	ThirdPartyServiceUserAuditPlatformRoleGranted = "service_user.platform_role_granted"
	ThirdPartyServiceUserAuditPlatformRoleRevoked = "service_user.platform_role_revoked"
	ThirdPartyServiceUserAuditGrantCreated        = "service_user.resource_grant_created"
	ThirdPartyServiceUserAuditGrantRevoked        = "service_user.resource_grant_revoked"
	ThirdPartyServiceUserAuditSecretRotated       = "client_secret.rotated"
)

type ThirdPartyApplication struct {
	ID                          uuid.UUID                         `json:"id"`
	ClientID                    string                            `json:"client_id"`
	Name                        string                            `json:"name"`
	Description                 *string                           `json:"description,omitempty"`
	LogoURL                     *string                           `json:"logo_url,omitempty"`
	ClientType                  string                            `json:"client_type"`
	EnabledGrantTypes           []string                          `json:"enabled_grant_types"`
	RedirectURIs                []string                          `json:"redirect_uris"`
	Scopes                      []string                          `json:"scopes"`
	OwnerUserIDs                []uuid.UUID                       `json:"owner_user_ids"`
	ManagingOrganizationID      uuid.UUID                         `json:"managing_organization_id"`
	DiscoverableOrganizationIDs []uuid.UUID                       `json:"discoverable_organization_ids"`
	Enablements                 []ThirdPartyApplicationEnablement `json:"enablements"`
	ServiceUserID               *uuid.UUID                        `json:"service_user_id,omitempty"`
	ClientSecretPrefix          *string                           `json:"client_secret_prefix,omitempty"`
	ClientSecretCreatedAt       *time.Time                        `json:"client_secret_created_at,omitempty"`
	PreferredManagementSurface  string                            `json:"preferred_management_surface"`
	ControlPanelFallback        bool                              `json:"control_panel_fallback"`
	RequiresPKCE                bool                              `json:"requires_pkce"`
	CreatedBy                   *uuid.UUID                        `json:"created_by,omitempty"`
	UpdatedBy                   *uuid.UUID                        `json:"updated_by,omitempty"`
	CreatedAt                   time.Time                         `json:"created_at"`
	UpdatedAt                   time.Time                         `json:"updated_at"`
	RevokedAt                   *time.Time                        `json:"revoked_at,omitempty"`
}

type ThirdPartyApplicationEnablement struct {
	ApplicationID       uuid.UUID  `json:"application_id"`
	OrganizationID      uuid.UUID  `json:"organization_id"`
	Enabled             bool       `json:"enabled"`
	ProjectResourceIDs  []string   `json:"project_resource_ids"`
	MarkingIDs          []string   `json:"marking_ids"`
	OrganizationConsent bool       `json:"organization_consent"`
	UpdatedBy           *uuid.UUID `json:"updated_by,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type CreateThirdPartyApplicationRequest struct {
	Name                        string      `json:"name"`
	Description                 *string     `json:"description,omitempty"`
	LogoURL                     *string     `json:"logo_url,omitempty"`
	ClientType                  string      `json:"client_type"`
	EnabledGrantTypes           []string    `json:"enabled_grant_types"`
	RedirectURIs                []string    `json:"redirect_uris"`
	Scopes                      []string    `json:"scopes"`
	OwnerUserIDs                []uuid.UUID `json:"owner_user_ids"`
	ManagingOrganizationID      *uuid.UUID  `json:"managing_organization_id,omitempty"`
	DiscoverableOrganizationIDs []uuid.UUID `json:"discoverable_organization_ids"`
	EnablementOrganizationIDs   []uuid.UUID `json:"enablement_organization_ids"`
	PreferredManagementSurface  *string     `json:"preferred_management_surface,omitempty"`
	ControlPanelFallback        *bool       `json:"control_panel_fallback,omitempty"`
}

type UpdateThirdPartyApplicationRequest struct {
	Name                        *string      `json:"name,omitempty"`
	Description                 **string     `json:"description,omitempty"`
	LogoURL                     **string     `json:"logo_url,omitempty"`
	ClientType                  *string      `json:"client_type,omitempty"`
	EnabledGrantTypes           *[]string    `json:"enabled_grant_types,omitempty"`
	RedirectURIs                *[]string    `json:"redirect_uris,omitempty"`
	Scopes                      *[]string    `json:"scopes,omitempty"`
	OwnerUserIDs                *[]uuid.UUID `json:"owner_user_ids,omitempty"`
	DiscoverableOrganizationIDs *[]uuid.UUID `json:"discoverable_organization_ids,omitempty"`
	PreferredManagementSurface  *string      `json:"preferred_management_surface,omitempty"`
	ControlPanelFallback        *bool        `json:"control_panel_fallback,omitempty"`
}

type CreateThirdPartyApplicationResponse struct {
	Application  ThirdPartyApplication `json:"application"`
	ClientSecret *string               `json:"client_secret,omitempty"`
	Warning      string                `json:"warning"`
}

type RotateThirdPartyApplicationSecretResponse struct {
	Application  ThirdPartyApplication `json:"application"`
	ClientSecret string                `json:"client_secret"`
	Warning      string                `json:"warning"`
}

type UpsertThirdPartyApplicationEnablementRequest struct {
	Enabled             bool     `json:"enabled"`
	ProjectResourceIDs  []string `json:"project_resource_ids,omitempty"`
	MarkingIDs          []string `json:"marking_ids,omitempty"`
	OrganizationConsent bool     `json:"organization_consent,omitempty"`
}

type ThirdPartyAppServiceUserSeed struct {
	ID             uuid.UUID
	Email          string
	Username       string
	Name           string
	OrganizationID uuid.UUID
	Attributes     json.RawMessage
	CreatedBy      uuid.UUID
}

type ThirdPartyServiceUserGrant struct {
	ID            uuid.UUID  `json:"id"`
	ApplicationID uuid.UUID  `json:"application_id"`
	ServiceUserID uuid.UUID  `json:"service_user_id"`
	ScopeType     string     `json:"scope_type"`
	ScopeID       string     `json:"scope_id"`
	RoleKey       string     `json:"role_key"`
	GrantedBy     *uuid.UUID `json:"granted_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
}

type CreateThirdPartyServiceUserGrantRequest struct {
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
	RoleKey   string `json:"role_key"`
}

type ThirdPartyServiceUserAuditEvent struct {
	ID            uuid.UUID       `json:"id"`
	ApplicationID uuid.UUID       `json:"application_id"`
	ServiceUserID *uuid.UUID      `json:"service_user_id,omitempty"`
	ActorID       *uuid.UUID      `json:"actor_id,omitempty"`
	Action        string          `json:"action"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

type ThirdPartyServiceUserInspection struct {
	Application              ThirdPartyApplication          `json:"application"`
	ServiceUser              *User                          `json:"service_user,omitempty"`
	ClientCredentialsEnabled bool                           `json:"client_credentials_enabled"`
	PlatformRoles            []Role                         `json:"platform_roles"`
	Permissions              []string                       `json:"permissions"`
	ResourceGrants           []ThirdPartyServiceUserGrant   `json:"resource_grants"`
	AuditEvents              []ThirdPartyServiceUserAuditEvent `json:"audit_events"`
	Warning                  string                         `json:"warning"`
}

type ThirdPartyOAuthAuthorizationCode struct {
	ID                  uuid.UUID  `json:"id"`
	CodeHash            string     `json:"-"`
	ApplicationID       uuid.UUID  `json:"application_id"`
	ClientID            string     `json:"client_id"`
	UserID              uuid.UUID  `json:"user_id"`
	OrganizationID      uuid.UUID  `json:"organization_id"`
	RedirectURI         string     `json:"redirect_uri"`
	State               string     `json:"state"`
	CodeChallenge       string     `json:"-"`
	CodeChallengeMethod string     `json:"code_challenge_method"`
	RequestedScopes     []string   `json:"requested_scopes"`
	GrantedScopes       []string   `json:"granted_scopes"`
	CreatedAt           time.Time  `json:"created_at"`
	ExpiresAt           time.Time  `json:"expires_at"`
	ConsumedAt          *time.Time `json:"consumed_at,omitempty"`
	RevokedAt           *time.Time `json:"revoked_at,omitempty"`
}

type ThirdPartyOAuthRefreshToken struct {
	ID             uuid.UUID  `json:"id"`
	TokenHash      string     `json:"-"`
	FamilyID       uuid.UUID  `json:"family_id"`
	ApplicationID  uuid.UUID  `json:"application_id"`
	ClientID       string     `json:"client_id"`
	SubjectUserID  uuid.UUID  `json:"subject_user_id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	Scopes         []string   `json:"scopes"`
	IssuedAt       time.Time  `json:"issued_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	UsedAt         *time.Time `json:"used_at,omitempty"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
}

type ThirdPartyOAuthAuthorization struct {
	ID              uuid.UUID  `json:"id"`
	ApplicationID   uuid.UUID  `json:"application_id"`
	ApplicationName string     `json:"application_name"`
	ClientID        string     `json:"client_id"`
	OrganizationID  uuid.UUID  `json:"organization_id"`
	Scopes          []string   `json:"scopes"`
	IssuedAt        time.Time  `json:"issued_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
}

type OAuthAuthorizePromptResponse struct {
	Application          ThirdPartyApplication `json:"application"`
	ClientID             string                `json:"client_id"`
	RedirectURI          string                `json:"redirect_uri"`
	State                string                `json:"state"`
	OrganizationID       uuid.UUID             `json:"organization_id"`
	RequestedScopes      []string              `json:"requested_scopes"`
	GrantedScopes        []string              `json:"granted_scopes"`
	MissingScopes        []string              `json:"missing_scopes,omitempty"`
	CodeChallengeMethod  string                `json:"code_challenge_method"`
	OrganizationConsent  bool                  `json:"organization_consent"`
	RequiresUserConsent  bool                  `json:"requires_user_consent"`
	ConsentPrompt        string                `json:"consent_prompt"`
	EnablementConstraint string                `json:"enablement_constraint"`
}

type OAuthConsentRequest struct {
	ClientID            string    `json:"client_id"`
	RedirectURI         string    `json:"redirect_uri"`
	ResponseType        string    `json:"response_type,omitempty"`
	Scope               string    `json:"scope,omitempty"`
	Scopes              []string  `json:"scopes,omitempty"`
	State               string    `json:"state"`
	CodeChallenge       string    `json:"code_challenge"`
	CodeChallengeMethod string    `json:"code_challenge_method"`
	OrganizationID      uuid.UUID `json:"organization_id,omitempty"`
	Approve             bool      `json:"approve"`
}

type OAuthConsentResponse struct {
	RedirectURI      string    `json:"redirect_uri"`
	Code             string    `json:"code,omitempty"`
	State            string    `json:"state"`
	GrantedScopes    []string  `json:"granted_scopes,omitempty"`
	ExpiresAt        time.Time `json:"expires_at,omitempty"`
	Error            string    `json:"error,omitempty"`
	ErrorDescription string    `json:"error_description,omitempty"`
}

type OAuthTokenRequest struct {
	GrantType      string   `json:"grant_type"`
	Code           string   `json:"code,omitempty"`
	RedirectURI    string   `json:"redirect_uri,omitempty"`
	ClientID       string   `json:"client_id,omitempty"`
	ClientSecret   string   `json:"client_secret,omitempty"`
	CodeVerifier   string   `json:"code_verifier,omitempty"`
	RefreshToken   string   `json:"refresh_token,omitempty"`
	Scope          string   `json:"scope,omitempty"`
	Scopes         []string `json:"scopes,omitempty"`
	OrganizationID string   `json:"organization_id,omitempty"`
}

type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

type OAuthRevokeRequest struct {
	Token         string `json:"token"`
	TokenTypeHint string `json:"token_type_hint,omitempty"`
	ClientID      string `json:"client_id,omitempty"`
	ClientSecret  string `json:"client_secret,omitempty"`
}

type ListOAuthAuthorizationsResponse struct {
	Items []ThirdPartyOAuthAuthorization `json:"items"`
	Total int                            `json:"total"`
}

const ThirdPartyApplicationRegistrationWarning = "Developer Console is the preferred management surface for third-party application configuration. Control Panel registration is a fallback for deployments where Developer Console is unavailable; OAuth authorization and consent are handled by the dedicated OAuth flow."
