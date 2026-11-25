package sim

import (
	"context"
	"math"
	"reflect"
	"strings"
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
			wantErr  string
		}{
			{name: "no parties", scenario: Scenario{Name: "empty", Regions: DefaultRegions()}, wantErr: "has no parties"},
			{name: "no regions", scenario: Scenario{Name: "empty", Parties: DefaultParties()}, wantErr: "has no regions"},
			{
				name: "no voter groups",
				scenario: Scenario{
					Name:    "empty groups",
					Parties: DefaultParties(),
					Regions: []Region{{ID: "xx", Seats: 10, Method: MethodDHondt}},
				},
				wantErr: "has no voter groups",
			},
			{
				name: "duplicate region id",
				scenario: Scenario{
					Name:    "dup region",
					Parties: DefaultParties(),
					Regions: []Region{
						{ID: "xx", Seats: 10, Groups: []VoterGroup{validGroup}},
						{ID: "xx", Seats: 10, Groups: []VoterGroup{validGroup}},
					},
				},
				wantErr: "duplicate region id",
			},
			{
				name: "unknown EU group on party",
				scenario: Scenario{
					Name:   "bad group ref",
					Groups: []EUGroup{{ID: "epp"}},
					Regions: []Region{{
						ID:      "xx",
						Seats:   10,
						Groups:  []VoterGroup{validGroup},
						Parties: []Party{{ID: "p1", GroupID: "missing_group"}},
					}},
				},
				wantErr: "unknown EU group",
			},
			{
				name: "unknown voter group in district_tilts",
				scenario: Scenario{
					Name:    "bad tilt ref",
					Parties: DefaultParties(),
					Regions: []Region{{
						ID:            "xx",
						Seats:         10,
						Groups:        []VoterGroup{validGroup},
						DistrictTilts: map[string]map[string]float64{"XX1": {"ghost_group": 1.5}},
					}},
				},
				wantErr: "unknown voter group",
			},
			{
				name: "unknown method",
				scenario: Scenario{
					Name:    "bad method",
					Parties: DefaultParties(),
					Regions: []Region{{ID: "xx", Seats: 10, Method: "dhont", Groups: []VoterGroup{validGroup}}},
				},
				wantErr: "unknown allocation method",
			},
			{
				name: "zero seats",
				scenario: Scenario{
					Name:    "no seats",
					Parties: DefaultParties(),
					Regions: []Region{{ID: "xx", Seats: 0, Groups: []VoterGroup{validGroup}}},
				},
				wantErr: "has no seats",
			},
			{
				name: "invalid threshold",
				scenario: Scenario{
					Name:    "bad threshold",
					Parties: DefaultParties(),
					Regions: []Region{{ID: "xx", Seats: 10, Threshold: 1.5, Groups: []VoterGroup{validGroup}}},
				},
				wantErr: "invalid threshold",
			},
			{
				name: "empty party id",
				scenario: Scenario{
					Name: "empty party",
					Regions: []Region{{
						ID:      "xx",
						Seats:   10,
						Groups:  []VoterGroup{validGroup},
						Parties: []Party{{ID: ""}},
					}},
				},
				wantErr: "empty id",
			},
			{
				name: "negative population",
				scenario: Scenario{
					Name:    "neg pop",
					Parties: DefaultParties(),
					Regions: []Region{{
						ID:     "xx",
						Seats:  10,
						Groups: []VoterGroup{{ID: "g1", Population: -10, Turnout: 0.5}},
					}},
				},
				wantErr: "negative population",
			},
			{
				name: "invalid turnout",
				scenario: Scenario{
					Name:    "bad turnout",
					Parties: DefaultParties(),
					Regions: []Region{{
						ID:     "xx",
						Seats:  10,
						Groups: []VoterGroup{{ID: "g1", Population: 1000, Turnout: 1.2}},
					}},
				},
				wantErr: "invalid turnout",
			},
			{
				name: "negative weight",
				scenario: Scenario{
					Name:    "neg weight",
					Parties: DefaultParties(),
					Regions: []Region{{
						ID:     "xx",
						Seats:  10,
						Groups: []VoterGroup{{ID: "g1", Population: 1000, Turnout: 0.5, Weights: PolicyVec{-1}}},
					}},
				},
				wantErr: "invalid weight",
			},
			{
				name: "NaN weight",
				scenario: Scenario{
					Name:    "nan weight",
					Parties: DefaultParties(),
					Regions: []Region{{
						ID:     "xx",
						Seats:  10,
						Groups: []VoterGroup{{ID: "g1", Population: 1000, Turnout: 0.5, Weights: PolicyVec{math.NaN()}}},
					}},
				},
				wantErr: "invalid weight",
			},
			{
				name: "NaN EU group position",
				scenario: Scenario{
					Name:    "nan group pos",
					Groups:  []EUGroup{{ID: "epp", Position: PolicyVec{math.NaN()}}},
					Parties: DefaultParties(),
					Regions: []Region{{ID: "xx", Seats: 10, Groups: []VoterGroup{validGroup}}},
				},
				wantErr: "non-finite position",
			},
			{
				name: "Inf EU group position",
				scenario: Scenario{
					Name:    "inf group pos",
					Groups:  []EUGroup{{ID: "epp", Position: PolicyVec{0, math.Inf(1)}}},
					Parties: DefaultParties(),
					Regions: []Region{{ID: "xx", Seats: 10, Groups: []VoterGroup{validGroup}}},
				},
				wantErr: "non-finite position",
			},
			{
				name: "NaN party appeal",
				scenario: Scenario{
					Name: "nan appeal",
					Regions: []Region{{
						ID:      "xx",
						Seats:   10,
						Groups:  []VoterGroup{validGroup},
						Parties: []Party{{ID: "p1", Appeal: math.NaN()}},
					}},
				},
				wantErr: "non-finite appeal",
			},
			{
				name: "Inf party appeal",
				scenario: Scenario{
					Name: "inf appeal",
					Regions: []Region{{
						ID:      "xx",
						Seats:   10,
						Groups:  []VoterGroup{validGroup},
						Parties: []Party{{ID: "p1", Appeal: math.Inf(-1)}},
					}},
				},
				wantErr: "non-finite appeal",
			},
			{
				name: "NaN party position",
				scenario: Scenario{
					Name: "nan party pos",
					Regions: []Region{{
						ID:      "xx",
						Seats:   10,
						Groups:  []VoterGroup{validGroup},
						Parties: []Party{{ID: "p1", Position: PolicyVec{0, 0, math.NaN()}}},
					}},
				},
				wantErr: "non-finite position",
			},
			{
				name: "Inf party position",
				scenario: Scenario{
					Name: "inf party pos",
					Regions: []Region{{
						ID:      "xx",
						Seats:   10,
						Groups:  []VoterGroup{validGroup},
						Parties: []Party{{ID: "p1", Position: PolicyVec{0, 0, math.Inf(1)}}},
					}},
				},
				wantErr: "non-finite position",
			},
			{
				name: "NaN voter group ideal",
				scenario: Scenario{
					Name:    "nan ideal",
					Parties: DefaultParties(),
					Regions: []Region{{
						ID:     "xx",
						Seats:  10,
						Groups: []VoterGroup{{ID: "g1", Population: 1000, Turnout: 0.5, Ideal: PolicyVec{math.NaN()}}},
					}},
				},
				wantErr: "non-finite ideal",
			},
			{
				name: "Inf voter group ideal",
				scenario: Scenario{
					Name:    "inf ideal",
					Parties: DefaultParties(),
					Regions: []Region{{
						ID:     "xx",
						Seats:  10,
						Groups: []VoterGroup{{ID: "g1", Population: 1000, Turnout: 0.5, Ideal: PolicyVec{0, math.Inf(-1)}}},
					}},
				},
				wantErr: "non-finite ideal",
			},
			{
				name: "negative prev_seats",
				scenario: Scenario{
					Name:      "neg prev",
					Parties:   DefaultParties(),
					PrevSeats: map[string]int{"epp": -5},
					Regions:   []Region{{ID: "xx", Seats: 10, Groups: []VoterGroup{validGroup}}},
				},
				wantErr: "negative seat count",
			},
			{
				name: "unknown EU group in prev_seats",
				scenario: Scenario{
					Name:      "unknown prev group",
					Groups:    []EUGroup{{ID: "epp"}},
					PrevSeats: map[string]int{"ghost": 10},
					Regions: []Region{{
						ID:      "xx",
						Seats:   10,
						Groups:  []VoterGroup{validGroup},
						Parties: []Party{{ID: "p1", GroupID: "epp"}},
					}},
				},
				wantErr: "unknown EU group",
			},
			{
				name: "unknown party in prev_seats without groups",
				scenario: Scenario{
					Name:      "unknown prev party",
					Parties:   DefaultParties(),
					PrevSeats: map[string]int{"ghost": 10},
					Regions:   []Region{{ID: "xx", Seats: 10, Groups: []VoterGroup{validGroup}}},
				},
				wantErr: "unknown party",
			},
			{
				name: "NaN party bonus",
				scenario: Scenario{
					Name:    "nan bonus",
					Parties: DefaultParties(),
					Regions: []Region{{
						ID:         "xx",
						Seats:      10,
						Groups:     []VoterGroup{validGroup},
						PartyBonus: map[string]float64{"epp": math.NaN()},
					}},
				},
				wantErr: "party_bonus",
			},
			{
				name: "negative district tilt",
				scenario: Scenario{
					Name:    "neg tilt",
					Parties: DefaultParties(),
					Regions: []Region{{
						ID:            "xx",
						Seats:         10,
						Groups:        []VoterGroup{validGroup},
						DistrictTilts: map[string]map[string]float64{"XX1": {"g1": -0.5}},
					}},
				},
				wantErr: "invalid multiplier",
			},
			{
				name: "invalid poll close hour",
				scenario: Scenario{
					Name:    "bad poll close",
					Parties: DefaultParties(),
					Regions: []Region{{
						ID:            "xx",
						Seats:         10,
						Groups:        []VoterGroup{validGroup},
						PollCloseHour: 31.0,
					}},
				},
				wantErr: "poll_close_hour",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.scenario.Validate()
				if err == nil {
					t.Fatalf("expected validation error for %s, got nil", tc.name)
				}
				if tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %q, want substring %q", err.Error(), tc.wantErr)
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

	baseCfg := ElectionConfig{
		Temperature: 0.85,
		Swing:       0.15,
		Seed:        42,
		Waves:       1,
	}
	wantByPrecinct := make(map[string]map[string]int, region.Precincts)
	for rep := range StreamElection(context.Background(), []Region{region}, parties, baseCfg) {
		wantByPrecinct[rep.PrecinctID] = rep.Votes
	}

	cfg := baseCfg
	cfg.Waves = 3

	gotByPrecinct := make(map[string]map[string]int, len(wantByPrecinct))
	lastPct := make(map[string]float64, len(wantByPrecinct))
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

	if waveCount != len(wantByPrecinct)*3 {
		t.Fatalf("got %d wave events, want %d", waveCount, len(wantByPrecinct)*3)
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

func TestHierarchicalCorrelatedSwingsSpread(t *testing.T) {
	s, err := LoadScenario("../scenarios/eu27.json")
	if err != nil {
		t.Fatalf("LoadScenario(eu27.json) failed: %v", err)
	}

	const runs = 30
	eppSeats := make([]float64, runs)
	pfeSeats := make([]float64, runs)
	for i := range runs {
		cfg := ElectionConfig{
			Temperature: 0.45,
			Swing:       0.15,
			Seed:        int64(100 + i),
		}
		euShift := euPolicyShock(cfg.Seed, cfg.Swing)
		regionSeats := make(map[string]map[string]int, len(s.Regions))
		for ri, reg := range s.Regions {
			votes := make(map[string]int)
			for _, rep := range simulatePrecincts(reg, s.Parties, cfg.Temperature, cfg.Swing, cfg.Seed+int64(ri), euShift) {
				for pid, v := range rep.Votes {
					votes[pid] += v
				}
			}
			regionSeats[reg.ID] = AllocateSeats(votes, reg, reg.EffectiveParties(s.Parties))
		}
		eu := s.AggregateEUSeats(regionSeats)
		eppSeats[i] = float64(eu["epp"])
		pfeSeats[i] = float64(eu["pfe"])
	}

	stdDev := func(vals []float64) (mean, sd float64) {
		for _, v := range vals {
			mean += v
		}
		mean /= float64(len(vals))
		var sumSq float64
		for _, v := range vals {
			d := v - mean
			sumSq += d * d
		}
		return mean, math.Sqrt(sumSq / float64(len(vals)))
	}

	eppMean, eppSD := stdDev(eppSeats)
	pfeMean, pfeSD := stdDev(pfeSeats)
	t.Logf("30-seed spread: EPP mean=%.1f sd=%.1f | PfE mean=%.1f sd=%.1f", eppMean, eppSD, pfeMean, pfeSD)

	if eppSD < 3.0 || pfeSD < 3.0 {
		t.Errorf("expected realistic multi-seed seat spread (>=3 seats), got EPP sd=%.2f, PfE sd=%.2f", eppSD, pfeSD)
	}
	if eppSD > 45.0 || pfeSD > 45.0 {
		t.Errorf("multi-seed seat spread too extreme (>45 seats), got EPP sd=%.2f, PfE sd=%.2f", eppSD, pfeSD)
	}
}

func TestExitPollSeatsMatchesZeroSwing(t *testing.T) {
	parties := DefaultParties()
	regions := DefaultRegions()
	cfg := ElectionConfig{Temperature: 0.85, Swing: 0, Seed: 42}

	exitSeats := ExitPollSeats(regions, parties, cfg)
	st := NewElectionState(Scenario{Name: "zero", Parties: parties, Regions: regions})
	for rep := range StreamElection(context.Background(), regions, parties, cfg) {
		st.Apply(rep)
	}
	for _, r := range regions {
		if !reflect.DeepEqual(exitSeats[r.ID], st.RegionSeats(r.ID)) {
			t.Errorf("region %s zero-swing exit poll %v != final %v", r.ID, exitSeats[r.ID], st.RegionSeats(r.ID))
		}
	}
}

func TestPrepareElectionMatchesStreamAndExitPoll(t *testing.T) {
	parties := DefaultParties()
	regions := DefaultRegions()
	cfg := ElectionConfig{
		Temperature: 0.85,
		Swing:       0.15,
		Seed:        42,
		Waves:       3,
	}

	prep := PrepareElection(regions, parties, cfg)
	if !reflect.DeepEqual(prep.ExitPollSeats(), ExitPollSeats(regions, parties, cfg)) {
		t.Fatalf("PreparedElection.ExitPollSeats() diverged from ExitPollSeats()")
	}

	collectByPrecinct := func(ch <-chan PrecinctReport) map[string]map[string]int {
		out := make(map[string]map[string]int)
		for rep := range ch {
			if out[rep.PrecinctID] == nil {
				out[rep.PrecinctID] = make(map[string]int)
			}
			for pid, v := range rep.Votes {
				out[rep.PrecinctID][pid] += v
			}
		}
		return out
	}

	gotPrep := collectByPrecinct(prep.Stream(context.Background()))
	gotStream := collectByPrecinct(StreamElection(context.Background(), regions, parties, cfg))
	if !reflect.DeepEqual(gotPrep, gotStream) {
		t.Fatalf("PreparedElection.Stream() votes diverged from StreamElection()")
	}
}
