package sim

import "testing"

func TestAllocateRegionSeatsSplitEVAndParallelMMM(t *testing.T) {
	const pack = "test_district_pack"
	RegisterGeoPack(pack, map[string][]PrecinctUnit{
		"me": {
			{ID: "ME_A", DistrictID: "ME-1", Name: "Cumberland", Weight: 0.55},
			{ID: "ME_B", DistrictID: "ME-2", Name: "Penobscot", Weight: 0.45},
		},
		"jp_block": {
			{ID: "JP_1", DistrictID: "JP-1", Name: "Seat 1", Weight: 0.5},
			{ID: "JP_2", DistrictID: "JP-2", Name: "Seat 2", Weight: 0.5},
		},
		"uk_region": {
			{ID: "UK_1", DistrictID: "UK-1", Name: "Constituency 1", Weight: 0.5},
			{ID: "UK_2", DistrictID: "UK-2", Name: "Constituency 2", Weight: 0.5},
		},
	})

	// Maine: 4 EVs = 2 at-large (FPTP) + ME-1 (1 EV) + ME-2 (1 EV).
	// Dem wins statewide (520 vs 480) and ME-1 (350 vs 200), Rep wins ME-2 (280 vs 170).
	me := Region{ID: "me", Seats: 4, DistrictSeats: 1, Method: MethodFPTP, GeoPack: pack}
	seats, winners := AllocateRegionSeats(
		map[string]int{"dem": 520, "rep": 480},
		map[string]map[string]int{
			"ME-1": {"dem": 350, "rep": 200},
			"ME-2": {"dem": 170, "rep": 280},
		},
		me,
		nil,
	)
	if seats["dem"] != 3 || seats["rep"] != 1 {
		t.Errorf("Maine split EVs = %v, want dem:3 rep:1", seats)
	}
	if winners["ME-1"] != "dem" || winners["ME-2"] != "rep" {
		t.Errorf("Maine district winners = %v, want ME-1:dem ME-2:rep", winners)
	}

	// Japan-style Parallel MMM: 2 SMDs (both won by A) + 4 D'Hondt PR seats (50/50 split -> A:2, B:2).
	jp := Region{ID: "jp_block", Seats: 6, DistrictSeats: 1, Method: MethodDHondt, GeoPack: pack}
	jpSeats, jpWin := AllocateRegionSeats(
		map[string]int{"a": 500, "b": 500},
		map[string]map[string]int{
			"JP-1": {"a": 260, "b": 240},
			"JP-2": {"a": 260, "b": 240},
		},
		jp,
		nil,
	)
	if jpSeats["a"] != 4 || jpSeats["b"] != 2 {
		t.Errorf("Parallel MMM seats = %v, want a:4 b:2", jpSeats)
	}
	if jpWin["JP-1"] != "a" || jpWin["JP-2"] != "a" {
		t.Errorf("Parallel MMM winners = %v", jpWin)
	}

	// UK-style pure single-member constituencies: 2 seats, 2 districts, 0 at-large.
	uk := Region{ID: "uk_region", Seats: 2, DistrictSeats: 1, Method: MethodFPTP, GeoPack: pack}
	ukSeats, _ := AllocateRegionSeats(
		map[string]int{"lab": 600, "con": 400},
		map[string]map[string]int{
			"UK-1": {"lab": 450, "con": 100},
			"UK-2": {"lab": 150, "con": 300},
		},
		uk,
		nil,
	)
	if ukSeats["lab"] != 1 || ukSeats["con"] != 1 {
		t.Errorf("UK constituency seats = %v, want lab:1 con:1", ukSeats)
	}
}

func TestElectionStatePartialDistrictReportingPreservesTotalSeats(t *testing.T) {
	const pack = "test_partial_district_pack"
	RegisterGeoPack(pack, map[string][]PrecinctUnit{
		"ne": {
			{ID: "NE_1A", DistrictID: "NE-1", Name: "Lancaster", Weight: 0.34},
			{ID: "NE_2A", DistrictID: "NE-2", Name: "Douglas", Weight: 0.33},
			{ID: "NE_3A", DistrictID: "NE-3", Name: "Hall", Weight: 0.33},
		},
	})

	sc := Scenario{
		Name: "Nebraska Partial Test",
		Parties: []Party{
			{ID: "dem", Name: "Dem", ShortName: "D", Color: "#2563eb"},
			{ID: "rep", Name: "Rep", ShortName: "R", Color: "#dc2626"},
		},
		Regions: []Region{
			{
				ID:            "ne",
				Name:          "Nebraska",
				Seats:         5,
				DistrictSeats: 1,
				Method:        MethodFPTP,
				GeoPack:       pack,
				Groups:        []VoterGroup{{ID: "all", Name: "All", Population: 1000, Turnout: 0.7}},
			},
		},
	}
	if err := sc.Validate(); err != nil {
		t.Fatalf("expected valid scenario, got %v", err)
	}

	st := NewElectionState(sc)
	st.SeedSeats(map[string]map[string]int{"ne": {"rep": 4, "dem": 1}})
	st.SeedDistrictWinners(map[string]map[string]string{
		"ne": {"NE-1": "rep", "NE-2": "dem", "NE-3": "rep"},
	})

	// Only NE-2 reports first: Dem wins NE-2 AND temporarily leads statewide (2 at-large + NE-2 = 3),
	// while NE-1 & NE-3 keep their seeded Rep winners (2) -> total stays 5!
	st.Apply(PrecinctReport{
		RegionID:    "ne",
		DistrictID:  "NE-2",
		PrecinctID:  "NE_2A",
		ReportedPct: 1.0,
		Votes:       map[string]int{"dem": 600, "rep": 400},
	})
	got := st.RegionSeats("ne")
	if got["dem"]+got["rep"] != 5 {
		t.Fatalf("partial Nebraska total EVs = %d (%v), want 5", got["dem"]+got["rep"], got)
	}
	if got["dem"] != 3 || got["rep"] != 2 {
		t.Errorf("partial Nebraska seats = %v, want dem:3 rep:2", got)
	}

	// NE-1 and NE-3 report heavily Rep -> flips statewide back to Rep (4 R, 1 D).
	st.Apply(PrecinctReport{
		RegionID:    "ne",
		DistrictID:  "NE-1",
		PrecinctID:  "NE_1A",
		ReportedPct: 1.0,
		Votes:       map[string]int{"dem": 300, "rep": 700},
	})
	st.Apply(PrecinctReport{
		RegionID:    "ne",
		DistrictID:  "NE-3",
		PrecinctID:  "NE_3A",
		ReportedPct: 1.0,
		Votes:       map[string]int{"dem": 200, "rep": 800},
	})
	got = st.RegionSeats("ne")
	if got["rep"] != 4 || got["dem"] != 1 {
		t.Errorf("final Nebraska seats = %v, want rep:4 dem:1", got)
	}
	dw := st.DistrictWinners("ne")
	if dw["NE-1"] != "rep" || dw["NE-2"] != "dem" || dw["NE-3"] != "rep" {
		t.Errorf("final Nebraska district winners = %v", dw)
	}
}

func TestScenarioValidateDistrictSeats(t *testing.T) {
	const pack = "test_validate_district_pack"
	RegisterGeoPack(pack, map[string][]PrecinctUnit{
		"r1": {
			{ID: "U1", DistrictID: "D1", Name: "U1", Weight: 0.5},
			{ID: "U2", DistrictID: "D2", Name: "U2", Weight: 0.5},
		},
	})

	makeScenario := func(seats, districtSeats int, gp string) Scenario {
		return Scenario{
			Name:    "test",
			Parties: DefaultParties(),
			Regions: []Region{{
				ID:            "r1",
				Name:          "R1",
				Seats:         seats,
				DistrictSeats: districtSeats,
				GeoPack:       gp,
				Groups:        []VoterGroup{{ID: "g", Name: "G", Population: 100, Turnout: 0.5}},
			}},
		}
	}

	if err := makeScenario(4, -1, pack).Validate(); err == nil {
		t.Errorf("expected error for negative district_seats")
	}
	if err := makeScenario(4, 1, "").Validate(); err == nil {
		t.Errorf("expected error for district_seats without geopack districts")
	}
	if err := makeScenario(1, 1, pack).Validate(); err == nil {
		t.Errorf("expected error when district seats exceed region total seats")
	}
	if err := makeScenario(2, 1, pack).Validate(); err != nil {
		t.Errorf("expected valid exact-fit district_seats, got %v", err)
	}
}

func TestUSA2024ScenarioBaseline(t *testing.T) {
	sc, err := LoadScenario("../scenarios/usa2024.json")
	if err != nil {
		t.Fatalf("LoadScenario(usa2024.json) failed: %v", err)
	}
	if len(sc.Regions) != 51 {
		t.Fatalf("got %d US regions, want 51 (50 states + DC)", len(sc.Regions))
	}

	totalEV := 0
	for _, r := range sc.Regions {
		totalEV += r.Seats
	}
	if totalEV != 538 {
		t.Fatalf("got %d total EVs, want 538", totalEV)
	}

	meta := sc.EffectiveMeta()
	if meta.ChartType != "bar" || !meta.HideCoalitions || meta.Timezone != "EST" {
		t.Errorf("unexpected USA meta: %+v", meta)
	}

	prepared := PrepareElection(sc.Regions, sc.Parties, ElectionConfig{
		Temperature: 0.45,
		Swing:       0.0,
		Seed:        42,
	})
	exitSeats := prepared.ExitPollSeats()
	exitWinners := prepared.ExitPollDistrictWinners()
	totals := sc.AggregateEUSeats(exitSeats)

	if totals["rep"] != 312 || totals["dem"] != 226 {
		var diffs []string
		for _, r := range sc.Regions {
			s := exitSeats[r.ID]
			diffs = append(diffs, r.ID+":"+string(rune('0'+s["dem"]/10))+string(rune('0'+s["dem"]%10))+"D/"+string(rune('0'+s["rep"]/10))+string(rune('0'+s["rep"]%10))+"R")
		}
		t.Errorf("USA 2024 baseline EVs = %v, want rep:312 dem:226 (me=%v, ne=%v)", totals, exitWinners["me"], exitWinners["ne"])
	}
	if exitWinners["me"]["ME-1"] != "dem" || exitWinners["me"]["ME-2"] != "rep" {
		t.Errorf("Maine split = %v, want ME-1:dem ME-2:rep", exitWinners["me"])
	}
	if exitWinners["ne"]["NE-1"] != "rep" || exitWinners["ne"]["NE-2"] != "dem" || exitWinners["ne"]["NE-3"] != "rep" {
		t.Errorf("Nebraska split = %v, want NE-1:rep NE-2:dem NE-3:rep", exitWinners["ne"])
	}
}
