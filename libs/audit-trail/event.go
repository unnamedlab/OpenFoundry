package audittrail

import "time"

// EventKind is the wire token in the `kind` field of every audit
// event. Stable so SIEM filters key off it; never rename.
type EventKind string

const (
	KindMediaSetCreated              EventKind = "media_set.created"
	KindMediaSetDeleted              EventKind = "media_set.deleted"
	KindMediaSetMarkingsChanged      EventKind = "media_set.markings_changed"
	KindMediaSetRetentionChanged     EventKind = "media_set.retention_changed"
	KindMediaSetTransactionOpened    EventKind = "media_set.transaction_opened"
	KindMediaSetTransactionCommitted EventKind = "media_set.transaction_committed"
	KindMediaSetTransactionAborted   EventKind = "media_set.transaction_aborted"
	KindMediaSetAccessPatternInvoked EventKind = "media_set.access_pattern_invoked"
	KindMediaItemUploaded            EventKind = "media_item.uploaded"
	KindMediaItemDownloaded          EventKind = "media_item.downloaded"
	KindMediaItemDeleted             EventKind = "media_item.deleted"
	KindMediaItemMarkingOverridden   EventKind = "media_item.marking_overridden"
	KindVirtualMediaItemRegistered   EventKind = "virtual_media_item.registered"
	KindCompassResourcePurged        EventKind = "compass.resource.purged"
	KindCompassViewReqPropagated     EventKind = "compass.view_requirements.propagated"

	// Identity-federation variants (T8 compliance closure).
	KindAuthLogin      EventKind = "auth.login"
	KindIdentityLinked EventKind = "auth.identity_linked"
	KindTokenIssued    EventKind = "auth.token_issued"
)

// CategoriesFor returns the audit categories assigned to `kind`. Media
// event mappings mirror Rust; Compass extensions use the same category
// taxonomy.
func CategoriesFor(kind EventKind) []AuditCategory {
	switch kind {
	case KindMediaSetCreated:
		return []AuditCategory{CategoryDataCreate}
	case KindMediaSetDeleted, KindMediaItemDeleted, KindCompassResourcePurged:
		return []AuditCategory{CategoryDataDelete}
	case KindMediaSetMarkingsChanged, KindMediaItemMarkingOverridden, KindCompassViewReqPropagated:
		return []AuditCategory{CategoryManagementMarkings}
	case KindMediaSetRetentionChanged,
		KindMediaSetTransactionOpened,
		KindMediaSetTransactionCommitted,
		KindMediaSetTransactionAborted:
		return []AuditCategory{CategoryDataUpdate}
	case KindMediaSetAccessPatternInvoked:
		return []AuditCategory{CategoryDataLoad}
	case KindMediaItemUploaded:
		return []AuditCategory{CategoryDataImport}
	case KindMediaItemDownloaded:
		return []AuditCategory{CategoryDataExport}
	case KindVirtualMediaItemRegistered:
		return []AuditCategory{CategoryDataCreate}
	case KindAuthLogin, KindIdentityLinked, KindTokenIssued:
		return []AuditCategory{CategoryAuthentication}
	default:
		return nil
	}
}

// AuditEvent is the discriminated union of every recordable mutation.
//
// One struct with all variant fields is the closest Go gets to
// Rust's sealed enum without resorting to interface boxing for every
// emit call. The trade-off is mild type laxity in code; the
// invariant — only the fields relevant to Kind are populated — is
// enforced by the per-variant constructors below
// (NewMediaSetCreated, NewMediaItemUploaded, …).
//
// JSON wire format keeps the Rust shape for media events: `kind` is the
// discriminator (e.g. "media_set.created"), payload-specific fields
// land at the same level, and unset fields are omitted entirely.
type AuditEvent struct {
	Kind            EventKind `json:"kind"`
	ResourceRID     string    `json:"resource_rid"`
	ProjectRID      string    `json:"project_rid"`
	MarkingsAtEvent []string  `json:"markings_at_event"`

	// MediaSetCreated.
	Name              string `json:"name,omitempty"`
	Schema            string `json:"schema,omitempty"`
	TransactionPolicy string `json:"transaction_policy,omitempty"`
	Virtual           *bool  `json:"virtual,omitempty"`

	// Markings + retention changes.
	PreviousMarkings         []string `json:"previous_markings,omitempty"`
	PreviousRetentionSeconds *int64   `json:"previous_retention_seconds,omitempty"`
	NewRetentionSeconds      *int64   `json:"new_retention_seconds,omitempty"`

	// Transaction events.
	TransactionRID string `json:"transaction_rid,omitempty"`
	Branch         string `json:"branch,omitempty"`

	// Access patterns.
	AccessPattern string `json:"access_pattern,omitempty"`
	Persistence   string `json:"persistence,omitempty"`

	// Media item.
	MediaSetRID string  `json:"media_set_rid,omitempty"`
	Path        string  `json:"path,omitempty"`
	MimeType    string  `json:"mime_type,omitempty"`
	SizeBytes   *int64  `json:"size_bytes,omitempty"`
	SHA256      string  `json:"sha256,omitempty"`
	TTLSeconds  *uint64 `json:"ttl_seconds,omitempty"`

	// Virtual media item.
	PhysicalPath string `json:"physical_path,omitempty"`
	ItemPath     string `json:"item_path,omitempty"`

	// Compass resource purge.
	ResourceType           string              `json:"resource_type,omitempty"`
	DisplayName            string              `json:"display_name,omitempty"`
	DeletedAt              string              `json:"deleted_at,omitempty"`
	DeletedBy              string              `json:"deleted_by,omitempty"`
	PurgedBy               string              `json:"purged_by,omitempty"`
	RetentionDays          *int                `json:"retention_days,omitempty"`
	PurgeAfter             string              `json:"purge_after,omitempty"`
	PurgeMode              string              `json:"purge_mode,omitempty"`
	AffectedDependents     []AffectedDependent `json:"affected_dependents,omitempty"`
	DependentListTruncated *bool               `json:"dependent_list_truncated,omitempty"`

	// Compass view-requirement propagation.
	PropagationJobID   string `json:"propagation_job_id,omitempty"`
	ParentResourceRID  string `json:"parent_resource_rid,omitempty"`
	ParentResourceKind string `json:"parent_resource_kind,omitempty"`
	TotalFolders       *int   `json:"total_folders,omitempty"`
	ChangedFolders     *int   `json:"changed_folders,omitempty"`
	TotalResources     *int   `json:"total_resources,omitempty"`
	ChangedResources   *int   `json:"changed_resources,omitempty"`

	// Auth variants (auth.login / auth.identity_linked / auth.token_issued).
	// Each field is omitempty so the wire shape only carries the slots the
	// variant actually populates. The compliance/audit-sink consumer keys
	// off `kind` to know which slots to read.
	UserID       string    `json:"user_id,omitempty"`
	TenantID     string    `json:"tenant_id,omitempty"`
	Provider     string    `json:"provider,omitempty"`
	Subject      string    `json:"subject,omitempty"`
	LoginEmail   string    `json:"login_email,omitempty"`
	MFASatisfied *bool     `json:"mfa_satisfied,omitempty"`
	AuthMethods  []string  `json:"auth_methods,omitempty"`
	TokenID      string    `json:"token_id,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
}

// Categories is shorthand for CategoriesFor(e.Kind).
func (e *AuditEvent) Categories() []AuditCategory { return CategoriesFor(e.Kind) }

// ─── Variant constructors ───────────────────────────────────────────────

func boolPtr(b bool) *bool    { return &b }
func i64Ptr(v int64) *int64   { return &v }
func intPtr(v int) *int       { return &v }
func u64Ptr(v uint64) *uint64 { return &v }

// AffectedDependent identifies a row/resource whose state changes because
// another Compass resource is permanently purged.
type AffectedDependent struct {
	Kind         string `json:"kind"`
	RID          string `json:"rid,omitempty"`
	ID           string `json:"id,omitempty"`
	Relationship string `json:"relationship,omitempty"`
	Action       string `json:"action,omitempty"`
}

// NewMediaSetCreated builds an event for a freshly created media set.
func NewMediaSetCreated(rid, projectRID string, markings []string, name, schema, txPolicy string, virtual bool) AuditEvent {
	return AuditEvent{
		Kind:              KindMediaSetCreated,
		ResourceRID:       rid,
		ProjectRID:        projectRID,
		MarkingsAtEvent:   markings,
		Name:              name,
		Schema:            schema,
		TransactionPolicy: txPolicy,
		Virtual:           boolPtr(virtual),
	}
}

// NewMediaSetDeleted builds an event for a media-set deletion.
func NewMediaSetDeleted(rid, projectRID string, markings []string) AuditEvent {
	return AuditEvent{
		Kind:            KindMediaSetDeleted,
		ResourceRID:     rid,
		ProjectRID:      projectRID,
		MarkingsAtEvent: markings,
	}
}

// NewMediaSetMarkingsChanged builds the markings-changed audit event.
func NewMediaSetMarkingsChanged(rid, projectRID string, markings, previous []string) AuditEvent {
	return AuditEvent{
		Kind:             KindMediaSetMarkingsChanged,
		ResourceRID:      rid,
		ProjectRID:       projectRID,
		MarkingsAtEvent:  markings,
		PreviousMarkings: previous,
	}
}

// NewMediaSetRetentionChanged builds the retention-change audit event.
func NewMediaSetRetentionChanged(rid, projectRID string, markings []string, previousSecs, newSecs int64) AuditEvent {
	return AuditEvent{
		Kind:                     KindMediaSetRetentionChanged,
		ResourceRID:              rid,
		ProjectRID:               projectRID,
		MarkingsAtEvent:          markings,
		PreviousRetentionSeconds: i64Ptr(previousSecs),
		NewRetentionSeconds:      i64Ptr(newSecs),
	}
}

// NewMediaSetTransactionOpened/Committed/Aborted build the three
// dataset-transaction audit events.
func NewMediaSetTransactionOpened(rid, projectRID string, markings []string, txRID, branch string) AuditEvent {
	return AuditEvent{Kind: KindMediaSetTransactionOpened, ResourceRID: rid, ProjectRID: projectRID, MarkingsAtEvent: markings, TransactionRID: txRID, Branch: branch}
}
func NewMediaSetTransactionCommitted(rid, projectRID string, markings []string, txRID, branch string) AuditEvent {
	return AuditEvent{Kind: KindMediaSetTransactionCommitted, ResourceRID: rid, ProjectRID: projectRID, MarkingsAtEvent: markings, TransactionRID: txRID, Branch: branch}
}
func NewMediaSetTransactionAborted(rid, projectRID string, markings []string, txRID, branch string) AuditEvent {
	return AuditEvent{Kind: KindMediaSetTransactionAborted, ResourceRID: rid, ProjectRID: projectRID, MarkingsAtEvent: markings, TransactionRID: txRID, Branch: branch}
}

// NewMediaSetAccessPatternInvoked records server-side materialisation
// (image transform, OCR, transcription, …).
func NewMediaSetAccessPatternInvoked(rid, projectRID string, markings []string, pattern, persistence string) AuditEvent {
	return AuditEvent{
		Kind:            KindMediaSetAccessPatternInvoked,
		ResourceRID:     rid,
		ProjectRID:      projectRID,
		MarkingsAtEvent: markings,
		AccessPattern:   pattern,
		Persistence:     persistence,
	}
}

// NewMediaItemUploaded records a fresh media-item upload.
//
// `transactionRID` is optional — pass "" outside transactional contexts.
func NewMediaItemUploaded(itemRID, mediaSetRID, projectRID string, markings []string,
	path, mime string, size int64, sha256, transactionRID string) AuditEvent {
	return AuditEvent{
		Kind:            KindMediaItemUploaded,
		ResourceRID:     itemRID,
		MediaSetRID:     mediaSetRID,
		ProjectRID:      projectRID,
		MarkingsAtEvent: markings,
		Path:            path,
		MimeType:        mime,
		SizeBytes:       i64Ptr(size),
		SHA256:          sha256,
		TransactionRID:  transactionRID,
	}
}

// NewMediaItemDownloaded records a media-item download.
func NewMediaItemDownloaded(itemRID, mediaSetRID, projectRID string, markings []string, size int64, ttl uint64) AuditEvent {
	return AuditEvent{
		Kind:            KindMediaItemDownloaded,
		ResourceRID:     itemRID,
		MediaSetRID:     mediaSetRID,
		ProjectRID:      projectRID,
		MarkingsAtEvent: markings,
		SizeBytes:       i64Ptr(size),
		TTLSeconds:      u64Ptr(ttl),
	}
}

// NewMediaItemDeleted records a media-item deletion.
func NewMediaItemDeleted(itemRID, mediaSetRID, projectRID string, markings []string, size int64) AuditEvent {
	return AuditEvent{
		Kind:            KindMediaItemDeleted,
		ResourceRID:     itemRID,
		MediaSetRID:     mediaSetRID,
		ProjectRID:      projectRID,
		MarkingsAtEvent: markings,
		SizeBytes:       i64Ptr(size),
	}
}

// NewMediaItemMarkingOverridden records a per-item marking override.
func NewMediaItemMarkingOverridden(itemRID, mediaSetRID, projectRID string, markings, previous []string) AuditEvent {
	return AuditEvent{
		Kind:             KindMediaItemMarkingOverridden,
		ResourceRID:      itemRID,
		MediaSetRID:      mediaSetRID,
		ProjectRID:       projectRID,
		MarkingsAtEvent:  markings,
		PreviousMarkings: previous,
	}
}

// NewVirtualMediaItemRegistered records the registration of a virtual
// (pointer-only) media item in a media set.
func NewVirtualMediaItemRegistered(itemRID, mediaSetRID, projectRID string, markings []string, physicalPath, itemPath string) AuditEvent {
	return AuditEvent{
		Kind:            KindVirtualMediaItemRegistered,
		ResourceRID:     itemRID,
		MediaSetRID:     mediaSetRID,
		ProjectRID:      projectRID,
		MarkingsAtEvent: markings,
		PhysicalPath:    physicalPath,
		ItemPath:        itemPath,
	}
}

// NewCompassResourcePurged records a permanent Compass resource delete.
func NewCompassResourcePurged(rid, projectRID string, markings []string, resourceType, displayName, deletedAt, deletedBy, purgedBy string, retentionDays int, purgeAfter, purgeMode string, dependents []AffectedDependent, truncated bool) AuditEvent {
	return AuditEvent{
		Kind:                   KindCompassResourcePurged,
		ResourceRID:            rid,
		ProjectRID:             projectRID,
		MarkingsAtEvent:        markings,
		ResourceType:           resourceType,
		DisplayName:            displayName,
		DeletedAt:              deletedAt,
		DeletedBy:              deletedBy,
		PurgedBy:               purgedBy,
		RetentionDays:          intPtr(retentionDays),
		PurgeAfter:             purgeAfter,
		PurgeMode:              purgeMode,
		AffectedDependents:     dependents,
		DependentListTruncated: boolPtr(truncated),
	}
}

// NewCompassViewRequirementsPropagated records a background copy of
// legacy "Propagate view requirements" markings to descendants.
func NewCompassViewRequirementsPropagated(parentRID, projectRID string, markings, previous []string, parentKind, jobID string, totalFolders, changedFolders, totalResources, changedResources int, dependents []AffectedDependent, truncated bool) AuditEvent {
	return AuditEvent{
		Kind:                   KindCompassViewReqPropagated,
		ResourceRID:            parentRID,
		ProjectRID:             projectRID,
		MarkingsAtEvent:        markings,
		PreviousMarkings:       previous,
		ResourceType:           parentKind,
		PropagationJobID:       jobID,
		ParentResourceRID:      parentRID,
		ParentResourceKind:     parentKind,
		TotalFolders:           intPtr(totalFolders),
		ChangedFolders:         intPtr(changedFolders),
		TotalResources:         intPtr(totalResources),
		ChangedResources:       intPtr(changedResources),
		AffectedDependents:     dependents,
		DependentListTruncated: boolPtr(truncated),
	}
}

// UserResourceRID builds the canonical RID for an identity-federation
// user. Kept in this package so audit producers and the audit-sink
// consumer derive the same string from a raw UUID.
func UserResourceRID(userID string) string {
	return "ri.identity.main.user." + userID
}

// NewAuthLogin records a successful SSO/OIDC/SAML login. `userID` is
// the platform user UUID; `tenantID` may be empty when the user is
// global. `mfaSatisfied` is the boolean MFA gate result (true also
// when MFA is not configured for the user). `subject` is the IdP-side
// identifier (email, sub claim, NameID).
func NewAuthLogin(userID, tenantID, provider, subject, loginEmail string, mfaSatisfied bool, authMethods []string) AuditEvent {
	return AuditEvent{
		Kind:            KindAuthLogin,
		ResourceRID:     UserResourceRID(userID),
		ProjectRID:      tenantID,
		MarkingsAtEvent: nil,
		UserID:          userID,
		TenantID:        tenantID,
		Provider:        provider,
		Subject:         subject,
		LoginEmail:      loginEmail,
		MFASatisfied:    boolPtr(mfaSatisfied),
		AuthMethods:     authMethods,
	}
}

// NewIdentityLinked records the first-time binding of an IdP subject
// to a platform user. Re-logins do not emit this — only the row
// insertion does, since the binding is the audit-worthy state change.
func NewIdentityLinked(userID, tenantID, provider, subject, loginEmail string) AuditEvent {
	return AuditEvent{
		Kind:            KindIdentityLinked,
		ResourceRID:     UserResourceRID(userID),
		ProjectRID:      tenantID,
		MarkingsAtEvent: nil,
		UserID:          userID,
		TenantID:        tenantID,
		Provider:        provider,
		Subject:         subject,
		LoginEmail:      loginEmail,
	}
}

// NewTokenIssued records an access-token mint. The deterministic
// event_id falls out of (tokenID || userID || expiresAt) — a retried
// callback that successfully re-mints under the same JTI collapses
// into the same outbox row.
func NewTokenIssued(tokenID, userID, tenantID string, expiresAt time.Time, scopes []string) AuditEvent {
	return AuditEvent{
		Kind:            KindTokenIssued,
		ResourceRID:     UserResourceRID(userID),
		ProjectRID:      tenantID,
		MarkingsAtEvent: nil,
		UserID:          userID,
		TenantID:        tenantID,
		TokenID:         tokenID,
		ExpiresAt:       expiresAt,
		Scopes:          scopes,
	}
}
