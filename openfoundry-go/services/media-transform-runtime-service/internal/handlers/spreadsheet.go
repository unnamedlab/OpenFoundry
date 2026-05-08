package handlers

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"sort"
	"strings"
)

type renderSheetParams struct {
	SheetName string `json:"sheet_name,omitempty"`
	HasHeader *bool  `json:"has_header,omitempty"`
}

type RenderSheetOutput struct {
	SheetName   string           `json:"sheet_name"`
	Rows        []map[string]any `json:"rows"`
	HTML        string           `json:"html"`
	RowCount    int              `json:"row_count"`
	ColumnCount int              `json:"column_count"`
}

// RenderSheet converts a spreadsheet-shaped payload into the JSON + HTML table
// envelope consumed by callers. The notebook-runtime service does not expose a
// spreadsheet-render HTTP route today, so this is the explicit in-process adapter
// for CSV and simple JSON sheet payloads.
func RenderSheet(mime string, params json.RawMessage, src []byte) (HandlerOutput, error) {
	p := renderSheetParams{SheetName: "Sheet1"}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return HandlerOutput{}, invalidParams("render_sheet", err.Error())
		}
	}
	p.SheetName = strings.TrimSpace(p.SheetName)
	if p.SheetName == "" {
		p.SheetName = "Sheet1"
	}

	rows, columnCount, err := parseSheetRows(mime, p, src)
	if err != nil {
		return HandlerOutput{}, err
	}
	out := RenderSheetOutput{
		SheetName:   p.SheetName,
		Rows:        rows,
		HTML:        renderRowsHTML(p.SheetName, rows),
		RowCount:    len(rows),
		ColumnCount: columnCount,
	}
	return HandlerOutput{OutputMimeType: "application/json", OutputJSON: out}, nil
}

func parseSheetRows(mime string, p renderSheetParams, src []byte) ([]map[string]any, int, error) {
	switch mime {
	case "text/csv", "application/csv", "application/vnd.ms-excel", "text/plain":
		return parseCSVSheet(p, src)
	case "application/json":
		return parseJSONSheet(src)
	default:
		return nil, 0, fmt.Errorf("%w `%s` for transformation `render_sheet`", ErrUnsupportedMime, mime)
	}
}

func parseCSVSheet(p renderSheetParams, src []byte) ([]map[string]any, int, error) {
	r := csv.NewReader(bytes.NewReader(src))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		if err == io.EOF {
			return []map[string]any{}, 0, nil
		}
		return nil, 0, fmt.Errorf("%w: %s", ErrDecode, err.Error())
	}
	if len(records) == 0 {
		return []map[string]any{}, 0, nil
	}
	hasHeader := true
	if p.HasHeader != nil {
		hasHeader = *p.HasHeader
	}
	columnCount := maxRecordLen(records)
	headers := make([]string, columnCount)
	start := 0
	if hasHeader {
		for i := range headers {
			headers[i] = headerName(records[0], i)
		}
		start = 1
	} else {
		for i := range headers {
			headers[i] = fmt.Sprintf("column_%d", i+1)
		}
	}
	rows := make([]map[string]any, 0, len(records)-start)
	for _, record := range records[start:] {
		row := make(map[string]any, columnCount)
		for i, header := range headers {
			value := ""
			if i < len(record) {
				value = record[i]
			}
			row[header] = value
		}
		rows = append(rows, row)
	}
	return rows, columnCount, nil
}

func parseJSONSheet(src []byte) ([]map[string]any, int, error) {
	var objectRows []map[string]any
	if err := json.Unmarshal(src, &objectRows); err == nil {
		return objectRows, countJSONColumns(objectRows), nil
	}
	var matrix [][]any
	if err := json.Unmarshal(src, &matrix); err != nil {
		return nil, 0, fmt.Errorf("%w: %s", ErrDecode, err.Error())
	}
	if len(matrix) == 0 {
		return []map[string]any{}, 0, nil
	}
	columnCount := maxMatrixLen(matrix)
	headers := make([]string, columnCount)
	for i := range headers {
		headers[i] = fmt.Sprintf("column_%d", i+1)
	}
	rows := make([]map[string]any, 0, len(matrix))
	for _, values := range matrix {
		row := make(map[string]any, columnCount)
		for i, header := range headers {
			var value any = ""
			if i < len(values) {
				value = values[i]
			}
			row[header] = value
		}
		rows = append(rows, row)
	}
	return rows, columnCount, nil
}

func renderRowsHTML(title string, rows []map[string]any) string {
	var b strings.Builder
	b.WriteString(`<table data-sheet="`)
	b.WriteString(html.EscapeString(title))
	b.WriteString(`"><thead><tr>`)
	headers := orderedHeaders(rows)
	for _, h := range headers {
		b.WriteString(`<th>`)
		b.WriteString(html.EscapeString(h))
		b.WriteString(`</th>`)
	}
	b.WriteString(`</tr></thead><tbody>`)
	for _, row := range rows {
		b.WriteString(`<tr>`)
		for _, h := range headers {
			b.WriteString(`<td>`)
			b.WriteString(html.EscapeString(fmt.Sprint(row[h])))
			b.WriteString(`</td>`)
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table>`)
	return b.String()
}

func headerName(record []string, idx int) string {
	if idx < len(record) {
		v := strings.TrimSpace(record[idx])
		if v != "" {
			return v
		}
	}
	return fmt.Sprintf("column_%d", idx+1)
}

func maxRecordLen(records [][]string) int {
	max := 0
	for _, r := range records {
		if len(r) > max {
			max = len(r)
		}
	}
	return max
}

func maxMatrixLen(records [][]any) int {
	max := 0
	for _, r := range records {
		if len(r) > max {
			max = len(r)
		}
	}
	return max
}

func countJSONColumns(rows []map[string]any) int {
	seen := map[string]struct{}{}
	for _, row := range rows {
		for key := range row {
			seen[key] = struct{}{}
		}
	}
	return len(seen)
}

func orderedHeaders(rows []map[string]any) []string {
	seen := map[string]struct{}{}
	for _, row := range rows {
		for key := range row {
			seen[key] = struct{}{}
		}
	}
	headers := make([]string, 0, len(seen))
	for key := range seen {
		headers = append(headers, key)
	}
	sort.Strings(headers)
	return headers
}
