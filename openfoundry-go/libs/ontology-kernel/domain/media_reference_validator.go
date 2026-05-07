// Foundry-style validation for the `media_reference` property type
// that goes beyond the structural shape check in
// [ValidatePropertyValue] (type_system.go).
//
// `ValidatePropertyValue` is a pure JSON shape gate. It accepts a
// string OR an object with a non-empty `uri`/`url` — fine for the
// pre-H6 stub. H6 lifts media-reference properties into the Ontology
// proper and demands two extra invariants:
//
//  1. **Set existence.** The mediaSetRid must address a media set
//     that exists.
//  2. **Clearance covers markings.** The user editing the property
//     must hold every marking on the backing media set.
//
// Mirrors `libs/ontology-kernel/src/domain/media_reference_validator.rs`.

package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ParsedMediaReference mirrors `struct ParsedMediaReference`.
type ParsedMediaReference struct {
	MediaSetRID  string
	MediaItemRID string
	Branch       *string
	Schema       *string
}

// ResolvedMediaSet mirrors `struct ResolvedMediaSet`. Markings are
// expected lower-cased by callers (the Rust source documents the same
// contract).
type ResolvedMediaSet struct {
	MediaSetRID string
	Markings    []string
}

// MediaReferenceContext mirrors `struct MediaReferenceContext<'a>`.
// `ResolveSet` is the closure the Rust source carries as
// `Box<dyn Fn(&str) -> Option<ResolvedMediaSet>>`; the Go equivalent
// is a function value plus the user's clearances.
type MediaReferenceContext struct {
	ResolveSet     func(rid string) *ResolvedMediaSet
	UserClearances []string
}

// MediaReferenceValidationErrorKind tags the error variants. Mirrors
// `enum MediaReferenceValidationError` in the Rust source.
type MediaReferenceValidationErrorKind int

const (
	// MediaRefNotAnObject — payload is not a JSON object.
	MediaRefNotAnObject MediaReferenceValidationErrorKind = iota
	// MediaRefMissingField — required field is absent.
	MediaRefMissingField
	// MediaRefEmptyField — required field is present but blank.
	MediaRefEmptyField
	// MediaRefUnknownMediaSet — resolveSet returned nil.
	MediaRefUnknownMediaSet
	// MediaRefInsufficientClearance — user is missing one of the
	// markings required by the resolved media set.
	MediaRefInsufficientClearance
)

// MediaReferenceValidationError mirrors the variant enum. Field
// presence depends on Kind: MissingField/EmptyField → Field;
// UnknownMediaSet → MediaSetRID; InsufficientClearance → Missing.
type MediaReferenceValidationError struct {
	Kind        MediaReferenceValidationErrorKind
	Field       string
	MediaSetRID string
	Missing     string
}

// Error mirrors the Display impl — produces the same human-readable
// string the Rust `thiserror` macro generates.
func (e *MediaReferenceValidationError) Error() string {
	switch e.Kind {
	case MediaRefNotAnObject:
		return "media_reference value must be a JSON object on H6 ontology surfaces"
	case MediaRefMissingField:
		return fmt.Sprintf("media_reference is missing field `%s`", e.Field)
	case MediaRefEmptyField:
		return fmt.Sprintf("media_reference field `%s` must be a non-empty string", e.Field)
	case MediaRefUnknownMediaSet:
		return fmt.Sprintf("media set `%s` does not exist", e.MediaSetRID)
	case MediaRefInsufficientClearance:
		return fmt.Sprintf("missing clearance: %s", e.Missing)
	}
	return "media_reference validation error"
}

var _ error = (*MediaReferenceValidationError)(nil)

// IsMediaRefError reports whether the error chain contains a
// [MediaReferenceValidationError] of the given kind.
func IsMediaRefError(err error, kind MediaReferenceValidationErrorKind) bool {
	var m *MediaReferenceValidationError
	if !errors.As(err, &m) {
		return false
	}
	return m.Kind == kind
}

// parseMediaReference mirrors `fn parse`. Accepts both camelCase
// (`mediaSetRid`, `mediaItemRid`, …) and snake_case
// (`media_set_rid`, `media_item_rid`, …) payloads — the OSDK
// round-trips through camelCase but the Rust services serialise via
// the struct, so both must work.
func parseMediaReference(value json.RawMessage) (ParsedMediaReference, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(value, &obj); err != nil || obj == nil {
		return ParsedMediaReference{}, &MediaReferenceValidationError{Kind: MediaRefNotAnObject}
	}

	pull := func(camel, snake string) (string, bool) {
		raw, ok := obj[camel]
		if !ok {
			raw, ok = obj[snake]
			if !ok {
				return "", false
			}
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", false
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return "", false
		}
		return s, true
	}

	mediaSetRID, ok := pull("mediaSetRid", "media_set_rid")
	if !ok {
		return ParsedMediaReference{}, &MediaReferenceValidationError{
			Kind: MediaRefMissingField, Field: "mediaSetRid",
		}
	}
	mediaItemRID, ok := pull("mediaItemRid", "media_item_rid")
	if !ok {
		return ParsedMediaReference{}, &MediaReferenceValidationError{
			Kind: MediaRefMissingField, Field: "mediaItemRid",
		}
	}

	parsed := ParsedMediaReference{
		MediaSetRID:  mediaSetRID,
		MediaItemRID: mediaItemRID,
	}
	if v, ok := pull("branch", "branch"); ok {
		parsed.Branch = &v
	}
	if v, ok := pull("schema", "schema"); ok {
		parsed.Schema = &v
	}
	return parsed, nil
}

// ValidateMediaReference mirrors `pub fn validate`. Runs the H6
// contract: shape parse → set exists → clearances cover the set's
// markings. Returns the parsed reference on success so the caller
// can persist the canonical form (avoids a re-parse).
func ValidateMediaReference(value json.RawMessage, ctx MediaReferenceContext) (ParsedMediaReference, error) {
	parsed, err := parseMediaReference(value)
	if err != nil {
		return ParsedMediaReference{}, err
	}
	if ctx.ResolveSet == nil {
		return ParsedMediaReference{}, &MediaReferenceValidationError{
			Kind: MediaRefUnknownMediaSet, MediaSetRID: parsed.MediaSetRID,
		}
	}
	resolved := ctx.ResolveSet(parsed.MediaSetRID)
	if resolved == nil {
		return ParsedMediaReference{}, &MediaReferenceValidationError{
			Kind: MediaRefUnknownMediaSet, MediaSetRID: parsed.MediaSetRID,
		}
	}
	if !coversClearance(ctx.UserClearances, resolved.Markings) {
		missing := firstMissingMarking(ctx.UserClearances, resolved.Markings)
		if missing == "" {
			missing = strings.Join(resolved.Markings, ", ")
		}
		return ParsedMediaReference{}, &MediaReferenceValidationError{
			Kind: MediaRefInsufficientClearance, Missing: missing,
		}
	}
	return parsed, nil
}

// coversClearance mirrors `fn covers_clearance`. Builds a sorted
// set of the user's non-blank clearances and verifies every
// non-blank required marking lives in it.
func coversClearance(userClearances, requiredMarkings []string) bool {
	user := stringSet(userClearances)
	for _, marking := range requiredMarkings {
		marking = strings.TrimSpace(marking)
		if marking == "" {
			continue
		}
		if !user[marking] {
			return false
		}
	}
	return true
}

// firstMissingMarking mirrors `fn first_missing_marking`. Iteration
// order matches Rust: the required slice's order is preserved.
func firstMissingMarking(userClearances, requiredMarkings []string) string {
	user := stringSet(userClearances)
	for _, marking := range requiredMarkings {
		marking = strings.TrimSpace(marking)
		if marking == "" {
			continue
		}
		if !user[marking] {
			return marking
		}
	}
	return ""
}

// stringSet builds a set from a slice, trimming + dropping blanks.
// Mirrors the BTreeSet construction in the Rust source.
func stringSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		out[v] = true
	}
	return out
}

// MediaReferenceContextFromMap mirrors `pub fn context_from_map`.
// Useful in tests + the inline-edit fast path that already loaded the
// set.
func MediaReferenceContextFromMap(sets map[string]ResolvedMediaSet, userClearances []string) MediaReferenceContext {
	// Snapshot keys so the closure's view of the map is stable even
	// if the caller mutates it after building the context.
	keys := make([]string, 0, len(sets))
	for k := range sets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	frozen := make(map[string]ResolvedMediaSet, len(sets))
	for _, k := range keys {
		frozen[k] = sets[k]
	}
	return MediaReferenceContext{
		ResolveSet: func(rid string) *ResolvedMediaSet {
			if v, ok := frozen[rid]; ok {
				return &v
			}
			return nil
		},
		UserClearances: append([]string(nil), userClearances...),
	}
}
