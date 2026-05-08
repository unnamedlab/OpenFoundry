package agents

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

func tool(name, mode string, execConfig map[string]any) models.ToolDefinition {
	cfgJSON, _ := json.Marshal(execConfig)
	return models.ToolDefinition{
		ID:              uuid.New(),
		Name:            name,
		Description:     name + " description",
		Category:        "analysis",
		ExecutionMode:   mode,
		ExecutionConfig: cfgJSON,
		Status:          "active",
		InputSchema:     json.RawMessage("{}"),
		OutputSchema:    json.RawMessage("{}"),
	}
}

func toolNamePtr(s string) *string { return &s }

func TestExecutePlanEmptyPlanReturnsEmpty(t *testing.T) {
	t.Parallel()
	traces := ExecutePlan(context.Background(), nil, nil, nil, "msg", "obj", nil, nil, nil)
	assert.Empty(t, traces)
}

func TestExecutePlanRetrieveContextEmitsCitations(t *testing.T) {
	t.Parallel()
	plan := []models.AgentPlanStep{{ID: "retrieve-context", Title: "Retrieve"}}
	hits := []models.KnowledgeSearchResult{{
		DocumentTitle: "doc-1",
		Score:         0.91,
		Excerpt:       "key insight",
	}}
	traces := ExecutePlan(context.Background(), nil, plan, nil, "", "", nil, nil, hits)
	require.Len(t, traces, 1)
	assert.Equal(t, "Retrieved 1 knowledge hit(s).", traces[0].Observation)
	var out map[string]any
	require.NoError(t, json.Unmarshal(traces[0].Output, &out))
	assert.Contains(t, out, "citations")
}

func TestExecutePlanSynthesizeAnswerCountsSuccessfulInvocations(t *testing.T) {
	t.Parallel()
	mockedTool := tool("hello", "simulated", map[string]any{
		"mock_response": map[string]string{"reply": "hi"},
	})
	plan := []models.AgentPlanStep{
		{ID: "step-1", Title: "Call hello", ToolName: toolNamePtr("hello")},
		{ID: "synthesize-answer", Title: "Synthesize"},
	}
	tools := []models.ToolDefinition{mockedTool}
	traces := ExecutePlan(context.Background(), nil, plan, tools, "msg", "obj", nil, nil, nil)
	require.Len(t, traces, 2)
	assert.Equal(t, "Executed tool 'hello'.", traces[0].Observation)
	assert.Contains(t, traces[1].Observation, "1 successful tool invocation(s)")
}

func TestExecutePlanFinalGenericStep(t *testing.T) {
	t.Parallel()
	plan := []models.AgentPlanStep{{ID: "analyze", Title: "Analyze the request"}}
	traces := ExecutePlan(context.Background(), nil, plan, nil, "msg", "obj", nil, nil, nil)
	require.Len(t, traces, 1)
	assert.Equal(t, "Completed plan step 'Analyze the request'.", traces[0].Observation)
}

func TestExecuteToolMissingToolFails(t *testing.T) {
	t.Parallel()
	out := ExecuteTool(context.Background(), nil, nil, "ghost", "", "", nil, nil, nil)
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "failed", got["status"])
	assert.Equal(t, "tool definition not found", got["error"])
}

func TestExecuteSimulatedToolWithMock(t *testing.T) {
	t.Parallel()
	mockedTool := tool("hello", "simulated", map[string]any{"mock_response": map[string]string{"reply": "hi"}})
	out := ExecuteTool(context.Background(), nil, &mockedTool, "hello", "u", "o", nil, nil, nil)
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "completed", got["status"])
	assert.True(t, got["simulated"].(bool))
	assert.NotNil(t, got["request"])
	assert.NotNil(t, got["response"])
}

func TestExecuteSimulatedToolWithoutMockSkips(t *testing.T) {
	t.Parallel()
	mockedTool := tool("hello", "simulated", nil)
	out := ExecuteTool(context.Background(), nil, &mockedTool, "hello", "u", "o", nil, nil, nil)
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "skipped", got["status"])
	assert.Contains(t, got["reason"], "no mock_response")
}

func TestEnforceToolPolicyBlocksWhenSensitiveAndUnapproved(t *testing.T) {
	t.Parallel()
	t1 := tool("danger", "simulated", map[string]any{"sensitivity": "high"})
	out := ExecuteTool(context.Background(), nil, &t1, "danger", "msg", "obj", nil, nil, nil)
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "blocked", got["status"])
	assert.True(t, got["approval_required"].(bool))
	assert.Equal(t, "high", got["sensitivity"])
}

func TestEnforceToolPolicyAllowsWhenContextApproves(t *testing.T) {
	t.Parallel()
	t1 := tool("danger", "simulated", map[string]any{
		"sensitivity":    "admin",
		"mock_response":  map[string]string{"ok": "yes"},
	})
	ctxValue := json.RawMessage(`{"tool_policy":{"allow_sensitive_tools":true}}`)
	out := ExecuteTool(context.Background(), nil, &t1, "danger", "msg", "obj", ctxValue, nil, nil)
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "completed", got["status"])
}

func TestExecuteKnowledgeSearchToolRanksAndTruncates(t *testing.T) {
	t.Parallel()
	t1 := tool("kb", "knowledge_search", map[string]any{"top_k": 1, "min_score": 0.0})
	hits := []models.KnowledgeSearchResult{
		{DocumentTitle: "alpha doc", Score: 0.9, Excerpt: "latency tuning"},
		{DocumentTitle: "beta doc", Score: 0.5, Excerpt: "unrelated"},
	}
	inputs := json.RawMessage(`{"query":"latency"}`)
	out := executeKnowledgeSearchTool(&t1, inputs, hits)
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "completed", got["status"])
	results, _ := got["results"].([]any)
	require.Len(t, results, 1, "top_k=1 truncates")
	first := results[0].(map[string]any)
	assert.Equal(t, "alpha doc", first["document_title"])
}

func TestExecuteKnowledgeSearchToolEmptyQueryUsesRetrievalScore(t *testing.T) {
	t.Parallel()
	t1 := tool("kb", "knowledge_search", map[string]any{"top_k": 5, "min_score": 0.0})
	hits := []models.KnowledgeSearchResult{
		{DocumentTitle: "x", Score: 0.7, Excerpt: ""},
	}
	out := executeKnowledgeSearchTool(&t1, json.RawMessage(`{}`), hits)
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	results, _ := got["results"].([]any)
	require.Len(t, results, 1)
	assert.Equal(t, 0.7, results[0].(map[string]any)["score"])
}

func TestExecuteHTTPToolHappyPath(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "user_message")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	t1 := tool("call_api", "http_json", map[string]any{
		"url":    server.URL,
		"method": "POST",
	})
	out := ExecuteTool(context.Background(), server.Client(), &t1, "call_api", "msg", "obj", nil, http.Header{}, nil)
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "completed", got["status"])
	assert.Equal(t, float64(http.StatusOK), got["http_status"])
}

func TestExecuteHTTPToolMissingURLFailsHTTPJson(t *testing.T) {
	t.Parallel()
	t1 := tool("noop", "http_json", map[string]any{})
	out := ExecuteTool(context.Background(), nil, &t1, "noop", "", "", nil, http.Header{}, nil)
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "failed", got["status"])
	assert.Equal(t, "missing execution_config.url", got["error"])
}

func TestExecuteHTTPToolMissingURLFailsOpenfoundryAPI(t *testing.T) {
	t.Parallel()
	t1 := tool("noop", "openfoundry_api", map[string]any{})
	out := ExecuteTool(context.Background(), nil, &t1, "noop", "", "", nil, http.Header{}, nil)
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "failed", got["status"])
	assert.Contains(t, got["error"], "openfoundry_api")
}

func TestExecuteUnsupportedModeSkips(t *testing.T) {
	t.Parallel()
	t1 := tool("ghost", "made_up", map[string]any{})
	out := ExecuteTool(context.Background(), nil, &t1, "ghost", "", "", nil, nil, nil)
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "skipped", got["status"])
	assert.Contains(t, got["reason"], "made_up")
}

func TestExecuteNativeSQLToolGeneratesQuery(t *testing.T) {
	t.Parallel()
	t1 := tool("sql", "native_sql", map[string]any{
		"default_dataset_name": "ml_runs",
		"metric_hints":         []string{"latency_ms"},
	})
	out := ExecuteTool(context.Background(), nil, &t1, "sql",
		"how is latency over the last 30 days", "improve latency",
		json.RawMessage(`{"tool_inputs":{"sql":{}}}`), nil, nil)
	var got map[string]any
	require.NoError(t, json.Unmarshal(out, &got))
	assert.Equal(t, "completed", got["status"])
	assert.Equal(t, "ml_runs", got["dataset_name"])
	assert.Equal(t, float64(30), got["lookback_days"])
}

func TestInferTimeWindowDays(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 90, inferTimeWindowDays("90 day rolling window"))
	assert.Equal(t, 30, inferTimeWindowDays("compare last 30-day"))
	assert.Equal(t, 30, inferTimeWindowDays("over the last month"))
	assert.Equal(t, 1, inferTimeWindowDays("today's incidents"))
	assert.Equal(t, 7, inferTimeWindowDays("recent activity"))
}

func TestInferObjectTypesFallbackAndMatches(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"OperationalObject"}, inferObjectTypes(""))
	got := inferObjectTypes("incident on dataset and pipeline")
	assert.Contains(t, got, "Incident")
	assert.Contains(t, got, "Dataset")
	assert.Contains(t, got, "Pipeline")
}

func TestInferRepoBranchSlugTrimsAndDedupes(t *testing.T) {
	t.Parallel()
	slug := inferRepoBranchSlug("Hi--there!", "objective with spaces")
	assert.NotContains(t, slug, "--")
	assert.False(t, strings.HasPrefix(slug, "-"))
	assert.False(t, strings.HasSuffix(slug, "-"))
	assert.LessOrEqual(t, len(slug), 48)
	assert.Equal(t, "agent-change", inferRepoBranchSlug("", ""))
}

func TestInferReportChannelsDedupesAndSorts(t *testing.T) {
	t.Parallel()
	channels := inferReportChannels("send to slack and webhook", []string{"email"})
	assert.Equal(t, []string{"email", "slack", "webhook"}, channels)
}

func TestLexicalScoreFiltersShortTerms(t *testing.T) {
	t.Parallel()
	score := lexicalScore("the quick brown fox", "quick brown bear")
	// only "quick"+"brown"+"fox" pass the >2-char filter (the+quick+brown+fox=4 terms)
	// hits = 2 (quick, brown), terms = 4 → 2/4 = 0.5
	assert.InDelta(t, 0.5, score, 0.001)
	assert.Equal(t, float32(0), lexicalScore("a b c", "anything"), "all-short-terms returns 0")
}

func TestRenderTemplateSubstitutes(t *testing.T) {
	t.Parallel()
	got := renderTemplate("hello {name}, age {age}", map[string]any{"name": "alice", "age": 30})
	assert.Equal(t, "hello alice, age 30", got)
}
