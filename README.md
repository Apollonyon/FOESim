# ParliamentSim

A universal election-night simulator written in Go with no third-party dependencies, paired with an interactive HTML5 Canvas (`Path2D`) county atlas streamed over Server-Sent Events (SSE) and embedded via `//go:embed`.

## What It Simulates

- **EU-27 (720 seats):** All 27 Member States, 154 national parties rolling up into 9 European Parliament political groups, across 1,152 continental Eurostat NUTS-3 counties.
- **USA 2024 (538 Electoral Votes):** All 50 states + DC across 3,141 U.S. counties (`us-atlas` Albers USA) with YAPms-style clickable callout badges (`DC`, `ME-1/2`, `NE-1/2/3`, small East Coast states), realistic EST poll-closing waves, a 270-to-win Electoral College bar, and exact 2024 calibration (`REP 312 / DEM 226`).
- **Universal `GeoPack` & Two-Tier Allocation:** Pluggable geography packs (`eu_nuts3`, `us_counties`), `ScenarioMeta` UI configuration, `SeatAllocator` (`Sainte-Laguë`, `D'Hondt`, `Hare`, `STV`, `FPTP`), and parallel two-tier district allocation (`district_seats`) supporting Maine/Nebraska split EVs as well as UK/India constituencies and Japan/Russia mixed systems.
- **6-axis spatial voting & hierarchical swings:** Temperature-scaled Softmax over weighted Euclidean distance with pan-regional, state, district, and county correlated shocks, plus a live **System** switcher in the web UI.
- **Embedded web atlas:** Plain HTML/CSS/JS ES modules (XSS-safe DOM, HiDPI Canvas `Path2D` with offscreen caching & spatial grid), SVG hemicycle & electoral bar, 6 data overlays, Coalition Finder, and Light/Dark themes.

> **Note:** Built as a hands-on Go learning project: core Go domain logic and algorithms are written with AI coaching/review, while frontend JS/CSS, dataset scripts, and boilerplate scaffolding are AI-assisted.

## Quick Start

Requires **Go 1.24+**.

```bash
# Run the EU-27 720-seat web dashboard (http://localhost:8080)
go run .

# Run the USA 2024 538-EV (3,141 counties) web dashboard
go run . -scenario scenarios/usa2024.json

# Run the 3-country EU starter scenario (DE, FR, IT)
go run . -scenario scenarios/eu_starter.json

# Run live election night in the terminal instead of starting HTTP server
go run . -cli

# Custom listen address and SSE stream caps (defaults: 256 global, 16 per IP; use -max-streams-per-ip=0 behind a reverse proxy)
go run . -addr 127.0.0.1:9090 -max-streams 128 -max-streams-per-ip 8
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
  usa2024.json           USA 2024 538-EV scenario (51 states/DC, 3,141 counties, ME/NE CD splits)
scripts/
  build_us_pack.go       Reproducible Go generator for U.S. Census boundaries & 2024 calibration
sim/
  policy.go              6-axis PolicyVec and weighted Euclidean distance
  party.go               National Party and EUGroup types
  region.go              Region, VoterGroup, GeoPack selection, and hierarchical tilts
  vote.go                Softmax VoteShares, SimulateRegion, and PartyBonus
  seats.go               SeatAllocator interface, D'Hondt, Sainte-Laguë, Hare, FPTP, two-tier AllocateRegionSeats
  stv.go                 Droop-quota Ranked-Ballot STV with PolicyVec preference transfers
  state.go               Incremental ElectionState vote/seat/district/progress tracker
  seed.go                Built-in test fixture parties and regions
  geopack.go             Thread-safe lazy GeoPack registry (eu_nuts3, us_counties)
  nuts.go                Embedded Eurostat NUTS-3 metadata & socioeconomic tilts
  us_geopack.go          Embedded U.S. 3,141-county GeoPack registration
  election.go            Hierarchical swings, PrepareElection, deterministic waves, SSE streaming
  coalition.go           Minimal-winning Coalition Finder ranked by PolicyVec spread
  scenario.go            JSON scenario loader, ScenarioMeta defaults, Validate(), seat aggregation
web/
  server.go              net/http server, //go:embed static assets, and SSE /api/stream
  static/                Zero-build HTML, Light/Dark CSS, XSS-safe ES modules, EU & US Canvas GeoJSON
```

## Data Sources & Attribution

- **EU Boundaries & Stats:** Eurostat GISCO NUTS 2021 (`10M` / `20M`), © EuroGeographics; Eurostat (`demo_r_pjanaggr3`, `nama_10r_3gdp`, `demo_r_pjanind3`).
- **USA Boundaries & Results:** U.S. Census Bureau TIGER/Line 2023 Cartographic Boundaries (`cb_2023_us_state_20m`, `cb_2023_us_county_20m`, `cb_2023_us_cd118_20m`) and certified 2024 county presidential totals.

## License

Licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)** — see [LICENSE](LICENSE) and [NOTICE](NOTICE) for full terms, attribution requirements, and trademark guidelines.
