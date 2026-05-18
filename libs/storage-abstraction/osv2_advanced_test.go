package storageabstraction

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testObjectStore struct{ rows map[ObjectId]Object }

func (s *testObjectStore) Get(_ context.Context, tenant TenantId, id ObjectId, _ ReadConsistency) (*Object, error) {
	obj, ok := s.rows[id]
	if !ok || obj.Tenant != tenant {
		return nil, nil
	}
	return &obj, nil
}
func (s *testObjectStore) Put(_ context.Context, obj Object, _ *uint64) (PutOutcome, error) {
	s.rows[obj.ID] = obj
	return PutOutcome{Kind: PutInserted}, nil
}
func (s *testObjectStore) Delete(_ context.Context, _ TenantId, id ObjectId) (bool, error) {
	_, ok := s.rows[id]
	delete(s.rows, id)
	return ok, nil
}
func (s *testObjectStore) ListByType(_ context.Context, tenant TenantId, typeID TypeId, _ Page, _ ReadConsistency) (PagedResult[Object], error) {
	out := []Object{}
	for _, obj := range s.rows {
		if obj.Tenant == tenant && obj.TypeID == typeID {
			out = append(out, obj)
		}
	}
	return PagedResult[Object]{Items: out}, nil
}
func (s *testObjectStore) ListByOwner(context.Context, TenantId, OwnerId, Page, ReadConsistency) (PagedResult[Object], error) {
	return PagedResult[Object]{}, nil
}
func (s *testObjectStore) ListByMarking(context.Context, TenantId, MarkingId, Page, ReadConsistency) (PagedResult[Object], error) {
	return PagedResult[Object]{}, nil
}

func TestInMemorySpatialIndexSupportsBoxRadiusAndPolygon(t *testing.T) {
	idx := NewInMemoryOSV2AdvancedStore()
	require.NoError(t, idx.IndexSpatial(context.Background(), "acme", "Aircraft", "inside", "location", GeoPoint{Lat: 37.78, Lon: -122.42}, 1))
	require.NoError(t, idx.IndexSpatial(context.Background(), "acme", "Aircraft", "outside", "location", GeoPoint{Lat: 40.71, Lon: -74.0}, 1))

	box, err := idx.SearchSpatial(context.Background(), SpatialQuery{Tenant: "acme", TypeID: "Aircraft", Property: "location", Predicate: SpatialBoundingBox, BoundingBox: &BoundingBox{MinLat: 37, MaxLat: 38, MinLon: -123, MaxLon: -122}, Page: Page{Size: 10}}, Strong())
	require.NoError(t, err)
	require.Len(t, box.Items, 1)
	assert.Equal(t, ObjectId("inside"), box.Items[0].ID)

	radius, err := idx.SearchSpatial(context.Background(), SpatialQuery{Tenant: "acme", TypeID: "Aircraft", Property: "location", Predicate: SpatialRadius, Center: &GeoPoint{Lat: 37.78, Lon: -122.42}, RadiusMeters: 1000, Page: Page{Size: 10}}, Strong())
	require.NoError(t, err)
	assert.Len(t, radius.Items, 1)

	polygon, err := idx.SearchSpatial(context.Background(), SpatialQuery{Tenant: "acme", TypeID: "Aircraft", Property: "location", Predicate: SpatialPolygonContains, Polygon: []GeoPoint{{Lat: 37, Lon: -123}, {Lat: 38, Lon: -123}, {Lat: 38, Lon: -122}, {Lat: 37, Lon: -122}}, Page: Page{Size: 10}}, Strong())
	require.NoError(t, err)
	assert.Len(t, polygon.Items, 1)
}

func TestInMemoryTimeSeriesRangeDownsamplingAndAggregations(t *testing.T) {
	store := NewInMemoryOSV2AdvancedStore()
	samples := []TimeSeriesSample{
		{Tenant: "acme", TypeID: "Sensor", ObjectID: "s-1", Property: "temp", TimestampMs: 1000, Value: 10},
		{Tenant: "acme", TypeID: "Sensor", ObjectID: "s-1", Property: "temp", TimestampMs: 1500, Value: 20},
		{Tenant: "acme", TypeID: "Sensor", ObjectID: "s-1", Property: "temp", TimestampMs: 2500, Value: 40},
	}
	require.NoError(t, store.PutTimeSeriesSamples(context.Background(), samples))

	raw, err := store.QueryTimeSeries(context.Background(), TimeSeriesQuery{Tenant: "acme", TypeID: "Sensor", ObjectID: "s-1", Property: "temp", StartMs: 1000, EndMs: 2000}, Strong())
	require.NoError(t, err)
	assert.Len(t, raw.Samples, 2)

	agg, err := store.QueryTimeSeries(context.Background(), TimeSeriesQuery{Tenant: "acme", TypeID: "Sensor", ObjectID: "s-1", Property: "temp", StartMs: 1000, EndMs: 3000, Aggregation: TimeSeriesAvg, DownsampleEveryMs: 1000}, Strong())
	require.NoError(t, err)
	require.Len(t, agg.Buckets, 2)
	assert.Equal(t, float64(15), agg.Buckets[0].Value)
	assert.Equal(t, float64(40), agg.Buckets[1].Value)
}

func TestBranchOverlayPrefersBranchRowsAndAvoidsDoubleCounting(t *testing.T) {
	main := &testObjectStore{rows: map[ObjectId]Object{
		"obj-1": {Tenant: "acme", ID: "obj-1", TypeID: "Aircraft", Version: 1, UpdatedAtMs: 1, Payload: json.RawMessage(`{"status":"main"}`)},
		"obj-2": {Tenant: "acme", ID: "obj-2", TypeID: "Aircraft", Version: 1, UpdatedAtMs: 2, Payload: json.RawMessage(`{"status":"keep"}`)},
	}}
	overlay := NewInMemoryOSV2AdvancedStore()
	_, err := overlay.PutBranchObject(context.Background(), "br-1", Object{Tenant: "acme", ID: "obj-1", TypeID: "Aircraft", Version: 1, UpdatedAtMs: 3, Payload: json.RawMessage(`{"status":"branch"}`)}, nil)
	require.NoError(t, err)
	_, err = overlay.PutBranchObject(context.Background(), "br-1", Object{Tenant: "acme", ID: "obj-3", TypeID: "Aircraft", Version: 1, UpdatedAtMs: 4, Payload: json.RawMessage(`{"status":"branch"}`)}, nil)
	require.NoError(t, err)
	_, err = overlay.DeleteBranchObject(context.Background(), "br-1", "acme", "obj-2")
	require.NoError(t, err)

	page, err := overlay.ListBranchObjectsByType(context.Background(), "br-1", "acme", "Aircraft", main, Page{Size: 10}, Strong())
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	ids := []ObjectId{page.Items[0].ID, page.Items[1].ID}
	assert.ElementsMatch(t, []ObjectId{"obj-1", "obj-3"}, ids)

	filtered, err := overlay.QueryBranchObjectsByProperty(context.Background(), "br-1", "acme", "Aircraft", PropertyPredicate{PropertyName: "status", Operator: "eq", Value: "branch"}, main, Page{Size: 10}, Strong())
	require.NoError(t, err)
	assert.Len(t, filtered.Items, 2)
}

func TestChangeSubscriptionsReplayAfterCursorAndFilterRevokedByAuthorizer(t *testing.T) {
	stream := NewInMemoryOSV2AdvancedStore()
	first, err := stream.PublishChange(context.Background(), ChangeEvent{Tenant: "acme", TypeID: "Aircraft", ObjectID: "obj-1", Kind: ChangeObjectUpsert, OccurredMs: time.Now().UnixMilli()})
	require.NoError(t, err)
	_, err = stream.PublishChange(context.Background(), ChangeEvent{Tenant: "acme", TypeID: "Aircraft", ObjectID: "obj-secret", Kind: ChangeObjectUpsert})
	require.NoError(t, err)
	_, err = stream.PublishChange(context.Background(), ChangeEvent{Tenant: "other", TypeID: "Aircraft", ObjectID: "obj-2", Kind: ChangeObjectUpsert})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	typeID := TypeId("Aircraft")
	events, err := stream.SubscribeChanges(ctx, ChangePredicate{Tenant: "acme", TypeID: &typeID, SinceCursor: first}, func(evt ChangeEvent) bool {
		return evt.ObjectID != "obj-secret"
	})
	require.NoError(t, err)

	_, err = stream.PublishChange(context.Background(), ChangeEvent{Tenant: "acme", TypeID: "Aircraft", ObjectID: "obj-3", Kind: ChangeObjectUpsert})
	require.NoError(t, err)
	select {
	case evt := <-events:
		assert.Equal(t, ObjectId("obj-3"), evt.ObjectID)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscription event")
	}
}
