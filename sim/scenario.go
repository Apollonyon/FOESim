package sim

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
)

// Scenario bundles the EU groups, parties, and regions for an election simulation.
type Scenario struct {
	Name    string    `json:"name"`
	Groups  []EUGroup `json:"groups,omitempty"`
	Parties []Party   `json:"parties,omitempty"`
	Regions []Region  `json:"regions"`
}

// LoadScenario reads and validates a JSON election scenario from path.
func LoadScenario(path string) (Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, err
	}

	var s Scenario
	if err = json.Unmarshal(data, &s); err != nil {
		return Scenario{}, err
	}
	if err = s.validate(); err != nil {
		return Scenario{}, err
	}
	return s, nil
}

// UsesNationalParties reports whether the scenario defines EU political groups
// that national parties affiliate into.
func (s Scenario) UsesNationalParties() bool {
	return len(s.Groups) > 0
}

// EffectiveGroups returns the scenario's EU groups, or synthesizes groups from
// pan-European Parties when no separate Groups are defined.
func (s Scenario) EffectiveGroups() []EUGroup {
	if len(s.Groups) > 0 {
		return s.Groups
	}
	groups := make([]EUGroup, len(s.Parties))
	for i, p := range s.Parties {
		groups[i] = EUGroup{
			ID:        p.ID,
			Name:      p.Name,
			ShortName: p.ShortName,
			Color:     p.Color,
			Position:  p.Position,
		}
	}
	return groups
}

// AggregateEUSeats sums per-region seat maps into parliament-wide totals.
// When the scenario defines EU political groups, national party seats are rolled up
// into their EU group IDs (defaulting unaffiliated parties to NonInscritsID).
func (s Scenario) AggregateEUSeats(regionSeats map[string]map[string]int) map[string]int {
	if !s.UsesNationalParties() {
		return SumSeats(regionSeats)
	}
	regionByID := make(map[string]Region, len(s.Regions))
	for _, r := range s.Regions {
		regionByID[r.ID] = r
	}
	groupedByRegion := make(map[string]map[string]int, len(regionSeats))
	for rid, seats := range regionSeats {
		reg := regionByID[rid]
		groupedByRegion[rid] = RollupGroupSeats(seats, reg.EffectiveParties(s.Parties))
	}
	return SumSeats(groupedByRegion)
}

func (s Scenario) validate() error {
	if len(s.Regions) == 0 {
		return fmt.Errorf("scenario %q has no regions", s.Name)
	}

	knownGroups := make(map[string]bool, len(s.Groups))
	for _, g := range s.Groups {
		if g.ID == "" {
			return fmt.Errorf("scenario %q has an EU group with empty id", s.Name)
		}
		if knownGroups[g.ID] {
			return fmt.Errorf("scenario %q has duplicate EU group id %q", s.Name, g.ID)
		}
		knownGroups[g.ID] = true
	}

	seenRegions := make(map[string]bool, len(s.Regions))
	for _, r := range s.Regions {
		if r.ID == "" {
			return fmt.Errorf("scenario %q has a region with empty id", s.Name)
		}
		if seenRegions[r.ID] {
			return fmt.Errorf("scenario %q has duplicate region id %q", s.Name, r.ID)
		}
		seenRegions[r.ID] = true
		if r.Seats <= 0 {
			return fmt.Errorf("region %q in scenario %q has no seats", r.ID, s.Name)
		}
		if len(r.Groups) == 0 {
			return fmt.Errorf("region %q in scenario %q has no voter groups", r.ID, s.Name)
		}
		if r.Threshold < 0 || r.Threshold > 1 || math.IsNaN(r.Threshold) {
			return fmt.Errorf("region %q in scenario %q has invalid threshold %g", r.ID, s.Name, r.Threshold)
		}
		parties := r.EffectiveParties(s.Parties)
		if len(parties) == 0 {
			return fmt.Errorf("region %q in scenario %q has no parties", r.ID, s.Name)
		}
		seenParties := make(map[string]bool, len(parties))
		for _, p := range parties {
			if p.ID == "" {
				return fmt.Errorf("region %q has a party with empty id", r.ID)
			}
			if seenParties[p.ID] {
				return fmt.Errorf("region %q has duplicate party id %q", r.ID, p.ID)
			}
			seenParties[p.ID] = true
			if s.UsesNationalParties() && p.GroupID != "" && !knownGroups[p.GroupID] {
				return fmt.Errorf("party %q in region %q references unknown EU group %q", p.ID, r.ID, p.GroupID)
			}
		}

		for _, vg := range r.Groups {
			if vg.Population < 0 {
				return fmt.Errorf("region %q has a voter group %q with negative population", r.ID, vg.ID)
			}
			if vg.Turnout < 0 || vg.Turnout > 1 || math.IsNaN(vg.Turnout) {
				return fmt.Errorf("region %q has a voter group %q with invalid turnout %g", r.ID, vg.ID, vg.Turnout)
			}
			for ax, w := range vg.Weights {
				if w < 0 || math.IsNaN(w) || math.IsInf(w, 0) {
					return fmt.Errorf("region %q has a voter group %q with invalid weight %g for axis %d", r.ID, vg.ID, w, ax)
				}
			}
		}
		if len(r.DistrictTilts) > 0 {
			knownVoterGroups := make(map[string]bool, len(r.Groups))
			for _, vg := range r.Groups {
				if vg.ID != "" {
					knownVoterGroups[vg.ID] = true
				}
			}
			for districtID, tilts := range r.DistrictTilts {
				for vgID := range tilts {
					if !knownVoterGroups[vgID] {
						return fmt.Errorf("region %q district_tilts[%q] references unknown voter group %q", r.ID, districtID, vgID)
					}
				}
			}
		}

		switch r.Method {
		case "", MethodSainteLague, MethodDHondt, MethodHare:
		default:
			return fmt.Errorf("region %q has unknown allocation method %q", r.ID, r.Method)
		}
	}
	return nil
}
