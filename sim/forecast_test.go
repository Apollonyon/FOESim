package sim

import (
	"context"
	"math"
	"reflect"
	"testing"
)

func TestRunForecastDeterminismAndPercentiles(t *testing.T) {
	sc := Scenario{
		Name:    "Forecast Test EU",
		Parties: DefaultParties(),
		Regions: DefaultRegions(),
	}

	cfg1 := ForecastConfig{
		Runs:        25,
		Temperature: 0.45,
		Swing:       0.18,
		BaseSeed:    77,
		Workers:     1,
	}
	cfg4 := cfg1
	cfg4.Workers = 4

	res1, err := RunForecast(context.Background(), sc, cfg1)
	if err != nil {
		t.Fatalf("RunForecast(w=1) failed: %v", err)
	}
	res4, err := RunForecast(context.Background(), sc, cfg4)
	if err != nil {
		t.Fatalf("RunForecast(w=4) failed: %v", err)
	}

	res1.DurationMs, res4.DurationMs = 0, 0
	if !reflect.DeepEqual(res1, res4) {
		t.Fatalf("worker count changed deterministic forecast output:\nw=1: %+v\nw=4: %+v", res1, res4)
	}

	if res1.Runs != 25 || res1.TotalSeats <= 0 || res1.Majority != res1.TotalSeats/2+1 {
		t.Fatalf("unexpected header metadata: %+v", res1)
	}

	if res1.TieProb < 0 || res1.TieProb > 1 {
		t.Errorf("invalid TieProb = %f", res1.TieProb)
	}
	meanSum := 0.0
	largestSum := 0.0
	for gid, dist := range res1.Groups {
		if !(dist.Min <= dist.P10 && dist.P10 <= dist.P25 && dist.P25 <= dist.P50 &&
			dist.P50 <= dist.P75 && dist.P75 <= dist.P90 && dist.P90 <= dist.Max) {
			t.Errorf("percentile ordering violated for %s: %+v", gid, dist)
		}
		if dist.MajorityProb < 0 || dist.MajorityProb > 1 || dist.LargestProb < 0 || dist.LargestProb > 1 {
			t.Errorf("invalid probability for %s: %+v", gid, dist)
		}
		binCount := 0
		for bi, b := range dist.Bins {
			if b.Start > b.End || b.Count < 0 {
				t.Errorf("invalid bin %d for %s: %+v", bi, gid, b)
			}
			binCount += b.Count
		}
		if binCount != res1.Runs || dist.Bins[0].Start != dist.Min || dist.Bins[len(dist.Bins)-1].End != dist.Max {
			t.Errorf("histogram bins mismatch for %s: count=%d want %d, min=%d..max=%d bins=%+v",
				gid, binCount, res1.Runs, dist.Min, dist.Max, dist.Bins)
		}
		meanSum += dist.Mean
		largestSum += dist.LargestProb
	}
	if math.Abs(meanSum-float64(res1.TotalSeats)) > 0.2 {
		t.Errorf("mean seat sum = %.3f, want %d", meanSum, res1.TotalSeats)
	}
	if math.Abs(largestSum-1.0) > 0.01 {
		t.Errorf("largest_prob sum = %.4f, want 1.0", largestSum)
	}

	for rid, rf := range res1.Regions {
		winSum := 0.0
		for _, p := range rf.WinProb {
			winSum += p
		}
		if math.Abs(winSum-1.0) > 0.01 {
			t.Errorf("region %s win_prob sum = %.4f, want 1.0", rid, winSum)
		}
	}
}

func TestRunForecastZeroSwingCollapsesVariance(t *testing.T) {
	sc := Scenario{
		Name:    "Deterministic Baseline",
		Parties: DefaultParties(),
		Regions: DefaultRegions(),
	}

	res, err := RunForecast(context.Background(), sc, ForecastConfig{
		Runs:        12,
		Temperature: 0.45,
		Swing:       0,
		BaseSeed:    42,
		Workers:     2,
	})
	if err != nil {
		t.Fatalf("RunForecast(swing=0) failed: %v", err)
	}

	for gid, dist := range res.Groups {
		if dist.Min != dist.Max || dist.P10 != dist.P50 || dist.P50 != dist.P90 {
			t.Errorf("expected zero variance for %s when swing=0, got %+v", gid, dist)
		}
	}
	for rid, rf := range res.Regions {
		foundOne := false
		for _, p := range rf.WinProb {
			if p == 1.0 {
				foundOne = true
			} else if p != 0 {
				t.Errorf("region %s expected crisp 0/1 win_prob when swing=0, got %+v", rid, rf.WinProb)
			}
		}
		if !foundOne {
			t.Errorf("region %s missing 1.0 winner when swing=0: %+v", rid, rf.WinProb)
		}
	}
}

func TestRunForecastCancellation(t *testing.T) {
	sc := Scenario{
		Name:    "Cancel Check",
		Parties: DefaultParties(),
		Regions: DefaultRegions(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := RunForecast(ctx, sc, ForecastConfig{Runs: 20, Swing: 0.15})
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestRunForecastDistrictWinProb(t *testing.T) {
	testPack := "test_forecast_districts"
	RegisterGeoPack(testPack, map[string][]PrecinctUnit{
		"me": {
			{ID: "23001", Name: "County A", DistrictID: "ME-1", Weight: 0.55},
			{ID: "23003", Name: "County B", DistrictID: "ME-2", Weight: 0.45},
		},
	})
	t.Cleanup(func() {
		geoPacksMu.Lock()
		delete(geoPacks, testPack)
		geoPacksMu.Unlock()
	})

	sc := Scenario{
		Name: "Two-Tier Test",
		Parties: []Party{
			{ID: "dem", Name: "Dem", ShortName: "D", Color: "#2563eb", Position: PolicyVec{-0.4, -0.3}},
			{ID: "rep", Name: "Rep", ShortName: "R", Color: "#dc2626", Position: PolicyVec{0.5, 0.4}},
		},
		Regions: []Region{
			{
				ID:            "me",
				Name:          "Maine",
				Seats:         4,
				GeoPack:       testPack,
				DistrictSeats: 1,
				Method:        MethodFPTP,
				DistrictTilts: map[string]map[string]float64{
					"ME-1": {"u": 1.6},
					"ME-2": {"r": 1.8},
				},
				Groups: []VoterGroup{
					{ID: "u", Name: "South", Population: 50000, Turnout: 0.7, Ideal: PolicyVec{-0.4, -0.3}, Weights: PolicyVec{1, 1}},
					{ID: "r", Name: "North", Population: 50000, Turnout: 0.7, Ideal: PolicyVec{0.5, 0.4}, Weights: PolicyVec{1, 1}},
				},
			},
		},
	}

	res, err := RunForecast(context.Background(), sc, ForecastConfig{Runs: 10, Swing: 0.15, BaseSeed: 7})
	if err != nil {
		t.Fatalf("RunForecast failed: %v", err)
	}
	me := res.Regions["me"]
	if len(me.DistrictWinProb) != 2 || len(me.DistrictWinProb["ME-1"]) == 0 || len(me.DistrictWinProb["ME-2"]) == 0 {
		t.Fatalf("expected ME-1 and ME-2 probabilities in DistrictWinProb, got %+v", me.DistrictWinProb)
	}
}
