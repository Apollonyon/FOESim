package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"parliamentsim/sim"
	"parliamentsim/web"
)

func main() {
	scenarioPath := flag.String("scenario", "scenarios/eu27.json", "path to JSON scenario file")
	addr := flag.String("addr", ":8080", "HTTP listen address")
	cliMode := flag.Bool("cli", false, "run live election night in the terminal instead of starting the web server")
	maxStreams := flag.Int("max-streams", 256, "maximum concurrent SSE election streams (0 = unlimited)")
	flag.Parse()

	scenario, err := sim.LoadScenario(*scenarioPath)
	if err != nil {
		log.Fatal(err)
	}

	if *cliMode {
		runCLI(scenario)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := web.NewServer(scenario)
	srv.SetMaxStreams(*maxStreams)
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("ParliamentSim dashboard listening on http://%s", displayAddr(ln.Addr().String()))
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			log.Fatal(err)
		}
	case <-ctx.Done():
		log.Println("Shutting down server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown error: %v", err)
		}
	}
}

func runCLI(scenario sim.Scenario) {
	parties := scenario.Parties
	regions := scenario.Regions
	euGroups := scenario.EffectiveGroups()

	state := sim.NewElectionState(scenario)

	cfg := sim.ElectionConfig{
		Temperature:    0.45,
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
		method := r.Method
		if method == "" {
			method = sim.MethodSainteLague
		}
		fmt.Printf("\n--- %s (%s) ---\n", r.Name, method)
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

func displayAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "::" || host == "0.0.0.0" {
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
}
