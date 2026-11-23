package sim

import "math"

// Axis indexes a dimension in PolicyVec.
type Axis int

const (
	AxisEcon Axis = iota
	AxisSocial
	AxisEU
	AxisDefense
	AxisEnv
	AxisMigration
	NumAxes
)

// PolicyVec holds a political position across all policy axes, typically in [-1, 1].
type PolicyVec [NumAxes]float64

// WeightedDistance returns the Euclidean distance between p and other,
// scaling each squared axis difference by the corresponding entry in weights.
func (p PolicyVec) WeightedDistance(other, weights PolicyVec) float64 {
	var sum float64
	for i := range p {
		diff := p[i] - other[i]
		sum += weights[i] * diff * diff
	}
	return math.Sqrt(sum)
}
