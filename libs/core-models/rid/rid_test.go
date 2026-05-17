package rid_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/core-models/rid"
)

func TestParseRID(t *testing.T) {
	t.Parallel()

	parsed, err := rid.Parse("ri.foundry.main.dataset.018f2f1c-aaaa-bbbb-cccc-000000000001")
	require.NoError(t, err)

	assert.Equal(t, "foundry", parsed.Service)
	assert.Equal(t, "main", parsed.Instance)
	assert.Equal(t, "dataset", parsed.ResourceType)
	assert.Equal(t, "018f2f1c-aaaa-bbbb-cccc-000000000001", parsed.Locator)
	assert.Equal(t, "ri.foundry.main.dataset.018f2f1c-aaaa-bbbb-cccc-000000000001", parsed.String())
}

func TestParseAllowsSpecLocatorCharacters(t *testing.T) {
	t.Parallel()

	parsed, err := rid.Parse("ri.foundry.main.object-type.Customer.v1")
	require.NoError(t, err)

	assert.Equal(t, "Customer.v1", parsed.Locator)
}

func TestParseUUID(t *testing.T) {
	t.Parallel()

	parsed, err := rid.ParseUUID("ri.foundry.main.dataset.018f2f1c-aaaa-bbbb-cccc-000000000001")
	require.NoError(t, err)

	got, ok := parsed.UUID()
	require.True(t, ok)
	assert.Equal(t, "018f2f1c-aaaa-bbbb-cccc-000000000001", got.String())

	_, err = rid.ParseUUID("ri.foundry.main.dataset.customer-table")
	require.Error(t, err)
}

func TestMintUUIDV7(t *testing.T) {
	t.Parallel()

	minted, err := rid.MintUUIDV7("foundry", rid.DefaultInstance, "dataset")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(minted.String(), "ri.foundry.main.dataset."))
	id, ok := minted.UUID()
	require.True(t, ok)
	assert.Equal(t, uuid.Version(7), id.Version())
}

func TestRejectsMalformedRID(t *testing.T) {
	t.Parallel()

	cases := []string{
		"",
		"ri.foundry.main.dataset",
		"rid.foundry.main.dataset.018f2f1c-aaaa-bbbb-cccc-000000000001",
		"ri.Foundry.main.dataset.018f2f1c-aaaa-bbbb-cccc-000000000001",
		"ri.foundry.main.data_set.018f2f1c-aaaa-bbbb-cccc-000000000001",
		"ri.foundry.main.dataset.",
	}

	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			_, err := rid.Parse(input)
			require.Error(t, err)
			var invalid *rid.InvalidError
			assert.True(t, errors.As(err, &invalid))
		})
	}
}

func TestJSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := rid.MustNew("foundry", "main", "dataset", "018f2f1c-aaaa-bbbb-cccc-000000000001")
	out, err := json.Marshal(original)
	require.NoError(t, err)
	assert.JSONEq(t, `"ri.foundry.main.dataset.018f2f1c-aaaa-bbbb-cccc-000000000001"`, string(out))

	var parsed rid.ResourceIdentifier
	require.NoError(t, json.Unmarshal(out, &parsed))
	assert.Equal(t, original, parsed)
}
