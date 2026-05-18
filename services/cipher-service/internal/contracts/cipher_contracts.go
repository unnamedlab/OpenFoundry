package contracts

import (
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/domain"
)

// ColumnEncryptionMetadata is the dataset schema extension used by CIP.11.
type ColumnEncryptionMetadata struct {
	ColumnName  string           `json:"column_name"`
	CipherKeyID uuid.UUID        `json:"cipher_key_id"`
	Algorithm   domain.Algorithm `json:"algorithm"`
}

// DatasetSchema captures the cipher-relevant subset of a dataset schema.
type DatasetSchema struct {
	DatasetRID string                     `json:"dataset_rid"`
	Columns    []ColumnEncryptionMetadata `json:"columns"`
}

// ValidateColumnEncryptionMetadata enforces CIP.11's writer contract: an
// encrypted column must declare both a cipher key and a supported algorithm.
func ValidateColumnEncryptionMetadata(schema DatasetSchema) error {
	for _, col := range schema.Columns {
		if col.ColumnName == "" {
			return errors.New("cipher column metadata: column_name is required")
		}
		if col.CipherKeyID == uuid.Nil {
			return fmt.Errorf("cipher column metadata: %s missing cipher_key_id", col.ColumnName)
		}
		if !col.Algorithm.SupportsCipherKeyResource() {
			return fmt.Errorf("cipher column metadata: %s has unsupported algorithm %q", col.ColumnName, col.Algorithm)
		}
	}
	return nil
}

// JoinPlan is the CIP.10 planner contract shared by Pipeline Builder and OQL.
type JoinPlan struct {
	Mode               string `json:"mode"`
	Reason             string `json:"reason,omitempty"`
	AuditRequired      bool   `json:"audit_required"`
	RateAccounting     bool   `json:"rate_accounting"`
	CiphertextPushdown bool   `json:"ciphertext_pushdown"`
}

// PlanCipherAwareJoin permits ciphertext comparison only when both sides use
// the same deterministic SIV key and the caller is decrypt-authorized.
func PlanCipherAwareJoin(left, right ColumnEncryptionMetadata, callerCanDecrypt bool) JoinPlan {
	if left.CipherKeyID == right.CipherKeyID && left.CipherKeyID != uuid.Nil && left.Algorithm == domain.AlgorithmAES256SIV && right.Algorithm == domain.AlgorithmAES256SIV && callerCanDecrypt {
		return JoinPlan{Mode: "ciphertext_join", CiphertextPushdown: true}
	}
	return JoinPlan{Mode: "decrypt_then_join", Reason: "requires same AES_256_SIV key and decrypt permission", AuditRequired: true, RateAccounting: true}
}

// DecryptOnReadRequest is the CIP.12 non-materializing view request.
type DecryptOnReadRequest struct {
	SelectedColumns         []string        `json:"selected_columns"`
	PermittedColumns        map[string]bool `json:"permitted_columns"`
	RestrictedEncryptedOnly map[string]bool `json:"restricted_encrypted_only"`
}

type DecryptOnReadColumnPlan struct {
	ColumnName string `json:"column_name"`
	Mode       string `json:"mode"`
}

type DecryptOnReadPlan struct {
	Streaming bool                      `json:"streaming"`
	Columns   []DecryptOnReadColumnPlan `json:"columns"`
}

// PlanDecryptOnRead never materializes plaintext; it labels selected columns
// for streaming decrypt only when permissions and restricted-view rules allow.
func PlanDecryptOnRead(schema DatasetSchema, req DecryptOnReadRequest) DecryptOnReadPlan {
	selected := map[string]bool{}
	for _, col := range req.SelectedColumns {
		selected[col] = true
	}
	all := len(selected) == 0
	plan := DecryptOnReadPlan{Streaming: true}
	for _, col := range schema.Columns {
		if !all && !selected[col.ColumnName] {
			continue
		}
		mode := "encrypted_only"
		if req.PermittedColumns[col.ColumnName] && !req.RestrictedEncryptedOnly[col.ColumnName] {
			mode = "streaming_decrypt"
		}
		plan.Columns = append(plan.Columns, DecryptOnReadColumnPlan{ColumnName: col.ColumnName, Mode: mode})
	}
	return plan
}

// ObjectPropertyEncryptionMetadata is the CIP.13 property-definition extension.
type ObjectPropertyEncryptionMetadata struct {
	ObjectType   string           `json:"object_type"`
	PropertyName string           `json:"property_name"`
	CipherKeyID  uuid.UUID        `json:"cipher_key_id"`
	Algorithm    domain.Algorithm `json:"algorithm"`
}

type ObjectPropertySearchPlan struct {
	Mode   string `json:"mode"`
	Reason string `json:"reason,omitempty"`
}

// PlanObjectPropertySearch captures CIP.13: search ignores encrypted
// properties except exact-match predicates over SIV-encrypted properties.
func PlanObjectPropertySearch(meta ObjectPropertyEncryptionMetadata, predicate string) ObjectPropertySearchPlan {
	if meta.CipherKeyID != uuid.Nil && meta.Algorithm == domain.AlgorithmAES256SIV && predicate == "exact_match" {
		return ObjectPropertySearchPlan{Mode: "ciphertext_exact_match"}
	}
	return ObjectPropertySearchPlan{Mode: "ignore_property", Reason: "encrypted property search requires AES_256_SIV and exact_match"}
}

// PlanActionValidation states whether action validation may use decrypted values.
func PlanActionValidation(actorCanDecrypt bool) string {
	if actorCanDecrypt {
		return "decrypt_then_validate"
	}
	return "ciphertext_predicate_validation"
}

// RewrapTargetType identifies the encrypted surface a background rotation job
// needs to rewrite after a successor key is provisioned.
type RewrapTargetType string

const (
	RewrapDatasetColumn  RewrapTargetType = "dataset_column"
	RewrapObjectProperty RewrapTargetType = "object_property"
)

// RewrapTarget is the CIP.16 unit of background work. The job reads existing
// envelopes under OldKeyID and writes new envelopes under NewKeyID without ever
// materializing a durable plaintext copy.
type RewrapTarget struct {
	Type        RewrapTargetType `json:"type"`
	ResourceRID string           `json:"resource_rid"`
	Field       string           `json:"field"`
	OldKeyID    uuid.UUID        `json:"old_key_id"`
	NewKeyID    uuid.UUID        `json:"new_key_id"`
}

// RewrapSchedulePlan is intentionally small: orchestrators can persist it as a
// queue item, while services that only need planning can validate target/key
// relationships without importing the handler or repo packages.
type RewrapSchedulePlan struct {
	Mode    string         `json:"mode"`
	Targets []RewrapTarget `json:"targets"`
	Reason  string         `json:"reason,omitempty"`
}

// PlanRewrapSchedule keeps old envelopes decryptable until all dataset columns
// and object properties have been re-encrypted to the successor key.
func PlanRewrapSchedule(oldKeyID, newKeyID uuid.UUID, targets []RewrapTarget) RewrapSchedulePlan {
	if oldKeyID == uuid.Nil || newKeyID == uuid.Nil || oldKeyID == newKeyID {
		return RewrapSchedulePlan{Mode: "blocked", Reason: "rotation requires distinct old and new key ids"}
	}
	out := make([]RewrapTarget, 0, len(targets))
	for _, target := range targets {
		if target.ResourceRID == "" || target.Field == "" {
			return RewrapSchedulePlan{Mode: "blocked", Reason: "rewrap targets require resource_rid and field"}
		}
		target.OldKeyID = oldKeyID
		target.NewKeyID = newKeyID
		out = append(out, target)
	}
	return RewrapSchedulePlan{Mode: "scheduled", Targets: out}
}
