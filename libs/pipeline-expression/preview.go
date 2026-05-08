package pipelineexpression

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"
)

// PreviewOutput is the wire shape returned by [PreviewNode].
type PreviewOutput struct {
	PipelineID   string                       `json:"pipeline_id"`
	NodeID       string                       `json:"node_id"`
	Columns      []string                     `json:"columns"`
	Rows         []map[string]json.RawMessage `json:"rows"`
	SampleSize   int                          `json:"sample_size"`
	GeneratedAt  time.Time                    `json:"generated_at"`
	Seed         uint64                       `json:"seed"`
	SourceChain  []string                     `json:"source_chain"`
	Fresh        bool                         `json:"fresh"`
}

// PreviewErrorKind tags a [PreviewError].
type PreviewErrorKind int

const (
	// PreviewErrNodeNotFound — referenced node is missing from the DAG.
	PreviewErrNodeNotFound PreviewErrorKind = iota
	// PreviewErrCycle — a dependency cycle was detected.
	PreviewErrCycle
	// PreviewErrSeedLoader — the seed loader returned an error.
	PreviewErrSeedLoader
	// PreviewErrTransform — applying a transform failed.
	PreviewErrTransform
)

// PreviewError is the failure mode for [PreviewNode].
type PreviewError struct {
	Kind      PreviewErrorKind
	NodeID    string
	Transform string
	Message   string
}

// Error implements [error]. Mirrors the Rust thiserror messages.
func (e *PreviewError) Error() string {
	switch e.Kind {
	case PreviewErrNodeNotFound:
		return fmt.Sprintf("node '%s' not found in pipeline", e.NodeID)
	case PreviewErrCycle:
		return fmt.Sprintf("cycle detected reaching node '%s'", e.NodeID)
	case PreviewErrSeedLoader:
		return fmt.Sprintf("seed loader failed for node '%s': %s", e.NodeID, e.Message)
	case PreviewErrTransform:
		return fmt.Sprintf("transform '%s' on node '%s': %s", e.Transform, e.NodeID, e.Message)
	}
	return "unknown preview error"
}

// PreviewNodeView is the minimal view a preview consumer must expose
// per-node. Mirrors the Rust trait of the same name.
type PreviewNodeView interface {
	ID() string
	TransformType() string
	Config() json.RawMessage
	DependsOn() []string
}

// SeedLoader provides the source rows for nodes with no upstream
// dependencies. Mirrors the Rust trait of the same name.
type SeedLoader interface {
	Load(node PreviewNodeView, sampleSize int) ([]Row, error)
}

// DeterministicSeedLoader synthesises rows deterministically from
// `pipeline_id + node_id`. Used when no real dataset is bound.
type DeterministicSeedLoader struct {
	PipelineID string
}

// Load implements [SeedLoader].
func (l DeterministicSeedLoader) Load(node PreviewNodeView, sampleSize int) ([]Row, error) {
	seed := computeSeed(l.PipelineID, node.ID())
	cap := sampleSize
	if cap > 50 {
		cap = 50
	}
	return synthesiseRows(seed, node.ID(), cap), nil
}

// DefaultSampleSize matches the Rust constant.
const DefaultSampleSize = 50_000

// PreviewNode runs the preview engine. Mirrors the Rust public surface.
func PreviewNode(
	pipelineID, nodeID string,
	nodes []PreviewNodeView,
	loader SeedLoader,
	sampleSize *int,
) (*PreviewOutput, error) {
	if !nodesContain(nodes, nodeID) {
		return nil, &PreviewError{Kind: PreviewErrNodeNotFound, NodeID: nodeID}
	}
	chain, err := topologicalChain(nodeID, nodes)
	if err != nil {
		return nil, err
	}
	cap := DefaultSampleSize
	if sampleSize != nil {
		cap = *sampleSize
	}
	if cap > DefaultSampleSize {
		cap = DefaultSampleSize
	}
	seed := computeSeed(pipelineID, nodeID)

	produced := map[string][]Row{}
	for _, nid := range chain {
		var node PreviewNodeView
		for _, n := range nodes {
			if n.ID() == nid {
				node = n
				break
			}
		}
		if node == nil {
			return nil, &PreviewError{Kind: PreviewErrNodeNotFound, NodeID: nid}
		}
		var rows []Row
		if len(node.DependsOn()) == 0 {
			loaded, lErr := loader.Load(node, cap)
			if lErr != nil {
				return nil, &PreviewError{
					Kind:    PreviewErrSeedLoader,
					NodeID:  nid,
					Message: lErr.Error(),
				}
			}
			rows = loaded
		} else {
			tRows, tErr := applyTransform(node, produced, cap)
			if tErr != nil {
				return nil, tErr
			}
			rows = tRows
		}
		produced[nid] = rows
	}

	finalRows, ok := produced[nodeID]
	if !ok {
		return nil, &PreviewError{Kind: PreviewErrNodeNotFound, NodeID: nodeID}
	}
	delete(produced, nodeID)
	columns := deriveColumns(finalRows)

	return &PreviewOutput{
		PipelineID:  pipelineID,
		NodeID:      nodeID,
		SampleSize:  len(finalRows),
		Columns:     columns,
		Rows:        rowsToMaps(finalRows),
		GeneratedAt: time.Now().UTC(),
		Seed:        seed,
		SourceChain: chain,
		Fresh:       true,
	}, nil
}

func nodesContain(nodes []PreviewNodeView, id string) bool {
	for _, n := range nodes {
		if n.ID() == id {
			return true
		}
	}
	return false
}

func topologicalChain(target string, nodes []PreviewNodeView) ([]string, error) {
	byID := map[string]PreviewNodeView{}
	for _, n := range nodes {
		byID[n.ID()] = n
	}
	if _, ok := byID[target]; !ok {
		return nil, &PreviewError{Kind: PreviewErrNodeNotFound, NodeID: target}
	}

	needed := map[string]struct{}{}
	queue := []string{target}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if _, seen := needed[id]; seen {
			continue
		}
		needed[id] = struct{}{}
		if node, ok := byID[id]; ok {
			for _, dep := range node.DependsOn() {
				queue = append(queue, dep)
			}
		}
	}

	indeg := map[string]int{}
	adj := map[string][]string{}
	for id := range needed {
		indeg[id] = 0
		adj[id] = []string{}
	}
	for id := range needed {
		if node, ok := byID[id]; ok {
			for _, dep := range node.DependsOn() {
				if _, ok := needed[dep]; !ok {
					continue
				}
				indeg[id]++
				adj[dep] = append(adj[dep], id)
			}
		}
	}

	var frontier []string
	for k, d := range indeg {
		if d == 0 {
			frontier = append(frontier, k)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(frontier)))

	var order []string
	for len(frontier) > 0 {
		id := frontier[len(frontier)-1]
		frontier = frontier[:len(frontier)-1]
		order = append(order, id)
		children, ok := adj[id]
		if ok {
			delete(adj, id)
			var next []string
			for _, child := range children {
				if d, ok := indeg[child]; ok {
					indeg[child] = d - 1
					if d-1 == 0 {
						next = append(next, child)
					}
				}
			}
			sort.Strings(next)
			for i := len(next) - 1; i >= 0; i-- {
				frontier = append(frontier, next[i])
			}
		}
	}
	if len(order) != len(needed) {
		return nil, &PreviewError{Kind: PreviewErrCycle, NodeID: target}
	}
	return order, nil
}

func applyTransform(node PreviewNodeView, produced map[string][]Row, cap int) ([]Row, error) {
	kind := strings.ToLower(node.TransformType())
	var upstream [][]Row
	for _, d := range node.DependsOn() {
		if rows, ok := produced[d]; ok {
			upstream = append(upstream, rows)
		}
	}
	if len(upstream) == 0 {
		return nil, nil
	}
	primary := upstream[0]

	switch kind {
	case "passthrough":
		return takeRows(primary, cap), nil
	case "cast", "title_case", "clean_string":
		return applyColumnsTransform(node, primary, cap, kind)
	case "filter":
		return applyFilter(node, primary, cap)
	case "join":
		if len(upstream) < 2 {
			return nil, &PreviewError{
				Kind:      PreviewErrTransform,
				NodeID:    node.ID(),
				Transform: kind,
				Message:   "join requires two upstream tables",
			}
		}
		return applyJoin(node, primary, upstream[1], cap)
	case "union":
		merged := []Row{}
		for _, table := range upstream {
			for _, row := range table {
				if len(merged) >= cap {
					return merged, nil
				}
				merged = append(merged, cloneRow(row))
			}
		}
		return merged, nil
	}
	return takeRows(primary, cap), nil
}

func applyColumnsTransform(node PreviewNodeView, primary []Row, cap int, kind string) ([]Row, error) {
	cfg := node.Config()
	colsRaw := rawField(cfg, "columns")
	var columns []string
	if len(colsRaw) > 0 {
		var arr []json.RawMessage
		if err := json.Unmarshal(colsRaw, &arr); err == nil {
			for _, item := range arr {
				var s string
				if err := json.Unmarshal(item, &s); err == nil {
					columns = append(columns, s)
				}
			}
		}
	}

	target := stringField(cfg, "cast_target")
	if target == "" {
		target = "STRING"
	}

	out := make([]Row, 0, minInt(len(primary), cap))
	limit := minInt(len(primary), cap)
	for i := 0; i < limit; i++ {
		row := primary[i]
		next := cloneRow(row)
		for _, col := range columns {
			value, ok := next[col]
			if !ok {
				continue
			}
			evaled := EvalValueFromJSON(value)
			var transformed EvalValue
			switch kind {
			case "title_case":
				if evaled.Kind == EvalKindString {
					transformed = EvalString(toTitleCase(evaled.Str))
				} else {
					transformed = evaled
				}
			case "clean_string":
				if evaled.Kind == EvalKindString {
					transformed = EvalString(cleanString(evaled.Str))
				} else {
					transformed = evaled
				}
			case "cast":
				casted, err := castHelper(evaled, target)
				if err != nil {
					return nil, &PreviewError{
						Kind:      PreviewErrTransform,
						NodeID:    node.ID(),
						Transform: kind,
						Message:   err.Error(),
					}
				}
				transformed = casted
			default:
				transformed = evaled
			}
			next[col] = transformed.ToJSON()
		}
		out = append(out, next)
	}
	return out, nil
}

func applyFilter(node PreviewNodeView, primary []Row, cap int) ([]Row, error) {
	predicate := stringField(node.Config(), "predicate")
	if predicate == "" {
		return nil, &PreviewError{
			Kind:      PreviewErrTransform,
			NodeID:    node.ID(),
			Transform: "filter",
			Message:   "missing `predicate`",
		}
	}
	parsed, err := ParseExpr(predicate)
	if err != nil {
		return nil, &PreviewError{
			Kind:      PreviewErrTransform,
			NodeID:    node.ID(),
			Transform: "filter",
			Message:   fmt.Sprintf("predicate parse error: %s", err.Error()),
		}
	}
	var out []Row
	limit := minInt(len(primary), cap)
	for i := 0; i < limit; i++ {
		row := primary[i]
		result, err := Eval(parsed, row)
		if err != nil {
			return nil, &PreviewError{
				Kind:      PreviewErrTransform,
				NodeID:    node.ID(),
				Transform: "filter",
				Message:   err.Error(),
			}
		}
		if result.Kind == EvalKindBool && result.Bool {
			out = append(out, cloneRow(row))
		}
	}
	return out, nil
}

func applyJoin(node PreviewNodeView, left, right []Row, cap int) ([]Row, error) {
	cfg := node.Config()
	how := strings.ToLower(stringField(cfg, "how"))
	if how == "" {
		how = "inner"
	}
	onRaw := rawField(cfg, "on")
	var on []string
	if len(onRaw) > 0 {
		var arr []json.RawMessage
		if err := json.Unmarshal(onRaw, &arr); err == nil {
			for _, item := range arr {
				var s string
				if err := json.Unmarshal(item, &s); err == nil {
					on = append(on, s)
				}
			}
		}
	}
	if len(on) == 0 {
		return nil, &PreviewError{
			Kind:      PreviewErrTransform,
			NodeID:    node.ID(),
			Transform: "join",
			Message:   "missing `on` keys",
		}
	}

	index := map[string][]Row{}
	for _, row := range right {
		key := joinKey(row, on)
		index[key] = append(index[key], row)
	}

	var out []Row
	for _, leftRow := range left {
		key := joinKey(leftRow, on)
		if matches, ok := index[key]; ok {
			for _, rightRow := range matches {
				if len(out) >= cap {
					return out, nil
				}
				merged := cloneRow(leftRow)
				for k, v := range rightRow {
					if _, exists := merged[k]; !exists {
						merged[k] = append(json.RawMessage(nil), v...)
					}
				}
				out = append(out, merged)
			}
		} else if how == "left" {
			if len(out) >= cap {
				return out, nil
			}
			out = append(out, cloneRow(leftRow))
		}
	}
	return out, nil
}

func takeRows(rows []Row, cap int) []Row {
	limit := minInt(len(rows), cap)
	out := make([]Row, limit)
	for i := 0; i < limit; i++ {
		out[i] = cloneRow(rows[i])
	}
	return out
}

func deriveColumns(rows []Row) []string {
	if len(rows) == 0 {
		return []string{}
	}
	first := rows[0]
	keys := make([]string, 0, len(first))
	for k := range first {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func joinKey(row Row, keys []string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		v, ok := row[k]
		if !ok {
			parts[i] = "<null>"
			continue
		}
		// Match the Rust Value::to_string semantics — JSON-encode the
		// raw value (strings end up double-quoted, numbers / bools
		// / null bare).
		parts[i] = string(v)
	}
	return strings.Join(parts, "")
}

func computeSeed(pipelineID, nodeID string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(pipelineID))
	h.Write([]byte{0})
	h.Write([]byte(nodeID))
	return h.Sum64()
}

func synthesiseRows(seed uint64, nodeID string, cap int) []Row {
	state := seed
	if state == 0 {
		state = 0xdead_beef
	}
	out := make([]Row, 0, cap)
	for i := 0; i < cap; i++ {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		row := Row{}
		row["id"] = json.RawMessage(fmt.Sprintf("%d", i))
		row["source_node"] = mustMarshal(nodeID)
		row["synthetic"] = json.RawMessage("true")
		row["value"] = json.RawMessage(fmt.Sprintf("%d", int64(state%1000)))
		out = append(out, row)
	}
	return out
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func cleanString(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func castHelper(v EvalValue, target string) (EvalValue, error) {
	upper := strings.ToUpper(strings.TrimSpace(target))
	switch upper {
	case "STRING":
		switch v.Kind {
		case EvalKindString:
			return v, nil
		case EvalKindInteger:
			return EvalString(fmt.Sprintf("%d", v.Int)), nil
		case EvalKindDouble:
			return EvalString(fmt.Sprintf("%g", v.Double)), nil
		case EvalKindBool:
			if v.Bool {
				return EvalString("true"), nil
			}
			return EvalString("false"), nil
		}
	case "INTEGER", "LONG":
		switch v.Kind {
		case EvalKindInteger:
			return v, nil
		case EvalKindDouble:
			return EvalInt(int64(v.Double)), nil
		case EvalKindString:
			i, err := parseInt64(v.Str)
			if err != nil {
				return EvalValue{}, fmt.Errorf("cannot cast '%s' to %s", v.Str, upper)
			}
			return EvalInt(i), nil
		}
	case "DOUBLE", "DECIMAL":
		switch v.Kind {
		case EvalKindInteger:
			return EvalDouble(float64(v.Int)), nil
		case EvalKindDouble:
			return v, nil
		case EvalKindString:
			d, err := parseFloat64(v.Str)
			if err != nil {
				return EvalValue{}, fmt.Errorf("cannot cast '%s' to %s", v.Str, upper)
			}
			return EvalDouble(d), nil
		}
	case "BOOLEAN":
		if v.Kind == EvalKindBool {
			return v, nil
		}
	}
	if v.Kind == EvalKindNull {
		return EvalNull(), nil
	}
	return EvalValue{}, fmt.Errorf("cannot cast %s to %s", typeHintDebug(v.TypeHint()), upper)
}

func parseInt64(s string) (int64, error) {
	var i int64
	_, err := fmt.Sscan(s, &i)
	return i, err
}

func parseFloat64(s string) (float64, error) {
	var d float64
	_, err := fmt.Sscan(s, &d)
	return d, err
}

func cloneRow(row Row) Row {
	out := make(Row, len(row))
	for k, v := range row {
		clone := append(json.RawMessage(nil), v...)
		out[k] = clone
	}
	return out
}

func rowsToMaps(rows []Row) []map[string]json.RawMessage {
	out := make([]map[string]json.RawMessage, len(rows))
	for i, r := range rows {
		out[i] = map[string]json.RawMessage(r)
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// JSONPipelineNode is a convenience implementation of [PreviewNodeView]
// used by tests and callers that already have a JSON `Value` per node.
type JSONPipelineNode struct {
	NodeID            string          `json:"id"`
	NodeTransformType string          `json:"transform_type"`
	NodeConfig        json.RawMessage `json:"config"`
	NodeDependsOn     []string        `json:"depends_on"`
}

// ID implements [PreviewNodeView].
func (n JSONPipelineNode) ID() string { return n.NodeID }

// TransformType implements [PreviewNodeView].
func (n JSONPipelineNode) TransformType() string { return n.NodeTransformType }

// Config implements [PreviewNodeView].
func (n JSONPipelineNode) Config() json.RawMessage { return n.NodeConfig }

// DependsOn implements [PreviewNodeView].
func (n JSONPipelineNode) DependsOn() []string { return n.NodeDependsOn }

// NewJSONPipelineNode builds a [JSONPipelineNode].
func NewJSONPipelineNode(id, transformType string, config json.RawMessage, dependsOn []string) JSONPipelineNode {
	deps := make([]string, len(dependsOn))
	copy(deps, dependsOn)
	return JSONPipelineNode{
		NodeID:            id,
		NodeTransformType: transformType,
		NodeConfig:        config,
		NodeDependsOn:     deps,
	}
}
