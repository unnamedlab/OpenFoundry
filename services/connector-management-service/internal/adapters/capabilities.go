package adapters

import (
	"sort"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

// CapabilitiesProvider is an optional adapter interface for declaring the
// capabilities implemented by an adapter without issuing live source calls.
type CapabilitiesProvider interface {
	Capabilities() models.ConnectorCapabilityMatrix
}

// CapabilityMatrix returns the per-connector runtime implementation matrix for
// the supplied catalog connector types. Registered adapters may override the
// default matrix by implementing CapabilitiesProvider; otherwise the matrix is
// derived from the centrally curated implementation inventory below.
func (r *Registry) CapabilityMatrix(catalogConnectorTypes []string) []models.ConnectorCapabilityMatrix {
	seen := map[string]struct{}{}
	for _, connectorType := range catalogConnectorTypes {
		if connectorType != "" {
			seen[connectorType] = struct{}{}
		}
	}
	for _, connectorType := range r.Names() {
		seen[connectorType] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]models.ConnectorCapabilityMatrix, 0, len(names))
	for _, name := range names {
		capability := DefaultCapabilities(name)
		if adapter, err := r.Lookup(name); err == nil {
			if provider, ok := adapter.(CapabilitiesProvider); ok {
				capability = provider.Capabilities()
				capability.ConnectorType = name
			}
		}
		out = append(out, capability)
	}
	return out
}

// DefaultCapabilities is the no-I/O implementation inventory used when a
// concrete adapter has not opted into CapabilitiesProvider yet. It is explicit
// so ErrNotImplemented skeletons remain visible to clients instead of being
// presented as successful capabilities.
func DefaultCapabilities(connectorType string) models.ConnectorCapabilityMatrix {
	capability := models.ConnectorCapabilityMatrix{ConnectorType: connectorType}
	switch connectorType {
	case "mysql":
		capability.DiscoverSources = true
		capability.QueryVirtualTable = true
		capability.StreamArrow = true
		capability.BuildIngestSpec = true
	case "oracle":
		capability.DiscoverSources = true
		capability.QueryVirtualTable = true
		capability.Limitations = []string{"stream_arrow is not implemented", "build_ingest_spec is not implemented"}
	case "s3":
		capability.DiscoverSources = true
		capability.QueryVirtualTable = true
		capability.BuildIngestSpec = true
		capability.Limitations = []string{"stream_arrow is not implemented"}
	case "kafka":
		capability.DiscoverSources = true
		capability.QueryVirtualTable = true
		capability.Limitations = []string{"stream_arrow is not implemented", "build_ingest_spec is not implemented"}
	case "excel", "graphql", "ldap", "sftp":
		capability.Limitations = []string{"adapter scaffold returns ErrNotImplemented for discover_sources, query_virtual_table, stream_arrow, and build_ingest_spec"}
	default:
		capability.Limitations = []string{"runtime adapter capabilities are unknown until the adapter declares Capabilities"}
	}
	return capability
}
