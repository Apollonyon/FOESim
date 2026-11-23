package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"parliamentsim/sim"
)

//go:embed static
var staticFiles embed.FS

// Server serves the ParliamentSim web dashboard and JSON/SSE API endpoints.
type Server struct {
	scenario sim.Scenario
}

// NewServer creates a Server configured with the given election scenario.
func NewServer(scenario sim.Scenario) *Server {
	return &Server{scenario: scenario}
}

// Handler returns the HTTP router for the dashboard and API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/scenario", s.handleScenario)
	mux.HandleFunc("GET /api/stream", s.handleStream)

	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServerFS(sub))

	return mux
}

func (s *Server) handleScenario(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.scenario)
}

type exitPollPayload struct {
	RegionSeats     map[string]map[string]int `json:"region_seats"`
	EUSeats         map[string]int            `json:"eu_seats"`
	PrevEUSeats     map[string]int            `json:"prev_eu_seats"`
	TotalPrecincts  int                       `json:"total_precincts"`
}

type precinctPayload struct {
	Report            sim.PrecinctReport        `json:"report"`
	RegionVotes       map[string]int            `json:"region_votes"`
	RegionSeats       map[string]int            `json:"region_seats"`
	EUSeats           map[string]int            `json:"eu_seats"`
	ReportedPrecincts int                       `json:"reported_precincts"`
	TotalPrecincts    int                       `json:"total_precincts"`
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	cfg := parseElectionConfig(r)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	regionByID := make(map[string]sim.Region, len(s.scenario.Regions))
	tallies := make(map[string]map[string]int, len(s.scenario.Regions))
	regionSeats := make(map[string]map[string]int, len(s.scenario.Regions))
	prevRegionSeats := make(map[string]map[string]int, len(s.scenario.Regions))
	totalPrecincts := 0

	for ri, reg := range s.scenario.Regions {
		regionByID[reg.ID] = reg
		tallies[reg.ID] = make(map[string]int)
		n := reg.Precincts
		if n <= 0 {
			n = 1
		}
		totalPrecincts += n

		// Generate a noisy 0%-reported exit poll and a baseline previous-election result.
		regionSeats[reg.ID] = noisyRegionSeats(reg, s.scenario.Parties, cfg.Temperature, 0.25, (cfg.Seed^0x51a7)+int64(ri))
		prevRegionSeats[reg.ID] = noisyRegionSeats(reg, s.scenario.Parties, cfg.Temperature*1.15, 0.20, 1001+int64(ri))
	}

	if err := writeSSE(w, flusher, "exit_poll", exitPollPayload{
		RegionSeats:    regionSeats,
		EUSeats:        sim.SumSeats(regionSeats),
		PrevEUSeats:    sim.SumSeats(prevRegionSeats),
		TotalPrecincts: totalPrecincts,
	}); err != nil {
		return
	}

	reports := sim.StreamElection(r.Context(), s.scenario.Regions, s.scenario.Parties, cfg)
	reported := 0

	for rep := range reports {
		reported++
		for partyID, v := range rep.Votes {
			tallies[rep.RegionID][partyID] += v
		}

		reg := regionByID[rep.RegionID]
		regionSeats[rep.RegionID] = sim.AllocateSeats(tallies[rep.RegionID], reg.Seats, reg.Method, reg.Threshold)

		payload := precinctPayload{
			Report:            rep,
			RegionVotes:       tallies[rep.RegionID],
			RegionSeats:       regionSeats[rep.RegionID],
			EUSeats:           sim.SumSeats(regionSeats),
			ReportedPrecincts: reported,
			TotalPrecincts:    totalPrecincts,
		}
		if err := writeSSE(w, flusher, "precinct", payload); err != nil {
			return
		}
	}

	// Only emit complete if the client did not cancel early.
	if r.Context().Err() == nil {
		_ = writeSSE(w, flusher, "complete", map[string]int{
			"reported_precincts": reported,
			"total_precincts":    totalPrecincts,
		})
	}
}

func parseElectionConfig(r *http.Request) sim.ElectionConfig {
	q := r.URL.Query()
	cfg := sim.ElectionConfig{
		Temperature: 0.85,
		Swing:       0.15,
		Seed:        42,
		BaseDelay:   180 * time.Millisecond,
	}
	if v, err := strconv.ParseFloat(q.Get("temp"), 64); err == nil && v > 0 {
		cfg.Temperature = v
	}
	if v, err := strconv.ParseFloat(q.Get("swing"), 64); err == nil && v >= 0 {
		cfg.Swing = v
	}
	if v, err := strconv.ParseInt(q.Get("seed"), 10, 64); err == nil {
		cfg.Seed = v
	}
	if v, err := strconv.Atoi(q.Get("delay_ms")); err == nil && v >= 0 {
		cfg.BaseDelay = time.Duration(v) * time.Millisecond
	}
	return cfg
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func noisyRegionSeats(reg sim.Region, parties []sim.Party, temp, swing float64, seed int64) map[string]int {
	sample := reg
	sample.Precincts = 1
	reports := sim.SimulatePrecincts(sample, parties, temp, swing, seed)
	if len(reports) == 0 {
		return map[string]int{}
	}
	return sim.AllocateSeats(reports[0].Votes, reg.Seats, reg.Method, reg.Threshold)
}
