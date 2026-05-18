package auditmonitoring

import (
	"testing"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

func TestStarterPackCountsCategoryDrivenSecurityMonitors(t *testing.T) {
	t.Parallel()
	events := []models.AuditEvent{
		{Categories: []string{"dataExport"}, Status: "success"},
		{Categories: []string{"networkEgress"}, Status: "success"},
		{Categories: []string{"managementPermissions"}, Status: "success"},
		{Categories: []string{"authenticationCheck"}, Status: "denied", Outcome: "unauthorized"},
		{Categories: []string{"tokenGeneration"}, Status: "success"},
	}
	pack := StarterPack(events)
	if !pack.ExternalSIEMSupported || !pack.FoundryDatasetSupported {
		t.Fatalf("expected SIEM and dataset handoff support: %+v", pack)
	}
	if len(pack.Queries) < 8 || len(pack.Dashboards) == 0 || len(pack.Monitors) < 5 {
		t.Fatalf("starter pack incomplete: %+v", pack)
	}
	counts := map[string]int{}
	for _, monitor := range pack.Monitors {
		counts[monitor.ID] = monitor.CurrentCount
	}
	if counts["data_export"] != 2 || counts["admin_changes"] != 1 || counts["failed_access"] != 1 || counts["token_creation"] != 1 {
		t.Fatalf("monitor counts drift: %+v", counts)
	}
}
