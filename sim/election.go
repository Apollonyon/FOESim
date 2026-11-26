package sim

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"time"
)

// PrecinctReport holds a full or partial vote batch and metadata for a reporting precinct.
type PrecinctReport struct {
	RegionID       string         `json:"region_id"`
	DistrictID     string         `json:"district_id,omitempty"`
	PrecinctID     string         `json:"precinct_id"`
	PrecinctName   string         `json:"precinct_name"`
	UrbanType      int            `json:"urban_type,omitempty"`
	Precinct       int            `json:"precinct"`
	TotalPrecincts int            `json:"total_precincts"`
	ReportedPct    float64        `json:"reported_pct"` // cumulative fraction reported for this precinct in (0, 1]
	EligibleVoters int            `json:"eligible_voters"`
	Votes          map[string]int `json:"votes"`
}

// ElectionConfig controls parameters for a live election-night simulation.
type ElectionConfig struct {
	Temperature    float64
	Swing          float64
	Tactical       float64
	NationalShift  map[string]float64
	Seed           int64
	Waves          int           // partial reporting waves per precinct (<= 1 emits one 100% report)
	BaseDelay      time.Duration // per-report delay when TargetDuration is 0
	TargetDuration time.Duration // optional total stream duration per region (scales cleanly for NUTS-3)
}

type blocShock struct {
	policyShift PolicyVec
	partyAppeal map[string]float64
	turnoutBias float64
}

type nationalRunShock struct {
	policyShift PolicyVec
	partyAppeal map[string]float64
	turnoutTilt float64
	blocShocks  map[string]blocShock
}

// SimulatePrecincts splits r into reporting batches with a seeded hierarchical swing
// (country-level policy & party-valence shocks, NUTS-1 regional tilts, and local county noise).
// Unlike StreamElection, it does not apply a cross-region shared nationalRunShock.
// When r.UseNUTS3 is true and embedded Eurostat NUTS-3 units exist for r.ID,
// those units (with urban/rural demographic tilt) are simulated instead of synthetic batches.
func SimulatePrecincts(r Region, parties []Party, temperature, swing float64, seed int64) []PrecinctReport {
	return simulatePrecincts(r, parties, temperature, swing, 0, seed, nationalRunShock{}, nil)
}

// PreparedElection holds concurrently simulated region reports and correlated
// exit-poll projections ready to stream without recomputing precinct votes.
type PreparedElection struct {
	regions             []Region
	cfg                 ElectionConfig
	perRegion           [][]PrecinctReport
	exitSeats           map[string]map[string]int
	exitDistrictWinners map[string]map[string]string
}

// PrepareElection simulates all regions concurrently once and derives correlated
// exit-poll seat projections, allowing callers to inspect ExitPollSeats and
// stream the same precinct reports via Stream without simulating twice.
func PrepareElection(regions []Region, parties []Party, cfg ElectionConfig) *PreparedElection {
	shockKeys := collectNationalShockKeys(regions, parties)
	blocs := collectBlocs(regions)
	shock := computeNationalShock(cfg.Seed, cfg.Swing, shockKeys, blocs)
	pollSigma := 0.0
	if cfg.Swing > 0 {
		pollSigma = math.Min(0.06, 0.02+cfg.Swing*0.20)
	}

	perRegion := make([][]PrecinctReport, len(regions))
	exitByIdx := make([]map[string]int, len(regions))
	exitWinnersByIdx := make([]map[string]string, len(regions))
	var wg sync.WaitGroup

	for ri, reg := range regions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reports := simulatePrecincts(reg, parties, cfg.Temperature, cfg.Swing, cfg.Tactical, cfg.Seed+int64(ri), shock, cfg.NationalShift)
			perRegion[ri] = reports
			exitByIdx[ri], exitWinnersByIdx[ri] = computeExitPollRegionSeats(reg, reports, cfg.Seed, ri, pollSigma, parties)
		}()
	}
	wg.Wait()

	exitSeats := make(map[string]map[string]int, len(regions))
	exitDistrictWinners := make(map[string]map[string]string)
	for ri, reg := range regions {
		exitSeats[reg.ID] = exitByIdx[ri]
		if len(exitWinnersByIdx[ri]) > 0 {
			exitDistrictWinners[reg.ID] = exitWinnersByIdx[ri]
		}
	}

	return &PreparedElection{
		regions:             slices.Clone(regions),
		cfg:                 cfg,
		perRegion:           perRegion,
		exitSeats:           exitSeats,
		exitDistrictWinners: exitDistrictWinners,
	}
}

// ExitPollSeats returns a copy of the precomputed exit-poll seat projection by region.
func (p *PreparedElection) ExitPollSeats() map[string]map[string]int {
	out := make(map[string]map[string]int, len(p.exitSeats))
	for rid, seats := range p.exitSeats {
		out[rid] = maps.Clone(seats)
	}
	return out
}

// ExitPollDistrictWinners returns a copy of the precomputed exit-poll district
// winners for two-tier regions (regionID -> districtID -> partyID).
func (p *PreparedElection) ExitPollDistrictWinners() map[string]map[string]string {
	if len(p.exitDistrictWinners) == 0 {
		return nil
	}
	out := make(map[string]map[string]string, len(p.exitDistrictWinners))
	for rid, dw := range p.exitDistrictWinners {
		out[rid] = maps.Clone(dw)
	}
	return out
}

// Stream emits the precomputed precinct reports concurrently across regions,
// closing the returned channel when all regions finish or ctx is canceled.
func (p *PreparedElection) Stream(ctx context.Context) <-chan PrecinctReport {
	out := make(chan PrecinctReport, max(1, len(p.regions)*4))
	var wg sync.WaitGroup
	minClose, closeSpan := pollCloseWindow(p.regions)

	for ri, r := range p.regions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			regionDuration, ok := waitStartOffset(ctx, r, p.cfg.TargetDuration, minClose, closeSpan)
			if !ok {
				return
			}
			streamRegionReports(ctx, out, ri, r, slices.Clone(p.perRegion[ri]), p.cfg, regionDuration)
		}()
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

// ExitPollSeats computes an initial per-region seat projection correlated with
// the exact same pan-European and national shocks that StreamElection will stream
// for cfg. When cfg.Swing > 0, realistic small exit-poll sampling noise is added
// on top of the true regional vote totals; when cfg.Swing == 0, it matches exactly.
func ExitPollSeats(regions []Region, parties []Party, cfg ElectionConfig) map[string]map[string]int {
	return PrepareElection(regions, parties, cfg).ExitPollSeats()
}

func aggregateReportVotes(reports []PrecinctReport, hasDistricts bool) (map[string]int, map[string]map[string]int) {
	votes := make(map[string]int)
	var districtVotes map[string]map[string]int
	if hasDistricts {
		districtVotes = make(map[string]map[string]int)
	}
	for _, rep := range reports {
		for pid, v := range rep.Votes {
			votes[pid] += v
		}
		if hasDistricts && rep.DistrictID != "" {
			if districtVotes[rep.DistrictID] == nil {
				districtVotes[rep.DistrictID] = make(map[string]int, len(rep.Votes))
			}
			for pid, v := range rep.Votes {
				districtVotes[rep.DistrictID][pid] += v
			}
		}
	}
	return votes, districtVotes
}

func computeExitPollRegionSeats(reg Region, reports []PrecinctReport, seed int64, ri int, pollSigma float64, parties []Party) (map[string]int, map[string]string) {
	votes, districtVotes := aggregateReportVotes(reports, reg.DistrictSeats > 0)
	if pollSigma > 0 {
		rng := rand.New(rand.NewPCG(uint64(seed)^0x51a7, uint64(ri+1)))
		mults := make(map[string]float64, len(votes))
		for _, pid := range slices.Sorted(maps.Keys(votes)) {
			mult := math.Max(0.5, 1.0+rng.NormFloat64()*pollSigma)
			mults[pid] = mult
			votes[pid] = int(math.Round(float64(votes[pid]) * mult))
		}
		for _, dv := range districtVotes {
			for pid, v := range dv {
				dv[pid] = int(math.Round(float64(v) * mults[pid]))
			}
		}
	}
	return AllocateRegionSeats(votes, districtVotes, reg, reg.EffectiveParties(parties))
}

func nationalShockKey(p Party) string {
	if p.GroupID != "" {
		return p.GroupID
	}
	return p.ID
}

func collectNationalShockKeys(regions []Region, topParties []Party) []string {
	seen := make(map[string]struct{})
	for _, p := range topParties {
		if k := nationalShockKey(p); k != "" {
			seen[k] = struct{}{}
		}
	}
	for _, r := range regions {
		for _, p := range r.Parties {
			if k := nationalShockKey(p); k != "" {
				seen[k] = struct{}{}
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

const studentTNormScale = 1.2909944487358056 // math.Sqrt(5.0 / 3.0)

// unitStudentT returns a Student-t(df=5) draw normalized to unit variance and
// clamped to [-4.5, 4.5], producing realistic heavy-tailed election shocks.
func unitStudentT(rng *rand.Rand) float64 {
	z := rng.NormFloat64()
	chi2 := 0.0
	for range 5 {
		x := rng.NormFloat64()
		chi2 += x * x
	}
	if chi2 < 1e-9 {
		chi2 = 1e-9
	}
	t := z / math.Sqrt(chi2/5.0) / studentTNormScale
	return math.Max(-4.5, math.Min(4.5, t))
}

func collectBlocs(regions []Region) []string {
	seen := make(map[string]struct{})
	for _, r := range regions {
		if r.Bloc != "" {
			seen[r.Bloc] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(seen))
}

func unitAverageTurnout(units []PrecinctUnit) float64 {
	totalW := 0.0
	weighted := 0.0
	for _, u := range units {
		if u.Turnout > 0 && u.Weight > 0 {
			totalW += u.Weight
			weighted += u.Weight * u.Turnout
		}
	}
	if totalW <= 0 {
		return 0
	}
	return weighted / totalW
}

func computeNationalShock(seed int64, swing float64, keys, blocs []string) nationalRunShock {
	if swing <= 0 {
		return nationalRunShock{}
	}
	rng := rand.New(rand.NewPCG(uint64(seed)^0x9e3779b97f4a7c15, 0x6a09e667f3bcc908))
	var policyShift PolicyVec
	sigma := swing * 0.40
	for ax := range policyShift {
		policyShift[ax] = unitStudentT(rng) * sigma
	}
	partyAppeal := make(map[string]float64, len(keys))
	appealSigma := swing * 0.45
	for _, k := range keys {
		partyAppeal[k] = unitStudentT(rng) * appealSigma
	}
	turnoutTilt := unitStudentT(rng) * math.Min(0.08, swing*0.35)

	var blocShocks map[string]blocShock
	if len(blocs) > 0 {
		blocShocks = make(map[string]blocShock, len(blocs))
		bPolicySigma := swing * 0.25
		bAppealSigma := swing * 0.28
		bTurnoutSigma := math.Min(0.02, swing*0.10)
		for _, b := range blocs {
			var bs blocShock
			for ax := range bs.policyShift {
				bs.policyShift[ax] = unitStudentT(rng) * bPolicySigma
			}
			bs.partyAppeal = make(map[string]float64, len(keys))
			for _, k := range keys {
				bs.partyAppeal[k] = unitStudentT(rng) * bAppealSigma
			}
			bs.turnoutBias = unitStudentT(rng) * bTurnoutSigma
			blocShocks[b] = bs
		}
	}

	return nationalRunShock{
		policyShift: policyShift,
		partyAppeal: partyAppeal,
		turnoutTilt: turnoutTilt,
		blocShocks:  blocShocks,
	}
}

func applyNationalAppealShifts(parties []Party, bloc string, shock nationalRunShock, nationalShift map[string]float64, swing float64) []Party {
	hasShift := len(nationalShift) > 0
	hasShock := swing > 0 && len(shock.partyAppeal) > 0
	bShock, hasBlocShock := shock.blocShocks[bloc]
	hasBlocAppeal := swing > 0 && hasBlocShock && len(bShock.partyAppeal) > 0
	if !hasShift && !hasShock && !hasBlocAppeal && swing <= 0 {
		return parties
	}
	out := slices.Clone(parties)
	for i := range out {
		if hasShift {
			if shift, ok := nationalShift[out[i].ID]; ok {
				if !isNonFinite(shift) {
					out[i].Appeal += shift
				}
			} else if out[i].GroupID != "" {
				if shift, ok := nationalShift[out[i].GroupID]; ok && !isNonFinite(shift) {
					out[i].Appeal += shift
				}
			}
		}
		if hasShock {
			out[i].Appeal += shock.partyAppeal[nationalShockKey(out[i])]
		}
		if hasBlocAppeal {
			out[i].Appeal += bShock.partyAppeal[nationalShockKey(out[i])]
		}
	}
	return out
}

func simulatePrecincts(r Region, parties []Party, temperature, swing, tactical float64, seed int64, shock nationalRunShock, nationalShift map[string]float64) []PrecinctReport {
	parties = applyPartyBonus(r.EffectiveParties(parties), r.PartyBonus)
	parties = applyNationalAppealShifts(parties, r.Bloc, shock, nationalShift, swing)
	units := resolveUnits(r)
	n := len(units)

	rng := rand.New(rand.NewPCG(uint64(seed), 0))
	bShock := shock.blocShocks[r.Bloc]

	// Hierarchical Level 2: country/region policy shift, party valence shock, and differential turnout.
	var countryShift PolicyVec
	countryTurnoutNoise := 0.0
	groupTurnoutMult := make([]float64, len(r.Groups))
	for gi := range groupTurnoutMult {
		groupTurnoutMult[gi] = 1.0
	}
	if swing > 0 {
		policySigma := swing * 0.50
		for ax := range countryShift {
			countryShift[ax] = rng.NormFloat64() * policySigma
		}
		appealSigma := swing * 0.45
		for i := range parties {
			parties[i].Appeal += rng.NormFloat64() * appealSigma
		}
		countryTurnoutNoise = rng.NormFloat64() * math.Min(0.03, swing*0.18)

		countryTurnoutTilt := rng.NormFloat64() * math.Min(0.06, swing*0.25)
		totalTilt := shock.turnoutTilt + countryTurnoutTilt
		groupSigma := math.Min(0.06, swing*0.25)
		for gi, g := range r.Groups {
			lean := (g.Ideal[AxisEcon] + g.Ideal[AxisSocial] - g.Ideal[AxisEnv] + g.Ideal[AxisMigration]) * 0.25
			m := 1.0 + totalTilt*lean + rng.NormFloat64()*groupSigma
			groupTurnoutMult[gi] = math.Max(0.78, math.Min(1.22, m))
		}
	}

	// Precompute per-group party vote shares under combined national + bloc + region policy shift.
	groupShares := make([]map[string]float64, len(r.Groups))
	for gi, g := range r.Groups {
		if swing > 0 {
			for ax := range g.Ideal {
				g.Ideal[ax] = math.Max(-1.0, math.Min(1.0, g.Ideal[ax]+shock.policyShift[ax]+bShock.policyShift[ax]+countryShift[ax]))
			}
		}
		groupShares[gi] = VoteShares(g, parties, temperature)
	}

	// Hierarchical Level 3a: NUTS-1 regional (district) demographic shocks in deterministic order.
	districtShocks := make(map[string][]float64)
	if swing > 0 {
		districtSet := make(map[string]struct{})
		for _, u := range units {
			if u.DistrictID != "" {
				districtSet[u.DistrictID] = struct{}{}
			}
		}
		districtSigma := swing * 0.35
		for _, did := range slices.Sorted(maps.Keys(districtSet)) {
			shocks := make([]float64, len(r.Groups))
			for gi := range shocks {
				shocks[gi] = rng.NormFloat64() * districtSigma
			}
			districtShocks[did] = shocks
		}
	}

	reports := make([]PrecinctReport, 0, n)

	totalPop := float64(r.TotalPopulation())
	unitBaseTurnout := unitAverageTurnout(units)
	for i, u := range units {
		votes := make(map[string]int, len(parties))
		eligibleVoters := 0

		// Normalize tilts and swing so each county keeps its Eurostat electorate size.
		tiltedSum := 0.0
		tilts := make([]float64, len(r.Groups))
		dShock := districtShocks[u.DistrictID]
		for gi, g := range r.Groups {
			factor := 1.0
			if swing > 0 {
				local := rng.NormFloat64() * (swing * 0.65)
				regional := 0.0
				if gi < len(dShock) {
					regional = dShock[gi]
				}
				factor = math.Max(0.1, 1.0+regional+local)
			}
			tilts[gi] = countyTilt(g, u) * r.unitTilt(u.ID, u.DistrictID, g.ID) * factor
			tiltedSum += float64(g.Population) * tilts[gi]
		}
		norm := 1.0
		if tiltedSum > 0 && totalPop > 0 {
			norm = totalPop / tiltedSum
		}
		turnoutNoise := 1.0
		if swing > 0 {
			localTurnout := rng.NormFloat64() * math.Min(0.03, swing*0.18)
			turnoutNoise = math.Max(0.75, math.Min(1.25, 1.0+bShock.turnoutBias+countryTurnoutNoise+localTurnout))
		}
		unitTurnoutMult := 1.0
		if u.Turnout > 0 && unitBaseTurnout > 0 {
			unitTurnoutMult = u.Turnout / unitBaseTurnout
		}

		for gi, g := range r.Groups {
			pop := float64(g.Population) * tilts[gi] * norm * u.Weight
			eligibleVoters += int(math.Round(pop))

			effTurnout := math.Max(0.0, math.Min(1.0, g.Turnout*unitTurnoutMult*turnoutNoise*groupTurnoutMult[gi]))
			voters := pop * effTurnout
			for partyID, share := range groupShares[gi] {
				votes[partyID] += int(math.Round(voters * share))
			}
		}

		reports = append(reports, PrecinctReport{
			RegionID:       r.ID,
			DistrictID:     u.DistrictID,
			PrecinctID:     u.ID,
			PrecinctName:   u.Name,
			UrbanType:      u.UrbanType,
			Precinct:       i + 1,
			TotalPrecincts: n,
			ReportedPct:    1.0,
			EligibleVoters: eligibleVoters,
			Votes:          votes,
		})
	}

	if tactical > 0 {
		applyTacticalToReports(reports, r, parties, tactical)
	}

	return reports
}

func resolveUnits(r Region) []PrecinctUnit {
	if pack := r.EffectiveGeoPack(); pack != "" {
		if units := GeoPackUnits(pack, r.ID); len(units) > 0 {
			return units
		}
	}
	n := r.Precincts
	if n <= 0 {
		n = 1
	}
	w := 1.0 / float64(n)
	units := make([]PrecinctUnit, n)
	for i := range n {
		units[i] = PrecinctUnit{
			ID:     fmt.Sprintf("%s-%d", r.ID, i+1),
			Name:   fmt.Sprintf("Precinct %d", i+1),
			Weight: w,
		}
	}
	return units
}

type scheduledReport struct {
	at     float64 // normalized timeline position in [0, 1]
	report PrecinctReport
}

func expandIntoWaves(full []PrecinctReport, w int, rng *rand.Rand) []scheduledReport {
	out := make([]scheduledReport, 0, len(full)*w)
	for _, rep := range full {
		fracs := make([]float64, w)
		sumF := 0.0
		for i := range w {
			fracs[i] = 0.6 + rng.Float64()*0.8
			sumF += fracs[i]
		}
		for i := range w {
			fracs[i] /= sumF
		}

		// Sort keys before drawing from rng so wave skews stay deterministic.
		partySkew := make(map[string]float64, len(rep.Votes))
		for _, pid := range slices.Sorted(maps.Keys(rep.Votes)) {
			partySkew[pid] = (rng.Float64() - 0.5) * 0.35
		}

		emitted := make(map[string]int, len(rep.Votes))
		cumPct := 0.0
		t := rng.Float64() * (0.55 / float64(w))
		stepWindow := (1.0 - t) / float64(max(1, w-1))

		for wi := range w {
			isLast := wi == w-1
			cumPct += fracs[wi]
			if isLast {
				cumPct = 1.0
			}

			waveVotes := make(map[string]int, len(rep.Votes))
			waveDir := -1.0 + 2.0*(float64(wi)/float64(max(1, w-1)))
			for pid, totalV := range rep.Votes {
				if isLast {
					waveVotes[pid] = totalV - emitted[pid]
				} else {
					mult := math.Max(0.3, 1.0+partySkew[pid]*waveDir)
					v := int(math.Round(float64(totalV) * fracs[wi] * mult))
					if rem := totalV - emitted[pid]; v > rem {
						v = rem
					}
					if v < 0 {
						v = 0
					}
					waveVotes[pid] = v
					emitted[pid] += v
				}
			}

			if wi > 0 {
				t = math.Min(1.0, t+stepWindow*(0.45+rng.Float64()*0.75))
			}

			waveRep := rep
			waveRep.ReportedPct = math.Round(cumPct*1000) / 1000
			waveRep.Votes = waveVotes
			out = append(out, scheduledReport{at: t, report: waveRep})
		}
	}

	slices.SortStableFunc(out, func(a, b scheduledReport) int {
		if c := cmp.Compare(a.at, b.at); c != 0 {
			return c
		}
		return cmp.Compare(a.report.ReportedPct, b.report.ReportedPct)
	})
	return out
}

// StreamElection simulates all regions concurrently and streams precinct reports
// on the returned channel, closing it when all regions finish or ctx is canceled.
func StreamElection(ctx context.Context, regions []Region, parties []Party, cfg ElectionConfig) <-chan PrecinctReport {
	out := make(chan PrecinctReport, max(1, len(regions)*4))
	var wg sync.WaitGroup
	minClose, closeSpan := pollCloseWindow(regions)
	shockKeys := collectNationalShockKeys(regions, parties)
	blocs := collectBlocs(regions)
	shock := computeNationalShock(cfg.Seed, cfg.Swing, shockKeys, blocs)

	for ri, r := range regions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			regionDuration, ok := waitStartOffset(ctx, r, cfg.TargetDuration, minClose, closeSpan)
			if !ok {
				return
			}
			reports := simulatePrecincts(r, parties, cfg.Temperature, cfg.Swing, cfg.Tactical, cfg.Seed+int64(ri), shock, cfg.NationalShift)
			streamRegionReports(ctx, out, ri, r, reports, cfg, regionDuration)
		}()
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func pollCloseWindow(regions []Region) (minClose, closeSpan float64) {
	minClose = math.Inf(1)
	maxClose := math.Inf(-1)
	for _, r := range regions {
		if r.PollCloseHour > 0 {
			if r.PollCloseHour < minClose {
				minClose = r.PollCloseHour
			}
			if r.PollCloseHour > maxClose {
				maxClose = r.PollCloseHour
			}
		}
	}
	if !math.IsInf(minClose, 1) && maxClose > minClose {
		closeSpan = maxClose - minClose
	}
	return minClose, closeSpan
}

func waitStartOffset(ctx context.Context, r Region, targetDuration time.Duration, minClose, closeSpan float64) (time.Duration, bool) {
	// Stagger poll-closing start across the first 30% of TargetDuration.
	startOffset := time.Duration(0)
	if targetDuration > 0 && closeSpan > 0 && r.PollCloseHour > minClose {
		frac := (r.PollCloseHour - minClose) / closeSpan
		startOffset = time.Duration(frac * 0.30 * float64(targetDuration))
	}
	if startOffset > 0 {
		timer := time.NewTimer(startOffset)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0, false
		case <-timer.C:
		}
	}

	regionDuration := targetDuration - startOffset
	if regionDuration < 0 {
		regionDuration = 0
	}
	return regionDuration, true
}

func streamRegionReports(ctx context.Context, out chan<- PrecinctReport, ri int, r Region, reports []PrecinctReport, cfg ElectionConfig, regionDuration time.Duration) {
	rng := rand.New(rand.NewPCG(uint64(cfg.Seed), uint64(ri+1)))

	if cfg.Waves > 1 {
		schedule := expandIntoWaves(reports, cfg.Waves, rng)
		prevAt := 0.0
		for _, item := range schedule {
			var delay time.Duration
			if regionDuration > 0 {
				delta := math.Max(0, item.at-prevAt)
				delay = time.Duration(delta * float64(regionDuration))
				prevAt = item.at
			} else if cfg.BaseDelay > 0 {
				delay = time.Duration(float64(cfg.BaseDelay) * (0.5 + rng.Float64()))
			}
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			select {
			case <-ctx.Done():
				return
			case out <- item.report:
			}
		}
		return
	}

	if r.EffectiveGeoPack() != "" && len(reports) > 1 {
		rng.Shuffle(len(reports), func(i, j int) {
			reports[i], reports[j] = reports[j], reports[i]
		})
		for idx := range reports {
			reports[idx].Precinct = idx + 1
		}
	}

	stepDelay := cfg.BaseDelay
	if regionDuration > 0 && len(reports) > 0 {
		// Floor denominator at 3 so 1-2 county states (LU, CY, MT) don't overrun TargetDuration.
		stepDelay = regionDuration / time.Duration(max(3, len(reports)))
	}

	for _, report := range reports {
		if stepDelay > 0 {
			delay := time.Duration(float64(stepDelay) * (0.5 + rng.Float64()))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}

		select {
		case <-ctx.Done():
			return
		case out <- report:
		}
	}
}
