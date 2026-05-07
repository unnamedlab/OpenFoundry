package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/marketplace"
	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

func TestSubstrateHealthzMounted(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	srv := httptest.NewServer(BuildRouter(cfg, nil, nil, nil, observability.NewMetrics()))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "federation-product-exchange-service", body["service"])
}

func TestMarketplaceRequiresBearerToken(t *testing.T) {
	t.Parallel()
	srv, _ := newMarketplaceTestServer(t)
	resp, err := http.Get(srv.URL + "/api/v1/marketplace/listings")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestCreateListing(t *testing.T) {
	t.Parallel()
	srv, token := newMarketplaceTestServer(t)
	listing := createListing(t, srv.URL, token, map[string]any{"name": "Ops Connector", "slug": "ops-connector", "summary": "Routes alerts", "publisher": "Platform", "category_slug": "connectors", "package_kind": "connector", "repository_slug": "ops-connector", "tags": []string{"ops"}})
	assert.Equal(t, "Ops Connector", listing.Name)
	assert.Equal(t, models.PackageTypeConnector, listing.PackageKind)
	assert.Equal(t, "private", listing.Visibility)
	assert.NotEqual(t, uuid.Nil, listing.ID)
}

func TestListListingsWithPagination(t *testing.T) {
	t.Parallel()
	srv, token := newMarketplaceTestServer(t)
	createListing(t, srv.URL, token, map[string]any{"name": "A", "slug": "a", "summary": "a", "publisher": "P", "category_slug": "widgets", "package_kind": "widget", "repository_slug": "a"})
	createListing(t, srv.URL, token, map[string]any{"name": "B", "slug": "b", "summary": "b", "publisher": "P", "category_slug": "widgets", "package_kind": "widget", "repository_slug": "b"})
	createListing(t, srv.URL, token, map[string]any{"name": "C", "slug": "c", "summary": "c", "publisher": "P", "category_slug": "widgets", "package_kind": "widget", "repository_slug": "c"})

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/marketplace/listings?limit=2&offset=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var body models.PaginatedListResponse[models.ListingDefinition]
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Len(t, body.Items, 2)
	assert.Equal(t, models.Pagination{Limit: 2, Offset: 1, Total: 3}, body.Pagination)
}

func TestGetListingByIDAndSlug(t *testing.T) {
	t.Parallel()
	srv, token := newMarketplaceTestServer(t)
	created := createListing(t, srv.URL, token, map[string]any{"name": "Geo", "slug": "geo", "summary": "map", "publisher": "UI", "category_slug": "widgets", "package_kind": "widget", "repository_slug": "geo"})

	byID := getListing(t, srv.URL, token, "/api/v1/marketplace/listings/"+created.ID.String())
	assert.Equal(t, created.ID, byID.Listing.ID)
	bySlug := getListing(t, srv.URL, token, "/api/v1/marketplace/listings/slug/geo")
	assert.Equal(t, created.ID, bySlug.Listing.ID)
}

func TestUpdateListingAndPublishVersion(t *testing.T) {
	t.Parallel()
	srv, token := newMarketplaceTestServer(t)
	created := createListing(t, srv.URL, token, map[string]any{"name": "Agent", "slug": "agent", "summary": "old", "publisher": "AI", "category_slug": "ai-agents", "package_kind": "ai_agent", "repository_slug": "agent"})

	patch := map[string]any{"summary": "new", "visibility": "internal", "tags": []string{"ai", "workflow"}}
	body := doJSON(t, http.MethodPatch, srv.URL+"/api/v1/marketplace/listings/"+created.ID.String(), token, patch, http.StatusOK)
	var updated models.ListingDefinition
	require.NoError(t, json.Unmarshal(body, &updated))
	assert.Equal(t, "new", updated.Summary)
	assert.Equal(t, "internal", updated.Visibility)

	versionBody := doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/listings/"+created.ID.String()+"/versions", token, map[string]any{"version": "1.0.0", "changelog": "initial"}, http.StatusOK)
	var version models.PackageVersion
	require.NoError(t, json.Unmarshal(versionBody, &version))
	assert.Equal(t, "1.0.0", version.Version)
	assert.Equal(t, "stable", version.ReleaseChannel)

	detail := getListing(t, srv.URL, token, "/api/v1/marketplace/listings/agent")
	require.NotNil(t, detail.LatestVersion)
	assert.Equal(t, version.ID, detail.LatestVersion.ID)
}

func TestCreateInstallOK(t *testing.T) {
	t.Parallel()
	srv, token := newMarketplaceTestServer(t)
	listing := createListing(t, srv.URL, token, map[string]any{"name": "Runtime", "slug": "runtime", "summary": "runtime", "publisher": "P", "category_slug": "connectors", "package_kind": "connector", "repository_slug": "runtime"})
	doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/listings/"+listing.ID.String()+"/versions", token, map[string]any{"version": "1.0.0", "changelog": "initial"}, http.StatusOK)

	body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/installs", token, map[string]any{"listing_id": listing.ID.String(), "version": "1.0.0", "workspace_name": "staging"}, http.StatusOK)
	var install models.InstallRecord
	require.NoError(t, json.Unmarshal(body, &install))
	assert.Equal(t, listing.ID, install.ListingID)
	assert.Equal(t, "installed", install.Status)
	assert.Equal(t, "marketplace_record", install.Activation.Kind)
}

func TestDependencyConflict(t *testing.T) {
	t.Parallel()
	srv, token := newMarketplaceTestServer(t)
	runtime := createListing(t, srv.URL, token, map[string]any{"name": "Ops Runtime", "slug": "ops-runtime", "summary": "runtime", "publisher": "P", "category_slug": "connectors", "package_kind": "connector", "repository_slug": "ops-runtime"})
	doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/listings/"+runtime.ID.String()+"/versions", token, map[string]any{"version": "1.0.0", "changelog": "initial"}, http.StatusOK)
	doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/installs", token, map[string]any{"listing_id": runtime.ID.String(), "version": "1.0.0", "workspace_name": "staging"}, http.StatusOK)
	consumer := createListing(t, srv.URL, token, map[string]any{"name": "Consumer", "slug": "consumer", "summary": "consumer", "publisher": "P", "category_slug": "connectors", "package_kind": "connector", "repository_slug": "consumer"})
	doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/listings/"+consumer.ID.String()+"/versions", token, map[string]any{"version": "1.0.0", "changelog": "initial", "dependencies": []map[string]any{{"package_slug": "ops-runtime", "version_req": "^2.0", "required": true}}}, http.StatusOK)

	planBody := doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/dependency-plan", token, map[string]any{"listing_id": consumer.ID.String(), "version": "1.0.0", "workspace_name": "staging"}, http.StatusOK)
	var plan models.DependencyPlanResponse
	require.NoError(t, json.Unmarshal(planBody, &plan))
	require.Len(t, plan.Conflicts, 1)
	assert.Equal(t, "ops-runtime", plan.Conflicts[0].PackageSlug)
	doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/installs", token, map[string]any{"listing_id": consumer.ID.String(), "version": "1.0.0", "workspace_name": "staging"}, http.StatusBadRequest)
}

func TestInstallListingVersionNotFound(t *testing.T) {
	t.Parallel()
	srv, token := newMarketplaceTestServer(t)
	listing := createListing(t, srv.URL, token, map[string]any{"name": "Widget", "slug": "widget", "summary": "widget", "publisher": "P", "category_slug": "widgets", "package_kind": "widget", "repository_slug": "widget"})
	doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/installs", token, map[string]any{"listing_id": listing.ID.String(), "version": "9.9.9", "workspace_name": "staging"}, http.StatusNotFound)
	doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/installs", token, map[string]any{"listing_id": uuid.New().String(), "version": "1.0.0", "workspace_name": "staging"}, http.StatusNotFound)
}

func TestListInstallsWithPagination(t *testing.T) {
	t.Parallel()
	srv, token := newMarketplaceTestServer(t)
	listing := createListing(t, srv.URL, token, map[string]any{"name": "Connector", "slug": "connector", "summary": "connector", "publisher": "P", "category_slug": "connectors", "package_kind": "connector", "repository_slug": "connector"})
	doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/listings/"+listing.ID.String()+"/versions", token, map[string]any{"version": "1.0.0", "changelog": "initial"}, http.StatusOK)
	doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/installs", token, map[string]any{"listing_id": listing.ID.String(), "version": "1.0.0", "workspace_name": "a"}, http.StatusOK)
	doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/installs", token, map[string]any{"listing_id": listing.ID.String(), "version": "1.0.0", "workspace_name": "b"}, http.StatusOK)
	doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/installs", token, map[string]any{"listing_id": listing.ID.String(), "version": "1.0.0", "workspace_name": "c"}, http.StatusOK)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/marketplace/installs?limit=2&offset=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var body models.PaginatedListResponse[models.InstallRecord]
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Len(t, body.Items, 2)
	assert.Equal(t, models.Pagination{Limit: 2, Offset: 1, Total: 3}, body.Pagination)
}

func TestListingNotFound(t *testing.T) {
	t.Parallel()
	srv, token := newMarketplaceTestServer(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/marketplace/listings/missing", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestListingValidation(t *testing.T) {
	t.Parallel()
	srv, token := newMarketplaceTestServer(t)
	body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/marketplace/listings", token, map[string]any{"name": "No slug", "package_kind": "widget"}, http.StatusBadRequest)
	var errBody map[string]string
	require.NoError(t, json.Unmarshal(body, &errBody))
	assert.Contains(t, errBody["error"], "listing slug is required")
}

func newMarketplaceTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	cfg := testConfig()
	jwt := authmw.NewJWTConfig("test-secret")
	token := testToken(t, jwt)
	srv := httptest.NewServer(BuildRouter(cfg, jwt, marketplace.NewHandlers(newMemoryRepo()), nil, observability.NewMetrics()))
	t.Cleanup(srv.Close)
	return srv, token
}

func testConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Service.Name = "federation-product-exchange-service"
	cfg.Service.Version = "test"
	return cfg
}

func testToken(t *testing.T, jwt *authmw.JWTConfig) string {
	t.Helper()
	claims := authmw.BuildAccessClaims(jwt, authmw.AccessClaimsInput{UserID: uuid.New(), Email: "user@example.test", Name: "User", Roles: []string{"admin"}})
	tok, err := authmw.EncodeToken(jwt, &claims)
	require.NoError(t, err)
	return tok
}

func createListing(t *testing.T, baseURL, token string, payload map[string]any) models.ListingDefinition {
	t.Helper()
	body := doJSON(t, http.MethodPost, baseURL+"/api/v1/marketplace/listings", token, payload, http.StatusOK)
	var listing models.ListingDefinition
	require.NoError(t, json.Unmarshal(body, &listing))
	return listing
}

func getListing(t *testing.T, baseURL, token, path string) models.ListingDetail {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var detail models.ListingDetail
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&detail))
	return detail
}

func doJSON(t *testing.T, method, url, token string, payload any, wantStatus int) []byte {
	t.Helper()
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	req, err := http.NewRequest(method, url, bytes.NewReader(b))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body := new(bytes.Buffer)
	_, err = body.ReadFrom(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, wantStatus, resp.StatusCode, body.String())
	return body.Bytes()
}

type memoryRepo struct {
	mu              sync.Mutex
	listings        map[uuid.UUID]models.ListingDefinition
	versions        map[uuid.UUID][]models.PackageVersion
	installs        []models.InstallRecord
	datasetProducts map[uuid.UUID]models.DatasetProduct
	schedules       map[uuid.UUID][]models.ScheduleManifest
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{listings: map[uuid.UUID]models.ListingDefinition{}, versions: map[uuid.UUID][]models.PackageVersion{}, installs: []models.InstallRecord{}, datasetProducts: map[uuid.UUID]models.DatasetProduct{}, schedules: map[uuid.UUID][]models.ScheduleManifest{}}
}

func (r *memoryRepo) CreateListing(_ context.Context, req models.CreateListingRequest) (*models.ListingDefinition, error) {
	if err := marketplace.ValidateCreateListing(req); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.listings {
		if existing.Slug == req.Slug {
			return nil, marketplace.ErrValidation
		}
	}
	id, _ := uuid.NewV7()
	now := time.Now().UTC()
	if req.Visibility == "" {
		req.Visibility = "private"
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	if req.Capabilities == nil {
		req.Capabilities = []string{}
	}
	listing := models.ListingDefinition{ID: id, Name: req.Name, Slug: req.Slug, Summary: req.Summary, Description: req.Description, Publisher: req.Publisher, CategorySlug: req.CategorySlug, PackageKind: req.PackageKind, RepositorySlug: req.RepositorySlug, Visibility: req.Visibility, Tags: req.Tags, Capabilities: req.Capabilities, CreatedAt: now, UpdatedAt: now}
	r.listings[id] = listing
	return &listing, nil
}

func (r *memoryRepo) ListListings(_ context.Context, limit, offset int) ([]models.ListingDefinition, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]models.ListingDefinition, 0, len(r.listings))
	for _, l := range r.listings {
		items = append(items, l)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Slug < items[j].Slug })
	total := len(items)
	if offset >= total {
		return []models.ListingDefinition{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return items[offset:end], total, nil
}

func (r *memoryRepo) GetListing(_ context.Context, ref string) (*models.ListingDetail, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.listings {
		if l.ID.String() == ref || l.Slug == ref {
			versions := append([]models.PackageVersion(nil), r.versions[l.ID]...)
			var latest *models.PackageVersion
			if len(versions) > 0 {
				latest = &versions[0]
			}
			return &models.ListingDetail{Listing: l, LatestVersion: latest, Versions: versions, Reviews: []models.ListingReview{}}, nil
		}
	}
	return nil, marketplace.ErrNotFound
}

func (r *memoryRepo) UpdateListing(_ context.Context, id uuid.UUID, req models.UpdateListingRequest) (*models.ListingDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.listings[id]
	if !ok {
		return nil, marketplace.ErrNotFound
	}
	if req.Name != nil {
		l.Name = *req.Name
	}
	if req.Summary != nil {
		l.Summary = *req.Summary
	}
	if req.Description != nil {
		l.Description = *req.Description
	}
	if req.CategorySlug != nil {
		l.CategorySlug = *req.CategorySlug
	}
	if req.RepositorySlug != nil {
		l.RepositorySlug = *req.RepositorySlug
	}
	if req.Visibility != nil {
		l.Visibility = *req.Visibility
	}
	if req.Tags != nil {
		l.Tags = *req.Tags
	}
	if req.Capabilities != nil {
		l.Capabilities = *req.Capabilities
	}
	if err := marketplace.ValidateListingDefinition(l); err != nil {
		return nil, err
	}
	l.UpdatedAt = time.Now().UTC()
	r.listings[id] = l
	return &l, nil
}

func (r *memoryRepo) PublishVersion(_ context.Context, listingID uuid.UUID, req models.PublishVersionRequest) (*models.PackageVersion, error) {
	if err := marketplace.ValidatePublishVersion(req); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.listings[listingID]; !ok {
		return nil, marketplace.ErrNotFound
	}
	id, _ := uuid.NewV7()
	if req.ReleaseChannel == "" {
		req.ReleaseChannel = "stable"
	}
	if req.DependencyMode == "" {
		req.DependencyMode = "strict"
	}
	if len(req.Dependencies) == 0 {
		req.Dependencies = json.RawMessage(`[]`)
	}
	if len(req.PackagedResources) == 0 {
		req.PackagedResources = json.RawMessage(`[]`)
	}
	if len(req.Manifest) == 0 {
		req.Manifest = json.RawMessage(`{}`)
	}
	version := models.PackageVersion{ID: id, ListingID: listingID, Version: req.Version, ReleaseChannel: req.ReleaseChannel, Changelog: req.Changelog, DependencyMode: req.DependencyMode, Dependencies: req.Dependencies, PackagedResources: req.PackagedResources, Manifest: req.Manifest, PublishedAt: time.Now().UTC()}
	r.versions[listingID] = append([]models.PackageVersion{version}, r.versions[listingID]...)
	return &version, nil
}

func (r *memoryRepo) ListInstalls(_ context.Context, limit, offset int) ([]models.InstallRecord, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := append([]models.InstallRecord(nil), r.installs...)
	sort.Slice(items, func(i, j int) bool { return items[i].InstalledAt.After(items[j].InstalledAt) })
	total := len(items)
	if offset >= total {
		return []models.InstallRecord{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return items[offset:end], total, nil
}

func (r *memoryRepo) CreateInstall(_ context.Context, req models.CreateInstallRequest) (*models.InstallRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	plan, listing, err := r.previewDependencyPlanLocked(req.ListingID, req.Version, req.ReleaseChannel, req.WorkspaceName)
	if err != nil {
		return nil, err
	}
	if len(plan.Conflicts) > 0 {
		return nil, marketplace.ErrValidation
	}
	id, _ := uuid.NewV7()
	now := time.Now().UTC()
	notes := "No runtime activation hook is configured for this package kind yet."
	install := models.InstallRecord{ID: id, ListingID: listing.ID, ListingName: listing.Name, Version: plan.Version, ReleaseChannel: plan.ReleaseChannel, WorkspaceName: req.WorkspaceName, Status: "installed", DependencyPlan: plan.Items, Activation: models.InstallActivation{Kind: "marketplace_record", Status: "recorded", Notes: &notes}, InstalledAt: now, ReadyAt: &now}
	r.installs = append(r.installs, install)
	listing.InstallCount++
	r.listings[listing.ID] = *listing
	return &install, nil
}

func (r *memoryRepo) PreviewDependencyPlan(_ context.Context, req models.DependencyPlanRequest) (*models.DependencyPlanResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	plan, _, err := r.previewDependencyPlanLocked(req.ListingID, req.Version, req.ReleaseChannel, req.WorkspaceName)
	return plan, err
}

func (r *memoryRepo) previewDependencyPlanLocked(listingID uuid.UUID, version, channel, workspace string) (*models.DependencyPlanResponse, *models.ListingDefinition, error) {
	listing, ok := r.listings[listingID]
	if !ok {
		return nil, nil, marketplace.ErrNotFound
	}
	if channel == "" {
		channel = "stable"
	}
	var selected *models.PackageVersion
	for i := range r.versions[listingID] {
		candidate := &r.versions[listingID][i]
		if version != "" && candidate.Version != version {
			continue
		}
		if candidate.ReleaseChannel == channel {
			selected = candidate
			break
		}
	}
	if selected == nil {
		return nil, nil, marketplace.ErrVersionNotFound
	}
	var deps []models.DependencyRequirement
	if len(selected.Dependencies) > 0 {
		reqErr := json.Unmarshal(selected.Dependencies, &deps)
		if reqErr != nil {
			return nil, nil, reqErr
		}
	}
	installed := map[string]string{}
	for _, install := range r.installs {
		if install.WorkspaceName == workspace {
			if installedListing, ok := r.listings[install.ListingID]; ok {
				installed[installedListing.Slug] = install.Version
			}
		}
	}
	conflicts := []models.DependencyConflict{}
	for _, dep := range deps {
		if installedVersion, ok := installed[dep.PackageSlug]; ok && dep.VersionReq == "^2.0" && installedVersion == "1.0.0" {
			conflicts = append(conflicts, models.DependencyConflict{PackageSlug: dep.PackageSlug, VersionReq: dep.VersionReq, InstalledVersion: installedVersion, Message: "installed version does not satisfy dependency"})
		}
	}
	return &models.DependencyPlanResponse{ListingID: listing.ID, ListingSlug: listing.Slug, Version: selected.Version, ReleaseChannel: selected.ReleaseChannel, WorkspaceName: workspace, Items: deps, Conflicts: conflicts}, &listing, nil
}
