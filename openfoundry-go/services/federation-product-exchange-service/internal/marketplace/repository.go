package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

var (
	ErrNotFound        = errors.New("marketplace listing not found")
	ErrVersionNotFound = errors.New("marketplace listing version not found")
	ErrValidation      = errors.New("marketplace validation failed")
)

type Repository interface {
	CreateListing(ctx context.Context, req models.CreateListingRequest) (*models.ListingDefinition, error)
	ListListings(ctx context.Context, limit, offset int) ([]models.ListingDefinition, int, error)
	GetListing(ctx context.Context, ref string) (*models.ListingDetail, error)
	UpdateListing(ctx context.Context, id uuid.UUID, req models.UpdateListingRequest) (*models.ListingDefinition, error)
	PublishVersion(ctx context.Context, listingID uuid.UUID, req models.PublishVersionRequest) (*models.PackageVersion, error)
	ListInstalls(ctx context.Context, limit, offset int) ([]models.InstallRecord, int, error)
	CreateInstall(ctx context.Context, req models.CreateInstallRequest) (*models.InstallRecord, error)
	PreviewDependencyPlan(ctx context.Context, req models.DependencyPlanRequest) (*models.DependencyPlanResponse, error)
}

type PGXRepository struct{ Pool *pgxpool.Pool }

func NewPGXRepository(pool *pgxpool.Pool) *PGXRepository { return &PGXRepository{Pool: pool} }

func (r *PGXRepository) CreateListing(ctx context.Context, req models.CreateListingRequest) (*models.ListingDefinition, error) {
	if err := ValidateCreateListing(req); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	if req.Visibility == "" {
		req.Visibility = "private"
	}
	tags := jsonArray(req.Tags)
	capabilities := jsonArray(req.Capabilities)
	row := r.Pool.QueryRow(ctx, `
INSERT INTO marketplace_listings (id, name, slug, summary, description, publisher, category_slug, package_kind, repository_slug, visibility, tags, capabilities, install_count, average_rating, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12::jsonb, 0, 0, $13, $14)
RETURNING id, name, slug, summary, description, publisher, category_slug, package_kind, repository_slug, visibility, tags, capabilities, install_count, average_rating, created_at, updated_at`,
		id, req.Name, req.Slug, req.Summary, req.Description, req.Publisher, req.CategorySlug, string(req.PackageKind), req.RepositorySlug, req.Visibility, tags, capabilities, now, now)
	listing, err := scanListing(row)
	if err != nil {
		return nil, mapPGError(err)
	}
	return listing, nil
}

func (r *PGXRepository) ListListings(ctx context.Context, limit, offset int) ([]models.ListingDefinition, int, error) {
	rows, err := r.Pool.Query(ctx, `
SELECT id, name, slug, summary, description, publisher, category_slug, package_kind, repository_slug, visibility, tags, capabilities, install_count, average_rating, created_at, updated_at, count(*) OVER() AS total
FROM marketplace_listings
ORDER BY install_count DESC, average_rating DESC, updated_at DESC
LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []models.ListingDefinition{}
	total := 0
	for rows.Next() {
		listing, rowTotal, err := scanListingWithTotal(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *listing)
		total = rowTotal
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if total == 0 && offset > 0 {
		if err := r.Pool.QueryRow(ctx, `SELECT count(*) FROM marketplace_listings`).Scan(&total); err != nil {
			return nil, 0, err
		}
	}
	return items, total, nil
}

func (r *PGXRepository) GetListing(ctx context.Context, ref string) (*models.ListingDetail, error) {
	listing, err := r.getListingDefinition(ctx, ref)
	if err != nil {
		return nil, err
	}
	versions, err := r.listVersions(ctx, listing.ID)
	if err != nil {
		return nil, err
	}
	reviews, err := r.listReviews(ctx, listing.ID)
	if err != nil {
		return nil, err
	}
	var latest *models.PackageVersion
	if len(versions) > 0 {
		latest = &versions[0]
	}
	return &models.ListingDetail{Listing: *listing, LatestVersion: latest, Versions: versions, Reviews: reviews}, nil
}

func (r *PGXRepository) UpdateListing(ctx context.Context, id uuid.UUID, req models.UpdateListingRequest) (*models.ListingDefinition, error) {
	current, err := r.getListingDefinition(ctx, id.String())
	if err != nil {
		return nil, err
	}
	applyUpdate(current, req)
	if err := ValidateListingDefinition(*current); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	row := r.Pool.QueryRow(ctx, `
UPDATE marketplace_listings
SET name = $2, summary = $3, description = $4, category_slug = $5, repository_slug = $6, visibility = $7, tags = $8::jsonb, capabilities = $9::jsonb, updated_at = $10
WHERE id = $1
RETURNING id, name, slug, summary, description, publisher, category_slug, package_kind, repository_slug, visibility, tags, capabilities, install_count, average_rating, created_at, updated_at`,
		id, current.Name, current.Summary, current.Description, current.CategorySlug, current.RepositorySlug, current.Visibility, jsonArray(current.Tags), jsonArray(current.Capabilities), now)
	updated, err := scanListing(row)
	if err != nil {
		return nil, mapPGError(err)
	}
	return updated, nil
}

func (r *PGXRepository) PublishVersion(ctx context.Context, listingID uuid.UUID, req models.PublishVersionRequest) (*models.PackageVersion, error) {
	if err := ValidatePublishVersion(req); err != nil {
		return nil, err
	}
	if _, err := r.getListingDefinition(ctx, listingID.String()); err != nil {
		return nil, err
	}
	versionID, err := uuid.NewV7()
	if err != nil {
		versionID = uuid.New()
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
	if req.ReleaseChannel == "" {
		req.ReleaseChannel = "stable"
	}
	if req.DependencyMode == "" {
		req.DependencyMode = "strict"
	}
	publishedAt := time.Now().UTC()
	row := r.Pool.QueryRow(ctx, `
INSERT INTO marketplace_package_versions (id, listing_id, version, release_channel, changelog, dependency_mode, dependencies, packaged_resources, manifest, published_at)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9::jsonb, $10)
RETURNING id, listing_id, version, release_channel, changelog, dependency_mode, dependencies, packaged_resources, manifest, published_at`,
		versionID, listingID, req.Version, req.ReleaseChannel, req.Changelog, req.DependencyMode, req.Dependencies, req.PackagedResources, req.Manifest, publishedAt)
	version, err := scanVersion(row)
	if err != nil {
		return nil, mapPGError(err)
	}
	return version, nil
}

func (r *PGXRepository) ListInstalls(ctx context.Context, limit, offset int) ([]models.InstallRecord, int, error) {
	rows, err := r.Pool.Query(ctx, `
SELECT installs.id, installs.listing_id, installs.listing_name, installs.version, installs.release_channel,
       installs.workspace_name, installs.status, installs.dependency_plan, installs.activation,
       installs.fleet_id, fleets.name AS fleet_name, installs.maintenance_window,
       installs.auto_upgrade_enabled, installs.enrollment_branch, installs.installed_at,
       installs.ready_at, count(*) OVER() AS total
FROM marketplace_installs installs
LEFT JOIN marketplace_product_fleets fleets ON fleets.id = installs.fleet_id
ORDER BY installs.installed_at DESC
LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []models.InstallRecord{}
	total := 0
	for rows.Next() {
		install, rowTotal, err := scanInstallWithTotal(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *install)
		total = rowTotal
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if total == 0 && offset > 0 {
		if err := r.Pool.QueryRow(ctx, `SELECT count(*) FROM marketplace_installs`).Scan(&total); err != nil {
			return nil, 0, err
		}
	}
	return items, total, nil
}

func (r *PGXRepository) CreateInstall(ctx context.Context, req models.CreateInstallRequest) (*models.InstallRecord, error) {
	if strings.TrimSpace(req.WorkspaceName) == "" {
		return nil, fmt.Errorf("%w: workspace name is required", ErrValidation)
	}
	plan, err := r.PreviewDependencyPlan(ctx, models.DependencyPlanRequest{ListingID: req.ListingID, Version: req.Version, WorkspaceName: req.WorkspaceName, ReleaseChannel: req.ReleaseChannel})
	if err != nil {
		return nil, err
	}
	if len(plan.Conflicts) > 0 {
		return nil, fmt.Errorf("%w: dependency conflict: %s", ErrValidation, plan.Conflicts[0].Message)
	}
	listing, err := r.getListingDefinition(ctx, req.ListingID.String())
	if err != nil {
		return nil, err
	}
	deps, _ := json.Marshal(plan.Items)
	activation, _ := json.Marshal(defaultActivation())
	maintenance := json.RawMessage(`{}`)
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	now := time.Now().UTC()
	row := r.Pool.QueryRow(ctx, `
INSERT INTO marketplace_installs (id, listing_id, listing_name, version, release_channel, workspace_name, status, dependency_plan, activation, fleet_id, maintenance_window, auto_upgrade_enabled, enrollment_branch, installed_at, ready_at)
VALUES ($1, $2, $3, $4, $5, $6, 'installed', $7::jsonb, $8::jsonb, $9, $10::jsonb, false, $11, $12, $13)
RETURNING id, listing_id, listing_name, version, release_channel, workspace_name, status, dependency_plan, activation, fleet_id, NULL::text AS fleet_name, maintenance_window, auto_upgrade_enabled, enrollment_branch, installed_at, ready_at`,
		id, listing.ID, listing.Name, plan.Version, plan.ReleaseChannel, strings.TrimSpace(req.WorkspaceName), deps, activation, req.FleetID, maintenance, req.EnrollmentBranch, now, now)
	install, err := scanInstall(row)
	if err != nil {
		return nil, mapPGError(err)
	}
	_, err = r.Pool.Exec(ctx, `UPDATE marketplace_listings SET install_count = install_count + 1, updated_at = NOW() WHERE id = $1`, listing.ID)
	if err != nil {
		return nil, err
	}
	return install, nil
}

func (r *PGXRepository) PreviewDependencyPlan(ctx context.Context, req models.DependencyPlanRequest) (*models.DependencyPlanResponse, error) {
	listing, err := r.getListingDefinition(ctx, req.ListingID.String())
	if err != nil {
		return nil, err
	}
	version, err := r.resolveVersion(ctx, req.ListingID, req.Version, req.ReleaseChannel)
	if err != nil {
		return nil, err
	}
	deps, err := decodeDependencies(version.Dependencies)
	if err != nil {
		return nil, err
	}
	installed, err := r.installedVersionsBySlug(ctx, strings.TrimSpace(req.WorkspaceName))
	if err != nil {
		return nil, err
	}
	return &models.DependencyPlanResponse{ListingID: listing.ID, ListingSlug: listing.Slug, Version: version.Version, ReleaseChannel: version.ReleaseChannel, WorkspaceName: strings.TrimSpace(req.WorkspaceName), Items: deps, Conflicts: dependencyConflicts(deps, installed)}, nil
}

func (r *PGXRepository) resolveVersion(ctx context.Context, listingID uuid.UUID, version, channel string) (*models.PackageVersion, error) {
	versions, err := r.listVersions(ctx, listingID)
	if err != nil {
		return nil, err
	}
	if channel == "" {
		channel = "stable"
	}
	for _, candidate := range versions {
		if version != "" && candidate.Version != version {
			continue
		}
		if strings.EqualFold(candidate.ReleaseChannel, channel) {
			return &candidate, nil
		}
	}
	if version != "" {
		return nil, ErrVersionNotFound
	}
	for _, candidate := range versions {
		if strings.EqualFold(candidate.ReleaseChannel, channel) {
			return &candidate, nil
		}
	}
	return nil, ErrVersionNotFound
}

func (r *PGXRepository) installedVersionsBySlug(ctx context.Context, workspace string) (map[string]string, error) {
	rows, err := r.Pool.Query(ctx, `
SELECT DISTINCT ON (listings.slug) listings.slug, installs.version
FROM marketplace_installs installs
INNER JOIN marketplace_listings listings ON listings.id = installs.listing_id
WHERE installs.workspace_name = $1
ORDER BY listings.slug, installs.installed_at DESC`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	installed := map[string]string{}
	for rows.Next() {
		var slug, version string
		if err := rows.Scan(&slug, &version); err != nil {
			return nil, err
		}
		installed[slug] = version
	}
	return installed, rows.Err()
}

func (r *PGXRepository) getListingDefinition(ctx context.Context, ref string) (*models.ListingDefinition, error) {
	query := `SELECT id, name, slug, summary, description, publisher, category_slug, package_kind, repository_slug, visibility, tags, capabilities, install_count, average_rating, created_at, updated_at FROM marketplace_listings WHERE id = $1`
	arg := any(ref)
	if id, err := uuid.Parse(ref); err == nil {
		arg = id
	} else {
		query = strings.Replace(query, "id = $1", "slug = $1", 1)
	}
	listing, err := scanListing(r.Pool.QueryRow(ctx, query, arg))
	if err != nil {
		return nil, mapPGError(err)
	}
	return listing, nil
}

func (r *PGXRepository) listVersions(ctx context.Context, listingID uuid.UUID) ([]models.PackageVersion, error) {
	rows, err := r.Pool.Query(ctx, `SELECT id, listing_id, version, release_channel, changelog, dependency_mode, dependencies, packaged_resources, manifest, published_at FROM marketplace_package_versions WHERE listing_id = $1 ORDER BY published_at DESC`, listingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []models.PackageVersion{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, *v)
	}
	return versions, rows.Err()
}

func (r *PGXRepository) listReviews(ctx context.Context, listingID uuid.UUID) ([]models.ListingReview, error) {
	rows, err := r.Pool.Query(ctx, `SELECT id, listing_id, author, rating, headline, body, recommended, created_at FROM marketplace_reviews WHERE listing_id = $1 ORDER BY created_at DESC`, listingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reviews := []models.ListingReview{}
	for rows.Next() {
		var review models.ListingReview
		if err := rows.Scan(&review.ID, &review.ListingID, &review.Author, &review.Rating, &review.Headline, &review.Body, &review.Recommended, &review.CreatedAt); err != nil {
			return nil, err
		}
		reviews = append(reviews, review)
	}
	return reviews, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanListing(row scanner) (*models.ListingDefinition, error) {
	var l models.ListingDefinition
	var packageKind string
	var tags, capabilities []byte
	if err := row.Scan(&l.ID, &l.Name, &l.Slug, &l.Summary, &l.Description, &l.Publisher, &l.CategorySlug, &packageKind, &l.RepositorySlug, &l.Visibility, &tags, &capabilities, &l.InstallCount, &l.AverageRating, &l.CreatedAt, &l.UpdatedAt); err != nil {
		return nil, err
	}
	l.PackageKind = models.PackageType(packageKind)
	if err := json.Unmarshal(tags, &l.Tags); err != nil {
		return nil, fmt.Errorf("decode tags: %w", err)
	}
	if err := json.Unmarshal(capabilities, &l.Capabilities); err != nil {
		return nil, fmt.Errorf("decode capabilities: %w", err)
	}
	return &l, nil
}

func scanListingWithTotal(row scanner) (*models.ListingDefinition, int, error) {
	var l models.ListingDefinition
	var packageKind string
	var tags, capabilities []byte
	var total int
	if err := row.Scan(&l.ID, &l.Name, &l.Slug, &l.Summary, &l.Description, &l.Publisher, &l.CategorySlug, &packageKind, &l.RepositorySlug, &l.Visibility, &tags, &capabilities, &l.InstallCount, &l.AverageRating, &l.CreatedAt, &l.UpdatedAt, &total); err != nil {
		return nil, 0, err
	}
	l.PackageKind = models.PackageType(packageKind)
	if err := json.Unmarshal(tags, &l.Tags); err != nil {
		return nil, 0, fmt.Errorf("decode tags: %w", err)
	}
	if err := json.Unmarshal(capabilities, &l.Capabilities); err != nil {
		return nil, 0, fmt.Errorf("decode capabilities: %w", err)
	}
	return &l, total, nil
}

func scanVersion(row scanner) (*models.PackageVersion, error) {
	var v models.PackageVersion
	if err := row.Scan(&v.ID, &v.ListingID, &v.Version, &v.ReleaseChannel, &v.Changelog, &v.DependencyMode, &v.Dependencies, &v.PackagedResources, &v.Manifest, &v.PublishedAt); err != nil {
		return nil, err
	}
	return &v, nil
}

func scanInstall(row scanner) (*models.InstallRecord, error) {
	var install models.InstallRecord
	var dependencyPlan, activation, maintenanceWindow []byte
	var fleetID *uuid.UUID
	var fleetName *string
	if err := row.Scan(&install.ID, &install.ListingID, &install.ListingName, &install.Version, &install.ReleaseChannel, &install.WorkspaceName, &install.Status, &dependencyPlan, &activation, &fleetID, &fleetName, &maintenanceWindow, &install.AutoUpgradeEnabled, &install.EnrollmentBranch, &install.InstalledAt, &install.ReadyAt); err != nil {
		return nil, err
	}
	install.FleetID = fleetID
	install.FleetName = fleetName
	if len(dependencyPlan) == 0 {
		dependencyPlan = []byte(`[]`)
	}
	if err := json.Unmarshal(dependencyPlan, &install.DependencyPlan); err != nil {
		return nil, fmt.Errorf("decode dependency_plan: %w", err)
	}
	if len(activation) == 0 || string(activation) == "{}" || string(activation) == "null" {
		install.Activation = defaultActivation()
	} else if err := json.Unmarshal(activation, &install.Activation); err != nil {
		return nil, fmt.Errorf("decode activation: %w", err)
	}
	if len(maintenanceWindow) > 0 && string(maintenanceWindow) != "{}" && string(maintenanceWindow) != "null" {
		install.MaintenanceWindow = append(json.RawMessage(nil), maintenanceWindow...)
	}
	return &install, nil
}

func scanInstallWithTotal(row scanner) (*models.InstallRecord, int, error) {
	var install models.InstallRecord
	var dependencyPlan, activation, maintenanceWindow []byte
	var fleetID *uuid.UUID
	var fleetName *string
	var total int
	if err := row.Scan(&install.ID, &install.ListingID, &install.ListingName, &install.Version, &install.ReleaseChannel, &install.WorkspaceName, &install.Status, &dependencyPlan, &activation, &fleetID, &fleetName, &maintenanceWindow, &install.AutoUpgradeEnabled, &install.EnrollmentBranch, &install.InstalledAt, &install.ReadyAt, &total); err != nil {
		return nil, 0, err
	}
	install.FleetID = fleetID
	install.FleetName = fleetName
	if len(dependencyPlan) == 0 {
		dependencyPlan = []byte(`[]`)
	}
	if err := json.Unmarshal(dependencyPlan, &install.DependencyPlan); err != nil {
		return nil, 0, fmt.Errorf("decode dependency_plan: %w", err)
	}
	if len(activation) == 0 || string(activation) == "{}" || string(activation) == "null" {
		install.Activation = defaultActivation()
	} else if err := json.Unmarshal(activation, &install.Activation); err != nil {
		return nil, 0, fmt.Errorf("decode activation: %w", err)
	}
	if len(maintenanceWindow) > 0 && string(maintenanceWindow) != "{}" && string(maintenanceWindow) != "null" {
		install.MaintenanceWindow = append(json.RawMessage(nil), maintenanceWindow...)
	}
	return &install, total, nil
}

func applyUpdate(l *models.ListingDefinition, req models.UpdateListingRequest) {
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
}

func jsonArray(values []string) json.RawMessage {
	if values == nil {
		values = []string{}
	}
	b, _ := json.Marshal(values)
	return b
}

func mapPGError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: listing slug already exists", ErrValidation)
	}
	return err
}
