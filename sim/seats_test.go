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
			name:       "all parties below threshold returns empty map",
			votes:      map[string]int{"a": 50, "b": 50},
			totalSeats: 5,
			method:     MethodHare,
			threshold:  0.60,
			want:       map[string]int{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AllocateSeats(tc.votes, tc.totalSeats, tc.method, tc.threshold)
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
