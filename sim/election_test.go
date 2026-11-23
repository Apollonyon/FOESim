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
		if !s.UsesNationalParties() || len(s.Groups) < 8 {
			t.Errorf("expected EU political groups in starter scenario, got %d", len(s.Groups))
		}
		if len(s.Regions) != 3 {
			t.Errorf("got %d regions, want 3", len(s.Regions))
		}
		for _, r := range s.Regions {
			if len(r.Parties) == 0 {
				t.Errorf("expected national parties on region %s", r.ID)
			}
		}
	})

	t.Run("valid full EU-27 720-seat scenario", func(t *testing.T) {
		s, err := LoadScenario("../scenarios/eu27.json")
		if err != nil {
			t.Fatalf("LoadScenario(eu27.json) failed: %v", err)
		}
		if len(s.Regions) != 27 {
			t.Fatalf("got %d EU regions, want 27", len(s.Regions))
		}
		totalSeats := 0
		totalCounties := 0
		for _, r := range s.Regions {
			totalSeats += r.Seats
			n := r.EffectivePrecinctCount()
			if n <= 0 {
				t.Errorf("region %s resolved 0 NUTS-3 counties", r.ID)
			}
			totalCounties += n
		}
		if totalSeats != 720 {
			t.Errorf("EU-27 total seats = %d, want 720", totalSeats)
		}
		if totalCounties != 1152 {
			t.Errorf("EU-27 NUTS-3 counties = %d, want 1152", totalCounties)
		}
	})

	t.Run("missing file returns error", func(t *testing.T) {
		if _, err := LoadScenario("does_not_exist.json"); err == nil {
			t.Error("expected error for missing file, got nil")
		}
	})

	t.Run("validation catches empty or malformed scenarios", func(t *testing.T) {
		validGroup := VoterGroup{ID: "g1", Name: "G1", Population: 1000, Turnout: 0.5}
		cases := []struct {
			name     string
			scenario Scenario
		}{
			{name: "no parties", scenario: Scenario{Name: "empty", Regions: DefaultRegions()}},
			{name: "no regions", scenario: Scenario{Name: "empty", Parties: DefaultParties()}},
			{
				name: "no voter groups",
				scenario: Scenario{
					Name:    "empty groups",
					Parties: DefaultParties(),
					Regions: []Region{{ID: "xx", Method: MethodDHondt}},
				},
			},
			{
				name: "duplicate region id",
				scenario: Scenario{
					Name:    "dup region",
					Parties: DefaultParties(),
					Regions: []Region{
						{ID: "xx", Groups: []VoterGroup{validGroup}},
						{ID: "xx", Groups: []VoterGroup{validGroup}},
					},
				},
			},
			{
				name: "unknown EU group on party",
				scenario: Scenario{
					Name:   "bad group ref",
					Groups: []EUGroup{{ID: "epp"}},
					Regions: []Region{{
						ID:      "xx",
						Groups:  []VoterGroup{validGroup},
						Parties: []Party{{ID: "p1", GroupID: "missing_group"}},
					}},
				},
			},
			{
				name: "unknown voter group in district_tilts",
				scenario: Scenario{
					Name:    "bad tilt ref",
					Parties: DefaultParties(),
					Regions: []Region{{
						ID:            "xx",
						Groups:        []VoterGroup{validGroup},
						DistrictTilts: map[string]map[string]float64{"XX1": {"ghost_group": 1.5}},
					}},
				},
			},
			{
				name: "unknown method",
				scenario: Scenario{
					Name:    "bad method",
					Parties: DefaultParties(),
					Regions: []Region{{ID: "xx", Method: "dhont", Groups: []VoterGroup{validGroup}}},
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

func TestStreamElectionNUTS3AndTargetDuration(t *testing.T) {
	s, err := LoadScenario("../scenarios/eu_starter.json")
	if err != nil {
		t.Fatalf("LoadScenario failed: %v", err)
	}
	wantTotal := 0
	for _, r := range s.Regions {
		wantTotal += r.EffectivePrecinctCount()
	}

	cfg := ElectionConfig{
		Temperature:    0.85,
		Swing:          0.10,
		Seed:           42,
		TargetDuration: 20 * time.Millisecond,
	}

	got := 0
	for range StreamElection(context.Background(), s.Regions, s.Parties, cfg) {
		got++
	}
	if got != wantTotal {
		t.Fatalf("received %d NUTS-3 reports, want %d", got, wantTotal)
	}
}

func TestStreamElectionWavesSumExact(t *testing.T) {
	parties := DefaultParties()
	region := DefaultRegions()[0]

	full := SimulatePrecincts(region, parties, 0.85, 0.15, 42)
	wantByPrecinct := make(map[string]map[string]int, len(full))
	for _, rep := range full {
		wantByPrecinct[rep.PrecinctID] = rep.Votes
	}

	cfg := ElectionConfig{
		Temperature: 0.85,
		Swing:       0.15,
		Seed:        42,
		Waves:       3,
	}

	gotByPrecinct := make(map[string]map[string]int, len(full))
	lastPct := make(map[string]float64, len(full))
	waveCount := 0

	var run1 []PrecinctReport
	for rep := range StreamElection(context.Background(), []Region{region}, parties, cfg) {
		run1 = append(run1, rep)
		waveCount++
		if rep.ReportedPct < lastPct[rep.PrecinctID] {
			t.Errorf("precinct %s reported_pct went backwards: %v -> %v",
				rep.PrecinctID, lastPct[rep.PrecinctID], rep.ReportedPct)
		}
		lastPct[rep.PrecinctID] = rep.ReportedPct
		if gotByPrecinct[rep.PrecinctID] == nil {
			gotByPrecinct[rep.PrecinctID] = make(map[string]int)
		}
		for pid, v := range rep.Votes {
			gotByPrecinct[rep.PrecinctID][pid] += v
		}
	}

	var run2 []PrecinctReport
	for rep := range StreamElection(context.Background(), []Region{region}, parties, cfg) {
		run2 = append(run2, rep)
	}
	if !reflect.DeepEqual(run1, run2) {
		t.Errorf("same seed produced non-deterministic wave sequence")
	}

	if waveCount != len(full)*3 {
		t.Fatalf("got %d wave events, want %d", waveCount, len(full)*3)
	}
	for pid, wantVotes := range wantByPrecinct {
		if lastPct[pid] != 1.0 {
			t.Errorf("precinct %s final reported_pct = %v, want 1.0", pid, lastPct[pid])
		}
		if !reflect.DeepEqual(gotByPrecinct[pid], wantVotes) {
			t.Errorf("precinct %s wave sum %v != full %v", pid, gotByPrecinct[pid], wantVotes)
		}
	}
}
