package handler

import "net/http"

// Rule-engine surface absorbed from `ontology-security-service` per
// ADR-0030. URL grid mirrors `lib.rs::build_router::rules_routes`.

func ListRules(w http.ResponseWriter, _ *http.Request) { writeEmptyList(w) }
func GetMachineryInsights(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"insights": []any{}})
}
func GetMachineryQueue(w http.ResponseWriter, _ *http.Request) { writeEmptyList(w) }
func GetRule(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, nil)
}
func ListRulesForObjectType(w http.ResponseWriter, _ *http.Request) { writeEmptyList(w) }
func ListObjectRuleRuns(w http.ResponseWriter, _ *http.Request)     { writeEmptyList(w) }

func CreateRule(w http.ResponseWriter, r *http.Request)              { notImplemented(w, r) }
func UpdateRule(w http.ResponseWriter, r *http.Request)              { notImplemented(w, r) }
func DeleteRule(w http.ResponseWriter, r *http.Request)              { notImplemented(w, r) }
func SimulateRule(w http.ResponseWriter, r *http.Request)            { notImplemented(w, r) }
func ApplyRule(w http.ResponseWriter, r *http.Request)               { notImplemented(w, r) }
func UpdateMachineryQueueItem(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
