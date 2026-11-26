package sim

import (
	"context"
	"math"
	"math/rand/v2"
	"testing"
)

func TestUnitStudentTBoundsAndHeavyTails(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 99))
	const n = 20000
	var sum, sumSq, sum4 float64
	tailCount := 0
	for range n {
		x := unitStudentT(rng)
		if math.IsNaN(x) || math.IsInf(x, 0) {
			t.Fatalf("non-finite Student-t sample: %v", x)
		}
		if x < -4.5 || x > 4.5 {
			t.Fatalf("Student-t sample %v outside [-4.5, 4.5]", x)
		}
		sum += x
		sumSq += x * x
		sum4 += x * x * x * x
		if math.Abs(x) > 3.0 {
			tailCount++
		}
	}
	mean := sum / n
	variance := sumSq/n - mean*mean
	kurtosis := (sum4 / n) / (variance * variance)
	if math.Abs(mean) > 0.05 {
		t.Fatalf("expected mean near 0, got %v", mean)
	}
	if variance < 0.75 || variance > 1.25 {
		t.Fatalf("expected variance near 1, got %v", variance)
	}
	// Normal has kurtosis 3.0 and P(|Z|>3) ≈ 0.27% (~54 of 20k); clamped t(df=5) has heavier tails.
	if kurtosis <= 3.3 {
		t.Fatalf("expected heavy-tailed kurtosis > 3.3, got %v", kurtosis)
	}
	if tailCount < 100 {
		t.Fatalf("expected more extreme tail draws (>3 sigma) than Gaussian, got %d / %d", tailCount, n)
	}
}

func TestBlocCorrelatesRegionalOutcomes(t *testing.T) {
	parties := []Party{
		{ID: "left", Name: "Left", Position: PolicyVec{-0.6, -0.5, 0.4, -0.2, 0.5, -0.4}},
		{ID: "right", Name: "Right", Position: PolicyVec{0.6, 0.5, -0.4, 0.4, -0.4, 0.5}},
	}
	makeRegion := func(id, bloc string) Region {
		return Region{
			ID:        id,
			Name:      id,
			Bloc:      bloc,
			Seats:     10,
			Precincts: 2,
			Method:    MethodSainteLague,
			Groups: []VoterGroup{
				{ID: "g1", Name: "G1", Population: 50000, Turnout: 0.65, Ideal: PolicyVec{-0.4, -0.3, 0.2, 0, 0.3, -0.2}, Weights: PolicyVec{1, 1, 1, 1, 1, 1}},
				{ID: "g2", Name: "G2", Population: 50000, Turnout: 0.65, Ideal: PolicyVec{0.4, 0.3, -0.2, 0.2, -0.3, 0.2}, Weights: PolicyVec{1, 1, 1, 1, 1, 1}},
			},
		}
	}
	regions := []Region{
		makeRegion("a1", "rust_belt"),
		makeRegion("a2", "rust_belt"),
		makeRegion("b1", "sun_belt"),
	}

	keys := collectNationalShockKeys(regions, parties)
	blocs := collectBlocs(regions)
	if len(blocs) != 2 {
		t.Fatalf("expected 2 blocs, got %v", blocs)
	}

	const runs = 160
	a1Shares := make([]float64, runs)
	a2Shares := make([]float64, runs)
	b1Shares := make([]float64, runs)

	leftShare := func(reps []PrecinctReport) float64 {
		l, tot := 0, 0
		for _, rep := range reps {
			l += rep.Votes["left"]
			tot += rep.Votes["left"] + rep.Votes["right"]
		}
		return float64(l) / float64(max(1, tot))
	}

	for i := range runs {
		seed := int64(1000 + i*97)
		shock := computeNationalShock(seed, 0.35, keys, blocs)
		// Strip Level 1 national shock to isolate Level 1.5 bloc correlation directly.
		shock.policyShift = PolicyVec{}
		shock.partyAppeal = nil
		shock.turnoutTilt = 0
		a1Shares[i] = leftShare(simulatePrecincts(regions[0], parties, 0.45, 0.35, 0, seed+1, shock, nil))
		a2Shares[i] = leftShare(simulatePrecincts(regions[1], parties, 0.45, 0.35, 0, seed+2, shock, nil))
		b1Shares[i] = leftShare(simulatePrecincts(regions[2], parties, 0.45, 0.35, 0, seed+3, shock, nil))
	}

	pearson := func(xs, ys []float64) float64 {
		var mx, my float64
		for i := range xs {
			mx += xs[i]
			my += ys[i]
		}
		mx /= float64(len(xs))
		my /= float64(len(ys))
		var num, dx, dy float64
		for i := range xs {
			a, b := xs[i]-mx, ys[i]-my
			num += a * b
			dx += a * a
			dy += b * b
		}
		return num / math.Sqrt(dx*dy)
	}

	sameBlocCorr := pearson(a1Shares, a2Shares)
	crossBlocCorr := pearson(a1Shares, b1Shares)
	if sameBlocCorr <= crossBlocCorr+0.10 || sameBlocCorr < 0.15 {
		t.Fatalf("expected strong positive same-bloc correlation vs cross-bloc: same=%.3f cross=%.3f", sameBlocCorr, crossBlocCorr)
	}
}

func TestPerUnitTurnoutApplied(t *testing.T) {
	const packName = "test_unit_turnout_pack"
	raw := `{"r1":[
		{"id":"u_low","name":"Low Turnout","weight":0.5,"turnout":0.40},
		{"id":"u_high","name":"High Turnout","weight":0.5,"turnout":0.80}
	]}`
	RegisterGeoPackJSON(packName, []byte(raw))

	parties := []Party{
		{ID: "p1", Name: "P1", Position: PolicyVec{-0.5, 0, 0, 0, 0, 0}},
		{ID: "p2", Name: "P2", Position: PolicyVec{0.5, 0, 0, 0, 0, 0}},
	}
	reg := Region{
		ID:      "r1",
		Name:    "R1",
		Seats:   2,
		Method:  MethodSainteLague,
		GeoPack: packName,
		Groups: []VoterGroup{
			{ID: "g1", Name: "G1", Population: 100000, Turnout: 0.60, Ideal: PolicyVec{-0.4, 0, 0, 0, 0, 0}, Weights: PolicyVec{1, 1, 1, 1, 1, 1}},
			{ID: "g2", Name: "G2", Population: 100000, Turnout: 0.60, Ideal: PolicyVec{0.4, 0, 0, 0, 0, 0}, Weights: PolicyVec{1, 1, 1, 1, 1, 1}},
		},
	}

	reports := SimulatePrecincts(reg, parties, 0.45, 0, 42)
	if len(reports) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(reports))
	}
	turnouts := map[string]float64{}
	for _, rep := range reports {
		tot := rep.Votes["p1"] + rep.Votes["p2"]
		turnouts[rep.PrecinctID] = float64(tot) / float64(rep.EligibleVoters)
	}
	if math.Abs(turnouts["u_low"]-0.40) > 0.01 {
		t.Fatalf("expected u_low turnout ~0.40, got %.4f", turnouts["u_low"])
	}
	if math.Abs(turnouts["u_high"]-0.80) > 0.01 {
		t.Fatalf("expected u_high turnout ~0.80, got %.4f", turnouts["u_high"])
	}

	// Scaling g.Turnout (e.g. from Scenario Editor) must scale unit turnouts rather than cancel out.
	reg.Groups[0].Turnout = 0.72
	reg.Groups[1].Turnout = 0.72
	scaledReports := SimulatePrecincts(reg, parties, 0.45, 0, 42)
	for _, rep := range scaledReports {
		tot := rep.Votes["p1"] + rep.Votes["p2"]
		got := float64(tot) / float64(rep.EligibleVoters)
		want := 0.48
		if rep.PrecinctID == "u_high" {
			want = 0.96
		}
		if math.Abs(got-want) > 0.01 {
			t.Fatalf("scaled %s turnout = %.4f, want %.4f", rep.PrecinctID, got, want)
		}
	}
}

func TestUKConstituenciesHaveRealTurnoutRange(t *testing.T) {
	units := GeoPackUnits(GeoPackUK, "lon")
	if len(units) == 0 {
		t.Fatal("expected London units in uk_constituencies geopack")
	}
	minT, maxT := 1.0, 0.0
	for _, u := range units {
		if u.Turnout <= 0 || u.Turnout > 1 {
			t.Fatalf("unit %s has invalid turnout %v", u.ID, u.Turnout)
		}
		if u.Turnout < minT {
			minT = u.Turnout
		}
		if u.Turnout > maxT {
			maxT = u.Turnout
		}
	}
	if maxT-minT < 0.15 {
		t.Fatalf("expected realistic London constituency turnout spread (>15pt), got min=%.3f max=%.3f", minT, maxT)
	}
}

func TestApplyTacticalSqueezeFPTPAndPR(t *testing.T) {
	parties := []Party{
		{ID: "grn", Position: PolicyVec{-0.8, -0.8, 0.8, -0.7, 0.9, -0.8}},
		{ID: "lab", Position: PolicyVec{-0.4, -0.4, 0.4, 0.3, 0.5, -0.1}},
		{ID: "con", Position: PolicyVec{0.6, 0.5, -0.5, 0.6, -0.2, 0.6}},
		{ID: "ref", Position: PolicyVec{0.8, 0.7, -0.9, 0.5, -0.8, 0.9}},
	}
	raw := map[string]int{"lab": 18000, "con": 19000, "grn": 4000, "ref": 2000}

	// strength=0 is exact no-op.
	unchanged := ApplyTacticalSqueeze(raw, parties, MethodFPTP, 0, 0)
	if unchanged["grn"] != 4000 || unchanged["lab"] != 18000 {
		t.Fatalf("expected strength=0 no-op, got %v", unchanged)
	}

	// FPTP squeeze: grn -> lab (closer to lab), ref -> con (closer to con); total conserved.
	squeezed := ApplyTacticalSqueeze(raw, parties, MethodFPTP, 0, 0.6)
	sumVotes := func(m map[string]int) int {
		s := 0
		for _, v := range m {
			s += v
		}
		return s
	}
	if sumVotes(squeezed) != sumVotes(raw) {
		t.Fatalf("total votes not conserved: raw=%d squeezed=%d", sumVotes(raw), sumVotes(squeezed))
	}
	if squeezed["grn"] >= raw["grn"] || squeezed["lab"] <= raw["lab"] {
		t.Fatalf("expected grn voters to squeeze toward lab: raw=%v squeezed=%v", raw, squeezed)
	}
	if squeezed["ref"] >= raw["ref"] || squeezed["con"] <= raw["con"] {
		t.Fatalf("expected ref voters to squeeze toward con: raw=%v squeezed=%v", raw, squeezed)
	}

	// PR threshold squeeze: party well below 5% shifts toward closest viable party.
	prVotes := map[string]int{"lab": 40000, "con": 40000, "grn": 2000, "ref": 18000}
	prSqueezed := ApplyTacticalSqueeze(prVotes, parties, MethodSainteLague, 0.05, 0.7)
	if sumVotes(prSqueezed) != sumVotes(prVotes) {
		t.Fatalf("PR total votes not conserved: raw=%d squeezed=%d", sumVotes(prVotes), sumVotes(prSqueezed))
	}
	if prSqueezed["grn"] >= prVotes["grn"] || prSqueezed["lab"] <= prVotes["lab"] {
		t.Fatalf("expected sub-threshold grn (2%%) to squeeze toward lab: raw=%v squeezed=%v", prVotes, prSqueezed)
	}
	if prSqueezed["ref"] != prVotes["ref"] {
		t.Fatalf("expected viable ref (18%%) not to lose votes under PR squeeze: raw=%v squeezed=%v", prVotes, prSqueezed)
	}
}

func TestLiveAndForecastParityWithRealismUpgrades(t *testing.T) {
	sc, err := LoadScenario("../scenarios/uk2024.json")
	if err != nil {
		t.Fatalf("LoadScenario(uk2024): %v", err)
	}
	// Verify all UK regions carry a Bloc tag.
	for _, r := range sc.Regions {
		if r.Bloc == "" {
			t.Fatalf("UK region %s missing bloc tag", r.ID)
		}
	}

	// With swing=0 and tactical=0.5, PrepareElection (Live) and RunForecast (Forecast)
	// must produce identical seat totals.
	cfg := ElectionConfig{Temperature: 0.45, Swing: 0, Tactical: 0.5, Seed: 77}
	prepared := PrepareElection(sc.Regions, sc.Parties, cfg)
	liveSeats := sc.AggregateEUSeats(prepared.ExitPollSeats())

	fc, err := RunForecast(context.Background(), sc, ForecastConfig{
		Runs:        10,
		Temperature: 0.45,
		Swing:       0,
		Tactical:    0.5,
		BaseSeed:    77,
		Workers:     2,
	})
	if err != nil {
		t.Fatalf("RunForecast failed: %v", err)
	}
	for pid, ls := range liveSeats {
		if fc.Baseline[pid] != ls {
			t.Fatalf("Live vs Forecast baseline mismatch for %s: live=%d forecast=%d", pid, ls, fc.Baseline[pid])
		}
		if fc.Groups[pid].P50 != ls {
			t.Fatalf("Live vs Forecast P50 mismatch at swing=0 for %s: live=%d p50=%d", pid, ls, fc.Groups[pid].P50)
		}
	}

	// Verify exact RNG parity between Live (simulatePrecincts) and Forecast (simulateRegionForecastRun)
	// when swing > 0 AND tactical > 0 on the same seed.
	const swingSeed = int64(314)
	keys := collectNationalShockKeys(sc.Regions, sc.Parties)
	blocs := collectBlocs(sc.Regions)
	shock := computeNationalShock(swingSeed, 0.25, keys, blocs)
	preRegions := buildPrecomputedRegions(sc)
	for ri, reg := range sc.Regions {
		reps := simulatePrecincts(reg, sc.Parties, 0.45, 0.25, 0.4, swingSeed+int64(ri), shock, nil)
		liveVotes, liveDistVotes := aggregateReportVotes(reps, reg.DistrictSeats > 0)
		liveRegSeats, liveDistW := AllocateRegionSeats(liveVotes, liveDistVotes, reg, reg.EffectiveParties(sc.Parties))

		fcVotes, fcRegSeats, fcDistW := simulateRegionForecastRun(preRegions[ri], 0.45, 0.25, 0.4, swingSeed+int64(ri), shock, nil)
		for pid, v := range liveVotes {
			if fcVotes[pid] != v {
				t.Fatalf("region %s party %s vote mismatch at swing=0.25 tactical=0.4: live=%d fc=%d", reg.ID, pid, v, fcVotes[pid])
			}
		}
		for pid, s := range liveRegSeats {
			if fcRegSeats[pid] != s {
				t.Fatalf("region %s party %s seat mismatch at swing=0.25 tactical=0.4: live=%d fc=%d", reg.ID, pid, s, fcRegSeats[pid])
			}
		}
		for did, w := range liveDistW {
			if fcDistW[did] != w {
				t.Fatalf("region %s district %s winner mismatch: live=%s fc=%s", reg.ID, did, w, fcDistW[did])
			}
		}
	}
}

func TestDifferentialTurnoutShiftsPartyVoteShares(t *testing.T) {
	parties := []Party{
		{ID: "left", Name: "Left", Position: PolicyVec{-0.8, -0.8, 0.8, -0.4, 0.8, -0.8}},
		{ID: "right", Name: "Right", Position: PolicyVec{0.8, 0.8, -0.8, 0.4, -0.8, 0.8}},
	}
	reg := Region{
		ID:        "r1",
		Name:      "R1",
		Seats:     10,
		Precincts: 1,
		Method:    MethodSainteLague,
		Groups: []VoterGroup{
			{ID: "g_left", Name: "Left", Population: 100000, Turnout: 0.65, Ideal: PolicyVec{-0.8, -0.8, 0.8, -0.4, 0.8, -0.8}, Weights: PolicyVec{2, 2, 2, 2, 2, 2}},
			{ID: "g_right", Name: "Right", Population: 100000, Turnout: 0.65, Ideal: PolicyVec{0.8, 0.8, -0.8, 0.4, -0.8, 0.8}, Weights: PolicyVec{2, 2, 2, 2, 2, 2}},
		},
	}

	// Compare positive vs negative national turnoutTilt with identical local seed.
	posShock := nationalRunShock{turnoutTilt: 0.15}
	negShock := nationalRunShock{turnoutTilt: -0.15}
	posRep := simulatePrecincts(reg, parties, 0.45, 0.05, 0, 123, posShock, nil)[0]
	negRep := simulatePrecincts(reg, parties, 0.45, 0.05, 0, 123, negShock, nil)[0]

	posRightShare := float64(posRep.Votes["right"]) / float64(posRep.Votes["left"]+posRep.Votes["right"])
	negRightShare := float64(negRep.Votes["right"]) / float64(negRep.Votes["left"]+negRep.Votes["right"])
	if posRightShare <= negRightShare+0.03 {
		t.Fatalf("expected positive turnoutTilt to boost right-leaning group turnout share: pos=%.4f neg=%.4f", posRightShare, negRightShare)
	}
}
