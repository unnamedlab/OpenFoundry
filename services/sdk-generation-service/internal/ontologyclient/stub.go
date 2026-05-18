package ontologyclient

import (
	"context"

	"github.com/google/uuid"
)

// StubClient returns a fixed snapshot regardless of (tenantID, version).
// Used in dev when ONTOLOGY_SERVICE_URL is unset, and in tests that
// want a deterministic input without spinning up the producer service.
//
// The shape mirrors a tiny but representative ontology — a Customer
// with a few primitive properties, an Order with a numeric total, a
// one-to-many link between them, and a single action — so the
// generator exercises every TS type-mapping branch.
type StubClient struct {
	// Snapshot, if set, overrides DefaultStubSnapshot. Tests use this
	// to inject custom inputs without standing up an HTTP fake.
	Snapshot *OntologySnapshot
}

// GetOntologySnapshot ignores tenant/version and returns the canned
// catalog. The Version field of the returned snapshot is rewritten to
// match the caller's request so the downstream generator sees the
// intended version label.
func (s *StubClient) GetOntologySnapshot(_ context.Context, _ uuid.UUID, version string) (*OntologySnapshot, error) {
	snap := s.Snapshot
	if snap == nil {
		s := DefaultStubSnapshot()
		snap = &s
	}
	clone := *snap
	clone.ObjectTypes = append([]OntologyObjectType(nil), snap.ObjectTypes...)
	clone.LinkTypes = append([]OntologyLinkType(nil), snap.LinkTypes...)
	clone.ActionTypes = append([]OntologyActionType(nil), snap.ActionTypes...)
	if version != "" {
		clone.Version = version
	}
	return &clone, nil
}

// DefaultStubSnapshot is the canned ontology used in dev + tests. It
// is exposed so the generator snapshot tests can pin their golden
// output to a stable input.
func DefaultStubSnapshot() OntologySnapshot {
	return OntologySnapshot{
		Version: "v0.0.0-stub",
		ObjectTypes: []OntologyObjectType{
			{
				Name:               "Customer",
				APIName:            "customer",
				DisplayName:        "Customer",
				Description:        "A customer of the platform.",
				PrimaryKeyProperty: "id",
				Properties: []OntologyProperty{
					{Name: "id", PropertyType: "string", Required: true},
					{Name: "email", PropertyType: "string", Required: true},
					{Name: "displayName", PropertyType: "string"},
					{Name: "age", PropertyType: "integer"},
					{Name: "active", PropertyType: "boolean", Required: true},
					{Name: "createdAt", PropertyType: "datetime", Required: true},
					{Name: "homeLocation", PropertyType: "geo_point"},
				},
			},
			{
				Name:               "Order",
				APIName:            "order",
				DisplayName:        "Order",
				PrimaryKeyProperty: "id",
				Properties: []OntologyProperty{
					{Name: "id", PropertyType: "string", Required: true},
					{Name: "total", PropertyType: "double", Required: true},
					{Name: "placedAt", PropertyType: "datetime", Required: true},
				},
			},
		},
		LinkTypes: []OntologyLinkType{
			{
				Name:             "CustomerOrders",
				APIName:          "customer_orders",
				SourceObjectType: "Customer",
				TargetObjectType: "Order",
				Cardinality:      "one_to_many",
				Label:            "orders",
				ReverseLabel:     "customer",
			},
		},
		ActionTypes: []OntologyActionType{
			{
				Name:        "PlaceOrder",
				APIName:     "place_order",
				DisplayName: "Place Order",
				Parameters: []OntologyActionParameter{
					{Name: "customerId", PropertyType: "string", Required: true},
					{Name: "total", PropertyType: "double", Required: true},
				},
			},
		},
	}
}
