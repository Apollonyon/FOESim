package sim

import "math"

// allocateSTV implements a party-level Droop-quota STV approximation operating
// on party vote totals (without individual ranked ballots). Elected parties
// subtract one quota and keep their surplus; when no party reaches quota, the
// lowest-vote remaining party is eliminated and 100% of its votes transfer to
// the nearest remaining party in policy space (no ballot exhaustion).
func allocateSTV(votes map[string]int, totalSeats int, parties []Party) map[string]int {
	if totalSeats <= 0 || len(votes) == 0 {
		return map[string]int{}
	}

	remaining := make(map[string]float64, len(votes))
	totalVotes := 0
	for id, v := range votes {
		if v > 0 {
			remaining[id] = float64(v)
			totalVotes += v
		}
	}
	if totalVotes == 0 {
		return map[string]int{}
	}
	if totalVotes < totalSeats {
		return allocateHare(votes, totalSeats, 0)
	}

	posByID := make(map[string]PolicyVec, len(parties))
	for _, p := range parties {
		posByID[p.ID] = p.Position
	}

	quota := math.Floor(float64(totalVotes)/float64(totalSeats+1)) + 1
	seats := make(map[string]int)
	awarded := 0

	for awarded < totalSeats && len(remaining) > 0 {
		bestID, worstID := "", ""
		bestVotes, worstVotes := -1.0, math.Inf(1)

		for id, v := range remaining {
			if bestID == "" || v > bestVotes || (v == bestVotes && id < bestID) {
				bestVotes = v
				bestID = id
			}
			if worstID == "" || v < worstVotes || (v == worstVotes && id > worstID) {
				worstVotes = v
				worstID = id
			}
		}

		if remaining[bestID] >= quota {
			seats[bestID]++
			awarded++
			remaining[bestID] -= quota
		} else if len(remaining) == 1 {
			seats[bestID] += totalSeats - awarded
			break
		} else {
			transfer := remaining[worstID]
			delete(remaining, worstID)
			target := nearestParty(worstID, remaining, posByID)
			remaining[target] += transfer
		}
	}

	return seats
}

func nearestParty(fromID string, remaining map[string]float64, posByID map[string]PolicyVec) string {
	fromPos, hasFrom := posByID[fromID]
	bestID := ""
	bestDist := math.Inf(1)
	unitWeights := PolicyVec{1, 1, 1, 1, 1, 1}
	for id := range remaining {
		dist := math.Inf(1)
		if toPos, ok := posByID[id]; hasFrom && ok {
			dist = fromPos.WeightedDistance(toPos, unitWeights)
		}
		if bestID == "" || dist < bestDist || (dist == bestDist && id < bestID) {
			bestDist = dist
			bestID = id
		}
	}
	return bestID
}
