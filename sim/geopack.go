package sim

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
)

const (
	GeoPackEU = "eu_nuts3"
	GeoPackUS = "us_counties"
	GeoPackUK = "uk_constituencies"
)

type geoPackEntry struct {
	raw    []byte
	parsed map[string][]PrecinctUnit
}

var (
	geoPacksMu sync.RWMutex
	geoPacks   = make(map[string]*geoPackEntry)
)

// RegisterGeoPack registers an in-memory map of regionID -> reporting units under pack.
func RegisterGeoPack(pack string, data map[string][]PrecinctUnit) {
	cloned := make(map[string][]PrecinctUnit, len(data))
	for rid, units := range data {
		cloned[strings.ToLower(rid)] = slices.Clone(units)
	}
	geoPacksMu.Lock()
	geoPacks[pack] = &geoPackEntry{parsed: cloned}
	geoPacksMu.Unlock()
}

// RegisterGeoPackJSON registers raw JSON bytes (regionID -> []PrecinctUnit) to be
// decoded lazily on the first GeoPackUnits call for pack.
func RegisterGeoPackJSON(pack string, raw []byte) {
	geoPacksMu.Lock()
	geoPacks[pack] = &geoPackEntry{raw: slices.Clone(raw)}
	geoPacksMu.Unlock()
}

// HasGeoPack reports whether pack is registered.
func HasGeoPack(pack string) bool {
	geoPacksMu.RLock()
	_, ok := geoPacks[pack]
	geoPacksMu.RUnlock()
	return ok
}

// GeoPackUnits returns a defensive copy of the reporting units for regionID
// from the named GeoPack, or nil if the pack or region does not exist.
func GeoPackUnits(pack, regionID string) []PrecinctUnit {
	if pack == "" {
		return nil
	}
	key := strings.ToLower(regionID)

	geoPacksMu.RLock()
	entry, ok := geoPacks[pack]
	if !ok {
		geoPacksMu.RUnlock()
		return nil
	}
	if entry.parsed != nil {
		units := slices.Clone(entry.parsed[key])
		geoPacksMu.RUnlock()
		return units
	}
	geoPacksMu.RUnlock()

	geoPacksMu.Lock()
	defer geoPacksMu.Unlock()
	entry, ok = geoPacks[pack]
	if !ok {
		return nil
	}
	if entry.parsed == nil {
		raw := make(map[string][]PrecinctUnit)
		if err := json.Unmarshal(entry.raw, &raw); err != nil {
			panic(fmt.Sprintf("sim: invalid geopack %q JSON: %v", pack, err))
		}
		normalized := make(map[string][]PrecinctUnit, len(raw))
		for rid, units := range raw {
			normalized[strings.ToLower(rid)] = units
		}
		entry.parsed = normalized
		entry.raw = nil
	}
	return slices.Clone(entry.parsed[key])
}
