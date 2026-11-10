package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type runRecord struct {
	ExitCode  int     `json:"exit_code"`
	DurationS float64 `json:"duration_s"`
	TimedOut  bool    `json:"timed_out"`
	Parsed    parsed  `json:"parsed,omitempty"`
	Daemons   int     `json:"-"`
}

type loopResult struct {
	ID             string      `json:"id"`
	Kind           string      `json:"kind"`
	Cmd            string      `json:"cmd"`
	Cwd            string      `json:"cwd"`
	Status         string      `json:"status"`
	Reason         string      `json:"reason,omitempty"`
	ExitCode       *int        `json:"exit_code,omitempty"`
	DurationS      float64     `json:"duration_s,omitempty"`
	TimedOut       bool        `json:"timed_out,omitempty"`
	ExitIsResult   bool        `json:"exit_is_result,omitempty"`
	RealHome       bool        `json:"real_home,omitempty"`
	DaemonsKilled  int         `json:"daemons_killed,omitempty"`
	Parsed         parsed      `json:"parsed,omitempty"`
	Flaky          *bool       `json:"flaky,omitempty"`
	Runs           []runRecord `json:"runs,omitempty"`
	StdoutTail     []string    `json:"stdout_tail,omitempty"`
	StderrTail     []string    `json:"stderr_tail,omitempty"`
	FailureExcerpt []string    `json:"failure_excerpt,omitempty"`
}

type loopResults struct {
	SchemaVersion int            `json:"schema_version"`
	Tool          toolInfo       `json:"tool"`
	Workdir       string         `json:"workdir"`
	StartedAt     string         `json:"started_at"`
	FinishedAt    string         `json:"finished_at"`
	RepeatTests   int            `json:"repeat_tests"`
	EnvNames      []string       `json:"env_names_passed"`
	Summary       map[string]int `json:"summary"`
	Complete      bool           `json:"complete"`
	SandboxHome   string         `json:"sandbox_home"`
	RealHomeLoops []string       `json:"real_home_loops,omitempty"`
	TotalTimeoutS int            `json:"total_timeout_s"`
	TotalTimedOut bool           `json:"total_timeout_hit,omitempty"`
	DaemonsNote   string         `json:"daemons_note,omitempty"`
	Truncated     []string       `json:"truncated,omitempty"`
	Loops         []loopResult   `json:"loops"`
}

type loopsFile struct {
	SchemaVersion int      `json:"schema_version"`
	Repo          string   `json:"repo"`
	Loops         []loop   `json:"loops"`
	Truncated     []string `json:"truncated"`
}

// runOpts are the knobs of a run.
type runOpts struct {
	Repeat       int
	Only         map[string]bool
	Prev         *loopResults
	Getenv       func(string) string
	Now          func() time.Time
	Progress     func(string)
	Checkpoint   func(loopResults) // called with the results so far after every loop
	TotalTimeout time.Duration
}

var reEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// toolchainDirs are tool installations under the real home that a loop may need
// to find its interpreters (version managers, rustup). They are passed by path,
// as read-only-in-practice tool locations; credentials directories never are.
var toolchainDirs = []struct{ Env, Sub string }{
	{"MISE_DATA_DIR", ".local/share/mise"}, {"MISE_CONFIG_DIR", ".config/mise"}, {"RUSTUP_HOME", ".rustup"},
	{"ASDF_DATA_DIR", ".asdf"}, {"NVM_DIR", ".nvm"}, {"PYENV_ROOT", ".pyenv"}, {"RBENV_ROOT", ".rbenv"},
	{"SDKMAN_DIR", ".sdkman"}, {"VOLTA_HOME", ".volta"}, {"GOROOT_FALLBACK_UNUSED", ""},
}

// sandboxHomeFor is the throwaway home of a workdir: <workdir>/../.home-<name>.
func sandboxHomeFor(workdir string) string {
	return filepath.Join(filepath.Dir(workdir), ".home-"+filepath.Base(workdir))
}

// baseEnv is the whole environment a loop starts with, before its extra_env.
// HOME and the XDG directories point into a sandbox next to the workdir, so
// installs and tests cannot read ~/.ssh, ~/.aws, ~/.npmrc or ~/.config, nor
// write caches into the real home. The caller's other variables (which may hold
// real credentials) never pass. With realHome the loop gets the real HOME and
// no XDG overrides: an opt-in for private-registry installs.
func baseEnv(getenv func(string) string, sandbox string, realHome bool) map[string]string {
	env := map[string]string{"CI": "1"}
	for _, k := range []string{"PATH", "TMPDIR", "USER", "SHELL"} {
		if v := getenv(k); v != "" {
			env[k] = v
		}
	}
	real := getenv("HOME")
	if realHome {
		if real != "" {
			env["HOME"] = real
		}
	} else {
		env["HOME"] = sandbox
		env["XDG_CONFIG_HOME"] = sandbox
		env["XDG_CACHE_HOME"] = sandbox
		env["XDG_DATA_HOME"] = sandbox
		for _, t := range toolchainDirs {
			if t.Sub == "" || real == "" {
				continue
			}
			if p := filepath.Join(real, t.Sub); isDir(p) {
				env[t.Env] = p
			}
		}
	}
	env["LANG"] = getenv("LANG")
	if env["LANG"] == "" {
		env["LANG"] = "en_US.UTF-8"
	}
	if env["TMPDIR"] == "" {
		env["TMPDIR"] = os.TempDir()
	}
	return env
}

func envList(m map[string]string) []string {
	var out []string
	for _, k := range sortedKeys(m) {
		out = append(out, k+"="+m[k])
	}
	return out
}

func (a *app) cmdRunLoops(args []string) error {
	fl := flag.NewFlagSet("run-loops", flag.ContinueOnError)
	workdir := fl.String("workdir", "", "directory to run in: an export of the repo (required)")
	out := fl.String("out", "", "results.json to write (required)")
	repeat := fl.Int("repeat-tests", 2, "run each test loop this many times; differing counts mark it flaky")
	only := fl.String("only", "", "comma-separated loop ids to run (even if disabled)")
	totalTimeout := fl.Duration("total-timeout", 45*time.Minute, "stop starting loops after this long in total")
	fl.Usage = func() {
		fmt.Fprintln(a.stderr, "usage: codebase-baseline run-loops <loops.json> --workdir <export-dir> --out results.json [--repeat-tests 2] [--only id,...] [--total-timeout 45m]")
		fl.PrintDefaults()
	}
	pos, err := parseFlags(fl, args, a.stderr)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *workdir == "" || *out == "" {
		fl.Usage()
		return fmt.Errorf("run-loops needs <loops.json>, --workdir and --out")
	}
	var lf loopsFile
	if err := readJSON(pos[0], &lf); err != nil {
		return err
	}
	if !isDir(*workdir) {
		return fmt.Errorf("--workdir %s is not a directory", *workdir)
	}
	if _, err := os.Stat(filepath.Join(*workdir, ".git")); err == nil {
		return fmt.Errorf("--workdir %s holds a .git: run loops in an export (codebase-baseline export), never in a working repo", *workdir)
	}
	if lf.Repo != "" && pathInside(*out, lf.Repo) {
		return fmt.Errorf("--out is inside the analysed repo")
	}
	if *repeat < 1 {
		*repeat = 1
	}
	var onlySet map[string]bool
	if *only != "" {
		onlySet = map[string]bool{}
		for _, id := range strings.Split(*only, ",") {
			onlySet[strings.TrimSpace(id)] = true
		}
		known := map[string]bool{}
		for _, l := range lf.Loops {
			known[l.ID] = true
		}
		for id := range onlySet {
			if !known[id] {
				return fmt.Errorf("--only: no loop with id %q", id)
			}
		}
	}
	var prev *loopResults
	if onlySet != nil {
		var p loopResults
		if err := readJSON(*out, &p); err == nil && len(p.Loops) > 0 {
			prev = &p
		}
	}
	var cpErr error
	res := runLoops(lf, absPath(*workdir), runOpts{Repeat: *repeat, Only: onlySet, Prev: prev, Getenv: os.Getenv, Now: a.now,
		Progress: func(msg string) { fmt.Fprintln(a.stderr, msg) }, TotalTimeout: *totalTimeout,
		Checkpoint: func(p loopResults) {
			if err := writeJSONAtomic(*out, p); err != nil && cpErr == nil {
				cpErr = err
			}
		}})
	if cpErr != nil {
		return cpErr
	}
	if err := writeJSONAtomic(*out, res); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "wrote %s:", *out)
	for _, k := range sortedKeys(res.Summary) {
		fmt.Fprintf(a.stdout, " %s=%d", k, res.Summary[k])
	}
	fmt.Fprintln(a.stdout)
	for _, r := range res.Loops {
		line := fmt.Sprintf("  %-9s %-28s", r.Status, r.ID)
		if r.Parsed != nil {
			line += fmt.Sprintf(" %v", map[string]any(r.Parsed))
		}
		if r.Flaky != nil && *r.Flaky {
			line += " FLAKY"
		}
		if r.Reason != "" {
			line += " (" + r.Reason + ")"
		}
		fmt.Fprintln(a.stdout, line)
	}
	return nil
}

// runLoops runs the selected loops. With only set and a previous results file,
// the selected loops' entries replace their old ones and every other entry is
// kept, so one loop can be rerun alone.
func runLoops(lf loopsFile, workdir string, o runOpts) loopResults {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	progress := o.Progress
	if progress == nil {
		progress = func(string) {}
	}
	if o.Repeat < 1 {
		o.Repeat = 1
	}
	sandbox := sandboxHomeFor(workdir)
	_ = os.MkdirAll(sandbox, 0o755)
	res := loopResults{SchemaVersion: schemaVersion, Tool: currentTool(), Workdir: workdir, StartedAt: now().UTC().Format(time.RFC3339), RepeatTests: o.Repeat,
		Summary: map[string]int{}, SandboxHome: sandbox, TotalTimeoutS: int(o.TotalTimeout.Seconds()), Truncated: lf.Truncated,
		DaemonsNote: "when a loop times out its process group is killed and processes whose command line names the workdir are killed too; daemons started elsewhere (gradle, docker, language servers) may survive"}
	base := baseEnv(o.Getenv, sandbox, false)
	names := map[string]bool{}
	for k := range base {
		names[k] = true
	}
	failed := map[string]string{}
	old := map[string]loopResult{}
	if o.Prev != nil {
		for _, p := range o.Prev.Loops {
			old[p.ID] = p
			if !o.Only[p.ID] && (p.Status == "failed" || p.Status == "timeout" || p.Status == "tool-missing") {
				failed[p.ID] = p.Status
			}
		}
		res.StartedAt = o.Prev.StartedAt
		for _, n := range o.Prev.EnvNames {
			names[n] = true
		}
	}
	var deadline time.Time
	if o.TotalTimeout > 0 {
		deadline = time.Now().Add(o.TotalTimeout)
	}
	seen := map[string]bool{}
	snapshotOf := func(i int) loopResults {
		p := res
		p.Summary = map[string]int{}
		p.Loops = append([]loopResult(nil), res.Loops...)
		if o.Prev != nil { // loops not reached yet keep their earlier entries
			for _, l := range lf.Loops[i+1:] {
				if e, ok := old[l.ID]; ok {
					p.Loops = append(p.Loops, e)
				}
			}
		}
		for _, l := range p.Loops {
			p.Summary[l.Status]++
		}
		p.EnvNames = sortedKeys(names)
		p.Complete = false
		return p
	}
	for i, l := range lf.Loops {
		seen[l.ID] = true
		r := loopResult{ID: l.ID, Kind: l.Kind, Cmd: l.Cmd, Cwd: l.Cwd}
		switch {
		case o.Only != nil && !o.Only[l.ID]:
			if p, ok := old[l.ID]; ok {
				r = p
			} else {
				r.Status = "not_selected"
			}
		case o.Only == nil && !l.Enabled:
			r.Status = "disabled"
			r.Reason = "disabled in loops.json"
			if len(l.Needs) > 0 {
				r.Reason += " (needs: " + strings.Join(l.Needs, ", ") + ")"
			}
		case !deadline.IsZero() && !time.Now().Before(deadline):
			r.Status, r.Reason = "skipped", fmt.Sprintf("total timeout of %s reached before this loop started", o.TotalTimeout)
			res.TotalTimedOut = true
		default:
			var left time.Duration
			if !deadline.IsZero() {
				left = time.Until(deadline)
			}
			env := base
			if l.RealHome {
				env = baseEnv(o.Getenv, sandbox, true)
				res.RealHomeLoops = append(res.RealHomeLoops, l.ID)
			}
			r = runOne(l, workdir, o.Repeat, env, failed, left, progress)
			for k := range l.ExtraEnv {
				names[k] = true
			}
		}
		if r.Status == "failed" || r.Status == "timeout" || r.Status == "tool-missing" {
			failed[l.ID] = r.Status
		}
		res.Summary[r.Status]++
		res.Loops = append(res.Loops, r)
		if o.Checkpoint != nil {
			o.Checkpoint(snapshotOf(i))
		}
	}
	// Entries of loops that are no longer in loops.json stay, so nothing is lost.
	if o.Prev != nil {
		for _, p := range o.Prev.Loops {
			if !seen[p.ID] {
				res.Summary[p.Status]++
				res.Loops = append(res.Loops, p)
			}
		}
	}
	res.EnvNames = sortedKeys(names)
	res.FinishedAt = now().UTC().Format(time.RFC3339)
	res.Complete = true
	return res
}

var reToolMissing = regexp.MustCompile(`(?i)command not found|is not recognized as an internal or external command|executable file not found|: not found$|no such file or directory.*\b(node|npm|pnpm|yarn|python3?|pip|go|cargo|mvn|gradle|dotnet|bundle|ruby|composer|php)\b`)

// runOne runs one loop. left caps its timeout by what remains of the run's total.
func runOne(l loop, workdir string, repeat int, base map[string]string, failed map[string]string, left time.Duration, progress func(string)) loopResult {
	r := loopResult{ID: l.ID, Kind: l.Kind, Cwd: l.Cwd, RealHome: l.RealHome}
	secretValues := l.ExtraEnv
	r.Cmd = redactSecrets(l.Cmd, secretValues)
	for _, req := range l.Requires {
		if st, bad := failed[req]; bad {
			r.Status, r.Reason = "skipped", fmt.Sprintf("requires %s, which %s", req, st)
			return r
		}
	}
	rel := filepath.Clean(filepath.FromSlash(l.Cwd))
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		r.Status, r.Reason = "skipped", "cwd leaves the workdir"
		return r
	}
	dir := filepath.Join(workdir, rel)
	if !isDir(dir) {
		r.Status, r.Reason = "skipped", "cwd does not exist in the workdir"
		return r
	}
	env := map[string]string{}
	for k, v := range base {
		env[k] = v
	}
	for k, v := range l.ExtraEnv {
		if !reEnvName.MatchString(k) {
			r.Status, r.Reason = "skipped", "extra_env has an invalid name"
			return r
		}
		env[k] = v
	}
	timeout := time.Duration(l.TimeoutS) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	if left > 0 && left < timeout {
		timeout = left
	}
	runs := 1
	if l.Kind == "test" {
		runs = repeat
	}
	for i := 0; i < runs; i++ {
		progress(fmt.Sprintf("run %s (%s) in %s, run %d/%d", l.ID, r.Cmd, l.Cwd, i+1, runs))
		rec, stdout, stderr := execOnce(l.Cmd, dir, workdir, env, timeout, l.Kind)
		r.Runs = append(r.Runs, rec)
		r.DaemonsKilled += rec.Daemons
		if i == 0 {
			r.ExitCode = &rec.ExitCode
			r.DurationS, r.TimedOut, r.Parsed = rec.DurationS, rec.TimedOut, rec.Parsed
			r.StdoutTail = tailRedacted(stdout, secretValues)
			r.StderrTail = tailRedacted(stderr, secretValues)
			if rec.ExitCode != 0 {
				r.FailureExcerpt = redactLines(failureExcerpt(stdout+"\n"+stderr, 30), secretValues)
			}
			if rec.ExitCode == 127 || (rec.ExitCode != 0 && rec.Parsed == nil && reToolMissing.MatchString(stripANSI(stderr+"\n"+stdout))) {
				r.Status = "tool-missing"
				r.Reason = "the loop's tool is not installed or not on PATH (exit " + fmt.Sprint(rec.ExitCode) + "); this is not a failure of the code"
				r.Runs = nil
				return r
			}
		}
		if rec.TimedOut {
			break
		}
	}
	switch {
	case r.TimedOut:
		r.Status = "timeout"
	case *r.ExitCode == 0:
		r.Status = "passed"
	default:
		r.Status = "failed"
		// An audit that finds advisories exits non-zero: that is its result. A
		// parsed error (no network, no lockfile) is still a broken loop.
		if l.ExitIsResult {
			r.ExitIsResult = true
			if _, broken := r.Parsed["error"]; !broken {
				r.Status = "findings"
			}
		}
	}
	if l.Kind == "test" && len(r.Runs) > 1 {
		flaky := false
		for _, o := range r.Runs[1:] {
			if !sameOutcome(r.Runs[0], o) {
				flaky = true
			}
		}
		r.Flaky = &flaky
	}
	if len(r.Runs) == 1 {
		r.Runs = nil
	}
	return r
}

// writeJSONAtomic writes next to the target and renames, so a killed run never
// leaves half a file.
func writeJSONAtomic(p string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func tailRedacted(text string, env map[string]string) []string {
	return redactLines(tailLines(text, 80), env)
}

func redactLines(lines []string, env map[string]string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = redactSecrets(l, env)
	}
	return out
}

// sameOutcome compares two runs of a test loop by exit code and the counts.
func sameOutcome(a, b runRecord) bool {
	if a.ExitCode != b.ExitCode || a.TimedOut != b.TimedOut {
		return false
	}
	for _, k := range []string{"passed", "failed", "skipped", "total"} {
		if fmt.Sprint(a.Parsed[k]) != fmt.Sprint(b.Parsed[k]) {
			return false
		}
	}
	return true
}

// execOnce runs one command through /bin/sh in an empty environment and
// returns what it printed. The output goes to temp files outside the workdir so
// a chatty test run can't fill memory or the pipe.
func execOnce(cmdline, dir, workdir string, env map[string]string, timeout time.Duration, kind string) (runRecord, string, string) {
	rec := runRecord{}
	so, err1 := os.CreateTemp("", "cb-out-")
	se, err2 := os.CreateTemp("", "cb-err-")
	if err1 != nil || err2 != nil {
		rec.ExitCode = -1
		return rec, "", "cannot create temp files for output"
	}
	defer os.Remove(so.Name())
	defer os.Remove(se.Name())
	defer so.Close()
	defer se.Close()
	c := exec.Command("/bin/sh", "-c", cmdline)
	c.Dir = dir
	c.Env = envList(env)
	c.Stdout, c.Stderr = so, se
	c.Stdin = nil
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	start := time.Now()
	if err := c.Start(); err != nil {
		rec.ExitCode = -1
		return rec, "", err.Error()
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	var werr error
	select {
	case werr = <-done:
	case <-time.After(timeout):
		rec.TimedOut = true
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		werr = <-done
		rec.Daemons = killDaemons(workdir)
	}
	rec.DurationS = round2(time.Since(start).Seconds())
	if werr == nil {
		rec.ExitCode = 0
	} else if ee, ok := werr.(*exec.ExitError); ok {
		rec.ExitCode = ee.ExitCode()
	} else {
		rec.ExitCode = -1
	}
	if rec.TimedOut {
		rec.ExitCode = -1
	}
	stdout := readTail(so, 24<<20)
	stderr := readTail(se, 24<<20)
	rec.Parsed = parseOutput(kind, cmdline, stdout+"\n"+stderr, rec.ExitCode)
	return rec, stdout, stderr
}

// readTail returns up to max bytes from the end of f.
func readTail(f *os.File, max int64) string {
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	off := int64(0)
	if st.Size() > max {
		off = st.Size() - max
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return ""
	}
	b, _ := io.ReadAll(f)
	return string(b)
}

// killDaemons is a best effort after a timeout: it kills processes whose command
// line names the workdir (dev servers and watchers started by the loop that left
// their process group). It cannot reach daemons that never mention the workdir.
func killDaemons(workdir string) int {
	out, err := exec.Command("pgrep", "-f", "--", workdir).Output()
	if err != nil {
		return 0
	}
	n := 0
	for _, f := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(f)
		if err != nil || pid == os.Getpid() || pid == os.Getppid() {
			continue
		}
		if syscall.Kill(pid, syscall.SIGKILL) == nil {
			n++
		}
	}
	return n
}
