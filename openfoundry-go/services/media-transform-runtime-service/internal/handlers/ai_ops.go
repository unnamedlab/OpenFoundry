package handlers

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/rag"
)

const aiOutputMimeType = "application/json"

// AIProvider is the fakeable adapter behind the AI-backed catalog
// entries. The default implementation uses ai-kernel-go's deterministic
// local primitives so dev/test deployments do not need provider secrets;
// production callers can swap this in tests or a future wiring layer
// without changing the media-transform wire contract.
type AIProvider interface {
	Embed(input AIRequest) (any, error)
	Transcribe(input AIRequest) (any, error)
	ExtractLayout(input AIRequest) (any, error)
	VLMExtract(input AIRequest) (any, error)
}

// AIRequest carries the runtime envelope plus decoded bytes into the
// provider adapter. Params stays raw so providers can evolve fields
// without forcing the runtime contract to rev.
type AIRequest struct {
	Kind     string
	MimeType string
	Params   json.RawMessage
	Bytes    []byte
}

var aiProvider AIProvider = deterministicAIProvider{}

// SetAIProviderForTest swaps the AI adapter and returns a restore
// function. It is intentionally test-scoped by name; production wiring
// should prefer a constructor once an external provider is introduced.
func SetAIProviderForTest(p AIProvider) func() {
	prev := aiProvider
	aiProvider = p
	return func() { aiProvider = prev }
}

func Embedding(mime string, params json.RawMessage, src []byte) (HandlerOutput, error) {
	out, err := aiProvider.Embed(AIRequest{Kind: "embedding", MimeType: mime, Params: params, Bytes: src})
	if err != nil {
		return HandlerOutput{}, err
	}
	return HandlerOutput{OutputMimeType: aiOutputMimeType, OutputJSON: out}, nil
}

func Transcription(mime string, params json.RawMessage, src []byte) (HandlerOutput, error) {
	out, err := aiProvider.Transcribe(AIRequest{Kind: "transcription", MimeType: mime, Params: params, Bytes: src})
	if err != nil {
		return HandlerOutput{}, err
	}
	return HandlerOutput{OutputMimeType: aiOutputMimeType, OutputJSON: out}, nil
}

func LayoutAwareV2(mime string, params json.RawMessage, src []byte) (HandlerOutput, error) {
	out, err := aiProvider.ExtractLayout(AIRequest{Kind: "layout_aware_v2", MimeType: mime, Params: params, Bytes: src})
	if err != nil {
		return HandlerOutput{}, err
	}
	return HandlerOutput{OutputMimeType: aiOutputMimeType, OutputJSON: out}, nil
}

func VLMExtract(mime string, params json.RawMessage, src []byte) (HandlerOutput, error) {
	out, err := aiProvider.VLMExtract(AIRequest{Kind: "vlm_extract", MimeType: mime, Params: params, Bytes: src})
	if err != nil {
		return HandlerOutput{}, err
	}
	return HandlerOutput{OutputMimeType: aiOutputMimeType, OutputJSON: out}, nil
}

type deterministicAIProvider struct{}

type embeddingOutput struct {
	Embedding  []float32 `json:"embedding"`
	Dimensions int       `json:"dimensions"`
	Model      string    `json:"model"`
	Provider   string    `json:"provider"`
}

type transcriptionOutput struct {
	Text     string                 `json:"text"`
	Language string                 `json:"language,omitempty"`
	Segments []transcriptionSegment `json:"segments"`
	Model    string                 `json:"model"`
	Provider string                 `json:"provider"`
}

type transcriptionSegment struct {
	StartSeconds float64 `json:"start_seconds"`
	EndSeconds   float64 `json:"end_seconds"`
	Text         string  `json:"text"`
}

type layoutOutput struct {
	Text     string       `json:"text"`
	Pages    []layoutPage `json:"pages"`
	Model    string       `json:"model"`
	Provider string       `json:"provider"`
}

type layoutPage struct {
	PageNumber int           `json:"page_number"`
	Blocks     []layoutBlock `json:"blocks"`
}

type layoutBlock struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type vlmOutput struct {
	Text     string         `json:"text"`
	Prompt   string         `json:"prompt,omitempty"`
	Fields   map[string]any `json:"fields,omitempty"`
	Model    string         `json:"model"`
	Provider string         `json:"provider"`
}

func (deterministicAIProvider) Embed(input AIRequest) (any, error) {
	text := textForAI(input.Bytes)
	vec := rag.EmbedText(text)
	return embeddingOutput{
		Embedding:  vec,
		Dimensions: len(vec),
		Model:      "deterministic-dev-embedder",
		Provider:   "ai-kernel-go/rag",
	}, nil
}

func (deterministicAIProvider) Transcribe(input AIRequest) (any, error) {
	var p struct {
		Language string `json:"language,omitempty"`
	}
	if len(input.Params) > 0 {
		if err := json.Unmarshal(input.Params, &p); err != nil {
			return nil, invalidParams("transcription", err.Error())
		}
	}
	text := strings.TrimSpace(textForAI(input.Bytes))
	segments := []transcriptionSegment{}
	if text != "" {
		segments = append(segments, transcriptionSegment{StartSeconds: 0, EndSeconds: estimatedAudioSeconds(len(input.Bytes)), Text: text})
	}
	return transcriptionOutput{
		Text:     text,
		Language: p.Language,
		Segments: segments,
		Model:    "deterministic-dev-transcriber",
		Provider: "ai-kernel-go/local",
	}, nil
}

func (deterministicAIProvider) ExtractLayout(input AIRequest) (any, error) {
	text := strings.TrimSpace(textForAI(input.Bytes))
	blocks := []layoutBlock{}
	for _, para := range splitParagraphs(text) {
		blocks = append(blocks, layoutBlock{Kind: "paragraph", Text: para})
	}
	return layoutOutput{
		Text:     text,
		Pages:    []layoutPage{{PageNumber: 1, Blocks: blocks}},
		Model:    "deterministic-dev-layout-aware-v2",
		Provider: "ai-kernel-go/local",
	}, nil
}

func (deterministicAIProvider) VLMExtract(input AIRequest) (any, error) {
	var p struct {
		Prompt string         `json:"prompt,omitempty"`
		Fields map[string]any `json:"fields,omitempty"`
	}
	if len(input.Params) > 0 {
		if err := json.Unmarshal(input.Params, &p); err != nil {
			return nil, invalidParams("vlm_extract", err.Error())
		}
	}
	text := strings.TrimSpace(textForAI(input.Bytes))
	if p.Prompt != "" && text != "" {
		text = p.Prompt + "\n\n" + text
	}
	return vlmOutput{
		Text:     text,
		Prompt:   p.Prompt,
		Fields:   p.Fields,
		Model:    "deterministic-dev-vlm-extract",
		Provider: "ai-kernel-go/local",
	}, nil
}

func textForAI(src []byte) string {
	if utf8.Valid(src) {
		return string(src)
	}
	return ""
}

func splitParagraphs(text string) []string {
	parts := strings.Split(text, "\n\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func estimatedAudioSeconds(byteLen int) float64 {
	if byteLen == 0 {
		return 0
	}
	return float64(byteLen) / 16000.0
}
