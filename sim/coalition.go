package sim

import (
	"cmp"
	"math"
	"slices"
)

// Coalition represents a candidate minimal-winning governing majority of EU political groups.
type Coalition struct {
	GroupIDs []string `json:"group_ids"`
	Names    []string `json:"names"`
	Seats    int      `json:"seats"`
	Majority int      `json:"majority"`
	Spread   float64  `json:"spread"`   // lower = more ideologically cohesive
	Cohesion int      `json:"cohesion"` // 0..100 human-readable cohesion score
}

// FindCoalitions enumerates minimal-winning majority coalitions among EU political groups
// (excluding NonInscritsID), ranked by ideological compatibility using PolicyVec spread.
func FindCoalitions(euSeats map[string]int, groups []EUGroup, maxResults int) []Coalition {
	totalSeats := 0
	for _, s := range euSeats {
		if s > 0 {
			totalSeats += s
		}
	}
	if totalSeats == 0 {
		return nil
	}
	majority := totalSeats/2 + 1

	eligible := make([]EUGroup, 0, len(groups))
	for _, g := range groups {
		if g.ID == NonInscritsID {
			continue
		}
		if euSeats[g.ID] > 0 {
			eligible = append(eligible, g)
		}
	}
	n := len(eligible)
	if n == 0 {
		return nil
	}
	if n > 16 {
		slices.SortFunc(eligible, func(a, b EUGroup) int {
			if c := cmp.Compare(euSeats[b.ID], euSeats[a.ID]); c != 0 {
				return c
			}
			return cmp.Compare(a.ID, b.ID)
		})
		eligible = eligible[:16]
		n = 16
	}

	unitWeights := PolicyVec{}
	for i := range unitWeights {
		unitWeights[i] = 1.0
	}

	var results []Coalition
	limit := 1 << n
	for mask := 1; mask < limit; mask++ {
		seats := 0
		minMemberSeats := math.MaxInt
		for i := range n {
			if mask&(1<<i) != 0 {
				s := euSeats[eligible[i].ID]
				seats += s
				if s < minMemberSeats {
					minMemberSeats = s
				}
			}
		}
		if seats < majority {
			continue
		}
		// Keep only minimal-winning coalitions (dropping any member loses the majority).
		if seats-minMemberSeats >= majority {
			continue
		}

		var centroid PolicyVec
		var members []EUGroup
		for i := range n {
			if mask&(1<<i) != 0 {
				g := eligible[i]
				members = append(members, g)
				w := float64(euSeats[g.ID]) / float64(seats)
				for ax := range centroid {
					centroid[ax] += g.Position[ax] * w
				}
			}
		}

		spread := 0.0
		for _, g := range members {
			w := float64(euSeats[g.ID]) / float64(seats)
			spread += w * g.Position.WeightedDistance(centroid, unitWeights)
		}
		if len(members) > 2 {
			spread += 0.04 * float64(len(members)-2)
		}

		slices.SortFunc(members, func(a, b EUGroup) int {
			if c := cmp.Compare(euSeats[b.ID], euSeats[a.ID]); c != 0 {
				return c
			}
			return cmp.Compare(a.ID, b.ID)
		})

		ids := make([]string, len(members))
		names := make([]string, len(members))
		for i, g := range members {
			ids[i] = g.ID
			names[i] = g.ShortName
		}

		cohesion := int(math.Round(100 * (1.0 - spread/1.75)))
		if cohesion < 0 {
			cohesion = 0
		} else if cohesion > 100 {
			cohesion = 100
		}

		results = append(results, Coalition{
			GroupIDs: ids,
			Names:    names,
			Seats:    seats,
			Majority: majority,
			Spread:   math.Round(spread*1000) / 1000,
			Cohesion: cohesion,
		})
	}

	slices.SortFunc(results, func(a, b Coalition) int {
		if c := cmp.Compare(a.Spread, b.Spread); c != 0 {
			return c
		}
		if c := cmp.Compare(len(a.GroupIDs), len(b.GroupIDs)); c != 0 {
			return c
		}
		if c := cmp.Compare(b.Seats, a.Seats); c != 0 {
			return c
		}
		return slices.Compare(a.GroupIDs, b.GroupIDs)
	})

	if maxResults > 0 && len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}
