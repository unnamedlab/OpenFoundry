package ts_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/generator/ts"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/ontologyclient"
)

// fixedTenant pins the UUID embedded in package.json so the rendered
// output is byte-stable across runs.
var fixedTenant = uuid.MustParse("11111111-1111-1111-1111-111111111111")

func fixedRequest() domain.SDKRequest {
	return domain.SDKRequest{
		TenantID:        fixedTenant,
		OntologyVersion: "v1.2.3",
		Target:          domain.TargetTypeScript,
	}
}

func fixedSnapshot() *ontologyclient.OntologySnapshot {
	snap := ontologyclient.DefaultStubSnapshot()
	snap.Version = "v1.2.3"
	return &snap
}

func TestGeneratorRendersExpectedFiles(t *testing.T) {
	t.Parallel()
	g := &ts.Generator{}
	files, err := g.Generate(fixedRequest(), fixedSnapshot())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	wantFiles := []string{"package.json", "tsconfig.json", "index.ts", "types.ts", "actions.ts", "client.ts"}
	for _, name := range wantFiles {
		if _, ok := files[name]; !ok {
			t.Errorf("missing %s; got %v", name, fileNames(files))
		}
	}

	// package.json captures tenant + version metadata.
	pkg := string(files["package.json"])
	for _, needle := range []string{
		`"name": "@openfoundry-sdk/11111111-1111-1111-1111-111111111111"`,
		`"version": "1.2.3"`,
		`"ontologyVersion": "v1.2.3"`,
	} {
		if !strings.Contains(pkg, needle) {
			t.Errorf("package.json missing %q\n%s", needle, pkg)
		}
	}

	// types.ts has typed interfaces per object type, alphabetized.
	types := string(files["types.ts"])
	customerIdx := strings.Index(types, "export interface Customer")
	orderIdx := strings.Index(types, "export interface Order")
	if customerIdx < 0 || orderIdx < 0 {
		t.Fatalf("types.ts missing expected interfaces:\n%s", types)
	}
	if customerIdx > orderIdx {
		t.Errorf("types.ts ordering: Customer should precede Order; got %d > %d", customerIdx, orderIdx)
	}
	for _, needle := range []string{
		"id: string;",        // required string
		"age?: number;",      // optional integer
		"active: boolean;",   // required boolean
		"createdAt: string;", // datetime → string
		`source: "Customer"`, // link wiring
	} {
		if !strings.Contains(types, needle) {
			t.Errorf("types.ts missing %q\n%s", needle, types)
		}
	}

	// actions.ts emits a Params + Result interface per action.
	actions := string(files["actions.ts"])
	for _, needle := range []string{
		"export interface PlaceOrderParams",
		"export interface PlaceOrderResult",
		"customerId: string;",
		"total: number;",
		`actionName: "place_order"`,
	} {
		if !strings.Contains(actions, needle) {
			t.Errorf("actions.ts missing %q\n%s", needle, actions)
		}
	}

	// client.ts exposes objects.<name>.list/get and actions.<name>().
	client := string(files["client.ts"])
	for _, needle := range []string{
		"export class OSDKClient",
		"customer: {",
		"order: {",
		"placeOrder: async (params: _PlaceOrderParams)",
		`"/api/v1/ontology/objects/customer"`,
		`"/api/v1/ontology/actions/place_order"`,
	} {
		if !strings.Contains(client, needle) {
			t.Errorf("client.ts missing %q\n%s", needle, client)
		}
	}
}

func TestGeneratorIsByteDeterministic(t *testing.T) {
	t.Parallel()
	g := &ts.Generator{}
	first, err := g.Generate(fixedRequest(), fixedSnapshot())
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	second, err := g.Generate(fixedRequest(), fixedSnapshot())
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("file count drift: %d vs %d", len(first), len(second))
	}
	for name, body := range first {
		other, ok := second[name]
		if !ok {
			t.Errorf("missing %s on second run", name)
			continue
		}
		if string(body) != string(other) {
			t.Errorf("byte drift in %s:\nfirst:\n%s\n---\nsecond:\n%s", name, body, other)
		}
	}

	// tar.gz packaging is deterministic too — the snapshot tests upstream
	// depend on byte-equal artifacts to compute build content hashes.
	tar1, err := ts.TarGz(first)
	if err != nil {
		t.Fatalf("tar1: %v", err)
	}
	tar2, err := ts.TarGz(second)
	if err != nil {
		t.Fatalf("tar2: %v", err)
	}
	if len(tar1) != len(tar2) {
		t.Fatalf("tar length drift: %d vs %d", len(tar1), len(tar2))
	}
	for i := range tar1 {
		if tar1[i] != tar2[i] {
			t.Fatalf("tar byte %d differs: %x vs %x", i, tar1[i], tar2[i])
		}
	}
}

func TestGeneratorRespectsIncludeFilters(t *testing.T) {
	t.Parallel()
	g := &ts.Generator{}
	req := fixedRequest()
	req.IncludeObjectTypes = []string{"Customer"}
	req.IncludeActionTypes = []string{} // nil/empty → include all actions
	files, err := g.Generate(req, fixedSnapshot())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	types := string(files["types.ts"])
	if !strings.Contains(types, "export interface Customer") {
		t.Errorf("Customer should still be present:\n%s", types)
	}
	if strings.Contains(types, "export interface Order") {
		t.Errorf("Order should be filtered out:\n%s", types)
	}
	// Link to Order should be dropped because target type is filtered.
	if strings.Contains(types, `target: "Order"`) {
		t.Errorf("link to filtered-out Order should be dropped:\n%s", types)
	}
}

func TestGeneratorRejectsNonTSTarget(t *testing.T) {
	t.Parallel()
	g := &ts.Generator{}
	req := fixedRequest()
	req.Target = domain.TargetPython
	if _, err := g.Generate(req, fixedSnapshot()); err == nil {
		t.Fatalf("expected error for python target")
	}
}

func fileNames(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for name := range files {
		out = append(out, name)
	}
	return out
}
