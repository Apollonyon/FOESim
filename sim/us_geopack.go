package sim

import _ "embed"

//go:embed data/us_counties_meta.json
var usCountiesMetaRaw []byte

func init() {
	RegisterGeoPackJSON(GeoPackUS, usCountiesMetaRaw)
}
