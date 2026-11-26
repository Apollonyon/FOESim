# FOES

Interactive election-night simulator, scenario editor, and Monte Carlo sandbox written in standard-library Go with an HTML5 Canvas map.

![EU27 election night](screenshots/eu27.png)

![USA 2024 county map](screenshots/usa2024.png)

### Usage

Requires Go 1.24+. There are no third-party dependencies.

```bash
go run .                              # web UI on http://localhost:8080 (eu27)
go run . -scenario scenarios/usa2024.json
go run . -cli                         # terminal mode
```

Run `go run . -h` to see all CLI flags. A `Dockerfile` is also included if you want to run it in a container.

### Scenarios

- `eu27`: European Parliament across all 27 member states (720 seats, 154 parties, 1,152 NUTS-3 regions), calibrated to 2024 EP results and CHES-Europe.
- `usa2024`: 2024 U.S. Presidential Election (538 EVs, 3,141 counties), including Maine and Nebraska congressional district splits.
- `uk2024`: 2024 UK General Election across all 650 Westminster constituencies (12 regions/nations, GB & Northern Ireland parties, 326-to-win bar), calibrated to certified UK Parliament results.
- `eu_starter`: Small synthetic 3-country scenario (DE, FR, IT) for quick local testing.

### How it Works

### How it Works

FOES simulates election night county by county, streaming live results to the browser as polls close across time zones. The `eu27` and `usa2024` presets are tuned to roughly match the real 2024 results by default, so you can tweak party swings, turnout, or randomness from a realistic baseline.

You can also run Monte Carlo mode (100 to 500 randomized elections) to see win probabilities and close races, test different seat-allocation systems (Sainte-Laguë, D'Hondt, Hare-Niemeyer, FPTP, and an STV approximation) on the same votes, or press `E` to edit a scenario in the browser and share it via URL.

### Tests

```bash
go vet ./...
go test -race -count=1 ./...
```

### Data & License

Built with Eurostat GISCO NUTS 2021 (© EuroGeographics), 2024 CHES-Europe, European Parliament Open Data, U.S. Census TIGER/Line 2023, 2024 U.S. county presidential results, ONS Open Geography Portal (OGL v3.0), and UK Parliament Open Data.

Licensed under [AGPL-3.0](LICENSE) (see [NOTICE](NOTICE) for Section 7 additional terms).