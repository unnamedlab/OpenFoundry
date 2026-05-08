// Composition helpers — link CRUD primitives shared by the actions,
// links and indexer handlers. Mirrors
// `libs/ontology-kernel/src/domain/composition.rs`.
package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// stableLinkNamespace mirrors `Uuid::NAMESPACE_OID` (RFC 4122 OID
// namespace) used by the Rust impl. Hard-coded so the Go output is
// byte-identical to the Rust UUIDv5 derivation.
var stableLinkNamespace = uuid.NameSpaceOID

// StableLinkID mirrors `pub fn stable_link_id`. Material is the same
// `openfoundry/ontology-link/<lt>/<from>/<to>` string the Rust
// version hashes through UUIDv5 → SHA-1.
func StableLinkID(linkType storage.LinkTypeId, from, to storage.ObjectId) uuid.UUID {
	material := "openfoundry/ontology-link/" + string(linkType) + "/" + string(from) + "/" + string(to)
	return uuid.NewSHA1(stableLinkNamespace, []byte(material))
}

// ── CompositionError variants (mirror the Rust enum) ─────────────────

// ErrEmptyTenant / ErrEmptyLinkType / ErrEmptyEndpoint / ErrSelfLoop
// mirror the Rust `CompositionError` variants used by `create_link`.
var (
	ErrEmptyTenant   = errors.New("tenant must not be empty")
	ErrEmptyLinkType = errors.New("link_type must not be empty")
	ErrEmptyEndpoint = errors.New("link endpoint must not be empty")
	ErrSelfLoop      = errors.New("link source and target must differ (no self-loops)")
)

// CreateLink mirrors `pub async fn create_link`. Idempotent on the
// (tenant, link_type, from, to) tuple; returns (false, nil) when the
// LinkStore already carries the same triple.
//
// Validation cascade:
//
//   - Tenant / link_type / endpoints must be non-empty (matches Rust).
//   - from == to triggers ErrSelfLoop.
//   - We probe `ListOutgoing` first so the caller can distinguish
//     "newly inserted" from "already present" without a separate
//     existence check; the LinkStore's `Put` is idempotent so the
//     second store call is a no-op when the row already lands.
func CreateLink(
	ctx context.Context,
	store storage.LinkStore,
	tenant storage.TenantId,
	linkType storage.LinkTypeId,
	from, to storage.ObjectId,
	payload json.RawMessage,
	createdAtMs int64,
) (bool, error) {
	if strings.TrimSpace(string(tenant)) == "" {
		return false, ErrEmptyTenant
	}
	if strings.TrimSpace(string(linkType)) == "" {
		return false, ErrEmptyLinkType
	}
	if strings.TrimSpace(string(from)) == "" || strings.TrimSpace(string(to)) == "" {
		return false, ErrEmptyEndpoint
	}
	if from == to {
		return false, ErrSelfLoop
	}

	existing, err := store.ListOutgoing(ctx, tenant, linkType, from,
		storage.Page{Size: 256}, storage.Strong())
	if err != nil {
		return false, fmt.Errorf("failed to scan link store: %w", err)
	}
	for _, link := range existing.Items {
		if link.To == to {
			return false, nil // already present
		}
	}

	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if err := store.Put(ctx, storage.Link{
		Tenant:      tenant,
		LinkType:    linkType,
		From:        from,
		To:          to,
		Payload:     payload,
		CreatedAtMs: createdAtMs,
	}); err != nil {
		return false, fmt.Errorf("failed to write link: %w", err)
	}
	return true, nil
}

// DeleteLink mirrors the symmetric helper in the Rust crate's
// composition module. Returns `(false, nil)` when the row was
// already absent (idempotent delete).
func DeleteLink(
	ctx context.Context,
	store storage.LinkStore,
	tenant storage.TenantId,
	linkType storage.LinkTypeId,
	from, to storage.ObjectId,
) (bool, error) {
	if strings.TrimSpace(string(tenant)) == "" {
		return false, ErrEmptyTenant
	}
	deleted, err := store.Delete(ctx, tenant, linkType, from, to)
	if err != nil {
		return false, fmt.Errorf("failed to delete link: %w", err)
	}
	return deleted, nil
}
