package sim

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"time"
)

// PrecinctReport holds a full or partial vote batch and metadata for a reporting precinct.
type PrecinctReport struct {
	RegionID       string         `json:"region_id"`
	DistrictID     string         `json:"district_id,omitempty"`
	PrecinctID     string         `json:"precinct_id"`
	PrecinctName   string         `json:"precinct_name"`
	UrbanType      int            `json:"urban_type,omitempty"`
	Precinct       int            `json:"precinct"`
	TotalPrecincts int            `json:"total_precincts"`
	ReportedPct    float64        `json:"reported_pct"` // cumulative fraction reported for this precinct in (0, 1]
	EligibleVoters int            `json:"eligible_voters"`
	Votes          map[string]int `json:"votes"`
}

// ElectionConfig controls parameters for a live election-night simulation.
type ElectionConfig struct {
	Temperature    float64
	Swing          float64
	Seed           int64
	Waves          int           // partial reporting waves per precinct (<= 1 emits one 100% report)
	BaseDelay      time.Duration // per-report delay when TargetDuration is 0
	TargetDuration time.Duration // optional total stream duration per region (scales cleanly for NUTS-3)
}

// SimulatePrecincts splits r into reporting batches with a seeded random swing.
// When r.UseNUTS3 is true and embedded Eurostat NUTS-3 units exist for r.ID,
// those units (with urban/rural demographic tilt) are simulated instead of synthetic batches.
func SimulatePrecincts(r Region, parties []Party, temperature, swing float64, seed int64) []PrecinctReport {
	parties = applyPartyBonus(r.EffectiveParties(parties), r.PartyBonus)

	units := resolveUnits(r)
	n := len(units)

	// Precompute each voter group's party shares once instead of once per precinct.
	groupShares := make([]map[string]float64, len(r.Groups))
	for gi, g := range r.Groups {
		groupShares[gi] = VoteShares(g, parties, temperature)
	}

	rng := rand.New(rand.NewPCG(uint64(seed), 0))
	reports := make([]PrecinctReport, 0, n)

	totalPop := float64(r.TotalPopulation())
	for i, u := range units {
		votes := make(map[string]int, len(parties))
		eligibleVoters := 0

		// Compute normalized demographic + swing weights so the county keeps its real electorate size.
		tiltedSum := 0.0
		tilts := make([]float64, len(r.Groups))
		for gi, g := range r.Groups {
			factor := 1.0
			if swing > 0 {
				factor = math.Max(0.1, 1.0+rng.NormFloat64()*swing)
			}
			tilts[gi] = countyTilt(g, u) * r.unitTilt(u.ID, u.DistrictID, g.ID) * factor
			tiltedSum += float64(g.Population) * tilts[gi]
		}
		norm := 1.0
		if tiltedSum > 0 && totalPop > 0 {
			norm = totalPop / tiltedSum
		}
		// Keep overall county turnout noise gentle (~3-4% stddev) even when party swings are larger.
		turnoutNoise := 1.0
		if swing > 0 {
			turnoutNoise = math.Max(0.75, math.Min(1.25, 1.0+rng.NormFloat64()*math.Min(0.04, swing*0.25)))
		}

		for gi, g := range r.Groups {
			pop := float64(g.Population) * tilts[gi] * norm * u.Weight
			eligibleVoters += int(math.Round(pop))

			voters := pop * math.Min(1.0, g.Turnout*turnoutNoise)
			for partyID, share := range groupShares[gi] {
				votes[partyID] += int(math.Round(voters * share))
			}
		}

		reports = append(reports, PrecinctReport{
			RegionID:       r.ID,
			DistrictID:     u.DistrictID,
			PrecinctID:     u.ID,
			PrecinctName:   u.Name,
			UrbanType:      u.UrbanType,
			Precinct:       i + 1,
			TotalPrecincts: n,
			ReportedPct:    1.0,
			EligibleVoters: eligibleVoters,
			Votes:          votes,
		})
	}

	return reports
}

func resolveUnits(r Region) []PrecinctUnit {
	if r.UseNUTS3 {
		if units := NUTS3Units(r.ID); len(units) > 0 {
			return units
		}
	}
	n := r.Precincts
	if n <= 0 {
		n = 1
	}
	w := 1.0 / float64(n)
	units := make([]PrecinctUnit, n)
	for i := range n {
		units[i] = PrecinctUnit{
			ID:     fmt.Sprintf("%s-%d", r.ID, i+1),
			Name:   fmt.Sprintf("Precinct %d", i+1),
			Weight: w,
		}
	}
	return units
}

type scheduledReport struct {
	at     float64 // normalized timeline position in [0, 1]
	report PrecinctReport
}

// expandIntoWaves splits each precinct's full report into w ordered partial batches
// whose votes sum exactly to the original precinct totals, with realistic wave-to-wave skew.
func expandIntoWaves(full []PrecinctReport, w int, rng *rand.Rand) []scheduledReport {
	out := make([]scheduledReport, 0, len(full)*w)
	for _, rep := range full {
		// Randomize wave split fractions that sum to 1.0.
		fracs := make([]float64, w)
		sumF := 0.0
		for i := range w {
			fracs[i] = 0.6 + rng.Float64()*0.8
			sumF += fracs[i]
		}
		for i := range w {
			fracs[i] /= sumF
		}

		// Per-party early-vs-late wave bias in sorted key order so RNG draws are 100% deterministic.
		partySkew := make(map[string]float64, len(rep.Votes))
		for _, pid := range slices.Sorted(maps.Keys(rep.Votes)) {
			partySkew[pid] = (rng.Float64() - 0.5) * 0.35 // +/- 17.5% early/late shift
		}

		emitted := make(map[string]int, len(rep.Votes))
		cumPct := 0.0
		// Spread waves across the night while strictly preserving Wave 1 < Wave 2 < ... < Wave W.
		t := rng.Float64() * (0.55 / float64(w))
		stepWindow := (1.0 - t) / float64(w)

		for wi := range w {
			isLast := wi == w-1
			cumPct += fracs[wi]
			if isLast {
				cumPct = 1.0
			}

			waveVotes := make(map[string]int, len(rep.Votes))
			// Early waves skew one way (-1), late waves the opposite (+1), canceling out by final wave.
			waveDir := -1.0 + 2.0*(float64(wi)/float64(max(1, w-1)))
			for pid, totalV := range rep.Votes {
				if isLast {
					waveVotes[pid] = totalV - emitted[pid]
				} else {
					mult := math.Max(0.3, 1.0+partySkew[pid]*waveDir)
					v := int(math.Round(float64(totalV) * fracs[wi] * mult))
					if rem := totalV - emitted[pid]; v > rem {
						v = rem
					}
					if v < 0 {
						v = 0
					}
					waveVotes[pid] = v
					emitted[pid] += v
				}
			}

			if wi > 0 {
				t = math.Min(1.0, t+stepWindow*(0.45+rng.Float64()*0.75))
			}

			waveRep := rep
			waveRep.ReportedPct = math.Round(cumPct*1000) / 1000
			waveRep.Votes = waveVotes
			out = append(out, scheduledReport{at: t, report: waveRep})
		}
	}

	slices.SortStableFunc(out, func(a, b scheduledReport) int {
		if c := cmp.Compare(a.at, b.at); c != 0 {
			return c
		}
		return cmp.Compare(a.report.ReportedPct, b.report.ReportedPct)
	})
	return out
}

// StreamElection simulates all regions concurrently and streams precinct reports
// on the returned channel, closing it when all regions finish or ctx is canceled.
func StreamElection(ctx context.Context, regions []Region, parties []Party, cfg ElectionConfig) <-chan PrecinctReport {
	out := make(chan PrecinctReport, max(1, len(regions)*4))
	var wg sync.WaitGroup

	// Compute poll-closing time window across regions for realistic staggered European waves.
	minClose, maxClose := math.Inf(1), math.Inf(-1)
	for _, r := range regions {
		if r.PollCloseHour > 0 {
			if r.PollCloseHour < minClose {
				minClose = r.PollCloseHour
			}
			if r.PollCloseHour > maxClose {
				maxClose = r.PollCloseHour
			}
		}
	}
	closeSpan := 0.0
	if !math.IsInf(minClose, 1) && maxClose > minClose {
		closeSpan = maxClose - minClose
	}

	for ri, r := range regions {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Wait for this Member State's polls to close (staggered across the first 30% of TargetDuration).
			startOffset := time.Duration(0)
			if cfg.TargetDuration > 0 && closeSpan > 0 && r.PollCloseHour > minClose {
				frac := (r.PollCloseHour - minClose) / closeSpan
				startOffset = time.Duration(frac * 0.30 * float64(cfg.TargetDuration))
			}
			if startOffset > 0 {
				timer := time.NewTimer(startOffset)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}

			regionDuration := cfg.TargetDuration - startOffset
			if regionDuration < 0 {
				regionDuration = 0
			}

			reports := SimulatePrecincts(r, parties, cfg.Temperature, cfg.Swing, cfg.Seed+int64(ri))
			rng := rand.New(rand.NewPCG(uint64(cfg.Seed), uint64(ri+1)))

			// Multi-wave partial county reporting mode.
			if cfg.Waves > 1 {
				schedule := expandIntoWaves(reports, cfg.Waves, rng)
				prevAt := 0.0
				for _, item := range schedule {
					var delay time.Duration
					if regionDuration > 0 {
						delta := math.Max(0, item.at-prevAt)
						delay = time.Duration(delta * float64(regionDuration))
						prevAt = item.at
					} else if cfg.BaseDelay > 0 {
						delay = time.Duration(float64(cfg.BaseDelay) * (0.5 + rng.Float64()))
					}
					if delay > 0 {
						timer := time.NewTimer(delay)
						select {
						case <-ctx.Done():
							timer.Stop()
							return
						case <-timer.C:
						}
					}
					select {
					case <-ctx.Done():
						return
					case out <- item.report:
					}
				}
				return
			}

			// Single-wave mode: randomize reporting order across counties.
			if r.UseNUTS3 && len(reports) > 1 {
				rng.Shuffle(len(reports), func(i, j int) {
					reports[i], reports[j] = reports[j], reports[i]
				})
				for idx := range reports {
					reports[idx].Precinct = idx + 1
				}
			}

			stepDelay := cfg.BaseDelay
			if regionDuration > 0 && len(reports) > 0 {
				// Cap per-unit delay for tiny 1-2 county countries (e.g. LU, CY, MT)
				// so jitter never stretches the whole broadcast past TargetDuration.
				stepDelay = regionDuration / time.Duration(max(3, len(reports)))
			}

			for _, report := range reports {
				if stepDelay > 0 {
					// Randomize reporting speed between 0.5x and 1.5x stepDelay.
					delay := time.Duration(float64(stepDelay) * (0.5 + rng.Float64()))
					timer := time.NewTimer(delay)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
				}

				select {
				case <-ctx.Done():
					return
				case out <- report:
				}
			}
		}()
	}

	// Close out once every region goroutine has finished.
	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}
