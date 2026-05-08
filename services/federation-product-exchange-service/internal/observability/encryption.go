package observability

import (
	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

// Posture ports `domain::encryption::posture` 1:1.
//
// Profile defaults to "mutual-tls+envelope" when no contract is provided;
// key_version defaults to "key/pending"; encrypted-* default to true. The
// recommendation is "rotation current" when both encrypted_in_transit and
// encrypted_at_rest are true, otherwise "provision key rotation before
// replication".
func Posture(
	share *models.SharedDataset,
	contract *models.SharingContract,
	syncStatus *models.SyncStatus,
) models.EncryptionPosture {
	profile := "mutual-tls+envelope"
	if contract != nil {
		profile = contract.EncryptionProfile
	}

	keyVersion := "key/pending"
	encryptedInTransit := true
	encryptedAtRest := true
	if syncStatus != nil {
		keyVersion = syncStatus.KeyVersion
		encryptedInTransit = syncStatus.EncryptedInTransit
		encryptedAtRest = syncStatus.EncryptedAtRest
	}

	recommendation := "provision key rotation before replication"
	if encryptedInTransit && encryptedAtRest {
		recommendation = "rotation current"
	}

	transportCipher := "disabled"
	if encryptedInTransit {
		transportCipher = "TLS 1.3 mTLS"
	}
	atRestCipher := "disabled"
	if encryptedAtRest {
		atRestCipher = "AES-256-GCM envelope"
	}

	return models.EncryptionPosture{
		ShareID:            share.ID,
		TransportCipher:    transportCipher,
		AtRestCipher:       atRestCipher,
		KeyVersion:         keyVersion,
		Profile:            profile,
		EncryptedInTransit: encryptedInTransit,
		EncryptedAtRest:    encryptedAtRest,
		Recommendation:     recommendation,
	}
}
