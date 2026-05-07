package handler

import "net/http"

// Function-package surface absorbed from `ontology-functions-service`
// per ADR-0030. URL grid mirrors `lib.rs::build_router::functions_routes`.

func ListFunctionPackages(w http.ResponseWriter, _ *http.Request) { writeEmptyList(w) }
func GetFunctionAuthoringSurface(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"languages": []string{"typescript", "python"}})
}
func GetFunctionPackage(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, nil)
}
func ListFunctionPackageRuns(w http.ResponseWriter, _ *http.Request) { writeEmptyList(w) }
func GetFunctionPackageMetrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"runs": 0, "errors": 0})
}

func CreateFunctionPackage(w http.ResponseWriter, r *http.Request)   { notImplemented(w, r) }
func UpdateFunctionPackage(w http.ResponseWriter, r *http.Request)   { notImplemented(w, r) }
func DeleteFunctionPackage(w http.ResponseWriter, r *http.Request)   { notImplemented(w, r) }
func ValidateFunctionPackage(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func SimulateFunctionPackage(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
