package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"net/http"
	"strconv"
	"time"

	"parliamentsim/sim"
)

const (
	defaultMaxStreams = 256
	maxTargetDuration = 5 * time.Minute
	maxBaseDelay      = 2 * time.Second
)

//go:embed static
var staticFiles embed.FS

// Server serves the ParliamentSim web dashboard and JSON/SSE API endpoints.
type Server struct {
	scenario  sim.Scenario
	streamSem chan struct{}
}

// NewServer creates a Server configured with the given election scenario.
func NewServer(scenario sim.Scenario) *Server {
	return &Server{
		scenario:  scenario,
		streamSem: make(chan struct{}, defaultMaxStreams),
	}
}

// SetMaxStreams overrides the maximum number of concurrent SSE streams.
// Values <= 0 disable the cap.
func (s *Server) SetMaxStreams(n int) {
	if n <= 0 {
		s.streamSem = nil
		return
	}
	s.streamSem = make(chan struct{}, n)
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
	RegionSeats    map[string]map[string]int `json:"region_seats"`
	EUSeats        map[string]int            `json:"eu_seats"`
	PrevEUSeats    map[string]int            `json:"prev_eu_seats"`
	Coalitions     []sim.Coalition           `json:"coalitions"`
	TotalPrecincts int                       `json:"total_precincts"`
}

type precinctPayload struct {
	Report            sim.PrecinctReport `json:"report"`
	RegionVotes       map[string]int     `json:"region_votes"`
	RegionSeats       map[string]int     `json:"region_seats"`
	EUSeats           map[string]int     `json:"eu_seats"`
	Coalitions        []sim.Coalition    `json:"coalitions"`
	ReportedPrecincts int                `json:"reported_precincts"`
	TotalPrecincts    int                `json:"total_precincts"`
	CountedPct        float64            `json:"counted_pct"`
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	if s.streamSem != nil {
		select {
		case s.streamSem <- struct{}{}:
			defer func() { <-s.streamSem }()
		default:
			w.Header().Set("Retry-After", "5")
			http.Error(w, "too many concurrent streams", http.StatusServiceUnavailable)
			return
		}
	}

	cfg := s.parseElectionConfig(r)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	state := sim.NewElectionState(s.scenario)
	exitRegionSeats := make(map[string]map[string]int, len(s.scenario.Regions))
	var prevEUSeats map[string]int
	if len(s.scenario.PrevSeats) > 0 {
		for ri, reg := range s.scenario.Regions {
			exitRegionSeats[reg.ID] = noisyRegionSeats(reg, s.scenario.Parties, cfg.Temperature, 0.25, (cfg.Seed^0x51a7)+int64(ri))
		}
		prevEUSeats = maps.Clone(s.scenario.PrevSeats)
	} else {
		prevRegionSeats := make(map[string]map[string]int, len(s.scenario.Regions))
		for ri, reg := range s.scenario.Regions {
			exitRegionSeats[reg.ID] = noisyRegionSeats(reg, s.scenario.Parties, cfg.Temperature, 0.25, (cfg.Seed^0x51a7)+int64(ri))
			prevRegionSeats[reg.ID] = noisyRegionSeats(reg, s.scenario.Parties, cfg.Temperature*1.15, 0.20, 1001+int64(ri))
		}
		prevEUSeats = s.scenario.AggregateEUSeats(prevRegionSeats)
	}
	state.SeedSeats(exitRegionSeats)
	_, totalPrecincts, _ := state.Progress()

	if err := writeSSE(w, flusher, "exit_poll", exitPollPayload{
		RegionSeats:    exitRegionSeats,
		EUSeats:        state.EUSeats(),
		PrevEUSeats:    prevEUSeats,
		Coalitions:     state.Coalitions(),
		TotalPrecincts: totalPrecincts,
	}); err != nil {
		return
	}

	reports := sim.StreamElection(r.Context(), s.scenario.Regions, s.scenario.Parties, cfg)

	for rep := range reports {
		state.Apply(rep)
		completed, total, countedPct := state.Progress()

		payload := precinctPayload{
			Report:            rep,
			RegionVotes:       state.RegionVotes(rep.RegionID),
			RegionSeats:       state.RegionSeats(rep.RegionID),
			EUSeats:           state.EUSeats(),
			Coalitions:        state.Coalitions(),
			ReportedPrecincts: completed,
			TotalPrecincts:    total,
			CountedPct:        countedPct,
		}
		if err := writeSSE(w, flusher, "precinct", payload); err != nil {
			return
		}
	}

	// Only emit complete if the client did not cancel early.
	if r.Context().Err() == nil {
		completed, total, _ := state.Progress()
		_ = writeSSE(w, flusher, "complete", map[string]int{
			"reported_precincts": completed,
			"total_precincts":    total,
		})
	}
}

func (s *Server) parseElectionConfig(r *http.Request) sim.ElectionConfig {
	q := r.URL.Query()
	cfg := sim.ElectionConfig{
		Temperature:    0.85,
		Swing:          0.15,
		Seed:           42,
		TargetDuration: 45 * time.Second,
	}
	if v, err := strconv.ParseFloat(q.Get("temp"), 64); err == nil && v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
		cfg.Temperature = v
	}
	if v, err := strconv.ParseFloat(q.Get("swing"), 64); err == nil && v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
		cfg.Swing = v
	}
	if v, err := strconv.ParseInt(q.Get("seed"), 10, 64); err == nil {
		cfg.Seed = v
	}
	if v, err := strconv.Atoi(q.Get("waves")); err == nil && v >= 1 && v <= 6 {
		cfg.Waves = v
	}
	if v, err := strconv.Atoi(q.Get("duration_ms")); err == nil && v >= 0 {
		cfg.TargetDuration = min(time.Duration(v)*time.Millisecond, maxTargetDuration)
		cfg.BaseDelay = 0
	} else if v, err := strconv.Atoi(q.Get("delay_ms")); err == nil && v >= 0 {
		cfg.BaseDelay = min(time.Duration(v)*time.Millisecond, maxBaseDelay)
		cfg.TargetDuration = 0
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
	reports := sim.SimulatePrecincts(reg, parties, temp, swing, seed)
	votes := make(map[string]int)
	for _, rep := range reports {
		for pid, v := range rep.Votes {
			votes[pid] += v
		}
	}
	return sim.AllocateSeats(votes, reg.Seats, reg.Method, reg.Threshold)
}
