// projects_sg26.go: SG.26 — Project templates.
//
// Palantir-style Project templates are administered per Space and deployed
// while creating a Project. The local implementation records the complete
// template application so generated groups, default grants, markings and
// constraints are auditable even where the owning entity lives in another
// service.

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/workspace"
)

const projectTemplateColumns = `id, key, name, description, space_slug, default_role,
        point_of_contact_user_id, point_of_contact_email,
        variables, folder_structure, generated_groups, default_role_grants,
        markings, constraints, governance_tags, active, created_by, created_at, updated_at`

type projectTemplateHTTPError struct {
	status  int
	message string
}

func (e projectTemplateHTTPError) Error() string { return e.message }

type projectTemplateFolderPlan struct {
	key       string
	parentKey string
	request   models.CreateOntologyProjectFolderRequest
}

type projectTemplateDeployment struct {
	template    *models.ProjectTemplate
	variables   map[string]string
	validation  models.ProjectTemplateValidationResult
	folderPlans []projectTemplateFolderPlan
	generated   []models.ProjectTemplateGeneratedGroupResult
	markings    []models.ProjectTemplateAppliedMarking
	constraints []models.ProjectTemplateConstraint
	markingRIDs []string
}

// CreateProjectTemplate handles POST /api/v1/projects/templates.
func (h *ProjectsHandlers) CreateProjectTemplate(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	if !canWriteProjectTemplates(claims) {
		writeJSONErr(w, http.StatusForbidden, "forbidden: project template administration requires project_templates:write or control_panel:write")
		return
	}
	var body models.CreateProjectTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	template, err := normalizeCreateProjectTemplateRequest(&body, &claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := insertProjectTemplate(r.Context(), h.Pool, template)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("failed to create project template: %s", err))
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// GetProjectTemplate handles GET /api/v1/projects/templates/{key}.
func (h *ProjectsHandlers) GetProjectTemplate(w http.ResponseWriter, r *http.Request) {
	if _, ok := authClaims(w, r); !ok {
		return
	}
	key, err := normalizeSlug(chi.URLParam(r, "key"), "template_key")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	template, err := loadProjectTemplateByKey(r.Context(), h.Pool, key)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if template == nil {
		writeJSONErr(w, http.StatusNotFound, "project template not found")
		return
	}
	writeJSON(w, http.StatusOK, template)
}

// ListProjectTemplateApplications exposes the immutable deployment audit for
// one Project. It is intentionally owner/admin-only because it can reveal
// generated group IDs and marking decisions.
func (h *ProjectsHandlers) ListProjectTemplateApplications(w http.ResponseWriter, r *http.Request) {
	claims, ok := authClaims(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDParam(w, r, "id", "id")
	if !ok {
		return
	}
	project, err := loadProject(r.Context(), h.Pool, projectID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if project == nil {
		writeJSONErr(w, http.StatusNotFound, "ontology project not found")
		return
	}
	if err := ensureProjectOwnerOrAdmin(project, claims); err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}
	apps, err := listProjectTemplateApplications(r.Context(), h.Pool, projectID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.ListProjectTemplateApplicationsResponse{Data: apps})
}

func listProjectTemplates(ctx context.Context, pool *pgxpool.Pool, spaceSlug *string) ([]models.ProjectTemplate, error) {
	rows, err := pool.Query(ctx,
		`SELECT `+projectTemplateColumns+`
		   FROM ontology_project_templates
		  WHERE active = TRUE
		    AND ($1::text IS NULL OR space_slug IS NULL OR space_slug = $1)
		  ORDER BY CASE WHEN key = 'default' THEN 0 ELSE 1 END, name ASC`,
		spaceSlug,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ProjectTemplate, 0)
	for rows.Next() {
		tpl, err := scanProjectTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *tpl)
	}
	return out, rows.Err()
}

func loadProjectTemplateByKey(ctx context.Context, pool *pgxpool.Pool, key string) (*models.ProjectTemplate, error) {
	row := pool.QueryRow(ctx,
		`SELECT `+projectTemplateColumns+`
		   FROM ontology_project_templates
		  WHERE key = $1 AND active = TRUE`,
		key,
	)
	return scanProjectTemplate(row)
}

func scanProjectTemplate(row projectScannable) (*models.ProjectTemplate, error) {
	tpl := &models.ProjectTemplate{}
	var defaultRole string
	var rawVariables, rawFolders, rawGroups, rawGrants, rawMarkings, rawConstraints, rawTags []byte
	err := row.Scan(
		&tpl.ID, &tpl.Key, &tpl.Name, &tpl.Description, &tpl.SpaceSlug, &defaultRole,
		&tpl.PointOfContactUserID, &tpl.PointOfContactEmail,
		&rawVariables, &rawFolders, &rawGroups, &rawGrants,
		&rawMarkings, &rawConstraints, &rawTags,
		&tpl.Active, &tpl.CreatedBy, &tpl.CreatedAt, &tpl.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load project template: %w", err)
	}
	tpl.DefaultRole = models.OntologyProjectRole(defaultRole)
	if err := unmarshalJSONDefault(rawVariables, &tpl.Variables); err != nil {
		return nil, fmt.Errorf("decode project template variables: %w", err)
	}
	if err := unmarshalJSONDefault(rawFolders, &tpl.FolderStructure); err != nil {
		return nil, fmt.Errorf("decode project template folder_structure: %w", err)
	}
	if err := unmarshalJSONDefault(rawGroups, &tpl.GeneratedGroups); err != nil {
		return nil, fmt.Errorf("decode project template generated_groups: %w", err)
	}
	if err := unmarshalJSONDefault(rawGrants, &tpl.DefaultRoleGrants); err != nil {
		return nil, fmt.Errorf("decode project template default_role_grants: %w", err)
	}
	if err := unmarshalJSONDefault(rawMarkings, &tpl.Markings); err != nil {
		return nil, fmt.Errorf("decode project template markings: %w", err)
	}
	if err := unmarshalJSONDefault(rawConstraints, &tpl.Constraints); err != nil {
		return nil, fmt.Errorf("decode project template constraints: %w", err)
	}
	if err := unmarshalJSONDefault(rawTags, &tpl.GovernanceTags); err != nil {
		return nil, fmt.Errorf("decode project template governance_tags: %w", err)
	}
	return tpl, nil
}

func insertProjectTemplate(ctx context.Context, pool *pgxpool.Pool, tpl *models.ProjectTemplate) (*models.ProjectTemplate, error) {
	variables, err := json.Marshal(tpl.Variables)
	if err != nil {
		return nil, err
	}
	folders, err := json.Marshal(tpl.FolderStructure)
	if err != nil {
		return nil, err
	}
	groups, err := json.Marshal(tpl.GeneratedGroups)
	if err != nil {
		return nil, err
	}
	grants, err := json.Marshal(tpl.DefaultRoleGrants)
	if err != nil {
		return nil, err
	}
	markings, err := json.Marshal(tpl.Markings)
	if err != nil {
		return nil, err
	}
	constraints, err := json.Marshal(tpl.Constraints)
	if err != nil {
		return nil, err
	}
	tags, err := json.Marshal(tpl.GovernanceTags)
	if err != nil {
		return nil, err
	}
	row := pool.QueryRow(ctx,
		`INSERT INTO ontology_project_templates (
		     id, key, name, description, space_slug, default_role,
		     point_of_contact_user_id, point_of_contact_email,
		     variables, folder_structure, generated_groups, default_role_grants,
		     markings, constraints, governance_tags, active, created_by
		 )
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
		         $9::jsonb, $10::jsonb, $11::jsonb, $12::jsonb,
		         $13::jsonb, $14::jsonb, $15::jsonb, $16, $17)
		 RETURNING `+projectTemplateColumns,
		tpl.ID, tpl.Key, tpl.Name, tpl.Description, tpl.SpaceSlug, string(tpl.DefaultRole),
		tpl.PointOfContactUserID, tpl.PointOfContactEmail,
		variables, folders, groups, grants, markings, constraints, tags, tpl.Active, tpl.CreatedBy,
	)
	return scanProjectTemplate(row)
}

func normalizeCreateProjectTemplateRequest(body *models.CreateProjectTemplateRequest, createdBy *uuid.UUID) (*models.ProjectTemplate, error) {
	key, err := normalizeSlug(body.Key, "key")
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	spaceSlug, err := normalizeOptionalSlug(body.SpaceSlug, "space_slug")
	if err != nil {
		return nil, err
	}
	role := models.OntologyProjectRoleViewer
	if body.DefaultRole != nil {
		role, err = models.ParseOntologyProjectRole(string(*body.DefaultRole))
		if err != nil {
			return nil, err
		}
	}
	active := true
	if body.Active != nil {
		active = *body.Active
	}
	tpl := &models.ProjectTemplate{
		ID:                   ids.New(),
		Key:                  key,
		Name:                 name,
		Description:          strings.TrimSpace(body.Description),
		SpaceSlug:            spaceSlug,
		DefaultRole:          role,
		PointOfContactUserID: body.PointOfContactUserID,
		PointOfContactEmail:  normalizeOptionalString(body.PointOfContactEmail),
		Variables:            append([]models.ProjectTemplateVariable{}, body.Variables...),
		FolderStructure:      append([]models.ProjectTemplateFolderSpec{}, body.FolderStructure...),
		GeneratedGroups:      append([]models.ProjectTemplateGeneratedGroup{}, body.GeneratedGroups...),
		DefaultRoleGrants:    append([]models.ProjectTemplateRoleGrant{}, body.DefaultRoleGrants...),
		Markings:             append([]models.ProjectTemplateMarking{}, body.Markings...),
		Constraints:          append([]models.ProjectTemplateConstraint{}, body.Constraints...),
		GovernanceTags:       normalizeStringValues(body.GovernanceTags),
		Active:               active,
		CreatedBy:            createdBy,
	}
	if err := normalizeProjectTemplate(tpl); err != nil {
		return nil, err
	}
	return tpl, nil
}

func normalizeProjectTemplate(tpl *models.ProjectTemplate) error {
	if tpl.Variables == nil {
		tpl.Variables = []models.ProjectTemplateVariable{}
	}
	if tpl.FolderStructure == nil {
		tpl.FolderStructure = []models.ProjectTemplateFolderSpec{}
	}
	if tpl.GeneratedGroups == nil {
		tpl.GeneratedGroups = []models.ProjectTemplateGeneratedGroup{}
	}
	if tpl.DefaultRoleGrants == nil {
		tpl.DefaultRoleGrants = []models.ProjectTemplateRoleGrant{}
	}
	if tpl.Markings == nil {
		tpl.Markings = []models.ProjectTemplateMarking{}
	}
	if tpl.Constraints == nil {
		tpl.Constraints = []models.ProjectTemplateConstraint{}
	}
	if tpl.GovernanceTags == nil {
		tpl.GovernanceTags = []string{}
	}
	seenVariables := map[string]struct{}{}
	for i := range tpl.Variables {
		key, err := normalizeTemplateVariableKey(tpl.Variables[i].Key)
		if err != nil {
			return err
		}
		if _, ok := seenVariables[key]; ok {
			return fmt.Errorf("duplicate template variable %q", key)
		}
		seenVariables[key] = struct{}{}
		tpl.Variables[i].Key = key
		tpl.Variables[i].Label = strings.TrimSpace(tpl.Variables[i].Label)
		tpl.Variables[i].Description = strings.TrimSpace(tpl.Variables[i].Description)
		if tpl.Variables[i].DefaultValue != nil {
			v := strings.TrimSpace(*tpl.Variables[i].DefaultValue)
			tpl.Variables[i].DefaultValue = &v
		}
	}
	for i := range tpl.FolderStructure {
		if strings.TrimSpace(tpl.FolderStructure[i].Name) == "" {
			return fmt.Errorf("folder_structure[%d].name is required", i)
		}
		tpl.FolderStructure[i].Key = strings.TrimSpace(tpl.FolderStructure[i].Key)
		tpl.FolderStructure[i].Name = strings.TrimSpace(tpl.FolderStructure[i].Name)
		tpl.FolderStructure[i].Description = strings.TrimSpace(tpl.FolderStructure[i].Description)
		if tpl.FolderStructure[i].ParentKey != nil {
			parent := strings.TrimSpace(*tpl.FolderStructure[i].ParentKey)
			tpl.FolderStructure[i].ParentKey = &parent
		}
	}
	for i := range tpl.GeneratedGroups {
		role, err := models.ParseOntologyProjectRole(string(tpl.GeneratedGroups[i].Role))
		if err != nil {
			return fmt.Errorf("generated_groups[%d].role: %w", i, err)
		}
		tpl.GeneratedGroups[i].Role = role
		tpl.GeneratedGroups[i].SlugSuffix = strings.TrimSpace(tpl.GeneratedGroups[i].SlugSuffix)
		tpl.GeneratedGroups[i].DisplayNameTemplate = strings.TrimSpace(tpl.GeneratedGroups[i].DisplayNameTemplate)
		tpl.GeneratedGroups[i].Description = strings.TrimSpace(tpl.GeneratedGroups[i].Description)
		for j := range tpl.GeneratedGroups[i].ManagesGeneratedRoles {
			managed, err := models.ParseOntologyProjectRole(string(tpl.GeneratedGroups[i].ManagesGeneratedRoles[j]))
			if err != nil {
				return fmt.Errorf("generated_groups[%d].manages_generated_roles[%d]: %w", i, j, err)
			}
			tpl.GeneratedGroups[i].ManagesGeneratedRoles[j] = managed
		}
	}
	for i := range tpl.DefaultRoleGrants {
		role, err := models.ParseOntologyProjectRole(string(tpl.DefaultRoleGrants[i].Role))
		if err != nil {
			return fmt.Errorf("default_role_grants[%d].role: %w", i, err)
		}
		tpl.DefaultRoleGrants[i].Role = role
		if tpl.DefaultRoleGrants[i].GeneratedGroupRole != nil {
			parsed, err := models.ParseOntologyProjectRole(string(*tpl.DefaultRoleGrants[i].GeneratedGroupRole))
			if err != nil {
				return fmt.Errorf("default_role_grants[%d].generated_group_role: %w", i, err)
			}
			tpl.DefaultRoleGrants[i].GeneratedGroupRole = &parsed
		}
		if err := validateTemplateRoleGrant(tpl.DefaultRoleGrants[i]); err != nil {
			return fmt.Errorf("default_role_grants[%d]: %w", i, err)
		}
	}
	for i := range tpl.Markings {
		tpl.Markings[i].DisplayName = strings.TrimSpace(tpl.Markings[i].DisplayName)
		if tpl.Markings[i].DisplayName == "" {
			return fmt.Errorf("markings[%d].display_name is required", i)
		}
		if tpl.Markings[i].MarkingID == nil && tpl.Markings[i].MarkingRID == nil && !tpl.Markings[i].CreateIfMissing {
			return fmt.Errorf("markings[%d] must specify marking_id, marking_rid, or create_if_missing", i)
		}
		tpl.Markings[i].RequiredFor = normalizeStringValues(tpl.Markings[i].RequiredFor)
	}
	for i := range tpl.Constraints {
		tpl.Constraints[i].Name = strings.TrimSpace(tpl.Constraints[i].Name)
		if tpl.Constraints[i].Name == "" {
			return fmt.Errorf("constraints[%d].name is required", i)
		}
		tpl.Constraints[i].Mode = strings.TrimSpace(tpl.Constraints[i].Mode)
		if tpl.Constraints[i].Mode == "" {
			tpl.Constraints[i].Mode = "allow"
		}
		if tpl.Constraints[i].Metadata == nil {
			tpl.Constraints[i].Metadata = map[string]any{}
		}
	}
	return nil
}

func validateTemplateRoleGrant(grant models.ProjectTemplateRoleGrant) error {
	switch grant.PrincipalKind {
	case models.ProjectTemplatePrincipalUser, models.ProjectTemplatePrincipalGroup:
		if grant.PrincipalID == nil || *grant.PrincipalID == uuid.Nil {
			return errors.New("principal_id is required for user/group grants")
		}
	case models.ProjectTemplatePrincipalProjectCreator:
		if grant.PrincipalID != nil {
			return errors.New("principal_id must be omitted for project_creator grants")
		}
	case models.ProjectTemplatePrincipalGeneratedGroup:
		if grant.GeneratedGroupRole == nil {
			return errors.New("generated_group_role is required for generated_group grants")
		}
		parsed, err := models.ParseOntologyProjectRole(string(*grant.GeneratedGroupRole))
		if err != nil {
			return err
		}
		*grant.GeneratedGroupRole = parsed
	default:
		return fmt.Errorf("unsupported principal_kind %q", grant.PrincipalKind)
	}
	return nil
}

func (h *ProjectsHandlers) prepareProjectTemplateDeployment(
	ctx context.Context,
	claims *authmw.Claims,
	body *models.CreateOntologyProjectRequest,
	projectSlug string,
	projectName string,
) (*projectTemplateDeployment, error) {
	if body.TemplateKey == nil || strings.TrimSpace(*body.TemplateKey) == "" {
		return nil, nil
	}
	key, err := normalizeSlug(*body.TemplateKey, "template_key")
	if err != nil {
		return nil, projectTemplateHTTPError{status: http.StatusBadRequest, message: err.Error()}
	}
	template, err := loadProjectTemplateByKey(ctx, h.Pool, key)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, projectTemplateHTTPError{status: http.StatusNotFound, message: "project template not found"}
	}
	variables, err := resolveProjectTemplateVariables(template, body, projectSlug, projectName, claims.Sub)
	if err != nil {
		return nil, projectTemplateHTTPError{status: http.StatusBadRequest, message: err.Error()}
	}
	validation := validateProjectTemplateDeployment(claims, template)
	if !validation.Allowed {
		return nil, projectTemplateHTTPError{
			status:  http.StatusForbidden,
			message: "project template deployment denied: missing permissions: " + strings.Join(validation.MissingPermissions, ", "),
		}
	}
	if body.WorkspaceSlug == nil && template.SpaceSlug != nil {
		ws := *template.SpaceSlug
		body.WorkspaceSlug = &ws
	}
	if body.DefaultRole == nil {
		role := template.DefaultRole
		body.DefaultRole = &role
	}
	if body.PointOfContactUserID == nil && template.PointOfContactUserID != nil {
		id := *template.PointOfContactUserID
		body.PointOfContactUserID = &id
	}
	if body.PointOfContactEmail == nil && template.PointOfContactEmail != nil {
		email := *template.PointOfContactEmail
		body.PointOfContactEmail = &email
	}
	deployment := &projectTemplateDeployment{
		template:    template,
		variables:   variables,
		validation:  validation,
		constraints: applyTemplateConstraints(template.Constraints, variables),
	}
	for _, folder := range template.FolderStructure {
		request := models.CreateOntologyProjectFolderRequest{
			Name:        substituteTemplateString(folder.Name, variables),
			Description: stringPtrOrNil(substituteTemplateString(folder.Description, variables)),
		}
		parentKey := ""
		if folder.ParentKey != nil {
			parentKey = strings.TrimSpace(*folder.ParentKey)
		}
		deployment.folderPlans = append(deployment.folderPlans, projectTemplateFolderPlan{
			key:       strings.TrimSpace(folder.Key),
			parentKey: parentKey,
			request:   request,
		})
	}
	return deployment, nil
}

func (d *projectTemplateDeployment) insertFolders(
	ctx context.Context,
	tx pgx.Tx,
	projectID uuid.UUID,
	projectRID string,
	createdBy uuid.UUID,
) error {
	if d == nil || len(d.folderPlans) == 0 {
		return nil
	}
	createdByKey := map[string]uuid.UUID{}
	for i := range d.folderPlans {
		plan := d.folderPlans[i]
		var parentID *uuid.UUID
		if plan.parentKey != "" {
			resolved, ok := createdByKey[plan.parentKey]
			if !ok {
				return fmt.Errorf("template folder %q references unknown parent_key %q", plan.request.Name, plan.parentKey)
			}
			parentID = &resolved
		} else {
			resolved, status, msg, err := resolveProjectFolderParent(ctx, tx, projectID, projectRID, &plan.request)
			if err != nil {
				return err
			}
			if status != 0 {
				return projectTemplateHTTPError{status: status, message: msg}
			}
			parentID = resolved
		}
		folder, err := insertProjectFolder(ctx, tx, projectID, createdBy, &plan.request, parentID)
		if err != nil {
			return err
		}
		if plan.key != "" {
			createdByKey[plan.key] = folder.ID
		}
		if err := workspace.UpsertFolderSearchIndexTx(ctx, tx, folder.ID, workspace.ResourceSearchEventCreated); err != nil {
			return fmt.Errorf("failed to index ontology project folder: %w", err)
		}
	}
	return nil
}

func (d *projectTemplateDeployment) applyPostCreate(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, projectSlug string, actor uuid.UUID) error {
	if d == nil {
		return nil
	}
	generatedByRole := map[models.OntologyProjectRole]models.ProjectTemplateGeneratedGroupResult{}
	for _, spec := range d.template.GeneratedGroups {
		group, err := d.createGeneratedGroup(ctx, tx, projectID, projectSlug, actor, spec)
		if err != nil {
			return err
		}
		d.generated = append(d.generated, group)
		generatedByRole[group.Role] = group
	}
	for _, grant := range d.template.DefaultRoleGrants {
		if err := d.applyRoleGrant(ctx, tx, projectID, actor, grant, generatedByRole); err != nil {
			return err
		}
	}
	for _, marking := range d.template.Markings {
		applied, err := d.applyMarking(ctx, tx, projectID, marking)
		if err != nil {
			return err
		}
		d.markings = append(d.markings, applied)
		d.markingRIDs = append(d.markingRIDs, applied.MarkingRID)
	}
	if len(d.markingRIDs) > 0 {
		markingRIDs, err := json.Marshal(normalizeStringValues(d.markingRIDs))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE ontology_projects
			    SET marking_rids = $2::jsonb, updated_at = NOW()
			  WHERE id = $1`,
			projectID, markingRIDs,
		); err != nil {
			return fmt.Errorf("apply project template markings: %w", err)
		}
	}
	return d.recordApplication(ctx, tx, projectID, actor)
}

func (d *projectTemplateDeployment) createGeneratedGroup(
	ctx context.Context,
	tx pgx.Tx,
	projectID uuid.UUID,
	projectSlug string,
	actor uuid.UUID,
	spec models.ProjectTemplateGeneratedGroup,
) (models.ProjectTemplateGeneratedGroupResult, error) {
	groupID := ids.New()
	suffix := substituteTemplateString(spec.SlugSuffix, d.variables)
	if strings.TrimSpace(suffix) == "" {
		suffix = string(spec.Role) + "s"
	}
	slug, err := normalizeSlug(projectSlug+"-"+suffix, "generated_group_slug")
	if err != nil {
		return models.ProjectTemplateGeneratedGroupResult{}, err
	}
	displayName := substituteTemplateString(spec.DisplayNameTemplate, d.variables)
	if strings.TrimSpace(displayName) == "" {
		displayName = fmt.Sprintf("%s %ss", d.variables["project.name"], projectTemplateRoleTitle(spec.Role))
	}
	description := substituteTemplateString(spec.Description, d.variables)
	if _, err := tx.Exec(ctx,
		`INSERT INTO ontology_project_group_memberships
		   (project_id, group_id, role, granted_by)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (project_id, group_id) DO UPDATE
		   SET role = EXCLUDED.role, granted_by = EXCLUDED.granted_by, updated_at = NOW()`,
		projectID, groupID, string(spec.Role), actor,
	); err != nil {
		return models.ProjectTemplateGeneratedGroupResult{}, fmt.Errorf("bind generated %s group: %w", spec.Role, err)
	}
	reviewers, _ := json.Marshal([]uuid.UUID{actor})
	groupKind := models.ProjectAccessGroupKindInternal
	if spec.ExternalRequestMessage != nil || spec.ExternalRequestURL != nil {
		groupKind = models.ProjectAccessGroupKindExternal
	}
	excluded := !spec.Requestable
	if _, err := tx.Exec(ctx,
		`INSERT INTO ontology_project_access_group_settings (
		     project_id, group_id, group_display_name, group_kind, request_role,
		     reviewer_user_ids, external_request_message, external_request_url,
		     excluded_from_request_forms, updated_by
		 )
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10)
		 ON CONFLICT (project_id, group_id) DO UPDATE SET
		     group_display_name = EXCLUDED.group_display_name,
		     group_kind = EXCLUDED.group_kind,
		     request_role = EXCLUDED.request_role,
		     reviewer_user_ids = EXCLUDED.reviewer_user_ids,
		     external_request_message = EXCLUDED.external_request_message,
		     external_request_url = EXCLUDED.external_request_url,
		     excluded_from_request_forms = EXCLUDED.excluded_from_request_forms,
		     updated_by = EXCLUDED.updated_by,
		     updated_at = NOW()`,
		projectID, groupID, displayName, groupKind, string(spec.Role), reviewers,
		spec.ExternalRequestMessage, spec.ExternalRequestURL, excluded, actor,
	); err != nil {
		return models.ProjectTemplateGeneratedGroupResult{}, fmt.Errorf("record generated group request settings: %w", err)
	}
	return models.ProjectTemplateGeneratedGroupResult{
		GroupID:               groupID,
		Role:                  spec.Role,
		Slug:                  slug,
		DisplayName:           displayName,
		Description:           description,
		ManagesGeneratedRoles: append([]models.OntologyProjectRole{}, spec.ManagesGeneratedRoles...),
		Requestable:           spec.Requestable,
		ExternalRequestURL:    spec.ExternalRequestURL,
	}, nil
}

func (d *projectTemplateDeployment) applyRoleGrant(
	ctx context.Context,
	tx pgx.Tx,
	projectID uuid.UUID,
	actor uuid.UUID,
	grant models.ProjectTemplateRoleGrant,
	generatedByRole map[models.OntologyProjectRole]models.ProjectTemplateGeneratedGroupResult,
) error {
	switch grant.PrincipalKind {
	case models.ProjectTemplatePrincipalProjectCreator:
		_, err := tx.Exec(ctx,
			`INSERT INTO ontology_project_memberships (project_id, user_id, role)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (project_id, user_id) DO UPDATE
			   SET role = EXCLUDED.role, updated_at = NOW()`,
			projectID, actor, string(grant.Role),
		)
		return err
	case models.ProjectTemplatePrincipalUser:
		_, err := tx.Exec(ctx,
			`INSERT INTO ontology_project_memberships (project_id, user_id, role)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (project_id, user_id) DO UPDATE
			   SET role = EXCLUDED.role, updated_at = NOW()`,
			projectID, *grant.PrincipalID, string(grant.Role),
		)
		return err
	case models.ProjectTemplatePrincipalGroup:
		return upsertTemplateGroupGrant(ctx, tx, projectID, *grant.PrincipalID, grant.Role, actor)
	case models.ProjectTemplatePrincipalGeneratedGroup:
		group, ok := generatedByRole[*grant.GeneratedGroupRole]
		if !ok {
			return fmt.Errorf("generated group role %s was not created by this template", *grant.GeneratedGroupRole)
		}
		return upsertTemplateGroupGrant(ctx, tx, projectID, group.GroupID, grant.Role, actor)
	default:
		return fmt.Errorf("unsupported role grant principal_kind %q", grant.PrincipalKind)
	}
}

func upsertTemplateGroupGrant(ctx context.Context, tx pgx.Tx, projectID, groupID uuid.UUID, role models.OntologyProjectRole, actor uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO ontology_project_group_memberships
		   (project_id, group_id, role, granted_by)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (project_id, group_id) DO UPDATE
		   SET role = EXCLUDED.role, granted_by = EXCLUDED.granted_by, updated_at = NOW()`,
		projectID, groupID, string(role), actor,
	)
	return err
}

func (d *projectTemplateDeployment) applyMarking(
	ctx context.Context,
	tx pgx.Tx,
	projectID uuid.UUID,
	marking models.ProjectTemplateMarking,
) (models.ProjectTemplateAppliedMarking, error) {
	markingID := uuid.Nil
	created := false
	if marking.MarkingID != nil {
		markingID = *marking.MarkingID
	} else {
		markingID = ids.New()
		created = true
	}
	ridValue := ""
	if marking.MarkingRID != nil && strings.TrimSpace(*marking.MarkingRID) != "" {
		ridValue = strings.TrimSpace(*marking.MarkingRID)
	} else {
		ridValue = "ri.security.main.marking." + markingID.String()
	}
	displayName := substituteTemplateString(marking.DisplayName, d.variables)
	reviewersJSON, err := json.Marshal(marking.ReviewerUserIDs)
	if err != nil {
		return models.ProjectTemplateAppliedMarking{}, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO ontology_project_required_markings (
		     project_id, marking_id, marking_name, reason_prompt, reviewer_user_ids
		 )
		 VALUES ($1, $2, $3, $4, $5::jsonb)
		 ON CONFLICT (project_id, marking_id) DO UPDATE SET
		     marking_name = EXCLUDED.marking_name,
		     reason_prompt = EXCLUDED.reason_prompt,
		     reviewer_user_ids = EXCLUDED.reviewer_user_ids,
		     updated_at = NOW()`,
		projectID, markingID, displayName, marking.ReasonPrompt, reviewersJSON,
	); err != nil {
		return models.ProjectTemplateAppliedMarking{}, fmt.Errorf("apply project template marking: %w", err)
	}
	return models.ProjectTemplateAppliedMarking{
		MarkingID:       markingID,
		MarkingRID:      ridValue,
		DisplayName:     displayName,
		ReasonPrompt:    marking.ReasonPrompt,
		ReviewerUserIDs: append([]uuid.UUID{}, marking.ReviewerUserIDs...),
		Created:         created || marking.CreateIfMissing,
	}, nil
}

func (d *projectTemplateDeployment) recordApplication(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, actor uuid.UUID) error {
	variablesJSON, err := json.Marshal(d.variables)
	if err != nil {
		return err
	}
	generated := d.generated
	if generated == nil {
		generated = []models.ProjectTemplateGeneratedGroupResult{}
	}
	groupsJSON, err := json.Marshal(generated)
	if err != nil {
		return err
	}
	markings := d.markings
	if markings == nil {
		markings = []models.ProjectTemplateAppliedMarking{}
	}
	markingsJSON, err := json.Marshal(markings)
	if err != nil {
		return err
	}
	constraints := d.constraints
	if constraints == nil {
		constraints = []models.ProjectTemplateConstraint{}
	}
	constraintsJSON, err := json.Marshal(constraints)
	if err != nil {
		return err
	}
	validationJSON, err := json.Marshal(d.validation)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO ontology_project_template_applications (
		     id, template_id, template_key, project_id, applied_by,
		     variables, generated_groups, applied_markings, applied_constraints, validation
		 )
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8::jsonb, $9::jsonb, $10::jsonb)`,
		ids.New(), d.template.ID, d.template.Key, projectID, actor,
		variablesJSON, groupsJSON, markingsJSON, constraintsJSON, validationJSON,
	)
	if err != nil {
		return fmt.Errorf("record project template application: %w", err)
	}
	return nil
}

func listProjectTemplateApplications(ctx context.Context, pool *pgxpool.Pool, projectID uuid.UUID) ([]models.ProjectTemplateApplication, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, template_id, template_key, project_id, applied_by,
		        variables, generated_groups, applied_markings, applied_constraints, validation, created_at
		   FROM ontology_project_template_applications
		  WHERE project_id = $1
		  ORDER BY created_at DESC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ProjectTemplateApplication, 0)
	for rows.Next() {
		app, err := scanProjectTemplateApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, app)
	}
	return out, rows.Err()
}

func scanProjectTemplateApplication(row projectScannable) (models.ProjectTemplateApplication, error) {
	var app models.ProjectTemplateApplication
	var rawVariables, rawGroups, rawMarkings, rawConstraints, rawValidation []byte
	err := row.Scan(
		&app.ID, &app.TemplateID, &app.TemplateKey, &app.ProjectID, &app.AppliedBy,
		&rawVariables, &rawGroups, &rawMarkings, &rawConstraints, &rawValidation, &app.CreatedAt,
	)
	if err != nil {
		return app, err
	}
	if err := unmarshalJSONDefault(rawVariables, &app.Variables); err != nil {
		return app, err
	}
	if err := unmarshalJSONDefault(rawGroups, &app.GeneratedGroups); err != nil {
		return app, err
	}
	if err := unmarshalJSONDefault(rawMarkings, &app.AppliedMarkings); err != nil {
		return app, err
	}
	if err := unmarshalJSONDefault(rawConstraints, &app.AppliedConstraints); err != nil {
		return app, err
	}
	if err := unmarshalJSONDefault(rawValidation, &app.Validation); err != nil {
		return app, err
	}
	return app, nil
}

func resolveProjectTemplateVariables(
	template *models.ProjectTemplate,
	body *models.CreateOntologyProjectRequest,
	projectSlug string,
	projectName string,
	creator uuid.UUID,
) (map[string]string, error) {
	values := map[string]string{
		"project.slug": projectSlug,
		"project.name": projectName,
		"creator.id":   creator.String(),
	}
	if body.WorkspaceSlug != nil {
		values["workspace.slug"] = strings.TrimSpace(*body.WorkspaceSlug)
	} else if template.SpaceSlug != nil {
		values["workspace.slug"] = strings.TrimSpace(*template.SpaceSlug)
	}
	provided := map[string]string{}
	for k, v := range body.TemplateVariables {
		key, err := normalizeTemplateVariableKey(k)
		if err != nil {
			return nil, err
		}
		provided[key] = strings.TrimSpace(v)
	}
	for _, variable := range template.Variables {
		value, ok := provided[variable.Key]
		if !ok && variable.DefaultValue != nil {
			value = *variable.DefaultValue
			ok = true
		}
		if !ok && variable.Required {
			return nil, fmt.Errorf("template variable %q is required", variable.Key)
		}
		if ok {
			values[variable.Key] = value
			values["var."+variable.Key] = value
		}
	}
	for key, value := range provided {
		values[key] = value
		values["var."+key] = value
	}
	return values, nil
}

func substituteTemplateString(input string, values map[string]string) string {
	out := input
	for {
		start := strings.Index(out, "{{")
		if start < 0 {
			return out
		}
		end := strings.Index(out[start+2:], "}}")
		if end < 0 {
			return out
		}
		end += start + 2
		key := strings.TrimSpace(out[start+2 : end])
		value := values[key]
		out = out[:start] + value + out[end+2:]
	}
}

func validateProjectTemplateDeployment(claims *authmw.Claims, template *models.ProjectTemplate) models.ProjectTemplateValidationResult {
	result := models.ProjectTemplateValidationResult{Allowed: true}
	addCheck := func(key, description string, allowed bool, missing string) {
		result.Checks = append(result.Checks, models.ProjectTemplateValidationCheck{
			Key:         key,
			Allowed:     allowed,
			Description: description,
		})
		if !allowed {
			result.Allowed = false
			result.MissingPermissions = append(result.MissingPermissions, missing)
		}
	}
	defaultsRequirePermission := template.DefaultRole != models.OntologyProjectRoleViewer ||
		template.PointOfContactUserID != nil ||
		template.PointOfContactEmail != nil ||
		len(template.DefaultRoleGrants) > 0
	defaultAllowed := !defaultsRequirePermission ||
		hasAnyPermissionKey(claims, "projects:write", "projects:manage", "project_templates:deploy", "control_panel:write")
	addCheck("project-defaults", "Set template default role, point of contact and project defaults.", defaultAllowed, "projects:write or projects:manage")
	if len(template.GeneratedGroups) > 0 {
		allowed := hasAnyPermissionKey(claims, "groups:write", "groups:manage", "identity:write", "identity:manage", "control_panel:write")
		addCheck("generated-groups", "Create and bind generated owner/editor/viewer groups.", allowed, "groups:write or groups:manage")
	}
	if len(template.Markings) > 0 {
		allowed := hasAnyPermissionKey(claims, "markings:apply", "markings:write", "markings:manage", "control_panel:write")
		addCheck("markings", "Apply existing or newly generated markings to the project.", allowed, "markings:apply or markings:write")
	}
	if len(template.Constraints) > 0 {
		allowed := hasAnyPermissionKey(claims, "project_constraints:apply", "project_constraints:write", "control_panel:write")
		addCheck("constraints", "Apply project constraints configured by the template.", allowed, "project_constraints:apply or project_constraints:write")
	}
	if len(result.MissingPermissions) == 0 {
		result.MissingPermissions = []string{}
	}
	return result
}

func canWriteProjectTemplates(claims *authmw.Claims) bool {
	return hasAnyPermissionKey(claims, "project_templates:write", "projects:manage", "control_panel:write")
}

func hasAnyPermissionKey(claims *authmw.Claims, keys ...string) bool {
	if claims == nil {
		return false
	}
	for _, key := range keys {
		if claims.HasPermissionKey(key) {
			return true
		}
	}
	return false
}

func normalizeTemplateVariableKey(value string) (string, error) {
	key := strings.TrimSpace(strings.ToLower(value))
	key = strings.ReplaceAll(key, " ", "_")
	if key == "" {
		return "", errors.New("template variable key is required")
	}
	for _, ch := range key {
		ok := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' || ch == '.'
		if !ok {
			return "", fmt.Errorf("template variable key %q contains unsupported characters", value)
		}
	}
	return key, nil
}

func applyTemplateConstraints(in []models.ProjectTemplateConstraint, variables map[string]string) []models.ProjectTemplateConstraint {
	out := make([]models.ProjectTemplateConstraint, 0, len(in))
	for _, constraint := range in {
		next := constraint
		next.Name = substituteTemplateString(next.Name, variables)
		out = append(out, next)
	}
	return out
}

func normalizeStringValues(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, value := range in {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func stringPtrOrNil(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func projectTemplateRoleTitle(role models.OntologyProjectRole) string {
	switch role {
	case models.OntologyProjectRoleDiscoverer:
		return "Discoverer"
	case models.OntologyProjectRoleViewer:
		return "Viewer"
	case models.OntologyProjectRoleEditor:
		return "Editor"
	case models.OntologyProjectRoleOwner:
		return "Owner"
	default:
		return string(role)
	}
}

func unmarshalJSONDefault(raw []byte, dest any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dest)
}
