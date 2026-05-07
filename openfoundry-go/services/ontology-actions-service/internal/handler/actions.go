package handler

import "net/http"

// Action / inline-edit / what-if surface.
//
// Mirrors the URL grid in `services/ontology-actions-service/src/lib.rs`,
// section "actions". GETs return empty envelopes; mutating endpoints
// return 501 until the kernel handler slice is ported.

func ListActionTypes(w http.ResponseWriter, _ *http.Request) { writeEmptyList(w) }
func GetActionType(w http.ResponseWriter, _ *http.Request)   { writeJSON(w, http.StatusNotFound, nil) }
func GetActionMetrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"action_id": nil, "metrics": []any{}})
}
func ListActionWhatIfBranches(w http.ResponseWriter, _ *http.Request) { writeEmptyList(w) }
func ListApplicableActions(w http.ResponseWriter, _ *http.Request)    { writeEmptyList(w) }

func CreateActionType(w http.ResponseWriter, r *http.Request)         { notImplemented(w, r) }
func UpdateActionType(w http.ResponseWriter, r *http.Request)         { notImplemented(w, r) }
func DeleteActionType(w http.ResponseWriter, r *http.Request)         { notImplemented(w, r) }
func ValidateAction(w http.ResponseWriter, r *http.Request)           { notImplemented(w, r) }
func ExecuteAction(w http.ResponseWriter, r *http.Request)            { notImplemented(w, r) }
func ExecuteActionBatch(w http.ResponseWriter, r *http.Request)       { notImplemented(w, r) }
func CreateActionWhatIfBranch(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func DeleteActionWhatIfBranch(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func ExecuteInlineEdit(w http.ResponseWriter, r *http.Request)        { notImplemented(w, r) }
func ExecuteInlineEditBatch(w http.ResponseWriter, r *http.Request)   { notImplemented(w, r) }
func UploadActionAttachment(w http.ResponseWriter, r *http.Request)   { notImplemented(w, r) }
