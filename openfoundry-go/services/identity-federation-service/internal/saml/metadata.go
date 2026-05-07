package saml

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ParseMetadataDefaults mirrors fn `parse_metadata_defaults`.
//
// Walks the IdP-published EntityDescriptor and harvests three
// optional fields:
//   - entity_id  — `EntityDescriptor[@entityID]`
//   - sso_url    — first `SingleSignOnService[@Location]` (any binding)
//   - certificate — first `X509Certificate` text body, whitespace-stripped
//
// The walk is local-name-only, namespace-insensitive — matching the
// Rust roxmltree impl. Whitespace-only attribute values surface as
// nil (consistent with `trimmed`).
func ParseMetadataDefaults(metadataXML string) (MetadataDefaults, error) {
	dec := xml.NewDecoder(strings.NewReader(metadataXML))
	dec.Strict = false

	var (
		out         MetadataDefaults
		captureCert bool
		certBody    strings.Builder
	)

	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return MetadataDefaults{}, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "EntityDescriptor":
				if out.EntityID == nil {
					if v := attrValue(t.Attr, "entityID"); v != nil {
						out.EntityID = v
					}
				}
			case "SingleSignOnService":
				if out.SsoURL == nil {
					if v := attrValue(t.Attr, "Location"); v != nil {
						out.SsoURL = v
					}
				}
			case "X509Certificate":
				if out.Certificate == nil {
					captureCert = true
					certBody.Reset()
				}
			}
		case xml.CharData:
			if captureCert {
				certBody.Write(t)
			}
		case xml.EndElement:
			if t.Name.Local == "X509Certificate" && captureCert {
				captureCert = false
				body := stripAllWhitespace(certBody.String())
				if body != "" {
					out.Certificate = &body
				}
			}
		}
	}

	return out, nil
}

// ResolveMetadataDefaults mirrors fn `resolve_metadata_defaults` —
// HTTP-GETs the metadata URL and forwards the body to
// `ParseMetadataDefaults`. Non-2xx responses surface as errors.
//
// The supplied context governs both the dial + read deadlines via
// http.NewRequestWithContext; if the caller wants a global wall
// clock it should set it on its own ctx before calling. The
// httpClient argument lets tests inject a stubbed transport
// (production passes http.DefaultClient).
func ResolveMetadataDefaults(ctx context.Context, httpClient *http.Client, metadataURL string) (MetadataDefaults, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return MetadataDefaults{}, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return MetadataDefaults{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return MetadataDefaults{}, fmt.Errorf("metadata fetch failed with status %d %s", resp.StatusCode, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return MetadataDefaults{}, err
	}
	return ParseMetadataDefaults(string(body))
}

// attrValue returns the first attribute matching `local` (any
// namespace), trimmed and nil-on-empty. Mirrors the
// `node.attribute("...").and_then(trimmed)` pattern.
func attrValue(attrs []xml.Attr, local string) *string {
	for _, a := range attrs {
		if a.Name.Local == local {
			return trimmed(a.Value)
		}
	}
	return nil
}
