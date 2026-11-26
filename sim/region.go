package sim

import (
	"maps"
	"math"
	"slices"
)

// VoterGroup is a demographic slice of a region's electorate with shared
// policy preferences, issue weights, and turnout rate.
type VoterGroup struct {
	ID         string    `json:"id,omitempty"`
	Name       string    `json:"name"`
	Population int       `json:"population"`
	Turnout    float64   `json:"turnout"`
	Ideal      PolicyVec `json:"ideal"`
	Weights    PolicyVec `json:"weights"`
}

// Region is an electoral district with a fixed seat quota and reporting precincts.
type Region struct {
	ID            string                        `json:"id"`
	Name          string                        `json:"name"`
	Bloc          string                        `json:"bloc,omitempty"` // optional macro-region tag for correlated regional shocks
	Seats         int                           `json:"seats"`
	Precincts     int                           `json:"precincts"`
	Groups        []VoterGroup                  `json:"groups"`
	Method        AllocationMethod              `json:"method"`
	Threshold     float64                       `json:"threshold"`
	PartyBonus    map[string]float64            `json:"party_bonus,omitempty"`
	DistrictTilts map[string]map[string]float64 `json:"district_tilts,omitempty"`
	Parties       []Party                       `json:"parties,omitempty"`
	UseNUTS3      bool                          `json:"use_nuts3,omitempty"`
	GeoPack       string                        `json:"geo_pack,omitempty"`
	DistrictSeats int                           `json:"district_seats,omitempty"`  // seats awarded per DistrictID (parallel two-tier when > 0)
	PollCloseHour float64                       `json:"poll_close_hour,omitempty"` // closing hour in scenario clock (e.g. 18.0 to 25.0)
}

// ExpectedVoters returns the rounded number of voters expected to turn out.
func (g VoterGroup) ExpectedVoters() int {
	return int(math.Round(float64(g.Population) * g.Turnout))
}

// TotalPopulation returns the combined eligible population across all groups in r.
func (r Region) TotalPopulation() int {
	sum := 0
	for _, g := range r.Groups {
		sum += g.Population
	}
	return sum
}

// EffectiveParties returns r's national parties if defined, otherwise fallback.
func (r Region) EffectiveParties(fallback []Party) []Party {
	if len(r.Parties) > 0 {
		return r.Parties
	}
	return fallback
}

// EffectiveGeoPack returns the GeoPack name for r.
// Explicit r.GeoPack takes precedence; legacy r.UseNUTS3 maps to GeoPackEU.
func (r Region) EffectiveGeoPack() string {
	if r.GeoPack != "" {
		return r.GeoPack
	}
	if r.UseNUTS3 {
		return GeoPackEU
	}
	return ""
}

// DistrictIDs returns the sorted unique non-empty DistrictIDs belonging to r's
// GeoPack units, or nil if r does not use a GeoPack or has no district IDs.
func (r Region) DistrictIDs() []string {
	pack := r.EffectiveGeoPack()
	if pack == "" {
		return nil
	}
	units := GeoPackUnits(pack, r.ID)
	if len(units) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	for _, u := range units {
		if u.DistrictID != "" {
			seen[u.DistrictID] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(seen))
}

// EffectivePrecinctCount returns the number of reporting units simulated for r.
func (r Region) EffectivePrecinctCount() int {
	if pack := r.EffectiveGeoPack(); pack != "" {
		if units := GeoPackUnits(pack, r.ID); len(units) > 0 {
			return len(units)
		}
	}
	if r.Precincts > 0 {
		return r.Precincts
	}
	return 1
}

// unitTilt returns the most specific regional demographic multiplier for groupID,
// checking NUTS-3 (precinctID), NUTS-2 (4-char prefix), and NUTS-1 (districtID) in order.
func (r Region) unitTilt(precinctID, districtID, groupID string) float64 {
	if groupID == "" || len(r.DistrictTilts) == 0 {
		return 1.0
	}
	keys := [3]string{precinctID, "", districtID}
	if len(precinctID) >= 4 {
		keys[1] = precinctID[:4]
	}
	for _, k := range keys {
		if k == "" {
			continue
		}
		if mult, ok := r.DistrictTilts[k][groupID]; ok && mult > 0 {
			return mult
		}
	}
	return 1.0
}
