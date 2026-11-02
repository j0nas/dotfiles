package main

import (
	"bytes"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	t0        = 1_793_670_000.0
	fiveReset = 1_793_674_200
	weekReset = 1_794_200_000
)

func ptr[T any](v T) *T { return &v }

// snap is the i-th reading, an hour after the one before; both reset times carry a fraction
// of a second, as the endpoint gives them.
func snap(i int, five, week, jitter float64) Entry {
	return Entry{T: t0 + 3600*float64(i), Kind: "snap", Five: five, Week: week,
		FiveReset: ptr(fiveReset + jitter), WeekReset: ptr(weekReset + jitter), Scoped: Scoped{Vals: map[string]*float64{}}}
}

// logWithAgent: eight readings of one window. An Opus agent runs through the first four
// hours: 3.6M tokens while the 5-hour window rose 72 points, so 20 points per Mtok where the
// starting guess is 14. The window stays put in the last three readings, which keeps the fit
// from explaining the rise as per-hour baseline.
func logWithAgent() []Entry {
	const run = 4
	jitters := []float64{0.46, 0.79, 0.12, 0.93, 0.31, 0.68, 0.05, 0.84}
	log := []Entry{{T: t0, Kind: "start", ID: "a1", Label: "x", Model: "opus", Task: ptr("implement")}}
	for i, j := range jitters {
		log = append(log, snap(i, 18*float64(min(i, run)), 4*float64(min(i, run)), j))
	}
	return append(log, Entry{T: t0 + 3600*run, Kind: "end", ID: "a1", Tokens: ptr(3_600_000.0), Outcome: ptr("merged")})
}

// harness runs the tool in-process against a scratch state dir, a fixed clock and a stub
// usage endpoint; no network, no keychain.
type harness struct {
	t       *testing.T
	a       *app
	out     *bytes.Buffer
	err     *bytes.Buffer
	state   string
	clock   float64
	fetches int
	usage   *usageResp
	fetchEr error
	slept   []time.Duration
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	time.Local = time.UTC
	h := &harness{t: t, out: &bytes.Buffer{}, err: &bytes.Buffer{}, state: t.TempDir(), clock: t0 + 1000}
	n := 0
	h.a = &app{
		stdout: h.out, stderr: h.err,
		lookupEnv: func(k string) (string, bool) {
			if k == "XDG_STATE_HOME" {
				return h.state, true
			}
			return "", false
		},
		home:    t.TempDir(),
		now:     func() time.Time { return time.UnixMicro(int64(h.clock * 1e6)) },
		sleep:   func(d time.Duration) { h.slept = append(h.slept, d); h.clock += d.Seconds() },
		token:   func() (string, error) { return "tok", nil },
		randHex: func() string { n++; return strings.Repeat(string(rune('a'+n)), 4) },
		fetch: func(string) (*usageResp, error) {
			h.fetches++
			return h.usage, h.fetchEr
		},
	}
	return h
}

func (h *harness) run(args ...string) (int, string, string) {
	h.out.Reset()
	h.err.Reset()
	code := h.a.run(args)
	return code, h.out.String(), h.err.String()
}

// seed writes entries to the log the way the Python version formats them.
func (h *harness) seed(lines ...string) {
	h.t.Helper()
	dir := filepath.Join(h.state, "claude-quota")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "log.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) logLines() []string {
	b, _ := os.ReadFile(filepath.Join(h.state, "claude-quota", "log.jsonl"))
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// freshSnap is a reading taken at the harness clock, so commands don't try the endpoint.
func (h *harness) freshSnap(five, week int) string {
	return `{"t": ` + ftoa(h.clock) + `, "kind": "snap", "five": ` + itoa(five) + `.0, "five_reset": ` + ftoa(h.clock+7200) +
		`, "week": ` + itoa(week) + `.0, "week_reset": ` + ftoa(h.clock+3*86400) + `, "scoped": {}}`
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
func itoa(i int) string     { return strconv.Itoa(i) }

// --- window matching (commit 106a940) --------------------------------------------------------

func TestFractionOfASecondIsOneWindow(t *testing.T) {
	a, b := snap(0, 0, 0, 0.46), snap(1, 8, 1, 0.79)
	if !sameWindow(a, b, "five") || !sameWindow(a, b, "week") {
		t.Fatal("readings 0.33 s apart in reset time should be one window")
	}
}

func TestNextWindowIsAnother(t *testing.T) {
	a := snap(0, 90, 30, 0.46)
	b := snap(1, 2, 31, 0.79)
	b.FiveReset = ptr(fiveReset + 5*3600 + 0.79)
	if sameWindow(a, b, "five") {
		t.Error("a reset time five hours on is a new 5h window")
	}
	if !sameWindow(a, b, "week") {
		t.Error("the week did not change")
	}
}

func TestSameWindowWithoutResetTimes(t *testing.T) {
	a, b := snap(0, 0, 0, 0), snap(1, 0, 0, 0)
	a.FiveReset, b.FiveReset = nil, nil
	if !sameWindow(a, b, "five") {
		t.Error("two unstarted windows are the same")
	}
	b.FiveReset = ptr(1.0)
	if sameWindow(a, b, "five") {
		t.Error("an unstarted window and a started one are not")
	}
}

func TestRefilled(t *testing.T) {
	if !refilled(snap(0, 50, 20, 0.46), snap(1, 3, 1, 0.79), "five") {
		t.Error("a drop with a jittered reset time is a refill")
	}
	if refilled(snap(0, 3, 1, 0.46), snap(1, 50, 20, 0.79), "five") {
		t.Error("a rise is not")
	}
	b := snap(1, 3, 20, 0.79)
	b.FiveReset = ptr(fiveReset + 5*3600 + 0.79)
	if refilled(snap(0, 50, 20, 0.46), b, "five") {
		t.Error("a drop into a new window is not")
	}
}

func TestJitteredReadingsMoveTheRateOffItsPrior(t *testing.T) {
	m := learn(logWithAgent())
	got := m.Rates["five"]["opus"]
	if got <= prior["five"]["opus"]+1 {
		t.Errorf("5h rate %.2f stayed near its starting guess: readings were skipped", got)
	}
	if got >= 20.5 {
		t.Errorf("5h rate %.2f above the 20 the readings show", got)
	}
	if m.Agents != 1 {
		t.Errorf("agents = %d, want 1", m.Agents)
	}
}

func TestWeekIsDerivedFromTheFiveHourRates(t *testing.T) {
	// 40 points of 5h movement clears ratioMinFive only if every jittered pair is counted.
	if !learn(logWithAgent()).Derived {
		t.Error("week not derived from the 5h rates")
	}
}

func TestLearnSkipsPairsAcrossARefill(t *testing.T) {
	base := learn(logWithAgent())
	// a ninth reading with the 5h window dropped and its reset time (to a fraction of a
	// second) unchanged: a redeemed reset, whose drop must not be fitted as negative spend
	log := logWithAgent()
	log = append(log, snap(8, 2, 28, 0.5))
	m := learn(log)
	for _, mo := range models {
		if math.Abs(m.Rates["five"][mo]-base.Rates["five"][mo]) > 1e-9 || math.Abs(m.Baseline["five"]-base.Baseline["five"]) > 1e-9 {
			t.Fatalf("the refill moved the %s rate: %v -> %v", mo, base.Rates["five"][mo], m.Rates["five"][mo])
		}
	}
}

// --- fitting ------------------------------------------------------------------------------

func TestLstsqSolvesAndRejectsSingular(t *testing.T) {
	x := lstsq([][]float64{{1, 0}, {0, 1}, {1, 1}}, []float64{2, 3, 5})
	if x == nil || math.Abs(x[0]-2) > 1e-9 || math.Abs(x[1]-3) > 1e-9 {
		t.Errorf("lstsq = %v, want [2 3]", x)
	}
	if lstsq([][]float64{{1, 1}, {2, 2}}, []float64{1, 2}) != nil {
		t.Error("collinear columns should have no solution")
	}
}

func TestNnlsClampsNegativeToZero(t *testing.T) {
	// y = 2*x0 - 1*x1 has a negative second coefficient; the constrained fit keeps it at 0.
	rows := [][]float64{{1, 0}, {0, 1}, {1, 1}, {2, 1}}
	ys := []float64{2, -1, 1, 3}
	x := nnls(rows, ys)
	if x[1] != 0 || x[0] <= 0 {
		t.Errorf("nnls = %v", x)
	}
}

func TestMedianOddAndEven(t *testing.T) {
	if median([]float64{3, 1, 2}) != 2 || median([]float64{4, 1, 2, 3}) != 2.5 {
		t.Error("median")
	}
}

// --- log ------------------------------------------------------------------------------------

func TestReadsPythonWrittenLogAsIs(t *testing.T) {
	h := newHarness(t)
	h.seed(
		`{"t": 1000.5, "kind": "snap", "five": 65.0, "five_reset": null, "week": 50.0, "week_reset": 1792141199.0, "scoped": {"Opus": 12.0, "Sonnet": null}}`,
		``,
		`{"t": 900.0, "kind": "start", "id": "a1", "label": "x", "model": "sonnet", "task": null}`,
		`{"t": 1100.0, "kind": "end", "id": "a1", "tokens": 229000.0, "outcome": null}`,
	)
	log, err := h.a.entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 3 || log[0].Kind != "start" || log[1].FiveReset != nil {
		t.Fatalf("log = %+v", log)
	}
	if got := log[1].Scoped.Keys; len(got) != 2 || got[0] != "Opus" || log[1].Scoped.Vals["Sonnet"] != nil {
		t.Errorf("scoped = %+v", log[1].Scoped)
	}
	if ag := agents(log).m["a1"]; ag == nil || ag.End != 1100 || *ag.AgentTokens != 229000 || ag.task() != "-" {
		t.Errorf("agent = %+v", ag)
	}
}

func TestSortIsStable(t *testing.T) {
	h := newHarness(t)
	h.seed(`{"t": 5, "kind": "start", "id": "b", "label": "", "model": "opus"}`,
		`{"t": 1, "kind": "start", "id": "c", "label": "", "model": "opus"}`,
		`{"t": 5, "kind": "start", "id": "a", "label": "", "model": "opus"}`)
	log, _ := h.a.entries()
	if log[0].ID != "c" || log[1].ID != "b" || log[2].ID != "a" {
		t.Errorf("order = %s %s %s", log[0].ID, log[1].ID, log[2].ID)
	}
}

func TestCorruptLineIsReportedWithItsNumber(t *testing.T) {
	h := newHarness(t)
	h.seed(`{"t": 1, "kind": "start", "id": "a", "label": "", "model": "opus"}`, `{oops`)
	code, _, stderr := h.run("log")
	if code != 1 || !strings.Contains(stderr, "line 2") {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}

func TestAgentPausesAreLeftOutOfItsRun(t *testing.T) {
	log := []Entry{
		{T: 0, Kind: "start", ID: "a", Model: "opus"},
		{T: 1000, Kind: "suspend", ID: "a", Ref: ptr("r"), Note: ptr("where")},
		{T: 4000, Kind: "resume", ID: "a"},
		{T: 5000, Kind: "end", ID: "a", Tokens: ptr(1e5)},
	}
	a := agents(log).m["a"]
	if got := active(a, a.T, a.End); got != 2000 {
		t.Errorf("active = %v, want 2000 (5000 less a 3000 s pause)", got)
	}
	if a.AgentRef != "r" || str(a.AgentNote) != "where" {
		t.Errorf("ref/note = %q %q", a.AgentRef, str(a.AgentNote))
	}
	open := agents(log[:2]).m["a"]
	if !suspended(open) || len(running(log[:2])) != 0 {
		t.Error("a paused agent is suspended, not running")
	}
	if got := active(open, 0, 9000); got != 1000 {
		t.Errorf("an open pause runs to the end of the span: active = %v, want 1000", got)
	}
}

func TestQueuedKeepsOrderAndDropsHandedOut(t *testing.T) {
	log := []Entry{
		{T: 1, Kind: "queue", ID: "q1"}, {T: 2, Kind: "queue", ID: "q2"}, {T: 3, Kind: "queue", ID: "q3"},
		{T: 4, Kind: "drop", ID: "q2"}, {T: 5, Kind: "resume", ID: "q1"},
	}
	q := queued(log)
	if len(q) != 1 || q[0].ID != "q3" {
		t.Errorf("queued = %+v", q)
	}
}

func TestResetRedemptionIsReadOffTheSnapshots(t *testing.T) {
	a, b := snap(0, 60, 40, 0.1), snap(1, 3, 1, 0.9) // both windows refill, reset times jittered
	log := []Entry{
		{T: t0 - 10, Kind: "reset", ID: "r1", Window: "both", Expires: t0 + 99999},
		{T: t0 - 10, Kind: "reset", ID: "r2", Window: "five", Expires: t0 + 50000},
		{T: t0 - 10, Kind: "reset", ID: "r3", Window: "week", Expires: t0 + 1},
		a, b,
	}
	used := map[string]float64{}
	for _, r := range resets(log) {
		used[r.ID] = r.Used
	}
	// the week refill goes to the soonest-expiring week/both reset (r3 expires before b), the 5h one to r2
	if used["r3"] != b.T || used["r2"] != b.T || used["r1"] != 0 {
		t.Errorf("used = %v", used)
	}
	if live := liveResets(log, b.T); len(live) != 1 || live[0].ID != "r1" {
		t.Errorf("live = %+v", live)
	}
}

func TestAppendedRecordsReadBackInPython(t *testing.T) {
	h := newHarness(t)
	h.seed(h.freshSnap(10, 5))
	if code, _, e := h.run("start", "ünï \"q\" <a&b>", "Sonnet", "--kind", "implement"); code != 0 {
		t.Fatal(e)
	}
	h.run("queue", "add", "later", "opus")
	lines := h.logLines()
	last := lines[len(lines)-1]
	// every key the Python reader indexes is there, null where it had no value, and non-ASCII
	// and quotes survive
	for _, want := range []string{`"kind":"queue"`, `"task":null`, `"note":null`, `"model":"opus"`, `"label":"later"`} {
		if !strings.Contains(last, want) {
			t.Errorf("queue record %s lacks %s", last, want)
		}
	}
	if s := lines[len(lines)-2]; !strings.Contains(s, `"label":"ünï \"q\" <a&b>"`) || !strings.Contains(s, `"model":"sonnet"`) {
		t.Errorf("start record %s", s)
	}
}

// --- snapshot --------------------------------------------------------------------------------

func TestFreshReadingIsReusedWithoutFetching(t *testing.T) {
	h := newHarness(t)
	h.seed(h.freshSnap(10, 5))
	if _, err := h.a.snapshot(fresh); err != nil {
		t.Fatal(err)
	}
	if h.fetches != 0 {
		t.Errorf("fetched %d times for a reading under a minute old", h.fetches)
	}
}

func TestStaleReadingFetchesAndLogsWithScopedOrder(t *testing.T) {
	h := newHarness(t)
	h.seed(`{"t": 1, "kind": "snap", "five": 1.0, "five_reset": 5.0, "week": 1.0, "week_reset": 6.0, "scoped": {}}`)
	h.usage = &usageResp{Limits: []struct {
		Kind    string   `json:"kind"`
		Percent *float64 `json:"percent"`
		Scope   *struct {
			Model *struct {
				DisplayName *string `json:"display_name"`
			} `json:"model"`
		} `json:"scope"`
	}{{Kind: "weekly_scoped", Percent: ptr(7.0)}, {Kind: "other"}}}
	h.usage.FiveHour = &struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    *string `json:"resets_at"`
	}{42, ptr("2026-11-02T20:00:00.250Z")}
	h.usage.SevenDay = &struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    *string `json:"resets_at"`
	}{30, nil}
	s, err := h.a.snapshot(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if h.fetches != 1 || s.Five != 42 {
		t.Fatalf("fetches %d five %v", h.fetches, s.Five)
	}
	if *s.WeekReset != h.clock+7*86400 {
		t.Errorf("a window with no reset time ends a full week from the reading, got %v", *s.WeekReset)
	}
	last := h.logLines()[1]
	if !strings.Contains(last, `"week_reset":null`) || !strings.Contains(last, `"scoped":{"?":7}`) || !strings.Contains(last, `"five_reset":1793649600.25`) {
		t.Errorf("logged %s", last)
	}
}

func TestEndpointDownFallsBackToTheLastReading(t *testing.T) {
	h := newHarness(t)
	h.seed(`{"t": 1, "kind": "snap", "five": 9.0, "five_reset": 5.0, "week": 1.0, "week_reset": 6.0, "scoped": {}}`)
	h.fetchEr = errors.New("HTTP Error 429: Too Many Requests")
	code, out, stderr := h.run("status")
	if code != 0 || !strings.Contains(out, "5h    9%") {
		t.Errorf("code %d out %q", code, out)
	}
	if !strings.Contains(stderr, "can't read the usage (HTTP Error 429: Too Many Requests); using the reading from") {
		t.Errorf("stderr %q", stderr)
	}
}

func TestPlanCountsAgentsStillRunning(t *testing.T) {
	h := newHarness(t)
	h.seed(`{"t": `+ftoa(h.clock)+`, "kind": "start", "id": "a1", "label": "w", "model": "opus", "task": null}`, h.freshSnap(10, 5))
	_, out, _ := h.run("plan", "haiku=1")
	// no history: the priors, a 300k opus agent just started = 0.3 Mtok x 14 (5h) and x 3.5 (week)
	if !strings.Contains(out, "running 1 agent still going ≈ 4 pts more of this 5h window, 1.1 of the week: counted as spent below\n") {
		t.Errorf("plan:\n%s", out)
	}
}

func TestEndpointDownWithNoReadingExits1(t *testing.T) {
	h := newHarness(t)
	h.fetchEr = errors.New("boom")
	code, _, stderr := h.run("status")
	if code != 1 || stderr != "claude-quota: can't read the usage (boom) and there's no earlier reading\n" {
		t.Errorf("code %d stderr %q", code, stderr)
	}
}

// --- commands ---------------------------------------------------------------------------------

func TestStartEndRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.seed(h.freshSnap(10, 5))
	code, out, e := h.run("start", "build it", "OPUS", "--kind", "implement")
	if code != 0 {
		t.Fatal(e)
	}
	id := strings.TrimSpace(out)
	if !strings.HasPrefix(id, "a1102-") && !strings.HasPrefix(id, "a11") {
		t.Logf("id %s", id)
	}
	h.clock += 600
	h.seed(append(h.logLines(), h.freshSnap(12, 5))...)
	code, out, e = h.run("end", id, "229k", "--outcome", "merged")
	if code != 0 || !strings.Contains(out, id+" build it: ran 10m, 229k tokens") {
		t.Errorf("code %d out %q err %q", code, out, e)
	}
	last := h.logLines()
	if !strings.Contains(last[len(last)-1], `"tokens":229000`) {
		t.Errorf("end record %s", last[len(last)-1])
	}
}

func TestBadInputExitCodes(t *testing.T) {
	h := newHarness(t)
	h.seed(h.freshSnap(10, 5))
	for _, c := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"start", "x", "gpt"}, 1, "claude-quota: model must be one of ('haiku', 'sonnet', 'opus', 'fable'), got 'gpt'\n"},
		{[]string{"end", "x", "1k", "--outcome", "zz"}, 1, "claude-quota: outcome must be one of ('merged', 'fixed', 'partial', 'discarded')\n"},
		{[]string{"suspend", "nope"}, 1, "claude-quota: no open agent 'nope'; `claude-quota status` lists them\n"},
		{[]string{"queue", "drop", "nope"}, 1, "claude-quota: nothing queued as 'nope'; `claude-quota queue` lists it\n"},
		{[]string{"reset", "used", "nope"}, 1, "claude-quota: no reset 'nope'; `claude-quota reset` lists them\n"},
		{[]string{"reset", "add", "week", "someday"}, 1, "claude-quota: expiry must be a date or date and time, e.g. 2026-10-22 or 2026-10-22T18:00, got 'someday'\n"},
	} {
		code, out, stderr := h.run(c.args...)
		if code != c.code || stderr != c.msg || out != "" {
			t.Errorf("%v: code %d stdout %q stderr %q", c.args, code, out, stderr)
		}
	}
}

func TestSuspendTwiceAndResume(t *testing.T) {
	h := newHarness(t)
	h.seed(`{"t": 1, "kind": "start", "id": "a1", "label": "work", "model": "opus", "task": null}`, h.freshSnap(10, 5))
	if code, _, e := h.run("suspend", "a1", "--ref", "ag7", "--note", "at step 3"); code != 0 {
		t.Fatal(e)
	}
	if code, _, e := h.run("suspend", "a1"); code != 1 || e != "claude-quota: a1 is already suspended\n" {
		t.Errorf("code %d err %q", code, e)
	}
	n := len(h.logLines())
	_, out, _ := h.run("resume", "--dry-run")
	if out != "resume a1  opus:-  work → resume it (SendMessage ag7): at step 3\n" {
		t.Errorf("dry run: %q", out)
	}
	if len(h.logLines()) != n {
		t.Error("--dry-run logged")
	}
	h.run("resume")
	if _, out, _ = h.run("resume"); out != "nothing suspended or queued\n" {
		t.Errorf("after hand-out: %q", out)
	}
}

func TestQueueAddListDrop(t *testing.T) {
	h := newHarness(t)
	h.seed(h.freshSnap(10, 5))
	_, id, _ := h.run("queue", "add", "review it", "sonnet:review", "--note", "after reset")
	id = strings.TrimSpace(id)
	if _, out, _ := h.run("queue"); out != id+"  sonnet:review  review it  — after reset\n" {
		t.Errorf("list: %q", out)
	}
	h.run("queue", "drop", id)
	if _, out, _ := h.run("queue", "list"); out != "" {
		t.Errorf("after drop: %q", out)
	}
}

func TestResetAddAndList(t *testing.T) {
	h := newHarness(t)
	h.seed(h.freshSnap(10, 5))
	code, id, e := h.run("reset", "add", "5h", "2026-11-03T09:30", "--note", "spare")
	if code != 0 {
		t.Fatal(e)
	}
	id = strings.TrimSpace(id)
	_, out, _ := h.run("reset")
	want := id + "  5h    expires 2026-11-03 09:30  open, "
	if !strings.HasPrefix(out, want) || !strings.HasSuffix(out, " left  spare\n") {
		t.Errorf("list: %q", out)
	}
	h.run("reset", "used", id)
	if _, out, _ = h.run("reset", "list"); !strings.Contains(out, "  redeemed 11-03 ") {
		t.Errorf("redeemed: %q", out)
	}
}

func TestLogCountFollowsPythonSlicing(t *testing.T) {
	h := newHarness(t)
	var lines []string
	for i := 1; i <= 5; i++ {
		lines = append(lines, `{"t": `+itoa(i)+`, "kind": "drop", "id": "d`+itoa(i)+`"}`)
	}
	h.seed(lines...)
	for _, c := range []struct {
		n    string
		want []string
	}{{"2", []string{"d4", "d5"}}, {"0", []string{"d1", "d2", "d3", "d4", "d5"}}, {"-3", []string{"d4", "d5"}}, {"99", []string{"d1", "d2", "d3", "d4", "d5"}}} {
		_, out, _ := h.run("log", "-n", c.n)
		for _, id := range []string{"d1", "d2", "d3", "d4", "d5"} {
			if strings.Contains(out, id) != contains(c.want, id) {
				t.Errorf("-n %s: output %q, want %v", c.n, out, c.want)
				break
			}
		}
	}
}

func TestLearnPrintsFittedRates(t *testing.T) {
	h := newHarness(t)
	var lines []string
	for _, e := range logWithAgent() {
		switch e.Kind {
		case "snap":
			lines = append(lines, `{"t": `+ftoa(e.T)+`, "kind": "snap", "five": `+ftoa(e.Five)+`, "five_reset": `+ftoa(*e.FiveReset)+`, "week": `+ftoa(e.Week)+`, "week_reset": `+ftoa(*e.WeekReset)+`, "scoped": {}}`)
		case "start":
			lines = append(lines, `{"t": `+ftoa(e.T)+`, "kind": "start", "id": "a1", "label": "x", "model": "opus", "task": "implement"}`)
		default:
			lines = append(lines, `{"t": `+ftoa(e.T)+`, "kind": "end", "id": "a1", "tokens": 3600000.0, "outcome": "merged"}`)
		}
	}
	h.seed(lines...)
	_, out, _ := h.run("learn")
	for _, want := range []string{
		"1 finished agents. % of each window per Mtok (week from the 5h rates × the week/5h ratio; priors weigh 2.0 Mtok):\n",
		"  haiku   week",
		"typical agent 3600k   (logged 3.6 Mtok)",
		"by model and kind: median tokens, outcomes (merged/fixed/partial/discarded)\n  opus:implement     3600k   1/0/0/0\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("learn output lacks %q:\n%s", want, out)
		}
	}
}

func TestPlanFitsAndFallsShort(t *testing.T) {
	h := newHarness(t)
	h.seed(h.freshSnap(10, 5))
	_, out, _ := h.run("plan", "sonnet:implement=2")
	for _, want := range []string{"to run  sonnet:implement×2  ≈ ", "week    fits with ", "5h      fits in this window ("} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	_, out, _ = h.run("plan", "opus=200", "--size", "opus=1m")
	for _, want := range []string{"week    short by ", "5h      about 5 of 200 fit in this window"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
}

func TestGuardSuspendsWhenTheWindowWillFillFirst(t *testing.T) {
	h := newHarness(t)
	h.seed(
		`{"t": `+ftoa(h.clock-1200)+`, "kind": "start", "id": "a1", "label": "w", "model": "opus", "task": null}`,
		`{"t": `+ftoa(h.clock-1200)+`, "kind": "snap", "five": 50.0, "five_reset": `+ftoa(h.clock+1800)+`, "week": 5.0, "week_reset": `+ftoa(h.clock+86400)+`, "scoped": {}}`,
		`{"t": `+ftoa(h.clock)+`, "kind": "snap", "five": 80.0, "five_reset": `+ftoa(h.clock+1800)+`, "week": 6.0, "week_reset": `+ftoa(h.clock+86400)+`, "scoped": {}}`,
	)
	_, out, _ := h.run("guard")
	if !strings.HasPrefix(out, "suspend: at 80%, filling ~98 pts/h (from the fitted rates of what's running): its limit in ~12m, before it resets in 30m\nTell each running agent") {
		t.Errorf("guard: %q", out)
	}
	_, out, _ = h.run("guard", "--at", "99", "--lead", "1")
	if !strings.HasPrefix(out, "ok: at 80%, filling ~98 pts/h (from the fitted rates of what's running): its limit in ~12m; suspend at 99% or 1 min before it\n") {
		t.Errorf("guard with later thresholds: %q", out)
	}
}

func TestGuardHoldsOnTheObservedRateWithNothingRunning(t *testing.T) {
	h := newHarness(t)
	h.seed(
		`{"t": `+ftoa(h.clock-1200)+`, "kind": "snap", "five": 50.0, "five_reset": `+ftoa(h.clock+1800)+`, "week": 5.0, "week_reset": `+ftoa(h.clock+86400)+`, "scoped": {}}`,
		`{"t": `+ftoa(h.clock)+`, "kind": "snap", "five": 80.0, "five_reset": `+ftoa(h.clock+1800.4)+`, "week": 6.0, "week_reset": `+ftoa(h.clock+86400)+`, "scoped": {}}`,
	)
	_, out, _ := h.run("guard")
	if !strings.HasPrefix(out, "hold: at 80%, filling ~90 pts/h (seen over the last 20m): its limit in ~13m, before it resets in 30m\nStart nothing more in this window") {
		t.Errorf("guard: %q", out)
	}
}

func TestWatchEndsWhenNoAgentsRun(t *testing.T) {
	h := newHarness(t)
	h.seed(h.freshSnap(10, 5))
	_, out, _ := h.run("watch", "--every", "5")
	if !strings.HasPrefix(out, "no agents running (") || len(h.slept) != 0 {
		t.Errorf("out %q slept %v", out, h.slept)
	}
}

func TestWaitSleepsToTheResetThenHandsOut(t *testing.T) {
	h := newHarness(t)
	h.seed(h.freshSnap(60, 5)) // resets 7200 s on
	h.a.fetch = func(string) (*usageResp, error) {
		h.fetches++
		reset := "2026-11-03T03:00:00Z"
		return &usageResp{
			FiveHour: &struct {
				Utilization float64 `json:"utilization"`
				ResetsAt    *string `json:"resets_at"`
			}{1, &reset},
			SevenDay: &struct {
				Utilization float64 `json:"utilization"`
				ResetsAt    *string `json:"resets_at"`
			}{5, &reset},
		}, nil
	}
	code, out, e := h.run("wait", "--margin", "1")
	if code != 0 {
		t.Fatal(e)
	}
	if len(h.slept) == 0 || h.slept[0] != 7260*time.Second {
		t.Errorf("slept %v, want 7260s first", h.slept)
	}
	if !strings.HasPrefix(out, "waiting for the 5h window to reset (~") || !strings.HasSuffix(out, "nothing suspended or queued\n") {
		t.Errorf("out %q", out)
	}
	if h.fetches != 1 {
		t.Errorf("fetches = %d, want 1 (the post-reset reading)", h.fetches)
	}
}

func TestStatusShowsOpenAgentsQueueAndScoped(t *testing.T) {
	h := newHarness(t)
	h.seed(
		`{"t": `+ftoa(h.clock-100)+`, "kind": "start", "id": "a1", "label": "work", "model": "sonnet", "task": "review"}`,
		`{"t": `+ftoa(h.clock-50)+`, "kind": "queue", "id": "q1", "label": "later", "model": "haiku", "task": null, "note": null}`,
		`{"t": `+ftoa(h.clock)+`, "kind": "snap", "five": 4.0, "five_reset": `+ftoa(h.clock+3600)+`, "week": 40.4, "week_reset": `+ftoa(h.clock+86400)+`, "scoped": {"Opus": 12.0, "Sonnet": 0.0}}`,
	)
	_, out, _ := h.run("status")
	want := "5h    4%  resets in 1h0m   (86 pts to the 90% cap)\n" +
		"week  40%  resets in 1d0h   (52 pts to the 92% target)\n" +
		"      Opus weekly 12%\n" +
		"open  a1  sonnet:review  work  (ran 1m)\n" +
		"queued q1  haiku:-  later\n"
	if !strings.HasPrefix(out, want) {
		t.Errorf("status:\n%s\nwant prefix:\n%s", out, want)
	}
}

// --- command line -----------------------------------------------------------------------------

func TestHelpGoesToStdoutWithExit0(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"--help"}, {"-h"}, {"plan", "--help"}, {"queue", "-h"}, {"reset", "add", "--help"}} {
		code, out, stderr := h.run(args...)
		if code != 0 || !strings.HasPrefix(out, "usage: claude-quota") || stderr != "" {
			t.Errorf("%v: code %d out %q err %q", args, code, out, stderr)
		}
	}
	_, out, _ := h.run("--help")
	if !strings.Contains(out, "    status              snapshot and show the windows (the default)\n") {
		t.Errorf("top help: %s", out)
	}
}

func TestUsageErrorsExit2OnStderr(t *testing.T) {
	h := newHarness(t)
	h.seed(h.freshSnap(10, 5))
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"start"}, "usage: claude-quota start [-h] [--kind KIND] label model\nclaude-quota start: error: the following arguments are required: label, model\n"},
		{[]string{"start", "a", "b", "c"}, topUsage + "claude-quota: error: unrecognized arguments: c\n"},
		{[]string{"bogus"}, topUsage + "claude-quota: error: invalid choice: 'bogus' (choose from 'status', 'start', 'end', 'plan', 'learn', 'guard', 'watch', 'suspend', 'queue', 'wait', 'resume', 'reset', 'log')\n"},
		{[]string{"status", "--bad"}, topUsage + "claude-quota: error: unrecognized arguments: --bad\n"},
		{[]string{"log", "-n", "x"}, "usage: claude-quota log [-h] [-n N]\nclaude-quota log: error: argument -n: invalid int value: 'x'\n"},
		{[]string{"log", "-n"}, "usage: claude-quota log [-h] [-n N]\nclaude-quota log: error: argument -n: expected one argument\n"},
		{[]string{"plan", "--serial"}, "usage: claude-quota plan [-h] [--size [SIZE ...]] [--target TARGET] [--serial]\n                         agents [agents ...]\nclaude-quota plan: error: the following arguments are required: agents\n"},
		{[]string{"plan", "--s", "opus"}, "usage: claude-quota plan [-h] [--size [SIZE ...]] [--target TARGET] [--serial]\n                         agents [agents ...]\nclaude-quota plan: error: ambiguous option: --s could match --size, --serial\n"},
		{[]string{"queue", "foo"}, "usage: claude-quota queue [-h] {add,drop,list} ...\nclaude-quota queue: error: invalid choice: 'foo' (choose from 'add', 'drop', 'list')\n"},
		{[]string{"reset", "add", "x", "y"}, "usage: claude-quota reset add [-h] [--note NOTE] {week,5h,both} expires\nclaude-quota reset add: error: argument window: invalid choice: 'x' (choose from 'week', '5h', 'both')\n"},
		{[]string{"status", "--target", "high"}, "usage: claude-quota status [-h] [--target TARGET]\nclaude-quota status: error: argument --target: invalid float value: 'high'\n"},
	} {
		code, out, stderr := h.run(c.args...)
		if code != 2 || out != "" || stderr != c.msg {
			t.Errorf("%v: code %d stdout %q stderr\n%q\nwant\n%q", c.args, code, out, stderr, c.msg)
		}
	}
}

func TestParsingMatchesArgparse(t *testing.T) {
	p, err := parseArgs([]string{"plan", "sonnet:implement=3", "opus=1", "--size", "sonnet=250k", "opus=1m", "--serial", "--tar=80"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.vals["agents"], ","); got != "sonnet:implement=3,opus=1" {
		t.Errorf("agents = %s", got)
	}
	if got := strings.Join(p.vals["size"], ","); got != "sonnet=250k,opus=1m" {
		t.Errorf("--size is greedy: %s", got)
	}
	if !p.flag("serial") || p.float("target", 0) != 80 {
		t.Errorf("serial %v target %v", p.flag("serial"), p.float("target", 0))
	}
	// options may come between the positionals; a negative number is an argument
	p, err = parseArgs([]string{"start", "label", "--kind", "review", "sonnet"})
	if err != nil || *p.str("model") != "sonnet" || *p.str("kind") != "review" {
		t.Errorf("interleaved: %+v %v", p, err)
	}
	if p, err = parseArgs([]string{"log", "-n", "-3"}); err != nil || *p.str("n") != "-3" {
		t.Errorf("negative: %+v %v", p, err)
	}
	if p, err = parseArgs([]string{"log", "-n5"}); err != nil || *p.str("n") != "5" {
		t.Errorf("-n5: %+v %v", p, err)
	}
	if p, err = parseArgs(nil); err != nil || p.cmd != "status" {
		t.Errorf("no args: %+v %v", p, err)
	}
	if p, err = parseArgs([]string{"reset"}); err != nil || p.cmd != "reset list" {
		t.Errorf("bare reset: %+v %v", p, err)
	}
}

func TestTargetEnvironmentVariable(t *testing.T) {
	h := newHarness(t)
	h.seed(h.freshSnap(10, 5))
	env := h.a.lookupEnv
	h.a.lookupEnv = func(k string) (string, bool) {
		if k == "CLAUDE_QUOTA_TARGET" {
			return "80", true
		}
		return env(k)
	}
	_, out, _ := h.run("status")
	if !strings.Contains(out, "(75 pts to the 80% target)") {
		t.Errorf("status: %q", out)
	}
}

// --- helpers ---------------------------------------------------------------------------------

func TestHours(t *testing.T) {
	for _, c := range []struct {
		s    float64
		want string
	}{{-5, "0m"}, {59, "0m"}, {3599, "59m"}, {3600, "1h0m"}, {7380, "2h3m"}, {86400, "1d0h"}, {90000 + 3599, "1d1h"}} {
		if got := hours(c.s); got != c.want {
			t.Errorf("hours(%v) = %s, want %s", c.s, got, c.want)
		}
	}
}

func TestAmount(t *testing.T) {
	for in, want := range map[string]float64{"229k": 229000, "1.5M": 1.5e6, " 42 ": 42, "7": 7} {
		if got, err := amount(in); err != nil || got != want {
			t.Errorf("amount(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := amount("lots"); err == nil {
		t.Error("amount(lots) should fail")
	}
}

func TestParseISO(t *testing.T) {
	loc := time.FixedZone("x", 3600)
	for in, want := range map[string]string{
		"2026-10-22":                "2026-10-22T00:00:00+01:00",
		"2026-10-22T18:00":          "2026-10-22T18:00:00+01:00",
		"2026-10-22 18:00:30":       "2026-10-22T18:00:30+01:00",
		"2026-10-22T18:00:00Z":      "2026-10-22T18:00:00Z",
		"2026-10-22T18:00:00+02:00": "2026-10-22T18:00:00+02:00",
	} {
		got, ok := parseISO(in, loc)
		if !ok || got.Format(time.RFC3339) != want {
			t.Errorf("parseISO(%q) = %v %v, want %s", in, got.Format(time.RFC3339), ok, want)
		}
	}
	for _, bad := range []string{"", "tomorrow", "2026-13-01", "2026-02-30", "2026-10-22T25:00"} {
		if _, ok := parseISO(bad, loc); ok {
			t.Errorf("parseISO(%q) should fail", bad)
		}
	}
}

func TestPyRepr(t *testing.T) {
	for in, want := range map[string]string{"a": "'a'", "it's": `"it's"`, `a"b'c`: `'a"b\'c'`, "x\ny": `'x\ny'`} {
		if got := pyRepr(in); got != want {
			t.Errorf("pyRepr(%q) = %s, want %s", in, got, want)
		}
	}
}
