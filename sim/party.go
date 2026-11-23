package sim

// Party represents a political party competing across regions.
type Party struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ShortName string    `json:"short_name"`
	Color     string    `json:"color"`
	Position  PolicyVec `json:"position"`
	Appeal    float64   `json:"appeal"`
}
