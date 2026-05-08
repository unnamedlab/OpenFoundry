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

func makeEncodedImage(t *testing.T, w, h int, c color.RGBA, mime string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, c)
		}
	}
	f, err := formatForMime(mime, "test")
	require.NoError(t, err)
	out, err := encodeImage(img, f)
	require.NoError(t, err)
	return out
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

func TestGrayscaleEmitsEqualRGBChannels(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 1, 1, color.RGBA{R: 200, G: 40, B: 10, A: 255})
	out, err := Grayscale("image/png", src)
	require.NoError(t, err)
	assert.Equal(t, "image/png", out.OutputMimeType)

	img := decodePNG(t, out.OutputBytes)
	r, g, b, a := img.At(0, 0).RGBA()
	assert.Equal(t, r, g)
	assert.Equal(t, g, b)
	assert.Equal(t, uint32(0xffff), a)
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

func TestResizeExactDimensionsAcrossRustImageMimes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mime string
	}{
		{name: "png", mime: "image/png"},
		{name: "jpeg", mime: "image/jpeg"},
		{name: "webp", mime: "image/webp"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := makeEncodedImage(t, 9, 6, color.RGBA{R: 64, G: 128, B: 192, A: 255}, tc.mime)
			out, err := Resize(tc.mime, json.RawMessage(`{"width": 3, "height": 2}`), src)
			require.NoError(t, err)
			assert.Equal(t, tc.mime, out.OutputMimeType)
			require.NotEmpty(t, out.OutputBytes)

			decoded, _, err := decodeImage(out.OutputBytes, tc.mime, "resize")
			require.NoError(t, err)
			assert.Equal(t, 3, decoded.Bounds().Dx())
			assert.Equal(t, 2, decoded.Bounds().Dy())
		})
	}
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

func TestGeoTileRendersPNGTile(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 64, 64, color.RGBA{R: 20, G: 80, B: 160, A: 255})
	out, err := GeoTile("image/png", json.RawMessage(`{"media_set_rid":"ri.foundry.main.media_set.demo","z":1,"x":1,"y":1,"tile_size":32}`), src)
	require.NoError(t, err)
	assert.Equal(t, "image/png", out.OutputMimeType)
	img := decodePNG(t, out.OutputBytes)
	assert.Equal(t, 32, img.Bounds().Dx())
	assert.Equal(t, 32, img.Bounds().Dy())
	meta := out.OutputJSON.(GeoTileMetadata)
	assert.Equal(t, uint8(1), meta.Coord.Z)
	assert.Equal(t, "/tiles/ri.foundry.main.media_set.demo/1/1/1.png", meta.TilePath)
}

func TestGeoTileMetadataWithoutBytes(t *testing.T) {
	t.Parallel()
	out, err := GeoTile("image/png", json.RawMessage(`{"media_set_rid":"ri.foundry.main.media_set.demo"}`), nil)
	require.NoError(t, err)
	assert.Equal(t, "application/json", out.OutputMimeType)
	assert.Nil(t, out.OutputBytes)
	meta := out.OutputJSON.(GeoTileMetadata)
	assert.Equal(t, "/tiles/ri.foundry.main.media_set.demo/{z}/{x}/{y}.png", meta.Descriptor.TileURLTemplate)
}

func TestRenderSheetCSVProducesRowsAndHTML(t *testing.T) {
	t.Parallel()
	out, err := RenderSheet("text/csv", json.RawMessage(`{"sheet_name":"Revenue"}`), []byte("city,total\nParis,10\nMadrid,12\n"))
	require.NoError(t, err)
	assert.Equal(t, "application/json", out.OutputMimeType)
	result := out.OutputJSON.(RenderSheetOutput)
	assert.Equal(t, "Revenue", result.SheetName)
	assert.Equal(t, 2, result.RowCount)
	assert.Equal(t, 2, result.ColumnCount)
	assert.Equal(t, "Paris", result.Rows[0]["city"])
	assert.Contains(t, result.HTML, `<table data-sheet="Revenue">`)
}

func TestRenderSheetRejectsUnsupportedMime(t *testing.T) {
	t.Parallel()
	_, err := RenderSheet("application/octet-stream", nil, []byte("x"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnsupportedMime))
}
