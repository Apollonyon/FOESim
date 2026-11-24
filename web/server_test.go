package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	})
}

func TestParseElectionConfigClamps(t *testing.T) {
	srv := NewServer(sim.Scenario{})

	req := httptest.NewRequest(http.MethodGet, "/api/stream?duration_ms=999999999&temp=NaN&swing=Inf", nil)
	cfg := srv.parseElectionConfig(req)
	if cfg.TargetDuration != maxTargetDuration {
		t.Errorf("TargetDuration = %v, want clamped %v", cfg.TargetDuration, maxTargetDuration)
	}
	if cfg.Temperature != 0.85 {
		t.Errorf("Temperature = %v, want fallback 0.85 for NaN", cfg.Temperature)
	}
	if cfg.Swing != 0.15 {
		t.Errorf("Swing = %v, want fallback 0.15 for Inf", cfg.Swing)
	}

	delayReq := httptest.NewRequest(http.MethodGet, "/api/stream?delay_ms=999999999", nil)
	delayCfg := srv.parseElectionConfig(delayReq)
	if delayCfg.BaseDelay != maxBaseDelay {
		t.Errorf("BaseDelay = %v, want clamped %v", delayCfg.BaseDelay, maxBaseDelay)
	}
}

func TestServerStreamConcurrencyCap(t *testing.T) {
	scenario := sim.Scenario{
		Name:    "Cap Test",
		Parties: sim.DefaultParties(),
		Regions: sim.DefaultRegions(),
	}
	srv := NewServer(scenario)
	srv.streamSem = make(chan struct{}, 1)
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

	// Cancel immediately after exit_poll? Simpler: run with duration_ms=0 (fast) and inspect exit_poll.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/stream?duration_ms=0&seed=42", nil).WithContext(ctx)
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
