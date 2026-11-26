package sim

import (
	"cmp"
	"maps"
	"math"
	"slices"
)

type tacticalTransfer struct {
	from string
	to   string
	frac float64
}

// ApplyTacticalSqueeze models strategic ("don't waste your vote") defections while
// preserving total votes cast. strength is clamped to [0, 1]; 0 returns votes unchanged.
//   - Under FPTP (or single-tier district contests): supporters of parties outside the top 2
//     that trail 2nd place significantly shift partially toward the ideologically closer
//     of the top 2 contenders.
//   - Under proportional rules with threshold > 0: supporters of parties well below the
//     electoral threshold shift partially toward the ideologically closest viable party
//     above the threshold.
func ApplyTacticalSqueeze(
	votes map[string]int,
	parties []Party,
	method AllocationMethod,
	threshold float64,
	strength float64,
) map[string]int {
	transfers := computeTacticalTransfers(votes, parties, method, threshold, strength)
	return applyTacticalTransfers(votes, transfers)
}

func computeTacticalTransfers(
	votes map[string]int,
	parties []Party,
	method AllocationMethod,
	threshold float64,
	strength float64,
) []tacticalTransfer {
	if strength <= 0 || isNonFinite(strength) || len(votes) < 3 {
		return nil
	}
	if strength > 1 {
		strength = 1
	}

	total := 0
	for _, v := range votes {
		if v > 0 {
			total += v
		}
	}
	if total == 0 {
		return nil
	}

	partyByID := make(map[string]Party, len(parties))
	for _, p := range parties {
		partyByID[p.ID] = p
	}
	unitW := PolicyVec{1, 1, 1, 1, 1, 1}

	closestTarget := func(srcID string, targets []string) string {
		src, hasSrc := partyByID[srcID]
		bestID := targets[0]
		bestDist := math.Inf(1)
		for _, tid := range targets {
			if tid == srcID {
				continue
			}
			dst, hasDst := partyByID[tid]
			d := 1.0
			if hasSrc && hasDst {
				d = src.Position.WeightedDistance(dst.Position, unitW)
			}
			if d < bestDist || (d == bestDist && tid < bestID) {
				bestDist = d
				bestID = tid
			}
		}
		return bestID
	}

	if method == MethodFPTP {
		ranked := slices.SortedFunc(maps.Keys(votes), func(a, b string) int {
			if c := cmp.Compare(votes[b], votes[a]); c != 0 {
				return c
			}
			return cmp.Compare(a, b)
		})
		if len(ranked) < 3 || votes[ranked[1]] <= 0 {
			return nil
		}
		topTwo := ranked[:2]
		runnerUp := float64(votes[ranked[1]])
		var transfers []tacticalTransfer
		for _, pid := range ranked[2:] {
			v := votes[pid]
			if v <= 0 {
				continue
			}
			ratio := float64(v) / runnerUp
			if ratio >= 0.70 {
				continue // viable 3rd contender: little tactical defection
			}
			defectFrac := strength * (1.0 - ratio/0.70) * 0.65
			if defectFrac <= 0 {
				continue
			}
			transfers = append(transfers, tacticalTransfer{
				from: pid,
				to:   closestTarget(pid, topTwo),
				frac: defectFrac,
			})
		}
		return transfers
	}

	if threshold > 0 {
		var viable []string
		for _, pid := range slices.Sorted(maps.Keys(votes)) {
			if float64(votes[pid])/float64(total) >= threshold {
				viable = append(viable, pid)
			}
		}
		if len(viable) == 0 {
			return nil
		}
		cutoff := threshold * 0.85
		var transfers []tacticalTransfer
		for _, pid := range slices.Sorted(maps.Keys(votes)) {
			v := votes[pid]
			if v <= 0 {
				continue
			}
			share := float64(v) / float64(total)
			if share >= cutoff {
				continue
			}
			defectFrac := strength * (1.0 - share/cutoff) * 0.60
			if defectFrac <= 0 {
				continue
			}
			transfers = append(transfers, tacticalTransfer{
				from: pid,
				to:   closestTarget(pid, viable),
				frac: defectFrac,
			})
		}
		return transfers
	}

	return nil
}

func applyTacticalTransfers(votes map[string]int, transfers []tacticalTransfer) map[string]int {
	if len(transfers) == 0 {
		return votes
	}
	out := maps.Clone(votes)
	for _, tr := range transfers {
		v := votes[tr.from]
		if v <= 0 {
			continue
		}
		moved := int(math.Round(float64(v) * tr.frac))
		if moved <= 0 {
			continue
		}
		if moved > out[tr.from] {
			moved = out[tr.from]
		}
		out[tr.from] -= moved
		out[tr.to] += moved
	}
	return out
}

func applyTacticalToReports(reports []PrecinctReport, r Region, parties []Party, strength float64) {
	if strength <= 0 || isNonFinite(strength) || len(reports) == 0 {
		return
	}
	if r.DistrictSeats > 0 {
		regVotes, distVotes := aggregateReportVotes(reports, true)
		distTransfers := make(map[string][]tacticalTransfer, len(distVotes))
		for did, dv := range distVotes {
			if tr := computeTacticalTransfers(dv, parties, MethodFPTP, 0, strength); len(tr) > 0 {
				distTransfers[did] = tr
			}
		}
		var fallback []tacticalTransfer
		if r.Method == MethodFPTP || r.Threshold > 0 {
			fallback = computeTacticalTransfers(regVotes, parties, r.Method, r.Threshold, strength)
		}
		for i := range reports {
			if did := reports[i].DistrictID; did != "" {
				if tr := distTransfers[did]; len(tr) > 0 {
					reports[i].Votes = applyTacticalTransfers(reports[i].Votes, tr)
				}
			} else if len(fallback) > 0 {
				reports[i].Votes = applyTacticalTransfers(reports[i].Votes, fallback)
			}
		}
		return
	}
	if r.Method == MethodFPTP || r.Threshold > 0 {
		regVotes, _ := aggregateReportVotes(reports, false)
		tr := computeTacticalTransfers(regVotes, parties, r.Method, r.Threshold, strength)
		if len(tr) == 0 {
			return
		}
		for i := range reports {
			reports[i].Votes = applyTacticalTransfers(reports[i].Votes, tr)
		}
	}
}
