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
