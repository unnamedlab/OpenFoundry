package storageabstraction

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GeoPoint is a WGS84 coordinate used by OSV2.13 spatial indices.
type GeoPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// BoundingBox is an inclusive lon/lat bounding box.
type BoundingBox struct {
	MinLat float64 `json:"min_lat"`
	MinLon float64 `json:"min_lon"`
	MaxLat float64 `json:"max_lat"`
	MaxLon float64 `json:"max_lon"`
}

// SpatialPredicateKind selects the OSV2.13 spatial predicate.
type SpatialPredicateKind string

const (
	SpatialBoundingBox     SpatialPredicateKind = "bounding_box"
	SpatialRadius          SpatialPredicateKind = "radius"
	SpatialPolygonContains SpatialPredicateKind = "polygon_contains"
)

// SpatialQuery is the backend-neutral OSV2.13 Map/Vertex spatial pushdown shape.
type SpatialQuery struct {
	Tenant       TenantId             `json:"tenant"`
	TypeID       TypeId               `json:"type_id"`
	Property     string               `json:"property"`
	Predicate    SpatialPredicateKind `json:"predicate"`
	BoundingBox  *BoundingBox         `json:"bounding_box,omitempty"`
	Center       *GeoPoint            `json:"center,omitempty"`
	RadiusMeters float64              `json:"radius_meters,omitempty"`
	Polygon      []GeoPoint           `json:"polygon,omitempty"`
	Page         Page                 `json:"page"`
}

// SpatialIndexStore is implemented by R-tree/H3/S2-backed stores that can push
// Map and Vertex spatial predicates into the index layer.
type SpatialIndexStore interface {
	IndexSpatial(ctx context.Context, tenant TenantId, typeID TypeId, objectID ObjectId, property string, point GeoPoint, version uint64) error
	SearchSpatial(ctx context.Context, query SpatialQuery, consistency ReadConsistency) (PagedResult[SearchHit], error)
}

// TimeSeriesSample is one OSV2.14 per-tick property value.
type TimeSeriesSample struct {
	Tenant      TenantId `json:"tenant"`
	TypeID      TypeId   `json:"type_id"`
	ObjectID    ObjectId `json:"object_id"`
	Property    string   `json:"property"`
	TimestampMs int64    `json:"timestamp_ms"`
	Value       float64  `json:"value"`
	Quality     *string  `json:"quality,omitempty"`
}

// TimeSeriesAggregation selects fetch-time aggregation/downsampling behavior.
type TimeSeriesAggregation string

const (
	TimeSeriesRaw        TimeSeriesAggregation = "raw"
	TimeSeriesMin        TimeSeriesAggregation = "min"
	TimeSeriesMax        TimeSeriesAggregation = "max"
	TimeSeriesAvg        TimeSeriesAggregation = "avg"
	TimeSeriesPercentile TimeSeriesAggregation = "percentile"
)

// TimeSeriesQuery fetches samples for a time-series object property.
type TimeSeriesQuery struct {
	Tenant            TenantId              `json:"tenant"`
	TypeID            TypeId                `json:"type_id"`
	ObjectID          ObjectId              `json:"object_id"`
	Property          string                `json:"property"`
	StartMs           int64                 `json:"start_ms"`
	EndMs             int64                 `json:"end_ms"`
	Aggregation       TimeSeriesAggregation `json:"aggregation,omitempty"`
	Percentile        float64               `json:"percentile,omitempty"`
	DownsampleEveryMs int64                 `json:"downsample_every_ms,omitempty"`
}

// TimeSeriesBucket is an aggregated/downsampled result bucket.
type TimeSeriesBucket struct {
	StartMs int64   `json:"start_ms"`
	EndMs   int64   `json:"end_ms"`
	Value   float64 `json:"value"`
	Count   int     `json:"count"`
}

// TimeSeriesResult returns either raw samples or aggregate buckets.
type TimeSeriesResult struct {
	Samples []TimeSeriesSample `json:"samples,omitempty"`
	Buckets []TimeSeriesBucket `json:"buckets,omitempty"`
}

// TimeSeriesPropertyStore is the OSV2.14 columnar property store contract.
type TimeSeriesPropertyStore interface {
	PutTimeSeriesSamples(ctx context.Context, samples []TimeSeriesSample) error
	QueryTimeSeries(ctx context.Context, query TimeSeriesQuery, consistency ReadConsistency) (TimeSeriesResult, error)
}

// BranchID identifies a Global Branching overlay.
type BranchID string

// BranchOverlayStore stores OSV2.15/OSV2.16 branch-only object/link deltas.
type BranchOverlayStore interface {
	PutBranchObject(ctx context.Context, branch BranchID, obj Object, expectedVersion *uint64) (PutOutcome, error)
	DeleteBranchObject(ctx context.Context, branch BranchID, tenant TenantId, id ObjectId) (bool, error)
	GetBranchObject(ctx context.Context, branch BranchID, tenant TenantId, id ObjectId, main ObjectStore, consistency ReadConsistency) (*Object, error)
	ListBranchObjectsByType(ctx context.Context, branch BranchID, tenant TenantId, typeID TypeId, main ObjectStore, page Page, consistency ReadConsistency) (PagedResult[Object], error)
	QueryBranchObjectsByProperty(ctx context.Context, branch BranchID, tenant TenantId, typeID TypeId, predicate PropertyPredicate, main ObjectStore, page Page, consistency ReadConsistency) (PagedResult[Object], error)
	PutBranchLink(ctx context.Context, branch BranchID, link Link) error
	DeleteBranchLink(ctx context.Context, branch BranchID, tenant TenantId, linkType LinkTypeId, from, to ObjectId) (bool, error)
}

// ChangeKind is the object/link change discriminator used by OSV2.17 streams.
type ChangeKind string

const (
	ChangeObjectUpsert ChangeKind = "object_upsert"
	ChangeObjectDelete ChangeKind = "object_delete"
	ChangeLinkUpsert   ChangeKind = "link_upsert"
	ChangeLinkDelete   ChangeKind = "link_delete"
	ChangeRevoked      ChangeKind = "clearances_revoked"
)

// ChangeEvent is one resumable subscription event.
type ChangeEvent struct {
	Cursor     string          `json:"cursor"`
	Tenant     TenantId        `json:"tenant"`
	TypeID     TypeId          `json:"type_id,omitempty"`
	ObjectID   ObjectId        `json:"object_id,omitempty"`
	LinkType   LinkTypeId      `json:"link_type,omitempty"`
	Kind       ChangeKind      `json:"kind"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	OccurredMs int64           `json:"occurred_ms"`
}

// ChangePredicate selects a resumable OSV2.17 object/link subscription stream.
type ChangePredicate struct {
	Tenant      TenantId    `json:"tenant"`
	TypeID      *TypeId     `json:"type_id,omitempty"`
	ObjectID    *ObjectId   `json:"object_id,omitempty"`
	LinkType    *LinkTypeId `json:"link_type,omitempty"`
	SinceCursor string      `json:"since_cursor,omitempty"`
}

// ChangeAuthorizer enforces per-event visibility; returning false filters the
// event. Revocation is modeled by canceling the subscription context.
type ChangeAuthorizer func(ChangeEvent) bool

// ChangeSubscriptionStore is the OSV2.17 SSE-ready change stream contract.
type ChangeSubscriptionStore interface {
	PublishChange(ctx context.Context, event ChangeEvent) (string, error)
	SubscribeChanges(ctx context.Context, predicate ChangePredicate, authorize ChangeAuthorizer) (<-chan ChangeEvent, error)
}

type spatialEntry struct {
	tenant   TenantId
	typeID   TypeId
	objectID ObjectId
	property string
	point    GeoPoint
	version  uint64
}

type tsKey struct {
	tenant   TenantId
	typeID   TypeId
	objectID ObjectId
	property string
}

type branchObjectKey struct {
	branch BranchID
	tenant TenantId
	id     ObjectId
}

type branchLinkKey struct {
	branch   BranchID
	tenant   TenantId
	linkType LinkTypeId
	from     ObjectId
	to       ObjectId
}

type branchObjectOverlay struct {
	object  Object
	deleted bool
}

type subscription struct {
	predicate ChangePredicate
	authorize ChangeAuthorizer
	ch        chan ChangeEvent
}

// InMemoryOSV2AdvancedStore is a deterministic in-process implementation of
// OSV2.13–OSV2.17 contracts used by unit tests, local dev and Workshop demos.
type InMemoryOSV2AdvancedStore struct {
	mu            sync.Mutex
	spatial       map[string]spatialEntry
	timeSeries    map[tsKey][]TimeSeriesSample
	branchObjects map[branchObjectKey]branchObjectOverlay
	branchLinks   map[branchLinkKey]struct{}
	history       []ChangeEvent
	subscribers   map[int]subscription
	nextSub       int
	nextCursor    uint64
	snapshots     map[string]snapshotRecord
}

// NewInMemoryOSV2AdvancedStore returns an empty OSV2 advanced-index store.
func NewInMemoryOSV2AdvancedStore() *InMemoryOSV2AdvancedStore {
	return &InMemoryOSV2AdvancedStore{
		spatial:       map[string]spatialEntry{},
		timeSeries:    map[tsKey][]TimeSeriesSample{},
		branchObjects: map[branchObjectKey]branchObjectOverlay{},
		branchLinks:   map[branchLinkKey]struct{}{},
		subscribers:   map[int]subscription{},
		snapshots:     map[string]snapshotRecord{},
	}
}

var _ SpatialIndexStore = (*InMemoryOSV2AdvancedStore)(nil)
var _ TimeSeriesPropertyStore = (*InMemoryOSV2AdvancedStore)(nil)
var _ BranchOverlayStore = (*InMemoryOSV2AdvancedStore)(nil)
var _ ChangeSubscriptionStore = (*InMemoryOSV2AdvancedStore)(nil)

func (s *InMemoryOSV2AdvancedStore) IndexSpatial(_ context.Context, tenant TenantId, typeID TypeId, objectID ObjectId, property string, point GeoPoint, version uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := spatialKey(tenant, typeID, objectID, property)
	if existing, ok := s.spatial[key]; ok && existing.version > version {
		return nil
	}
	s.spatial[key] = spatialEntry{tenant: tenant, typeID: typeID, objectID: objectID, property: property, point: point, version: version}
	return nil
}

func (s *InMemoryOSV2AdvancedStore) SearchSpatial(_ context.Context, query SpatialQuery, _ ReadConsistency) (PagedResult[SearchHit], error) {
	s.mu.Lock()
	entries := make([]spatialEntry, 0, len(s.spatial))
	for _, entry := range s.spatial {
		entries = append(entries, entry)
	}
	s.mu.Unlock()

	hits := []SearchHit{}
	for _, entry := range entries {
		if entry.tenant != query.Tenant || entry.typeID != query.TypeID || entry.property != query.Property {
			continue
		}
		if !spatialMatches(entry.point, query) {
			continue
		}
		hits = append(hits, SearchHit{ID: entry.objectID, TypeID: entry.typeID, Score: 1})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].ID < hits[j].ID })
	return pageHits(hits, query.Page), nil
}

func (s *InMemoryOSV2AdvancedStore) PutTimeSeriesSamples(_ context.Context, samples []TimeSeriesSample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sample := range samples {
		key := tsKey{tenant: sample.Tenant, typeID: sample.TypeID, objectID: sample.ObjectID, property: sample.Property}
		bucket := s.timeSeries[key]
		replaced := false
		for i := range bucket {
			if bucket[i].TimestampMs == sample.TimestampMs {
				bucket[i] = sample
				replaced = true
				break
			}
		}
		if !replaced {
			bucket = append(bucket, sample)
		}
		sort.Slice(bucket, func(i, j int) bool { return bucket[i].TimestampMs < bucket[j].TimestampMs })
		s.timeSeries[key] = bucket
	}
	return nil
}

func (s *InMemoryOSV2AdvancedStore) QueryTimeSeries(_ context.Context, query TimeSeriesQuery, _ ReadConsistency) (TimeSeriesResult, error) {
	s.mu.Lock()
	bucket := append([]TimeSeriesSample(nil), s.timeSeries[tsKey{tenant: query.Tenant, typeID: query.TypeID, objectID: query.ObjectID, property: query.Property}]...)
	s.mu.Unlock()

	filtered := []TimeSeriesSample{}
	for _, sample := range bucket {
		if sample.TimestampMs >= query.StartMs && sample.TimestampMs <= query.EndMs {
			filtered = append(filtered, sample)
		}
	}
	if query.Aggregation == "" || query.Aggregation == TimeSeriesRaw {
		return TimeSeriesResult{Samples: filtered}, nil
	}
	return TimeSeriesResult{Buckets: aggregateSamples(filtered, query)}, nil
}

func (s *InMemoryOSV2AdvancedStore) PutBranchObject(_ context.Context, branch BranchID, obj Object, expectedVersion *uint64) (PutOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := branchObjectKey{branch: branch, tenant: obj.Tenant, id: obj.ID}
	existing, present := s.branchObjects[key]
	if !present || existing.deleted {
		if expectedVersion != nil && *expectedVersion != 0 {
			return PutOutcome{Kind: PutVersionConflict, ExpectedVersion: *expectedVersion, ActualVersion: 0}, nil
		}
		toStore := obj
		if toStore.Version == 0 {
			toStore.Version = 1
		}
		s.branchObjects[key] = branchObjectOverlay{object: toStore}
		return PutOutcome{Kind: PutInserted}, nil
	}
	if expectedVersion != nil && *expectedVersion != existing.object.Version {
		return PutOutcome{Kind: PutVersionConflict, ExpectedVersion: *expectedVersion, ActualVersion: existing.object.Version}, nil
	}
	toStore := obj
	toStore.Version = existing.object.Version + 1
	s.branchObjects[key] = branchObjectOverlay{object: toStore}
	return PutOutcome{Kind: PutUpdated, PreviousVersion: existing.object.Version, NewVersion: toStore.Version}, nil
}

func (s *InMemoryOSV2AdvancedStore) DeleteBranchObject(_ context.Context, branch BranchID, tenant TenantId, id ObjectId) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := branchObjectKey{branch: branch, tenant: tenant, id: id}
	_, existed := s.branchObjects[key]
	s.branchObjects[key] = branchObjectOverlay{object: Object{Tenant: tenant, ID: id}, deleted: true}
	return existed, nil
}

func (s *InMemoryOSV2AdvancedStore) GetBranchObject(ctx context.Context, branch BranchID, tenant TenantId, id ObjectId, main ObjectStore, consistency ReadConsistency) (*Object, error) {
	s.mu.Lock()
	overlay, ok := s.branchObjects[branchObjectKey{branch: branch, tenant: tenant, id: id}]
	s.mu.Unlock()
	if ok {
		if overlay.deleted {
			return nil, nil
		}
		obj := overlay.object
		return &obj, nil
	}
	if main == nil {
		return nil, nil
	}
	return main.Get(ctx, tenant, id, consistency)
}

func (s *InMemoryOSV2AdvancedStore) ListBranchObjectsByType(ctx context.Context, branch BranchID, tenant TenantId, typeID TypeId, main ObjectStore, page Page, consistency ReadConsistency) (PagedResult[Object], error) {
	base := []Object{}
	if main != nil {
		mainPage, err := main.ListByType(ctx, tenant, typeID, Page{Size: 10_000}, consistency)
		if err != nil {
			return PagedResult[Object]{}, err
		}
		base = append(base, mainPage.Items...)
	}
	return s.mergeBranchObjects(branch, tenant, typeID, base, func(Object) bool { return true }, page), nil
}

func (s *InMemoryOSV2AdvancedStore) QueryBranchObjectsByProperty(ctx context.Context, branch BranchID, tenant TenantId, typeID TypeId, predicate PropertyPredicate, main ObjectStore, page Page, consistency ReadConsistency) (PagedResult[Object], error) {
	base := []Object{}
	if main != nil {
		mainPage, err := main.ListByType(ctx, tenant, typeID, Page{Size: 10_000}, consistency)
		if err != nil {
			return PagedResult[Object]{}, err
		}
		base = append(base, mainPage.Items...)
	}
	return s.mergeBranchObjects(branch, tenant, typeID, base, func(obj Object) bool { return objectMatchesPredicate(obj, predicate) }, page), nil
}

func (s *InMemoryOSV2AdvancedStore) PutBranchLink(_ context.Context, branch BranchID, link Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.branchLinks[branchLinkKey{branch: branch, tenant: link.Tenant, linkType: link.LinkType, from: link.From, to: link.To}] = struct{}{}
	return nil
}

func (s *InMemoryOSV2AdvancedStore) DeleteBranchLink(_ context.Context, branch BranchID, tenant TenantId, linkType LinkTypeId, from, to ObjectId) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := branchLinkKey{branch: branch, tenant: tenant, linkType: linkType, from: from, to: to}
	_, existed := s.branchLinks[key]
	delete(s.branchLinks, key)
	return existed, nil
}

func (s *InMemoryOSV2AdvancedStore) PublishChange(_ context.Context, event ChangeEvent) (string, error) {
	s.mu.Lock()
	s.nextCursor++
	event.Cursor = strconv.FormatUint(s.nextCursor, 10)
	if event.OccurredMs == 0 {
		event.OccurredMs = time.Now().UTC().UnixMilli()
	}
	s.history = append(s.history, event)
	subs := make([]subscription, 0, len(s.subscribers))
	for _, sub := range s.subscribers {
		subs = append(subs, sub)
	}
	s.mu.Unlock()

	for _, sub := range subs {
		if changeMatches(event, sub.predicate) && (sub.authorize == nil || sub.authorize(event)) {
			select {
			case sub.ch <- event:
			default:
			}
		}
	}
	return event.Cursor, nil
}

func (s *InMemoryOSV2AdvancedStore) SubscribeChanges(ctx context.Context, predicate ChangePredicate, authorize ChangeAuthorizer) (<-chan ChangeEvent, error) {
	ch := make(chan ChangeEvent, 32)
	s.mu.Lock()
	id := s.nextSub
	s.nextSub++
	s.subscribers[id] = subscription{predicate: predicate, authorize: authorize, ch: ch}
	history := append([]ChangeEvent(nil), s.history...)
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.subscribers, id)
			s.mu.Unlock()
			close(ch)
		}()
		for _, event := range history {
			if ctx.Err() != nil {
				return
			}
			if changeMatches(event, predicate) && cursorAfter(event.Cursor, predicate.SinceCursor) && (authorize == nil || authorize(event)) {
				ch <- event
			}
		}
		<-ctx.Done()
	}()
	return ch, nil
}

func (s *InMemoryOSV2AdvancedStore) mergeBranchObjects(branch BranchID, tenant TenantId, typeID TypeId, main []Object, predicate func(Object) bool, page Page) PagedResult[Object] {
	s.mu.Lock()
	overlays := map[ObjectId]branchObjectOverlay{}
	for key, overlay := range s.branchObjects {
		if key.branch == branch && key.tenant == tenant {
			overlays[key.id] = overlay
		}
	}
	s.mu.Unlock()

	byID := map[ObjectId]Object{}
	for _, obj := range main {
		if obj.Tenant == tenant && obj.TypeID == typeID && predicate(obj) {
			byID[obj.ID] = obj
		}
	}
	for id, overlay := range overlays {
		if overlay.deleted {
			delete(byID, id)
			continue
		}
		if overlay.object.TypeID == typeID && predicate(overlay.object) {
			byID[id] = overlay.object
		} else {
			delete(byID, id)
		}
	}
	items := make([]Object, 0, len(byID))
	for _, obj := range byID {
		items = append(items, obj)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].UpdatedAtMs != items[j].UpdatedAtMs {
			return items[i].UpdatedAtMs > items[j].UpdatedAtMs
		}
		return items[i].ID < items[j].ID
	})
	limit := int(page.Size)
	if limit < 1 {
		limit = 1
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return PagedResult[Object]{Items: items}
}

func spatialKey(tenant TenantId, typeID TypeId, objectID ObjectId, property string) string {
	return string(tenant) + "\x00" + string(typeID) + "\x00" + string(objectID) + "\x00" + property
}

func spatialMatches(point GeoPoint, query SpatialQuery) bool {
	switch query.Predicate {
	case SpatialBoundingBox:
		return query.BoundingBox != nil && pointInBox(point, *query.BoundingBox)
	case SpatialRadius:
		return query.Center != nil && haversineMeters(point, *query.Center) <= query.RadiusMeters
	case SpatialPolygonContains:
		return pointInPolygon(point, query.Polygon)
	default:
		return false
	}
}

func pointInBox(point GeoPoint, box BoundingBox) bool {
	return point.Lat >= box.MinLat && point.Lat <= box.MaxLat && point.Lon >= box.MinLon && point.Lon <= box.MaxLon
}

func haversineMeters(a, b GeoPoint) float64 {
	const earthMeters = 6371008.8
	lat1, lat2 := a.Lat*math.Pi/180, b.Lat*math.Pi/180
	dLat := (b.Lat - a.Lat) * math.Pi / 180
	dLon := (b.Lon - a.Lon) * math.Pi / 180
	sinLat := math.Sin(dLat / 2)
	sinLon := math.Sin(dLon / 2)
	h := sinLat*sinLat + math.Cos(lat1)*math.Cos(lat2)*sinLon*sinLon
	return 2 * earthMeters * math.Asin(math.Sqrt(h))
}

func pointInPolygon(point GeoPoint, polygon []GeoPoint) bool {
	if len(polygon) < 3 {
		return false
	}
	inside := false
	j := len(polygon) - 1
	for i := range polygon {
		pi, pj := polygon[i], polygon[j]
		intersects := ((pi.Lat > point.Lat) != (pj.Lat > point.Lat)) &&
			(point.Lon < (pj.Lon-pi.Lon)*(point.Lat-pi.Lat)/(pj.Lat-pi.Lat)+pi.Lon)
		if intersects {
			inside = !inside
		}
		j = i
	}
	return inside
}

func pageHits(items []SearchHit, page Page) PagedResult[SearchHit] {
	limit := int(page.Size)
	if limit < 1 {
		limit = 1
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return PagedResult[SearchHit]{Items: items}
}

func aggregateSamples(samples []TimeSeriesSample, query TimeSeriesQuery) []TimeSeriesBucket {
	if len(samples) == 0 {
		return nil
	}
	width := query.DownsampleEveryMs
	if width <= 0 {
		width = query.EndMs - query.StartMs + 1
		if width <= 0 {
			width = 1
		}
	}
	byBucket := map[int64][]float64{}
	for _, sample := range samples {
		bucketStart := query.StartMs + ((sample.TimestampMs - query.StartMs) / width * width)
		byBucket[bucketStart] = append(byBucket[bucketStart], sample.Value)
	}
	starts := make([]int64, 0, len(byBucket))
	for start := range byBucket {
		starts = append(starts, start)
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })
	out := make([]TimeSeriesBucket, 0, len(starts))
	for _, start := range starts {
		values := byBucket[start]
		out = append(out, TimeSeriesBucket{StartMs: start, EndMs: start + width - 1, Value: aggregateValues(values, query), Count: len(values)})
	}
	return out
}

func aggregateValues(values []float64, query TimeSeriesQuery) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	switch query.Aggregation {
	case TimeSeriesMin:
		return sorted[0]
	case TimeSeriesMax:
		return sorted[len(sorted)-1]
	case TimeSeriesPercentile:
		p := query.Percentile
		if p <= 0 {
			p = 50
		}
		if p > 100 {
			p = 100
		}
		idx := int(math.Ceil((p/100)*float64(len(sorted)))) - 1
		if idx < 0 {
			idx = 0
		}
		return sorted[idx]
	default:
		var sum float64
		for _, v := range values {
			sum += v
		}
		return sum / float64(len(values))
	}
}

func objectMatchesPredicate(obj Object, predicate PropertyPredicate) bool {
	if predicate.PropertyName == "" {
		return true
	}
	payload := map[string]any{}
	if err := json.Unmarshal(obj.Payload, &payload); err != nil {
		return false
	}
	actual, ok := payload[predicate.PropertyName]
	switch strings.ToLower(predicate.Operator) {
	case "eq", "=", "":
		return ok && strings.EqualFold(stringifyValue(actual), stringifyValue(predicate.Value))
	case "starts_with":
		return ok && strings.HasPrefix(strings.ToLower(stringifyValue(actual)), strings.ToLower(stringifyValue(predicate.Value)))
	case "contains":
		return ok && strings.Contains(strings.ToLower(stringifyValue(actual)), strings.ToLower(stringifyValue(predicate.Value)))
	default:
		return false
	}
}

func stringifyValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func changeMatches(event ChangeEvent, predicate ChangePredicate) bool {
	if event.Tenant != predicate.Tenant {
		return false
	}
	if predicate.TypeID != nil && event.TypeID != *predicate.TypeID {
		return false
	}
	if predicate.ObjectID != nil && event.ObjectID != *predicate.ObjectID {
		return false
	}
	if predicate.LinkType != nil && event.LinkType != *predicate.LinkType {
		return false
	}
	return true
}

func cursorAfter(cursor, since string) bool {
	if since == "" {
		return true
	}
	c, cErr := strconv.ParseUint(cursor, 10, 64)
	s, sErr := strconv.ParseUint(since, 10, 64)
	if cErr != nil || sErr != nil {
		return cursor > since
	}
	return c > s
}
