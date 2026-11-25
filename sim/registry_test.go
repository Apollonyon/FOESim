package sim

import "testing"

func TestScenarioRegistry(t *testing.T) {
	validScenario := func(name string, seatsPerRegion int, regionIDs ...string) Scenario {
		regions := make([]Region, len(regionIDs))
		for i, id := range regionIDs {
			regions[i] = Region{
				ID:     id,
				Name:   id,
				Seats:  seatsPerRegion,
				Groups: []VoterGroup{{ID: "g1", Name: "All", Population: 1000, Turnout: 0.5}},
			}
		}
		return Scenario{
			Name:    name,
			Parties: DefaultParties(),
			Regions: regions,
		}
	}

	t.Run("validation errors", func(t *testing.T) {
		alpha := validScenario("Alpha", 10, "r1")

		if _, err := NewScenarioRegistry("", nil); err == nil {
			t.Error("expected error for empty entries, got nil")
		}
		if _, err := NewScenarioRegistry("", []ScenarioEntry{{ID: "", Scenario: alpha}}); err == nil {
			t.Error("expected error for empty entry ID, got nil")
		}
		if _, err := NewScenarioRegistry("", []ScenarioEntry{
			{ID: "dup", Scenario: alpha},
			{ID: "dup", Scenario: alpha},
		}); err == nil {
			t.Error("expected error for duplicate entry ID, got nil")
		}
		if _, err := NewScenarioRegistry("", []ScenarioEntry{
			{ID: "bad", Scenario: Scenario{Name: "Empty"}},
		}); err == nil {
			t.Error("expected error for invalid scenario, got nil")
		}
		if _, err := NewScenarioRegistry("missing", []ScenarioEntry{
			{ID: "alpha", Scenario: alpha},
		}); err == nil {
			t.Error("expected error for unknown defaultID, got nil")
		}
	})

	t.Run("empty defaultID picks alphabetically first", func(t *testing.T) {
		reg, err := NewScenarioRegistry("", []ScenarioEntry{
			{ID: "zebra", Scenario: validScenario("Zebra", 5, "z1")},
			{ID: "alpha", Scenario: validScenario("Alpha", 7, "a1", "a2")},
		})
		if err != nil {
			t.Fatalf("NewScenarioRegistry failed: %v", err)
		}
		if got := reg.DefaultID(); got != "alpha" {
			t.Errorf("DefaultID() = %q, want %q", got, "alpha")
		}
	})

	t.Run("ordering, Get, and List summaries", func(t *testing.T) {
		us := validScenario("USA 2024", 10, "ca", "tx")
		us.Meta = ScenarioMeta{Title: "U.S. Electoral College", SeatLabel: "EVs"}

		eu := validScenario("EU-27", 20, "de", "fr", "it")
		starter := validScenario("EU Starter", 15, "de")

		reg, err := NewScenarioRegistry("eu27", []ScenarioEntry{
			{ID: "usa2024", Scenario: us},
			{ID: "eu_starter", Scenario: starter},
			{ID: "eu27", Scenario: eu},
		})
		if err != nil {
			t.Fatalf("NewScenarioRegistry failed: %v", err)
		}

		if got := reg.DefaultID(); got != "eu27" {
			t.Errorf("DefaultID() = %q, want %q", got, "eu27")
		}

		// Get("") returns default; Get(id) returns matching; Get("missing") returns false.
		if s, ok := reg.Get(""); !ok || s.Name != "EU-27" {
			t.Errorf("Get(\"\") = (%q, %v), want (\"EU-27\", true)", s.Name, ok)
		}
		if s, ok := reg.Get("usa2024"); !ok || s.Name != "USA 2024" {
			t.Errorf("Get(\"usa2024\") = (%q, %v), want (\"USA 2024\", true)", s.Name, ok)
		}
		if _, ok := reg.Get("nope"); ok {
			t.Error("Get(\"nope\") = true, want false")
		}

		// List puts default first ("eu27"), then remaining sorted alphabetically ("eu_starter", "usa2024").
		list := reg.List()
		if len(list) != 3 {
			t.Fatalf("len(List()) = %d, want 3", len(list))
		}
		wantOrder := []string{"eu27", "eu_starter", "usa2024"}
		for i, wantID := range wantOrder {
			if list[i].ID != wantID {
				t.Errorf("list[%d].ID = %q, want %q", i, list[i].ID, wantID)
			}
		}

		// Verify summary fields for EU-27 (EffectiveMeta defaults) and USA (custom Meta).
		if list[0].Title != "European Parliament" || list[0].SeatLabel != "seats" || list[0].Seats != 60 || list[0].Regions != 3 {
			t.Errorf("eu27 summary = %+v", list[0])
		}
		if list[2].Title != "U.S. Electoral College" || list[2].SeatLabel != "EVs" || list[2].Seats != 20 || list[2].Regions != 2 {
			t.Errorf("usa2024 summary = %+v", list[2])
		}
	})
}

func TestLoadScenariosDir(t *testing.T) {
	reg, err := LoadScenariosDir("../scenarios", "eu27")
	if err != nil {
		t.Fatalf("LoadScenariosDir failed: %v", err)
	}
	list := reg.List()
	if len(list) < 3 {
		t.Fatalf("got %d scenarios, want at least 3", len(list))
	}
	if list[0].ID != "eu27" {
		t.Errorf("first summary ID = %q, want %q", list[0].ID, "eu27")
	}
	for _, wantID := range []string{"eu27", "eu_starter", "usa2024"} {
		if _, ok := reg.Get(wantID); !ok {
			t.Errorf("expected scenario %q to be loaded", wantID)
		}
	}
}
