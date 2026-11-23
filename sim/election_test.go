package sim

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestSimulatePrecinctsDeterministic(t *testing.T) {
	parties := DefaultParties()
	region := DefaultRegions()[0]

	a := SimulatePrecincts(region, parties, 0.85, 0.15, 42)
	b := SimulatePrecincts(region, parties, 0.85, 0.15, 42)

	if len(a) != region.Precincts {
		t.Fatalf("got %d reports, want %d", len(a), region.Precincts)
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("same seed produced different precinct reports")
	}

	// With zero swing, summed precinct votes should closely match SimulateRegion.
	zeroSwing := SimulatePrecincts(region, parties, 0.85, 0.0, 42)
	sumVotes := make(map[string]int, len(parties))
	for _, rep := range zeroSwing {
		for id, v := range rep.Votes {
			sumVotes[id] += v
		}
	}

	expectedVotes, _ := SimulateRegion(region, parties, 0.85)
	maxRoundingDiff := float64(len(region.Groups) * region.Precincts)
	for _, p := range parties {
		diff := math.Abs(float64(sumVotes[p.ID] - expectedVotes[p.ID]))
		if diff > maxRoundingDiff {
			t.Errorf("party %s: precinct sum %d vs region %d (diff %.0f > %.0f)",
				p.ID, sumVotes[p.ID], expectedVotes[p.ID], diff, maxRoundingDiff)
		}
	}
}

func TestPartyBonusDoesNotMutateInput(t *testing.T) {
	parties := DefaultParties()
	originalAppeals := make([]float64, len(parties))
	for i, p := range parties {
		originalAppeals[i] = p.Appeal
	}

	region := DefaultRegions()[0]
	baseVotes, _ := SimulateRegion(region, parties, 0.85)

	region.PartyBonus = map[string]float64{"greens": 0.50}
	boostedVotes, _ := SimulateRegion(region, parties, 0.85)

	if boostedVotes["greens"] <= baseVotes["greens"] {
		t.Errorf("expected greens votes to increase with bonus: got %d, base %d",
			boostedVotes["greens"], baseVotes["greens"])
	}

	for i, p := range parties {
		if p.Appeal != originalAppeals[i] {
			t.Errorf("party %s appeal mutated: got %v, want %v", p.ID, p.Appeal, originalAppeals[i])
		}
	}
}

func TestStreamElection(t *testing.T) {
	parties := DefaultParties()
	regions := DefaultRegions()

	wantTotal := 0
	for _, r := range regions {
		wantTotal += r.Precincts
	}

	cfg := ElectionConfig{
		Temperature: 0.85,
		Swing:       0.15,
		Seed:        42,
		BaseDelay:   0,
	}

	seenPerRegion := make(map[string]int, len(regions))
	total := 0
	for rep := range StreamElection(context.Background(), regions, parties, cfg) {
		seenPerRegion[rep.RegionID]++
		total++
	}

	if total != wantTotal {
		t.Fatalf("received %d reports, want %d", total, wantTotal)
	}
	for _, r := range regions {
		if seenPerRegion[r.ID] != r.Precincts {
			t.Errorf("region %s: got %d reports, want %d", r.ID, seenPerRegion[r.ID], r.Precincts)
		}
	}
}

func TestStreamElectionCancel(t *testing.T) {
	parties := DefaultParties()
	regions := DefaultRegions()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := ElectionConfig{
		Temperature: 0.85,
		Swing:       0.15,
		Seed:        42,
		BaseDelay:   50 * time.Millisecond,
	}

	ch := StreamElection(ctx, regions, parties, cfg)

	// Read one report, then cancel immediately and ensure the channel closes promptly.
	if _, ok := <-ch; !ok {
		t.Fatal("channel closed before receiving any report")
	}
	cancel()

	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StreamElection did not close channel promptly after context cancellation")
	}
}

func TestLoadScenario(t *testing.T) {
	t.Run("valid starter scenario", func(t *testing.T) {
		s, err := LoadScenario("../scenarios/eu_starter.json")
		if err != nil {
			t.Fatalf("LoadScenario failed: %v", err)
		}
		if s.Name == "" {
			t.Error("expected non-empty scenario name")
		}
		if len(s.Parties) != 8 {
			t.Errorf("got %d parties, want 8", len(s.Parties))
		}
		if len(s.Regions) != 3 {
			t.Errorf("got %d regions, want 3", len(s.Regions))
		}
		if len(s.Regions[0].PartyBonus) == 0 {
			t.Error("expected party_bonus to be populated on first region")
		}
	})

	t.Run("missing file returns error", func(t *testing.T) {
		if _, err := LoadScenario("does_not_exist.json"); err == nil {
			t.Error("expected error for missing file, got nil")
		}
	})

	t.Run("validation catches empty or malformed scenarios", func(t *testing.T) {
		cases := []struct {
			name     string
			scenario Scenario
		}{
			{name: "no parties", scenario: Scenario{Name: "empty", Regions: DefaultRegions()}},
			{name: "no regions", scenario: Scenario{Name: "empty", Parties: DefaultParties()}},
			{
				name:    "unknown method",
				scenario: Scenario{
					Name:    "bad method",
					Parties: DefaultParties(),
					Regions: []Region{{ID: "xx", Method: "dhont"}},
				},
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if err := tc.scenario.validate(); err == nil {
					t.Errorf("expected validation error for %s, got nil", tc.name)
				}
			})
		}
	})
}
