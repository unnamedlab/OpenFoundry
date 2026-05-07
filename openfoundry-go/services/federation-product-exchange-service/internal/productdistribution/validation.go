package productdistribution

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

var (
	ErrNotFound   = errors.New("product distribution resource not found")
	ErrValidation = errors.New("product distribution validation failed")
)

const authenticatedPeerStatus = "authenticated"

func ValidateCreatePeer(req models.CreatePeerRequest) error {
	if strings.TrimSpace(req.Slug) == "" || strings.TrimSpace(req.DisplayName) == "" {
		return fmt.Errorf("%w: peer slug and display name are required", ErrValidation)
	}
	if strings.TrimSpace(req.Region) == "" {
		return fmt.Errorf("%w: peer region is required", ErrValidation)
	}
	if strings.TrimSpace(req.EndpointURL) == "" {
		return fmt.Errorf("%w: peer endpoint_url is required", ErrValidation)
	}
	if strings.TrimSpace(req.AuthMode) == "" {
		return fmt.Errorf("%w: peer auth_mode is required", ErrValidation)
	}
	if strings.TrimSpace(req.TrustLevel) == "" {
		return fmt.Errorf("%w: peer trust_level is required", ErrValidation)
	}
	if strings.TrimSpace(req.PublicKeyFingerprint) == "" {
		return fmt.Errorf("%w: peer public_key_fingerprint is required", ErrValidation)
	}
	return nil
}

func ValidateCreateShare(req models.CreateShareRequest, contract sharingContract, provider, consumer models.PeerOrganization, now time.Time) error {
	if strings.TrimSpace(req.DatasetName) == "" {
		return fmt.Errorf("%w: dataset name is required", ErrValidation)
	}
	if provider.ID == consumer.ID {
		return fmt.Errorf("%w: provider and consumer peers must differ", ErrValidation)
	}
	if contract.PeerID != provider.ID {
		return fmt.Errorf("%w: contract peer must match the provider peer", ErrValidation)
	}
	if contract.Status != "active" || contract.ExpiresAt.Before(now) || contract.ExpiresAt.Equal(now) {
		return fmt.Errorf("%w: shares can only be created from an active, unexpired contract", ErrValidation)
	}
	if provider.Status != authenticatedPeerStatus {
		return fmt.Errorf("%w: provider peer must be authenticated", ErrValidation)
	}
	if consumer.Status != authenticatedPeerStatus {
		return fmt.Errorf("%w: consumer peer must be authenticated", ErrValidation)
	}
	if !replicationCompatible(req.ReplicationMode, contract.ReplicationMode) {
		return fmt.Errorf("%w: unsupported replication mode '%s' for contract mode '%s'", ErrValidation, req.ReplicationMode, contract.ReplicationMode)
	}
	return nil
}

func replicationCompatible(shareMode, contractMode string) bool {
	shareRank, ok := replicationRank(shareMode)
	if !ok {
		return false
	}
	contractRank, ok := replicationRank(contractMode)
	return ok && shareRank <= contractRank
}

func replicationRank(mode string) (int, bool) {
	switch mode {
	case "query_only":
		return 0, true
	case "snapshot":
		return 1, true
	case "incremental_replication":
		return 2, true
	case "continuous":
		return 3, true
	default:
		return 0, false
	}
}
