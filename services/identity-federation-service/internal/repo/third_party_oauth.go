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

func (r *Repo) GetThirdPartyApplicationByClientID(ctx context.Context, clientID string) (*models.ThirdPartyApplication, *string, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+thirdPartyApplicationSelectColumns+`, client_secret_hash
		 FROM third_party_applications
		 WHERE client_id = $1 AND revoked_at IS NULL`,
		clientID,
	)
	app := &models.ThirdPartyApplication{}
	var secretHash *string
	if err := scanThirdPartyApplicationWithSecret(app, &secretHash, row); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	enablements, err := r.listThirdPartyApplicationEnablements(ctx, []uuid.UUID{app.ID})
	if err != nil {
		return nil, nil, err
	}
	app.Enablements = enablements[app.ID]
	return app, secretHash, nil
}

func (r *Repo) CreateThirdPartyOAuthAuthorizationCode(ctx context.Context, code *models.ThirdPartyOAuthAuthorizationCode) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO third_party_oauth_authorization_codes
		   (id, code_hash, application_id, client_id, user_id, organization_id,
		    redirect_uri, state, code_challenge, code_challenge_method,
		    requested_scopes, granted_scopes, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		code.ID, code.CodeHash, code.ApplicationID, code.ClientID, code.UserID, code.OrganizationID,
		code.RedirectURI, code.State, code.CodeChallenge, code.CodeChallengeMethod,
		code.RequestedScopes, code.GrantedScopes, code.CreatedAt, code.ExpiresAt,
	)
	return err
}

func (r *Repo) ConsumeThirdPartyOAuthAuthorizationCode(ctx context.Context, codeHash string, now time.Time) (*models.ThirdPartyOAuthAuthorizationCode, error) {
	row := r.Pool.QueryRow(ctx,
		`UPDATE third_party_oauth_authorization_codes
		 SET consumed_at = $2
		 WHERE code_hash = $1
		   AND consumed_at IS NULL
		   AND revoked_at IS NULL
		   AND expires_at > $2
		 RETURNING id, code_hash, application_id, client_id, user_id, organization_id,
		           redirect_uri, state, code_challenge, code_challenge_method,
		           requested_scopes, granted_scopes, created_at, expires_at, consumed_at, revoked_at`,
		codeHash, now,
	)
	code := &models.ThirdPartyOAuthAuthorizationCode{}
	if err := scanThirdPartyOAuthAuthorizationCode(code, row); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return code, nil
}

func (r *Repo) UpsertThirdPartyOAuthConsent(ctx context.Context, applicationID, userID, organizationID uuid.UUID, scopes []string, now time.Time) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO third_party_oauth_consents
		   (application_id, user_id, organization_id, scopes, consented_at, revoked_at)
		 VALUES ($1, $2, $3, $4, $5, NULL)
		 ON CONFLICT (application_id, user_id, organization_id) DO UPDATE SET
		   scopes = EXCLUDED.scopes,
		   consented_at = EXCLUDED.consented_at,
		   revoked_at = NULL`,
		applicationID, userID, organizationID, scopes, now,
	)
	return err
}

func (r *Repo) InsertThirdPartyOAuthRefreshToken(ctx context.Context, token *models.ThirdPartyOAuthRefreshToken) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO third_party_oauth_refresh_tokens
		   (id, token_hash, family_id, application_id, client_id, subject_user_id,
		    organization_id, scopes, issued_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		token.ID, token.TokenHash, token.FamilyID, token.ApplicationID, token.ClientID,
		token.SubjectUserID, token.OrganizationID, token.Scopes, token.IssuedAt, token.ExpiresAt,
	)
	return err
}

func (r *Repo) FindThirdPartyOAuthRefreshToken(ctx context.Context, tokenHash string) (*models.ThirdPartyOAuthRefreshToken, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT id, token_hash, family_id, application_id, client_id, subject_user_id,
		        organization_id, scopes, issued_at, expires_at, used_at, revoked_at
		 FROM third_party_oauth_refresh_tokens
		 WHERE token_hash = $1`,
		tokenHash,
	)
	token := &models.ThirdPartyOAuthRefreshToken{}
	if err := scanThirdPartyOAuthRefreshToken(token, row); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return token, nil
}

func (r *Repo) RotateThirdPartyOAuthRefreshToken(ctx context.Context, currentID uuid.UUID, next *models.ThirdPartyOAuthRefreshToken, now time.Time) (bool, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin oauth refresh rotation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx,
		`UPDATE third_party_oauth_refresh_tokens
		 SET used_at = $2
		 WHERE id = $1 AND used_at IS NULL AND revoked_at IS NULL`,
		currentID, now,
	)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO third_party_oauth_refresh_tokens
		   (id, token_hash, family_id, application_id, client_id, subject_user_id,
		    organization_id, scopes, issued_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		next.ID, next.TokenHash, next.FamilyID, next.ApplicationID, next.ClientID,
		next.SubjectUserID, next.OrganizationID, next.Scopes, next.IssuedAt, next.ExpiresAt,
	); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repo) RevokeThirdPartyOAuthRefreshTokenByHash(ctx context.Context, tokenHash string, now time.Time) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE third_party_oauth_refresh_tokens
		 SET revoked_at = COALESCE(revoked_at, $2)
		 WHERE token_hash = $1`,
		tokenHash, now,
	)
	return err
}

func (r *Repo) RevokeThirdPartyOAuthRefreshTokenFamily(ctx context.Context, familyID uuid.UUID, now time.Time) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE third_party_oauth_refresh_tokens
		 SET revoked_at = COALESCE(revoked_at, $2)
		 WHERE family_id = $1`,
		familyID, now,
	)
	return err
}

func (r *Repo) RevokeThirdPartyOAuthRefreshTokenByID(ctx context.Context, subjectUserID, tokenID uuid.UUID, now time.Time) (bool, error) {
	tag, err := r.Pool.Exec(ctx,
		`UPDATE third_party_oauth_refresh_tokens
		 SET revoked_at = COALESCE(revoked_at, $3)
		 WHERE id = $1 AND subject_user_id = $2`,
		tokenID, subjectUserID, now,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repo) RevokeThirdPartyOAuthRefreshTokensForUser(ctx context.Context, userID uuid.UUID, now time.Time) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE third_party_oauth_refresh_tokens
		 SET revoked_at = COALESCE(revoked_at, $2)
		 WHERE subject_user_id = $1`,
		userID, now,
	)
	return err
}

func (r *Repo) RevokeThirdPartyOAuthRefreshTokensForApplication(ctx context.Context, applicationID uuid.UUID, now time.Time) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE third_party_oauth_refresh_tokens
		 SET revoked_at = COALESCE(revoked_at, $2)
		 WHERE application_id = $1`,
		applicationID, now,
	)
	return err
}

func (r *Repo) RevokeThirdPartyOAuthRefreshTokensForApplicationOrganization(ctx context.Context, applicationID, organizationID uuid.UUID, now time.Time) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE third_party_oauth_refresh_tokens
		 SET revoked_at = COALESCE(revoked_at, $3)
		 WHERE application_id = $1 AND organization_id = $2`,
		applicationID, organizationID, now,
	)
	return err
}

func (r *Repo) ListThirdPartyOAuthAuthorizations(ctx context.Context, subjectUserID uuid.UUID, now time.Time) ([]models.ThirdPartyOAuthAuthorization, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT t.id, t.application_id, a.name, t.client_id, t.organization_id,
		        t.scopes, t.issued_at, t.expires_at, t.revoked_at
		 FROM third_party_oauth_refresh_tokens t
		 INNER JOIN third_party_applications a ON a.id = t.application_id
		 WHERE t.subject_user_id = $1
		   AND t.expires_at > $2
		 ORDER BY t.issued_at DESC`,
		subjectUserID, now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ThirdPartyOAuthAuthorization, 0)
	for rows.Next() {
		var item models.ThirdPartyOAuthAuthorization
		if err := rows.Scan(
			&item.ID, &item.ApplicationID, &item.ApplicationName, &item.ClientID,
			&item.OrganizationID, &item.Scopes, &item.IssuedAt, &item.ExpiresAt, &item.RevokedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type thirdPartyApplicationWithSecretScanner interface {
	Scan(dest ...any) error
}

func scanThirdPartyApplicationWithSecret(app *models.ThirdPartyApplication, secretHash **string, row thirdPartyApplicationWithSecretScanner) error {
	if err := row.Scan(
		&app.ID, &app.ClientID, &app.Name, &app.Description, &app.LogoURL,
		&app.ClientType, &app.EnabledGrantTypes, &app.RedirectURIs, &app.Scopes, &app.OwnerUserIDs,
		&app.ManagingOrganizationID, &app.DiscoverableOrganizationIDs, &app.ServiceUserID,
		&app.ClientSecretPrefix, &app.ClientSecretCreatedAt, &app.PreferredManagementSurface,
		&app.ControlPanelFallback, &app.CreatedBy, &app.UpdatedBy, &app.CreatedAt, &app.UpdatedAt, &app.RevokedAt,
		secretHash,
	); err != nil {
		return err
	}
	app.RequiresPKCE = app.ClientType == models.ThirdPartyClientTypePublic
	return nil
}

type thirdPartyOAuthScanner interface {
	Scan(dest ...any) error
}

func scanThirdPartyOAuthAuthorizationCode(code *models.ThirdPartyOAuthAuthorizationCode, row thirdPartyOAuthScanner) error {
	return row.Scan(
		&code.ID, &code.CodeHash, &code.ApplicationID, &code.ClientID, &code.UserID,
		&code.OrganizationID, &code.RedirectURI, &code.State, &code.CodeChallenge,
		&code.CodeChallengeMethod, &code.RequestedScopes, &code.GrantedScopes,
		&code.CreatedAt, &code.ExpiresAt, &code.ConsumedAt, &code.RevokedAt,
	)
}

func scanThirdPartyOAuthRefreshToken(token *models.ThirdPartyOAuthRefreshToken, row thirdPartyOAuthScanner) error {
	return row.Scan(
		&token.ID, &token.TokenHash, &token.FamilyID, &token.ApplicationID, &token.ClientID,
		&token.SubjectUserID, &token.OrganizationID, &token.Scopes, &token.IssuedAt,
		&token.ExpiresAt, &token.UsedAt, &token.RevokedAt,
	)
}
