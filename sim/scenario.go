package sim

import (
	"encoding/json"
	"fmt"
	"os"
)

// Scenario bundles the parties and regions for an election simulation.
type Scenario struct {
	Name    string   `json:"name"`
	Parties []Party  `json:"parties"`
	Regions []Region `json:"regions"`
}

// LoadScenario reads and validates a JSON election scenario from path.
func LoadScenario(path string) (Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, err
	}

	var s Scenario
	if err = json.Unmarshal(data, &s); err != nil {
		return Scenario{}, err
	}
	if err = s.validate(); err != nil {
		return Scenario{}, err
	}
	return s, nil
}

func (s Scenario) validate() error {
	if len(s.Parties) == 0 {
		return fmt.Errorf("scenario %q has no parties", s.Name)
	}
	if len(s.Regions) == 0 {
		return fmt.Errorf("scenario %q has no regions", s.Name)
	}
	for _, r := range s.Regions {
		switch r.Method {
		case "", MethodSainteLague, MethodDHondt, MethodHare:
		default:
			return fmt.Errorf("region %q has unknown allocation method %q", r.ID, r.Method)
		}
	}
	return nil
}
