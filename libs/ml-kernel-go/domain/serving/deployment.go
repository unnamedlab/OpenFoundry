package serving

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

var ErrRuntimeUnavailable = errors.New("deployment runtime unavailable")

type DeploymentRuntime interface {
	Deploy(ctx context.Context, deployment models.ModelDeployment) error
	Transition(ctx context.Context, deployment models.ModelDeployment, status string) error
}

type UnavailableDeploymentRuntime struct {
	Reason string
}

func (r UnavailableDeploymentRuntime) Deploy(context.Context, models.ModelDeployment) error {
	return r.err()
}

func (r UnavailableDeploymentRuntime) Transition(context.Context, models.ModelDeployment, string) error {
	return r.err()
}

func (r UnavailableDeploymentRuntime) err() error {
	if strings.TrimSpace(r.Reason) == "" {
		return ErrRuntimeUnavailable
	}
	return fmt.Errorf("%w: %s", ErrRuntimeUnavailable, r.Reason)
}

// NoopDeploymentRuntime is retained for explicitly injected tests that do not
// want side effects. Service wiring should prefer UnavailableDeploymentRuntime
// when no real backend is configured, so production cannot silently no-op.
type NoopDeploymentRuntime struct{}

func (NoopDeploymentRuntime) Deploy(context.Context, models.ModelDeployment) error { return nil }
func (NoopDeploymentRuntime) Transition(context.Context, models.ModelDeployment, string) error {
	return nil
}

// HTTPDeploymentRuntime is the production adapter for an external model serving
// control plane. It posts deployment lifecycle events to a configured backend;
// non-2xx responses keep the deployment transaction from being persisted.
type HTTPDeploymentRuntime struct {
	BaseURL string
	Client  *http.Client
}

func NewHTTPDeploymentRuntime(baseURL string, client *http.Client) *HTTPDeploymentRuntime {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &HTTPDeploymentRuntime{BaseURL: strings.TrimRight(baseURL, "/"), Client: client}
}

func (r *HTTPDeploymentRuntime) Deploy(ctx context.Context, deployment models.ModelDeployment) error {
	return r.post(ctx, "/deployments", deploymentRuntimeRequest{Deployment: deployment})
}

func (r *HTTPDeploymentRuntime) Transition(ctx context.Context, deployment models.ModelDeployment, status string) error {
	return r.post(ctx, fmt.Sprintf("/deployments/%s/transition", deployment.ID), deploymentRuntimeRequest{Deployment: deployment, Status: status})
}

func (r *HTTPDeploymentRuntime) post(ctx context.Context, path string, payload deploymentRuntimeRequest) error {
	if r == nil || strings.TrimSpace(r.BaseURL) == "" {
		return ErrRuntimeUnavailable
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRuntimeUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("serving backend returned %d: %s", resp.StatusCode, msg)
	}
	return nil
}

type deploymentRuntimeRequest struct {
	Deployment models.ModelDeployment `json:"deployment"`
	Status     string                 `json:"status,omitempty"`
}

type FakeDeploymentRuntime struct {
	Available     bool
	DeployErr     error
	TransitionErr error
	mu            sync.Mutex
	Deployments   []models.ModelDeployment
	Transitions   []DeploymentTransition
}

type DeploymentTransition struct {
	DeploymentID uuid.UUID
	Status       string
}

func NewFakeDeploymentRuntime() *FakeDeploymentRuntime {
	return &FakeDeploymentRuntime{Available: true}
}

func (r *FakeDeploymentRuntime) Deploy(_ context.Context, deployment models.ModelDeployment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.Available {
		return ErrRuntimeUnavailable
	}
	if r.DeployErr != nil {
		return r.DeployErr
	}
	r.Deployments = append(r.Deployments, deployment)
	return nil
}

func (r *FakeDeploymentRuntime) Transition(_ context.Context, deployment models.ModelDeployment, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.Available {
		return ErrRuntimeUnavailable
	}
	if r.TransitionErr != nil {
		return r.TransitionErr
	}
	r.Transitions = append(r.Transitions, DeploymentTransition{DeploymentID: deployment.ID, Status: status})
	return nil
}
