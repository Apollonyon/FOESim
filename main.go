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
	scenarioPath := flag.String("scenario", "scenarios/eu27.json", "path to JSON scenario file")
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
	euGroups := scenario.EffectiveGroups()

	state := sim.NewElectionState(scenario)

	cfg := sim.ElectionConfig{
		Temperature:    0.85,
		Swing:          0.15,
		Seed:           42,
		TargetDuration: 4 * time.Second,
	}

	fmt.Printf("=== Live Election Night: %s ===\n", scenario.Name)
	reports := sim.StreamElection(context.Background(), regions, parties, cfg)

	for rep := range reports {
		state.Apply(rep)
		euProjected := state.EUSeats()
		pct := float64(rep.Precinct) / float64(rep.TotalPrecincts) * 100
		fmt.Printf("[%s %2d/%2d (%3.0f%%)]", strings.ToUpper(rep.RegionID), rep.Precinct, rep.TotalPrecincts, pct)
		for _, g := range euGroups {
			if s := euProjected[g.ID]; s > 0 {
				fmt.Printf("  %s:%d", g.ShortName, s)
			}
		}
		fmt.Println()
	}

	fmt.Println("\n=== Final Results by Region ===")
	for _, r := range regions {
		fmt.Printf("\n--- %s (%s) ---\n", r.Name, r.Method)
		seats := state.RegionSeats(r.ID)
		votes := state.RegionVotes(r.ID)
		for _, p := range r.EffectiveParties(parties) {
			fmt.Printf("  %-8s %3d seats (%d votes)\n", p.ShortName, seats[p.ID], votes[p.ID])
		}
	}

	totalSeats := state.EUSeats()
	fmt.Println("\n=== European Parliament Final Total ===")
	for _, g := range euGroups {
		fmt.Printf("  %-8s %3d seats\n", g.ShortName, totalSeats[g.ID])
	}
}
