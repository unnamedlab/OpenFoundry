// Package effectdispatcher ports
// `services/workflow-automation-service/src/domain/effect_dispatcher.rs`
// 1:1.
//
// Calls `ontology-actions-service::POST /api/v1/ontology/actions/{id}/execute`
// on behalf of an AutomationRun. Same endpoint, same body shape, same
// retry envelope (5 attempts, 30s initial → 10m max, exponential
// backoff matching the legacy Go activity).
package effectdispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const headerAuditCorrelation = "x-audit-correlation-id"

// RetryPolicy mirrors the Rust struct of the same name. Defaults match
// the legacy Go activity (workers-go/workflow-automation/workflows/
// automation_run.go::ao).
type RetryPolicy struct {
	MaxAttempts        uint32
	InitialBackoff     time.Duration
	MaxBackoff         time.Duration
	BackoffMultiplier  float64
}

// DefaultRetryPolicy mirrors `impl Default for RetryPolicy`.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:       5,
		InitialBackoff:    30 * time.Second,
		MaxBackoff:        600 * time.Second,
		BackoffMultiplier: 2.0,
	}
}

// NextBackoff mirrors `RetryPolicy::next_backoff`. Returns 0 for the
// very first attempt; otherwise initial * mult^(attempt-2), capped at
// max_backoff.
func (p RetryPolicy) NextBackoff(attemptNumber uint32) time.Duration {
	if attemptNumber <= 1 {
		return 0
	}
	exponent := int(attemptNumber) - 2
	raw := float64(p.InitialBackoff.Seconds()) * math.Pow(p.BackoffMultiplier, float64(exponent))
	max := p.MaxBackoff.Seconds()
	if raw > max {
		raw = max
	}
	if raw < 0 {
		raw = 0
	}
	return time.Duration(raw * float64(time.Second))
}

// ErrorKind discriminates DispatchError variants 1:1 with the Rust enum.
type ErrorKind string

const (
	KindInvalidPayload ErrorKind = "invalid_payload"
	KindUnconfigured   ErrorKind = "unconfigured"
	KindNonRetryable   ErrorKind = "non_retryable"
	KindRetryable      ErrorKind = "retryable"
	KindExhausted      ErrorKind = "exhausted"
)

// DispatchError mirrors the Rust enum.
type DispatchError struct {
	Kind     ErrorKind
	Status   uint16 // KindNonRetryable
	Message  string
	Attempts uint32 // KindExhausted
}

// Error renders the DispatchError to a string in the same shape as
// the Rust thiserror display strings.
func (e *DispatchError) Error() string {
	switch e.Kind {
	case KindInvalidPayload:
		return "invalid trigger payload: " + e.Message
	case KindUnconfigured:
		return "ontology-actions-service unconfigured: " + e.Message
	case KindNonRetryable:
		return fmt.Sprintf("upstream non-retryable %d: %s", e.Status, e.Message)
	case KindRetryable:
		return "upstream retryable error: " + e.Message
	case KindExhausted:
		return fmt.Sprintf("retry envelope exhausted after %d attempts: %s", e.Attempts, e.Message)
	default:
		return e.Message
	}
}

// IsTerminal returns true for error variants that should land the run
// in a terminal Failed state without further attempts.
func (e *DispatchError) IsTerminal() bool {
	switch e.Kind {
	case KindInvalidPayload, KindUnconfigured, KindNonRetryable, KindExhausted:
		return true
	}
	return false
}

// AsDispatchError extracts a *DispatchError from an err, or nil.
func AsDispatchError(err error) *DispatchError {
	var de *DispatchError
	if errors.As(err, &de) {
		return de
	}
	return nil
}

// OntologyActionRequest mirrors the materialised request handed to
// the dispatcher. Kept separate from AutomateConditionV1 so the
// dispatcher contract is testable without going through serde.
type OntologyActionRequest struct {
	ActionID            string
	TargetObjectID      *string
	Parameters          json.RawMessage
	Justification       *string
	AuditCorrelationID  uuid.UUID
}

// Body mirrors `OntologyActionRequest::body`. Includes only the
// optional fields that are present.
func (r *OntologyActionRequest) Body() (json.RawMessage, error) {
	body := map[string]any{
		"parameters": json.RawMessage(`{}`),
	}
	if len(r.Parameters) > 0 {
		body["parameters"] = r.Parameters
	}
	if r.TargetObjectID != nil {
		body["target_object_id"] = *r.TargetObjectID
	}
	if r.Justification != nil {
		body["justification"] = *r.Justification
	}
	return json.Marshal(body)
}

// ExtractActionRequest mirrors `extract_action_request`. Pure
// extraction of the ontology-action invocation from a trigger_payload
// JSON value. Honours root `action_id` first, then nested
// `ontology_action.action_id`.
func ExtractActionRequest(triggerPayload json.RawMessage, correlationID uuid.UUID) (*OntologyActionRequest, error) {
	var top map[string]json.RawMessage
	if len(triggerPayload) == 0 {
		top = map[string]json.RawMessage{}
	} else if err := json.Unmarshal(triggerPayload, &top); err != nil {
		// Non-object trigger_payload (e.g. raw scalar) → treat as missing fields.
		top = map[string]json.RawMessage{}
	}

	scope := top
	if nested, ok := top["ontology_action"]; ok {
		var nestedObj map[string]json.RawMessage
		if err := json.Unmarshal(nested, &nestedObj); err == nil {
			scope = nestedObj
		}
	}

	actionID, ok := readString(scope, "action_id")
	if !ok || strings.TrimSpace(actionID) == "" {
		return nil, &DispatchError{
			Kind:    KindInvalidPayload,
			Message: "trigger_payload.action_id is required to dispatch an ontology action",
		}
	}

	target, _ := readNonEmptyString(scope, "target_object_id")
	justification, _ := readNonEmptyString(scope, "justification")

	parameters := json.RawMessage(`{}`)
	if raw, ok := scope["parameters"]; ok {
		var holder any
		if err := json.Unmarshal(raw, &holder); err == nil {
			if _, isObject := holder.(map[string]any); isObject {
				parameters = raw
			}
		}
	}

	return &OntologyActionRequest{
		ActionID:           actionID,
		TargetObjectID:     target,
		Parameters:         parameters,
		Justification:      justification,
		AuditCorrelationID: correlationID,
	}, nil
}

func readString(obj map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := obj[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func readNonEmptyString(obj map[string]json.RawMessage, key string) (*string, bool) {
	s, ok := readString(obj, key)
	if !ok || s == "" {
		return nil, false
	}
	out := s
	return &out, true
}

// DispatchOutcome is the success-path payload returned to the
// consumer so it can stamp the `attempts` count on the outcome event.
type DispatchOutcome struct {
	Response json.RawMessage
	Attempts uint32
}

// EffectDispatcher is the HTTP effect dispatcher. Cheap to share —
// safe for concurrent use as long as the underlying http.Client is.
type EffectDispatcher struct {
	Client      *http.Client
	BaseURL     string
	BearerToken string
}

// New mirrors `EffectDispatcher::new`. Normalises the base URL
// (prepends `http://` when no scheme present) and the bearer token
// (prepends `Bearer ` when no auth scheme present).
func New(client *http.Client, baseURL, bearerToken string) *EffectDispatcher {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &EffectDispatcher{
		Client:      client,
		BaseURL:     normalizeBaseURL(baseURL),
		BearerToken: normalizeBearerToken(bearerToken),
	}
}

// DispatchOnce mirrors `EffectDispatcher::dispatch_once`.
func (d *EffectDispatcher) DispatchOnce(ctx context.Context, req *OntologyActionRequest) (json.RawMessage, error) {
	if strings.TrimSpace(d.BaseURL) == "" {
		return nil, &DispatchError{Kind: KindUnconfigured, Message: "OF_ONTOLOGY_ACTIONS_URL is empty"}
	}
	if strings.TrimSpace(d.BearerToken) == "" {
		return nil, &DispatchError{Kind: KindUnconfigured, Message: "OF_ONTOLOGY_ACTIONS_BEARER_TOKEN is empty"}
	}

	url := fmt.Sprintf("%s/api/v1/ontology/actions/%s/execute",
		strings.TrimRight(d.BaseURL, "/"), urlEncode(req.ActionID))

	body, err := req.Body()
	if err != nil {
		return nil, &DispatchError{Kind: KindInvalidPayload, Message: err.Error()}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, &DispatchError{Kind: KindRetryable, Message: fmt.Sprintf("transport error: %s", err)}
	}
	httpReq.Header.Set("Authorization", d.BearerToken)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(headerAuditCorrelation, req.AuditCorrelationID.String())

	resp, err := d.Client.Do(httpReq)
	if err != nil {
		return nil, &DispatchError{Kind: KindRetryable, Message: fmt.Sprintf("transport error: %s", err)}
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &DispatchError{Kind: KindRetryable, Message: fmt.Sprintf("failed to read response body: %s", err)}
	}
	payload := decodeJSON(bodyBytes)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return payload, nil
	}
	message := responseMessage(payload, bodyBytes)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
		return nil, &DispatchError{Kind: KindNonRetryable, Status: uint16(resp.StatusCode), Message: message}
	}
	return nil, &DispatchError{
		Kind:    KindRetryable,
		Message: fmt.Sprintf("ontology action returned %d: %s", resp.StatusCode, message),
	}
}

// DispatchWithRetries mirrors `EffectDispatcher::dispatch_with_retries`.
// Sleeps between attempts per the supplied RetryPolicy; surfaces
// non-retryable errors immediately and wraps the final retryable
// failure as KindExhausted.
func (d *EffectDispatcher) DispatchWithRetries(ctx context.Context, req *OntologyActionRequest, policy RetryPolicy) (DispatchOutcome, error) {
	var attempt uint32 = 0
	for {
		attempt++
		response, err := d.DispatchOnce(ctx, req)
		if err == nil {
			return DispatchOutcome{Response: response, Attempts: attempt}, nil
		}
		de := AsDispatchError(err)
		if de == nil {
			return DispatchOutcome{}, err
		}
		if de.IsTerminal() {
			return DispatchOutcome{}, de
		}
		if attempt >= policy.MaxAttempts {
			return DispatchOutcome{}, &DispatchError{
				Kind:     KindExhausted,
				Attempts: attempt,
				Message:  de.Error(),
			}
		}
		backoff := policy.NextBackoff(attempt + 1)
		select {
		case <-ctx.Done():
			return DispatchOutcome{}, ctx.Err()
		case <-time.After(backoff):
		}
	}
}

// normalizeBaseURL mirrors `normalize_base_url`.
func normalizeBaseURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.Contains(trimmed, "://") {
		return trimmed
	}
	return "http://" + trimmed
}

// normalizeBearerToken mirrors `normalize_bearer_token`.
func normalizeBearerToken(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return trimmed
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "bearer ") {
		return trimmed
	}
	return "Bearer " + trimmed
}

func decodeJSON(body []byte) json.RawMessage {
	if allWhitespace(body) {
		return json.RawMessage(`{}`)
	}
	var holder any
	if err := json.Unmarshal(body, &holder); err != nil {
		raw, _ := json.Marshal(map[string]string{"raw": string(body)})
		return raw
	}
	out, _ := json.Marshal(holder)
	return out
}

func responseMessage(payload json.RawMessage, body []byte) string {
	for _, key := range []string{"error", "message", "details"} {
		var holder map[string]json.RawMessage
		if err := json.Unmarshal(payload, &holder); err == nil {
			if raw, ok := holder[key]; ok {
				var s string
				if err := json.Unmarshal(raw, &s); err == nil && s != "" {
					return s
				}
				if !rawIsNull(raw) {
					return string(raw)
				}
			}
		}
	}
	if allWhitespace(body) {
		return "upstream error"
	}
	return string(body)
}

func allWhitespace(body []byte) bool {
	for _, b := range body {
		switch b {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			continue
		default:
			return false
		}
	}
	return true
}

func rawIsNull(raw json.RawMessage) bool {
	return string(raw) == "null"
}

// urlEncode mirrors the Rust inner urlencoding::encode — only RFC 3986
// unreserved characters pass through; everything else is %HH-escaped.
func urlEncode(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for i := 0; i < len(value); i++ {
		c := value[i]
		safe := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_' || c == '~'
		if safe {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
