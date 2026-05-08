package handler

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	livellogs "github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/logs"
)

func TestStreamJobLogsInitialHistory(t *testing.T) {
	mem := livellogs.NewMemoryService()
	mem.Emit("job-1", livellogs.LogInfo, "hello", nil)
	restoreSvc := SetJobLogService(&livellogs.Service{Store: mem, Subscriber: mem})
	defer restoreSvc()
	restoreCfg := SetJobLogStreamConfig(0, time.Millisecond)
	defer restoreCfg()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/job-1/logs/stream?follow=false", nil)
	rr := httptest.NewRecorder()
	StreamJobLogs(rr, req)

	res := rr.Result()
	defer res.Body.Close()
	body := rr.Body.String()
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Contains(t, res.Header.Get("Content-Type"), "text/event-stream")
	require.Contains(t, body, "event: heartbeat")
	require.Contains(t, body, "event: log")
	require.Contains(t, body, `"sequence":1`)
	require.Contains(t, body, `"level":"INFO"`)
	require.Contains(t, body, `"message":"hello"`)
	require.NotContains(t, body, "unimplemented")
}

func TestStreamJobLogsMultipleLiveEvents(t *testing.T) {
	mem := livellogs.NewMemoryService()
	restoreSvc := SetJobLogService(&livellogs.Service{Store: mem, Subscriber: mem})
	defer restoreSvc()
	restoreCfg := SetJobLogStreamConfig(0, time.Millisecond)
	defer restoreCfg()

	server := httptest.NewServer(http.HandlerFunc(StreamJobLogs))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/jobs/job-2/logs/stream")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	reader := bufio.NewReader(resp.Body)
	heartbeat := readSSEEvent(t, reader)
	require.Contains(t, heartbeat, "event: heartbeat")

	mem.Emit("job-2", livellogs.LogWarn, "first", nil)
	mem.Emit("job-2", livellogs.LogError, "second", nil)

	first := readSSEEvent(t, reader)
	second := readSSEEvent(t, reader)
	require.Contains(t, first, "event: log")
	require.Contains(t, first, `"message":"first"`)
	require.Contains(t, second, "event: log")
	require.Contains(t, second, `"message":"second"`)
}

func TestStreamJobLogsClientCancellationUnsubscribes(t *testing.T) {
	store := livellogs.NewMemoryService()
	sub := &trackingSubscriber{ch: make(chan livellogs.LogEntry)}
	restoreSvc := SetJobLogService(&livellogs.Service{Store: store, Subscriber: sub})
	defer restoreSvc()
	restoreCfg := SetJobLogStreamConfig(0, time.Millisecond)
	defer restoreCfg()

	server := httptest.NewServer(http.HandlerFunc(StreamJobLogs))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/jobs/job-3/logs/stream")
	require.NoError(t, err)
	reader := bufio.NewReader(resp.Body)
	require.Contains(t, readSSEEvent(t, reader), "event: heartbeat")
	require.NoError(t, resp.Body.Close())

	select {
	case <-sub.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("expected StreamJobLogs to unsubscribe after client cancellation")
	}
}

func TestStreamJobLogsStoreError(t *testing.T) {
	restoreSvc := SetJobLogService(&livellogs.Service{Store: failingStore{err: errors.New("db down")}, Subscriber: nil})
	defer restoreSvc()
	restoreCfg := SetJobLogStreamConfig(0, time.Millisecond)
	defer restoreCfg()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/job-4/logs/stream?follow=false", nil)
	rr := httptest.NewRecorder()
	StreamJobLogs(rr, req)

	res := rr.Result()
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	require.Contains(t, res.Header.Get("Content-Type"), "application/json")
	require.Contains(t, string(body), "log_store_unavailable")
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var b strings.Builder
	for {
		line, err := reader.ReadString('\n')
		require.NoError(t, err)
		if line == "\n" || line == "\r\n" {
			return b.String()
		}
		b.WriteString(line)
	}
}

type failingStore struct{ err error }

func (f failingStore) History(context.Context, string, livellogs.Query) ([]livellogs.LogEntry, error) {
	return nil, f.err
}

type trackingSubscriber struct {
	ch        chan livellogs.LogEntry
	once      sync.Once
	cancelled chan struct{}
}

func (t *trackingSubscriber) Subscribe(ctx context.Context, _ string) (<-chan livellogs.LogEntry, func(), error) {
	if t.cancelled == nil {
		t.cancelled = make(chan struct{})
	}
	cancel := func() {
		t.once.Do(func() { close(t.cancelled) })
	}
	go func() {
		<-ctx.Done()
		cancel()
	}()
	return t.ch, cancel, nil
}
