package cipherclient

import (
	"strings"
	"testing"
)

func TestGeneratedNamespacesExposeCipherHelpers(t *testing.T) {
	for _, snippet := range []string{TypeScriptNamespace, PythonNamespace} {
		for _, name := range []string{"encrypt", "decrypt", "tokenize"} {
			if !strings.Contains(snippet, name) {
				t.Fatalf("snippet missing %s: %s", name, snippet)
			}
		}
	}
	if got := RequiredCallerForwardingHeaders(); len(got) != 3 || got[0] != "Authorization" {
		t.Fatalf("unexpected forwarding headers: %+v", got)
	}
}
