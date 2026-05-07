package geospatialtiles_test

import (
	"errors"
	"testing"

	geospatialtiles "github.com/openfoundry/openfoundry-go/libs/geospatial-tiles"
)

func TestZ0IsOneTile(t *testing.T) {
	t.Parallel()
	if got := geospatialtiles.MaxIndex(0); got != 0 {
		t.Fatalf("MaxIndex(0) = %d, want 0", got)
	}
	if _, err := geospatialtiles.NewTileCoord(0, 0, 0); err != nil {
		t.Fatalf("NewTileCoord(0,0,0) errored: %v", err)
	}
	if _, err := geospatialtiles.NewTileCoord(0, 1, 0); err == nil {
		t.Fatal("NewTileCoord(0,1,0) should error")
	}
}

func TestZ3Has64Tiles(t *testing.T) {
	t.Parallel()
	if got := geospatialtiles.MaxIndex(3); got != 7 {
		t.Fatalf("MaxIndex(3) = %d, want 7", got)
	}
	if _, err := geospatialtiles.NewTileCoord(3, 7, 7); err != nil {
		t.Fatalf("NewTileCoord(3,7,7) errored: %v", err)
	}
	if _, err := geospatialtiles.NewTileCoord(3, 8, 0); err == nil {
		t.Fatal("NewTileCoord(3,8,0) should error")
	}
}

func TestURLRoundTrips(t *testing.T) {
	t.Parallel()
	coord, err := geospatialtiles.NewTileCoord(5, 12, 13)
	if err != nil {
		t.Fatalf("NewTileCoord: %v", err)
	}
	path := geospatialtiles.TileURLPath("ri.foundry.main.media_set.tiles", coord)
	if want := "/tiles/ri.foundry.main.media_set.tiles/5/12/13.png"; path != want {
		t.Fatalf("TileURLPath = %q, want %q", path, want)
	}
	rid, parsed, ok := geospatialtiles.ParseTileURLPath(path)
	if !ok {
		t.Fatal("ParseTileURLPath returned !ok")
	}
	if rid != "ri.foundry.main.media_set.tiles" {
		t.Fatalf("rid = %q, want %q", rid, "ri.foundry.main.media_set.tiles")
	}
	if parsed != coord {
		t.Fatalf("parsed = %+v, want %+v", parsed, coord)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	t.Parallel()
	cases := []string{
		"/foo/bar",
		"/tiles/r/1/2/3.jpg",
		"/tiles/r/extra/1/2/3.png",
	}
	for _, in := range cases {
		if _, _, ok := geospatialtiles.ParseTileURLPath(in); ok {
			t.Fatalf("ParseTileURLPath(%q) should have rejected", in)
		}
	}
}

func TestDescriptorCarriesCanonicalTemplate(t *testing.T) {
	t.Parallel()
	desc := geospatialtiles.NewTileSourceDescriptor("ri.foundry.main.media_set.x")
	if want := "/tiles/ri.foundry.main.media_set.x/{z}/{x}/{y}.png"; desc.TileURLTemplate != want {
		t.Fatalf("TileURLTemplate = %q, want %q", desc.TileURLTemplate, want)
	}
	if desc.TileSize != 256 {
		t.Fatalf("TileSize = %d, want 256", desc.TileSize)
	}
	if desc.MaxZoom != 22 {
		t.Fatalf("MaxZoom = %d, want 22", desc.MaxZoom)
	}
	if desc.MinZoom != 0 {
		t.Fatalf("MinZoom = %d, want 0", desc.MinZoom)
	}
	if want := "© OpenFoundry media-sets-service · access pattern: geo_tile"; desc.Attribution != want {
		t.Fatalf("Attribution = %q, want %q", desc.Attribution, want)
	}
}

func TestErrorKindMatching(t *testing.T) {
	t.Parallel()
	_, err := geospatialtiles.NewTileCoord(25, 0, 0)
	if err == nil {
		t.Fatal("expected zoom-out-of-range error")
	}
	var te *geospatialtiles.TileError
	if !errors.As(err, &te) {
		t.Fatalf("err is not *TileError: %T", err)
	}
	if te.Kind != geospatialtiles.ErrZoomOutOfRange {
		t.Fatalf("kind = %v, want ErrZoomOutOfRange", te.Kind)
	}
	if te.Z != 25 {
		t.Fatalf("Z = %d, want 25", te.Z)
	}
}
