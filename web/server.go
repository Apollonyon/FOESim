package web

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"parliamentsim/sim"
)

const (
	defaultMaxStreams      = 256
	defaultMaxStreamsPerIP = 16
	maxTargetDuration      = 5 * time.Minute
	maxTargetDurationMs    = int(maxTargetDuration / time.Millisecond)
	maxBaseDelay           = 2 * time.Second
	maxBaseDelayMs         = int(maxBaseDelay / time.Millisecond)
	minTemperature         = 0.05
	maxTemperature         = 10.0
	maxSwing               = 2.0
)

//go:embed static
var staticFiles embed.FS

// Server serves the ParliamentSim web dashboard and JSON/SSE API endpoints.
type Server struct {
	scenario        sim.Scenario
	registry        *sim.ScenarioRegistry
	streamSem       chan struct{}
	maxStreamsPerIP int
	ipMu            sync.Mutex
	ipStreams       map[string]int
}

// NewServer creates a Server configured with the given single election scenario.
func NewServer(scenario sim.Scenario) *Server {
	return &Server{
		scenario:        scenario,
		streamSem:       make(chan struct{}, defaultMaxStreams),
		maxStreamsPerIP: defaultMaxStreamsPerIP,
		ipStreams:       make(map[string]int),
	}
}

// NewServerWithRegistry creates a Server backed by a multi-scenario registry.
func NewServerWithRegistry(registry *sim.ScenarioRegistry) *Server {
	srv := NewServer(sim.Scenario{})
	srv.registry = registry
	if registry != nil {
		if def, ok := registry.Get(""); ok {
			srv.scenario = def
		}
	}
	return srv
}

// SetMaxStreams overrides the maximum number of concurrent SSE streams.
// Values <= 0 disable the cap. Must be called before serving requests.
func (s *Server) SetMaxStreams(n int) {
	if n <= 0 {
		s.streamSem = nil
		return
	}
	s.streamSem = make(chan struct{}, n)
}

// SetMaxStreamsPerIP overrides the maximum concurrent SSE streams allowed per client IP.
// Values <= 0 disable the per-IP cap. Must be called before serving requests.
func (s *Server) SetMaxStreamsPerIP(n int) {
	s.maxStreamsPerIP = n
}

// Handler returns the HTTP router for the dashboard and API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /api/scenarios", gzipMiddleware(http.HandlerFunc(s.handleScenarios)))
	mux.Handle("GET /api/scenario", gzipMiddleware(http.HandlerFunc(s.handleScenario)))
	mux.HandleFunc("GET /api/stream", s.handleStream)

	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	etags := buildStaticETags(sub)
	fileSrv := http.FileServerFS(sub)
	mux.Handle("GET /", gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		cleanPath := strings.TrimPrefix(p, "/")
		if cleanPath == "" {
			cleanPath = "index.html"
		}
		if etag, ok := etags[cleanPath]; ok {
			w.Header().Set("ETag", etag)
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		if strings.HasPrefix(p, "/geo/") || strings.HasPrefix(p, "/js/") || strings.HasSuffix(p, ".css") {
			w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileSrv.ServeHTTP(w, r)
	})))

	return securityHeadersMiddleware(mux)
}

func buildStaticETags(fsys fs.FS) map[string]string {
	tags := make(map[string]string)
	_ = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		tags[path] = `"` + hex.EncodeToString(sum[:8]) + `"`
		return nil
	})
	return tags
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz        *gzip.Writer
	wroteBody bool
	noBody    bool
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	if code == http.StatusNotModified || code == http.StatusNoContent {
		w.noBody = true
		w.Header().Del("Content-Encoding")
	} else {
		w.Header().Del("Content-Length")
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) Write(p []byte) (int, error) {
	if w.noBody {
		return 0, nil
	}
	w.Header().Del("Content-Length")
	w.wroteBody = true
	return w.gz.Write(p)
}

var gzipPool = sync.Pool{
	New: func() any {
		gz, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return gz
	},
}

func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Accept-Encoding")
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzipPool.Get().(*gzip.Writer)
		gz.Reset(w)
		grw := &gzipResponseWriter{ResponseWriter: w, gz: gz}
		defer func() {
			if grw.wroteBody {
				_ = gz.Close()
			}
			gz.Reset(io.Discard)
			gzipPool.Put(gz)
		}()
		next.ServeHTTP(grw, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) acquireIPSlot(ip string) bool {
	if s.maxStreamsPerIP <= 0 || ip == "" {
		return true
	}
	s.ipMu.Lock()
	defer s.ipMu.Unlock()
	if s.ipStreams[ip] >= s.maxStreamsPerIP {
		return false
	}
	s.ipStreams[ip]++
	return true
}

func (s *Server) releaseIPSlot(ip string) {
	if s.maxStreamsPerIP <= 0 || ip == "" {
		return
	}
	s.ipMu.Lock()
	defer s.ipMu.Unlock()
	if s.ipStreams[ip] <= 1 {
		delete(s.ipStreams, ip)
	} else {
		s.ipStreams[ip]--
	}
}

type scenariosListPayload struct {
	DefaultID string                `json:"default_id"`
	Scenarios []sim.ScenarioSummary `json:"scenarios"`
}

func (s *Server) resolveScenario(id string) (sim.Scenario, bool) {
	if s.registry != nil {
		return s.registry.Get(id)
	}
	if id == "" || id == "default" {
		return s.scenario, true
	}
	return sim.Scenario{}, false
}

func (s *Server) handleScenarios(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	var payload scenariosListPayload
	if s.registry != nil {
		payload.DefaultID = s.registry.DefaultID()
		payload.Scenarios = s.registry.List()
	} else {
		payload.DefaultID = "default"
		payload.Scenarios = []sim.ScenarioSummary{s.scenario.Summary("default")}
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) handleScenario(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.resolveScenario(r.URL.Query().Get("scenario"))
	if !ok {
		http.Error(w, "unknown scenario", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	sc.Meta = sc.EffectiveMeta()
	_ = json.NewEncoder(w).Encode(sc)
}

type exitPollPayload struct {
	RegionSeats     map[string]map[string]int    `json:"region_seats"`
	DistrictWinners map[string]map[string]string `json:"district_winners,omitempty"`
	EUSeats         map[string]int               `json:"eu_seats"`
	PrevEUSeats     map[string]int               `json:"prev_eu_seats"`
	Coalitions      []sim.Coalition              `json:"coalitions"`
	TotalPrecincts  int                          `json:"total_precincts"`
}

type precinctPayload struct {
	Report            sim.PrecinctReport `json:"report"`
	RegionVotes       map[string]int     `json:"region_votes"`
	RegionSeats       map[string]int     `json:"region_seats"`
	DistrictWinners   map[string]string  `json:"district_winners,omitempty"`
	EUSeats           *map[string]int    `json:"eu_seats,omitempty"`
	Coalitions        []sim.Coalition    `json:"coalitions,omitempty"`
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

	baseScenario, ok := s.resolveScenario(r.URL.Query().Get("scenario"))
	if !ok {
		http.Error(w, "unknown scenario", http.StatusNotFound)
		return
	}

	ip := clientIP(r)
	if !s.acquireIPSlot(ip) {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "too many concurrent streams from this address", http.StatusTooManyRequests)
		return
	}
	defer s.releaseIPSlot(ip)

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

	if r.Context().Err() != nil {
		return
	}

	cfg := s.parseElectionConfig(r)
	scenario := scenarioForMethod(baseScenario, parseMethodOverride(r.URL.Query().Get("method")))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	state := sim.NewElectionState(scenario)
	prepared := sim.PrepareElection(scenario.Regions, scenario.Parties, cfg)
	exitRegionSeats := prepared.ExitPollSeats()
	exitDistrictWinners := prepared.ExitPollDistrictWinners()
	var prevEUSeats map[string]int
	if len(scenario.PrevSeats) > 0 {
		prevEUSeats = maps.Clone(scenario.PrevSeats)
	} else {
		prevRegionSeats := make(map[string]map[string]int, len(scenario.Regions))
		for ri, reg := range scenario.Regions {
			prevRegionSeats[reg.ID] = noisyRegionSeats(reg, scenario.Parties, cfg.Temperature*1.15, 0.20, 1001+int64(ri))
		}
		prevEUSeats = scenario.AggregateEUSeats(prevRegionSeats)
	}
	state.SeedSeats(exitRegionSeats)
	state.SeedDistrictWinners(exitDistrictWinners)
	_, totalPrecincts, _ := state.Progress()

	if err := writeSSE(w, flusher, "exit_poll", exitPollPayload{
		RegionSeats:     exitRegionSeats,
		DistrictWinners: exitDistrictWinners,
		EUSeats:         state.EUSeats(),
		PrevEUSeats:     prevEUSeats,
		Coalitions:      state.Coalitions(),
		TotalPrecincts:  totalPrecincts,
	}); err != nil {
		return
	}

	streamCtx, cancel := context.WithTimeout(r.Context(), maxTargetDuration+5*time.Second)
	defer cancel()

	reports := prepared.Stream(streamCtx)

	for rep := range reports {
		euChanged := state.Apply(rep)
		completed, total, countedPct := state.Progress()

		payload := precinctPayload{
			Report:            rep,
			RegionVotes:       state.RegionVotes(rep.RegionID),
			RegionSeats:       state.RegionSeats(rep.RegionID),
			DistrictWinners:   state.DistrictWinners(rep.RegionID),
			ReportedPrecincts: completed,
			TotalPrecincts:    total,
			CountedPct:        countedPct,
		}
		if euChanged {
			eu := state.EUSeats()
			payload.EUSeats = &eu
			payload.Coalitions = state.Coalitions()
		}
		if err := writeSSE(w, flusher, "precinct", payload); err != nil {
			return
		}
	}

	// Only emit complete if the client did not cancel or hit the stream timeout.
	if streamCtx.Err() == nil {
		completed, total, _ := state.Progress()
		_ = writeSSE(w, flusher, "complete", map[string]int{
			"reported_precincts": completed,
			"total_precincts":    total,
		})
	}
}

func scenarioForMethod(base sim.Scenario, override sim.AllocationMethod) sim.Scenario {
	if override == "" {
		return base
	}
	sc := base
	sc.Regions = slices.Clone(base.Regions)
	for i := range sc.Regions {
		sc.Regions[i].Method = override
		sc.Regions[i].DistrictSeats = 0
	}
	return sc
}

func parseMethodOverride(raw string) sim.AllocationMethod {
	switch sim.AllocationMethod(raw) {
	case sim.MethodSainteLague, sim.MethodDHondt, sim.MethodHare, sim.MethodFPTP, sim.MethodSTV:
		return sim.AllocationMethod(raw)
	default:
		return ""
	}
}

func (s *Server) parseElectionConfig(r *http.Request) sim.ElectionConfig {
	q := r.URL.Query()
	cfg := sim.ElectionConfig{
		Temperature:    0.45,
		Swing:          0.15,
		Seed:           42,
		TargetDuration: 45 * time.Second,
	}
	if v, err := strconv.ParseFloat(q.Get("temp"), 64); err == nil && v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
		cfg.Temperature = max(minTemperature, min(v, maxTemperature))
	}
	if v, err := strconv.ParseFloat(q.Get("swing"), 64); err == nil && v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
		cfg.Swing = min(v, maxSwing)
	}
	if v, err := strconv.ParseInt(q.Get("seed"), 10, 64); err == nil {
		cfg.Seed = v
	}
	if v, err := strconv.Atoi(q.Get("waves")); err == nil && v >= 1 && v <= 4 {
		cfg.Waves = v
	}
	if v, err := strconv.Atoi(q.Get("duration_ms")); err == nil && v >= 0 {
		cfg.TargetDuration = time.Duration(min(v, maxTargetDurationMs)) * time.Millisecond
		cfg.BaseDelay = 0
	} else if v, err := strconv.Atoi(q.Get("delay_ms")); err == nil && v >= 0 {
		cfg.BaseDelay = time.Duration(min(v, maxBaseDelayMs)) * time.Millisecond
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
	var districtVotes map[string]map[string]int
	if reg.DistrictSeats > 0 {
		districtVotes = make(map[string]map[string]int)
	}
	for _, rep := range reports {
		for pid, v := range rep.Votes {
			votes[pid] += v
		}
		if reg.DistrictSeats > 0 && rep.DistrictID != "" {
			if districtVotes[rep.DistrictID] == nil {
				districtVotes[rep.DistrictID] = make(map[string]int, len(rep.Votes))
			}
			for pid, v := range rep.Votes {
				districtVotes[rep.DistrictID][pid] += v
			}
		}
	}
	seats, _ := sim.AllocateRegionSeats(votes, districtVotes, reg, reg.EffectiveParties(parties))
	return seats
}
