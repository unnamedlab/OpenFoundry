// Package anomaly implements the local CIP.24 decrypt-anomaly detector used by
// the cipher service's background audit job. It is deliberately dependency-free;
// production deployments can adapt Notifier to Pulse.
package anomaly

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

type DecryptEvent struct {
	ActorID uuid.UUID
	KeyID   uuid.UUID
	At      time.Time
}

type Finding struct {
	ActorID uuid.UUID `json:"actor_id"`
	KeyID   uuid.UUID `json:"key_id"`
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
}

type Notifier interface {
	NotifyCipherAnomaly(context.Context, Finding) error
}

type Detector struct {
	mu         sync.Mutex
	burstLimit int
	window     time.Duration
	seenActors map[uuid.UUID]map[uuid.UUID]bool
	recent     map[uuid.UUID][]time.Time
	findings   []Finding
	notify     Notifier
}

func NewDetector(burstLimit int, window time.Duration, notify Notifier) *Detector {
	if burstLimit <= 0 {
		burstLimit = 1000
	}
	if window <= 0 {
		window = time.Hour
	}
	return &Detector{burstLimit: burstLimit, window: window, seenActors: map[uuid.UUID]map[uuid.UUID]bool{}, recent: map[uuid.UUID][]time.Time{}, notify: notify}
}

func (d *Detector) Record(ctx context.Context, ev DecryptEvent) {
	if d == nil || ev.ActorID == uuid.Nil || ev.KeyID == uuid.Nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	var findings []Finding
	d.mu.Lock()
	if d.seenActors[ev.KeyID] == nil {
		d.seenActors[ev.KeyID] = map[uuid.UUID]bool{}
	}
	if !d.seenActors[ev.KeyID][ev.ActorID] {
		d.seenActors[ev.KeyID][ev.ActorID] = true
		findings = append(findings, Finding{ActorID: ev.ActorID, KeyID: ev.KeyID, Reason: "new_actor", At: ev.At})
	}
	if ev.At.Hour() < 6 || ev.At.Hour() > 22 {
		findings = append(findings, Finding{ActorID: ev.ActorID, KeyID: ev.KeyID, Reason: "off_hours", At: ev.At})
	}
	cutoff := ev.At.Add(-d.window)
	items := d.recent[ev.ActorID]
	kept := items[:0]
	for _, ts := range items {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	kept = append(kept, ev.At)
	d.recent[ev.ActorID] = kept
	if len(kept) > d.burstLimit {
		findings = append(findings, Finding{ActorID: ev.ActorID, KeyID: ev.KeyID, Reason: "sudden_burst", At: ev.At})
	}
	d.findings = append(d.findings, findings...)
	d.mu.Unlock()
	for _, f := range findings {
		if d.notify != nil {
			_ = d.notify.NotifyCipherAnomaly(ctx, f)
		}
	}
}

func (d *Detector) Findings() []Finding {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Finding, len(d.findings))
	copy(out, d.findings)
	return out
}
