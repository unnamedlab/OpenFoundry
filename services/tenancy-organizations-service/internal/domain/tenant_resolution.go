package domain

import (
	"strings"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
)

// TenantResolutionContract is the JSON contract emitted by the tenancy
// resolve handler. The shape (field set, JSON keys, null vs string for
// optionals) is byte-exact with the Rust struct so cross-language
// callers parse one representation.
type TenantResolutionContract struct {
	TenantID       *uuid.UUID          `json:"tenant_id"`
	OrganizationID *uuid.UUID          `json:"organization_id"`
	ScopeID        string              `json:"scope_id"`
	Workspace      *string             `json:"workspace"`
	TenantTier     string              `json:"tenant_tier"`
	Quotas         TenantQuotaContract `json:"quotas"`
	Source         string              `json:"source"`
}

// TenantQuotaContract is the per-tenant resource budget echoed inside
// `TenantResolutionContract`. The numeric widths follow the Go
// `authmw.TenantQuotaPolicy` so the values flow with no lossy casts.
type TenantQuotaContract struct {
	MaxQueryLimit              uint32 `json:"max_query_limit"`
	MaxDistributedQueryWorkers uint32 `json:"max_distributed_query_workers"`
	MaxPipelineWorkers         uint32 `json:"max_pipeline_workers"`
	MaxRequestBodyBytes        uint64 `json:"max_request_body_bytes"`
	RequestsPerMinute          uint32 `json:"requests_per_minute"`
}

// TenantQuotaContractFromContext lifts a per-request `TenantContext`
// (the JWT-derived projection) into the wire contract.
func TenantQuotaContractFromContext(context authmw.TenantContext) TenantQuotaContract {
	return TenantQuotaContract{
		MaxQueryLimit:              context.Quotas.MaxQueryLimit,
		MaxDistributedQueryWorkers: context.Quotas.MaxDistributedQueryWorkers,
		MaxPipelineWorkers:         context.Quotas.MaxPipelineWorkers,
		MaxRequestBodyBytes:        context.Quotas.MaxRequestBodyBytes,
		RequestsPerMinute:          context.Quotas.RequestsPerMinute,
	}
}

// TenantQuotaContractFromPolicy lifts a control-plane
// `ResourceQuotaSettings` into the wire contract, dropping the storage
// / shared-spaces / guest-sessions extras that the per-request quota
// shape does not advertise.
func TenantQuotaContractFromPolicy(policy models.ResourceQuotaSettings) TenantQuotaContract {
	return TenantQuotaContract{
		MaxQueryLimit:              policy.MaxQueryLimit,
		MaxDistributedQueryWorkers: policy.MaxDistributedQueryWorkers,
		MaxPipelineWorkers:         policy.MaxPipelineWorkers,
		MaxRequestBodyBytes:        policy.MaxRequestBodyBytes,
		RequestsPerMinute:          policy.RequestsPerMinute,
	}
}

// ResolveTenantContract folds JWT claims, the org table snapshot, the
// IDP mapping table and the resource-policy table into a single
// answer. Resolution precedence matches the Rust impl exactly:
//
//   - workspace: claims-derived first, then organization default;
//   - tenant_tier: organization tier > IDP-mapping default workspace
//     hint > policy tier > claims-derived tier;
//   - quotas: policy quota when a policy applies, otherwise the
//     claims-derived quota;
//   - source: "tenancy-organizations-service" when an organization
//     row matches, "claims-fallback" otherwise.
func ResolveTenantContract(
	claims *authmw.Claims,
	organizations []models.Organization,
	identityProviderMappings []models.IdentityProviderMapping,
	resourcePolicies []models.ResourceManagementPolicy,
) TenantResolutionContract {
	context := authmw.TenantContextFromClaims(claims)

	var organization *models.Organization
	if claims.OrgID != nil {
		for i := range organizations {
			if organizations[i].ID == *claims.OrgID {
				organization = &organizations[i]
				break
			}
		}
	}

	workspace := stringPtrIfNonEmpty(context.Workspace)
	if workspace == nil && organization != nil && organization.DefaultWorkspace != nil {
		workspace = stringPtrIfNonEmpty(*organization.DefaultWorkspace)
	}

	var mappingMatch *models.IdentityProviderMapping
	if v, ok := claims.Attribute("identity_provider"); ok {
		if providerSlug, isStr := v.(string); isStr {
			for i := range identityProviderMappings {
				if identityProviderMappings[i].ProviderSlug == providerSlug {
					mappingMatch = &identityProviderMappings[i]
					break
				}
			}
		}
	}

	var policyMatch *models.ResourceManagementPolicy
	for i := range resourcePolicies {
		policy := &resourcePolicies[i]
		orgMatches := len(policy.AppliesToOrgIDs) == 0 ||
			(claims.OrgID != nil && uuidSliceContains(policy.AppliesToOrgIDs, *claims.OrgID))
		workspaceMatches := len(policy.AppliesToWorkspaces) == 0 ||
			(workspace != nil && containsCaseFold(policy.AppliesToWorkspaces, *workspace))
		if orgMatches && workspaceMatches {
			policyMatch = policy
			break
		}
	}

	tenantTier := ""
	if organization != nil && organization.TenantTier != nil && *organization.TenantTier != "" {
		tenantTier = *organization.TenantTier
	}
	if tenantTier == "" && mappingMatch != nil && mappingMatch.DefaultWorkspace != nil {
		tenantTier = context.Tier
	}
	if tenantTier == "" && policyMatch != nil {
		tenantTier = policyMatch.TenantTier
	}
	if tenantTier == "" {
		tenantTier = context.Tier
	}

	var quotas TenantQuotaContract
	if policyMatch != nil {
		quotas = TenantQuotaContractFromPolicy(policyMatch.Quota)
	} else {
		quotas = TenantQuotaContractFromContext(context)
	}

	source := "claims-fallback"
	if organization != nil {
		source = "tenancy-organizations-service"
	}

	return TenantResolutionContract{
		TenantID:       claims.OrgID,
		OrganizationID: claims.OrgID,
		ScopeID:        context.ScopeID,
		Workspace:      workspace,
		TenantTier:     tenantTier,
		Quotas:         quotas,
		Source:         source,
	}
}

// stringPtrIfNonEmpty mirrors the Rust `Option<String>` projection of
// `TenantContext.workspace`: treat empty / whitespace-only as absent.
func stringPtrIfNonEmpty(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func uuidSliceContains(haystack []uuid.UUID, needle uuid.UUID) bool {
	for _, candidate := range haystack {
		if candidate == needle {
			return true
		}
	}
	return false
}

// containsCaseFold replays Rust's `eq_ignore_ascii_case` semantics —
// no Unicode-aware folding, ASCII letters only.
func containsCaseFold(haystack []string, needle string) bool {
	for _, candidate := range haystack {
		if asciiEqualFold(candidate, needle) {
			return true
		}
	}
	return false
}

// asciiEqualFold compares two strings byte-for-byte after folding the
// 26 ASCII letters case-insensitively. Mirrors Rust's
// `str::eq_ignore_ascii_case`; non-ASCII bytes still compare strictly.
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
