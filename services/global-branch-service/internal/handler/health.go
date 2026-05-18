// Package handler hosts the HTTP handlers for the global-branch-service.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/openfoundry/openfoundry-go/libs/core-models/health"
)

// ServiceName is the canonical name used in /healthz payloads.
const ServiceName = "global-branch-service"

// Milestone marks the parity milestone tracked in
// docs/migration/foundry-global-branching-1to1-checklist.md.
const Milestone = "GB"

// Health handles GET /healthz with the standard liveness payload.
func Health(serviceName, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(serviceName, version))
	}
}
