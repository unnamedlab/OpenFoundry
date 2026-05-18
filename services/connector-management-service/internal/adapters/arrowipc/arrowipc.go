// Package arrowipc converts adapter JSON preview results into real Apache
// Arrow IPC stream bytes.
package arrowipc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
)

// MaterializeResult serializes the supplied adapter result as a single Arrow
// IPC stream containing one record batch. Columns are taken from Result.Columns
// first and then extended with any object keys found in Result.Rows.
func MaterializeResult(res *adapters.Result) ([]byte, error) {
	if res == nil {
		return nil, fmt.Errorf("arrowipc: result is nil")
	}
	rows, columns, err := decodeRows(res.Columns, res.Rows)
	if err != nil {
		return nil, err
	}
	return materialize(columns, rows)
}

// SingleFrameStream adapts one complete IPC stream buffer to the adapters
// ArrowStream pull interface.
type SingleFrameStream struct {
	Frame    []byte
	consumed bool
}

// Next returns the IPC bytes once, then io.EOF.
func (s *SingleFrameStream) Next(context.Context) ([]byte, error) {
	if s.consumed {
		return nil, io.EOF
	}
	s.consumed = true
	return append([]byte(nil), s.Frame...), nil
}

// Close releases no resources; the frame is already materialized.
func (s *SingleFrameStream) Close() error { return nil }

type rowMap map[string]any

type columnKind int

const (
	kindNull columnKind = iota
	kindBool
	kindInt
	kindFloat
	kindTimestamp
	kindString
)

func decodeRows(seedColumns []string, rawRows []json.RawMessage) ([]rowMap, []string, error) {
	columns := append([]string(nil), seedColumns...)
	seen := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		seen[column] = struct{}{}
	}

	rows := make([]rowMap, 0, len(rawRows))
	for i, raw := range rawRows {
		var decoded any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&decoded); err != nil {
			return nil, nil, fmt.Errorf("arrowipc: decode row %d: %w", i, err)
		}
		row, ok := decoded.(map[string]any)
		if !ok {
			row = rowMap{"value": decoded}
			if _, exists := seen["value"]; !exists {
				seen["value"] = struct{}{}
				columns = append(columns, "value")
			}
			rows = append(rows, row)
			continue
		}
		if row == nil {
			row = rowMap{}
		}
		for _, column := range orderedKeys(raw, row) {
			if _, ok := seen[column]; ok {
				continue
			}
			seen[column] = struct{}{}
			columns = append(columns, column)
		}
		rows = append(rows, row)
	}
	return rows, columns, nil
}

func orderedKeys(raw json.RawMessage, row rowMap) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil
	}
	out := make([]string, 0, len(row))
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		key, ok := tok.(string)
		if !ok {
			break
		}
		out = append(out, key)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			break
		}
	}
	return out
}

func materialize(columns []string, rows []rowMap) ([]byte, error) {
	mem := memory.NewGoAllocator()
	fields := make([]arrow.Field, 0, len(columns))
	arrays := make([]arrow.Array, 0, len(columns))
	for _, column := range columns {
		kind := inferKind(column, rows)
		field, arr, err := buildArray(mem, column, kind, rows)
		if err != nil {
			for _, existing := range arrays {
				existing.Release()
			}
			return nil, err
		}
		fields = append(fields, field)
		arrays = append(arrays, arr)
	}

	schema := arrow.NewSchema(fields, nil)
	rec := array.NewRecord(schema, arrays, int64(len(rows)))
	defer rec.Release()
	for _, arr := range arrays {
		arr.Release()
	}

	var buf bytes.Buffer
	writer := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(mem))
	if err := writer.Write(rec); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("arrowipc: write record batch: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("arrowipc: close stream: %w", err)
	}
	return buf.Bytes(), nil
}

func inferKind(column string, rows []rowMap) columnKind {
	kind := kindNull
	for _, row := range rows {
		value, ok := row[column]
		if !ok || value == nil {
			continue
		}
		valueKind := valueKind(value)
		if kind == kindNull {
			kind = valueKind
			continue
		}
		kind = mergeKind(kind, valueKind)
	}
	return kind
}

func valueKind(value any) columnKind {
	switch v := value.(type) {
	case bool:
		return kindBool
	case json.Number:
		if _, err := v.Int64(); err == nil {
			return kindInt
		}
		if _, err := v.Float64(); err == nil {
			return kindFloat
		}
		return kindString
	case float64:
		if math.Trunc(v) == v {
			return kindInt
		}
		return kindFloat
	case string:
		if _, ok := parseTimestamp(v); ok {
			return kindTimestamp
		}
		return kindString
	default:
		return kindString
	}
}

func mergeKind(left, right columnKind) columnKind {
	if left == right {
		return left
	}
	if left == kindNull {
		return right
	}
	if right == kindNull {
		return left
	}
	if (left == kindInt && right == kindFloat) || (left == kindFloat && right == kindInt) {
		return kindFloat
	}
	return kindString
}

func buildArray(mem memory.Allocator, column string, kind columnKind, rows []rowMap) (arrow.Field, arrow.Array, error) {
	switch kind {
	case kindNull:
		builder := array.NewNullBuilder(mem)
		defer builder.Release()
		builder.AppendNulls(len(rows))
		return arrow.Field{Name: column, Type: arrow.Null, Nullable: true}, builder.NewArray(), nil
	case kindBool:
		builder := array.NewBooleanBuilder(mem)
		defer builder.Release()
		for _, row := range rows {
			if v, ok := row[column].(bool); ok {
				builder.Append(v)
			} else {
				builder.AppendNull()
			}
		}
		return arrow.Field{Name: column, Type: arrow.FixedWidthTypes.Boolean, Nullable: true}, builder.NewArray(), nil
	case kindInt:
		builder := array.NewInt64Builder(mem)
		defer builder.Release()
		for _, row := range rows {
			v, ok := int64Value(row[column])
			if !ok {
				builder.AppendNull()
				continue
			}
			builder.Append(v)
		}
		return arrow.Field{Name: column, Type: arrow.PrimitiveTypes.Int64, Nullable: true}, builder.NewArray(), nil
	case kindFloat:
		builder := array.NewFloat64Builder(mem)
		defer builder.Release()
		for _, row := range rows {
			v, ok := float64Value(row[column])
			if !ok {
				builder.AppendNull()
				continue
			}
			builder.Append(v)
		}
		return arrow.Field{Name: column, Type: arrow.PrimitiveTypes.Float64, Nullable: true}, builder.NewArray(), nil
	case kindTimestamp:
		dtype := &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
		builder := array.NewTimestampBuilder(mem, dtype)
		defer builder.Release()
		for _, row := range rows {
			v, ok := row[column].(string)
			if !ok {
				builder.AppendNull()
				continue
			}
			parsed, ok := parseTimestamp(v)
			if !ok {
				builder.AppendNull()
				continue
			}
			builder.AppendTime(parsed.UTC())
		}
		return arrow.Field{Name: column, Type: dtype, Nullable: true}, builder.NewArray(), nil
	default:
		builder := array.NewStringBuilder(mem)
		defer builder.Release()
		for _, row := range rows {
			v, ok := stringValue(row[column])
			if !ok {
				builder.AppendNull()
				continue
			}
			builder.Append(v)
		}
		return arrow.Field{Name: column, Type: arrow.BinaryTypes.String, Nullable: true}, builder.NewArray(), nil
	}
}

func int64Value(value any) (int64, bool) {
	switch v := value.(type) {
	case nil:
		return 0, false
	case json.Number:
		out, err := v.Int64()
		return out, err == nil
	case float64:
		if math.Trunc(v) != v {
			return 0, false
		}
		return int64(v), true
	default:
		return 0, false
	}
}

func float64Value(value any) (float64, bool) {
	switch v := value.(type) {
	case nil:
		return 0, false
	case json.Number:
		out, err := v.Float64()
		return out, err == nil
	case float64:
		return v, true
	default:
		return 0, false
	}
}

func stringValue(value any) (string, bool) {
	if value == nil {
		return "", false
	}
	switch v := value.(type) {
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case bool:
		return strconv.FormatBool(v), true
	default:
		buf, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v), true
		}
		return string(buf), true
	}
}

func parseTimestamp(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		parsed, err := time.Parse(layout, trimmed)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
