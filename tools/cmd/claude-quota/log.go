package main

// The log: one JSON object per line, appended to and read as the Python version did.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Scoped is a snapshot's per-model weekly percents, in the order the endpoint gave them.
type Scoped struct {
	Keys []string
	Vals map[string]*float64
}

func (s *Scoped) UnmarshalJSON(b []byte) error {
	s.Keys, s.Vals = nil, map[string]*float64{}
	if string(bytes.TrimSpace(b)) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if _, err := dec.Token(); err != nil {
		return err
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return err
		}
		var v *float64
		if err := dec.Decode(&v); err != nil {
			return err
		}
		s.set(k.(string), v)
	}
	return nil
}

func (s *Scoped) set(k string, v *float64) {
	if s.Vals == nil {
		s.Vals = map[string]*float64{}
	}
	if _, ok := s.Vals[k]; !ok {
		s.Keys = append(s.Keys, k)
	}
	s.Vals[k] = v
}

func (s Scoped) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range s.Keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, err := json.Marshal(s.Vals[k])
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Entry is any log line. Which fields are set depends on Kind; the rest stay zero.
type Entry struct {
	T         float64  `json:"t"`
	Kind      string   `json:"kind"`
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Model     string   `json:"model"`
	Task      *string  `json:"task"`
	Tokens    *float64 `json:"tokens"`
	Outcome   *string  `json:"outcome"`
	Ref       *string  `json:"ref"`
	Note      *string  `json:"note"`
	Window    string   `json:"window"`
	Expires   float64  `json:"expires"`
	Five      float64  `json:"five"`
	Week      float64  `json:"week"`
	FiveReset *float64 `json:"five_reset"`
	WeekReset *float64 `json:"week_reset"`
	Scoped    Scoped   `json:"scoped"`
}

// str is a nullable string as Python's `x or ""` reads it.
func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (e Entry) task() string {
	if t := str(e.Task); t != "" {
		return t
	}
	return "-"
}

func (e Entry) reset(w string) *float64 {
	if w == "five" {
		return e.FiveReset
	}
	return e.WeekReset
}

func (e Entry) level(w string) float64 {
	if w == "five" {
		return e.Five
	}
	return e.Week
}

func (a *app) stateDir() string {
	if v, ok := a.lookupEnv("XDG_STATE_HOME"); ok {
		return filepath.Join(v, "claude-quota")
	}
	return filepath.Join(a.home, ".local/state", "claude-quota")
}

func (a *app) logPath() string { return filepath.Join(a.stateDir(), "log.jsonl") }

func (a *app) appendEntry(e map[string]any) error {
	if err := os.MkdirAll(a.stateDir(), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(a.logPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e); err != nil {
		f.Close()
		return err
	}
	_, err = f.Write(b.Bytes()) // one write, so concurrent appenders don't interleave
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// entries reads the whole log, sorted by time (stable: a start and its snapshot keep their order).
func (a *app) entries() ([]Entry, error) {
	f, err := os.Open(a.logPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("%s line %d: %v", a.logPath(), n, err)
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out, nil
}

// ordered keeps insertion order the way a Python dict does: replacing a key keeps its place.
type ordered[V any] struct {
	keys []string
	m    map[string]V
}

func newOrdered[V any]() *ordered[V] { return &ordered[V]{m: map[string]V{}} }

func (o *ordered[V]) put(k string, v V) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

func (o *ordered[V]) del(k string) {
	if _, ok := o.m[k]; !ok {
		return
	}
	delete(o.m, k)
	for i, x := range o.keys {
		if x == k {
			o.keys = append(o.keys[:i:i], o.keys[i+1:]...)
			break
		}
	}
}

func (o *ordered[V]) values() []V {
	out := make([]V, 0, len(o.keys))
	for _, k := range o.keys {
		out = append(out, o.m[k])
	}
	return out
}

type pause struct {
	from, to float64
	open     bool
}

// Agent is a logged agent: its start entry, plus end time, tokens and outcome once it
// ended, and its pauses with the session's handle on it and where it was.
type Agent struct {
	Entry
	End          float64 // 0 until it ends
	AgentTokens  *float64
	AgentOutcome *string
	Paused       []pause
	AgentRef     string
	AgentNote    *string
}

func agents(log []Entry) *ordered[*Agent] {
	out := newOrdered[*Agent]()
	for _, e := range log {
		if e.Kind == "start" {
			out.put(e.ID, &Agent{Entry: e})
			continue
		}
		a := out.m[e.ID]
		if a == nil {
			continue
		}
		switch e.Kind {
		case "end":
			a.End, a.AgentTokens, a.AgentOutcome = e.T, e.Tokens, e.Outcome
		case "suspend":
			a.Paused = append(a.Paused, pause{from: e.T, open: true})
			if r := str(e.Ref); r != "" {
				a.AgentRef = r
			}
			a.AgentNote = e.Note
		case "resume":
			if n := len(a.Paused); n > 0 && a.Paused[n-1].open {
				a.Paused[n-1].to, a.Paused[n-1].open = e.T, false
			}
		}
	}
	return out
}

func suspended(a *Agent) bool {
	return a.End == 0 && len(a.Paused) > 0 && a.Paused[len(a.Paused)-1].open
}

// running: the agents started and neither ended nor paused.
func running(log []Entry) []*Agent {
	var out []*Agent
	for _, a := range agents(log).values() {
		if a.End == 0 && !suspended(a) {
			out = append(out, a)
		}
	}
	return out
}

// active: seconds of [lo, hi] the agent ran, from its start to its end (or hi), less its pauses.
func active(a *Agent, lo, hi float64) float64 {
	end := hi
	if a.End != 0 {
		end = a.End
	}
	span := max(0, min(hi, end)-max(lo, a.T))
	for _, p := range a.Paused {
		p1 := end
		if !p.open {
			p1 = p.to
		}
		span -= max(0, min(hi, end, p1)-max(lo, a.T, p.from))
	}
	return max(0, span)
}

// queued: work queued for a later window and not yet handed out or dropped.
func queued(log []Entry) []Entry {
	out := newOrdered[Entry]()
	for _, e := range log {
		switch e.Kind {
		case "queue":
			out.put(e.ID, e)
		case "resume", "drop":
			out.del(e.ID)
		}
	}
	return out.values()
}

// sameWindow: both readings are of one window. The endpoint gives its reset time a different
// fraction of a second at each reading, so they're compared to within sameReset; a new window
// resets a full window later.
func sameWindow(a, b Entry, w string) bool {
	ra, rb := a.reset(w), b.reset(w)
	if ra == nil || rb == nil {
		return ra == nil && rb == nil
	}
	return abs(*ra-*rb) < sameReset
}

// refilled: the window dropped while its reset time stayed put, so a limit reset was
// redeemed (or Anthropic reset everyone's limits).
func refilled(a, b Entry, w string) bool {
	return sameWindow(a, b, w) && b.level(w) < a.level(w)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// Reset is a logged limit reset; Used is when it was redeemed (0 if not).
type Reset struct {
	Entry
	Used float64
}

// resets: every logged reset, with Used set from `reset used`, else read off the first refill
// of its window after it was logged and before it expired.
func resets(log []Entry) []*Reset {
	out := newOrdered[*Reset]()
	for _, e := range log {
		switch e.Kind {
		case "reset":
			out.put(e.ID, &Reset{Entry: e})
		case "redeem":
			if r := out.m[e.ID]; r != nil {
				r.Used = e.T
			}
		}
	}
	var snaps []Entry
	for _, e := range log {
		if e.Kind == "snap" {
			snaps = append(snaps, e)
		}
	}
	for i := 0; i+1 < len(snaps); i++ {
		a, b := snaps[i], snaps[i+1]
		for _, w := range []string{"week", "five"} {
			if !refilled(a, b, w) {
				continue
			}
			var best *Reset
			for _, r := range out.values() {
				if (r.Window == w || r.Window == "both") && r.Used == 0 && r.T <= b.T && r.Expires >= a.T {
					if best == nil || r.Expires < best.Expires {
						best = r
					}
				}
			}
			if best != nil {
				best.Used = b.T
			}
		}
	}
	return out.values()
}

func liveResets(log []Entry, now float64) []*Reset {
	var out []*Reset
	for _, r := range resets(log) {
		if r.Used == 0 && r.Expires > now {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Expires < out[j].Expires })
	return out
}
