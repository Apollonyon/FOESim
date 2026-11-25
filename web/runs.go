package web

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"parliamentsim/sim"
)

const (
	defaultRunMaxEntries = 256
	defaultRunTTL        = 15 * time.Minute
	maxCustomRunBodySize = 2 << 20 // 2 MiB
)

type runEntry struct {
	scenario  sim.Scenario
	expiresAt time.Time
}

// runStore keeps ephemeral user-submitted custom scenarios in memory with a TTL
// and bounded capacity. It only locks briefly during Put/Get/evict and never
// serializes active SSE streams.
type runStore struct {
	mu         sync.RWMutex
	entries    map[string]runEntry
	maxEntries int
	ttl        time.Duration
	now        func() time.Time
}

func newRunStore(maxEntries int, ttl time.Duration) *runStore {
	if maxEntries <= 0 {
		maxEntries = defaultRunMaxEntries
	}
	if ttl <= 0 {
		ttl = defaultRunTTL
	}
	return &runStore{
		entries:    make(map[string]runEntry),
		maxEntries: maxEntries,
		ttl:        ttl,
		now:        time.Now,
	}
}

func newRunID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func (rs *runStore) Put(sc sim.Scenario) (string, error) {
	id, err := newRunID()
	if err != nil {
		return "", err
	}
	now := rs.now()

	rs.mu.Lock()
	defer rs.mu.Unlock()

	for k, e := range rs.entries {
		if !now.Before(e.expiresAt) {
			delete(rs.entries, k)
		}
	}
	for len(rs.entries) >= rs.maxEntries {
		var oldestKey string
		var oldestExp time.Time
		first := true
		for k, e := range rs.entries {
			if first || e.expiresAt.Before(oldestExp) {
				oldestKey = k
				oldestExp = e.expiresAt
				first = false
			}
		}
		if first {
			break
		}
		delete(rs.entries, oldestKey)
	}

	rs.entries[id] = runEntry{
		scenario:  sc,
		expiresAt: now.Add(rs.ttl),
	}
	return id, nil
}

func (rs *runStore) Get(id string) (sim.Scenario, bool) {
	if id == "" {
		return sim.Scenario{}, false
	}
	now := rs.now()

	rs.mu.RLock()
	entry, ok := rs.entries[id]
	rs.mu.RUnlock()
	if !ok {
		return sim.Scenario{}, false
	}
	if !now.Before(entry.expiresAt) {
		rs.mu.Lock()
		if cur, stillThere := rs.entries[id]; stillThere && !now.Before(cur.expiresAt) {
			delete(rs.entries, id)
		}
		rs.mu.Unlock()
		return sim.Scenario{}, false
	}
	return entry.scenario, true
}

func validateCustomRunLimits(sc sim.Scenario) error {
	if len(sc.Regions) > 100 {
		return fmt.Errorf("too many regions (%d > 100)", len(sc.Regions))
	}
	if len(sc.Groups) > 32 {
		return fmt.Errorf("too many groups (%d > 32)", len(sc.Groups))
	}
	if len(sc.Parties) > 64 {
		return fmt.Errorf("too many top-level parties (%d > 64)", len(sc.Parties))
	}
	if len(sc.PrevSeats) > 128 {
		return fmt.Errorf("too many prev_seats entries (%d > 128)", len(sc.PrevSeats))
	}

	totalSeats := 0
	totalPrecincts := 0
	for _, r := range sc.Regions {
		if r.Seats <= 0 || r.Seats > 2000 {
			return fmt.Errorf("region %q seats (%d) must be between 1 and 2000", r.ID, r.Seats)
		}
		if len(r.Groups) > 32 {
			return fmt.Errorf("region %q has too many voter groups (%d > 32)", r.ID, len(r.Groups))
		}
		if len(r.Parties) > 64 {
			return fmt.Errorf("region %q has too many parties (%d > 64)", r.ID, len(r.Parties))
		}
		precincts := r.EffectivePrecinctCount()
		if precincts < 0 || precincts > 5000 {
			return fmt.Errorf("region %q effective precincts (%d) out of bounds", r.ID, precincts)
		}
		totalSeats += r.Seats
		totalPrecincts += precincts
	}
	if totalSeats > 2000 {
		return fmt.Errorf("total seats (%d) exceed limit of 2000", totalSeats)
	}
	if totalPrecincts > 5000 {
		return fmt.Errorf("total effective precincts (%d) exceed limit of 5000", totalPrecincts)
	}
	if len(sc.Regions) == 0 {
		return errors.New("scenario must contain at least one region")
	}
	return nil
}
