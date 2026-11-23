package sim

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
)

//go:embed data/nuts3_meta.json
var nuts3MetaRaw []byte

// PrecinctUnit describes a geographic reporting unit (such as a Eurostat NUTS-3 county).
type PrecinctUnit struct {
	ID         string  `json:"id"`
	DistrictID string  `json:"district_id,omitempty"`
	Name       string  `json:"name"`
	UrbanType  int     `json:"urban_type,omitempty"` // 1 = urban, 2 = intermediate, 3 = rural
	Population int     `json:"population,omitempty"` // real Eurostat 2023 resident population
	Weight     float64 `json:"weight"`               // normalized share of region population
	WealthZ    float64 `json:"wealth_z,omitempty"`   // country-relative log-GDP/capita z-score in [-1.5, 1.5]
	AgeZ       float64 `json:"age_z,omitempty"`      // country-relative age 65+ share z-score in [-1.5, 1.5]
}

var (
	nuts3Once sync.Once
	nuts3Data map[string][]PrecinctUnit
)

// NUTS3Units returns a copy of the embedded Eurostat NUTS-3 reporting units for
// a 2-letter Eurostat country code (e.g. "de", "fr", "it", "el"), or nil if none exist.
func NUTS3Units(regionID string) []PrecinctUnit {
	nuts3Once.Do(func() {
		nuts3Data = make(map[string][]PrecinctUnit)
		if err := json.Unmarshal(nuts3MetaRaw, &nuts3Data); err != nil {
			panic(fmt.Sprintf("sim: invalid embedded nuts3_meta.json: %v", err))
		}
	})
	return slices.Clone(nuts3Data[strings.ToLower(regionID)])
}

// countyTilt returns a socioeconomic demographic multiplier for voter group g in
// county u using Eurostat URBN_TYPE, country-relative GDP/capita (WealthZ), and age 65+ share (AgeZ).
func countyTilt(g VoterGroup, u PrecinctUnit) float64 {
	tilt := 1.0

	// 1. Urban vs. Rural cosmopolitan affinity
	var urbanBias float64
	switch u.UrbanType {
	case 1:
		urbanBias = 0.48
	case 3:
		urbanBias = -0.48
	}
	if urbanBias != 0 {
		affinity := (g.Ideal[AxisEU] - g.Ideal[AxisSocial] - g.Ideal[AxisMigration]) / 3.0
		tilt *= math.Max(0.35, 1.0+urbanBias*affinity)
	}

	// 2. Wealth (GDP/capita): wealthier counties tilt toward pro-market/pro-EU groups;
	// lower-GDP counties tilt toward redistribution/anti-establishment groups.
	if u.WealthZ != 0 {
		wealthAffinity := g.Ideal[AxisEcon]*0.65 + g.Ideal[AxisEU]*0.35
		tilt *= math.Max(0.45, 1.0+0.18*u.WealthZ*wealthAffinity)
	}

	// 3. Age structure (65+ share): older counties tilt toward traditional/conservative groups;
	// younger counties tilt toward progressive/green groups.
	if u.AgeZ != 0 {
		ageAffinity := g.Ideal[AxisSocial]*0.65 - g.Ideal[AxisEnv]*0.35
		tilt *= math.Max(0.45, 1.0+0.18*u.AgeZ*ageAffinity)
	}

	return math.Max(0.25, math.Min(3.0, tilt))
}
