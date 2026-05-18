package products

import (
	"encoding/json"
	"testing"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

func TestValidateGovernanceResourcesBlocksRestrictedViewDataAndRequiresMappings(t *testing.T) {
	payload := json.RawMessage(`{
		"kind":"restricted_view_policy",
		"includes_data":true,
		"requires_dataset_mappings":["dataset-main"],
		"requires_group_mappings":["group-analysts"],
		"requires_marking_mappings":["marking-pii"],
		"requires_organization_mappings":["org-host"],
		"unsupported_features":["package_backing_dataset_rows"]
	}`)

	result := ValidateGovernanceResources([]ResourceSnapshot{{
		Type:    models.ProductResourceRestrictedView,
		Ref:     "rv-customer-scope",
		Payload: payload,
	}}, GovernanceInstallMappings{Datasets: map[string]string{"dataset-main": "target-dataset"}})

	if !containsString(result.Warnings, RestrictedViewPolicyPackagingWarning) {
		t.Fatalf("warnings=%#v", result.Warnings)
	}
	for _, want := range []string{"group:group-analysts", "marking:marking-pii", "organization:org-host"} {
		if !containsString(result.RequiredMappings, want) {
			t.Fatalf("required mappings %#v missing %q", result.RequiredMappings, want)
		}
	}
	for _, want := range []string{"restricted view rv-customer-scope cannot package backing data", "rv-customer-scope uses unsupported governance feature package_backing_dataset_rows"} {
		if !containsString(result.BlockingIssues, want) {
			t.Fatalf("blocking issues %#v missing %q", result.BlockingIssues, want)
		}
	}
}

func TestGovernanceResourceTypesUseGovernanceBundleDirectories(t *testing.T) {
	cases := map[models.ProductResourceType]string{
		models.ProductResourceRestrictedView:            "governance/restricted-views",
		models.ProductResourceProjectTemplate:           "governance/project-templates",
		models.ProductResourceApplicationAccessMetadata: "governance/application-access",
		models.ProductResourceDashboard:                 "governance/dashboards",
		models.ProductResourceGovernanceConfig:          "governance/config",
	}
	for typ, want := range cases {
		if !typ.Valid() {
			t.Fatalf("%s should be a valid product resource type", typ)
		}
		if got := ResourceDir(typ); got != want {
			t.Fatalf("ResourceDir(%s)=%s want %s", typ, got, want)
		}
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
