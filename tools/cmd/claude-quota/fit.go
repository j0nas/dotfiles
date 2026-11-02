package main

// Fitting: how much of each window a million tokens of each model costs.

import (
	"fmt"
	"math"
	"sort"
)

var (
	models   = []string{"haiku", "sonnet", "opus", "fable"}
	outcomes = []string{"merged", "fixed", "partial", "discarded"}
)

const (
	fiveCap       = 90.0 // never plan the 5-hour window past this
	priorWeight   = 2.0  // Mtok of evidence each starting guess weighs
	fresh         = 60.0 // seconds a reading stays good enough for start, end and plan
	ratioMinFive  = 30.0 // 5h points observed before the week is derived from the 5h rates
	suspendAt     = fiveCap
	lead          = 20.0 // minutes: pause sooner when the limit is this close
	burnSpan      = 30 * 60
	watchEvery    = 180.0
	waitMargin    = 2.0
	sameReset     = 300.0 // seconds: readings whose reset times are this close are of one window
	defaultTarget = 92.0
)

// Starting guesses, in % of the window per million tokens an agent's notification reports,
// and a typical agent's tokens. Seeded from 2026-10-13: Sonnet agents moved the week ~5
// points over ~2.7M tokens and the 5-hour window ~4× as fast; the other models are scaled by price.
var prior = map[string]map[string]float64{
	"week": {"haiku": 0.5, "sonnet": 1.5, "opus": 3.5, "fable": 3.5},
	"five": {"haiku": 2.0, "sonnet": 6.0, "opus": 14.0, "fable": 14.0},
}

var priorSize = map[string]float64{"haiku": 100_000, "sonnet": 250_000, "opus": 300_000, "fable": 300_000}

func modelIndex(m string) int {
	for i, x := range models {
		if x == m {
			return i
		}
	}
	return -1
}

// lstsq: ordinary least squares by the normal equations (a handful of unknowns); nil if singular.
func lstsq(rows [][]float64, ys []float64) []float64 {
	k := len(rows[0])
	a := make([][]float64, k)
	b := make([]float64, k)
	for i := 0; i < k; i++ {
		a[i] = make([]float64, k)
		for j := 0; j < k; j++ {
			s := 0.0
			for _, r := range rows {
				s += r[i] * r[j]
			}
			a[i][j] = s
		}
		s := 0.0
		for n, r := range rows {
			s += r[i] * ys[n]
		}
		b[i] = s
	}
	for c := 0; c < k; c++ {
		p := c
		for i := c + 1; i < k; i++ {
			if math.Abs(a[i][c]) > math.Abs(a[p][c]) {
				p = i
			}
		}
		if math.Abs(a[p][c]) < 1e-12 {
			return nil
		}
		a[c], a[p] = a[p], a[c]
		b[c], b[p] = b[p], b[c]
		for i := 0; i < k; i++ {
			if i != c {
				f := a[i][c] / a[c][c]
				for j := range a[i] {
					a[i][j] -= f * a[c][j]
				}
				b[i] -= f * b[c]
			}
		}
	}
	x := make([]float64, k)
	for i := range x {
		x[i] = b[i] / a[i][i]
	}
	return x
}

// combinations of n of 0..k-1, in lexicographic order (itertools.combinations).
func combinations(k, n int, f func([]int)) {
	idx := make([]int, n)
	var rec func(pos, from int)
	rec = func(pos, from int) {
		if pos == n {
			f(idx)
			return
		}
		for c := from; c < k; c++ {
			idx[pos] = c
			rec(pos+1, c+1)
		}
	}
	rec(0, 0)
}

// nnls: non-negative least squares by trying every subset of free columns (k is tiny).
func nnls(rows [][]float64, ys []float64) []float64 {
	k := len(rows[0])
	best := make([]float64, k)
	bestErr := 0.0
	for _, y := range ys {
		bestErr += y * y
	}
	for n := 1; n <= k; n++ {
		combinations(k, n, func(cols []int) {
			sub := make([][]float64, len(rows))
			for i, r := range rows {
				sub[i] = make([]float64, n)
				for j, c := range cols {
					sub[i][j] = r[c]
				}
			}
			sol := lstsq(sub, ys)
			if sol == nil {
				return
			}
			for _, v := range sol {
				if v < 0 {
					return
				}
			}
			x := make([]float64, k)
			for j, c := range cols {
				x[c] = sol[j]
			}
			err := 0.0
			for i, r := range rows {
				s := 0.0
				for j := 0; j < k; j++ {
					s += r[j] * x[j]
				}
				d := s - ys[i]
				err += d * d
			}
			if err < bestErr {
				best, bestErr = x, err
			}
		})
	}
	return best
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

type kindKey struct{ model, kind string }

// Model is what learn fits.
type Model struct {
	Rates     map[string]map[string]float64
	Baseline  map[string]float64
	Derived   bool
	Sizes     map[kindKey]float64
	ModelSize map[string]float64
	Outcomes  map[kindKey]map[string]int
	RunHours  float64
	Agents    int
	Evidence  map[string]float64
}

func learn(log []Entry) *Model {
	var snaps []Entry
	for _, e := range log {
		if e.Kind == "snap" {
			snaps = append(snaps, e)
		}
	}
	var ended []*Agent
	for _, a := range agents(log).values() {
		if a.End != 0 && a.AgentTokens != nil && *a.AgentTokens != 0 && modelIndex(a.Model) >= 0 {
			ended = append(ended, a)
		}
	}
	m := &Model{Rates: map[string]map[string]float64{}, Baseline: map[string]float64{}}
	moved := map[string]float64{"week": 0, "five": 0} // points each window rose where neither reset
	for i := 0; i+1 < len(snaps); i++ {
		a, b := snaps[i], snaps[i+1]
		if sameWindow(a, b, "five") && sameWindow(a, b, "week") && !refilled(a, b, "five") && !refilled(a, b, "week") {
			for _, w := range []string{"week", "five"} {
				moved[w] += max(0, b.level(w)-a.level(w))
			}
		}
	}
	for _, window := range []string{"week", "five"} {
		var rows [][]float64
		var ys []float64
		for i := 0; i+1 < len(snaps); i++ {
			a, b := snaps[i], snaps[i+1]
			if !sameWindow(a, b, window) || b.T <= a.T || refilled(a, b, window) {
				continue // the window reset (or was refilled) in between: the delta means nothing
			}
			row := make([]float64, len(models)+1)
			for _, ag := range ended {
				run := max(active(ag, ag.T, ag.End), 1.0)
				overlap := active(ag, a.T, b.T)
				if overlap > 0 {
					row[modelIndex(ag.Model)] += *ag.AgentTokens / 1e6 * overlap / run
				}
			}
			row[len(models)] = (b.T - a.T) / 3600
			rows = append(rows, row)
			ys = append(ys, b.level(window)-a.level(window))
		}
		for i, mo := range models { // the priors, as pseudo-observations
			row := make([]float64, len(models)+1)
			row[i] = priorWeight
			rows = append(rows, row)
			ys = append(ys, prior[window][mo]*priorWeight)
		}
		x := nnls(rows, ys)
		m.Rates[window] = map[string]float64{}
		for i, mo := range models {
			m.Rates[window][mo] = x[i]
		}
		m.Baseline[window] = x[len(x)-1]
	}
	m.Derived = moved["five"] >= ratioMinFive && moved["week"] > 0
	if m.Derived { // the week from the finer-grained 5h rates
		ratio := moved["week"] / moved["five"]
		m.Rates["week"] = map[string]float64{}
		for _, mo := range models {
			m.Rates["week"][mo] = m.Rates["five"][mo] * ratio
		}
		m.Baseline["week"] = m.Baseline["five"] * ratio
	}
	m.Sizes = map[kindKey]float64{}
	m.Outcomes = map[kindKey]map[string]int{}
	byModel := map[string][]float64{}
	byKind := map[kindKey][]float64{}
	for _, ag := range ended {
		key := kindKey{ag.Model, ag.task()}
		byModel[ag.Model] = append(byModel[ag.Model], *ag.AgentTokens)
		byKind[key] = append(byKind[key], *ag.AgentTokens)
		if o := str(ag.AgentOutcome); o != "" && contains(outcomes, o) {
			if m.Outcomes[key] == nil {
				m.Outcomes[key] = map[string]int{}
			}
			m.Outcomes[key][o]++
		}
	}
	for key, toks := range byKind {
		m.Sizes[key] = median(toks)
	}
	m.ModelSize = map[string]float64{}
	m.Evidence = map[string]float64{}
	for _, mo := range models {
		if t := byModel[mo]; len(t) > 0 {
			m.ModelSize[mo] = median(t)
		} else {
			m.ModelSize[mo] = priorSize[mo]
		}
		s := 0.0
		for _, v := range byModel[mo] {
			s += v
		}
		m.Evidence[mo] = s / 1e6
	}
	var runs []float64
	for _, ag := range ended {
		runs = append(runs, active(ag, ag.T, ag.End)/3600)
	}
	m.RunHours = 0.5
	if len(runs) > 0 {
		m.RunHours = median(runs)
	}
	m.Agents = len(ended)
	return m
}

func (m *Model) sizeOf(model, kind string) float64 {
	if kind == "" {
		kind = "-"
	}
	if s := m.Sizes[kindKey{model, kind}]; s != 0 {
		return s
	}
	return m.ModelSize[model]
}

func taskOf(a *Agent) string {
	if t := str(a.Task); t != "" {
		return t
	}
	return ""
}

// inFlight: what the agents running now will still spend, in points of each window: a typical
// agent of its model and kind, less the share of a typical run it has had. One past its typical
// run is still going, so it's counted at a quarter at least.
func (m *Model) inFlight(log []Entry, now float64) (map[string]float64, int) {
	cost := map[string]float64{"week": 0, "five": 0}
	going := running(log)
	for _, a := range going {
		had := active(a, a.T, now) / 3600 / max(m.RunHours, 0.1)
		left := m.sizeOf(a.Model, taskOf(a)) * max(0.25, 1-had)
		for _, w := range []string{"week", "five"} {
			cost[w] += left / 1e6 * m.Rates[w][a.Model]
		}
	}
	return cost, len(going)
}

// started: a window nothing has been used in yet has no reset time; it ends a full window
// from the reading.
func started(s Entry) Entry {
	if s.FiveReset == nil || *s.FiveReset == 0 {
		v := s.T + 5*3600
		s.FiveReset = &v
	}
	if s.WeekReset == nil || *s.WeekReset == 0 {
		v := s.T + 7*86400
		s.WeekReset = &v
	}
	return s
}

// burn: how fast the 5-hour window is filling, in points an hour: the faster of what the last
// half hour of readings shows and what the running agents' fitted rates say (a batch that just
// started hasn't shown in the readings yet).
func (m *Model) burn(log []Entry, s Entry) (float64, string) {
	var recent []Entry
	for _, e := range log {
		if e.Kind == "snap" && 0 <= s.T-e.T && s.T-e.T <= burnSpan {
			if e = started(e); sameWindow(e, s, "five") { // this window only
				recent = append(recent, e)
			}
		}
	}
	seen, haveSeen := 0.0, false
	if len(recent) > 0 && s.T-recent[0].T >= 300 {
		seen, haveSeen = max(0, s.Five-recent[0].Five)/((s.T-recent[0].T)/3600), true
	}
	modelled := m.Baseline["five"]
	for _, a := range running(log) {
		perHour := m.sizeOf(a.Model, taskOf(a)) / max(m.RunHours, 0.1)
		modelled += perHour / 1e6 * m.Rates["five"][a.Model]
	}
	if haveSeen && seen >= modelled {
		return seen, "seen over the last " + hours(s.T-recent[0].T)
	}
	return modelled, "from the fitted rates of what's running"
}

// verdict: ok, hold (start nothing more), suspend (pause the running agents) or limited (the
// window is spent): whether the 5-hour window will reach its limit before it resets.
func (m *Model) verdict(log []Entry, s Entry, at, leadMin float64) (string, string) {
	now := s.T
	toReset := max(0, *s.FiveReset-now)
	if s.Five >= 100 {
		return "limited", "the 5h window is spent until it resets in " + hours(toReset)
	}
	rate, how := m.burn(log, s)
	eta := math.Inf(1)
	if rate > 0 {
		eta = (100 - s.Five) / rate * 3600
	}
	pace := fmt.Sprintf("at %.0f%%, filling ~%.0f pts/h (%s)", s.Five, rate, how)
	if eta >= toReset {
		return "ok", fmt.Sprintf("%s: it resets in %s, before its limit", pace, hours(toReset))
	}
	if s.Five >= at || eta <= leadMin*60 {
		state := "hold"
		if len(running(log)) > 0 {
			state = "suspend"
		}
		return state, fmt.Sprintf("%s: its limit in ~%s, before it resets in %s", pace, hours(eta), hours(toReset))
	}
	return "ok", fmt.Sprintf("%s: its limit in ~%s; suspend at %.0f%% or %.0f min before it", pace, hours(eta), at, leadMin)
}

func sortKinds(keys []kindKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].model != keys[j].model {
			return keys[i].model < keys[j].model
		}
		return keys[i].kind < keys[j].kind
	})
}
