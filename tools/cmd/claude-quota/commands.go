package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

func (a *app) cmdStart(p *parsed) error {
	model, err := modelOf(*p.str("model"))
	if err != nil {
		return err
	}
	if _, err := a.snapshot(fresh); err != nil {
		return err
	}
	id := a.newID("a")
	var task any
	if k := p.str("kind"); k != nil {
		task = *k
	}
	if err := a.appendEntry(map[string]any{"t": a.time(), "kind": "start", "id": id, "label": *p.str("label"), "model": model, "task": task}); err != nil {
		return err
	}
	a.println(id)
	return nil
}

func (a *app) cmdEnd(p *parsed) error {
	outcome := p.str("outcome")
	if outcome != nil && *outcome != "" && !contains(outcomes, *outcome) {
		return die("outcome must be one of ('merged', 'fixed', 'partial', 'discarded')")
	}
	tokens, err := amount(*p.str("tokens"))
	if err != nil {
		return err
	}
	s, err := a.snapshot(fresh)
	if err != nil {
		return err
	}
	id := *p.str("id")
	var oc any
	if outcome != nil {
		oc = *outcome
	}
	if err := a.appendEntry(map[string]any{"t": a.time(), "kind": "end", "id": id, "tokens": tokens, "outcome": oc}); err != nil {
		return err
	}
	log, err := a.entries()
	if err != nil {
		return err
	}
	if ag := agents(log).m[id]; ag != nil {
		a.printf("%s %s: ran %s, %.0fk tokens\n", id, ag.Label, hours(active(ag, ag.T, s.T)), tokens/1e3)
	}
	return nil
}

func (a *app) cmdLearn() error {
	log, err := a.entries()
	if err != nil {
		return err
	}
	m := learn(log)
	src := "fitted directly"
	if m.Derived {
		src = "from the 5h rates × the week/5h ratio"
	}
	a.printf("%d finished agents. %% of each window per Mtok (week %s; priors weigh %.1f Mtok):\n", m.Agents, src, priorWeight)
	for _, mo := range models {
		a.printf("  %-7s week %5.2f   5h %5.2f   typical agent %4.0fk   (logged %.1f Mtok)\n",
			mo, m.Rates["week"][mo], m.Rates["five"][mo], m.ModelSize[mo]/1e3, m.Evidence[mo])
	}
	a.printf("  baseline (main session and other use): week %.2f %%/h, 5h %.2f %%/h\n", m.Baseline["week"], m.Baseline["five"])
	a.printf("  an agent's median run: %.0f min\n", m.RunHours*60)
	if len(m.Sizes) > 0 {
		a.println("by model and kind: median tokens, outcomes (merged/fixed/partial/discarded)")
		keys := make([]kindKey, 0, len(m.Sizes))
		for k := range m.Sizes {
			keys = append(keys, k)
		}
		sortKinds(keys)
		for _, k := range keys {
			tally := "-"
			if o, ok := m.Outcomes[k]; ok {
				parts := make([]string, len(outcomes))
				for i, x := range outcomes {
					parts[i] = strconv.Itoa(o[x])
				}
				tally = strings.Join(parts, "/")
			}
			a.printf("  %s:%s %5.0fk   %s\n", k.model, padTo(k.kind, 12), m.Sizes[k]/1e3, tally)
		}
	}
	return nil
}

// padTo pads like Python's {s:12}: to a width counted in characters.
func padTo(s string, w int) string {
	if n := len([]rune(s)); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func (a *app) cmdPlan(p *parsed) error {
	type want struct {
		model, kind string
		n           float64
	}
	var wants []want
	for _, item := range p.vals["agents"] {
		spec, n, _ := strings.Cut(item, "=")
		model, kind, _ := strings.Cut(spec, ":")
		mo, err := modelOf(model)
		if err != nil {
			return err
		}
		cnt := 1.0
		if n != "" {
			f, ok := pyFloat(n)
			if !ok {
				return die("can't read %s as a count", pyRepr(n))
			}
			cnt = f
		}
		wants = append(wants, want{mo, kind, cnt})
	}
	override := map[string]float64{}
	for _, x := range p.vals["size"] {
		k, v, _ := strings.Cut(x, "=")
		mo, err := modelOf(k)
		if err != nil {
			return err
		}
		f, err := amount(v)
		if err != nil {
			return err
		}
		override[mo] = f
	}
	s, err := a.snapshot(fresh)
	if err != nil {
		return err
	}
	log, err := a.entries()
	if err != nil {
		return err
	}
	m := learn(log)
	target := p.float("target", a.target)
	cost := map[string]float64{"week": 0, "five": 0}
	count := 0.0
	for _, w := range wants {
		size := override[w.model]
		if size == 0 {
			size = m.sizeOf(w.model, w.kind)
		}
		for _, win := range []string{"week", "five"} {
			cost[win] += w.n * size / 1e6 * m.Rates[win][w.model]
		}
		count += w.n
	}
	// The main session (and anything else) keeps spending while the agents run: a parallel
	// batch lasts about one agent's run, a serial one the sum.
	hoursRunning := m.RunHours
	if p.flag("serial") {
		hoursRunning *= count
	}
	for _, win := range []string{"week", "five"} {
		cost[win] += m.Baseline[win] * hoursRunning
	}
	now := s.T
	// The agents already running keep spending too: what's left is what they leave.
	going, nGoing := m.inFlight(log, now)
	// Each unredeemed reset is a full window more. One that refills both counts for the week
	// only: it's worth most redeemed once the week is spent, not to widen the 5-hour window at hand.
	extra := map[string]float64{"week": 0, "five": 0}
	for _, r := range liveResets(log, now) {
		if r.Window == "five" {
			extra["five"] += 100
		} else {
			extra["week"] += 100
		}
	}
	weekLeft := target - s.Week + extra["week"] - going["week"]
	fiveLeft := max(0, fiveCap-s.Five+extra["five"]-going["five"])
	if err := a.showStatus(s, target); err != nil {
		return err
	}
	a.println("")
	if nGoing > 0 {
		pl := ""
		if nGoing > 1 {
			pl = "s"
		}
		a.printf("running %d agent%s still going ≈ %.0f pts more of this 5h window, %.1f of the week: counted as spent below\n", nGoing, pl, going["five"], going["week"])
	}
	items := make([]string, len(wants))
	for i, w := range wants {
		k := ""
		if w.kind != "" {
			k = ":" + w.kind
		}
		items[i] = fmt.Sprintf("%s%s×%.6g", w.model, k, w.n)
	}
	a.printf("to run  %s  ≈ %.1f pts of the week, %.0f pts of a 5h window (incl. ~%.1f h of the main session alongside)\n",
		strings.Join(items, ", "), cost["week"], cost["five"], hoursRunning)
	if cost["week"] > weekLeft {
		a.printf("week    short by %.1f pts: move agents down a model (opus→sonnet→haiku) first, then drop the least valuable items.\n", cost["week"]-weekLeft)
	} else {
		spare := weekLeft - cost["week"]
		a.printf("week    fits with %.1f pts to spare; the week doesn't limit this. Don't pad the work.\n", spare)
		if extra["week"] != 0 {
			a.println("        That counts the unredeemed limit reset: spend the week toward its limit before it expires, then redeem it.")
		}
		if *s.WeekReset-now < 24*3600 && spare > 15 {
			a.println("        The reset is close and the spare is large: put it into quality (a stronger model, a review pass).")
		}
	}
	if cost["five"] <= fiveLeft {
		a.printf("5h      fits in this window (%.0f pts left, resets in %s): run it in parallel.\n", fiveLeft, hours(*s.FiveReset-now))
	} else {
		each := 0.0
		if count != 0 {
			each = cost["five"] / count
		}
		fit := 0
		if each != 0 {
			fit = int(math.Floor(fiveLeft / each))
		}
		a.printf("5h      about %d of %.6g fit in this window (~%.1f pts each, %.0f left); queue the rest for the reset in %s (`claude-quota queue add`, then `claude-quota wait` in the background).\n",
			fit, count, each, fiveLeft, hours(*s.FiveReset-now))
	}
	if count != 0 {
		a.println("        While agents run, keep `claude-quota watch` going in the background: it exits in time to pause them before the window's limit stops them.")
	}
	return nil
}

// reading takes a snapshot and then the verdict on it from the log as it stands after.
func (a *app) reading(at, leadMin float64) (Entry, string, string, error) {
	s, err := a.snapshot(fresh)
	if err != nil {
		return s, "", "", err
	}
	log, err := a.entries()
	if err != nil {
		return s, "", "", err
	}
	state, why := learn(log).verdict(log, s, at, leadMin)
	return s, state, why, nil
}

func (a *app) cmdGuard(p *parsed) error {
	s, state, why, err := a.reading(p.float("at", suspendAt), p.float("lead", lead))
	if err != nil {
		return err
	}
	a.printf("%s: %s\n", state, why)
	if state != "ok" {
		a.println(advise(state, s))
	}
	return nil
}

// cmdWatch reads the usage every few minutes while agents run, and exits (waking the session
// that started it in the background) once they should be paused, or once none are running.
func (a *app) cmdWatch(p *parsed) error {
	every := p.float("every", watchEvery)
	for {
		s, state, why, err := a.reading(p.float("at", suspendAt), p.float("lead", lead))
		if err != nil {
			return err
		}
		if state != "ok" {
			a.printf("%s (%s): %s\n%s\n", state, atClock(s.T), why, advise(state, s))
			return nil
		}
		log, err := a.entries()
		if err != nil {
			return err
		}
		if len(running(log)) == 0 {
			a.printf("no agents running (%s): watch ends. %s\n", atClock(s.T), why)
			return nil
		}
		a.sleep(time.Duration(every * float64(time.Second)))
	}
}

func (a *app) cmdSuspend(p *parsed) error {
	log, err := a.entries()
	if err != nil {
		return err
	}
	id := *p.str("id")
	ag := agents(log).m[id]
	if ag == nil || ag.End != 0 {
		return die("no open agent %s; `claude-quota status` lists them", pyRepr(id))
	}
	if suspended(ag) {
		return die("%s is already suspended", id)
	}
	return a.appendEntry(map[string]any{"t": a.time(), "kind": "suspend", "id": id, "ref": nullable(p.str("ref")), "note": nullable(p.str("note"))})
}

func nullable(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func (a *app) cmdQueueAdd(p *parsed) error {
	model, kind, _ := strings.Cut(*p.str("spec"), ":")
	mo, err := modelOf(model)
	if err != nil {
		return err
	}
	var task any
	if kind != "" {
		task = kind
	}
	id := a.newID("q")
	if err := a.appendEntry(map[string]any{"t": a.time(), "kind": "queue", "id": id, "label": *p.str("label"), "model": mo, "task": task, "note": nullable(p.str("note"))}); err != nil {
		return err
	}
	a.println(id)
	return nil
}

func (a *app) cmdQueueDrop(p *parsed) error {
	log, err := a.entries()
	if err != nil {
		return err
	}
	id := *p.str("id")
	found := false
	for _, q := range queued(log) {
		found = found || q.ID == id
	}
	if !found {
		return die("nothing queued as %s; `claude-quota queue` lists it", pyRepr(id))
	}
	return a.appendEntry(map[string]any{"t": a.time(), "kind": "drop", "id": id})
}

func (a *app) cmdQueueList() error {
	log, err := a.entries()
	if err != nil {
		return err
	}
	for _, q := range queued(log) {
		line := fmt.Sprintf("%s  %s:%s  %s", q.ID, q.Model, q.task(), q.Label)
		if n := str(q.Note); n != "" {
			line += "  — " + n
		}
		a.println(line)
	}
	return nil
}

// handOut lists the suspended agents and the queued work, and (unless dry) logs them handed
// out: a suspended agent runs again from now, a queued item is started with `start` as any
// agent is.
func (a *app) handOut(dry bool) error {
	log, err := a.entries()
	if err != nil {
		return err
	}
	var paused []*Agent
	for _, ag := range agents(log).values() {
		if suspended(ag) {
			paused = append(paused, ag)
		}
	}
	waiting := queued(log)
	if len(paused) == 0 && len(waiting) == 0 {
		a.println("nothing suspended or queued")
		return nil
	}
	for _, ag := range paused {
		to := " → resume it"
		if ag.AgentRef != "" {
			to = " → resume it (SendMessage " + ag.AgentRef + ")"
		}
		line := fmt.Sprintf("resume %s  %s:%s  %s%s", ag.ID, ag.Model, ag.task(), ag.Label, to)
		if n := str(ag.AgentNote); n != "" {
			line += ": " + n
		}
		a.println(line)
	}
	for _, q := range waiting {
		line := fmt.Sprintf("start  %s  %s:%s  %s → `claude-quota start` it", q.ID, q.Model, q.task(), q.Label)
		if n := str(q.Note); n != "" {
			line += ": " + n
		}
		a.println(line)
	}
	if len(waiting) > 0 {
		a.println("Price the queued work with `claude-quota plan` first, leaving room for the session itself, and keep `claude-quota watch` going in the background while it runs.")
	}
	if !dry {
		var ids []string
		for _, ag := range paused {
			ids = append(ids, ag.ID)
		}
		for _, q := range waiting {
			ids = append(ids, q.ID)
		}
		for _, id := range ids {
			if err := a.appendEntry(map[string]any{"t": a.time(), "kind": "resume", "id": id}); err != nil {
				return err
			}
		}
	}
	return nil
}

// cmdWait sleeps until the 5-hour window resets, confirms it with a fresh reading, then hands
// out what's suspended and queued. Run in the background, its exit wakes the session at the reset.
func (a *app) cmdWait(p *parsed) error {
	s, err := a.snapshot(fresh)
	if err != nil {
		return err
	}
	target := *s.FiveReset + p.float("margin", waitMargin)*60
	a.printf("waiting for the 5h window to reset (~%s, in %s)\n", atClock(target), hours(target-a.time()))
	a.sleep(time.Duration(max(0, target-a.time()) * float64(time.Second)))
	n := s
	for i := 0; i < 15; i++ { // the reading may lag the reset by a minute or two
		if n, err = a.snapshot(0); err != nil {
			return err
		}
		if *n.FiveReset > *s.FiveReset+60 || n.Five < s.Five {
			break
		}
		a.sleep(120 * time.Second)
	}
	if err := a.showStatus(n, a.target); err != nil {
		return err
	}
	a.println("")
	return a.handOut(false)
}

// deadline: an expiry as Settings > Usage shows it, in local time. A bare date means its
// start, so a reset is never planned past the moment it lapses.
func deadline(s string) (float64, error) {
	t, ok := parseISO(s, time.Local)
	if !ok {
		return 0, die("expiry must be a date or date and time, e.g. 2026-10-22 or 2026-10-22T18:00, got %s", pyRepr(s))
	}
	return float64(t.UnixMicro()) / 1e6, nil
}

func (a *app) cmdResetAdd(p *parsed) error {
	window := map[string]string{"week": "week", "5h": "five", "both": "both"}[*p.str("window")]
	exp, err := deadline(*p.str("expires"))
	if err != nil {
		return err
	}
	id := a.newID("r")
	if err := a.appendEntry(map[string]any{"t": a.time(), "kind": "reset", "id": id, "window": window, "expires": exp, "note": nullable(p.str("note"))}); err != nil {
		return err
	}
	a.println(id)
	return nil
}

func (a *app) cmdResetUsed(p *parsed) error {
	log, err := a.entries()
	if err != nil {
		return err
	}
	id := *p.str("id")
	found := false
	for _, r := range resets(log) {
		found = found || r.ID == id
	}
	if !found {
		return die("no reset %s; `claude-quota reset` lists them", pyRepr(id))
	}
	return a.appendEntry(map[string]any{"t": a.time(), "kind": "redeem", "id": id})
}

func (a *app) cmdResetList() error {
	log, err := a.entries()
	if err != nil {
		return err
	}
	now := a.time()
	for _, r := range resets(log) {
		name := map[string]string{"week": "week", "five": "5h", "both": "both"}[r.Window]
		var state string
		switch {
		case r.Used != 0:
			state = "redeemed " + at(r.Used).Format("01-02 15:04")
		case r.Expires <= now:
			state = "lapsed"
		default:
			state = "open, " + hours(r.Expires-now) + " left"
		}
		line := fmt.Sprintf("%s  %-4s  expires %s  %s", r.ID, name, at(r.Expires).Format("2006-01-02 15:04"), state)
		if n := str(r.Note); n != "" {
			line += "  " + n
		}
		a.println(line)
	}
	return nil
}

func (a *app) cmdLog(p *parsed) error {
	log, err := a.entries()
	if err != nil {
		return err
	}
	n := 20
	if v := p.str("n"); v != nil {
		n, _ = strconv.Atoi(strings.TrimSpace(*v))
	}
	switch { // Python's log[-n:]
	case n == 0:
	case n > 0:
		log = log[max(0, len(log)-n):]
	default:
		log = log[min(len(log), -n):]
	}
	for _, e := range log {
		when := at(e.T).Format("01-02 15:04")
		switch e.Kind {
		case "snap":
			a.printf("%s  snap   5h %3.0f%%  week %3.0f%%\n", when, e.Five, e.Week)
		case "start":
			a.printf("%s  start  %s  %s:%s  %s\n", when, e.ID, e.Model, e.task(), e.Label)
		case "reset":
			a.printf("%s  reset  %s  %s until %s\n", when, e.ID, e.Window, at(e.Expires).Format("01-02 15:04"))
		case "redeem":
			a.printf("%s  redeem %s\n", when, e.ID)
		case "suspend":
			line := fmt.Sprintf("%s  pause  %s", when, e.ID)
			if n := str(e.Note); n != "" {
				line += "  " + n
			}
			a.println(line)
		case "queue":
			a.printf("%s  queue  %s  %s:%s  %s\n", when, e.ID, e.Model, e.task(), e.Label)
		case "resume", "drop":
			a.printf("%s  %-6s %s\n", when, e.Kind, e.ID)
		default:
			tok := 0.0
			if e.Tokens != nil {
				tok = *e.Tokens
			}
			a.printf("%s  end    %s  %.0fk  %s\n", when, e.ID, tok/1e3, str(e.Outcome))
		}
	}
	return nil
}
