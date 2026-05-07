package pipelineexpression

import (
	"encoding/json"
	"fmt"
	"strings"
)

// NodeValidationError is one validation failure attached to a single
// pipeline node.
type NodeValidationError struct {
	NodeID  string  `json:"node_id"`
	Column  *string `json:"column"`
	Message string  `json:"message"`
}

// NodeValidationReport is the per-node result of [ValidateNodesJSON].
type NodeValidationReport struct {
	NodeID string                `json:"node_id"`
	Status string                `json:"status"`
	Errors []NodeValidationError `json:"errors"`
}

// PipelineValidationReport is the full pipeline result of
// [ValidateNodesJSON].
type PipelineValidationReport struct {
	PipelineID string                 `json:"pipeline_id"`
	AllValid   bool                   `json:"all_valid"`
	Nodes      []NodeValidationReport `json:"nodes"`
}

const (
	statusValid   = "VALID"
	statusInvalid = "INVALID"
)

// ValidateNodesJSON walks the pipeline DAG (an array of node objects
// shaped like the persisted JSON) and returns one report per node, in
// input order.
//
// Mirrors the Rust public surface so `pipeline-authoring-service` can
// call it from the Go port without changes to the wire format.
func ValidateNodesJSON(pipelineID string, nodesJSON json.RawMessage) PipelineValidationReport {
	nodes := parseNodesArray(nodesJSON)

	nodeReports := make([]NodeValidationReport, 0, len(nodes))
	for _, node := range nodes {
		nodeReports = append(nodeReports, validateOne(node, nodes))
	}
	allValid := true
	for _, r := range nodeReports {
		if r.Status != statusValid {
			allValid = false
			break
		}
	}
	return PipelineValidationReport{
		PipelineID: pipelineID,
		AllValid:   allValid,
		Nodes:      nodeReports,
	}
}

// parseNodesArray decodes the raw nodes array. Anything that doesn't
// decode as a JSON array becomes an empty slice — matches the Rust
// `nodes_json.as_array().unwrap_or(&empty)` behaviour.
func parseNodesArray(raw json.RawMessage) []json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	return arr
}

func validateOne(node json.RawMessage, allNodes []json.RawMessage) NodeValidationReport {
	id := nodeID(node)
	transform := stringField(node, "transform_type")
	configRaw := rawField(node, "config")
	dependsOn := stringArrayField(node, "depends_on")

	env := synthEnv(dependsOn, allNodes)
	var errors []NodeValidationError

	switch strings.ToLower(transform) {
	case "passthrough":
		// no-op
	case "filter":
		checkFilter(id, configRaw, env, &errors)
	case "cast", "title_case", "clean_string":
		checkColumnsArray(id, transform, configRaw, &errors)
	case "join":
		checkRequiredString(id, "how", configRaw, &errors)
		checkRequiredArray(id, "on", configRaw, &errors)
	case "union":
		if len(dependsOn) < 2 {
			errors = append(errors, NodeValidationError{
				NodeID:  id,
				Message: "union requires at least 2 upstream nodes",
			})
		}
	case "group_by":
		checkRequiredStringArray(id, "keys", configRaw, &errors)
		checkRequiredArray(id, "aggregations", configRaw, &errors)
	case "window":
		checkRequiredStringArray(id, "partition_by", configRaw, &errors)
		checkRequiredStringArray(id, "order_by", configRaw, &errors)
	case "pivot":
		checkRequiredString(id, "pivot_column", configRaw, &errors)
		checkRequiredString(id, "value_column", configRaw, &errors)
	default:
		other := strings.ToLower(transform)
		if _, ok := TransformSignatureFor(other); !ok && !isKnownOther(other) {
			errors = append(errors, NodeValidationError{
				NodeID:  id,
				Message: fmt.Sprintf("unknown transform_type '%s'", transform),
			})
		}
	}

	status := statusValid
	if len(errors) > 0 {
		status = statusInvalid
	}
	if errors == nil {
		errors = []NodeValidationError{}
	}
	return NodeValidationReport{
		NodeID: id,
		Status: status,
		Errors: errors,
	}
}

func isKnownOther(name string) bool {
	switch name {
	case "sql", "python", "llm", "wasm",
		"media_set_input", "media_set_output", "media_transform",
		"convert_media_set_to_table_rows", "get_media_references",
		"sync", "health_check", "analytical", "export":
		return true
	}
	return false
}

func checkFilter(nodeID string, config json.RawMessage, env ColumnEnv, errors *[]NodeValidationError) {
	predicate := stringField(config, "predicate")
	if strings.TrimSpace(predicate) == "" {
		*errors = append(*errors, NodeValidationError{
			NodeID:  nodeID,
			Message: "filter requires a non-empty `predicate` string",
		})
		return
	}

	parsed, err := ParseExpr(predicate)
	if err != nil {
		*errors = append(*errors, NodeValidationError{
			NodeID:  nodeID,
			Message: fmt.Sprintf("predicate parse error: %s", err.Error()),
		})
		return
	}

	// Empty env => the upstream nodes don't expose schema info yet;
	// suppress UnknownColumn errors so the squiggle UI doesn't spam.
	permissive := env.IsEmpty()

	t, typeErrors := InferExpr(parsed, env)
	if len(typeErrors) == 0 {
		if t.Kind != KindBoolean {
			*errors = append(*errors, NodeValidationError{
				NodeID:  nodeID,
				Message: fmt.Sprintf("predicate must return Boolean, got %s", typeHintDebug(t)),
			})
		}
		return
	}
	for _, te := range typeErrors {
		if permissive && te.Kind == TypeErrUnknownColumn {
			continue
		}
		col := columnFromTypeError(te)
		*errors = append(*errors, NodeValidationError{
			NodeID:  nodeID,
			Column:  col,
			Message: te.Error(),
		})
	}
}

func columnFromTypeError(err TypeError) *string {
	if err.Kind == TypeErrUnknownColumn {
		s := err.Detail
		return &s
	}
	return nil
}

func checkColumnsArray(nodeID, transform string, config json.RawMessage, errors *[]NodeValidationError) {
	raw := rawField(config, "columns")
	if len(raw) == 0 || string(raw) == "null" {
		*errors = append(*errors, NodeValidationError{
			NodeID:  nodeID,
			Message: fmt.Sprintf("%s requires a `columns` config key", transform),
		})
		return
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		*errors = append(*errors, NodeValidationError{
			NodeID:  nodeID,
			Message: fmt.Sprintf("%s requires `columns` to be a non-empty array of strings", transform),
		})
		return
	}
	if len(arr) == 0 {
		*errors = append(*errors, NodeValidationError{
			NodeID:  nodeID,
			Message: fmt.Sprintf("%s requires `columns` to be a non-empty array of strings", transform),
		})
		return
	}
	for _, raw := range arr {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			*errors = append(*errors, NodeValidationError{
				NodeID:  nodeID,
				Message: fmt.Sprintf("%s requires `columns` to be a non-empty array of strings", transform),
			})
			return
		}
	}
}

func checkRequiredStringArray(nodeID, key string, config json.RawMessage, errors *[]NodeValidationError) {
	raw := rawField(config, key)
	if len(raw) == 0 {
		*errors = append(*errors, NodeValidationError{
			NodeID:  nodeID,
			Message: fmt.Sprintf("`%s` must be an array of strings", key),
		})
		return
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		*errors = append(*errors, NodeValidationError{
			NodeID:  nodeID,
			Message: fmt.Sprintf("`%s` must be an array of strings", key),
		})
		return
	}
	for _, item := range arr {
		var s string
		if err := json.Unmarshal(item, &s); err != nil {
			*errors = append(*errors, NodeValidationError{
				NodeID:  nodeID,
				Message: fmt.Sprintf("`%s` must be an array of strings", key),
			})
			return
		}
	}
}

func checkRequiredArray(nodeID, key string, config json.RawMessage, errors *[]NodeValidationError) {
	raw := rawField(config, key)
	if len(raw) == 0 {
		*errors = append(*errors, NodeValidationError{
			NodeID:  nodeID,
			Message: fmt.Sprintf("`%s` must be an array", key),
		})
		return
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		*errors = append(*errors, NodeValidationError{
			NodeID:  nodeID,
			Message: fmt.Sprintf("`%s` must be an array", key),
		})
	}
}

func checkRequiredString(nodeID, key string, config json.RawMessage, errors *[]NodeValidationError) {
	v := stringField(config, key)
	if strings.TrimSpace(v) == "" {
		*errors = append(*errors, NodeValidationError{
			NodeID:  nodeID,
			Message: fmt.Sprintf("`%s` must be a non-empty string", key),
		})
	}
}

func synthEnv(dependsOn []string, allNodes []json.RawMessage) ColumnEnv {
	env := NewColumnEnv()
	for _, upstreamID := range dependsOn {
		var upstream json.RawMessage
		for _, n := range allNodes {
			if nodeID(n) == upstreamID {
				upstream = n
				break
			}
		}
		if len(upstream) == 0 {
			continue
		}
		cfg := rawField(upstream, "config")
		for _, key := range []string{"columns", "output_columns"} {
			raw := rawField(cfg, key)
			if len(raw) == 0 {
				continue
			}
			var arr []json.RawMessage
			if err := json.Unmarshal(raw, &arr); err != nil {
				continue
			}
			for _, item := range arr {
				var s string
				if err := json.Unmarshal(item, &s); err != nil {
					continue
				}
				env.Insert(s, StringType())
			}
		}
	}
	return env
}

// nodeID extracts the `id` field from a JSON node object. Returns ""
// when the field is missing or not a string.
func nodeID(node json.RawMessage) string {
	return stringField(node, "id")
}

func stringField(raw json.RawMessage, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	field, ok := obj[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(field, &s); err != nil {
		return ""
	}
	return s
}

func stringArrayField(raw json.RawMessage, key string) []string {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	field, ok := obj[key]
	if !ok {
		return nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(field, &arr); err != nil {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		var s string
		if err := json.Unmarshal(item, &s); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out
}

func rawField(raw json.RawMessage, key string) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	return obj[key]
}
