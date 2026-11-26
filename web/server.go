package web

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Apollonyon/FOESim/sim"
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
	maxPackGeoAssetBytes   = 32 << 20 // 32 MiB
)

//go:embed static
var staticFiles embed.FS

// Server serves the ParliamentSim web dashboard and JSON/SSE API endpoints.
type Server struct {
	scenario        sim.Scenario
	registry        *sim.ScenarioRegistry
	packsDir        string
	runs            *runStore
	runsLimiter     *ipRateLimiter
	streamSem       chan struct{}
	forecastSem     chan struct{}
	maxStreamsPerIP int
	proxyHeader     string
	ipMu            sync.Mutex
	ipStreams       map[string]int
}

// NewServer creates a Server configured with the given single election scenario.
func NewServer(scenario sim.Scenario) *Server {
	return &Server{
		scenario:        scenario,
		runs:            newRunStore(defaultRunMaxEntries, defaultRunTTL),
		runsLimiter:     newIPRateLimiter(defaultMaxRunsPerMin, time.Minute),
		streamSem:       make(chan struct{}, defaultMaxStreams),
		forecastSem:     make(chan struct{}, min(max(2, runtime.NumCPU()), 8)),
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

// SetTrustProxy controls whether the rightmost X-Forwarded-For hop is used to
// identify client IPs behind a trusted reverse proxy (e.g. Caddy or Nginx).
// Must be called before serving requests.
func (s *Server) SetTrustProxy(trust bool) {
	if trust {
		s.proxyHeader = "xff"
		return
	}
	s.proxyHeader = ""
}

// SetProxyHeader selects which reverse-proxy client IP header to trust:
// "xff" (rightmost X-Forwarded-For), "cf" (CF-Connecting-IP), "fly" (Fly-Client-IP),
// or "" / "off" to disable proxy header trust. Must be called before serving requests.
func (s *Server) SetProxyHeader(mode string) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "xff", "cf", "fly":
		s.proxyHeader = strings.ToLower(strings.TrimSpace(mode))
	default:
		s.proxyHeader = ""
	}
}

// SetMaxRunsPerMin overrides the per-IP custom scenario creation limit per minute.
// Values <= 0 disable rate limiting. Must be called before serving requests.
func (s *Server) SetMaxRunsPerMin(n int) {
	if n <= 0 {
		s.runsLimiter = nil
		return
	}
	s.runsLimiter = newIPRateLimiter(n, time.Minute)
}

// SetPacksDir configures a filesystem directory of external GeoPacks whose
// map JSON files (<dir>/<pack>/<file>.json) are served at /geo/packs/<pack>/<file>.json.
// Empty disables external pack serving. Must be called before serving requests.
func (s *Server) SetPacksDir(dir string) {
	s.packsDir = strings.TrimSpace(dir)
}

// Handler returns the HTTP router for the dashboard and API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("GET /api/scenarios", gzipMiddleware(http.HandlerFunc(s.handleScenarios)))
	mux.Handle("GET /api/scenario", gzipMiddleware(http.HandlerFunc(s.handleScenario)))
	mux.Handle("GET /api/forecast", gzipMiddleware(http.HandlerFunc(s.handleForecast)))
	mux.HandleFunc("POST /api/runs", s.handleCreateRun)
	mux.HandleFunc("GET /api/stream", s.handleStream)
	if s.packsDir != "" {
		mux.Handle("GET /geo/packs/", gzipMiddleware(http.HandlerFunc(s.handlePackGeo)))
	}

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

func parseCandidateIP(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	addr, err := netip.ParseAddr(s)
	if err != nil || !addr.IsValid() {
		return ""
	}
	return addr.Unmap().String()
}

func (s *Server) clientIP(r *http.Request) string {
	switch s.proxyHeader {
	case "cf":
		if ip := parseCandidateIP(r.Header.Get("CF-Connecting-IP")); ip != "" {
			return ip
		}
	case "fly":
		if ip := parseCandidateIP(r.Header.Get("Fly-Client-IP")); ip != "" {
			return ip
		}
	case "xff":
		if vals := r.Header.Values("X-Forwarded-For"); len(vals) > 0 {
			// Take the rightmost XFF entry added by the immediate trusted proxy.
			lastHeader := vals[len(vals)-1]
			last := lastHeader
			if idx := strings.LastIndexByte(lastHeader, ','); idx >= 0 {
				last = lastHeader[idx+1:]
			}
			if ip := parseCandidateIP(last); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}

func isSafePackGeoFilename(name string) bool {
	if name == "" || len(name) > 128 || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") || strings.Contains(name, "..") {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}

func (s *Server) handlePackGeo(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/geo/packs/")
	packName, fileName, ok := strings.Cut(rel, "/")
	if !ok || !sim.IsValidGeoPackName(packName) || !isSafePackGeoFilename(fileName) {
		http.NotFound(w, r)
		return
	}

	root, err := os.OpenRoot(s.packsDir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()

	f, err := root.Open(packName + "/" + fileName)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxPackGeoAssetBytes {
		http.NotFound(w, r)
		return
	}

	etag := fmt.Sprintf("\"%x-%x\"", st.Size(), st.ModTime().UnixNano())
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method == http.MethodHead {
		w.Header().Del("Content-Encoding")
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = io.Copy(w, f)
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
	GeoPacks  []string              `json:"geo_packs,omitempty"`
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
	payload.GeoPacks = sim.ListGeoPacks()
	_ = json.NewEncoder(w).Encode(payload)
}

type createRunResponse struct {
	RunID        string `json:"run_id"`
	ExpiresInSec int    `json:"expires_in_sec"`
}

func (s *Server) resolveScenarioForRequest(r *http.Request) (sim.Scenario, bool, bool) {
	if runID := r.URL.Query().Get("run"); runID != "" {
		if s.runs == nil {
			return sim.Scenario{}, true, false
		}
		sc, ok := s.runs.Get(runID)
		return sc, true, ok
	}
	sc, ok := s.resolveScenario(r.URL.Query().Get("scenario"))
	return sc, false, ok
}

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	if s.runs == nil {
		http.Error(w, "custom runs disabled", http.StatusServiceUnavailable)
		return
	}
	if s.runsLimiter != nil && !s.runsLimiter.Allow(s.clientIP(r)) {
		w.Header().Set("Retry-After", "10")
		http.Error(w, "too many custom scenarios created; try again shortly", http.StatusTooManyRequests)
		return
	}

	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(15 * time.Second))
	r.Body = http.MaxBytesReader(w, r.Body, maxCustomRunBodySize)
	dec := json.NewDecoder(r.Body)

	var sc sim.Scenario
	if err := dec.Decode(&sc); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "scenario payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, fmt.Sprintf("invalid scenario JSON: %v", err), http.StatusBadRequest)
		return
	}

	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "scenario payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "unexpected trailing JSON data", http.StatusBadRequest)
		return
	}

	if err := sc.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id, err := s.runs.Put(sc)
	if err != nil {
		http.Error(w, "failed to store custom run", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(createRunResponse{
		RunID:        id,
		ExpiresInSec: int(s.runs.ttl / time.Second),
	})
}

func (s *Server) handleScenario(w http.ResponseWriter, r *http.Request) {
	sc, isCustom, ok := s.resolveScenarioForRequest(r)
	if !ok {
		if isCustom {
			http.Error(w, "custom run not found or expired", http.StatusNotFound)
		} else {
			http.Error(w, "unknown scenario", http.StatusNotFound)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if isCustom {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	sc.Meta = sc.EffectiveMeta()
	_ = json.NewEncoder(w).Encode(sc)
}

func (s *Server) handleForecast(w http.ResponseWriter, r *http.Request) {
	baseScenario, isCustom, ok := s.resolveScenarioForRequest(r)
	if !ok {
		if isCustom {
			http.Error(w, "custom run not found or expired", http.StatusNotFound)
		} else {
			http.Error(w, "unknown scenario", http.StatusNotFound)
		}
		return
	}

	ip := s.clientIP(r)
	if !s.acquireIPSlot(ip) {
		w.Header().Set("Retry-After", "3")
		http.Error(w, "too many concurrent requests from this address", http.StatusTooManyRequests)
		return
	}
	defer s.releaseIPSlot(ip)

	if s.forecastSem != nil {
		select {
		case s.forecastSem <- struct{}{}:
			defer func() { <-s.forecastSem }()
		default:
			w.Header().Set("Retry-After", "3")
			http.Error(w, "forecast worker pool busy", http.StatusServiceUnavailable)
			return
		}
	}

	q := r.URL.Query()
	streamCfg := s.parseElectionConfig(r)
	runs := 200
	if v, err := strconv.Atoi(q.Get("runs")); err == nil && v > 0 {
		runs = max(10, min(v, 500))
	}

	sc := scenarioForMethod(baseScenario, parseMethodOverride(q.Get("method")))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	res, err := sim.RunForecast(ctx, sc, sim.ForecastConfig{
		Runs:          runs,
		Temperature:   streamCfg.Temperature,
		Swing:         streamCfg.Swing,
		Tactical:      streamCfg.Tactical,
		NationalShift: streamCfg.NationalShift,
		BaseSeed:      streamCfg.Seed,
	})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		http.Error(w, "forecast timed out or failed", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if isCustom {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	_ = json.NewEncoder(w).Encode(res)
}

type exitPollPayload struct {
	RegionSeats     map[string]map[string]int    `json:"region_seats"`
	DistrictWinners map[string]map[string]string `json:"district_winners,omitempty"`
	EUSeats         map[string]int               `json:"eu_seats"`
	PrevEUSeats     map[string]int               `json:"prev_eu_seats,omitempty"`
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

	baseScenario, isCustom, ok := s.resolveScenarioForRequest(r)
	if !ok {
		if isCustom {
			http.Error(w, "custom run not found or expired", http.StatusNotFound)
		} else {
			http.Error(w, "unknown scenario", http.StatusNotFound)
		}
		return
	}

	ip := s.clientIP(r)
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
	rawUnc := q.Get("uncertainty")
	if rawUnc == "" {
		rawUnc = q.Get("swing")
	}
	if v, err := strconv.ParseFloat(rawUnc, 64); err == nil && v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
		cfg.Swing = min(v, maxSwing)
	}
	if v, err := strconv.ParseFloat(q.Get("tactical"), 64); err == nil && v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
		cfg.Tactical = min(v, 1.0)
	}
	if rawShift := strings.TrimSpace(q.Get("shift")); rawShift != "" {
		cfg.NationalShift = parseNationalShift(rawShift)
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

func parseNationalShift(raw string) map[string]float64 {
	parts := strings.Split(raw, ",")
	if len(parts) > 32 {
		parts = parts[:32]
	}
	var out map[string]float64
	for _, part := range parts {
		key, valStr, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(valStr), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		v = max(-2.0, min(v, 2.0))
		if out == nil {
			out = make(map[string]float64)
		}
		out[key] = v
	}
	return out
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
