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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Apollonyon/FOESim/sim"
	"github.com/Apollonyon/FOESim/web"
)

func main() {
	scenarioPath := flag.String("scenario", "scenarios/eu27.json", "path to default JSON scenario file")
	scenariosDir := flag.String("scenarios-dir", "scenarios", "directory of JSON scenarios for the web switcher (empty disables directory scan)")
	packsDir := flag.String("packs-dir", "packs", "directory of external GeoPacks (<dir>/<pack>/units.json + map JSONs; empty disables external packs)")
	addr := flag.String("addr", ":8080", "HTTP listen address")
	cliMode := flag.Bool("cli", false, "run live election night in the terminal instead of starting the web server")
	maxStreams := flag.Int("max-streams", 256, "maximum concurrent SSE election streams (0 = unlimited)")
	maxStreamsPerIP := flag.Int("max-streams-per-ip", 16, "maximum concurrent SSE streams per client IP (0 = unlimited; set 0 behind a reverse proxy)")
	trustProxy := flag.Bool("trust-proxy", false, "trust rightmost X-Forwarded-For hop for client IP limits behind Caddy/Nginx")
	proxyHeader := flag.String("proxy-header", "", "trusted client IP proxy header mode: xff, cf, or fly (overrides -trust-proxy)")
	shutdownTimeout := flag.Duration("shutdown-timeout", 30*time.Second, "graceful drain timeout on SIGINT/SIGTERM")
	flag.Parse()

	packsDirExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "packs-dir" {
			packsDirExplicit = true
		}
	})

	if err := loadExternalPacks(*packsDir, packsDirExplicit); err != nil {
		log.Fatal(err)
	}

	scenario, err := sim.LoadScenario(*scenarioPath)
	if err != nil {
		log.Fatal(err)
	}

	if *cliMode {
		runCLI(scenario)
		return
	}

	reg, err := buildScenarioRegistry(*scenariosDir, *scenarioPath, scenario)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := web.NewServerWithRegistry(reg)
	srv.SetPacksDir(*packsDir)
	srv.SetMaxStreams(*maxStreams)
	srv.SetMaxStreamsPerIP(*maxStreamsPerIP)
	if *proxyHeader != "" {
		srv.SetProxyHeader(*proxyHeader)
	} else {
		srv.SetTrustProxy(*trustProxy)
	}
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("FOES dashboard listening on http://%s (%d scenarios, default=%s)",
			displayAddr(ln.Addr().String()), len(reg.List()), reg.DefaultID())
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
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

	meta := scenario.EffectiveMeta()
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
			fmt.Printf("  %-8s %3d %s (%d votes)\n", p.ShortName, seats[p.ID], meta.SeatLabel, votes[p.ID])
		}
	}

	totalSeats := state.EUSeats()
	fmt.Printf("\n=== %s Final Total ===\n", meta.Title)
	for _, g := range euGroups {
		fmt.Printf("  %-8s %3d %s\n", g.ShortName, totalSeats[g.ID], meta.SeatLabel)
	}
}

func loadExternalPacks(dir string, explicit bool) error {
	if dir == "" {
		return nil
	}
	loaded, err := sim.LoadGeoPacksDir(dir)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(loaded) > 0 {
		log.Printf("Loaded %d external GeoPack(s) from %s: %s", len(loaded), dir, strings.Join(loaded, ", "))
	}
	return nil
}

func buildScenarioRegistry(dir, primaryPath string, primary sim.Scenario) (*sim.ScenarioRegistry, error) {
	primaryID := strings.TrimSuffix(filepath.Base(primaryPath), ".json")
	if primaryID == "" {
		primaryID = "default"
	}

	entries := []sim.ScenarioEntry{{ID: primaryID, Scenario: primary}}
	if dir != "" {
		dirEntries, err := os.ReadDir(dir)
		if err == nil {
			for _, de := range dirEntries {
				if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
					continue
				}
				id := strings.TrimSuffix(de.Name(), ".json")
				if id == "" || id == primaryID {
					continue
				}
				sc, err := sim.LoadScenario(filepath.Join(dir, de.Name()))
				if err != nil {
					log.Printf("skipping scenario %s: %v", de.Name(), err)
					continue
				}
				entries = append(entries, sim.ScenarioEntry{ID: id, Scenario: sc})
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return sim.NewScenarioRegistry(primaryID, entries)
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
