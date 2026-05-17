package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/generator"
)

// Generator is the minimum surface the handler needs from the driver.
// The integration test stubs it; production uses *generator.Driver.
type Generator interface {
	Generate(ctx context.Context, service string, lang generator.Lang) (string, error)
}

// GenerateHandler wires POST /api/v1/sdk/generate. It runs the
// underlying generator and streams the produced output back to the
// caller as a zip.
type GenerateHandler struct {
	Driver Generator
}

// GenerateSDKRequest is the JSON body of POST /api/v1/sdk/generate.
type GenerateSDKRequest struct {
	Service  string `json:"service"`
	Language string `json:"language"`
}

func (g *GenerateHandler) Generate(w http.ResponseWriter, r *http.Request) {
	var body GenerateSDKRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeText(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Service == "" {
		writeText(w, http.StatusBadRequest, "service is required")
		return
	}
	lang, err := generator.ParseLang(body.Language)
	if err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	dir, err := g.Driver.Generate(r.Context(), body.Service, lang)
	if err != nil {
		slog.Error("sdk generation failed",
			slog.String("service", body.Service),
			slog.String("lang", string(lang)),
			slog.String("error", err.Error()),
		)
		writeText(w, http.StatusBadGateway, "generator failed: "+err.Error())
		return
	}
	defer os.RemoveAll(dir)

	// Build the archive in memory; SDK trees for the two POC services
	// are tiny (< 1 MiB compressed). If this ever needs to handle
	// large SDKs we switch to streaming via a chi ResponseWriter.
	var buf bytes.Buffer
	if err := generator.ZipDirectory(&buf, dir); err != nil {
		slog.Error("zip output failed", slog.String("error", err.Error()))
		writeText(w, http.StatusInternalServerError, "zip failed: "+err.Error())
		return
	}
	filename := fmt.Sprintf("%s-%s-sdk.zip", body.Service, lang)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", buf.Len()))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
