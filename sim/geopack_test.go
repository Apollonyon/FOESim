package sim

import "testing"

func TestGeoPackRegistryAndDefensiveCopy(t *testing.T) {
	if !HasGeoPack(GeoPackEU) {
		t.Fatalf("expected built-in GeoPack %q to be registered", GeoPackEU)
	}
	if HasGeoPack("does_not_exist") {
		t.Fatalf("expected unknown GeoPack to report false")
	}

	const testPack = "test_custom_pack"
	RegisterGeoPack(testPack, map[string][]PrecinctUnit{
		"XX": {
			{ID: "XX01", DistrictID: "XX-1", Name: "North County", Weight: 0.6},
			{ID: "XX02", DistrictID: "XX-2", Name: "South County", Weight: 0.4},
		},
	})

	if !HasGeoPack(testPack) {
		t.Fatalf("expected %q to be registered", testPack)
	}

	units := GeoPackUnits(testPack, "xx")
	if len(units) != 2 {
		t.Fatalf("got %d units, want 2", len(units))
	}
	units[0].Name = "MUTATED"

	fresh := GeoPackUnits(testPack, "XX")
	if fresh[0].Name != "North County" {
		t.Fatalf("GeoPackUnits exposed shared slice: got %q, want %q", fresh[0].Name, "North County")
	}
}

func TestRegionEffectiveGeoPackAndSimulation(t *testing.T) {
	const testPack = "test_sim_pack"
	RegisterGeoPack(testPack, map[string][]PrecinctUnit{
		"me": {
			{ID: "23005", DistrictID: "ME-1", Name: "Cumberland", UrbanType: 1, Weight: 0.5},
			{ID: "23019", DistrictID: "ME-2", Name: "Penobscot", UrbanType: 3, Weight: 0.5},
		},
	})

	rLegacy := Region{ID: "de", UseNUTS3: true}
	if got := rLegacy.EffectiveGeoPack(); got != GeoPackEU {
		t.Errorf("legacy UseNUTS3: got %q, want %q", got, GeoPackEU)
	}

	rExplicit := Region{ID: "me", UseNUTS3: true, GeoPack: testPack}
	if got := rExplicit.EffectiveGeoPack(); got != testPack {
		t.Errorf("explicit GeoPack precedence: got %q, want %q", got, testPack)
	}

	rNone := Region{ID: "me", Precincts: 4}
	if got := rNone.EffectiveGeoPack(); got != "" {
		t.Errorf("no geopack: got %q, want empty", got)
	}
	if got := rNone.EffectivePrecinctCount(); got != 4 {
		t.Errorf("synthetic precinct count: got %d, want 4", got)
	}

	reg := DefaultRegions()[0]
	reg.ID = "me"
	reg.UseNUTS3 = false
	reg.GeoPack = testPack
	if got := reg.EffectivePrecinctCount(); got != 2 {
		t.Fatalf("GeoPack precinct count: got %d, want 2", got)
	}

	reports := SimulatePrecincts(reg, DefaultParties(), 0.85, 0.0, 42)
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want 2", len(reports))
	}
	if reports[0].PrecinctID != "23005" || reports[0].DistrictID != "ME-1" {
		t.Errorf("unexpected first report: id=%q district=%q", reports[0].PrecinctID, reports[0].DistrictID)
	}
}

func TestScenarioEffectiveMeta(t *testing.T) {
	defaults := (Scenario{}).EffectiveMeta()
	if defaults.Title != "European Parliament" {
		t.Errorf("default Title = %q, want %q", defaults.Title, "European Parliament")
	}
	if defaults.SeatLabel != "seats" {
		t.Errorf("default SeatLabel = %q, want %q", defaults.SeatLabel, "seats")
	}
	if defaults.RegionLabel != "Member State" {
		t.Errorf("default RegionLabel = %q, want %q", defaults.RegionLabel, "Member State")
	}
	if defaults.UnitLabel != "County" {
		t.Errorf("default UnitLabel = %q, want %q", defaults.UnitLabel, "County")
	}
	if defaults.ChartType != "hemicycle" {
		t.Errorf("default ChartType = %q, want %q", defaults.ChartType, "hemicycle")
	}
	if defaults.Timezone != "CET" {
		t.Errorf("default Timezone = %q, want %q", defaults.Timezone, "CET")
	}
	if defaults.ClockStartHour != 18.0 || defaults.ClockEndHour != 25.5 {
		t.Errorf("default clock = [%.1f, %.1f], want [18.0, 25.5]", defaults.ClockStartHour, defaults.ClockEndHour)
	}
	if defaults.RegionsURL != "/geo/europe_nuts0.json" || defaults.UnitsURL != "/geo/nuts3.json" {
		t.Errorf("default geo URLs = (%q, %q)", defaults.RegionsURL, defaults.UnitsURL)
	}
	if defaults.ViewBox != "0 0 1000 860" || defaults.OverviewLabel != "Europe" {
		t.Errorf("default map view = (%q, %q)", defaults.ViewBox, defaults.OverviewLabel)
	}
	if defaults.WealthLabel != "GDP / capita" || defaults.AgeLabel != "Age 65+ share" {
		t.Errorf("default overlay labels = (%q, %q)", defaults.WealthLabel, defaults.AgeLabel)
	}

	custom := (Scenario{
		Meta: ScenarioMeta{
			Title:          "U.S. Electoral College",
			SeatLabel:      "EVs",
			ChartType:      "bar",
			HideCoalitions: true,
			Timezone:       "EST",
			ClockStartHour: 19.0,
			ClockEndHour:   25.0,
		},
	}).EffectiveMeta()
	if custom.Title != "U.S. Electoral College" || custom.ChartType != "bar" || !custom.HideCoalitions {
		t.Errorf("custom overrides lost: %+v", custom)
	}
	if custom.UnitLabel != "County" {
		t.Errorf("unset custom field should fall back to default: got %q, want %q", custom.UnitLabel, "County")
	}
}

func TestScenarioValidateGeoPackAndChartType(t *testing.T) {
	base := func() Scenario {
		return Scenario{
			Name:    "test",
			Parties: DefaultParties(),
			Regions: []Region{DefaultRegions()[0]},
		}
	}

	s := base()
	s.Regions[0].GeoPack = "no_such_pack"
	if err := s.Validate(); err == nil {
		t.Errorf("expected error for unknown geo_pack, got nil")
	}

	s = base()
	s.Regions[0].GeoPack = GeoPackEU
	if err := s.Validate(); err != nil {
		t.Errorf("expected valid built-in geo_pack, got %v", err)
	}

	s = base()
	s.Meta.ChartType = "pie"
	if err := s.Validate(); err == nil {
		t.Errorf("expected error for invalid chart_type, got nil")
	}

	s = base()
	s.Meta.ChartType = "bar"
	if err := s.Validate(); err != nil {
		t.Errorf("expected valid chart_type %q, got %v", "bar", err)
	}

	s = base()
	s.Meta.RegionsURL = "https://evil.example/map.json"
	if err := s.Validate(); err == nil {
		t.Errorf("expected error for external regions_url, got nil")
	}

	s = base()
	s.Meta.UnitsURL = "/geo/../secret.json"
	if err := s.Validate(); err == nil {
		t.Errorf("expected error for traversal units_url, got nil")
	}

	s = base()
	s.Meta.UnitsURL = "/geo/%2e%2e/api/scenario.json"
	if err := s.Validate(); err == nil {
		t.Errorf("expected error for percent-encoded traversal units_url, got nil")
	}

	s = base()
	s.Meta.RegionsURL = "/geo//us_states.json"
	if err := s.Validate(); err == nil {
		t.Errorf("expected error for non-clean regions_url, got nil")
	}

	s = base()
	s.Meta.RegionsURL = "/geo/us_states.json"
	s.Meta.UnitsURL = "/geo/us_counties.json"
	if err := s.Validate(); err != nil {
		t.Errorf("expected valid /geo/*.json URLs, got %v", err)
	}
}
