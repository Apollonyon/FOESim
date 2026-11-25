package sim

import (
	"context"
	"maps"
	"math"
	"math/rand/v2"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultForecastRuns = 200
	forecastSeedStride  = 10007
)

// ForecastConfig controls a bounded-worker Monte Carlo election forecast.
type ForecastConfig struct {
	Runs          int
	Temperature   float64
	Swing         float64
	NationalShift map[string]float64
	BaseSeed      int64
	Workers       int
}

// SeatBin counts how many simulation runs landed inside [Start, End] seats.
type SeatBin struct {
	Start int `json:"start"`
	End   int `json:"end"`
	Count int `json:"count"`
}

// SeatDistribution summarizes simulated seat outcomes and threshold odds for a group or party.
type SeatDistribution struct {
	Mean         float64   `json:"mean"`
	Min          int       `json:"min"`
	P10          int       `json:"p10"`
	P25          int       `json:"p25"`
	P50          int       `json:"p50"`
	P75          int       `json:"p75"`
	P90          int       `json:"p90"`
	Max          int       `json:"max"`
	MajorityProb float64   `json:"majority_prob"`
	LargestProb  float64   `json:"largest_prob"`
	Bins         []SeatBin `json:"bins,omitempty"`
}

// RegionForecast summarizes win probabilities and mean seats for one region.
type RegionForecast struct {
	WinProb         map[string]float64            `json:"win_prob"`
	MeanSeats       map[string]float64            `json:"mean_seats"`
	DistrictWinProb map[string]map[string]float64 `json:"district_win_prob,omitempty"`
}

// ForecastResult holds aggregated Monte Carlo statistics across all simulated elections.
type ForecastResult struct {
	Runs       int                         `json:"runs"`
	TotalSeats int                         `json:"total_seats"`
	Majority   int                         `json:"majority"`
	TieProb    float64                     `json:"tie_prob,omitempty"`
	Baseline   map[string]int              `json:"baseline,omitempty"`
	Groups     map[string]SeatDistribution `json:"groups"`
	Regions    map[string]RegionForecast   `json:"regions"`
	DurationMs int64                       `json:"duration_ms"`
}

type precomputedRegion struct {
	region            Region
	units             []PrecinctUnit
	partiesWithBonus  []Party
	effectiveParties  []Party
	totalPop          float64
	sortedDistrictIDs []string
	unitGroupTilts    [][]float64
}

type singleRunOutcome struct {
	euSeats         map[string]int
	regionSeats     []map[string]int
	regionWinners   [][]string
	districtWinners []map[string]string
}

func roundFloat4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func percentileSorted(sorted []int, q float64) int {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Round(q * float64(len(sorted)-1)))
	idx = max(0, min(idx, len(sorted)-1))
	return sorted[idx]
}

func buildHistogramBins(sorted []int, maxBins int) []SeatBin {
	if len(sorted) == 0 {
		return nil
	}
	minV, maxV := sorted[0], sorted[len(sorted)-1]
	if minV == maxV || maxBins <= 1 {
		return []SeatBin{{Start: minV, End: maxV, Count: len(sorted)}}
	}
	span := maxV - minV + 1
	step := max(1, (span+maxBins-1)/maxBins)
	numBins := (span + step - 1) / step
	bins := make([]SeatBin, numBins)
	for i := range numBins {
		bins[i].Start = minV + i*step
		bins[i].End = min(maxV, minV+(i+1)*step-1)
	}
	for _, v := range sorted {
		bi := min(numBins-1, (v-minV)/step)
		bins[bi].Count++
	}
	return bins
}

func buildPrecomputedRegions(sc Scenario) []precomputedRegion {
	out := make([]precomputedRegion, len(sc.Regions))
	for ri, r := range sc.Regions {
		units := resolveUnits(r)
		effParties := r.EffectiveParties(sc.Parties)
		withBonus := applyPartyBonus(effParties, r.PartyBonus)

		distSet := make(map[string]struct{})
		for _, did := range r.DistrictIDs() {
			distSet[did] = struct{}{}
		}
		for _, u := range units {
			if u.DistrictID != "" {
				distSet[u.DistrictID] = struct{}{}
			}
		}
		sortedDists := slices.Sorted(maps.Keys(distSet))

		unitTilts := make([][]float64, len(units))
		for ui, u := range units {
			row := make([]float64, len(r.Groups))
			for gi, g := range r.Groups {
				row[gi] = countyTilt(g, u) * r.unitTilt(u.ID, u.DistrictID, g.ID)
			}
			unitTilts[ui] = row
		}

		out[ri] = precomputedRegion{
			region:            r,
			units:             units,
			partiesWithBonus:  withBonus,
			effectiveParties:  effParties,
			totalPop:          float64(r.TotalPopulation()),
			sortedDistrictIDs: sortedDists,
			unitGroupTilts:    unitTilts,
		}
	}
	return out
}

func simulateRegionForecastRun(
	pre precomputedRegion,
	temperature float64,
	swing float64,
	seed int64,
	shock nationalRunShock,
	nationalShift map[string]float64,
) (votes map[string]int, seats map[string]int, districtWinners map[string]string) {
	r := pre.region
	parties := applyNationalAppealShifts(pre.partiesWithBonus, shock, nationalShift, swing)
	units := pre.units
	rng := rand.New(rand.NewPCG(uint64(seed), 0))

	var countryShift PolicyVec
	countryTurnoutNoise := 0.0
	if swing > 0 {
		policySigma := swing * 0.50
		for ax := range countryShift {
			countryShift[ax] = rng.NormFloat64() * policySigma
		}
		appealSigma := swing * 0.45
		for i := range parties {
			parties[i].Appeal += rng.NormFloat64() * appealSigma
		}
		countryTurnoutNoise = rng.NormFloat64() * math.Min(0.03, swing*0.18)
	}

	groupShares := make([]map[string]float64, len(r.Groups))
	for gi, g := range r.Groups {
		if swing > 0 {
			for ax := range g.Ideal {
				g.Ideal[ax] = math.Max(-1.0, math.Min(1.0, g.Ideal[ax]+shock.policyShift[ax]+countryShift[ax]))
			}
		}
		groupShares[gi] = VoteShares(g, parties, temperature)
	}

	var districtShocks map[string][]float64
	if swing > 0 && len(pre.sortedDistrictIDs) > 0 {
		districtShocks = make(map[string][]float64, len(pre.sortedDistrictIDs))
		districtSigma := swing * 0.35
		for _, did := range pre.sortedDistrictIDs {
			shocks := make([]float64, len(r.Groups))
			for gi := range shocks {
				shocks[gi] = rng.NormFloat64() * districtSigma
			}
			districtShocks[did] = shocks
		}
	}

	votes = make(map[string]int, len(parties))
	var districtVotes map[string]map[string]int
	if r.DistrictSeats > 0 {
		districtVotes = make(map[string]map[string]int, len(pre.sortedDistrictIDs))
	}

	tilts := make([]float64, len(r.Groups))
	for ui, u := range units {
		tiltedSum := 0.0
		var dShock []float64
		if districtShocks != nil {
			dShock = districtShocks[u.DistrictID]
		}
		baseRow := pre.unitGroupTilts[ui]
		for gi, g := range r.Groups {
			factor := 1.0
			if swing > 0 {
				local := rng.NormFloat64() * (swing * 0.65)
				regional := 0.0
				if gi < len(dShock) {
					regional = dShock[gi]
				}
				factor = math.Max(0.1, 1.0+regional+local)
			}
			tilts[gi] = baseRow[gi] * factor
			tiltedSum += float64(g.Population) * tilts[gi]
		}

		norm := 1.0
		if tiltedSum > 0 && pre.totalPop > 0 {
			norm = pre.totalPop / tiltedSum
		}
		turnoutNoise := 1.0
		if swing > 0 {
			localTurnout := rng.NormFloat64() * math.Min(0.03, swing*0.18)
			turnoutNoise = math.Max(0.75, math.Min(1.25, 1.0+countryTurnoutNoise+localTurnout))
		}

		var dVotes map[string]int
		if districtVotes != nil && u.DistrictID != "" {
			dVotes = districtVotes[u.DistrictID]
			if dVotes == nil {
				dVotes = make(map[string]int, len(parties))
				districtVotes[u.DistrictID] = dVotes
			}
		}

		for gi, g := range r.Groups {
			pop := float64(g.Population) * tilts[gi] * norm * u.Weight
			voters := pop * math.Min(1.0, g.Turnout*turnoutNoise)
			for partyID, share := range groupShares[gi] {
				v := int(math.Round(voters * share))
				votes[partyID] += v
				if dVotes != nil {
					dVotes[partyID] += v
				}
			}
		}
	}

	seats, districtWinners = allocateRegionSeatsWithDistricts(votes, districtVotes, r, pre.effectiveParties, pre.sortedDistrictIDs)
	return votes, seats, districtWinners
}

func winningParties(votes, seats map[string]int, candidateParties []Party) []string {
	bestSeats := -1
	bestVotes := -1
	var winners []string
	seen := make(map[string]bool, len(candidateParties))

	consider := func(pid string) {
		if seen[pid] {
			return
		}
		seen[pid] = true
		s := seats[pid]
		v := votes[pid]
		if s > bestSeats || (s == bestSeats && v > bestVotes) {
			bestSeats = s
			bestVotes = v
			winners = append(winners[:0], pid)
		} else if s == bestSeats && v == bestVotes {
			winners = append(winners, pid)
		}
	}

	for _, p := range candidateParties {
		consider(p.ID)
	}
	for _, pid := range slices.Sorted(maps.Keys(seats)) {
		consider(pid)
	}
	for _, pid := range slices.Sorted(maps.Keys(votes)) {
		consider(pid)
	}
	return winners
}

// RunForecast simulates cfg.Runs correlated elections concurrently across a
// bounded worker pool and returns parliament seat percentiles and regional win odds.
func RunForecast(ctx context.Context, sc Scenario, cfg ForecastConfig) (ForecastResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg.Runs <= 0 {
		cfg.Runs = defaultForecastRuns
	}
	if cfg.Temperature <= 0 {
		cfg.Temperature = 0.45
	}
	if cfg.Swing < 0 {
		cfg.Swing = 0
	}
	if cfg.Workers <= 0 {
		cfg.Workers = min(max(1, runtime.GOMAXPROCS(0)), 8)
	}
	if cfg.Workers > cfg.Runs {
		cfg.Workers = cfg.Runs
	}

	start := time.Now()
	preRegions := buildPrecomputedRegions(sc)
	shockKeys := collectNationalShockKeys(sc.Regions, sc.Parties)

	baseRegSeats := make(map[string]map[string]int, len(preRegions))
	for ri, pre := range preRegions {
		_, seats, _ := simulateRegionForecastRun(pre, cfg.Temperature, 0, cfg.BaseSeed+int64(ri), nationalRunShock{}, cfg.NationalShift)
		baseRegSeats[pre.region.ID] = seats
	}
	baseline := sc.AggregateEUSeats(baseRegSeats)

	outcomes := make([]singleRunOutcome, cfg.Runs)

	var nextRun atomic.Int64
	var wg sync.WaitGroup
	for range cfg.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				idx := int(nextRun.Add(1) - 1)
				if idx >= cfg.Runs {
					return
				}
				runSeed := cfg.BaseSeed + int64(idx)*forecastSeedStride
				shock := computeNationalShock(runSeed, cfg.Swing, shockKeys)

				regSeatsMap := make(map[string]map[string]int, len(preRegions))
				regSeatsSlice := make([]map[string]int, len(preRegions))
				regWinnersSlice := make([][]string, len(preRegions))
				distWinnersSlice := make([]map[string]string, len(preRegions))

				for ri, pre := range preRegions {
					if ctx.Err() != nil {
						return
					}
					votes, seats, distW := simulateRegionForecastRun(pre, cfg.Temperature, cfg.Swing, runSeed+int64(ri), shock, cfg.NationalShift)
					regSeatsMap[pre.region.ID] = seats
					regSeatsSlice[ri] = seats
					regWinnersSlice[ri] = winningParties(votes, seats, pre.effectiveParties)
					distWinnersSlice[ri] = distW
				}

				outcomes[idx] = singleRunOutcome{
					euSeats:         sc.AggregateEUSeats(regSeatsMap),
					regionSeats:     regSeatsSlice,
					regionWinners:   regWinnersSlice,
					districtWinners: distWinnersSlice,
				}
			}
		}()
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return ForecastResult{}, err
	}

	totalSeats := 0
	for _, r := range sc.Regions {
		totalSeats += r.Seats
	}
	majority := totalSeats/2 + 1

	// Collect stable list of all parliament-level groups or parties.
	groupSet := make(map[string]bool)
	var allGroupIDs []string
	addGroupID := func(id string) {
		if id == "" || groupSet[id] {
			return
		}
		groupSet[id] = true
		allGroupIDs = append(allGroupIDs, id)
	}
	if sc.UsesNationalParties() {
		for _, g := range sc.Groups {
			addGroupID(g.ID)
		}
	} else {
		for _, p := range sc.Parties {
			addGroupID(p.ID)
		}
		for _, r := range sc.Regions {
			for _, p := range r.Parties {
				addGroupID(p.ID)
			}
		}
	}
	for _, out := range outcomes {
		for gid := range out.euSeats {
			addGroupID(gid)
		}
	}

	perGroupRuns := make(map[string][]int, len(allGroupIDs))
	for _, gid := range allGroupIDs {
		perGroupRuns[gid] = make([]int, cfg.Runs)
	}
	majCount := make(map[string]int, len(allGroupIDs))
	largestSum := make(map[string]float64, len(allGroupIDs))
	tieRuns := 0

	for runIdx, out := range outcomes {
		maxSeats := 0
		for _, gid := range allGroupIDs {
			s := out.euSeats[gid]
			perGroupRuns[gid][runIdx] = s
			if s >= majority {
				majCount[gid]++
			}
			if s > maxSeats {
				maxSeats = s
			}
		}
		if maxSeats > 0 {
			var topIDs []string
			for _, gid := range allGroupIDs {
				if out.euSeats[gid] == maxSeats {
					topIDs = append(topIDs, gid)
				}
			}
			if len(topIDs) > 1 {
				tieRuns++
			}
			share := 1.0 / float64(len(topIDs))
			for _, gid := range topIDs {
				largestSum[gid] += share
			}
		}
	}

	groupsOut := make(map[string]SeatDistribution, len(allGroupIDs))
	runsF := float64(cfg.Runs)
	for _, gid := range allGroupIDs {
		vals := perGroupRuns[gid]
		sum := 0
		for _, v := range vals {
			sum += v
		}
		slices.Sort(vals)
		groupsOut[gid] = SeatDistribution{
			Mean:         roundFloat4(float64(sum) / runsF),
			Min:          vals[0],
			P10:          percentileSorted(vals, 0.10),
			P25:          percentileSorted(vals, 0.25),
			P50:          percentileSorted(vals, 0.50),
			P75:          percentileSorted(vals, 0.75),
			P90:          percentileSorted(vals, 0.90),
			Max:          vals[len(vals)-1],
			MajorityProb: roundFloat4(float64(majCount[gid]) / runsF),
			LargestProb:  roundFloat4(largestSum[gid] / runsF),
			Bins:         buildHistogramBins(vals, 16),
		}
	}

	regionsOut := make(map[string]RegionForecast, len(preRegions))
	for ri, pre := range preRegions {
		winSum := make(map[string]float64, len(pre.effectiveParties))
		seatSum := make(map[string]int, len(pre.effectiveParties))
		var distWinSum map[string]map[string]float64

		for _, out := range outcomes {
			winners := out.regionWinners[ri]
			if len(winners) > 0 {
				w := 1.0 / float64(len(winners))
				for _, pid := range winners {
					winSum[pid] += w
				}
			}
			for pid, s := range out.regionSeats[ri] {
				seatSum[pid] += s
			}
			if dw := out.districtWinners[ri]; len(dw) > 0 {
				if distWinSum == nil {
					distWinSum = make(map[string]map[string]float64, len(dw))
				}
				for did, pid := range dw {
					if pid == "" {
						continue
					}
					if distWinSum[did] == nil {
						distWinSum[did] = make(map[string]float64)
					}
					distWinSum[did][pid] += 1.0
				}
			}
		}

		winProb := make(map[string]float64, len(winSum))
		for pid, w := range winSum {
			winProb[pid] = roundFloat4(w / runsF)
		}
		meanSeats := make(map[string]float64, len(seatSum))
		for pid, s := range seatSum {
			meanSeats[pid] = roundFloat4(float64(s) / runsF)
		}
		var distWinProb map[string]map[string]float64
		if distWinSum != nil {
			distWinProb = make(map[string]map[string]float64, len(distWinSum))
			for did, counts := range distWinSum {
				probs := make(map[string]float64, len(counts))
				for pid, c := range counts {
					probs[pid] = roundFloat4(c / runsF)
				}
				distWinProb[did] = probs
			}
		}

		regionsOut[pre.region.ID] = RegionForecast{
			WinProb:         winProb,
			MeanSeats:       meanSeats,
			DistrictWinProb: distWinProb,
		}
	}

	return ForecastResult{
		Runs:       cfg.Runs,
		TotalSeats: totalSeats,
		Majority:   majority,
		TieProb:    roundFloat4(float64(tieRuns) / runsF),
		Baseline:   baseline,
		Groups:     groupsOut,
		Regions:    regionsOut,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}
