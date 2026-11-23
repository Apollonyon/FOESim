package sim

import (
	"slices"
	"testing"
)

func TestFindCoalitions(t *testing.T) {
	groups := []EUGroup{
		{ID: "left", ShortName: "LEFT", Position: PolicyVec{-0.85, -0.70, -0.25, -0.75, 0.65, -0.75}},
		{ID: "sd", ShortName: "S&D", Position: PolicyVec{-0.45, -0.45, 0.55, 0.15, 0.45, -0.25}},
		{ID: "greens", ShortName: "GREENS", Position: PolicyVec{-0.30, -0.80, 0.80, 0.15, 0.95, -0.70}},
		{ID: "renew", ShortName: "RENEW", Position: PolicyVec{0.45, -0.35, 0.80, 0.50, 0.25, -0.05}},
		{ID: "epp", ShortName: "EPP", Position: PolicyVec{0.45, 0.30, 0.50, 0.60, -0.05, 0.45}},
		{ID: "ecr", ShortName: "ECR", Position: PolicyVec{0.50, 0.70, -0.35, 0.65, -0.45, 0.80}},
		{ID: "pfe", ShortName: "PfE", Position: PolicyVec{0.05, 0.85, -0.80, -0.30, -0.70, 0.95}},
		{ID: "esn", ShortName: "ESN", Position: PolicyVec{0.35, 0.95, -0.95, -0.60, -0.90, 1.00}},
		{ID: "ni", ShortName: "NI", Position: PolicyVec{-0.25, 0.35, -0.65, -0.80, -0.25, 0.65}},
	}

	// Realistic 720-seat European Parliament distribution (majority = 361).
	seats := map[string]int{
		"epp":    188,
		"sd":     136,
		"pfe":    84,
		"ecr":    78,
		"renew":  77,
		"greens": 53,
		"left":   46,
		"esn":    25,
		"ni":     33,
	}

	got := FindCoalitions(seats, groups, 5)
	if len(got) == 0 {
		t.Fatal("expected viable majority coalitions, got none")
	}

	for i, c := range got {
		if c.Seats < 361 {
			t.Errorf("coalition %d (%v) has %d seats, want >= 361", i, c.Names, c.Seats)
		}
		if slices.Contains(c.GroupIDs, NonInscritsID) {
			t.Errorf("coalition %v should exclude Non-Inscrits", c.GroupIDs)
		}
		if i > 0 && c.Spread < got[i-1].Spread {
			t.Errorf("coalitions not sorted by spread: %v before %v", got[i-1].Spread, c.Spread)
		}
	}

	// Classic Grand Coalition (EPP + S&D + Renew = 401 seats) should be among top 3 most cohesive.
	foundGrand := false
	for _, c := range got[:min(3, len(got))] {
		if len(c.GroupIDs) == 3 &&
			slices.Contains(c.GroupIDs, "epp") &&
			slices.Contains(c.GroupIDs, "sd") &&
			slices.Contains(c.GroupIDs, "renew") {
			foundGrand = true
		}
	}
	if !foundGrand {
		t.Errorf("expected EPP+S&D+Renew in top 3 coalitions, got %+v", got)
	}

	t.Run("empty seats returns nil", func(t *testing.T) {
		if res := FindCoalitions(nil, groups, 5); len(res) != 0 {
			t.Errorf("got %v, want empty", res)
		}
	})

	t.Run("single party outright majority has 100 cohesion", func(t *testing.T) {
		res := FindCoalitions(map[string]int{"epp": 400, "sd": 320}, groups, 0)
		if len(res) != 1 || len(res[0].GroupIDs) != 1 || res[0].GroupIDs[0] != "epp" || res[0].Cohesion != 100 {
			t.Errorf("got %+v, want single EPP coalition with cohesion 100", res)
		}
	})

	t.Run("impossible majority when Non-Inscrits holds over half", func(t *testing.T) {
		res := FindCoalitions(map[string]int{"ni": 400, "epp": 200, "sd": 120}, groups, 5)
		if len(res) != 0 {
			t.Errorf("got %+v, want no valid coalitions", res)
		}
	})
}

func BenchmarkFindCoalitions(b *testing.B) {
	s, err := LoadScenario("../scenarios/eu_starter.json")
	if err != nil {
		b.Fatal(err)
	}
	seats := map[string]int{
		"epp": 188, "sd": 136, "pfe": 84, "ecr": 78,
		"renew": 77, "greens": 53, "left": 46, "esn": 25, "ni": 33,
	}
	b.ResetTimer()
	for b.Loop() {
		FindCoalitions(seats, s.Groups, 5)
	}
}
