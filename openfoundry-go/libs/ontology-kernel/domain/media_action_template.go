// H6 — "Upload media" action-type scaffolding.
//
// Foundry's `Upload media.md` doc describes a Foundry-style action
// that lets an operator pick a file in the action form and persists
// it to a backing media set on submit (deferred upload — orphaned
// files are never created when the form is cancelled or fails).
//
// On the OpenFoundry side, an "Upload media" action is just an
// `update_object` action whose input schema contains a single
// `media_reference`-typed field, plus the convention that the
// field's `default_value` carries the backing media set's RID.
//
// Mirrors `libs/ontology-kernel/src/domain/media_action_template.rs`.

package domain

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// MediaSetBacking mirrors `struct MediaSetBacking`.
type MediaSetBacking struct {
	FieldName   string
	MediaSetRID string
}

// BuildUploadMediaActionInput mirrors `pub fn build_upload_media_action_input`.
// Returns the canonical [models.ActionInputField] for an Upload-media
// action: required, property_type=media_reference, default_value
// carrying the backing media-set RID under `media_set_rid`.
func BuildUploadMediaActionInput(fieldName, displayName, backingMediaSetRID string) models.ActionInputField {
	dn := displayName
	desc := "Upload media field. The selected file is persisted to the backing " +
		"media set on submit; canceled forms never create orphans."
	defaultValue := json.RawMessage(`{"media_set_rid":` + mustQuote(backingMediaSetRID) + `}`)
	return models.ActionInputField{
		Name:         fieldName,
		DisplayName:  &dn,
		Description:  &desc,
		PropertyType: "media_reference",
		Required:     true,
		DefaultValue: defaultValue,
	}
}

// CollectMediaSetBackings mirrors `pub fn collect_media_set_backings`.
// Walks every media_reference input on an action and pulls out its
// backing media-set RID via either camelCase or snake_case.
func CollectMediaSetBackings(inputSchema []models.ActionInputField) []MediaSetBacking {
	out := []MediaSetBacking{}
	for _, field := range inputSchema {
		if field.PropertyType != "media_reference" {
			continue
		}
		if len(field.DefaultValue) == 0 {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(field.DefaultValue, &obj); err != nil {
			continue
		}
		rid := pullTrimmedString(obj, "mediaSetRid", "media_set_rid")
		if rid == "" {
			continue
		}
		out = append(out, MediaSetBacking{FieldName: field.Name, MediaSetRID: rid})
	}
	return out
}

// MediaBackingWarningCode mirrors the discriminant of the Rust enum
// `MediaBackingWarning`. The string values render in
// SCREAMING_SNAKE_CASE on the wire to match the Rust serde
// `#[serde(tag = "code", rename_all = "SCREAMING_SNAKE_CASE")]`.
type MediaBackingWarningCode string

const (
	MediaBackingMultipleSets        MediaBackingWarningCode = "MULTIPLE_BACKING_SETS"
	MediaBackingListNotSupported    MediaBackingWarningCode = "MEDIA_REFERENCE_LIST_NOT_SUPPORTED"
)

// MediaBackingWarning mirrors `enum MediaBackingWarning`. Serialises
// with the discriminant inlined under `code`. The two variants carry
// distinct payload fields — `Message`/`DistinctSets` for the
// multiple-backing-set warning, `FieldName` for the list-not-
// supported warning.
type MediaBackingWarning struct {
	Code         MediaBackingWarningCode `json:"code"`
	Message      string                  `json:"message,omitempty"`
	DistinctSets []string                `json:"distinct_sets,omitempty"`
	FieldName    string                  `json:"field_name,omitempty"`
}

// BackingWarnings mirrors `pub fn backing_warnings`. Runs the two
// doc-driven warnings against an action's input schema:
//
//  1. Multiple distinct backing sets — Foundry "strongly discourages"
//     this; we return [MediaBackingMultipleSets] with the sorted
//     list of distinct RIDs.
//  2. Array of media_reference under struct_fields — Foundry says
//     "media reference list properties are not supported on an
//     object"; we surface a [MediaBackingListNotSupported] per
//     offending top-level field.
func BackingWarnings(inputSchema []models.ActionInputField) []MediaBackingWarning {
	warnings := []MediaBackingWarning{}

	backings := CollectMediaSetBackings(inputSchema)
	distinct := stringSetSorted(backings)
	if len(distinct) > 1 {
		warnings = append(warnings, MediaBackingWarning{
			Code: MediaBackingMultipleSets,
			Message: "Multiple media sets backing a single Upload-media action are " +
				"strongly discouraged (Foundry: Using media in the Ontology). " +
				"Uploads in actions are not fully supported in this case.",
			DistinctSets: distinct,
		})
	}

	// Foundry: media reference *list* properties are not supported.
	// We approximate "list" as a struct field whose property_type
	// chain involves media_reference repeated under an array.
	for _, field := range inputSchema {
		if field.PropertyType != "array" {
			continue
		}
		if field.StructFields == nil {
			continue
		}
		hasMediaRef := false
		for _, sub := range *field.StructFields {
			if sub.PropertyType == "media_reference" {
				hasMediaRef = true
				break
			}
		}
		if hasMediaRef {
			warnings = append(warnings, MediaBackingWarning{
				Code:      MediaBackingListNotSupported,
				FieldName: field.Name,
			})
		}
	}
	return warnings
}

// MediaUploadPlaceholder mirrors `struct MediaUploadPlaceholder` —
// the camelCase JSON shape the front-end submits when the user
// picked a file but the upload to the backing media set hasn't
// happened yet.
type MediaUploadPlaceholder struct {
	PendingUpload bool   `json:"pendingUpload"`
	MediaSetRID   string `json:"mediaSetRid"`
	FileName      string `json:"fileName"`
	MimeType      string `json:"mimeType"`
	BlobToken     string `json:"blobToken"`
}

// TryParseMediaUploadPlaceholder mirrors `MediaUploadPlaceholder::try_from_value`.
// Returns nil when the payload is not an object, or when the
// `pendingUpload` flag is missing/false, or when any of the
// required RID/fileName/blobToken fields are blank.
func TryParseMediaUploadPlaceholder(value json.RawMessage) *MediaUploadPlaceholder {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(value, &obj); err != nil || obj == nil {
		return nil
	}
	pendingRaw, ok := obj["pendingUpload"]
	if !ok {
		pendingRaw, ok = obj["pending_upload"]
	}
	if !ok {
		return nil
	}
	var pending bool
	if err := json.Unmarshal(pendingRaw, &pending); err != nil || !pending {
		return nil
	}
	mediaSetRID := pullTrimmedString(obj, "mediaSetRid", "media_set_rid")
	if mediaSetRID == "" {
		return nil
	}
	fileName := pullTrimmedString(obj, "fileName", "file_name")
	if fileName == "" {
		return nil
	}
	blobToken := pullTrimmedString(obj, "blobToken", "blob_token")
	if blobToken == "" {
		return nil
	}
	mimeType := pullTrimmedString(obj, "mimeType", "mime_type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return &MediaUploadPlaceholder{
		PendingUpload: true,
		MediaSetRID:   mediaSetRID,
		FileName:      fileName,
		MimeType:      mimeType,
		BlobToken:     blobToken,
	}
}

// DetectPendingUploads mirrors `pub fn detect_pending_uploads`.
// Walks the inputs map and surfaces every (field_name, placeholder)
// pair so the action submit handler knows which fields to resolve.
// The returned ordering matches Rust's HashMap iteration → not
// guaranteed stable; callers MUST sort if they need determinism.
func DetectPendingUploads(inputs map[string]json.RawMessage) []DetectedUpload {
	out := []DetectedUpload{}
	keys := make([]string, 0, len(inputs))
	for k := range inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if placeholder := TryParseMediaUploadPlaceholder(inputs[key]); placeholder != nil {
			out = append(out, DetectedUpload{FieldName: key, Placeholder: *placeholder})
		}
	}
	return out
}

// DetectedUpload mirrors the `(String, MediaUploadPlaceholder)`
// tuple Rust returns from [detect_pending_uploads].
type DetectedUpload struct {
	FieldName   string
	Placeholder MediaUploadPlaceholder
}

// ---- helpers --------------------------------------------------------------

// pullTrimmedString extracts the first present non-blank string at
// any of the candidate keys.
func pullTrimmedString(obj map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		raw, ok := obj[k]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		s = strings.TrimSpace(s)
		if s != "" {
			return s
		}
	}
	return ""
}

// stringSetSorted returns the sorted list of distinct media-set
// RIDs. Mirrors the Rust `BTreeSet<&str>::collect()` behaviour
// (deterministic iteration order via lexical sort).
func stringSetSorted(backings []MediaSetBacking) []string {
	seen := map[string]bool{}
	for _, b := range backings {
		seen[b.MediaSetRID] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// mustQuote returns the JSON-quoted form of s. Keeps
// [BuildUploadMediaActionInput] free of the marshaling-error path
// because the input is a single string and json.Marshal cannot fail.
func mustQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
