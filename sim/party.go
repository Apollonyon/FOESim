package sim

// NonInscritsID is the group ID assigned to parties without an EU political group.
const NonInscritsID = "ni"

// Party represents a political party competing in a region or across Europe.
type Party struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ShortName string    `json:"short_name"`
	Color     string    `json:"color"`
	Position  PolicyVec `json:"position"`
	Appeal    float64   `json:"appeal"`
	GroupID   string    `json:"group_id,omitempty"`
}

// EUGroup represents a pan-European political group in the European Parliament.
type EUGroup struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ShortName string    `json:"short_name"`
	Color     string    `json:"color"`
	Position  PolicyVec `json:"position"`
}
