// Package ts emits a TypeScript OSDK package from an ontology
// snapshot. It is the v0 backend for sdk-generation-service: pure Go
// templates, stdlib-only tar+gzip output, no external generator
// binaries.
package ts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/ontologyclient"
)

// Generator is the entry point the worker calls. It is stateless;
// callers can share one instance across goroutines.
type Generator struct{}

// Generate renders the TS package for the given request + snapshot.
// The returned map is keyed by file name (no leading directory) so the
// caller can either tar it up via TarGz or write it to disk for
// debugging.
//
// Determinism: object/action/link iteration is sorted by name, so the
// rendered bytes are stable across runs for any fixed input.
func (g *Generator) Generate(req domain.SDKRequest, snapshot *ontologyclient.OntologySnapshot) (map[string][]byte, error) {
	if req.Target != domain.TargetTypeScript {
		return nil, fmt.Errorf("ts generator only handles target=ts, got %q", req.Target)
	}
	if snapshot == nil {
		return nil, fmt.Errorf("nil snapshot")
	}
	model := buildModel(req, snapshot)

	files := map[string][]byte{}
	for name, body := range map[string]string{
		"package.json": tmplPackageJSON,
		"tsconfig.json": tmplTSConfig,
		"index.ts":     tmplIndex,
		"types.ts":     tmplTypes,
		"actions.ts":   tmplActions,
		"client.ts":    tmplClient,
	} {
		rendered, err := render(name, body, model)
		if err != nil {
			return nil, err
		}
		files[name] = rendered
	}
	return files, nil
}

// TarGz packages a file map into a deterministic tar.gz blob. The
// caller owns the writer; on error the writer's state is undefined.
//
// Deterministic output requires: lexicographic file order, fixed
// header mode, zero mtime, and a gzip Header with empty Name and zero
// ModTime — the defaults populate the OS field with the local kernel
// build which would otherwise drift between hosts.
func TarGz(files map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	// gzip.Writer.Header is settable until the first Write; we leave
	// Name + ModTime empty so the header byte sequence is host-stable.
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		body := files[name]
		if err := tw.WriteHeader(&tar.Header{
			Name:   name,
			Mode:   0o644,
			Size:   int64(len(body)),
			Format: tar.FormatPAX,
		}); err != nil {
			return nil, fmt.Errorf("tar header %s: %w", name, err)
		}
		if _, err := tw.Write(body); err != nil {
			return nil, fmt.Errorf("tar body %s: %w", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("tar close: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("gzip close: %w", err)
	}
	return buf.Bytes(), nil
}

func render(name, body string, data any) ([]byte, error) {
	t, err := template.New(name).Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// ─── template model ─────────────────────────────────────────────────

type model struct {
	TenantID        string
	TenantSlug      string
	OntologyVersion string
	PackageVersion  string
	ObjectTypes     []objectTypeModel
	LinkTypes       []linkTypeModel
	ActionTypes     []actionTypeModel
}

type objectTypeModel struct {
	TSName     string
	TSField    string
	APIName    string
	Properties []propertyModel
}

type propertyModel struct {
	Name     string
	TSType   string
	Optional bool
}

type linkTypeModel struct {
	Name        string
	Source      string
	Target      string
	Cardinality string
}

type actionTypeModel struct {
	TSName     string
	TSField    string
	APIName    string
	Parameters []propertyModel
}

func buildModel(req domain.SDKRequest, snap *ontologyclient.OntologySnapshot) model {
	objectAllow := stringSet(req.IncludeObjectTypes)
	actionAllow := stringSet(req.IncludeActionTypes)
	knownObjects := map[string]bool{}

	objects := make([]objectTypeModel, 0, len(snap.ObjectTypes))
	for _, ot := range snap.ObjectTypes {
		if !objectAllow.contains(ot.Name) {
			continue
		}
		props := make([]propertyModel, 0, len(ot.Properties))
		for _, p := range ot.Properties {
			props = append(props, propertyModel{
				Name:     p.Name,
				TSType:   MapType(p.PropertyType),
				Optional: !p.Required,
			})
		}
		sort.Slice(props, func(i, j int) bool { return props[i].Name < props[j].Name })
		apiName := ot.APIName
		if apiName == "" {
			apiName = ot.Name
		}
		objects = append(objects, objectTypeModel{
			TSName:     pascalCase(ot.Name),
			TSField:    camelCase(ot.Name),
			APIName:    apiName,
			Properties: props,
		})
		knownObjects[ot.Name] = true
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].TSName < objects[j].TSName })

	links := make([]linkTypeModel, 0, len(snap.LinkTypes))
	for _, lt := range snap.LinkTypes {
		// Drop links pointing at filtered-out object types — the
		// generated package wouldn't compile otherwise.
		if !knownObjects[lt.SourceObjectType] || !knownObjects[lt.TargetObjectType] {
			continue
		}
		links = append(links, linkTypeModel{
			Name:        camelCase(lt.Name),
			Source:      pascalCase(lt.SourceObjectType),
			Target:      pascalCase(lt.TargetObjectType),
			Cardinality: lt.Cardinality,
		})
	}
	sort.Slice(links, func(i, j int) bool { return links[i].Name < links[j].Name })

	actions := make([]actionTypeModel, 0, len(snap.ActionTypes))
	for _, at := range snap.ActionTypes {
		if !actionAllow.contains(at.Name) {
			continue
		}
		params := make([]propertyModel, 0, len(at.Parameters))
		for _, p := range at.Parameters {
			params = append(params, propertyModel{
				Name:     p.Name,
				TSType:   MapType(p.PropertyType),
				Optional: !p.Required,
			})
		}
		sort.Slice(params, func(i, j int) bool { return params[i].Name < params[j].Name })
		apiName := at.APIName
		if apiName == "" {
			apiName = at.Name
		}
		actions = append(actions, actionTypeModel{
			TSName:     pascalCase(at.Name),
			TSField:    camelCase(at.Name),
			APIName:    apiName,
			Parameters: params,
		})
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].TSName < actions[j].TSName })

	return model{
		TenantID:        req.TenantID.String(),
		TenantSlug:      req.TenantID.String(),
		OntologyVersion: snap.Version,
		PackageVersion:  packageVersion(snap.Version),
		ObjectTypes:     objects,
		LinkTypes:       links,
		ActionTypes:     actions,
	}
}

// packageVersion coerces the ontology version into a valid npm semver.
// `v1.2.3` → `1.2.3`. Anything else falls back to `0.0.0-<sanitized>`
// so npm install still works during development with content-hash
// labels.
func packageVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "0.0.0"
	}
	if strings.HasPrefix(v, "v") && len(v) > 1 && isDigit(v[1]) {
		v = v[1:]
	}
	if looksSemver(v) {
		return v
	}
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '.', r == '-':
			return r
		default:
			return '-'
		}
	}, v)
	cleaned = strings.Trim(cleaned, "-")
	if cleaned == "" {
		cleaned = "snapshot"
	}
	return "0.0.0-" + cleaned
}

func looksSemver(v string) bool {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts[:2] {
		if p == "" {
			return false
		}
		for _, r := range p {
			if !isDigit(byte(r)) {
				return false
			}
		}
	}
	return true
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// ─── set helper ─────────────────────────────────────────────────────

type allowSet struct {
	enabled bool
	values  map[string]bool
}

func stringSet(in []string) allowSet {
	if len(in) == 0 {
		return allowSet{}
	}
	s := allowSet{enabled: true, values: make(map[string]bool, len(in))}
	for _, v := range in {
		s.values[v] = true
	}
	return s
}

func (s allowSet) contains(name string) bool {
	if !s.enabled {
		return true
	}
	return s.values[name]
}

// ─── identifier helpers ─────────────────────────────────────────────

func pascalCase(s string) string {
	parts := splitIdent(s)
	for i, p := range parts {
		parts[i] = upperFirst(p)
	}
	return strings.Join(parts, "")
}

func camelCase(s string) string {
	parts := splitIdent(s)
	if len(parts) == 0 {
		return s
	}
	parts[0] = lowerFirst(parts[0])
	for i := 1; i < len(parts); i++ {
		parts[i] = upperFirst(parts[i])
	}
	return strings.Join(parts, "")
}

func splitIdent(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_' || c == '-' || c == ' ' || c == '.':
			flush()
		case c >= 'A' && c <= 'Z':
			if cur.Len() > 0 {
				// keep runs of uppercase together but break before a
				// camel hump that follows a lowercase letter.
				prev := cur.String()[cur.Len()-1]
				if prev >= 'a' && prev <= 'z' {
					flush()
				}
			}
			cur.WriteByte(c)
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'A' && s[0] <= 'Z' {
		return string(s[0]+32) + s[1:]
	}
	return s
}
