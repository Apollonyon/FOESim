package web

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/Apollonyon/FOESim/sim"
)

const (
	defaultRunMaxEntries      = 256
	defaultRunTTL             = 15 * time.Minute
	maxCustomRunBodySize      = 2 << 20 // 2 MiB
	defaultMaxRunsPerMin      = 30
	defaultRateLimiterMaxKeys = 4096
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

type rateWindow struct {
	count   int
	resetAt time.Time
}

type ipRateLimiter struct {
	mu      sync.Mutex
	entries map[string]rateWindow
	limit   int
	window  time.Duration
	maxKeys int
	now     func() time.Time
}

func newIPRateLimiter(limit int, window time.Duration) *ipRateLimiter {
	if window <= 0 {
		window = time.Minute
	}
	return &ipRateLimiter{
		entries: make(map[string]rateWindow),
		limit:   limit,
		window:  window,
		maxKeys: defaultRateLimiterMaxKeys,
		now:     time.Now,
	}
}

func (rl *ipRateLimiter) Allow(key string) bool {
	if rl == nil || rl.limit <= 0 || key == "" {
		return true
	}
	now := rl.now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry, ok := rl.entries[key]
	if ok && now.Before(entry.resetAt) {
		entry.count++
		rl.entries[key] = entry
		return entry.count <= rl.limit
	}

	if !ok && len(rl.entries) >= rl.maxKeys {
		for k, e := range rl.entries {
			if !now.Before(e.resetAt) {
				delete(rl.entries, k)
			}
		}
		if len(rl.entries) >= rl.maxKeys {
			var earliestKey string
			var earliestReset time.Time
			first := true
			for k, e := range rl.entries {
				if first || e.resetAt.Before(earliestReset) {
					earliestKey = k
					earliestReset = e.resetAt
					first = false
				}
			}
			if !first {
				delete(rl.entries, earliestKey)
			}
		}
	}

	rl.entries[key] = rateWindow{
		count:   1,
		resetAt: now.Add(rl.window),
	}
	return true
}
