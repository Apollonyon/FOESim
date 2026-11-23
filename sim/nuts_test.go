package sim

import (
	"math"
	"testing"
)

func TestNUTS3UnitsAndTilt(t *testing.T) {
	de := NUTS3Units("de")
	if len(de) < 350 {
		t.Fatalf("expected ~401 German NUTS-3 units, got %d", len(de))
	}

	sumW := 0.0
	for _, u := range de {
		if u.ID == "" || u.Name == "" {
			t.Fatalf("invalid NUTS-3 unit: %+v", u)
		}
		sumW += u.Weight
	}
	if math.Abs(sumW-1.0) > 0.02 {
		t.Errorf("DE NUTS-3 weights sum = %v, want ~1.0", sumW)
	}

	reg := DefaultRegions()[0]
	reg.UseNUTS3 = true
	reports := SimulatePrecincts(reg, DefaultParties(), 0.85, 0.0, 42)
	if len(reports) != len(de) {
		t.Fatalf("got %d NUTS-3 reports, want %d", len(reports), len(de))
	}
	if reports[0].DistrictID == "" || reports[0].PrecinctID != de[0].ID {
		t.Errorf("expected real NUTS-3 IDs, got id=%q district=%q", reports[0].PrecinctID, reports[0].DistrictID)
	}

	// Verify real Eurostat population proportions: Berlin (DE300) should have >2M eligible voters.
	berlinVoters := 0
	for _, rep := range reports {
		if rep.PrecinctID == "DE300" {
			berlinVoters = rep.EligibleVoters
			break
		}
	}
	if berlinVoters < 2_000_000 || berlinVoters > 3_500_000 {
		t.Errorf("expected Berlin (DE300) eligible voters in [2M, 3.5M], got %d", berlinVoters)
	}

	// Verify DistrictTilts shifts NUTS-1 demographics (Saxony DED vs Bavaria DE2) without skewing total population.
	s, err := LoadScenario("../scenarios/eu_starter.json")
	if err != nil {
		t.Fatalf("LoadScenario failed: %v", err)
	}
	deReports := SimulatePrecincts(s.Regions[0], s.Parties, 0.85, 0.0, 42)
	var saxonyAfD, saxonyTotal, bavariaAfD, bavariaTotal int
	for _, rep := range deReports {
		for _, v := range rep.Votes {
			if rep.DistrictID == "DED" {
				saxonyTotal += v
			} else if rep.DistrictID == "DE2" {
				bavariaTotal += v
			}
		}
		if rep.DistrictID == "DED" {
			saxonyAfD += rep.Votes["de_afd"]
		} else if rep.DistrictID == "DE2" {
			bavariaAfD += rep.Votes["de_afd"]
		}
	}
	saxShare := float64(saxonyAfD) / float64(saxonyTotal)
	bavShare := float64(bavariaAfD) / float64(bavariaTotal)
	if saxShare <= bavShare {
		t.Errorf("expected Saxony AfD share (%.3f) > Bavaria AfD share (%.3f)", saxShare, bavShare)
	}
}

func TestNUTS3UnitsReturnsCopy(t *testing.T) {
	a := NUTS3Units("de")
	origID := a[0].ID
	a[0].ID = "MUTATED"

	b := NUTS3Units("de")
	if b[0].ID != origID {
		t.Fatalf("NUTS3Units exposed shared backing slice: got %q, want %q", b[0].ID, origID)
	}
}

func TestUnitTiltPrecedence(t *testing.T) {
	r := Region{
		DistrictTilts: map[string]map[string]float64{
			"DEA":   {"social_left": 1.2},
			"DEA5":  {"social_left": 1.6},
			"DEA52": {"social_left": 2.0},
		},
	}

	if got := r.unitTilt("DEA52", "DEA", "social_left"); got != 2.0 {
		t.Errorf("NUTS-3 override: got %v, want 2.0", got)
	}
	if got := r.unitTilt("DEA5C", "DEA", "social_left"); got != 1.6 {
		t.Errorf("NUTS-2 override: got %v, want 1.6", got)
	}
	if got := r.unitTilt("DEA11", "DEA", "social_left"); got != 1.2 {
		t.Errorf("NUTS-1 fallback: got %v, want 1.2", got)
	}
	if got := r.unitTilt("DE212", "DE2", "social_left"); got != 1.0 {
		t.Errorf("unspecified district default: got %v, want 1.0", got)
	}
}
