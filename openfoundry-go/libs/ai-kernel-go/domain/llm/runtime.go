package llm

import (
	"context"
	"math"
	"strings"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

// EmbedText is the embedding entrypoint the handlers use when the
// configured embedding_provider resolves to a real LlmProvider row.
//
// PLACEHOLDER: the full Rust runtime (581 LOC, libs/ai-kernel/src/
// domain/llm/runtime.rs) implements per-provider HTTP request/response
// shapes, retries and credential injection. Until that port lands this
// shim returns the deterministic 12-dim embedding (same algorithm as
// rag.EmbedText) so the surrounding handler code path is exercised
// end-to-end and the JSON output stays wire-compatible (only the
// embedding *values* differ from the live runtime).
//
// We intentionally inline the algorithm here rather than importing
// libs/ai-kernel-go/domain/rag — rag/retriever.go already imports
// llm for cosine similarity, so importing rag here would cycle.
// When the full runtime ships, this signature stays identical.
func EmbedText(_ context.Context, _ *models.LlmProvider, text string) ([]float32, error) {
	return offlineEmbedding(text), nil
}

func offlineEmbedding(content string) []float32 {
	vector := make([]float32, 12)
	vectorLen := len(vector)
	idx := 0
	for _, token := range strings.Fields(strings.ToLower(content)) {
		if token == "" {
			continue
		}
		var tokenValue uint32
		for _, b := range []byte(token) {
			tokenValue += uint32(b)
		}
		vector[idx%vectorLen] += float32(tokenValue%997) / 997.0
		idx++
	}
	var sumSq float32
	for _, v := range vector {
		sumSq += v * v
	}
	magnitude := float32(math.Sqrt(float64(sumSq)))
	if magnitude > 0 {
		for i := range vector {
			vector[i] /= magnitude
		}
	}
	return vector
}

// CompleteText mirrors the Rust runtime::complete_text entrypoint used
// by the chat / agents executors. PLACEHOLDER until the full runtime
// port lands; returns a deterministic stub so callers can be wired
// without HTTP. The real signature returns (text, prompt_tokens,
// completion_tokens, finish_reason).
func CompleteText(_ context.Context, _ *models.LlmProvider, prompt string) (text string, promptTokens int32, completionTokens int32, finishReason string, err error) {
	tokens := EstimateTokens(prompt)
	return "", tokens, 0, "stop", nil
}
