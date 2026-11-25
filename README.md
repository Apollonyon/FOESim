# ParliamentSim

A European Parliament election simulator written in Go with no third-party dependencies, paired with an interactive Eurostat NUTS-3 election-night web map streamed over Server-Sent Events (SSE) and embedded via `//go:embed`.

## What It Simulates

- **EU-27 (720 seats):** All 27 Member States, 154 national parties rolling up into 9 European Parliament political groups, across 1,152 continental NUTS-3 counties.
- **Eurostat county data:** Official 2023 county population (`demo_r_pjanaggr3`), GDP per capita (`nama_10r_3gdp`), age 65+ share (`demo_r_pjanind3`), and urban/rural typology (`URBN_TYPE`).
- **6-axis spatial voting:** Temperature-scaled Softmax over weighted Euclidean distance (`Econ`, `Social`, `EU`, `Defense`, `Env`, `Migration`) and 6 voter blocs per country.
- **National allocation rules & live "What-If" swapper:** Pluggable `SeatAllocator` supporting `Sainte-Laguë`, `D'Hondt`, `Hare Quota`, ranked-ballot `STV` (Ireland & Malta via `PolicyVec` preference transfers), and `FPTP` (Winner-Take-All), with a live **System** switcher in the web UI.
- **Live election-night SSE stream:** Concurrent per-country goroutines with staggered CET poll closing (`18:00 -> 23:00`), hierarchical correlated swings, deterministic multi-wave partial county reporting, and a minimal-winning Coalition Finder.
- **Embedded web atlas:** Plain HTML/CSS/JS modules, SVG hemicycle, Lambert Azimuthal Equal-Area Europe map, 6 data overlays, and Light/Dark themes.

> **Note:** `scenarios/eu27.json` compares seat swings (`+/-`) against official June 2024 EP results (`prev_seats`, 720 seats), falling back to a synthetic baseline for scenarios without `prev_seats`. Built as a hands-on Go learning project: core Go domain logic and algorithms are written with AI coaching/review, while frontend JS/CSS, dataset scripts, and boilerplate scaffolding are AI-assisted.

## Quick Start

Requires **Go 1.24+**.

```bash
# Run the EU-27 720-seat web dashboard (http://localhost:8080)
go run .

# Run the 3-country starter scenario (DE, FR, IT)
go run . -scenario scenarios/eu_starter.json

# Run live election night in the terminal instead of starting HTTP server
go run . -cli

# Custom listen address and concurrent SSE stream cap (default 256)
go run . -addr 127.0.0.1:9090 -max-streams 128
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
  seats.go               SeatAllocator interface, D'Hondt, Sainte-Laguë, Hare, FPTP, RollupGroupSeats
  stv.go                 Droop-quota Ranked-Ballot STV with PolicyVec preference transfers
  state.go               Incremental ElectionState vote/seat/progress tracker
  seed.go                Built-in test fixture parties and regions
  nuts.go                Embedded Eurostat NUTS-3 metadata & socioeconomic tilts
  election.go            Hierarchical swings, PrepareElection, deterministic waves, SSE streaming
  coalition.go           Minimal-winning Coalition Finder ranked by PolicyVec spread
  scenario.go            JSON scenario loader, Validate(), and EU seat aggregation
web/
  server.go              net/http server, //go:embed static assets, and SSE /api/stream
  static/                Zero-build HTML, Light/Dark CSS, ES modules, and Eurostat SVG GeoJSON
```

## Data Sources & Attribution

- **Boundaries:** Eurostat GISCO NUTS 2021 (`10M` / `20M`), © EuroGeographics.
- **Statistics:** Eurostat (`demo_r_pjanaggr3`, `nama_10r_3gdp`, `demo_r_pjanind3`).

## License

Licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)** — see [LICENSE](LICENSE) and [NOTICE](NOTICE) for full terms, attribution requirements, and trademark guidelines.
