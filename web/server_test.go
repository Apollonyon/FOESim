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
}
