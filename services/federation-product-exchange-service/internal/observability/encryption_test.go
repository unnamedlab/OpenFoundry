package observability

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

func TestPosture_DefaultsWhenNoContractOrStatus(t *testing.T) {
	share := models.SharedDataset{ID: uuid.New()}
	got := Posture(&share, nil, nil)
	if got.Profile != "mutual-tls+envelope" {
		t.Fatalf("profile default: %q", got.Profile)
	}
	if got.KeyVersion != "key/pending" {
		t.Fatalf("key_version default: %q", got.KeyVersion)
	}
	if !got.EncryptedInTransit || !got.EncryptedAtRest {
		t.Fatalf("encryption defaults should be true: %+v", got)
	}
	if got.TransportCipher != "TLS 1.3 mTLS" {
		t.Fatalf("transport_cipher: %q", got.TransportCipher)
	}
	if got.AtRestCipher != "AES-256-GCM envelope" {
		t.Fatalf("at_rest_cipher: %q", got.AtRestCipher)
	}
	if got.Recommendation != "rotation current" {
		t.Fatalf("recommendation: %q", got.Recommendation)
	}
	if got.ShareID != share.ID {
		t.Fatalf("share_id not propagated")
	}
}

func TestPosture_DisabledCiphersAndRecommendation(t *testing.T) {
	share := models.SharedDataset{ID: uuid.New()}
	status := models.SyncStatus{
		ShareID:            share.ID,
		KeyVersion:         "key/v3",
		EncryptedInTransit: false,
		EncryptedAtRest:    true,
	}
	got := Posture(&share, nil, &status)
	if got.TransportCipher != "disabled" {
		t.Fatalf("transport_cipher: want disabled, got %q", got.TransportCipher)
	}
	if got.AtRestCipher != "AES-256-GCM envelope" {
		t.Fatalf("at_rest_cipher: %q", got.AtRestCipher)
	}
	if got.Recommendation != "provision key rotation before replication" {
		t.Fatalf("recommendation: %q", got.Recommendation)
	}
	if got.KeyVersion != "key/v3" {
		t.Fatalf("key_version: %q", got.KeyVersion)
	}
}

func TestPosture_BothDisabled(t *testing.T) {
	share := models.SharedDataset{ID: uuid.New()}
	status := models.SyncStatus{ShareID: share.ID, EncryptedInTransit: false, EncryptedAtRest: false, KeyVersion: "key/v0"}
	got := Posture(&share, nil, &status)
	if got.TransportCipher != "disabled" || got.AtRestCipher != "disabled" {
		t.Fatalf("ciphers not disabled: %+v", got)
	}
	if got.Recommendation != "provision key rotation before replication" {
		t.Fatalf("recommendation: %q", got.Recommendation)
	}
}

func TestPosture_ContractProfileOverridesDefault(t *testing.T) {
	share := models.SharedDataset{ID: uuid.New()}
	contract := models.SharingContract{EncryptionProfile: "custom-profile"}
	got := Posture(&share, &contract, nil)
	if got.Profile != "custom-profile" {
		t.Fatalf("profile: want custom-profile, got %q", got.Profile)
	}
}
