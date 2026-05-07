// Package transformclient is the HTTP client media-sets-service uses
// to invoke media-transform-runtime-service.
//
// Wire format mirrors the worker's TransformInput / TransformOutput
// 1:1 — see services/media-transform-runtime-service/internal/runtime
// for the canonical Go struct definitions on the worker side. We
// re-declare them here (rather than importing) to keep the module
// dependency graph one-way: the caller never depends on the worker's
// internal packages.
package transformclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Status mirrors the worker's TransformStatus. SCREAMING_SNAKE_CASE
// values match the wire format ("OK" / "NOT_IMPLEMENTED").
type Status string

const (
	StatusOK             Status = "OK"
	StatusNotImplemented Status = "NOT_IMPLEMENTED"
)

// TransformRequest is the POST /transform body. Bytes flow base64-
// encoded so a single JSON envelope works for image, audio and
// document inputs without a multipart parser.
type TransformRequest struct {
	Kind        string          `json:"kind"`
	MimeType    string          `json:"mime_type"`
	Schema      string          `json:"schema"`
	Params      json.RawMessage `json:"params,omitempty"`
	BytesBase64 string          `json:"bytes_base64"`
}

// TransformResponse is the worker's response body.
type TransformResponse struct {
	Status            Status  `json:"status"`
	Kind              string  `json:"kind"`
	OutputMimeType    string  `json:"output_mime_type"`
	ComputeSeconds    uint64  `json:"compute_seconds"`
	OutputBytesBase64 *string `json:"output_bytes_base64,omitempty"`
	OutputJSON        any     `json:"output_json,omitempty"`
	Reason            *string `json:"reason,omitempty"`
}

// Client is the typed HTTP client. Construct with New.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Option mutates a Client at construction time.
type Option func(*Client)

// WithHTTPClient overrides the default http.Client (timeout = 30s).
// Use this in tests to wire an httptest.Server.
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) { cl.httpClient = c }
}

// New builds a Client. baseURL is the worker base (e.g.
// "http://media-transform-runtime-service:50173") with no trailing
// slash; the helper appends "/transform" to it.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ErrorEnvelope is the worker's {error, code} body for 4xx/5xx.
type ErrorEnvelope struct {
	StatusCode int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"error"`
}

func (e *ErrorEnvelope) Error() string {
	return fmt.Sprintf("media-transform-runtime: %s (HTTP %d, code=%s)", e.Message, e.StatusCode, e.Code)
}

// AsErrorEnvelope returns the wrapped envelope when the error came
// from a 4xx/5xx worker response. Allows callers to switch on the
// canonical worker error codes.
func AsErrorEnvelope(err error) (*ErrorEnvelope, bool) {
	var env *ErrorEnvelope
	if errors.As(err, &env) {
		return env, true
	}
	return nil, false
}

// Transform issues POST /transform and decodes the response. A 200
// with status="NOT_IMPLEMENTED" is NOT an error — the caller checks
// the response status to know whether to surface the reason verbatim
// (Foundry's "graceful degrade" path).
func (c *Client) Transform(ctx context.Context, req TransformRequest) (*TransformResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode transform request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/transform", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build transform request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send transform request: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read transform response: %w", err)
	}
	if resp.StatusCode >= 400 {
		var env ErrorEnvelope
		_ = json.Unmarshal(rawBody, &env)
		env.StatusCode = resp.StatusCode
		return nil, &env
	}
	var out TransformResponse
	if err := json.Unmarshal(rawBody, &out); err != nil {
		return nil, fmt.Errorf("decode transform response: %w", err)
	}
	return &out, nil
}
