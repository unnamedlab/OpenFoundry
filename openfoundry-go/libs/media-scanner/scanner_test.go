package mediascanner_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	mediascanner "github.com/openfoundry/openfoundry-go/libs/media-scanner"
)

func finding(item string, tag mediascanner.PiiTag, matched string) mediascanner.SdsFinding {
	return mediascanner.SdsFinding{
		MediaSetRID: "ri.foundry.main.media_set.x",
		ItemRID:     item,
		Tag:         tag,
		Matched:     matched,
		Confidence:  0.9,
	}
}

func TestScanReturnsScriptedFindings(t *testing.T) {
	t.Parallel()
	mock := mediascanner.NewMockMediaScanRuntime()
	mock.PutReport("doc-1", mediascanner.SdsScanReport{
		MediaSetRID: "ri.foundry.main.media_set.x",
		ItemRID:     "doc-1",
		Findings: []mediascanner.SdsFinding{
			finding("doc-1", mediascanner.PiiGovernmentID, "123-45-6789"),
			finding("doc-1", mediascanner.PiiEmail, "ops@example.com"),
		},
	})

	report, err := mock.ScanItem(context.Background(), "ri.foundry.main.media_set.x", "doc-1")
	if err != nil {
		t.Fatalf("ScanItem: %v", err)
	}
	if !report.HasFindings() {
		t.Fatal("HasFindings should be true")
	}
	tags := report.DistinctTags()
	got := []string{tags[0].String(), tags[1].String()}
	want := []string{"GOVERNMENT_ID", "EMAIL"}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("tag[%d] = %q, want %q (full: %v)", i, got[i], w, got)
		}
	}

	calls := mock.Calls()
	if len(calls) != 1 || calls[0].ItemRID != "doc-1" {
		t.Fatalf("calls = %+v, want one entry for doc-1", calls)
	}
}

func TestMissingItemSurfacesNotFound(t *testing.T) {
	t.Parallel()
	mock := mediascanner.NewMockMediaScanRuntime()
	_, err := mock.ScanItem(context.Background(), "set", "ghost")
	if err == nil {
		t.Fatal("expected NotFound error")
	}
	var se *mediascanner.ScanError
	if !errors.As(err, &se) {
		t.Fatalf("err is not *ScanError: %T", err)
	}
	if se.Kind != mediascanner.ErrNotFound {
		t.Fatalf("kind = %v, want ErrNotFound", se.Kind)
	}
	if se.Detail != "ghost" {
		t.Fatalf("detail = %q, want %q", se.Detail, "ghost")
	}
}

func TestPiiTagJSONRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tag  mediascanner.PiiTag
		json string
	}{
		{mediascanner.PiiGovernmentID, `"governmentId"`},
		{mediascanner.PiiEmail, `"email"`},
		{mediascanner.PiiPhoneNumber, `"phoneNumber"`},
		{mediascanner.PiiCreditCard, `"creditCard"`},
		{mediascanner.PiiAddress, `"address"`},
		{mediascanner.PiiDateOfBirth, `"dateOfBirth"`},
		{mediascanner.PiiPersonName, `"personName"`},
	}
	for _, c := range cases {
		out, err := json.Marshal(c.tag)
		if err != nil {
			t.Fatalf("Marshal(%v): %v", c.tag, err)
		}
		if string(out) != c.json {
			t.Fatalf("Marshal(%v) = %s, want %s", c.tag, out, c.json)
		}
		var back mediascanner.PiiTag
		if err := json.Unmarshal([]byte(c.json), &back); err != nil {
			t.Fatalf("Unmarshal(%s): %v", c.json, err)
		}
		if back != c.tag {
			t.Fatalf("Unmarshal(%s) = %v, want %v", c.json, back, c.tag)
		}
	}
}

func TestSdsFindingPageOmitEmpty(t *testing.T) {
	t.Parallel()
	f := mediascanner.SdsFinding{
		MediaSetRID: "set",
		ItemRID:     "item",
		Tag:         mediascanner.PiiEmail,
		Matched:     "x@y",
		Confidence:  0.5,
	}
	out, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got := string(out); got != `{"media_set_rid":"set","item_rid":"item","tag":"email","matched":"x@y","confidence":0.5}` {
		t.Fatalf("Marshal omitempty form mismatch: %s", got)
	}

	page := uint32(7)
	f.Page = &page
	out, err = json.Marshal(f)
	if err != nil {
		t.Fatalf("Marshal with page: %v", err)
	}
	if got := string(out); got != `{"media_set_rid":"set","item_rid":"item","tag":"email","matched":"x@y","confidence":0.5,"page":7}` {
		t.Fatalf("Marshal with page mismatch: %s", got)
	}
}

func TestDistinctTagsSortedByEnumOrder(t *testing.T) {
	t.Parallel()
	report := mediascanner.SdsScanReport{
		Findings: []mediascanner.SdsFinding{
			{Tag: mediascanner.PiiPersonName},
			{Tag: mediascanner.PiiAddress},
			{Tag: mediascanner.PiiEmail},
			{Tag: mediascanner.PiiAddress}, // duplicate, should be deduped
		},
	}
	got := report.DistinctTags()
	want := []mediascanner.PiiTag{
		mediascanner.PiiEmail,
		mediascanner.PiiAddress,
		mediascanner.PiiPersonName,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d tags, want %d (got=%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("tag[%d] = %v, want %v (full: %v)", i, got[i], w, got)
		}
	}
}

func TestQuotaRemainingTracking(t *testing.T) {
	t.Parallel()
	mock := mediascanner.NewMockMediaScanRuntime()
	mock.PutQuota("tenant-A", 12345)
	got, ok := mock.QuotaRemaining(context.Background(), "tenant-A")
	if !ok || got != 12345 {
		t.Fatalf("tenant-A quota = (%d, %v), want (12345, true)", got, ok)
	}
	if _, ok := mock.QuotaRemaining(context.Background(), "tenant-Z"); ok {
		t.Fatal("tenant-Z should report ok=false (unlimited)")
	}
}

func TestScanErrorMessages(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  *mediascanner.ScanError
		want string
	}{
		{&mediascanner.ScanError{Kind: mediascanner.ErrNotFound, Detail: "x"}, "media item `x` not found"},
		{&mediascanner.ScanError{Kind: mediascanner.ErrQuotaExhausted, Detail: "t"}, "quota exhausted for tenant `t`"},
		{&mediascanner.ScanError{Kind: mediascanner.ErrRuntime, Detail: "boom"}, "upstream OCR runtime returned: boom"},
		{&mediascanner.ScanError{Kind: mediascanner.ErrUnscannableKind, Detail: "audio"}, "media kind `audio` is not scannable (no OCR/extract_text path)"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Fatalf("Error() = %q, want %q", got, c.want)
		}
	}
}
