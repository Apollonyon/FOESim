package sim

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

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

func TestUK2024ScenarioAndGeoPack(t *testing.T) {
	if !HasGeoPack(GeoPackUK) {
		t.Fatalf("expected built-in GeoPack %q to be registered", GeoPackUK)
	}

	sc, err := LoadScenario("../scenarios/uk2024.json")
	if err != nil {
		t.Fatalf("LoadScenario(uk2024.json) failed: %v", err)
	}
	if len(sc.Regions) != 12 {
		t.Fatalf("expected 12 UK regions/nations, got %d", len(sc.Regions))
	}

	totalSeats := 0
	totalUnits := 0
	for _, r := range sc.Regions {
		totalSeats += r.Seats
		units := GeoPackUnits(GeoPackUK, r.ID)
		totalUnits += len(units)
		if r.DistrictSeats != 1 {
			t.Errorf("region %q: district_seats = %d, want 1", r.ID, r.DistrictSeats)
		}
		if len(units) != r.Seats {
			t.Errorf("region %q: %d geopack units != %d seats", r.ID, len(units), r.Seats)
		}
	}
	if totalSeats != 650 || totalUnits != 650 {
		t.Fatalf("expected 650 UK seats and 650 constituencies, got seats=%d units=%d", totalSeats, totalUnits)
	}

	prep := PrepareElection(sc.Regions, sc.Parties, ElectionConfig{
		Temperature: 0.45,
		Swing:       0,
		Seed:        42,
	})
	totals := sc.AggregateEUSeats(prep.ExitPollSeats())
	seatSum := 0
	for _, s := range totals {
		seatSum += s
	}
	if seatSum != 650 {
		t.Fatalf("simulated UK seat sum = %d, want 650", seatSum)
	}
	if totals["lab"] < 400 || totals["lab"] > 420 {
		t.Errorf("expected calibrated Labour baseline ~411, got %d", totals["lab"])
	}
	if totals["con"] < 115 || totals["con"] > 130 {
		t.Errorf("expected calibrated Conservative baseline ~121, got %d", totals["con"])
	}
	if totals["ld"] < 68 || totals["ld"] > 76 {
		t.Errorf("expected calibrated Lib Dem baseline ~72, got %d", totals["ld"])
	}
	if totals["snp"] < 7 || totals["snp"] > 11 {
		t.Errorf("expected calibrated SNP baseline ~9, got %d", totals["snp"])
	}
	if totals["sf"] != 7 || totals["dup"] != 5 || totals["apni"] != 1 {
		t.Errorf("expected calibrated NI baseline (SF=7, DUP=5, APNI=1), got SF=%d DUP=%d APNI=%d",
			totals["sf"], totals["dup"], totals["apni"])
	}

	fc, err := RunForecast(context.Background(), sc, ForecastConfig{
		Runs:        20,
		Temperature: 0.45,
		Swing:       0.15,
		BaseSeed:    42,
	})
	if err != nil {
		t.Fatalf("RunForecast(uk2024) failed: %v", err)
	}
	if fc.TotalSeats != 650 || fc.Majority != 326 {
		t.Errorf("forecast seats/majority = %d/%d, want 650/326", fc.TotalSeats, fc.Majority)
	}
	distCount := 0
	for _, rf := range fc.Regions {
		distCount += len(rf.DistrictWinProb)
	}
	if distCount != 650 {
		t.Errorf("forecast district_win_prob count = %d, want 650", distCount)
	}
}

func TestLoadGeoPacksDirAndValidation(t *testing.T) {
	tmp := t.TempDir()

	// Valid external pack with units.json.
	packDir := filepath.Join(tmp, "demo_ext_pack")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatalf("mkdir packDir: %v", err)
	}
	validUnits := `{
		"north": [
			{"id": "N01", "district_id": "N-1", "name": "North City", "urban_type": 1, "turnout": 0.72, "weight": 0.6},
			{"id": "N02", "district_id": "N-2", "name": "North Valley", "urban_type": 3, "turnout": 0.65, "weight": 0.4}
		]
	}`
	if err := os.WriteFile(filepath.Join(packDir, "units.json"), []byte(validUnits), 0o644); err != nil {
		t.Fatalf("write units.json: %v", err)
	}

	// Map-only pack (no units.json) and hidden dir should be skipped cleanly.
	if err := os.MkdirAll(filepath.Join(tmp, "map_only"), 0o755); err != nil {
		t.Fatalf("mkdir map_only: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "map_only", "regions.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write map_only regions.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, ".hidden"), 0o755); err != nil {
		t.Fatalf("mkdir .hidden: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".hidden", "units.json"), []byte(`{invalid`), 0o644); err != nil {
		t.Fatalf("write .hidden units.json: %v", err)
	}

	t.Cleanup(func() {
		geoPacksMu.Lock()
		delete(geoPacks, "demo_ext_pack")
		geoPacksMu.Unlock()
	})

	loaded, err := LoadGeoPacksDir(tmp)
	if err != nil {
		t.Fatalf("LoadGeoPacksDir failed: %v", err)
	}
	if !slices.Equal(loaded, []string{"demo_ext_pack"}) {
		t.Fatalf("loaded = %v, want [demo_ext_pack]", loaded)
	}
	if !HasGeoPack("demo_ext_pack") {
		t.Fatalf("expected demo_ext_pack to be registered")
	}
	if !slices.Contains(ListGeoPacks(), "demo_ext_pack") {
		t.Errorf("ListGeoPacks() missing demo_ext_pack: %v", ListGeoPacks())
	}

	units := GeoPackUnits("demo_ext_pack", "NORTH")
	if len(units) != 2 || units[0].ID != "N01" || units[0].Turnout != 0.72 {
		t.Fatalf("unexpected loaded units: %+v", units)
	}

	// Scenario referencing the external pack and /geo/packs/... URLs passes Validate().
	reg := DefaultRegions()[0]
	reg.ID = "north"
	reg.UseNUTS3 = false
	reg.GeoPack = "demo_ext_pack"
	sc := Scenario{
		Name: "External Pack Scenario",
		Meta: ScenarioMeta{
			RegionsURL: "/geo/packs/demo_ext_pack/regions.json",
			UnitsURL:   "/geo/packs/demo_ext_pack/units_map.json",
		},
		Parties: DefaultParties(),
		Regions: []Region{reg},
	}
	if err := sc.Validate(); err != nil {
		t.Fatalf("expected scenario with external pack to validate, got %v", err)
	}

	// Invalid pack names and malformed/invalid unit payloads are rejected.
	badCases := []struct {
		name string
		pack string
		body string
	}{
		{"invalid pack name", "../evil", validUnits},
		{"empty pack name", "", validUnits},
		{"malformed json", "bad_json", "{broken"},
		{"empty object", "empty_obj", "{}"},
		{"empty unit id", "empty_uid", `{"r":[{"id":"","weight":1}]}`},
		{"duplicate unit id", "dup_uid", `{"r":[{"id":"u1","weight":0.5},{"id":"u1","weight":0.5}]}`},
		{"negative weight", "neg_w", `{"r":[{"id":"u1","weight":-1}]}`},
		{"zero total weight", "zero_w", `{"r":[{"id":"u1","weight":0}]}`},
		{"turnout > 1", "bad_t", `{"r":[{"id":"u1","weight":1,"turnout":1.5}]}`},
		{"case-insensitive dup region", "dup_reg", `{"DE":[{"id":"u1","weight":1}],"de":[{"id":"u2","weight":1}]}`},
		{"negative population", "neg_pop", `{"r":[{"id":"u1","weight":1,"population":-10}]}`},
		{"invalid urban_type", "bad_urban", `{"r":[{"id":"u1","weight":1,"urban_type":9}]}`},
	}
	for _, tc := range badCases {
		p := filepath.Join(tmp, tc.name+".json")
		if err := os.WriteFile(p, []byte(tc.body), 0o644); err != nil {
			t.Fatalf("write %s: %v", tc.name, err)
		}
		if err := LoadGeoPackFile(tc.pack, p); err == nil {
			t.Errorf("case %q: expected error, got nil", tc.name)
		}
	}

	// Symlink escaping packsDir must be rejected by LoadGeoPacksDir (os.OpenRoot).
	outside := filepath.Join(t.TempDir(), "outside_units.json")
	if err := os.WriteFile(outside, []byte(validUnits), 0o644); err != nil {
		t.Fatalf("write outside_units.json: %v", err)
	}
	symRoot := t.TempDir()
	symPack := filepath.Join(symRoot, "sym_pack")
	if err := os.MkdirAll(symPack, 0o755); err != nil {
		t.Fatalf("mkdir symPack: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(symPack, "units.json")); err == nil {
		if _, err := LoadGeoPacksDir(symRoot); err == nil {
			t.Errorf("expected symlink escaping packsDir to be rejected, got nil")
		}
	}
}
