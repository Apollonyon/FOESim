package sim

import _ "embed"

//go:embed data/uk_constituencies_meta.json
var ukConstituenciesMetaRaw []byte

func init() {
	RegisterGeoPackJSON(GeoPackUK, ukConstituenciesMetaRaw)
}
