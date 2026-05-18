// Package handler — kernels.go proxies the gateway-side /api/kernels
// surface. The frontend uses these to introspect / drop kernels on the
// upstream jupyter/kernel-gateway when admin operations are needed; the
// happy path goes through CreateGatewaySession + ExecuteGatewayCell.
package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// CreateKernel — POST /api/v1/kernels  body: {"spec":"python3"}
// Proxies to gateway POST /api/kernels.
func (s *State) CreateKernel(w http.ResponseWriter, r *http.Request) {
	if claims := requireClaims(w, r); claims == nil {
		return
	}
	if s.KernelGW == nil {
		writeJSON(w, http.StatusServiceUnavailable, errBody("kernel gateway is not configured"))
		return
	}
	var body struct {
		Spec string `json:"spec"`
	}
	_ = decodeJSON(r, &body) // empty body is fine — gateway picks default
	k, err := s.KernelGW.CreateKernel(r.Context(), body.Spec)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, k)
}

// ListKernels — GET /api/v1/kernels → gateway GET /api/kernels.
func (s *State) ListKernels(w http.ResponseWriter, r *http.Request) {
	if claims := requireClaims(w, r); claims == nil {
		return
	}
	if s.KernelGW == nil {
		writeJSON(w, http.StatusServiceUnavailable, errBody("kernel gateway is not configured"))
		return
	}
	out, err := s.KernelGW.ListKernels(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

// DeleteKernel — DELETE /api/v1/kernels/{kernel_id}.
func (s *State) DeleteKernel(w http.ResponseWriter, r *http.Request) {
	if claims := requireClaims(w, r); claims == nil {
		return
	}
	if s.KernelGW == nil {
		writeJSON(w, http.StatusServiceUnavailable, errBody("kernel gateway is not configured"))
		return
	}
	kid := chi.URLParam(r, "kernel_id")
	if kid == "" {
		writeJSON(w, http.StatusBadRequest, errBody("missing kernel id"))
		return
	}
	if err := s.KernelGW.DeleteKernel(r.Context(), kid); err != nil {
		writeJSON(w, http.StatusBadGateway, errBody(err.Error()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
