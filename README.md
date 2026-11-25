# FOES — Free Open Election Simulator

Interactive multi-country election-night simulator, scenario editor, and Monte Carlo sandbox written in zero-dependency Go (`//go:embed` + Server-Sent Events) with an HTML5 Canvas county atlas. *(The Go module and binary name remain `parliamentsim`.)*

---

## Scenarios

| ID | Election | Scale | Calibrated to | Notes |
| :--- | :--- | :--- | :--- | :--- |
| `eu27` *(default)* | European Parliament (27 Member States) | 720 seats, 154 parties → 9 groups, 1,152 NUTS-3 counties | EP 2024 result + CHES-Europe (in-sample) | Per-country PR (`Sainte-Laguë`, `D'Hondt`, `Hare`, `STV approx`) |
| `usa2024` | 2024 U.S. Presidential Election | 538 EVs, 50 states + DC, 3,141 counties | Certified 2024 county totals (in-sample) | `ME-1/2` & `NE-1/2/3` splits, callout badges, 270-to-win bar |
| `eu_starter` | 3-Country Starter (DE, FR, IT) | 228 seats across 3 states | Synthetic fixture | Fast local testing; omits historical `prev_seats` deltas |

---

## Features

- **Live Election-Night Broadcast:** Independent per-connection SSE streams (`/api/stream`) with Start / Pause / Resume / Replay / Reset, `1× / 2× / 5× / Instant` speeds, staggered poll closing hours, directional party shift, seed/temperature controls, and Coalition Finder.
- **6 Canvas Map Overlays & Light/Dark Atlas:** Offscreen bitmap caching and spatial-grid hit-testing across 3,141 counties with **Winner**, **Party/Group %**, **Turnout**, **Axis 2** (Pro-EU / Political spectrum), **Economic**, and **Migration** layers.
- **In-Browser Scenario Editor (`E`):** Edit party appeal, colors, policy coordinates, region seats, thresholds, counting methods, poll close hours, district seats, demographic tilts, voter group ideals/weights, and simulation modifiers (`POST /api/runs`).
- **Stateless `#share=` Links:** Share edited scenarios and modifiers via compressed URL diffs (`CompressionStream`), plus JSON export/import.
- **Monte Carlo Simulation Mode:** Runs `100–500` correlated election draws (`GET /api/forecast`) with a **Uncertainty** volatility slider, directional **Shift** slider, win-probability map, `P10–P90` seat histograms, and **Closest Races**.
- **Electoral System Lab:** Compare `Sainte-Laguë`, `D'Hondt`, `Hare-Niemeyer`, `FPTP`, and party-level `STV (approx)`.

---

## Model & Limitations

FOES is an interactive **scenario simulator**, not a polling-based election forecaster:

- **In-sample calibration:** `eu27` and `usa2024` reproduce 2024 results closely at `Uncertainty = 0` because their party appeals and regional tilts were calibrated to 2024 data in-sample. When `prev_seats` matches the 2024 target, live deltas measure divergence from that 2024 baseline.
- **Uncertainty vs. directional Shift:** **Uncertainty** scales zero-mean hierarchical shocks (shared cross-region policy + national per-party shock plus regional/local noise), while **Shift** (`shift=id:val`) applies a directional national swing. Under winner-take-all (e.g. USA 2024), adding symmetric uncertainty pulls the average electoral-vote total back toward 270 even with zero bias because narrow swing-state wins have more EVs to lose than safe states can gain; the Forecast panel shows the deterministic `Base` alongside `P10 / Med / P90`.
- **Party-level STV approximation:** `STV (approx)` operates on party vote totals with a Droop quota rather than individual ranked ballots — winning parties keep surplus minus one quota and eliminated parties transfer 100% of votes to the nearest remaining party in policy space with no vote exhaustion.
- **Method override drops districts:** Switching the global **System** override sets `district_seats = 0`, removing two-tier splits (such as `ME-1/2` and `NE-1/2/3`) while active.
- **Default seed (`42`):** Live streams use seed `42` by default for reproducible demos; use `↻` next to Seed to randomize.

---

## Quick Start

Requires **Go 1.24+** (standard library only — zero third-party dependencies).

```bash
go run .                              # web UI on http://localhost:8080 (default: eu27)
go run . -scenario scenarios/usa2024.json
go run . -cli                         # terminal broadcast mode
```

<details>
<summary>CLI Flags & HTTP API Reference</summary>

### CLI Flags

| Flag | Default | Description |
| :--- | :--- | :--- |
| `-scenario` | `scenarios/eu27.json` | Path to default JSON scenario file |
| `-scenarios-dir` | `scenarios` | Directory of `*.json` scenarios for the HUD switcher (empty disables scan) |
| `-addr` | `:8080` | HTTP listen address |
| `-cli` | `false` | Stream live election night in the terminal |
| `-max-streams` | `256` | Global concurrent SSE stream limit (`0` = unlimited) |
| `-max-streams-per-ip` | `16` | Per-IP concurrent SSE/forecast limit (`0` = unlimited) |
| `-trust-proxy` | `false` | Trust rightmost `X-Forwarded-For` hop behind Caddy/Nginx |
| `-proxy-header` | `""` | Explicit trusted IP header mode: `xff`, `cf`, or `fly` (overrides `-trust-proxy`) |
| `-shutdown-timeout` | `30s` | Graceful drain timeout on `SIGINT`/`SIGTERM` |

### HTTP Endpoints

| Endpoint | Description |
| :--- | :--- |
| `GET /api/scenarios` | Scenario catalog + `default_id` |
| `GET /api/scenario[?scenario=id\|run=id]` | Scenario definition and resolved metadata |
| `GET /api/stream[?scenario=id\|run=id&duration_ms=...&seed=...&temp=...&uncertainty=...&shift=...&waves=...&method=...]` | Independent SSE stream (`exit_poll`, `precinct`, `complete`; accepts legacy `swing=` alias) |
| `GET /api/forecast[?scenario=id\|run=id&runs=200&seed=...&temp=...&uncertainty=...&shift=...&method=...]` | Monte Carlo `baseline`, `min/p10/p50/p90/max`, `bins`, and regional odds |
| `POST /api/runs` | Validate and store an ephemeral custom scenario (max `2 MiB`, 15m TTL, per-IP rate-limited) |
| `GET /healthz` | Liveness check (`200 ok`) |

</details>

---

## Self-Hosting & Docker

Static assets (`web/static/`) are embedded into the binary via `//go:embed`; `scenarios/*.json` are loaded from disk.

```bash
docker build -t foes .
docker run -d --name foes -p 127.0.0.1:8080:8080 foes
```

The [Dockerfile](Dockerfile) runs a non-root Alpine container with proxy header trust **disabled by default** so directly exposed instances cannot be spoofed. When running behind a reverse proxy (bind to `127.0.0.1` only), pass `-trust-proxy` (for Caddy/Nginx rightmost `X-Forwarded-For`) or `-proxy-header cf` / `-proxy-header fly`. Health checks can poll `GET /healthz`.

---

## Development & Tests

```bash
gofmt -l .
go vet ./...
go test -race -count=1 ./...
```

---

## Data Sources & Attribution

- **EU Boundaries & Stats:** Eurostat GISCO NUTS 2021 (`10M` / `20M`), © EuroGeographics; Eurostat (`demo_r_pjanaggr3`, `nama_10r_3gdp`, `demo_r_pjanind3`).
- **Party Positions & EP Baselines:** 2024 Chapel Hill Expert Survey (CHES-Europe) and European Parliament Open Data (`results.elections.europa.eu`).
- **USA Boundaries & 2024 Results:** U.S. Census Bureau TIGER/Line 2023 Cartographic Boundaries (`cb_2023_us_state_20m`, `cb_2023_us_county_20m`, `cb_2023_us_cd118_20m`) and certified 2024 county presidential totals.

---

## License

Licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)** — see [LICENSE](LICENSE) and [NOTICE](NOTICE) for full terms and Additional Terms under Section 7. In plain terms, if you modify FOES and run it on a server for users to interact with over the network, you must make the corresponding source code of your modified version available under AGPL-3.0.
