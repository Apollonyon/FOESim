package main

import (
	"fmt"
	"parliamentsim/sim"
)

func main() {
	parties := sim.DefaultParties()
	regions := sim.DefaultRegions()

	totalSeats := make(map[string]int)

	for _, r := range regions {
		_, seats := sim.SimulateRegion(r, parties, 0.85)
		fmt.Printf("=== %s (%s) === \n", r.Name, r.Method)
		for _, p := range parties {
			s := seats[p.ID]
			totalSeats[p.ID] += s
			fmt.Printf("  %-8s %3d\n", p.ShortName, s)
		}
	}
	fmt.Println("\n=== European Parliament Total ===")
	for _, p := range parties {
		fmt.Printf("  %-8s %3d seats\n", p.ShortName, totalSeats[p.ID])
	}

}
