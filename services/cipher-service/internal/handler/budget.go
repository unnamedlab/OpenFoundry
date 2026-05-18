package handler

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

type budgetCounter struct {
	used        uint32
	windowStart time.Time
}

type budgetKey struct {
	actor uuid.UUID
	key   uuid.UUID
}

// DecryptBudgetManager enforces CIP.23 hard caps per caller/key over a fixed
// window. A nil manager means budgets are not configured for the deployment.
type DecryptBudgetManager struct {
	mu           sync.Mutex
	defaultLimit uint32
	window       time.Duration
	perUserKey   map[budgetKey]uint32
	counters     map[budgetKey]budgetCounter
	now          func() time.Time
}

func NewDecryptBudgetManager(defaultLimit uint32, window time.Duration) *DecryptBudgetManager {
	if window <= 0 {
		window = time.Hour
	}
	return &DecryptBudgetManager{defaultLimit: defaultLimit, window: window, perUserKey: map[budgetKey]uint32{}, counters: map[budgetKey]budgetCounter{}, now: time.Now}
}

func (m *DecryptBudgetManager) SetUserKeyBudget(actorID, keyID uuid.UUID, limit uint32) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.perUserKey[budgetKey{actor: actorID, key: keyID}] = limit
}

func (m *DecryptBudgetManager) Allow(actorID, keyID uuid.UUID) bool {
	if m == nil || actorID == uuid.Nil || keyID == uuid.Nil {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	bk := budgetKey{actor: actorID, key: keyID}
	limit, ok := m.perUserKey[bk]
	if !ok {
		limit = m.defaultLimit
	}
	if limit == 0 {
		return true
	}
	now := m.now().UTC()
	ctr := m.counters[bk]
	if ctr.windowStart.IsZero() || now.Sub(ctr.windowStart) >= m.window {
		ctr = budgetCounter{windowStart: now}
	}
	if ctr.used >= limit {
		m.counters[bk] = ctr
		return false
	}
	ctr.used++
	m.counters[bk] = ctr
	return true
}
