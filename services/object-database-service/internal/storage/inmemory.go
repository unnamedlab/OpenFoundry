package storage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// InMemoryObjectStore is a concurrency-safe map fake. Drop-in for
// dev / unit tests. Mirror of `storage_abstraction::repositories::noop`.
type InMemoryObjectStore struct {
	mu         sync.Mutex
	rows       map[objectKey]Object
	aggregates map[string]MaterializedAggregate
	costs      []QueryCostRecord
	budgets    map[string]budgetWindow
}

type budgetWindow struct {
	used     uint64
	resetsAt time.Time
}

type objectKey struct {
	tenant TenantId
	id     ObjectId
}

func NewInMemoryObjectStore() *InMemoryObjectStore {
	return &InMemoryObjectStore{
		rows:       make(map[objectKey]Object),
		aggregates: make(map[string]MaterializedAggregate),
		budgets:    make(map[string]budgetWindow),
	}
}

func (s *InMemoryObjectStore) Get(_ context.Context, tenant TenantId, id ObjectId, _ ReadConsistency) (*Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.rows[objectKey{tenant, id}]
	if !ok {
		return nil, nil
	}
	cp := obj
	return &cp, nil
}

func (s *InMemoryObjectStore) GetByTypeAndPrimaryKey(ctx context.Context, tenant TenantId, typeID TypeId, primaryKey string, c ReadConsistency) (*Object, error) {
	obj, err := s.Get(ctx, tenant, ObjectId(primaryKey), c)
	if err != nil || obj == nil {
		return obj, err
	}
	if obj.TypeID != typeID {
		return nil, nil
	}
	return obj, nil
}

func (s *InMemoryObjectStore) Put(_ context.Context, obj Object, expectedVersion *uint64) (PutOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := objectKey{obj.Tenant, obj.ID}
	existing, present := s.rows[key]

	switch {
	case !present && (expectedVersion == nil || *expectedVersion == 0):
		toInsert := obj
		toInsert.Version = 1
		s.rows[key] = toInsert
		return PutOutcome{Kind: PutInserted}, nil
	case !present:
		return PutOutcome{
			Kind:            PutVersionConflict,
			ExpectedVersion: *expectedVersion,
			ActualVersion:   0,
		}, nil
	default:
		if expectedVersion != nil && *expectedVersion != existing.Version {
			return PutOutcome{
				Kind:            PutVersionConflict,
				ExpectedVersion: *expectedVersion,
				ActualVersion:   existing.Version,
			}, nil
		}
		newVersion := existing.Version + 1
		toUpdate := obj
		toUpdate.Version = newVersion
		s.rows[key] = toUpdate
		return PutOutcome{
			Kind:            PutUpdated,
			PreviousVersion: existing.Version,
			NewVersion:      newVersion,
		}, nil
	}
}

func (s *InMemoryObjectStore) Delete(_ context.Context, tenant TenantId, id ObjectId) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := objectKey{tenant, id}
	if _, ok := s.rows[key]; !ok {
		return false, nil
	}
	delete(s.rows, key)
	return true, nil
}

func (s *InMemoryObjectStore) snapshot() []Object {
	out := make([]Object, 0, len(s.rows))
	for _, v := range s.rows {
		out = append(out, v)
	}
	return out
}

func (s *InMemoryObjectStore) listFiltered(filter func(Object) bool, page Page) PagedResult[Object] {
	s.mu.Lock()
	rows := s.snapshot()
	s.mu.Unlock()

	items := make([]Object, 0, len(rows))
	for _, o := range rows {
		if filter(o) {
			items = append(items, o)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].UpdatedAtMs > items[j].UpdatedAtMs
	})
	limit := int(page.Size)
	if limit < 1 {
		limit = 1
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return PagedResult[Object]{Items: items, NextToken: nil}
}

func (s *InMemoryObjectStore) ListByType(_ context.Context, tenant TenantId, typeID TypeId, page Page, _ ReadConsistency) (PagedResult[Object], error) {
	return s.listFiltered(func(o Object) bool {
		return o.Tenant == tenant && o.TypeID == typeID
	}, page), nil
}

func (s *InMemoryObjectStore) ListByOwner(_ context.Context, tenant TenantId, owner OwnerId, page Page, _ ReadConsistency) (PagedResult[Object], error) {
	return s.listFiltered(func(o Object) bool {
		return o.Tenant == tenant && o.Owner != nil && *o.Owner == owner
	}, page), nil
}

func (s *InMemoryObjectStore) ListByMarking(_ context.Context, tenant TenantId, marking MarkingId, page Page, _ ReadConsistency) (PagedResult[Object], error) {
	return s.listFiltered(func(o Object) bool {
		if o.Tenant != tenant {
			return false
		}
		for _, m := range o.Markings {
			if m == marking {
				return true
			}
		}
		return false
	}, page), nil
}

// InMemoryLinkStore is the link-side counterpart.
type InMemoryLinkStore struct {
	mu   sync.Mutex
	rows []Link
}

func NewInMemoryLinkStore() *InMemoryLinkStore {
	return &InMemoryLinkStore{}
}

func (s *InMemoryLinkStore) Put(_ context.Context, link Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range s.rows {
		if l.Tenant == link.Tenant && l.LinkType == link.LinkType && l.From == link.From && l.To == link.To {
			return nil
		}
	}
	s.rows = append(s.rows, link)
	return nil
}

func (s *InMemoryLinkStore) Delete(_ context.Context, tenant TenantId, lt LinkTypeId, from, to ObjectId) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := len(s.rows)
	kept := s.rows[:0]
	for _, l := range s.rows {
		if l.Tenant == tenant && l.LinkType == lt && l.From == from && l.To == to {
			continue
		}
		kept = append(kept, l)
	}
	s.rows = kept
	return len(s.rows) != before, nil
}

func (s *InMemoryLinkStore) listLinks(filter func(Link) bool, cursorKey func(Link) string, page Page) PagedResult[Link] {
	s.mu.Lock()
	rows := append([]Link(nil), s.rows...)
	s.mu.Unlock()

	items := make([]Link, 0, len(rows))
	for _, l := range rows {
		if filter(l) {
			items = append(items, l)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if cursorKey(items[i]) != cursorKey(items[j]) {
			return cursorKey(items[i]) < cursorKey(items[j])
		}
		return items[i].CreatedAtMs < items[j].CreatedAtMs
	})

	after := decodeInMemoryLinkCursor(page.Token)
	start := 0
	if after != "" {
		for start < len(items) && cursorKey(items[start]) <= after {
			start++
		}
	}
	limit := int(page.Size)
	if limit < 1 {
		limit = 1
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	var next *string
	if end < len(items) && end > start {
		next = encodeInMemoryLinkCursor(cursorKey(items[end-1]))
	}
	return PagedResult[Link]{Items: items[start:end], NextToken: next}
}

func encodeInMemoryLinkCursor(value string) *string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte("link:v1:" + value))
	return &encoded
}

func decodeInMemoryLinkCursor(token *string) string {
	if token == nil || strings.TrimSpace(*token) == "" {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(*token)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(raw)), "link:v1:")
}

// DeleteIncident removes every link whose `from` or `to` equals `id`.
// Implements the cascade hook used by the ontology DeleteObject
// handler so unit / integration tests on the in-memory store exercise
// the same surface the production indexer is expected to emit.
func (s *InMemoryLinkStore) DeleteIncident(_ context.Context, tenant TenantId, id ObjectId) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := len(s.rows)
	kept := s.rows[:0]
	for _, l := range s.rows {
		if l.Tenant == tenant && (l.From == id || l.To == id) {
			continue
		}
		kept = append(kept, l)
	}
	s.rows = kept
	return before - len(s.rows), nil
}

func (s *InMemoryLinkStore) ListOutgoing(_ context.Context, tenant TenantId, lt LinkTypeId, from ObjectId, page Page, _ ReadConsistency) (PagedResult[Link], error) {
	return s.listLinks(func(l Link) bool {
		return l.Tenant == tenant && l.LinkType == lt && l.From == from
	}, func(l Link) string { return string(l.To) }, page), nil
}

func (s *InMemoryLinkStore) ListIncoming(_ context.Context, tenant TenantId, lt LinkTypeId, to ObjectId, page Page, _ ReadConsistency) (PagedResult[Link], error) {
	return s.listLinks(func(l Link) bool {
		return l.Tenant == tenant && l.LinkType == lt && l.To == to
	}, func(l Link) string { return string(l.From) }, page), nil
}

func (s *InMemoryObjectStore) QueryByProperty(_ context.Context, tenant TenantId, typeID TypeId, predicate PropertyPredicate, page Page, _ ReadConsistency) (PagedResult[Object], error) {
	return s.listFiltered(func(o Object) bool {
		if o.Tenant != tenant || o.TypeID != typeID {
			return false
		}
		props := map[string]any{}
		if len(o.Payload) > 0 {
			_ = json.Unmarshal(o.Payload, &props)
		}
		return matchesPropertyPredicate(props[predicate.PropertyName], predicate)
	}, page), nil
}

func matchesPropertyPredicate(actual any, predicate PropertyPredicate) bool {
	op := strings.ToLower(strings.TrimSpace(predicate.Operator))
	if op == "" {
		op = "equals"
	}
	switch op {
	case "equals", "eq", "=":
		return comparePropertyValues(actual, predicate.Value) == 0
	case "not_equals", "neq", "!=":
		return comparePropertyValues(actual, predicate.Value) != 0
	case "contains":
		return strings.Contains(strings.ToLower(strings.TrimSpace(fmt.Sprint(actual))), strings.ToLower(strings.TrimSpace(fmt.Sprint(predicate.Value))))
	case "starts_with", "prefix":
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(fmt.Sprint(actual))), strings.ToLower(strings.TrimSpace(fmt.Sprint(predicate.Value))))
	case "gte", ">=":
		return comparePropertyValues(actual, predicate.Value) >= 0
	case "lte", "<=":
		return comparePropertyValues(actual, predicate.Value) <= 0
	case "gt", ">":
		return comparePropertyValues(actual, predicate.Value) > 0
	case "lt", "<":
		return comparePropertyValues(actual, predicate.Value) < 0
	case "in":
		switch values := predicate.Value.(type) {
		case []any:
			for _, value := range values {
				if comparePropertyValues(actual, value) == 0 {
					return true
				}
			}
		case []string:
			for _, value := range values {
				if comparePropertyValues(actual, value) == 0 {
					return true
				}
			}
		}
	}
	return false
}

func comparePropertyValues(a any, b any) int {
	af, aok := propertyNumber(a)
	bf, bok := propertyNumber(b)
	if aok && bok {
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		default:
			return 0
		}
	}
	as := strings.ToLower(strings.TrimSpace(fmt.Sprint(a)))
	bs := strings.ToLower(strings.TrimSpace(fmt.Sprint(b)))
	return strings.Compare(as, bs)
}

func propertyNumber(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint64:
		return float64(t), true
	case uint32:
		return float64(t), true
	case json.Number:
		n, err := t.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

// PropertyHistogram computes the current OSV2.21 per-property histogram from
// the in-memory rows. Production stores persist the same shape beside property
// indexes; the fake recomputes so tests exercise planner semantics without a
// background compactor.
func (s *InMemoryObjectStore) PropertyHistogram(_ context.Context, tenant TenantId, typeID TypeId, propertyName string) (PropertyHistogram, bool, error) {
	propertyName = strings.TrimSpace(propertyName)
	if propertyName == "" {
		return PropertyHistogram{}, false, nil
	}
	s.mu.Lock()
	rows := s.snapshot()
	s.mu.Unlock()
	counts := map[string]uint64{}
	var total, nulls uint64
	for _, obj := range rows {
		if obj.Tenant != tenant || obj.TypeID != typeID {
			continue
		}
		total++
		props := map[string]any{}
		if len(obj.Payload) > 0 {
			_ = json.Unmarshal(obj.Payload, &props)
		}
		value, ok := props[propertyName]
		if !ok || value == nil || strings.TrimSpace(normalizeHistogramValue(value)) == "" {
			nulls++
			continue
		}
		counts[normalizeHistogramValue(value)]++
	}
	if total == 0 {
		return PropertyHistogram{}, false, nil
	}
	buckets := make([]HistogramBucket, 0, len(counts))
	for value, count := range counts {
		buckets = append(buckets, HistogramBucket{Value: value, Count: count})
	}
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].Count != buckets[j].Count {
			return buckets[i].Count > buckets[j].Count
		}
		return buckets[i].Value < buckets[j].Value
	})
	if len(buckets) > 128 {
		buckets = buckets[:128]
	}
	return PropertyHistogram{Tenant: tenant, TypeID: typeID, PropertyName: propertyName, TotalRows: total, NullRows: nulls, Distinct: uint64(len(counts)), Buckets: buckets, UpdatedAt: time.Now().UTC()}, true, nil
}

// RefreshStatistics is a no-op for the in-memory store because histograms are
// recomputed from current rows on demand. Cassandra implementations refresh
// persisted histograms after bulk writes.
func (s *InMemoryObjectStore) RefreshStatistics(_ context.Context, _ TenantId, _ TypeId) error {
	return nil
}

func aggregateKey(tenant TenantId, typeID TypeId, fn, property, groupBy string) string {
	return string(tenant) + "\x00" + string(typeID) + "\x00" + strings.ToLower(strings.TrimSpace(fn)) + "\x00" + strings.TrimSpace(property) + "\x00" + strings.TrimSpace(groupBy)
}

func (s *InMemoryObjectStore) DeclareMaterializedAggregate(_ context.Context, aggregate MaterializedAggregate) error {
	aggregate.Function = strings.ToLower(strings.TrimSpace(aggregate.Function))
	if aggregate.Name == "" {
		aggregate.Name = aggregateKey(aggregate.Tenant, aggregate.TypeID, aggregate.Function, aggregate.PropertyName, aggregate.GroupBy)
	}
	aggregate.UpdatedAt = time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aggregates[aggregateKey(aggregate.Tenant, aggregate.TypeID, aggregate.Function, aggregate.PropertyName, aggregate.GroupBy)] = aggregate
	return nil
}

func (s *InMemoryObjectStore) ReadMaterializedAggregate(_ context.Context, tenant TenantId, typeID TypeId, function, propertyName, groupBy string) (MaterializedAggregateResult, bool, error) {
	key := aggregateKey(tenant, typeID, function, propertyName, groupBy)
	s.mu.Lock()
	agg, ok := s.aggregates[key]
	rows := s.snapshot()
	s.mu.Unlock()
	if !ok {
		return MaterializedAggregateResult{}, false, nil
	}
	var count uint64
	var total float64
	groups := map[string]any{}
	groupCounts := map[string]uint64{}
	for _, obj := range rows {
		if obj.Tenant != tenant || obj.TypeID != typeID {
			continue
		}
		props := map[string]any{}
		if len(obj.Payload) > 0 {
			_ = json.Unmarshal(obj.Payload, &props)
		}
		group := ""
		if groupBy != "" {
			group = normalizeHistogramValue(props[groupBy])
			if group == "" {
				group = "(null)"
			}
		}
		switch strings.ToLower(strings.TrimSpace(function)) {
		case "count":
			count++
			if groupBy != "" {
				groupCounts[group]++
			}
		case "sum":
			if n, ok := materializedNumber(props[propertyName]); ok {
				count++
				total += n
				if groupBy != "" {
					cur, _ := groups[group].(float64)
					groups[group] = cur + n
				}
			}
		}
	}
	if groupBy != "" && strings.EqualFold(function, "count") {
		for group, value := range groupCounts {
			groups[group] = value
		}
	}
	var value any = count
	if strings.EqualFold(function, "sum") {
		value = total
	}
	return MaterializedAggregateResult{Aggregate: agg, Value: value, Groups: groups, Count: count}, true, nil
}

func materializedNumber(v any) (float64, bool) {
	switch t := v.(type) {
	case json.Number:
		n, err := t.Float64()
		return n, err == nil
	case float64:
		return t, true
	case int:
		return float64(t), true
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func (s *InMemoryObjectStore) RecordQueryCost(_ context.Context, record QueryCostRecord) error {
	record.RecordedAt = time.Now().UTC()
	record.WallTimeMs = float64(record.WallTime.Microseconds()) / 1000
	s.mu.Lock()
	defer s.mu.Unlock()
	s.costs = append(s.costs, record)
	return nil
}

func (s *InMemoryObjectStore) QueryCostSummary(_ context.Context, tenant TenantId, projectID string) (QueryCostSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	summary := QueryCostSummary{Tenant: tenant, ProjectID: projectID}
	indices := map[string]bool{}
	for _, record := range s.costs {
		if record.Tenant != tenant || (projectID != "" && record.ProjectID != projectID) {
			continue
		}
		summary.QueryCount++
		summary.RowsScanned += record.RowsScanned
		summary.RowsReturned += record.RowsReturned
		summary.WallTimeMs += float64(record.WallTime.Microseconds()) / 1000
		for _, index := range record.IndicesHit {
			indices[index] = true
		}
	}
	for index := range indices {
		summary.IndicesHit = append(summary.IndicesHit, index)
	}
	sort.Strings(summary.IndicesHit)
	return summary, nil
}

func (s *InMemoryObjectStore) ReserveQueryBudget(_ context.Context, tenant TenantId, projectID, callerID string, units uint64) (QueryBudget, error) {
	const defaultLimit = uint64(10000000)
	window := time.Hour
	if units == 0 {
		units = 1
	}
	key := string(tenant) + "\x00" + projectID + "\x00" + callerID
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.budgets[key]
	if entry.resetsAt.IsZero() || now.After(entry.resetsAt) {
		entry = budgetWindow{resetsAt: now.Add(window)}
	}
	if entry.used+units > defaultLimit {
		retry := time.Until(entry.resetsAt)
		budget := QueryBudget{Limit: defaultLimit, Used: entry.used, Window: window, WindowMs: int64(window / time.Millisecond), ResetsAt: entry.resetsAt, SoftWarning: true, RetryAfter: retry, RetryAfterMs: int64(retry / time.Millisecond)}
		return budget, &RepoError{Kind: ErrBackend, Msg: "OSV2 query budget exceeded; retry after " + retry.String()}
	}
	entry.used += units
	s.budgets[key] = entry
	return QueryBudget{Limit: defaultLimit, Used: entry.used, Window: window, WindowMs: int64(window / time.Millisecond), ResetsAt: entry.resetsAt, SoftWarning: entry.used*100 >= defaultLimit*80}, nil
}

// LinkFanout computes OSV2.21 fan-out distributions for join ordering.
func (s *InMemoryLinkStore) LinkFanout(_ context.Context, tenant TenantId, linkType LinkTypeId, direction string) (LinkFanoutDistribution, bool, error) {
	direction = strings.ToLower(strings.TrimSpace(direction))
	if direction == "" {
		direction = "outgoing"
	}
	s.mu.Lock()
	rows := append([]Link(nil), s.rows...)
	s.mu.Unlock()
	fanout := map[ObjectId]uint64{}
	var edges uint64
	for _, link := range rows {
		if link.Tenant != tenant || link.LinkType != linkType {
			continue
		}
		edges++
		if direction == "incoming" {
			fanout[link.To]++
		} else {
			fanout[link.From]++
		}
	}
	if edges == 0 {
		return LinkFanoutDistribution{}, false, nil
	}
	values := make([]uint64, 0, len(fanout))
	var max uint64
	for _, value := range fanout {
		values = append(values, value)
		if value > max {
			max = value
		}
	}
	return LinkFanoutDistribution{Tenant: tenant, LinkType: linkType, Direction: direction, SourceNodes: uint64(len(fanout)), EdgeCount: edges, P50: percentileUint64(append([]uint64(nil), values...), 0.50), P95: percentileUint64(append([]uint64(nil), values...), 0.95), Max: max, UpdatedAt: time.Now().UTC()}, true, nil
}

func (s *InMemoryLinkStore) RefreshLinkStatistics(_ context.Context, _ TenantId, _ LinkTypeId) error {
	return nil
}
