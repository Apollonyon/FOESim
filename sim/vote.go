package sim

import (
	"math"
	"slices"
)

// VoteShares returns each party's vote probability for group,
// using softmax over (-WeightedDistance + Appeal) / temperature.
// Non-positive temperatures default to 1.0.
func VoteShares(group VoterGroup, parties []Party, temperature float64) map[string]float64 {
	if len(parties) == 0 {
		return map[string]float64{}
	}
	if temperature <= 0 || math.IsNaN(temperature) || math.IsInf(temperature, 0) {
		temperature = 1.0 // Preventing Division by Zero or neg./non-finite values
	}
	scores := make([]float64, len(parties))
	maxScore := math.Inf(-1)

	for i, p := range parties {
		dist := group.Ideal.WeightedDistance(p.Position, group.Weights)
		score := (-dist + p.Appeal) / temperature
		scores[i] = score
		if score > maxScore {
			maxScore = score
		}
	}
	var total float64
	for i, s := range scores {
		expVal := math.Exp(s - maxScore)
		total += expVal
		scores[i] = expVal
	}
	shares := make(map[string]float64, len(parties))
	for i, p := range parties {
		shares[p.ID] = scores[i] / total
	}
	return shares
}

// SimulateRegion computes deterministic vote totals and seat allocations for r.
func SimulateRegion(r Region, parties []Party, temperature float64) (map[string]int, map[string]int) {
	parties = applyPartyBonus(r.EffectiveParties(parties), r.PartyBonus)
	votes := make(map[string]int)
	for _, g := range r.Groups {
		voters := g.ExpectedVoters()
		shares := VoteShares(g, parties, temperature)
		for partyID, share := range shares {
			votes[partyID] += int(math.Round(float64(voters) * share))
		}
	}

	seats := AllocateSeats(votes, r, parties)
	return votes, seats
}

func applyPartyBonus(parties []Party, bonus map[string]float64) []Party {
	if len(bonus) == 0 {
		return parties
	}
	adjusted := slices.Clone(parties)
	for i, p := range adjusted {
		adjusted[i].Appeal += bonus[p.ID]
	}
	return adjusted
}
