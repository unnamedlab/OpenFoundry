package authmw

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPurposeCheckpointClientAllow(t *testing.T) {
	actorID := uuid.New()
	var got PurposeCheckpointRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, PurposeCheckpointEnforcePath, r.URL.Path)
		require.Equal(t, "Bearer service-token", r.Header.Get("Authorization"))
		require.Equal(t, "trace-123", r.Header.Get("X-Trace-ID"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_ = json.NewEncoder(w).Encode(PurposeCheckpointEvaluation{
			RecordID:        uuid.New(),
			Approved:        true,
			Status:          "approved",
			RequiredPrompts: []string{},
		})
	}))
	defer srv.Close()

	client := NewPurposeCheckpointClient(srv.URL,
		WithPurposeCheckpointBearerToken("service-token"),
		WithPurposeCheckpointHeader("X-Trace-ID", "trace-123"),
	)
	err := client.Enforce(context.Background(), PurposeCheckpointRequest{
		InteractionType:         "ai_chat_completion",
		ActorID:                 &actorID,
		PurposeJustification:    strPtr("incident response on customer outage"),
		RequestedPrivateNetwork: true,
		Tags:                    []string{"ai", "chat", "private-network"},
		Evidence:                json.RawMessage(`{"flag_count":1}`),
	})
	require.NoError(t, err)
	require.Equal(t, "ai_chat_completion", got.InteractionType)
	require.Equal(t, actorID, *got.ActorID)
	require.True(t, got.RequestedPrivateNetwork)
	require.JSONEq(t, `{"flag_count":1}`, string(got.Evidence))
}

func TestPurposeCheckpointClientDeny(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(PurposeCheckpointEvaluation{
			RecordID: uuid.New(),
			Approved: false,
			Status:   "pending_justification",
			Reason:   strPtr("purpose justification is required for this sensitive interaction"),
		})
	}))
	defer srv.Close()

	err := NewPurposeCheckpointClient(srv.URL).Enforce(context.Background(), PurposeCheckpointRequest{
		InteractionType:  "ai_agent_execution",
		RequiresApproval: true,
	})
	require.Error(t, err)
	var denied *PurposeCheckpointDeniedError
	require.True(t, errors.As(err, &denied))
	require.Equal(t, "pending_justification", denied.Evaluation.Status)
	require.EqualError(t, err, "purpose justification is required for this sensitive interaction")
}

func TestPurposeCheckpointClientServiceAndInvalidResponseErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer srv.Close()

	err := NewPurposeCheckpointClient(srv.URL).Enforce(context.Background(), PurposeCheckpointRequest{InteractionType: "ai_chat_completion"})
	require.Error(t, err)
	var serviceErr *PurposeCheckpointServiceError
	require.True(t, errors.As(err, &serviceErr))
	require.Equal(t, http.StatusBadGateway, serviceErr.StatusCode)
	require.Contains(t, serviceErr.Body, "unavailable")
	require.Contains(t, serviceErr.Error(), "unavailable")

	invalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer invalid.Close()

	err = NewPurposeCheckpointClient(invalid.URL).Enforce(context.Background(), PurposeCheckpointRequest{InteractionType: "ai_chat_completion"})
	require.Error(t, err)
	var invalidErr *PurposeCheckpointInvalidResponseError
	require.True(t, errors.As(err, &invalidErr))

	timeoutSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
	}))
	defer timeoutSrv.Close()

	err = NewPurposeCheckpointClient(timeoutSrv.URL, WithPurposeCheckpointTimeout(time.Nanosecond)).Enforce(context.Background(), PurposeCheckpointRequest{InteractionType: "ai_chat_completion"})
	require.Error(t, err)
	require.True(t, errors.As(err, &serviceErr))
}

func strPtr(value string) *string { return &value }
