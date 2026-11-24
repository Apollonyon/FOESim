package sim

import (
	"math"
	"testing"
)

func testStateScenario() Scenario {
	return Scenario{
		Name: "state test",
		Groups: []EUGroup{
			{ID: "epp", ShortName: "EPP", Position: PolicyVec{0.4, 0.2, 0.5, 0.5, 0.0, 0.3}},
			{ID: "sd", ShortName: "S&D", Position: PolicyVec{-0.4, -0.4, 0.5, 0.1, 0.4, -0.2}},
			{ID: "greens", ShortName: "GREENS", Position: PolicyVec{-0.3, -0.7, 0.7, 0.0, 0.9, -0.6}},
			{ID: NonInscritsID, ShortName: "NI", Position: PolicyVec{0.0, 0.8, -0.8, -0.5, -0.6, 0.8}},
		},
		Regions: []Region{
			{
				ID:        "de",
				Name:      "Germany",
				Seats:     10,
				Precincts: 2,
				Method:    MethodSainteLague,
				Groups:    []VoterGroup{{ID: "g1", Population: 1000, Turnout: 0.7}},
				Parties: []Party{
					{ID: "cdu", ShortName: "CDU", GroupID: "epp"},
					{ID: "spd", ShortName: "SPD", GroupID: "sd"},
					{ID: "ind", ShortName: "IND", GroupID: ""}, // rolls up to NonInscritsID
				},
			},
			{
				ID:        "fr",
				Name:      "France",
				Seats:     10,
				Precincts: 2,
				Method:    MethodDHondt,
				Groups:    []VoterGroup{{ID: "g1", Population: 1000, Turnout: 0.7}},
				Parties: []Party{
					{ID: "lr", ShortName: "LR", GroupID: "epp"},
					{ID: "eelv", ShortName: "EELV", GroupID: "greens"},
				},
			},
		},
	}
}

func TestElectionStateApply(t *testing.T) {
	st := NewElectionState(testStateScenario())

	completed, total, pct := st.Progress()
	if completed != 0 || total != 4 || pct != 0 {
		t.Fatalf("initial Progress() = (%d, %d, %g), want (0, 4, 0)", completed, total, pct)
	}

	st.Apply(PrecinctReport{
		RegionID:    "de",
		PrecinctID:  "de-1",
		ReportedPct: 1.0,
		Votes:       map[string]int{"cdu": 500, "spd": 300, "ind": 200},
	})

	votes := st.RegionVotes("de")
	if votes["cdu"] != 500 || votes["spd"] != 300 || votes["ind"] != 200 {
		t.Fatalf("RegionVotes(de) after precinct 1 = %v", votes)
	}
	seats := st.RegionSeats("de")
	if seats["cdu"]+seats["spd"]+seats["ind"] != 10 {
		t.Fatalf("RegionSeats(de) sum = %d, want 10 (%v)", seats["cdu"]+seats["spd"]+seats["ind"], seats)
	}

	eu := st.EUSeats()
	if eu["epp"] != seats["cdu"] || eu["sd"] != seats["spd"] || eu[NonInscritsID] != seats["ind"] {
		t.Fatalf("EUSeats() = %v, want rollup of %v", eu, seats)
	}
	if len(st.Coalitions()) == 0 {
		t.Fatal("expected at least one coalition after first region reports")
	}

	// Second precinct in the same region accumulates votes.
	st.Apply(PrecinctReport{
		RegionID:    "de",
		PrecinctID:  "de-2",
		ReportedPct: 1.0,
		Votes:       map[string]int{"cdu": 100, "spd": 400},
	})
	votes = st.RegionVotes("de")
	if votes["cdu"] != 600 || votes["spd"] != 700 || votes["ind"] != 200 {
		t.Fatalf("RegionVotes(de) after precinct 2 = %v", votes)
	}

	// Unknown region report is ignored safely.
	st.Apply(PrecinctReport{
		RegionID:    "unknown",
		PrecinctID:  "unknown-1",
		ReportedPct: 1.0,
		Votes:       map[string]int{"ghost": 999},
	})
	completed, total, pct = st.Progress()
	if completed != 2 || total != 4 || math.Abs(pct-50.0) > 1e-9 {
		t.Fatalf("Progress() after 2/4 precincts + unknown = (%d, %d, %g), want (2, 4, 50)", completed, total, pct)
	}
}

func TestElectionStateMultiWaveProgress(t *testing.T) {
	s := testStateScenario()
	s.Regions = s.Regions[:1] // single region with 2 precincts
	st := NewElectionState(s)

	// Wave 1: 40% of precinct 1.
	st.Apply(PrecinctReport{RegionID: "de", PrecinctID: "de-1", ReportedPct: 0.4, Votes: map[string]int{"cdu": 100}})
	completed, total, pct := st.Progress()
	if completed != 0 || total != 2 || math.Abs(pct-20.0) > 1e-9 {
		t.Fatalf("after 40%% wave: Progress() = (%d, %d, %g), want (0, 2, 20)", completed, total, pct)
	}

	// Stale/out-of-order lower percentage must not move progress backwards.
	st.Apply(PrecinctReport{RegionID: "de", PrecinctID: "de-1", ReportedPct: 0.3, Votes: map[string]int{"cdu": 50}})
	completed, _, pct = st.Progress()
	if completed != 0 || math.Abs(pct-20.0) > 1e-9 {
		t.Fatalf("after stale wave: Progress() = (%d, %g), want (0, 20)", completed, pct)
	}

	// Wave 2 completes precinct 1.
	st.Apply(PrecinctReport{RegionID: "de", PrecinctID: "de-1", ReportedPct: 1.0, Votes: map[string]int{"cdu": 150}})
	completed, _, pct = st.Progress()
	if completed != 1 || math.Abs(pct-50.0) > 1e-9 {
		t.Fatalf("after completing de-1: Progress() = (%d, %g), want (1, 50)", completed, pct)
	}

	// Duplicate 1.0 report must not double-count completed.
	st.Apply(PrecinctReport{RegionID: "de", PrecinctID: "de-1", ReportedPct: 1.0, Votes: map[string]int{"cdu": 10}})
	completed, _, pct = st.Progress()
	if completed != 1 || math.Abs(pct-50.0) > 1e-9 {
		t.Fatalf("after duplicate 1.0 wave: Progress() = (%d, %g), want (1, 50)", completed, pct)
	}

	// Omitted/zero ReportedPct defaults to 1.0 (full precinct report).
	st.Apply(PrecinctReport{RegionID: "de", PrecinctID: "de-2", ReportedPct: 0, Votes: map[string]int{"spd": 200}})
	completed, total, pct = st.Progress()
	if completed != 2 || total != 2 || math.Abs(pct-100.0) > 1e-9 {
		t.Fatalf("after zero-pct full report: Progress() = (%d, %d, %g), want (2, 2, 100)", completed, total, pct)
	}
}

func TestElectionStateSeedSeats(t *testing.T) {
	st := NewElectionState(testStateScenario())

	seed := map[string]map[string]int{
		"de": {"cdu": 6, "spd": 4},
		"fr": {"lr": 5, "eelv": 5},
	}
	st.SeedSeats(seed)

	// Mutating caller's seed map after SeedSeats must not affect internal state.
	seed["de"]["cdu"] = 999

	if got := st.RegionSeats("de")["cdu"]; got != 6 {
		t.Fatalf("RegionSeats(de)[cdu] = %d, want 6 (SeedSeats did not clone input)", got)
	}
	eu := st.EUSeats()
	if eu["epp"] != 11 || eu["sd"] != 4 || eu["greens"] != 5 {
		t.Fatalf("seeded EUSeats() = %v, want epp:11 sd:4 greens:5", eu)
	}
	if len(st.Coalitions()) == 0 {
		t.Fatal("expected coalitions to be populated from seeded seats")
	}
	if completed, _, pct := st.Progress(); completed != 0 || pct != 0 {
		t.Fatalf("SeedSeats should not advance Progress(), got completed=%d pct=%g", completed, pct)
	}

	// Applying real votes for Germany replaces Germany's seeded seats while preserving France's.
	st.Apply(PrecinctReport{
		RegionID:    "de",
		PrecinctID:  "de-1",
		ReportedPct: 1.0,
		Votes:       map[string]int{"spd": 1000},
	})
	if got := st.RegionSeats("de")["spd"]; got != 10 {
		t.Fatalf("after Apply(de), RegionSeats(de)[spd] = %d, want 10", got)
	}
	if got := st.RegionSeats("fr")["eelv"]; got != 5 {
		t.Fatalf("after Apply(de), seeded RegionSeats(fr)[eelv] = %d, want 5", got)
	}
	eu = st.EUSeats()
	if eu["epp"] != 5 || eu["sd"] != 10 || eu["greens"] != 5 {
		t.Fatalf("after Apply(de), EUSeats() = %v, want epp:5 sd:10 greens:5", eu)
	}
}

func TestElectionStateDefensiveClones(t *testing.T) {
	st := NewElectionState(testStateScenario())
	st.Apply(PrecinctReport{
		RegionID:    "de",
		PrecinctID:  "de-1",
		ReportedPct: 1.0,
		Votes:       map[string]int{"cdu": 600, "spd": 400},
	})

	// Mutate every getter's returned map/slice.
	votes := st.RegionVotes("de")
	votes["cdu"] = 999999
	delete(votes, "spd")

	seats := st.RegionSeats("de")
	seats["cdu"] = 999

	eu := st.EUSeats()
	eu["epp"] = 999

	coalitions := st.Coalitions()
	if len(coalitions) == 0 {
		t.Fatal("expected non-empty coalitions")
	}
	origSeats := coalitions[0].Seats
	origGroupID := coalitions[0].GroupIDs[0]
	coalitions[0].Seats = 999
	coalitions[0].GroupIDs[0] = "tampered"

	// Re-read and verify internal state is intact.
	if got := st.RegionVotes("de"); got["cdu"] != 600 || got["spd"] != 400 {
		t.Fatalf("RegionVotes mutated internal state: %v", got)
	}
	if got := st.RegionSeats("de")["cdu"]; got == 999 {
		t.Fatalf("RegionSeats mutated internal state: %d", got)
	}
	if got := st.EUSeats()["epp"]; got == 999 {
		t.Fatalf("EUSeats mutated internal state: %d", got)
	}
	if got := st.Coalitions()[0]; got.Seats != origSeats || got.GroupIDs[0] != origGroupID {
		t.Fatalf("Coalitions mutated internal state: got seats=%d group0=%q, want seats=%d group0=%q",
			got.Seats, got.GroupIDs[0], origSeats, origGroupID)
	}
}

func TestElectionStateApplyChangeAndZeroSeatRemoval(t *testing.T) {
	st := NewElectionState(testStateScenario())

	// First report allocates 10 seats in Germany -> EU seats change.
	changed := st.Apply(PrecinctReport{
		RegionID:    "de",
		PrecinctID:  "de-1",
		ReportedPct: 1.0,
		Votes:       map[string]int{"cdu": 500, "spd": 300, "ind": 200},
	})
	if !changed {
		t.Fatal("expected first Apply to report EU seat change")
	}

	// Tiny follow-up vote batch that doesn't alter seat counts -> false.
	changed = st.Apply(PrecinctReport{
		RegionID:    "de",
		PrecinctID:  "de-2",
		ReportedPct: 1.0,
		Votes:       map[string]int{"cdu": 1},
	})
	if changed {
		t.Fatal("expected unchanged seat allocation to return false")
	}

	// Overwhelming SPD batch drops CDU (epp) and IND (ni) to 0 seats; keys must be removed.
	changed = st.Apply(PrecinctReport{
		RegionID:    "de",
		PrecinctID:  "de-2",
		ReportedPct: 1.0,
		Votes:       map[string]int{"spd": 100_000},
	})
	if !changed {
		t.Fatal("expected sweeping SPD batch to report EU seat change")
	}
	eu := st.EUSeats()
	if _, exists := eu["epp"]; exists {
		t.Errorf("expected 0-seat group epp to be deleted from EUSeats, got %v", eu)
	}
	if _, exists := eu[NonInscritsID]; exists {
		t.Errorf("expected 0-seat group ni to be deleted from EUSeats, got %v", eu)
	}
	if eu["sd"] != 10 {
		t.Errorf("EUSeats[sd] = %d, want 10", eu["sd"])
	}
}

func TestElectionStateClampsReportedPct(t *testing.T) {
	s := testStateScenario()
	s.Regions = s.Regions[:1] // 2 precincts
	st := NewElectionState(s)

	st.Apply(PrecinctReport{
		RegionID:    "de",
		PrecinctID:  "de-1",
		ReportedPct: 1.5,
		Votes:       map[string]int{"cdu": 100},
	})
	completed, total, pct := st.Progress()
	if completed != 1 || total != 2 || math.Abs(pct-50.0) > 1e-9 {
		t.Fatalf("after clamped >1.0 report: Progress() = (%d, %d, %g), want (1, 2, 50)", completed, total, pct)
	}

	st.Apply(PrecinctReport{
		RegionID:    "de",
		PrecinctID:  "de-2",
		ReportedPct: 2.0,
		Votes:       map[string]int{"spd": 100},
	})
	completed, total, pct = st.Progress()
	if completed != 2 || total != 2 || math.Abs(pct-100.0) > 1e-9 {
		t.Fatalf("after second clamped >1.0 report: Progress() = (%d, %d, %g), want (2, 2, 100)", completed, total, pct)
	}
}

func BenchmarkElectionStateApply(b *testing.B) {
	s, err := LoadScenario("../scenarios/eu27.json")
	if err != nil {
		b.Fatal(err)
	}
	prep := PrepareElection(s.Regions, s.Parties, ElectionConfig{
		Temperature: 0.45,
		Swing:       0.15,
		Seed:        42,
	})
	var reports []PrecinctReport
	for _, regionReps := range prep.perRegion {
		reports = append(reports, regionReps...)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		st := NewElectionState(s)
		for _, rep := range reports {
			st.Apply(rep)
		}
	}
}
