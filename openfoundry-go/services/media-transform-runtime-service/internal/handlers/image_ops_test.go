package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makePNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func decodePNG(t *testing.T, src []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(src))
	require.NoError(t, err)
	return img
}

func TestThumbnailStaysInsideMaxDim(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 512, 256, color.RGBA{A: 255})
	out, err := Thumbnail("image/png", json.RawMessage(`{"max_dim": 64}`), src)
	require.NoError(t, err)
	require.NotNil(t, out.OutputBytes)
	img := decodePNG(t, out.OutputBytes)
	assert.LessOrEqual(t, img.Bounds().Dx(), 64)
	assert.LessOrEqual(t, img.Bounds().Dy(), 64)
}

func TestRotate90SwapsAxes(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 4, 2, color.RGBA{A: 255})
	out, err := Rotate("image/png", json.RawMessage(`{"degrees": 90}`), src)
	require.NoError(t, err)
	img := decodePNG(t, out.OutputBytes)
	assert.Equal(t, 2, img.Bounds().Dx())
	assert.Equal(t, 4, img.Bounds().Dy())
}

func TestRotate180PreservesDimensions(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 4, 2, color.RGBA{A: 255})
	out, err := Rotate("image/png", json.RawMessage(`{"degrees": 180}`), src)
	require.NoError(t, err)
	img := decodePNG(t, out.OutputBytes)
	assert.Equal(t, 4, img.Bounds().Dx())
	assert.Equal(t, 2, img.Bounds().Dy())
}

func TestRotate270SwapsAxes(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 4, 2, color.RGBA{A: 255})
	out, err := Rotate("image/png", json.RawMessage(`{"degrees": 270}`), src)
	require.NoError(t, err)
	img := decodePNG(t, out.OutputBytes)
	assert.Equal(t, 2, img.Bounds().Dx())
	assert.Equal(t, 4, img.Bounds().Dy())
}

func TestRotateNegativeNormalises(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 4, 2, color.RGBA{A: 255})
	// -90 ≡ 270 (mod 360); should swap axes.
	out, err := Rotate("image/png", json.RawMessage(`{"degrees": -90}`), src)
	require.NoError(t, err)
	img := decodePNG(t, out.OutputBytes)
	assert.Equal(t, 2, img.Bounds().Dx())
	assert.Equal(t, 4, img.Bounds().Dy())
}

func TestRotateArbitraryAngleRejected(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 4, 2, color.RGBA{A: 255})
	_, err := Rotate("image/png", json.RawMessage(`{"degrees": 45}`), src)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidParams))
}

func TestGrayscalePreservesDimensions(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 1, 1, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	out, err := Grayscale("image/png", src)
	require.NoError(t, err)
	img := decodePNG(t, out.OutputBytes)
	assert.Equal(t, 1, img.Bounds().Dx())
	assert.Equal(t, 1, img.Bounds().Dy())
}

func TestResizeExactDimensions(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 8, 8, color.RGBA{A: 255})
	out, err := Resize("image/png", json.RawMessage(`{"width": 2, "height": 4}`), src)
	require.NoError(t, err)
	img := decodePNG(t, out.OutputBytes)
	assert.Equal(t, 2, img.Bounds().Dx())
	assert.Equal(t, 4, img.Bounds().Dy())
}

func TestResizeWithinBoundingBoxPreservesAspect(t *testing.T) {
	t.Parallel()
	// 8×4 source; 4×4 bounding box ⇒ 4×2 (aspect ratio 2:1 preserved).
	src := makePNG(t, 8, 4, color.RGBA{A: 255})
	out, err := ResizeWithinBoundingBox("image/png", json.RawMessage(`{"width": 4, "height": 4}`), src)
	require.NoError(t, err)
	img := decodePNG(t, out.OutputBytes)
	assert.Equal(t, 4, img.Bounds().Dx())
	assert.Equal(t, 2, img.Bounds().Dy())
}

func TestResizeWithinBBoxRequiresWidthAndHeight(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 4, 4, color.RGBA{A: 255})
	_, err := ResizeWithinBoundingBox("image/png", json.RawMessage(`{"width": 4}`), src)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidParams))
	assert.Contains(t, err.Error(), "height is required")
}

func TestCropHonoursBounds(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 8, 8, color.RGBA{A: 255})
	out, err := Crop("image/png", json.RawMessage(`{"x": 1, "y": 1, "width": 4, "height": 3}`), src)
	require.NoError(t, err)
	img := decodePNG(t, out.OutputBytes)
	assert.Equal(t, 4, img.Bounds().Dx())
	assert.Equal(t, 3, img.Bounds().Dy())
}

func TestCropOutOfRangeRejected(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 4, 4, color.RGBA{A: 255})
	_, err := Crop("image/png", json.RawMessage(`{"x": 2, "y": 2, "width": 4, "height": 4}`), src)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidParams))
	assert.Contains(t, err.Error(), "exceeds image bounds")
}

func TestUnsupportedMimeRejected(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 4, 4, color.RGBA{A: 255})
	_, err := Grayscale("image/avif", src)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnsupportedMime))
}

func TestDispatchUnknownKindRejected(t *testing.T) {
	t.Parallel()
	_, err := Dispatch("not-a-kind", "image/png", nil, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidParams))
}

func TestWebPRoundTrip(t *testing.T) {
	t.Parallel()
	// Build a 4×4 RGBA, encode as nativewebp via the encoder we use,
	// then run grayscale → re-encode WebP and confirm it still decodes.
	src := makePNG(t, 4, 4, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	// Decode the PNG, re-encode as WebP via the production helper.
	srcImg, _, err := decodeImage(src, "image/png", "thumbnail")
	require.NoError(t, err)
	webpBytes, err := encodeImage(srcImg, formatWebP)
	require.NoError(t, err)

	out, err := Grayscale("image/webp", webpBytes)
	require.NoError(t, err)
	require.NotNil(t, out.OutputBytes)
	assert.Equal(t, "image/webp", out.OutputMimeType)

	// Round-trip the output back through the decoder to confirm it's
	// a valid WebP.
	roundTrip, _, err := decodeImage(out.OutputBytes, "image/webp", "grayscale")
	require.NoError(t, err)
	assert.Equal(t, 4, roundTrip.Bounds().Dx())
	assert.Equal(t, 4, roundTrip.Bounds().Dy())
}
