package handlers

import (
	"encoding/json"
	"image"
	"strings"

	"github.com/disintegration/imaging"

	geospatialtiles "github.com/openfoundry/openfoundry-go/libs/geospatial-tiles"
)

const defaultGeoTileSize = uint32(256)

type geoTileParams struct {
	MediaSetRID string  `json:"media_set_rid,omitempty"`
	Z           *uint8  `json:"z,omitempty"`
	X           *uint32 `json:"x,omitempty"`
	Y           *uint32 `json:"y,omitempty"`
	TileSize    *uint32 `json:"tile_size,omitempty"`
}

type GeoTileMetadata struct {
	Descriptor geospatialtiles.TileSourceDescriptor `json:"descriptor"`
	Coord      geospatialtiles.TileCoord            `json:"coord"`
	TilePath   string                               `json:"tile_path"`
}

// GeoTile renders an XYZ Web-Mercator raster tile from an input image.
// The geospatial-tiles library owns the shared descriptor/path/coordinate
// validation contract; this runtime adapter supplies the image crop + PNG
// encode step for media-transform-runtime-service.
func GeoTile(mime string, params json.RawMessage, src []byte) (HandlerOutput, error) {
	p, err := parseGeoTileParams(params)
	if err != nil {
		return HandlerOutput{}, err
	}
	coord, err := geospatialtiles.NewTileCoord(valueOr(p.Z, 0), valueOr(p.X, 0), valueOr(p.Y, 0))
	if err != nil {
		return HandlerOutput{}, invalidParams("geo_tile", err.Error())
	}
	tileSize := valueOr(p.TileSize, defaultGeoTileSize)
	if tileSize == 0 || tileSize > 4096 {
		return HandlerOutput{}, invalidParams("geo_tile", "tile_size must be between 1 and 4096")
	}

	mediaSetRID := strings.TrimSpace(p.MediaSetRID)
	if mediaSetRID == "" {
		mediaSetRID = "inline"
	}
	metadata := GeoTileMetadata{
		Descriptor: geospatialtiles.NewTileSourceDescriptor(mediaSetRID),
		Coord:      coord,
		TilePath:   geospatialtiles.TileURLPath(mediaSetRID, coord),
	}

	if len(src) == 0 {
		return HandlerOutput{OutputMimeType: "application/json", OutputJSON: metadata}, nil
	}

	img, _, err := decodeImage(src, mime, "geo_tile")
	if err != nil {
		return HandlerOutput{}, err
	}
	cropped := cropXYZTile(img, coord)
	resized := imaging.Resize(cropped, int(tileSize), int(tileSize), imaging.Lanczos)
	out, err := encodeImage(resized, formatPNG)
	if err != nil {
		return HandlerOutput{}, err
	}
	return HandlerOutput{OutputMimeType: "image/png", OutputBytes: out, OutputJSON: metadata}, nil
}

func parseGeoTileParams(params json.RawMessage) (geoTileParams, error) {
	var p geoTileParams
	if len(params) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return p, invalidParams("geo_tile", err.Error())
	}
	return p, nil
}

func cropXYZTile(img image.Image, coord geospatialtiles.TileCoord) image.Image {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	divisions := int(uint32(1) << coord.Z)
	if divisions <= 0 {
		divisions = 1
	}
	x0 := bounds.Min.X + int((int64(w)*int64(coord.X))/int64(divisions))
	x1 := bounds.Min.X + int((int64(w)*int64(coord.X+1))/int64(divisions))
	y0 := bounds.Min.Y + int((int64(h)*int64(coord.Y))/int64(divisions))
	y1 := bounds.Min.Y + int((int64(h)*int64(coord.Y+1))/int64(divisions))
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	return imaging.Crop(img, image.Rect(x0, y0, x1, y1))
}

func valueOr[T any](v *T, fallback T) T {
	if v == nil {
		return fallback
	}
	return *v
}
