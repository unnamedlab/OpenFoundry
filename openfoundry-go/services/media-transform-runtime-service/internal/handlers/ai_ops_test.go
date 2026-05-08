package handlers

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAIProvider struct {
	req AIRequest
	err error
}

func (f *fakeAIProvider) Embed(input AIRequest) (any, error) {
	f.req = input
	return map[string]any{"embedding": []float32{0.25, 0.75}}, f.err
}
func (f *fakeAIProvider) Transcribe(input AIRequest) (any, error) {
	f.req = input
	return map[string]any{"text": "hello"}, f.err
}
func (f *fakeAIProvider) ExtractLayout(input AIRequest) (any, error) {
	f.req = input
	return map[string]any{"pages": []any{}}, f.err
}
func (f *fakeAIProvider) VLMExtract(input AIRequest) (any, error) {
	f.req = input
	return map[string]any{"text": "answer"}, f.err
}

func TestAIHandlersUseFakeableProvider(t *testing.T) {
	fake := &fakeAIProvider{}
	restore := SetAIProviderForTest(fake)
	t.Cleanup(restore)

	out, err := Dispatch("embedding", "image/png", json.RawMessage(`{"model":"test"}`), []byte("image bytes"))
	require.NoError(t, err)
	assert.Equal(t, "application/json", out.OutputMimeType)
	assert.Equal(t, map[string]any{"embedding": []float32{0.25, 0.75}}, out.OutputJSON)
	assert.Equal(t, "embedding", fake.req.Kind)
	assert.Equal(t, "image/png", fake.req.MimeType)
	assert.JSONEq(t, `{"model":"test"}`, string(fake.req.Params))
	assert.Equal(t, []byte("image bytes"), fake.req.Bytes)
}

func TestAIHandlerProviderErrorsPropagate(t *testing.T) {
	boom := errors.New("provider unavailable")
	fake := &fakeAIProvider{err: boom}
	restore := SetAIProviderForTest(fake)
	t.Cleanup(restore)

	_, err := Dispatch("vlm_extract", "application/pdf", nil, []byte("doc"))
	require.ErrorIs(t, err, boom)
}

func TestDefaultAIProviderOutputsCatalogShapes(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want string
	}{
		{kind: "embedding", want: "embedding"},
		{kind: "transcription", want: "text"},
		{kind: "layout_aware_v2", want: "pages"},
		{kind: "vlm_extract", want: "text"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			out, err := Dispatch(tc.kind, "text/plain", json.RawMessage(`{"prompt":"summarize"}`), []byte("hello world"))
			require.NoError(t, err)
			assert.Equal(t, "application/json", out.OutputMimeType)
			b, err := json.Marshal(out.OutputJSON)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(b, &payload))
			assert.Contains(t, payload, tc.want)
			assert.Contains(t, payload, "provider")
		})
	}
}
