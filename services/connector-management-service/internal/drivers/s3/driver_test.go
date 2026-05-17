package s3

import (
	"encoding/json"
	"testing"
)

func TestConfigFromJSON_RequiresBucketOrURL(t *testing.T) {
	t.Parallel()
	if _, err := ConfigFromJSON(json.RawMessage(`{}`)); err == nil {
		t.Fatalf("expected error for empty config")
	}
	if _, err := ConfigFromJSON(json.RawMessage(`{"region":"us-east-1"}`)); err == nil {
		t.Fatalf("expected error when bucket and url are missing")
	}
}

func TestConfigFromJSON_AcceptsBucket(t *testing.T) {
	t.Parallel()
	cfg, err := ConfigFromJSON(json.RawMessage(`{"bucket":"b"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Bucket != "b" {
		t.Fatalf("bucket = %q", cfg.Bucket)
	}
}

func TestConfigFromJSON_RejectsNonS3URL(t *testing.T) {
	t.Parallel()
	if _, err := ConfigFromJSON(json.RawMessage(`{"url":"https://example.com/x"}`)); err == nil {
		t.Fatalf("expected error for non-s3 url")
	}
}

func TestDriverJoinPrefix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		base, in, want string
	}{
		{"", "", ""},
		{"", "a/b", "a/b"},
		{"/sub/", "a/b", "sub/a/b"},
		{"sub", "", "sub/"},
	}
	for _, tc := range cases {
		d := &Driver{cfg: Config{Subfolder: tc.base}}
		if got := d.joinPrefix(tc.in); got != tc.want {
			t.Fatalf("joinPrefix(base=%q, in=%q) = %q, want %q", tc.base, tc.in, got, tc.want)
		}
	}
}
