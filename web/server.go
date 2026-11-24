package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
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

	cfg := s.parseElectionConfig(r)

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
		totalPrecincts += reg.EffectivePrecinctCount()

		regionSeats[reg.ID] = noisyRegionSeats(reg, s.scenario.Parties, cfg.Temperature, 0.25, (cfg.Seed^0x51a7)+int64(ri))
		prevRegionSeats[reg.ID] = noisyRegionSeats(reg, s.scenario.Parties, cfg.Temperature*1.15, 0.20, 1001+int64(ri))
	}

	euGroups := s.scenario.EffectiveGroups()
	lastEU := s.scenario.AggregateEUSeats(regionSeats)
	lastCoalitions := sim.FindCoalitions(lastEU, euGroups, 5)

	if err := writeSSE(w, flusher, "exit_poll", exitPollPayload{
		RegionSeats:    regionSeats,
		EUSeats:        lastEU,
		PrevEUSeats:    s.scenario.AggregateEUSeats(prevRegionSeats),
		Coalitions:     lastCoalitions,
		TotalPrecincts: totalPrecincts,
	}); err != nil {
		return
	}

	reports := sim.StreamElection(r.Context(), s.scenario.Regions, s.scenario.Parties, cfg)
	precinctPct := make(map[string]float64, totalPrecincts)
	sumPct := 0.0
	completed := 0

	for rep := range reports {
		for partyID, v := range rep.Votes {
			tallies[rep.RegionID][partyID] += v
		}

		prev := precinctPct[rep.PrecinctID]
		cur := rep.ReportedPct
		if cur <= 0 {
			cur = 1.0
		}
		if cur > prev {
			sumPct += cur - prev
			precinctPct[rep.PrecinctID] = cur
			if prev < 1.0 && cur >= 1.0 {
				completed++
			}
		}

		reg := regionByID[rep.RegionID]
		regionSeats[rep.RegionID] = sim.AllocateSeats(tallies[rep.RegionID], reg.Seats, reg.Method, reg.Threshold)

		countedPct := 0.0
		if totalPrecincts > 0 {
			countedPct = (sumPct / float64(totalPrecincts)) * 100
		}

		curEU := s.scenario.AggregateEUSeats(regionSeats)
		if !maps.Equal(curEU, lastEU) {
			lastEU = curEU
			lastCoalitions = sim.FindCoalitions(curEU, euGroups, 5)
		}

		payload := precinctPayload{
			Report:            rep,
			RegionVotes:       tallies[rep.RegionID],
			RegionSeats:       regionSeats[rep.RegionID],
			EUSeats:           curEU,
			Coalitions:        lastCoalitions,
			ReportedPrecincts: completed,
			TotalPrecincts:    totalPrecincts,
			CountedPct:        countedPct,
		}
		if err := writeSSE(w, flusher, "precinct", payload); err != nil {
			return
		}
	}

	// Only emit complete if the client did not cancel early.
	if r.Context().Err() == nil {
		_ = writeSSE(w, flusher, "complete", map[string]int{
			"reported_precincts": completed,
			"total_precincts":    totalPrecincts,
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
	if v, err := strconv.ParseFloat(q.Get("temp"), 64); err == nil && v > 0 {
		cfg.Temperature = v
	}
	if v, err := strconv.ParseFloat(q.Get("swing"), 64); err == nil && v >= 0 {
		cfg.Swing = v
	}
	if v, err := strconv.ParseInt(q.Get("seed"), 10, 64); err == nil {
		cfg.Seed = v
	}
	if v, err := strconv.Atoi(q.Get("waves")); err == nil && v >= 1 && v <= 6 {
		cfg.Waves = v
	}
	if v, err := strconv.Atoi(q.Get("duration_ms")); err == nil && v >= 0 {
		cfg.TargetDuration = time.Duration(v) * time.Millisecond
		cfg.BaseDelay = 0
	} else if v, err := strconv.Atoi(q.Get("delay_ms")); err == nil && v >= 0 {
		cfg.BaseDelay = time.Duration(v) * time.Millisecond
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
