package contracts

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/domain"
)

func TestPlanCipherAwareJoin(t *testing.T) {
	key := uuid.New()
	left := ColumnEncryptionMetadata{ColumnName: "a", CipherKeyID: key, Algorithm: domain.AlgorithmAES256SIV}
	right := ColumnEncryptionMetadata{ColumnName: "b", CipherKeyID: key, Algorithm: domain.AlgorithmAES256SIV}
	if got := PlanCipherAwareJoin(left, right, true); got.Mode != "ciphertext_join" || !got.CiphertextPushdown {
		t.Fatalf("same SIV key should push down ciphertext join: %+v", got)
	}
	if got := PlanCipherAwareJoin(left, right, false); got.Mode != "decrypt_then_join" || !got.AuditRequired || !got.RateAccounting {
		t.Fatalf("missing permission should force audited decrypt join: %+v", got)
	}
	right.CipherKeyID = uuid.New()
	if got := PlanCipherAwareJoin(left, right, true); got.Mode != "decrypt_then_join" {
		t.Fatalf("different keys should not ciphertext-join: %+v", got)
	}
}

func TestColumnMetadataAndDecryptOnRead(t *testing.T) {
	key := uuid.New()
	schema := DatasetSchema{DatasetRID: "ri.dataset.main.x", Columns: []ColumnEncryptionMetadata{{ColumnName: "ssn", CipherKeyID: key, Algorithm: domain.AlgorithmAES256SIV}}}
	if err := ValidateColumnEncryptionMetadata(schema); err != nil {
		t.Fatalf("valid metadata rejected: %v", err)
	}
	plan := PlanDecryptOnRead(schema, DecryptOnReadRequest{SelectedColumns: []string{"ssn"}, PermittedColumns: map[string]bool{"ssn": true}})
	if !plan.Streaming || len(plan.Columns) != 1 || plan.Columns[0].Mode != "streaming_decrypt" {
		t.Fatalf("permitted decrypt-on-read should stream: %+v", plan)
	}
	plan = PlanDecryptOnRead(schema, DecryptOnReadRequest{PermittedColumns: map[string]bool{"ssn": true}, RestrictedEncryptedOnly: map[string]bool{"ssn": true}})
	if plan.Columns[0].Mode != "encrypted_only" {
		t.Fatalf("restricted view must pin encrypted mode: %+v", plan)
	}
}

func TestObjectPropertyEncryptionPlans(t *testing.T) {
	meta := ObjectPropertyEncryptionMetadata{ObjectType: "Person", PropertyName: "ssn", CipherKeyID: uuid.New(), Algorithm: domain.AlgorithmAES256SIV}
	if got := PlanObjectPropertySearch(meta, "exact_match"); got.Mode != "ciphertext_exact_match" {
		t.Fatalf("SIV exact-match should search ciphertext: %+v", got)
	}
	if got := PlanObjectPropertySearch(meta, "contains"); got.Mode != "ignore_property" {
		t.Fatalf("non exact match should ignore property: %+v", got)
	}
	if got := PlanActionValidation(true); got != "decrypt_then_validate" {
		t.Fatalf("permitted actor validation = %s", got)
	}
	if got := PlanActionValidation(false); got != "ciphertext_predicate_validation" {
		t.Fatalf("non-permitted actor validation = %s", got)
	}
}

func TestPlanRewrapSchedule(t *testing.T) {
	oldKey := uuid.New()
	newKey := uuid.New()
	plan := PlanRewrapSchedule(oldKey, newKey, []RewrapTarget{{Type: RewrapDatasetColumn, ResourceRID: "ri.dataset.main.sales", Field: "customer_id"}})
	if plan.Mode != "scheduled" || len(plan.Targets) != 1 || plan.Targets[0].OldKeyID != oldKey || plan.Targets[0].NewKeyID != newKey {
		t.Fatalf("rotation rewrap should schedule target with both key ids: %+v", plan)
	}
	blocked := PlanRewrapSchedule(oldKey, oldKey, []RewrapTarget{{Type: RewrapObjectProperty, ResourceRID: "ri.ontology.main.object.Person", Field: "ssn"}})
	if blocked.Mode != "blocked" {
		t.Fatalf("same-key rewrap must be blocked: %+v", blocked)
	}
}
