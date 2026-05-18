package handlers

import (
	"net/http"
	"strings"
	"sync"
	"time"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/object-database-service/internal/storage"
)

const objectReadCacheTTL = 2 * time.Second

type objectReadCache struct {
	mu   sync.Mutex
	rows map[string]cachedObject
}

type cachedObject struct {
	object    storage.Object
	expiresAt time.Time
}

func (h *Handlers) objectCache() *objectReadCache {
	h.cacheOnce.Do(func() {
		h.cache = &objectReadCache{rows: map[string]cachedObject{}}
	})
	return h.cache
}

func objectCacheKey(tenant storage.TenantId, typeID storage.TypeId, primaryKey string) string {
	return string(tenant) + "\x00" + string(typeID) + "\x00" + strings.TrimSpace(primaryKey)
}

func (h *Handlers) getCachedObject(tenant storage.TenantId, typeID storage.TypeId, primaryKey string) (*storage.Object, bool) {
	cache := h.objectCache()
	key := objectCacheKey(tenant, typeID, primaryKey)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.rows[key]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(cache.rows, key)
		return nil, false
	}
	cp := entry.object
	return &cp, true
}

func (h *Handlers) putCachedObject(tenant storage.TenantId, typeID storage.TypeId, primaryKey string, obj *storage.Object) {
	if obj == nil {
		return
	}
	cache := h.objectCache()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.rows[objectCacheKey(tenant, typeID, primaryKey)] = cachedObject{object: *obj, expiresAt: time.Now().Add(objectReadCacheTTL)}
}

func (h *Handlers) bustObjectCache(tenant storage.TenantId, typeID storage.TypeId, primaryKey string) {
	if h.cache == nil {
		return
	}
	h.cache.mu.Lock()
	defer h.cache.mu.Unlock()
	delete(h.cache.rows, objectCacheKey(tenant, typeID, primaryKey))
}

func (h *Handlers) bustObjectIDFromCache(tenant storage.TenantId, primaryKey string) {
	if h.cache == nil {
		return
	}
	prefix := string(tenant) + "\x00"
	suffix := "\x00" + strings.TrimSpace(primaryKey)
	h.cache.mu.Lock()
	defer h.cache.mu.Unlock()
	for key := range h.cache.rows {
		if strings.HasPrefix(key, prefix) && strings.HasSuffix(key, suffix) {
			delete(h.cache.rows, key)
		}
	}
}

func (h *Handlers) getObjectByTypePrimaryKey(r *http.Request, tenant storage.TenantId, typeID storage.TypeId, primaryKey string) (*storage.Object, error) {
	if obj, ok := h.getCachedObject(tenant, typeID, primaryKey); ok {
		return obj, nil
	}
	consistency := parseConsistency(r.URL.Query().Get("consistency"))
	if point, ok := h.Objects.(storage.PointReadStore); ok {
		obj, err := point.GetByTypeAndPrimaryKey(r.Context(), tenant, typeID, primaryKey, consistency)
		if err != nil || obj == nil {
			return obj, err
		}
		h.putCachedObject(tenant, typeID, primaryKey, obj)
		return obj, nil
	}
	obj, err := h.Objects.Get(r.Context(), tenant, storage.ObjectId(primaryKey), consistency)
	if err != nil || obj == nil || obj.TypeID != typeID {
		return nil, err
	}
	h.putCachedObject(tenant, typeID, primaryKey, obj)
	return obj, nil
}

func callerCanReadStorageObject(r *http.Request, obj *storage.Object) bool {
	if obj == nil || len(obj.Markings) == 0 {
		return true
	}
	required := storageMarkingsToStrings(obj.Markings)
	if claims, ok := authmw.FromContext(r.Context()); ok && claims != nil {
		return claims.AllowsAllMarkings(required)
	}
	allowed := splitQueryCSV(r.Header.Get("x-openfoundry-allowed-markings"))
	if len(allowed) == 0 {
		return true
	}
	allowedSet := map[string]bool{}
	for _, marking := range allowed {
		allowedSet[strings.ToLower(strings.TrimSpace(marking))] = true
	}
	for _, marking := range required {
		if !allowedSet[strings.ToLower(strings.TrimSpace(marking))] {
			return false
		}
	}
	return true
}

func filterStorageObjectsForCaller(r *http.Request, items []storage.Object) ([]storage.Object, int) {
	out := make([]storage.Object, 0, len(items))
	omitted := 0
	for i := range items {
		if callerCanReadStorageObject(r, &items[i]) {
			out = append(out, items[i])
			continue
		}
		omitted++
	}
	return out, omitted
}

func storageMarkingsToStrings(markings []storage.MarkingId) []string {
	out := make([]string, 0, len(markings))
	for _, marking := range markings {
		if trimmed := strings.TrimSpace(string(marking)); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func callerCanReadOntologyObject(r *http.Request, item ontologyObject) bool {
	if item.Marking == nil || strings.TrimSpace(*item.Marking) == "" {
		return true
	}
	obj := &storage.Object{Markings: []storage.MarkingId{storage.MarkingId(*item.Marking)}}
	return callerCanReadStorageObject(r, obj)
}

func filterOntologyObjectsForMarkings(r *http.Request, items []ontologyObject) ([]ontologyObject, int) {
	out := make([]ontologyObject, 0, len(items))
	omitted := 0
	for _, item := range items {
		if callerCanReadOntologyObject(r, item) {
			out = append(out, item)
			continue
		}
		omitted++
	}
	return out, omitted
}
