# FOES — Free Open Election Simulator

**FOES** is a zero-dependency Go election-night simulator, scenario sandbox, and Monte Carlo forecaster paired with an interactive HTML5 Canvas (`Path2D`) county atlas streamed over Server-Sent Events (SSE) and embedded into a single static binary via `//go:embed`. *(The Go module and binary name remain `parliamentsim`.)*

---

## Included Scenarios

| ID | Election | Scale | Geography & Method |
| :--- | :--- | :--- | :--- |
| `eu27` *(default)* | European Parliament (27 Member States) | 720 seats, 154 parties → 9 political groups | 1,152 NUTS-3 counties, per-country PR (`Sainte-Laguë`, `D'Hondt`, `Hare`, `STV`), real 2024 baselines |
| `usa2024` | 2024 U.S. Presidential Election | 538 EVs, 50 states + DC | 3,141 counties (`us-atlas` Albers USA), callout badges (`DC`, Small East Coast states, `ME-1/2`, `NE-1/2/3`), 270-to-win bar |
| `eu_starter` | 3-Country Starter (DE, FR, IT) | 228 seats across 3 states | Synthetic precincts, quick local testing |

---

## Features

- **Live Election Night Stream:** Independent per-connection SSE broadcasts (`/api/stream`) with Start / Pause / Resume / Replay / Reset playback, `1× / 2× / 5× / Instant` speeds, staggered poll-closing windows, Seed & Temperature controls, and live Coalition Finder.
- **6 Data Overlays & Light/Dark Atlas:** Offscreen bitmap caching + `32×32` spatial grid hit-testing for `60fps` zoom/pan across 3,141 counties, switching cleanly across **Winner**, **Party/Group %**, **Turnout**, **Axis 2** (Pro-EU / Political spectrum), **Economic**, and **Migration** overlays.
- **In-Browser Scenario & Modifier Editor (`E`):** Visually adjust party appeal, colors, policy coordinates, region seats, thresholds, counting rules, poll close hours, district seats, demographic tilts, voter group ideals/weights, turnout scaling, and stream modifiers (`POST /api/runs`).
- **Stateless Link Sharing (`#share=`):** Share edited scenarios and modifiers via permanent compressed diff URLs (using native browser `CompressionStream`) with zero database required, plus full JSON download and import.
- **Monte Carlo Forecast Mode:** Bounded worker pool simulating `100–500` correlated elections (`GET /api/forecast`) with a live **Swing** slider, win-probability map, `P10–P90` seat distribution histograms, win-odds bar, and a clickable **Closest Races** panel.
- **Electoral System Lab:** Compare `Sainte-Laguë`, `D'Hondt`, `Hare-Niemeyer`, `FPTP`, Ranked-Ballot `STV` (Droop quota with spatial preference transfers), and parallel two-tier district allocation (`district_seats`).

---

## Quick Start

Requires **Go 1.24+** (standard library only — zero third-party Go dependencies).

```bash
# Start web dashboard loading all scenarios in ./scenarios (http://localhost:8080)
go run .

# Start with a specific default scenario selected
go run . -scenario scenarios/usa2024.json

# Run live election night in the terminal instead of starting the HTTP server
go run . -cli
```

### CLI Flags

| Flag | Default | Description |
| :--- | :--- | :--- |
| `-scenario` | `scenarios/eu27.json` | Path to default JSON scenario file |
| `-scenarios-dir` | `scenarios` | Directory of `*.json` scenarios for the HUD switcher (empty disables directory scan) |
| `-addr` | `:8080` | HTTP listen address |
| `-cli` | `false` | Stream live election night in the terminal instead of running the web server |
| `-max-streams` | `256` | Global concurrent SSE stream limit (`0` = unlimited) |
| `-max-streams-per-ip` | `16` | Per-IP concurrent SSE/forecast limit (`0` = unlimited) |
| `-trust-proxy` | `false` | Trust `CF-Connecting-IP`, `Fly-Client-IP`, and `X-Forwarded-For` behind a reverse proxy |
| `-shutdown-timeout` | `30s` | Graceful drain timeout on `SIGINT`/`SIGTERM` so active streams finish cleanly |

---

## Self-Hosting & Docker

Static web assets (`web/static/`) are compiled directly into the binary via `//go:embed`, while `scenarios/*.json` are loaded from disk at startup.

```bash
docker build -t foes .
docker run -p 8080:8080 foes
```

The included [Dockerfile](Dockerfile) runs a non-root Alpine image exposing `:8080` with `-trust-proxy` enabled and a health check endpoint at `GET /healthz`. For zero-downtime rolling deploys behind Caddy/Nginx/Fly.io, point health checks at `/healthz` and allow `-shutdown-timeout` (default `30s`) for in-flight SSE broadcasts to finish.

---

## HTTP API Overview

| Endpoint | Description |
| :--- | :--- |
| `GET /api/scenarios` | Ordered scenario catalog + `default_id` for the UI switcher |
| `GET /api/scenario[?scenario=id\|run=id]` | Scenario definition and `ScenarioMeta` defaults |
| `GET /api/stream[?scenario=id\|run=id&duration_ms=...&seed=...&temp=...&swing=...&waves=...&method=...]` | Independent SSE stream emitting `exit_poll`, `precinct`, and `complete` events |
| `GET /api/forecast[?scenario=id\|run=id&runs=200&seed=...&temp=...&swing=...&method=...]` | Monte Carlo percentiles (`min/p10/p50/p90/max`), `bins`, majority odds, and regional win probabilities |
| `POST /api/runs` | Validate and store an ephemeral custom scenario (max `2 MiB`, 15m TTL, bounded DoS limits, per-IP rate-limited) |
| `GET /healthz` | Liveness check (`200 ok`) |

---

## Adding or Contributing Scenarios

1. Use the in-browser **Scenario Editor** (`E`) and click **JSON → Download JSON**, or author a JSON file matching `sim.Scenario`.
2. Place the `.json` file inside `scenarios/` (or submit a pull request). Note that `meta.regions_url` and `meta.units_url` must point to `/geo/*.json` assets embedded under `web/static/geo/`.
3. Verify your scenario passes structural & numeric bounds checks (`Scenario.Validate()`):
   ```bash
   go vet ./... && go test -race -count=1 ./...
   ```

---

## Development & Tests

```bash
# Run unit, HTTP, registry, forecast, and race-detector tests
go test -race -count=1 -v ./...

# Static checks
go vet ./...

# Benchmarks
go test -bench=. ./sim
```

### Project Layout

```text
main.go                  CLI flags, terminal streamer, graceful shutdown HTTP server
Dockerfile               Multi-stage non-root Alpine runtime image
scenarios/               Disk-loaded JSON scenarios (eu27.json, usa2024.json, eu_starter.json)
sim/
  policy.go              6-axis PolicyVec and weighted Euclidean distance
  party.go               Party and EUGroup models
  region.go              Region, VoterGroup, GeoPack selection, demographic tilts
  vote.go                Softmax VoteShares, SimulateRegion, PartyBonus
  seats.go               SeatAllocator interface, Sainte-Laguë, D'Hondt, Hare, FPTP, two-tier districts
  stv.go                 Droop-quota Ranked-Ballot STV with preference transfers
  state.go               Incremental ElectionState vote/seat/district/progress tracker
  seed.go                Built-in test fixture parties and regions
  geopack.go             Thread-safe lazy GeoPack registry (eu_nuts3, us_counties)
  nuts.go                Embedded Eurostat NUTS-3 units & socioeconomic tilts
  us_geopack.go          Embedded U.S. 3,141-county GeoPack registration
  election.go            Hierarchical swings, PrepareElection, deterministic waves, SSE streaming
  forecast.go            Bounded-worker Monte Carlo aggregator & seat histogram binning
  coalition.go           Minimal-winning Coalition Finder ranked by policy spread
  registry.go            Multi-scenario loader & ordered catalog registry
  scenario.go            JSON loader, ScenarioMeta defaults, Validate(), seat aggregation
web/
  server.go              net/http server, //go:embed static assets, SSE/forecast/healthz handlers, CSP/gzip
  runs.go                Bounded TTL custom runStore, DoS validation bounds, and per-IP rate limiter
  static/                Zero-build HTML/CSS/ES modules, Scenario Editor, #share diff codec, Changelog UI, GeoJSON
```

---

## Data Sources & Attribution

- **EU Boundaries & Stats:** Eurostat GISCO NUTS 2021 (`10M` / `20M`), © EuroGeographics; Eurostat (`demo_r_pjanaggr3`, `nama_10r_3gdp`, `demo_r_pjanind3`).
- **Party Positions & EP Baselines:** 2024 Chapel Hill Expert Survey (CHES-Europe) and European Parliament Open Data (`results.elections.europa.eu`).
- **USA Boundaries & 2024 Results:** U.S. Census Bureau TIGER/Line 2023 Cartographic Boundaries (`cb_2023_us_state_20m`, `cb_2023_us_county_20m`, `cb_2023_us_cd118_20m`) and certified 2024 county presidential totals.

---

## License

Licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)** — see [LICENSE](LICENSE) and [NOTICE](NOTICE) for full terms, attribution requirements, and Additional Terms under Section 7.
