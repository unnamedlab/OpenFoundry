package governanceops

const SelfHostedResponsibilityWarning = "Self-hosted OpenFoundry operators retain responsibility for host, network, certificate, backup, patching, and monitoring controls; this checklist does not imply Palantir-managed infrastructure guarantees."

type SelfHostedChecklistItem struct {
	Category         string   `json:"category"`
	Requirement      string   `json:"requirement"`
	LogSources       []string `json:"log_sources"`
	AuditIntegration string   `json:"audit_integration"`
	Environment      string   `json:"environment"`
}
type SelfHostedChecklist struct {
	Environment string                    `json:"environment"`
	Warning     string                    `json:"warning"`
	Items       []SelfHostedChecklistItem `json:"items"`
}

func BuildSelfHostedChecklist(environment string) SelfHostedChecklist {
	if environment == "" {
		environment = "production"
	}
	base := []SelfHostedChecklistItem{
		{Category: "host", Requirement: "Harden hosts, restrict SSH/admin access, and collect OS authentication/security logs.", LogSources: []string{"auth.log", "auditd", "syslog"}, AuditIntegration: "forward host auth events to platform audit investigation index"},
		{Category: "network", Requirement: "Segment control/data planes, restrict ingress, and log firewall/load-balancer decisions.", LogSources: []string{"firewall", "load_balancer", "dns"}, AuditIntegration: "correlate network flows with user/session audit events"},
		{Category: "audit", Requirement: "Preserve append-only platform audit logs and host security logs with restricted access.", LogSources: []string{"audit.3", "outbox", "siem"}, AuditIntegration: "join host and platform events by time, actor, IP, and resource"},
		{Category: "patch", Requirement: "Track OS, Kubernetes, database, and OpenFoundry patch cadence with emergency vulnerability process.", LogSources: []string{"package_manager", "image_scanner", "vuln_scanner"}, AuditIntegration: "create findings for missed critical patch SLAs"},
		{Category: "certificate", Requirement: "Rotate TLS certificates and private keys before expiry and monitor trust-chain changes.", LogSources: []string{"cert_manager", "ingress", "kms"}, AuditIntegration: "alert on certificate expiry or unapproved issuer changes"},
		{Category: "backup", Requirement: "Encrypt, test, and monitor backups and disaster recovery restores.", LogSources: []string{"backup_controller", "object_store", "database"}, AuditIntegration: "record restore tests and backup failures as audit evidence"},
		{Category: "monitoring", Requirement: "Monitor capacity, availability, suspicious host behavior, and platform security controls.", LogSources: []string{"prometheus", "node_exporter", "edr", "audit_monitor"}, AuditIntegration: "link anomalies to security findings and incident cases"},
	}
	for i := range base {
		base[i].Environment = environment
	}
	return SelfHostedChecklist{Environment: environment, Warning: SelfHostedResponsibilityWarning, Items: base}
}
