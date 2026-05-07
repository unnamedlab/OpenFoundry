package controlbus

// schema_registry.go ports libs/event-bus-control/src/schema_registry.rs.
//
// Schema Registry primitives shared by ingestion-replication-service
// (storage + REST API) and the data-connection plane connectors
// (validation of incoming samples).
//
// This module is intentionally pure: no DB, no HTTP. The only inputs
// are a schema string (Avro JSON IDL, JSON Schema document, or
// Protobuf FileDescriptorSet base64-encoded) and a payload to
// validate. The Schema Registry service layers persistence,
// versioning, references and Confluent-style HTTP routes on top of
// these helpers.
//
// Four entry points mirror the Rust crate:
//   - SchemaType / CompatibilityMode parsing
//   - Fingerprint  — deterministic SHA-256 over canonicalised schema
//   - ValidatePayload  — parse the schema once, then check the payload
//   - CheckCompatibility  — compare a new schema against the previous
//     one under a Confluent compatibility mode (BACKWARD, FORWARD,
//     FULL, NONE).

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	avro "github.com/hamba/avro/v2"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/proto"
)

// SchemaType is the supported schema-language enum.
type SchemaType string

// Supported schema languages.
const (
	SchemaTypeAvro     SchemaType = "avro"
	SchemaTypeProtobuf SchemaType = "protobuf"
	SchemaTypeJSON     SchemaType = "json"
)

// String returns the canonical lower-case wire form.
func (t SchemaType) String() string { return string(t) }

// ParseSchemaType parses a case-insensitive schema-type label.
// Accepts the same set of aliases the Rust impl accepts:
// `avro` / `protobuf|proto` / `json|json_schema|jsonschema`.
func ParseSchemaType(value string) (SchemaType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "avro":
		return SchemaTypeAvro, nil
	case "protobuf", "proto":
		return SchemaTypeProtobuf, nil
	case "json", "json_schema", "jsonschema":
		return SchemaTypeJSON, nil
	default:
		return "", &SchemaError{Kind: SchemaErrUnsupportedSchemaType, Msg: value}
	}
}

// CompatibilityMode is the Confluent-style compatibility level.
type CompatibilityMode string

// Confluent-compatible compatibility levels.
const (
	CompatibilityNone               CompatibilityMode = "NONE"
	CompatibilityBackward           CompatibilityMode = "BACKWARD"
	CompatibilityBackwardTransitive CompatibilityMode = "BACKWARD_TRANSITIVE"
	CompatibilityForward            CompatibilityMode = "FORWARD"
	CompatibilityForwardTransitive  CompatibilityMode = "FORWARD_TRANSITIVE"
	CompatibilityFull               CompatibilityMode = "FULL"
	CompatibilityFullTransitive     CompatibilityMode = "FULL_TRANSITIVE"
)

// ParseCompatibilityMode parses a case-insensitive compatibility-mode
// label.
func ParseCompatibilityMode(value string) (CompatibilityMode, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "NONE":
		return CompatibilityNone, nil
	case "BACKWARD":
		return CompatibilityBackward, nil
	case "BACKWARD_TRANSITIVE":
		return CompatibilityBackwardTransitive, nil
	case "FORWARD":
		return CompatibilityForward, nil
	case "FORWARD_TRANSITIVE":
		return CompatibilityForwardTransitive, nil
	case "FULL":
		return CompatibilityFull, nil
	case "FULL_TRANSITIVE":
		return CompatibilityFullTransitive, nil
	default:
		return "", &SchemaError{Kind: SchemaErrUnsupportedCompatibility, Msg: value}
	}
}

// SchemaErrorKind tags the SchemaError variants. Mirrors the Rust
// thiserror::Error enum.
type SchemaErrorKind uint8

// Error-kind tags.
const (
	SchemaErrUnsupportedSchemaType SchemaErrorKind = iota
	SchemaErrUnsupportedCompatibility
	SchemaErrParse
	SchemaErrValidation
	SchemaErrCompatibility
)

// SchemaError is the typed error returned by every schema-registry
// helper. Use errors.As with *SchemaError to discriminate on Kind.
type SchemaError struct {
	Kind SchemaErrorKind
	Msg  string
}

// Error implements error.
func (e *SchemaError) Error() string {
	switch e.Kind {
	case SchemaErrUnsupportedSchemaType:
		return "unsupported schema type: " + e.Msg
	case SchemaErrUnsupportedCompatibility:
		return "unsupported compatibility mode: " + e.Msg
	case SchemaErrParse:
		return "schema parse error: " + e.Msg
	case SchemaErrValidation:
		return "payload validation failed: " + e.Msg
	case SchemaErrCompatibility:
		return "compatibility check failed: " + e.Msg
	default:
		return "schema error: " + e.Msg
	}
}

// ─── fingerprint ───────────────────────────────────────────────────────

// Fingerprint returns a stable SHA-256 digest over the canonicalised
// schema bytes.
//
// For Avro and JSON we re-emit the parsed value through encoding/json
// so whitespace and key ordering are normalised — same approach as
// the Rust impl. For Protobuf the input is already a deterministic
// descriptor-set base64, so we hash it raw.
//
// Output format: "sha256:<lowercase-hex>".
func Fingerprint(schemaType SchemaType, schemaText string) (string, error) {
	var canonical string
	switch schemaType {
	case SchemaTypeAvro, SchemaTypeJSON:
		var v any
		if err := json.Unmarshal([]byte(schemaText), &v); err != nil {
			return "", &SchemaError{Kind: SchemaErrParse, Msg: err.Error()}
		}
		// Go's encoding/json sorts map keys alphabetically and emits
		// no extraneous whitespace by default — matches the Rust
		// `serde_json::to_string(&value)` round-trip.
		canon, err := json.Marshal(canonicalize(v))
		if err != nil {
			return "", &SchemaError{Kind: SchemaErrParse, Msg: err.Error()}
		}
		canonical = string(canon)
	case SchemaTypeProtobuf:
		canonical = schemaText
	default:
		return "", &SchemaError{Kind: SchemaErrUnsupportedSchemaType, Msg: string(schemaType)}
	}
	sum := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("sha256:%x", sum[:]), nil
}

// canonicalize rewrites map[string]any with sorted keys recursively so
// json.Marshal emits a deterministic byte stream. Slices are walked
// element-wise; primitives pass through.
func canonicalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		// json.Marshal already sorts map keys, so returning the map
		// as-is is enough — kept here for parity with the Rust path
		// that goes Value → String via serde_json::to_string.
		out := make(map[string]any, len(x))
		for _, k := range keys {
			out[k] = canonicalize(x[k])
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = canonicalize(item)
		}
		return out
	default:
		return v
	}
}

// ─── ValidatePayload ───────────────────────────────────────────────────

// ValidatePayload returns nil when payload conforms to the schema.
// payload may be any json.RawMessage / []byte slice that already
// holds a JSON-encoded value — the connectors hand us JSON-decoded
// sample messages, so the wire format is the same as Rust's serde_json::Value.
func ValidatePayload(schemaType SchemaType, schemaText string, payload json.RawMessage) error {
	switch schemaType {
	case SchemaTypeAvro:
		return validateAvro(schemaText, payload)
	case SchemaTypeJSON:
		return validateJSON(schemaText, payload)
	case SchemaTypeProtobuf:
		return validateProtobuf(schemaText, payload)
	default:
		return &SchemaError{Kind: SchemaErrUnsupportedSchemaType, Msg: string(schemaType)}
	}
}

// CheckCompatibility returns nil when `next` is compatible with
// `previous` under `mode`. Mode `NONE` is always Ok.
func CheckCompatibility(schemaType SchemaType, previous, next string, mode CompatibilityMode) error {
	if mode == CompatibilityNone {
		return nil
	}
	switch schemaType {
	case SchemaTypeAvro:
		return avroCompatibility(previous, next, mode)
	case SchemaTypeJSON:
		return jsonCompatibility(previous, next, mode)
	case SchemaTypeProtobuf:
		return protobufCompatibility(previous, next, mode)
	default:
		return &SchemaError{Kind: SchemaErrUnsupportedSchemaType, Msg: string(schemaType)}
	}
}

// ─── Avro ─────────────────────────────────────────────────────────────

func parseAvro(schemaText string) (avro.Schema, error) {
	s, err := avro.Parse(schemaText)
	if err != nil {
		return nil, &SchemaError{Kind: SchemaErrParse, Msg: "avro: " + err.Error()}
	}
	return s, nil
}

func validateAvro(schemaText string, payload json.RawMessage) error {
	schema, err := parseAvro(schemaText)
	if err != nil {
		return err
	}
	// Decode payload preserving integer-shape for numeric values —
	// json.Unmarshal into `any` would coerce every JSON number into
	// float64, which hamba/avro rejects when the schema declares
	// long/int. Mirrors the Rust `json_to_avro` projection that
	// branches on `n.as_i64()` first, then falls back to f64.
	native, err := decodeJSONForAvro(payload)
	if err != nil {
		return &SchemaError{Kind: SchemaErrValidation, Msg: "json decode: " + err.Error()}
	}
	bin, err := avro.Marshal(schema, native)
	if err != nil {
		return &SchemaError{Kind: SchemaErrValidation, Msg: "avro payload does not match schema: " + err.Error()}
	}
	// Round-trip back so a producer-side bug that still encodes is
	// caught on decode (mirrors the Rust validator's strictness).
	var sink any
	if err := avro.Unmarshal(schema, bin, &sink); err != nil {
		return &SchemaError{Kind: SchemaErrValidation, Msg: "avro round-trip failed: " + err.Error()}
	}
	return nil
}

// decodeJSONForAvro decodes `payload` into a native Go tree where
// JSON numbers are int64 when integer-shaped and float64 otherwise.
// Mirrors the Rust `json_to_avro` projection in spirit (Long vs
// Double dispatch).
func decodeJSONForAvro(payload json.RawMessage) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	return reshapeForAvro(raw), nil
}

func reshapeForAvro(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		if f, err := x.Float64(); err == nil {
			return f
		}
		return x.String()
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = reshapeForAvro(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = reshapeForAvro(val)
		}
		return out
	default:
		return v
	}
}

func avroCompatibility(previous, next string, mode CompatibilityMode) error {
	prev, err := parseAvro(previous)
	if err != nil {
		return err
	}
	nx, err := parseAvro(next)
	if err != nil {
		return err
	}
	resolver := avro.NewSchemaCompatibility()
	backward := func() error {
		// Reader = next, Writer = previous → "can a next-version
		// reader decode a previous-version record?" That's the
		// canonical BACKWARD definition (Rust uses
		// `SchemaCompatibility::can_read(prev, next)` with the same
		// reader/writer convention).
		if err := resolver.Compatible(nx, prev); err != nil {
			return &SchemaError{Kind: SchemaErrCompatibility, Msg: "backward: " + err.Error()}
		}
		return nil
	}
	forward := func() error {
		if err := resolver.Compatible(prev, nx); err != nil {
			return &SchemaError{Kind: SchemaErrCompatibility, Msg: "forward: " + err.Error()}
		}
		return nil
	}
	switch mode {
	case CompatibilityNone:
		return nil
	case CompatibilityBackward, CompatibilityBackwardTransitive:
		return backward()
	case CompatibilityForward, CompatibilityForwardTransitive:
		return forward()
	case CompatibilityFull, CompatibilityFullTransitive:
		if err := backward(); err != nil {
			return err
		}
		return forward()
	default:
		return &SchemaError{Kind: SchemaErrUnsupportedCompatibility, Msg: string(mode)}
	}
}

// ─── JSON Schema ──────────────────────────────────────────────────────

func validateJSON(schemaText string, payload json.RawMessage) error {
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", strings.NewReader(schemaText)); err != nil {
		return &SchemaError{Kind: SchemaErrParse, Msg: "json schema: " + err.Error()}
	}
	schema, err := compiler.Compile("schema.json")
	if err != nil {
		return &SchemaError{Kind: SchemaErrParse, Msg: "json schema compile: " + err.Error()}
	}
	var v any
	if err := json.Unmarshal(payload, &v); err != nil {
		return &SchemaError{Kind: SchemaErrValidation, Msg: "json decode: " + err.Error()}
	}
	if err := schema.Validate(v); err != nil {
		return &SchemaError{Kind: SchemaErrValidation, Msg: err.Error()}
	}
	return nil
}

// jsonCompatibility implements the same pragmatic structural check
// the Rust impl uses: under BACKWARD, every `required` property in
// `next` must already exist (as an own property) in `previous`.
// Under FORWARD the reverse. FULL = both. This catches the common
// breaking change ("added new required field") without pulling in a
// schema-diff lib.
func jsonCompatibility(previous, next string, mode CompatibilityMode) error {
	var prev, nx map[string]any
	if err := json.Unmarshal([]byte(previous), &prev); err != nil {
		return &SchemaError{Kind: SchemaErrParse, Msg: "json schema previous: " + err.Error()}
	}
	if err := json.Unmarshal([]byte(next), &nx); err != nil {
		return &SchemaError{Kind: SchemaErrParse, Msg: "json schema next: " + err.Error()}
	}
	prevRequired := jsonRequiredProps(prev)
	nextRequired := jsonRequiredProps(nx)
	prevProps := jsonPropertyNames(prev)
	nextProps := jsonPropertyNames(nx)

	backward := func() error {
		for _, field := range nextRequired {
			if !contains(prevProps, field) {
				return &SchemaError{
					Kind: SchemaErrCompatibility,
					Msg:  fmt.Sprintf("BACKWARD: new required field '%s' is not present in previous schema", field),
				}
			}
		}
		return nil
	}
	forward := func() error {
		for _, field := range prevRequired {
			if !contains(nextProps, field) {
				return &SchemaError{
					Kind: SchemaErrCompatibility,
					Msg:  fmt.Sprintf("FORWARD: previous required field '%s' was removed in next schema", field),
				}
			}
		}
		return nil
	}
	switch mode {
	case CompatibilityNone:
		return nil
	case CompatibilityBackward, CompatibilityBackwardTransitive:
		return backward()
	case CompatibilityForward, CompatibilityForwardTransitive:
		return forward()
	case CompatibilityFull, CompatibilityFullTransitive:
		if err := backward(); err != nil {
			return err
		}
		return forward()
	default:
		return &SchemaError{Kind: SchemaErrUnsupportedCompatibility, Msg: string(mode)}
	}
}

func jsonRequiredProps(schema map[string]any) []string {
	raw, ok := schema["required"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func jsonPropertyNames(schema map[string]any) []string {
	raw, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for k := range raw {
		out = append(out, k)
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// ─── Protobuf (descriptor-set based) ──────────────────────────────────

func parseProtobuf(schemaText string) (*protoregistry.Files, *descriptorpb.FileDescriptorSet, error) {
	bin, err := base64.StdEncoding.DecodeString(strings.TrimSpace(schemaText))
	if err != nil {
		return nil, nil, &SchemaError{Kind: SchemaErrParse, Msg: "protobuf base64: " + err.Error()}
	}
	var fdSet descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(bin, &fdSet); err != nil {
		return nil, nil, &SchemaError{Kind: SchemaErrParse, Msg: "protobuf descriptor: " + err.Error()}
	}
	files, err := protodesc.NewFiles(&fdSet)
	if err != nil {
		return nil, nil, &SchemaError{Kind: SchemaErrParse, Msg: "protobuf descriptor: " + err.Error()}
	}
	return files, &fdSet, nil
}

func validateProtobuf(schemaText string, payload json.RawMessage) error {
	files, _, err := parseProtobuf(schemaText)
	if err != nil {
		return err
	}
	var v map[string]any
	if err := json.Unmarshal(payload, &v); err != nil {
		return &SchemaError{Kind: SchemaErrValidation, Msg: "protobuf payload must be an object"}
	}
	var descriptor protoreflect.MessageDescriptor
	if name, ok := v["__type"].(string); ok && name != "" {
		d, lookupErr := files.FindDescriptorByName(protoreflect.FullName(name))
		if lookupErr != nil {
			return &SchemaError{Kind: SchemaErrValidation, Msg: "unknown message: " + name}
		}
		md, ok := d.(protoreflect.MessageDescriptor)
		if !ok {
			return &SchemaError{Kind: SchemaErrValidation, Msg: "unknown message: " + name}
		}
		descriptor = md
	} else {
		descriptor = firstMessage(files)
		if descriptor == nil {
			return &SchemaError{Kind: SchemaErrParse, Msg: "protobuf descriptor set has no messages"}
		}
	}
	fields := descriptor.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		if field.Cardinality() != protoreflect.Required {
			continue
		}
		if _, ok := v[string(field.Name())]; !ok {
			return &SchemaError{
				Kind: SchemaErrValidation,
				Msg:  fmt.Sprintf("missing required protobuf field '%s'", field.Name()),
			}
		}
	}
	return nil
}

func firstMessage(files *protoregistry.Files) protoreflect.MessageDescriptor {
	var found protoreflect.MessageDescriptor
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		msgs := fd.Messages()
		if msgs.Len() > 0 {
			found = msgs.Get(0)
			return false
		}
		return true
	})
	return found
}

func protobufCompatibility(previous, next string, mode CompatibilityMode) error {
	_, prevFds, err := parseProtobuf(previous)
	if err != nil {
		return err
	}
	_, nextFds, err := parseProtobuf(next)
	if err != nil {
		return err
	}
	prevMsg := firstMessageDescriptor(prevFds)
	if prevMsg == nil {
		return &SchemaError{Kind: SchemaErrParse, Msg: "previous descriptor set is empty"}
	}
	nextMsg := firstMessageDescriptor(nextFds)
	if nextMsg == nil {
		return &SchemaError{Kind: SchemaErrParse, Msg: "next descriptor set is empty"}
	}

	prevTags := tagMap(prevMsg)
	nextTags := tagMap(nextMsg)

	backward := func() error {
		for tag, name := range prevTags {
			if newName, ok := nextTags[tag]; ok && newName != name {
				return &SchemaError{
					Kind: SchemaErrCompatibility,
					Msg:  fmt.Sprintf("BACKWARD: tag %d renamed from '%s' to '%s'", tag, name, newName),
				}
			}
		}
		return nil
	}
	forward := func() error {
		for tag, name := range nextTags {
			if prevName, ok := prevTags[tag]; ok && prevName != name {
				return &SchemaError{
					Kind: SchemaErrCompatibility,
					Msg:  fmt.Sprintf("FORWARD: tag %d renamed from '%s' to '%s'", tag, prevName, name),
				}
			}
		}
		return nil
	}
	switch mode {
	case CompatibilityNone:
		return nil
	case CompatibilityBackward, CompatibilityBackwardTransitive:
		return backward()
	case CompatibilityForward, CompatibilityForwardTransitive:
		return forward()
	case CompatibilityFull, CompatibilityFullTransitive:
		if err := backward(); err != nil {
			return err
		}
		return forward()
	default:
		return &SchemaError{Kind: SchemaErrUnsupportedCompatibility, Msg: string(mode)}
	}
}

// firstMessageDescriptor returns the first message descriptor from
// the first file that has any messages. Mirrors `pool.all_messages().next()`
// on the Rust side.
func firstMessageDescriptor(fds *descriptorpb.FileDescriptorSet) *descriptorpb.DescriptorProto {
	for _, file := range fds.GetFile() {
		if msgs := file.GetMessageType(); len(msgs) > 0 {
			return msgs[0]
		}
	}
	return nil
}

func tagMap(msg *descriptorpb.DescriptorProto) map[int32]string {
	out := make(map[int32]string, len(msg.GetField()))
	for _, f := range msg.GetField() {
		out[f.GetNumber()] = f.GetName()
	}
	return out
}
