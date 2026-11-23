package sim

import "math"

// VoterGroup is a demographic slice of a region's electorate with shared
// policy preferences, issue weights, and turnout rate.
type VoterGroup struct {
	Name       string    `json:"name"`
	Population int       `json:"population"`
	Turnout    float64   `json:"turnout"`
	Ideal      PolicyVec `json:"ideal"`
	Weights    PolicyVec `json:"weights"`
}

// Region is an electoral district with a fixed seat quota and reporting precincts.
type Region struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Seats      int                `json:"seats"`
	Precincts  int                `json:"precincts"`
	Groups     []VoterGroup       `json:"groups"`
	Method     AllocationMethod   `json:"method"`
	Threshold  float64            `json:"threshold"`
	PartyBonus map[string]float64 `json:"party_bonus,omitempty"`
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
