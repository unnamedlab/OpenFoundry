package governance

import "strings"

const (
	ExecutionIdentityUser    = "user"
	ExecutionIdentityService = "service_account"
)

type AIExecutionRequest struct {
	SessionID                  string
	InvokingUserID             string
	ConfiguredServiceAccountID string
	EffectiveIdentityType      string
	EffectiveSubjectID         string
	Operation                  string
	Mutating                   bool
	UserApproved               bool
	SessionPreApproved         bool
	ProtectedBranch            bool
	LLMUsageSubjectID          string
	RateLimitSubjectID         string
	AISessionLogEnabled        bool
	StandardAuditLogEnabled    bool
}

type AIExecutionDecision struct {
	Allowed            bool     `json:"allowed"`
	IdentitySubjectID  string   `json:"identity_subject_id"`
	LLMAttributionID   string   `json:"llm_attribution_id"`
	RateLimitSubjectID string   `json:"rate_limit_subject_id"`
	RequiresApproval   bool     `json:"requires_approval"`
	RequiresAudit      bool     `json:"requires_audit"`
	BlockingReasons    []string `json:"blocking_reasons"`
	GovernanceWarnings []string `json:"governance_warnings"`
}

func EvaluateAIExecution(req AIExecutionRequest) AIExecutionDecision {
	identityType := strings.TrimSpace(req.EffectiveIdentityType)
	if identityType == "" {
		identityType = ExecutionIdentityUser
	}
	identitySubject := strings.TrimSpace(req.EffectiveSubjectID)
	if identitySubject == "" && identityType == ExecutionIdentityUser {
		identitySubject = strings.TrimSpace(req.InvokingUserID)
	}
	decision := AIExecutionDecision{
		Allowed:            true,
		IdentitySubjectID:  identitySubject,
		LLMAttributionID:   strings.TrimSpace(req.LLMUsageSubjectID),
		RateLimitSubjectID: strings.TrimSpace(req.RateLimitSubjectID),
		RequiresApproval:   req.Mutating && !req.UserApproved && !(req.SessionPreApproved && !req.ProtectedBranch),
		RequiresAudit:      true,
	}
	if decision.LLMAttributionID == "" {
		decision.LLMAttributionID = identitySubject
	}
	if decision.RateLimitSubjectID == "" {
		decision.RateLimitSubjectID = decision.LLMAttributionID
	}
	if strings.TrimSpace(req.InvokingUserID) == "" {
		decision.BlockingReasons = append(decision.BlockingReasons, "invoking user identity is required")
	}
	switch identityType {
	case ExecutionIdentityUser:
		if identitySubject != strings.TrimSpace(req.InvokingUserID) {
			decision.BlockingReasons = append(decision.BlockingReasons, "AI user-scoped execution cannot change the effective user identity")
		}
	case ExecutionIdentityService:
		if strings.TrimSpace(req.ConfiguredServiceAccountID) == "" || identitySubject != strings.TrimSpace(req.ConfiguredServiceAccountID) {
			decision.BlockingReasons = append(decision.BlockingReasons, "service-account execution requires an explicitly configured service identity")
		}
	default:
		decision.BlockingReasons = append(decision.BlockingReasons, "effective identity type must be user or service_account")
	}
	if decision.RequiresApproval {
		decision.BlockingReasons = append(decision.BlockingReasons, "mutating AI operation requires explicit user approval")
	}
	if req.Mutating && req.ProtectedBranch && req.SessionPreApproved && !req.UserApproved {
		decision.GovernanceWarnings = append(decision.GovernanceWarnings, "session pre-approval is not sufficient for protected-branch mutation")
	}
	if !req.AISessionLogEnabled || !req.StandardAuditLogEnabled {
		decision.BlockingReasons = append(decision.BlockingReasons, "AI operations require both AI session logs and standard audit logs")
	}
	if len(decision.BlockingReasons) > 0 {
		decision.Allowed = false
	}
	return decision
}
