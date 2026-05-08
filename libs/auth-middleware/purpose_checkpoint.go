package authmw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	defaultPurposeCheckpointTimeout = 2 * time.Second
	// PurposeCheckpointEnforcePath is the consolidated Go endpoint for
	// purpose-of-use enforcement. Keep this exported so service wiring and
	// edge routing tests can depend on the same path as the client.
	PurposeCheckpointEnforcePath = "/api/v1/checkpoints/purpose/enforce"
)

// PurposeCheckpointClient calls the authorization-policy purpose gate used by
// sensitive AI agent/chat routes. Its wire contract mirrors Rust
// enforce_purpose_checkpoint but targets the public Go consolidation endpoint:
// POST /api/v1/checkpoints/purpose/enforce.
type PurposeCheckpointClient struct {
	baseURL       string
	httpClient    *http.Client
	timeout       time.Duration
	staticHeaders http.Header
}

// PurposeCheckpointOption customizes NewPurposeCheckpointClient.
type PurposeCheckpointOption func(*PurposeCheckpointClient)

// WithPurposeCheckpointHeader adds a static header to every enforcement
// request. This is intentionally generic so deployments can pass a service
// bearer token or mTLS-routing header without coupling auth-middleware to a
// particular secret source. Empty names or values are ignored.
func WithPurposeCheckpointHeader(name, value string) PurposeCheckpointOption {
	return func(c *PurposeCheckpointClient) {
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" {
			return
		}
		if c.staticHeaders == nil {
			c.staticHeaders = make(http.Header)
		}
		c.staticHeaders.Set(name, value)
	}
}

// WithPurposeCheckpointBearerToken sets the Authorization header used for
// service-to-service calls to the protected /api/v1 enforcement endpoint.
// Pass the raw token; the Bearer prefix is added when absent.
func WithPurposeCheckpointBearerToken(token string) PurposeCheckpointOption {
	token = strings.TrimSpace(token)
	if token != "" && !strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = "Bearer " + token
	}
	return WithPurposeCheckpointHeader("Authorization", token)
}

// WithPurposeCheckpointHTTPClient injects the HTTP client used for requests.
func WithPurposeCheckpointHTTPClient(client *http.Client) PurposeCheckpointOption {
	return func(c *PurposeCheckpointClient) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithPurposeCheckpointTimeout sets a per-call timeout. Use a non-positive
// duration to rely solely on the caller's context and the injected HTTP client.
func WithPurposeCheckpointTimeout(timeout time.Duration) PurposeCheckpointOption {
	return func(c *PurposeCheckpointClient) { c.timeout = timeout }
}

// NewPurposeCheckpointClient returns a purpose-checkpoint enforcement client.
func NewPurposeCheckpointClient(baseURL string, opts ...PurposeCheckpointOption) *PurposeCheckpointClient {
	c := &PurposeCheckpointClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{Timeout: defaultPurposeCheckpointTimeout},
		timeout:    defaultPurposeCheckpointTimeout,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// PurposeCheckpointRequest is the Rust EvaluateCheckpointRequest wire shape.
type PurposeCheckpointRequest struct {
	InteractionType         string          `json:"interaction_type"`
	ActorID                 *uuid.UUID      `json:"actor_id,omitempty"`
	PurposeJustification    *string         `json:"purpose_justification,omitempty"`
	RequestedPrivateNetwork bool            `json:"requested_private_network"`
	RequiresApproval        bool            `json:"requires_approval"`
	Tags                    []string        `json:"tags"`
	Evidence                json.RawMessage `json:"evidence"`
}

// PurposeCheckpointEvaluation is the Rust CheckpointEvaluation wire shape.
type PurposeCheckpointEvaluation struct {
	RecordID        uuid.UUID `json:"record_id"`
	Approved        bool      `json:"approved"`
	Status          string    `json:"status"`
	RequiredPrompts []string  `json:"required_prompts"`
	PolicySlug      *string   `json:"policy_slug"`
	Reason          *string   `json:"reason"`
}

// PurposeCheckpointDeniedError is returned when the checkpoint evaluates but
// does not approve the interaction.
type PurposeCheckpointDeniedError struct {
	Evaluation PurposeCheckpointEvaluation
}

func (e *PurposeCheckpointDeniedError) Error() string {
	if e == nil {
		return "purpose checkpoint denied"
	}
	if e.Evaluation.Reason != nil && strings.TrimSpace(*e.Evaluation.Reason) != "" {
		return *e.Evaluation.Reason
	}
	return fmt.Sprintf("purpose checkpoint blocked with status %s", e.Evaluation.Status)
}

// PurposeCheckpointServiceError is returned for transport errors or non-2xx
// responses from the checkpoint service.
type PurposeCheckpointServiceError struct {
	StatusCode int
	Message    string
	Body       string
	Cause      error
}

func (e *PurposeCheckpointServiceError) Error() string {
	if e == nil {
		return "purpose checkpoint service error"
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	if strings.TrimSpace(e.Body) != "" {
		return fmt.Sprintf("%s: %s", e.Message, strings.TrimSpace(e.Body))
	}
	return e.Message
}

func (e *PurposeCheckpointServiceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// PurposeCheckpointInvalidResponseError is returned when a successful service
// response cannot be decoded as a checkpoint evaluation.
type PurposeCheckpointInvalidResponseError struct {
	Body  string
	Cause error
}

func (e *PurposeCheckpointInvalidResponseError) Error() string {
	if e == nil {
		return "invalid purpose checkpoint response"
	}
	return fmt.Sprintf("invalid purpose checkpoint response: %v", e.Cause)
}

func (e *PurposeCheckpointInvalidResponseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Enforce submits a purpose checkpoint and returns nil only when approved.
func (c *PurposeCheckpointClient) Enforce(ctx context.Context, req PurposeCheckpointRequest) error {
	if c == nil {
		return &PurposeCheckpointServiceError{Message: "purpose checkpoint client is nil"}
	}
	if c.baseURL == "" {
		return &PurposeCheckpointServiceError{Message: "purpose checkpoint service URL is empty"}
	}
	if len(req.Evidence) == 0 {
		req.Evidence = json.RawMessage(`{}`)
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return &PurposeCheckpointServiceError{Message: "purpose checkpoint request encode failed", Cause: err}
	}

	callCtx := ctx
	cancel := func() {}
	if c.timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, c.timeout)
	}
	defer cancel()

	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.baseURL+PurposeCheckpointEnforcePath, bytes.NewReader(body))
	if err != nil {
		return &PurposeCheckpointServiceError{Message: "purpose checkpoint request build failed", Cause: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	for name, values := range c.staticHeaders {
		for _, value := range values {
			httpReq.Header.Add(name, value)
		}
	}

	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return &PurposeCheckpointServiceError{Message: "purpose checkpoint request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &PurposeCheckpointServiceError{StatusCode: resp.StatusCode, Message: "purpose checkpoint body failed", Cause: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &PurposeCheckpointServiceError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("purpose checkpoint service returned %d", resp.StatusCode), Body: string(respBody)}
	}

	var evaluation PurposeCheckpointEvaluation
	if err := json.Unmarshal(respBody, &evaluation); err != nil {
		return &PurposeCheckpointInvalidResponseError{Body: string(respBody), Cause: err}
	}
	if !evaluation.Approved {
		return &PurposeCheckpointDeniedError{Evaluation: evaluation}
	}
	return nil
}
