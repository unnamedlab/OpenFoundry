package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

const thirdPartyApplicationSelectColumns = `id, client_id, name, description, logo_url,
	client_type, enabled_grant_types, redirect_uris, scopes, owner_user_ids,
	managing_organization_id, discoverable_organization_ids, service_user_id,
	client_secret_prefix, client_secret_created_at, preferred_management_surface,
	control_panel_fallback, created_by, updated_by, created_at, updated_at, revoked_at`

func (r *Repo) ListThirdPartyApplications(ctx context.Context, includeRevoked bool) ([]models.ThirdPartyApplication, error) {
	query := `SELECT ` + thirdPartyApplicationSelectColumns + `
		FROM third_party_applications`
	if !includeRevoked {
		query += ` WHERE revoked_at IS NULL`
	}
	query += ` ORDER BY created_at DESC`
	rows, err := r.Pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	apps := make([]models.ThirdPartyApplication, 0)
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var app models.ThirdPartyApplication
		if err := scanThirdPartyApplication(&app, rows); err != nil {
			return nil, err
		}
		apps = append(apps, app)
		ids = append(ids, app.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	enablements, err := r.listThirdPartyApplicationEnablements(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range apps {
		apps[i].Enablements = enablements[apps[i].ID]
	}
	return apps, nil
}

func (r *Repo) GetThirdPartyApplication(ctx context.Context, id uuid.UUID) (*models.ThirdPartyApplication, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+thirdPartyApplicationSelectColumns+`
		 FROM third_party_applications WHERE id = $1`,
		id,
	)
	app := &models.ThirdPartyApplication{}
	if err := scanThirdPartyApplication(app, row); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	enablements, err := r.listThirdPartyApplicationEnablements(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	app.Enablements = enablements[id]
	return app, nil
}

func (r *Repo) CreateThirdPartyApplication(ctx context.Context, app *models.ThirdPartyApplication, serviceUser *models.ThirdPartyAppServiceUserSeed, clientSecretHash *string) (*models.ThirdPartyApplication, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin third-party application create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if serviceUser != nil {
		if _, err := tx.Exec(ctx,
			`INSERT INTO users
			   (id, email, username, name, password_hash, is_active, auth_source, realm,
			    organization_id, attributes, preregistered, invited_by)
			 VALUES ($1, $2, $3, $4, '', TRUE, 'oauth_client', 'service_user',
			         $5, $6::jsonb, FALSE, $7)`,
			serviceUser.ID, serviceUser.Email, serviceUser.Username, serviceUser.Name,
			serviceUser.OrganizationID, serviceUser.Attributes, serviceUser.CreatedBy,
		); err != nil {
			return nil, fmt.Errorf("insert third-party application service user: %w", err)
		}
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO third_party_applications
		   (id, client_id, name, description, logo_url, client_type, enabled_grant_types,
		    redirect_uris, scopes, owner_user_ids, managing_organization_id,
		    discoverable_organization_ids, service_user_id, client_secret_hash,
		    client_secret_prefix, client_secret_created_at, preferred_management_surface,
		    control_panel_fallback, created_by, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		         $11, $12, $13, $14, $15, $16, $17, $18, $19, $19)`,
		app.ID, app.ClientID, app.Name, app.Description, app.LogoURL, app.ClientType, app.EnabledGrantTypes,
		app.RedirectURIs, app.Scopes, app.OwnerUserIDs, app.ManagingOrganizationID,
		app.DiscoverableOrganizationIDs, app.ServiceUserID, clientSecretHash,
		app.ClientSecretPrefix, app.ClientSecretCreatedAt, app.PreferredManagementSurface,
		app.ControlPanelFallback, app.CreatedBy,
	); err != nil {
		return nil, fmt.Errorf("insert third-party application: %w", err)
	}
	for _, enablement := range app.Enablements {
		if _, err := tx.Exec(ctx,
			`INSERT INTO third_party_application_enablements
			   (application_id, organization_id, enabled, project_resource_ids, marking_ids,
			    organization_consent, updated_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 ON CONFLICT (application_id, organization_id) DO UPDATE SET
			   enabled = EXCLUDED.enabled,
			   project_resource_ids = EXCLUDED.project_resource_ids,
			   marking_ids = EXCLUDED.marking_ids,
			   organization_consent = EXCLUDED.organization_consent,
			   updated_by = EXCLUDED.updated_by,
			   updated_at = NOW()`,
			app.ID, enablement.OrganizationID, enablement.Enabled, enablement.ProjectResourceIDs,
			enablement.MarkingIDs, enablement.OrganizationConsent, enablement.UpdatedBy,
		); err != nil {
			return nil, fmt.Errorf("insert third-party application enablement: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetThirdPartyApplication(ctx, app.ID)
}

func (r *Repo) UpdateThirdPartyApplication(ctx context.Context, app *models.ThirdPartyApplication, serviceUser *models.ThirdPartyAppServiceUserSeed) (*models.ThirdPartyApplication, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin third-party application update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if serviceUser != nil {
		if _, err := tx.Exec(ctx,
			`INSERT INTO users
			   (id, email, username, name, password_hash, is_active, auth_source, realm,
			    organization_id, attributes, preregistered, invited_by)
			 VALUES ($1, $2, $3, $4, '', TRUE, 'oauth_client', 'service_user',
			         $5, $6::jsonb, FALSE, $7)
			 ON CONFLICT (id) DO NOTHING`,
			serviceUser.ID, serviceUser.Email, serviceUser.Username, serviceUser.Name,
			serviceUser.OrganizationID, serviceUser.Attributes, serviceUser.CreatedBy,
		); err != nil {
			return nil, fmt.Errorf("insert third-party application service user: %w", err)
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE third_party_applications SET
		   name = $2,
		   description = $3,
		   logo_url = $4,
		   client_type = $5,
		   enabled_grant_types = $6,
		   redirect_uris = $7,
		   scopes = $8,
		   owner_user_ids = $9,
		   discoverable_organization_ids = $10,
		   service_user_id = $11,
		   preferred_management_surface = $12,
		   control_panel_fallback = $13,
		   updated_by = $14,
		   updated_at = NOW()
		 WHERE id = $1 AND revoked_at IS NULL`,
		app.ID, app.Name, app.Description, app.LogoURL, app.ClientType, app.EnabledGrantTypes,
		app.RedirectURIs, app.Scopes, app.OwnerUserIDs, app.DiscoverableOrganizationIDs,
		app.ServiceUserID, app.PreferredManagementSurface, app.ControlPanelFallback, app.UpdatedBy,
	); err != nil {
		return nil, fmt.Errorf("update third-party application: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetThirdPartyApplication(ctx, app.ID)
}

func (r *Repo) RevokeThirdPartyApplication(ctx context.Context, id, actor uuid.UUID, at time.Time) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE third_party_applications
		 SET revoked_at = $3, updated_by = $2, updated_at = $3
		 WHERE id = $1 AND revoked_at IS NULL`,
		id, actor, at,
	)
	if err != nil {
		return err
	}
	return r.RevokeThirdPartyOAuthRefreshTokensForApplication(ctx, id, at)
}

func (r *Repo) RotateThirdPartyApplicationSecret(ctx context.Context, id uuid.UUID, secretHash, prefix string, actor uuid.UUID, at time.Time) (*models.ThirdPartyApplication, error) {
	_, err := r.Pool.Exec(ctx,
		`UPDATE third_party_applications
		 SET client_secret_hash = $2,
		     client_secret_prefix = $3,
		     client_secret_created_at = $4,
		     updated_by = $5,
		     updated_at = $4
		 WHERE id = $1
		   AND revoked_at IS NULL
		   AND client_type = 'confidential'`,
		id, secretHash, prefix, at, actor,
	)
	if err != nil {
		return nil, err
	}
	return r.GetThirdPartyApplication(ctx, id)
}

func (r *Repo) UpsertThirdPartyApplicationEnablement(ctx context.Context, applicationID, organizationID uuid.UUID, body models.UpsertThirdPartyApplicationEnablementRequest, actor uuid.UUID) (*models.ThirdPartyApplication, error) {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO third_party_application_enablements
		   (application_id, organization_id, enabled, project_resource_ids, marking_ids,
		    organization_consent, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (application_id, organization_id) DO UPDATE SET
		   enabled = EXCLUDED.enabled,
		   project_resource_ids = EXCLUDED.project_resource_ids,
		   marking_ids = EXCLUDED.marking_ids,
		   organization_consent = EXCLUDED.organization_consent,
		   updated_by = EXCLUDED.updated_by,
		   updated_at = NOW()`,
		applicationID, organizationID, body.Enabled, body.ProjectResourceIDs, body.MarkingIDs,
		body.OrganizationConsent, actor,
	)
	if err != nil {
		return nil, err
	}
	return r.GetThirdPartyApplication(ctx, applicationID)
}

func (r *Repo) DisableThirdPartyApplicationEnablement(ctx context.Context, applicationID, organizationID, actor uuid.UUID) (*models.ThirdPartyApplication, error) {
	_, err := r.Pool.Exec(ctx,
		`UPDATE third_party_application_enablements
		 SET enabled = FALSE, updated_by = $3, updated_at = NOW()
		 WHERE application_id = $1 AND organization_id = $2`,
		applicationID, organizationID, actor,
	)
	if err != nil {
		return nil, err
	}
	if err := r.RevokeThirdPartyOAuthRefreshTokensForApplicationOrganization(ctx, applicationID, organizationID, time.Now().UTC()); err != nil {
		return nil, err
	}
	return r.GetThirdPartyApplication(ctx, applicationID)
}

func (r *Repo) listThirdPartyApplicationEnablements(ctx context.Context, appIDs []uuid.UUID) (map[uuid.UUID][]models.ThirdPartyApplicationEnablement, error) {
	out := make(map[uuid.UUID][]models.ThirdPartyApplicationEnablement, len(appIDs))
	if len(appIDs) == 0 {
		return out, nil
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT application_id, organization_id, enabled, project_resource_ids,
		        marking_ids, organization_consent, updated_by, created_at, updated_at
		 FROM third_party_application_enablements
		 WHERE application_id = ANY($1)
		 ORDER BY organization_id`,
		appIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var enablement models.ThirdPartyApplicationEnablement
		if err := rows.Scan(
			&enablement.ApplicationID, &enablement.OrganizationID, &enablement.Enabled,
			&enablement.ProjectResourceIDs, &enablement.MarkingIDs, &enablement.OrganizationConsent,
			&enablement.UpdatedBy, &enablement.CreatedAt, &enablement.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out[enablement.ApplicationID] = append(out[enablement.ApplicationID], enablement)
	}
	return out, rows.Err()
}

type thirdPartyApplicationScanner interface {
	Scan(dest ...any) error
}

func scanThirdPartyApplication(app *models.ThirdPartyApplication, row thirdPartyApplicationScanner) error {
	if err := row.Scan(
		&app.ID, &app.ClientID, &app.Name, &app.Description, &app.LogoURL,
		&app.ClientType, &app.EnabledGrantTypes, &app.RedirectURIs, &app.Scopes, &app.OwnerUserIDs,
		&app.ManagingOrganizationID, &app.DiscoverableOrganizationIDs, &app.ServiceUserID,
		&app.ClientSecretPrefix, &app.ClientSecretCreatedAt, &app.PreferredManagementSurface,
		&app.ControlPanelFallback, &app.CreatedBy, &app.UpdatedBy, &app.CreatedAt, &app.UpdatedAt, &app.RevokedAt,
	); err != nil {
		return err
	}
	app.RequiresPKCE = app.ClientType == models.ThirdPartyClientTypePublic
	return nil
}
