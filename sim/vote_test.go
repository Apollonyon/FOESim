package sim

import (
	"math"
	"testing"
)

func TestVoteShares(t *testing.T) {
	group := VoterGroup{
		Name:       "Test Voters",
		Population: 100_000,
		Turnout:    0.70,
		Ideal:      PolicyVec{AxisEcon: -0.5, AxisSocial: -0.5, AxisEU: 0.5, AxisDefense: 0.0, AxisEnv: 0.8, AxisMigration: -0.4},
		Weights:    PolicyVec{1, 1, 1, 1, 1, 1},
	}

	closeParty := Party{
		ID:       "close",
		Position: PolicyVec{AxisEcon: -0.4, AxisSocial: -0.5, AxisEU: 0.5, AxisDefense: 0.0, AxisEnv: 0.8, AxisMigration: -0.3},
	}
	farParty := Party{
		ID:       "far",
		Position: PolicyVec{AxisEcon: 0.8, AxisSocial: 0.8, AxisEU: -0.8, AxisDefense: 0.5, AxisEnv: -0.8, AxisMigration: 0.9},
	}

	t.Run("empty parties returns empty map", func(t *testing.T) {
		shares := VoteShares(group, nil, 1.0)
		if len(shares) != 0 {
			t.Errorf("got %v, want empty map", shares)
		}
	})

	t.Run("shares sum to 1 and closer party leads", func(t *testing.T) {
		shares := VoteShares(group, []Party{closeParty, farParty}, 0.85)
		sum := shares["close"] + shares["far"]
		if math.Abs(sum-1.0) > 1e-9 {
			t.Errorf("shares sum = %v, want 1.0", sum)
		}
		if shares["close"] <= shares["far"] {
			t.Errorf("expected closer party to lead: close=%v, far=%v", shares["close"], shares["far"])
		}
	})

	t.Run("lower temperature sharpens winner advantage", func(t *testing.T) {
		cold := VoteShares(group, []Party{closeParty, farParty}, 0.25)
		warm := VoteShares(group, []Party{closeParty, farParty}, 2.0)
		if cold["close"] <= warm["close"] {
			t.Errorf("expected lower temperature to sharpen lead: cold=%v, warm=%v", cold["close"], warm["close"])
		}
	})

	t.Run("non-positive temperature defaults to 1.0", func(t *testing.T) {
		zeroTemp := VoteShares(group, []Party{closeParty, farParty}, 0)
		unitTemp := VoteShares(group, []Party{closeParty, farParty}, 1.0)
		if math.Abs(zeroTemp["close"]-unitTemp["close"]) > 1e-9 {
			t.Errorf("temperature 0 gave %v, want same as 1.0 (%v)", zeroTemp["close"], unitTemp["close"])
		}
	})
}

func TestSimulateRegion(t *testing.T) {
	parties := DefaultParties()
	for _, r := range DefaultRegions() {
		t.Run(r.ID, func(t *testing.T) {
			votes, seats := SimulateRegion(r, parties, 0.85)

			totalVotes := 0
			for _, v := range votes {
				totalVotes += v
			}
			if totalVotes <= 0 {
				t.Errorf("expected positive votes for %s, got %d", r.ID, totalVotes)
			}

			totalSeats := 0
			for _, s := range seats {
				totalSeats += s
			}
			if totalSeats != r.Seats {
				t.Errorf("allocated %d seats for %s, want %d", totalSeats, r.ID, r.Seats)
			}
		})
	}
}
