// Package rid implements OpenFoundry Resource Identifiers.
//
// The wire form follows Palantir's public Resource Identifier specification:
//
//	ri.<service>.<instance>.<type>.<locator>
//
// OpenFoundry resources that are minted by the platform use a UUID locator,
// normally UUID v7 for new resources, so a resource can be renamed or moved
// without changing its canonical reference.
package rid

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
)

const (
	// FormatPrefix is the fixed RID format marker.
	FormatPrefix = "ri"
	// DefaultInstance is the canonical OpenFoundry instance id for resources
	// that do not need a more specific cluster or tenant instance.
	DefaultInstance = "main"
)

var (
	servicePattern  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	instancePattern = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*)?$`)
	typePattern     = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	locatorPattern  = regexp.MustCompile(`^[a-zA-Z0-9\-._]+$`)
)

// ResourceIdentifier is a parsed RID. The locator may be any value allowed by
// the RID spec; use ParseUUID or UUID when the resource contract requires a
// UUID locator.
type ResourceIdentifier struct {
	Service      string
	Instance     string
	ResourceType string
	Locator      string
}

// New validates and builds a ResourceIdentifier from components.
func New(service, instance, resourceType, locator string) (ResourceIdentifier, error) {
	out := ResourceIdentifier{
		Service:      strings.TrimSpace(service),
		Instance:     strings.TrimSpace(instance),
		ResourceType: strings.TrimSpace(resourceType),
		Locator:      strings.TrimSpace(locator),
	}
	if !servicePattern.MatchString(out.Service) {
		return ResourceIdentifier{}, invalidComponent("service", out.Service, "[a-z][a-z0-9-]*")
	}
	if !instancePattern.MatchString(out.Instance) {
		return ResourceIdentifier{}, invalidComponent("instance", out.Instance, "([a-z0-9][a-z0-9-]*)?")
	}
	if !typePattern.MatchString(out.ResourceType) {
		return ResourceIdentifier{}, invalidComponent("type", out.ResourceType, "[a-z][a-z0-9-]*")
	}
	if !locatorPattern.MatchString(out.Locator) {
		return ResourceIdentifier{}, invalidComponent("locator", out.Locator, "[a-zA-Z0-9-._]+")
	}
	return out, nil
}

// MustNew validates and builds a ResourceIdentifier, panicking on invalid
// compile-time constants.
func MustNew(service, instance, resourceType, locator string) ResourceIdentifier {
	out, err := New(service, instance, resourceType, locator)
	if err != nil {
		panic(err)
	}
	return out
}

// NewUUID validates RID namespace components and builds a RID with a UUID
// locator. The UUID is rendered in canonical lowercase form.
func NewUUID(service, instance, resourceType string, id uuid.UUID) (ResourceIdentifier, error) {
	if id == uuid.Nil {
		return ResourceIdentifier{}, &InvalidError{Input: id.String(), Reason: "uuid locator must not be nil"}
	}
	return New(service, instance, resourceType, id.String())
}

// MustNewUUID is NewUUID for compile-time constants and already-validated IDs.
func MustNewUUID(service, instance, resourceType string, id uuid.UUID) ResourceIdentifier {
	out, err := NewUUID(service, instance, resourceType, id)
	if err != nil {
		panic(err)
	}
	return out
}

// MintUUIDV7 creates a new OpenFoundry RID with a UUID v7 locator.
func MintUUIDV7(service, instance, resourceType string) (ResourceIdentifier, error) {
	return NewUUID(service, instance, resourceType, ids.New())
}

// MustMintUUIDV7 is MintUUIDV7 for trusted namespace constants.
func MustMintUUIDV7(service, instance, resourceType string) ResourceIdentifier {
	out, err := MintUUIDV7(service, instance, resourceType)
	if err != nil {
		panic(err)
	}
	return out
}

// Parse validates a RID and returns its components. The locator component may
// contain periods, so parsing only splits the first four separators.
func Parse(s string) (ResourceIdentifier, error) {
	input := strings.TrimSpace(s)
	parts := strings.SplitN(input, ".", 5)
	if len(parts) != 5 || parts[0] != FormatPrefix {
		return ResourceIdentifier{}, &InvalidError{
			Input:  s,
			Reason: "expected ri.<service>.<instance>.<type>.<locator>",
		}
	}
	out, err := New(parts[1], parts[2], parts[3], parts[4])
	if err != nil {
		return ResourceIdentifier{}, &InvalidError{Input: s, Reason: err.Error()}
	}
	return out, nil
}

// ParseUUID validates a RID whose locator must be a UUID.
func ParseUUID(s string) (ResourceIdentifier, error) {
	out, err := Parse(s)
	if err != nil {
		return ResourceIdentifier{}, err
	}
	if _, err := uuid.Parse(out.Locator); err != nil {
		return ResourceIdentifier{}, &InvalidError{
			Input:  s,
			Reason: fmt.Sprintf("locator must be a UUID: %v", err),
		}
	}
	return out, nil
}

// Is reports whether s is a syntactically valid RID.
func Is(s string) bool {
	_, err := Parse(s)
	return err == nil
}

// IsUUID reports whether s is a syntactically valid RID with a UUID locator.
func IsUUID(s string) bool {
	_, err := ParseUUID(s)
	return err == nil
}

// String returns the canonical RID string.
func (r ResourceIdentifier) String() string {
	return strings.Join([]string{FormatPrefix, r.Service, r.Instance, r.ResourceType, r.Locator}, ".")
}

// UUID parses the locator as a UUID.
func (r ResourceIdentifier) UUID() (uuid.UUID, bool) {
	parsed, err := uuid.Parse(r.Locator)
	if err != nil {
		return uuid.UUID{}, false
	}
	return parsed, true
}

// MarshalText returns the canonical RID string.
func (r ResourceIdentifier) MarshalText() ([]byte, error) {
	if _, err := New(r.Service, r.Instance, r.ResourceType, r.Locator); err != nil {
		return nil, err
	}
	return []byte(r.String()), nil
}

// UnmarshalText parses a RID string.
func (r *ResourceIdentifier) UnmarshalText(text []byte) error {
	parsed, err := Parse(string(text))
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

// MarshalJSON encodes the RID as a JSON string.
func (r ResourceIdentifier) MarshalJSON() ([]byte, error) {
	text, err := r.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(text))
}

// UnmarshalJSON decodes the RID from a JSON string.
func (r *ResourceIdentifier) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	return r.UnmarshalText([]byte(text))
}

// InvalidError describes a malformed RID or RID component.
type InvalidError struct {
	Input  string
	Reason string
}

func (e *InvalidError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("invalid RID %q", e.Input)
	}
	return fmt.Sprintf("invalid RID %q: %s", e.Input, e.Reason)
}

func invalidComponent(name, value, pattern string) error {
	return &InvalidError{
		Input:  value,
		Reason: fmt.Sprintf("%s must match %s", name, pattern),
	}
}
