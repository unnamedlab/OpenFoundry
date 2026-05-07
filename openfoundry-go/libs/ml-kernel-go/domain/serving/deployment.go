package serving

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

var ErrRuntimeUnavailable = errors.New("deployment runtime unavailable")

type DeploymentRuntime interface {
	Deploy(ctx context.Context, deployment models.ModelDeployment) error
	Transition(ctx context.Context, deployment models.ModelDeployment, status string) error
}

type NoopDeploymentRuntime struct{}

func (NoopDeploymentRuntime) Deploy(context.Context, models.ModelDeployment) error { return nil }
func (NoopDeploymentRuntime) Transition(context.Context, models.ModelDeployment, string) error {
	return nil
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
