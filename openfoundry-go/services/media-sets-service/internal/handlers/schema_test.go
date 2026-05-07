package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidSchemaAcceptsDICOM pins the H7 expansion: migration
// 0008_dicom_schema.sql added DICOM to the CHECK constraint, and the
// HTTP validator must mirror it.
func TestValidSchemaAcceptsDICOM(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"IMAGE", "AUDIO", "VIDEO", "DOCUMENT", "SPREADSHEET", "EMAIL", "DICOM"} {
		assert.True(t, validSchema(s), "schema %q must be accepted", s)
	}
}

func TestValidSchemaRejectsUnknown(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "BANANA", "image", "Dicom"} {
		assert.False(t, validSchema(s), "schema %q must be rejected", s)
	}
}
