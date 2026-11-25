package sim

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ScenarioSummary holds lightweight metadata for listing available scenarios.
type ScenarioSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	SeatLabel string `json:"seat_label"`
	Seats     int    `json:"seats"`
	Regions   int    `json:"regions"`
}

// ScenarioEntry pairs a scenario ID with its loaded Scenario.
type ScenarioEntry struct {
	ID       string
	Scenario Scenario
}

// Summary builds a lightweight ScenarioSummary for s under the given id.
func (s Scenario) Summary(id string) ScenarioSummary {
	meta := s.EffectiveMeta()
	totalSeats := 0
	for _, reg := range s.Regions {
		totalSeats += reg.Seats
	}
	return ScenarioSummary{
		ID:        id,
		Name:      s.Name,
		Title:     meta.Title,
		SeatLabel: meta.SeatLabel,
		Seats:     totalSeats,
		Regions:   len(s.Regions),
	}
}

// ScenarioRegistry stores validated scenarios by ID with a deterministic display order.
type ScenarioRegistry struct {
	order     []string
	scenarios map[string]Scenario
	defaultID string
}

// NewScenarioRegistry validates entries, sets the default scenario ID (or picks
// the alphabetically first ID when defaultID is empty), and orders summaries
// with defaultID first followed by remaining IDs sorted alphabetically.
func NewScenarioRegistry(defaultID string, entries []ScenarioEntry) (*ScenarioRegistry, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("scenario registry requires at least one entry")
	}

	scenarios := make(map[string]Scenario, len(entries))
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.ID == "" {
			return nil, fmt.Errorf("scenario registry entry has empty id")
		}
		if _, exists := scenarios[e.ID]; exists {
			return nil, fmt.Errorf("duplicate scenario id %q", e.ID)
		}
		if err := e.Scenario.Validate(); err != nil {
			return nil, fmt.Errorf("invalid scenario %q: %w", e.ID, err)
		}
		scenarios[e.ID] = e.Scenario
		ids = append(ids, e.ID)
	}
	slices.Sort(ids)

	if defaultID == "" {
		defaultID = ids[0]
	} else if _, ok := scenarios[defaultID]; !ok {
		return nil, fmt.Errorf("default scenario %q not found in registry", defaultID)
	}

	order := make([]string, 0, len(ids))
	order = append(order, defaultID)
	for _, id := range ids {
		if id != defaultID {
			order = append(order, id)
		}
	}

	return &ScenarioRegistry{
		order:     order,
		scenarios: scenarios,
		defaultID: defaultID,
	}, nil
}

// LoadScenariosDir loads all *.json scenario files in dir (keyed by filename
// without the .json extension) into a validated ScenarioRegistry.
func LoadScenariosDir(dir, defaultID string) (*ScenarioRegistry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var entries []ScenarioEntry
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(de.Name(), ".json")
		sc, err := LoadScenario(filepath.Join(dir, de.Name()))
		if err != nil {
			return nil, fmt.Errorf("load scenario %q: %w", de.Name(), err)
		}
		entries = append(entries, ScenarioEntry{ID: id, Scenario: sc})
	}
	return NewScenarioRegistry(defaultID, entries)
}

// DefaultID returns the registry's default scenario ID.
func (r *ScenarioRegistry) DefaultID() string {
	if r == nil {
		return ""
	}
	return r.defaultID
}

// Get returns the scenario for id (or the default scenario when id is empty).
func (r *ScenarioRegistry) Get(id string) (Scenario, bool) {
	if r == nil {
		return Scenario{}, false
	}
	if id == "" {
		id = r.defaultID
	}
	s, ok := r.scenarios[id]
	return s, ok
}

// List returns summaries for all scenarios in deterministic order (default first).
func (r *ScenarioRegistry) List() []ScenarioSummary {
	if r == nil {
		return nil
	}
	out := make([]ScenarioSummary, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.scenarios[id].Summary(id))
	}
	return out
}
