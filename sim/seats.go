package sim

import "math"

// AllocationMethod identifies the seat-allocation rule used by a region.
type AllocationMethod string

const (
	MethodSainteLague AllocationMethod = "sainte_lague"
	MethodDHondt      AllocationMethod = "dhondt"
	MethodHare        AllocationMethod = "hare"
)

// AllocateSeats distributes totalSeats among parties according to method,
// excluding parties whose vote share is below threshold (in [0, 1]).
func AllocateSeats(votes map[string]int, totalSeats int, method AllocationMethod, threshold float64) map[string]int {
	switch method {
	case MethodDHondt:
		return allocateHighestAverages(votes, totalSeats, threshold, func(seats int) float64 {
			return float64(seats + 1)
		})
	case MethodHare:
		return allocateHare(votes, totalSeats, threshold)
	default:
		return allocateHighestAverages(votes, totalSeats, threshold, func(seats int) float64 {
			return float64(2*seats + 1)
		})
	}
}

// filterEligible filters out parties that don't meet the threshold
func filterEligible(votes map[string]int, threshold float64) (map[string]int, int) {
	totalVotes := 0
	for _, v := range votes {
		if v > 0 {
			totalVotes += v
		}
	}
	if totalVotes == 0 {
		return map[string]int{}, 0
	}

	eligible := make(map[string]int)
	eligibleVotes := 0
	minVotes := threshold * float64(totalVotes)
	for id, v := range votes {
		if v > 0 && float64(v) >= minVotes {
			eligible[id] = v
			eligibleVotes += v
		}
	}
	return eligible, eligibleVotes
}

// allocateHighestAverages allocates seats using the highest averages method
func allocateHighestAverages(votes map[string]int, totalSeats int, threshold float64, divisor func(seats int) float64) map[string]int {
	if totalSeats <= 0 || len(votes) == 0 {
		return map[string]int{}
	}
	eligible, _ := filterEligible(votes, threshold)
	if len(eligible) == 0 {
		return map[string]int{}
	}

	seats := make(map[string]int)
	for range totalSeats {
		bestID := ""
		bestQ := -1.0
		for id, v := range eligible {
			q := float64(v) / divisor(seats[id])
			if q > bestQ || (q == bestQ && id < bestID) {
				bestQ = q
				bestID = id
			}
		}
		seats[bestID]++
	}
	return seats
}

// allocateHare allocates seats using the Hare quota method
func allocateHare(votes map[string]int, totalSeats int, threshold float64) map[string]int {
	if totalSeats <= 0 || len(votes) == 0 {
		return map[string]int{}
	}
	eligible, eligibleVotes := filterEligible(votes, threshold)
	if len(eligible) == 0 {
		return map[string]int{}
	}

	quota := float64(eligibleVotes) / float64(totalSeats)
	seats := make(map[string]int)
	remainders := make(map[string]float64, len(eligible))
	awarded := 0

	for id, v := range eligible {
		exact := float64(v) / quota
		whole := int(math.Floor(exact))
		if whole > 0 {
			seats[id] = whole
		}
		awarded += whole
		remainders[id] = exact - float64(whole)
	}

	for awarded < totalSeats && len(remainders) > 0 {
		bestID := ""
		bestRem := -1.0
		for id, rem := range remainders {
			if rem > bestRem || (rem == bestRem && id < bestID) {
				bestRem = rem
				bestID = id
			}
		}
		seats[bestID]++
		awarded++
		delete(remainders, bestID)
	}
	return seats
}
