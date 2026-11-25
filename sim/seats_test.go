package sim

import (
	"reflect"
	"testing"
)

func TestAllocateSeats(t *testing.T) {
	votes := map[string]int{
		"a": 100_000,
		"b": 80_000,
		"c": 30_000,
		"d": 20_000,
	}

	tests := []struct {
		name       string
		votes      map[string]int
		totalSeats int
		method     AllocationMethod
		threshold  float64
		want       map[string]int
	}{
		{
			name:       "dhondt favors larger parties",
			votes:      votes,
			totalSeats: 8,
			method:     MethodDHondt,
			threshold:  0,
			want:       map[string]int{"a": 4, "b": 3, "c": 1},
		},
		{
			name:       "sainte-lague gives seat to smaller party",
			votes:      votes,
			totalSeats: 8,
			method:     MethodSainteLague,
			threshold:  0,
			want:       map[string]int{"a": 3, "b": 3, "c": 1, "d": 1},
		},
		{
			name:       "hare quota largest remainder",
			votes:      votes,
			totalSeats: 8,
			method:     MethodHare,
			threshold:  0,
			want:       map[string]int{"a": 3, "b": 3, "c": 1, "d": 1},
		},
		{
			name:       "threshold excludes small party under dhondt",
			votes:      map[string]int{"a": 600, "b": 350, "c": 50},
			totalSeats: 10,
			method:     MethodDHondt,
			threshold:  0.10,
			want:       map[string]int{"a": 6, "b": 4},
		},
		{
			name:       "threshold excludes small party under hare",
			votes:      map[string]int{"a": 600, "b": 350, "c": 50},
			totalSeats: 10,
			method:     MethodHare,
			threshold:  0.10,
			want:       map[string]int{"a": 6, "b": 4},
		},
		{
			name:       "deterministic alphabetical tie-break",
			votes:      map[string]int{"b": 100, "a": 100},
			totalSeats: 1,
			method:     MethodSainteLague,
			threshold:  0,
			want:       map[string]int{"a": 1},
		},
		{
			name:       "zero seats returns empty map",
			votes:      votes,
			totalSeats: 0,
			method:     MethodDHondt,
			threshold:  0,
			want:       map[string]int{},
		},
		{
			name:       "all parties below threshold falls back to all voted parties (hare)",
			votes:      map[string]int{"a": 50, "b": 50},
			totalSeats: 5,
			method:     MethodHare,
			threshold:  0.60,
			want:       map[string]int{"a": 3, "b": 2},
		},
		{
			name:       "all parties below threshold falls back to all voted parties (dhondt)",
			votes:      map[string]int{"a": 50, "b": 50},
			totalSeats: 5,
			method:     MethodDHondt,
			threshold:  0.60,
			want:       map[string]int{"a": 3, "b": 2},
		},
		{
			name:       "all parties below threshold falls back to all voted parties (sainte-lague)",
			votes:      map[string]int{"a": 50, "b": 50},
			totalSeats: 5,
			method:     MethodSainteLague,
			threshold:  0.60,
			want:       map[string]int{"a": 3, "b": 2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AllocateSeats(tc.votes, Region{Seats: tc.totalSeats, Method: tc.method, Threshold: tc.threshold}, nil)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSumSeats(t *testing.T) {
	got := SumSeats(map[string]map[string]int{
		"de": {"epp": 30, "sd": 14},
		"fr": {"epp": 8, "pfe": 30},
	})
	want := map[string]int{"epp": 38, "sd": 14, "pfe": 30}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRollupGroupSeats(t *testing.T) {
	parties := []Party{
		{ID: "de_cdu", GroupID: "epp"},
		{ID: "de_grune", GroupID: "greens"},
		{ID: "de_volt", GroupID: "greens"},
		{ID: "de_bsw", GroupID: ""},
	}
	seats := map[string]int{
		"de_cdu":   29,
		"de_grune": 12,
		"de_volt":  3,
		"de_bsw":   6,
		"unknown":  2,
		"zero":     0,
		"neg":      -1,
	}

	got := RollupGroupSeats(seats, parties)
	want := map[string]int{
		"epp":         29,
		"greens":      15,
		NonInscritsID: 8, // de_bsw (6) + unknown (2)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAllocateFPTPAndSTV(t *testing.T) {
	t.Run("fptp gives all seats to plurality winner regardless of threshold", func(t *testing.T) {
		r := Region{Seats: 12, Method: MethodFPTP, Threshold: 0.50}
		got := AllocateSeats(map[string]int{"a": 35, "b": 40, "c": 25}, r, nil)
		want := map[string]int{"b": 12}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("fptp alphabetical tie-break and zero votes", func(t *testing.T) {
		r := Region{Seats: 5, Method: MethodFPTP}
		if got := AllocateSeats(map[string]int{"b": 100, "a": 100}, r, nil); !reflect.DeepEqual(got, map[string]int{"a": 5}) {
			t.Errorf("tie-break got %v, want a:5", got)
		}
		if got := AllocateSeats(map[string]int{"a": 0, "b": 0}, r, nil); len(got) != 0 {
			t.Errorf("zero votes got %v, want empty", got)
		}
	})

	t.Run("stv transfers eliminated party votes to ideologically nearest party", func(t *testing.T) {
		// 5 seats, 1000 votes -> Droop quota = floor(1000/6)+1 = 167.
		// Initial quotas: left_main=2 (rem 116), right=2 (rem 116), left_small=0 (rem 100).
		// left_small is eliminated and transfers 100 votes to left_main (closer in PolicyVec),
		// pushing left_main to 216 >= 167 to win the 5th seat.
		parties := []Party{
			{ID: "left_main", Position: PolicyVec{-0.6, -0.6, 0.5, 0.0, 0.6, -0.5}},
			{ID: "left_small", Position: PolicyVec{-0.7, -0.7, 0.6, 0.0, 0.8, -0.6}},
			{ID: "right", Position: PolicyVec{0.7, 0.7, -0.5, 0.5, -0.6, 0.8}},
		}
		r := Region{Seats: 5, Method: MethodSTV, Parties: parties}
		votes := map[string]int{"left_main": 450, "right": 450, "left_small": 100}

		// Pass nil parties to also verify stvAllocator resolves r.EffectiveParties(parties).
		got := AllocateSeats(votes, r, nil)
		want := map[string]int{"left_main": 3, "right": 2}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("stv multi-step elimination transfers across four parties", func(t *testing.T) {
		// 4 seats, 1000 votes -> Droop quota = floor(1000/5)+1 = 201.
		// Initial quotas: d=2 (rem 48), a=1 (rem 149), b=0 (90), c=0 (110).
		// Elimination 1: lowest is d (rem 48) -> transfers 48 to nearest b (rem 138).
		// Elimination 2: lowest is c (110) -> transfers 110 to nearest b (248 >= 201 -> b wins 4th seat).
		parties := []Party{
			{ID: "a", Position: PolicyVec{-0.8}},
			{ID: "b", Position: PolicyVec{-0.2}},
			{ID: "c", Position: PolicyVec{-0.4}},
			{ID: "d", Position: PolicyVec{0.9}},
		}
		r := Region{Seats: 4, Method: MethodSTV, Parties: parties}
		votes := map[string]int{"a": 350, "b": 90, "c": 110, "d": 450}

		got := AllocateSeats(votes, r, nil)
		want := map[string]int{"a": 1, "b": 1, "d": 2}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}
