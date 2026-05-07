package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWindowReducedSemantics(t *testing.T) {
	t.Parallel()
	cases := []struct {
		previous, next int64
		want           bool
		why            string
	}{
		{0, 0, false, "forever stays forever"},
		{0, 60, true, "shrink from forever to 60s is a reduction"},
		{60, 0, false, "expanding to forever is never a reduction"},
		{120, 60, true, "60 < 120 reduces"},
		{60, 120, false, "120 > 60 expands"},
		{60, 60, false, "no-op equals neither reduces nor expands"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, windowReduced(c.previous, c.next),
			"prev=%d next=%d: %s", c.previous, c.next, c.why)
	}
}
