package sim

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

const (
	GeoPackEU = "eu_nuts3"
	GeoPackUS = "us_counties"
	GeoPackUK = "uk_constituencies"

	maxGeoPackNameLen   = 64
	maxGeoPackFileBytes = 32 << 20 // 32 MiB
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

// self explaining :D.
func ListGeoPacks() []string {
	geoPacksMu.RLock()
	names := slices.Sorted(maps.Keys(geoPacks))
	geoPacksMu.RUnlock()
	return names
}

// self explanatory
func IsValidGeoPackName(name string) bool {
	if name == "" || len(name) > maxGeoPackNameLen {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

// I feel like im repeating myself a tiny bit
func LoadGeoPackFile(pack, filePath string) error {
	if !IsValidGeoPackName(pack) {
		return fmt.Errorf("invalid geopack name %q (must match [A-Za-z0-9_-]{1,%d})", pack, maxGeoPackNameLen)
	}

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	return loadGeoPackFromFile(pack, filePath, f)
}

func loadGeoPackFromFile(pack, source string, f *os.File) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("geopack %q file %s is not a regular file", pack, source)
	}

	raw, err := io.ReadAll(io.LimitReader(f, maxGeoPackFileBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxGeoPackFileBytes {
		return fmt.Errorf("geopack %q file %s exceeds %d byte limit", pack, source, maxGeoPackFileBytes)
	}

	var data map[string][]PrecinctUnit
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("decode geopack %q (%s): %w", pack, source, err)
	}
	if err := validateGeoPackData(pack, data); err != nil {
		return err
	}

	RegisterGeoPack(pack, data)
	return nil
}

func validateGeoPackData(pack string, data map[string][]PrecinctUnit) error {
	if len(data) == 0 {
		return fmt.Errorf("geopack %q has no regions", pack)
	}
	seenRegion := make(map[string]bool, len(data))
	totalUnits := 0
	for rid, units := range data {
		normRID := strings.ToLower(strings.TrimSpace(rid))
		if normRID == "" {
			return fmt.Errorf("geopack %q contains an empty region id", pack)
		}
		if seenRegion[normRID] {
			return fmt.Errorf("geopack %q contains duplicate region id %q (case-insensitive)", pack, rid)
		}
		seenRegion[normRID] = true
		if len(units) == 0 {
			return fmt.Errorf("geopack %q region %q has no units", pack, rid)
		}
		seenUnit := make(map[string]bool, len(units))
		weightSum := 0.0
		for _, u := range units {
			if u.ID == "" {
				return fmt.Errorf("geopack %q region %q has a unit with empty id", pack, rid)
			}
			if seenUnit[u.ID] {
				return fmt.Errorf("geopack %q region %q has duplicate unit id %q", pack, rid, u.ID)
			}
			seenUnit[u.ID] = true
			if u.Population < 0 {
				return fmt.Errorf("geopack %q region %q unit %q has negative population %d", pack, rid, u.ID, u.Population)
			}
			if u.UrbanType < 0 || u.UrbanType > 3 {
				return fmt.Errorf("geopack %q region %q unit %q has invalid urban_type %d (must be 0..3)", pack, rid, u.ID, u.UrbanType)
			}
			if u.Weight < 0 || isNonFinite(u.Weight) {
				return fmt.Errorf("geopack %q region %q unit %q has invalid weight %g", pack, rid, u.ID, u.Weight)
			}
			if u.Turnout < 0 || u.Turnout > 1 || isNonFinite(u.Turnout) {
				return fmt.Errorf("geopack %q region %q unit %q has invalid turnout %g", pack, rid, u.ID, u.Turnout)
			}
			if isNonFinite(u.WealthZ) || isNonFinite(u.AgeZ) {
				return fmt.Errorf("geopack %q region %q unit %q has non-finite demographic z-score", pack, rid, u.ID)
			}
			weightSum += u.Weight
			totalUnits++
		}
		if weightSum <= 0 {
			return fmt.Errorf("geopack %q region %q has no positive unit weights", pack, rid)
		}
	}
	if totalUnits == 0 {
		return fmt.Errorf("geopack %q has no reporting units", pack)
	}
	return nil
}

// LoadGeoPacksDir scans dir for pack subdirectories (<dir>/<pack>/units.json)
// and registers each valid pack into the GeoPack registry. Subdirectories without
// a units.json file (e.g. map-only packs) and dotfiles are skipped. Symlinks
// escaping dir are rejected via os.OpenRoot.
func LoadGeoPacksDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	var loaded []string
	for _, de := range entries {
		name := de.Name()
		if !de.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		relUnits := name + "/units.json"
		f, err := root.Open(relUnits)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("open geopack %q (%s): %w", name, filepath.Join(dir, name, "units.json"), err)
		}
		st, statErr := f.Stat()
		if statErr != nil {
			_ = f.Close()
			return nil, statErr
		}
		if st.IsDir() {
			_ = f.Close()
			continue
		}
		if !IsValidGeoPackName(name) {
			_ = f.Close()
			return nil, fmt.Errorf("invalid geopack name %q (must match [A-Za-z0-9_-]{1,%d})", name, maxGeoPackNameLen)
		}
		err = loadGeoPackFromFile(name, filepath.Join(dir, name, "units.json"), f)
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, name)
	}
	slices.Sort(loaded)
	return loaded, nil
}
