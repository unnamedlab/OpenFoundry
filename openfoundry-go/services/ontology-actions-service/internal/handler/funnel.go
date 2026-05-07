package handler

import "net/http"

// Funnel + storage surface absorbed from `ontology-funnel-service`
// per ADR-0030. URL grid mirrors `lib.rs::build_router::funnel_routes`.

func GetFunnelHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "sources": []any{}})
}
func GetStorageInsights(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"insights": []any{}, "total": 0})
}
func ListFunnelSources(w http.ResponseWriter, _ *http.Request) { writeEmptyList(w) }
func GetFunnelSource(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, nil)
}
func GetFunnelSourceHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "unknown"})
}
func ListFunnelRuns(w http.ResponseWriter, _ *http.Request) { writeEmptyList(w) }
func GetFunnelRun(w http.ResponseWriter, _ *http.Request)   { writeJSON(w, http.StatusNotFound, nil) }

func CreateFunnelSource(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func UpdateFunnelSource(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func DeleteFunnelSource(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func TriggerFunnelRun(w http.ResponseWriter, r *http.Request)   { notImplemented(w, r) }
