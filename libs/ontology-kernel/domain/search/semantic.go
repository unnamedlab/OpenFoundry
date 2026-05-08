// Embedding-based semantic scorer for ontology search.
//
// Two backends:
//
//   - Deterministic hash — a stable per-token folding hash that
//     produces a 16-dimensional unit vector; reproducible across
//     processes without any IO. Used as the default and as the
//     fallback whenever a remote provider fails.
//   - Provider — an HTTP-backed embedding API (OpenAI-compatible
//     `/embeddings` endpoint or Ollama-style `/embeddings` with a
//     single `embedding` field). Resolved against the
//     `ai_providers` PG table by `provider:<uuid>` reference.
//
// Mirrors `libs/ontology-kernel/src/domain/search/semantic.rs`.

package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EmbeddingBackendKind tags the [EmbeddingBackend] variants. Mirrors
// the Rust `enum EmbeddingBackend`.
type EmbeddingBackendKind int

const (
	BackendDeterministicHash EmbeddingBackendKind = iota
	BackendProvider
)

// EmbeddingBackend mirrors `enum EmbeddingBackend`. The provider
// variant carries the resolved shape; the deterministic variant has
// none.
type EmbeddingBackend struct {
	Kind     EmbeddingBackendKind
	Provider EmbeddingProvider
}

// EmbeddingProvider mirrors `struct EmbeddingProvider`.
type EmbeddingProvider struct {
	Reference           string
	ModelName           string
	EndpointURL         string
	APIMode             string
	CredentialReference *string
}

// embeddingProviderRow mirrors the private Rust row struct.
type embeddingProviderRow struct {
	ID                  uuid.UUID
	ModelName           string
	EndpointURL         string
	APIMode             string
	CredentialReference *string
	Enabled             bool
}

// remoteEmbeddingCache backs the `OnceLock<Mutex<HashMap<...>>>` in
// the Rust source. Process-wide; entries are keyed on
// `provider_reference + content_hash` so different providers don't
// share results.
var remoteEmbeddingCache = &sync.Map{}

func cacheKey(providerReference, content string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(content))
	return fmt.Sprintf("%s:%d", providerReference, h.Sum64())
}

// normalizedProviderReference mirrors `fn normalized_provider_reference`.
// Caller-provided trumps the configured default; both get trimmed.
func normalizedProviderReference(requested *string, configured string) string {
	if requested != nil {
		v := strings.TrimSpace(*requested)
		if v != "" {
			return v
		}
	}
	return strings.TrimSpace(configured)
}

// ResolveBackend mirrors `pub async fn resolve_backend`. Falls back
// to the deterministic backend whenever the requested provider
// reference is empty, equal to "deterministic-hash", malformed, not
// found, or disabled. Logs a warning on each fallback (plain stderr
// — log/slog when available; the Rust source uses `tracing::warn!`).
func ResolveBackend(ctx context.Context, db *pgxpool.Pool, configured string, requested *string) (EmbeddingBackend, error) {
	reference := normalizedProviderReference(requested, configured)
	if reference == "" || reference == "deterministic-hash" {
		return EmbeddingBackend{Kind: BackendDeterministicHash}, nil
	}

	rest, ok := strings.CutPrefix(reference, "provider:")
	if !ok {
		warnf("unknown ontology search embedding provider reference %q, falling back to deterministic hash", reference)
		return EmbeddingBackend{Kind: BackendDeterministicHash}, nil
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		warnf("malformed embedding provider reference %q, falling back to deterministic hash", reference)
		return EmbeddingBackend{Kind: BackendDeterministicHash}, nil
	}
	if db == nil {
		warnf("no PG pool available to resolve embedding provider %q, falling back to deterministic hash", reference)
		return EmbeddingBackend{Kind: BackendDeterministicHash}, nil
	}

	var row embeddingProviderRow
	err = db.QueryRow(ctx,
		`SELECT id, model_name, endpoint_url, api_mode, credential_reference, enabled
           FROM ai_providers
           WHERE id = $1`,
		id,
	).Scan(&row.ID, &row.ModelName, &row.EndpointURL, &row.APIMode, &row.CredentialReference, &row.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		warnf("embedding provider %q not found, falling back to deterministic hash", reference)
		return EmbeddingBackend{Kind: BackendDeterministicHash}, nil
	}
	if err != nil {
		return EmbeddingBackend{}, err
	}
	if !row.Enabled {
		warnf("embedding provider %q disabled, falling back to deterministic hash", reference)
		return EmbeddingBackend{Kind: BackendDeterministicHash}, nil
	}

	return EmbeddingBackend{
		Kind: BackendProvider,
		Provider: EmbeddingProvider{
			Reference:           "provider:" + row.ID.String(),
			ModelName:           row.ModelName,
			EndpointURL:         row.EndpointURL,
			APIMode:             row.APIMode,
			CredentialReference: row.CredentialReference,
		},
	}, nil
}

// BackendReference mirrors `pub fn backend_reference`.
func BackendReference(backend EmbeddingBackend) string {
	if backend.Kind == BackendDeterministicHash {
		return "deterministic-hash"
	}
	return backend.Provider.Reference
}

// EmbedText mirrors `pub fn embed_text` — a deterministic 16-dim
// vector built by folding lower-cased whitespace tokens. The same
// content always produces the same vector across processes.
func EmbedText(content string) []float32 {
	const dim = 16
	vec := make([]float32, dim)
	tokens := strings.Fields(strings.ToLower(content))
	for i, token := range tokens {
		if token == "" {
			continue
		}
		var sum uint32
		for _, b := range []byte(token) {
			sum += uint32(b) // wrapping is implicit in uint32 arithmetic
		}
		vec[i%dim] += float32(sum%997) / 997.0
	}
	return normalizeEmbedding(vec)
}

// CosineSimilarity mirrors `pub fn cosine_similarity`. Empty or
// length-mismatched inputs return 0; result is clamped to [-1, 1].
func CosineSimilarity(left, right []float32) float32 {
	if len(left) != len(right) || len(left) == 0 {
		return 0
	}
	var sum float32
	for i := range left {
		sum += left[i] * right[i]
	}
	if sum < -1 {
		return -1
	}
	if sum > 1 {
		return 1
	}
	return sum
}

// SemanticScore mirrors `pub fn score` from `semantic.rs` —
// heuristic deterministic-hash-only path used for the lexical
// pre-pass. Renamed to avoid the collision with [LexicalScore]
// from fulltext.go (Go has no module scoping inside a package).
func SemanticScore(query, text string) float32 {
	q := EmbedText(query)
	t := EmbedText(text)
	return CosineSimilarity(q, t)
}

// EmbedWithBackend mirrors `pub async fn embed_with_backend`.
// Routes through the deterministic hash when [BackendDeterministicHash]
// is selected; otherwise hits the cached HTTP backend.
func EmbedWithBackend(ctx context.Context, client *http.Client, backend EmbeddingBackend, content string) ([]float32, error) {
	switch backend.Kind {
	case BackendDeterministicHash:
		return EmbedText(content), nil
	case BackendProvider:
		return embedRemoteWithCache(ctx, client, backend.Provider, content)
	default:
		return nil, fmt.Errorf("unknown embedding backend kind %d", backend.Kind)
	}
}

// ScoreWithQueryEmbedding mirrors `pub async fn score_with_query_embedding`.
// Pre-computed query embedding + per-document text → cosine similarity.
func ScoreWithQueryEmbedding(ctx context.Context, client *http.Client, backend EmbeddingBackend, queryEmbedding []float32, text string) (float32, error) {
	textEmbedding, err := EmbedWithBackend(ctx, client, backend, text)
	if err != nil {
		return 0, err
	}
	return CosineSimilarity(queryEmbedding, textEmbedding), nil
}

func normalizeEmbedding(vec []float32) []float32 {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum <= 0 {
		return vec
	}
	mag := float32(math.Sqrt(sum))
	for i := range vec {
		vec[i] /= mag
	}
	return vec
}

// ---- Remote backend --------------------------------------------------------

func embedRemoteWithCache(ctx context.Context, client *http.Client, provider EmbeddingProvider, content string) ([]float32, error) {
	if strings.TrimSpace(content) == "" {
		return []float32{}, nil
	}
	key := cacheKey(provider.Reference, content)
	if cached, ok := remoteEmbeddingCache.Load(key); ok {
		out := make([]float32, len(cached.([]float32)))
		copy(out, cached.([]float32))
		return out, nil
	}

	var (
		embedding []float32
		err       error
	)
	switch provider.APIMode {
	case "chat_completions":
		embedding, err = embedOpenAICompatible(ctx, client, provider, content)
	case "chat":
		embedding, err = embedOllama(ctx, client, provider, content)
	default:
		return nil, fmt.Errorf("embedding provider api_mode '%s' does not support ontology search embeddings", provider.APIMode)
	}
	if err != nil {
		return nil, err
	}
	remoteEmbeddingCache.Store(key, embedding)
	return embedding, nil
}

func providerToken(provider EmbeddingProvider) string {
	if provider.CredentialReference == nil {
		return ""
	}
	v := os.Getenv(*provider.CredentialReference)
	if strings.TrimSpace(v) == "" {
		return ""
	}
	return v
}

func endpoint(base, suffix string) string {
	if strings.HasSuffix(base, suffix) {
		return base
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(suffix, "/")
}

func embedOpenAICompatible(ctx context.Context, client *http.Client, provider EmbeddingProvider, content string) ([]float32, error) {
	body, _ := json.Marshal(map[string]any{
		"model": provider.ModelName,
		"input": content,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(provider.EndpointURL, "/embeddings"), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %s", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := providerToken(provider); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := doHTTP(client, req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %s", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding provider returned %d %s: %s", resp.StatusCode, resp.Status, string(raw))
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("embedding response parse failed: %s", err)
	}
	return parseOpenAIEmbedding(payload)
}

func embedOllama(ctx context.Context, client *http.Client, provider EmbeddingProvider, content string) ([]float32, error) {
	body, _ := json.Marshal(map[string]any{
		"model":  provider.ModelName,
		"prompt": content,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(provider.EndpointURL, "/embeddings"), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %s", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := doHTTP(client, req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %s", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding provider returned %d %s: %s", resp.StatusCode, resp.Status, string(raw))
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("embedding response parse failed: %s", err)
	}
	arr, ok := payload["embedding"].([]any)
	if !ok || len(arr) == 0 {
		return nil, fmt.Errorf("embedding payload did not include an embedding vector")
	}
	out, _ := valueArrayToFloat32(arr)
	if len(out) == 0 {
		return nil, fmt.Errorf("embedding payload did not include an embedding vector")
	}
	return out, nil
}

func parseOpenAIEmbedding(payload map[string]any) ([]float32, error) {
	data, ok := payload["data"].([]any)
	if !ok || len(data) == 0 {
		return nil, fmt.Errorf("embedding payload did not include an embedding vector")
	}
	first, ok := data[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("embedding payload did not include an embedding vector")
	}
	arr, ok := first["embedding"].([]any)
	if !ok || len(arr) == 0 {
		return nil, fmt.Errorf("embedding payload did not include an embedding vector")
	}
	out, _ := valueArrayToFloat32(arr)
	if len(out) == 0 {
		// Rust `value_array_to_f32` returns `Vec::new()` on the first
		// non-numeric entry; the surrounding `.filter(|emb| !emb.is_empty())`
		// then surfaces the same payload-shape error.
		return nil, fmt.Errorf("embedding payload did not include an embedding vector")
	}
	return out, nil
}

// valueArrayToFloat32 mirrors Rust `fn value_array_to_f32`: on the
// FIRST non-numeric entry it silently returns an empty slice (and
// nil error). The caller's `len(out) == 0` check then surfaces the
// canonical "embedding payload did not include an embedding vector"
// error, byte-identical to Rust. Returning a typed error here would
// drift the user-visible message.
func valueArrayToFloat32(values []any) ([]float32, error) {
	out := make([]float32, 0, len(values))
	for _, v := range values {
		f, ok := v.(float64)
		if !ok {
			return []float32{}, nil
		}
		out = append(out, float32(f))
	}
	return normalizeEmbedding(out), nil
}

// doHTTP allows tests to short-circuit by passing a nil client; we
// fall back to http.DefaultClient when nil.
func doHTTP(client *http.Client, req *http.Request) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(req)
}

// warnf logs a fallback warning. Stderr is the closest neutral
// equivalent to Rust's `tracing::warn!` without forcing a slog
// configuration on every caller.
func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[ontology-kernel/search] "+format+"\n", args...)
}
