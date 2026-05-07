// Time-series + Quiver Vega-Lite spec builder. Mirrors
// `libs/ontology-kernel/src/domain/time_series.rs`.
//
// Despite the file name (kept for 1:1 parity with the Rust source),
// this module is the spec-template for the Quiver visual-function
// surface — pure logic, no IO.

package domain

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// NormalizeChartKind mirrors `normalize_chart_kind`. The set of
// allowed values + the verbatim error string are pinned by tests.
func NormalizeChartKind(chartKind string) (string, error) {
	switch chartKind {
	case "line", "area", "bar", "point":
		return chartKind, nil
	default:
		return "", fmt.Errorf("chart_kind must be one of: line, area, bar, point (received %s)", chartKind)
	}
}

// BuildQuiverVegaSpec mirrors `build_quiver_vega_spec`. Returns the
// JSON-encoded Vega-Lite spec as raw bytes; callers usually feed it
// straight into a HTTP body.
func BuildQuiverVegaSpec(draft models.QuiverVisualFunctionDraft) (json.RawMessage, error) {
	chartKind, err := NormalizeChartKind(draft.ChartKind)
	if err != nil {
		return nil, err
	}
	selectedGroup := ""
	if draft.SelectedGroup != nil {
		selectedGroup = *draft.SelectedGroup
	}
	joinMode := "single_object_set"
	if draft.SecondaryTypeID != nil {
		joinMode = "joined_object_sets"
	}

	var timeSeriesMark map[string]any
	if chartKind == "area" {
		timeSeriesMark = map[string]any{
			"type":        "area",
			"line":        true,
			"point":       true,
			"interpolate": "monotone",
			"opacity":     0.22,
		}
	} else {
		timeSeriesMark = map[string]any{
			"type":        chartKind,
			"point":       chartKind != "bar",
			"interpolate": "monotone",
		}
	}

	description := draft.Description
	if strings.TrimSpace(description) == "" {
		description = fmt.Sprintf("Quiver visual function '%s' generated from ontology analytics.", draft.Name)
	}

	subtitle := fmt.Sprintf(
		"%s • metric %s • group %s • join mode %s",
		draft.DateField, draft.MetricField, draft.GroupField, joinMode,
	)

	spec := map[string]any{
		"$schema":     "https://vega.github.io/schema/vega-lite/v5.json",
		"description": description,
		"title": map[string]any{
			"text":     draft.Name,
			"subtitle": subtitle,
			"anchor":   "start",
		},
		"spacing":    20,
		"background": "#ffffff",
		"datasets": map[string]any{
			"timeSeries": []any{},
			"grouped":    []any{},
		},
		"params": []any{
			map[string]any{
				"name":  "selectedGroup",
				"value": selectedGroup,
			},
		},
		"vconcat": []any{
			map[string]any{
				"data": map[string]any{"name": "timeSeries"},
				"mark": timeSeriesMark,
				"encoding": map[string]any{
					"x": map[string]any{
						"field": "date",
						"type":  "temporal",
						"title": draft.DateField,
					},
					"y": map[string]any{
						"field": "value",
						"type":  "quantitative",
						"title": draft.MetricField,
					},
					"tooltip": []any{
						map[string]any{"field": "date", "type": "temporal", "title": draft.DateField},
						map[string]any{"field": "value", "type": "quantitative", "title": draft.MetricField},
						map[string]any{"field": "count", "type": "quantitative", "title": "count"},
					},
				},
			},
			map[string]any{
				"data": map[string]any{"name": "grouped"},
				"mark": map[string]any{
					"type":                  "bar",
					"cornerRadiusTopLeft":   6,
					"cornerRadiusTopRight":  6,
				},
				"encoding": map[string]any{
					"x": map[string]any{
						"field": "group",
						"type":  "nominal",
						"sort":  "-y",
						"title": draft.GroupField,
					},
					"y": map[string]any{
						"field": "value",
						"type":  "quantitative",
						"title": draft.MetricField,
					},
					"color": map[string]any{
						"field":  "group",
						"type":   "nominal",
						"legend": nil,
					},
					"tooltip": []any{
						map[string]any{"field": "group", "type": "nominal", "title": draft.GroupField},
						map[string]any{"field": "value", "type": "quantitative", "title": draft.MetricField},
						map[string]any{"field": "count", "type": "quantitative", "title": "count"},
					},
				},
			},
		},
		"config": map[string]any{
			"view": map[string]any{"stroke": "#dbe4ea", "cornerRadius": 18},
			"axis": map[string]any{
				"labelColor": "#334155",
				"titleColor": "#0f172a",
				"gridColor":  "#e2e8f0",
				"tickColor":  "#cbd5e1",
			},
			"header": map[string]any{
				"titleColor": "#0f172a",
				"labelColor": "#475569",
			},
		},
		"usermeta": map[string]any{
			"quiver": map[string]any{
				"primary_type_id":      draft.PrimaryTypeID,
				"secondary_type_id":    draft.SecondaryTypeID,
				"join_field":           draft.JoinField,
				"secondary_join_field": draft.SecondaryJoinField,
				"date_field":           draft.DateField,
				"metric_field":         draft.MetricField,
				"group_field":          draft.GroupField,
				"selected_group":       draft.SelectedGroup,
				"chart_kind":           chartKind,
				"shared":               draft.Shared,
			},
		},
	}
	return json.Marshal(spec)
}
