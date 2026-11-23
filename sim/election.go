package sim

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"time"
)

// PrecinctReport holds the vote tally and metadata for a single reporting precinct batch.
type PrecinctReport struct {
	RegionID       string         `json:"region_id"`
	DistrictID     string         `json:"district_id,omitempty"`
	PrecinctID     string         `json:"precinct_id"`
	PrecinctName   string         `json:"precinct_name"`
	Precinct       int            `json:"precinct"`
	TotalPrecincts int            `json:"total_precincts"`
	EligibleVoters int            `json:"eligible_voters"`
	Votes          map[string]int `json:"votes"`
}

// ElectionConfig controls parameters for a live election-night simulation.
type ElectionConfig struct {
	Temperature float64
	Swing       float64
	Seed        int64
	BaseDelay   time.Duration
}

// SimulatePrecincts splits r into reporting batches with a seeded random swing.
func SimulatePrecincts(r Region, parties []Party, temperature, swing float64, seed int64) []PrecinctReport {
	parties = applyPartyBonus(parties, r.PartyBonus)
	n := r.Precincts
	if n <= 0 {
		n = 1
	}

	// Precompute each voter group's party shares once instead of once per precinct.
	groupShares := make([]map[string]float64, len(r.Groups))
	for gi, g := range r.Groups {
		groupShares[gi] = VoteShares(g, parties, temperature)
	}

	rng := rand.New(rand.NewPCG(uint64(seed), 0))
	reports := make([]PrecinctReport, 0, n)

	for i := range n {
		votes := make(map[string]int, len(parties))
		eligibleVoters := 0

		for gi, g := range r.Groups {
			factor := math.Max(0, 1.0+rng.NormFloat64()*swing)
			pop := float64(g.Population) / float64(n) * factor
			eligibleVoters += int(math.Round(pop))

			voters := pop * g.Turnout
			for partyID, share := range groupShares[gi] {
				votes[partyID] += int(math.Round(voters * share))
			}
		}

		reports = append(reports, PrecinctReport{
			RegionID:       r.ID,
			PrecinctID:     fmt.Sprintf("%s-%d", r.ID, i+1),
			PrecinctName:   fmt.Sprintf("Precinct %d", i+1),
			Precinct:       i + 1,
			TotalPrecincts: n,
			EligibleVoters: eligibleVoters,
			Votes:          votes,
		})
	}

	return reports
}

// StreamElection simulates all regions concurrently and streams precinct reports
// on the returned channel, closing it when all regions finish or ctx is canceled.
func StreamElection(ctx context.Context, regions []Region, parties []Party, cfg ElectionConfig) <-chan PrecinctReport {
	out := make(chan PrecinctReport)
	var wg sync.WaitGroup

	for ri, r := range regions {
		wg.Add(1)
		go func() {
			defer wg.Done()

			reports := SimulatePrecincts(r, parties, cfg.Temperature, cfg.Swing, cfg.Seed+int64(ri))
			rng := rand.New(rand.NewPCG(uint64(cfg.Seed), uint64(ri+1)))

			for _, report := range reports {
				if cfg.BaseDelay > 0 {
					// Randomize reporting speed between 0.5x and 1.5x BaseDelay.
					delay := time.Duration(float64(cfg.BaseDelay) * (0.5 + rng.Float64()))
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
