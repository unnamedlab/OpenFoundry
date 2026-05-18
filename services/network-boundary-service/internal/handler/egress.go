package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

const (
	EgressPolicyKindDirect           = "direct"
	EgressPolicyKindAgentProxy       = "agent_proxy"
	EgressPolicyKindSameRegionBucket = "same_region_bucket"

	EgressPolicyStatePendingApproval = "pending_approval"
	EgressPolicyStateActive          = "active"
	EgressPolicyStatePaused          = "paused"
	EgressPolicyStateRevoked         = "revoked"

	EgressApprovalStatusPending  = "pending"
	EgressApprovalStatusApproved = "approved"
	EgressApprovalStatusDenied   = "denied"

	egressPermissionManage        = "network-egress:manage"
	egressPermissionApprove       = "network-egress:approve"
	egressPermissionGrantImporter = "network-egress:grant-importer"
)

var (
	errNotFound        = errors.New("egress policy not found")
	errDestinationMove = errors.New("egress policy destinations are immutable")

	hostPattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
)

type EgressEndpoint struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type EgressPort struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type EgressRiskWarning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type EgressPolicyAuditEvent struct {
	ID                  string         `json:"id"`
	Timestamp           time.Time      `json:"timestamp"`
	ActorID             string         `json:"actor_id"`
	Action              string         `json:"action"`
	Categories          []string       `json:"categories"`
	Outcome             string         `json:"outcome"`
	Reason              string         `json:"reason,omitempty"`
	HighRisk            bool           `json:"high_risk,omitempty"`
	PotentialDataExport bool           `json:"potential_data_export,omitempty"`
	WorkloadID          string         `json:"workload_id,omitempty"`
	WorkloadKind        string         `json:"workload_kind,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
}

type EgressApprovalTask struct {
	ID             string         `json:"id"`
	PolicyID       string         `json:"policy_id"`
	Action         string         `json:"action"`
	Status         string         `json:"status"`
	RequestedBy    string         `json:"requested_by"`
	RequestedAt    time.Time      `json:"requested_at"`
	RequestedState string         `json:"requested_state,omitempty"`
	RequiredRoles  []string       `json:"required_roles"`
	Summary        string         `json:"summary"`
	Reason         string         `json:"reason,omitempty"`
	DecidedBy      string         `json:"decided_by,omitempty"`
	DecidedAt      *time.Time     `json:"decided_at,omitempty"`
	DecisionReason string         `json:"decision_reason,omitempty"`
	HighRisk       bool           `json:"high_risk"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type EgressPolicyWorkloadUsage struct {
	WorkloadID          string         `json:"workload_id"`
	WorkloadKind        string         `json:"workload_kind"`
	ActorID             string         `json:"actor_id"`
	OrganizationID      string         `json:"organization_id,omitempty"`
	LastDecision        string         `json:"last_decision"`
	LastUsedAt          time.Time      `json:"last_used_at"`
	PotentialDataExport bool           `json:"potential_data_export"`
	ExportRiskReason    string         `json:"export_risk_reason,omitempty"`
	Destination         EgressEndpoint `json:"destination"`
	Port                int            `json:"port,omitempty"`
}

type NetworkEgressPolicy struct {
	ID                       string                      `json:"id"`
	Name                     string                      `json:"name"`
	Description              string                      `json:"description"`
	Kind                     string                      `json:"kind"`
	Address                  EgressEndpoint              `json:"address"`
	Port                     EgressPort                  `json:"port"`
	Protocol                 string                      `json:"protocol,omitempty"`
	ProxyMode                string                      `json:"proxy_mode,omitempty"`
	SNIBehavior              string                      `json:"sni_behavior,omitempty"`
	Agents                   []string                    `json:"agents"`
	BucketName               string                      `json:"bucket_name,omitempty"`
	BucketAccessLevel        string                      `json:"bucket_access_level,omitempty"`
	State                    string                      `json:"state"`
	Status                   string                      `json:"status"`
	AllowedOrganizations     []string                    `json:"allowed_organizations"`
	IsGlobal                 bool                        `json:"is_global"`
	ViewerGrants             []string                    `json:"viewer_grants"`
	ImporterGrants           []string                    `json:"importer_grants"`
	AdminGrants              []string                    `json:"admin_grants"`
	Permissions              []string                    `json:"permissions"`
	ImportHighRisk           bool                        `json:"importer_grants_high_risk"`
	RiskWarnings             []EgressRiskWarning         `json:"risk_warnings"`
	EgressIPRanges           []string                    `json:"egress_ip_ranges"`
	AgentHosts               []string                    `json:"agent_hosts"`
	OverlapPolicyIDs         []string                    `json:"overlap_policy_ids"`
	BucketPolicyRequirements []string                    `json:"bucket_policy_requirements"`
	ApprovalTasks            []EgressApprovalTask        `json:"approval_tasks,omitempty"`
	WorkloadUsages           []EgressPolicyWorkloadUsage `json:"workload_usages,omitempty"`
	CreatedBy                string                      `json:"created_by"`
	UpdatedBy                string                      `json:"updated_by,omitempty"`
	CreatedAt                time.Time                   `json:"created_at"`
	UpdatedAt                time.Time                   `json:"updated_at"`
	ApprovedAt               *time.Time                  `json:"approved_at,omitempty"`
	PausedAt                 *time.Time                  `json:"paused_at,omitempty"`
	RevokedAt                *time.Time                  `json:"revoked_at,omitempty"`
	AuditEvents              []EgressPolicyAuditEvent    `json:"audit_events,omitempty"`
}

type CreateEgressPolicyRequest struct {
	Name                 string         `json:"name"`
	Description          string         `json:"description"`
	Kind                 string         `json:"kind"`
	Address              EgressEndpoint `json:"address"`
	Port                 EgressPort     `json:"port"`
	Protocol             string         `json:"protocol"`
	ProxyMode            string         `json:"proxy_mode"`
	SNIBehavior          string         `json:"sni_behavior"`
	Agents               []string       `json:"agents"`
	BucketName           string         `json:"bucket_name"`
	BucketAccessLevel    string         `json:"bucket_access_level"`
	State                string         `json:"state"`
	Status               string         `json:"status"`
	AllowedOrganizations []string       `json:"allowed_organizations"`
	IsGlobal             bool           `json:"is_global"`
	ViewerGrants         []string       `json:"viewer_grants"`
	ImporterGrants       []string       `json:"importer_grants"`
	AdminGrants          []string       `json:"admin_grants"`
	Permissions          []string       `json:"permissions"`
	Reason               string         `json:"reason"`
}

type UpdateEgressPolicyStateRequest struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

type UpdateEgressPolicySharingRequest struct {
	ViewerGrants   []string `json:"viewer_grants"`
	ImporterGrants []string `json:"importer_grants"`
	AdminGrants    []string `json:"admin_grants"`
	Permissions    []string `json:"permissions"`
	Reason         string   `json:"reason"`
}

type DecideEgressApprovalRequest struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type EvaluateWorkloadEgressRequest struct {
	WorkloadID     string         `json:"workload_id"`
	WorkloadKind   string         `json:"workload_kind"`
	PolicyIDs      []string       `json:"policy_ids"`
	Destination    EgressEndpoint `json:"destination"`
	Port           int            `json:"port"`
	Protocol       string         `json:"protocol"`
	OrganizationID string         `json:"organization_id"`
	ActorGrants    []string       `json:"actor_grants"`
}

type EgressPolicyRuntimeDecision struct {
	PolicyID string   `json:"policy_id,omitempty"`
	Allowed  bool     `json:"allowed"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Reasons  []string `json:"reasons,omitempty"`
}

type EvaluateWorkloadEgressResponse struct {
	Allowed    bool                          `json:"allowed"`
	WorkloadID string                        `json:"workload_id,omitempty"`
	Decisions  []EgressPolicyRuntimeDecision `json:"decisions"`
}

type EgressPolicyStore interface {
	ListPolicies() []NetworkEgressPolicy
	CreatePolicy(policy NetworkEgressPolicy) NetworkEgressPolicy
	GetPolicy(id string) (NetworkEgressPolicy, bool)
	ListApprovals(status string) []EgressApprovalTask
	RequestStateChange(id string, state string, actor string, reason string) (NetworkEgressPolicy, error)
	DecideApproval(taskID string, decision string, actor string, reason string) (NetworkEgressPolicy, EgressApprovalTask, error)
	UpdateState(id string, state string, actor string, reason string) (NetworkEgressPolicy, error)
	UpdateSharing(id string, viewer []string, importer []string, admin []string, actor string, reason string) (NetworkEgressPolicy, error)
	RecordRuntimeUse(policyID string, claims *authmw.Claims, body EvaluateWorkloadEgressRequest, decision EgressPolicyRuntimeDecision) error
}

type MemoryEgressPolicyStore struct {
	mu       sync.RWMutex
	policies map[string]NetworkEgressPolicy
}

func NewMemoryEgressPolicyStore() *MemoryEgressPolicyStore {
	return &MemoryEgressPolicyStore{policies: map[string]NetworkEgressPolicy{}}
}

func (s *MemoryEgressPolicyStore) ListPolicies() []NetworkEgressPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]NetworkEgressPolicy, 0, len(s.policies))
	for _, policy := range s.policies {
		out = append(out, decoratePolicyWithInventory(policy, s.policies))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (s *MemoryEgressPolicyStore) CreatePolicy(policy NetworkEgressPolicy) NetworkEgressPolicy {
	s.mu.Lock()
	defer s.mu.Unlock()
	policy = clonePolicy(policy)
	policy = decoratePolicyWithInventory(policy, s.policies)
	s.policies[policy.ID] = policy
	return clonePolicy(policy)
}

func (s *MemoryEgressPolicyStore) GetPolicy(id string) (NetworkEgressPolicy, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	policy, ok := s.policies[id]
	if !ok {
		return NetworkEgressPolicy{}, false
	}
	return decoratePolicyWithInventory(policy, s.policies), true
}

func (s *MemoryEgressPolicyStore) ListApprovals(status string) []EgressApprovalTask {
	status = strings.TrimSpace(status)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []EgressApprovalTask{}
	for _, policy := range s.policies {
		for _, task := range policy.ApprovalTasks {
			if status != "" && task.Status != status {
				continue
			}
			out = append(out, cloneApprovalTask(task))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RequestedAt.After(out[j].RequestedAt)
	})
	return out
}

func (s *MemoryEgressPolicyStore) RequestStateChange(id string, state string, actor string, reason string) (NetworkEgressPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	policy, ok := s.policies[id]
	if !ok {
		return NetworkEgressPolicy{}, errNotFound
	}
	state = normalizeState(state)
	if state == "" {
		return NetworkEgressPolicy{}, fmt.Errorf("state must be one of pending_approval, active, paused, revoked")
	}
	if policy.State == EgressPolicyStateRevoked && state != EgressPolicyStateRevoked {
		return NetworkEgressPolicy{}, fmt.Errorf("revoked egress policies cannot be reactivated")
	}
	task := newApprovalTask(policy.ID, "state_change", actor, reason, state, map[string]any{"from_state": policy.State, "to_state": state})
	policy.ApprovalTasks = append(policy.ApprovalTasks, task)
	policy.AuditEvents = append(policy.AuditEvents, auditEvent(actor, "network_egress.approval.requested", "pending", reason, true, map[string]any{"approval_task_id": task.ID, "requested_state": state}))
	policy.UpdatedAt = time.Now().UTC()
	policy.UpdatedBy = actor
	policy = decoratePolicyWithInventory(policy, s.policies)
	s.policies[id] = policy
	return clonePolicy(policy), nil
}

func (s *MemoryEgressPolicyStore) DecideApproval(taskID string, decision string, actor string, reason string) (NetworkEgressPolicy, EgressApprovalTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision != EgressApprovalStatusApproved && decision != EgressApprovalStatusDenied {
		return NetworkEgressPolicy{}, EgressApprovalTask{}, fmt.Errorf("decision must be approved or denied")
	}
	for policyID, policy := range s.policies {
		for idx, task := range policy.ApprovalTasks {
			if task.ID != taskID {
				continue
			}
			if task.Status != EgressApprovalStatusPending {
				return NetworkEgressPolicy{}, EgressApprovalTask{}, fmt.Errorf("approval task is already %s", task.Status)
			}
			now := time.Now().UTC()
			task.Status = decision
			task.DecidedBy = actor
			task.DecidedAt = &now
			task.DecisionReason = strings.TrimSpace(reason)
			policy.ApprovalTasks[idx] = task
			policy.UpdatedAt = now
			policy.UpdatedBy = actor
			if decision == EgressApprovalStatusApproved {
				switch task.Action {
				case "create_policy":
					policy.State = EgressPolicyStateActive
					policy.Status = EgressPolicyStateActive
					policy.ApprovedAt = &now
				case "state_change":
					policy.State = task.RequestedState
					policy.Status = task.RequestedState
					switch task.RequestedState {
					case EgressPolicyStateActive:
						policy.ApprovedAt = &now
						policy.PausedAt = nil
					case EgressPolicyStatePaused:
						policy.PausedAt = &now
					case EgressPolicyStateRevoked:
						policy.RevokedAt = &now
					}
				}
			} else if task.Action == "create_policy" {
				policy.State = EgressPolicyStateRevoked
				policy.Status = EgressPolicyStateRevoked
				policy.RevokedAt = &now
			}
			policy.AuditEvents = append(policy.AuditEvents, auditEvent(actor, "network_egress.approval."+decision, decision, reason, true, map[string]any{"approval_task_id": task.ID, "action": task.Action, "requested_state": task.RequestedState}))
			if decision == EgressApprovalStatusApproved && task.Action == "state_change" {
				policy.AuditEvents = append(policy.AuditEvents, auditEvent(actor, lifecycleAuditAction(task.RequestedState), "success", reason, task.RequestedState == EgressPolicyStateRevoked || task.RequestedState == EgressPolicyStatePaused, map[string]any{"state": task.RequestedState, "approval_task_id": task.ID, "approved_via_workflow": true}))
			}
			policy = decoratePolicyWithInventory(policy, s.policies)
			s.policies[policyID] = policy
			return clonePolicy(policy), cloneApprovalTask(task), nil
		}
	}
	return NetworkEgressPolicy{}, EgressApprovalTask{}, errNotFound
}

func (s *MemoryEgressPolicyStore) UpdateState(id string, state string, actor string, reason string) (NetworkEgressPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	policy, ok := s.policies[id]
	if !ok {
		return NetworkEgressPolicy{}, errNotFound
	}
	state = normalizeState(state)
	if state == "" {
		return NetworkEgressPolicy{}, fmt.Errorf("state must be one of pending_approval, active, paused, revoked")
	}
	if policy.State == EgressPolicyStateRevoked && state != EgressPolicyStateRevoked {
		return NetworkEgressPolicy{}, fmt.Errorf("revoked egress policies cannot be reactivated")
	}
	now := time.Now().UTC()
	previousState := policy.State
	policy.State = state
	policy.Status = state
	policy.UpdatedAt = now
	policy.UpdatedBy = actor
	switch state {
	case EgressPolicyStateActive:
		policy.ApprovedAt = &now
		policy.PausedAt = nil
	case EgressPolicyStatePaused:
		policy.PausedAt = &now
	case EgressPolicyStateRevoked:
		policy.RevokedAt = &now
	}
	task := newApprovalTask(policy.ID, "state_change", actor, reason, state, map[string]any{"from_state": previousState, "to_state": state})
	nowDecision := now
	task.Status = EgressApprovalStatusApproved
	task.DecidedBy = actor
	task.DecidedAt = &nowDecision
	task.DecisionReason = strings.TrimSpace(reason)
	policy.ApprovalTasks = append(policy.ApprovalTasks, task)
	policy.AuditEvents = append(policy.AuditEvents, auditEvent(actor, lifecycleAuditAction(state), "success", reason, state == EgressPolicyStateRevoked || state == EgressPolicyStatePaused, map[string]any{"state": state, "previous_state": previousState, "approval_task_id": task.ID}))
	policy = decoratePolicyWithInventory(policy, s.policies)
	s.policies[id] = policy
	return clonePolicy(policy), nil
}

func (s *MemoryEgressPolicyStore) UpdateSharing(id string, viewer []string, importer []string, admin []string, actor string, reason string) (NetworkEgressPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	policy, ok := s.policies[id]
	if !ok {
		return NetworkEgressPolicy{}, errNotFound
	}
	if hasDestinationMutation(policy, policy) {
		return NetworkEgressPolicy{}, errDestinationMove
	}
	policy.ViewerGrants = normalizeStringSet(viewer)
	policy.ImporterGrants = normalizeStringSet(importer)
	policy.AdminGrants = normalizeStringSet(admin)
	policy.Permissions = append([]string(nil), policy.ImporterGrants...)
	now := time.Now().UTC()
	policy.UpdatedAt = now
	policy.UpdatedBy = actor
	policy.AuditEvents = append(policy.AuditEvents, auditEvent(actor, "network_egress.policy.importer_grants_changed", "success", reason, true, map[string]any{"importer_grants": policy.ImporterGrants}))
	policy = decoratePolicyWithInventory(policy, s.policies)
	s.policies[id] = policy
	return clonePolicy(policy), nil
}

func (s *MemoryEgressPolicyStore) RecordRuntimeUse(policyID string, claims *authmw.Claims, body EvaluateWorkloadEgressRequest, decision EgressPolicyRuntimeDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	policy, ok := s.policies[policyID]
	if !ok {
		return errNotFound
	}
	actor := ""
	if claims != nil {
		actor = claims.Sub.String()
	}
	now := time.Now().UTC()
	outcome := "denied"
	if decision.Allowed {
		outcome = "success"
	}
	potentialExport := true
	usage := EgressPolicyWorkloadUsage{
		WorkloadID:          strings.TrimSpace(body.WorkloadID),
		WorkloadKind:        strings.TrimSpace(body.WorkloadKind),
		ActorID:             actor,
		OrganizationID:      strings.TrimSpace(body.OrganizationID),
		LastDecision:        outcome,
		LastUsedAt:          now,
		PotentialDataExport: potentialExport,
		ExportRiskReason:    "External egress can move data in either direction; review workloads that import this policy as possible export paths.",
		Destination:         body.Destination,
		Port:                body.Port,
	}
	policy.WorkloadUsages = upsertWorkloadUsage(policy.WorkloadUsages, usage)
	policy.AuditEvents = append(policy.AuditEvents, runtimeAuditEvent(actor, "network_egress.policy.used", outcome, potentialExport, body, decision))
	policy.UpdatedAt = now
	s.policies[policyID] = decoratePolicyWithInventory(policy, s.policies)
	return nil
}

type EgressHandler struct {
	Store EgressPolicyStore
}

func NewEgressHandler(store EgressPolicyStore) *EgressHandler {
	if store == nil {
		store = NewMemoryEgressPolicyStore()
	}
	return &EgressHandler{Store: store}
}

func (h *EgressHandler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	claims, ok := egressClaims(w, r)
	if !ok {
		return
	}
	policies := h.Store.ListPolicies()
	visible := make([]NetworkEgressPolicy, 0, len(policies))
	for _, policy := range policies {
		if policyVisibleTo(policy, claims) {
			visible = append(visible, policy)
		}
	}
	writeEgressJSON(w, http.StatusOK, visible)
}

func (h *EgressHandler) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	claims, ok := egressClaims(w, r)
	if !ok {
		return
	}
	var body CreateEgressPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeEgressError(w, http.StatusBadRequest, "invalid body")
		return
	}
	policy, err := policyFromCreate(body, claims)
	if err != nil {
		writeEgressError(w, http.StatusBadRequest, err.Error())
		return
	}
	if policy.State == EgressPolicyStateActive && !canApproveEgress(claims) {
		writeEgressError(w, http.StatusForbidden, "approval permission required to create an active egress policy")
		return
	}
	if policy.State == EgressPolicyStatePendingApproval {
		policy.ApprovalTasks = append(policy.ApprovalTasks, newApprovalTask(policy.ID, "create_policy", claims.Sub.String(), body.Reason, EgressPolicyStateActive, map[string]any{"kind": policy.Kind, "destination": policy.Address.Value}))
	} else if policy.State == EgressPolicyStateActive {
		task := newApprovalTask(policy.ID, "create_policy", claims.Sub.String(), body.Reason, EgressPolicyStateActive, map[string]any{"kind": policy.Kind, "destination": policy.Address.Value})
		now := time.Now().UTC()
		task.Status = EgressApprovalStatusApproved
		task.DecidedBy = claims.Sub.String()
		task.DecidedAt = &now
		task.DecisionReason = strings.TrimSpace(body.Reason)
		policy.ApprovalTasks = append(policy.ApprovalTasks, task)
	}
	policy.AuditEvents = append(policy.AuditEvents, auditEvent(claims.Sub.String(), "network_egress.policy.created", "success", body.Reason, false, map[string]any{"kind": policy.Kind, "state": policy.State}))
	if policy.State == EgressPolicyStatePendingApproval && len(policy.ApprovalTasks) > 0 {
		policy.AuditEvents = append(policy.AuditEvents, auditEvent(claims.Sub.String(), "network_egress.approval.requested", "pending", body.Reason, true, map[string]any{"approval_task_id": policy.ApprovalTasks[len(policy.ApprovalTasks)-1].ID, "action": "create_policy"}))
	}
	if len(policy.ImporterGrants) > 0 {
		policy.AuditEvents = append(policy.AuditEvents, auditEvent(claims.Sub.String(), "network_egress.policy.importer_grants_changed", "success", body.Reason, true, map[string]any{"importer_grants": policy.ImporterGrants}))
	}
	writeEgressJSON(w, http.StatusCreated, h.Store.CreatePolicy(policy))
}

func (h *EgressHandler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	claims, ok := egressClaims(w, r)
	if !ok {
		return
	}
	policy, found := h.Store.GetPolicy(chi.URLParam(r, "id"))
	if !found {
		writeEgressError(w, http.StatusNotFound, "egress policy not found")
		return
	}
	if !policyVisibleTo(policy, claims) {
		writeEgressError(w, http.StatusForbidden, "egress policy is not visible to this caller")
		return
	}
	writeEgressJSON(w, http.StatusOK, policy)
}

func (h *EgressHandler) ListApprovals(w http.ResponseWriter, r *http.Request) {
	claims, ok := egressClaims(w, r)
	if !ok {
		return
	}
	if !canApproveEgress(claims) {
		writeEgressError(w, http.StatusForbidden, "network egress approval permission required")
		return
	}
	writeEgressJSON(w, http.StatusOK, h.Store.ListApprovals(strings.TrimSpace(r.URL.Query().Get("status"))))
}

func (h *EgressHandler) DecideApproval(w http.ResponseWriter, r *http.Request) {
	claims, ok := egressClaims(w, r)
	if !ok {
		return
	}
	if !canApproveEgress(claims) {
		writeEgressError(w, http.StatusForbidden, "network egress approval permission required")
		return
	}
	var body DecideEgressApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeEgressError(w, http.StatusBadRequest, "invalid body")
		return
	}
	policy, task, err := h.Store.DecideApproval(chi.URLParam(r, "task_id"), body.Decision, claims.Sub.String(), body.Reason)
	if errors.Is(err, errNotFound) {
		writeEgressError(w, http.StatusNotFound, "approval task not found")
		return
	}
	if err != nil {
		writeEgressError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeEgressJSON(w, http.StatusOK, map[string]any{"policy": policy, "approval_task": task})
}

func (h *EgressHandler) UpdateState(w http.ResponseWriter, r *http.Request) {
	claims, ok := egressClaims(w, r)
	if !ok {
		return
	}
	var body UpdateEgressPolicyStateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeEgressError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(body.State) == "" {
		writeEgressError(w, http.StatusBadRequest, "state is required")
		return
	}
	if normalizeState(body.State) == "" {
		writeEgressError(w, http.StatusBadRequest, "state must be one of pending_approval, active, paused, revoked")
		return
	}
	current, found := h.Store.GetPolicy(chi.URLParam(r, "id"))
	if !found {
		writeEgressError(w, http.StatusNotFound, "egress policy not found")
		return
	}
	if !canManagePolicy(current, claims) && current.CreatedBy != claims.Sub.String() {
		writeEgressError(w, http.StatusForbidden, "network egress grant management permission required")
		return
	}
	if !canApproveEgress(claims) {
		policy, err := h.Store.RequestStateChange(current.ID, body.State, claims.Sub.String(), body.Reason)
		if err != nil {
			writeEgressError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeEgressJSON(w, http.StatusAccepted, policy)
		return
	}
	policy, err := h.Store.UpdateState(chi.URLParam(r, "id"), body.State, claims.Sub.String(), body.Reason)
	if errors.Is(err, errNotFound) {
		writeEgressError(w, http.StatusNotFound, "egress policy not found")
		return
	}
	if err != nil {
		writeEgressError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeEgressJSON(w, http.StatusOK, policy)
}

func (h *EgressHandler) UpdateSharing(w http.ResponseWriter, r *http.Request) {
	claims, ok := egressClaims(w, r)
	if !ok {
		return
	}
	var body UpdateEgressPolicySharingRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeEgressError(w, http.StatusBadRequest, "invalid body")
		return
	}
	policy, found := h.Store.GetPolicy(chi.URLParam(r, "id"))
	if !found {
		writeEgressError(w, http.StatusNotFound, "egress policy not found")
		return
	}
	if !canManagePolicy(policy, claims) {
		writeEgressError(w, http.StatusForbidden, "network egress grant management permission required")
		return
	}
	importer := body.ImporterGrants
	if len(importer) == 0 && len(body.Permissions) > 0 {
		importer = body.Permissions
	}
	updated, err := h.Store.UpdateSharing(policy.ID, body.ViewerGrants, importer, body.AdminGrants, claims.Sub.String(), body.Reason)
	if err != nil {
		writeEgressError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeEgressJSON(w, http.StatusOK, updated)
}

func (h *EgressHandler) DeletePolicy(w http.ResponseWriter, r *http.Request) {
	writeEgressError(w, http.StatusMethodNotAllowed, "egress policies are immutable; revoke the policy instead of deleting it")
}

func (h *EgressHandler) EvaluateWorkload(w http.ResponseWriter, r *http.Request) {
	claims, ok := egressClaims(w, r)
	if !ok {
		return
	}
	var body EvaluateWorkloadEgressRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeEgressError(w, http.StatusBadRequest, "invalid body")
		return
	}
	response := h.evaluateRuntime(body, claims)
	status := http.StatusOK
	if !response.Allowed {
		status = http.StatusForbidden
	}
	writeEgressJSON(w, status, response)
}

func (h *EgressHandler) evaluateRuntime(body EvaluateWorkloadEgressRequest, claims *authmw.Claims) EvaluateWorkloadEgressResponse {
	response := EvaluateWorkloadEgressResponse{Allowed: false, WorkloadID: strings.TrimSpace(body.WorkloadID), Decisions: []EgressPolicyRuntimeDecision{}}
	policyIDs := normalizeStringSet(body.PolicyIDs)
	if len(policyIDs) == 0 {
		response.Decisions = append(response.Decisions, EgressPolicyRuntimeDecision{
			Allowed: false,
			Code:    "explicit_policy_import_required",
			Message: "Workloads must import at least one explicit egress policy before reaching external destinations.",
		})
		return response
	}
	for _, policyID := range policyIDs {
		policy, found := h.Store.GetPolicy(policyID)
		if !found {
			response.Decisions = append(response.Decisions, EgressPolicyRuntimeDecision{PolicyID: policyID, Allowed: false, Code: "policy_not_found", Message: "Imported egress policy does not exist."})
			continue
		}
		decision := evaluatePolicyRuntimeUse(policy, body, claims)
		response.Decisions = append(response.Decisions, decision)
		_ = h.Store.RecordRuntimeUse(policy.ID, claims, body, decision)
	}
	if len(response.Decisions) > 0 {
		response.Allowed = true
		for _, decision := range response.Decisions {
			if !decision.Allowed {
				response.Allowed = false
				break
			}
		}
	}
	if !response.Allowed && len(response.Decisions) == 0 {
		response.Decisions = append(response.Decisions, EgressPolicyRuntimeDecision{Allowed: false, Code: "policy_not_allowed", Message: "No imported egress policy allows this workload request."})
	}
	return response
}

func evaluatePolicyRuntimeUse(policy NetworkEgressPolicy, body EvaluateWorkloadEgressRequest, claims *authmw.Claims) EgressPolicyRuntimeDecision {
	reasons := []string{}
	if policy.State != EgressPolicyStateActive {
		reasons = append(reasons, "policy_state_"+policy.State)
	}
	if org := strings.TrimSpace(body.OrganizationID); org != "" && len(policy.AllowedOrganizations) > 0 && !containsString(policy.AllowedOrganizations, org) {
		reasons = append(reasons, "organization_not_allowed")
	}
	if !callerCanImport(policy, claims, body.ActorGrants) {
		reasons = append(reasons, "importer_grant_required_high_risk")
	}
	if body.Destination.Value != "" && !policyAllowsDestination(policy, body.Destination) {
		reasons = append(reasons, "destination_not_allowed")
	}
	if body.Port > 0 && !policyAllowsPort(policy.Port, body.Port) {
		reasons = append(reasons, "port_not_allowed")
	}
	if len(reasons) > 0 {
		return EgressPolicyRuntimeDecision{
			PolicyID: policy.ID,
			Allowed:  false,
			Code:     "network_egress_denied",
			Message:  "Imported egress policy does not satisfy runtime state, importer, organization, destination, or port requirements.",
			Reasons:  reasons,
		}
	}
	return EgressPolicyRuntimeDecision{PolicyID: policy.ID, Allowed: true, Code: "network_egress_allowed", Message: "Imported active egress policy allows this workload request."}
}

func policyFromCreate(body CreateEgressPolicyRequest, claims *authmw.Claims) (NetworkEgressPolicy, error) {
	now := time.Now().UTC()
	kind := normalizeKind(body.Kind)
	state := normalizeState(firstNonBlank(body.State, body.Status))
	if state == "" {
		state = EgressPolicyStatePendingApproval
	}
	importer := body.ImporterGrants
	if len(importer) == 0 && len(body.Permissions) > 0 {
		importer = body.Permissions
	}
	policy := NetworkEgressPolicy{
		ID:                   uuid.NewString(),
		Name:                 strings.TrimSpace(body.Name),
		Description:          strings.TrimSpace(body.Description),
		Kind:                 kind,
		Address:              EgressEndpoint{Kind: strings.TrimSpace(body.Address.Kind), Value: strings.TrimSpace(body.Address.Value)},
		Port:                 EgressPort{Kind: strings.TrimSpace(body.Port.Kind), Value: strings.TrimSpace(body.Port.Value)},
		Protocol:             normalizeProtocol(body.Protocol),
		ProxyMode:            normalizeProxyMode(body.ProxyMode),
		SNIBehavior:          normalizeSNIBehavior(body.SNIBehavior),
		Agents:               normalizeStringSet(body.Agents),
		BucketName:           strings.TrimSpace(body.BucketName),
		BucketAccessLevel:    normalizeBucketAccessLevel(body.BucketAccessLevel),
		State:                state,
		Status:               state,
		AllowedOrganizations: normalizeStringSet(body.AllowedOrganizations),
		IsGlobal:             body.IsGlobal,
		ViewerGrants:         normalizeStringSet(body.ViewerGrants),
		ImporterGrants:       normalizeStringSet(importer),
		AdminGrants:          normalizeStringSet(body.AdminGrants),
		CreatedBy:            claims.Sub.String(),
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	policy.Permissions = append([]string(nil), policy.ImporterGrants...)
	if err := validatePolicy(policy); err != nil {
		return NetworkEgressPolicy{}, err
	}
	if policy.State == EgressPolicyStateActive {
		policy.ApprovedAt = &now
	}
	return decoratePolicy(policy), nil
}

func validatePolicy(policy NetworkEgressPolicy) error {
	if policy.Name == "" {
		return fmt.Errorf("name is required")
	}
	if policy.Kind == "" {
		return fmt.Errorf("kind must be direct, agent_proxy, or same_region_bucket")
	}
	if policy.Protocol == "" {
		return fmt.Errorf("protocol must be tcp, tls, http, or https")
	}
	if policy.SNIBehavior == "" {
		return fmt.Errorf("sni_behavior must be verify, disabled, or passthrough")
	}
	if policy.Address.Value == "" {
		return fmt.Errorf("address is required")
	}
	if err := validateEndpoint(policy.Kind, policy.Address); err != nil {
		return err
	}
	if err := validatePort(policy.Kind, policy.Address, policy.Port); err != nil {
		return err
	}
	switch policy.Kind {
	case EgressPolicyKindDirect:
		if policy.ProxyMode != "" && policy.ProxyMode != "none" {
			return fmt.Errorf("direct egress policies cannot use an agent proxy mode")
		}
	case EgressPolicyKindAgentProxy:
		if policy.ProxyMode == "" || policy.ProxyMode == "none" {
			return fmt.Errorf("agent proxy policies require http_connect, socks5, or mtls_tunnel proxy mode")
		}
		if len(policy.Agents) == 0 {
			return fmt.Errorf("agent proxy policies require at least one connector agent")
		}
	case EgressPolicyKindSameRegionBucket:
		if policy.BucketName == "" {
			return fmt.Errorf("same-region bucket egress policies require bucket_name")
		}
		if policy.BucketAccessLevel == "" {
			return fmt.Errorf("same-region bucket egress policies require bucket_access_level")
		}
		if policy.Port.Kind != "single" || strings.TrimSpace(policy.Port.Value) != "443" {
			return fmt.Errorf("same-region bucket egress policies must use port 443")
		}
	}
	return nil
}

func validateEndpoint(kind string, endpoint EgressEndpoint) error {
	endpointKind := strings.TrimSpace(endpoint.Kind)
	value := strings.ToLower(strings.TrimSpace(endpoint.Value))
	switch endpointKind {
	case "host":
		host := strings.TrimPrefix(value, "*.")
		if strings.Contains(value, "*") && !strings.HasPrefix(value, "*.") {
			return fmt.Errorf("wildcard host policies must use the *.example.com form")
		}
		if !hostPattern.MatchString(host) {
			return fmt.Errorf("host policies require a DNS name such as api.example.com or *.example.com")
		}
		if kind == EgressPolicyKindSameRegionBucket && strings.HasPrefix(value, "*.") {
			return fmt.Errorf("same-region bucket egress cannot use wildcard hosts")
		}
	case "ip":
		if kind == EgressPolicyKindSameRegionBucket {
			return fmt.Errorf("same-region bucket egress policies require a DNS host endpoint")
		}
		if _, err := netip.ParseAddr(value); err != nil {
			return fmt.Errorf("IP policies require an IP address such as 10.20.30.40")
		}
	case "cidr":
		if kind == EgressPolicyKindSameRegionBucket {
			return fmt.Errorf("same-region bucket egress policies require a DNS host endpoint")
		}
		if _, err := netip.ParsePrefix(value); err != nil {
			return fmt.Errorf("CIDR policies require a CIDR block such as 10.20.0.0/16")
		}
	default:
		return fmt.Errorf("address kind must be host, ip, or cidr")
	}
	return nil
}

func validatePort(kind string, endpoint EgressEndpoint, port EgressPort) error {
	switch strings.TrimSpace(port.Kind) {
	case "single":
		parsed, err := strconv.Atoi(strings.TrimSpace(port.Value))
		if err != nil || parsed < 1 || parsed > 65535 {
			return fmt.Errorf("port must be a number between 1 and 65535")
		}
	case "range":
		if endpoint.Kind == "host" {
			return fmt.Errorf("DNS host egress policies must use a single port")
		}
		parts := strings.Split(strings.TrimSpace(port.Value), "-")
		if len(parts) != 2 {
			return fmt.Errorf("port range must look like 8000-9000")
		}
		start, startErr := strconv.Atoi(strings.TrimSpace(parts[0]))
		end, endErr := strconv.Atoi(strings.TrimSpace(parts[1]))
		if startErr != nil || endErr != nil || start < 1 || end > 65535 || start > end {
			return fmt.Errorf("port range must be between 1 and 65535, with the lower port first")
		}
	case "any":
		if kind == EgressPolicyKindSameRegionBucket {
			return fmt.Errorf("same-region bucket egress policies must use port 443")
		}
	default:
		return fmt.Errorf("port kind must be single, range, or any")
	}
	return nil
}

func decoratePolicy(policy NetworkEgressPolicy) NetworkEgressPolicy {
	policy.Status = policy.State
	policy.ImportHighRisk = len(policy.ImporterGrants) > 0
	policy.RiskWarnings = nil
	policy.EgressIPRanges = nil
	policy.AgentHosts = nil
	policy.BucketPolicyRequirements = nil
	if len(policy.ImporterGrants) > 0 {
		policy.RiskWarnings = append(policy.RiskWarnings, EgressRiskWarning{
			Code:     "importer-grant-high-risk",
			Severity: "warning",
			Message:  "Importer grants let workloads import this egress policy and reach external destinations.",
		})
	}
	if policy.State != EgressPolicyStateActive {
		policy.RiskWarnings = append(policy.RiskWarnings, EgressRiskWarning{
			Code:     "runtime-blocked-by-state",
			Severity: "info",
			Message:  "Runtime egress is denied until the policy state is active.",
		})
	}
	if policy.Address.Kind == "ip" || policy.Address.Kind == "cidr" {
		policy.EgressIPRanges = append(policy.EgressIPRanges, policy.Address.Value)
		policy.RiskWarnings = append(policy.RiskWarnings, EgressRiskWarning{
			Code:     "ip-range-allowlist",
			Severity: "info",
			Message:  "Destination firewall rules should explicitly review this IP/CIDR egress range.",
		})
	}
	if policy.Kind == EgressPolicyKindAgentProxy {
		policy.AgentHosts = append([]string(nil), policy.Agents...)
		policy.RiskWarnings = append(policy.RiskWarnings, EgressRiskWarning{
			Code:     "agent-host-egress",
			Severity: "info",
			Message:  "Traffic for this policy originates from the listed connector agent host infrastructure.",
		})
	}
	if policy.Kind == EgressPolicyKindSameRegionBucket {
		policy.BucketPolicyRequirements = []string{
			"Require the S3 bucket policy to allow inbound traffic only from the enrollment VPC endpoint.",
			"Do not rely solely on VPC endpoint allowlisting; keep bucket authentication and object-level authorization in place.",
			"Bucket endpoint policies are not supported for virtual table workloads.",
		}
		policy.RiskWarnings = append(policy.RiskWarnings, EgressRiskWarning{
			Code:     "same-region-s3-bucket-policy-required",
			Severity: "warning",
			Message:  "Same-region S3 egress requires a matching bucket policy for the enrollment VPC endpoint and separate bucket authentication.",
		})
	}
	return clonePolicy(policy)
}

func decoratePolicyWithInventory(policy NetworkEgressPolicy, inventory map[string]NetworkEgressPolicy) NetworkEgressPolicy {
	policy = decoratePolicy(policy)
	policy.OverlapPolicyIDs = overlappingPolicyIDs(policy, inventory)
	if len(policy.OverlapPolicyIDs) > 0 {
		policy.RiskWarnings = append(policy.RiskWarnings, EgressRiskWarning{
			Code:     "overlapping-egress-policy",
			Severity: "warning",
			Message:  "This destination/port overlaps another policy; reviewers should consider granting importer access to an existing route or revoking the redundant policy.",
		})
	}
	return clonePolicy(policy)
}

func normalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case EgressPolicyKindDirect, "":
		return EgressPolicyKindDirect
	case EgressPolicyKindAgentProxy:
		return EgressPolicyKindAgentProxy
	case EgressPolicyKindSameRegionBucket, "bucket_endpoint", "same-region-bucket":
		return EgressPolicyKindSameRegionBucket
	default:
		return ""
	}
}

func normalizeState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", "pending_review", "pending-approval", "pending approval":
		return EgressPolicyStatePendingApproval
	case EgressPolicyStatePendingApproval:
		return EgressPolicyStatePendingApproval
	case EgressPolicyStateActive:
		return EgressPolicyStateActive
	case EgressPolicyStatePaused:
		return EgressPolicyStatePaused
	case EgressPolicyStateRevoked:
		return EgressPolicyStateRevoked
	default:
		return ""
	}
}

func normalizeProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "", "tcp":
		return "tcp"
	case "tls", "http", "https":
		return strings.ToLower(strings.TrimSpace(protocol))
	default:
		return ""
	}
}

func normalizeProxyMode(proxyMode string) string {
	switch strings.ToLower(strings.TrimSpace(proxyMode)) {
	case "", "none":
		return "none"
	case "http_connect", "socks5", "mtls_tunnel":
		return strings.ToLower(strings.TrimSpace(proxyMode))
	default:
		return ""
	}
}

func normalizeSNIBehavior(sni string) string {
	switch strings.ToLower(strings.TrimSpace(sni)) {
	case "", "verify":
		return "verify"
	case "disabled", "passthrough":
		return strings.ToLower(strings.TrimSpace(sni))
	default:
		return ""
	}
}

func normalizeBucketAccessLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "":
		return ""
	case "read", "write", "read_write":
		return strings.ToLower(strings.TrimSpace(level))
	default:
		return ""
	}
}

func normalizeStringSet(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func policyVisibleTo(policy NetworkEgressPolicy, claims *authmw.Claims) bool {
	return canManageEgress(claims) ||
		policy.CreatedBy == claims.Sub.String() ||
		matchesGrantList(policy.ViewerGrants, claims, nil) ||
		matchesGrantList(policy.ImporterGrants, claims, nil) ||
		matchesGrantList(policy.AdminGrants, claims, nil)
}

func canManagePolicy(policy NetworkEgressPolicy, claims *authmw.Claims) bool {
	return canManageEgress(claims) || matchesGrantList(policy.AdminGrants, claims, nil)
}

func callerCanImport(policy NetworkEgressPolicy, claims *authmw.Claims, actorGrants []string) bool {
	return canManageEgress(claims) || matchesGrantList(policy.ImporterGrants, claims, actorGrants)
}

func canManageEgress(claims *authmw.Claims) bool {
	return claims != nil && (claims.HasRole("admin") ||
		claims.HasPermissionKey(egressPermissionManage) ||
		claims.HasPermissionKey("network.egress.manage") ||
		claims.HasPermissionKey("data_connection.egress.manage"))
}

func canApproveEgress(claims *authmw.Claims) bool {
	return canManageEgress(claims) ||
		(claims != nil && (claims.HasPermissionKey(egressPermissionApprove) ||
			claims.HasPermissionKey(egressPermissionGrantImporter) ||
			claims.HasPermissionKey("network.egress.approve")))
}

func matchesGrantList(grants []string, claims *authmw.Claims, actorGrants []string) bool {
	if len(grants) == 0 || claims == nil {
		return false
	}
	candidates := map[string]struct{}{
		strings.ToLower(claims.Sub.String()): {},
		strings.ToLower(claims.Email):        {},
	}
	for _, role := range claims.Roles {
		candidates[strings.ToLower("role:"+role)] = struct{}{}
		candidates[strings.ToLower(role)] = struct{}{}
	}
	for _, permission := range claims.Permissions {
		candidates[strings.ToLower("permission:"+permission)] = struct{}{}
		candidates[strings.ToLower(permission)] = struct{}{}
	}
	for _, grant := range actorGrants {
		candidates[strings.ToLower(strings.TrimSpace(grant))] = struct{}{}
	}
	for _, grant := range grants {
		key := strings.ToLower(strings.TrimSpace(grant))
		if key == "*" {
			return true
		}
		if _, ok := candidates[key]; ok {
			return true
		}
	}
	return false
}

func policyAllowsDestination(policy NetworkEgressPolicy, destination EgressEndpoint) bool {
	policyKind := strings.ToLower(strings.TrimSpace(policy.Address.Kind))
	value := strings.ToLower(strings.TrimSpace(policy.Address.Value))
	target := strings.ToLower(strings.TrimSpace(destination.Value))
	switch policyKind {
	case "host":
		if strings.HasPrefix(value, "*.") {
			return strings.HasSuffix(target, strings.TrimPrefix(value, "*"))
		}
		return target == value
	case "ip":
		return target == value
	case "cidr":
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return false
		}
		addr, err := netip.ParseAddr(target)
		return err == nil && prefix.Contains(addr)
	default:
		return false
	}
}

func policyAllowsPort(port EgressPort, candidate int) bool {
	if candidate < 1 || candidate > 65535 {
		return false
	}
	switch strings.TrimSpace(port.Kind) {
	case "any":
		return true
	case "single":
		parsed, err := strconv.Atoi(strings.TrimSpace(port.Value))
		return err == nil && parsed == candidate
	case "range":
		parts := strings.Split(strings.TrimSpace(port.Value), "-")
		if len(parts) != 2 {
			return false
		}
		start, startErr := strconv.Atoi(strings.TrimSpace(parts[0]))
		end, endErr := strconv.Atoi(strings.TrimSpace(parts[1]))
		return startErr == nil && endErr == nil && candidate >= start && candidate <= end
	default:
		return false
	}
}

func overlappingPolicyIDs(policy NetworkEgressPolicy, inventory map[string]NetworkEgressPolicy) []string {
	out := []string{}
	for id, other := range inventory {
		if id == policy.ID {
			continue
		}
		if policiesOverlap(policy, other) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func policiesOverlap(left, right NetworkEgressPolicy) bool {
	if left.Kind != right.Kind {
		return false
	}
	return addressesOverlap(left.Address, right.Address) && portsOverlap(left.Port, right.Port)
}

func addressesOverlap(left, right EgressEndpoint) bool {
	leftKind := strings.TrimSpace(left.Kind)
	rightKind := strings.TrimSpace(right.Kind)
	leftValue := strings.ToLower(strings.TrimSpace(left.Value))
	rightValue := strings.ToLower(strings.TrimSpace(right.Value))
	if leftValue == "" || rightValue == "" {
		return false
	}
	if leftKind == "host" || rightKind == "host" {
		if leftKind != "host" || rightKind != "host" {
			return false
		}
		return hostMatches(leftValue, rightValue) || hostMatches(rightValue, leftValue)
	}
	if leftKind == "ip" && rightKind == "ip" {
		return leftValue == rightValue
	}
	if leftKind == "cidr" && rightKind == "cidr" {
		leftPrefix, leftErr := netip.ParsePrefix(leftValue)
		rightPrefix, rightErr := netip.ParsePrefix(rightValue)
		return leftErr == nil && rightErr == nil && (leftPrefix.Contains(rightPrefix.Addr()) || rightPrefix.Contains(leftPrefix.Addr()))
	}
	if leftKind == "cidr" && rightKind == "ip" {
		prefix, prefixErr := netip.ParsePrefix(leftValue)
		addr, addrErr := netip.ParseAddr(rightValue)
		return prefixErr == nil && addrErr == nil && prefix.Contains(addr)
	}
	if leftKind == "ip" && rightKind == "cidr" {
		return addressesOverlap(right, left)
	}
	return false
}

func hostMatches(pattern string, candidate string) bool {
	if strings.HasPrefix(pattern, "*.") {
		return strings.HasSuffix(candidate, strings.TrimPrefix(pattern, "*"))
	}
	return pattern == candidate
}

func portsOverlap(left, right EgressPort) bool {
	leftStart, leftEnd, leftOK := portBounds(left)
	rightStart, rightEnd, rightOK := portBounds(right)
	if !leftOK || !rightOK {
		return false
	}
	return leftStart <= rightEnd && rightStart <= leftEnd
}

func portBounds(port EgressPort) (int, int, bool) {
	switch strings.TrimSpace(port.Kind) {
	case "any":
		return 1, 65535, true
	case "single":
		parsed, err := strconv.Atoi(strings.TrimSpace(port.Value))
		return parsed, parsed, err == nil
	case "range":
		parts := strings.Split(strings.TrimSpace(port.Value), "-")
		if len(parts) != 2 {
			return 0, 0, false
		}
		start, startErr := strconv.Atoi(strings.TrimSpace(parts[0]))
		end, endErr := strconv.Atoi(strings.TrimSpace(parts[1]))
		return start, end, startErr == nil && endErr == nil
	default:
		return 0, 0, false
	}
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasDestinationMutation(existing, candidate NetworkEgressPolicy) bool {
	return existing.Kind != candidate.Kind ||
		existing.Address != candidate.Address ||
		existing.Port != candidate.Port ||
		existing.Protocol != candidate.Protocol ||
		existing.BucketName != candidate.BucketName
}

func newApprovalTask(policyID string, action string, actor string, reason string, requestedState string, metadata map[string]any) EgressApprovalTask {
	summary := "Review network egress policy proposal"
	if action == "state_change" {
		summary = "Review sensitive network egress lifecycle change"
	}
	return EgressApprovalTask{
		ID:             uuid.NewString(),
		PolicyID:       policyID,
		Action:         action,
		Status:         EgressApprovalStatusPending,
		RequestedBy:    actor,
		RequestedAt:    time.Now().UTC(),
		RequestedState: requestedState,
		RequiredRoles:  []string{"Information Security Officer", "Enrollment Administrator"},
		Summary:        summary,
		Reason:         strings.TrimSpace(reason),
		HighRisk:       true,
		Metadata:       metadata,
	}
}

func upsertWorkloadUsage(usages []EgressPolicyWorkloadUsage, usage EgressPolicyWorkloadUsage) []EgressPolicyWorkloadUsage {
	key := usage.WorkloadID
	if key == "" {
		key = usage.WorkloadKind + ":" + usage.ActorID
	}
	for idx, existing := range usages {
		existingKey := existing.WorkloadID
		if existingKey == "" {
			existingKey = existing.WorkloadKind + ":" + existing.ActorID
		}
		if existingKey == key {
			usages[idx] = usage
			return usages
		}
	}
	return append(usages, usage)
}

func runtimeAuditEvent(actor string, action string, outcome string, potentialExport bool, body EvaluateWorkloadEgressRequest, decision EgressPolicyRuntimeDecision) EgressPolicyAuditEvent {
	event := auditEvent(actor, action, outcome, decision.Message, potentialExport, map[string]any{
		"decision_code": decision.Code,
		"reasons":       decision.Reasons,
		"destination":   body.Destination.Value,
		"port":          body.Port,
	})
	event.WorkloadID = strings.TrimSpace(body.WorkloadID)
	event.WorkloadKind = strings.TrimSpace(body.WorkloadKind)
	event.PotentialDataExport = potentialExport
	return event
}

func auditEvent(actor, action, outcome, reason string, highRisk bool, metadata map[string]any) EgressPolicyAuditEvent {
	potentialExport := potentialDataExportAction(action)
	return EgressPolicyAuditEvent{
		ID:                  uuid.NewString(),
		Timestamp:           time.Now().UTC(),
		ActorID:             actor,
		Action:              action,
		Categories:          auditCategories(action, highRisk),
		Outcome:             outcome,
		Reason:              strings.TrimSpace(reason),
		HighRisk:            highRisk,
		PotentialDataExport: potentialExport,
		Metadata:            metadata,
	}
}

func lifecycleAuditAction(state string) string {
	switch state {
	case EgressPolicyStatePaused:
		return "network_egress.policy.paused"
	case EgressPolicyStateRevoked:
		return "network_egress.policy.revoked"
	case EgressPolicyStateActive:
		return "network_egress.policy.activated"
	default:
		return "network_egress.policy.state_changed"
	}
}

func potentialDataExportAction(action string) bool {
	return strings.HasPrefix(action, "network_egress.policy.") || strings.Contains(action, "egress_policy_attached") || strings.Contains(action, "egress_policy_detached")
}

func auditCategories(action string, highRisk bool) []string {
	categories := []string{"networkEgress"}
	if strings.Contains(action, "used") || potentialDataExportAction(action) {
		categories = append(categories, "dataExport")
	}
	if strings.Contains(action, "created") || strings.Contains(action, "changed") || strings.Contains(action, "approval") || strings.Contains(action, "activated") || strings.Contains(action, "paused") || strings.Contains(action, "revoked") || highRisk {
		categories = append(categories, "managementPermissions")
	}
	return normalizeStringSet(categories)
}

func clonePolicy(policy NetworkEgressPolicy) NetworkEgressPolicy {
	policy.Agents = append([]string(nil), policy.Agents...)
	policy.AllowedOrganizations = append([]string(nil), policy.AllowedOrganizations...)
	policy.ViewerGrants = append([]string(nil), policy.ViewerGrants...)
	policy.ImporterGrants = append([]string(nil), policy.ImporterGrants...)
	policy.AdminGrants = append([]string(nil), policy.AdminGrants...)
	policy.Permissions = append([]string(nil), policy.Permissions...)
	policy.RiskWarnings = append([]EgressRiskWarning(nil), policy.RiskWarnings...)
	policy.EgressIPRanges = append([]string(nil), policy.EgressIPRanges...)
	policy.AgentHosts = append([]string(nil), policy.AgentHosts...)
	policy.OverlapPolicyIDs = append([]string(nil), policy.OverlapPolicyIDs...)
	policy.BucketPolicyRequirements = append([]string(nil), policy.BucketPolicyRequirements...)
	policy.ApprovalTasks = append([]EgressApprovalTask(nil), policy.ApprovalTasks...)
	for idx := range policy.ApprovalTasks {
		policy.ApprovalTasks[idx] = cloneApprovalTask(policy.ApprovalTasks[idx])
	}
	policy.WorkloadUsages = append([]EgressPolicyWorkloadUsage(nil), policy.WorkloadUsages...)
	policy.AuditEvents = append([]EgressPolicyAuditEvent(nil), policy.AuditEvents...)
	return policy
}

func cloneApprovalTask(task EgressApprovalTask) EgressApprovalTask {
	task.RequiredRoles = append([]string(nil), task.RequiredRoles...)
	if task.Metadata != nil {
		metadata := map[string]any{}
		for key, value := range task.Metadata {
			metadata[key] = value
		}
		task.Metadata = metadata
	}
	return task
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func egressClaims(w http.ResponseWriter, r *http.Request) (*authmw.Claims, bool) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok || claims == nil {
		writeEgressError(w, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	return claims, true
}

func writeEgressJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeEgressError(w http.ResponseWriter, status int, message string) {
	writeEgressJSON(w, status, map[string]string{"error": message})
}
