package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

// ─── Network boundary policies ─────────────────────────────────────

const nbpSelect = `SELECT id, name, direction, boundary_kind, allowed_hosts,
	blocked_hosts, allow_private_networks, allow_insecure_http, proxy_mode,
	private_link_enabled, updated_by, created_at, updated_at
	FROM network_boundary_policies`

func (r *Repo) ListNetworkBoundaryPolicies(ctx context.Context) ([]models.NetworkBoundaryPolicy, error) {
	rows, err := r.Pool.Query(ctx, nbpSelect+` ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.NetworkBoundaryPolicy, 0)
	for rows.Next() {
		v, err := scanNBP(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetNetworkBoundaryPolicy(ctx context.Context, id uuid.UUID) (*models.NetworkBoundaryPolicy, error) {
	row := r.Pool.QueryRow(ctx, nbpSelect+` WHERE id = $1`, id)
	v, err := scanNBP(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CreateNetworkBoundaryPolicy(ctx context.Context, body *models.CreateNetworkBoundaryPolicyRequest, updatedBy string) (*models.NetworkBoundaryPolicy, error) {
	id := uuid.New()
	allowPriv := false
	if body.AllowPrivateNetworks != nil {
		allowPriv = *body.AllowPrivateNetworks
	}
	allowInsec := false
	if body.AllowInsecureHTTP != nil {
		allowInsec = *body.AllowInsecureHTTP
	}
	proxyMode := "direct"
	if body.ProxyMode != nil {
		proxyMode = *body.ProxyMode
	}
	plEnabled := false
	if body.PrivateLinkEnabled != nil {
		plEnabled = *body.PrivateLinkEnabled
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO network_boundary_policies
		    (id, name, direction, boundary_kind, allowed_hosts, blocked_hosts,
		     allow_private_networks, allow_insecure_http, proxy_mode,
		     private_link_enabled, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id, name, direction, boundary_kind, allowed_hosts, blocked_hosts,
		           allow_private_networks, allow_insecure_http, proxy_mode,
		           private_link_enabled, updated_by, created_at, updated_at`,
		id, body.Name, body.Direction, body.BoundaryKind,
		defaultJSON(body.AllowedHosts, "[]"),
		defaultJSON(body.BlockedHosts, "[]"),
		allowPriv, allowInsec, proxyMode, plEnabled, updatedBy,
	)
	return scanNBP(row)
}

func (r *Repo) UpdateNetworkBoundaryPolicy(ctx context.Context, id uuid.UUID, body *models.UpdateNetworkBoundaryPolicyRequest, updatedBy string) (*models.NetworkBoundaryPolicy, error) {
	current, err := r.GetNetworkBoundaryPolicy(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	allowed := current.AllowedHosts
	if len(body.AllowedHosts) > 0 {
		allowed = body.AllowedHosts
	}
	blocked := current.BlockedHosts
	if len(body.BlockedHosts) > 0 {
		blocked = body.BlockedHosts
	}
	allowPriv := current.AllowPrivateNetworks
	if body.AllowPrivateNetworks != nil {
		allowPriv = *body.AllowPrivateNetworks
	}
	allowInsec := current.AllowInsecureHTTP
	if body.AllowInsecureHTTP != nil {
		allowInsec = *body.AllowInsecureHTTP
	}
	pm := current.ProxyMode
	if body.ProxyMode != nil {
		pm = *body.ProxyMode
	}
	pl := current.PrivateLinkEnabled
	if body.PrivateLinkEnabled != nil {
		pl = *body.PrivateLinkEnabled
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE network_boundary_policies SET
		    allowed_hosts = $2, blocked_hosts = $3,
		    allow_private_networks = $4, allow_insecure_http = $5,
		    proxy_mode = $6, private_link_enabled = $7,
		    updated_by = $8, updated_at = $9
		  WHERE id = $1
		  RETURNING id, name, direction, boundary_kind, allowed_hosts, blocked_hosts,
		            allow_private_networks, allow_insecure_http, proxy_mode,
		            private_link_enabled, updated_by, created_at, updated_at`,
		id, allowed, blocked, allowPriv, allowInsec, pm, pl, updatedBy, time.Now().UTC(),
	)
	return scanNBP(row)
}

func (r *Repo) DeleteNetworkBoundaryPolicy(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM network_boundary_policies WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func scanNBP(r rowLikeT) (*models.NetworkBoundaryPolicy, error) {
	v := &models.NetworkBoundaryPolicy{}
	if err := r.Scan(&v.ID, &v.Name, &v.Direction, &v.BoundaryKind,
		&v.AllowedHosts, &v.BlockedHosts,
		&v.AllowPrivateNetworks, &v.AllowInsecureHTTP,
		&v.ProxyMode, &v.PrivateLinkEnabled, &v.UpdatedBy,
		&v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, nil
}

// ─── Network private links ─────────────────────────────────────────

const nplSelect = `SELECT id, name, target_host, transport, enabled,
	created_at, updated_at FROM network_private_links`

func (r *Repo) ListNetworkPrivateLinks(ctx context.Context) ([]models.NetworkPrivateLink, error) {
	rows, err := r.Pool.Query(ctx, nplSelect+` ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.NetworkPrivateLink, 0)
	for rows.Next() {
		v, err := scanNPL(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetNetworkPrivateLink(ctx context.Context, id uuid.UUID) (*models.NetworkPrivateLink, error) {
	row := r.Pool.QueryRow(ctx, nplSelect+` WHERE id = $1`, id)
	v, err := scanNPL(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CreateNetworkPrivateLink(ctx context.Context, body *models.CreateNetworkPrivateLinkRequest) (*models.NetworkPrivateLink, error) {
	id := uuid.New()
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO network_private_links (id, name, target_host, transport, enabled)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, name, target_host, transport, enabled, created_at, updated_at`,
		id, body.Name, body.TargetHost, body.Transport, enabled,
	)
	return scanNPL(row)
}

func (r *Repo) UpdateNetworkPrivateLink(ctx context.Context, id uuid.UUID, body *models.UpdateNetworkPrivateLinkRequest) (*models.NetworkPrivateLink, error) {
	current, err := r.GetNetworkPrivateLink(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	target := current.TargetHost
	if body.TargetHost != nil {
		target = *body.TargetHost
	}
	transport := current.Transport
	if body.Transport != nil {
		transport = *body.Transport
	}
	enabled := current.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE network_private_links SET
		    target_host = $2, transport = $3, enabled = $4, updated_at = $5
		  WHERE id = $1
		  RETURNING id, name, target_host, transport, enabled, created_at, updated_at`,
		id, target, transport, enabled, time.Now().UTC(),
	)
	return scanNPL(row)
}

func (r *Repo) DeleteNetworkPrivateLink(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM network_private_links WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func scanNPL(r rowLikeT) (*models.NetworkPrivateLink, error) {
	v := &models.NetworkPrivateLink{}
	if err := r.Scan(&v.ID, &v.Name, &v.TargetHost, &v.Transport,
		&v.Enabled, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, nil
}

// ─── Network proxy definitions ─────────────────────────────────────

const npdSelect = `SELECT id, name, proxy_url, mode, enabled,
	created_at, updated_at FROM network_proxy_definitions`

func (r *Repo) ListNetworkProxyDefinitions(ctx context.Context) ([]models.NetworkProxyDefinition, error) {
	rows, err := r.Pool.Query(ctx, npdSelect+` ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.NetworkProxyDefinition, 0)
	for rows.Next() {
		v, err := scanNPD(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetNetworkProxyDefinition(ctx context.Context, id uuid.UUID) (*models.NetworkProxyDefinition, error) {
	row := r.Pool.QueryRow(ctx, npdSelect+` WHERE id = $1`, id)
	v, err := scanNPD(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CreateNetworkProxyDefinition(ctx context.Context, body *models.CreateNetworkProxyDefinitionRequest) (*models.NetworkProxyDefinition, error) {
	id := uuid.New()
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO network_proxy_definitions (id, name, proxy_url, mode, enabled)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, name, proxy_url, mode, enabled, created_at, updated_at`,
		id, body.Name, body.ProxyURL, body.Mode, enabled,
	)
	return scanNPD(row)
}

func (r *Repo) UpdateNetworkProxyDefinition(ctx context.Context, id uuid.UUID, body *models.UpdateNetworkProxyDefinitionRequest) (*models.NetworkProxyDefinition, error) {
	current, err := r.GetNetworkProxyDefinition(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	url := current.ProxyURL
	if body.ProxyURL != nil {
		url = *body.ProxyURL
	}
	mode := current.Mode
	if body.Mode != nil {
		mode = *body.Mode
	}
	enabled := current.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE network_proxy_definitions SET
		    proxy_url = $2, mode = $3, enabled = $4, updated_at = $5
		  WHERE id = $1
		  RETURNING id, name, proxy_url, mode, enabled, created_at, updated_at`,
		id, url, mode, enabled, time.Now().UTC(),
	)
	return scanNPD(row)
}

func (r *Repo) DeleteNetworkProxyDefinition(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM network_proxy_definitions WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func scanNPD(r rowLikeT) (*models.NetworkProxyDefinition, error) {
	v := &models.NetworkProxyDefinition{}
	if err := r.Scan(&v.ID, &v.Name, &v.ProxyURL, &v.Mode,
		&v.Enabled, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, nil
}
