package marketplace

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

func (h *Handlers) GetOverview(w http.ResponseWriter, r *http.Request) {
	listings, _, err := h.Repo.ListListings(r.Context(), 100, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	categories := featuredCategories(listings)
	featured := listings
	if len(featured) > 3 {
		featured = featured[:3]
	}
	var totalInstalls int64
	for _, listing := range listings {
		totalInstalls += listing.InstallCount
	}
	writeJSON(w, http.StatusOK, models.MarketplaceOverview{ListingCount: len(listings), CategoryCount: len(categories), Featured: featured, TotalInstalls: totalInstalls})
}

func (h *Handlers) ListCategories(w http.ResponseWriter, r *http.Request) {
	listings, _, err := h.Repo.ListListings(r.Context(), 100, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.CategoryDefinition]{Items: featuredCategories(listings)})
}

func (h *Handlers) ListListingsEnvelope(w http.ResponseWriter, r *http.Request) {
	items, _, err := h.Repo.ListListings(r.Context(), 100, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.ListingDefinition]{Items: items})
}

func (h *Handlers) SearchListings(w http.ResponseWriter, r *http.Request) {
	items, _, err := h.Repo.ListListings(r.Context(), 100, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	query := r.URL.Query().Get("q")
	if query == "" {
		query = "widget"
	}
	category := r.URL.Query().Get("category")
	results := []models.SearchResult{}
	for _, listing := range items {
		if category != "" && category != listing.CategorySlug {
			continue
		}
		score := scoreListing(listing, query)
		if score > 0.45 {
			results = append(results, models.SearchResult{Listing: listing, Score: score})
		}
	}
	writeJSON(w, http.StatusOK, models.SearchResponse{Query: query, Results: results})
}

func (h *Handlers) ListVersions(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	versions, err := h.Repo.ListVersions(r.Context(), id)
	if err != nil {
		handleRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.PackageVersion]{Items: versions})
}

func (h *Handlers) IncludeActionInProduct(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	var req models.IncludeActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	version, err := h.Repo.IncludeActionInProduct(r.Context(), id, req)
	if err != nil {
		handleRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, version)
}

func (h *Handlers) CreateDatasetProduct(w http.ResponseWriter, r *http.Request) {
	var req models.CreateDatasetProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	product, err := h.Repo.CreateDatasetProduct(r.Context(), chi.URLParam(r, "rid"), req)
	if err != nil {
		handleRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, product)
}

func (h *Handlers) GetDatasetProduct(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	product, err := h.Repo.GetDatasetProduct(r.Context(), id)
	if err != nil {
		handleRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, product)
}

func (h *Handlers) InstallDatasetProduct(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	var req models.InstallDatasetProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	install, err := h.Repo.InstallDatasetProduct(r.Context(), id, req)
	if err != nil {
		handleRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, install)
}

func (h *Handlers) AddScheduleManifest(w http.ResponseWriter, r *http.Request) {
	var req models.AddScheduleManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := h.Repo.AddScheduleManifest(r.Context(), req)
	if err != nil {
		handleRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handlers) MaterialiseInstallSchedules(w http.ResponseWriter, r *http.Request) {
	var req models.InstallSchedulesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := h.Repo.MaterialiseInstallSchedules(r.Context(), req)
	if err != nil {
		handleRepoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func featuredCategories(listings []models.ListingDefinition) []models.CategoryDefinition {
	categories := []models.CategoryDefinition{
		{Slug: "connectors", Name: "Connectors", Description: "Operational and SaaS integrations"},
		{Slug: "transforms", Name: "Transforms", Description: "Reusable data transformation packages"},
		{Slug: "widgets", Name: "Widgets", Description: "UI widgets and dashboards"},
		{Slug: "templates", Name: "App Templates", Description: "Starter apps and composition templates"},
		{Slug: "ml-models", Name: "ML Models", Description: "Packaged models and inference adapters"},
		{Slug: "ai-agents", Name: "AI Agents", Description: "Agent workflows and copilots"},
	}
	for i := range categories {
		for _, listing := range listings {
			if listing.CategorySlug == categories[i].Slug {
				categories[i].ListingCount++
			}
		}
	}
	return categories
}

func scoreListing(listing models.ListingDefinition, query string) float64 {
	q := strings.ToLower(query)
	score := 0.4
	if strings.Contains(strings.ToLower(listing.Name), q) {
		score += 0.25
	}
	if strings.Contains(strings.ToLower(listing.Summary), q) {
		score += 0.2
	}
	for _, tag := range listing.Tags {
		if strings.Contains(strings.ToLower(tag), q) {
			score += 0.1
			break
		}
	}
	return math.Min(score+listing.AverageRating/10.0, 0.99)
}
