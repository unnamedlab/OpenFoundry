package models

type AuditMonitoringQuery struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Categories  []string `json:"categories"`
	Query       string   `json:"query"`
}

type AuditMonitoringWidget struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	QueryID     string `json:"query_id"`
	Chart       string `json:"chart"`
}

type AuditMonitoringDashboard struct {
	ID          string                  `json:"id"`
	Title       string                  `json:"title"`
	Description string                  `json:"description"`
	Widgets     []AuditMonitoringWidget `json:"widgets"`
}

type AuditMonitorDefinition struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Severity          string   `json:"severity"`
	Categories        []string `json:"categories"`
	QueryID           string   `json:"query_id"`
	Schedule          string   `json:"schedule"`
	RecommendedAction string   `json:"recommended_action"`
	CurrentCount      int      `json:"current_count"`
}

type AuditMonitoringStarterPack struct {
	AccessTier              string                     `json:"access_tier"`
	RestrictedTo            []string                   `json:"restricted_to"`
	ExternalSIEMSupported   bool                       `json:"external_siem_supported"`
	FoundryDatasetSupported bool                       `json:"foundry_dataset_supported"`
	Queries                 []AuditMonitoringQuery     `json:"queries"`
	Dashboards              []AuditMonitoringDashboard `json:"dashboards"`
	Monitors                []AuditMonitorDefinition   `json:"monitors"`
	AuditExcerpt            []AuditEvent               `json:"audit_excerpt"`
}
