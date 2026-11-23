package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"parliamentsim/sim"
	"parliamentsim/web"
)

func main() {
	scenarioPath := flag.String("scenario", "scenarios/eu_starter.json", "path to JSON scenario file")
	addr := flag.String("addr", ":8080", "HTTP listen address")
	cliMode := flag.Bool("cli", false, "run live election night in the terminal instead of starting the web server")
	flag.Parse()

	scenario, err := sim.LoadScenario(*scenarioPath)
	if err != nil {
		log.Fatal(err)
	}

	if *cliMode {
		runCLI(scenario)
		return
	}

	srv := web.NewServer(scenario)
	log.Printf("ParliamentSim dashboard listening on http://localhost%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}

func runCLI(scenario sim.Scenario) {
	parties := scenario.Parties
	regions := scenario.Regions

	regionByID := make(map[string]sim.Region, len(regions))
	tallies := make(map[string]map[string]int, len(regions))
	regionSeats := make(map[string]map[string]int, len(regions))
	for _, r := range regions {
		regionByID[r.ID] = r
		tallies[r.ID] = make(map[string]int)
	}

	cfg := sim.ElectionConfig{
		Temperature: 0.85,
		Swing:       0.15,
		Seed:        42,
		BaseDelay:   120 * time.Millisecond,
	}

	fmt.Printf("=== Live Election Night: %s ===\n", scenario.Name)
	reports := sim.StreamElection(context.Background(), regions, parties, cfg)

	for rep := range reports {
		for partyID, v := range rep.Votes {
			tallies[rep.RegionID][partyID] += v
		}

		r := regionByID[rep.RegionID]
		regionSeats[rep.RegionID] = sim.AllocateSeats(tallies[rep.RegionID], r.Seats, r.Method, r.Threshold)

		euProjected := sim.SumSeats(regionSeats)
		pct := float64(rep.Precinct) / float64(rep.TotalPrecincts) * 100
		fmt.Printf("[%s %2d/%2d (%3.0f%%)]", strings.ToUpper(r.ID), rep.Precinct, rep.TotalPrecincts, pct)
		for _, p := range parties {
			if s := euProjected[p.ID]; s > 0 {
				fmt.Printf("  %s:%d", p.ShortName, s)
			}
		}
		fmt.Println()
	}

	fmt.Println("\n=== Final Results by Region ===")
	for _, r := range regions {
		fmt.Printf("\n--- %s (%s) ---\n", r.Name, r.Method)
		for _, p := range parties {
			fmt.Printf("  %-8s %3d seats (%d votes)\n", p.ShortName, regionSeats[r.ID][p.ID], tallies[r.ID][p.ID])
		}
	}

	totalSeats := sim.SumSeats(regionSeats)
	fmt.Println("\n=== European Parliament Final Total ===")
	for _, p := range parties {
		fmt.Printf("  %-8s %3d seats\n", p.ShortName, totalSeats[p.ID])
	}
}
