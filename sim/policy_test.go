package sim

import (
	"math"
	"testing"
)

func TestWeightedDistance(t *testing.T) {
	tests := []struct {
		name    string
		a, b    PolicyVec
		weights PolicyVec
		want    float64
	}{
		{
			name:    "identical vectors",
			a:       PolicyVec{0.5, -0.5, 0.2, 0.1, -0.8, 0.3},
			b:       PolicyVec{0.5, -0.5, 0.2, 0.1, -0.8, 0.3},
			weights: PolicyVec{1, 1, 1, 1, 1, 1},
			want:    0.0,
		},
		{
			name:    "unit weights 3-4-5 triangle",
			a:       PolicyVec{AxisEcon: 3, AxisSocial: 4},
			b:       PolicyVec{},
			weights: PolicyVec{1, 1, 1, 1, 1, 1},
			want:    5.0,
		},
		{
			name:    "zero weight ignores differing axis",
			a:       PolicyVec{AxisEnv: 1.0},
			b:       PolicyVec{AxisEnv: -1.0},
			weights: PolicyVec{AxisEcon: 1, AxisSocial: 1, AxisEU: 1, AxisDefense: 1, AxisEnv: 0, AxisMigration: 1},
			want:    0.0,
		},
		{
			name:    "weighted axis scales squared distance",
			a:       PolicyVec{AxisDefense: 1.0},
			b:       PolicyVec{},
			weights: PolicyVec{AxisDefense: 4.0},
			want:    2.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.a.WeightedDistance(tc.b, tc.weights)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
