package sim

import (
	"maps"
	"slices"
)

type ElectionState struct {
	scenario         Scenario
	usesNational     bool
	regionByID       map[string]Region
	partyGroup       map[string]map[string]string
	euGroups         []EUGroup
	tallies          map[string]map[string]int
	districtTallies  map[string]map[string]map[string]int
	regionDistricts  map[string][]string
	districtWinners  map[string]map[string]string
	regionSeats      map[string]map[string]int
	regionGroupSeats map[string]map[string]int
	euSeats          map[string]int
	coalitions       []Coalition
	precinctPct      map[string]float64
	sumPct           float64
	completed        int
	totalPrecincts   int
}

func NewElectionState(s Scenario) *ElectionState {
	usesNational := s.UsesNationalParties()
	e := &ElectionState{
		scenario:         s,
		usesNational:     usesNational,
		euGroups:         s.EffectiveGroups(),
		regionByID:       make(map[string]Region, len(s.Regions)),
		tallies:          make(map[string]map[string]int, len(s.Regions)),
		districtTallies:  make(map[string]map[string]map[string]int),
		regionDistricts:  make(map[string][]string),
		districtWinners:  make(map[string]map[string]string),
		regionSeats:      make(map[string]map[string]int, len(s.Regions)),
		regionGroupSeats: make(map[string]map[string]int, len(s.Regions)),
		euSeats:          make(map[string]int),
	}
	if usesNational {
		e.partyGroup = make(map[string]map[string]string, len(s.Regions))
	}

	for _, r := range s.Regions {
		e.regionByID[r.ID] = r
		e.tallies[r.ID] = make(map[string]int)
		e.totalPrecincts += r.EffectivePrecinctCount()
		if r.DistrictSeats > 0 {
			e.districtTallies[r.ID] = make(map[string]map[string]int)
			e.regionDistricts[r.ID] = r.DistrictIDs()
			e.districtWinners[r.ID] = make(map[string]string)
		}
		if usesNational {
			parties := r.EffectiveParties(s.Parties)
			pg := make(map[string]string, len(parties))
			for _, p := range parties {
				gid := p.GroupID
				if gid == "" {
					gid = NonInscritsID
				}
				pg[p.ID] = gid
			}
			e.partyGroup[r.ID] = pg
		}
	}

	e.precinctPct = make(map[string]float64, e.totalPrecincts)

	return e
}

func (e *ElectionState) Apply(rep PrecinctReport) bool {
	reg, ok := e.regionByID[rep.RegionID]
	if !ok {
		return false
	}
	for partyID, v := range rep.Votes {
		e.tallies[rep.RegionID][partyID] += v
	}
	if reg.DistrictSeats > 0 && rep.DistrictID != "" {
		dt := e.districtTallies[rep.RegionID]
		if dt[rep.DistrictID] == nil {
			dt[rep.DistrictID] = make(map[string]int, len(rep.Votes))
		}
		for partyID, v := range rep.Votes {
			dt[rep.DistrictID][partyID] += v
		}
	}

	prev := e.precinctPct[rep.PrecinctID]
	cur := rep.ReportedPct
	if cur <= 0 || cur > 1.0 {
		cur = 1.0
	}
	if cur > prev {
		e.sumPct += cur - prev
		e.precinctPct[rep.PrecinctID] = cur
		if prev < 1.0 && cur >= 1.0 {
			e.completed++
		}
	}

	var newSeats map[string]int
	if reg.DistrictSeats > 0 {
		var liveWinners map[string]string
		newSeats, liveWinners = allocateRegionSeatsWithDistricts(
			e.tallies[rep.RegionID],
			e.districtTallies[rep.RegionID],
			reg,
			reg.EffectiveParties(e.scenario.Parties),
			e.regionDistricts[rep.RegionID],
		)
		// Preserve exit-poll winners for districts with 0 votes reported so far so
		// total seat counts stay constant while partial counts stream in.
		mergedWinners := e.districtWinners[rep.RegionID]
		if mergedWinners == nil {
			mergedWinners = make(map[string]string, len(liveWinners))
			e.districtWinners[rep.RegionID] = mergedWinners
		}
		for did, winner := range liveWinners {
			mergedWinners[did] = winner
		}
		for _, did := range e.regionDistricts[rep.RegionID] {
			if _, reported := liveWinners[did]; reported {
				continue
			}
			if prevWinner := mergedWinners[did]; prevWinner != "" {
				newSeats[prevWinner] += reg.DistrictSeats
			}
		}
	} else {
		newSeats = AllocateSeats(e.tallies[rep.RegionID], reg, reg.EffectiveParties(e.scenario.Parties))
	}

	if e.updateRegionSeats(rep.RegionID, newSeats) {
		if !e.scenario.Meta.HideCoalitions {
			e.coalitions = FindCoalitions(e.euSeats, e.euGroups, 5)
		}
		return true
	}
	return false
}

func (e *ElectionState) SeedSeats(regionSeats map[string]map[string]int) {
	for rid, seats := range regionSeats {
		if _, ok := e.regionByID[rid]; !ok {
			continue
		}
		e.updateRegionSeats(rid, maps.Clone(seats))
	}
	if !e.scenario.Meta.HideCoalitions {
		e.coalitions = FindCoalitions(e.euSeats, e.euGroups, 5)
	}
}

// SeedDistrictWinners seeds initial per-district winners (e.g. from exit polls)
// for two-tier regions before all districts have reported votes.
func (e *ElectionState) SeedDistrictWinners(winners map[string]map[string]string) {
	for rid, dw := range winners {
		if _, ok := e.regionByID[rid]; !ok {
			continue
		}
		e.districtWinners[rid] = maps.Clone(dw)
	}
}

// DistrictWinners returns a defensive copy of the current winning party ID per DistrictID
// for regionID, or nil if regionID does not use district-tier seats.
func (e *ElectionState) DistrictWinners(regionID string) map[string]string {
	if len(e.districtWinners[regionID]) == 0 {
		return nil
	}
	return maps.Clone(e.districtWinners[regionID])
}

func (e *ElectionState) rollupRegion(regionID string, seats map[string]int) map[string]int {
	grouped := make(map[string]int, len(seats))
	pg := e.partyGroup[regionID]
	for pid, count := range seats {
		if count <= 0 {
			continue
		}
		if !e.usesNational {
			grouped[pid] += count
			continue
		}
		gid := pg[pid]
		if gid == "" {
			gid = NonInscritsID
		}
		grouped[gid] += count
	}
	return grouped
}

func (e *ElectionState) updateRegionSeats(regionID string, newSeats map[string]int) bool {
	if maps.Equal(e.regionSeats[regionID], newSeats) {
		return false
	}
	e.regionSeats[regionID] = newSeats

	oldGrp := e.regionGroupSeats[regionID]
	newGrp := e.rollupRegion(regionID, newSeats)
	if maps.Equal(oldGrp, newGrp) {
		return false
	}

	for gid, c := range oldGrp {
		if c == 0 {
			continue
		}
		e.euSeats[gid] -= c
		if e.euSeats[gid] <= 0 {
			delete(e.euSeats, gid)
		}
	}
	for gid, c := range newGrp {
		if c > 0 {
			e.euSeats[gid] += c
		}
	}
	e.regionGroupSeats[regionID] = newGrp
	return true
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
	if len(e.coalitions) == 0 {
		return nil
	}
	out := make([]Coalition, len(e.coalitions))
	for i, c := range e.coalitions {
		c.GroupIDs = slices.Clone(c.GroupIDs)
		c.Names = slices.Clone(c.Names)
		out[i] = c
	}
	return out
}

func (e *ElectionState) Progress() (completed, total int, countedPct float64) {
	if e.totalPrecincts > 0 {
		countedPct = (e.sumPct / float64(e.totalPrecincts)) * 100
	}
	return e.completed, e.totalPrecincts, countedPct
}
