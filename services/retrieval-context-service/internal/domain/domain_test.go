package domain_test

import (
	"errors"
	"testing"

	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/models"
)

func TestValidateCreateJob(t *testing.T) {
	t.Parallel()
	bad := models.JobStatus("weird")
	tests := []struct {
		name    string
		req     models.CreateJobRequest
		wantErr error
	}{
		{"ok", models.CreateJobRequest{SourceURI: "s3://x", Pipeline: "p"}, nil},
		{"missing source", models.CreateJobRequest{Pipeline: "p"}, domain.ErrInvalidInput},
		{"missing pipeline", models.CreateJobRequest{SourceURI: "s3://x"}, domain.ErrInvalidInput},
		{"bad status", models.CreateJobRequest{SourceURI: "s", Pipeline: "p", Status: &bad}, domain.ErrUnknownStatus},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := domain.ValidateCreateJob(tc.req)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("expected nil, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestAllowedTransition(t *testing.T) {
	t.Parallel()
	tests := []struct {
		from, to models.JobStatus
		want     bool
	}{
		{models.JobStatusQueued, models.JobStatusRunning, true},
		{models.JobStatusQueued, models.JobStatusCancelled, true},
		{models.JobStatusQueued, models.JobStatusSucceeded, false},
		{models.JobStatusRunning, models.JobStatusSucceeded, true},
		{models.JobStatusRunning, models.JobStatusFailed, true},
		{models.JobStatusRunning, models.JobStatusQueued, false},
		{models.JobStatusSucceeded, models.JobStatusRunning, false},
		{models.JobStatusFailed, models.JobStatusRunning, false},
		{models.JobStatusCancelled, models.JobStatusRunning, false},
		// Self-transition allowed for idempotent replay.
		{models.JobStatusSucceeded, models.JobStatusSucceeded, true},
	}
	for _, tc := range tests {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			t.Parallel()
			if got := domain.AllowedTransition(tc.from, tc.to); got != tc.want {
				t.Fatalf("AllowedTransition(%s,%s)=%v want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func TestValidateUpdateJob(t *testing.T) {
	t.Parallel()
	running := models.JobStatusRunning
	queued := models.JobStatusQueued
	bad := models.JobStatus("weird")
	current := models.Job{Status: models.JobStatusQueued}
	terminal := models.Job{Status: models.JobStatusSucceeded}

	tests := []struct {
		name    string
		cur     models.Job
		req     models.UpdateJobRequest
		wantErr error
	}{
		{"empty patch", current, models.UpdateJobRequest{}, domain.ErrInvalidInput},
		{"legal transition", current, models.UpdateJobRequest{Status: &running}, nil},
		{"illegal transition", terminal, models.UpdateJobRequest{Status: &queued}, domain.ErrIllegalStateTransition},
		{"unknown status", current, models.UpdateJobRequest{Status: &bad}, domain.ErrUnknownStatus},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := domain.ValidateUpdateJob(tc.cur, tc.req)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("expected nil, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateRecordExtraction_ConfidenceRange(t *testing.T) {
	t.Parallel()
	low := float32(-0.1)
	high := float32(1.1)
	ok := float32(0.7)
	if err := domain.ValidateRecordExtraction(models.RecordExtractionRequest{ExtractionKind: "text", Confidence: &low}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("low conf: %v", err)
	}
	if err := domain.ValidateRecordExtraction(models.RecordExtractionRequest{ExtractionKind: "text", Confidence: &high}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("high conf: %v", err)
	}
	if err := domain.ValidateRecordExtraction(models.RecordExtractionRequest{ExtractionKind: "text", Confidence: &ok}); err != nil {
		t.Fatalf("ok: %v", err)
	}
	if err := domain.ValidateRecordExtraction(models.RecordExtractionRequest{ExtractionKind: ""}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("empty kind: %v", err)
	}
}
