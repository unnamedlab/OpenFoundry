// Package handlers hosts the native (pure-Go) image transformations
// + the dispatch table the REST runtime uses to route a `kind` to its
// implementation. External-binary handlers (`ffmpeg`, `tesseract`, …)
// are intentionally absent; the runtime returns 501 for those kinds
// via the catalog without reaching this package.
package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"

	"github.com/HugoSmits86/nativewebp"
	"github.com/disintegration/imaging"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"
)

// HandlerOutput is what a handler hands back to the REST layer.
type HandlerOutput struct {
	OutputMimeType string
	OutputBytes    []byte
	OutputJSON     any
}

// Sentinel error categories. Wrap with %w (or use the helpers) so the
// runtime can map to the matching wire-format code.
var (
	ErrUnsupportedMime = errors.New("unsupported mime type")
	ErrInvalidParams   = errors.New("invalid params")
	ErrDecode          = errors.New("decode failed")
	ErrEncode          = errors.New("encode failed")
)

// imageFormat mirrors the discriminator the Rust runtime uses to drive
// the codec (`image::ImageFormat`). The MIME → format map matches the
// Rust `format_for_mime` exactly.
type imageFormat int

const (
	formatPNG imageFormat = iota
	formatJPEG
	formatWebP
	formatGIF
	formatTIFF
	formatBMP
)

func formatForMime(mime, kind string) (imageFormat, error) {
	switch mime {
	case "image/png":
		return formatPNG, nil
	case "image/jpeg", "image/jpg":
		return formatJPEG, nil
	case "image/webp":
		return formatWebP, nil
	case "image/gif":
		return formatGIF, nil
	case "image/tiff":
		return formatTIFF, nil
	case "image/bmp":
		return formatBMP, nil
	default:
		return 0, fmt.Errorf("%w `%s` for transformation `%s`", ErrUnsupportedMime, mime, kind)
	}
}

func decodeImage(src []byte, mime, kind string) (image.Image, imageFormat, error) {
	f, err := formatForMime(mime, kind)
	if err != nil {
		return nil, 0, err
	}
	r := bytes.NewReader(src)
	var img image.Image
	switch f {
	case formatPNG:
		img, err = png.Decode(r)
	case formatJPEG:
		img, err = jpeg.Decode(r)
	case formatGIF:
		img, err = gif.Decode(r)
	case formatWebP:
		img, err = webp.Decode(r)
	case formatTIFF:
		img, err = tiff.Decode(r)
	case formatBMP:
		img, err = bmp.Decode(r)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %s", ErrDecode, err.Error())
	}
	return img, f, nil
}

func encodeImage(img image.Image, f imageFormat) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	switch f {
	case formatPNG:
		err = png.Encode(&buf, img)
	case formatJPEG:
		err = jpeg.Encode(&buf, img, nil)
	case formatGIF:
		err = gif.Encode(&buf, img, nil)
	case formatWebP:
		// Pure-Go WebP encoder (lossless VP8L). Matches Rust output
		// shape — a WebP-in / WebP-out round trip stays WebP.
		err = nativewebp.Encode(&buf, img, nil)
	case formatTIFF:
		err = tiff.Encode(&buf, img, nil)
	case formatBMP:
		err = bmp.Encode(&buf, img)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrEncode, err.Error())
	}
	return buf.Bytes(), nil
}

// resizeParams mirrors the Rust struct of the same name.
type resizeParams struct {
	Width  *uint32 `json:"width,omitempty"`
	Height *uint32 `json:"height,omitempty"`
	// Default Lanczos3 — Foundry's "Resize" produces high-fidelity
	// downscales; matches the upstream sample renders.
	Filter *string `json:"filter,omitempty"`
}

func parseFilter(name *string) imaging.ResampleFilter {
	want := "lanczos3"
	if name != nil {
		want = *name
	}
	switch want {
	case "nearest":
		return imaging.NearestNeighbor
	case "triangle":
		// Rust `image::FilterType::Triangle` is the linear-tap filter.
		return imaging.Linear
	case "catmullrom":
		return imaging.CatmullRom
	case "gaussian":
		return imaging.Gaussian
	default:
		return imaging.Lanczos
	}
}

func invalidParams(kind, msg string) error {
	return fmt.Errorf("%w for `%s`: %s", ErrInvalidParams, kind, msg)
}

// Thumbnail returns a fixed-box thumbnail. Foundry's default longest
// edge is 256 px; `max_dim` overrides it.
func Thumbnail(mime string, params json.RawMessage, src []byte) (HandlerOutput, error) {
	img, f, err := decodeImage(src, mime, "thumbnail")
	if err != nil {
		return HandlerOutput{}, err
	}
	maxDim := uint32(256)
	if len(params) > 0 {
		var p struct {
			MaxDim *uint32 `json:"max_dim,omitempty"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return HandlerOutput{}, invalidParams("thumbnail", err.Error())
		}
		if p.MaxDim != nil {
			maxDim = *p.MaxDim
		}
	}
	// imaging.Thumbnail crops to the exact box; for parity with Rust's
	// `DynamicImage::thumbnail` (which preserves aspect ratio inside
	// a max×max box) we use Fit instead.
	scaled := imaging.Fit(img, int(maxDim), int(maxDim), imaging.Lanczos)
	out, err := encodeImage(scaled, f)
	if err != nil {
		return HandlerOutput{}, err
	}
	return HandlerOutput{OutputMimeType: mime, OutputBytes: out}, nil
}

// Resize matches Rust's `resize_exact` — width and height are honoured
// independently (no aspect-ratio preservation).
func Resize(mime string, params json.RawMessage, src []byte) (HandlerOutput, error) {
	var p resizeParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return HandlerOutput{}, invalidParams("resize", err.Error())
		}
	}
	img, f, err := decodeImage(src, mime, "resize")
	if err != nil {
		return HandlerOutput{}, err
	}
	bounds := img.Bounds()
	tw := uint32(bounds.Dx())
	th := uint32(bounds.Dy())
	if p.Width != nil {
		tw = *p.Width
	}
	if p.Height != nil {
		th = *p.Height
	}
	resized := imaging.Resize(img, int(tw), int(th), parseFilter(p.Filter))
	out, err := encodeImage(resized, f)
	if err != nil {
		return HandlerOutput{}, err
	}
	return HandlerOutput{OutputMimeType: mime, OutputBytes: out}, nil
}

// ResizeWithinBoundingBox preserves aspect ratio within a max W×H box.
// width and height are required (Rust enforces the same).
func ResizeWithinBoundingBox(mime string, params json.RawMessage, src []byte) (HandlerOutput, error) {
	var p resizeParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return HandlerOutput{}, invalidParams("resize_within_bounding_box", err.Error())
		}
	}
	if p.Width == nil {
		return HandlerOutput{}, invalidParams("resize_within_bounding_box", "width is required")
	}
	if p.Height == nil {
		return HandlerOutput{}, invalidParams("resize_within_bounding_box", "height is required")
	}
	img, f, err := decodeImage(src, mime, "resize_within_bounding_box")
	if err != nil {
		return HandlerOutput{}, err
	}
	resized := imaging.Fit(img, int(*p.Width), int(*p.Height), parseFilter(p.Filter))
	out, err := encodeImage(resized, f)
	if err != nil {
		return HandlerOutput{}, err
	}
	return HandlerOutput{OutputMimeType: mime, OutputBytes: out}, nil
}

// Rotate honours only quarter-turns (0/90/180/270 deg, clockwise) —
// arbitrary angles are rejected with a 400, matching the Rust handler.
func Rotate(mime string, params json.RawMessage, src []byte) (HandlerOutput, error) {
	deg := 0
	if len(params) > 0 {
		var p struct {
			Degrees *int `json:"degrees,omitempty"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return HandlerOutput{}, invalidParams("rotate", err.Error())
		}
		if p.Degrees != nil {
			deg = *p.Degrees
		}
	}
	// Mirror Rust's `rem_euclid(360)` so negative degrees normalise.
	deg = ((deg % 360) + 360) % 360

	img, f, err := decodeImage(src, mime, "rotate")
	if err != nil {
		return HandlerOutput{}, err
	}
	var rotated image.Image
	switch deg {
	case 0:
		rotated = img
	case 90:
		// Rust `image::DynamicImage::rotate90` rotates 90° clockwise;
		// disintegration/imaging's `Rotate90` rotates 90° counter-
		// clockwise. Use `Rotate270` to match Rust's clockwise semantic.
		rotated = imaging.Rotate270(img)
	case 180:
		rotated = imaging.Rotate180(img)
	case 270:
		rotated = imaging.Rotate90(img)
	default:
		return HandlerOutput{}, invalidParams("rotate", fmt.Sprintf("only 0/90/180/270 deg supported, got %d", deg))
	}
	out, err := encodeImage(rotated, f)
	if err != nil {
		return HandlerOutput{}, err
	}
	return HandlerOutput{OutputMimeType: mime, OutputBytes: out}, nil
}

// Crop returns the rectangle (x, y, width, height); bounds are
// validated so an out-of-range crop is a 400, matching Rust.
func Crop(mime string, params json.RawMessage, src []byte) (HandlerOutput, error) {
	var p struct {
		X      uint32 `json:"x"`
		Y      uint32 `json:"y"`
		Width  uint32 `json:"width"`
		Height uint32 `json:"height"`
	}
	if len(params) == 0 {
		return HandlerOutput{}, invalidParams("crop", "x, y, width, height required")
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return HandlerOutput{}, invalidParams("crop", err.Error())
	}
	img, f, err := decodeImage(src, mime, "crop")
	if err != nil {
		return HandlerOutput{}, err
	}
	w := uint32(img.Bounds().Dx())
	h := uint32(img.Bounds().Dy())
	xEnd, okX := safeAdd(p.X, p.Width)
	yEnd, okY := safeAdd(p.Y, p.Height)
	if !okX || !okY || xEnd > w || yEnd > h {
		return HandlerOutput{}, invalidParams("crop", "crop rectangle exceeds image bounds")
	}
	rect := image.Rect(int(p.X), int(p.Y), int(xEnd), int(yEnd))
	cropped := imaging.Crop(img, rect)
	out, err := encodeImage(cropped, f)
	if err != nil {
		return HandlerOutput{}, err
	}
	return HandlerOutput{OutputMimeType: mime, OutputBytes: out}, nil
}

// Grayscale emits a luminance-only render of the input. No params.
func Grayscale(mime string, src []byte) (HandlerOutput, error) {
	img, f, err := decodeImage(src, mime, "grayscale")
	if err != nil {
		return HandlerOutput{}, err
	}
	gray := imaging.Grayscale(img)
	out, err := encodeImage(gray, f)
	if err != nil {
		return HandlerOutput{}, err
	}
	return HandlerOutput{OutputMimeType: mime, OutputBytes: out}, nil
}

func safeAdd(a, b uint32) (uint32, bool) {
	r := a + b
	if r < a {
		return 0, false
	}
	return r, true
}

// Dispatch routes a request to the matching native handler. Catalogue-
// only kinds (External / NotImplemented) never reach this function —
// the REST layer short-circuits them with the catalog status.
func Dispatch(kind, mime string, params json.RawMessage, src []byte) (HandlerOutput, error) {
	switch kind {
	case "thumbnail":
		return Thumbnail(mime, params, src)
	case "resize":
		return Resize(mime, params, src)
	case "resize_within_bounding_box":
		return ResizeWithinBoundingBox(mime, params, src)
	case "rotate":
		return Rotate(mime, params, src)
	case "crop":
		return Crop(mime, params, src)
	case "grayscale":
		return Grayscale(mime, src)
	case "embedding":
		return Embedding(mime, params, src)
	case "transcription":
		return Transcription(mime, params, src)
	case "layout_aware_v2":
		return LayoutAwareV2(mime, params, src)
	case "vlm_extract":
		return VLMExtract(mime, params, src)
	default:
		return HandlerOutput{}, invalidParams(kind, "no native handler — caller should not have reached Dispatch()")
	}
}
