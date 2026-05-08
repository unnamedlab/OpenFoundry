package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/copilot"
	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

// ExecutePlan walks the plan, dispatching each step that names a
// tool to ExecuteTool and synthesising structured observations for
// the synthesize-answer / retrieve-context built-in steps. Mirrors
// libs/ai-kernel/src/domain/agents/executor.rs::execute_plan.
//
// `client`, `incomingHeaders`, and `ctx` are forwarded to the HTTP
// tool dispatch (http_json + openfoundry_api). All other tool modes
// are pure-logic and don't touch the network.
func ExecutePlan(
	ctx context.Context,
	client *http.Client,
	plan []models.AgentPlanStep,
	tools []models.ToolDefinition,
	userMessage, objective string,
	contextValue json.RawMessage,
	incomingHeaders http.Header,
	knowledgeHits []models.KnowledgeSearchResult,
) []models.AgentExecutionTrace {
	if client == nil {
		client = http.DefaultClient
	}
	traces := make([]models.AgentExecutionTrace, 0, len(plan))
	successfulInvocations := 0

	for _, step := range plan {
		var output json.RawMessage
		var observation string

		if step.ToolName != nil && *step.ToolName != "" {
			toolName := *step.ToolName
			tool := findTool(tools, toolName)
			out := ExecuteTool(ctx, client, tool, toolName, userMessage, objective, contextValue, incomingHeaders, knowledgeHits)
			output = out
			status := jsonStringField(out, "status")
			switch status {
			case "completed":
				observation = fmt.Sprintf("Executed tool '%s'.", toolName)
				successfulInvocations++
			case "failed":
				observation = fmt.Sprintf("Tool '%s' failed.", toolName)
			case "skipped":
				observation = fmt.Sprintf("Tool '%s' was skipped.", toolName)
			case "":
				observation = fmt.Sprintf("Tool '%s' produced an unstructured response.", toolName)
			default:
				observation = fmt.Sprintf("Tool '%s' finished with status '%s'.", toolName, status)
			}
		} else if step.ID == "retrieve-context" {
			citations := make([]map[string]any, 0, len(knowledgeHits))
			for _, hit := range knowledgeHits {
				citations = append(citations, map[string]any{
					"document_title": hit.DocumentTitle,
					"score":          hit.Score,
					"excerpt":        hit.Excerpt,
				})
			}
			output, _ = json.Marshal(map[string]any{"citations": citations})
			observation = fmt.Sprintf("Retrieved %d knowledge hit(s).", len(knowledgeHits))
		} else if step.ID == "synthesize-answer" {
			output = json.RawMessage(`{"status":"completed"}`)
			observation = fmt.Sprintf(
				"Prepared final synthesis with %d successful tool invocation(s) and %d knowledge hit(s).",
				successfulInvocations, len(knowledgeHits))
		} else {
			output = json.RawMessage(`{"status":"completed"}`)
			observation = fmt.Sprintf("Completed plan step '%s'.", step.Title)
		}

		traces = append(traces, models.AgentExecutionTrace{
			StepID:      step.ID,
			Title:       step.Title,
			ToolName:    step.ToolName,
			Observation: observation,
			Output:      output,
		})
	}
	return traces
}

// ExecuteTool dispatches a single tool by execution_mode. Mirrors
// fn execute_tool. Returns a status="failed" envelope when the tool
// definition is missing.
func ExecuteTool(
	ctx context.Context,
	client *http.Client,
	tool *models.ToolDefinition,
	toolName, userMessage, objective string,
	contextValue json.RawMessage,
	incomingHeaders http.Header,
	knowledgeHits []models.KnowledgeSearchResult,
) json.RawMessage {
	if tool == nil {
		out, _ := json.Marshal(map[string]any{
			"tool":   toolName,
			"status": "failed",
			"error":  "tool definition not found",
		})
		return out
	}

	toolInputs := resolveToolInputs(tool, toolName, userMessage, objective, contextValue)
	if blocked := enforceToolPolicy(tool, toolInputs, contextValue); blocked != nil {
		return blocked
	}

	switch tool.ExecutionMode {
	case "simulated":
		return executeSimulatedTool(tool, toolInputs)
	case "knowledge_search":
		return executeKnowledgeSearchTool(tool, toolInputs, knowledgeHits)
	case "native_sql":
		return executeNativeSQLTool(tool, userMessage, objective, toolInputs, knowledgeHits)
	case "native_dataset":
		return executeNativeDatasetTool(tool, userMessage, objective, toolInputs, knowledgeHits)
	case "native_ontology":
		return executeNativeOntologyTool(tool, userMessage, objective, toolInputs, knowledgeHits)
	case "native_pipeline":
		return executeNativePipelineTool(tool, userMessage, objective, toolInputs)
	case "native_report":
		return executeNativeReportTool(tool, userMessage, objective, toolInputs)
	case "native_workflow":
		return executeNativeWorkflowTool(tool, userMessage, objective, toolInputs)
	case "native_code_repo":
		return executeNativeCodeRepoTool(tool, userMessage, objective, toolInputs)
	case "http_json":
		return executeHTTPTool(ctx, client, tool, toolInputs, incomingHeaders, false)
	case "openfoundry_api":
		return executeHTTPTool(ctx, client, tool, toolInputs, incomingHeaders, true)
	default:
		out, _ := json.Marshal(map[string]any{
			"tool":     tool.Name,
			"category": tool.Category,
			"status":   "skipped",
			"reason":   fmt.Sprintf("unsupported execution_mode '%s'", tool.ExecutionMode),
		})
		return out
	}
}

func findTool(tools []models.ToolDefinition, name string) *models.ToolDefinition {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

func resolveToolInputs(tool *models.ToolDefinition, toolName, userMessage, objective string, contextValue json.RawMessage) json.RawMessage {
	if len(contextValue) > 0 {
		var ctxObj map[string]json.RawMessage
		if err := json.Unmarshal(contextValue, &ctxObj); err == nil {
			if rawInputs, ok := ctxObj["tool_inputs"]; ok {
				var inputs map[string]json.RawMessage
				if err := json.Unmarshal(rawInputs, &inputs); err == nil {
					if v, ok := inputs[toolName]; ok && len(v) > 0 {
						return v
					}
					if v, ok := inputs[tool.ID.String()]; ok && len(v) > 0 {
						return v
					}
				}
			}
		}
	}
	defaultInputs := map[string]string{
		"question":     userMessage,
		"user_message": userMessage,
		"objective":    objective,
	}
	out, _ := json.Marshal(defaultInputs)
	return out
}

func enforceToolPolicy(tool *models.ToolDefinition, toolInputs, contextValue json.RawMessage) json.RawMessage {
	var execConfig map[string]any
	if len(tool.ExecutionConfig) > 0 {
		_ = json.Unmarshal(tool.ExecutionConfig, &execConfig)
	}
	sensitivity := stringFromMap(execConfig, "sensitivity", "normal")
	requiresApproval := false
	if v, ok := execConfig["requires_approval"].(bool); ok {
		requiresApproval = v
	} else {
		switch sensitivity {
		case "high", "mutating", "admin":
			requiresApproval = true
		}
	}
	if !requiresApproval {
		return nil
	}

	var ctxObj map[string]any
	if len(contextValue) > 0 {
		_ = json.Unmarshal(contextValue, &ctxObj)
	}
	policy, _ := ctxObj["tool_policy"].(map[string]any)
	allowSensitive := false
	if v, ok := policy["allow_sensitive_tools"].(bool); ok {
		allowSensitive = v
	}
	approvedTools := stringArrayFrom(policy["approved_tools"])
	approved := allowSensitive
	if !approved {
		toolID := tool.ID.String()
		for _, entry := range approvedTools {
			if entry == tool.Name || entry == toolID {
				approved = true
				break
			}
		}
	}
	if approved {
		return nil
	}

	var inputsAny any
	_ = json.Unmarshal(toolInputs, &inputsAny)

	out, _ := json.Marshal(map[string]any{
		"tool":                     tool.Name,
		"category":                 tool.Category,
		"status":                   "blocked",
		"approval_required":        true,
		"sensitivity":              sensitivity,
		"required_approval_scope":  stringFromMap(execConfig, "approval_scope", "operator"),
		"request":                  inputsAny,
		"reason":                   fmt.Sprintf("tool '%s' requires approval before execution", tool.Name),
	})
	return out
}

func executeSimulatedTool(tool *models.ToolDefinition, toolInputs json.RawMessage) json.RawMessage {
	var execConfig map[string]json.RawMessage
	if len(tool.ExecutionConfig) > 0 {
		_ = json.Unmarshal(tool.ExecutionConfig, &execConfig)
	}
	mockResponse, hasMock := execConfig["mock_response"]
	var inputsAny any
	_ = json.Unmarshal(toolInputs, &inputsAny)
	if hasMock {
		var mockAny any
		_ = json.Unmarshal(mockResponse, &mockAny)
		out, _ := json.Marshal(map[string]any{
			"tool":      tool.Name,
			"category":  tool.Category,
			"status":    "completed",
			"simulated": true,
			"request":   inputsAny,
			"response":  mockAny,
		})
		return out
	}
	out, _ := json.Marshal(map[string]any{
		"tool":      tool.Name,
		"category":  tool.Category,
		"status":    "skipped",
		"simulated": true,
		"reason":    "simulated tool has no mock_response configured",
	})
	return out
}

func executeKnowledgeSearchTool(tool *models.ToolDefinition, toolInputs json.RawMessage, knowledgeHits []models.KnowledgeSearchResult) json.RawMessage {
	var inputs map[string]any
	_ = json.Unmarshal(toolInputs, &inputs)
	var execConfig map[string]any
	_ = json.Unmarshal(tool.ExecutionConfig, &execConfig)

	query := nonEmptyString(inputs, "query")
	if query == "" {
		query = nonEmptyString(inputs, "question")
	}
	topK := uintFromMap(inputs, "top_k", 0)
	if topK == 0 {
		topK = uintFromMap(execConfig, "top_k", 4)
	}
	if topK == 0 {
		topK = 1
	}
	minScore := float32FromMap(inputs, "min_score", -1)
	if minScore < 0 {
		minScore = float32FromMap(execConfig, "min_score", 0.15)
	}

	type ranked struct {
		score          float32
		retrievalScore float32
		hit            models.KnowledgeSearchResult
	}
	candidates := make([]ranked, 0, len(knowledgeHits))
	for _, h := range knowledgeHits {
		lex := lexicalScore(query, h.DocumentTitle+" "+h.Excerpt)
		score := h.Score
		if strings.TrimSpace(query) != "" {
			score = h.Score*0.6 + lex*0.4
			if score > 1.0 {
				score = 1.0
			}
		}
		if score < minScore {
			continue
		}
		candidates = append(candidates, ranked{score: score, retrievalScore: h.Score, hit: h})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	if int(topK) < len(candidates) {
		candidates = candidates[:topK]
	}

	results := make([]map[string]any, 0, len(candidates))
	for _, c := range candidates {
		results = append(results, map[string]any{
			"document_title":  c.hit.DocumentTitle,
			"score":           c.score,
			"retrieval_score": c.retrievalScore,
			"excerpt":         c.hit.Excerpt,
			"source_uri":      c.hit.SourceURI,
			"metadata":        rawOrAny(c.hit.Metadata),
		})
	}

	out, _ := json.Marshal(map[string]any{
		"tool":         tool.Name,
		"category":     tool.Category,
		"status":       "completed",
		"query":        query,
		"result_count": len(results),
		"results":      results,
	})
	return out
}

func executeNativeSQLTool(tool *models.ToolDefinition, userMessage, objective string, toolInputs json.RawMessage, knowledgeHits []models.KnowledgeSearchResult) json.RawMessage {
	var inputs map[string]any
	_ = json.Unmarshal(toolInputs, &inputs)
	var execConfig map[string]any
	_ = json.Unmarshal(tool.ExecutionConfig, &execConfig)

	question := nonEmptyString(inputs, "question")
	if question == "" {
		question = userMessage
	}
	datasetIDs := extractUUIDArray(inputs["dataset_ids"])
	draft := copilot.Assist(question, datasetIDs, nil, knowledgeHits, true, false)

	tableName := nonEmptyString(inputs, "dataset_name")
	if tableName == "" {
		tableName = nonEmptyString(execConfig, "default_dataset_name")
	}
	if tableName == "" {
		tableName = "your_dataset"
	}
	timeColumn := nonEmptyString(inputs, "time_column")
	if timeColumn == "" {
		timeColumn = nonEmptyString(execConfig, "time_column")
	}
	if timeColumn == "" {
		if strings.Contains(strings.ToLower(question), "event") {
			timeColumn = "event_date"
		} else {
			timeColumn = "created_at"
		}
	}
	limit := uintFromMap(inputs, "limit", 0)
	if limit == 0 {
		limit = uintFromMap(execConfig, "default_limit", 100)
	}
	if limit == 0 {
		limit = 1
	}
	lookbackDays := inferTimeWindowDays(question)
	metricHints := append([]string{}, stringArrayFrom(execConfig["metric_hints"])...)
	metricHints = append(metricHints, stringArrayFrom(inputs["metric_hints"])...)

	var generatedSQL string
	if draft.SuggestedSQL != nil {
		generatedSQL = *draft.SuggestedSQL
	} else {
		orderColumn := timeColumn
		for _, hint := range metricHints {
			lower := strings.ToLower(hint)
			if strings.Contains(lower, "latency") || strings.Contains(lower, "error") {
				orderColumn = metricHints[0]
				break
			}
		}
		generatedSQL = fmt.Sprintf(
			"SELECT *\nFROM %s\nWHERE %s >= CURRENT_DATE - INTERVAL '%d days'\nORDER BY %s DESC\nLIMIT %d;",
			tableName, timeColumn, lookbackDays, orderColumn, limit,
		)
	}

	out, _ := json.Marshal(map[string]any{
		"tool":          tool.Name,
		"category":      tool.Category,
		"status":        "completed",
		"sql":           generatedSQL,
		"dataset_name":  tableName,
		"lookback_days": lookbackDays,
		"limit":         limit,
		"objective":     objective,
		"explanation": fmt.Sprintf(
			"Generated starter SQL for '%s' using '%s' as the working dataset.",
			question, tableName),
	})
	return out
}

func executeNativeOntologyTool(tool *models.ToolDefinition, userMessage, objective string, toolInputs json.RawMessage, knowledgeHits []models.KnowledgeSearchResult) json.RawMessage {
	var inputs map[string]any
	_ = json.Unmarshal(toolInputs, &inputs)
	var execConfig map[string]any
	_ = json.Unmarshal(tool.ExecutionConfig, &execConfig)

	answer := nonEmptyString(inputs, "answer")
	if answer == "" {
		answer = userMessage
	}
	ontologyTypeIDs := extractUUIDArray(inputs["ontology_type_ids"])
	draft := copilot.Assist(answer, nil, ontologyTypeIDs, knowledgeHits, false, false)

	objectTypes := append([]string{}, stringArrayFrom(execConfig["default_object_types"])...)
	objectTypes = append(objectTypes, inferObjectTypes(answer)...)
	dedupSort(&objectTypes)

	linkType := nonEmptyString(execConfig, "default_link_type")
	if linkType == "" {
		linkType = "RELATED_TO"
	}

	actions := inferActionHints(answer, objective)
	links := []string{}
	if len(objectTypes) >= 2 {
		links = append(links, fmt.Sprintf("%s --%s--> %s", objectTypes[0], linkType, objectTypes[1]))
	}

	out, _ := json.Marshal(map[string]any{
		"tool":                tool.Name,
		"category":            tool.Category,
		"status":              "completed",
		"object_types":        objectTypes,
		"link_suggestions":    links,
		"ontology_hints":      draft.OntologyHints,
		"recommended_actions": actions,
	})
	return out
}

func executeNativeDatasetTool(tool *models.ToolDefinition, userMessage, objective string, toolInputs json.RawMessage, knowledgeHits []models.KnowledgeSearchResult) json.RawMessage {
	var inputs map[string]any
	_ = json.Unmarshal(toolInputs, &inputs)
	var execConfig map[string]any
	_ = json.Unmarshal(tool.ExecutionConfig, &execConfig)

	question := nonEmptyString(inputs, "question")
	if question == "" {
		question = userMessage
	}
	datasetIDs := extractUUIDArray(inputs["dataset_ids"])
	draft := copilot.Assist(question, datasetIDs, nil, knowledgeHits, true, false)

	datasetName := nonEmptyString(inputs, "dataset_name")
	if datasetName == "" {
		datasetName = nonEmptyString(execConfig, "default_dataset_name")
	}
	if datasetName == "" {
		datasetName = "operational_dataset"
	}
	operation := inferDatasetOperation(question, objective)
	branchPrefix := nonEmptyString(execConfig, "branch_prefix")
	if branchPrefix == "" {
		branchPrefix = "analysis"
	}
	branch := branchPrefix + "-" + strings.ReplaceAll(datasetName, "_", "-")
	governance := inferDatasetGovernance(question, objective)

	out, _ := json.Marshal(map[string]any{
		"tool":                 tool.Name,
		"category":             tool.Category,
		"status":               "completed",
		"dataset_name":         datasetName,
		"dataset_ids":          datasetIDs,
		"operation":            operation,
		"recommended_branch":   branch,
		"governance_checks":    governance,
		"suggested_sql":        draft.SuggestedSQL,
		"next_actions": []string{
			fmt.Sprintf("Preview rows from '%s'.", datasetName),
			fmt.Sprintf("Run dataset linter before mutating '%s'.", datasetName),
			fmt.Sprintf("Open a branch and stage changes for '%s'.", datasetName),
		},
	})
	return out
}

func executeNativePipelineTool(tool *models.ToolDefinition, userMessage, objective string, toolInputs json.RawMessage) json.RawMessage {
	var inputs map[string]any
	_ = json.Unmarshal(toolInputs, &inputs)
	var execConfig map[string]any
	_ = json.Unmarshal(tool.ExecutionConfig, &execConfig)

	question := nonEmptyString(inputs, "question")
	if question == "" {
		question = userMessage
	}
	pipelineName := nonEmptyString(inputs, "pipeline_name")
	if pipelineName == "" {
		pipelineName = nonEmptyString(execConfig, "default_pipeline_name")
	}
	if pipelineName == "" {
		pipelineName = "platform_pipeline"
	}
	runMode := "incremental"
	combined := strings.ToLower(question + " " + objective)
	if strings.Contains(combined, "full rebuild") {
		runMode = "full_rebuild"
	}
	trigger := inferPipelineTrigger(question, objective)

	out, _ := json.Marshal(map[string]any{
		"tool":                 tool.Name,
		"category":             tool.Category,
		"status":               "completed",
		"pipeline_name":        pipelineName,
		"run_mode":             runMode,
		"trigger_reason":       trigger,
		"recommended_inputs":   stringArrayFrom(execConfig["input_datasets"]),
		"recommended_outputs":  stringArrayFrom(execConfig["output_datasets"]),
		"next_actions": []string{
			fmt.Sprintf("Inspect latest run for '%s'.", pipelineName),
			fmt.Sprintf("Trigger %s execution for '%s'.", runMode, pipelineName),
			"Review downstream lineage impact before promotion.",
		},
	})
	return out
}

func executeNativeReportTool(tool *models.ToolDefinition, userMessage, objective string, toolInputs json.RawMessage) json.RawMessage {
	var inputs map[string]any
	_ = json.Unmarshal(toolInputs, &inputs)
	var execConfig map[string]any
	_ = json.Unmarshal(tool.ExecutionConfig, &execConfig)

	prompt := nonEmptyString(inputs, "question")
	if prompt == "" {
		prompt = userMessage
	}
	reportName := nonEmptyString(inputs, "report_name")
	if reportName == "" {
		reportName = nonEmptyString(execConfig, "default_report_name")
	}
	if reportName == "" {
		reportName = "operations_digest"
	}
	channels := inferReportChannels(prompt, stringArrayFrom(execConfig["default_channels"]))

	out, _ := json.Marshal(map[string]any{
		"tool":                  tool.Name,
		"category":              tool.Category,
		"status":                "completed",
		"report_name":           reportName,
		"distribution_channels": channels,
		"schedule_hint":         inferReportSchedule(prompt, objective),
		"delivery_actions": []string{
			fmt.Sprintf("Generate preview for '%s'.", reportName),
			fmt.Sprintf("Dispatch '%s' through the selected channels.", reportName),
			"Archive the execution manifest in object storage.",
		},
	})
	return out
}

func executeNativeWorkflowTool(tool *models.ToolDefinition, userMessage, objective string, toolInputs json.RawMessage) json.RawMessage {
	var inputs map[string]any
	_ = json.Unmarshal(toolInputs, &inputs)
	var execConfig map[string]any
	_ = json.Unmarshal(tool.ExecutionConfig, &execConfig)

	prompt := nonEmptyString(inputs, "question")
	if prompt == "" {
		prompt = userMessage
	}
	workflowName := nonEmptyString(inputs, "workflow_name")
	if workflowName == "" {
		workflowName = nonEmptyString(execConfig, "default_workflow_name")
	}
	if workflowName == "" {
		workflowName = "operator_review"
	}
	approvalMode := nonEmptyString(execConfig, "approval_scope")
	if approvalMode == "" {
		approvalMode = "operator"
	}

	out, _ := json.Marshal(map[string]any{
		"tool":           tool.Name,
		"category":       tool.Category,
		"status":         "completed",
		"workflow_name":  workflowName,
		"proposal_type":  inferWorkflowProposal(prompt, objective),
		"approval_mode":  approvalMode,
		"steps": []string{
			"Assemble action proposal",
			"Request human approval",
			"Execute submit_action or notification step after approval",
		},
	})
	return out
}

func executeNativeCodeRepoTool(tool *models.ToolDefinition, userMessage, objective string, toolInputs json.RawMessage) json.RawMessage {
	var inputs map[string]any
	_ = json.Unmarshal(toolInputs, &inputs)
	var execConfig map[string]any
	_ = json.Unmarshal(tool.ExecutionConfig, &execConfig)

	prompt := nonEmptyString(inputs, "question")
	if prompt == "" {
		prompt = userMessage
	}
	repository := nonEmptyString(inputs, "repository")
	if repository == "" {
		repository = nonEmptyString(execConfig, "default_repository")
	}
	if repository == "" {
		repository = "openfoundry-platform"
	}
	branchPrefix := nonEmptyString(execConfig, "branch_prefix")
	if branchPrefix == "" {
		branchPrefix = "agent"
	}
	branch := branchPrefix + "/" + inferRepoBranchSlug(prompt, objective)

	out, _ := json.Marshal(map[string]any{
		"tool":                  tool.Name,
		"category":              tool.Category,
		"status":                "completed",
		"repository":            repository,
		"branch":                branch,
		"merge_request_title":   inferRepoMRTitle(prompt, objective),
		"required_checks":       stringArrayFrom(execConfig["required_checks"]),
		"next_actions": []string{
			fmt.Sprintf("Create or update branch '%s'.", branch),
			"Run required CI checks before merge.",
			"Open a merge request with operator-facing summary.",
		},
	})
	return out
}

func executeHTTPTool(ctx context.Context, client *http.Client, tool *models.ToolDefinition, toolInputs json.RawMessage, incomingHeaders http.Header, platformMode bool) json.RawMessage {
	var inputs map[string]any
	_ = json.Unmarshal(toolInputs, &inputs)
	var execConfig map[string]any
	_ = json.Unmarshal(tool.ExecutionConfig, &execConfig)

	resolvedURL := resolveHTTPURL(execConfig, inputs, platformMode)
	if resolvedURL == "" {
		errMsg := "missing execution_config.url"
		if platformMode {
			errMsg = "missing execution_config.path or execution_config.url/base_url for openfoundry_api"
		}
		out, _ := json.Marshal(map[string]any{
			"tool":     tool.Name,
			"category": tool.Category,
			"status":   "failed",
			"error":    errMsg,
		})
		return out
	}

	method := strings.ToUpper(stringFromMap(execConfig, "method", "POST"))
	authMode := "none"
	if platformMode {
		authMode = "forward_bearer"
	}
	authMode = stringFromMap(execConfig, "auth_mode", authMode)

	var body io.Reader
	requestURL := resolvedURL
	if method == http.MethodGet {
		if obj, ok := inputs["question"]; ok {
			_ = obj
		}
		// rebuild URL with query string from inputs map
		query := url.Values{}
		for k, v := range inputs {
			query.Set(k, queryValue(v))
		}
		if len(query) > 0 {
			sep := "?"
			if strings.Contains(requestURL, "?") {
				sep = "&"
			}
			requestURL = requestURL + sep + query.Encode()
		}
	} else {
		body = bytes.NewReader(toolInputs)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		out, _ := json.Marshal(map[string]any{
			"tool":     tool.Name,
			"category": tool.Category,
			"status":   "failed",
			"error":    fmt.Sprintf("tool request failed: %s", err.Error()),
			"url":      resolvedURL,
		})
		return out
	}
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}
	if authMode == "forward_bearer" {
		if v := incomingHeaders.Get("Authorization"); v != "" {
			req.Header.Set("Authorization", v)
		}
	}
	if extraHeaders, ok := execConfig["headers"].(map[string]any); ok {
		for k, v := range extraHeaders {
			if s, ok := v.(string); ok {
				req.Header.Set(k, s)
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		out, _ := json.Marshal(map[string]any{
			"tool":     tool.Name,
			"category": tool.Category,
			"status":   "failed",
			"error":    fmt.Sprintf("tool request failed: %s", err.Error()),
			"url":      resolvedURL,
		})
		return out
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		out, _ := json.Marshal(map[string]any{
			"tool":        tool.Name,
			"category":    tool.Category,
			"status":      "failed",
			"http_status": resp.StatusCode,
			"error":       fmt.Sprintf("failed to parse tool response: %s", err.Error()),
			"url":         resolvedURL,
		})
		return out
	}
	var responseAny any
	if err := json.Unmarshal(payload, &responseAny); err != nil {
		out, _ := json.Marshal(map[string]any{
			"tool":        tool.Name,
			"category":    tool.Category,
			"status":      "failed",
			"http_status": resp.StatusCode,
			"error":       fmt.Sprintf("failed to parse tool response: %s", err.Error()),
			"url":         resolvedURL,
		})
		return out
	}
	status := "completed"
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status = "failed"
	}
	var inputsAny any
	_ = json.Unmarshal(toolInputs, &inputsAny)
	out, _ := json.Marshal(map[string]any{
		"tool":        tool.Name,
		"category":    tool.Category,
		"status":      status,
		"http_status": resp.StatusCode,
		"request":     inputsAny,
		"response":    responseAny,
		"url":         resolvedURL,
	})
	return out
}

func resolveHTTPURL(execConfig, toolInputs map[string]any, platformMode bool) string {
	if rawURL, ok := execConfig["url"].(string); ok {
		rendered := renderTemplate(rawURL, toolInputs)
		if strings.TrimSpace(rendered) != "" {
			return rendered
		}
	}
	if !platformMode {
		return ""
	}
	rawPath, _ := execConfig["path"].(string)
	if rawPath == "" {
		rawPath, _ = execConfig["path_template"].(string)
	}
	if rawPath == "" {
		return ""
	}
	rendered := renderTemplate(rawPath, toolInputs)
	if strings.TrimSpace(rendered) == "" {
		return ""
	}
	if strings.HasPrefix(rendered, "http://") || strings.HasPrefix(rendered, "https://") {
		return rendered
	}
	if !strings.HasPrefix(rendered, "/api/") {
		return ""
	}
	baseURL, _ := execConfig["base_url"].(string)
	if baseURL == "" {
		baseURL = os.Getenv("OPENFOUNDRY_API_BASE_URL")
	}
	if baseURL == "" {
		return ""
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(rendered, "/")
}

// --- inference helpers (verbatim Rust ports) ----------------------

func inferTimeWindowDays(question string) int {
	lowered := strings.ToLower(question)
	switch {
	case strings.Contains(lowered, "90 day"), strings.Contains(lowered, "90-day"):
		return 90
	case strings.Contains(lowered, "30 day"), strings.Contains(lowered, "30-day"), strings.Contains(lowered, "month"):
		return 30
	case strings.Contains(lowered, "24 hour"), strings.Contains(lowered, "today"):
		return 1
	}
	return 7
}

func inferObjectTypes(content string) []string {
	lowered := strings.ToLower(content)
	types := []string{}
	if strings.Contains(lowered, "incident") || strings.Contains(lowered, "alert") {
		types = append(types, "Incident")
	}
	if strings.Contains(lowered, "provider") || strings.Contains(lowered, "model") {
		types = append(types, "Provider")
	}
	if strings.Contains(lowered, "dataset") || strings.Contains(lowered, "table") {
		types = append(types, "Dataset")
	}
	if strings.Contains(lowered, "workflow") || strings.Contains(lowered, "approval") {
		types = append(types, "Workflow")
	}
	if strings.Contains(lowered, "pipeline") || strings.Contains(lowered, "build") {
		types = append(types, "Pipeline")
	}
	if len(types) == 0 {
		types = append(types, "OperationalObject")
	}
	return types
}

func inferActionHints(content, objective string) []string {
	combined := strings.ToLower(content + " " + objective)
	actions := []string{}
	if strings.Contains(combined, "reroute") || strings.Contains(combined, "fallback") {
		actions = append(actions, "Submit reroute action for the affected provider.")
	}
	if strings.Contains(combined, "notify") || strings.Contains(combined, "alert") {
		actions = append(actions, "Notify operators and attach the generated context.")
	}
	if strings.Contains(combined, "approve") || strings.Contains(combined, "review") {
		actions = append(actions, "Open approval workflow with the proposed change set.")
	}
	if len(actions) == 0 {
		actions = append(actions, "Prepare a human-reviewable action proposal.")
	}
	return actions
}

func inferDatasetOperation(content, objective string) string {
	combined := strings.ToLower(content + " " + objective)
	switch {
	case strings.Contains(combined, "lint"), strings.Contains(combined, "quality"):
		return "lint_and_quality_review"
	case strings.Contains(combined, "branch"), strings.Contains(combined, "what-if"):
		return "branch_and_preview"
	case strings.Contains(combined, "export"):
		return "export_and_share"
	}
	return "preview_and_investigate"
}

func inferDatasetGovernance(content, objective string) []string {
	combined := strings.ToLower(content + " " + objective)
	checks := []string{"lineage impact review"}
	if strings.Contains(combined, "pii") || strings.Contains(combined, "sensitive") {
		checks = append(checks, "marking and restricted-view validation")
	}
	if strings.Contains(combined, "export") || strings.Contains(combined, "share") {
		checks = append(checks, "delivery recipient review")
	}
	return checks
}

func inferPipelineTrigger(content, objective string) string {
	combined := strings.ToLower(content + " " + objective)
	switch {
	case strings.Contains(combined, "incident"), strings.Contains(combined, "failed"):
		return "recover_failed_run"
	case strings.Contains(combined, "backfill"):
		return "historical_backfill"
	}
	return "operator_requested_refresh"
}

func inferReportChannels(content string, defaults []string) []string {
	lowered := strings.ToLower(content)
	channels := append([]string{}, defaults...)
	if strings.Contains(lowered, "slack") {
		channels = append(channels, "slack")
	}
	if strings.Contains(lowered, "teams") {
		channels = append(channels, "teams")
	}
	if strings.Contains(lowered, "email") || len(channels) == 0 {
		channels = append(channels, "email")
	}
	if strings.Contains(lowered, "webhook") {
		channels = append(channels, "webhook")
	}
	dedupSort(&channels)
	return channels
}

func inferReportSchedule(content, objective string) string {
	combined := strings.ToLower(content + " " + objective)
	switch {
	case strings.Contains(combined, "daily"):
		return "daily"
	case strings.Contains(combined, "weekly"):
		return "weekly"
	case strings.Contains(combined, "incident"), strings.Contains(combined, "on-demand"):
		return "manual"
	}
	return "cron"
}

func inferWorkflowProposal(content, objective string) string {
	combined := strings.ToLower(content + " " + objective)
	switch {
	case strings.Contains(combined, "submit action"), strings.Contains(combined, "mutate"):
		return "submit_action"
	case strings.Contains(combined, "approval"), strings.Contains(combined, "review"):
		return "approval_review"
	}
	return "notification_orchestration"
}

func inferRepoBranchSlug(content, objective string) string {
	combined := strings.ToLower(content + " " + objective)
	var b strings.Builder
	for _, ch := range combined {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			b.WriteRune(ch)
		} else {
			b.WriteRune('-')
		}
	}
	slug := b.String()
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "agent-change"
	}
	runes := []rune(slug)
	if len(runes) > 48 {
		runes = runes[:48]
	}
	return string(runes)
}

func inferRepoMRTitle(content, objective string) string {
	prompt := strings.TrimSpace(content)
	if prompt == "" {
		return "Agent proposal: " + strings.TrimSpace(objective)
	}
	return "Agent proposal: " + prompt
}

func lexicalScore(query, haystack string) float32 {
	normalised := strings.ToLower(query)
	terms := []string{}
	for _, t := range strings.Fields(normalised) {
		if len([]rune(t)) > 2 {
			terms = append(terms, t)
		}
	}
	if len(terms) == 0 {
		return 0
	}
	hay := strings.ToLower(haystack)
	hits := 0
	for _, t := range terms {
		if strings.Contains(hay, t) {
			hits++
		}
	}
	return float32(hits) / float32(len(terms))
}

func renderTemplate(template string, values map[string]any) string {
	rendered := template
	for k, v := range values {
		rendered = strings.ReplaceAll(rendered, "{"+k+"}", queryValue(v))
	}
	return rendered
}

func queryValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}

// --- generic helpers --------------------------------------------------

func dedupSort(s *[]string) {
	sort.Strings(*s)
	out := (*s)[:0]
	last := ""
	for i, v := range *s {
		if i == 0 || v != last {
			out = append(out, v)
		}
		last = v
	}
	*s = out
}

func nonEmptyString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func stringFromMap(m map[string]any, key, fallback string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func stringArrayFrom(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func uintFromMap(m map[string]any, key string, fallback uint32) uint32 {
	if m == nil {
		return fallback
	}
	switch x := m[key].(type) {
	case float64:
		if x >= 0 {
			return uint32(x)
		}
	case int:
		if x >= 0 {
			return uint32(x)
		}
	case int64:
		if x >= 0 {
			return uint32(x)
		}
	case json.Number:
		if n, err := x.Int64(); err == nil && n >= 0 {
			return uint32(n)
		}
	}
	return fallback
}

func float32FromMap(m map[string]any, key string, fallback float32) float32 {
	if m == nil {
		return fallback
	}
	switch x := m[key].(type) {
	case float64:
		return float32(x)
	case float32:
		return x
	case int:
		return float32(x)
	case int64:
		return float32(x)
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return float32(f)
		}
	}
	return fallback
}

func extractUUIDArray(v any) []uuid.UUID {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]uuid.UUID, 0, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil {
			continue
		}
		out = append(out, id)
	}
	return out
}

func jsonStringField(raw json.RawMessage, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	if s, ok := obj[key].(string); ok {
		return s
	}
	return ""
}

func rawOrAny(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}
