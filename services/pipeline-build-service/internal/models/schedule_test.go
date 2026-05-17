package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScheduleResourceRIDsCollectsRIDArrays(t *testing.T) {
	trigger := json.RawMessage(`{
		"kind": {
			"event": {
				"type": "DATA_UPDATED",
				"target_rid": "ri.dataset.trigger"
			}
		}
	}`)
	target := json.RawMessage(`{
		"kind": {
			"dataset_build": {
				"dataset_rid": "ri.dataset.root",
				"output_dataset_rids": ["ri.dataset.a", "ri.dataset.b", "ri.dataset.a"],
				"target_sets": [
					{
						"strategy": "connecting",
						"input_dataset_rid": "ri.dataset.input",
						"target_dataset_rid": "ri.dataset.target"
					}
				]
			}
		}
	}`)

	require.Equal(t, []string{
		"ri.dataset.a",
		"ri.dataset.b",
		"ri.dataset.input",
		"ri.dataset.root",
		"ri.dataset.target",
		"ri.dataset.trigger",
	}, ScheduleResourceRIDs(trigger, target))
}
