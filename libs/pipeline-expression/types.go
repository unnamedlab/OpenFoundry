package pipelineexpression

import (
	"encoding/json"
	"fmt"
)

// Kind tags a [PipelineType]. Mirrors the Rust enum discriminator
// serialised via serde's `tag = "kind"` attribute.
type Kind string

const (
	KindBoolean   Kind = "BOOLEAN"
	KindInteger   Kind = "INTEGER"
	KindLong      Kind = "LONG"
	KindDouble    Kind = "DOUBLE"
	KindDecimal   Kind = "DECIMAL"
	KindString    Kind = "STRING"
	KindDate      Kind = "DATE"
	KindTimestamp Kind = "TIMESTAMP"
	KindGeometry  Kind = "GEOMETRY"
	KindArray     Kind = "ARRAY"
	KindStruct    Kind = "STRUCT"
)

// StructField is a single field inside a [PipelineType] of [KindStruct].
// Mirrors the Rust `(String, PipelineType)` tuple — serialised as a
// 2-element JSON array on the wire.
type StructField struct {
	Name string
	Type PipelineType
}

// PipelineType is the Pipeline Builder runtime type lattice.
//
// Mirrors the canonical lattice documented in Foundry's "Supported
// languages" reference. Numeric promotion follows the SQL-flavoured
// order Boolean < Integer < Long < Double < Decimal; cross-family
// promotion (e.g. String → Integer) requires an explicit `cast`.
type PipelineType struct {
	Kind   Kind
	Inner  *PipelineType // populated when Kind == KindArray
	Fields []StructField // populated when Kind == KindStruct
}

// BooleanType returns a Boolean type literal.
func BooleanType() PipelineType { return PipelineType{Kind: KindBoolean} }

// IntegerType returns an Integer type literal.
func IntegerType() PipelineType { return PipelineType{Kind: KindInteger} }

// LongType returns a Long type literal.
func LongType() PipelineType { return PipelineType{Kind: KindLong} }

// DoubleType returns a Double type literal.
func DoubleType() PipelineType { return PipelineType{Kind: KindDouble} }

// DecimalType returns a Decimal type literal.
func DecimalType() PipelineType { return PipelineType{Kind: KindDecimal} }

// StringType returns a String type literal.
func StringType() PipelineType { return PipelineType{Kind: KindString} }

// DateType returns a Date type literal.
func DateType() PipelineType { return PipelineType{Kind: KindDate} }

// TimestampType returns a Timestamp type literal.
func TimestampType() PipelineType { return PipelineType{Kind: KindTimestamp} }

// GeometryType returns a Geometry type literal.
func GeometryType() PipelineType { return PipelineType{Kind: KindGeometry} }

// ArrayOf builds an Array<inner> type.
func ArrayOf(inner PipelineType) PipelineType {
	return PipelineType{Kind: KindArray, Inner: &inner}
}

// StructOf builds a Struct type from a list of (name, type) pairs.
func StructOf(fields []StructField) PipelineType {
	out := make([]StructField, len(fields))
	copy(out, fields)
	return PipelineType{Kind: KindStruct, Fields: out}
}

// IsNumeric reports whether the type is in the numeric family.
func (t PipelineType) IsNumeric() bool {
	switch t.Kind {
	case KindInteger, KindLong, KindDouble, KindDecimal:
		return true
	}
	return false
}

// IsTextual reports whether the type is the String type.
func (t PipelineType) IsTextual() bool {
	return t.Kind == KindString
}

// IsTemporal reports whether the type is Date or Timestamp.
func (t PipelineType) IsTemporal() bool {
	return t.Kind == KindDate || t.Kind == KindTimestamp
}

// Equal compares two PipelineType values structurally.
func (t PipelineType) Equal(other PipelineType) bool {
	if t.Kind != other.Kind {
		return false
	}
	switch t.Kind {
	case KindArray:
		if t.Inner == nil || other.Inner == nil {
			return t.Inner == other.Inner
		}
		return t.Inner.Equal(*other.Inner)
	case KindStruct:
		if len(t.Fields) != len(other.Fields) {
			return false
		}
		for i := range t.Fields {
			if t.Fields[i].Name != other.Fields[i].Name {
				return false
			}
			if !t.Fields[i].Type.Equal(other.Fields[i].Type) {
				return false
			}
		}
		return true
	}
	return true
}

// numericRank mirrors the Rust helper. Booleans cannot silently widen
// to numerics — only `cast` does that. Cross-family promotion
// (numeric/string/date) is rejected.
func numericRank(t PipelineType) (uint8, bool) {
	switch t.Kind {
	case KindInteger:
		return 0, true
	case KindLong:
		return 1, true
	case KindDouble:
		return 2, true
	case KindDecimal:
		return 3, true
	}
	return 0, false
}

// CanPromote returns true when `from` can be implicitly promoted to
// `to`. A type is always promotable to itself.
func CanPromote(from, to PipelineType) bool {
	if from.Equal(to) {
		return true
	}
	if a, ok1 := numericRank(from); ok1 {
		if b, ok2 := numericRank(to); ok2 {
			return a <= b
		}
	}
	// Date silently widens to Timestamp (a Timestamp covers a Date with
	// the time component zeroed). The reverse requires an explicit cast.
	if from.Kind == KindDate && to.Kind == KindTimestamp {
		return true
	}
	// Array<T> promotes elementwise.
	if from.Kind == KindArray && to.Kind == KindArray && from.Inner != nil && to.Inner != nil {
		return CanPromote(*from.Inner, *to.Inner)
	}
	return false
}

// Promote computes the least upper bound of two types. Returns
// (zero, false) when no common supertype exists (i.e. the operation
// is a type error).
func Promote(left, right PipelineType) (PipelineType, bool) {
	if left.Equal(right) {
		return left, true
	}
	if a, ok1 := numericRank(left); ok1 {
		if b, ok2 := numericRank(right); ok2 {
			if a >= b {
				return left, true
			}
			return right, true
		}
	}
	if (left.Kind == KindDate && right.Kind == KindTimestamp) ||
		(left.Kind == KindTimestamp && right.Kind == KindDate) {
		return TimestampType(), true
	}
	if left.Kind == KindArray && right.Kind == KindArray && left.Inner != nil && right.Inner != nil {
		inner, ok := Promote(*left.Inner, *right.Inner)
		if !ok {
			return PipelineType{}, false
		}
		return ArrayOf(inner), true
	}
	return PipelineType{}, false
}

// MarshalJSON implements [json.Marshaler] producing the same on-wire
// shape as the Rust serde-tagged enum.
func (t PipelineType) MarshalJSON() ([]byte, error) {
	switch t.Kind {
	case KindArray:
		if t.Inner == nil {
			return nil, fmt.Errorf("pipelineexpression: ARRAY type missing inner")
		}
		return json.Marshal(struct {
			Kind  Kind         `json:"kind"`
			Inner PipelineType `json:"inner"`
		}{Kind: KindArray, Inner: *t.Inner})
	case KindStruct:
		// serde renders Vec<(String, PipelineType)> as a JSON array of
		// 2-element arrays.
		fields := make([][2]any, len(t.Fields))
		for i, f := range t.Fields {
			fields[i] = [2]any{f.Name, f.Type}
		}
		return json.Marshal(struct {
			Kind   Kind       `json:"kind"`
			Fields [][2]any   `json:"fields"`
		}{Kind: KindStruct, Fields: fields})
	default:
		return json.Marshal(struct {
			Kind Kind `json:"kind"`
		}{Kind: t.Kind})
	}
}

// UnmarshalJSON implements [json.Unmarshaler] accepting the same
// on-wire shape as the Rust serde-tagged enum.
func (t *PipelineType) UnmarshalJSON(data []byte) error {
	var head struct {
		Kind Kind `json:"kind"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	switch head.Kind {
	case KindBoolean, KindInteger, KindLong, KindDouble, KindDecimal,
		KindString, KindDate, KindTimestamp, KindGeometry:
		*t = PipelineType{Kind: head.Kind}
		return nil
	case KindArray:
		var v struct {
			Inner PipelineType `json:"inner"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*t = ArrayOf(v.Inner)
		return nil
	case KindStruct:
		var v struct {
			Fields []json.RawMessage `json:"fields"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		fields := make([]StructField, 0, len(v.Fields))
		for _, raw := range v.Fields {
			var pair [2]json.RawMessage
			if err := json.Unmarshal(raw, &pair); err != nil {
				return err
			}
			var name string
			if err := json.Unmarshal(pair[0], &name); err != nil {
				return err
			}
			var inner PipelineType
			if err := json.Unmarshal(pair[1], &inner); err != nil {
				return err
			}
			fields = append(fields, StructField{Name: name, Type: inner})
		}
		*t = StructOf(fields)
		return nil
	}
	return fmt.Errorf("pipelineexpression: unknown PipelineType kind %q", head.Kind)
}
