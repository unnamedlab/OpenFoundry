package products

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

const RestrictedViewPolicyPackagingWarning = "Restricted view product resources package policy metadata and install-time mappings only; backing rows or datasets are never included in the Marketplace bundle."

type GovernanceInstallMappings struct {
	Datasets      map[string]string `json:"datasets"`
	Groups        map[string]string `json:"groups"`
	Markings      map[string]string `json:"markings"`
	Organizations map[string]string `json:"organizations"`
}

type GovernanceBundleValidation struct {
	Warnings         []string `json:"warnings"`
	RequiredMappings []string `json:"required_mappings"`
	BlockingIssues   []string `json:"blocking_issues"`
}

type governanceResourcePayload struct {
	Kind                    string   `json:"kind"`
	IncludesData            bool     `json:"includes_data"`
	RequiresDatasetMappings []string `json:"requires_dataset_mappings"`
	RequiresGroupMappings   []string `json:"requires_group_mappings"`
	RequiresMarkingMappings []string `json:"requires_marking_mappings"`
	RequiresOrgMappings     []string `json:"requires_organization_mappings"`
	UnsupportedFeatures     []string `json:"unsupported_features"`
}

// ValidateGovernanceResources enforces the install-time contract for SG.46
// Marketplace governance packaging. Restricted-view resources are policy-only:
// product bundles may include policy JSON and mapping requirements, but never
// backing data. Installers must provide target dataset, group, marking, and org
// IDs before governance resources are materialised in a target organization.
func ValidateGovernanceResources(snapshots []ResourceSnapshot, mappings GovernanceInstallMappings) GovernanceBundleValidation {
	result := GovernanceBundleValidation{Warnings: []string{}, RequiredMappings: []string{}, BlockingIssues: []string{}}
	for _, snap := range snapshots {
		if !isGovernanceResource(snap.Type) {
			continue
		}
		payload := decodeGovernancePayload(snap.Payload)
		if snap.Type == models.ProductResourceRestrictedView {
			result.Warnings = append(result.Warnings, RestrictedViewPolicyPackagingWarning)
			if payload.IncludesData {
				result.BlockingIssues = append(result.BlockingIssues, fmt.Sprintf("restricted view %s cannot package backing data", snap.Ref))
			}
		}
		for _, feature := range payload.UnsupportedFeatures {
			feature = strings.TrimSpace(feature)
			if feature != "" {
				result.BlockingIssues = append(result.BlockingIssues, fmt.Sprintf("%s uses unsupported governance feature %s", snap.Ref, feature))
			}
		}
		result.RequiredMappings = append(result.RequiredMappings, missingMappings("dataset", payload.RequiresDatasetMappings, mappings.Datasets)...)
		result.RequiredMappings = append(result.RequiredMappings, missingMappings("group", payload.RequiresGroupMappings, mappings.Groups)...)
		result.RequiredMappings = append(result.RequiredMappings, missingMappings("marking", payload.RequiresMarkingMappings, mappings.Markings)...)
		result.RequiredMappings = append(result.RequiredMappings, missingMappings("organization", payload.RequiresOrgMappings, mappings.Organizations)...)
	}
	result.Warnings = normalizeBundleStrings(result.Warnings)
	result.RequiredMappings = normalizeBundleStrings(result.RequiredMappings)
	result.BlockingIssues = normalizeBundleStrings(result.BlockingIssues)
	return result
}

func isGovernanceResource(t models.ProductResourceType) bool {
	switch t {
	case models.ProductResourceRestrictedView, models.ProductResourceProjectTemplate,
		models.ProductResourceApplicationAccessMetadata, models.ProductResourceDashboard,
		models.ProductResourceGovernanceConfig:
		return true
	default:
		return false
	}
}

func decodeGovernancePayload(raw json.RawMessage) governanceResourcePayload {
	payload := governanceResourcePayload{}
	if len(raw) == 0 {
		return payload
	}
	_ = json.Unmarshal(raw, &payload)
	return payload
}

func missingMappings(kind string, required []string, provided map[string]string) []string {
	missing := []string{}
	for _, id := range required {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if strings.TrimSpace(provided[id]) == "" {
			missing = append(missing, kind+":"+id)
		}
	}
	return missing
}

func normalizeBundleStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}
