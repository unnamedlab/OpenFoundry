// Package mediascanner is the Sensitive Data Scanner integration for
// media sets. Mirrors libs/media-scanner/src/lib.rs verbatim — same
// PII tag taxonomy, same finding/report shapes, same Scanner trait,
// same Mock runtime semantics.
//
// Per Foundry's "Sensitive Data Scanner / Media set scanning" doc:
// per item, run an OCR/extract-text pass and surface a list of
// findings (per-PII tag) the SDS dashboard ranks. The scanner runs
// under per-tenant quotas — the trait surfaces a QuotaRemaining check
// the binary enforces before queueing an item.
//
// services/sds-service/src/main.rs is currently fn main(){} so we
// keep the scanning surface in this dependency-light package. The H7
// integration test wires a MockMediaScanRuntime against the
// MediaScanner interface; the production runtime will plug in a
// runtime that calls media-transform-runtime-service for OCR + the
// platform PII taxonomy.
package mediascanner

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// PiiTag is the Foundry "Sensitive Data Scanner" PII tag taxonomy.
// JSON wire form is camelCase (matches what the SDS UI dashboard
// already parses); String() returns SCREAMING_SNAKE_CASE.
type PiiTag int

const (
	// PiiGovernmentID — US / EU government IDs. The doc's flagship
	// "high severity" tag. Maps to SSN/passport/national-id detectors
	// upstream.
	PiiGovernmentID PiiTag = iota
	PiiEmail
	PiiPhoneNumber
	// PiiCreditCard — Luhn-validated upstream.
	PiiCreditCard
	PiiAddress
	PiiDateOfBirth
	// PiiPersonName — free-form name detection, typically lowest
	// confidence.
	PiiPersonName
)

// String returns the SCREAMING_SNAKE_CASE token (matches Rust
// PiiTag::as_str — used by the SDS UI tooltip).
func (t PiiTag) String() string {
	switch t {
	case PiiGovernmentID:
		return "GOVERNMENT_ID"
	case PiiEmail:
		return "EMAIL"
	case PiiPhoneNumber:
		return "PHONE_NUMBER"
	case PiiCreditCard:
		return "CREDIT_CARD"
	case PiiAddress:
		return "ADDRESS"
	case PiiDateOfBirth:
		return "DATE_OF_BIRTH"
	case PiiPersonName:
		return "PERSON_NAME"
	default:
		return ""
	}
}

func (t PiiTag) jsonForm() string {
	switch t {
	case PiiGovernmentID:
		return "governmentId"
	case PiiEmail:
		return "email"
	case PiiPhoneNumber:
		return "phoneNumber"
	case PiiCreditCard:
		return "creditCard"
	case PiiAddress:
		return "address"
	case PiiDateOfBirth:
		return "dateOfBirth"
	case PiiPersonName:
		return "personName"
	default:
		return ""
	}
}

// MarshalJSON pins the camelCase wire form
// (#[serde(rename_all = "camelCase")] on the Rust side).
func (t PiiTag) MarshalJSON() ([]byte, error) {
	form := t.jsonForm()
	if form == "" {
		return nil, fmt.Errorf("media-scanner: unknown PiiTag %d", int(t))
	}
	return json.Marshal(form)
}

// UnmarshalJSON accepts the camelCase wire form.
func (t *PiiTag) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch s {
	case "governmentId":
		*t = PiiGovernmentID
	case "email":
		*t = PiiEmail
	case "phoneNumber":
		*t = PiiPhoneNumber
	case "creditCard":
		*t = PiiCreditCard
	case "address":
		*t = PiiAddress
	case "dateOfBirth":
		*t = PiiDateOfBirth
	case "personName":
		*t = PiiPersonName
	default:
		return fmt.Errorf("media-scanner: unknown PiiTag %q", s)
	}
	return nil
}

// SdsFinding is one row of the result of scanning a single item — one
// per finding, plus the set of distinct tags the parent SdsScanReport
// exposes via DistinctTags. The UI badge "PII detected" toggles on
// SdsScanReport.HasFindings.
type SdsFinding struct {
	MediaSetRID string `json:"media_set_rid"`
	ItemRID     string `json:"item_rid"`
	// Lowercased tag — lets SDS rules match without case folding.
	Tag PiiTag `json:"tag"`
	// Verbatim token surfaced by the upstream detector. Truncated to
	// 80 chars by the SDS doc; the producer mirrors that cap.
	Matched string `json:"matched"`
	// 0.0..=1.0 — model-reported confidence the upstream returned.
	Confidence float32 `json:"confidence"`
	// Page index for documents (nil for images / audio). Honours
	// skip_serializing_if = "Option::is_none" on the Rust side.
	Page *uint32 `json:"page,omitempty"`
}

// SdsScanReport is the aggregate result returned by
// MediaScanner.ScanItem.
type SdsScanReport struct {
	MediaSetRID string       `json:"media_set_rid"`
	ItemRID     string       `json:"item_rid"`
	Findings    []SdsFinding `json:"findings"`
}

// HasFindings reports whether at least one finding was produced.
func (r *SdsScanReport) HasFindings() bool { return len(r.Findings) > 0 }

// DistinctTags returns the distinct PII tags hit, sorted by enum order
// (GovernmentID < Email < PhoneNumber < CreditCard < Address <
// DateOfBirth < PersonName) — matches the Rust BTreeSet<PiiTag>
// iteration. The SDS UI uses this to render the per-item badge
// tooltip ("PII detected: GOVERNMENT_ID, PHONE_NUMBER").
func (r *SdsScanReport) DistinctTags() []PiiTag {
	seen := make(map[PiiTag]struct{}, len(r.Findings))
	for _, f := range r.Findings {
		seen[f.Tag] = struct{}{}
	}
	out := make([]PiiTag, 0, len(seen))
	for tag := range seen {
		out = append(out, tag)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// ScanErrorKind tags the failure case. Mirrors the thiserror enum.
type ScanErrorKind int

const (
	// ErrNotFound — media item id not found.
	ErrNotFound ScanErrorKind = iota
	// ErrQuotaExhausted — tenant has consumed its window.
	ErrQuotaExhausted
	// ErrRuntime — upstream OCR runtime returned a non-success.
	ErrRuntime
	// ErrUnscannableKind — kind has no OCR/extract_text path.
	ErrUnscannableKind
)

// ScanError reports a scanner failure. Mirrors the thiserror enum on
// the Rust side; the matching argument lives in Detail.
type ScanError struct {
	Kind   ScanErrorKind
	Detail string
}

func (e *ScanError) Error() string {
	switch e.Kind {
	case ErrNotFound:
		return fmt.Sprintf("media item `%s` not found", e.Detail)
	case ErrQuotaExhausted:
		return fmt.Sprintf("quota exhausted for tenant `%s`", e.Detail)
	case ErrRuntime:
		return fmt.Sprintf("upstream OCR runtime returned: %s", e.Detail)
	case ErrUnscannableKind:
		return fmt.Sprintf("media kind `%s` is not scannable (no OCR/extract_text path)", e.Detail)
	default:
		return "media-scanner: unknown error"
	}
}

// MediaScanner is the minimum surface a scanner runtime exposes. The
// interface stays small so test mocks + production runtimes implement
// the same shape:
//
//   - ScanItem — OCR/extract_text the bytes for one item and return
//     findings.
//   - QuotaRemaining — let the SDS dispatcher pre-check before
//     enqueueing N items.
type MediaScanner interface {
	ScanItem(ctx context.Context, mediaSetRID, itemRID string) (SdsScanReport, error)
	// QuotaRemaining returns the compute-seconds the tenant has left
	// in the current window. ok=false ⇒ unlimited (typical for dev /
	// small tenants).
	QuotaRemaining(ctx context.Context, tenantID string) (remaining uint64, ok bool)
}

// ─────────────────────── Mock runtime (test fixture) ──────────────────────

// CallEntry is one (mediaSetRID, itemRID) pair the mock's call log
// records.
type CallEntry struct {
	MediaSetRID string
	ItemRID     string
}

// MockMediaScanRuntime is the in-memory test fixture. Mirrors the
// Rust MockMediaScanRuntime: pre-scripted reports keyed by item_rid +
// per-tenant quota table + call log.
type MockMediaScanRuntime struct {
	mu      sync.Mutex
	reports map[string]SdsScanReport
	quotas  map[string]uint64
	calls   []CallEntry
}

// NewMockMediaScanRuntime returns an empty mock runtime.
func NewMockMediaScanRuntime() *MockMediaScanRuntime {
	return &MockMediaScanRuntime{
		reports: make(map[string]SdsScanReport),
		quotas:  make(map[string]uint64),
	}
}

// PutReport pre-scripts the report ScanItem will return for `itemRID`.
func (m *MockMediaScanRuntime) PutReport(itemRID string, report SdsScanReport) *MockMediaScanRuntime {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reports[itemRID] = report
	return m
}

// PutQuota pre-scripts the remaining-quota QuotaRemaining will return
// for `tenantID`.
func (m *MockMediaScanRuntime) PutQuota(tenantID string, remaining uint64) *MockMediaScanRuntime {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.quotas[tenantID] = remaining
	return m
}

// Calls returns a snapshot of the (mediaSetRID, itemRID) pairs ScanItem
// has been called with, in invocation order.
func (m *MockMediaScanRuntime) Calls() []CallEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]CallEntry, len(m.calls))
	copy(out, m.calls)
	return out
}

// ScanItem implements MediaScanner. Returns a *ScanError with
// Kind=ErrNotFound when no report was pre-scripted for `itemRID`.
func (m *MockMediaScanRuntime) ScanItem(_ context.Context, mediaSetRID, itemRID string) (SdsScanReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, CallEntry{MediaSetRID: mediaSetRID, ItemRID: itemRID})
	rep, ok := m.reports[itemRID]
	if !ok {
		return SdsScanReport{}, &ScanError{Kind: ErrNotFound, Detail: itemRID}
	}
	return rep, nil
}

// QuotaRemaining implements MediaScanner.
func (m *MockMediaScanRuntime) QuotaRemaining(_ context.Context, tenantID string) (uint64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	remaining, ok := m.quotas[tenantID]
	return remaining, ok
}
