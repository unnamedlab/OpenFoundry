package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	cedarauthzlocal "github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/cedarauthz"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/transactions"
)

// TransactionHandlers wires the transaction HTTP surface.
type TransactionHandlers struct {
	Service *transactions.Service
}

// OpenTransaction — POST /api/v1/media-sets/{rid}/transactions
func (h *TransactionHandlers) OpenTransaction(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	var body models.OpenTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	row, err := h.Service.Open(r.Context(), transactions.OpenInput{
		MediaSetRID: rid, Body: body, Claims: caller, AuditCtx: auditCtxFromRequest(caller, r),
	})
	if err != nil {
		writeTransactionError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

// CommitTransaction — POST /api/v1/transactions/{rid}/commit
func (h *TransactionHandlers) CommitTransaction(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	row, err := h.Service.Commit(r.Context(), transactions.CloseInput{
		RID: rid, Claims: caller, AuditCtx: auditCtxFromRequest(caller, r),
	})
	if err != nil {
		writeTransactionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// AbortTransaction — POST /api/v1/transactions/{rid}/abort
func (h *TransactionHandlers) AbortTransaction(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	row, err := h.Service.Abort(r.Context(), transactions.CloseInput{
		RID: rid, Claims: caller, AuditCtx: auditCtxFromRequest(caller, r),
	})
	if err != nil {
		writeTransactionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// ListTransactions — GET /api/v1/media-sets/{rid}/transactions
func (h *TransactionHandlers) ListTransactions(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	rows, err := h.Service.ListHistory(r.Context(), transactions.ListInput{
		MediaSetRID: rid, Claims: caller,
	})
	if err != nil {
		writeTransactionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.TransactionHistoryEntry]{Items: rows})
}

// writeTransactionError maps service errors to HTTP codes. Conflict
// → 409, transactionless → 422, terminal-state → 422, bad request →
// 400, not found → 404, Cedar deny → 403.
func writeTransactionError(w http.ResponseWriter, err error) {
	var bad *transactions.ErrBadRequest
	var notFound *transactions.ErrNotFound
	var transactionless *transactions.ErrTransactionless
	var terminal *transactions.ErrTransactionTerminal
	var conflict *transactions.ErrTransactionConflict
	var forbidden *cedarauthzlocal.ErrForbidden
	switch {
	case errors.As(err, &bad):
		writeJSONErr(w, http.StatusBadRequest, bad.Msg)
	case errors.As(err, &notFound):
		writeJSONErr(w, http.StatusNotFound, notFound.Error())
	case errors.As(err, &transactionless):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": transactionless.Error(),
			"code":  "MEDIA_SET_TRANSACTIONLESS",
		})
	case errors.As(err, &terminal):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": terminal.Error(),
			"code":  "MEDIA_TRANSACTION_TERMINAL",
		})
	case errors.As(err, &conflict):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": conflict.Error(),
			"code":  "MEDIA_TRANSACTION_CONFLICT",
		})
	case errors.As(err, &forbidden):
		writeJSONErr(w, http.StatusForbidden, forbidden.Error())
	default:
		slog.Error("transaction handler", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
	}
}
