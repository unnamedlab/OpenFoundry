package notepad

import (
	"strings"
	"testing"
)

func TestRenderMarkdownHeadingsAndList(t *testing.T) {
	t.Parallel()
	got := RenderMarkdown("# Title\n\n- one\n- two\n\nbody")
	want := []string{"<h1>Title</h1>", "<ul><li>one</li><li>two</li></ul>", "<p>body</p>"}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("expected %q in output: %s", w, got)
		}
	}
}

func TestRenderMarkdownEscapes(t *testing.T) {
	t.Parallel()
	got := RenderMarkdown("a <b> & c")
	if !strings.Contains(got, "&lt;b&gt;") || !strings.Contains(got, "&amp;") {
		t.Fatalf("escaping failed: %s", got)
	}
}

func TestSlugify(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"Hello World":     "hello-world",
		"  __spaces__":    "spaces",
		"Mix3d-content!!": "mix3d-content",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q)=%q want %q", in, got, want)
		}
	}
}

func TestPreviewExcerptCapsAt180(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 250)
	got := previewExcerpt(long)
	if len([]rune(got)) != 180 {
		t.Fatalf("expected 180 runes, got %d", len([]rune(got)))
	}
}

func TestPreviewExcerptFallsBackToDefault(t *testing.T) {
	t.Parallel()
	if got := previewExcerpt("\n\n   "); got != "Document export" {
		t.Fatalf("unexpected fallback: %q", got)
	}
}
