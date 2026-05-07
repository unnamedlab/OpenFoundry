package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// EffectivePropertyDefinition mirrors `struct EffectivePropertyDefinition`
// in `libs/ontology-kernel/src/domain/schema.rs`. The `source` field
// identifies which precedence layer contributed the definition.
type EffectivePropertyDefinition struct {
	Name             string          `json:"name"`
	DisplayName      string          `json:"display_name"`
	Description      string          `json:"description"`
	PropertyType     string          `json:"property_type"`
	Required         bool            `json:"required"`
	UniqueConstraint bool            `json:"unique_constraint"`
	TimeDependent    bool            `json:"time_dependent"`
	DefaultValue     json.RawMessage `json:"default_value"`
	ValidationRules  json.RawMessage `json:"validation_rules"`
	Source           string          `json:"source"`
}

// Precedence constants mirror the per-source layering in schema.rs:
// shared < interface < direct. Higher precedence wins on conflict.
const (
	SharedPropertyPrecedence    uint8 = 0
	InterfacePropertyPrecedence uint8 = 1
	DirectPropertyPrecedence    uint8 = 2
)

// effectivePropertyEntry pairs a precedence with its definition while
// merging — mirrors the Rust `(u8, EffectivePropertyDefinition)` tuple.
type effectivePropertyEntry struct {
	precedence uint8
	definition EffectivePropertyDefinition
}

// MergeEffectiveDefinitions mirrors `merge_effective_definitions`. On
// duplicate `name` the existing entry wins iff its precedence is `>=`
// the candidate; otherwise the candidate replaces it. Output ordering
// is lexicographic by name (Rust uses `BTreeMap<String, _>`, which Go
// reproduces by sorting the collected map keys before flattening).
func MergeEffectiveDefinitions(entries []effectivePropertyEntry) []EffectivePropertyDefinition {
	merged := map[string]effectivePropertyEntry{}
	for _, entry := range entries {
		existing, ok := merged[entry.definition.Name]
		if ok && existing.precedence >= entry.precedence {
			continue
		}
		merged[entry.definition.Name] = entry
	}
	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]EffectivePropertyDefinition, 0, len(merged))
	for _, name := range names {
		out = append(out, merged[name].definition)
	}
	return out
}

// LoadEffectiveProperties mirrors `load_effective_properties`. The
// three SQL queries are byte-identical to the Rust source so the same
// migrations and indexes back both ports during the migration window.
func LoadEffectiveProperties(ctx context.Context, db *pgxpool.Pool, objectTypeID uuid.UUID) ([]EffectivePropertyDefinition, error) {
	shared, err := loadSharedProperties(ctx, db, objectTypeID)
	if err != nil {
		return nil, err
	}
	direct, err := loadDirectProperties(ctx, db, objectTypeID)
	if err != nil {
		return nil, err
	}
	interfaces, err := loadInterfaceProperties(ctx, db, objectTypeID)
	if err != nil {
		return nil, err
	}

	entries := make([]effectivePropertyEntry, 0, len(shared)+len(interfaces)+len(direct))
	for _, p := range shared {
		entries = append(entries, effectivePropertyEntry{
			precedence: SharedPropertyPrecedence,
			definition: EffectivePropertyDefinition{
				Name:             p.Name,
				DisplayName:      p.DisplayName,
				Description:      p.Description,
				PropertyType:     p.PropertyType,
				Required:         p.Required,
				UniqueConstraint: p.UniqueConstraint,
				TimeDependent:    p.TimeDependent,
				DefaultValue:     p.DefaultValue,
				ValidationRules:  p.ValidationRules,
				Source:           "shared_property_type",
			},
		})
	}
	for _, p := range interfaces {
		entries = append(entries, effectivePropertyEntry{
			precedence: InterfacePropertyPrecedence,
			definition: EffectivePropertyDefinition{
				Name:             p.Name,
				DisplayName:      p.DisplayName,
				Description:      p.Description,
				PropertyType:     p.PropertyType,
				Required:         p.Required,
				UniqueConstraint: p.UniqueConstraint,
				TimeDependent:    p.TimeDependent,
				DefaultValue:     p.DefaultValue,
				ValidationRules:  p.ValidationRules,
				Source:           "interface",
			},
		})
	}
	for _, p := range direct {
		entries = append(entries, effectivePropertyEntry{
			precedence: DirectPropertyPrecedence,
			definition: EffectivePropertyDefinition{
				Name:             p.Name,
				DisplayName:      p.DisplayName,
				Description:      p.Description,
				PropertyType:     p.PropertyType,
				Required:         p.Required,
				UniqueConstraint: p.UniqueConstraint,
				TimeDependent:    p.TimeDependent,
				DefaultValue:     p.DefaultValue,
				ValidationRules:  p.ValidationRules,
				Source:           "object_type",
			},
		})
	}
	return MergeEffectiveDefinitions(entries), nil
}

// SQL strings are kept as package-level constants so test code can
// assert on byte-level shape if needed.
const (
	sharedPropertiesSQL = `SELECT spt.id, spt.name, spt.display_name, spt.description, spt.property_type,
                  spt.required, spt.unique_constraint, spt.time_dependent, spt.default_value,
                  spt.validation_rules, spt.owner_id, spt.created_at, spt.updated_at
           FROM shared_property_types spt
           INNER JOIN object_type_shared_property_types otsp
                ON otsp.shared_property_type_id = spt.id
           WHERE otsp.object_type_id = $1
           ORDER BY otsp.created_at ASC, spt.created_at ASC`

	directPropertiesSQL = `SELECT id, object_type_id, name, display_name, description, property_type, required,
                  unique_constraint, time_dependent, default_value, validation_rules,
                  inline_edit_config, created_at, updated_at
           FROM properties
           WHERE object_type_id = $1
           ORDER BY created_at ASC`

	interfacePropertiesSQL = `SELECT ip.id, ip.interface_id, ip.name, ip.display_name, ip.description, ip.property_type,
                  ip.required, ip.unique_constraint, ip.time_dependent, ip.default_value,
                  ip.validation_rules, ip.created_at, ip.updated_at
           FROM interface_properties ip
           INNER JOIN object_type_interfaces oti ON oti.interface_id = ip.interface_id
           WHERE oti.object_type_id = $1
           ORDER BY ip.created_at ASC`
)

func loadSharedProperties(ctx context.Context, db *pgxpool.Pool, objectTypeID uuid.UUID) ([]models.SharedPropertyType, error) {
	rows, err := db.Query(ctx, sharedPropertiesSQL, objectTypeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.SharedPropertyType
	for rows.Next() {
		var p models.SharedPropertyType
		if err := rows.Scan(
			&p.ID, &p.Name, &p.DisplayName, &p.Description, &p.PropertyType,
			&p.Required, &p.UniqueConstraint, &p.TimeDependent, &p.DefaultValue,
			&p.ValidationRules, &p.OwnerID, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func loadDirectProperties(ctx context.Context, db *pgxpool.Pool, objectTypeID uuid.UUID) ([]models.Property, error) {
	rows, err := db.Query(ctx, directPropertiesSQL, objectTypeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Property
	for rows.Next() {
		var (
			p         models.Property
			inlineRaw []byte
		)
		if err := rows.Scan(
			&p.ID, &p.ObjectTypeID, &p.Name, &p.DisplayName, &p.Description, &p.PropertyType,
			&p.Required, &p.UniqueConstraint, &p.TimeDependent, &p.DefaultValue, &p.ValidationRules,
			&inlineRaw, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(inlineRaw) > 0 {
			var cfg models.PropertyInlineEditConfig
			if err := json.Unmarshal(inlineRaw, &cfg); err == nil {
				p.InlineEditConfig = &cfg
			}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func loadInterfaceProperties(ctx context.Context, db *pgxpool.Pool, objectTypeID uuid.UUID) ([]models.InterfaceProperty, error) {
	rows, err := db.Query(ctx, interfacePropertiesSQL, objectTypeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.InterfaceProperty
	for rows.Next() {
		var p models.InterfaceProperty
		if err := rows.Scan(
			&p.ID, &p.InterfaceID, &p.Name, &p.DisplayName, &p.Description, &p.PropertyType,
			&p.Required, &p.UniqueConstraint, &p.TimeDependent, &p.DefaultValue, &p.ValidationRules,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ValidateObjectProperties mirrors `validate_object_properties`. The
// input must be a JSON object; unknown keys reject; required keys
// missing reject; each present value is validated by
// [ValidatePropertyValue]. Returns the normalized object (only known
// keys, with `default_value` injected for absent non-required keys).
func ValidateObjectProperties(definitions []EffectivePropertyDefinition, properties json.RawMessage) (json.RawMessage, error) {
	var input map[string]json.RawMessage
	if err := json.Unmarshal(properties, &input); err != nil || input == nil {
		return nil, fmt.Errorf("object properties must be a JSON object")
	}
	known := map[string]bool{}
	for _, d := range definitions {
		known[d.Name] = true
	}
	// Reject unknown keys in deterministic (sorted) order so error
	// messages are stable across runs.
	unknownKeys := make([]string, 0)
	for key := range input {
		if !known[key] {
			unknownKeys = append(unknownKeys, key)
		}
	}
	sort.Strings(unknownKeys)
	if len(unknownKeys) > 0 {
		return nil, fmt.Errorf("unknown property '%s'", unknownKeys[0])
	}

	normalized := map[string]json.RawMessage{}
	for _, d := range definitions {
		value, present := input[d.Name]
		if !present && len(d.DefaultValue) > 0 {
			value = d.DefaultValue
			present = true
		}
		if !present {
			if d.Required {
				return nil, fmt.Errorf("%s is required", d.Name)
			}
			continue
		}
		if err := ValidatePropertyValue(d.PropertyType, value); err != nil {
			return nil, fmt.Errorf("%s: %s", d.Name, err.Error())
		}
		normalized[d.Name] = value
	}
	return json.Marshal(normalized)
}
