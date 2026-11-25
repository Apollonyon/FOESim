package sim

import (
	"maps"
	"math"
	"slices"
)

// AllocationMethod identifies the seat-allocation rule used by a region.
type AllocationMethod string

const (
	MethodSainteLague AllocationMethod = "sainte_lague"
	MethodDHondt      AllocationMethod = "dhondt"
	MethodHare        AllocationMethod = "hare"
	MethodFPTP        AllocationMethod = "fptp"
	MethodSTV         AllocationMethod = "stv"
)

type SeatAllocator interface {
	Allocate(votes map[string]int, r Region, parties []Party) map[string]int
}
type hareAllocator struct{}

func (hareAllocator) Allocate(votes map[string]int, r Region, _ []Party) map[string]int {
	return allocateHare(votes, r.Seats, r.Threshold)
}

type dHondtAllocator struct{}

func (dHondtAllocator) Allocate(votes map[string]int, r Region, _ []Party) map[string]int {
	return allocateHighestAverages(votes, r.Seats, r.Threshold, func(seats int) float64 {
		return float64(seats + 1)
	})
}

type sainteLagueAllocator struct{}

func (sainteLagueAllocator) Allocate(votes map[string]int, r Region, _ []Party) map[string]int {
	return allocateHighestAverages(votes, r.Seats, r.Threshold, func(seats int) float64 {
		return float64(2*seats + 1)
	})
}

type fptpAllocator struct{}

func (fptpAllocator) Allocate(votes map[string]int, r Region, _ []Party) map[string]int {
	return allocateFPTP(votes, r.Seats)
}

type stvAllocator struct{}

func (stvAllocator) Allocate(votes map[string]int, r Region, parties []Party) map[string]int {
	return allocateSTV(votes, r.Seats, r.EffectiveParties(parties))
}

func NewAllocator(method AllocationMethod) SeatAllocator {
	switch method {
	case MethodDHondt:
		return dHondtAllocator{}
	case MethodHare:
		return hareAllocator{}
	case MethodFPTP:
		return fptpAllocator{}
	case MethodSTV:
		return stvAllocator{}
	default:
		return sainteLagueAllocator{}
	}
}

func AllocateSeats(votes map[string]int, r Region, parties []Party) map[string]int {
	return NewAllocator(r.Method).Allocate(votes, r, parties)
}

// AllocateRegionSeats computes seat totals for r, supporting both standard single-tier
// regions (r.DistrictSeats == 0) and parallel two-tier regions (r.DistrictSeats > 0),
// where each DistrictID awards r.DistrictSeats by FPTP and any remaining seats
// (r.Seats - len(districts)*r.DistrictSeats) are awarded from regionVotes via r.Method.
// When r.DistrictSeats > 0, it also returns the winning party ID per reported DistrictID.
func AllocateRegionSeats(
	regionVotes map[string]int,
	districtVotes map[string]map[string]int,
	r Region,
	parties []Party,
) (map[string]int, map[string]string) {
	if r.DistrictSeats <= 0 {
		return AllocateSeats(regionVotes, r, parties), nil
	}
	return allocateRegionSeatsWithDistricts(regionVotes, districtVotes, r, parties, r.DistrictIDs())
}

func allocateRegionSeatsWithDistricts(
	regionVotes map[string]int,
	districtVotes map[string]map[string]int,
	r Region,
	parties []Party,
	cachedDistricts []string,
) (map[string]int, map[string]string) {
	if r.DistrictSeats <= 0 {
		return AllocateSeats(regionVotes, r, parties), nil
	}

	districts := cachedDistricts
	needsMerge := len(districts) == 0
	if !needsMerge {
		for did := range districtVotes {
			if did != "" && !slices.Contains(districts, did) {
				needsMerge = true
				break
			}
		}
	}
	if needsMerge {
		knownSet := make(map[string]struct{}, len(cachedDistricts)+len(districtVotes))
		for _, did := range cachedDistricts {
			knownSet[did] = struct{}{}
		}
		for did := range districtVotes {
			if did != "" {
				knownSet[did] = struct{}{}
			}
		}
		districts = slices.Sorted(maps.Keys(knownSet))
	}

	totalSeats := make(map[string]int)
	districtWinners := make(map[string]string, len(districts))

	atLarge := r.Seats - len(districts)*r.DistrictSeats
	if atLarge > 0 {
		atLargeReg := r
		atLargeReg.Seats = atLarge
		for pid, count := range AllocateSeats(regionVotes, atLargeReg, parties) {
			if count > 0 {
				totalSeats[pid] += count
			}
		}
	}

	for _, did := range districts {
		dv := districtVotes[did]
		if len(dv) == 0 {
			continue
		}
		for pid, count := range allocateFPTP(dv, r.DistrictSeats) {
			if count > 0 {
				totalSeats[pid] += count
				districtWinners[did] = pid
			}
		}
	}
	return totalSeats, districtWinners
}

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
	// If every party with votes fell below the threshold, fall back to all parties
	// so seats do not silently vanish.
	if len(eligible) == 0 {
		for id, v := range votes {
			if v > 0 {
				eligible[id] = v
			}
		}
		return eligible, totalVotes
	}
	return eligible, eligibleVotes
}
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

func allocateFPTP(votes map[string]int, totalSeats int) map[string]int {
	if totalSeats <= 0 || len(votes) == 0 {
		return map[string]int{}
	}
	bestID := ""
	bestVotes := 0
	for id, v := range votes {
		if v > bestVotes || (v == bestVotes && id < bestID) {
			bestVotes = v
			bestID = id
		}
	}
	if bestID == "" {
		return map[string]int{}
	}
	return map[string]int{bestID: totalSeats}
}

// SumSeats aggregates per-region seat maps into parliament-wide totals keyed by Party.ID.
func SumSeats(regionSeats map[string]map[string]int) map[string]int {
	totals := make(map[string]int)
	for _, seats := range regionSeats {
		for partyID, count := range seats {
			totals[partyID] += count
		}
	}
	return totals
}

// RollupGroupSeats aggregates national party seats into EU political group totals.
// Parties without a GroupID are assigned to NonInscritsID ("ni").
func RollupGroupSeats(seats map[string]int, parties []Party) map[string]int {
	groupOf := make(map[string]string, len(parties))
	for _, p := range parties {
		groupOf[p.ID] = p.GroupID
	}

	grouped := make(map[string]int)
	for partyID, count := range seats {
		if count <= 0 {
			continue
		}
		gid := groupOf[partyID]
		if gid == "" {
			gid = NonInscritsID
		}
		grouped[gid] += count
	}
	return grouped
}
