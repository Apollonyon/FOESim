# ParliamentSim

A live **European Parliament election simulator** written in Go (zero external dependencies) with an interactive **Eurostat NUTS-3** election-night web atlas powered by Server-Sent Events (SSE) and `//go:embed`.

## Features

- **Full EU-27 Coverage (720 Seats):** All 27 EU Member States, 154 national parties rolling up into 9 European Parliament political groups, and **1,152 continental NUTS-3 counties**.
- **Real Eurostat Socioeconomic Data:** Every NUTS-3 county uses official Eurostat 2023 population (`demo_r_pjanaggr3`), GDP per capita (`nama_10r_3gdp`), age 65+ share (`demo_r_pjanind3`), and urban/rural typology (`URBN_TYPE`).
- **6-Axis Spatial Voting Model:** Numerically stable Softmax choice over weighted Euclidean distances across 6 policy axes (`Econ`, `Social`, `EU`, `Defense`, `Env`, `Migration`) and 6 standardized European voter blocs.
- **National Electoral Laws:** Per-country seat quotas, thresholds, and allocation algorithms (`D'Hondt`, `Sainte-Laguë`, and `Hare Quota / Largest Remainder`).
- **Live Election-Night Streaming:** Concurrent per-country goroutines with staggered CET poll-closing waves (`18:00 → 23:00`), deterministic multi-wave partial county reporting, live seat swings, and a real-time minimal-winning **Coalition Finder**.
- **Zero-Build Web Atlas:** Native ES modules + SVG hemicycle + Lambert Azimuthal Equal-Area Europe map with 6 live overlays (`Winner`, `Group %`, `Turnout`, `Pro-EU`, `Econ`, `Migration`), Light/Dark theme toggle, and auto-switching EU Group / National Party colors on zoom.

## Quick Start

Requires **Go 1.24+** (developed on Go 1.26).

```bash
# Run the EU-27 720-seat web dashboard (http://localhost:8080)
go run .

# Run the 3-country starter scenario (DE, FR, IT)
go run . -scenario scenarios/eu_starter.json

# Run live election night in the terminal instead of starting HTTP server
go run . -cli

# Custom listen address
go run . -addr :9090
```

## Tests & Benchmarks

```bash
go test -race -count=1 -v ./...
go vet ./...
go test -bench=. ./sim
```

## Project Structure

```text
main.go                  CLI flags, terminal streamer, and HTTP server entrypoint
scenarios/
  eu27.json              Full EU-27 720-seat scenario (154 national parties)
  eu_starter.json        Starter 3-country scenario (DE, FR, IT)
sim/
  policy.go              6-axis PolicyVec and weighted Euclidean distance
  party.go               National Party and EUGroup types
  region.go              Region, VoterGroup, and hierarchical NUTS-1/2/3 tilts
  vote.go                Softmax VoteShares, SimulateRegion, and PartyBonus
  seats.go               D'Hondt, Sainte-Laguë, Hare Quota, SumSeats, RollupGroupSeats
  nuts.go                Embedded Eurostat NUTS-3 metadata & socioeconomic tilts
  election.go            Precinct simulation, deterministic partial waves, concurrent streaming
  coalition.go           Minimal-winning Coalition Finder ranked by PolicyVec spread
  scenario.go            JSON scenario loader, validator, and EU seat aggregation
web/
  server.go              net/http server, //go:embed static assets, and SSE /api/stream
  static/                Zero-build HTML, Light/Dark CSS, ES modules, and Eurostat SVG GeoJSON
```

## Data Sources & Attribution

- **Boundaries:** Eurostat GISCO NUTS 2021 (`10M` / `20M`), © EuroGeographics.
- **Statistics:** Eurostat (`demo_r_pjanaggr3`, `nama_10r_3gdp`, `demo_r_pjanind3`).

## License

Licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)** — see [LICENSE](LICENSE) and [NOTICE](NOTICE) for full terms, attribution requirements, and trademark guidelines.
