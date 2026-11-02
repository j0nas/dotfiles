// Command claude-quota reads the account's Claude usage, logs it, learns what agents cost
// and plans spend. The 5-hour and weekly utilisation come from /api/oauth/usage, the endpoint
// /usage and the statusline read, with the OAuth token from the macOS keychain (else
// ~/.claude/.credentials.json). Every call appends a snapshot to
// $XDG_STATE_HOME/claude-quota/log.jsonl, and each agent is logged from `start` (model, kind of
// task) to `end` (the tokens its notification reported, and what became of its work). `learn`
// fits, over every pair of consecutive snapshots, how much of each window a million tokens of
// each model costs, spreading an agent's tokens evenly over its run, plus a per-hour baseline
// for the main session and whatever else ran. The utilisation is whole percents and the 5-hour
// window moves several times faster than the week, so the weekly rates come from the 5-hour
// ones times the observed week-to-5h ratio once there's enough of it. `plan` prices the work
// still to do against the budget left. A limit reset (Settings > Usage) refills the week, the
// 5-hour window or both; logged with `reset add`, it counts as a full week more to spend until
// it expires, and its redemption is read off the snapshots: a window drops while its reset time
// stays put.
//
// The 5-hour window is a hard limit: an agent running when it's reached is stopped mid-task.
// `watch`, run in the background beside them, exits once the window will reach its limit before
// it resets, so the session can have them commit their work and stop. `suspend` logs each one
// paused, `queue` holds work not started yet, and `wait` sleeps until the window resets and
// then hands both out again. See `claude-quota --help`.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const usageURL = "https://api.anthropic.com/api/oauth/usage"

// usageResp is the part of the usage endpoint's answer we read.
type usageResp struct {
	FiveHour *struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    *string `json:"resets_at"`
	} `json:"five_hour"`
	SevenDay *struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    *string `json:"resets_at"`
	} `json:"seven_day"`
	Limits []struct {
		Kind    string   `json:"kind"`
		Percent *float64 `json:"percent"`
		Scope   *struct {
			Model *struct {
				DisplayName *string `json:"display_name"`
			} `json:"model"`
		} `json:"scope"`
	} `json:"limits"`
}

type app struct {
	stdout, stderr io.Writer
	lookupEnv      func(string) (string, bool)
	home           string
	now            func() time.Time
	sleep          func(time.Duration)
	token          func() (string, error)
	fetch          func(token string) (*usageResp, error)
	randHex        func() string
	target         float64 // CLAUDE_QUOTA_TARGET
}

// exitError ends the run with a message on stderr and a code.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func die(format string, a ...any) error {
	return &exitError{1, "claude-quota: " + fmt.Sprintf(format, a...)}
}

func main() {
	home, _ := os.UserHomeDir()
	a := &app{
		stdout: os.Stdout, stderr: os.Stderr,
		lookupEnv: os.LookupEnv, home: home, now: time.Now, sleep: time.Sleep,
		randHex: func() string {
			b := make([]byte, 2)
			_, _ = rand.Read(b)
			return hex.EncodeToString(b)
		},
		fetch: fetchUsage,
	}
	a.token = a.credentials
	os.Exit(a.run(os.Args[1:]))
}

func (a *app) run(args []string) int {
	a.target = defaultTarget
	if v, ok := a.lookupEnv("CLAUDE_QUOTA_TARGET"); ok {
		f, ok := pyFloat(v)
		if !ok {
			fmt.Fprintf(a.stderr, "claude-quota: CLAUDE_QUOTA_TARGET is not a number: %s\n", pyRepr(v))
			return 1
		}
		a.target = f
	}
	p, perr := parseArgs(args)
	if perr != nil {
		io.WriteString(a.stdout, perr.out)
		io.WriteString(a.stderr, perr.err)
		return perr.code
	}
	if err := a.dispatch(p); err != nil {
		if ee, ok := err.(*exitError); ok {
			fmt.Fprintln(a.stderr, ee.msg)
			return ee.code
		}
		fmt.Fprintln(a.stderr, "claude-quota:", err)
		return 1
	}
	return 0
}

func (a *app) dispatch(p *parsed) error {
	switch p.cmd {
	case "status":
		return a.cmdStatus(p)
	case "start":
		return a.cmdStart(p)
	case "end":
		return a.cmdEnd(p)
	case "plan":
		return a.cmdPlan(p)
	case "learn":
		return a.cmdLearn()
	case "guard":
		return a.cmdGuard(p)
	case "watch":
		return a.cmdWatch(p)
	case "suspend":
		return a.cmdSuspend(p)
	case "queue add":
		return a.cmdQueueAdd(p)
	case "queue drop":
		return a.cmdQueueDrop(p)
	case "queue list":
		return a.cmdQueueList()
	case "wait":
		return a.cmdWait(p)
	case "resume":
		return a.handOut(p.flag("dry_run"))
	case "reset add":
		return a.cmdResetAdd(p)
	case "reset used":
		return a.cmdResetUsed(p)
	case "reset list":
		return a.cmdResetList()
	case "log":
		return a.cmdLog(p)
	}
	return die("unknown command %q", p.cmd)
}

func (a *app) printf(format string, v ...any) { fmt.Fprintf(a.stdout, format, v...) }
func (a *app) println(s string)               { fmt.Fprintln(a.stdout, s) }

func (a *app) time() float64 { return float64(a.now().UnixMicro()) / 1e6 }

func at(t float64) time.Time { return time.UnixMicro(int64(math.Round(t * 1e6))) }

// --- reading ------------------------------------------------------------------------------

func (a *app) credentials() (string, error) {
	raw := ""
	if runtime.GOOS == "darwin" {
		out, _ := exec.Command("security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
		raw = strings.TrimSpace(string(out))
	}
	if raw == "" {
		if b, err := os.ReadFile(filepath.Join(a.home, ".claude/.credentials.json")); err == nil {
			raw = string(b)
		}
	}
	if raw == "" {
		return "", die("no Claude Code OAuth credentials found")
	}
	var c struct {
		O struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil || c.O.AccessToken == "" {
		return "", die("the Claude Code credentials hold no OAuth access token")
	}
	return c.O.AccessToken, nil
}

func fetchUsage(token string) (*usageResp, error) {
	req, _ := http.NewRequest("GET", usageURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP Error %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	var u usageResp
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, &decodeError{err}
	}
	return &u, nil
}

// decodeError marks an answer that arrived but wasn't the usage JSON (a bug, not an outage).
type decodeError struct{ error }

var isoRe = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2})(?::(\d{2})(?::(\d{2})(?:[.,](\d+))?)?)?(Z|[+-]\d{2}(?::?\d{2})?)?)?$`)

// parseISO is datetime.fromisoformat for the shapes Settings and the endpoint give: a date,
// or a date and time, with an optional zone; naive times are in loc.
func parseISO(s string, loc *time.Location) (time.Time, bool) {
	m := isoRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
	nanos := 0
	if m[7] != "" {
		f := (m[7] + "000000000")[:9]
		nanos, _ = strconv.Atoi(f)
	}
	if z := m[8]; z != "" {
		if z == "Z" {
			loc = time.UTC
		} else {
			d := strings.ReplaceAll(z[1:], ":", "")
			h, _ := strconv.Atoi(d[:2])
			mi := 0
			if len(d) > 2 {
				mi, _ = strconv.Atoi(d[2:])
			}
			off := h*3600 + mi*60
			if z[0] == '-' {
				off = -off
			}
			loc = time.FixedZone("", off)
		}
	}
	t := time.Date(n(1), time.Month(n(2)), n(3), n(4), n(5), n(6), nanos, loc)
	if t.Year() != n(1) || int(t.Month()) != n(2) || t.Day() != n(3) || n(4) > 23 || n(5) > 59 || n(6) > 59 {
		return time.Time{}, false
	}
	return t, true
}

func epoch(iso *string) (*float64, error) {
	if iso == nil || *iso == "" {
		return nil, nil
	}
	t, ok := parseISO(*iso, time.UTC)
	if !ok {
		return nil, fmt.Errorf("unreadable reset time %q", *iso)
	}
	v := float64(t.UnixMicro()) / 1e6
	return &v, nil
}

// snapshot is a fresh reading, logged; the last one if it's under maxAge old (the endpoint is
// rate-limited: a burst of calls gets 429s) or if the endpoint can't be reached.
func (a *app) snapshot(maxAge float64) (Entry, error) {
	log, err := a.entries()
	if err != nil {
		return Entry{}, err
	}
	var last *Entry
	for i := len(log) - 1; i >= 0; i-- {
		if log[i].Kind == "snap" {
			last = &log[i]
			break
		}
	}
	if last != nil && a.time()-last.T < maxAge {
		return started(*last), nil
	}
	tok, err := a.token()
	if err != nil {
		return Entry{}, err
	}
	u, err := a.fetch(tok)
	if err != nil {
		if _, bad := err.(*decodeError); bad {
			return Entry{}, err
		}
		if last == nil {
			return Entry{}, die("can't read the usage (%v) and there's no earlier reading", err)
		}
		fmt.Fprintf(a.stderr, "claude-quota: can't read the usage (%v); using the reading from %s ago\n", err, hours(a.time()-last.T))
		return started(*last), nil
	}
	if u.FiveHour == nil || u.SevenDay == nil {
		return Entry{}, die("the usage answer has no five_hour or seven_day window")
	}
	var scoped Scoped
	scoped.Vals = map[string]*float64{}
	for _, lim := range u.Limits {
		if lim.Kind != "weekly_scoped" {
			continue
		}
		name := "?"
		if lim.Scope != nil && lim.Scope.Model != nil && lim.Scope.Model.DisplayName != nil {
			name = *lim.Scope.Model.DisplayName
		}
		scoped.set(name, lim.Percent)
	}
	fr, err := epoch(u.FiveHour.ResetsAt)
	if err != nil {
		return Entry{}, die("%v", err)
	}
	wr, err := epoch(u.SevenDay.ResetsAt)
	if err != nil {
		return Entry{}, die("%v", err)
	}
	s := Entry{T: a.time(), Kind: "snap", Five: u.FiveHour.Utilization, FiveReset: fr, Week: u.SevenDay.Utilization, WeekReset: wr, Scoped: scoped}
	if err := a.appendEntry(map[string]any{"t": s.T, "kind": "snap", "five": s.Five, "five_reset": fr, "week": s.Week, "week_reset": wr, "scoped": scoped}); err != nil {
		return Entry{}, err
	}
	return started(s), nil
}

// --- parsing ------------------------------------------------------------------------------

// amount parses "229k" or "1.5m" or a plain number.
func amount(s string) (float64, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "k"):
		mult, s = 1e3, s[:len(s)-1]
	case strings.HasSuffix(s, "m"):
		mult, s = 1e6, s[:len(s)-1]
	}
	f, ok := pyFloat(s)
	if !ok {
		return 0, die("can't read %s as an amount of tokens", pyRepr(s))
	}
	return f * mult, nil
}

func modelOf(s string) (string, error) {
	m := strings.ToLower(strings.TrimSpace(s))
	if modelIndex(m) < 0 {
		return "", die("model must be one of ('haiku', 'sonnet', 'opus', 'fable'), got %s", pyRepr(s))
	}
	return m, nil
}

func hours(seconds float64) string {
	seconds = max(0, seconds)
	switch {
	case seconds >= 86400:
		return fmt.Sprintf("%dd%dh", int(math.Floor(seconds/86400)), int(math.Floor(math.Mod(seconds, 86400)/3600)))
	case seconds >= 3600:
		return fmt.Sprintf("%dh%dm", int(math.Floor(seconds/3600)), int(math.Floor(math.Mod(seconds, 3600)/60)))
	}
	return fmt.Sprintf("%dm", int(math.Floor(seconds/60)))
}

func atClock(t float64) string { return at(t).Format("15:04") }

func (a *app) newID(prefix string) string {
	return prefix + a.now().UTC().Format("0102-150405-") + a.randHex()
}

func advise(state string, s Entry) string {
	wake := atClock(*s.FiveReset + waitMargin*60)
	then := fmt.Sprintf("then run `claude-quota wait` in the background: it exits at the reset (~%s) with what to resume, which wakes the session.", wake)
	switch state {
	case "suspend":
		return "Tell each running agent to commit its work in progress and stop at the next safe point, saying where it was. Log each with `claude-quota suspend <id> --ref <agent> --note \"…\"`, queue anything not started (`claude-quota queue add`), " + then
	case "hold":
		return "Start nothing more in this window: queue it (`claude-quota queue add`), " + then
	case "limited":
		return "Agents still running are stopped by the limit: log each with `claude-quota suspend <id> --ref <agent> --note \"…\"`, " + then
	}
	return ""
}

// --- commands -----------------------------------------------------------------------------

func (a *app) showStatus(s Entry, target float64) error {
	log, err := a.entries()
	if err != nil {
		return err
	}
	now := s.T
	a.printf("5h    %.0f%%  resets in %s   (%.0f pts to the %.0f%% cap)\n", s.Five, hours(*s.FiveReset-now), max(0, fiveCap-s.Five), fiveCap)
	a.printf("week  %.0f%%  resets in %s   (%.0f pts to the %.0f%% target)\n", s.Week, hours(*s.WeekReset-now), max(0, target-s.Week), target)
	for _, name := range s.Scoped.Keys {
		if pct := s.Scoped.Vals[name]; pct != nil && *pct != 0 {
			a.printf("      %s weekly %.0f%%\n", name, *pct)
		}
	}
	for _, r := range liveResets(log, now) {
		name := map[string]string{"week": "week", "five": "5h", "both": "week + 5h"}[r.Window]
		left := r.Expires - now
		lvl := s.Week
		if r.Window == "five" {
			lvl = s.Five
		}
		need := 100 - lvl
		line := fmt.Sprintf("reset %s  a %s reset, expires in %s (%s), not redeemed", r.ID, name, hours(left), at(r.Expires).Format("Jan 02 15:04"))
		if n := str(r.Note); n != "" {
			line += ": " + n
		}
		a.println(line)
		if r.Window != "five" {
			per := need / max(left/(5*3600), 1)
			a.printf("      worth up to %.0f pts if the week reaches its limit first: ≈ %.0f week pts per 5-hour window until then. Redeem it in Settings > Usage (web or desktop app).\n", need, per)
		}
		if left < 24*3600 {
			a.println("      It lapses within a day: redeem it before then, near the limit or not.")
		}
	}
	for _, ag := range agents(log).values() {
		if ag.End == 0 {
			ran := hours(active(ag, ag.T, now))
			paused := ""
			if suspended(ag) {
				paused = ", suspended " + hours(now-ag.Paused[len(ag.Paused)-1].from) + " ago"
			}
			a.printf("open  %s  %s:%s  %s  (ran %s%s)\n", ag.ID, ag.Model, ag.task(), ag.Label, ran, paused)
		}
	}
	for _, q := range queued(log) {
		a.printf("queued %s  %s:%s  %s\n", q.ID, q.Model, q.task(), q.Label)
	}
	return nil
}

func (a *app) cmdStatus(p *parsed) error {
	s, err := a.snapshot(fresh)
	if err != nil {
		return err
	}
	if err := a.showStatus(s, p.float("target", a.target)); err != nil {
		return err
	}
	log, err := a.entries()
	if err != nil {
		return err
	}
	if len(running(log)) > 0 {
		state, why := learn(log).verdict(log, s, suspendAt, lead)
		a.printf("5h    %s: %s\n", state, why)
		if state != "ok" {
			a.printf("      %s\n", advise(state, s))
		}
	}
	return nil
}
