package sim

// DefaultParties returns a starter pan-European party roster spanning the
// 5-axis policy space (Econ, Social, EU, Defense, Env).
func DefaultParties() []Party {
	return []Party{
		{
			ID:        "left",
			Name:      "The Left",
			ShortName: "LEFT",
			Color:     "#B71C1C",
			Position:  PolicyVec{AxisEcon: -0.85, AxisSocial: -0.75, AxisEU: -0.20, AxisDefense: -0.75, AxisEnv: 0.65},
			Appeal:    -0.15,
		},
		{
			ID:        "sd",
			Name:      "Socialists & Democrats",
			ShortName: "S&D",
			Color:     "#E53935",
			Position:  PolicyVec{AxisEcon: -0.45, AxisSocial: -0.45, AxisEU: 0.55, AxisDefense: 0.15, AxisEnv: 0.45},
			Appeal:    0.20,
		},
		{
			ID:        "greens",
			Name:      "Greens/EFA",
			ShortName: "GREENS",
			Color:     "#43A047",
			Position:  PolicyVec{AxisEcon: -0.35, AxisSocial: -0.80, AxisEU: 0.75, AxisDefense: 0.05, AxisEnv: 0.95},
			Appeal:    0.05,
		},
		{
			ID:        "volt",
			Name:      "Volt Europa",
			ShortName: "VOLT",
			Color:     "#6A1B9A",
			Position:  PolicyVec{AxisEcon: 0.10, AxisSocial: -0.75, AxisEU: 0.95, AxisDefense: 0.45, AxisEnv: 0.75},
			Appeal:    -0.55,
		},
		{
			ID:        "renew",
			Name:      "Renew Europe",
			ShortName: "RENEW",
			Color:     "#FBC02D",
			Position:  PolicyVec{AxisEcon: 0.45, AxisSocial: -0.35, AxisEU: 0.80, AxisDefense: 0.45, AxisEnv: 0.25},
			Appeal:    0.10,
		},
		{
			ID:        "epp",
			Name:      "European People's Party",
			ShortName: "EPP",
			Color:     "#1E88E5",
			Position:  PolicyVec{AxisEcon: 0.45, AxisSocial: 0.30, AxisEU: 0.50, AxisDefense: 0.60, AxisEnv: -0.05},
			Appeal:    0.35,
		},
		{
			ID:        "ecr",
			Name:      "European Conservatives & Reformists",
			ShortName: "ECR",
			Color:     "#1565C0",
			Position:  PolicyVec{AxisEcon: 0.55, AxisSocial: 0.70, AxisEU: -0.40, AxisDefense: 0.70, AxisEnv: -0.45},
			Appeal:    0.10,
		},
		{
			ID:        "pfe",
			Name:      "Patriots for Europe",
			ShortName: "PfE",
			Color:     "#0D47A1",
			Position:  PolicyVec{AxisEcon: 0.10, AxisSocial: 0.90, AxisEU: -0.85, AxisDefense: -0.35, AxisEnv: -0.75},
			Appeal:    0.15,
		},
	}
}

// DefaultRegions returns three starter EU member states with different
// seat-allocation methods and thresholds.
func DefaultRegions() []Region {
	return []Region{
		{
			ID:        "de",
			Name:      "Germany",
			Seats:     96,
			Precincts: 16,
			Method:    MethodSainteLague,
			Threshold: 0.0,
			Groups: []VoterGroup{
				{
					Name:       "Urban Youth",
					Population: 11_000_000,
					Turnout:    0.62,
					Ideal:      PolicyVec{AxisEcon: -0.35, AxisSocial: -0.75, AxisEU: 0.75, AxisDefense: 0.10, AxisEnv: 0.85},
					Weights:    PolicyVec{AxisEcon: 1.0, AxisSocial: 1.4, AxisEU: 1.3, AxisDefense: 0.7, AxisEnv: 1.8},
				},
				{
					Name:       "Suburban Professionals",
					Population: 18_000_000,
					Turnout:    0.72,
					Ideal:      PolicyVec{AxisEcon: 0.35, AxisSocial: -0.10, AxisEU: 0.60, AxisDefense: 0.50, AxisEnv: 0.20},
					Weights:    PolicyVec{AxisEcon: 1.6, AxisSocial: 0.9, AxisEU: 1.2, AxisDefense: 1.1, AxisEnv: 1.0},
				},
				{
					Name:       "Industrial Workers",
					Population: 16_000_000,
					Turnout:    0.58,
					Ideal:      PolicyVec{AxisEcon: -0.30, AxisSocial: 0.35, AxisEU: -0.25, AxisDefense: -0.10, AxisEnv: -0.40},
					Weights:    PolicyVec{AxisEcon: 1.7, AxisSocial: 1.2, AxisEU: 1.0, AxisDefense: 0.8, AxisEnv: 1.1},
				},
				{
					Name:       "Rural Retirees",
					Population: 16_000_000,
					Turnout:    0.70,
					Ideal:      PolicyVec{AxisEcon: 0.25, AxisSocial: 0.55, AxisEU: 0.15, AxisDefense: 0.35, AxisEnv: -0.25},
					Weights:    PolicyVec{AxisEcon: 1.2, AxisSocial: 1.6, AxisEU: 0.9, AxisDefense: 1.1, AxisEnv: 0.7},
				},
			},
		},
		{
			ID:        "fr",
			Name:      "France",
			Seats:     81,
			Precincts: 14,
			Method:    MethodDHondt,
			Threshold: 0.05,
			Groups: []VoterGroup{
				{
					Name:       "Metro Progressives",
					Population: 12_000_000,
					Turnout:    0.56,
					Ideal:      PolicyVec{AxisEcon: -0.55, AxisSocial: -0.70, AxisEU: 0.45, AxisDefense: 0.05, AxisEnv: 0.75},
					Weights:    PolicyVec{AxisEcon: 1.4, AxisSocial: 1.3, AxisEU: 1.0, AxisDefense: 0.7, AxisEnv: 1.5},
				},
				{
					Name:       "Urban Centrists",
					Population: 11_000_000,
					Turnout:    0.64,
					Ideal:      PolicyVec{AxisEcon: 0.40, AxisSocial: -0.25, AxisEU: 0.75, AxisDefense: 0.55, AxisEnv: 0.15},
					Weights:    PolicyVec{AxisEcon: 1.5, AxisSocial: 0.9, AxisEU: 1.5, AxisDefense: 1.2, AxisEnv: 0.9},
				},
				{
					Name:       "Peri-Urban Voters",
					Population: 15_000_000,
					Turnout:    0.54,
					Ideal:      PolicyVec{AxisEcon: -0.05, AxisSocial: 0.70, AxisEU: -0.65, AxisDefense: -0.10, AxisEnv: -0.60},
					Weights:    PolicyVec{AxisEcon: 1.4, AxisSocial: 1.6, AxisEU: 1.5, AxisDefense: 0.8, AxisEnv: 1.1},
				},
				{
					Name:       "Conservative Seniors",
					Population: 11_000_000,
					Turnout:    0.66,
					Ideal:      PolicyVec{AxisEcon: 0.45, AxisSocial: 0.55, AxisEU: 0.10, AxisDefense: 0.50, AxisEnv: -0.20},
					Weights:    PolicyVec{AxisEcon: 1.3, AxisSocial: 1.5, AxisEU: 1.0, AxisDefense: 1.1, AxisEnv: 0.6},
				},
			},
		},
		{
			ID:        "it",
			Name:      "Italy",
			Seats:     76,
			Precincts: 12,
			Method:    MethodHare,
			Threshold: 0.04,
			Groups: []VoterGroup{
				{
					Name:       "Northern Businesses",
					Population: 13_000_000,
					Turnout:    0.58,
					Ideal:      PolicyVec{AxisEcon: 0.55, AxisSocial: 0.45, AxisEU: -0.10, AxisDefense: 0.45, AxisEnv: -0.35},
					Weights:    PolicyVec{AxisEcon: 1.7, AxisSocial: 1.1, AxisEU: 1.2, AxisDefense: 0.9, AxisEnv: 0.9},
				},
				{
					Name:       "Urban Center-Left",
					Population: 12_000_000,
					Turnout:    0.56,
					Ideal:      PolicyVec{AxisEcon: -0.45, AxisSocial: -0.50, AxisEU: 0.60, AxisDefense: 0.10, AxisEnv: 0.55},
					Weights:    PolicyVec{AxisEcon: 1.3, AxisSocial: 1.3, AxisEU: 1.2, AxisDefense: 0.8, AxisEnv: 1.3},
				},
				{
					Name:       "Southern Households",
					Population: 14_000_000,
					Turnout:    0.45,
					Ideal:      PolicyVec{AxisEcon: -0.25, AxisSocial: 0.50, AxisEU: -0.35, AxisDefense: -0.15, AxisEnv: -0.20},
					Weights:    PolicyVec{AxisEcon: 1.6, AxisSocial: 1.3, AxisEU: 1.2, AxisDefense: 0.8, AxisEnv: 0.7},
				},
				{
					Name:       "Young Graduates",
					Population: 8_000_000,
					Turnout:    0.52,
					Ideal:      PolicyVec{AxisEcon: -0.10, AxisSocial: -0.70, AxisEU: 0.80, AxisDefense: 0.25, AxisEnv: 0.75},
					Weights:    PolicyVec{AxisEcon: 1.2, AxisSocial: 1.3, AxisEU: 1.5, AxisDefense: 0.8, AxisEnv: 1.6},
				},
			},
		},
	}
}
