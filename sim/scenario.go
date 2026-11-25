package sim

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path"
	"strings"
)

// ScenarioMeta describes presentation, clock, and map metadata for a scenario.
// Empty fields fall back to EU-27 defaults via Scenario.EffectiveMeta.
type ScenarioMeta struct {
	Title          string    `json:"title,omitempty"`
	Description    string    `json:"description,omitempty"`
	SeatLabel      string    `json:"seat_label,omitempty"`
	RegionLabel    string    `json:"region_label,omitempty"`
	UnitLabel      string    `json:"unit_label,omitempty"`
	ChartType      string    `json:"chart_type,omitempty"` // "hemicycle" or "bar"
	HideCoalitions bool      `json:"hide_coalitions,omitempty"`
	Timezone       string    `json:"timezone,omitempty"`
	ClockStartHour float64   `json:"clock_start_hour,omitempty"`
	ClockEndHour   float64   `json:"clock_end_hour,omitempty"`
	RegionsURL     string    `json:"regions_url,omitempty"`
	UnitsURL       string    `json:"units_url,omitempty"`
	ViewBox        string    `json:"view_box,omitempty"`
	OverviewLabel  string    `json:"overview_label,omitempty"`
	Attribution    string    `json:"attribution,omitempty"`
	WealthLabel    string    `json:"wealth_label,omitempty"`
	AgeLabel       string    `json:"age_label,omitempty"`
	Axis2Label     string    `json:"axis2_label,omitempty"`
	Axis2Poles     [3]string `json:"axis2_poles,omitempty"`
}

// Scenario bundles the political groups, parties, regions, and UI metadata for an election simulation.
type Scenario struct {
	Name      string         `json:"name"`
	Meta      ScenarioMeta   `json:"meta,omitzero"`
	Groups    []EUGroup      `json:"groups,omitempty"`
	Parties   []Party        `json:"parties,omitempty"`
	PrevSeats map[string]int `json:"prev_seats,omitempty"`
	Regions   []Region       `json:"regions"`
}

// EffectiveMeta returns s.Meta with EU-27 defaults filled in for any empty fields.
func (s Scenario) EffectiveMeta() ScenarioMeta {
	m := s.Meta
	if m.Title == "" {
		m.Title = "European Parliament"
	}
	if m.SeatLabel == "" {
		m.SeatLabel = "seats"
	}
	if m.RegionLabel == "" {
		m.RegionLabel = "Member State"
	}
	if m.UnitLabel == "" {
		m.UnitLabel = "County"
	}
	if m.ChartType == "" {
		m.ChartType = "hemicycle"
	}
	if m.Timezone == "" {
		m.Timezone = "CET"
	}
	if m.ClockStartHour <= 0 {
		m.ClockStartHour = 18.0
	}
	if m.ClockEndHour <= 0 {
		m.ClockEndHour = 25.5
	}
	if m.RegionsURL == "" {
		m.RegionsURL = "/geo/europe_nuts0.json"
	}
	if m.UnitsURL == "" {
		m.UnitsURL = "/geo/nuts3.json"
	}
	if m.ViewBox == "" {
		m.ViewBox = "0 0 1000 860"
	}
	if m.OverviewLabel == "" {
		m.OverviewLabel = "Europe"
	}
	if m.Attribution == "" {
		m.Attribution = "© EuroGeographics / Eurostat"
	}
	if m.WealthLabel == "" {
		m.WealthLabel = "GDP / capita"
	}
	if m.AgeLabel == "" {
		m.AgeLabel = "Age 65+ share"
	}
	if m.Axis2Label == "" {
		m.Axis2Label = "Pro-EU"
	}
	if m.Axis2Poles[0] == "" && m.Axis2Poles[1] == "" && m.Axis2Poles[2] == "" {
		m.Axis2Poles = [3]string{"Eurosceptic", "Neutral", "Pro-EU"}
	}
	return m
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
	if err = s.Validate(); err != nil {
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

const (
	MaxScenarioDescriptionBytes = 500
	MaxScenarioRegions          = 100
	MaxScenarioGroups           = 32
	MaxScenarioParties          = 64
	MaxScenarioPrevSeats        = 128
	MaxScenarioTotalSeats       = 2000
	MaxScenarioTotalPrecincts   = 5000
	MaxRegionVoterGroups        = 32
)

// Validate checks structural and numeric invariants of s.
func (s Scenario) Validate() error {
	if len(s.Meta.Description) > MaxScenarioDescriptionBytes {
		return fmt.Errorf("scenario %q description too long (%d > %d)", s.Name, len(s.Meta.Description), MaxScenarioDescriptionBytes)
	}
	if len(s.Regions) == 0 {
		return fmt.Errorf("scenario %q has no regions", s.Name)
	}
	if len(s.Regions) > MaxScenarioRegions {
		return fmt.Errorf("scenario %q has too many regions (%d > %d)", s.Name, len(s.Regions), MaxScenarioRegions)
	}
	if len(s.Groups) > MaxScenarioGroups {
		return fmt.Errorf("scenario %q has too many groups (%d > %d)", s.Name, len(s.Groups), MaxScenarioGroups)
	}
	if len(s.Parties) > MaxScenarioParties {
		return fmt.Errorf("scenario %q has too many top-level parties (%d > %d)", s.Name, len(s.Parties), MaxScenarioParties)
	}
	if len(s.PrevSeats) > MaxScenarioPrevSeats {
		return fmt.Errorf("scenario %q has too many prev_seats entries (%d > %d)", s.Name, len(s.PrevSeats), MaxScenarioPrevSeats)
	}
	switch s.Meta.ChartType {
	case "", "hemicycle", "bar":
	default:
		return fmt.Errorf("scenario %q has unknown chart_type %q", s.Name, s.Meta.ChartType)
	}
	if err := validateGeoURL(s.Name, "regions_url", s.Meta.RegionsURL); err != nil {
		return err
	}
	if err := validateGeoURL(s.Name, "units_url", s.Meta.UnitsURL); err != nil {
		return err
	}

	knownGroups := make(map[string]bool, len(s.Groups))
	for _, g := range s.Groups {
		if ax, bad := nonFiniteAxis(g.Position); bad {
			return fmt.Errorf("scenario %q has an EU group with non-finite position %v on axis %d", s.Name, g.Position, ax)
		}
		if g.ID == "" {
			return fmt.Errorf("scenario %q has an EU group with empty id", s.Name)
		}
		if knownGroups[g.ID] {
			return fmt.Errorf("scenario %q has duplicate EU group id %q", s.Name, g.ID)
		}
		knownGroups[g.ID] = true
	}

	allPartyIDs := make(map[string]bool, len(s.Parties))
	for _, p := range s.Parties {
		if p.ID != "" {
			allPartyIDs[p.ID] = true
		}
	}

	seenRegions := make(map[string]bool, len(s.Regions))
	totalSeats := 0
	totalPrecincts := 0
	for _, r := range s.Regions {
		if r.ID == "" {
			return fmt.Errorf("scenario %q has a region with empty id", s.Name)
		}
		if seenRegions[r.ID] {
			return fmt.Errorf("scenario %q has duplicate region id %q", s.Name, r.ID)
		}
		seenRegions[r.ID] = true
		if r.Seats <= 0 || r.Seats > MaxScenarioTotalSeats {
			if r.Seats <= 0 {
				return fmt.Errorf("region %q in scenario %q has no seats", r.ID, s.Name)
			}
			return fmt.Errorf("region %q in scenario %q seats (%d) must be between 1 and %d", r.ID, s.Name, r.Seats, MaxScenarioTotalSeats)
		}
		if len(r.Groups) == 0 {
			return fmt.Errorf("region %q in scenario %q has no voter groups", r.ID, s.Name)
		}
		if len(r.Groups) > MaxRegionVoterGroups {
			return fmt.Errorf("region %q in scenario %q has too many voter groups (%d > %d)", r.ID, s.Name, len(r.Groups), MaxRegionVoterGroups)
		}
		if len(r.Parties) > MaxScenarioParties {
			return fmt.Errorf("region %q in scenario %q has too many parties (%d > %d)", r.ID, s.Name, len(r.Parties), MaxScenarioParties)
		}
		if r.Precincts < 0 || r.Precincts > MaxScenarioTotalPrecincts {
			return fmt.Errorf("region %q in scenario %q precincts (%d) out of bounds", r.ID, s.Name, r.Precincts)
		}
		precincts := r.EffectivePrecinctCount()
		if precincts < 0 || precincts > MaxScenarioTotalPrecincts {
			return fmt.Errorf("region %q in scenario %q effective precincts (%d) out of bounds", r.ID, s.Name, precincts)
		}
		totalSeats += r.Seats
		totalPrecincts += precincts
		if r.Threshold < 0 || r.Threshold > 1 || math.IsNaN(r.Threshold) {
			return fmt.Errorf("region %q in scenario %q has invalid threshold %g", r.ID, s.Name, r.Threshold)
		}
		if r.PollCloseHour < 0 || r.PollCloseHour > 30 || isNonFinite(r.PollCloseHour) {
			return fmt.Errorf("region %q in scenario %q has invalid poll_close_hour %g", r.ID, s.Name, r.PollCloseHour)
		}
		if r.GeoPack != "" && !HasGeoPack(r.GeoPack) {
			return fmt.Errorf("region %q in scenario %q references unknown geo_pack %q", r.ID, s.Name, r.GeoPack)
		}
		if r.DistrictSeats < 0 {
			return fmt.Errorf("region %q in scenario %q has negative district_seats %d", r.ID, s.Name, r.DistrictSeats)
		}
		if r.DistrictSeats > 0 {
			districts := r.DistrictIDs()
			if len(districts) == 0 {
				return fmt.Errorf("region %q in scenario %q sets district_seats=%d but has no geopack districts", r.ID, s.Name, r.DistrictSeats)
			}
			if len(districts)*r.DistrictSeats > r.Seats {
				return fmt.Errorf("region %q in scenario %q district seats (%d × %d) exceed total seats (%d)", r.ID, s.Name, len(districts), r.DistrictSeats, r.Seats)
			}
		}
		parties := r.EffectiveParties(s.Parties)
		if len(parties) == 0 {
			return fmt.Errorf("region %q in scenario %q has no parties", r.ID, s.Name)
		}
		seenParties := make(map[string]bool, len(parties))
		for _, p := range parties {
			if isNonFinite(p.Appeal) {
				return fmt.Errorf("region %q has a party with non-finite appeal %g", r.ID, p.Appeal)
			}
			if ax, bad := nonFiniteAxis(p.Position); bad {
				return fmt.Errorf("region %q has a party with non-finite position %v on axis %d", r.ID, p.Position, ax)
			}
			if p.ID == "" {
				return fmt.Errorf("region %q has a party with empty id", r.ID)
			}
			if seenParties[p.ID] {
				return fmt.Errorf("region %q has duplicate party id %q", r.ID, p.ID)
			}
			seenParties[p.ID] = true
			allPartyIDs[p.ID] = true
			if s.UsesNationalParties() && p.GroupID != "" && !knownGroups[p.GroupID] {
				return fmt.Errorf("party %q in region %q references unknown EU group %q", p.ID, r.ID, p.GroupID)
			}
		}
		for pid, bonus := range r.PartyBonus {
			if !seenParties[pid] {
				return fmt.Errorf("region %q party_bonus references unknown party %q", r.ID, pid)
			}
			if isNonFinite(bonus) {
				return fmt.Errorf("region %q party_bonus[%q] has non-finite value %g", r.ID, pid, bonus)
			}
		}

		for _, vg := range r.Groups {
			if ax, bad := nonFiniteAxis(vg.Ideal); bad {
				return fmt.Errorf("region %q has a voter group %q with non-finite ideal %v on axis %d", r.ID, vg.ID, vg.Ideal, ax)
			}
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
				for vgID, mult := range tilts {
					if !knownVoterGroups[vgID] {
						return fmt.Errorf("region %q district_tilts[%q] references unknown voter group %q", r.ID, districtID, vgID)
					}
					if mult <= 0 || isNonFinite(mult) {
						return fmt.Errorf("region %q district_tilts[%q][%q] has invalid multiplier %g", r.ID, districtID, vgID, mult)
					}
				}
			}
		}

		switch r.Method {
		case "", MethodSainteLague, MethodDHondt, MethodHare, MethodFPTP, MethodSTV:
		default:
			return fmt.Errorf("region %q has unknown allocation method %q", r.ID, r.Method)
		}
	}
	if totalSeats > MaxScenarioTotalSeats {
		return fmt.Errorf("scenario %q total seats (%d) exceed limit of %d", s.Name, totalSeats, MaxScenarioTotalSeats)
	}
	if totalPrecincts > MaxScenarioTotalPrecincts {
		return fmt.Errorf("scenario %q total effective precincts (%d) exceed limit of %d", s.Name, totalPrecincts, MaxScenarioTotalPrecincts)
	}

	for id, seats := range s.PrevSeats {
		if seats < 0 {
			return fmt.Errorf("scenario %q prev_seats[%q] has negative seat count %d", s.Name, id, seats)
		}
		if s.UsesNationalParties() {
			if !knownGroups[id] {
				return fmt.Errorf("scenario %q prev_seats references unknown EU group %q", s.Name, id)
			}
		} else if !allPartyIDs[id] {
			return fmt.Errorf("scenario %q prev_seats references unknown party %q", s.Name, id)
		}
	}
	return nil
}

func isNonFinite(v float64) bool {
	return math.IsNaN(v) || math.IsInf(v, 0)
}

func nonFiniteAxis(v PolicyVec) (int, bool) {
	for ax, val := range v {
		if isNonFinite(val) {
			return ax, true
		}
	}
	return -1, false
}

func validateGeoURL(scenarioName, field, raw string) error {
	if raw == "" {
		return nil
	}
	if !strings.HasPrefix(raw, "/geo/") || !strings.HasSuffix(raw, ".json") || strings.Contains(raw, "..") || path.Clean(raw) != raw {
		return fmt.Errorf("scenario %q has invalid %s %q (must be /geo/*.json)", scenarioName, field, raw)
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlphaNum && c != '/' && c != '_' && c != '-' && c != '.' {
			return fmt.Errorf("scenario %q has invalid %s %q (must be /geo/*.json)", scenarioName, field, raw)
		}
	}
	return nil
}
