package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"parliamentsim/sim"
)

func TestServerRoutes(t *testing.T) {
	scenario := sim.Scenario{
		Name:    "Test EU",
		Parties: sim.DefaultParties(),
		Regions: sim.DefaultRegions(),
	}
	handler := NewServer(scenario).Handler()

	t.Run("GET /api/scenario returns scenario JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/scenario", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		var got sim.Scenario
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("failed to decode response JSON: %v", err)
		}
		if got.Name != scenario.Name || len(got.Parties) != len(scenario.Parties) || len(got.Regions) != len(scenario.Regions) {
			t.Errorf("got scenario %q (%d parties, %d regions), want %q (%d, %d)",
				got.Name, len(got.Parties), len(got.Regions),
				scenario.Name, len(scenario.Parties), len(scenario.Regions))
		}
	})

	t.Run("GET / serves embedded index.html", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), "ParliamentSim") {
			t.Errorf("expected index.html body to mention ParliamentSim")
		}
	})

	t.Run("GET /api/stream emits exit_poll, all precincts, and complete", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/stream?delay_ms=0&seed=42", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
			t.Errorf("Content-Type = %q, want text/event-stream", ct)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "event: exit_poll\n") {
			t.Errorf("expected exit_poll event in stream")
		}
		wantPrecincts := 0
		for _, r := range scenario.Regions {
			wantPrecincts += r.Precincts
		}
		if got := strings.Count(body, "event: precinct\n"); got != wantPrecincts {
			t.Errorf("got %d precinct events, want %d", got, wantPrecincts)
		}
		if !strings.Contains(body, "event: complete\n") {
			t.Errorf("expected complete event in stream")
		}
	})

	t.Run("GET /api/stream handles invalid query params with defaults", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/stream?delay_ms=0&temp=abc&swing=-5&seed=nope", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), "event: complete\n") {
			t.Errorf("expected stream to complete cleanly using fallback defaults")
		}
	})

	t.Run("GET /api/stream stops cleanly when client disconnects", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		req := httptest.NewRequest(http.MethodGet, "/api/stream?delay_ms=50", nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if strings.Contains(rec.Body.String(), "event: complete\n") {
			t.Errorf("did not expect complete event on canceled client context")
		}
	})

	t.Run("embedded geo and JS static assets serve 200 OK", func(t *testing.T) {
		for _, path := range []string{"/geo/europe_nuts0.json", "/geo/nuts3.json", "/js/app.js"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
				t.Errorf("GET %s: status=%d len=%d, want 200 non-empty", path, rec.Code, rec.Body.Len())
			}
		}
	})
}

func TestServerNationalPartiesAndNUTS3Stream(t *testing.T) {
	scenario, err := sim.LoadScenario("../scenarios/eu_starter.json")
	if err != nil {
		t.Fatalf("LoadScenario failed: %v", err)
	}
	handler := NewServer(scenario).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/stream?duration_ms=0&seed=42", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	wantPrecincts := 0
	for _, r := range scenario.Regions {
		wantPrecincts += r.EffectivePrecinctCount()
	}
	if got := strings.Count(body, "event: precinct\n"); got != wantPrecincts {
		t.Fatalf("got %d NUTS-3 precinct events, want %d", got, wantPrecincts)
	}

	// Verify exit_poll payload carries EU Group IDs in eu_seats and national Party IDs in region_seats.
	for _, block := range strings.Split(body, "\n\n") {
		if !strings.HasPrefix(block, "event: exit_poll\n") {
			continue
		}
		dataLine := strings.TrimPrefix(strings.Split(block, "\n")[1], "data: ")
		var payload exitPollPayload
		if err := json.Unmarshal([]byte(dataLine), &payload); err != nil {
			t.Fatalf("decode exit_poll: %v", err)
		}
		if payload.TotalPrecincts != wantPrecincts {
			t.Errorf("exit_poll total_precincts = %d, want %d", payload.TotalPrecincts, wantPrecincts)
		}
		if payload.EUSeats["epp"] == 0 {
			t.Errorf("expected EU group 'epp' seats in exit_poll eu_seats, got %v", payload.EUSeats)
		}
		if len(payload.Coalitions) == 0 {
			t.Errorf("expected viable coalitions in exit_poll payload, got none")
		}
		if payload.RegionSeats["de"]["de_cdu"] == 0 {
			t.Errorf("expected national party 'de_cdu' seats in region_seats[de], got %v", payload.RegionSeats["de"])
		}
		break
	}

	t.Run("waves=3 streams partial batches and reaches 100% counted", func(t *testing.T) {
		wReq := httptest.NewRequest(http.MethodGet, "/api/stream?duration_ms=0&waves=3&seed=42", nil)
		wRec := httptest.NewRecorder()
		handler.ServeHTTP(wRec, wReq)

		wBody := wRec.Body.String()
		if got := strings.Count(wBody, "event: precinct\n"); got != wantPrecincts*3 {
			t.Fatalf("got %d wave events, want %d", got, wantPrecincts*3)
		}
		if !strings.Contains(wBody, `"reported_precincts":604`) {
			t.Errorf("expected final reported_precincts to reach 604")
		}

		var omitted, included int
		for _, block := range strings.Split(wBody, "\n\n") {
			if !strings.HasPrefix(block, "event: precinct\n") {
				continue
			}
			dataLine := strings.TrimPrefix(strings.Split(block, "\n")[1], "data: ")
			var payload precinctPayload
			if err := json.Unmarshal([]byte(dataLine), &payload); err != nil {
				t.Fatalf("decode precinct payload: %v", err)
			}
			if payload.EUSeats == nil {
				omitted++
				if strings.Contains(dataLine, `"coalitions"`) || strings.Contains(dataLine, `"eu_seats"`) {
					t.Fatalf("expected unchanged precinct payload to omit eu_seats and coalitions, got %s", dataLine)
				}
			} else {
				included++
				if len(payload.Coalitions) == 0 {
					t.Errorf("expected changed precinct payload to include coalitions")
				}
			}
		}
		if omitted == 0 || included == 0 {
			t.Errorf("expected mix of omitted (%d) and included (%d) delta payloads across %d waves", omitted, included, wantPrecincts*3)
		}
	})
}

func TestParseElectionConfigClamps(t *testing.T) {
	srv := NewServer(sim.Scenario{})

	req := httptest.NewRequest(http.MethodGet, "/api/stream?duration_ms=10000000000000&temp=NaN&swing=Inf", nil)
	cfg := srv.parseElectionConfig(req)
	if cfg.TargetDuration != maxTargetDuration {
		t.Errorf("TargetDuration = %v, want clamped %v", cfg.TargetDuration, maxTargetDuration)
	}
	if cfg.Temperature != 0.45 {
		t.Errorf("Temperature = %v, want fallback 0.45 for NaN", cfg.Temperature)
	}
	if cfg.Swing != 0.15 {
		t.Errorf("Swing = %v, want fallback 0.15 for Inf", cfg.Swing)
	}

	extremeReq := httptest.NewRequest(http.MethodGet, "/api/stream?delay_ms=10000000000000&temp=1e-300&swing=100", nil)
	extremeCfg := srv.parseElectionConfig(extremeReq)
	if extremeCfg.BaseDelay != maxBaseDelay {
		t.Errorf("BaseDelay = %v, want clamped %v", extremeCfg.BaseDelay, maxBaseDelay)
	}
	if extremeCfg.Temperature != minTemperature {
		t.Errorf("Temperature = %v, want clamped %v", extremeCfg.Temperature, minTemperature)
	}
	if extremeCfg.Swing != maxSwing {
		t.Errorf("Swing = %v, want clamped %v", extremeCfg.Swing, maxSwing)
	}
}

func TestServerStreamConcurrencyCap(t *testing.T) {
	scenario := sim.Scenario{
		Name:    "Cap Test",
		Parties: sim.DefaultParties(),
		Regions: sim.DefaultRegions(),
	}
	srv := NewServer(scenario)
	srv.SetMaxStreams(1)
	handler := srv.Handler()

	// Occupy the only stream slot to simulate an active SSE client.
	srv.streamSem <- struct{}{}

	req := httptest.NewRequest(http.MethodGet, "/api/stream?delay_ms=0", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status when at cap = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	// Releasing the slot lets the next stream succeed normally.
	<-srv.streamSem
	req2 := httptest.NewRequest(http.MethodGet, "/api/stream?delay_ms=0", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status after slot freed = %d, want %d", rec2.Code, http.StatusOK)
	}
	if !strings.Contains(rec2.Body.String(), "event: complete\n") {
		t.Errorf("expected stream to complete after slot freed")
	}
}

func TestServerEU27RealPrevSeatsBaseline(t *testing.T) {
	scenario, err := sim.LoadScenario("../scenarios/eu27.json")
	if err != nil {
		t.Fatalf("LoadScenario(eu27.json) failed: %v", err)
	}
	handler := NewServer(scenario).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/stream?duration_ms=0&swing=0&seed=42", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	found := false
	for _, block := range strings.Split(rec.Body.String(), "\n\n") {
		if !strings.HasPrefix(block, "event: exit_poll\n") {
			continue
		}
		found = true
		dataLine := strings.TrimPrefix(strings.Split(block, "\n")[1], "data: ")
		var payload exitPollPayload
		if err := json.Unmarshal([]byte(dataLine), &payload); err != nil {
			t.Fatalf("decode exit_poll: %v", err)
		}
		if payload.PrevEUSeats["epp"] != 188 || payload.PrevEUSeats["sd"] != 136 || payload.PrevEUSeats["pfe"] != 84 {
			t.Fatalf("unexpected 2024 PrevEUSeats: %v", payload.PrevEUSeats)
		}
		sum := 0
		for _, seats := range payload.PrevEUSeats {
			sum += seats
		}
		if sum != 720 {
			t.Fatalf("PrevEUSeats sum = %d, want 720 (%v)", sum, payload.PrevEUSeats)
		}
		break
	}
	if !found {
		t.Fatal("expected exit_poll event in stream")
	}
}

func TestServerMethodOverrideDoesNotMutateScenario(t *testing.T) {
	scenario := sim.Scenario{
		Name:    "Override Test",
		Parties: sim.DefaultParties(),
		Regions: sim.DefaultRegions(),
	}
	origMethod := scenario.Regions[0].Method
	handler := NewServer(scenario).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/stream?duration_ms=0&swing=0&seed=42&method=fptp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Under FPTP, every region's exit_poll seats should go to a single winning party.
	foundExit := false
	for _, block := range strings.Split(rec.Body.String(), "\n\n") {
		if !strings.HasPrefix(block, "event: exit_poll\n") {
			continue
		}
		foundExit = true
		dataLine := strings.TrimPrefix(strings.Split(block, "\n")[1], "data: ")
		var payload exitPollPayload
		if err := json.Unmarshal([]byte(dataLine), &payload); err != nil {
			t.Fatalf("decode exit_poll: %v", err)
		}
		for _, r := range scenario.Regions {
			seats := payload.RegionSeats[r.ID]
			if len(seats) != 1 {
				t.Errorf("FPTP region %s gave seats to %d parties (%v), want 1", r.ID, len(seats), seats)
			}
			for _, count := range seats {
				if count != r.Seats {
					t.Errorf("FPTP region %s winner got %d seats, want all %d", r.ID, count, r.Seats)
				}
			}
		}
		break
	}
	if !foundExit {
		t.Fatal("expected exit_poll event")
	}
	if scenario.Regions[0].Method != origMethod {
		t.Errorf("method override mutated server scenario: got %q, want %q", scenario.Regions[0].Method, origMethod)
	}
}

func TestServerUSA2024ScenarioAndAssets(t *testing.T) {
	scenario, err := sim.LoadScenario("../scenarios/usa2024.json")
	if err != nil {
		t.Fatalf("LoadScenario(usa2024.json) failed: %v", err)
	}
	handler := NewServer(scenario).Handler()

	// Verify /api/scenario includes resolved USA metadata.
	req := httptest.NewRequest(http.MethodGet, "/api/scenario", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/scenario status = %d, want 200", rec.Code)
	}
	var gotScenario sim.Scenario
	if err := json.Unmarshal(rec.Body.Bytes(), &gotScenario); err != nil {
		t.Fatalf("decode /api/scenario: %v", err)
	}
	if gotScenario.Meta.ChartType != "bar" || gotScenario.Meta.RegionsURL != "/geo/us_states.json" {
		t.Fatalf("unexpected USA scenario meta: %+v", gotScenario.Meta)
	}

	// Verify embedded US GeoJSON assets are served.
	for _, asset := range []string{"/geo/us_states.json", "/geo/us_counties.json"} {
		aReq := httptest.NewRequest(http.MethodGet, asset, nil)
		aRec := httptest.NewRecorder()
		handler.ServeHTTP(aRec, aReq)
		if aRec.Code != http.StatusOK || aRec.Body.Len() < 1000 {
			t.Errorf("GET %s: status=%d len=%d", asset, aRec.Code, aRec.Body.Len())
		}
	}

	// Verify SSE stream emits exit_poll with ME/NE district winners and completes cleanly.
	sReq := httptest.NewRequest(http.MethodGet, "/api/stream?duration_ms=0&swing=0&seed=42", nil)
	sRec := httptest.NewRecorder()
	handler.ServeHTTP(sRec, sReq)
	if sRec.Code != http.StatusOK {
		t.Fatalf("/api/stream status = %d, want 200", sRec.Code)
	}
	body := sRec.Body.String()
	if !strings.Contains(body, "event: complete\n") {
		t.Errorf("expected complete event in USA SSE stream")
	}

	foundExit := false
	for _, block := range strings.Split(body, "\n\n") {
		if !strings.HasPrefix(block, "event: exit_poll\n") {
			continue
		}
		foundExit = true
		dataLine := strings.TrimPrefix(strings.Split(block, "\n")[1], "data: ")
		var payload exitPollPayload
		if err := json.Unmarshal([]byte(dataLine), &payload); err != nil {
			t.Fatalf("decode USA exit_poll: %v", err)
		}
		if payload.EUSeats["rep"] != 312 || payload.EUSeats["dem"] != 226 {
			t.Errorf("USA exit_poll EVs = %v, want rep:312 dem:226", payload.EUSeats)
		}
		if payload.DistrictWinners["me"]["ME-2"] != "rep" || payload.DistrictWinners["ne"]["NE-2"] != "dem" {
			t.Errorf("USA exit_poll split districts = %v", payload.DistrictWinners)
		}
		break
	}
	if !foundExit {
		t.Fatal("expected exit_poll event in USA SSE stream")
	}
}

func TestServerSecurityHeadersGzipAndPerIPCap(t *testing.T) {
	scenario := sim.Scenario{
		Name:    "Hardening Test",
		Parties: sim.DefaultParties(),
		Regions: sim.DefaultRegions(),
	}
	srv := NewServer(scenario)
	srv.SetMaxStreams(10)
	srv.SetMaxStreamsPerIP(1)
	handler := srv.Handler()

	// Security headers + gzip on /api/scenario
	req := httptest.NewRequest(http.MethodGet, "/api/scenario", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/scenario status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("expected Content-Encoding: gzip, got %q", rec.Header().Get("Content-Encoding"))
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("missing security headers: %v", rec.Header())
	}

	// ETag + 304 Not Modified on /geo/ static assets
	gReq := httptest.NewRequest(http.MethodGet, "/geo/us_states.json", nil)
	gRec := httptest.NewRecorder()
	handler.ServeHTTP(gRec, gReq)
	etag := gRec.Header().Get("ETag")
	if gRec.Code != http.StatusOK || etag == "" || !strings.Contains(gRec.Header().Get("Cache-Control"), "must-revalidate") {
		t.Errorf("static /geo asset headers: status=%d etag=%q cache=%q", gRec.Code, etag, gRec.Header().Get("Cache-Control"))
	}
	gReq304 := httptest.NewRequest(http.MethodGet, "/geo/us_states.json", nil)
	gReq304.Header.Set("If-None-Match", etag)
	gRec304 := httptest.NewRecorder()
	handler.ServeHTTP(gRec304, gReq304)
	if gRec304.Code != http.StatusNotModified {
		t.Errorf("conditional GET status = %d, want 304", gRec304.Code)
	}

	// Per-IP SSE stream cap returns 429
	if !srv.acquireIPSlot("198.51.100.7") {
		t.Fatal("expected first per-IP slot to succeed")
	}
	defer srv.releaseIPSlot("198.51.100.7")

	sReq := httptest.NewRequest(http.MethodGet, "/api/stream?duration_ms=0", nil)
	sReq.RemoteAddr = "198.51.100.7:54321"
	sRec := httptest.NewRecorder()
	handler.ServeHTTP(sRec, sReq)
	if sRec.Code != http.StatusTooManyRequests {
		t.Fatalf("per-IP cap status = %d, want 429", sRec.Code)
	}
}

func TestServerMultiScenarioRegistry(t *testing.T) {
	reg, err := sim.LoadScenariosDir("../scenarios", "eu27")
	if err != nil {
		t.Fatalf("LoadScenariosDir failed: %v", err)
	}
	handler := NewServerWithRegistry(reg).Handler()

	// GET /api/scenarios returns ordered summaries with gzip + security headers.
	req := httptest.NewRequest(http.MethodGet, "/api/scenarios", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/scenarios status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("unexpected headers on /api/scenarios: %v", rec.Header())
	}

	// Uncompressed /api/scenarios parses cleanly.
	rawReq := httptest.NewRequest(http.MethodGet, "/api/scenarios", nil)
	rawRec := httptest.NewRecorder()
	handler.ServeHTTP(rawRec, rawReq)
	var listResp scenariosListPayload
	if err := json.Unmarshal(rawRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode /api/scenarios: %v", err)
	}
	if listResp.DefaultID != "eu27" || len(listResp.Scenarios) < 3 || listResp.Scenarios[0].ID != "eu27" {
		t.Fatalf("unexpected /api/scenarios payload: %+v", listResp)
	}

	// GET /api/scenario?scenario=usa2024 resolves the requested scenario.
	scReq := httptest.NewRequest(http.MethodGet, "/api/scenario?scenario=usa2024", nil)
	scRec := httptest.NewRecorder()
	handler.ServeHTTP(scRec, scReq)
	if scRec.Code != http.StatusOK {
		t.Fatalf("/api/scenario?scenario=usa2024 status = %d, want 200", scRec.Code)
	}
	var gotUS sim.Scenario
	if err := json.Unmarshal(scRec.Body.Bytes(), &gotUS); err != nil {
		t.Fatalf("decode usa2024 scenario: %v", err)
	}
	if gotUS.Meta.ChartType != "bar" {
		t.Errorf("usa2024 ChartType = %q, want %q", gotUS.Meta.ChartType, "bar")
	}

	// Unknown scenario returns 404 on both /api/scenario and /api/stream.
	for _, path := range []string{"/api/scenario?scenario=does_not_exist", "/api/stream?scenario=does_not_exist"} {
		badReq := httptest.NewRequest(http.MethodGet, path, nil)
		badRec := httptest.NewRecorder()
		handler.ServeHTTP(badRec, badReq)
		if badRec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, badRec.Code)
		}
	}

	// GET /api/stream?scenario=usa2024 streams USA 2024 EVs even when default is eu27.
	stReq := httptest.NewRequest(http.MethodGet, "/api/stream?scenario=usa2024&duration_ms=0&swing=0&seed=42", nil)
	stRec := httptest.NewRecorder()
	handler.ServeHTTP(stRec, stReq)
	if stRec.Code != http.StatusOK {
		t.Fatalf("/api/stream?scenario=usa2024 status = %d, want 200", stRec.Code)
	}
	if !strings.Contains(stRec.Body.String(), `"rep":312`) || !strings.Contains(stRec.Body.String(), `"dem":226`) {
		t.Errorf("expected USA 2024 312/226 EVs in stream output")
	}
}

func TestRunStoreTTLEviction(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	rs := newRunStore(3, 10*time.Second)
	rs.now = func() time.Time { return now }

	makeSc := func(name string) sim.Scenario {
		return sim.Scenario{
			Name:    name,
			Parties: sim.DefaultParties(),
			Regions: sim.DefaultRegions(),
		}
	}

	id1, err := rs.Put(makeSc("one"))
	if err != nil || len(id1) != 24 {
		t.Fatalf("Put one failed: id=%q err=%v", id1, err)
	}

	now = now.Add(2 * time.Second)
	id2, _ := rs.Put(makeSc("two"))
	now = now.Add(2 * time.Second)
	id3, _ := rs.Put(makeSc("three"))

	// Capacity is 3: adding a fourth non-expired run evicts the earliest-expiring one (id1).
	now = now.Add(2 * time.Second)
	id4, _ := rs.Put(makeSc("four"))

	if _, ok := rs.Get(id1); ok {
		t.Fatalf("expected oldest run %s to be evicted at capacity", id1)
	}
	for _, pair := range []struct {
		id   string
		name string
	}{{id2, "two"}, {id3, "three"}, {id4, "four"}} {
		got, ok := rs.Get(pair.id)
		if !ok || got.Name != pair.name {
			t.Fatalf("Get(%s) = (%q, %v), want (%q, true)", pair.id, got.Name, ok, pair.name)
		}
	}

	// Advancing past id2's expiry removes id2 while leaving id4 valid.
	now = now.Add(6 * time.Second) // t = +12s: id2 expired at +12s, id4 expires at +16s
	if _, ok := rs.Get(id2); ok {
		t.Fatalf("expected %s to expire at TTL boundary", id2)
	}
	if _, ok := rs.Get(id4); !ok {
		t.Fatalf("expected %s to still be active before its TTL", id4)
	}
}

func TestServerCustomRunsLifecycle(t *testing.T) {
	base := sim.Scenario{
		Name:    "Base EU",
		Parties: sim.DefaultParties(),
		Regions: sim.DefaultRegions(),
	}
	srv := NewServer(base)
	handler := srv.Handler()

	custom := base
	custom.Name = "Custom Sandbox EU"
	custom.Regions = append([]sim.Region(nil), base.Regions...)
	custom.Regions[0].Seats = 110

	body, err := json.Marshal(custom)
	if err != nil {
		t.Fatalf("marshal custom scenario: %v", err)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/runs", bytes.NewReader(body))
	postReq.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusCreated {
		t.Fatalf("POST /api/runs status = %d, want 201 (%s)", postRec.Code, postRec.Body.String())
	}
	if cc := postRec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("POST /api/runs Cache-Control = %q, want no-store", cc)
	}

	var created createRunResponse
	if err := json.Unmarshal(postRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode createRunResponse: %v", err)
	}
	if created.RunID == "" || created.ExpiresInSec <= 0 {
		t.Fatalf("unexpected createRunResponse: %+v", created)
	}

	// GET /api/scenario?run=<id> returns the custom scenario with no-store.
	getReq := httptest.NewRequest(http.MethodGet, "/api/scenario?run="+created.RunID, nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /api/scenario?run status = %d, want 200", getRec.Code)
	}
	if cc := getRec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("GET /api/scenario?run Cache-Control = %q, want no-store", cc)
	}
	var gotSc sim.Scenario
	if err := json.Unmarshal(getRec.Body.Bytes(), &gotSc); err != nil {
		t.Fatalf("decode /api/scenario?run: %v", err)
	}
	if gotSc.Name != "Custom Sandbox EU" || gotSc.Regions[0].Seats != 110 {
		t.Fatalf("unexpected custom scenario: name=%q seats[0]=%d", gotSc.Name, gotSc.Regions[0].Seats)
	}

	// GET /api/stream?run=<id> simulates the custom run.
	stReq := httptest.NewRequest(http.MethodGet, "/api/stream?run="+created.RunID+"&duration_ms=0&seed=42&swing=0", nil)
	stRec := httptest.NewRecorder()
	handler.ServeHTTP(stRec, stReq)
	if stRec.Code != http.StatusOK {
		t.Fatalf("GET /api/stream?run status = %d, want 200 (%s)", stRec.Code, stRec.Body.String())
	}
	sseBody := stRec.Body.String()
	for _, want := range []string{"event: exit_poll", "event: precinct", "event: complete"} {
		if !strings.Contains(sseBody, want) {
			t.Errorf("expected %q in custom run SSE output", want)
		}
	}

	// Unknown/expired run returns 404 without leaking stream slots.
	for _, path := range []string{"/api/scenario?run=deadbeefdeadbeefdeadbeef", "/api/stream?run=deadbeefdeadbeefdeadbeef"} {
		missReq := httptest.NewRequest(http.MethodGet, path, nil)
		missRec := httptest.NewRecorder()
		handler.ServeHTTP(missRec, missReq)
		if missRec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, missRec.Code)
		}
	}
	if len(srv.streamSem) != 0 {
		t.Errorf("streamSem leaked after 404: len=%d", len(srv.streamSem))
	}
}

func TestServerCustomRunsValidationAndLimits(t *testing.T) {
	base := sim.Scenario{
		Name:    "Base EU",
		Parties: sim.DefaultParties(),
		Regions: sim.DefaultRegions(),
	}
	handler := NewServer(base).Handler()

	postJSON := func(raw []byte) int {
		req := httptest.NewRequest(http.MethodPost, "/api/runs", bytes.NewReader(raw))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := postJSON([]byte("{invalid")); code != http.StatusBadRequest {
		t.Errorf("malformed JSON status = %d, want 400", code)
	}

	validBytes, _ := json.Marshal(base)
	trailing := append(append([]byte(nil), validBytes...), []byte(`{"extra":1}`)...)
	if code := postJSON(trailing); code != http.StatusBadRequest {
		t.Errorf("trailing JSON token status = %d, want 400", code)
	}

	badGeo := base
	badGeo.Meta.RegionsURL = "../etc/passwd"
	badGeoBytes, _ := json.Marshal(badGeo)
	if code := postJSON(badGeoBytes); code != http.StatusBadRequest {
		t.Errorf("unsafe RegionsURL status = %d, want 400", code)
	}

	tooManySeats := base
	tooManySeats.Regions = append([]sim.Region(nil), base.Regions...)
	tooManySeats.Regions[0].Seats = 2001
	seatsBytes, _ := json.Marshal(tooManySeats)
	if code := postJSON(seatsBytes); code != http.StatusBadRequest {
		t.Errorf("excessive seats status = %d, want 400", code)
	}

	oversized := bytes.Repeat([]byte("a"), maxCustomRunBodySize+1024)
	if code := postJSON(oversized); code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body status = %d, want 413", code)
	}
}

func TestServerForecastEndpoint(t *testing.T) {
	base := sim.Scenario{
		Name:    "Forecast Web Test",
		Parties: sim.DefaultParties(),
		Regions: sim.DefaultRegions(),
	}
	srv := NewServer(base)
	handler := srv.Handler()

	// Preset forecast with runs clamping and positive swing.
	req := httptest.NewRequest(http.MethodGet, "/api/forecast?runs=9999&swing=0.18&temp=0.45&seed=19", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/forecast status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	var fc sim.ForecastResult
	if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
		t.Fatalf("decode ForecastResult: %v", err)
	}
	if fc.Runs != 500 || fc.TotalSeats <= 0 || len(fc.Groups) == 0 || len(fc.Regions) != len(base.Regions) {
		t.Fatalf("unexpected ForecastResult: runs=%d seats=%d groups=%d regions=%d",
			fc.Runs, fc.TotalSeats, len(fc.Groups), len(fc.Regions))
	}

	// Custom run forecast returns 200 with Cache-Control: no-store.
	body, _ := json.Marshal(base)
	postReq := httptest.NewRequest(http.MethodPost, "/api/runs", bytes.NewReader(body))
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)
	var created createRunResponse
	_ = json.Unmarshal(postRec.Body.Bytes(), &created)

	custReq := httptest.NewRequest(http.MethodGet, "/api/forecast?run="+created.RunID+"&runs=20&method=fptp", nil)
	custRec := httptest.NewRecorder()
	handler.ServeHTTP(custRec, custReq)
	if custRec.Code != http.StatusOK {
		t.Fatalf("GET /api/forecast?run status = %d, want 200 (%s)", custRec.Code, custRec.Body.String())
	}
	if cc := custRec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("custom /api/forecast Cache-Control = %q, want no-store", cc)
	}

	// Missing custom run returns 404 without leaking forecastSem.
	missReq := httptest.NewRequest(http.MethodGet, "/api/forecast?run=missingrunid", nil)
	missRec := httptest.NewRecorder()
	handler.ServeHTTP(missRec, missReq)
	if missRec.Code != http.StatusNotFound {
		t.Fatalf("missing run status = %d, want 404", missRec.Code)
	}
	if len(srv.forecastSem) != 0 {
		t.Errorf("forecastSem leaked after 404: len=%d", len(srv.forecastSem))
	}
}

func TestServerHealthz(t *testing.T) {
	handler := NewServer(sim.Scenario{
		Name:    "Healthz Test",
		Parties: sim.DefaultParties(),
		Regions: sim.DefaultRegions(),
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Fatalf("/healthz = (%d, %q), want (200, ok)", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("/healthz Cache-Control = %q, want no-store", cc)
	}
}

func TestServerTrustProxyClientIP(t *testing.T) {
	srv := NewServer(sim.Scenario{
		Name:    "Proxy IP Test",
		Parties: sim.DefaultParties(),
		Regions: sim.DefaultRegions(),
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:43210"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.5")
	req.Header.Set("Fly-Client-IP", "198.51.100.7")

	if got := srv.clientIP(req); got != "10.0.0.5" {
		t.Fatalf("clientIP with trustProxy=false = %q, want 10.0.0.5", got)
	}

	srv.SetTrustProxy(true)
	if got := srv.clientIP(req); got != "198.51.100.7" {
		t.Fatalf("clientIP with Fly-Client-IP = %q, want 198.51.100.7", got)
	}

	req.Header.Set("CF-Connecting-IP", "2001:db8::1")
	if got := srv.clientIP(req); got != "2001:db8::1" {
		t.Fatalf("clientIP with CF-Connecting-IP = %q, want 2001:db8::1", got)
	}

	req.Header.Del("CF-Connecting-IP")
	req.Header.Del("Fly-Client-IP")
	req.Header.Set("X-Forwarded-For", "not-an-ip, 10.0.0.5")
	if got := srv.clientIP(req); got != "10.0.0.5" {
		t.Fatalf("clientIP with malformed XFF = %q, want fallback 10.0.0.5", got)
	}
}

func TestServerCustomRunsRateLimit(t *testing.T) {
	base := sim.Scenario{
		Name:    "Rate Limit Test",
		Parties: sim.DefaultParties(),
		Regions: sim.DefaultRegions(),
	}
	srv := NewServer(base)
	srv.SetMaxRunsPerMin(2)
	handler := srv.Handler()
	body, _ := json.Marshal(base)

	postFrom := func(addr string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/runs", bytes.NewReader(body))
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := postFrom("192.0.2.10:1111"); code != http.StatusCreated {
		t.Fatalf("1st POST = %d, want 201", code)
	}
	if code := postFrom("192.0.2.10:2222"); code != http.StatusCreated {
		t.Fatalf("2nd POST = %d, want 201", code)
	}
	if code := postFrom("192.0.2.10:3333"); code != http.StatusTooManyRequests {
		t.Fatalf("3rd POST from same IP = %d, want 429", code)
	}
	if code := postFrom("192.0.2.11:4444"); code != http.StatusCreated {
		t.Fatalf("POST from different IP = %d, want 201", code)
	}
}
