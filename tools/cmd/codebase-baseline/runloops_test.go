package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeEnv(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestRunLoopsEmptyEnvironment(t *testing.T) {
	dir := t.TempDir()
	lf := loopsFile{Loops: []loop{
		{ID: "env", Kind: "other", Cmd: "env | sort", Cwd: ".", Enabled: true, TimeoutS: 30, ExtraEnv: map[string]string{"DATABASE_URL": "postgres://dummy-user:dummy-pass-123@localhost/x", "FEATURE": "on"}},
	}}
	res := runLoops(lf, dir, runOpts{Repeat: 1, Now: timeNow, Getenv: fakeEnv(map[string]string{"PATH": os.Getenv("PATH"), "HOME": "/home/x", "USER": "u", "SHELL": "/bin/sh", "AWS_SECRET_ACCESS_KEY": "do-not-leak"})})
	r := res.Loops[0]
	out := strings.Join(r.StdoutTail, "\n")
	if strings.Contains(out, "AWS_SECRET_ACCESS_KEY") || strings.Contains(out, "do-not-leak") {
		t.Fatalf("caller's environment leaked into the loop:\n%s", out)
	}
	sandbox := sandboxHomeFor(dir)
	if strings.Contains(out, "HOME=/home/x") {
		t.Errorf("the real HOME must not reach the loop:\n%s", out)
	}
	for _, want := range []string{"CI=1", "HOME=" + sandbox, "XDG_CONFIG_HOME=" + sandbox, "XDG_CACHE_HOME=" + sandbox, "XDG_DATA_HOME=" + sandbox, "FEATURE=on", "LANG="} {
		if !strings.Contains(out, want) {
			t.Errorf("env lacks %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "dummy-pass-123") {
		t.Errorf("extra_env value printed unredacted:\n%s", out)
	}
	if strings.Join(res.EnvNames, ",") != "CI,DATABASE_URL,FEATURE,HOME,LANG,PATH,SHELL,TMPDIR,USER,XDG_CACHE_HOME,XDG_CONFIG_HOME,XDG_DATA_HOME" {
		t.Errorf("env names = %v", res.EnvNames)
	}
}

func TestRunLoopsFlakyAndInstallDependants(t *testing.T) {
	dir := t.TempDir()
	// Passes on the first run and fails one test on the second.
	flaky := `n=$(cat count 2>/dev/null || echo 0); n=$((n+1)); echo $n > count; if [ $n -eq 1 ]; then echo "# tests 4"; echo "# pass 4"; echo "# fail 0"; else echo "# tests 4"; echo "# pass 3"; echo "# fail 1"; exit 1; fi`
	stable := `echo "# tests 2"; echo "# pass 2"; echo "# fail 0"`
	lf := loopsFile{Loops: []loop{
		{ID: "install", Kind: "install", Cmd: "exit 3", Cwd: ".", Enabled: true, TimeoutS: 30},
		{ID: "needs-install", Kind: "test", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 30, Requires: []string{"install"}},
		{ID: "flaky", Kind: "test", Cmd: flaky, Cwd: ".", Enabled: true, TimeoutS: 30},
		{ID: "stable", Kind: "test", Cmd: stable, Cwd: ".", Enabled: true, TimeoutS: 30},
		{ID: "off", Kind: "e2e", Cmd: "true", Cwd: ".", Enabled: false},
		{ID: "slow", Kind: "build", Cmd: "sleep 5", Cwd: ".", Enabled: true, TimeoutS: 1},
		{ID: "escape", Kind: "build", Cmd: "true", Cwd: "../..", Enabled: true, TimeoutS: 5},
	}}
	start := time.Now()
	res := runLoops(lf, dir, runOpts{Repeat: 2, Now: timeNow, Getenv: fakeEnv(map[string]string{"PATH": os.Getenv("PATH")})})
	if time.Since(start) > 4*time.Second {
		t.Errorf("timeout did not kill the sleep promptly: %s", time.Since(start))
	}
	by := map[string]loopResult{}
	for _, r := range res.Loops {
		by[r.ID] = r
	}
	if by["install"].Status != "failed" {
		t.Errorf("install = %+v", by["install"])
	}
	if r := by["needs-install"]; r.Status != "skipped" || !strings.Contains(r.Reason, "install") {
		t.Errorf("dependant = %+v", r)
	}
	if r := by["flaky"]; r.Flaky == nil || !*r.Flaky || len(r.Runs) != 2 || r.Parsed["passed"] != 4 {
		t.Errorf("flaky = %+v", r)
	}
	if r := by["stable"]; r.Flaky == nil || *r.Flaky || r.Status != "passed" || r.Parsed["total"] != 2 {
		t.Errorf("stable = %+v", r)
	}
	if by["off"].Status != "disabled" {
		t.Errorf("off = %+v", by["off"])
	}
	if r := by["slow"]; r.Status != "timeout" || !r.TimedOut {
		t.Errorf("slow = %+v", r)
	}
	if r := by["escape"]; r.Status != "skipped" || !strings.Contains(r.Reason, "workdir") {
		t.Errorf("escape = %+v", r)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "count")); err != nil || strings.TrimSpace(string(data)) != "2" {
		t.Errorf("flaky loop should have run twice: %q %v", data, err)
	}
}

func TestRunLoopsContainment(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "work")
	_ = os.MkdirAll(dir, 0o755)
	realHome := t.TempDir()
	_ = os.WriteFile(filepath.Join(realHome, ".npmrc"), []byte("//registry/:_authToken=abc"), 0o600)
	env := fakeEnv(map[string]string{"PATH": os.Getenv("PATH"), "HOME": realHome})
	lf := loopsFile{Truncated: []string{"python: 50 projects found, first 40 kept"}, Loops: []loop{
		{ID: "home", Kind: "other", Cmd: `echo "HOME=$HOME"; ls -a "$HOME" | grep -c npmrc`, Cwd: ".", Enabled: true, TimeoutS: 30},
		{ID: "private", Kind: "install", Cmd: `echo "HOME=$HOME"; ls -a "$HOME" | grep -c npmrc`, Cwd: ".", Enabled: true, TimeoutS: 30, RealHome: true},
		{ID: "missing", Kind: "lint", Cmd: "definitely-not-a-real-tool-xyz --version", Cwd: ".", Enabled: true, TimeoutS: 30},
		{ID: "after", Kind: "build", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 30, Requires: []string{"missing"}},
	}}
	var checkpoints []loopResults
	res := runLoops(lf, dir, runOpts{Repeat: 1, Now: timeNow, Getenv: env, Checkpoint: func(p loopResults) { checkpoints = append(checkpoints, p) }})
	sandbox := sandboxHomeFor(dir)
	if !isDir(sandbox) || filepath.Base(sandbox) != ".home-work" {
		t.Errorf("sandbox home = %s", sandbox)
	}
	by := map[string]loopResult{}
	for _, r := range res.Loops {
		by[r.ID] = r
	}
	if out := strings.Join(by["home"].StdoutTail, "\n"); !strings.Contains(out, "HOME="+sandbox) || strings.Contains(out, "\n1") {
		t.Errorf("sandboxed loop sees %q", out)
	}
	if out := strings.Join(by["private"].StdoutTail, "\n"); !strings.Contains(out, "HOME="+realHome) || !by["private"].RealHome {
		t.Errorf("real_home loop sees %q (flag %v)", out, by["private"].RealHome)
	}
	if strings.Join(res.RealHomeLoops, ",") != "private" {
		t.Errorf("real home loops = %v", res.RealHomeLoops)
	}
	if by["missing"].Status != "tool-missing" || by["after"].Status != "skipped" {
		t.Errorf("missing = %+v after = %+v", by["missing"], by["after"])
	}
	if len(checkpoints) != 4 || checkpoints[0].Complete || len(checkpoints[0].Loops) != 1 || len(checkpoints[2].Loops) != 3 || !res.Complete {
		t.Errorf("checkpoints = %d, first complete=%v loops=%d; final complete=%v", len(checkpoints), checkpoints[0].Complete, len(checkpoints[0].Loops), res.Complete)
	}
	if len(res.Truncated) != 1 {
		t.Errorf("truncated not carried: %v", res.Truncated)
	}
}

func TestRunLoopsTotalTimeout(t *testing.T) {
	dir := t.TempDir()
	env := fakeEnv(map[string]string{"PATH": os.Getenv("PATH")})
	lf := loopsFile{Loops: []loop{
		{ID: "slow", Kind: "build", Cmd: "sleep 5", Cwd: ".", Enabled: true, TimeoutS: 60},
		{ID: "never", Kind: "build", Cmd: "true", Cwd: ".", Enabled: true, TimeoutS: 60},
	}}
	start := time.Now()
	res := runLoops(lf, dir, runOpts{Repeat: 1, Now: timeNow, Getenv: env, TotalTimeout: 1 * time.Second})
	if time.Since(start) > 4*time.Second {
		t.Errorf("total timeout not enforced: %s", time.Since(start))
	}
	if res.Loops[0].Status != "timeout" || res.Loops[1].Status != "skipped" || !res.TotalTimedOut {
		t.Errorf("results = %+v / %+v hit=%v", res.Loops[0].Status, res.Loops[1], res.TotalTimedOut)
	}
}

func TestWriteJSONAtomic(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "r.json")
	if err := writeJSONAtomic(p, map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p + ".tmp"); err == nil {
		t.Error("temp file left behind")
	}
	var m map[string]int
	if err := readJSON(p, &m); err != nil || m["a"] != 1 {
		t.Errorf("%v %v", m, err)
	}
}
