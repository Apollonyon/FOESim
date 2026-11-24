package sim

import (
	"maps"
	"slices"
)

type ElectionState struct {
	scenario       Scenario
	regionByID     map[string]Region
	euGroups       []EUGroup
	tallies        map[string]map[string]int
	regionSeats    map[string]map[string]int
	euSeats        map[string]int
	coalitions     []Coalition
	precinctPct    map[string]float64
	sumPct         float64
	completed      int
	totalPrecincts int
}

func NewElectionState(s Scenario) *ElectionState {
	e := &ElectionState{
		scenario:    s,
		euGroups:    s.EffectiveGroups(),
		regionByID:  make(map[string]Region, len(s.Regions)),
		tallies:     make(map[string]map[string]int, len(s.Regions)),
		regionSeats: make(map[string]map[string]int, len(s.Regions)),
		euSeats:     make(map[string]int),
	}

	for _, r := range s.Regions {
		e.regionByID[r.ID] = r
		e.tallies[r.ID] = make(map[string]int)
		e.totalPrecincts += r.EffectivePrecinctCount()
	}

	e.precinctPct = make(map[string]float64, e.totalPrecincts)

	return e
}

func (e *ElectionState) Apply(rep PrecinctReport) {
	reg, ok := e.regionByID[rep.RegionID]
	if !ok {
		return
	}
	for partyID, v := range rep.Votes {
		e.tallies[rep.RegionID][partyID] += v
	}

	prev := e.precinctPct[rep.PrecinctID]
	cur := rep.ReportedPct
	if cur <= 0 {
		cur = 1.0
	}
	if cur > prev {
		e.sumPct += cur - prev
		e.precinctPct[rep.PrecinctID] = cur
		if prev < 1.0 && cur >= 1.0 {
			e.completed++
		}
	}

	e.regionSeats[rep.RegionID] = AllocateSeats(e.tallies[rep.RegionID], reg.Seats, reg.Method, reg.Threshold)

	curEU := e.scenario.AggregateEUSeats(e.regionSeats)
	if !maps.Equal(curEU, e.euSeats) {
		e.euSeats = curEU
		e.coalitions = FindCoalitions(curEU, e.euGroups, 5)
	}

}

func (e *ElectionState) SeedSeats(regionSeats map[string]map[string]int) {
	for rid, seats := range regionSeats {
		e.regionSeats[rid] = maps.Clone(seats)
	}
	e.euSeats = e.scenario.AggregateEUSeats(e.regionSeats)
	e.coalitions = FindCoalitions(e.euSeats, e.euGroups, 5)
}

func (e *ElectionState) RegionVotes(regionID string) map[string]int {
	return maps.Clone(e.tallies[regionID])
}

func (e *ElectionState) RegionSeats(regionID string) map[string]int {
	return maps.Clone(e.regionSeats[regionID])
}

func (e *ElectionState) EUSeats() map[string]int {
	return maps.Clone(e.euSeats)
}

func (e *ElectionState) Coalitions() []Coalition {
	return slices.Clone(e.coalitions)
}

func (e *ElectionState) Progress() (completed, total int, countedPct float64) {
	if e.totalPrecincts > 0 {
		countedPct = (e.sumPct / float64(e.totalPrecincts)) * 100
	}
	return e.completed, e.totalPrecincts, countedPct
}
